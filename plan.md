In the experiments/bioproxy directory there's a basic implementation of a proxy server for LLM endpoint. The main usecase for it is warmup the KV cache for the messages with a given configured prefix.

Let's create a new version of it in the current directory (project/bioproxy). 

Process/engineering guidelines:
1. New version need to be a better and improved version specifically for llama.cpp.
2. It should also serve as a learning exercise - while being very experienced, I don't have much exposure to Golang so I'd like to make sure I understand everything you do. Comment your code extensively.
3. New version should be well-covered with unit and integration tests. For integration testing, we can use llama.cpp itself or create mock server.
4. We should start step-by step and use small basic changes.
5. From the beginning, use prometheus-like format for metric export endpoint
6. avoid external dependencies, implement as much as possible using golang stdlib
7. modularity: have separate modules, avoid long files with lots on functionality.
8. composition > inheritance. Avoid creating lots of abstractions. There's no need to create a base module for 'llm-server' if we ever would want to support more than just llama.cpp (say, lmstudio, vllm, sglang). Each implementation can live in a separate module, and common functionality (if any) can be extracted to common 'utils' module.
9. documentation. Should be complete, but fairly high-level. Let's write extensive, sometimes even excessive comments in the code.
10. Every step should be covered with tests, add appropriate metrics exported.
11. Every change should be small enough to understand easily.
12. Avoid creating empty directories - create directories only when you have files to put in them.

