package esi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"go.uber.org/zap"
)

var logger *zap.Logger

// SetLogger sets the logger to be used for ESI processing
func SetLogger(l *zap.Logger) {
	logger = l
}

const include = "include"

var (
	closeInclude     = regexp.MustCompile("/>")
	srcAttribute     = regexp.MustCompile(`src="?(.+?)"?( |/>)`)
	altAttribute     = regexp.MustCompile(`alt="?(.+?)"?( |/>)`)
	onErrorAttribute = regexp.MustCompile(`onerror="?(.+?)"?( |/>)`)

	// HTTP client with increased connection pool for parallel ESI fetching
	httpClient = createHTTPClient()
)

func createHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 100, // Allow many parallel connections
			MaxConnsPerHost:     100,
		},
	}
}

// safe to pass to any origin.
var headersSafe = []string{
	"Accept",
	"Accept-Language",
}

// safe to pass only to same-origin (same scheme, same host, same port).
var headersUnsafe = []string{
	"Cookie",
	"Authorization",
}

type includeTag struct {
	*baseTag
	silent bool
	alt    string
	src    string
}

func (i *includeTag) loadAttributes(b []byte) error {
	src := srcAttribute.FindSubmatch(b)
	if src == nil {
		return errNotFound
	}

	i.src = string(src[1])

	alt := altAttribute.FindSubmatch(b)
	if alt != nil {
		i.alt = string(alt[1])
	}

	onError := onErrorAttribute.FindSubmatch(b)
	if onError != nil {
		i.silent = string(onError[1]) == "continue"
	}

	return nil
}

func sanitizeURL(u string, reqURL *url.URL) string {
	parsed, err := url.Parse(u)
	if err != nil || parsed == nil {
		return u
	}

	if reqURL == nil {
		return parsed.String()
	}

	return reqURL.ResolveReference(parsed).String()
}

func addHeaders(headers []string, req *http.Request, rq *http.Request) {
	for _, h := range headers {
		v := req.Header.Get(h)
		if v != "" {
			rq.Header.Add(h, v)
		}
	}
}

// Input (e.g. include src="https://domain.com/esi-include" alt="https://domain.com/alt-esi-include" />)
// With or without the alt
// With or without a space separator before the closing
// With or without the quotes around the src/alt value.
func (i *includeTag) Process(b []byte, req *http.Request) ([]byte, int) {
	closeIdx := closeInclude.FindIndex(b)

	if closeIdx == nil {
		return nil, len(b)
	}

	i.length = closeIdx[1]
	if e := i.loadAttributes(b[8:i.length]); e != nil {
		return nil, len(b)
	}

	cacheKey := sanitizeURL(i.src, req.URL)
	startTime := time.Now()

	// Use GetOrFetch to prevent cache stampede
	result, err := cache.GetOrFetch(cacheKey, func() ([]byte, *http.Response, error) {
		// Fetch the main URL
		rq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, cacheKey, nil)
		addHeaders(headersSafe, req, rq)

		if rq.URL.Scheme == req.URL.Scheme && rq.URL.Host == req.URL.Host {
			addHeaders(headersUnsafe, req, rq)
		}

		response, fetchErr := httpClient.Do(rq)
		elapsed := time.Since(startTime)
		if logger != nil {
			logger.Info("ESI include fetch completed",
				zap.String("url", cacheKey),
				zap.Duration("duration", elapsed),
				zap.Error(fetchErr))
		}
		newReq := rq

		// Try alt URL if main failed
		if (fetchErr != nil || response.StatusCode >= 400) && i.alt != "" {
			altKey := sanitizeURL(i.alt, req.URL)
			rq, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, altKey, nil)
			addHeaders(headersSafe, req, rq)

			if rq.URL.Scheme == req.URL.Scheme && rq.URL.Host == req.URL.Host {
				addHeaders(headersUnsafe, req, rq)
			}

			response, fetchErr = httpClient.Do(rq)
			newReq = rq

			if !i.silent && (fetchErr != nil || response.StatusCode >= 400) {
				return nil, nil, fetchErr
			}
		}

		if response == nil {
			return nil, nil, fetchErr
		}

		var buf bytes.Buffer
		defer response.Body.Close()
		_, _ = io.Copy(&buf, response.Body)

		rawContent := buf.Bytes()

		// Recursively parse nested ESI tags
		parsedContent := Parse(rawContent, newReq)

		return parsedContent, response, nil
	})

	if err != nil {
		return nil, len(b)
	}

	return result, i.length
}

func (*includeTag) HasClose(b []byte) bool {
	return closeInclude.FindIndex(b) != nil
}

func (*includeTag) GetClosePosition(b []byte) int {
	if idx := closeInclude.FindIndex(b); idx != nil {
		return idx[1]
	}

	return 0
}

// FetchContent fetches the include content without processing the document replacement.
// This is used for parallel fetching.
func (i *includeTag) FetchContent(b []byte, req *http.Request) []byte {
	closeIdx := closeInclude.FindIndex(b)

	if closeIdx == nil {
		return nil
	}

	i.length = closeIdx[1]
	if e := i.loadAttributes(b[8:i.length]); e != nil {
		return nil
	}

	cacheKey := sanitizeURL(i.src, req.URL)
	
	// Use GetOrFetch to prevent cache stampede
	result, err := cache.GetOrFetch(cacheKey, func() ([]byte, *http.Response, error) {
		// Fetch the main URL
		rq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, cacheKey, nil)
		addHeaders(headersSafe, req, rq)

		if rq.URL.Scheme == req.URL.Scheme && rq.URL.Host == req.URL.Host {
			addHeaders(headersUnsafe, req, rq)
		}

		response, fetchErr := httpClient.Do(rq)
		newReq := rq

		// Try alt URL if main failed
		if (fetchErr != nil || response.StatusCode >= 400) && i.alt != "" {
			altKey := sanitizeURL(i.alt, req.URL)
			rq, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, altKey, nil)
			addHeaders(headersSafe, req, rq)

			if rq.URL.Scheme == req.URL.Scheme && rq.URL.Host == req.URL.Host {
				addHeaders(headersUnsafe, req, rq)
			}

			response, fetchErr = httpClient.Do(rq)
			newReq = rq

			if !i.silent && (fetchErr != nil || response.StatusCode >= 400) {
				return nil, nil, fetchErr
			}
		}

		if response == nil {
			return nil, nil, fetchErr
		}

		var buf bytes.Buffer
		defer response.Body.Close()
		_, _ = io.Copy(&buf, response.Body)

		rawContent := buf.Bytes()

		// Recursively parse nested ESI tags
		parsedContent := Parse(rawContent, newReq)

		return parsedContent, response, nil
	})

	if err != nil {
		return nil
	}

	return result
}
