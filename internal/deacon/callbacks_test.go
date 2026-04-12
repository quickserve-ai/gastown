package deacon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClassifyCallback(t *testing.T) {
	tests := []struct {
		subject  string
		expected CallbackType
	}{
		{"POLECAT_DONE max", CallbackTypePolecat},
		{"POLECAT_DONE sigzil-mnwaw37h", CallbackTypePolecat},
		{"Merge Request Rejected: polecat/max-abc123", CallbackTypeMergeRejected},
		{"Merge Request Completed: polecat/max-abc123", CallbackTypeMergeComplete},
		{"HELP: Can't find test config", CallbackTypeHelp},
		{"ESCALATION: Dolt server unreachable", CallbackTypeEscalation},
		{"SLING_REQUEST: gt-abc123", CallbackTypeSling},
		{"Random unrelated message", CallbackTypeUnknown},
		{"", CallbackTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			result := ClassifyCallback(tt.subject)
			if result != tt.expected {
				t.Errorf("ClassifyCallback(%q) = %q, want %q", tt.subject, result, tt.expected)
			}
		})
	}
}

func TestCallbackState_LoadSaveRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	// Load from non-existent file — should return empty state
	state := LoadCallbackState(tmpDir)
	if state == nil {
		t.Fatal("LoadCallbackState returned nil")
	}
	if len(state.ProcessedMessages) != 0 {
		t.Errorf("expected empty ProcessedMessages, got %d", len(state.ProcessedMessages))
	}

	// Add an entry and save
	state.ProcessedMessages["msg-1"] = time.Now().UTC()
	state.ProcessedMessages["msg-2"] = time.Now().UTC()

	if err := SaveCallbackState(tmpDir, state); err != nil {
		t.Fatalf("SaveCallbackState: %v", err)
	}

	// Verify file was written
	stateFile := callbackStateFile(tmpDir)
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		t.Fatal("state file was not created")
	}

	// Reload and verify
	loaded := LoadCallbackState(tmpDir)
	if len(loaded.ProcessedMessages) != 2 {
		t.Errorf("expected 2 ProcessedMessages, got %d", len(loaded.ProcessedMessages))
	}
	if _, ok := loaded.ProcessedMessages["msg-1"]; !ok {
		t.Error("missing msg-1 in loaded state")
	}
	if _, ok := loaded.ProcessedMessages["msg-2"]; !ok {
		t.Error("missing msg-2 in loaded state")
	}
}

func TestCallbackState_CorruptedFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Write garbage to the state file
	stateFile := callbackStateFile(tmpDir)
	if err := os.MkdirAll(filepath.Dir(stateFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateFile, []byte("not valid json"), 0600); err != nil {
		t.Fatal(err)
	}

	// Should return fresh state, not error
	state := LoadCallbackState(tmpDir)
	if state == nil {
		t.Fatal("LoadCallbackState returned nil for corrupted file")
	}
	if len(state.ProcessedMessages) != 0 {
		t.Errorf("expected empty ProcessedMessages, got %d", len(state.ProcessedMessages))
	}
}

func TestCallbackStateFile(t *testing.T) {
	path := callbackStateFile("/tmp/town")
	expected := filepath.Join("/tmp/town", "deacon", "callbacks-state.json")
	if path != expected {
		t.Errorf("callbackStateFile = %q, want %q", path, expected)
	}
}

func TestSaveCallbackState_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	townRoot := filepath.Join(tmpDir, "nonexistent")

	state := &CallbackState{
		ProcessedMessages: map[string]time.Time{
			"test": time.Now().UTC(),
		},
	}

	if err := SaveCallbackState(townRoot, state); err != nil {
		t.Fatalf("SaveCallbackState failed: %v", err)
	}

	// Verify directory was created
	deaconDir := filepath.Join(townRoot, "deacon")
	if _, err := os.Stat(deaconDir); os.IsNotExist(err) {
		t.Error("deacon directory was not created")
	}
}

func TestPatternPolecatDone(t *testing.T) {
	tests := []struct {
		input    string
		match    bool
		expected string
	}{
		{"POLECAT_DONE max", true, "max"},
		{"POLECAT_DONE sigzil-mnwaw37h", true, "sigzil-mnwaw37h"},
		{"POLECAT_DONE ", false, ""},
		{"Not a polecat done", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			matches := PatternPolecatDone.FindStringSubmatch(tt.input)
			if tt.match {
				if len(matches) < 2 {
					t.Errorf("expected match for %q", tt.input)
				} else if matches[1] != tt.expected {
					t.Errorf("got %q, want %q", matches[1], tt.expected)
				}
			} else {
				if len(matches) >= 2 {
					t.Errorf("unexpected match for %q: %v", tt.input, matches)
				}
			}
		})
	}
}

func TestPatternMergeCompleted(t *testing.T) {
	tests := []struct {
		input    string
		match    bool
		expected string
	}{
		{"Merge Request Completed: polecat/max-abc", true, "polecat/max-abc"},
		{"Merge Request Completed: feature/foo", true, "feature/foo"},
		{"Not a merge", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			matches := PatternMergeCompleted.FindStringSubmatch(tt.input)
			if tt.match {
				if len(matches) < 2 {
					t.Errorf("expected match for %q", tt.input)
				} else if matches[1] != tt.expected {
					t.Errorf("got %q, want %q", matches[1], tt.expected)
				}
			} else if len(matches) >= 2 {
				t.Errorf("unexpected match for %q", tt.input)
			}
		})
	}
}

func TestPatternMergeRejected(t *testing.T) {
	matches := PatternMergeRejected.FindStringSubmatch("Merge Request Rejected: polecat/xyz")
	if len(matches) < 2 {
		t.Fatal("expected match")
	}
	if matches[1] != "polecat/xyz" {
		t.Errorf("got %q, want polecat/xyz", matches[1])
	}
}

func TestPatternHelp(t *testing.T) {
	matches := PatternHelp.FindStringSubmatch("HELP: Can't compile auth module")
	if len(matches) < 2 {
		t.Fatal("expected match")
	}
	if matches[1] != "Can't compile auth module" {
		t.Errorf("got %q", matches[1])
	}
}

func TestPatternEscalation(t *testing.T) {
	matches := PatternEscalation.FindStringSubmatch("ESCALATION: Dolt server down")
	if len(matches) < 2 {
		t.Fatal("expected match")
	}
	if matches[1] != "Dolt server down" {
		t.Errorf("got %q", matches[1])
	}
}

func TestPatternSling(t *testing.T) {
	matches := PatternSling.FindStringSubmatch("SLING_REQUEST: gt-abc123")
	if len(matches) < 2 {
		t.Fatal("expected match")
	}
	if matches[1] != "gt-abc123" {
		t.Errorf("got %q", matches[1])
	}
}
