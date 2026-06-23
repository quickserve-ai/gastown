package rig

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCopyOverlay_NoOverlayDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	destDir := t.TempDir()

	// No overlay directory exists
	err := CopyOverlay(tmpDir, destDir)
	if err != nil {
		t.Errorf("CopyOverlay() with no overlay directory should return nil, got %v", err)
	}
}

func TestCopyOverlay_CopiesFiles(t *testing.T) {
	rigDir := t.TempDir()
	destDir := t.TempDir()

	// Create overlay directory with test files
	overlayDir := filepath.Join(rigDir, ".runtime", "overlay")
	if err := os.MkdirAll(overlayDir, 0755); err != nil {
		t.Fatalf("Failed to create overlay dir: %v", err)
	}

	// Create test files
	testFile1 := filepath.Join(overlayDir, "test1.txt")
	testFile2 := filepath.Join(overlayDir, "test2.txt")

	if err := os.WriteFile(testFile1, []byte("content1"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := os.WriteFile(testFile2, []byte("content2"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Copy overlay
	err := CopyOverlay(rigDir, destDir)
	if err != nil {
		t.Fatalf("CopyOverlay() error = %v", err)
	}

	// Verify files were copied
	destFile1 := filepath.Join(destDir, "test1.txt")
	destFile2 := filepath.Join(destDir, "test2.txt")

	content1, err := os.ReadFile(destFile1)
	if err != nil {
		t.Errorf("File test1.txt was not copied: %v", err)
	}
	if string(content1) != "content1" {
		t.Errorf("test1.txt content = %q, want %q", string(content1), "content1")
	}

	content2, err := os.ReadFile(destFile2)
	if err != nil {
		t.Errorf("File test2.txt was not copied: %v", err)
	}
	if string(content2) != "content2" {
		t.Errorf("test2.txt content = %q, want %q", string(content2), "content2")
	}
}

func TestCopyOverlay_PreservesPermissions(t *testing.T) {
	rigDir := t.TempDir()
	destDir := t.TempDir()

	// Create overlay directory with a file
	overlayDir := filepath.Join(rigDir, ".runtime", "overlay")
	if err := os.MkdirAll(overlayDir, 0755); err != nil {
		t.Fatalf("Failed to create overlay dir: %v", err)
	}

	testFile := filepath.Join(overlayDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0755); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Copy overlay
	err := CopyOverlay(rigDir, destDir)
	if err != nil {
		t.Fatalf("CopyOverlay() error = %v", err)
	}

	// Verify permissions were preserved
	srcInfo, _ := os.Stat(testFile)
	destInfo, err := os.Stat(filepath.Join(destDir, "test.txt"))
	if err != nil {
		t.Fatalf("Failed to stat destination file: %v", err)
	}

	if srcInfo.Mode().Perm() != destInfo.Mode().Perm() {
		t.Errorf("Permissions not preserved: src=%v, dest=%v", srcInfo.Mode(), destInfo.Mode())
	}
}

func TestCopyOverlay_SkipsSubdirectories(t *testing.T) {
	rigDir := t.TempDir()
	destDir := t.TempDir()

	// Create overlay directory with a subdirectory
	overlayDir := filepath.Join(rigDir, ".runtime", "overlay")
	if err := os.MkdirAll(overlayDir, 0755); err != nil {
		t.Fatalf("Failed to create overlay dir: %v", err)
	}

	// Create a subdirectory
	subDir := filepath.Join(overlayDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	// Create a file in the overlay root
	testFile := filepath.Join(overlayDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a file in the subdirectory
	subFile := filepath.Join(subDir, "sub.txt")
	if err := os.WriteFile(subFile, []byte("subcontent"), 0644); err != nil {
		t.Fatalf("Failed to create sub file: %v", err)
	}

	// Copy overlay
	err := CopyOverlay(rigDir, destDir)
	if err != nil {
		t.Fatalf("CopyOverlay() error = %v", err)
	}

	// Verify root file was copied
	if _, err := os.Stat(filepath.Join(destDir, "test.txt")); err != nil {
		t.Error("Root file should be copied")
	}

	// Verify subdirectory was NOT copied
	if _, err := os.Stat(filepath.Join(destDir, "subdir")); err == nil {
		t.Error("Subdirectory should not be copied")
	}
	if _, err := os.Stat(filepath.Join(destDir, "subdir", "sub.txt")); err == nil {
		t.Error("File in subdirectory should not be copied")
	}
}

func TestCopyOverlay_EmptyOverlay(t *testing.T) {
	rigDir := t.TempDir()
	destDir := t.TempDir()

	// Create empty overlay directory
	overlayDir := filepath.Join(rigDir, ".runtime", "overlay")
	if err := os.MkdirAll(overlayDir, 0755); err != nil {
		t.Fatalf("Failed to create overlay dir: %v", err)
	}

	// Copy overlay
	err := CopyOverlay(rigDir, destDir)
	if err != nil {
		t.Fatalf("CopyOverlay() error = %v", err)
	}

	// Should succeed without errors
}

func TestCopyOverlay_OverwritesExisting(t *testing.T) {
	rigDir := t.TempDir()
	destDir := t.TempDir()

	// Create overlay directory with test file
	overlayDir := filepath.Join(rigDir, ".runtime", "overlay")
	if err := os.MkdirAll(overlayDir, 0755); err != nil {
		t.Fatalf("Failed to create overlay dir: %v", err)
	}

	testFile := filepath.Join(overlayDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("new content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create existing file in destination with different content
	destFile := filepath.Join(destDir, "test.txt")
	if err := os.WriteFile(destFile, []byte("old content"), 0644); err != nil {
		t.Fatalf("Failed to create dest file: %v", err)
	}

	// Copy overlay
	err := CopyOverlay(rigDir, destDir)
	if err != nil {
		t.Fatalf("CopyOverlay() error = %v", err)
	}

	// Verify file was overwritten
	content, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("Failed to read dest file: %v", err)
	}
	if string(content) != "new content" {
		t.Errorf("File content = %q, want %q", string(content), "new content")
	}
}

func TestCopyFilePreserveMode(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file
	srcFile := filepath.Join(tmpDir, "src.txt")
	if err := os.WriteFile(srcFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create src file: %v", err)
	}

	// Copy file
	dstFile := filepath.Join(tmpDir, "dst.txt")
	err := copyFilePreserveMode(srcFile, dstFile)
	if err != nil {
		t.Fatalf("copyFilePreserveMode() error = %v", err)
	}

	// Verify content
	content, err := os.ReadFile(dstFile)
	if err != nil {
		t.Errorf("Failed to read dst file: %v", err)
	}
	if string(content) != "test content" {
		t.Errorf("Content = %q, want %q", string(content), "test content")
	}

	// Verify permissions
	srcInfo, _ := os.Stat(srcFile)
	dstInfo, err := os.Stat(dstFile)
	if err != nil {
		t.Fatalf("Failed to stat dst file: %v", err)
	}
	if srcInfo.Mode().Perm() != dstInfo.Mode().Perm() {
		t.Errorf("Permissions not preserved: src=%v, dest=%v", srcInfo.Mode(), dstInfo.Mode())
	}
}

func TestCopyFilePreserveMode_NonexistentSource(t *testing.T) {
	tmpDir := t.TempDir()

	srcFile := filepath.Join(tmpDir, "nonexistent.txt")
	dstFile := filepath.Join(tmpDir, "dst.txt")

	err := copyFilePreserveMode(srcFile, dstFile)
	if err == nil {
		t.Error("copyFilePreserveMode() with nonexistent source should return error")
	}
}

func TestEnsureGitignorePatterns_CreatesNewFile(t *testing.T) {
	tmpDir := t.TempDir()

	err := EnsureGitignorePatterns(tmpDir)
	if err != nil {
		t.Fatalf("EnsureGitignorePatterns() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}

	// Check all required patterns are present (.beads/ intentionally excluded — see overlay.go)
	patterns := []string{".runtime/", ".claude/", ".logs/", "__pycache__/", "state.json"}
	for _, pattern := range patterns {
		if !containsLine(string(content), pattern) {
			t.Errorf(".gitignore missing pattern %q", pattern)
		}
	}
}

func TestEnsureGitignorePatterns_AppendsToExisting(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing .gitignore with some content
	existing := "node_modules/\n*.log\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatalf("Failed to create .gitignore: %v", err)
	}

	err := EnsureGitignorePatterns(tmpDir)
	if err != nil {
		t.Fatalf("EnsureGitignorePatterns() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}

	// Should preserve existing content
	if !containsLine(string(content), "node_modules/") {
		t.Error("Existing pattern node_modules/ was removed")
	}

	// Should add header
	if !containsLine(string(content), "# Gas Town (added by gt)") {
		t.Error("Missing Gas Town header comment")
	}

	// Should add required patterns (.beads/ intentionally excluded — see overlay.go)
	patterns := []string{".runtime/", ".claude/", ".logs/", "__pycache__/", "state.json"}
	for _, pattern := range patterns {
		if !containsLine(string(content), pattern) {
			t.Errorf(".gitignore missing pattern %q", pattern)
		}
	}
}

func TestEnsureGitignorePatterns_SkipsExistingPatterns(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing .gitignore with some Gas Town patterns already.
	// The broader ".claude/" covers ".claude/commands/", so it should
	// not add the narrower pattern.
	existing := ".runtime/\n.claude/\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatalf("Failed to create .gitignore: %v", err)
	}

	err := EnsureGitignorePatterns(tmpDir)
	if err != nil {
		t.Fatalf("EnsureGitignorePatterns() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}

	// Should not duplicate existing patterns
	count := countOccurrences(string(content), ".runtime/")
	if count != 1 {
		t.Errorf(".runtime/ appears %d times, expected 1", count)
	}

	// .claude/ is now a direct required pattern — should not be duplicated
	claudeCount := countOccurrences(string(content), ".claude/")
	if claudeCount != 1 {
		t.Errorf(".claude/ appears %d times, expected 1", claudeCount)
	}

	// Should add missing patterns
	if !containsLine(string(content), ".logs/") {
		t.Error(".gitignore missing pattern .logs/")
	}
	if !containsLine(string(content), "__pycache__/") {
		t.Error(".gitignore missing pattern __pycache__/")
	}
	if !containsLine(string(content), "state.json") {
		t.Error(".gitignore missing pattern state.json")
	}

	// Regression guard: .beads/ must NOT be in required patterns.
	// Beads manages its own .beads/.gitignore via bd init.
	// Adding .beads/ here breaks bd sync. This has regressed twice
	// (PR #753, #966). If this test fails, you're about to break polecats.
	if containsLine(string(content), ".beads/") {
		t.Error(".gitignore must NOT contain .beads/ - beads manages its own .gitignore (see overlay.go comment)")
	}
}

func TestEnsureGitignorePatterns_RecognizesVariants(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing .gitignore with variant patterns (without trailing slash).
	// ".claude" (no trailing slash) should be recognized as covering ".claude/commands/".
	existing := ".runtime\n/.claude\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatalf("Failed to create .gitignore: %v", err)
	}

	err := EnsureGitignorePatterns(tmpDir)
	if err != nil {
		t.Fatalf("EnsureGitignorePatterns() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}

	// Should recognize variants and not add duplicates
	// .runtime (no slash) should count as .runtime/
	runtimeCount := countOccurrences(string(content), ".runtime")
	if runtimeCount > 1 {
		t.Errorf(".runtime appears %d times (variant detection failed)", runtimeCount)
	}

	// /.claude (leading slash, no trailing slash) should cover .claude/
	if containsLine(string(content), ".claude/") {
		t.Error(".claude/ should not be added when /.claude already covers it")
	}
}

func TestEnsureGitignorePatterns_AllPatternsPresent(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing .gitignore with all required patterns.
	existing := ".runtime/\n.claude/\n.beads/\n.logs/\n__pycache__/\nstate.json\nCLAUDE.md\nCLAUDE.local.md\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatalf("Failed to create .gitignore: %v", err)
	}

	err := EnsureGitignorePatterns(tmpDir)
	if err != nil {
		t.Fatalf("EnsureGitignorePatterns() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}

	// File should be unchanged (no header added)
	if containsLine(string(content), "# Gas Town") {
		t.Error("Should not add header when all patterns already present")
	}

	// Content should match original
	if string(content) != existing {
		t.Errorf("File was modified when it shouldn't be.\nGot: %q\nWant: %q", string(content), existing)
	}
}

func TestEnsureGitignorePatterns_NarrowPatternPresent(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .gitignore with the exact required patterns
	existing := ".runtime/\n.claude/\n.logs/\n__pycache__/\nstate.json\nCLAUDE.md\nCLAUDE.local.md\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatalf("Failed to create .gitignore: %v", err)
	}

	err := EnsureGitignorePatterns(tmpDir)
	if err != nil {
		t.Fatalf("EnsureGitignorePatterns() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}

	// File should be unchanged
	if string(content) != existing {
		t.Errorf("File was modified when it shouldn't be.\nGot: %q\nWant: %q", string(content), existing)
	}
}

func TestEnsureGitignorePatterns_OldNarrowClaudeUpgraded(t *testing.T) {
	tmpDir := t.TempDir()

	// Simulate old installation with narrow .claude/commands/ pattern.
	// After upgrade, .claude/ (broad) should be added since .claude/commands/
	// does NOT cover .claude/ (the narrow is a subset, not a superset).
	existing := ".runtime/\n.claude/commands/\n.logs/\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatalf("Failed to create .gitignore: %v", err)
	}

	err := EnsureGitignorePatterns(tmpDir)
	if err != nil {
		t.Fatalf("EnsureGitignorePatterns() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}

	// .claude/ should be added (old .claude/commands/ doesn't cover it)
	if !containsLine(string(content), ".claude/") {
		t.Error(".claude/ should be added when only .claude/commands/ was present")
	}

	// __pycache__/ should be added
	if !containsLine(string(content), "__pycache__/") {
		t.Error("__pycache__/ should be added")
	}
}

func TestEnsureGitignorePatterns_UpgradePreservesBroadPattern(t *testing.T) {
	tmpDir := t.TempDir()

	// Simulate an existing installation that has .claude/ plus other Gas Town
	// patterns but is missing __pycache__/ (added later). After upgrade,
	// __pycache__/ should be appended.
	existing := "# Gas Town (added by gt)\n.runtime/\n.claude/\n.logs/\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatalf("Failed to create .gitignore: %v", err)
	}

	err := EnsureGitignorePatterns(tmpDir)
	if err != nil {
		t.Fatalf("EnsureGitignorePatterns() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}

	// __pycache__/ should be appended
	if !containsLine(string(content), "__pycache__/") {
		t.Error("__pycache__/ should be added during upgrade")
	}

	// Existing patterns should be preserved
	if !containsLine(string(content), ".runtime/") {
		t.Error(".runtime/ should be preserved")
	}
	if !containsLine(string(content), ".claude/") {
		t.Error(".claude/ should be preserved")
	}
}

// TestGasTownLocalExcludePatterns_IncludesBeads verifies that the local exclude
// patterns include .beads/ (defense-in-depth for gas-7vg) while the gitignore
// patterns do NOT include .beads/ (regression guard).
func TestGasTownLocalExcludePatterns_IncludesBeads(t *testing.T) {
	localPatterns := gasTownLocalExcludePatterns()
	found := false
	for _, p := range localPatterns {
		if p == ".beads/" {
			found = true
			break
		}
	}
	if !found {
		t.Error("gasTownLocalExcludePatterns() must include .beads/ (gas-7vg defense-in-depth)")
	}

	// Regression guard: .gitignore patterns must NOT include .beads/
	gitignorePatterns := gasTownIgnorePatterns()
	for _, p := range gitignorePatterns {
		if p == ".beads/" {
			t.Error("gasTownIgnorePatterns() must NOT include .beads/ - that breaks bd sync (see overlay.go)")
		}
	}
}

// TestEnsureGitignorePatterns_RespectsClaudeStarCarveOut verifies that a repo
// which carves out .claude/ via ".claude/*" + "!.claude/agents/" does NOT get a
// blanket ".claude/" appended after the negations (which last-match-wins would
// shadow, breaking git add for tracked carve-out siblings). Regression for the
// q-core recurring refinery PR reported by qcore/cyril (2026-06-22).
func TestEnsureGitignorePatterns_RespectsClaudeStarCarveOut(t *testing.T) {
	tmpDir := t.TempDir()

	// All required patterns are present (so only .claude/ is in question), with
	// the .claude carve-out expressed as the q-core .gitignore does it.
	existing := ".runtime/\n.claude/*\n!.claude/agents/\n!.claude/skills/\n.logs/\n__pycache__/\nstate.json\nCLAUDE.md\nCLAUDE.local.md\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatalf("Failed to create .gitignore: %v", err)
	}

	if err := EnsureGitignorePatterns(tmpDir); err != nil {
		t.Fatalf("EnsureGitignorePatterns() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, ".gitignore"))
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}

	// A blanket ".claude/" line must NOT be appended — ".claude/*" already covers it.
	if containsLine(string(content), ".claude/") {
		t.Error("blanket .claude/ must not be appended when .claude/* carve-out is present (would shadow !.claude/agents/)")
	}
	// And the file should be untouched entirely (no header, no new lines).
	if string(content) != existing {
		t.Errorf("File was modified when it shouldn't be.\nGot:  %q\nWant: %q", string(content), existing)
	}
}

// TestEnsureGitignorePatterns_SkipsTrackedPaths verifies gt never adds a
// .gitignore entry for a path the repo already tracks (e.g. CLAUDE.md at root,
// or .claude/ config files). Regression for qcore/cyril (2026-06-22).
func TestEnsureGitignorePatterns_SkipsTrackedPaths(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "test")

	// Track CLAUDE.md at root and a file under .claude/ (no carve-out glob here,
	// so only the tracked-path guard can prevent the blanket entries).
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# proj\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".claude", "agents"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude", "agents", "a.md"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "CLAUDE.md", ".claude/agents/a.md")

	// Minimal .gitignore with no .claude/ or CLAUDE.md handling.
	existing := ".runtime/\n.logs/\n__pycache__/\nstate.json\nCLAUDE.local.md\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureGitignorePatterns(dir); err != nil {
		t.Fatalf("EnsureGitignorePatterns() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if containsLine(string(content), ".claude/") {
		t.Error("blanket .claude/ must not be appended when repo tracks .claude/ files")
	}
	if containsLine(string(content), "CLAUDE.md") {
		t.Error("CLAUDE.md must not be appended when it is tracked at repo root")
	}
}

// TestEnsureGitignorePatterns_AppendsForUntrackedRepo verifies the common case is
// unchanged: a git repo that tracks none of the Gas Town paths still gets them.
func TestEnsureGitignorePatterns_AppendsForUntrackedRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")

	// A tracked unrelated file, but none of the Gas Town paths.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "README.md")

	if err := EnsureGitignorePatterns(dir); err != nil {
		t.Fatalf("EnsureGitignorePatterns() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".runtime/", ".claude/", ".logs/", "__pycache__/", "state.json", "CLAUDE.md"} {
		if !containsLine(string(content), p) {
			t.Errorf("untracked repo should still get pattern %q", p)
		}
	}
}

func TestMatchesGitignorePattern_StarGlobCoversDir(t *testing.T) {
	cases := []struct {
		line, pattern string
		want          bool
	}{
		{".claude/*", ".claude/", true},
		{".claude/*", ".claude", true},
		{".runtime/*", ".runtime/", true},
		{".claude/*", ".logs/", false},
		{".claude/commands/*", ".claude/", false}, // narrower glob does not cover the parent
	}
	for _, c := range cases {
		if got := matchesGitignorePattern(c.line, c.pattern); got != c.want {
			t.Errorf("matchesGitignorePattern(%q, %q) = %v, want %v", c.line, c.pattern, got, c.want)
		}
	}
}

// Helper functions

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func containsLine(content, pattern string) bool {
	for _, line := range splitLines(content) {
		if line == pattern {
			return true
		}
	}
	return false
}

func countOccurrences(content, pattern string) int {
	count := 0
	for _, line := range splitLines(content) {
		if line == pattern {
			count++
		}
	}
	return count
}

func splitLines(content string) []string {
	var lines []string
	start := 0
	for i, c := range content {
		if c == '\n' {
			lines = append(lines, content[start:i])
			start = i + 1
		}
	}
	if start < len(content) {
		lines = append(lines, content[start:])
	}
	return lines
}
