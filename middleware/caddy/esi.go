package caddy_esi

import (
	"bytes"
	"net/http"
	"sync"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/darkweak/go-esi/writer"
	"go.uber.org/zap"
)

var bufPool *sync.Pool = &sync.Pool{
	New: func() any {
		return &bytes.Buffer{}
	},
}

func init() {
	caddy.RegisterModule(ESI{})
	httpcaddyfile.RegisterGlobalOption("esi", func(h *caddyfile.Dispenser, _ interface{}) (interface{}, error) {
		return &ESI{}, nil
	})
	httpcaddyfile.RegisterHandlerDirective("esi", func(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
		return &ESI{}, nil
	})
}

// ESI to handle, process and serve ESI tags.
type ESI struct{
	logger *zap.Logger
}

// CaddyModule returns the Caddy module information.
func (ESI) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.esi",
		New: func() caddy.Module { return new(ESI) },
	}
}

// ServeHTTP implements caddyhttp.MiddlewareHandler
func (e *ESI) ServeHTTP(rw http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)
	cw := writer.NewWriter(buf, rw, r)
	go func(w *writer.Writer) {
		var i = 0
		for {
			if len(w.AsyncBuf) <= i {
				continue
			}
			rs := <-w.AsyncBuf[i]
			if rs == nil {
				w.Done <- true
				break
			}
			_, _ = rw.Write(rs)
			i++
		}
	}(cw)
	next.ServeHTTP(cw, r)
	cw.Header().Del("Content-Length")
	if cw.Rq.ProtoMajor == 1 {
		cw.Header().Set("Content-Encoding", "chunked")
	}
	cw.AsyncBuf = append(cw.AsyncBuf, make(chan []byte))
	go func(w *writer.Writer, iteration int) {
		w.AsyncBuf[iteration] <- nil
	}(cw, cw.Iteration)

	<-cw.Done

	return nil
}

// Provision implements caddy.Provisioner
func (e *ESI) Provision(ctx caddy.Context) error {
	e.logger = ctx.Logger()
	e.logger.Info("ESI middleware enabled with parallel processing")
	// Make logger available to the ESI package
	writer.SetLogger(e.logger)
	return nil
}

func (s ESI) Start() error { return nil }

func (s ESI) Stop() error { return nil }

// Interface guards
var (
	_ caddyhttp.MiddlewareHandler = (*ESI)(nil)
	_ caddy.Module                = (*ESI)(nil)
	_ caddy.Provisioner           = (*ESI)(nil)
	_ caddy.App                   = (*ESI)(nil)
)
