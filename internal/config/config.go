package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config represents the bioproxy configuration
type Config struct {
	// ProxyHost is the host/IP address for the user-facing proxy server
	// Use "localhost" or "127.0.0.1" for local-only access
	// Use "0.0.0.0" to listen on all interfaces
	// Default: "localhost"
	ProxyHost string `json:"proxy_host"`

	// ProxyPort is the port on which the user-facing proxy server listens
	// This is the port clients connect to for OpenAI-compatible API
	// Default: 8088
	ProxyPort int `json:"proxy_port"`

	// AdminHost is the host/IP address for the admin server
	// Use "localhost" or "127.0.0.1" for local-only access
	// Use "0.0.0.0" to listen on all interfaces
	// Default: "localhost"
	AdminHost string `json:"admin_host"`

	// AdminPort is the port for proxy administration endpoints
	// Provides /status, /config, and proxy-specific /metrics
	// Default: 8089
	AdminPort int `json:"admin_port"`

	// BackendURL is the URL of the llama.cpp server to proxy to
	// Default: http://localhost:8081
	BackendURL string `json:"backend_url"`

	// Prefixes maps message prefixes to template file paths
	// When a user message starts with a key, the corresponding template is used
	// Example: {"@code": "/path/to/code_template.txt"}
	Prefixes map[string]string `json:"prefixes"`
}

// DefaultConfig returns a Config with sensible default values
func DefaultConfig() *Config {
	return &Config{
		ProxyHost:  "localhost",
		ProxyPort:  8088,
		AdminHost:  "localhost",
		AdminPort:  8089,
		BackendURL: "http://localhost:8081",
		Prefixes:   make(map[string]string),
	}
}

// LoadConfig loads configuration from a JSON file
// It starts with default values and overrides them with values from the file
func LoadConfig(configPath string) (*Config, error) {
	// Start with defaults
	cfg := DefaultConfig()

	// If config file doesn't exist, return defaults
	// This allows running without a config file
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return cfg, nil
	}

	// Read the config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse JSON and override defaults
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config JSON: %w", err)
	}

	return cfg, nil
}

// DefaultConfigPath returns the default configuration file path
// Usually ~/.config/bioproxy/config.json
func DefaultConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(homeDir, ".config", "bioproxy", "config.json")
}
