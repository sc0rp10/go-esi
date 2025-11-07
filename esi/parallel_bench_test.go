package esi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/darkweak/go-esi/esi"
)

// BenchmarkParallelIncludes benchmarks parallel ESI include processing (now default)
func BenchmarkParallelIncludes(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate 10ms response time
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "<div>Fragment</div>")
	}))
	defer server.Close()

	html := []byte(fmt.Sprintf(`<html>
<esi:include src="%s/frag1"/>
<esi:include src="%s/frag2"/>
<esi:include src="%s/frag3"/>
</html>`, server.URL, server.URL, server.URL))

	req := httptest.NewRequest(http.MethodGet, "http://test.com", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		htmlCopy := make([]byte, len(html))
		copy(htmlCopy, html)
		esi.Parse(htmlCopy, req)
	}
}

// BenchmarkManyIncludes benchmarks performance with many includes (all fetched in parallel)
func BenchmarkManyIncludes(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "<div>F</div>")
	}))
	defer server.Close()

	html := []byte(fmt.Sprintf(`<html>
<esi:include src="%s/f1"/>
<esi:include src="%s/f2"/>
<esi:include src="%s/f3"/>
<esi:include src="%s/f4"/>
<esi:include src="%s/f5"/>
<esi:include src="%s/f6"/>
<esi:include src="%s/f7"/>
<esi:include src="%s/f8"/>
</html>`, server.URL, server.URL, server.URL, server.URL,
		server.URL, server.URL, server.URL, server.URL))

	req := httptest.NewRequest(http.MethodGet, "http://test.com", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		htmlCopy := make([]byte, len(html))
		copy(htmlCopy, html)
		esi.Parse(htmlCopy, req)
	}
}
