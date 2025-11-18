# Fork Notice

This is a fork of the excellent [go-esi](https://github.com/darkweak/go-esi) library originally created by [@darkweak](https://github.com/darkweak).

## Attribution

**Original Author:** darkweak (https://github.com/darkweak)
**Original Repository:** https://github.com/darkweak/go-esi
**Original License:** (see LICENSE file)

All credit for the original implementation, design, and ESI specification work goes to the original author. This codebase is entirely based on their work.

## Why This Fork?

This fork was created to:

1. **Fix Critical Bugs:** Address a critical bug where HTTP status codes (302, 404, 500, etc.) were being overridden with 200 OK, causing SEO issues and breaking proper error handling.

2. **Maintain Compatibility:** Due to the modifications required, maintaining this as a standalone library with its own import path (`github.com/sc0rp10/go-esi`) makes it easier to manage and use without import path mangling.

3. **Custom Enhancements:** Potential future customizations specific to our use case while preserving the ability to track upstream changes.

## Key Modifications

- **Fixed WriteHeader bug:** Status codes are now properly preserved through the response writer wrapper (see `writer/writer.go`)
- **Updated module path:** Changed from `github.com/darkweak/go-esi` to `github.com/sc0rp10/go-esi`
- **Added comprehensive tests:** Includes tests to prevent regression of the status code bug
- **Updated dependencies:** Go 1.25 and latest stable versions of all dependencies
- **Streamlined middlewares:** Removed Traefik and Roadrunner middlewares, focus on Caddy integration

## Upstream

We acknowledge and appreciate the original work. Users looking for the original implementation should visit:
- https://github.com/darkweak/go-esi

## License

This fork maintains the same license as the original project. See the LICENSE file for details.
