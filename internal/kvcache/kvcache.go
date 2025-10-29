// Package kvcache provides utilities for interacting with llama.cpp KV cache API.
// It handles saving and restoring KV cache state for template prefixes.
package kvcache

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/oleksandr/bioproxy/internal/admin"
)

// Client handles KV cache operations with llama.cpp backend.
type Client struct {
	backendURL string
	httpClient *http.Client
	metrics    *admin.Metrics
}

// New creates a new KV cache client.
// Parameters:
//   - backendURL: llama.cpp server URL (e.g., "http://localhost:8081")
//   - httpClient: HTTP client to use for requests
//   - metrics: Optional metrics collector (can be nil)
func New(backendURL string, httpClient *http.Client, metrics *admin.Metrics) *Client {
	return &Client{
		backendURL: backendURL,
		httpClient: httpClient,
		metrics:    metrics,
	}
}

// Restore restores KV cache from file via llama.cpp API.
// Parameters:
//   - prefix: Template prefix for metrics tracking (e.g., "@code")
//   - filename: Cache filename to restore (e.g., "code.bin")
//
// Returns:
//   - nil on success
//   - Error with 404 status if cache file doesn't exist
//   - Error on other failures
func (c *Client) Restore(prefix, filename string) error {
	url := fmt.Sprintf("%s/slots/0?action=restore", c.backendURL)

	reqBody := map[string]string{
		"filename": filename,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		if c.metrics != nil {
			c.metrics.RecordKVCacheRestore(prefix, "error")
		}
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		if c.metrics != nil {
			c.metrics.RecordKVCacheRestore(prefix, "error")
		}
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if c.metrics != nil {
			c.metrics.RecordKVCacheRestore(prefix, "error")
		}
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for logging
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		if c.metrics != nil {
			c.metrics.RecordKVCacheRestore(prefix, "not_found")
		}
		return fmt.Errorf("cache file not found (404)")
	}

	if resp.StatusCode != http.StatusOK {
		if c.metrics != nil {
			c.metrics.RecordKVCacheRestore(prefix, "error")
		}
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	if c.metrics != nil {
		c.metrics.RecordKVCacheRestore(prefix, "success")
	}
	log.Printf("KV cache restored for %s", filename)
	return nil
}

// Save saves KV cache to file via llama.cpp API.
// Parameters:
//   - prefix: Template prefix for metrics tracking (e.g., "@code")
//   - filename: Cache filename to save (e.g., "code.bin")
//
// Returns:
//   - nil on success
//   - Error on failure
func (c *Client) Save(prefix, filename string) error {
	url := fmt.Sprintf("%s/slots/0?action=save", c.backendURL)

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

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for logging
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	if c.metrics != nil {
		c.metrics.RecordKVCacheSave(prefix)
	}
	log.Printf("KV cache saved for %s", filename)
	return nil
}
