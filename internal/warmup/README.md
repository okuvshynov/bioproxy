# Warmup Module

The warmup module monitors template files for changes and automatically warms up KV caches in llama.cpp. This ensures templates are pre-loaded in the KV cache for faster response times.

## Architecture

```
Template Watcher → Warmup Manager → llama.cpp
   (detects changes)  (background loop)  (KV cache ops)
```

For each changed template:
1. Restore existing KV cache (if available)
2. Process template with empty message
3. Send to llama.cpp with `max_tokens=1`
4. Save updated KV cache
5. Mark template as warmed up

## Files

- **manager.go** - Warmup manager with background check loop
- **manager_test.go** - Unit tests with mock llama.cpp server (9 tests)
- **manual_test.go** - Integration tests requiring real llama.cpp (7 tests)

## Running Tests

### Unit Tests (No llama.cpp Required)

```bash
go test ./internal/warmup/...
```

These tests use a mock llama.cpp server to verify warmup logic, KV cache operations, and error handling.

### Manual Integration Tests (Requires llama.cpp)

```bash
go clean -testcache && go test -tags=manual -v ./internal/warmup/...
```

**Important:** llama.cpp must be started with `--slot-save-path` for these tests.

See [MANUAL_TESTING.md](../../MANUAL_TESTING.md) in the project root for complete guide.

## Usage Example

```go
import (
    "github.com/oleksandr/bioproxy/internal/config"
    "github.com/oleksandr/bioproxy/internal/template"
    "github.com/oleksandr/bioproxy/internal/warmup"
)

// Create config
cfg := &config.Config{
    BackendURL:          "http://localhost:8081",
    WarmupCheckInterval: 30, // seconds
}

// Create watcher and add templates
watcher := template.NewWatcher()
watcher.AddTemplate("@code", "/path/to/code_template.txt")

// Create and start warmup manager
mgr := warmup.New(cfg, watcher, cfg.BackendURL)
if err := mgr.Start(); err != nil {
    log.Fatal(err)
}
defer mgr.Stop()

// Manager now runs in background, checking every 30s
```

## Configuration

Add to `config.json`:

```json
{
  "warmup_check_interval": 30,
  "prefixes": {
    "@code": "/path/to/code_template.txt",
    "@debug": "/path/to/debug_template.txt"
  }
}
```

## Current Features

- ✅ Background template monitoring
- ✅ Automatic warmup on template changes
- ✅ KV cache restore/save operations
- ✅ Error handling with retry on next cycle
- ✅ Graceful start/stop
- ✅ Thread-safe operations

## Implementation Notes

### Warmup Sequence

For each template needing warmup:
1. Attempt to restore KV cache (404 on first warmup is expected)
2. Process template with `<{message}>` replaced by empty string
3. Send minimal completion request to llama.cpp
4. Save KV cache with filename `{prefix}.bin` (@ prefix removed)

### Error Handling

Errors don't stop the manager - they're logged and retried on next check cycle:
- Connection failures: Log and retry
- Missing cache files: Expected on first warmup
- Template read errors: Skip and continue

### Logging

```
INFO: Warmup manager background loop started
INFO: Template @code changed, needs warmup
INFO: Starting warmup for @code
INFO: KV cache saved for code.bin
INFO: Template @code warmup complete
```

## Design

See [WARMUP_DESIGN.md](../../WARMUP_DESIGN.md) for complete architecture documentation.

## Test Coverage

**Unit Tests:** 9 tests, all passing ✅
- Manager lifecycle
- Warmup execution
- KV cache operations
- Error scenarios
- Change detection

**Manual Tests:** 7 tests, requires llama.cpp
- Full warmup workflow
- Template change detection
- Multiple templates
- Real completion after warmup
- KV cache API verification
