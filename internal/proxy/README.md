# Proxy Module

The proxy module implements a reverse proxy server that forwards HTTP requests to a llama.cpp backend server. This is the core component that sits between clients and llama.cpp, enabling template injection and KV cache warmup functionality.

## Architecture

```
Client → Proxy (localhost:8088) → llama.cpp (localhost:8081)
```

The proxy uses Go's standard library `httputil.ReverseProxy` for efficient request forwarding with minimal overhead.

## Files

- **proxy.go** - Main proxy implementation with request forwarding and logging
- **proxy_test.go** - Unit tests using mock HTTP servers (9 tests)
- **manual_test.go** - Integration tests requiring a real llama.cpp server (6 tests)

## Running Tests

### Unit Tests (No llama.cpp Required)

```bash
go test ./internal/proxy/...
```

These tests cover proxy creation, request forwarding, headers, error handling, and lifecycle.

### Manual Integration Tests (Requires llama.cpp)

```bash
go clean -testcache && go test -tags=manual -v ./internal/proxy/...
```

See [docs/MANUAL_TESTING.md](../../docs/MANUAL_TESTING.md) for complete guide.

## Usage Example

```go
import (
    "github.com/oleksandr/bioproxy/internal/config"
    "github.com/oleksandr/bioproxy/internal/proxy"
)

// Create configuration
cfg := &config.Config{
    ProxyHost:  "localhost",
    ProxyPort:  8088,
    BackendURL: "http://localhost:8081",
}

// Create proxy
p, err := proxy.New(cfg)
if err != nil {
    log.Fatal(err)
}

// Start proxy (blocking)
if err := p.Start(); err != nil {
    log.Fatal(err)
}
defer p.Stop()

// Proxy now forwards requests from :8088 to :8081
```

## Current Features

- ✅ Reverse proxy with request/response forwarding
- ✅ HTTP method support (GET, POST, PUT, DELETE, PATCH)
- ✅ Header forwarding (bidirectional)
- ✅ Request/response logging
- ✅ Error handling for backend failures
- ✅ Graceful start/stop
- ✅ Thread-safe operations

## Planned Features

- 🚧 Template injection for chat completions
- 🚧 KV cache management (load before request, save after)
- 🚧 Request queue with prioritization
- 🚧 Warmup request handling
- 🚧 Metrics export (Prometheus format)

## Implementation Notes

### Logging

All requests are logged with INFO level:
```
INFO: Proxying POST /v1/chat/completions -> http://localhost:8081/v1/chat/completions
INFO: Backend responded with status 200 for POST /v1/chat/completions
```

Errors are logged with ERROR level:
```
ERROR: Proxy error for GET /test: dial tcp: connection refused
```

### Error Handling

When the backend is unavailable, the proxy returns:
- **Status:** 502 Bad Gateway
- **Body:** "Backend server unavailable"

### Performance

The proxy adds minimal overhead:
- Average latency: ~1-2ms on localhost
- Uses efficient connection pooling
- No request/response buffering for streaming

### Streaming Support

The proxy correctly handles Server-Sent Events (SSE) for streaming responses:
- `httputil.ReverseProxy` automatically flushes chunks as they arrive
- No buffering of streaming responses
- Headers (e.g., `Content-Type: text/event-stream`) are preserved
- Critical for real-time token generation from llama.cpp

## Test Coverage

**Unit Tests:** 9 tests, all passing ✅
- Basic functionality
- Error cases
- Different HTTP methods
- Header handling
- Lifecycle management

**Manual Tests:** 6 tests, requires llama.cpp
- Health checks
- KV cache operations
- Chat completions (non-streaming and streaming)
- SSE streaming verification
- Performance benchmarks
