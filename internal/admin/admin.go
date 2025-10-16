package admin

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/oleksandr/bioproxy/internal/config"
)

// Server represents the admin HTTP server that provides status and metrics endpoints.
// It runs on a separate port from the main proxy to allow separate access control.
type Server struct {
	// config holds the admin server configuration
	config *config.Config

	// server is the HTTP server instance
	server *http.Server

	// startTime records when the server was started, for uptime calculation
	startTime time.Time

	// metrics holds the collected request metrics
	metrics *Metrics

	// mu protects concurrent access to the server state
	mu sync.Mutex

	// running indicates whether the server is currently running
	running bool
}

// Metrics holds statistical data about proxy requests.
// All access to metrics must be synchronized via the mutex.
type Metrics struct {
	// mu protects concurrent access to metrics data
	mu sync.RWMutex

	// RequestCount tracks the total number of requests by endpoint and status code.
	// Structure: RequestCount[endpoint][statusCode] = count
	// Example: RequestCount["/health"]["200"] = 42
	RequestCount map[string]map[string]int64

	// TotalRequests is the total number of all requests processed
	TotalRequests int64

	// StartTime records when metrics collection started
	StartTime time.Time
}

// NewMetrics creates a new Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{
		RequestCount: make(map[string]map[string]int64),
		StartTime:    time.Now(),
	}
}

// RecordRequest increments the request counter for a given endpoint and status code.
// This method is thread-safe and can be called concurrently.
//
// endpoint: The request path (e.g., "/health", "/v1/chat/completions")
// statusCode: The HTTP status code as an integer (e.g., 200, 404, 500)
func (m *Metrics) RecordRequest(endpoint string, statusCode int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Convert status code to string for map key
	statusStr := fmt.Sprintf("%d", statusCode)

	// Initialize the endpoint map if it doesn't exist
	if m.RequestCount[endpoint] == nil {
		m.RequestCount[endpoint] = make(map[string]int64)
	}

	// Increment the counter for this endpoint+status combination
	m.RequestCount[endpoint][statusStr]++

	// Increment total request counter
	m.TotalRequests++
}

// GetSnapshot returns a read-only snapshot of the current metrics.
// This allows safe reading of metrics while they're being updated.
func (m *Metrics) GetSnapshot() map[string]map[string]int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create a deep copy of the metrics
	snapshot := make(map[string]map[string]int64)
	for endpoint, statusMap := range m.RequestCount {
		snapshot[endpoint] = make(map[string]int64)
		for status, count := range statusMap {
			snapshot[endpoint][status] = count
		}
	}

	return snapshot
}

// New creates a new admin server instance with the given configuration.
// The server provides /health and /metrics endpoints.
func New(cfg *config.Config, metrics *Metrics) *Server {
	return &Server{
		config:  cfg,
		metrics: metrics,
		running: false,
	}
}

// Start begins the admin HTTP server on the configured admin port.
// The server provides these endpoints:
//   - GET /health - Health check and uptime information
//   - GET /metrics - Prometheus-style metrics for monitoring
//
// This method is non-blocking and starts the server in a goroutine.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("admin server is already running")
	}

	// Record start time for uptime calculation
	s.startTime = time.Now()

	// Create HTTP mux and register handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", s.handleMetrics)

	// Build the listen address
	addr := fmt.Sprintf("%s:%d", s.config.AdminHost, s.config.AdminPort)

	// Create the HTTP server
	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	s.running = true

	log.Printf("INFO: Starting admin server on %s", addr)

	// Start the server in a goroutine
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("ERROR: Admin server error: %v", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the admin server.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return fmt.Errorf("admin server is not running")
	}

	log.Printf("INFO: Stopping admin server")

	// Shutdown gracefully
	if err := s.server.Shutdown(nil); err != nil {
		return fmt.Errorf("failed to shutdown admin server: %w", err)
	}

	s.running = false
	return nil
}

// IsRunning returns true if the admin server is currently running.
func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// handleHealth responds with health status and uptime information.
// GET /health
//
// Response format:
//
//	{
//	  "status": "ok",
//	  "uptime_seconds": 123.45,
//	  "start_time": "2025-10-15T12:00:00Z"
//	}
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Calculate uptime
	uptime := time.Since(s.startTime).Seconds()

	// Build response
	response := map[string]interface{}{
		"status":         "ok",
		"uptime_seconds": uptime,
		"start_time":     s.startTime.Format(time.RFC3339),
	}

	// Send JSON response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("ERROR: Failed to encode health response: %v", err)
	}
}

// handleMetrics responds with Prometheus-style metrics.
// GET /metrics
//
// Response format (Prometheus text format):
//
//	# HELP bioproxy_requests_total Total number of requests by endpoint and status code
//	# TYPE bioproxy_requests_total counter
//	bioproxy_requests_total{endpoint="/health",status="200"} 42
//	bioproxy_requests_total{endpoint="/v1/chat/completions",status="200"} 15
//	bioproxy_requests_total{endpoint="/v1/chat/completions",status="500"} 2
//
//	# HELP bioproxy_requests_count Total number of all requests
//	# TYPE bioproxy_requests_count counter
//	bioproxy_requests_count 59
//
//	# HELP bioproxy_uptime_seconds Time since server started
//	# TYPE bioproxy_uptime_seconds gauge
//	bioproxy_uptime_seconds 123.45
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get a snapshot of current metrics
	snapshot := s.metrics.GetSnapshot()

	// Calculate uptime
	uptime := time.Since(s.startTime).Seconds()

	// Build Prometheus text format response
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)

	// Write metric: bioproxy_requests_total (by endpoint and status)
	fmt.Fprintf(w, "# HELP bioproxy_requests_total Total number of requests by endpoint and status code\n")
	fmt.Fprintf(w, "# TYPE bioproxy_requests_total counter\n")

	for endpoint, statusMap := range snapshot {
		for status, count := range statusMap {
			// Prometheus format: metric_name{label1="value1",label2="value2"} value
			fmt.Fprintf(w, "bioproxy_requests_total{endpoint=\"%s\",status=\"%s\"} %d\n",
				endpoint, status, count)
		}
	}

	fmt.Fprintf(w, "\n")

	// Write metric: bioproxy_requests_count (total)
	fmt.Fprintf(w, "# HELP bioproxy_requests_count Total number of all requests\n")
	fmt.Fprintf(w, "# TYPE bioproxy_requests_count counter\n")
	fmt.Fprintf(w, "bioproxy_requests_count %d\n", s.metrics.TotalRequests)

	fmt.Fprintf(w, "\n")

	// Write metric: bioproxy_uptime_seconds
	fmt.Fprintf(w, "# HELP bioproxy_uptime_seconds Time since server started in seconds\n")
	fmt.Fprintf(w, "# TYPE bioproxy_uptime_seconds gauge\n")
	fmt.Fprintf(w, "bioproxy_uptime_seconds %.2f\n", uptime)
}
