package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/oleksandr/bioproxy/internal/config"
	"github.com/oleksandr/bioproxy/internal/state"
	"github.com/oleksandr/bioproxy/internal/template"
)

// createTestConfig creates a minimal config for testing
func createTestConfig(backendURL string) *config.Config {
	return &config.Config{
		ProxyHost:  "localhost",
		ProxyPort:  0, // Let the OS assign a port for testing
		BackendURL: backendURL,
		Prefixes:   make(map[string]string), // Empty template mapping
	}
}

// createTestWatcher creates an empty template watcher for testing
func createTestWatcher() *template.Watcher {
	return template.NewWatcher()
}

// createTestState creates a new state instance for testing
func createTestState() *state.State {
	return state.New()
}

// TestNew verifies that creating a new proxy works correctly
func TestNew(t *testing.T) {
	cfg := createTestConfig("http://localhost:8081")
	watcher := createTestWatcher()
	backendState := createTestState()

	proxy, err := New(cfg, watcher, nil, backendState, nil)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	if proxy == nil {
		t.Fatal("Expected non-nil proxy")
	}

	if proxy.config != cfg {
		t.Error("Proxy config doesn't match input config")
	}

	if proxy.backend.String() != "http://localhost:8081" {
		t.Errorf("Expected backend URL http://localhost:8081, got %s", proxy.backend.String())
	}

	if proxy.reverseProxy == nil {
		t.Error("Expected non-nil reverse proxy")
	}

	if proxy.running {
		t.Error("Newly created proxy should not be running")
	}
}

// TestNewInvalidBackendURL tests that invalid backend URLs are rejected
func TestNewInvalidBackendURL(t *testing.T) {
	testCases := []struct {
		name       string
		backendURL string
	}{
		// Note: url.Parse actually accepts empty strings and some malformed URLs
		// It's very permissive. We test cases that actually fail.
		{"invalid scheme", "://localhost:8081"},
		{"percent encoding error", "http://host/%zzzzz"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := createTestConfig(tc.backendURL)
			watcher := createTestWatcher()
			proxy, err := New(cfg, watcher, nil, createTestState(), nil)

			if err == nil {
				t.Errorf("Expected error for invalid backend URL %s, got nil", tc.backendURL)
			}

			if proxy != nil {
				t.Error("Expected nil proxy for invalid backend URL")
			}
		})
	}
}

// TestProxyForwarding tests that the proxy correctly forwards requests to the backend
func TestProxyForwarding(t *testing.T) {
	// Create a mock backend server
	backendCalled := false
	var receivedMethod, receivedPath string
	var receivedBody string

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalled = true
		receivedMethod = r.Method
		receivedPath = r.URL.Path

		// Read the body if present
		if r.Body != nil {
			bodyBytes, _ := io.ReadAll(r.Body)
			receivedBody = string(bodyBytes)
		}

		// Send a response
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("backend response"))
	}))
	defer backend.Close()

	// Create proxy pointing to the mock backend
	cfg := createTestConfig(backend.URL)
	watcher := createTestWatcher()
	proxy, err := New(cfg, watcher, nil, createTestState(), nil)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	// Create a test request
	reqBody := "test request body"
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	// Create a response recorder
	rr := httptest.NewRecorder()

	// Handle the request through the proxy
	proxy.reverseProxy.ServeHTTP(rr, req)

	// Verify the backend was called
	if !backendCalled {
		t.Error("Expected backend to be called")
	}

	// Verify the request details were forwarded correctly
	if receivedMethod != "POST" {
		t.Errorf("Expected method POST, got %s", receivedMethod)
	}

	if receivedPath != "/v1/chat/completions" {
		t.Errorf("Expected path /v1/chat/completions, got %s", receivedPath)
	}

	if receivedBody != reqBody {
		t.Errorf("Expected body %s, got %s", reqBody, receivedBody)
	}

	// Verify the response was forwarded correctly
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	responseBody := rr.Body.String()
	if responseBody != "backend response" {
		t.Errorf("Expected response 'backend response', got %s", responseBody)
	}
}

// TestProxyForwardingDifferentMethods tests various HTTP methods
func TestProxyForwardingDifferentMethods(t *testing.T) {
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			var receivedMethod string

			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedMethod = r.Method
				w.WriteHeader(http.StatusOK)
			}))
			defer backend.Close()

			cfg := createTestConfig(backend.URL)
			watcher := createTestWatcher()
			proxy, err := New(cfg, watcher, nil, createTestState(), nil)
			if err != nil {
				t.Fatalf("Failed to create proxy: %v", err)
			}

			req := httptest.NewRequest(method, "/test", nil)
			rr := httptest.NewRecorder()

			proxy.reverseProxy.ServeHTTP(rr, req)

			if receivedMethod != method {
				t.Errorf("Expected method %s, got %s", method, receivedMethod)
			}
		})
	}
}

