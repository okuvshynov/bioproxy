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
1. **Project structure** - Created go.mod, .gitignore, GitHub Actions CI
2. **Configuration module** (`internal/config/`)
   - Config struct with proxy settings (ProxyHost, ProxyPort, AdminHost, AdminPort, BackendURL)
   - Template prefix mappings
   - Default values with JSON override support
   - Comprehensive test coverage (7 tests)
3. **Template watching** (`internal/template/`)
   - Watcher monitors templates and includes for changes
   - Non-recursive placeholder processing: `<{message}>` and `<{filepath}>`
   - Template-level change detection (hash of processed template)
   - Thread-safe operations with RWMutex
   - Comprehensive logging (INFO/WARNING/ERROR)
   - 12 tests covering edge cases

### 🚧 Next Steps (Proxy Implementation)

#### Phase 1: Basic Reverse Proxy (NEXT)
**Goal**: Simple passthrough proxy that forwards all requests to llama.cpp

**Implementation**:
```go
// internal/proxy/proxy.go
type Proxy struct {
    config      *config.Config
    backend     *url.URL
    reverseProxy *httputil.ReverseProxy
}

func New(cfg *config.Config) (*Proxy, error)
func (p *Proxy) Start() error
func (p *Proxy) Stop() error
```

**What to build**:
1. HTTP server on ProxyHost:ProxyPort (default localhost:8088)
2. Forward ALL requests to BackendURL (http://localhost:8081)
3. Use httputil.ReverseProxy from stdlib
4. Basic logging of requests

**Testing**:
- Unit tests with httptest.NewServer
- Manual test: curl http://localhost:8088/health -> llama.cpp

**Files to create**:
- `internal/proxy/proxy.go`
- `internal/proxy/proxy_test.go`

#### Phase 2: Admin Endpoints
**Goal**: Add admin server with status and config endpoints

**Implementation**:
```go
// internal/admin/admin.go
type AdminServer struct {
    config *config.Config
    startTime time.Time
}

// GET /status - JSON with uptime, config summary
// GET /config - JSON with full configuration
```

**What to build**:
1. HTTP server on AdminHost:AdminPort (default localhost:8089)
2. `/status` endpoint - uptime, version, backend status
3. `/config` endpoint - return current configuration

**Testing**:
- Unit tests for handlers
- Manual test: curl http://localhost:8089/status

**Files to create**:
- `internal/admin/admin.go`
- `internal/admin/admin_test.go`

#### Phase 3: Template Injection in Proxy
**Goal**: Intercept `/v1/chat/completions` and inject templates

**What to modify**:
1. Parse request body in proxy
2. Check first user message for prefix match
3. If match found:
   - Load template using template.Watcher
   - Process template with user message
   - Replace message content in request
4. Forward modified request to llama.cpp

**Testing approach**:
- Need mock llama.cpp server for integration tests
- Mock should record requests to validate injection worked

#### Phase 4: Mock llama.cpp Server
**Location**: `tests/mock/llama.go`

**Features**:
```go
type LlamaServer struct {
    server *httptest.Server
    mu sync.Mutex
    requests []RecordedRequest
}

type RecordedRequest struct {
    Path string
    Method string
    Headers http.Header
    Body []byte
    BodyHash string
}

func (m *LlamaServer) LastRequest() RecordedRequest
func (m *LlamaServer) RequestCount() int
```

**Endpoints to mock**:
- POST `/v1/chat/completions` - return valid OpenAI response
- POST `/slots/0?action=save` - record call
- POST `/slots/0?action=restore` - record call
- GET `/metrics` - return fake prometheus metrics
- GET `/health` - return OK

#### Phase 5: KV Cache Integration
**Goal**: Load/save KV cache around requests

**What to build**:
1. Before processing request with template:
   - POST to `/slots/0?action=restore` with filename `{prefix}.bin`
   - Handle 404 if cache doesn't exist yet
2. After request completes:
   - POST to `/slots/0?action=save` with filename `{prefix}.bin`
3. Always use slot 0

#### Phase 6: Request Queue & Prioritization
**Goal**: Queue requests, prioritize user requests over warmup

**What to build**:
1. Request queue with priority levels (user vs warmup)
2. Only one request to llama.cpp at a time
3. Warmup only when no user requests waiting
4. Plan for future: cancellable warmup requests

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

## Housekeeping & Improvements

### Logging Migration
- [ ] **Migrate from `log` to `log/slog`**
  - Replace manual "INFO:", "ERROR:" prefixes with proper log levels
  - Use structured logging (key-value pairs) for better observability
  - Add level filtering capability (DEBUG, INFO, WARN, ERROR)
  - Support both text (development) and JSON (production) output formats
  - Remains stdlib-only, no external dependencies
  - Files to update:
    - `cmd/bioproxy/main.go`
    - `internal/proxy/proxy.go`
    - `internal/admin/admin.go`
    - `internal/template/template.go`
    - `internal/config/config.go`

### Streaming Safety Guard (Optional)
- [ ] **Consider adding runtime guard for response body reads**
  - **Issue:** Reading `resp.Body` in `ModifyResponse` breaks SSE streaming
  - **Current protection:** Extensive comments in code + `TestManualStreamingChat`
  - **Potential enhancement:** Add wrapper that panics if body is read
  - **Options:**
    1. Development-only guard (enabled via build tag or const)
    2. Always-on guard (slight performance overhead)
    3. Test-only detection (no production overhead)
  - **Location:** `internal/proxy/proxy.go` ModifyResponse callback
  - **Trade-off:** Safety vs performance (guard adds wrapper overhead)
  - **Current approach:** Documentation-first, rely on tests
  - **See commit:** Search for "CRITICAL STREAMING REQUIREMENT" comments

