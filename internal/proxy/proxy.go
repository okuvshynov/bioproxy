package proxy

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"

	"github.com/oleksandr/bioproxy/internal/config"
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

	// mu protects concurrent access to the proxy state
	mu sync.Mutex

	// running indicates whether the proxy is currently running
	running bool
}

// New creates a new Proxy instance with the given configuration.
// It parses the backend URL and sets up the reverse proxy.
//
// Returns an error if the backend URL is invalid.
func New(cfg *config.Config) (*Proxy, error) {
	// Parse the backend URL to ensure it's valid
	backend, err := url.Parse(cfg.BackendURL)
	if err != nil {
		return nil, fmt.Errorf("invalid backend URL %s: %w", cfg.BackendURL, err)
	}

	// Create the proxy instance
	p := &Proxy{
		config:  cfg,
		backend: backend,
		running: false,
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
	// but before sending it to the client. We use it for logging.
	p.reverseProxy.ModifyResponse = func(resp *http.Response) error {
		log.Printf("INFO: Backend responded with status %d for %s %s",
			resp.StatusCode,
			resp.Request.Method,
			resp.Request.URL.Path,
		)
		return nil
	}

	// ErrorHandler is called when the backend is unreachable or returns an error
	p.reverseProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("ERROR: Proxy error for %s %s: %v",
			r.Method,
			r.URL.Path,
			err,
		)
		// Return a 502 Bad Gateway when the backend is unavailable
		http.Error(w, "Backend server unavailable", http.StatusBadGateway)
	}

	return p, nil
}

// Start begins listening for HTTP requests on the configured proxy port.
// This is a blocking call that runs until Stop() is called or an error occurs.
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

	// Create the HTTP server
	p.server = &http.Server{
		Addr:    addr,
		Handler: p.reverseProxy,
	}

	p.running = true

	log.Printf("INFO: Starting proxy server on %s, forwarding to %s",
		addr,
		p.backend.String(),
	)

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

	// Shutdown gracefully with no timeout (caller can use context for timeout)
	if err := p.server.Shutdown(nil); err != nil {
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
