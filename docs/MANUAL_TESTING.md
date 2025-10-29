# Manual Testing Guide

Manual tests verify bioproxy works with a real llama.cpp server. These tests are excluded from CI/CD (tagged with `//go:build manual`).

## Quick Start

Assumes llama.cpp is already running at `http://localhost:8081`.

**Important:** For warmup tests, llama.cpp MUST be started with `--slot-save-path`:

```bash
./llama-server -m models/model.gguf --port 8081 --slot-save-path ./kv_cache
```

### Run All Manual Tests

```bash
go clean -testcache && go test -tags=manual -v ./...
```

### Run Specific Test Suites

```bash
# Proxy tests only
go clean -testcache && go test -tags=manual -v ./internal/proxy/...

# Warmup tests only
go clean -testcache && go test -tags=manual -v ./internal/warmup/...
```

### Run Individual Tests

```bash
# Specific warmup test
go clean -testcache && go test -tags=manual -v ./internal/warmup/ -run TestManualWarmupFullWorkflow

# Specific proxy test
go clean -testcache && go test -tags=manual -v ./internal/proxy/ -run TestManualChatCompletion
```

**Note:** Always use `go clean -testcache` to ensure tests actually execute.

## Test Suites

### Proxy Tests (`internal/proxy/manual_test.go`)

**6 tests total:**
1. `TestManualHealthCheck` - Basic proxying
2. `TestManualSlotSave` - KV cache save endpoint
3. `TestManualSlotRestore` - KV cache restore endpoint
4. `TestManualChatCompletion` - Non-streaming completions
5. `TestManualStreamingChat` - SSE streaming verification
6. `TestManualProxyPerformance` - Latency benchmarks (~1-2ms expected)

### Warmup Tests (`internal/warmup/manual_test.go`)

**7 tests total:**
1. `TestManualWarmupFullWorkflow` - Complete end-to-end warmup
2. `TestManualWarmupTemplateChange` - Change detection and re-warmup
3. `TestManualWarmupMultipleTemplates` - Concurrent warmup (3 templates)
4. `TestManualWarmupManagerLifecycle` - Start/stop safety
5. `TestManualWarmupWithRealCompletion` - Warmup + completion
6. `TestManualDirectKVCacheOperations` - Low-level KV cache API
7. `TestManualWarmupManagerLifecycle` - Manager lifecycle

## Verification

### Successful Test Output

```
=== RUN   TestManualWarmupFullWorkflow
    manual_test.go:52: ✓ llama.cpp server is available at http://localhost:8081
    manual_test.go:84: ✓ Warmup manager started
    manual_test.go:102: ✓ Template warmup completed!
--- PASS: TestManualWarmupFullWorkflow (3.45s)
```

### llama.cpp Server Logs

While tests run, check llama.cpp terminal for activity:

```
INFO: POST /slots/0?action=save
INFO: KV cache saved to kv_cache/code.bin (234 tokens)
INFO: POST /v1/chat/completions
```

### Check KV Cache Files

After warmup tests:

```bash
ls -lh ./kv_cache/
# Expected: code.bin, debug.bin, etc.
```

## Troubleshooting

**Test skipped with "server not available":**
- Check llama.cpp is running: `curl http://localhost:8081/health`
- Verify port 8081 is correct

**Warmup test fails with timeout:**
- Ensure llama.cpp started with `--slot-save-path ./kv_cache`
- Check llama.cpp logs for errors

**No activity in llama.cpp logs:**
- Run `go clean -testcache` before tests
- Verify llama.cpp is on port 8081

## Cleanup

```bash
# Remove test cache files
rm ./kv_cache/*.bin
```
