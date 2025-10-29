package warmup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/oleksandr/bioproxy/internal/admin"
	"github.com/oleksandr/bioproxy/internal/config"
	"github.com/oleksandr/bioproxy/internal/state"
	"github.com/oleksandr/bioproxy/internal/template"
)

// Manager handles automatic warmup of templates by monitoring changes
// and issuing warmup requests to llama.cpp
type Manager struct {
	config       *config.Config
	watcher      *template.Watcher
	backendURL   string
	client       *http.Client
	metrics      *admin.Metrics
	backendState *state.State

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}

	// Warmup cancellation support
	warmupMu          sync.RWMutex
	warmupInProgress  bool
	warmupPrefix      string        // Which prefix is currently warming up
	warmupCancelFunc  context.CancelFunc // Function to cancel in-progress warmup
}

// New creates a new warmup manager
func New(cfg *config.Config, watcher *template.Watcher, backendURL string, metrics *admin.Metrics, backendState *state.State) *Manager {
	return &Manager{
		config:       cfg,
		watcher:      watcher,
		backendURL:   strings.TrimSuffix(backendURL, "/"),
		metrics:      metrics,
		backendState: backendState,
		client: &http.Client{
			Timeout: 60 * time.Second, // Warmup can take a while
		},
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// Start begins the background warmup check loop
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("warmup manager already running")
	}

	m.running = true
	log.Printf("Starting warmup manager (check interval: %ds)", m.config.WarmupCheckInterval)

	go m.checkLoop()

	return nil
}

// Stop stops the background warmup loop
func (m *Manager) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.mu.Unlock()

	log.Printf("Stopping warmup manager...")
	close(m.stopCh)
	<-m.doneCh
	log.Printf("Warmup manager stopped")
}

// CancelWarmup attempts to cancel an in-progress warmup operation.
// This is called by the proxy when a user request arrives and needs priority.
// Returns true if a warmup was cancelled, false if no warmup was in progress.
func (m *Manager) CancelWarmup() bool {
	m.warmupMu.Lock()
	defer m.warmupMu.Unlock()

	if !m.warmupInProgress {
		return false
	}

	log.Printf("Cancelling in-progress warmup for %s (user request has priority)", m.warmupPrefix)

	// Call the cancel function to abort the HTTP request
	if m.warmupCancelFunc != nil {
		m.warmupCancelFunc()
	}

	// Record cancellation metric
	m.metrics.RecordWarmupCancellation(m.warmupPrefix)

	return true
}

// IsWarmupInProgress returns true if a warmup operation is currently running
func (m *Manager) IsWarmupInProgress() bool {
	m.warmupMu.RLock()
	defer m.warmupMu.RUnlock()
	return m.warmupInProgress
}

// GetWarmupPrefix returns the prefix currently being warmed up (or empty if none)
func (m *Manager) GetWarmupPrefix() string {
	m.warmupMu.RLock()
	defer m.warmupMu.RUnlock()
	return m.warmupPrefix
}

// checkLoop is the background goroutine that periodically checks for template changes
func (m *Manager) checkLoop() {
	defer close(m.doneCh)

	log.Printf("Warmup manager background loop started")

	// Perform immediate warmup check on startup
	// This ensures templates are warmed up right away instead of waiting
	// for the first interval (which could be 30+ seconds)
	log.Printf("Performing initial warmup check...")
	m.checkAndWarmup()

	// Create ticker for periodic checks
	ticker := time.NewTicker(time.Duration(m.config.WarmupCheckInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.checkAndWarmup()
		}
	}
}

// checkAndWarmup checks for changed templates and warms them up
func (m *Manager) checkAndWarmup() {
	log.Printf("Checking templates for changes...")

	// Record warmup check metric
	m.metrics.RecordWarmupCheck()

	// Get list of changed templates
	changedPrefixes := m.watcher.CheckForChanges()

	if len(changedPrefixes) == 0 {
		log.Printf("No template changes detected")
		return
	}

	log.Printf("Found %d template(s) that need warmup: %v", len(changedPrefixes), changedPrefixes)

	// Warmup each changed template
	for _, prefix := range changedPrefixes {
		if err := m.warmupTemplate(prefix); err != nil {
			// Check if error was due to cancellation
			if err.Error() == "warmup cancelled" {
				log.Printf("Warmup for %s was cancelled (user request had priority)", prefix)
				// Don't mark as warmed up - will retry on next check cycle
				continue
			}
			log.Printf("ERROR: Failed to warmup template %s: %v", prefix, err)
			// Continue with next template, will retry on next check cycle
			continue
		}

		// Mark as warmed up only if warmup completed successfully
		m.watcher.MarkWarmedUp(prefix)
		log.Printf("Template %s warmup complete", prefix)
	}
}

