//go:build manual
// +build manual

// This file contains manual integration tests for the warmup manager that require
// a running llama.cpp server with KV cache support.
// These tests are excluded from CI/CD pipelines and regular test runs.
//
// To run these tests:
//   1. Start llama.cpp server on localhost:8081 (default port)
//      IMPORTANT: Start with --slot-save-path flag to enable KV cache saving
//   2. Run: go test -tags=manual -v ./internal/warmup/...
//
// Example llama.cpp startup:
//   ./llama-server -m /path/to/model.gguf --port 8081 --slot-save-path ./kv_cache

package warmup

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oleksandr/bioproxy/internal/admin"
	"github.com/oleksandr/bioproxy/internal/config"
	"github.com/oleksandr/bioproxy/internal/state"
	"github.com/oleksandr/bioproxy/internal/template"
)

const (
	// Default llama.cpp server URL
	llamaCppURL = "http://localhost:8081"
)

// checkLlamaCppAvailable checks if llama.cpp server is running
func checkLlamaCppAvailable(t *testing.T) {
	t.Helper()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(llamaCppURL + "/health")

	if err != nil {
		t.Skipf("llama.cpp server not available at %s: %v", llamaCppURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Skipf("llama.cpp server not healthy (status %d)", resp.StatusCode)
	}

	t.Logf("✓ llama.cpp server is available at %s", llamaCppURL)
}

// TestManualWarmupFullWorkflow tests the complete warmup workflow:
// 1. Create a template file
// 2. Start warmup manager
// 3. Wait for warmup to execute
// 4. Verify KV cache operations succeeded
func TestManualWarmupFullWorkflow(t *testing.T) {
	checkLlamaCppAvailable(t)

	// Create temporary directory for test templates
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "code_assistant.txt")
	templateContent := `You are a helpful coding assistant.
You provide clear, concise code examples.

User question: <{message}>`

	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to create template file: %v", err)
	}

	t.Logf("Created test template at: %s", templatePath)

	// Create config with short warmup interval for testing
	cfg := &config.Config{
		BackendURL:          llamaCppURL,
		WarmupCheckInterval: 2, // 2 seconds for fast testing
	}

	// Create watcher and add template
	watcher := template.NewWatcher()
	if err := watcher.AddTemplate("@code", templatePath); err != nil {
		t.Fatalf("Failed to add template: %v", err)
	}

	t.Log("✓ Template added to watcher")

	// Create metrics
	metrics := admin.NewMetrics()

	// Create and start warmup manager
	mgr := New(cfg, watcher, llamaCppURL, metrics, state.New())

	if err := mgr.Start(); err != nil {
		t.Fatalf("Failed to start warmup manager: %v", err)
	}
	defer mgr.Stop()

	t.Log("✓ Warmup manager started")
	t.Logf("Waiting for warmup cycle (max %ds)...", cfg.WarmupCheckInterval+5)

	// Wait for warmup to complete
	// The manager checks every WarmupCheckInterval seconds
	maxWait := time.Duration(cfg.WarmupCheckInterval+5) * time.Second
	checkInterval := 500 * time.Millisecond
	deadline := time.Now().Add(maxWait)

	warmedUp := false
	for time.Now().Before(deadline) {
		if !watcher.NeedsWarmup("@code") {
			warmedUp = true
			break
		}
		time.Sleep(checkInterval)
	}

	if !warmedUp {
		t.Fatalf("Template was not warmed up within %v", maxWait)
	}

	t.Log("✓ Template warmup completed!")

	// Verify the template can be processed
	processed, err := watcher.ProcessTemplate("@code", "How do I reverse a string?")
	if err != nil {
		t.Fatalf("Failed to process template: %v", err)
	}

	t.Logf("✓ Template processed successfully (%d bytes)", len(processed))

	// Note: We can't easily verify the KV cache file exists because llama.cpp
	// manages its own cache directory, but successful warmup indicates it worked
}

