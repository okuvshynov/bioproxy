# Warmup Manager Design Document

## Overview

The warmup manager monitors template files for changes and automatically warms up the KV cache for llama.cpp. This ensures that when a user sends a request with a template prefix, the template is already loaded in the KV cache, providing faster responses.

## Architecture

```
┌──────────────────┐
│ Template Watcher │ (already exists)
│  - AddTemplate() │
│  - CheckChanges()│
│  - MarkWarmedUp()│
└────────┬─────────┘
         │ CheckForChanges()
         ↓
┌──────────────────┐
│  Warmup Manager  │
│                  │
│ Background Loop: │
│  1. Check        │
│  2. Queue        │
│  3. Execute      │
│  4. Save Cache   │
└────────┬─────────┘
         │ HTTP requests
         ↓
┌──────────────────┐
│   llama.cpp      │
│  Port: 8081      │
│                  │
│  Endpoints:      │
│  - POST /slots/0 │
│    ?action=      │
│     restore/save │
│  - POST /v1/chat/│
│     completions  │
└──────────────────┘
```

## Components

### 1. Warmup Manager (`internal/warmup/manager.go`)

**Struct:**
```go
type Manager struct {
    config       *config.Config
    watcher      *template.Watcher
    backendURL   string
    client       *http.Client

    mu           sync.Mutex
    running      bool
    stopCh       chan struct{}
}
```

**Key Methods:**
- `New(cfg, watcher, backendURL)` - Create manager
- `Start()` - Start background check loop
- `Stop()` - Stop background loop
- `checkLoop()` - Background goroutine
- `warmupTemplate(prefix)` - Execute warmup for one template
- `restoreKVCache(filename)` - Restore KV cache from file
- `saveKVCache(filename)` - Save KV cache to file

### 2. Configuration (`internal/config/config.go`)

**New field:**
```go
type Config struct {
    // ... existing fields ...

    // WarmupCheckInterval is how often to check templates for changes (seconds)
    // Default: 30
    WarmupCheckInterval int `json:"warmup_check_interval"`
}
```

## Workflow

### Startup Sequence

1. Load configuration
2. Create template watcher
3. Add all templates from config.Prefixes
4. Create warmup manager
5. Start warmup manager (background goroutine)
6. Start proxy server

### Background Check Loop

```
Loop every WarmupCheckInterval seconds:
  1. Call watcher.CheckForChanges()
  2. For each changed template:
     a. warmupTemplate(prefix)
     b. watcher.MarkWarmedUp(prefix)
  3. Sleep until next check
```

### Warmup Execution for Single Template

```
warmupTemplate(prefix):
  1. Restore KV cache (optional, may fail first time)
     POST /slots/0?action=restore
     Body: {"filename": "prefix.bin"}

  2. Generate warmup request
     messages = [{"role": "user", "content": ""}]
     Process template with empty message

  3. Send to llama.cpp
     POST /v1/chat/completions
     Body: {
       "messages": [{"role": "user", "content": processed_template}],
       "max_tokens": 1,  // Minimal generation
       "stream": false
     }

  4. Save KV cache
     POST /slots/0?action=save
     Body: {"filename": "prefix.bin"}

  5. Mark as warmed up
```

## Design Decisions

### 1. Warmup Message Content

**Decision:** Use empty string for `<{message}>` placeholder

**Rationale:**
- Consistent behavior (always same warmup)
- Minimal tokens generated (just template content)
- User message doesn't affect KV cache warmup

**Example:**
```
Template: "You are a code assistant. <{message}>"
Warmup:   "You are a code assistant. "
```

### 2. Check Frequency

**Decision:** Configurable via `WarmupCheckInterval` (default 30 seconds)

**Rationale:**
- Development: frequent checks (10s) for rapid iteration
- Production: less frequent (60s) to reduce overhead
- User can tune based on their needs

**Configuration:**
```json
{
  "warmup_check_interval": 30
}
```

### 3. Priority and Queuing

**Decision:** Simple background execution, no priority queue (Phase 6 feature)

**Current Implementation:**
- One warmup at a time (sequential)
- Runs in background goroutine
- Does not block user requests

**Future (Phase 6):**
- Priority queue (user requests > warmup)
- Cancellable warmup requests
- Rate limiting

### 4. Error Handling

**Decision:** Log errors and retry on next check cycle

**Scenarios:**

**llama.cpp is down:**
```
ERROR: Failed to warmup template @code: connection refused
(Will retry in 30 seconds)
```

**Template file deleted:**
```
WARNING: Template @code file not found, removing from watch list
```

**KV cache restore fails (first time):**
```
INFO: KV cache not found for @code (expected on first warmup)
```

**Warmup request fails:**
```
ERROR: Warmup request for @code failed: timeout
(Will retry in 30 seconds)
```

## Implementation Phases

### Phase 1: Basic Warmup (This Session)
- [x] Design architecture
- [ ] Add WarmupCheckInterval to config
- [ ] Create warmup manager module
- [ ] Implement KV cache restore/save
- [ ] Implement basic warmup loop
- [ ] Integration with proxy
- [ ] Unit tests
- [ ] Manual test with llama.cpp

### Phase 2: Template Injection (Next)
- [ ] Intercept /v1/chat/completions in proxy
- [ ] Parse request body
- [ ] Check for prefix match
- [ ] Process template with user message
- [ ] Forward modified request

