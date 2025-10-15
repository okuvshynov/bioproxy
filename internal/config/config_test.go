package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultConfig verifies that DefaultConfig returns expected defaults
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

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