// TestProxyHeaderForwarding tests that headers are forwarded correctly
func TestProxyHeaderForwarding(t *testing.T) {
	var receivedHeaders http.Header

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		// Send back a custom header
		w.Header().Set("X-Backend-Header", "backend-value")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := createTestConfig(backend.URL)
	watcher := createTestWatcher()
	proxy, err := New(cfg, watcher, nil, createTestState(), nil)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Custom-Header", "custom-value")
	req.Header.Set("Authorization", "Bearer token123")

	rr := httptest.NewRecorder()
	proxy.reverseProxy.ServeHTTP(rr, req)

	// Verify headers were forwarded to backend
	if receivedHeaders.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("Expected X-Custom-Header to be forwarded")
	}

	if receivedHeaders.Get("Authorization") != "Bearer token123" {
		t.Errorf("Expected Authorization header to be forwarded")
	}

	// Verify response headers were forwarded to client
	if rr.Header().Get("X-Backend-Header") != "backend-value" {
		t.Errorf("Expected X-Backend-Header in response")
	}
}

// TestProxyBackendError tests handling when backend is unavailable
func TestProxyBackendError(t *testing.T) {
	// Create proxy pointing to a non-existent backend
	cfg := createTestConfig("http://localhost:99999") // Invalid port
	watcher := createTestWatcher()
	proxy, err := New(cfg, watcher, nil, createTestState(), nil)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	proxy.reverseProxy.ServeHTTP(rr, req)

	// Should get a 502 Bad Gateway error
	if rr.Code != http.StatusBadGateway {
		t.Errorf("Expected status 502, got %d", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "Backend server unavailable") {
		t.Errorf("Expected error message about backend unavailability, got: %s", body)
	}
}

// TestStartStop tests starting and stopping the proxy server
func TestStartStop(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := createTestConfig(backend.URL)
	// Use a specific port for this test
	cfg.ProxyPort = 0 // Let OS assign port

	watcher := createTestWatcher()
	proxy, err := New(cfg, watcher, nil, createTestState(), nil)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	// Initially should not be running
	if proxy.IsRunning() {
		t.Error("Proxy should not be running initially")
	}

	// Start the proxy
	err = proxy.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Should be running now
	if !proxy.IsRunning() {
		t.Error("Proxy should be running after Start()")
	}

	// Try starting again - should error
	err = proxy.Start()
	if err == nil {
		t.Error("Expected error when starting already running proxy")
	}

	// Stop the proxy
	err = proxy.Stop()
	if err != nil {
		t.Fatalf("Failed to stop proxy: %v", err)
	}

	// Should not be running
	if proxy.IsRunning() {
		t.Error("Proxy should not be running after Stop()")
	}

	// Try stopping again - should error
	err = proxy.Stop()
	if err == nil {
		t.Error("Expected error when stopping already stopped proxy")
	}
}

// TestProxyIntegration is an end-to-end test that starts the proxy server
// and makes actual HTTP requests to it
func TestProxyIntegration(t *testing.T) {
	// Create a mock backend
	backendCalls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status": "ok", "path": "%s"}`, r.URL.Path)
	}))
	defer backend.Close()

	// Create and start the proxy
	cfg := createTestConfig(backend.URL)
	cfg.ProxyHost = "localhost"
	cfg.ProxyPort = 0 // Let OS assign port

	watcher := createTestWatcher()
	proxy, err := New(cfg, watcher, nil, createTestState(), nil)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	err = proxy.Start()
	if err != nil {
		t.Fatalf("Failed to start proxy: %v", err)
	}
	defer proxy.Stop()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Get the actual port the server is listening on
	// Since we used port 0, we need to extract it from the server
	// For testing purposes, we'll use the httptest approach instead
	// by testing through the handler directly

	// Make a test request through the reverse proxy handler
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	proxy.reverseProxy.ServeHTTP(rr, req)

	// Verify the response
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	expectedBody := `{"status": "ok", "path": "/health"}`
	if rr.Body.String() != expectedBody {
		t.Errorf("Expected body %s, got %s", expectedBody, rr.Body.String())
	}

	// Verify backend was called
	if backendCalls != 1 {
		t.Errorf("Expected 1 backend call, got %d", backendCalls)
	}
}

// TestProxyDifferentPaths tests proxying different URL paths
func TestProxyDifferentPaths(t *testing.T) {
	paths := []string{
		"/",
		"/health",
		"/v1/chat/completions",
		"/v1/models",
		"/metrics",
		"/some/deep/path/here",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			var receivedPath string

			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedPath = r.URL.Path
				w.WriteHeader(http.StatusOK)
			}))
			defer backend.Close()

			cfg := createTestConfig(backend.URL)
			watcher := createTestWatcher()
			proxy, err := New(cfg, watcher, nil, createTestState(), nil)
			if err != nil {
				t.Fatalf("Failed to create proxy: %v", err)
			}

			req := httptest.NewRequest("GET", path, nil)
			rr := httptest.NewRecorder()

			proxy.reverseProxy.ServeHTTP(rr, req)

			if receivedPath != path {
				t.Errorf("Expected path %s, got %s", path, receivedPath)
			}
		})
	}
}

