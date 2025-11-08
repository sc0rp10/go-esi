package esi

import (
	"container/list"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	defaultTTL      = 300 // 5 minutes
	maxCacheEntries = 1000
)

type cacheEntry struct {
	data      []byte
	expiresAt time.Time
	url       string
}

type fragmentCache struct {
	mu      sync.RWMutex
	entries map[string]*list.Element
	lru     *list.List
}

var cache = &fragmentCache{
	entries: make(map[string]*list.Element),
	lru:     list.New(),
}

// Get retrieves a cached fragment if it exists and is not expired
func (c *fragmentCache) Get(url string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	elem, ok := c.entries[url]
	if !ok {
		return nil, false
	}

	entry := elem.Value.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		// Expired, will be cleaned up by Put
		return nil, false
	}

	// Move to front (most recently used)
	c.lru.MoveToFront(elem)

	return entry.data, true
}

// Put stores a fragment in cache with TTL parsed from response headers
func (c *fragmentCache) Put(url string, data []byte, resp *http.Response) {
	ttl := parseTTL(resp)
	if ttl == 0 {
		// Don't cache if TTL is 0
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Update existing entry
	if elem, ok := c.entries[url]; ok {
		entry := elem.Value.(*cacheEntry)
		entry.data = data
		entry.expiresAt = time.Now().Add(time.Duration(ttl) * time.Second)
		c.lru.MoveToFront(elem)
		return
	}

	// Add new entry
	entry := &cacheEntry{
		data:      data,
		expiresAt: time.Now().Add(time.Duration(ttl) * time.Second),
		url:       url,
	}

	elem := c.lru.PushFront(entry)
	c.entries[url] = elem

	// Evict oldest entries if cache is full
	for c.lru.Len() > maxCacheEntries {
		oldest := c.lru.Back()
		if oldest != nil {
			c.lru.Remove(oldest)
			oldEntry := oldest.Value.(*cacheEntry)
			delete(c.entries, oldEntry.url)

			if logger != nil {
				logger.Debug("Cache evicted LRU entry", zap.String("url", oldEntry.url))
			}
		}
	}
}

// parseTTL extracts TTL from Cache-Control header, returns defaultTTL if not found
func parseTTL(resp *http.Response) int {
	if resp == nil {
		return defaultTTL
	}

	cacheControl := resp.Header.Get("Cache-Control")
	if cacheControl == "" {
		return defaultTTL
	}

	// Parse max-age from Cache-Control header
	// Example: "public, max-age=3600" or "max-age=3600, must-revalidate"
	directives := strings.Split(cacheControl, ",")
	for _, directive := range directives {
		directive = strings.TrimSpace(directive)
		if strings.HasPrefix(directive, "max-age=") {
			maxAgeStr := strings.TrimPrefix(directive, "max-age=")
			if maxAge, err := strconv.Atoi(maxAgeStr); err == nil && maxAge > 0 {
				return maxAge
			}
		}
		// Check for no-cache or no-store directives
		if directive == "no-cache" || directive == "no-store" {
			return 0
		}
	}

	return defaultTTL
}

// Stats returns cache statistics for monitoring
func (c *fragmentCache) Stats() (entries int, size int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entries = len(c.entries)
	for _, elem := range c.entries {
		entry := elem.Value.(*cacheEntry)
		size += int64(len(entry.data))
	}

	return entries, size
}

// Reset clears all cache entries (useful for testing)
func (c *fragmentCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*list.Element)
	c.lru = list.New()
}
