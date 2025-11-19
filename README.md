go-esi (sc0rp10 fork)
---------------------

> **Note:** This is a fork of [darkweak/go-esi](https://github.com/darkweak/go-esi). See [FORK_NOTICE.md](FORK_NOTICE.md) for details on modifications and attribution.

go-esi is the implementation of the non-standard ESI (Edge-Side-Include) specification from the w3. With that you'll be able to use the ESI tags and process them in your favorite golang servers.

### Fork Highlights

This fork includes:
- **Fixed critical bug:** HTTP status codes (302, 404, 500, etc.) are now properly preserved instead of being overridden with 200 OK
- **Enhanced testing:** Comprehensive tests to prevent status code regression
- **Maintained independently:** Module path changed to `github.com/sc0rp10/go-esi` for cleaner dependency management

## What are the ESI tags
The ESI tags were introduced by Akamai to add some dynamic tags and only re-render these parts on the server-side.
The goal of that is to render only specific parts. For example, we want to render a full e-commerce webpage but only the cart is user-dependent. So we could render the "static" parts and store with a predefined TTL (e.g. 60 minutes), and only the cart would be requested to render the block.

There are multiple `esi` tags that we can use but the most used is the `esi:include` because that's the one to request another resource.

We can have many `esi:include` tags in a single response, and each `esi:include` tags can itself have one or more `esi:include` tags.

![esi page example](https://github.com/darkweak/go-esi/blob/master/docs/esi_2.jpg?sanitize=true)

We can have multiple `esi:include` tags in the page to request another resource and add its content to the main page.

![esi process example](https://github.com/darkweak/go-esi/blob/master/docs/esi_1.jpg?sanitize=true)

## References
https://www.w3.org/TR/esi-lang/

## Install
```bash
go get -u github.com/sc0rp10/go-esi
```

## Usage

```go
import (
    // ...
    "github.com/sc0rp10/go-esi/esi"
)

//...

func functionToParseESITags(b []byte, r *http.Request) []byte {
    // Returns the parsed response with parallel fetching enabled
    res := esi.Parse(b, r)

    //...
    return res
}
```

### Parallel Processing (Default Behavior)

**All ESI includes at the same level are automatically fetched in parallel for optimal performance.**

**Performance Benefits:**
- If you have 5 includes that each take 2 seconds to load:
  - Without parallel processing: ~10 seconds total (sequential waterfall)
  - With parallel processing: ~2 seconds total (concurrent fetching)
- Real-world benchmarks show **~7.8x faster** performance with multiple includes

**How It Works:**
- All `<esi:include>` tags at the same level are collected
- HTTP requests are made concurrently using goroutines
- Results are safely assembled and inserted into the response
- Nested ESI tags are processed recursively

**Important Notes:**
- Parallel processing applies to includes at the same level
- Nested ESI tags in fetched content are still processed recursively
- Thread-safe implementation with proper synchronization
- Supports `alt` fallback and `onerror="continue"` attributes

## Available as middleware
- [x] Caddy

### Caddy middleware

#### Installation

```bash
xcaddy build --with github.com/sc0rp10/go-esi
```

Or with a specific version:
```bash
xcaddy build --with github.com/sc0rp10/go-esi@v1.3.0
```

#### Basic Configuration

```caddyfile
example.com {
    esi
    reverse_proxy localhost:9000
}
```

#### Advanced Configuration

The ESI middleware supports several configuration options:

```caddyfile
example.com {
    esi {
        # Enable debug logging (default: off)
        # Use: debug on|off or debug {$ENV_VAR}
        debug on

        # Minimum cache TTL in seconds (default: 300)
        # Overrides upstream Cache-Control headers if they specify a lower value
        minimum_cache_ttl 600

        # Cache TTL jitter in seconds (default: 0)
        # Adds random 0-N seconds to TTL to prevent cache stampede
        cache_ttl_jitter 60

        # Base URL for ESI fragment requests (default: use request URL)
        # Use this to fetch fragments from internal backend, bypassing CDN/WAF
        esi_base_url http://localhost:9000

        # Set custom headers on fragment requests (like proxy_set_header)
        esi_set_header X-Backend-Server "internal"
        esi_set_header X-Request-Source "esi"

        # Special: Override Host header (useful with esi_base_url)
        # esi_set_header Host "example.com"
    }

    reverse_proxy localhost:9000
}
```

**Configuration Options:**

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `debug` | on/off | off | Enable debug logging (supports env vars: `debug {$ESI_DEBUG}`) |
| `minimum_cache_ttl` | int | 300 | Minimum cache TTL in seconds, overrides low upstream values |
| `cache_ttl_jitter` | int | 0 | Random jitter (0-N seconds) added to TTL to spread cache expirations |
| `esi_base_url` | string | "" | Base URL for fragment requests (e.g., `http://localhost:9000`) to bypass CDN/WAF |
| `esi_set_header` | repeatable | - | Set a custom header on fragment requests (name value) |

**Common Use Case - Bypassing WAF/CDN:**

If your ESI fragments are blocked by Cloudflare or WAF rules when making external requests, use `esi_base_url` to fetch them from an internal endpoint:

```caddyfile
example.com {
    esi {
        # Fragments will use http://localhost:9000/_fragment instead of
        # https://example.com/_fragment (which would go through external network)
        esi_base_url http://localhost:9000
    }
    reverse_proxy localhost:9000
}
```

Refer to the [sample Caddyfile](https://github.com/sc0rp10/go-esi/blob/master/middleware/caddy/Caddyfile) for more examples.

### Examples

The repository includes several example implementations:

- **`middleware/server/`** - Basic HTTP server example
- **`middleware/standalone/`** - Standalone ESI processor example
- **`examples/`** - Additional usage examples

## TODO
- [x] choose tag
- [x] comment tag
- [x] escape tag
- [x] include tag
- [x] remove tag
- [x] otherwise tag
- [ ] try tag
- [x] vars tag
- [x] when tag
