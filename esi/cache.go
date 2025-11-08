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

type inFlightRequest struct {
	wg     sync.WaitGroup
	result []byte
	err    error
}

type fragmentCache struct {
	mu       sync.RWMutex
	entries  map[string]*list.Element
	lru      *list.List
	inFlight sync.Map // map[string]*inFlightRequest - prevents cache stampede
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
		if logger != nil {
			logger.Info("Cache Get: not found", zap.String("url", url))
		}
		return nil, false
	}

	entry := elem.Value.(*cacheEntry)
	now := time.Now()
	if now.After(entry.expiresAt) {
		// Expired, will be cleaned up by Put
		if logger != nil {
			logger.Info("Cache Get: expired",
				zap.String("url", url),
				zap.Time("expired_at", entry.expiresAt),
				zap.Time("now", now))
		}
		return nil, false
	}

	// Move to front (most recently used)
	c.lru.MoveToFront(elem)

	if logger != nil {
		logger.Info("Cache Get: hit",
			zap.String("url", url),
			zap.Time("expires_at", entry.expiresAt))
	}

	return entry.data, true
}

// GetOrFetch retrieves from cache or ensures only one fetch happens for concurrent requests.
// This prevents cache stampede when multiple requests arrive for an expired/missing entry.
// The fetchFn is called only once per URL, other requests wait for the result.
func (c *fragmentCache) GetOrFetch(url string, fetchFn func() ([]byte, *http.Response, error)) ([]byte, error) {
	// Fast path: check cache first
	if cached, ok := c.Get(url); ok {
		if logger != nil {
			logger.Info("ESI include cache hit", zap.String("url", url))
		}
		return cached, nil
	}

	// Cache miss - check if someone else is already fetching this URL
	flight, loaded := c.inFlight.LoadOrStore(url, &inFlightRequest{})
	req := flight.(*inFlightRequest)

	if loaded {
		// Another goroutine is fetching, wait for it
		if logger != nil {
			logger.Info("ESI include waiting for in-flight request", zap.String("url", url))
		}
		req.wg.Wait()

		// Return the shared result from the fetcher
		return req.result, req.err
	}

	// We're the first one - do the fetch
	req.wg.Add(1)
	defer func() {
		req.wg.Done()
		c.inFlight.Delete(url) // Clean up in-flight tracking
	}()

	if logger != nil {
		logger.Info("ESI include cache miss, fetching", zap.String("url", url))
	}

	// Call the fetch function
	data, resp, err := fetchFn()
	
	// Store result and error for waiting goroutines
	req.result = data
	req.err = err

	if err != nil {
		return nil, err
	}

	if resp != nil && resp.StatusCode == http.StatusOK {
		// Cache the result
		c.Put(url, data, resp)
		if logger != nil {
			logger.Info("ESI include cached", zap.String("url", url))
		}
	}

	return data, nil
}

// Put stores a fragment in cache with TTL parsed from response headers
func (c *fragmentCache) Put(url string, data []byte, resp *http.Response) {
	ttl := parseTTL(resp)
	if logger != nil {
		cacheControl := ""
		if resp != nil {
			cacheControl = resp.Header.Get("Cache-Control")
		}
		logger.Info("Cache Put called",
			zap.String("url", url),
			zap.Int("ttl", ttl),
			zap.String("cache_control", cacheControl),
			zap.Int("data_size", len(data)))
	}
	
	if ttl == 0 {
		// Don't cache if TTL is 0
		if logger != nil {
			logger.Info("Not caching (TTL=0)", zap.String("url", url))
		}
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
				logger.Info("Cache evicted LRU entry", zap.String("url", oldEntry.url))
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
			if maxAge, err := strconv.Atoi(maxAgeStr); err == nil && maxAge >= 0 {
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
