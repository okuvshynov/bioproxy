package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/oleksandr/bioproxy/internal/admin"
	"github.com/oleksandr/bioproxy/internal/config"
	"github.com/oleksandr/bioproxy/internal/state"
	"github.com/oleksandr/bioproxy/internal/template"
)

// Proxy represents the reverse proxy server that forwards requests to llama.cpp.
// It acts as a middleware between clients and the backend llama.cpp server,
// allowing us to intercept, modify, and monitor requests/responses.
type Proxy struct {
	// config holds the proxy configuration including ports and backend URL
	config *config.Config

	// backend is the parsed URL of the llama.cpp server
	backend *url.URL

	// reverseProxy is the stdlib reverse proxy that handles the actual forwarding
	reverseProxy *httputil.ReverseProxy

	// server is the HTTP server instance
	server *http.Server

	// watcher monitors templates and processes them for injection
	watcher *template.Watcher

	// metrics holds request statistics (can be nil if metrics not enabled)
	metrics *admin.Metrics

	// backendState tracks the inferred state of the llama.cpp backend
	// (which template prefix is currently loaded in KV cache)
	backendState *state.State

	// warmupManager allows the proxy to cancel warmup operations when
	// user requests arrive (can be nil if warmup not enabled)
	warmupManager WarmupManager

	// mu protects concurrent access to the proxy state
	mu sync.Mutex

	// running indicates whether the proxy is currently running
	running bool
}

// WarmupManager interface defines the methods the proxy needs from the warmup manager
// This allows the proxy to cancel warmup operations when user requests arrive
type WarmupManager interface {
	CancelWarmup() bool
	IsWarmupInProgress() bool
	GetWarmupPrefix() string
}

// New creates a new Proxy instance with the given configuration.
// It parses the backend URL and sets up the reverse proxy with template injection support.
//
// Parameters:
//   - cfg: Proxy configuration including backend URL and template mappings
//   - watcher: Template watcher for processing template injections (required)
//   - metrics: Optional metrics collector (pass nil to disable)
//   - backendState: Shared state tracker for llama.cpp backend (required)
//   - warmupMgr: Optional warmup manager for cancelling warmup on user requests (pass nil to disable)
//
// Returns an error if the backend URL is invalid.
func New(cfg *config.Config, watcher *template.Watcher, metrics *admin.Metrics, backendState *state.State, warmupMgr WarmupManager) (*Proxy, error) {
	// Parse the backend URL to ensure it's valid
	backend, err := url.Parse(cfg.BackendURL)
	if err != nil {
		return nil, fmt.Errorf("invalid backend URL %s: %w", cfg.BackendURL, err)
	}

	// Create the proxy instance
	p := &Proxy{
		config:        cfg,
		backend:       backend,
		watcher:       watcher,
		metrics:       metrics,
		backendState:  backendState,
		warmupManager: warmupMgr,
		running:       false,
	}

	// Create the reverse proxy using stdlib's httputil.ReverseProxy.
	// This handles all the complexity of forwarding requests, copying headers,
	// managing connections, etc.
	p.reverseProxy = httputil.NewSingleHostReverseProxy(backend)

	// Customize the Director function to add logging and prepare the request.
	// Director is called before each request is sent to the backend.
	originalDirector := p.reverseProxy.Director
	p.reverseProxy.Director = func(req *http.Request) {
		// Call the original director to set up the request properly
		originalDirector(req)

		// Log the incoming request for debugging and monitoring
		log.Printf("INFO: Proxying %s %s -> %s%s",
			req.Method,
			req.URL.Path,
			p.backend.String(),
			req.URL.Path,
		)
	}

	// ModifyResponse is called after receiving a response from the backend
	// but before sending it to the client. We use it for logging and metrics.
	//
	// CRITICAL STREAMING REQUIREMENT:
	// Do NOT read resp.Body in this function! Reading the body would buffer the
	// entire response in memory, breaking Server-Sent Events (SSE) streaming.
	// llama.cpp uses SSE to send tokens in real-time as they're generated.
	//
	// Safe to access:
	//   - resp.StatusCode    (status code from backend)
	//   - resp.Header        (response headers)
	//   - resp.Request       (original request)
	//
	// NEVER access:
	//   - resp.Body.Read()          (breaks streaming)
	//   - io.ReadAll(resp.Body)     (breaks streaming)
	//   - Any operation that reads the body
	//
	// The httputil.ReverseProxy automatically handles streaming by:
	// 1. Forwarding chunks as they arrive from backend
	// 2. Calling Flush() after each chunk if ResponseWriter supports it
	// 3. Preserving Content-Type: text/event-stream headers
	//
	// Validation: TestManualStreamingChat verifies SSE streaming works correctly.
	p.reverseProxy.ModifyResponse = func(resp *http.Response) error {
		log.Printf("INFO: Backend responded with status %d for %s %s",
			resp.StatusCode,
			resp.Request.Method,
			resp.Request.URL.Path,
		)

		// Record metrics if enabled
		if p.metrics != nil {
			p.metrics.RecordRequest(resp.Request.URL.Path, resp.StatusCode)
		}

		return nil
	}

	// ErrorHandler is called when the backend is unreachable or returns an error
	p.reverseProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("ERROR: Proxy error for %s %s: %v",
			r.Method,
			r.URL.Path,
			err,
		)

		// Record error metric if enabled
		if p.metrics != nil {
			p.metrics.RecordRequest(r.URL.Path, http.StatusBadGateway)
		}

		// Return a 502 Bad Gateway when the backend is unavailable
		http.Error(w, "Backend server unavailable", http.StatusBadGateway)
	}

	return p, nil
}

