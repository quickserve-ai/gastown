package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/constants"
)

var patrolNewRole string

var patrolNewCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a new patrol wisp with config variables",
	Long: `Create a new patrol wisp for the current role, injecting rig config
variables so the formula has correct settings baked in.

Role is auto-detected from GT_ROLE (set by the daemon). Use --role to override.

For refinery patrols, MQ config variables (run_tests, test_command,
target_branch, etc.) are read from the rig's config.json and settings/config.json and
passed as --var args to the wisp.

Examples:
  gt patrol new                  # Auto-detect role, create patrol
  gt patrol new --role refinery  # Explicitly create refinery patrol`,
	RunE: runPatrolNew,
}

func init() {
	patrolNewCmd.Flags().StringVar(&patrolNewRole, "role", "", "Role override (deacon, witness, refinery)")
}

func runPatrolNew(cmd *cobra.Command, args []string) error {
	// Resolve role
	roleInfo, err := GetRole()
	if err != nil {
		return fmt.Errorf("detecting role: %w", err)
	}

	// Allow --role flag to override; otherwise use the already-parsed role
	// (GetRole already handles GT_ROLE env var internally)
	roleName := string(roleInfo.Role)
	if patrolNewRole != "" {
		roleName = patrolNewRole
	}

	// Build config based on role
	var cfg PatrolConfig
	switch Role(roleName) {
	case RoleDeacon:
		cfg = PatrolConfig{
			RoleName:      "deacon",
			PatrolMolName: constants.MolDeaconPatrol,
			BeadsDir:      roleInfo.TownRoot,
			Assignee:      "deacon",
		}
	case RoleWitness:
		// BeadsDir must be the rig root, NOT the town root: witness queries
		// its own rig's DB via the redirect chain at <rig>/.beads/redirect →
		// mayor/rig/.beads. Passing TownRoot lands the patrol bead in the
		// hq DB where witness never looks, leaving it hookless after spawn.
		cfg = PatrolConfig{
			RoleName:      "witness",
			PatrolMolName: constants.MolWitnessPatrol,
			BeadsDir:      filepath.Join(roleInfo.TownRoot, roleInfo.Rig),
			Assignee:      roleInfo.Rig + "/witness",
		}
	case RoleRefinery:
		cfg = PatrolConfig{
			RoleName:      "refinery",
			PatrolMolName: constants.MolRefineryPatrol,
			BeadsDir:      filepath.Join(roleInfo.TownRoot, roleInfo.Rig),
			Assignee:      roleInfo.Rig + "/refinery",
			ExtraVars:     buildRefineryPatrolVars(roleInfo),
		}
	default:
		return fmt.Errorf("unsupported role for patrol: %q (expected deacon, witness, or refinery)", roleName)
	}

	// Create and hook the wisp
	patrolID, err := autoSpawnPatrol(cfg)
	if err != nil {
		if patrolID != "" {
			// Created but failed to hook
			fmt.Fprintf(os.Stderr, "warning: %s\n", err.Error())
			fmt.Println(patrolID)
			return nil
		}
		return err
	}

	fmt.Println(patrolID)
	return nil
}
