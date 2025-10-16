package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultConfig verifies that DefaultConfig returns expected defaults
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Verify proxy server settings
	if cfg.ProxyHost != "localhost" {
		t.Errorf("Expected ProxyHost 'localhost', got %q", cfg.ProxyHost)
	}

	if cfg.ProxyPort != 8088 {
		t.Errorf("Expected ProxyPort 8088, got %d", cfg.ProxyPort)
	}

	// Verify admin server settings
	if cfg.AdminHost != "localhost" {
		t.Errorf("Expected AdminHost 'localhost', got %q", cfg.AdminHost)
	}

	if cfg.AdminPort != 8089 {
		t.Errorf("Expected AdminPort 8089, got %d", cfg.AdminPort)
	}

	// Verify backend URL
	if cfg.BackendURL != "http://localhost:8081" {
		t.Errorf("Expected BackendURL 'http://localhost:8081', got %q", cfg.BackendURL)
	}

	// Verify prefixes map
	if cfg.Prefixes == nil {
		t.Error("Prefixes map should be initialized, got nil")
	}

	if len(cfg.Prefixes) != 0 {
		t.Errorf("Prefixes should be empty initially, got %d items", len(cfg.Prefixes))
	}
}

// TestLoadConfigNonexistent tests loading when config file doesn't exist
func TestLoadConfigNonexistent(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path/config.json")
	if err != nil {
		t.Errorf("LoadConfig should not error on nonexistent file, got: %v", err)
	}

	if cfg == nil {
		t.Fatal("LoadConfig should return default config, got nil")
	}

	if len(cfg.Prefixes) != 0 {
		t.Errorf("Default config should have empty prefixes, got %d", len(cfg.Prefixes))
	}
}

// TestLoadConfigValid tests loading a valid config file
func TestLoadConfigValid(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Write test config
	configContent := `{
		"prefixes": {
			"@test": "/tmp/test.txt",
			"@code": "/tmp/code.txt"
		}
	}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	// Load the config
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify prefixes were loaded
	if len(cfg.Prefixes) != 2 {
		t.Errorf("Expected 2 prefixes, got %d", len(cfg.Prefixes))
	}

	if cfg.Prefixes["@test"] != "/tmp/test.txt" {
		t.Errorf("Expected @test -> /tmp/test.txt, got %s", cfg.Prefixes["@test"])
	}

	if cfg.Prefixes["@code"] != "/tmp/code.txt" {
		t.Errorf("Expected @code -> /tmp/code.txt, got %s", cfg.Prefixes["@code"])
	}
}

// TestLoadConfigWithProxySettings tests loading config with all proxy fields
func TestLoadConfigWithProxySettings(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Write config with custom proxy settings
	configContent := `{
		"proxy_host": "0.0.0.0",
		"proxy_port": 9090,
		"admin_host": "127.0.0.1",
		"admin_port": 9091,
		"backend_url": "http://localhost:8080",
		"prefixes": {
			"@custom": "/path/to/template.txt"
		}
	}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	// Load the config
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify all fields were loaded correctly
	if cfg.ProxyHost != "0.0.0.0" {
		t.Errorf("Expected ProxyHost '0.0.0.0', got %q", cfg.ProxyHost)
	}

	if cfg.ProxyPort != 9090 {
		t.Errorf("Expected ProxyPort 9090, got %d", cfg.ProxyPort)
	}

	if cfg.AdminHost != "127.0.0.1" {
		t.Errorf("Expected AdminHost '127.0.0.1', got %q", cfg.AdminHost)
	}

	if cfg.AdminPort != 9091 {
		t.Errorf("Expected AdminPort 9091, got %d", cfg.AdminPort)
	}

	if cfg.BackendURL != "http://localhost:8080" {
		t.Errorf("Expected BackendURL 'http://localhost:8080', got %q", cfg.BackendURL)
	}

	if len(cfg.Prefixes) != 1 {
		t.Errorf("Expected 1 prefix, got %d", len(cfg.Prefixes))
	}

	if cfg.Prefixes["@custom"] != "/path/to/template.txt" {
		t.Errorf("Expected @custom -> /path/to/template.txt, got %s", cfg.Prefixes["@custom"])
	}
}

// TestLoadConfigPartialOverride tests that config overrides only specified fields
func TestLoadConfigPartialOverride(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Write config that only overrides some fields
	configContent := `{
		"proxy_port": 7777,
		"backend_url": "http://remote-llama:8081"
	}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Overridden fields
	if cfg.ProxyPort != 7777 {
		t.Errorf("Expected ProxyPort 7777, got %d", cfg.ProxyPort)
	}

	if cfg.BackendURL != "http://remote-llama:8081" {
		t.Errorf("Expected BackendURL 'http://remote-llama:8081', got %q", cfg.BackendURL)
	}

	// Non-overridden fields should still have defaults
	if cfg.ProxyHost != "localhost" {
		t.Errorf("Expected default ProxyHost 'localhost', got %q", cfg.ProxyHost)
	}

	if cfg.AdminPort != 8089 {
		t.Errorf("Expected default AdminPort 8089, got %d", cfg.AdminPort)
	}
}

// TestLoadConfigInvalidJSON tests loading an invalid JSON file
func TestLoadConfigInvalidJSON(t *testing.T) {
	// Create a temporary invalid config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Write invalid JSON
	if err := os.WriteFile(configPath, []byte("not valid json{"), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	// Attempt to load - should fail
	_, err := LoadConfig(configPath)
	if err == nil {
		t.Error("LoadConfig should fail on invalid JSON")
	}
}

// TestDefaultConfigPath verifies the default config path format
func TestDefaultConfigPath(t *testing.T) {
	path := DefaultConfigPath()

	// Should contain .config/bioproxy/config.json
	if !filepath.IsAbs(path) && path != "config.json" {
		t.Errorf("Path should be absolute or 'config.json', got: %s", path)
	}

	// If absolute, should end with the expected suffix
	if filepath.IsAbs(path) {
		expected := filepath.Join(".config", "bioproxy", "config.json")
		if !filepath.HasPrefix(path, "/") && !filepath.HasPrefix(path, filepath.VolumeName(path)) {
			t.Errorf("Expected absolute path, got: %s", path)
		}
		if !endsWithPath(path, expected) {
			t.Errorf("Path should end with %s, got: %s", expected, path)
		}
	}
}

// endsWithPath checks if a path ends with a given suffix path
func endsWithPath(path, suffix string) bool {
	return filepath.Base(path) == filepath.Base(suffix) &&
		filepath.Base(filepath.Dir(path)) == filepath.Base(filepath.Dir(suffix))
}
