package admin

import (
	"context"
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

// Metrics holds statistical data about proxy requests and warmup operations.
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

	// Warmup metrics

	// WarmupChecksTotal is the total number of warmup check cycles performed
	WarmupChecksTotal int64

	// WarmupExecutions tracks warmup executions per template prefix
	// Structure: WarmupExecutions[prefix] = count
	WarmupExecutions map[string]int64

	// WarmupErrors tracks errors per template prefix and error type
	// Structure: WarmupErrors[prefix][errorType] = count
	// Error types: "restore_failed", "completion_failed", "save_failed", "template_error"
	WarmupErrors map[string]map[string]int64

	// WarmupDurationTotal tracks total warmup duration per template (in seconds)
	WarmupDurationTotal map[string]float64

	// WarmupDurationCount tracks number of warmup operations (for calculating average)
	WarmupDurationCount map[string]int64

	// KVCacheSaves tracks successful KV cache saves per template
	KVCacheSaves map[string]int64

	// KVCacheRestores tracks KV cache restore attempts per template and status
	// Structure: KVCacheRestores[prefix][status] = count
	// Status values: "success", "not_found", "error"
	KVCacheRestores map[string]map[string]int64

	// WarmupCancellations tracks warmup operations cancelled due to user requests
	// Structure: WarmupCancellations[prefix] = count
	WarmupCancellations map[string]int64
}

// NewMetrics creates a new Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{
		RequestCount:        make(map[string]map[string]int64),
		StartTime:           time.Now(),
		WarmupExecutions:    make(map[string]int64),
		WarmupErrors:        make(map[string]map[string]int64),
		WarmupDurationTotal: make(map[string]float64),
		WarmupDurationCount: make(map[string]int64),
		KVCacheSaves:        make(map[string]int64),
		KVCacheRestores:     make(map[string]map[string]int64),
		WarmupCancellations: make(map[string]int64),
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

// RecordWarmupCheck increments the total warmup check counter.
// This should be called once per warmup check cycle.
func (m *Metrics) RecordWarmupCheck() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WarmupChecksTotal++
}

// RecordWarmupExecution records a warmup execution for a template.
// prefix: The template prefix (e.g., "@code")
// duration: How long the warmup took in seconds
func (m *Metrics) RecordWarmupExecution(prefix string, duration float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.WarmupExecutions[prefix]++
	m.WarmupDurationTotal[prefix] += duration
	m.WarmupDurationCount[prefix]++
}

// RecordWarmupError records a warmup error for a template.
// prefix: The template prefix (e.g., "@code")
// errorType: Type of error ("restore_failed", "completion_failed", "save_failed", "template_error")
func (m *Metrics) RecordWarmupError(prefix string, errorType string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.WarmupErrors[prefix] == nil {
		m.WarmupErrors[prefix] = make(map[string]int64)
	}
	m.WarmupErrors[prefix][errorType]++
}

// RecordKVCacheSave records a successful KV cache save operation.
// prefix: The template prefix (e.g., "@code")
func (m *Metrics) RecordKVCacheSave(prefix string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.KVCacheSaves[prefix]++
}

// RecordKVCacheRestore records a KV cache restore attempt.
// prefix: The template prefix (e.g., "@code")
// status: Status of the restore ("success", "not_found", "error")
func (m *Metrics) RecordKVCacheRestore(prefix string, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.KVCacheRestores[prefix] == nil {
		m.KVCacheRestores[prefix] = make(map[string]int64)
	}
	m.KVCacheRestores[prefix][status]++
}

