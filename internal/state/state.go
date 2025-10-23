package state

import (
	"sync"
)

// State tracks the inferred state of the llama.cpp backend.
// This allows us to optimize KV cache operations by only saving/loading
// when we know the state has changed (different template prefix).
//
// The state is "inferred" because we don't directly query llama.cpp -
// instead we track what requests we've sent and assume they succeeded.
// This is sufficient for optimization purposes.
//
// KV Cache Strategy:
// We only perform disk I/O when switching between templates.
// BEFORE sending a query with prefix "new" (when current state is "old"):
//   1. If old != "" AND old != new: save KV cache for "old" (preserving it)
//   2. If new != "" AND old != new: restore KV cache for "new" (loading it)
//   3. Update state to "new"
// AFTER query: nothing (no save needed)
//
// This means:
//   - Repeated queries with same template: no disk I/O
//   - Active template development: warmup runs repeatedly, no saves until switch
//   - Switching templates: save old, restore new (one-time cost)
type State struct {
	// mu protects concurrent access to the state
	mu sync.RWMutex

	// lastPrefix holds the last template prefix used in a request.
	// This can be:
	//   - "" (empty string): Last request had no template prefix
	//   - "code": Last request used @code prefix
	//   - "debug": Last request used @debug prefix
	//   - etc.
	//
	// On first startup, lastPrefix will be "" (zero value).
	lastPrefix string
}

// New creates a new State instance.
// Initial state has empty lastPrefix (equivalent to no template loaded).
func New() *State {
	return &State{
		lastPrefix: "",
	}
}

// GetLastPrefix returns the last prefix used.
// Returns empty string if no request has been sent yet, or if the last
// request had no template prefix.
//
// Thread-safe for concurrent reads.
func (s *State) GetLastPrefix() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastPrefix
}

// UpdatePrefix updates the state after sending a request with the given prefix.
// This should be called after successfully sending a request to llama.cpp,
// regardless of whether it's a warmup request or a user request.
//
// Parameters:
//   - prefix: The template prefix used (empty string for no prefix)
//
// Thread-safe for concurrent writes.
func (s *State) UpdatePrefix(prefix string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastPrefix = prefix
}

// ShouldSave determines if we need to save the OLD KV cache before switching
// to a new prefix.
//
// Returns true if:
//   - old is not empty ("" means nothing to save), AND
//   - old differs from new (we're switching away)
//
// Examples:
//   - old="", new=""      -> false (nothing to save)
//   - old="", new="code"  -> false (nothing to save)
//   - old="code", new="code" -> false (not switching)
//   - old="code", new="debug" -> true (save "code" before switching)
//   - old="code", new="" -> true (save "code" before clearing)
//
// Parameters:
//   - newPrefix: The prefix we want to switch to
//
// Thread-safe for concurrent reads.
func (s *State) ShouldSave(newPrefix string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Only save if old is not empty AND we're switching
	return s.lastPrefix != "" && s.lastPrefix != newPrefix
}

// ShouldRestore determines if we need to restore KV cache before sending
// a request with the given prefix.
//
// Returns true if:
//   - new is not empty ("" means no template to restore), AND
//   - new differs from old (we're switching to it)
//
// Examples:
//   - old="", new=""      -> false (no template to restore)
//   - old="", new="code"  -> true (load "code" template)
//   - old="code", new="code" -> false (already loaded)
//   - old="code", new="debug" -> true (load "debug" template)
//   - old="code", new="" -> false (clearing, nothing to restore)
//
// Parameters:
//   - newPrefix: The prefix we want to use
//
// Thread-safe for concurrent reads.
func (s *State) ShouldRestore(newPrefix string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Only restore if new is not empty AND we're switching
	return newPrefix != "" && s.lastPrefix != newPrefix
}

// Reset resets the state to empty (no template loaded).
// This should be called if we know the llama.cpp backend was restarted
// or the KV cache was cleared externally.
//
// Thread-safe for concurrent writes.
func (s *State) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastPrefix = ""
}
