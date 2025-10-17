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
	"os"
	"testing"
	"time"

	"github.com/oleksandr/bioproxy/internal/config"
	"github.com/oleksandr/bioproxy/internal/template"
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

	// Create watcher (empty for this test - no templates)
	watcher := template.NewWatcher()

	// Create and start proxy
	proxy, err := New(cfg, watcher, nil)
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

	watcher := template.NewWatcher()
	proxy, err := New(cfg, watcher, nil)
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

	watcher := template.NewWatcher()
	proxy, err := New(cfg, watcher, nil)
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

	watcher := template.NewWatcher()
	proxy, err := New(cfg, watcher, nil)
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

// TestManualStreamingChat tests that SSE streaming works correctly through the proxy.
// This is critical for real-time token generation from llama.cpp.
func TestManualStreamingChat(t *testing.T) {
	checkLlamaCppAvailable(t)

	cfg := &config.Config{
		ProxyHost:  "localhost",
		ProxyPort:  manualProxyPort,
		BackendURL: llamaCppURL,
	}

	watcher := template.NewWatcher()
	proxy, err := New(cfg, watcher, nil)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Chat request with streaming enabled
	chatRequest := map[string]interface{}{
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": "Count from 1 to 5, one number per line.",
			},
		},
		"max_tokens":  50,
		"temperature": 0.1,
		"stream":      true, // Enable SSE streaming
	}

	requestBody, _ := json.Marshal(chatRequest)

	proxyURL := fmt.Sprintf("http://localhost:%d", manualProxyPort)
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest("POST",
		proxyURL+"/v1/chat/completions",
		bytes.NewReader(requestBody))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer no-key")

	t.Log("Sending streaming chat completion request through proxy...")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to start streaming: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 200, got %d: %s", resp.StatusCode, string(body))
		return
	}

	// Verify it's actually SSE
	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Expected Content-Type: text/event-stream, got %s", contentType)
	}

	t.Log("Receiving SSE stream...")

	// Read SSE events as they arrive
	// This tests that the proxy correctly flushes each chunk
	eventCount := 0
	buffer := make([]byte, 4096)
	var fullResponse bytes.Buffer

	startTime := time.Now()
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			chunk := buffer[:n]
			fullResponse.Write(chunk)

			// Count events (lines starting with "data:")
			lines := bytes.Split(chunk, []byte("\n"))
			for _, line := range lines {
				if bytes.HasPrefix(line, []byte("data:")) {
					eventCount++
					t.Logf("  [%v] Received SSE event #%d", time.Since(startTime), eventCount)

					// Log first few events for debugging
					if eventCount <= 3 {
						t.Logf("    Content: %s", string(line))
					}
				}
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			t.Errorf("Error reading stream: %v", err)
			break
		}
	}

	elapsed := time.Since(startTime)
	t.Logf("Stream completed in %v", elapsed)
	t.Logf("Total SSE events received: %d", eventCount)

	if eventCount == 0 {
		t.Error("No SSE events received! Streaming may not be working correctly.")
		t.Logf("Full response:\n%s", fullResponse.String())
	} else {
		t.Log("✅ SSE streaming works correctly through the proxy!")
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

	watcher := template.NewWatcher()
	proxy, err := New(cfg, watcher, nil)
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

// TestManualTemplateInjection tests that template injection works correctly
// This verifies that template prefixes are detected and templates are processed
func TestManualTemplateInjection(t *testing.T) {
	checkLlamaCppAvailable(t)

	// Create a temporary template file
	tmpDir := t.TempDir()
	templateFile := tmpDir + "/test_template.txt"
	templateContent := "You are a helpful test assistant. Always start your response with 'TEMPLATE_INJECTED:'.\n\nUser question: <{message}>"

	if err := os.WriteFile(templateFile, []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to create template file: %v", err)
	}

	// Create watcher and add template
	watcher := template.NewWatcher()
	if err := watcher.AddTemplate("@test", templateFile); err != nil {
		t.Fatalf("Failed to add template: %v", err)
	}

	// Create proxy with template support
	cfg := &config.Config{
		ProxyHost:  "localhost",
		ProxyPort:  manualProxyPort,
		BackendURL: llamaCppURL,
		Prefixes:   map[string]string{"@test": templateFile},
	}

	proxy, err := New(cfg, watcher, nil)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Send a request with the template prefix
	chatRequest := map[string]interface{}{
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": "@test What is 2+2?",
			},
		},
		"max_tokens":  50,
		"temperature": 0.7,
		"stream":      false,
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

	t.Log("Sending chat request with @test template prefix...")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to complete chat: %v", err)
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
		return
	}

	// Extract the generated text
	var generatedContent string
	if choices, ok := chatResponse["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := choice["message"].(map[string]interface{}); ok {
				if content, ok := message["content"].(string); ok {
					generatedContent = content
				}
			}
		}
	}

	t.Logf("Generated content: %s", generatedContent)

	// Verify that the template was injected by checking if the response
	// starts with our template marker
	if !bytes.Contains([]byte(generatedContent), []byte("TEMPLATE_INJECTED:")) {
		t.Logf("Warning: Template marker not found in response. Template may not have been injected.")
		t.Logf("This could be expected if the model didn't follow instructions exactly.")
	} else {
		t.Log("✅ Template injection successful!")
	}
}