### Phase 3: KV Cache Load on User Request (Later)
- [ ] Before forwarding user request
- [ ] Check if template has KV cache
- [ ] Restore KV cache
- [ ] Forward request
- [ ] Save updated KV cache after response

### Phase 4: Request Queue & Prioritization (Future)
- [ ] Request queue with priority
- [ ] Gate to llama.cpp (one request at a time)
- [ ] Pause warmup when user requests arrive
- [ ] Resume warmup when idle

## File Structure

```
bioproxy/
├── internal/
│   ├── warmup/
│   │   ├── manager.go       (NEW - warmup manager)
│   │   ├── manager_test.go  (NEW - unit tests)
│   │   └── README.md        (NEW - documentation)
│   ├── config/
│   │   └── config.go        (MODIFY - add WarmupCheckInterval)
│   └── proxy/
│       └── proxy.go         (MODIFY - integrate warmup manager)
├── cmd/bioproxy/
│   └── main.go              (MODIFY - wire up warmup manager)
└── WARMUP_DESIGN.md         (this file)
```

## API Reference

### llama.cpp Slot Management API

**Restore KV Cache:**
```http
POST /slots/0?action=restore
Content-Type: application/json

{
  "filename": "prefix.bin"
}

Response 200 OK:
{
  "filename": "prefix.bin",
  "n_loaded": 1234,  // tokens loaded
  "slot_id": 0
}

Response 404 Not Found:
{
  "error": "file not found"  // First warmup, cache doesn't exist yet
}
```

**Save KV Cache:**
```http
POST /slots/0?action=save
Content-Type: application/json

{
  "filename": "prefix.bin"
}

Response 200 OK:
{
  "filename": "prefix.bin",
  "n_saved": 1234,  // tokens saved
  "slot_id": 0,
  "timings": {...}
}
```

## Metrics to Track

Add to admin server metrics:

- `bioproxy_warmup_checks_total` - Total warmup checks performed
- `bioproxy_warmup_executions_total{prefix}` - Warmup executions by template
- `bioproxy_warmup_errors_total{prefix,type}` - Warmup errors
- `bioproxy_warmup_duration_seconds{prefix}` - Warmup execution time
- `bioproxy_kv_cache_saves_total{prefix}` - KV cache saves
- `bioproxy_kv_cache_restores_total{prefix,status}` - KV cache restores (success/not_found/error)

## Testing Strategy

### Unit Tests

**Test warmup manager:**
- Start/Stop lifecycle
- Check loop timing
- Error handling (backend down)
- KV cache operations

**Mock llama.cpp server:**
- Record slot operations
- Simulate restore 404 (first time)
- Simulate save success
- Test warmup request format

### Manual Integration Tests

**With real llama.cpp:**
1. Start llama.cpp server
2. Create test template file
3. Add to config
4. Start bioproxy
5. Verify warmup executes
6. Check KV cache file created
7. Modify template
8. Verify re-warmup
9. Check cache file updated

## Configuration Example

```json
{
  "proxy_host": "localhost",
  "proxy_port": 8088,
  "admin_host": "localhost",
  "admin_port": 8089,
  "backend_url": "http://localhost:8081",
  "warmup_check_interval": 30,
  "prefixes": {
    "@code": "/path/to/code_template.txt",
    "@debug": "/path/to/debug_template.txt"
  }
}
```

## Logging Examples

```
INFO: Starting warmup manager (check interval: 30s)
INFO: Warmup manager background loop started
INFO: Checking templates for changes...
INFO: Template @code changed, needs warmup
INFO: Starting warmup for @code
INFO: KV cache not found for @code (first warmup)
INFO: Sending warmup request for @code
INFO: Warmup request completed for @code (234 tokens, 1.2s)
INFO: Saving KV cache for @code
INFO: KV cache saved for @code (234 tokens)
INFO: Template @code warmup complete
WARNING: Failed to warmup @debug: connection refused (will retry)
```

## Security Considerations

1. **Template files:**
   - Should be readable by bioproxy process
   - Not writable by untrusted users
   - Path traversal protection in config

2. **KV cache files:**
   - Stored in llama.cpp's cache directory
   - Named by prefix (sanitize prefix if needed)
   - Auto-created by llama.cpp

3. **llama.cpp access:**
   - Backend URL should be trusted
   - No authentication needed (local network)
   - Consider firewall rules in production

## Future Enhancements

1. **Warmup on demand** - Manual trigger via admin API
2. **Warmup statistics** - Track success rate, timing, cache sizes
3. **Template validation** - Check template syntax before warmup
4. **Parallel warmup** - Warm multiple templates simultaneously
5. **Smart scheduling** - Prioritize frequently used templates
6. **Cache expiration** - Remove old caches
7. **Differential warmup** - Only warm changed parts of template

## Questions & Decisions Log

**Q1: What content to use for `<{message}>` during warmup?**
A: Empty string "" - most consistent behavior

**Q2: How often to check for template changes?**
A: Configurable WarmupCheckInterval (default 30s)

**Q3: Should warmup block user requests?**
A: No, run in background (queue in Phase 6)

**Q4: How to handle warmup errors?**
A: Log error, retry on next check cycle

## References

- llama.cpp server API: `/path/to/llama.cpp/tools/server/README.md`
- bioproxy plan: `plan.md`
- Template watcher: `internal/template/watcher.go`