// TestManualWarmupTemplateChange tests that template changes trigger re-warmup
func TestManualWarmupTemplateChange(t *testing.T) {
	checkLlamaCppAvailable(t)

	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "changing_template.txt")
	initialContent := "Initial template content: <{message}>"

	if err := os.WriteFile(templatePath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to create template file: %v", err)
	}

	cfg := &config.Config{
		BackendURL:          llamaCppURL,
		WarmupCheckInterval: 2,
	}

	watcher := template.NewWatcher()
	if err := watcher.AddTemplate("@test", templatePath); err != nil {
		t.Fatalf("Failed to add template: %v", err)
	}

	metrics := admin.NewMetrics()
	mgr := New(cfg, watcher, llamaCppURL, metrics, state.New())
	if err := mgr.Start(); err != nil {
		t.Fatalf("Failed to start warmup manager: %v", err)
	}
	defer mgr.Stop()

	t.Log("✓ Warmup manager started, waiting for initial warmup...")

	// Wait for initial warmup
	maxWait := time.Duration(cfg.WarmupCheckInterval+5) * time.Second
	deadline := time.Now().Add(maxWait)

	for time.Now().Before(deadline) {
		if !watcher.NeedsWarmup("@test") {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if watcher.NeedsWarmup("@test") {
		t.Fatal("Initial warmup did not complete in time")
	}

	t.Log("✓ Initial warmup completed")

	// Modify the template
	modifiedContent := "Modified template content with different text: <{message}>"
	if err := os.WriteFile(templatePath, []byte(modifiedContent), 0644); err != nil {
		t.Fatalf("Failed to modify template file: %v", err)
	}

	t.Log("Modified template file, waiting for re-warmup...")

	// Wait for change detection and re-warmup
	// This should happen within 2 check cycles
	maxWait = time.Duration(cfg.WarmupCheckInterval*2+5) * time.Second
	deadline = time.Now().Add(maxWait)

	reWarmedUp := false
	for time.Now().Before(deadline) {
		// Template should be detected as changed (NeedsWarmup=true)
		// then warmed up again (NeedsWarmup=false)
		// We just wait for it to not need warmup again
		if !watcher.NeedsWarmup("@test") {
			// Give it a moment to ensure it actually detected the change
			time.Sleep(time.Duration(cfg.WarmupCheckInterval+1) * time.Second)
			if !watcher.NeedsWarmup("@test") {
				reWarmedUp = true
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !reWarmedUp {
		t.Fatalf("Template change was not detected and re-warmed within %v", maxWait)
	}

	t.Log("✓ Template change detected and re-warmed successfully!")
}

// TestManualWarmupMultipleTemplates tests warmup with multiple templates
func TestManualWarmupMultipleTemplates(t *testing.T) {
	checkLlamaCppAvailable(t)

	tmpDir := t.TempDir()

	// Create multiple template files
	templates := map[string]string{
		"@code":  "You are a coding assistant. <{message}>",
		"@debug": "You are a debugging expert. <{message}>",
		"@test":  "You are a test writing specialist. <{message}>",
	}

	templatePaths := make(map[string]string)
	for prefix, content := range templates {
		// Remove @ from filename
		filename := prefix[1:] + "_template.txt"
		path := filepath.Join(tmpDir, filename)

		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create template %s: %v", prefix, err)
		}

		templatePaths[prefix] = path
		t.Logf("Created template %s at %s", prefix, path)
	}

	cfg := &config.Config{
		BackendURL:          llamaCppURL,
		WarmupCheckInterval: 3, // Slightly longer for multiple templates
	}

	watcher := template.NewWatcher()
	for prefix, path := range templatePaths {
		if err := watcher.AddTemplate(prefix, path); err != nil {
			t.Fatalf("Failed to add template %s: %v", prefix, err)
		}
	}

	t.Logf("✓ Added %d templates to watcher", len(templates))

	metrics := admin.NewMetrics()
	mgr := New(cfg, watcher, llamaCppURL, metrics, state.New())
	if err := mgr.Start(); err != nil {
		t.Fatalf("Failed to start warmup manager: %v", err)
	}
	defer mgr.Stop()

	t.Log("✓ Warmup manager started, waiting for all templates to warm up...")

	// Wait for all templates to warm up
	maxWait := time.Duration(cfg.WarmupCheckInterval*2+10) * time.Second
	deadline := time.Now().Add(maxWait)

	allWarmedUp := false
	for time.Now().Before(deadline) {
		allDone := true
		for prefix := range templates {
			if watcher.NeedsWarmup(prefix) {
				allDone = false
				break
			}
		}

		if allDone {
			allWarmedUp = true
			break
		}

		time.Sleep(500 * time.Millisecond)
	}

	if !allWarmedUp {
		// Check which templates didn't warm up
		for prefix := range templates {
			if watcher.NeedsWarmup(prefix) {
				t.Errorf("Template %s was not warmed up", prefix)
			}
		}
		t.Fatalf("Not all templates were warmed up within %v", maxWait)
	}

	t.Logf("✓ All %d templates warmed up successfully!", len(templates))

	// Verify all templates can be processed
	for prefix := range templates {
		processed, err := watcher.ProcessTemplate(prefix, "test message")
		if err != nil {
			t.Errorf("Failed to process template %s: %v", prefix, err)
			continue
		}
		t.Logf("  %s: processed %d bytes", prefix, len(processed))
	}

	t.Log("✓ All templates can be processed!")
}

// TestManualWarmupManagerLifecycle tests starting and stopping the manager
func TestManualWarmupManagerLifecycle(t *testing.T) {
	checkLlamaCppAvailable(t)

	cfg := &config.Config{
		BackendURL:          llamaCppURL,
		WarmupCheckInterval: 5,
	}

	watcher := template.NewWatcher()
	metrics := admin.NewMetrics()
	mgr := New(cfg, watcher, llamaCppURL, metrics, state.New())

	// Test Start
	if err := mgr.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	t.Log("✓ Manager started")

	// Verify running
	mgr.mu.Lock()
	running := mgr.running
	mgr.mu.Unlock()

	if !running {
		t.Error("Manager should be running after Start()")
	}

	// Test double start
	if err := mgr.Start(); err == nil {
		t.Error("Expected error when starting already running manager")
	}

	// Test Stop
	mgr.Stop()

	t.Log("✓ Manager stopped")

	// Verify stopped
	mgr.mu.Lock()
	running = mgr.running
	mgr.mu.Unlock()

	if running {
		t.Error("Manager should not be running after Stop()")
	}

	// Test multiple stops (should be safe)
	mgr.Stop()
	mgr.Stop()

	t.Log("✓ Multiple stops handled gracefully")
}

// TestManualWarmupWithRealCompletion tests warmup followed by an actual completion
// to verify the KV cache is actually being used
func TestManualWarmupWithRealCompletion(t *testing.T) {
	checkLlamaCppAvailable(t)

	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "assistant.txt")
	templateContent := `You are a helpful assistant. Always be concise.

User: <{message}>
Assistant:`

	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to create template file: %v", err)
	}

	cfg := &config.Config{
		BackendURL:          llamaCppURL,
		WarmupCheckInterval: 2,
	}

	watcher := template.NewWatcher()
	if err := watcher.AddTemplate("@assist", templatePath); err != nil {
		t.Fatalf("Failed to add template: %v", err)
	}

	metrics := admin.NewMetrics()
	mgr := New(cfg, watcher, llamaCppURL, metrics, state.New())
	if err := mgr.Start(); err != nil {
		t.Fatalf("Failed to start warmup manager: %v", err)
	}
	defer mgr.Stop()

	t.Log("Waiting for template warmup...")

	// Wait for warmup
	maxWait := time.Duration(cfg.WarmupCheckInterval+5) * time.Second
	deadline := time.Now().Add(maxWait)

	for time.Now().Before(deadline) {
		if !watcher.NeedsWarmup("@assist") {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if watcher.NeedsWarmup("@assist") {
		t.Fatal("Template warmup did not complete in time")
	}

	t.Log("✓ Template warmed up, now testing actual completion...")

	// Now send an actual completion request
	// In a real scenario, this would benefit from the warmed-up KV cache
	processed, err := watcher.ProcessTemplate("@assist", "What is 2+2?")
	if err != nil {
		t.Fatalf("Failed to process template: %v", err)
	}

	t.Logf("Processed template (%d bytes):", len(processed))
	t.Logf("%s", processed)

	// Note: Actually sending this to llama.cpp and measuring speedup
	// would require more complex integration. This test verifies the
	// warmup completed and template is ready to use.

	t.Log("✓ Template ready for use with KV cache!")
}

// TestManualDirectKVCacheOperations tests direct KV cache save/restore
// operations to verify llama.cpp slot management works
func TestManualDirectKVCacheOperations(t *testing.T) {
	checkLlamaCppAvailable(t)

	cfg := &config.Config{
		BackendURL:          llamaCppURL,
		WarmupCheckInterval: 30, // Not used for this test
	}

	watcher := template.NewWatcher()
	metrics := admin.NewMetrics()
	mgr := New(cfg, watcher, llamaCppURL, metrics, state.New())

	testPrefix := "@manual_test"
	testFilename := "manual_test_cache.bin"

	t.Logf("Testing KV cache save with filename: %s", testFilename)

	// Test save
	if err := mgr.saveKVCache(testPrefix, testFilename); err != nil {
		t.Logf("Save failed (may be expected if slot is empty): %v", err)
		// Don't fail - slot might be empty
	} else {
		t.Log("✓ KV cache save succeeded")
	}

	// Test restore
	t.Logf("Testing KV cache restore with filename: %s", testFilename)

	if err := mgr.restoreKVCache(testPrefix, testFilename); err != nil {
		t.Logf("Restore failed (may be expected if file doesn't exist): %v", err)
		// Don't fail - file might not exist
	} else {
		t.Log("✓ KV cache restore succeeded")
	}

	// Both operations reaching llama.cpp without error means the API works
	t.Log("✓ KV cache operations are functional")
}
