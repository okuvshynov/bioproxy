package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/oleksandr/bioproxy/internal/config"
	"github.com/oleksandr/bioproxy/internal/proxy"
)

// main is the entry point for the bioproxy server.
// It loads configuration, creates the proxy, and runs it until interrupted.
func main() {
	// Define command-line flags
	// These allow users to override default configuration
	proxyHost := flag.String("host", "localhost", "Host to bind proxy server to (use 0.0.0.0 for all interfaces)")
	proxyPort := flag.Int("port", 8088, "Port for proxy server to listen on")
	adminHost := flag.String("admin-host", "localhost", "Host to bind admin server to")
	adminPort := flag.Int("admin-port", 8089, "Port for admin server to listen on")
	backendURL := flag.String("backend", "http://localhost:8081", "URL of the llama.cpp backend server")

	// Parse command-line flags
	flag.Parse()

	// Print startup banner
	fmt.Println("🚀 Starting bioproxy - llama.cpp reverse proxy with KV cache warmup")
	fmt.Println()

	// Load configuration
	// Command-line flags override default values
	// In the future, we can also read from a config file
	cfg := &config.Config{
		ProxyHost:  *proxyHost,
		ProxyPort:  *proxyPort,
		AdminHost:  *adminHost,
		AdminPort:  *adminPort,
		BackendURL: *backendURL,
	}

	// Print configuration
	fmt.Println("Configuration:")
	fmt.Printf("  Proxy listening on: http://%s:%d\n", cfg.ProxyHost, cfg.ProxyPort)
	fmt.Printf("  Backend llama.cpp:  %s\n", cfg.BackendURL)
	fmt.Printf("  Admin (future):     http://%s:%d\n", cfg.AdminHost, cfg.AdminPort)
	fmt.Println()

	// Create the proxy
	log.Println("INFO: Creating proxy server...")
	p, err := proxy.New(cfg)
	if err != nil {
		log.Fatalf("FATAL: Failed to create proxy: %v", err)
	}

	// Start the proxy in a goroutine
	// This is non-blocking, so we can handle signals below
	log.Println("INFO: Starting proxy server...")
	if err := p.Start(); err != nil {
		log.Fatalf("FATAL: Failed to start proxy: %v", err)
	}

	// Print ready message
	fmt.Println()
	fmt.Println("✅ Proxy server is running!")
	fmt.Println()
	fmt.Println("Try these commands:")
	fmt.Printf("  curl http://localhost:%d/health\n", cfg.ProxyPort)
	fmt.Printf("  curl http://localhost:%d/v1/models\n", cfg.ProxyPort)
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
	log.Println("INFO: Shutdown signal received, stopping proxy...")

	// Stop the proxy gracefully
	if err := p.Stop(); err != nil {
		log.Printf("ERROR: Error stopping proxy: %v", err)
		os.Exit(1)
	}

	log.Println("INFO: Proxy stopped cleanly")
	fmt.Println("👋 Goodbye!")
}