// Start begins listening for HTTP requests on the configured proxy port.
// This is a blocking call that runs until Stop() is called or an error occurs.
//
// The proxy uses custom routing:
//   - POST /v1/chat/completions -> handleChatCompletion (with template injection)
//   - All other requests -> reverseProxy (direct passthrough)
//
// Returns an error if the server fails to start or if already running.
func (p *Proxy) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Prevent starting the proxy multiple times
	if p.running {
		return fmt.Errorf("proxy is already running")
	}

	// Build the listen address from config
	addr := fmt.Sprintf("%s:%d", p.config.ProxyHost, p.config.ProxyPort)

	// Create a custom ServeMux for routing
	// This allows us to intercept specific endpoints while passing through others
	mux := http.NewServeMux()

	// Route chat completion requests to our custom handler for template injection
	mux.HandleFunc("/v1/chat/completions", p.handleChatCompletion)

	// Route all other requests to the reverse proxy for direct passthrough
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Only use reverse proxy for non-chat-completion requests
		if r.URL.Path != "/v1/chat/completions" {
			p.reverseProxy.ServeHTTP(w, r)
		}
	})

	// Create the HTTP server with our custom mux
	p.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	p.running = true

	log.Printf("INFO: Starting proxy server on %s, forwarding to %s",
		addr,
		p.backend.String(),
	)
	log.Printf("INFO: Template injection enabled for /v1/chat/completions")

	// Start the server in a goroutine so we can handle shutdown gracefully
	go func() {
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("ERROR: Proxy server error: %v", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the proxy server.
// It waits for active connections to complete.
//
// Returns an error if the server fails to shut down or if not running.
func (p *Proxy) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return fmt.Errorf("proxy is not running")
	}

	log.Printf("INFO: Stopping proxy server")

	// Shutdown gracefully
	// Use context.Background() for a clean shutdown without timeout.
	// Future: Could accept context parameter to allow caller to specify timeout.
	if err := p.server.Shutdown(context.Background()); err != nil {
		return fmt.Errorf("failed to shutdown proxy server: %w", err)
	}

	p.running = false
	return nil
}

