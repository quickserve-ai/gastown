package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/workspace"
)

// Lens-library distribution (hq-y614fo.5). The bridge repo holds a shared
// agent-lens library under bridge/.claude/agents/; gt prime mirrors it into
// each clone's .claude/agents/ so both runtimes (which discover .claude/agents/)
// see the same lenses. The mirror is idempotent, additive, and never blocks
// prime. Spec: bridge/migration/SPEC-hq-y614fo.5-lens-distribution.md.
const (
	// bridgeLensSubpath is the shared lens library, relative to the town root.
	bridgeLensSubpath = "bridge/.claude/agents"
	// cloneAgentsSubpath is the per-clone agent dir the lenses mirror into,
	// relative to the clone (cwd).
	cloneAgentsSubpath = ".claude/agents"
)

// lensSyncDryRun backs the hidden `gt lens-sync --dry-run` flag.
var lensSyncDryRun bool

// syncBridgeLenses mirrors *.md from <townRoot>/bridge/.claude/agents/ into
// <cwd>/.claude/agents/, idempotently and additively:
//
//   - overwrites a destination file only when its content differs from the
//     bridge copy (content compare, not mtime — mtime is unreliable across
//     fresh clones / git checkouts);
//   - never deletes a clone-local file: it only ever iterates bridge files, so
//     clone-local agents (e.g. derbysoft-continuation.md) inherently survive;
//   - creates <cwd>/.claude/agents/ on demand;
//   - is a silent no-op when the bridge lens dir is absent or unreadable, so a
//     fresh machine / partial checkout never blocks prime.
//
// When dryRun is true it reports the planned copies to stdout and writes
// nothing. Callers in the prime path treat any returned error as a soft skip —
// the lens library is optional and must never break session startup.
func syncBridgeLenses(townRoot, cwd string, dryRun bool) error {
	if townRoot == "" || cwd == "" {
		return nil
	}

	srcDir := filepath.Join(townRoot, bridgeLensSubpath)
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		// Bridge absent/unreadable: the lens library is optional — skip silently.
		return nil
	}

	destDir := filepath.Join(cwd, cloneAgentsSubpath)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		name := e.Name() // basename from ReadDir — no path traversal
		srcData, rerr := os.ReadFile(filepath.Join(srcDir, name))
		if rerr != nil {
			continue // unreadable lens — skip this one, not the batch
		}
		destPath := filepath.Join(destDir, name)
		if lensFileMatches(destPath, srcData) {
			continue // already current — no-op
		}
		if dryRun {
			fmt.Printf("[lens-sync] would sync: %s -> %s\n", name, destPath)
			continue
		}
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return fmt.Errorf("lens-sync: creating %s: %w", destDir, err)
		}
		if err := os.WriteFile(destPath, srcData, 0o644); err != nil {
			return fmt.Errorf("lens-sync: writing %s: %w", destPath, err)
		}
	}
	return nil
}

// lensFileMatches reports whether path exists and its bytes exactly equal want.
func lensFileMatches(path string, want []byte) bool {
	got, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return bytes.Equal(got, want)
}

// lensSyncCmd is a hidden helper to run the lens mirror manually (acceptance /
// debugging). The real distribution happens automatically inside gt prime.
var lensSyncCmd = &cobra.Command{
	Use:    "lens-sync",
	Short:  "Mirror the shared agent-lens library into this clone's .claude/agents/",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return err
		}
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		return syncBridgeLenses(townRoot, cwd, lensSyncDryRun)
	},
}

func init() {
	lensSyncCmd.Flags().BoolVar(&lensSyncDryRun, "dry-run", false,
		"Show planned lens copies without writing")
	rootCmd.AddCommand(lensSyncCmd)
}
