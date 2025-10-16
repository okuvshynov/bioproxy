# bioproxy

A reverse proxy for llama.cpp that enables KV cache warmup for templated prompts.

## Quick Start

### Prerequisites

1. **llama.cpp server running** on localhost:8081:
   ```bash
   cd /path/to/llama.cpp
   ./llama-server -m /path/to/model.gguf --port 8081
   ```

2. **Build bioproxy**:
   ```bash
   go build -o bioproxy ./cmd/bioproxy
   ```

### Running the Proxy

```bash
./bioproxy
```

You should see:
```
🚀 Starting bioproxy - llama.cpp reverse proxy with KV cache warmup

Configuration:
  Proxy listening on: http://localhost:8088
  Backend llama.cpp:  http://localhost:8081
  Admin (future):     http://localhost:8089

✅ Proxy server is running!

Try these commands:
  curl http://localhost:8088/health
  curl http://localhost:8088/v1/models

Press Ctrl+C to stop...
```

### Testing the Proxy

**Proxy endpoints (port 8088):**
```bash
# Health check (forwarded to llama.cpp)
curl http://localhost:8088/health

# List models (forwarded to llama.cpp)
curl http://localhost:8088/v1/models

# Chat completion (forwarded to llama.cpp)
curl http://localhost:8088/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer no-key" \
  -d '{
    "messages": [{"role": "user", "content": "Hello!"}],
    "max_tokens": 50
  }'
```

**Admin endpoints (port 8089):**
```bash
# Health and uptime
curl http://localhost:8089/health

# Prometheus-style metrics
curl http://localhost:8089/metrics
```

**From browser:**
- Proxy health: http://localhost:8088/health
- Admin health: http://localhost:8089/health
- Metrics: http://localhost:8089/metrics

**From any OpenAI-compatible client:**
- Point your client to `http://localhost:8088`
- All requests will be forwarded to llama.cpp and metrics will be collected

### Architecture

```
Client/Browser → bioproxy proxy (8088) → llama.cpp (8081)
                    ↓
                 metrics
                    ↓
              Admin server (8089) → /health, /metrics
```

- **Proxy (8088)**: Forwards requests to llama.cpp, collects metrics
- **Admin (8089)**: Provides health status and Prometheus-formatted metrics

## Development

### Running Tests

**Unit tests (fast, no llama.cpp needed):**
```bash
go test ./...
```

**Manual tests (requires llama.cpp):**
```bash
go clean -testcache && go test -tags=manual -v ./internal/proxy/...
```

### Project Structure

- `cmd/bioproxy/` - Main executable
- `internal/config/` - Configuration management
- `internal/proxy/` - Reverse proxy implementation
- `internal/admin/` - Admin server with health and metrics endpoints
- `internal/template/` - Template watching and processing

## Current Status

**Phase 1 Complete:** ✅ Basic reverse proxy
- Forwards all HTTP requests to llama.cpp
- Logs requests/responses
- Handles backend errors gracefully
- Minimal overhead (~1-2ms)

**Phase 2 Complete:** ✅ Admin endpoints and metrics
- Admin server on separate port (8089)
- `/health` endpoint with uptime information
- `/metrics` endpoint with Prometheus-style metrics
- Metrics include: request counts by endpoint and status code
- Thread-safe metrics collection

**Coming Next:**
- Phase 3: Template injection for chat completions
- Phase 4: KV cache save/restore integration
- Phase 5: Request queue with warmup prioritization

## License

See [LICENSE](LICENSE) file.