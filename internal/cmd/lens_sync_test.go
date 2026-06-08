package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeLens is a test helper: it writes content to <dir>/<name>, creating dir.
func writeLens(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// bridgeAgentsDir returns the bridge lens source dir under a town root.
func bridgeAgentsDir(townRoot string) string {
	return filepath.Join(townRoot, bridgeLensSubpath)
}

// cloneAgentsDir returns the per-clone agents dir under a clone root.
func cloneAgentsDir(cwd string) string {
	return filepath.Join(cwd, cloneAgentsSubpath)
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// AC1 (+ create-dest-dir): a clean clone with no .claude/agents/ is populated
// with every bridge lens, and the destination dir is created on demand.
func TestSyncBridgeLenses_CreatesDestAndCopies(t *testing.T) {
	townRoot := t.TempDir()
	cwd := t.TempDir()
	src := bridgeAgentsDir(townRoot)
	writeLens(t, src, "security-lens.md", "SEC")
	writeLens(t, src, "perf-lens.md", "PERF")

	if err := syncBridgeLenses(townRoot, cwd, false); err != nil {
		t.Fatalf("syncBridgeLenses: %v", err)
	}

	dest := cloneAgentsDir(cwd)
	if got := readFile(t, filepath.Join(dest, "security-lens.md")); got != "SEC" {
		t.Errorf("security-lens.md = %q, want %q", got, "SEC")
	}
	if got := readFile(t, filepath.Join(dest, "perf-lens.md")); got != "PERF" {
		t.Errorf("perf-lens.md = %q, want %q", got, "PERF")
	}
}

// AC3 (overwrite-on-diff): editing a bridge lens propagates to the clone copy.
func TestSyncBridgeLenses_OverwriteOnDiff(t *testing.T) {
	townRoot := t.TempDir()
	cwd := t.TempDir()
	writeLens(t, bridgeAgentsDir(townRoot), "db-migration-lens.md", "NEW CONTENT")
	// Pre-existing clone copy with stale content.
	dest := cloneAgentsDir(cwd)
	writeLens(t, dest, "db-migration-lens.md", "OLD CONTENT")

	if err := syncBridgeLenses(townRoot, cwd, false); err != nil {
		t.Fatalf("syncBridgeLenses: %v", err)
	}
	if got := readFile(t, filepath.Join(dest, "db-migration-lens.md")); got != "NEW CONTENT" {
		t.Errorf("after sync = %q, want overwrite to %q", got, "NEW CONTENT")
	}
}

// no-op-when-equal: a clone copy already byte-identical to the bridge lens is
// NOT rewritten (content compare, not mtime) — proven by an unchanged mtime.
func TestSyncBridgeLenses_NoOpWhenEqual(t *testing.T) {
	townRoot := t.TempDir()
	cwd := t.TempDir()
	writeLens(t, bridgeAgentsDir(townRoot), "domain-language-lens.md", "SAME")
	dest := cloneAgentsDir(cwd)
	destFile := writeLens(t, dest, "domain-language-lens.md", "SAME")

	// Pin mtime to a known past instant; a rewrite would move it forward.
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(destFile, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	before, err := os.Stat(destFile)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if err := syncBridgeLenses(townRoot, cwd, false); err != nil {
		t.Fatalf("syncBridgeLenses: %v", err)
	}

	after, err := os.Stat(destFile)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("identical file was rewritten: mtime %v -> %v", before.ModTime(), after.ModTime())
	}
}

// AC2 (never-delete-clone-local): a clone-local agent with no bridge counterpart
// survives the sync untouched.
func TestSyncBridgeLenses_NeverDeletesCloneLocal(t *testing.T) {
	townRoot := t.TempDir()
	cwd := t.TempDir()
	writeLens(t, bridgeAgentsDir(townRoot), "security-lens.md", "SEC")
	dest := cloneAgentsDir(cwd)
	local := writeLens(t, dest, "derbysoft-continuation.md", "CLONE LOCAL")

	if err := syncBridgeLenses(townRoot, cwd, false); err != nil {
		t.Fatalf("syncBridgeLenses: %v", err)
	}
	if got := readFile(t, local); got != "CLONE LOCAL" {
		t.Errorf("clone-local file changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "security-lens.md")); err != nil {
		t.Errorf("bridge lens not synced alongside clone-local: %v", err)
	}
}

// skip-on-absent-bridge: no bridge lens dir => silent no-op, no error, and the
// clone's .claude/agents/ is NOT created (prime must never break on a missing
// or partial bridge checkout).
func TestSyncBridgeLenses_SkipOnAbsentBridge(t *testing.T) {
	townRoot := t.TempDir() // no bridge/.claude/agents under it
	cwd := t.TempDir()

	if err := syncBridgeLenses(townRoot, cwd, false); err != nil {
		t.Fatalf("expected silent no-op, got error: %v", err)
	}
	if _, err := os.Stat(cloneAgentsDir(cwd)); !os.IsNotExist(err) {
		t.Errorf("dest dir should not be created when bridge is absent (err=%v)", err)
	}
}

// AC5 (dry-run-no-writes): --dry-run reports planned copies and writes nothing.
func TestSyncBridgeLenses_DryRunNoWrites(t *testing.T) {
	townRoot := t.TempDir()
	cwd := t.TempDir()
	writeLens(t, bridgeAgentsDir(townRoot), "security-lens.md", "SEC")

	out := captureStdout(t, func() {
		if err := syncBridgeLenses(townRoot, cwd, true); err != nil {
			t.Errorf("syncBridgeLenses dry-run: %v", err)
		}
	})

	if !strings.Contains(out, "would sync") || !strings.Contains(out, "security-lens.md") {
		t.Errorf("dry-run output missing planned copy: %q", out)
	}
	if _, err := os.Stat(cloneAgentsDir(cwd)); !os.IsNotExist(err) {
		t.Errorf("dry-run must not create dest dir (err=%v)", err)
	}
}

// basename-safety: only top-level *.md entries are mirrored — subdirectories and
// non-markdown files in the bridge dir are ignored.
func TestSyncBridgeLenses_IgnoresNonMarkdownAndSubdirs(t *testing.T) {
	townRoot := t.TempDir()
	cwd := t.TempDir()
	src := bridgeAgentsDir(townRoot)
	writeLens(t, src, "perf-lens.md", "PERF")
	writeLens(t, src, "README.txt", "not a lens")
	writeLens(t, filepath.Join(src, "nested"), "deep-lens.md", "should be ignored")

	if err := syncBridgeLenses(townRoot, cwd, false); err != nil {
		t.Fatalf("syncBridgeLenses: %v", err)
	}

	dest := cloneAgentsDir(cwd)
	if _, err := os.Stat(filepath.Join(dest, "perf-lens.md")); err != nil {
		t.Errorf("expected perf-lens.md to be synced: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.txt")); !os.IsNotExist(err) {
		t.Errorf("non-markdown file should not be synced (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "nested")); !os.IsNotExist(err) {
		t.Errorf("subdirectory should not be recursed/synced (err=%v)", err)
	}
}

// Defensive: empty townRoot or cwd is a no-op, never a panic or error.
func TestSyncBridgeLenses_EmptyArgs(t *testing.T) {
	if err := syncBridgeLenses("", t.TempDir(), false); err != nil {
		t.Errorf("empty townRoot: %v", err)
	}
	if err := syncBridgeLenses(t.TempDir(), "", false); err != nil {
		t.Errorf("empty cwd: %v", err)
	}
}
