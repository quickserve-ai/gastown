package deacon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
)

// DispatchGatedResult describes the outcome of dispatching gate-ready molecules.
type DispatchGatedResult struct {
	Dispatched []string // molecule IDs successfully dispatched
	Failed     []string // molecule IDs that failed to dispatch
	Skipped    []string // molecule IDs skipped (no rig resolved)
	Total      int      // total gate-ready molecules found across all rigs
}

// DispatchGated finds molecules blocked on closed gates across all rig beads
// directories and dispatches them to the appropriate polecats.
//
// This implements the async resume cycle: the molecule state IS the waiter —
// patrol discovers reality each cycle without explicit waiter tracking.
func DispatchGated(townRoot string) *DispatchGatedResult {
	result := &DispatchGatedResult{}

	dirs := gatedBeadsSearchDirs(townRoot)
	seen := make(map[string]bool)

	for _, dir := range dirs {
		b := beads.New(dir)
		molecules, err := b.ReadyGated()
		if err != nil {
			// Non-fatal: skip dirs where bd ready --gated fails (e.g., no gate table).
			continue
		}

		for _, mol := range molecules {
			if seen[mol.MoleculeID] {
				continue
			}
			seen[mol.MoleculeID] = true
			result.Total++

			rig := resolveRigFromBead(townRoot, mol.MoleculeID)
			if rig == "" {
				result.Skipped = append(result.Skipped, mol.MoleculeID)
				fmt.Fprintf(os.Stderr, "dispatch-gated: cannot determine rig for %s, skipping\n", mol.MoleculeID)
				continue
			}

			if err := slingBead(townRoot, mol.MoleculeID, rig, ""); err != nil {
				result.Failed = append(result.Failed, mol.MoleculeID)
				fmt.Fprintf(os.Stderr, "dispatch-gated: failed to sling %s → %s: %v\n", mol.MoleculeID, rig, err)
				continue
			}

			result.Dispatched = append(result.Dispatched, mol.MoleculeID)
		}
	}

	return result
}

// gatedBeadsSearchDirs returns all beads directories to scan for gate-ready
// molecules: the town root plus every rig that has a .beads/ subdirectory.
func gatedBeadsSearchDirs(townRoot string) []string {
	dirs := []string{townRoot}
	seen := map[string]bool{townRoot: true}

	entries, err := os.ReadDir(townRoot)
	if err != nil {
		return dirs
	}

	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "mayor" || e.Name() == "settings" {
			continue
		}
		rigDir := filepath.Join(townRoot, e.Name())
		beadsDir := filepath.Join(rigDir, ".beads")
		if _, err := os.Stat(beadsDir); err == nil && !seen[rigDir] {
			dirs = append(dirs, rigDir)
			seen[rigDir] = true
		}
		mayorRigDir := filepath.Join(rigDir, "mayor", "rig")
		mayorBeadsDir := filepath.Join(mayorRigDir, ".beads")
		if _, err := os.Stat(mayorBeadsDir); err == nil && !seen[mayorRigDir] {
			dirs = append(dirs, mayorRigDir)
			seen[mayorRigDir] = true
		}
	}

	return dirs
}
