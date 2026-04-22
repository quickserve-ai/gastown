package session

import (
	"errors"
	"testing"

	"github.com/steveyegge/gastown/internal/config"
)

func TestExpectedAgentName_OverrideWins(t *testing.T) {
	rc := &config.RuntimeConfig{ResolvedAgent: "claude-haiku"}
	got := ExpectedAgentName("gemini", rc)
	if got != "gemini" {
		t.Errorf("ExpectedAgentName override = %q, want %q", got, "gemini")
	}
}

func TestExpectedAgentName_FallsBackToResolved(t *testing.T) {
	rc := &config.RuntimeConfig{ResolvedAgent: "claude-haiku"}
	got := ExpectedAgentName("", rc)
	if got != "claude-haiku" {
		t.Errorf("ExpectedAgentName resolved = %q, want %q", got, "claude-haiku")
	}
}

func TestExpectedAgentName_NilRuntimeConfig(t *testing.T) {
	if got := ExpectedAgentName("", nil); got != "" {
		t.Errorf("ExpectedAgentName nil rc = %q, want empty string", got)
	}
}

func TestExpectedAgentName_EmptyResolvedReturnsEmpty(t *testing.T) {
	rc := &config.RuntimeConfig{ResolvedAgent: ""}
	if got := ExpectedAgentName("", rc); got != "" {
		t.Errorf("ExpectedAgentName empty resolved = %q, want empty string", got)
	}
}

func TestAgentDrift_Match(t *testing.T) {
	getEnv := func(session, key string) (string, error) {
		if key == "GT_AGENT" {
			return "claude-haiku", nil
		}
		return "", errors.New("not found")
	}
	if AgentDrift(getEnv, "session", "claude-haiku") {
		t.Error("AgentDrift should be false when stored matches expected")
	}
}

func TestAgentDrift_Mismatch(t *testing.T) {
	getEnv := func(session, key string) (string, error) {
		return "claude", nil
	}
	if !AgentDrift(getEnv, "session", "claude-haiku") {
		t.Error("AgentDrift should be true when stored differs from expected")
	}
}

func TestAgentDrift_LookupErrorTreatedAsDrift(t *testing.T) {
	// Legacy sessions predate GT_AGENT, or tmux env got cleared.
	// We cannot prove the baked-in command is correct → rebuild.
	getEnv := func(session, key string) (string, error) {
		return "", errors.New("env not found")
	}
	if !AgentDrift(getEnv, "session", "claude-haiku") {
		t.Error("AgentDrift should be true when lookup fails")
	}
}

func TestAgentDrift_EmptyStoredEmptyExpected(t *testing.T) {
	// Both empty: not drift (no contradictory information).
	getEnv := func(session, key string) (string, error) {
		return "", nil
	}
	if AgentDrift(getEnv, "session", "") {
		t.Error("AgentDrift should be false when both stored and expected are empty")
	}
}
