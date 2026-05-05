// Package deacon provides the Deacon agent infrastructure.
// The Deacon is a Claude agent that monitors Mayor and Witnesses,
// handles lifecycle requests, and keeps Gas Town running.
package deacon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/gastown/internal/session"
)

// Heartbeat age thresholds — these are compiled-in defaults.
// Configurable via operational.deacon.heartbeat_stale_threshold and
// operational.deacon.heartbeat_very_stale_threshold in settings/config.json.
const (
	// HeartbeatStaleThreshold is the age at which a heartbeat is considered stale.
	HeartbeatStaleThreshold = 5 * time.Minute

	// HeartbeatVeryStaleThreshold is the age at which a heartbeat is considered
	// very stale, meaning the Deacon should be poked or restarted.
	// Must be greater than patrol backoff-max (15m) to avoid false positives
	// during legitimate await-signal sleep.
	HeartbeatVeryStaleThreshold = 20 * time.Minute
)

// Heartbeat represents the Deacon's heartbeat file contents.
// Written by the Deacon on each wake cycle.
// Read by the Go daemon to decide whether to poke.
type Heartbeat struct {
	// Timestamp is when the heartbeat was written.
	Timestamp time.Time `json:"timestamp"`

	// Cycle is the current wake cycle number.
	Cycle int64 `json:"cycle"`

	// LastAction describes what the Deacon did in this cycle.
	LastAction string `json:"last_action,omitempty"`

	// HealthyAgents is the count of healthy agents observed.
	HealthyAgents int `json:"healthy_agents"`

	// UnhealthyAgents is the count of unhealthy agents observed.
	UnhealthyAgents int `json:"unhealthy_agents"`
}

// HeartbeatFile returns the path to the Deacon heartbeat file.
func HeartbeatFile(townRoot string) string {
	return filepath.Join(townRoot, "deacon", "heartbeat.json")
}

