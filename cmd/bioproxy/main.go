package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/oleksandr/bioproxy/internal/admission"
	"github.com/oleksandr/bioproxy/internal/admin"
	"github.com/oleksandr/bioproxy/internal/config"
	"github.com/oleksandr/bioproxy/internal/proxy"
	"github.com/oleksandr/bioproxy/internal/state"
	"github.com/oleksandr/bioproxy/internal/template"
	"github.com/oleksandr/bioproxy/internal/warmup"
)

// main is the entry point for the bioproxy server.
// It loads configuration, creates the proxy, and runs it until interrupted.
func main() {
	// Define command-line flags
	// These allow users to override default configuration
	configPath := flag.String("config", config.DefaultConfigPath(), "Path to configuration file")
	proxyHost := flag.String("host", "", "Host to bind proxy server to (use 0.0.0.0 for all interfaces)")
	proxyPort := flag.Int("port", 0, "Port for proxy server to listen on")
	adminHost := flag.String("admin-host", "", "Host to bind admin server to")
	adminPort := flag.Int("admin-port", 0, "Port for admin server to listen on")
	backendURL := flag.String("backend", "", "URL of the llama.cpp backend server")

	// Parse command-line flags
	flag.Parse()

	// Print startup banner
	fmt.Println("🚀 Starting bioproxy - llama.cpp reverse proxy with KV cache warmup")
	fmt.Println()

	// Load configuration from file
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("FATAL: Failed to load config: %v", err)
	}

	// Override with command-line flags if provided
	if *proxyHost != "" {
		cfg.ProxyHost = *proxyHost
	}
	if *proxyPort != 0 {
		cfg.ProxyPort = *proxyPort
	}
	if *adminHost != "" {
		cfg.AdminHost = *adminHost
	}
	if *adminPort != 0 {
		cfg.AdminPort = *adminPort
	}
	if *backendURL != "" {
		cfg.BackendURL = *backendURL
	}

	// Print configuration
	fmt.Println("Configuration:")
	fmt.Printf("  Proxy listening on: http://%s:%d\n", cfg.ProxyHost, cfg.ProxyPort)
	fmt.Printf("  Backend llama.cpp:  %s\n", cfg.BackendURL)
	fmt.Printf("  Admin server:       http://%s:%d\n", cfg.AdminHost, cfg.AdminPort)
	fmt.Printf("  Warmup interval:    %ds\n", cfg.WarmupCheckInterval)
	fmt.Printf("  Templates:          %d configured\n", len(cfg.Prefixes))
	fmt.Println()

	// Create template watcher
	log.Println("INFO: Creating template watcher...")
	watcher := template.NewWatcher()

	// Add templates from config
	for prefix, templatePath := range cfg.Prefixes {
		if err := watcher.AddTemplate(prefix, templatePath); err != nil {
			log.Printf("WARNING: Failed to add template %s: %v", prefix, err)
		}
	}

	// Create shared metrics instance
	// Both proxy, admin server, and warmup manager will use this
	metrics := admin.NewMetrics()

	// Create shared state instance for tracking llama.cpp backend state
	// Both proxy and warmup manager will update this to track which template
	// is currently loaded in the KV cache, allowing us to optimize save/restore
	backendState := state.New()

	// Create shared admission controller for atomic state transitions
	// This prevents race conditions between user requests and warmup operations
	// Both proxy and warmup manager use this to coordinate access to llama.cpp
	log.Println("INFO: Creating admission controller...")
	admissionCtrl := admission.New()

	// Create warmup manager with metrics, state, and admission controller
	log.Println("INFO: Creating warmup manager...")
	warmupMgr := warmup.New(cfg, watcher, cfg.BackendURL, metrics, backendState, admissionCtrl)

	// Create the proxy with template injection support, warmup manager, and admission controller
	// The admission controller ensures atomic state transitions to prevent race conditions
	log.Println("INFO: Creating proxy server...")
	p, err := proxy.New(cfg, watcher, metrics, backendState, admissionCtrl)
	if err != nil {
		log.Fatalf("FATAL: Failed to create proxy: %v", err)
	}

	// Create the admin server
	log.Println("INFO: Creating admin server...")
	adminServer := admin.New(cfg, metrics)

	// Start the proxy
	log.Println("INFO: Starting proxy server...")
	if err := p.Start(); err != nil {
		log.Fatalf("FATAL: Failed to start proxy: %v", err)
	}

	// Start the admin server
	log.Println("INFO: Starting admin server...")
	if err := adminServer.Start(); err != nil {
		log.Fatalf("FATAL: Failed to start admin server: %v", err)
	}

	// Start the warmup manager
	log.Println("INFO: Starting warmup manager...")
	if err := warmupMgr.Start(); err != nil {
		log.Fatalf("FATAL: Failed to start warmup manager: %v", err)
	}

	// Print ready message
	fmt.Println()
	fmt.Println("✅ Servers are running!")
	fmt.Println()
	fmt.Println("Proxy endpoints:")
	fmt.Printf("  curl http://localhost:%d/health\n", cfg.ProxyPort)
	fmt.Printf("  curl http://localhost:%d/v1/models\n", cfg.ProxyPort)
	fmt.Println()
	fmt.Println("Admin endpoints:")
	fmt.Printf("  curl http://localhost:%d/health\n", cfg.AdminPort)
	fmt.Printf("  curl http://localhost:%d/metrics\n", cfg.AdminPort)
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop...")
	fmt.Println()

	// Set up signal handling for graceful shutdown
	// When the user presses Ctrl+C, we want to cleanly stop the proxy
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Wait for interrupt signal
	<-sigChan

	// Shutdown signal received
	fmt.Println()
	log.Println("INFO: Shutdown signal received, stopping servers...")

	// Stop the warmup manager first
	warmupMgr.Stop()

	// Stop the admin server gracefully
	if err := adminServer.Stop(); err != nil {
		log.Printf("ERROR: Error stopping admin server: %v", err)
	}

	// Stop the proxy gracefully
	if err := p.Stop(); err != nil {
		log.Printf("ERROR: Error stopping proxy: %v", err)
		os.Exit(1)
	}

	log.Println("INFO: Servers stopped cleanly")
	fmt.Println("👋 Goodbye!")
}
