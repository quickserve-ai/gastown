package tmux

import (
	"strings"
	"testing"
)

// TestHardenWheelCopyMode verifies the wheel binding is rebound so scroll never
// enters copy-mode (the trigger for the tmux server double-free, hq-lfvex4 /
// upstream tmux #4777).
func TestHardenWheelCopyMode(t *testing.T) {
	tm := newTestTmux(t)
	if err := tm.HardenWheelCopyMode(); err != nil {
		t.Fatalf("HardenWheelCopyMode: %v", err)
	}
	for _, key := range []string{"WheelUpPane", "WheelDownPane"} {
		out, err := tm.run("list-keys", "-T", "root", key)
		if err != nil {
			t.Fatalf("list-keys %s: %v", key, err)
		}
		if strings.Contains(out, "copy-mode") {
			t.Errorf("%s must not enter copy-mode after hardening, got: %s", key, out)
		}
		if !strings.Contains(out, "send-keys") {
			t.Errorf("%s should forward the wheel via send-keys -M, got: %s", key, out)
		}
	}
}

// TestEnableMouseMode_WheelCopyModeGate verifies the hardening is OFF by default
// (deploy is inert) and only applies when @gt-harden-wheel-copy-mode is "on".
func TestEnableMouseMode_WheelCopyModeGate(t *testing.T) {
	tm := newTestTmux(t)

	// EnableMouseMode early-returns when global mouse is "off" (tmux default),
	// so enable it on the isolated test server first.
	if _, err := tm.run("set-option", "-g", "mouse", "on"); err != nil {
		t.Fatalf("set -g mouse on: %v", err)
	}

	session := "gt-wheel-gate-test"
	if err := tm.NewSessionWithCommand(session, "", "sleep 30"); err != nil {
		t.Fatalf("NewSessionWithCommand: %v", err)
	}
	t.Cleanup(func() { _, _ = tm.run("kill-session", "-t", session) })

	wheelBinding := func() string {
		out, _ := tm.run("list-keys", "-T", "root", "WheelUpPane")
		return out
	}

	t.Run("gate off leaves the wheel binding untouched", func(t *testing.T) {
		_, _ = tm.run("set-option", "-gu", "@gt-harden-wheel-copy-mode")
		// Install a sentinel binding we can detect.
		if _, err := tm.run("bind-key", "-T", "root", "WheelUpPane", "display-message", "wheel-sentinel"); err != nil {
			t.Fatalf("set sentinel binding: %v", err)
		}
		if err := tm.EnableMouseMode(session); err != nil {
			t.Fatalf("EnableMouseMode: %v", err)
		}
		if got := wheelBinding(); !strings.Contains(got, "wheel-sentinel") {
			t.Errorf("gate off: EnableMouseMode must not touch WheelUpPane, got: %s", got)
		}
	})

	t.Run("gate on hardens the wheel against copy-mode", func(t *testing.T) {
		if _, err := tm.run("set-option", "-g", "@gt-harden-wheel-copy-mode", "on"); err != nil {
			t.Fatalf("set gate on: %v", err)
		}
		t.Cleanup(func() { _, _ = tm.run("set-option", "-gu", "@gt-harden-wheel-copy-mode") })
		if err := tm.EnableMouseMode(session); err != nil {
			t.Fatalf("EnableMouseMode: %v", err)
		}
		got := wheelBinding()
		if strings.Contains(got, "copy-mode") {
			t.Errorf("gate on: WheelUpPane must not enter copy-mode, got: %s", got)
		}
		if !strings.Contains(got, "send-keys") {
			t.Errorf("gate on: WheelUpPane should forward the wheel via send-keys -M, got: %s", got)
		}
	})
}
