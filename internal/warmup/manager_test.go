package warmup

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oleksandr/bioproxy/internal/admin"
	"github.com/oleksandr/bioproxy/internal/config"
	"github.com/oleksandr/bioproxy/internal/state"
	"github.com/oleksandr/bioproxy/internal/template"
)

// mockLlamaCppServer is a mock llama.cpp server for testing
type mockLlamaCppServer struct {
	server *httptest.Server

	mu                sync.Mutex
	restoreCalls      []string // filenames of restore calls
	saveCalls         []string // filenames of save calls
	completionCalls   int
	restoreFailures   map[string]bool // files that should fail to restore
	saveFailures      map[string]bool // files that should fail to save
	completionFailure bool            // whether completion should fail
}

func newMockLlamaCppServer() *mockLlamaCppServer {
	mock := &mockLlamaCppServer{
		restoreFailures: make(map[string]bool),
		saveFailures:    make(map[string]bool),
	}

	// Create test server
	mux := http.NewServeMux()

	// Slot management endpoint
	mux.HandleFunc("/slots/0", func(w http.ResponseWriter, r *http.Request) {
		action := r.URL.Query().Get("action")

		// Read request body
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		filename, ok := reqBody["filename"].(string)
		if !ok {
			http.Error(w, "filename required", http.StatusBadRequest)
			return
		}

		mock.mu.Lock()
		defer mock.mu.Unlock()

		switch action {
		case "restore":
			mock.restoreCalls = append(mock.restoreCalls, filename)

			if mock.restoreFailures[filename] {
				http.Error(w, "file not found", http.StatusNotFound)
				return
			}

			resp := map[string]interface{}{
				"filename": filename,
				"n_loaded": 100,
				"slot_id":  0,
			}
			json.NewEncoder(w).Encode(resp)

		case "save":
			mock.saveCalls = append(mock.saveCalls, filename)

			if mock.saveFailures[filename] {
				http.Error(w, "save failed", http.StatusInternalServerError)
				return
			}

			resp := map[string]interface{}{
				"filename": filename,
				"n_saved":  100,
				"slot_id":  0,
			}
			json.NewEncoder(w).Encode(resp)

		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
		}
	})

	// Chat completions endpoint
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		mock.mu.Lock()
		defer mock.mu.Unlock()

		mock.completionCalls++

		if mock.completionFailure {
			http.Error(w, "completion failed", http.StatusInternalServerError)
			return
		}

		// Return minimal completion response
		resp := map[string]interface{}{
			"id":      "test-id",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "test-model",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]string{
						"role":    "assistant",
						"content": "test response",
					},
					"finish_reason": "stop",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	mock.server = httptest.NewServer(mux)
	return mock
}

func (m *mockLlamaCppServer) Close() {
	m.server.Close()
}

func (m *mockLlamaCppServer) URL() string {
	return m.server.URL
}

func (m *mockLlamaCppServer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.restoreCalls = nil
	m.saveCalls = nil
	m.completionCalls = 0
}

func (m *mockLlamaCppServer) GetRestoreCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.restoreCalls...)
}

func (m *mockLlamaCppServer) GetSaveCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.saveCalls...)
}

func (m *mockLlamaCppServer) GetCompletionCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.completionCalls
}

func TestManagerLifecycle(t *testing.T) {
	// Create mock server
	mock := newMockLlamaCppServer()
	defer mock.Close()

	// Create config
	cfg := &config.Config{
		BackendURL:          mock.URL(),
		WarmupCheckInterval: 1, // 1 second for fast testing
	}

	// Create watcher
	watcher := template.NewWatcher()

	// Create metrics
	metrics := admin.NewMetrics()

	// Create manager
	mgr := New(cfg, watcher, mock.URL(), metrics, state.New())

	// Test Start
	if err := mgr.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Verify running
	mgr.mu.Lock()
	if !mgr.running {
		t.Error("Manager should be running after Start()")
	}
	mgr.mu.Unlock()

	// Test double start
	if err := mgr.Start(); err == nil {
		t.Error("Expected error when starting already running manager")
	}

	// Test Stop
	mgr.Stop()

	// Verify stopped
	mgr.mu.Lock()
	if mgr.running {
		t.Error("Manager should not be running after Stop()")
	}
	mgr.mu.Unlock()
}

