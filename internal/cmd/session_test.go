package cmd

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/polecat"
)

func TestSessionInfoJSONOutput(t *testing.T) {
	info := &polecat.SessionInfo{
		Polecat:   "alpha",
		SessionID: "gt-alpha",
		Running:   true,
		RigName:   "gastown",
		Attached:  false,
		Created:   time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC),
		Windows:   1,
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if parsed["polecat"] != "alpha" {
		t.Errorf("polecat = %v, want alpha", parsed["polecat"])
	}
	if parsed["session_id"] != "gt-alpha" {
		t.Errorf("session_id = %v, want gt-alpha", parsed["session_id"])
	}
	if parsed["running"] != true {
		t.Errorf("running = %v, want true", parsed["running"])
	}
	if parsed["rig_name"] != "gastown" {
		t.Errorf("rig_name = %v, want gastown", parsed["rig_name"])
	}
}

func TestSessionStatusCmdJSONFlagWiring(t *testing.T) {
	// Verify --json flag is registered on the session status command.
	// This catches regressions where flag binding is accidentally removed,
	// which would silently break formulas that depend on --json output.
	f := sessionStatusCmd.Flags().Lookup("json")
	if f == nil {
		t.Fatal("session status command missing --json flag")
	}
	if f.DefValue != "false" {
		t.Errorf("--json default = %q, want \"false\"", f.DefValue)
	}
}

// TestSessionListItemTransportFields verifies the additive observability fields
// (gt-eflz) serialize under the documented keys. These feed the witness-patrol
// formula's reap decision; git_state is the work-preservation guardrail.
func TestSessionListItemTransportFields(t *testing.T) {
	la := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	item := SessionListItem{
		Rig:          "gastown",
		Polecat:      "furiosa",
		SessionID:    "gt-furiosa",
		Running:      true,
		AgentState:   "idle",
		HookBead:     "", // no work bead → the stuck/idle condition
		NoWorkBead:   true,
		GitState:     "clean",
		LastActivity: &la,
		IdleSeconds:  3600,
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if m["agent_state"] != "idle" {
		t.Errorf("agent_state = %v, want idle", m["agent_state"])
	}
	if m["no_work_bead"] != true {
		t.Errorf("no_work_bead = %v, want true", m["no_work_bead"])
	}
	if m["git_state"] != "clean" {
		t.Errorf("git_state = %v, want clean (the reap guardrail)", m["git_state"])
	}
	if _, ok := m["last_activity"]; !ok {
		t.Error("last_activity missing")
	}
	if v, ok := m["idle_seconds"].(float64); !ok || v != 3600 {
		t.Errorf("idle_seconds = %v, want 3600", m["idle_seconds"])
	}
	// hook_bead is omitempty: absent when empty.
	if _, ok := m["hook_bead"]; ok {
		t.Error("hook_bead should be omitted when empty")
	}
}

// TestSessionListItemOmitsEmptyState verifies optional state fields are omitted
// when the agent bead is unreadable, but no_work_bead is ALWAYS present so the
// guardrail/condition is never silently absent.
func TestSessionListItemOmitsEmptyState(t *testing.T) {
	item := SessionListItem{Rig: "r", Polecat: "p", SessionID: "s", Running: true}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	for _, k := range []string{"agent_state", "git_state", "hook_bead", "last_activity"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s should be omitted when empty", k)
		}
	}
	if _, ok := m["no_work_bead"]; !ok {
		t.Error("no_work_bead must always be present")
	}
}

func TestSessionListCmdJSONFlagWiring(t *testing.T) {
	f := sessionListCmd.Flags().Lookup("json")
	if f == nil {
		t.Fatal("session list command missing --json flag")
	}
}

func TestSessionInfoJSONOutputNotRunning(t *testing.T) {
	info := &polecat.SessionInfo{
		Polecat:   "beta",
		SessionID: "gt-beta",
		Running:   false,
		RigName:   "testrig",
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if parsed["running"] != false {
		t.Errorf("running = %v, want false", parsed["running"])
	}
}