// RecordWarmupCancellation records a warmup operation that was cancelled
// because a user request arrived and needed priority.
// prefix: The template prefix (e.g., "@code")
func (m *Metrics) RecordWarmupCancellation(prefix string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WarmupCancellations[prefix]++
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
	// Use context.Background() for a clean shutdown without timeout.
	// Future: Could accept context parameter to allow caller to specify timeout.
	if err := s.server.Shutdown(context.Background()); err != nil {
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

	fmt.Fprintf(w, "\n")

	// Write metric: bioproxy_warmup_checks_total
	fmt.Fprintf(w, "# HELP bioproxy_warmup_checks_total Total number of warmup check cycles performed\n")
	fmt.Fprintf(w, "# TYPE bioproxy_warmup_checks_total counter\n")
	fmt.Fprintf(w, "bioproxy_warmup_checks_total %d\n", s.metrics.WarmupChecksTotal)

	fmt.Fprintf(w, "\n")

	// Write metric: bioproxy_warmup_executions_total
	s.metrics.mu.RLock()
	if len(s.metrics.WarmupExecutions) > 0 {
		fmt.Fprintf(w, "# HELP bioproxy_warmup_executions_total Number of warmup executions per template\n")
		fmt.Fprintf(w, "# TYPE bioproxy_warmup_executions_total counter\n")
		for prefix, count := range s.metrics.WarmupExecutions {
			fmt.Fprintf(w, "bioproxy_warmup_executions_total{prefix=\"%s\"} %d\n", prefix, count)
		}
		fmt.Fprintf(w, "\n")
	}

	// Write metric: bioproxy_warmup_errors_total
	if len(s.metrics.WarmupErrors) > 0 {
		fmt.Fprintf(w, "# HELP bioproxy_warmup_errors_total Number of warmup errors by template and error type\n")
		fmt.Fprintf(w, "# TYPE bioproxy_warmup_errors_total counter\n")
		for prefix, errorTypes := range s.metrics.WarmupErrors {
			for errorType, count := range errorTypes {
				fmt.Fprintf(w, "bioproxy_warmup_errors_total{prefix=\"%s\",type=\"%s\"} %d\n", prefix, errorType, count)
			}
		}
		fmt.Fprintf(w, "\n")
	}

	// Write metric: bioproxy_warmup_duration_seconds_total
	if len(s.metrics.WarmupDurationTotal) > 0 {
		fmt.Fprintf(w, "# HELP bioproxy_warmup_duration_seconds_total Total warmup duration in seconds per template\n")
		fmt.Fprintf(w, "# TYPE bioproxy_warmup_duration_seconds_total counter\n")
		for prefix, duration := range s.metrics.WarmupDurationTotal {
			fmt.Fprintf(w, "bioproxy_warmup_duration_seconds_total{prefix=\"%s\"} %.2f\n", prefix, duration)
		}
		fmt.Fprintf(w, "\n")
	}

	// Write metric: bioproxy_warmup_duration_seconds_count
	if len(s.metrics.WarmupDurationCount) > 0 {
		fmt.Fprintf(w, "# HELP bioproxy_warmup_duration_seconds_count Number of warmup duration measurements per template\n")
		fmt.Fprintf(w, "# TYPE bioproxy_warmup_duration_seconds_count counter\n")
		for prefix, count := range s.metrics.WarmupDurationCount {
			fmt.Fprintf(w, "bioproxy_warmup_duration_seconds_count{prefix=\"%s\"} %d\n", prefix, count)
		}
		fmt.Fprintf(w, "\n")
	}

	// Write metric: bioproxy_kv_cache_saves_total
	if len(s.metrics.KVCacheSaves) > 0 {
		fmt.Fprintf(w, "# HELP bioproxy_kv_cache_saves_total Number of successful KV cache saves per template\n")
		fmt.Fprintf(w, "# TYPE bioproxy_kv_cache_saves_total counter\n")
		for prefix, count := range s.metrics.KVCacheSaves {
			fmt.Fprintf(w, "bioproxy_kv_cache_saves_total{prefix=\"%s\"} %d\n", prefix, count)
		}
		fmt.Fprintf(w, "\n")
	}

	// Write metric: bioproxy_kv_cache_restores_total
	if len(s.metrics.KVCacheRestores) > 0 {
		fmt.Fprintf(w, "# HELP bioproxy_kv_cache_restores_total Number of KV cache restore attempts per template and status\n")
		fmt.Fprintf(w, "# TYPE bioproxy_kv_cache_restores_total counter\n")
		for prefix, statuses := range s.metrics.KVCacheRestores {
			for status, count := range statuses {
				fmt.Fprintf(w, "bioproxy_kv_cache_restores_total{prefix=\"%s\",status=\"%s\"} %d\n", prefix, status, count)
			}
		}
		fmt.Fprintf(w, "\n")
	}

	// Write metric: bioproxy_warmup_cancellations_total
	if len(s.metrics.WarmupCancellations) > 0 {
		fmt.Fprintf(w, "# HELP bioproxy_warmup_cancellations_total Number of warmup operations cancelled due to user requests\n")
		fmt.Fprintf(w, "# TYPE bioproxy_warmup_cancellations_total counter\n")
		for prefix, count := range s.metrics.WarmupCancellations {
			fmt.Fprintf(w, "bioproxy_warmup_cancellations_total{prefix=\"%s\"} %d\n", prefix, count)
		}
		fmt.Fprintf(w, "\n")
	}
	s.metrics.mu.RUnlock()
}