// WriteHeartbeat writes a new heartbeat to disk.
// Called by the Deacon at the start of each wake cycle.
func WriteHeartbeat(townRoot string, hb *Heartbeat) error {
	hbFile := HeartbeatFile(townRoot)

	// Ensure deacon directory exists
	if err := os.MkdirAll(filepath.Dir(hbFile), 0755); err != nil {
		return err
	}

	// Set timestamp if not already set
	if hb.Timestamp.IsZero() {
		hb.Timestamp = time.Now().UTC()
	}

	data, err := json.MarshalIndent(hb, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(hbFile, data, 0600); err != nil {
		return err
	}

	// Also touch .deacon-heartbeat for backward compatibility with shell scripts
	// that check this file's mtime for liveness detection (stuck-agent-dog).
	// These scripts predate heartbeat.json and check mtime, not file contents.
	legacyFile := filepath.Join(filepath.Dir(hbFile), ".deacon-heartbeat")
	_ = os.WriteFile(legacyFile, []byte(""), 0644) //nolint:gosec // G306: world-readable liveness file is intentional

	// Dual-write a polecat-compatible heartbeat to .runtime/heartbeats/hq-deacon.json.
	// Schema mirrors polecat.SessionHeartbeat (state/context/bead) so observability
	// tooling that reads the unified .runtime/heartbeats path sees fresh data for the
	// deacon. The legacy path above remains canonical for plugins that haven't
	// migrated. Best-effort: write failures are silently ignored.
	writeSessionHeartbeat(townRoot, hb)

	return nil
}

// writeSessionHeartbeat writes a polecat-compatible heartbeat at
// $townRoot/.runtime/heartbeats/hq-deacon.json. The schema matches
// polecat.SessionHeartbeat — kept duplicated here to avoid importing polecat
// (heavy transitive deps) for one write. Errors are silently ignored.
func writeSessionHeartbeat(townRoot string, hb *Heartbeat) {
	dir := filepath.Join(townRoot, ".runtime", "heartbeats")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}

	// Mirrors polecat.SessionHeartbeat / polecat.HeartbeatWorking. The deacon's
	// legacy heartbeat path doesn't carry agent-reported state, so this writer
	// always reports "working" — the deacon would call gt heartbeat --state=stuck
	// directly via the polecat path if it needed to self-report stuck.
	payload := struct {
		Timestamp time.Time `json:"timestamp"`
		State     string    `json:"state,omitempty"`
		Context   string    `json:"context,omitempty"`
		Bead      string    `json:"bead,omitempty"`
	}{
		Timestamp: hb.Timestamp,
		State:     "working",
		Context:   hb.LastAction,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	path := filepath.Join(dir, session.DeaconSessionName()+".json")
	_ = os.WriteFile(path, data, 0644) //nolint:gosec // G306: liveness file matching polecat convention
}

// ReadHeartbeat reads the Deacon heartbeat from disk.
// Returns nil if the file doesn't exist or can't be read.
func ReadHeartbeat(townRoot string) *Heartbeat {
	hbFile := HeartbeatFile(townRoot)

	data, err := os.ReadFile(hbFile) //nolint:gosec // G304: path is constructed from trusted townRoot
	if err != nil {
		return nil
	}

	var hb Heartbeat
	if err := json.Unmarshal(data, &hb); err != nil {
		return nil
	}

	return &hb
}

// Age returns how old the heartbeat is.
// Returns a very large duration if the heartbeat is nil.
func (hb *Heartbeat) Age() time.Duration {
	if hb == nil {
		return 24 * time.Hour * 365 // Very stale
	}
	return time.Since(hb.Timestamp)
}

// IsFresh returns true if the heartbeat is less than 5 minutes old.
// A fresh heartbeat means the Deacon is actively working or recently finished.
func (hb *Heartbeat) IsFresh() bool {
	return hb != nil && hb.Age() < HeartbeatStaleThreshold
}

// IsFreshWith returns true if the heartbeat age is less than staleThreshold.
// Callers that load thresholds from operational config should prefer this over IsFresh.
func (hb *Heartbeat) IsFreshWith(staleThreshold time.Duration) bool {
	return hb != nil && hb.Age() < staleThreshold
}

// IsStale returns true if the heartbeat is 5-20 minutes old.
// A stale heartbeat may indicate the Deacon is doing a long operation.
func (hb *Heartbeat) IsStale() bool {
	if hb == nil {
		return false
	}
	age := hb.Age()
	return age >= HeartbeatStaleThreshold && age < HeartbeatVeryStaleThreshold
}

// IsStaleWith returns true if the heartbeat age is between staleThreshold and veryStaleThreshold.
// Callers that load thresholds from operational config should prefer this over IsStale.
func (hb *Heartbeat) IsStaleWith(staleThreshold, veryStaleThreshold time.Duration) bool {
	if hb == nil {
		return false
	}
	age := hb.Age()
	return age >= staleThreshold && age < veryStaleThreshold
}

// IsVeryStale returns true if the heartbeat is more than 20 minutes old.
// A very stale heartbeat means the Deacon should be poked.
func (hb *Heartbeat) IsVeryStale() bool {
	return hb == nil || hb.Age() >= HeartbeatVeryStaleThreshold
}

// IsVeryStaleWith returns true if the heartbeat is nil or older than veryStaleThreshold.
// Callers that load thresholds from operational config should prefer this over IsVeryStale.
func (hb *Heartbeat) IsVeryStaleWith(veryStaleThreshold time.Duration) bool {
	return hb == nil || hb.Age() >= veryStaleThreshold
}

// IsStuck returns true if the Deacon should be considered stuck based on heartbeat age.
// running must be true (session exists); stuck = running && very-stale heartbeat.
// Use the configured veryStaleThreshold from operational config rather than the
// hardcoded constant so operators can tune the threshold without recompiling.
func IsStuck(running bool, hb *Heartbeat, veryStaleThreshold time.Duration) bool {
	return running && hb.IsVeryStaleWith(veryStaleThreshold)
}

// Touch writes a minimal heartbeat with just the timestamp.
// This is a convenience function for simple heartbeat updates.
func Touch(townRoot string) error {
	// Read existing heartbeat to increment cycle
	existing := ReadHeartbeat(townRoot)
	cycle := int64(1)
	if existing != nil {
		cycle = existing.Cycle + 1
	}

	return WriteHeartbeat(townRoot, &Heartbeat{
		Timestamp: time.Now().UTC(),
		Cycle:     cycle,
	})
}

// TouchWithAction writes a heartbeat with an action description.
func TouchWithAction(townRoot, action string, healthy, unhealthy int) error {
	existing := ReadHeartbeat(townRoot)
	cycle := int64(1)
	if existing != nil {
		cycle = existing.Cycle + 1
	}

	return WriteHeartbeat(townRoot, &Heartbeat{
		Timestamp:       time.Now().UTC(),
		Cycle:           cycle,
		LastAction:      action,
		HealthyAgents:   healthy,
		UnhealthyAgents: unhealthy,
	})
}
