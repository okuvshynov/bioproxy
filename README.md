# bioproxy

A reverse proxy for llama.cpp that enables automatic KV cache warmup for templated prompts.

## Quick Start

### Prerequisites

**llama.cpp server** running with KV cache support:
```bash
cd /path/to/llama.cpp
./llama-server -m models/model.gguf --port 8081 --slot-save-path ./kv_cache
```

Note: `--slot-save-path` is required for KV cache warmup to work.

### Installation

```bash
git clone https://github.com/okuvshynov/bioproxy
cd bioproxy
go build -o bioproxy ./cmd/bioproxy
```

### Setup with Templates

**1. Use the example configuration:**

The repository includes example configuration and templates in the `examples/` directory:

```bash
# Copy example config to use as a starting point
cp examples/config.json config.json

# Or use it directly
./bioproxy -config examples/config.json
```

The example includes:
- `examples/config.json` - Full configuration with template mappings
- `examples/templates/code_assistant.txt` - Coding assistant template
- `examples/templates/debug_helper.txt` - Debugging helper with file inclusion
- `examples/templates/debugging_guide.txt` - Included debugging reference

**2. Run bioproxy:**

```bash
./bioproxy -config examples/config.json
```

You should see:
```
🚀 Starting bioproxy - llama.cpp reverse proxy with KV cache warmup

Configuration:
  Proxy listening on: http://localhost:8088
  Backend llama.cpp:  http://localhost:8081
  Admin server:       http://localhost:8089
  Warmup interval:    30s
  Templates:          2 configured

INFO: Creating template watcher...
INFO: Added template @code from examples/templates/code_assistant.txt (needs warmup)
INFO: Added template @debug from examples/templates/debug_helper.txt (needs warmup)
INFO: Starting warmup manager...
INFO: Warmup manager background loop started

✅ Servers are running!
```

Within 30 seconds, you'll see the warmup process:
```
INFO: Checking templates for changes...
INFO: Found 2 template(s) that need warmup: [@code @debug]
INFO: Starting warmup for @code
INFO: Sending warmup request for @code
INFO: Warmup request completed for @code (1.2s)
INFO: KV cache saved for code.bin
INFO: Template @code warmup complete
```

### Using Templates

Use templates by prefixing your message with a configured template prefix:

```bash
curl http://localhost:8088/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [{"role": "user", "content": "@code How do I reverse a string in Python?"}]
  }'
```

The `@code` prefix triggers template substitution. The proxy:
1. Detects the `@code` prefix
2. Processes the template with your message
3. Restores the pre-warmed KV cache (if needed)
4. Sends the expanded template to llama.cpp
5. Streams the response back to you

The pre-warmed KV cache makes the first response much faster!

### Basic Usage (Without Templates)

Run without configuration for basic proxying:

```bash
./bioproxy
```

Test the proxy:
```bash
# Health check
curl http://localhost:8088/health

# Chat completion
curl http://localhost:8088/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [{"role": "user", "content": "Hello!"}],
    "max_tokens": 50
  }'

# Check metrics
curl http://localhost:8089/metrics
```

## Command-Line Options

```bash
./bioproxy --help
```

Options:
- `-config` - Path to config file (default: `~/.config/bioproxy/config.json`)
- `-host` - Proxy host (overrides config)
- `-port` - Proxy port (overrides config)
- `-admin-host` - Admin server host (overrides config)
- `-admin-port` - Admin server port (overrides config)
- `-backend` - Backend llama.cpp URL (overrides config)

Example:
```bash
./bioproxy -config config.json -port 9000
```

## Configuration Reference

See [examples/config.json](examples/config.json) for a complete example.

**Required fields:**
- `backend_url` - llama.cpp server URL

**Optional fields:**
- `proxy_host` - Proxy bind address (default: "localhost")
- `proxy_port` - Proxy port (default: 8088)
- `admin_host` - Admin bind address (default: "localhost")
- `admin_port` - Admin port (default: 8089)
- `warmup_check_interval` - Template check interval in seconds (default: 30)
- `prefixes` - Template prefix mappings (object of prefix → file path)

