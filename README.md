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

**From terminal (curl):**
```bash
# Health check
curl http://localhost:8088/health

# List models
curl http://localhost:8088/v1/models

# Chat completion
curl http://localhost:8088/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer no-key" \
  -d '{
    "messages": [{"role": "user", "content": "Hello!"}],
    "max_tokens": 50
  }'
```

**From browser:**
- Open http://localhost:8088/health in your browser
- You should see: `{"status":"ok"}`

**From any OpenAI-compatible client:**
- Point your client to `http://localhost:8088`
- All requests will be forwarded to llama.cpp

### Architecture

```
Client/Browser → bioproxy (8088) → llama.cpp (8081)
```

The proxy currently acts as a passthrough, forwarding all requests to llama.cpp while logging traffic.

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
- `internal/template/` - Template watching and processing

## Current Status

**Phase 1 Complete:** ✅ Basic reverse proxy
- Forwards all HTTP requests to llama.cpp
- Logs requests/responses
- Handles backend errors gracefully
- Minimal overhead (~1-2ms)

**Coming Next:**
- Phase 2: Admin endpoints for status/metrics
- Phase 3: Template injection for chat completions
- Phase 4: KV cache save/restore integration
- Phase 5: Request queue with warmup prioritization

## License

See [LICENSE](LICENSE) file.