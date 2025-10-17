package template

import (
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
)

// messagePlaceholder is the keyword for user message in templates: <{message}>
const messagePlaceholder = "message"

// TemplateState represents the state of a single template
type TemplateState struct {
	// Prefix is the message prefix that triggers this template (e.g., "@code")
	Prefix string

	// TemplatePath is the path to the template file
	TemplatePath string

	// ProcessedHash is the SHA256 hash of the processed template (with empty message)
	// We hash the fully processed template rather than individual files
	ProcessedHash string

	// NeedsWarmup indicates whether the template has changed and needs warmup
	NeedsWarmup bool
}

// Watcher monitors templates for changes
type Watcher struct {
	mu sync.RWMutex

	// templates maps prefix to template state
	templates map[string]*TemplateState
}

// NewWatcher creates a new template watcher
func NewWatcher() *Watcher {
	return &Watcher{
		templates: make(map[string]*TemplateState),
	}
}

// AddTemplate adds a new template to watch
// prefix: the message prefix (e.g., "@code")
// templatePath: path to the template file
func (w *Watcher) AddTemplate(prefix, templatePath string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Process template with empty message to get initial hash
	processed, err := processTemplateFile(templatePath, "")
	if err != nil {
		log.Printf("ERROR: Failed to add template %s from %s: %v", prefix, templatePath, err)
		return fmt.Errorf("failed to process template %s: %w", prefix, err)
	}

	// Create initial state
	state := &TemplateState{
		Prefix:        prefix,
		TemplatePath:  templatePath,
		ProcessedHash: hashString(processed),
		NeedsWarmup:   true, // Initially needs warmup
	}

	w.templates[prefix] = state
	log.Printf("Added template %s from %s (needs warmup)", prefix, templatePath)
	return nil
}

// CheckForChanges checks all templates for changes
// Returns a slice of prefixes that have changed and need warmup
func (w *Watcher) CheckForChanges() []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	var changed []string

	for prefix, state := range w.templates {
		// Check if already marked as needing warmup (e.g., newly added)
		if state.NeedsWarmup {
			changed = append(changed, prefix)
			continue
		}

		// Process template with empty message
		processed, err := processTemplateFile(state.TemplatePath, "")
		if err != nil {
			// If we can't process template, skip it but log the error
			log.Printf("WARNING: Failed to check template %s: %v", prefix, err)
			continue
		}

		// Calculate new hash
		newHash := hashString(processed)

		// Check if hash changed
		if newHash != state.ProcessedHash {
			state.NeedsWarmup = true
			state.ProcessedHash = newHash
			changed = append(changed, prefix)
			log.Printf("Template %s changed, needs warmup", prefix)
		}
	}

	return changed
}

// MarkWarmedUp marks a template as having completed warmup
func (w *Watcher) MarkWarmedUp(prefix string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if state, exists := w.templates[prefix]; exists {
		state.NeedsWarmup = false
	}
}

// NeedsWarmup checks if a specific template needs warmup
func (w *Watcher) NeedsWarmup(prefix string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if state, exists := w.templates[prefix]; exists {
		return state.NeedsWarmup
	}
	return false
}

// ProcessTemplate processes a template by replacing placeholders with actual content
// IMPORTANT: Patterns are ONLY detected and replaced in the original template,
// not in substituted content. This prevents recursive replacement.
// - <{message}> → replaced with userMessage
// - <{filepath}> → replaced with content of the file
func (w *Watcher) ProcessTemplate(prefix, userMessage string) (string, error) {
	w.mu.RLock()
	state, exists := w.templates[prefix]
	w.mu.RUnlock()

	if !exists {
		log.Printf("ERROR: Template not found for prefix %s", prefix)
		return "", fmt.Errorf("template for prefix %s not found", prefix)
	}

	result, err := processTemplateFile(state.TemplatePath, userMessage)
	if err != nil {
		log.Printf("ERROR: Failed to process template %s: %v", prefix, err)
		return "", err
	}

	return result, nil
}

// processTemplateFile reads and processes a template file
func processTemplateFile(templatePath, userMessage string) (string, error) {
	// Read template file
	templateContent, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to read template: %w", err)
	}

	return ProcessTemplateString(string(templateContent), userMessage)
}

// ProcessTemplateString replaces all <{...}> placeholders with appropriate content
// This is a public function so it can be used independently and tested easily
// CRITICAL: Since regex only matches against the original template string,
// replacements are NOT recursive. Any <{...}> patterns in the substituted
// content (from files or user messages) will NOT be processed.
func ProcessTemplateString(template string, userMessage string) (string, error) {
	// Match <{...}> pattern
	// This regex will only find matches in the original template string
	re := regexp.MustCompile(`<\{([^}]+)\}>`)

	// Replace all matches using callback function
	// The key insight: ReplaceAllStringFunc operates on the original string,
	// so it won't see any patterns that appear in the replacement text
	result := re.ReplaceAllStringFunc(template, func(match string) string {
		// Extract content between <{ and }>
		// match format: "<{something}"
		placeholder := strings.TrimSpace(match[2 : len(match)-2])

		if placeholder == messagePlaceholder {
			// Replace with user message
			return userMessage
		}

		// Treat as file path
		content, err := os.ReadFile(placeholder)
		if err != nil {
			// Log the error and return error marker in output
			// Note: This error marker itself won't be processed even if it
			// contains <{...}> patterns, because we're already in the replacement
			log.Printf("WARNING: Failed to read included file %s: %v", placeholder, err)
			return fmt.Sprintf("[Error reading %s: %v]", placeholder, err)
		}

		return string(content)
	})

	return result, nil
}

// hashString calculates SHA256 hash of a string
func hashString(s string) string {
	hash := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", hash)
}
