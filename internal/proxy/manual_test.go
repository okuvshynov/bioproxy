//go:build manual
// +build manual

// This file contains manual integration tests that require a running llama.cpp server.
// These tests are excluded from CI/CD pipelines and regular test runs.
//
// To run these tests:
//   1. Start llama.cpp server on localhost:8081 (default port)
//   2. Run: go test -tags=manual -v ./internal/proxy/...
//
// Example llama.cpp startup:
//   ./llama-server -m /path/to/model.gguf --port 8081

package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/oleksandr/bioproxy/internal/config"
)

const (
	// Default llama.cpp server URL
	llamaCppURL = "http://localhost:8081"

	// Proxy will listen on this port for manual tests
	manualProxyPort = 18088
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

	t.Logf("llama.cpp server is available at %s", llamaCppURL)
}

// TestManualHealthCheck tests that the proxy correctly forwards health checks to llama.cpp
func TestManualHealthCheck(t *testing.T) {
	checkLlamaCppAvailable(t)

	// Create proxy configuration
	cfg := &config.Config{
		ProxyHost:  "localhost",
		ProxyPort:  manualProxyPort,
		BackendURL: llamaCppURL,
	}

	// Create and start proxy
	proxy, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	// Give the proxy a moment to start
	time.Sleep(100 * time.Millisecond)

	// Make a health check request through the proxy
	proxyURL := fmt.Sprintf("http://localhost:%d", manualProxyPort)
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(proxyURL + "/health")
	if err != nil {
		t.Fatalf("Failed to request through proxy: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	t.Logf("Health check response: %s", string(body))
}

// TestManualSlotSave tests the KV cache save endpoint
// This is critical for our warmup functionality
func TestManualSlotSave(t *testing.T) {
	checkLlamaCppAvailable(t)

	cfg := &config.Config{
		ProxyHost:  "localhost",
		ProxyPort:  manualProxyPort,
		BackendURL: llamaCppURL,
	}

	proxy, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Prepare the save request
	// This saves the current KV cache for slot 0 to a file
	saveRequest := map[string]interface{}{
		"filename": "test_manual_slot.bin",
	}

	requestBody, _ := json.Marshal(saveRequest)

	proxyURL := fmt.Sprintf("http://localhost:%d", manualProxyPort)
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequest("POST",
		proxyURL+"/slots/0?action=save",
		bytes.NewReader(requestBody))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer no-key")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to save slot through proxy: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.Logf("Slot save response (status %d): %s", resp.StatusCode, string(body))

	// llama.cpp might return different status codes depending on state
	// We just verify the request went through the proxy
	if resp.StatusCode >= 500 {
		t.Errorf("Server error saving slot: status %d", resp.StatusCode)
	}
}

// TestManualSlotRestore tests the KV cache restore endpoint
func TestManualSlotRestore(t *testing.T) {
	checkLlamaCppAvailable(t)

	cfg := &config.Config{
		ProxyHost:  "localhost",
		ProxyPort:  manualProxyPort,
		BackendURL: llamaCppURL,
	}

	proxy, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Prepare the restore request
	restoreRequest := map[string]interface{}{
		"filename": "test_manual_slot.bin",
	}

	requestBody, _ := json.Marshal(restoreRequest)

	proxyURL := fmt.Sprintf("http://localhost:%d", manualProxyPort)
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequest("POST",
		proxyURL+"/slots/0?action=restore",
		bytes.NewReader(requestBody))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer no-key")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to restore slot through proxy: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.Logf("Slot restore response (status %d): %s", resp.StatusCode, string(body))

	// File might not exist, which is expected for first run
	// We just verify the request went through
	if resp.StatusCode >= 500 {
		t.Errorf("Server error restoring slot: status %d", resp.StatusCode)
	}
}

// TestManualChatCompletion tests proxying a chat completion request
// This is the main endpoint we'll be intercepting for template injection
func TestManualChatCompletion(t *testing.T) {
	checkLlamaCppAvailable(t)

	cfg := &config.Config{
		ProxyHost:  "localhost",
		ProxyPort:  manualProxyPort,
		BackendURL: llamaCppURL,
	}

	proxy, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Prepare a simple chat completion request
	// This follows the OpenAI API format
	chatRequest := map[string]interface{}{
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": "Say 'proxy test successful' and nothing else.",
			},
		},
		"max_tokens":  20,
		"temperature": 0.7,
		// Stream false for simpler testing
		"stream": false,
	}

	requestBody, _ := json.Marshal(chatRequest)

	proxyURL := fmt.Sprintf("http://localhost:%d", manualProxyPort)
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest("POST",
		proxyURL+"/v1/chat/completions",
		bytes.NewReader(requestBody))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer no-key")

	t.Log("Sending chat completion request through proxy...")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to complete chat through proxy: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
		t.Logf("Response body: %s", string(body))
		return
	}

	// Parse the response
	var chatResponse map[string]interface{}
	if err := json.Unmarshal(body, &chatResponse); err != nil {
		t.Errorf("Failed to parse response: %v", err)
		t.Logf("Response body: %s", string(body))
		return
	}

	t.Logf("Chat completion successful!")
	t.Logf("Full response: %s", string(body))

	// Extract the generated text if available
	if choices, ok := chatResponse["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := choice["message"].(map[string]interface{}); ok {
				if content, ok := message["content"].(string); ok {
					t.Logf("Generated content: %s", content)
				}
			}
		}
	}
}

// TestManualProxyPerformance tests basic performance characteristics
// This helps verify the proxy doesn't add significant overhead
func TestManualProxyPerformance(t *testing.T) {
	checkLlamaCppAvailable(t)

	cfg := &config.Config{
		ProxyHost:  "localhost",
		ProxyPort:  manualProxyPort,
		BackendURL: llamaCppURL,
	}

	proxy, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	proxyURL := fmt.Sprintf("http://localhost:%d", manualProxyPort)
	client := &http.Client{Timeout: 5 * time.Second}

	// Warm up
	client.Get(proxyURL + "/health")

	// Measure 10 health check requests
	iterations := 10
	start := time.Now()

	for i := 0; i < iterations; i++ {
		resp, err := client.Get(proxyURL + "/health")
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Request %d returned status %d", i, resp.StatusCode)
		}
	}

	elapsed := time.Since(start)
	avgLatency := elapsed / time.Duration(iterations)

	t.Logf("Completed %d requests in %v", iterations, elapsed)
	t.Logf("Average latency: %v", avgLatency)

	// Proxy overhead should be minimal (< 1ms per request on localhost)
	if avgLatency > 100*time.Millisecond {
		t.Logf("Warning: Average latency seems high (%v). Check for performance issues.", avgLatency)
	}
}
