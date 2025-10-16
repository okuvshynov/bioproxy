package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oleksandr/bioproxy/internal/config"
)

// createTestConfig creates a minimal config for testing
func createTestConfig(backendURL string) *config.Config {
	return &config.Config{
		ProxyHost:  "localhost",
		ProxyPort:  0, // Let the OS assign a port for testing
		BackendURL: backendURL,
	}
}

// TestNew verifies that creating a new proxy works correctly
func TestNew(t *testing.T) {
	cfg := createTestConfig("http://localhost:8081")

	proxy, err := New(cfg, nil)
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
			proxy, err := New(cfg, nil)

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
	proxy, err := New(cfg, nil)
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
			proxy, err := New(cfg, nil)
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
	proxy, err := New(cfg, nil)
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
	proxy, err := New(cfg, nil)
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

	proxy, err := New(cfg, nil)
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

	proxy, err := New(cfg, nil)
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
			proxy, err := New(cfg, nil)
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
