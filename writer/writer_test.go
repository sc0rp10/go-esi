package writer

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockResponseWriter is a simple mock to track WriteHeader calls
type mockResponseWriter struct {
	headers    http.Header
	statusCode int
	written    bool
}

func newMockResponseWriter() *mockResponseWriter {
	return &mockResponseWriter{
		headers: make(http.Header),
	}
}

func (m *mockResponseWriter) Header() http.Header {
	return m.headers
}

func (m *mockResponseWriter) WriteHeader(statusCode int) {
	if !m.written {
		m.statusCode = statusCode
		m.written = true
	}
}

func (m *mockResponseWriter) Write([]byte) (int, error) {
	if !m.written {
		m.WriteHeader(http.StatusOK)
	}
	return 0, nil
}

// TestWriteHeader_PreservesStatusCodes tests that status codes are properly preserved
func TestWriteHeader_PreservesStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		expected   int
	}{
		{
			name:       "302 redirect should be preserved",
			statusCode: http.StatusFound,
			expected:   http.StatusFound,
		},
		{
			name:       "301 redirect should be preserved",
			statusCode: http.StatusMovedPermanently,
			expected:   http.StatusMovedPermanently,
		},
		{
			name:       "404 not found should be preserved",
			statusCode: http.StatusNotFound,
			expected:   http.StatusNotFound,
		},
		{
			name:       "500 error should be preserved",
			statusCode: http.StatusInternalServerError,
			expected:   http.StatusInternalServerError,
		},
		{
			name:       "200 OK should be preserved",
			statusCode: http.StatusOK,
			expected:   http.StatusOK,
		},
		{
			name:       "0 status should default to 200 OK",
			statusCode: 0,
			expected:   http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use mock ResponseWriter to capture WriteHeader call
			mock := newMockResponseWriter()
			req := httptest.NewRequest("GET", "http://example.com/test", nil)

			// Create writer (no buffer needed for this test)
			writer := &Writer{
				rw: mock,
				Rq: req,
			}

			// Call WriteHeader with the test status code
			writer.WriteHeader(tt.statusCode)

			// Check if the status code was preserved
			if mock.statusCode != tt.expected {
				t.Errorf("Expected status code %d, got %d", tt.expected, mock.statusCode)
			}
		})
	}
}

// TestWriteHeader_RedirectScenario tests a realistic redirect scenario
func TestWriteHeader_RedirectScenario(t *testing.T) {
	mock := newMockResponseWriter()
	req := httptest.NewRequest("GET", "http://example.com/old-page", nil)

	writer := &Writer{
		rw: mock,
		Rq: req,
	}

	// Simulate a redirect handler
	writer.Header().Set("Location", "/new-page")
	writer.WriteHeader(http.StatusFound) // 302

	// Verify the redirect status code is preserved
	if mock.statusCode != http.StatusFound {
		t.Errorf("Expected 302 Found, got %d - this breaks SEO!", mock.statusCode)
	}

	// Verify Location header is present
	if location := writer.Header().Get("Location"); location != "/new-page" {
		t.Errorf("Expected Location header '/new-page', got '%s'", location)
	}
}
