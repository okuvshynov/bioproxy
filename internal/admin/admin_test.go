package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oleksandr/bioproxy/internal/config"
)

// createTestConfig creates a minimal config for testing
func createTestConfig() *config.Config {
	return &config.Config{
		AdminHost: "localhost",
		AdminPort: 0, // Let OS assign port
	}
}

// TestNewMetrics verifies that creating a new metrics instance works
func TestNewMetrics(t *testing.T) {
	metrics := NewMetrics()

	if metrics == nil {
		t.Fatal("Expected non-nil metrics")
	}

	if metrics.RequestCount == nil {
		t.Error("Expected RequestCount map to be initialized")
	}

	if metrics.TotalRequests != 0 {
		t.Errorf("Expected TotalRequests to be 0, got %d", metrics.TotalRequests)
	}

	if metrics.StartTime.IsZero() {
		t.Error("Expected StartTime to be set")
	}
}

// TestMetricsRecordRequest tests recording individual requests
func TestMetricsRecordRequest(t *testing.T) {
	metrics := NewMetrics()

	// Record a few requests
	metrics.RecordRequest("/health", 200)
	metrics.RecordRequest("/health", 200)
	metrics.RecordRequest("/v1/chat/completions", 200)
	metrics.RecordRequest("/v1/chat/completions", 500)

	// Check total requests
	if metrics.TotalRequests != 4 {
		t.Errorf("Expected TotalRequests to be 4, got %d", metrics.TotalRequests)
	}

	// Check endpoint-specific counters
	snapshot := metrics.GetSnapshot()

	if snapshot["/health"]["200"] != 2 {
		t.Errorf("Expected /health 200 count to be 2, got %d", snapshot["/health"]["200"])
	}

	if snapshot["/v1/chat/completions"]["200"] != 1 {
		t.Errorf("Expected /v1/chat/completions 200 count to be 1, got %d",
			snapshot["/v1/chat/completions"]["200"])
	}

	if snapshot["/v1/chat/completions"]["500"] != 1 {
		t.Errorf("Expected /v1/chat/completions 500 count to be 1, got %d",
			snapshot["/v1/chat/completions"]["500"])
	}
}

// TestMetricsConcurrency tests that metrics can be safely accessed concurrently
func TestMetricsConcurrency(t *testing.T) {
	metrics := NewMetrics()

	// Launch multiple goroutines to record requests concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				metrics.RecordRequest("/test", 200)
			}
			done <- true
		}()
	}

	// Wait for all goroutines to finish
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have 1000 total requests
	if metrics.TotalRequests != 1000 {
		t.Errorf("Expected TotalRequests to be 1000, got %d", metrics.TotalRequests)
	}

	snapshot := metrics.GetSnapshot()
	if snapshot["/test"]["200"] != 1000 {
		t.Errorf("Expected /test 200 count to be 1000, got %d", snapshot["/test"]["200"])
	}
}

// TestNew verifies that creating a new admin server works
func TestNew(t *testing.T) {
	cfg := createTestConfig()
	metrics := NewMetrics()

	server := New(cfg, metrics)

	if server == nil {
		t.Fatal("Expected non-nil server")
	}

	if server.config != cfg {
		t.Error("Server config doesn't match input config")
	}

	if server.metrics != metrics {
		t.Error("Server metrics doesn't match input metrics")
	}

	if server.running {
		t.Error("Newly created server should not be running")
	}
}

// TestStartStop tests starting and stopping the admin server
func TestStartStop(t *testing.T) {
	cfg := createTestConfig()
	metrics := NewMetrics()
	server := New(cfg, metrics)

	// Initially should not be running
	if server.IsRunning() {
		t.Error("Server should not be running initially")
	}

	// Start the server
	err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Should be running now
	if !server.IsRunning() {
		t.Error("Server should be running after Start()")
	}

	// Try starting again - should error
	err = server.Start()
	if err == nil {
		t.Error("Expected error when starting already running server")
	}

	// Stop the server
	err = server.Stop()
	if err != nil {
		t.Fatalf("Failed to stop server: %v", err)
	}

	// Should not be running
	if server.IsRunning() {
		t.Error("Server should not be running after Stop()")
	}

	// Try stopping again - should error
	err = server.Stop()
	if err == nil {
		t.Error("Expected error when stopping already stopped server")
	}
}