// IsRunning returns true if the proxy is currently running.
func (p *Proxy) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// handleChatCompletion is a custom handler for /v1/chat/completions that performs
// template injection when a user message starts with a configured prefix.
//
// Flow:
//  1. Read and parse the incoming request body as JSON (using map[string]interface{})
//  2. Find the last user message in the messages array
//  3. Check if it starts with a configured template prefix (e.g., "@code ")
//  4. If yes:
//     - Extract message without prefix
//     - Process template using watcher.ProcessTemplate()
//     - Replace message content with processed template
//  5. Marshal the (possibly modified) request back to JSON
//  6. Forward to llama.cpp backend
//  7. Stream response back to client (preserving SSE streaming)
//
// IMPORTANT: Uses map[string]interface{} to preserve ALL request fields including
// stream, temperature, max_tokens, etc. This ensures streaming continues to work
// after template injection.
//
// Template injection only affects request; responses stream through unchanged.
func (p *Proxy) handleChatCompletion(w http.ResponseWriter, r *http.Request) {
	// PRIORITY HANDLING: User requests take priority over warmup operations
	// If a warmup is currently in progress, cancel it to ensure the user request
	// gets processed immediately without waiting
	if p.warmupManager != nil && p.warmupManager.IsWarmupInProgress() {
		warmupPrefix := p.warmupManager.GetWarmupPrefix()
		if p.warmupManager.CancelWarmup() {
			log.Printf("INFO: Cancelled warmup for %s to prioritize user request", warmupPrefix)
		}
	}

	// Read the entire request body
	// This is safe because chat completion requests are typically small (< 100KB)
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("ERROR: Failed to read request body: %v", err)
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}
	r.Body.Close()

	// Parse the chat completion request as a generic map to preserve ALL fields
	// This is critical - we must preserve stream, temperature, max_tokens, etc.
	var requestMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &requestMap); err != nil {
		log.Printf("ERROR: Failed to parse chat completion request: %v", err)
		http.Error(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	// Extract the messages array from the map
	messagesInterface, hasMessages := requestMap["messages"]
	if !hasMessages {
		log.Printf("ERROR: Request has no messages field")
		http.Error(w, "Request must include messages", http.StatusBadRequest)
		return
	}

	// Convert to array of message maps
	messagesArray, ok := messagesInterface.([]interface{})
	if !ok {
		log.Printf("ERROR: Messages field is not an array")
		http.Error(w, "Messages must be an array", http.StatusBadRequest)
		return
	}

	// Find the last user message for template injection
	// We check the last one because in multi-turn conversations,
	// only the most recent user input should trigger template selection
	lastUserIndex := -1
	for i := len(messagesArray) - 1; i >= 0; i-- {
		messageMap, ok := messagesArray[i].(map[string]interface{})
		if !ok {
			continue
		}
		if role, ok := messageMap["role"].(string); ok && role == "user" {
			lastUserIndex = i
			break
		}
	}

	// Track which prefix is used for this request (empty string if none)
	requestPrefix := ""

	// If there's a user message, check for template prefix
	if lastUserIndex >= 0 {
		messageMap := messagesArray[lastUserIndex].(map[string]interface{})
		userMessage, ok := messageMap["content"].(string)
		if !ok {
			log.Printf("ERROR: User message content is not a string")
			http.Error(w, "Message content must be a string", http.StatusBadRequest)
			return
		}

		// Check each configured prefix to see if the message starts with it
		for prefix := range p.config.Prefixes {
			// Check if message starts with the prefix followed by a space
			// Example: "@code how do I..." matches prefix "@code"
			prefixWithSpace := prefix + " "
			if strings.HasPrefix(userMessage, prefixWithSpace) {
				// Extract the actual message without the prefix
				messageWithoutPrefix := strings.TrimPrefix(userMessage, prefixWithSpace)

				log.Printf("INFO: Detected template prefix %s, processing template", prefix)

				// Process the template with the user's message
				processedTemplate, err := p.watcher.ProcessTemplate(prefix, messageWithoutPrefix)
				if err != nil {
					log.Printf("ERROR: Failed to process template %s: %v", prefix, err)
					http.Error(w, fmt.Sprintf("Template processing failed: %v", err), http.StatusInternalServerError)
					return
				}

				// Replace the message content with the processed template
				messageMap["content"] = processedTemplate
				requestPrefix = prefix // Track that we're using this prefix

				log.Printf("INFO: Template %s processed successfully (%d bytes)", prefix, len(processedTemplate))
				break // Only process the first matching prefix
			}
		}
	}

	// BEFORE sending the request to llama.cpp:
	// Perform KV cache save/restore operations based on state transitions

	// Step 1: Save old KV cache if we're switching away from a different template
	if p.backendState.ShouldSave(requestPrefix) {
		oldPrefix := p.backendState.GetLastPrefix()
		oldFilename := strings.TrimPrefix(oldPrefix, "@") + ".bin"
		log.Printf("Saving KV cache for %s before switching to %s", oldPrefix, requestPrefix)
		if err := p.saveKVCache(oldPrefix, oldFilename); err != nil {
			log.Printf("WARNING: Failed to save KV cache for %s: %v", oldPrefix, err)
			// Don't fail the request - continue
		}
	}

	// Step 2: Restore new KV cache if we're switching to a different template
	if p.backendState.ShouldRestore(requestPrefix) {
		cacheFilename := strings.TrimPrefix(requestPrefix, "@") + ".bin"
		log.Printf("Restoring KV cache for %s", requestPrefix)
		if err := p.restoreKVCache(requestPrefix, cacheFilename); err != nil {
			log.Printf("WARNING: Failed to restore KV cache for %s: %v", requestPrefix, err)
			// Don't fail the request - llama.cpp can handle it without cache
		}
	} else if requestPrefix != "" {
		log.Printf("Skipping KV cache restore for %s (already loaded)", requestPrefix)
	}

	// Marshal the (possibly modified) request back to JSON
	// This preserves ALL original fields including stream, temperature, max_tokens, etc.
	modifiedBody, err := json.Marshal(requestMap)
	if err != nil {
		log.Printf("ERROR: Failed to marshal modified request: %v", err)
		http.Error(w, "Failed to prepare request", http.StatusInternalServerError)
		return
	}

	// Create a new request to forward to llama.cpp
	// Clone the original request but with our modified body
	backendURL := *p.backend
	backendURL.Path = r.URL.Path
	backendURL.RawQuery = r.URL.RawQuery

	proxyReq, err := http.NewRequest(r.Method, backendURL.String(), bytes.NewReader(modifiedBody))
	if err != nil {
		log.Printf("ERROR: Failed to create backend request: %v", err)
		http.Error(w, "Failed to forward request", http.StatusInternalServerError)
		return
	}

	// Copy headers from original request
	proxyReq.Header = r.Header.Clone()
	// Update Content-Length since body might have changed
	proxyReq.ContentLength = int64(len(modifiedBody))

	log.Printf("INFO: Forwarding chat completion request to %s", backendURL.String())

	// Forward the request to llama.cpp and stream response back
	// We use the default HTTP client which supports streaming
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		log.Printf("ERROR: Backend request failed: %v", err)
		if p.metrics != nil {
			p.metrics.RecordRequest(r.URL.Path, http.StatusBadGateway)
		}
		http.Error(w, "Backend server unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	log.Printf("INFO: Backend responded with status %d", resp.StatusCode)

	// Update state to reflect that this prefix is now loaded
	// We do this AFTER the request succeeds, but BEFORE streaming the response
	// We do NOT save the KV cache here - we only save when switching away
	p.backendState.UpdatePrefix(requestPrefix)

	// Record metrics
	if p.metrics != nil {
		p.metrics.RecordRequest(r.URL.Path, resp.StatusCode)
	}

	// Copy response headers to client
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Set status code
	w.WriteHeader(resp.StatusCode)

	// Stream the response body back to the client
	// This supports both regular responses and Server-Sent Events (SSE) streaming.
	// For SSE, each chunk is flushed immediately as it arrives from llama.cpp.
	if flusher, ok := w.(http.Flusher); ok {
		// ResponseWriter supports flushing - enable streaming
		buf := make([]byte, 32*1024) // 32KB buffer
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				if _, writeErr := w.Write(buf[:n]); writeErr != nil {
					log.Printf("ERROR: Failed to write response: %v", writeErr)
					return
				}
				flusher.Flush() // Immediately send data to client
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Printf("ERROR: Failed to read backend response: %v", err)
				return
			}
		}
	} else {
		// Fallback: copy entire response at once (no streaming)
		// This should rarely happen as most ResponseWriters support flushing
		io.Copy(w, resp.Body)
	}
}

// restoreKVCache restores KV cache from file via llama.cpp API
func (p *Proxy) restoreKVCache(prefix, filename string) error {
	url := fmt.Sprintf("%s/slots/0?action=restore", p.backend.String())

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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for logging
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("cache file not found (404)")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("KV cache restored for %s", filename)
	return nil
}

// saveKVCache saves KV cache to file via llama.cpp API
func (p *Proxy) saveKVCache(prefix, filename string) error {
	url := fmt.Sprintf("%s/slots/0?action=save", p.backend.String())

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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for logging
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("KV cache saved for %s", filename)
	return nil
}
