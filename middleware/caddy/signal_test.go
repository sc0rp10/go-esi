package caddy_esi

import (
	"bytes"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sc0rp10/go-esi/writer"
)

// Test the new signal-based synchronization
func TestSignalBasedWriter(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "plain text",
			content:  "<html><body>Hello World</body></html>",
			expected: "<html><body>Hello World</body></html>",
		},
		{
			name:     "simple ESI include",
			content:  `<html><esi:include src="/test"/></html>`,
			expected: "<html>", // Partial match since ESI processing requires server
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create response recorder
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "http://example.com/test", nil)

			// Create writer
			buf := &bytes.Buffer{}
			w := writer.NewWriter(buf, rec, req)

			// Start reader goroutine (simulating ServeHTTP.func1)
			done := make(chan bool)
			var output []byte

			go func() {
				var i = 0
				for {
					// Wait for signal that a new channel is ready
					<-w.Ready

					// Safely read from AsyncBuf with mutex protection
					w.BufMu.Lock()
					if i >= len(w.AsyncBuf) {
						w.BufMu.Unlock()
						continue
					}
					ch := w.AsyncBuf[i]
					w.BufMu.Unlock()

					rs := <-ch
					if rs == nil {
						done <- true
						break
					}
					output = append(output, rs...)
					i++
				}
			}()

			// Simulate Write() being called
			_, err := w.Write([]byte(tt.content))
			if err != nil {
				t.Fatalf("Write failed: %v", err)
			}

			// Signal completion with mutex protection
			ch := make(chan []byte)
			w.BufMu.Lock()
			w.AsyncBuf = append(w.AsyncBuf, ch)
			w.BufMu.Unlock()
			w.Ready <- struct{}{}
			ch <- nil

			// Wait for completion with timeout
			select {
			case <-done:
				// Success
			case <-time.After(1 * time.Second):
				t.Fatal("Timeout waiting for processing to complete")
			}

			// Verify output contains expected content
			if !bytes.Contains(output, []byte(tt.expected)) {
				t.Errorf("Expected output to contain %q, got %q", tt.expected, string(output))
			}
		})
	}
}

// Test that Ready channel doesn't block writer
func TestReadyChannelNonBlocking(t *testing.T) {
	buf := &bytes.Buffer{}
	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	rec := httptest.NewRecorder()

	w := writer.NewWriter(buf, rec, req)

	// Send many signals without reader - should not block
	// because Ready is buffered
	for i := 0; i < 50; i++ {
		w.AsyncBuf = append(w.AsyncBuf, make(chan []byte))
		select {
		case w.Ready <- struct{}{}:
			// Good - didn't block
		case <-time.After(10 * time.Millisecond):
			t.Fatalf("Ready channel blocked on send %d", i)
		}
	}
}

// Benchmark the signal-based approach
func BenchmarkSignalBased(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := &bytes.Buffer{}
		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		rec := httptest.NewRecorder()

		w := writer.NewWriter(buf, rec, req)

		// Start reader
		done := make(chan bool)
		go func() {
			var idx = 0
			for {
				<-w.Ready
				rs := <-w.AsyncBuf[idx]
				if rs == nil {
					done <- true
					break
				}
				idx++
			}
		}()

		// Write some data
		content := []byte("<html><body>Test</body></html>")
		w.Write(content)

		// Signal completion
		w.AsyncBuf = append(w.AsyncBuf, make(chan []byte))
		w.Ready <- struct{}{}
		w.AsyncBuf[w.Iteration] <- nil

		<-done
	}
}
