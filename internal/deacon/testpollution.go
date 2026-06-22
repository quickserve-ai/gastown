//go:build !windows

// Package deacon provides the Deacon agent infrastructure.
package deacon

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/steveyegge/gastown/internal/doltserver"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/tmux"
)

// TestPollutionResult holds counts from a test pollution cleanup run.
type TestPollutionResult struct {
	RogueDoltKilled      int
	StaleTestDirsRemoved int
	StalePIDFilesRemoved int
	DeadWorktreesPruned  int
}

// CleanTestPollution detects and removes runtime test pollution left by dead
// processes. Only items where the owning process is confirmed dead are removed.
// Errors from individual categories are logged but do not abort the overall run.
func CleanTestPollution(townRoot string) (TestPollutionResult, error) {
	var result TestPollutionResult

	killed, err := killRogueDoltServers(townRoot)
	result.RogueDoltKilled = killed
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: test-pollution: rogue dolt cleanup: %v\n", err)
	}

	dirs, err := cleanStaleTestTempDirs()
	result.StaleTestDirsRemoved = dirs
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: test-pollution: stale temp dirs: %v\n", err)
	}

	pids, err := cleanStalePIDFiles()
	result.StalePIDFilesRemoved = pids
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: test-pollution: stale PID files: %v\n", err)
	}

	worktrees, err := pruneDeadDogWorktrees(townRoot)
	result.DeadWorktreesPruned = worktrees
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: test-pollution: dead dog worktrees: %v\n", err)
	}

	return result, nil
}

// killRogueDoltServers kills any dolt sql-server using this workspace's port
// but serving a different data directory (an "imposter").
// Returns the number of imposters killed (0 or 1).
func killRogueDoltServers(townRoot string) (int, error) {
	cfg := doltserver.DefaultConfig(townRoot)
	if cfg.IsRemote() {
		return 0, nil // remote dolt — imposter detection requires local server
	}

	conflictPID, _ := doltserver.CheckPortConflict(townRoot)
	if conflictPID == 0 {
		return 0, nil
	}

	if err := doltserver.KillImposters(townRoot); err != nil {
		return 0, fmt.Errorf("killing imposter PID %d: %w", conflictPID, err)
	}
	return 1, nil
}

// cleanStaleTestTempDirs removes stale test temp directories in TMPDIR that
// match beads test patterns and have no open file handles (confirmed dead).
func cleanStaleTestTempDirs() (int, error) {
	tmpDir := os.Getenv("TMPDIR")
	if tmpDir == "" {
		tmpDir = "/tmp"
	}

	patterns := []string{
		filepath.Join(tmpDir, "beads-test-dolt-*"),
		filepath.Join(tmpDir, "beads-bd-tests-*"),
	}

	var cleaned int
	var errs []string

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			errs = append(errs, fmt.Sprintf("glob %q: %v", pattern, err))
			continue
		}

		for _, dir := range matches {
			info, err := os.Stat(dir)
			if err != nil || !info.IsDir() {
				continue
			}

			if isDirInUse(dir) {
				continue
			}

			// Ensure the directory is writable before removal
			_ = exec.Command("chmod", "-R", "u+w", dir).Run()
			if err := os.RemoveAll(dir); err != nil {
				errs = append(errs, fmt.Sprintf("remove %q: %v", dir, err))
				continue
			}
			cleaned++
		}
	}

	if len(errs) > 0 {
		return cleaned, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return cleaned, nil
}

// cleanStalePIDFiles removes PID files in /tmp whose recorded process is dead.
func cleanStalePIDFiles() (int, error) {
	patterns := []string{
		"/tmp/dolt-test-server-*.pid",
		"/tmp/beads-test-dolt-*.pid",
	}

	var cleaned int
	var errs []string

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			errs = append(errs, fmt.Sprintf("glob %q: %v", pattern, err))
			continue
		}

		for _, pidFile := range matches {
			data, err := os.ReadFile(pidFile)
			if err != nil {
				continue
			}

			pidStr := strings.TrimSpace(string(data))
			pid, err := strconv.Atoi(pidStr)
			if err != nil || pid <= 0 {
				continue
			}

			if isPIDAlive(pid) {
				continue
			}

			if err := os.Remove(pidFile); err != nil {
				errs = append(errs, fmt.Sprintf("remove %q: %v", pidFile, err))
				continue
			}
			cleaned++
		}
	}

	if len(errs) > 0 {
		return cleaned, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return cleaned, nil
}