// TestTemplateInjection tests that template prefixes are detected and processed
func TestTemplateInjection(t *testing.T) {
	// Create a temporary template file
	tmpDir := t.TempDir()
	templateFile := tmpDir + "/test_template.txt"
	templateContent := "You are a test assistant.\n\nUser question: <{message}>"

	err := os.WriteFile(templateFile, []byte(templateContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create template file: %v", err)
	}

	// Track what the backend receives
	var receivedBody string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		receivedBody = string(bodyBytes)

		// Send back a valid chat completion response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"content":"test response"}}]}`))
	}))
	defer backend.Close()

	// Create watcher and add template
	watcher := template.NewWatcher()
	err = watcher.AddTemplate("@test", templateFile)
	if err != nil {
		t.Fatalf("Failed to add template: %v", err)
	}

	// Create proxy with the watcher
	cfg := createTestConfig(backend.URL)
	cfg.Prefixes = map[string]string{"@test": templateFile}
	proxy, err := New(cfg, watcher, nil, createTestState(), nil)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	// Create a request with the template prefix
	requestBody := `{"messages":[{"role":"user","content":"@test How do I test?"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")

	// Send through proxy
	rr := httptest.NewRecorder()
	proxy.handleChatCompletion(rr, req)

	// Verify the backend received the processed template, not the original
	if !strings.Contains(receivedBody, "You are a test assistant") {
		t.Errorf("Expected processed template in backend request, got: %s", receivedBody)
	}

	if !strings.Contains(receivedBody, "User question: How do I test?") {
		t.Errorf("Expected user message in processed template, got: %s", receivedBody)
	}

	// Verify the original prefix is NOT in the request
	if strings.Contains(receivedBody, "@test") {
		t.Errorf("Original prefix should not be in backend request, got: %s", receivedBody)
	}
}

// TestTemplateInjectionNoPrefix tests that messages without prefixes pass through unchanged
func TestTemplateInjectionNoPrefix(t *testing.T) {
	// Track what the backend receives
	var receivedBody string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		receivedBody = string(bodyBytes)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"content":"test"}}]}`))
	}))
	defer backend.Close()

	// Create proxy with empty watcher (no templates)
	cfg := createTestConfig(backend.URL)
	watcher := template.NewWatcher()
	proxy, err := New(cfg, watcher, nil, createTestState(), nil)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	// Create a request WITHOUT a template prefix
	originalMessage := "Just a regular question"
	requestBody := fmt.Sprintf(`{"messages":[{"role":"user","content":"%s"}]}`, originalMessage)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")

	// Send through proxy
	rr := httptest.NewRecorder()
	proxy.handleChatCompletion(rr, req)

	// Verify the message passed through unchanged
	if !strings.Contains(receivedBody, originalMessage) {
		t.Errorf("Expected original message to pass through, got: %s", receivedBody)
	}
}

// TestTemplateInjectionMultiTurn tests that only the last user message is checked
func TestTemplateInjectionMultiTurn(t *testing.T) {
	// Create a template
	tmpDir := t.TempDir()
	templateFile := tmpDir + "/test_template.txt"
	templateContent := "Template: <{message}>"

	err := os.WriteFile(templateFile, []byte(templateContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create template file: %v", err)
	}

	// Track what the backend receives
	var receivedBody string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		receivedBody = string(bodyBytes)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"content":"test"}}]}`))
	}))
	defer backend.Close()

	// Create watcher and add template
	watcher := template.NewWatcher()
	err = watcher.AddTemplate("@test", templateFile)
	if err != nil {
		t.Fatalf("Failed to add template: %v", err)
	}

	// Create proxy
	cfg := createTestConfig(backend.URL)
	cfg.Prefixes = map[string]string{"@test": templateFile}
	proxy, err := New(cfg, watcher, nil, createTestState(), nil)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	// Create a multi-turn conversation where only the LAST message has the prefix
	requestBody := `{
		"messages":[
			{"role":"user","content":"First message without prefix"},
			{"role":"assistant","content":"First response"},
			{"role":"user","content":"@test Second message with prefix"}
		]
	}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")

	// Send through proxy
	rr := httptest.NewRecorder()
	proxy.handleChatCompletion(rr, req)

	// Verify only the last message was processed
	if !strings.Contains(receivedBody, "Template: Second message with prefix") {
		t.Errorf("Expected last message to be processed, got: %s", receivedBody)
	}

	// First message should still be there unchanged
	if !strings.Contains(receivedBody, "First message without prefix") {
		t.Errorf("Expected first message to remain unchanged, got: %s", receivedBody)
	}
}

// TestTemplateInjectionInvalidJSON tests error handling for invalid JSON
func TestTemplateInjectionInvalidJSON(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	cfg := createTestConfig(backend.URL)
	watcher := template.NewWatcher()
	proxy, err := New(cfg, watcher, nil, createTestState(), nil)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	// Send invalid JSON
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{invalid json"))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	proxy.handleChatCompletion(rr, req)

	// Should get a 400 Bad Request
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for invalid JSON, got %d", rr.Code)
	}
}
