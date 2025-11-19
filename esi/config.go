package esi

import (
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"
)

// Config holds the configuration for ESI processing
type Config struct {
	// MinimumCacheTTL is the minimum TTL in seconds for cached fragments (default: 300)
	// This overrides upstream Cache-Control headers if they specify a lower value
	MinimumCacheTTL int

	// CacheTTLJitter is the maximum random jitter in seconds to add to TTL (default: 0)
	// This helps prevent cache stampede by spreading out cache expirations
	CacheTTLJitter int

	// BaseURL is the base URL to use for ESI fragment requests (e.g. "http://localhost:9000")
	// If set, all relative fragment URLs will be resolved against this URL instead of
	// the original request URL. This allows fragments to be fetched from an internal
	// endpoint that bypasses CDN/WAF rules.
	BaseURL string

	// Headers is a map of custom headers to set on fragment requests (like proxy_set_header)
	// Example: {"X-Backend-Server": "internal", "X-Request-Source": "esi"}
	// These headers are set with the specified values on every fragment request
	Headers map[string]string
}

var (
	globalConfig Config
	rng          = rand.New(rand.NewSource(time.Now().UnixNano()))
)

// Configure sets the global ESI configuration
func Configure(cfg Config) {
	globalConfig = cfg

	// Set defaults if not specified
	if globalConfig.MinimumCacheTTL == 0 {
		globalConfig.MinimumCacheTTL = defaultTTL
	}

	if logger != nil {
		logger.Info("ESI configuration updated",
			zap.Int("minimum_cache_ttl", globalConfig.MinimumCacheTTL),
			zap.Int("cache_ttl_jitter", globalConfig.CacheTTLJitter),
			zap.String("base_url", globalConfig.BaseURL),
			zap.Any("headers", globalConfig.Headers))
	}
}

// GetConfig returns the current global configuration
func GetConfig() Config {
	return globalConfig
}

// resolveFragmentURL resolves a fragment URL, optionally using the configured BaseURL
func resolveFragmentURL(fragmentURL string, requestURL *url.URL) string {
	// If BaseURL is configured, use it instead of the request URL
	if globalConfig.BaseURL != "" {
		baseURL, err := url.Parse(globalConfig.BaseURL)
		if err != nil {
			if logger != nil {
				logger.Warn("Failed to parse configured base_url, falling back to request URL",
					zap.String("base_url", globalConfig.BaseURL),
					zap.Error(err))
			}
			return sanitizeURL(fragmentURL, requestURL)
		}

		// Resolve fragment URL against configured base URL
		parsed, err := url.Parse(fragmentURL)
		if err != nil {
			return fragmentURL
		}

		resolved := baseURL.ResolveReference(parsed).String()

		if logger != nil {
			logger.Debug("ESI fragment URL resolved using configured base_url",
				zap.String("fragment", fragmentURL),
				zap.String("base_url", globalConfig.BaseURL),
				zap.String("resolved", resolved))
		}

		return resolved
	}

	// Use normal resolution against request URL
	return sanitizeURL(fragmentURL, requestURL)
}

// applyTTLJitter adds random jitter to the TTL if configured
func applyTTLJitter(ttl int) int {
	if globalConfig.CacheTTLJitter <= 0 {
		return ttl
	}

	// Add random jitter between 0 and CacheTTLJitter
	jitter := rng.Intn(globalConfig.CacheTTLJitter + 1)
	return ttl + jitter
}

// getCustomHeaders returns the map of custom headers to set on requests
func getCustomHeaders() map[string]string {
	return globalConfig.Headers
}

// setCustomHeaders sets configured custom headers on the request
func setCustomHeaders(req *http.Request) {
	for name, value := range globalConfig.Headers {
		req.Header.Set(name, value)
	}
}