## Template Syntax

Templates use `<{...}>` placeholders. See `examples/templates/` for working examples.

**Message placeholder:**
```
System prompt here.

User: <{message}>
Assistant:
```
Example: See [examples/templates/code_assistant.txt](examples/templates/code_assistant.txt)

**File inclusion:**
```
Reference documentation: <{examples/templates/debugging_guide.txt}>

Problem: <{message}>
```
Example: See [examples/templates/debug_helper.txt](examples/templates/debug_helper.txt)

When processed, the file content replaces the placeholder:
```
Reference documentation: Common debugging steps:
1. Reproduce the issue consistently
2. Isolate the problem area
...

Problem: [user's actual message]
```

**Note:** Placeholder replacement is non-recursive - patterns in substituted content are NOT processed. This prevents infinite loops and unexpected behavior.

## Architecture

```
Client → Proxy (8088) → llama.cpp (8081)
            ↓
        Metrics
            ↓
    Admin Server (8089)
            ↓
    Template Watcher
            ↓
    Warmup Manager
```

**Components:**
- **Proxy (port 8088)** - Forwards requests to llama.cpp, collects metrics
- **Admin (port 8089)** - Health status and Prometheus metrics
- **Template Watcher** - Monitors template files for changes
- **Warmup Manager** - Automatically warms up changed templates

## Current Features

- ✅ **Reverse proxy** - Forwards all requests to llama.cpp with minimal overhead
- ✅ **Template injection** - Automatically injects templates when user messages start with @prefix
- ✅ **Smart KV cache** - State tracking optimizes saves/restores (95% reduction in disk I/O)
- ✅ **Admin endpoints** - Health and Prometheus metrics on separate port
- ✅ **Template system** - File-based templates with message substitution and file inclusion
- ✅ **Template monitoring** - Detects file changes via hash comparison
- ✅ **Automatic warmup** - Background process warms templates at configurable intervals
- ✅ **Streaming support** - Full SSE streaming for chat completions

## Roadmap

**Phase 1: ✅ Basic Proxy** - Request forwarding and metrics
**Phase 2: ✅ Admin Server** - Health and metrics endpoints
**Phase 3: ✅ Template System** - File watching and processing
**Phase 4: ✅ Warmup Manager** - Automatic KV cache warmup
**Phase 5: ✅ Template Injection** - Intercept @prefix in user messages
**Phase 6: ✅ Smart KV Cache** - State tracking to optimize save/restore operations

**Future Enhancements:**
- Immediate warmup on startup (currently waits for first interval)
- Request queue with prioritization (user requests before warmup)
- Cross-platform release binaries (Linux, macOS, Windows)

## Development

### Running Tests

**Unit tests (fast, no llama.cpp needed):**
```bash
go test ./...
```

**Manual tests (requires llama.cpp with --slot-save-path):**
```bash
# See MANUAL_TESTING.md for complete guide
go clean -testcache && go test -tags=manual -v ./...
```

### Project Structure

```
bioproxy/
├── cmd/bioproxy/          - Main executable
├── internal/
│   ├── config/           - Configuration management
│   ├── proxy/            - Reverse proxy with template injection
│   ├── admin/            - Admin server (health, metrics)
│   ├── template/         - Template watching and processing
│   ├── warmup/           - KV cache warmup manager
│   └── state/            - Backend state tracking for KV cache optimization
├── examples/             - Example configuration and templates
│   ├── config.json       - Example configuration file
│   └── templates/        - Example template files
├── WARMUP_DESIGN.md      - Warmup architecture design
└── MANUAL_TESTING.md     - Manual testing guide
```

### Documentation

- **WARMUP_DESIGN.md** - Complete warmup architecture and design decisions
- **MANUAL_TESTING.md** - Guide for running manual tests with llama.cpp
- **internal/\*/README.md** - Module-specific documentation

## License

See [LICENSE](LICENSE) file.
