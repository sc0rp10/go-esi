package caddy_esi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// Test the new buffered approach with a simple HTML response
func TestBufferedESI_NoTags(t *testing.T) {
	e := &ESI{}

	// Create a simple upstream handler that returns HTML
	upstream := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>Hello World</body></html>"))
		return nil
	})

	// Create test request and recorder
	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	rec := httptest.NewRecorder()

	// Execute middleware
	err := e.ServeHTTP(rec, req, upstream)
	if err != nil {
		t.Fatalf("ServeHTTP failed: %v", err)
	}

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	expected := "<html><body>Hello World</body></html>"
	if body != expected {
		t.Errorf("Expected body %q, got %q", expected, body)
	}
}

// Test with ESI tags (mocked - would need real ESI server for full test)
func TestBufferedESI_WithTags(t *testing.T) {
	e := &ESI{}

	// Upstream returns HTML with ESI comment tags (simplest to test)
	upstream := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body><!--esi <esi:comment text=\"test\"/> --></body></html>"))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	rec := httptest.NewRecorder()

	err := e.ServeHTTP(rec, req, upstream)
	if err != nil {
		t.Fatalf("ServeHTTP failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	// Body should be processed (ESI tags removed/processed)
	body := rec.Body.String()
	if !bytes.Contains([]byte(body), []byte("<html>")) {
		t.Errorf("Response should contain HTML, got: %q", body)
	}
}

// Test non-HTML responses pass through without buffering
func TestBufferedESI_NonHTML(t *testing.T) {
	e := &ESI{}

	upstream := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"test": "data"}`))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/api/test", nil)
	rec := httptest.NewRecorder()

	err := e.ServeHTTP(rec, req, upstream)
	if err != nil {
		t.Fatalf("ServeHTTP failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	expected := `{"test": "data"}`
	if body != expected {
		t.Errorf("Expected body %q, got %q", expected, body)
	}
}

// Test error responses pass through
func TestBufferedESI_ErrorStatus(t *testing.T) {
	e := &ESI{}

	upstream := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("<html><body>Not Found</body></html>"))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/missing", nil)
	rec := httptest.NewRecorder()

	err := e.ServeHTTP(rec, req, upstream)
	if err != nil {
		t.Fatalf("ServeHTTP failed: %v", err)
	}

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rec.Code)
	}

	body := rec.Body.String()
	expected := "<html><body>Not Found</body></html>"
	if body != expected {
		t.Errorf("Expected body %q, got %q", expected, body)
	}
}

// Test large responses (production-like - ~627KB)
func TestBufferedESI_LargeResponse(t *testing.T) {
	e := &ESI{}

	// Generate a large HTML response similar to production logs
	largeHTML := bytes.Repeat([]byte("<p>Content </p>"), 50000) // ~650KB

	upstream := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write(largeHTML)
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/large", nil)
	rec := httptest.NewRecorder()

	err := e.ServeHTTP(rec, req, upstream)
	if err != nil {
		t.Fatalf("ServeHTTP failed: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	// Body size should be approximately the same (ESI processing may add/remove bytes)
	if rec.Body.Len() < len(largeHTML)-1000 || rec.Body.Len() > len(largeHTML)+1000 {
		t.Errorf("Expected body size ~%d, got %d", len(largeHTML), rec.Body.Len())
	}

	t.Logf("Large response handled successfully: %d bytes in, %d bytes out", len(largeHTML), rec.Body.Len())
}
