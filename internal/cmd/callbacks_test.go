package cmd

import "testing"

func TestClassifyCallback(t *testing.T) {
	tests := []struct {
		subject string
		want    CallbackType
	}{
		// Existing types
		{"POLECAT_DONE toast", CallbackPolecatDone},
		{"Merge Request Rejected: polecat/toast", CallbackMergeRejected},
		{"Merge Request Completed: polecat/toast", CallbackMergeCompleted},
		{"HELP: Can't compile", CallbackHelp},
		{"ESCALATION: Dolt is down", CallbackEscalation},
		{"SLING_REQUEST: gt-abc123", CallbackSling},

		// New types: DOG_DONE
		{"DOG_DONE alpha", CallbackDogDone},
		{"DOG_DONE: rebuild-gt success", CallbackDogDone},
		{"DOG_DONE:  orphan-scan completed 3 removed", CallbackDogDone},

		// New types: CONVOY_NEEDS_FEEDING
		{"CONVOY_NEEDS_FEEDING gt-conv-123", CallbackConvoyNeedsFeeding},
		{"CONVOY_NEEDS_FEEDING: convoy=gt-conv-123 issue=gt-abc", CallbackConvoyNeedsFeeding},

		// New types: RECOVERED_BEAD
		{"RECOVERED_BEAD gt-abc123", CallbackRecoveredBead},
		{"RECOVERED_BEAD bd-xyz", CallbackRecoveredBead},
		{"SPAWN_STORM RECOVERED_BEAD gt-abc123 (respawned 5x)", CallbackRecoveredBead},

		// Unknown
		{"Random subject", CallbackUnknown},
		{"", CallbackUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			got := classifyCallback(tt.subject)
			if got != tt.want {
				t.Errorf("classifyCallback(%q) = %q, want %q", tt.subject, got, tt.want)
			}
		})
	}
}

func TestPatternRecoveredBead(t *testing.T) {
	tests := []struct {
		subject string
		wantID  string
	}{
		{"RECOVERED_BEAD gt-abc123", "gt-abc123"},
		{"RECOVERED_BEAD bd-xyz", "bd-xyz"},
		{"SPAWN_STORM RECOVERED_BEAD gt-abc123 (respawned 5x)", "gt-abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			matches := patternRecoveredBead.FindStringSubmatch(tt.subject)
			if len(matches) < 2 {
				t.Fatalf("pattern did not match subject: %q", tt.subject)
			}
			if matches[1] != tt.wantID {
				t.Errorf("extracted bead ID = %q, want %q", matches[1], tt.wantID)
			}
		})
	}
}

func TestPatternDogDone(t *testing.T) {
	tests := []struct {
		subject  string
		wantInfo string
	}{
		{"DOG_DONE alpha", "alpha"},
		{"DOG_DONE: rebuild-gt success", "rebuild-gt success"},
	}

	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			matches := patternDogDone.FindStringSubmatch(tt.subject)
			if len(matches) < 2 {
				t.Fatalf("pattern did not match subject: %q", tt.subject)
			}
			got := matches[1]
			if got != tt.wantInfo {
				t.Errorf("extracted info = %q, want %q", got, tt.wantInfo)
			}
		})
	}
}

func TestPatternConvoyNeedsFeeding(t *testing.T) {
	tests := []struct {
		subject  string
		wantInfo string
	}{
		{"CONVOY_NEEDS_FEEDING gt-conv-123", "gt-conv-123"},
		{"CONVOY_NEEDS_FEEDING: convoy=gt-conv-123 issue=gt-abc", "convoy=gt-conv-123 issue=gt-abc"},
	}

	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			matches := patternConvoyNeedsFeeding.FindStringSubmatch(tt.subject)
			if len(matches) < 2 {
				t.Fatalf("pattern did not match subject: %q", tt.subject)
			}
			got := matches[1]
			if got != tt.wantInfo {
				t.Errorf("extracted info = %q, want %q", got, tt.wantInfo)
			}
		})
	}
}
