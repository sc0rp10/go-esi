package caddy_esi

import (
	"bytes"
	"net/http"
	"sync"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/darkweak/go-esi/esi"
	"github.com/darkweak/go-esi/writer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
type ESI struct {
	logger *zap.Logger
	
	// Prometheus metrics
	cacheHits          prometheus.Counter
	cacheMisses        prometheus.Counter
	cacheEvictions     prometheus.Counter
	cacheStampedeWaits prometheus.Counter
	cacheEntries       prometheus.Gauge
	cacheSizeBytes     prometheus.Gauge
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
	
	// Initialize Prometheus metrics if registry is available
	if reg := ctx.GetMetricsRegistry(); reg != nil {
		e.initMetrics(reg)
		// Register this ESI instance as the metrics observer for the cache
		esi.SetMetricsObserver(e)
		e.logger.Info("ESI cache metrics registered with Prometheus")
	}
	
	return nil
}

// OnCacheHit implements esi.MetricsObserver
func (e *ESI) OnCacheHit() {
	if e.cacheHits != nil {
		e.cacheHits.Inc()
	}
}

// OnCacheMiss implements esi.MetricsObserver
func (e *ESI) OnCacheMiss() {
	if e.cacheMisses != nil {
		e.cacheMisses.Inc()
	}
}

// OnCacheEviction implements esi.MetricsObserver
func (e *ESI) OnCacheEviction() {
	if e.cacheEvictions != nil {
		e.cacheEvictions.Inc()
	}
}

// OnStampedeWait implements esi.MetricsObserver
func (e *ESI) OnStampedeWait() {
	if e.cacheStampedeWaits != nil {
		e.cacheStampedeWaits.Inc()
	}
}

// initMetrics initializes Prometheus metrics
func (e *ESI) initMetrics(reg *prometheus.Registry) {
	const ns, sub = "caddy", "esi"
	
	factory := promauto.With(reg)
	
	e.cacheHits = factory.NewCounter(prometheus.CounterOpts{
		Namespace: ns,
		Subsystem: sub,
		Name:      "cache_hits_total",
		Help:      "Total number of ESI fragment cache hits",
	})
	
	e.cacheMisses = factory.NewCounter(prometheus.CounterOpts{
		Namespace: ns,
		Subsystem: sub,
		Name:      "cache_misses_total",
		Help:      "Total number of ESI fragment cache misses",
	})
	
	e.cacheEvictions = factory.NewCounter(prometheus.CounterOpts{
		Namespace: ns,
		Subsystem: sub,
		Name:      "cache_evictions_total",
		Help:      "Total number of ESI fragment cache evictions (LRU)",
	})
	
	e.cacheStampedeWaits = factory.NewCounter(prometheus.CounterOpts{
		Namespace: ns,
		Subsystem: sub,
		Name:      "cache_stampede_waits_total",
		Help:      "Total number of requests that waited for in-flight fetches (stampede prevention)",
	})
	
	e.cacheEntries = factory.NewGauge(prometheus.GaugeOpts{
		Namespace: ns,
		Subsystem: sub,
		Name:      "cache_entries",
		Help:      "Current number of entries in the ESI fragment cache",
	})
	
	e.cacheSizeBytes = factory.NewGauge(prometheus.GaugeOpts{
		Namespace: ns,
		Subsystem: sub,
		Name:      "cache_size_bytes",
		Help:      "Current size of the ESI fragment cache in bytes",
	})
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
