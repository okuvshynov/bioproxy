package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProcessTemplateString_Basic tests basic template processing
func TestProcessTemplateString_Basic(t *testing.T) {
	template := "Hello <{message}>, welcome!"
	result, err := ProcessTemplateString(template, "World")

	if err != nil {
		t.Fatalf("ProcessTemplateString failed: %v", err)
	}

	expected := "Hello World, welcome!"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// TestProcessTemplateString_EmptyMessage tests with empty message
func TestProcessTemplateString_EmptyMessage(t *testing.T) {
	template := "Start <{message}> End"
	result, err := ProcessTemplateString(template, "")

	if err != nil {
		t.Fatalf("ProcessTemplateString failed: %v", err)
	}

	expected := "Start  End"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// TestProcessTemplateString_NoPlaceholders tests template without placeholders
func TestProcessTemplateString_NoPlaceholders(t *testing.T) {
	template := "Just plain text"
	result, err := ProcessTemplateString(template, "unused")

	if err != nil {
		t.Fatalf("ProcessTemplateString failed: %v", err)
	}

	if result != template {
		t.Errorf("Expected %q, got %q", template, result)
	}
}

// TestProcessTemplateString_NonRecursive is the CRITICAL test:
// Ensures that patterns in substituted content are NOT processed
func TestProcessTemplateString_NonRecursive(t *testing.T) {
	// User message contains a pattern that should NOT be processed
	template := "Message: <{message}>"
	userMessage := "This has a pattern <{should_not_be_replaced}> in it"

	result, err := ProcessTemplateString(template, userMessage)
	if err != nil {
		t.Fatalf("ProcessTemplateString failed: %v", err)
	}

	// The pattern in the user message should remain as-is
	expected := "Message: This has a pattern <{should_not_be_replaced}> in it"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// TestProcessTemplateString_FileInclusion tests file inclusion
func TestProcessTemplateString_FileInclusion(t *testing.T) {
	// Create a temporary file
	tmpDir := t.TempDir()
	includePath := filepath.Join(tmpDir, "include.txt")
	includeContent := "Content from file"

	if err := os.WriteFile(includePath, []byte(includeContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	template := "Start <{" + includePath + "}> End"
	result, err := ProcessTemplateString(template, "")

	if err != nil {
		t.Fatalf("ProcessTemplateString failed: %v", err)
	}

	expected := "Start Content from file End"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// TestProcessTemplateString_FileWithPattern tests that patterns in included files
// are NOT processed (non-recursive)
func TestProcessTemplateString_FileWithPattern(t *testing.T) {
	tmpDir := t.TempDir()
	includePath := filepath.Join(tmpDir, "include.txt")

	// File contains a pattern that should NOT be processed
	includeContent := "File content with <{pattern}> inside"
	if err := os.WriteFile(includePath, []byte(includeContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	template := "Start <{" + includePath + "}> End"
	result, err := ProcessTemplateString(template, "")

	if err != nil {
		t.Fatalf("ProcessTemplateString failed: %v", err)
	}

	// The pattern in the included file should remain as-is
	expected := "Start File content with <{pattern}> inside End"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// TestProcessTemplateString_MultipleReplacements tests multiple placeholders
func TestProcessTemplateString_MultipleReplacements(t *testing.T) {
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")

	if err := os.WriteFile(file1, []byte("First"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := os.WriteFile(file2, []byte("Second"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	template := "<{" + file1 + "}> and <{" + file2 + "}> and <{message}>"
	result, err := ProcessTemplateString(template, "Third")

	if err != nil {
		t.Fatalf("ProcessTemplateString failed: %v", err)
	}

	expected := "First and Second and Third"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// TestProcessTemplateString_MissingFile tests error handling for missing files
func TestProcessTemplateString_MissingFile(t *testing.T) {
	template := "Start <{/nonexistent/file.txt}> End"
	result, err := ProcessTemplateString(template, "")

	if err != nil {
		t.Fatalf("ProcessTemplateString should not error, got: %v", err)
	}

	// Should contain error marker
	if !strings.Contains(result, "[Error reading") {
		t.Errorf("Expected error marker in result, got: %q", result)
	}
}

// TestWatcher_AddTemplate tests adding a template
func TestWatcher_AddTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "template.txt")

	if err := os.WriteFile(templatePath, []byte("Hello <{message}>"), 0644); err != nil {
		t.Fatalf("Failed to create template: %v", err)
	}

	w := NewWatcher()
	err := w.AddTemplate("@test", templatePath)

	if err != nil {
		t.Fatalf("AddTemplate failed: %v", err)
	}

	// Should need warmup initially
	if !w.NeedsWarmup("@test") {
		t.Error("New template should need warmup")
	}
}

// TestWatcher_CheckForChanges tests change detection
func TestWatcher_CheckForChanges(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "template.txt")
	includePath := filepath.Join(tmpDir, "include.txt")

	// Create initial files
	if err := os.WriteFile(includePath, []byte("Initial"), 0644); err != nil {
		t.Fatalf("Failed to create include file: %v", err)
	}
	templateContent := "Template: <{" + includePath + "}>"
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to create template: %v", err)
	}

	w := NewWatcher()
	if err := w.AddTemplate("@test", templatePath); err != nil {
		t.Fatalf("AddTemplate failed: %v", err)
	}

	// First check - should have changes (initial state)
	changed := w.CheckForChanges()
	if len(changed) != 0 {
		t.Errorf("Expected no changes on first check, got %v", changed)
	}

	// Modify the included file
	if err := os.WriteFile(includePath, []byte("Modified"), 0644); err != nil {
		t.Fatalf("Failed to modify include file: %v", err)
	}

	// Should detect change
	changed = w.CheckForChanges()
	if len(changed) != 1 || changed[0] != "@test" {
		t.Errorf("Expected [@test] to have changed, got %v", changed)
	}

	// Should need warmup
	if !w.NeedsWarmup("@test") {
		t.Error("Template should need warmup after change")
	}

	// Mark as warmed up
	w.MarkWarmedUp("@test")
	if w.NeedsWarmup("@test") {
		t.Error("Template should not need warmup after marking")
	}
}

// TestWatcher_ProcessTemplate tests processing through watcher
func TestWatcher_ProcessTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "template.txt")

	if err := os.WriteFile(templatePath, []byte("Hello <{message}>!"), 0644); err != nil {
		t.Fatalf("Failed to create template: %v", err)
	}

	w := NewWatcher()
	if err := w.AddTemplate("@greet", templatePath); err != nil {
		t.Fatalf("AddTemplate failed: %v", err)
	}

	result, err := w.ProcessTemplate("@greet", "World")
	if err != nil {
		t.Fatalf("ProcessTemplate failed: %v", err)
	}

	expected := "Hello World!"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// TestWatcher_ProcessTemplate_NotFound tests error on missing prefix
func TestWatcher_ProcessTemplate_NotFound(t *testing.T) {
	w := NewWatcher()
	_, err := w.ProcessTemplate("@nonexistent", "test")

	if err == nil {
		t.Error("Expected error for nonexistent template")
	}
}