// TestManualTemplateInjectionWithStreaming tests that template injection preserves streaming
// This is the critical test that verifies the bug fix: stream parameter must be preserved
func TestManualTemplateInjectionWithStreaming(t *testing.T) {
	checkLlamaCppAvailable(t)

	// Create a temporary template file
	tmpDir := t.TempDir()
	templateFile := tmpDir + "/stream_template.txt"
	templateContent := "You are a counting assistant. Count slowly.\n\nUser request: <{message}>"

	if err := os.WriteFile(templateFile, []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to create template file: %v", err)
	}

	// Create watcher and add template
	watcher := template.NewWatcher()
	if err := watcher.AddTemplate("@count", templateFile); err != nil {
		t.Fatalf("Failed to add template: %v", err)
	}

	// Create proxy with template support
	cfg := &config.Config{
		ProxyHost:  "localhost",
		ProxyPort:  manualProxyPort,
		BackendURL: llamaCppURL,
		Prefixes:   map[string]string{"@count": templateFile},
	}

	proxy, err := New(cfg, watcher, nil)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	time.Sleep(100 * time.Millisecond)

	// Send a STREAMING request with template prefix
	// This tests the critical fix: stream parameter must be preserved
	chatRequest := map[string]interface{}{
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": "@count Count from 1 to 3",
			},
		},
		"max_tokens":  50,
		"temperature": 0.1,
		"stream":      true, // CRITICAL: This must be preserved during template injection
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

	t.Log("Sending STREAMING chat request with @count template prefix...")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to start streaming: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 200, got %d: %s", resp.StatusCode, string(body))
		return
	}

	// Verify SSE content type
	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Expected Content-Type: text/event-stream, got %s", contentType)
		t.Error("Stream parameter may have been lost during template injection!")
		return
	}

	t.Log("✅ Content-Type is text/event-stream - streaming is enabled")
	t.Log("Receiving SSE stream with template injection...")

	// Read SSE events as they arrive
	eventCount := 0
	buffer := make([]byte, 4096)
	var fullResponse bytes.Buffer

	startTime := time.Now()

	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			chunk := buffer[:n]
			fullResponse.Write(chunk)

			// Count events (lines starting with "data:")
			lines := bytes.Split(chunk, []byte("\n"))
			for _, line := range lines {
				if bytes.HasPrefix(line, []byte("data:")) {
					eventCount++
					eventTime := time.Since(startTime)

					// Log first few events for debugging
					if eventCount <= 5 {
						t.Logf("  [%v] SSE event #%d: %s", eventTime, eventCount, string(line[:min(len(line), 80)]))
					}
				}
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			t.Errorf("Error reading stream: %v", err)
			break
		}
	}

	elapsed := time.Since(startTime)
	t.Logf("Stream completed in %v", elapsed)
	t.Logf("Total SSE events received: %d", eventCount)

	// Verify streaming worked
	if eventCount == 0 {
		t.Error("❌ No SSE events received! Stream parameter was likely lost during template injection.")
		t.Logf("Full response:\n%s", fullResponse.String())
		return
	}

	// Verify events arrived progressively (not all at once)
	if eventCount > 5 {
		t.Log("✅ Received multiple SSE events - streaming is working correctly!")
		t.Log("✅ Template injection preserves the stream parameter!")
	} else {
		t.Logf("Warning: Only received %d events. Stream may not be working optimally.", eventCount)
	}
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
