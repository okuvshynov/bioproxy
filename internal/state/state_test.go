package state

import (
	"sync"
	"testing"
)

func TestNew(t *testing.T) {
	s := New()
	if s.GetLastPrefix() != "" {
		t.Errorf("New state should have empty lastPrefix, got %q", s.GetLastPrefix())
	}
}

func TestUpdatePrefix(t *testing.T) {
	s := New()

	s.UpdatePrefix("code")
	if got := s.GetLastPrefix(); got != "code" {
		t.Errorf("After UpdatePrefix(\"code\"), got %q, want \"code\"", got)
	}

	s.UpdatePrefix("debug")
	if got := s.GetLastPrefix(); got != "debug" {
		t.Errorf("After UpdatePrefix(\"debug\"), got %q, want \"debug\"", got)
	}

	s.UpdatePrefix("")
	if got := s.GetLastPrefix(); got != "" {
		t.Errorf("After UpdatePrefix(\"\"), got %q, want \"\"", got)
	}
}

func TestReset(t *testing.T) {
	s := New()
	s.UpdatePrefix("code")

	s.Reset()
	if got := s.GetLastPrefix(); got != "" {
		t.Errorf("After Reset(), got %q, want \"\"", got)
	}
}

func TestShouldSave(t *testing.T) {
	tests := []struct {
		name      string
		oldPrefix string
		newPrefix string
		wantSave  bool
	}{
		{
			name:      "empty to empty - no save",
			oldPrefix: "",
			newPrefix: "",
			wantSave:  false,
		},
		{
			name:      "empty to code - no save (nothing to save)",
			oldPrefix: "",
			newPrefix: "code",
			wantSave:  false,
		},
		{
			name:      "code to code - no save (not switching)",
			oldPrefix: "code",
			newPrefix: "code",
			wantSave:  false,
		},
		{
			name:      "code to debug - save (switching templates)",
			oldPrefix: "code",
			newPrefix: "debug",
			wantSave:  true,
		},
		{
			name:      "code to empty - save (clearing template)",
			oldPrefix: "code",
			newPrefix: "",
			wantSave:  true,
		},
		{
			name:      "debug to code - save (switching templates)",
			oldPrefix: "debug",
			newPrefix: "code",
			wantSave:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			s.UpdatePrefix(tt.oldPrefix)

			got := s.ShouldSave(tt.newPrefix)
			if got != tt.wantSave {
				t.Errorf("ShouldSave(%q) with old=%q: got %v, want %v",
					tt.newPrefix, tt.oldPrefix, got, tt.wantSave)
			}
		})
	}
}

func TestShouldRestore(t *testing.T) {
	tests := []struct {
		name         string
		oldPrefix    string
		newPrefix    string
		wantRestore  bool
	}{
		{
			name:        "empty to empty - no restore",
			oldPrefix:   "",
			newPrefix:   "",
			wantRestore: false,
		},
		{
			name:        "empty to code - restore (loading template)",
			oldPrefix:   "",
			newPrefix:   "code",
			wantRestore: true,
		},
		{
			name:        "code to code - no restore (already loaded)",
			oldPrefix:   "code",
			newPrefix:   "code",
			wantRestore: false,
		},
		{
			name:        "code to debug - restore (switching templates)",
			oldPrefix:   "code",
			newPrefix:   "debug",
			wantRestore: true,
		},
		{
			name:        "code to empty - no restore (clearing, nothing to load)",
			oldPrefix:   "code",
			newPrefix:   "",
			wantRestore: false,
		},
		{
			name:        "debug to code - restore (switching templates)",
			oldPrefix:   "debug",
			newPrefix:   "code",
			wantRestore: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			s.UpdatePrefix(tt.oldPrefix)

			got := s.ShouldRestore(tt.newPrefix)
			if got != tt.wantRestore {
				t.Errorf("ShouldRestore(%q) with old=%q: got %v, want %v",
					tt.newPrefix, tt.oldPrefix, got, tt.wantRestore)
			}
		})
	}
}

