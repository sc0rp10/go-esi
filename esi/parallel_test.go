package esi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkweak/go-esi/esi"
)

// TestParallelProcessing verifies that parallel mode actually fetches includes concurrently
func TestParallelProcessing(t *testing.T) {
	t.Parallel()

	// Track request timings
	var mu sync.Mutex
	requestTimes := make(map[string]time.Time)

	// Create a test server that simulates slow responses
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestTimes[r.URL.Path] = time.Now()
		mu.Unlock()

		// Simulate slow response (100ms)
		time.Sleep(100 * time.Millisecond)

		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/fragment1":
			fmt.Fprint(w, "<div>Fragment 1</div>")
		case "/fragment2":
			fmt.Fprint(w, "<div>Fragment 2</div>")
		case "/fragment3":
			fmt.Fprint(w, "<div>Fragment 3</div>")
		default:
			fmt.Fprint(w, "<div>Unknown</div>")
		}
	}))
	defer server.Close()

	// Create HTML template
	htmlTemplate := fmt.Sprintf(`<html>
<body>
	<esi:include src="%s/fragment1"/>
	<esi:include src="%s/fragment2"/>
	<esi:include src="%s/fragment3"/>
</body>
</html>`, server.URL, server.URL, server.URL)

	req := httptest.NewRequest(http.MethodGet, "http://test.com", nil)

	// Parse with parallel processing (now default)
	htmlCopy := []byte(htmlTemplate)
	start := time.Now()
	result := esi.Parse(htmlCopy, req)
	duration := time.Since(start)

	// Verify output contains all fragments
	resultStr := string(result)
	if !strings.Contains(resultStr, "Fragment 1") ||
		!strings.Contains(resultStr, "Fragment 2") ||
		!strings.Contains(resultStr, "Fragment 3") {
		t.Errorf("Result missing fragments: %s", resultStr)
	}

	// With parallel processing (each fragment takes 100ms)
	// Total time should be ~100ms (all fetched concurrently) not ~300ms (sequential)
	t.Logf("Parallel processing duration: %v", duration)

	// Verify parallel processing is working - should be much faster than 250ms (which would indicate sequential)
	if duration > 250*time.Millisecond {
		t.Errorf("Processing took %v, expected ~100ms with parallel fetching (may indicate sequential processing)", duration)
	}
}

// TestParallelWithNestedIncludes verifies parallel processing handles nested ESI tags
func TestParallelWithNestedIncludes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/parent1":
			fmt.Fprintf(w, `<div>Parent1:<esi:include src="/child1"/></div>`)
		case "/parent2":
			fmt.Fprintf(w, `<div>Parent2:<esi:include src="/child2"/></div>`)
		case "/child1":
			fmt.Fprint(w, "<span>Child1</span>")
		case "/child2":
			fmt.Fprint(w, "<span>Child2</span>")
		}
	}))
	defer server.Close()

	html := []byte(fmt.Sprintf(`<html>
<esi:include src="%s/parent1"/>
<esi:include src="%s/parent2"/>
</html>`, server.URL, server.URL))

	req := httptest.NewRequest(http.MethodGet, "http://test.com", nil)
	result := esi.Parse(html, req)

	resultStr := string(result)
	if !strings.Contains(resultStr, "Parent1") ||
		!strings.Contains(resultStr, "Child1") ||
		!strings.Contains(resultStr, "Parent2") ||
		!strings.Contains(resultStr, "Child2") {
		t.Errorf("Result missing nested fragments: %s", resultStr)
	}
}

// TestParallelWithAltFallback verifies parallel mode handles alt attribute correctly
func TestParallelWithAltFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/working" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "<div>Working</div>")
		} else if r.URL.Path == "/fallback" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "<div>Fallback</div>")
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	html := []byte(fmt.Sprintf(
		`<esi:include src="%s/notfound" alt="%s/fallback"/>`,
		server.URL, server.URL))

	req := httptest.NewRequest(http.MethodGet, "http://test.com", nil)
	result := esi.Parse(html, req)

	if !strings.Contains(string(result), "Fallback") {
		t.Errorf("Alt fallback not working: %s", string(result))
	}
}