// pruneDeadDogWorktrees removes git worktrees for dogs whose tmux sessions are dead.
func pruneDeadDogWorktrees(townRoot string) (int, error) {
	kennelDir := filepath.Join(townRoot, "deacon", "dogs")

	entries, err := os.ReadDir(kennelDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading kennel %q: %w", kennelDir, err)
	}

	t := tmux.NewTmux()
	var pruned int
	var errs []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dogName := entry.Name()
		sessionName := session.DogSessionName(dogName)

		alive, err := t.HasSession(sessionName)
		if err != nil {
			// tmux not available or other error — skip this dog
			continue
		}
		if alive {
			continue
		}

		dogDir := filepath.Join(kennelDir, dogName)
		n, err := pruneWorktreesInDogDir(dogDir)
		pruned += n
		if err != nil {
			errs = append(errs, fmt.Sprintf("dog %q: %v", dogName, err))
		}
	}

	if len(errs) > 0 {
		return pruned, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return pruned, nil
}

// pruneWorktreesInDogDir removes git worktrees found directly inside dogDir.
// Each subdirectory that contains a ".git" entry is treated as a worktree.
func pruneWorktreesInDogDir(dogDir string) (int, error) {
	entries, err := os.ReadDir(dogDir)
	if err != nil {
		return 0, err
	}

	var pruned int
	var errs []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		worktreePath := filepath.Join(dogDir, entry.Name())

		// A worktree has a .git file (or directory); skip if absent.
		if _, err := os.Stat(filepath.Join(worktreePath, ".git")); os.IsNotExist(err) {
			continue
		}

		mainGitDir, err := resolveGitCommonDir(worktreePath)
		if err != nil || mainGitDir == "" {
			continue
		}

		// WorktreeRemove must be run from the main repo (bare) or its working tree.
		// Use the parent of the common .git dir as the working directory.
		repoWorkDir := mainGitDir
		if !isBareRepo(mainGitDir) {
			repoWorkDir = filepath.Dir(mainGitDir)
		}

		g := git.NewGit(repoWorkDir)
		if err := g.WorktreeRemove(worktreePath, true); err != nil {
			errs = append(errs, fmt.Sprintf("worktree remove %q: %v", worktreePath, err))
			continue
		}
		_ = g.WorktreePrune()
		pruned++
	}

	if len(errs) > 0 {
		return pruned, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return pruned, nil
}

// resolveGitCommonDir runs "git rev-parse --git-common-dir" inside worktreePath
// to find the path to the common .git directory of the repository.
// Returns an absolute path.
func resolveGitCommonDir(worktreePath string) (string, error) {
	cmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return "", nil
	}
	if filepath.IsAbs(raw) {
		return raw, nil
	}
	// Relative path is relative to worktreePath
	return filepath.Abs(filepath.Join(worktreePath, raw))
}

// isBareRepo returns true when gitDir is itself a bare repository (no parent
// working tree). A bare repo directory typically contains HEAD, objects/, etc.
// and is NOT nested inside a working tree as ".git/".
func isBareRepo(gitDir string) bool {
	// Bare repos have HEAD at their root and no parent working tree.
	// Heuristic: if the path ends with ".git" it is a conventional bare repo
	// clone; otherwise check for the presence of HEAD + objects/ without a
	// parent .git file.
	if strings.HasSuffix(gitDir, ".git") || strings.HasSuffix(gitDir, ".git/") {
		return true
	}
	// Check for HEAD + objects/ as bare-repo indicators
	_, errHead := os.Stat(filepath.Join(gitDir, "HEAD"))
	_, errObjs := os.Stat(filepath.Join(gitDir, "objects"))
	return errHead == nil && errObjs == nil
}

// isDirInUse returns true if any process has open files inside dir.
// Uses lsof(1) which is available on macOS and most Linux distributions.
// Returns false on error (fail-open: safe to remove).
func isDirInUse(dir string) bool {
	cmd := exec.Command("lsof", "+D", dir)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	// lsof prints a header line even with no results on some versions;
	// non-empty trimmed output after the header means files are open.
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	return len(lines) > 1
}

// isPIDAlive returns true if the given PID refers to a live process.
func isPIDAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