// TestTransitionSequence tests a realistic sequence of template transitions
func TestTransitionSequence(t *testing.T) {
	s := New()

	// Step 1: First query with @code
	// Should restore code, should not save anything
	if s.ShouldSave("code") {
		t.Error("Step 1: Should not save when starting from empty")
	}
	if !s.ShouldRestore("code") {
		t.Error("Step 1: Should restore code template")
	}
	s.UpdatePrefix("code")

	// Step 2: Second query with @code (warmup or user)
	// No save, no restore (already loaded)
	if s.ShouldSave("code") {
		t.Error("Step 2: Should not save when staying on same template")
	}
	if s.ShouldRestore("code") {
		t.Error("Step 2: Should not restore when already loaded")
	}
	s.UpdatePrefix("code")

	// Step 3: Switch to @debug
	// Should save code, should restore debug
	if !s.ShouldSave("debug") {
		t.Error("Step 3: Should save code before switching")
	}
	if !s.ShouldRestore("debug") {
		t.Error("Step 3: Should restore debug when switching")
	}
	s.UpdatePrefix("debug")

	// Step 4: Query with no template
	// Should save debug, should not restore anything
	if !s.ShouldSave("") {
		t.Error("Step 4: Should save debug before clearing")
	}
	if s.ShouldRestore("") {
		t.Error("Step 4: Should not restore when clearing to no template")
	}
	s.UpdatePrefix("")

	// Step 5: Another query with no template
	// No save, no restore
	if s.ShouldSave("") {
		t.Error("Step 5: Should not save when staying on no template")
	}
	if s.ShouldRestore("") {
		t.Error("Step 5: Should not restore when staying on no template")
	}
	s.UpdatePrefix("")

	// Step 6: Back to @code
	// Should not save (nothing to save), should restore code
	if s.ShouldSave("code") {
		t.Error("Step 6: Should not save when switching from empty")
	}
	if !s.ShouldRestore("code") {
		t.Error("Step 6: Should restore code when switching from empty")
	}
	s.UpdatePrefix("code")
}

// TestConcurrentAccess verifies thread-safety
func TestConcurrentAccess(t *testing.T) {
	s := New()
	var wg sync.WaitGroup

	// Run multiple goroutines that read and write state
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			prefix := "template"
			for j := 0; j < 100; j++ {
				// These should not race or panic
				s.ShouldSave(prefix)
				s.ShouldRestore(prefix)
				s.UpdatePrefix(prefix)
				s.GetLastPrefix()
			}
		}(i)
	}

	wg.Wait()
	// If we get here without panicking, thread-safety is working
}

// TestActiveTemplateDevelopment simulates the scenario where a user is
// actively editing a template and warmup runs repeatedly
func TestActiveTemplateDevelopment(t *testing.T) {
	s := New()

	// Initial warmup with @code
	if s.ShouldSave("code") {
		t.Error("Initial: Should not save from empty")
	}
	if !s.ShouldRestore("code") {
		t.Error("Initial: Should restore code")
	}
	s.UpdatePrefix("code")

	// User edits template, warmup runs again (10 times)
	// Each time: no save, no restore (same template)
	for i := 0; i < 10; i++ {
		if s.ShouldSave("code") {
			t.Errorf("Warmup %d: Should not save when template unchanged", i)
		}
		if s.ShouldRestore("code") {
			t.Errorf("Warmup %d: Should not restore when already loaded", i)
		}
		s.UpdatePrefix("code")
	}

	// User switches to @debug
	// Now we save code (finally!) and restore debug
	if !s.ShouldSave("debug") {
		t.Error("Switch: Should save code when switching away")
	}
	if !s.ShouldRestore("debug") {
		t.Error("Switch: Should restore debug when switching to it")
	}
	s.UpdatePrefix("debug")
}
