package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config represents the bioproxy configuration
// For now, we only support template prefix mappings
type Config struct {
	// Prefixes maps message prefixes to template file paths
	// When a user message starts with a key, the corresponding template is used
	// Example: {"@code": "/path/to/code_template.txt"}
	Prefixes map[string]string `json:"prefixes"`
}

// DefaultConfig returns a Config with sensible default values
func DefaultConfig() *Config {
	return &Config{
		Prefixes: make(map[string]string),
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
