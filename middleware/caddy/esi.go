package caddy_esi

import (
	"bytes"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sc0rp10/go-esi/esi"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
	httpcaddyfile.RegisterHandlerDirective("esi", parseCaddyfileHandlerDirective)
}

// parseCaddyfileHandlerDirective parses the ESI directive from Caddyfile
func parseCaddyfileHandlerDirective(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	e := &ESI{}
	err := e.UnmarshalCaddyfile(h.Dispenser)
	return e, err
}

// UnmarshalCaddyfile implements caddyfile.Unmarshaler
func (e *ESI) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "minimum_cache_ttl":
				var ttlStr string
				if !d.Args(&ttlStr) {
					return d.ArgErr()
				}
				ttl, err := strconv.Atoi(ttlStr)
				if err != nil {
					return d.Errf("invalid minimum_cache_ttl: %v", err)
				}
				e.MinimumCacheTTL = ttl
			case "cache_ttl_jitter":
				var jitterStr string
				if !d.Args(&jitterStr) {
					return d.ArgErr()
				}
				jitter, err := strconv.Atoi(jitterStr)
				if err != nil {
					return d.Errf("invalid cache_ttl_jitter: %v", err)
				}
				e.CacheTTLJitter = jitter
			case "esi_base_url":
				if !d.Args(&e.ESIBaseURL) {
					return d.ArgErr()
				}
			case "debug":
				// Enable or disable debug logging
				// Format: debug on|off or debug {$ENV_VAR}
				var debugValue string
				if !d.Args(&debugValue) {
					return d.ArgErr()
				}

				// Accept: "on", "true", "1", "yes" for true
				// Accept: "off", "false", "0", "no" for false
				debugLower := strings.ToLower(strings.TrimSpace(debugValue))
				switch debugLower {
				case "on", "true", "1", "yes":
					e.Debug = true
				case "off", "false", "0", "no":
					e.Debug = false
				default:
					return d.Errf("debug must be 'on' or 'off', got: %s", debugValue)
				}
			case "esi_set_header":
				// Set a custom header on ESI fragment requests (repeatable directive)
				// Format: esi_set_header X-Backend-Server "internal-server"
				if e.ESIHeaders == nil {
					e.ESIHeaders = make(map[string]string)
				}

				var headerName, headerValue string
				if !d.Args(&headerName, &headerValue) {
					return d.Err("esi_set_header requires header name and value")
				}

				e.ESIHeaders[headerName] = headerValue
			default:
				return d.Errf("unknown subdirective: %s", d.Val())
			}
		}
	}
	return nil
}

// ESI to handle, process and serve ESI tags.
type ESI struct {
	// Configuration
	MinimumCacheTTL int               `json:"minimum_cache_ttl,omitempty"`
	CacheTTLJitter  int               `json:"cache_ttl_jitter,omitempty"`
	ESIBaseURL      string            `json:"esi_base_url,omitempty"`
	ESIHeaders      map[string]string `json:"esi_headers,omitempty"`
	Debug           bool              `json:"debug,omitempty"`

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
	// Determine if we should buffer the response
	shouldBuffer := func(status int, header http.Header) bool {
		// Only buffer successful HTML responses
		if status != http.StatusOK {
			return false
		}

		// Don't buffer if Transfer-Encoding is chunked (streaming response)
		if header.Get("Transfer-Encoding") == "chunked" {
			return false
		}

		// Don't buffer very small responses (< 512 bytes) - unlikely to have ESI
		if cl := header.Get("Content-Length"); cl != "" {
			if size, err := strconv.Atoi(cl); err == nil && size < 512 {
				return false
			}
		}

		// Only buffer HTML/XHTML content types
		ct := header.Get("Content-Type")
		return ct != "" && (bytes.Contains([]byte(ct), []byte("text/html")) ||
			bytes.Contains([]byte(ct), []byte("application/xhtml+xml")))
	}

	// Create recorder to buffer the response
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	recorder := caddyhttp.NewResponseRecorder(rw, buf, shouldBuffer)

	// Let upstream write to the recorder
	err := next.ServeHTTP(recorder, r)
	if err != nil {
		return err
	}

	// If not buffered, response already written through
	if !recorder.Buffered() {
		return nil
	}

	// Get the buffered response body
	body := recorder.Buffer().Bytes()

	if e.logger != nil {
		e.logger.Debug("ESI middleware received response",
			zap.Int("status", recorder.Status()),
			zap.Int("body_size", len(body)),
			zap.Bool("has_esi", esi.HasOpenedTags(body)))
	}

	// Check if response contains ESI tags
	if !esi.HasOpenedTags(body) {
		// No ESI tags, write buffered response as-is
		rw.WriteHeader(recorder.Status())
		_, err = rw.Write(body)
		return err
	}

	// Process ESI tags
	if e.logger != nil {
		e.logger.Info("Processing ESI tags", zap.String("url", r.URL.String()))
	}

	processed := esi.Parse(body, r)

	// Write processed response
	rw.WriteHeader(recorder.Status())
	_, err = rw.Write(processed)
	return err
}

// Provision implements caddy.Provisioner
func (e *ESI) Provision(ctx caddy.Context) error {
	e.logger = ctx.Logger()

	// Check environment variable for debug logging
	if isDebugEnabled() {
		// Create debug-level logger
		config := zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
		debugLogger, err := config.Build()
		if err == nil {
			e.logger = debugLogger
			e.logger.Info("ESI debug logging enabled via ESI_DEBUG environment variable")
		}
	}

	e.logger.Info("ESI middleware enabled with buffered processing")

	// Pass logger to ESI package for cache logging
	esi.SetLogger(e.logger)

	// Configure ESI package with user settings
	config := esi.Config{
		MinimumCacheTTL: e.MinimumCacheTTL,
		CacheTTLJitter:  e.CacheTTLJitter,
		BaseURL:         e.ESIBaseURL,
		Headers:         e.ESIHeaders,
	}
	esi.Configure(config)

	e.logger.Info("ESI configuration applied",
		zap.Int("minimum_cache_ttl", e.MinimumCacheTTL),
		zap.Int("cache_ttl_jitter", e.CacheTTLJitter),
		zap.String("esi_base_url", e.ESIBaseURL),
		zap.Any("esi_headers", e.ESIHeaders))

	// Initialize Prometheus metrics if registry is available
	if reg := ctx.GetMetricsRegistry(); reg != nil {
		e.initMetrics(reg)
		// Register this ESI instance as the metrics observer for the cache
		esi.SetMetricsObserver(e)
		e.logger.Info("ESI cache metrics registered with Prometheus")
	}

	return nil
}

// isDebugEnabled checks environment variable for debug mode
func isDebugEnabled() bool {
	debugEnv := os.Getenv("ESI_DEBUG")
	if debugEnv == "" {
		return false
	}

	// Accept: "1", "true", "TRUE", "yes", "YES"
	debugEnv = strings.ToLower(strings.TrimSpace(debugEnv))
	return debugEnv == "1" || debugEnv == "true" || debugEnv == "yes"
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