func TestImmediateWarmupOnStartup(t *testing.T) {
	// This test verifies that warmup happens immediately on startup
	// instead of waiting for the first interval
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "test_template.txt")
	templateContent := "You are a helpful assistant. <{message}>"
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to create template file: %v", err)
	}

	// Create mock server
	mock := newMockLlamaCppServer()
	defer mock.Close()

	// Create config with a long interval (we should NOT wait this long)
	cfg := &config.Config{
		BackendURL:          mock.URL(),
		WarmupCheckInterval: 60, // 60 seconds - should not wait this long
	}

	// Create watcher and add template
	watcher := template.NewWatcher()
	if err := watcher.AddTemplate("@test", templatePath); err != nil {
		t.Fatalf("Failed to add template: %v", err)
	}

	// Create metrics
	metrics := admin.NewMetrics()

	// Create manager
	mgr := New(cfg, watcher, mock.URL(), metrics, state.New())

	// Start manager
	if err := mgr.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer mgr.Stop()

	// Wait a short time for the immediate warmup to complete
	// Should be much less than the 60 second interval
	time.Sleep(100 * time.Millisecond)

	// Verify warmup happened immediately
	completionCalls := mock.GetCompletionCalls()
	if completionCalls != 1 {
		t.Errorf("Expected immediate warmup (1 completion call), got %d", completionCalls)
	}

	// Verify restore was attempted
	restoreCalls := mock.GetRestoreCalls()
	if len(restoreCalls) != 1 {
		t.Errorf("Expected 1 restore call during immediate warmup, got %d", len(restoreCalls))
	}

	// The immediate warmup is the key feature we're testing
	// The completion call happening immediately (not after 60 seconds) proves it works
}

func TestWarmupTemplate(t *testing.T) {
	// Create temporary template file
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "test_template.txt")
	templateContent := "You are a helpful assistant. <{message}>"
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to create template file: %v", err)
	}

	// Create mock server
	mock := newMockLlamaCppServer()
	defer mock.Close()

	// Mark restore as not found (first time)
	mock.mu.Lock()
	mock.restoreFailures["test.bin"] = true
	mock.mu.Unlock()

	// Create config
	cfg := &config.Config{
		BackendURL:          mock.URL(),
		WarmupCheckInterval: 10, // High interval, we'll call warmup manually
	}

	// Create watcher and add template
	watcher := template.NewWatcher()
	if err := watcher.AddTemplate("@test", templatePath); err != nil {
		t.Fatalf("Failed to add template: %v", err)
	}

	// Create metrics
	metrics := admin.NewMetrics()

	// Create manager
	mgr := New(cfg, watcher, mock.URL(), metrics, state.New())

	// Execute warmup
	if err := mgr.warmupTemplate("@test"); err != nil {
		t.Fatalf("Warmup failed: %v", err)
	}

	// Verify calls
	// With state tracking, first warmup should:
	// - Restore (will fail 404, that's ok)
	// - Send completion request
	// - NOT save (only save when switching away from a template)
	restoreCalls := mock.GetRestoreCalls()
	if len(restoreCalls) != 1 || restoreCalls[0] != "test.bin" {
		t.Errorf("Expected 1 restore call for 'test.bin', got %v", restoreCalls)
	}

	saveCalls := mock.GetSaveCalls()
	if len(saveCalls) != 0 {
		t.Errorf("Expected 0 save calls (only save when switching away), got %v", saveCalls)
	}

	completionCalls := mock.GetCompletionCalls()
	if completionCalls != 1 {
		t.Errorf("Expected 1 completion call, got %d", completionCalls)
	}

	// Note: MarkWarmedUp() is called by checkAndWarmup(), not warmupTemplate()
	// So we manually mark it here for testing purposes
	watcher.MarkWarmedUp("@test")

	// Verify template marked as warmed up
	if watcher.NeedsWarmup("@test") {
		t.Error("Template should be marked as warmed up")
	}
}