// warmupTemplate executes the warmup sequence for a single template
func (m *Manager) warmupTemplate(prefix string) error {
	log.Printf("Starting warmup for %s", prefix)

	// Create cancellable context for this warmup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Track warmup state
	m.warmupMu.Lock()
	m.warmupInProgress = true
	m.warmupPrefix = prefix
	m.warmupCancelFunc = cancel
	m.warmupMu.Unlock()

	// Clean up warmup state when done
	defer func() {
		m.warmupMu.Lock()
		m.warmupInProgress = false
		m.warmupPrefix = ""
		m.warmupCancelFunc = nil
		m.warmupMu.Unlock()
	}()

	// Track warmup duration
	startTime := time.Now()

	// Get cache filename (remove @ prefix if present)
	cacheFilename := strings.TrimPrefix(prefix, "@") + ".bin"

	// BEFORE sending the warmup request:
	// Step 1: Save old KV cache if we're switching away from a different template
	if m.backendState.ShouldSave(prefix) {
		oldPrefix := m.backendState.GetLastPrefix()
		oldFilename := strings.TrimPrefix(oldPrefix, "@") + ".bin"
		log.Printf("Saving KV cache for %s before switching to %s", oldPrefix, prefix)
		if err := m.saveKVCache(oldPrefix, oldFilename); err != nil {
			log.Printf("WARNING: Failed to save KV cache for %s: %v", oldPrefix, err)
			// Don't fail the warmup - continue with the new template
		}
	}

	// Step 2: Restore new KV cache if we're switching to a different template
	if m.backendState.ShouldRestore(prefix) {
		log.Printf("Restoring KV cache for %s", prefix)
		if err := m.restoreKVCache(prefix, cacheFilename); err != nil {
			// Log but don't fail - this is expected on first warmup
			log.Printf("INFO: Could not restore KV cache for %s (may be first warmup): %v", prefix, err)
		}
	} else {
		log.Printf("Skipping KV cache restore for %s (already loaded)", prefix)
	}

	// Step 3: Process template with empty message to get warmup content
	warmupContent, err := m.watcher.ProcessTemplate(prefix, "")
	if err != nil {
		m.metrics.RecordWarmupError(prefix, "template_error")
		return fmt.Errorf("failed to process template: %w", err)
	}

	// Step 4: Send warmup request to llama.cpp with cancellation support
	if err := m.sendWarmupRequest(ctx, prefix, warmupContent); err != nil {
		// Check if we were cancelled
		if ctx.Err() == context.Canceled {
			log.Printf("Warmup for %s was cancelled", prefix)
			// Don't record error or update state - cancellation is expected
			return fmt.Errorf("warmup cancelled")
		}
		m.metrics.RecordWarmupError(prefix, "completion_failed")
		return fmt.Errorf("warmup request failed: %w", err)
	}

	// Step 5: Update state to reflect that this template is now loaded
	// We do NOT save the KV cache here - we only save when switching away
	m.backendState.UpdatePrefix(prefix)

	// Record successful warmup execution and duration
	duration := time.Since(startTime).Seconds()
	m.metrics.RecordWarmupExecution(prefix, duration)

	return nil
}

// restoreKVCache restores KV cache from file
func (m *Manager) restoreKVCache(prefix, filename string) error {
	url := fmt.Sprintf("%s/slots/0?action=restore", m.backendURL)

	reqBody := map[string]string{
		"filename": filename,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		m.metrics.RecordKVCacheRestore(prefix, "error")
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		m.metrics.RecordKVCacheRestore(prefix, "error")
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		m.metrics.RecordKVCacheRestore(prefix, "error")
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for logging
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		m.metrics.RecordKVCacheRestore(prefix, "not_found")
		return fmt.Errorf("cache file not found (404)")
	}

	if resp.StatusCode != http.StatusOK {
		m.metrics.RecordKVCacheRestore(prefix, "error")
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	m.metrics.RecordKVCacheRestore(prefix, "success")
	log.Printf("KV cache restored for %s", filename)
	return nil
}

// saveKVCache saves KV cache to file
func (m *Manager) saveKVCache(prefix, filename string) error {
	url := fmt.Sprintf("%s/slots/0?action=save", m.backendURL)

	reqBody := map[string]string{
		"filename": filename,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for logging
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	m.metrics.RecordKVCacheSave(prefix)
	log.Printf("KV cache saved for %s", filename)
	return nil
}

// sendWarmupRequest sends a chat completion request with the warmup content
// The context allows the request to be cancelled if a user request arrives
func (m *Manager) sendWarmupRequest(ctx context.Context, prefix, content string) error {
	url := fmt.Sprintf("%s/v1/chat/completions", m.backendURL)

	// Build minimal warmup request
	reqBody := map[string]interface{}{
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": content,
			},
		},
		"max_tokens": 1,     // Minimal generation
		"stream":     false, // Non-streaming
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	log.Printf("Sending warmup request for %s", prefix)

	startTime := time.Now()

	// Create request with cancellable context
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		// Check if error was due to context cancellation
		if ctx.Err() == context.Canceled {
			return fmt.Errorf("request cancelled: %w", ctx.Err())
		}
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(startTime)

	// Read response body
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("Warmup request completed for %s (%.2fs)", prefix, duration.Seconds())
	return nil
}
