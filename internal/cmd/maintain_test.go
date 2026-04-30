package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestMaintainCommand_Registered(t *testing.T) {
	var found bool
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "maintain" {
			found = true

			if f := cmd.Flags().Lookup("force"); f == nil {
				t.Error("expected --force flag")
			}
			if f := cmd.Flags().Lookup("dry-run"); f == nil {
				t.Error("expected --dry-run flag")
			}
			if f := cmd.Flags().Lookup("threshold"); f == nil {
				t.Error("expected --threshold flag")
			} else if f.DefValue != "100" {
				t.Errorf("expected threshold default 100, got %s", f.DefValue)
			}
			if f := cmd.Flags().Lookup("force-federation-break"); f == nil {
				t.Error("expected --force-federation-break flag (gt-rmd federation guardrail)")
			} else if f.DefValue != "false" {
				t.Errorf("expected force-federation-break default false, got %s", f.DefValue)
			}

			if cmd.GroupID != GroupServices {
				t.Errorf("expected GroupServices, got %s", cmd.GroupID)
			}
			break
		}
	}
	if !found {
		t.Fatal("maintain command not registered on rootCmd")
	}
}

// TestDoltFlattenCommand_FederationFlag verifies the federation guardrail flag
// is registered on `gt dolt flatten`. Part of gt-rmd.
func TestDoltFlattenCommand_FederationFlag(t *testing.T) {
	var doltCmd *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "dolt" {
			doltCmd = c
			break
		}
	}
	if doltCmd == nil {
		t.Fatal("dolt command not registered")
	}
	var flatten *cobra.Command
	for _, sub := range doltCmd.Commands() {
		if sub.Name() == "flatten" {
			flatten = sub
			break
		}
	}
	if flatten == nil {
		t.Fatal("dolt flatten subcommand not registered")
	}
	if f := flatten.Flags().Lookup("yes-i-am-sure"); f == nil {
		t.Error("expected --yes-i-am-sure flag (existing safety interlock)")
	}
	if f := flatten.Flags().Lookup("force-federation-break"); f == nil {
		t.Error("expected --force-federation-break flag (gt-rmd federation guardrail)")
	} else if f.DefValue != "false" {
		t.Errorf("expected force-federation-break default false, got %s", f.DefValue)
	}
}

// TestDoltRebaseCommand_FederationFlag verifies the federation guardrail flag
// is registered on `gt dolt rebase`. Part of gt-rmd.
func TestDoltRebaseCommand_FederationFlag(t *testing.T) {
	var doltCmd *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "dolt" {
			doltCmd = c
			break
		}
	}
	if doltCmd == nil {
		t.Fatal("dolt command not registered")
	}
	var rebase *cobra.Command
	for _, sub := range doltCmd.Commands() {
		if sub.Name() == "rebase" {
			rebase = sub
			break
		}
	}
	if rebase == nil {
		t.Fatal("dolt rebase subcommand not registered")
	}
	if f := rebase.Flags().Lookup("yes-i-am-sure"); f == nil {
		t.Error("expected --yes-i-am-sure flag (existing safety interlock)")
	}
	if f := rebase.Flags().Lookup("force-federation-break"); f == nil {
		t.Error("expected --force-federation-break flag (gt-rmd federation guardrail)")
	} else if f.DefValue != "false" {
		t.Errorf("expected force-federation-break default false, got %s", f.DefValue)
	}
}

func TestMaintainThreshold(t *testing.T) {
	tests := []struct {
		commits   int
		threshold int
		flatten   bool
	}{
		{0, 100, false},
		{50, 100, false},
		{99, 100, false},
		{100, 100, true},
		{200, 100, true},
		{1000, 100, true},
		{5, 5, true},
		{4, 5, false},
	}
	for _, tt := range tests {
		flatten := tt.commits >= tt.threshold
		if flatten != tt.flatten {
			t.Errorf("commits=%d threshold=%d: got flatten=%v, want %v",
				tt.commits, tt.threshold, flatten, tt.flatten)
		}
	}
}

func TestMaintainDBInfo(t *testing.T) {
	// Verify the struct can hold expected values.
	info := maintainDBInfo{
		name:        "gastown",
		commitCount: 500,
		hasBackup:   true,
	}
	if info.name != "gastown" {
		t.Errorf("expected name gastown, got %s", info.name)
	}
	if info.commitCount != 500 {
		t.Errorf("expected 500 commits, got %d", info.commitCount)
	}
	if !info.hasBackup {
		t.Error("expected hasBackup true")
	}
}

func TestMaintainConstants(t *testing.T) {
	if defaultMaintainThreshold != 100 {
		t.Errorf("expected default threshold 100, got %d", defaultMaintainThreshold)
	}
}