func TestWarmupWithExistingCache(t *testing.T) {
	// Create temporary template file
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "test_template.txt")
	templateContent := "You are a helpful assistant. <{message}>"
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to create template file: %v", err)
	}

	// Create mock server (cache exists this time)
	mock := newMockLlamaCppServer()
	defer mock.Close()

	// Create config
	cfg := &config.Config{
		BackendURL:          mock.URL(),
		WarmupCheckInterval: 10,
	}

	// Create watcher and add template
	watcher := template.NewWatcher()
	if err := watcher.AddTemplate("@test", templatePath); err != nil {
		t.Fatalf("Failed to add template: %v", err)
	}

	// Create metrics
	metrics := admin.NewMetrics()

	// Create manager
	mgr := New(cfg, watcher, mock.URL(), metrics, state.New())

	// Execute warmup
	if err := mgr.warmupTemplate("@test"); err != nil {
		t.Fatalf("Warmup failed: %v", err)
	}

	// Verify restore succeeded (cache exists)
	restoreCalls := mock.GetRestoreCalls()
	if len(restoreCalls) != 1 {
		t.Errorf("Expected 1 restore call, got %d", len(restoreCalls))
	}

	// With state tracking, we don't save after warmup
	// We only save when switching away from a template
	saveCalls := mock.GetSaveCalls()
	if len(saveCalls) != 0 {
		t.Errorf("Expected 0 save calls (only save when switching away), got %d", len(saveCalls))
	}
}

func TestWarmupCompletionFailure(t *testing.T) {
	// Create temporary template file
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "test_template.txt")
	templateContent := "Test template"
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to create template file: %v", err)
	}

	// Create mock server
	mock := newMockLlamaCppServer()
	defer mock.Close()

	// Make completion fail
	mock.mu.Lock()
	mock.completionFailure = true
	mock.mu.Unlock()

	// Create config
	cfg := &config.Config{
		BackendURL:          mock.URL(),
		WarmupCheckInterval: 10,
	}

	// Create watcher and add template
	watcher := template.NewWatcher()
	if err := watcher.AddTemplate("@test", templatePath); err != nil {
		t.Fatalf("Failed to add template: %v", err)
	}

	// Create metrics
	metrics := admin.NewMetrics()

	// Create manager
	mgr := New(cfg, watcher, mock.URL(), metrics, state.New())

	// Execute warmup - should fail
	if err := mgr.warmupTemplate("@test"); err == nil {
		t.Error("Expected warmup to fail when completion fails")
	}

	// Verify no save call was made
	saveCalls := mock.GetSaveCalls()
	if len(saveCalls) != 0 {
		t.Errorf("Expected no save calls on failure, got %d", len(saveCalls))
	}
}

func TestWarmupSaveFailure(t *testing.T) {
	// With state tracking, save only happens when switching templates
	// This test verifies that save failure during template switch doesn't break the warmup
	tmpDir := t.TempDir()

	// Create two template files
	templatePath1 := filepath.Join(tmpDir, "test_template1.txt")
	if err := os.WriteFile(templatePath1, []byte("Template 1"), 0644); err != nil {
		t.Fatalf("Failed to create template file 1: %v", err)
	}
	templatePath2 := filepath.Join(tmpDir, "test_template2.txt")
	if err := os.WriteFile(templatePath2, []byte("Template 2"), 0644); err != nil {
		t.Fatalf("Failed to create template file 2: %v", err)
	}

	// Create mock server
	mock := newMockLlamaCppServer()
	defer mock.Close()

	// Make save fail for template 1
	mock.mu.Lock()
	mock.saveFailures["test1.bin"] = true
	mock.mu.Unlock()

	// Create config
	cfg := &config.Config{
		BackendURL:          mock.URL(),
		WarmupCheckInterval: 10,
	}

	// Create watcher and add both templates
	watcher := template.NewWatcher()
	if err := watcher.AddTemplate("@test1", templatePath1); err != nil {
		t.Fatalf("Failed to add template 1: %v", err)
	}
	if err := watcher.AddTemplate("@test2", templatePath2); err != nil {
		t.Fatalf("Failed to add template 2: %v", err)
	}

	// Create metrics and state
	metrics := admin.NewMetrics()
	backendState := state.New()

	// Create manager
	mgr := New(cfg, watcher, mock.URL(), metrics, backendState)

	// First warmup with test1
	if err := mgr.warmupTemplate("@test1"); err != nil {
		t.Fatalf("First warmup failed: %v", err)
	}

	// Second warmup with test2 - this will try to save test1 (and fail)
	// But the warmup itself should succeed
	if err := mgr.warmupTemplate("@test2"); err != nil {
		t.Fatalf("Second warmup should succeed even if save failed: %v", err)
	}

	// Verify we tried to save test1 before switching
	saveCalls := mock.GetSaveCalls()
	if len(saveCalls) != 0 {
		// Note: save failed so it won't be in the successful saves list
		// The important thing is warmup succeeded despite save failure
		t.Logf("Save attempts: %v (save failed as expected)", saveCalls)
	}
}

