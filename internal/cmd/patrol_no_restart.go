package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

var patrolNoRestartCmd = &cobra.Command{
	Use:   "no-restart",
	Short: "Manage the patrol auto-restart blocklist",
	Long: `Manage the list of polecats that patrol scan must never auto-restart.

When a polecat is on the blocklist, patrol scan skips it entirely — no zombie
detection, no restart, no notification. Use this when Mayor has force-stopped an
agent that must remain stopped (e.g., while waiting for a blocking bug fix to land).

The blocklist is stored in settings/config.json under operational.witness.no_restart_polecats.
Each entry is "rig/polecat" (rig-qualified) or "polecat" (matches any rig).

Examples:
  gt patrol no-restart list                  # Show all blocked polecats
  gt patrol no-restart add qcore/lisa        # Block lisa in qcore
  gt patrol no-restart add lisa              # Block lisa in any rig
  gt patrol no-restart remove qcore/lisa     # Unblock lisa in qcore`,
}

var patrolNoRestartListCmd = &cobra.Command{
	Use:   "list",
	Short: "List polecats on the auto-restart blocklist",
	RunE:  runPatrolNoRestartList,
}

var patrolNoRestartAddCmd = &cobra.Command{
	Use:   "add <polecat>",
	Short: "Add a polecat to the auto-restart blocklist",
	Long: `Add a polecat to the patrol auto-restart blocklist.

The polecat argument may be:
  "lisa"         — blocks lisa in any rig
  "qcore/lisa"   — blocks lisa only in qcore`,
	Args: cobra.ExactArgs(1),
	RunE: runPatrolNoRestartAdd,
}

var patrolNoRestartRemoveCmd = &cobra.Command{
	Use:   "remove <polecat>",
	Short: "Remove a polecat from the auto-restart blocklist",
	Args:  cobra.ExactArgs(1),
	RunE:  runPatrolNoRestartRemove,
}

func init() {
	patrolNoRestartCmd.AddCommand(patrolNoRestartListCmd)
	patrolNoRestartCmd.AddCommand(patrolNoRestartAddCmd)
	patrolNoRestartCmd.AddCommand(patrolNoRestartRemoveCmd)
	patrolCmd.AddCommand(patrolNoRestartCmd)
}

func runPatrolNoRestartList(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	witCfg := config.LoadOperationalConfig(townRoot).GetWitnessConfig()
	if len(witCfg.NoRestartPolecats) == 0 {
		fmt.Printf("%s No polecats on the auto-restart blocklist\n", style.Dim.Render("○"))
		return nil
	}

	fmt.Printf("%s Auto-restart blocklist (%d):\n", style.Bold.Render("🚫"), len(witCfg.NoRestartPolecats))
	for _, entry := range witCfg.NoRestartPolecats {
		fmt.Printf("  - %s\n", entry)
	}
	return nil
}

func runPatrolNoRestartAdd(cmd *cobra.Command, args []string) error {
	entry := args[0]
	if err := validateNoRestartEntry(entry); err != nil {
		return err
	}

	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	settingsPath := config.TownSettingsPath(townRoot)
	ts, err := config.LoadOrCreateTownSettings(settingsPath)
	if err != nil {
		return fmt.Errorf("loading town settings: %w", err)
	}

	if ts.Operational == nil {
		ts.Operational = config.DefaultOperationalConfig()
	}
	if ts.Operational.Witness == nil {
		ts.Operational.Witness = &config.WitnessThresholds{}
	}

	for _, existing := range ts.Operational.Witness.NoRestartPolecats {
		if existing == entry {
			fmt.Printf("%s %s is already on the blocklist\n", style.Dim.Render("○"), entry)
			return nil
		}
	}

	ts.Operational.Witness.NoRestartPolecats = append(ts.Operational.Witness.NoRestartPolecats, entry)

	if err := config.SaveTownSettings(settingsPath, ts); err != nil {
		return fmt.Errorf("saving town settings: %w", err)
	}

	fmt.Printf("%s Added %s to auto-restart blocklist\n", style.Success.Render("✓"), style.Bold.Render(entry))
	fmt.Printf("  Patrol scan will skip this polecat on every cycle until removed.\n")
	fmt.Printf("  Remove with: gt patrol no-restart remove %s\n", entry)
	return nil
}

func runPatrolNoRestartRemove(cmd *cobra.Command, args []string) error {
	entry := args[0]

	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	settingsPath := config.TownSettingsPath(townRoot)
	ts, err := config.LoadOrCreateTownSettings(settingsPath)
	if err != nil {
		return fmt.Errorf("loading town settings: %w", err)
	}

	if ts.Operational == nil || ts.Operational.Witness == nil {
		fmt.Printf("%s %s is not on the blocklist\n", style.Dim.Render("○"), entry)
		return nil
	}

	before := ts.Operational.Witness.NoRestartPolecats
	var after []string
	for _, existing := range before {
		if existing != entry {
			after = append(after, existing)
		}
	}

	if len(after) == len(before) {
		fmt.Printf("%s %s is not on the blocklist\n", style.Dim.Render("○"), entry)
		return nil
	}

	ts.Operational.Witness.NoRestartPolecats = after

	if err := config.SaveTownSettings(settingsPath, ts); err != nil {
		return fmt.Errorf("saving town settings: %w", err)
	}

	fmt.Printf("%s Removed %s from auto-restart blocklist\n", style.Success.Render("✓"), style.Bold.Render(entry))
	fmt.Printf("  Patrol scan will resume normal detection for this polecat.\n")
	return nil
}

// validateNoRestartEntry checks that the entry is a valid polecat or rig/polecat identifier.
func validateNoRestartEntry(entry string) error {
	if entry == "" {
		return fmt.Errorf("polecat name cannot be empty")
	}
	if strings.Contains(entry, " ") {
		return fmt.Errorf("polecat name cannot contain spaces: %q", entry)
	}
	parts := strings.SplitN(entry, "/", 2)
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("invalid polecat identifier %q: empty component", entry)
		}
	}
	if len(parts) > 2 {
		return fmt.Errorf("invalid format %q: use 'polecat' or 'rig/polecat'", entry)
	}
	return nil
}