// TestHandleHealth tests the /health endpoint
func TestHandleHealth(t *testing.T) {
	cfg := createTestConfig()
	metrics := NewMetrics()
	server := New(cfg, metrics)

	// Manually set start time for testing
	server.startTime = time.Now().Add(-10 * time.Second)

	// Create a test request
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	// Call the handler
	server.handleHealth(rr, req)

	// Check status code
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	// Check content type
	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	// Parse response
	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check response fields
	if response["status"] != "ok" {
		t.Errorf("Expected status 'ok', got %v", response["status"])
	}

	// Uptime should be around 10 seconds
	uptime, ok := response["uptime_seconds"].(float64)
	if !ok {
		t.Error("uptime_seconds should be a number")
	}
	if uptime < 9 || uptime > 11 {
		t.Errorf("Expected uptime around 10 seconds, got %.2f", uptime)
	}

	// start_time should be present
	if response["start_time"] == nil {
		t.Error("Expected start_time to be present")
	}
}

// TestHandleHealthMethodNotAllowed tests that non-GET requests are rejected
func TestHandleHealthMethodNotAllowed(t *testing.T) {
	cfg := createTestConfig()
	metrics := NewMetrics()
	server := New(cfg, metrics)

	methods := []string{"POST", "PUT", "DELETE", "PATCH"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/health", nil)
			rr := httptest.NewRecorder()

			server.handleHealth(rr, req)

			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status 405, got %d", rr.Code)
			}
		})
	}
}

// TestHandleMetrics tests the /metrics endpoint
func TestHandleMetrics(t *testing.T) {
	cfg := createTestConfig()
	metrics := NewMetrics()
	server := New(cfg, metrics)

	// Record some test metrics
	metrics.RecordRequest("/health", 200)
	metrics.RecordRequest("/health", 200)
	metrics.RecordRequest("/v1/chat/completions", 200)
	metrics.RecordRequest("/v1/chat/completions", 500)

	// Set start time
	server.startTime = time.Now().Add(-30 * time.Second)

	// Create a test request
	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()

	// Call the handler
	server.handleMetrics(rr, req)

	// Check status code
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	// Check content type (Prometheus text format)
	contentType := rr.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Errorf("Expected Content-Type text/plain, got %s", contentType)
	}

	// Read response body
	body, err := io.ReadAll(rr.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	bodyStr := string(body)

	// Verify response contains expected metrics
	expectedStrings := []string{
		"# HELP bioproxy_requests_total",
		"# TYPE bioproxy_requests_total counter",
		`bioproxy_requests_total{endpoint="/health",status="200"} 2`,
		`bioproxy_requests_total{endpoint="/v1/chat/completions",status="200"} 1`,
		`bioproxy_requests_total{endpoint="/v1/chat/completions",status="500"} 1`,
		"# HELP bioproxy_requests_count",
		"# TYPE bioproxy_requests_count counter",
		"bioproxy_requests_count 4",
		"# HELP bioproxy_uptime_seconds",
		"# TYPE bioproxy_uptime_seconds gauge",
		"bioproxy_uptime_seconds",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(bodyStr, expected) {
			t.Errorf("Expected response to contain '%s', got:\n%s", expected, bodyStr)
		}
	}
}

// TestHandleMetricsMethodNotAllowed tests that non-GET requests are rejected
func TestHandleMetricsMethodNotAllowed(t *testing.T) {
	cfg := createTestConfig()
	metrics := NewMetrics()
	server := New(cfg, metrics)

	methods := []string{"POST", "PUT", "DELETE", "PATCH"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/metrics", nil)
			rr := httptest.NewRecorder()

			server.handleMetrics(rr, req)

			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status 405, got %d", rr.Code)
			}
		})
	}
}

// TestHandleMetricsEmpty tests metrics endpoint with no recorded requests
func TestHandleMetricsEmpty(t *testing.T) {
	cfg := createTestConfig()
	metrics := NewMetrics()
	server := New(cfg, metrics)
	server.startTime = time.Now()

	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()

	server.handleMetrics(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	body, _ := io.ReadAll(rr.Body)
	bodyStr := string(body)

	// Should still have the metric definitions and total count of 0
	if !strings.Contains(bodyStr, "bioproxy_requests_count 0") {
		t.Errorf("Expected total count of 0, got:\n%s", bodyStr)
	}
}

// TestMetricsSnapshot tests that GetSnapshot returns an independent copy
func TestMetricsSnapshot(t *testing.T) {
	metrics := NewMetrics()

	metrics.RecordRequest("/test", 200)

	// Get a snapshot
	snapshot := metrics.GetSnapshot()

	// Modify the snapshot
	snapshot["/test"]["200"] = 999
	snapshot["/modified"] = map[string]int64{"200": 100}

	// Original metrics should be unchanged
	originalSnapshot := metrics.GetSnapshot()
	if originalSnapshot["/test"]["200"] != 1 {
		t.Error("Original metrics were modified through snapshot")
	}

	if originalSnapshot["/modified"] != nil {
		t.Error("New entry in snapshot affected original metrics")
	}
}