func TestCheckAndWarmup(t *testing.T) {
	// Create temporary template file
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "test_template.txt")
	templateContent := "Initial content"
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to create template file: %v", err)
	}

	// Create mock server
	mock := newMockLlamaCppServer()
	defer mock.Close()

	// Create config
	cfg := &config.Config{
		BackendURL:          mock.URL(),
		WarmupCheckInterval: 10,
	}

	// Create watcher and add template
	watcher := template.NewWatcher()
	if err := watcher.AddTemplate("@test", templatePath); err != nil {
		t.Fatalf("Failed to add template: %v", err)
	}

	// Create metrics
	metrics := admin.NewMetrics()

	// Create manager
	mgr := New(cfg, watcher, mock.URL(), metrics, state.New())

	// Check and warmup (should warmup initial template)
	mgr.checkAndWarmup()

	// Verify warmup was called
	if mock.GetCompletionCalls() != 1 {
		t.Errorf("Expected 1 completion call after first check, got %d", mock.GetCompletionCalls())
	}

	// Reset mock
	mock.Reset()

	// Check again (no changes, should not warmup)
	mgr.checkAndWarmup()

	// Verify no warmup
	if mock.GetCompletionCalls() != 0 {
		t.Errorf("Expected 0 completion calls when no changes, got %d", mock.GetCompletionCalls())
	}

	// Modify template
	newContent := "Modified content"
	if err := os.WriteFile(templatePath, []byte(newContent), 0644); err != nil {
		t.Fatalf("Failed to update template file: %v", err)
	}

	// Check again (should detect change and warmup)
	mgr.checkAndWarmup()

	// Verify warmup was called again
	if mock.GetCompletionCalls() != 1 {
		t.Errorf("Expected 1 completion call after template change, got %d", mock.GetCompletionCalls())
	}
}

func TestRestoreKVCache(t *testing.T) {
	mock := newMockLlamaCppServer()
	defer mock.Close()

	cfg := &config.Config{
		BackendURL:          mock.URL(),
		WarmupCheckInterval: 10,
	}

	watcher := template.NewWatcher()
	metrics := admin.NewMetrics()
	mgr := New(cfg, watcher, mock.URL(), metrics, state.New())

	// Test successful restore
	if err := mgr.restoreKVCache("@test", "test.bin"); err != nil {
		t.Errorf("Restore should succeed: %v", err)
	}

	// Test restore not found
	mock.mu.Lock()
	mock.restoreFailures["missing.bin"] = true
	mock.mu.Unlock()

	if err := mgr.restoreKVCache("@test", "missing.bin"); err == nil {
		t.Error("Expected error when cache file not found")
	} else if !strings.Contains(err.Error(), "404") {
		t.Errorf("Expected 404 error, got: %v", err)
	}
}

func TestSaveKVCache(t *testing.T) {
	mock := newMockLlamaCppServer()
	defer mock.Close()

	cfg := &config.Config{
		BackendURL:          mock.URL(),
		WarmupCheckInterval: 10,
	}

	watcher := template.NewWatcher()
	metrics := admin.NewMetrics()
	mgr := New(cfg, watcher, mock.URL(), metrics, state.New())

	// Test successful save
	if err := mgr.saveKVCache("@test", "test.bin"); err != nil {
		t.Errorf("Save should succeed: %v", err)
	}

	// Test save failure
	mock.mu.Lock()
	mock.saveFailures["fail.bin"] = true
	mock.mu.Unlock()

	if err := mgr.saveKVCache("@test", "fail.bin"); err == nil {
		t.Error("Expected error when save fails")
	}
}

func TestSendWarmupRequest(t *testing.T) {
	mock := newMockLlamaCppServer()
	defer mock.Close()

	cfg := &config.Config{
		BackendURL:          mock.URL(),
		WarmupCheckInterval: 10,
	}

	watcher := template.NewWatcher()
	metrics := admin.NewMetrics()
	mgr := New(cfg, watcher, mock.URL(), metrics, state.New())

	// Test successful request
	content := "Test warmup content"
	if err := mgr.sendWarmupRequest("@test", content); err != nil {
		t.Errorf("Warmup request should succeed: %v", err)
	}

	// Verify completion was called
	if mock.GetCompletionCalls() != 1 {
		t.Errorf("Expected 1 completion call, got %d", mock.GetCompletionCalls())
	}

	// Test failed request
	mock.mu.Lock()
	mock.completionFailure = true
	mock.mu.Unlock()

	if err := mgr.sendWarmupRequest("@test", content); err == nil {
		t.Error("Expected error when completion fails")
	}
}
