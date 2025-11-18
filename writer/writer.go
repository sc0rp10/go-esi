package writer

import (
	"bytes"
	"net/http"
	"sync"

	"github.com/sc0rp10/go-esi/esi"
	"go.uber.org/zap"
)

var logger *zap.Logger

// SetLogger sets the logger to be used for ESI processing
func SetLogger(l *zap.Logger) {
	logger = l
	esi.SetLogger(l)
}

type Writer struct {
	buf       *bytes.Buffer
	rw        http.ResponseWriter
	Rq        *http.Request
	AsyncBuf  []chan []byte
	BufMu     sync.Mutex    // Protects AsyncBuf from concurrent access
	Ready     chan struct{} // Signals when a new channel is added to AsyncBuf
	Done      chan bool
	flushed   bool
	Iteration int
}

func NewWriter(buf *bytes.Buffer, rw http.ResponseWriter, rq *http.Request) *Writer {
	if rq.URL.Scheme == "" {
		if rq.TLS != nil {
			rq.URL.Scheme = "https"
		} else {
			rq.URL.Scheme = "http"
		}
	}

	if rq.URL.Host == "" {
		rq.URL.Host = rq.Host
	}

	return &Writer{
		buf:      buf,
		Rq:       rq,
		rw:       rw,
		AsyncBuf: make([]chan []byte, 0),
		Ready:    make(chan struct{}, 100), // Buffered to avoid blocking Write()
		Done:     make(chan bool),
	}
}

// Header implements http.ResponseWriter.
func (w *Writer) Header() http.Header {
	return w.rw.Header()
}

// WriteHeader implements http.ResponseWriter.
func (w *Writer) WriteHeader(statusCode int) {
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	w.rw.WriteHeader(statusCode)
}

// Flush implements http.Flusher.
func (w *Writer) Flush() {
	if !w.flushed {
		if flusher, ok := w.rw.(http.Flusher); ok {
			flusher.Flush()
		}

		w.flushed = true
	}
}

// Write will write the response body.
func (w *Writer) Write(b []byte) (int, error) {
	buf := append(w.buf.Bytes(), b...)
	w.buf.Reset()

	if logger != nil {
		logger.Info("Writer received chunk",
			zap.Int("chunk_size", len(b)),
			zap.Int("buffer_size", len(buf)),
			zap.Bool("has_esi", esi.HasOpenedTags(buf)))
	}

	if esi.HasOpenedTags(buf) {
		position := 0
		for position < len(buf) {
			startPos, nextPos, t := esi.ReadToTag(buf[position:], position)

			if startPos != 0 {
				ch := make(chan []byte)
				w.BufMu.Lock()
				w.AsyncBuf = append(w.AsyncBuf, ch)
				idx := w.Iteration
				w.Iteration++
				w.BufMu.Unlock()
				w.Ready <- struct{}{} // Signal that new channel is ready
				go func(tmpBuf []byte, i int, c chan []byte) {
					c <- tmpBuf
				}(buf[position:position+startPos], idx, ch)
			}

			if t == nil {
				break
			}

			closePosition := t.GetClosePosition(buf[position+startPos:])
			if closePosition == 0 {
				position += startPos

				break
			}

			position += nextPos

			ch := make(chan []byte)
			w.BufMu.Lock()
			w.AsyncBuf = append(w.AsyncBuf, ch)
			w.Iteration++
			w.BufMu.Unlock()
			w.Ready <- struct{}{} // Signal that new channel is ready

			go func(currentTag esi.Tag, tmpBuf []byte, req *http.Request, c chan []byte) {
				p, _ := currentTag.Process(tmpBuf, req)
				c <- p
			}(t, buf[position:(position-nextPos)+startPos+closePosition], w.Rq, ch)

			position += startPos + closePosition - nextPos
		}
		w.buf.Write(buf[position:])

		return len(b), nil
	}

	ch := make(chan []byte)
	w.BufMu.Lock()
	w.AsyncBuf = append(w.AsyncBuf, ch)
	w.BufMu.Unlock()
	w.Ready <- struct{}{} // Signal that new channel is ready
	ch <- buf

	return len(b), nil
}

var _ http.ResponseWriter = (*Writer)(nil)