Product/feature requirements:
1. proxy should read a config file (with sensible default, say, ~/.config/bioproxy/conf.json
2. configuration should have mapping for prefix -> template file.
3. each config option can be overridden. I suggest we have default value (in code), which can be overridden by config value in config file, which can be overridden by command-line options. For example, we can have configuration for llm server address and port proxy itself listens on. For that port, default value in the code is 8081, config might have 8083, and we might supply '-port 8088' in the command line, and 8088 will be used
4. internally, proxy will iterate over all templates, fill them in with file content (same as current experimental proxy implementation), and if it has changed from the last update, mark them as 'warmup needed'
5. warmup process itself will become more complex. llama.cpp server (check llama.cpp/tools/server/README.md) supports save/load of kv cache to file. Whenever we plan to issue any request with template name foo, we will: 
5a. try loading kv cache from filename foo.bin. It might not exist yet, and we need to handle that.
5b. after that, run the query (warmup or user-initiated).
5c. after query is completed, save the kv cache to foo.bin
6. assume we work on slot # 0 all the time
7. maintain internal request queue and act as a gate for the llama.cpp server. Warmup queries should be issued only if there's no user-initiated query waiting. At the moment, llama.cpp server doesn't support request cancellation, but we might need to implement that in future.

## Implementation Progress

### ✅ Completed

#### 1. Core Infrastructure
- **Project structure** - Created go.mod, .gitignore, GitHub Actions CI
- **Configuration module** (`internal/config/`)
  - Config struct with all settings (ProxyHost, ProxyPort, AdminHost, AdminPort, BackendURL)
  - Template prefix mappings (Prefixes map)
  - WarmupCheckInterval configuration
  - Default values with JSON override and CLI flag support
  - Comprehensive test coverage (7 tests)

#### 2. Template System (`internal/template/`)
- **Template Watcher** - Monitors templates and includes for changes
  - Non-recursive placeholder processing: `<{message}>` and `<{filepath}>`
  - Template-level change detection (hash of processed template)
  - Thread-safe operations with RWMutex
  - NeedsWarmup flag tracking
  - Comprehensive logging (INFO/WARNING/ERROR)
  - 12 tests covering edge cases

#### 3. Basic Reverse Proxy (`internal/proxy/`)
- **Simple HTTP Proxy** - Forwards ALL requests to llama.cpp (NO template injection yet)
  - Runs on ProxyHost:ProxyPort (default localhost:8088)
  - Uses httputil.ReverseProxy from stdlib
  - Request/response logging
  - Streaming support (SSE for `stream: true` requests)
  - Metrics collection for all requests
  - 8 unit tests + manual streaming test
  - **NOTE**: Currently just a passthrough proxy, does NOT intercept or modify requests

#### 4. Admin Server (`internal/admin/`)
- **Monitoring & Metrics** - Separate admin HTTP server
  - Runs on AdminHost:AdminPort (default localhost:8089)
  - `GET /health` - Health check with uptime
  - `GET /metrics` - Prometheus-format metrics endpoint
  - **Proxy Metrics**:
    - `bioproxy_requests_total{endpoint,status}` - Request counts
    - `bioproxy_requests_count` - Total requests
    - `bioproxy_uptime_seconds` - Server uptime
  - **Warmup Metrics**:
    - `bioproxy_warmup_checks_total` - Total warmup check cycles
    - `bioproxy_warmup_executions_total{prefix}` - Executions per template
    - `bioproxy_warmup_errors_total{prefix,type}` - Errors by template and type
    - `bioproxy_warmup_duration_seconds_total{prefix}` - Total duration
    - `bioproxy_warmup_duration_seconds_count{prefix}` - Operation count
    - `bioproxy_kv_cache_saves_total{prefix}` - Successful saves
    - `bioproxy_kv_cache_restores_total{prefix,status}` - Restore attempts
  - Thread-safe metrics with RWMutex
  - 3 tests

#### 5. Warmup Manager (`internal/warmup/`)
- **Background KV Cache Warmup** - Automatic template warmup (runs independently)
  - Background goroutine with configurable check interval
  - Monitors template changes via Watcher
  - **Warmup Sequence**:
    1. Attempt to restore KV cache from `{prefix}.bin`
    2. Send minimal completion request with processed template
    3. Save KV cache to `{prefix}.bin`
  - Comprehensive error handling and retry logic
  - Metrics recording for all operations
  - 9 unit tests + 7 manual integration tests
  - Design document: `WARMUP_DESIGN.md`

#### 6. Main Application (`cmd/bioproxy/`)
- **Complete Integration** - All components working together
  - Configuration loading from file with CLI overrides
  - Template watcher initialization
  - Shared metrics across proxy, admin, and warmup
  - Graceful shutdown with signal handling
  - Startup banner with configuration summary

#### 7. Documentation
- **README.md** - Complete setup guide with template examples
- **WARMUP_DESIGN.md** - Architecture design for warmup system
- **MANUAL_TESTING.md** - Manual testing procedures
- Extensive inline code comments

### 🚧 Next Steps

#### NEXT: Template Injection in Proxy
**Status**: Not yet implemented (proxy currently just forwards all requests)

**Goal**: Intercept `/v1/chat/completions` and inject templates when user message starts with configured prefix

**What to modify in `internal/proxy/proxy.go`**:
1. Add template.Watcher field to Proxy struct
2. Create custom handler (instead of using ReverseProxy directly) for `/v1/chat/completions`
3. In the handler:
   - Parse incoming request body (JSON with messages array)
   - Check first user message for prefix match (e.g., "@code", "@debug")
   - If prefix found:
     - Extract message without prefix: "@code how do I..." → "how do I..."
     - Use watcher.ProcessTemplate(prefix, message) to get processed template
     - Replace the message content with processed template result
     - Marshal modified request back to JSON
   - Forward request to llama.cpp (with or without modification)
   - Stream response back to client
4. Keep other endpoints forwarded directly via ReverseProxy

**Key Challenges**:
- Must preserve streaming capability (don't break SSE)
- Need to handle both streaming and non-streaming requests
- Buffer/modify request, but stream response unchanged
- Error handling when template processing fails

**Testing approach**:
- Unit test with mock backend to verify template injection
- Test that non-prefixed messages pass through unchanged
- Test that other endpoints (/health, /v1/models) still work
- Manual test with real llama.cpp for streaming

**Example Flow**:
```
Client sends:
{
  "messages": [
    {"role": "user", "content": "@code How do I reverse a string?"}
  ]
}

Proxy intercepts, processes template, sends to llama.cpp:
{
  "messages": [
    {"role": "user", "content": "You are a coding assistant...\n\nUser question: How do I reverse a string?"}
  ]
}

llama.cpp response streams back to client unchanged
```

#### Future: Request Queue & Prioritization
**Status**: Not yet designed in detail

**Current Situation**:
- Proxy forwards all user requests immediately to llama.cpp
- Warmup manager runs independently, sends warmup requests directly to llama.cpp
- No coordination between user and warmup requests
- llama.cpp handles queueing internally (but we have no visibility/control)

**Goal**: Queue and prioritize requests (user requests before warmup)

**What to build**:
1. **Request Queue** (`internal/queue/`)
   - Priority queue with two levels: user (high) and warmup (low)
   - Single-slot processing (only one request to llama.cpp at a time)
   - Thread-safe operations
2. **Proxy Integration**:
   - Enqueue user requests instead of forwarding directly
   - Wait for completion before returning response to client
   - Handle streaming responses through queue
3. **Warmup Integration**:
   - Warmup manager enqueues warmup requests (low priority)
   - Warmup only executes when no user requests waiting
4. **Metrics**: Queue depth, wait time, etc.

**Design Questions** (to be resolved):
- How to handle streaming with queued requests?
- Should we cancel in-progress warmup when user request arrives?
- What's the timeout for queued requests?
- How to handle llama.cpp returning errors?

**Note**: This is a significant architectural change. Template injection should be done first, as it's simpler and doesn't require queue infrastructure.

## Architecture Decisions

### Port Configuration
- **llama.cpp**: 8081 (backend)
- **Proxy user port**: 8088 (OpenAI-compatible API)
- **Proxy admin port**: 8089 (status, config, proxy metrics)

### Dual Port Design
- User port: forwards to llama.cpp, intercepts `/v1/chat/completions`
- Admin port: proxy-specific endpoints
- Allows separate access control (admin can be localhost-only)

### Testing Strategy
- **Unit tests**: httptest for handlers
- **Integration tests**: Mock llama.cpp server (stateful, records requests)
- **Manual tests**: Real llama.cpp server at localhost:8081
- Mock server avoids CI dependency on large models

## Future Improvements (Backlog)

### Logging Migration (Optional)
Currently using stdlib `log` package with manual "INFO:", "ERROR:" prefixes. Could migrate to `log/slog` for:
- Proper log levels (DEBUG, INFO, WARN, ERROR)
- Structured logging (key-value pairs)
- Level filtering
- JSON output option for production
- Still stdlib-only, no external dependencies

**Files affected**: `cmd/bioproxy/main.go`, `internal/proxy/proxy.go`, `internal/admin/admin.go`, `internal/template/template.go`, `internal/warmup/manager.go`, `internal/config/config.go`

**Trade-off**: Current approach is simple and works. Migration would be mostly cosmetic unless we need structured logging for log aggregation tools.

### Streaming Safety Guard (Optional)
**Issue**: Reading `resp.Body` in `ModifyResponse` breaks SSE streaming
**Current protection**: Extensive comments + `TestManualStreamingChat` test
**Potential enhancement**: Runtime guard that panics if body is read

**Options**:
1. Development-only guard (enabled via build tag)
2. Always-on guard (slight performance overhead)
3. Test-only detection (no production impact)

**Location**: `internal/proxy/proxy.go` ModifyResponse callback
**Current approach**: Documentation-first, rely on tests (search for "CRITICAL STREAMING REQUIREMENT")
**Decision**: Keep current approach unless we experience streaming bugs in practice

