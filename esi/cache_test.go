package esi

import (
	"container/list"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCacheBasicFunctionality(t *testing.T) {
	cache.Reset() // Start with clean cache

	// Create a test server that tracks request count
	requestCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Cache-Control", "max-age=10")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<p>Fragment content</p>"))
	}))
	defer ts.Close()

	htmlTemplate := `<html><esi:include src="` + ts.URL + `" /></html>`
	req := httptest.NewRequest("GET", "http://example.com", nil)

	// First request - cache miss
	html1 := []byte(htmlTemplate)
	result1 := Parse(html1, req)
	if requestCount != 1 {
		t.Errorf("Expected 1 request on first parse, got %d", requestCount)
	}

	// Second request - cache hit  
	html2 := []byte(htmlTemplate)
	result2 := Parse(html2, req)
	if requestCount != 1 {
		t.Errorf("Expected still 1 request on second parse (cache hit), got %d", requestCount)
	}

	// Results should be identical
	if string(result1) != string(result2) {
		t.Errorf("Cached result doesn't match original\nFirst:  %q\nSecond: %q", string(result1), string(result2))
	}
}

func TestCacheExpiration(t *testing.T) {
	cache.Reset() // Start with clean cache

	requestCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		// Very short TTL for testing
		w.Header().Set("Cache-Control", "max-age=1")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<p>Fragment content</p>"))
	}))
	defer ts.Close()

	htmlTemplate := `<html><esi:include src="` + ts.URL + `" /></html>`
	req := httptest.NewRequest("GET", "http://example.com", nil)

	// First request
	html1 := []byte(htmlTemplate)
	Parse(html1, req)
	if requestCount != 1 {
		t.Errorf("Expected 1 request, got %d", requestCount)
	}

	// Wait for cache to expire
	time.Sleep(1100 * time.Millisecond)

	// Second request after expiration - should fetch again
	html2 := []byte(htmlTemplate)
	Parse(html2, req)
	if requestCount != 2 {
		t.Errorf("Expected 2 requests after cache expiration, got %d", requestCount)
	}
}

func TestCacheOnlyStores200(t *testing.T) {
	cache.Reset() // Start with clean cache

	requestCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Cache-Control", "max-age=300")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Error"))
	}))
	defer ts.Close()

	htmlTemplate := `<html><esi:include src="` + ts.URL + `" /></html>`
	req := httptest.NewRequest("GET", "http://example.com", nil)

	// First request - 500 error
	html1 := []byte(htmlTemplate)
	Parse(html1, req)
	if requestCount != 1 {
		t.Errorf("Expected 1 request, got %d", requestCount)
	}

	// Second request - should NOT use cache (errors not cached)
	html2 := []byte(htmlTemplate)
	Parse(html2, req)
	if requestCount != 2 {
		t.Errorf("Expected 2 requests (errors should not be cached), got %d", requestCount)
	}
}

func TestParseTTL(t *testing.T) {
	tests := []struct {
		name         string
		cacheControl string
		expectedTTL  int
	}{
		{"max-age only", "max-age=3600", 3600},
		{"max-age with other directives", "public, max-age=7200, must-revalidate", 7200},
		{"no cache-control", "", defaultTTL},
		{"no-cache directive", "no-cache", 0},
		{"no-store directive", "no-store", 0},
		{"invalid max-age", "max-age=invalid", defaultTTL},
		{"zero max-age", "max-age=0", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				Header: http.Header{},
			}
			if tt.cacheControl != "" {
				resp.Header.Set("Cache-Control", tt.cacheControl)
			}

			ttl := parseTTL(resp)
			if ttl != tt.expectedTTL {
				t.Errorf("Expected TTL %d, got %d for Cache-Control: %s",
					tt.expectedTTL, ttl, tt.cacheControl)
			}
		})
	}
}

func TestCacheLRUEviction(t *testing.T) {
	// Create more servers than maxCacheEntries to test eviction
	servers := make([]*httptest.Server, maxCacheEntries+5)
	for i := range servers {
		i := i
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "max-age=300")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("<p>Content " + string(rune(i)) + "</p>"))
		}))
		defer servers[i].Close()
	}

	req := httptest.NewRequest("GET", "http://example.com", nil)

	// Fill cache beyond max
	for _, server := range servers {
		html := []byte(`<html><esi:include src="` + server.URL + `" /></html>`)
		Parse(html, req)
	}

	// Check cache stats
	entries, _ := cache.Stats()
	if entries > maxCacheEntries {
		t.Errorf("Cache exceeded max entries: got %d, max %d", entries, maxCacheEntries)
	}
}

func TestCacheStats(t *testing.T) {
	// Create fresh cache for this test
	oldCache := cache
	cache = &fragmentCache{
		entries: make(map[string]*list.Element),
		lru:     list.New(),
	}
	defer func() { cache = oldCache }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=300")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<p>Test content</p>"))
	}))
	defer ts.Close()

	html := []byte(`<html><esi:include src="` + ts.URL + `" /></html>`)
	req := httptest.NewRequest("GET", "http://example.com", nil)

	// Cache should be empty
	entries, size := cache.Stats()
	if entries != 0 || size != 0 {
		t.Errorf("Expected empty cache, got %d entries, %d bytes", entries, size)
	}

	// Add one entry
	Parse(html, req)

	// Cache should have one entry
	entries, size = cache.Stats()
	if entries != 1 {
		t.Errorf("Expected 1 cache entry, got %d", entries)
	}
	if size == 0 {
		t.Errorf("Expected non-zero cache size")
	}
}
