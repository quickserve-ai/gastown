package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/crew"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/workspace"
)

var (
	ompUpgradeDryRun bool
	ompUpgradeForce  bool
	ompUpgradeRig    string
)

var ompCmd = &cobra.Command{
	Use:     "omp",
	GroupID: GroupServices,
	Short:   "Manage Oh My Pi runtimes used by Gas Town sessions",
}

var ompUpgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade OMP and restart idle unhooked OMP crew sessions",
	Long: `Upgrade the on-PATH omp binary, then restart only crew sessions that are:

  - running with GT_AGENT=omp (or an omp pane command fallback)
  - at an idle prompt
  - not assigned hooked or in-progress work

Busy or hooked OMP sessions are skipped. Non-OMP sessions are ignored. Run this
from a non-OMP context; replacing the binary under the running process is fine,
but restarting the process driving this command is not.`,
	RunE: runOmpUpgrade,
}

func init() {
	ompUpgradeCmd.Flags().BoolVar(&ompUpgradeDryRun, "dry-run", false, "Show upgrade and restart actions without changing anything")
	ompUpgradeCmd.Flags().BoolVar(&ompUpgradeForce, "force", false, "Run omp update --force even when --check reports no update")
	ompUpgradeCmd.Flags().StringVar(&ompUpgradeRig, "rig", "", "Only consider OMP crew sessions in this rig")
	ompCmd.AddCommand(ompUpgradeCmd)
	rootCmd.AddCommand(ompCmd)
}

type ompCrewRestartTarget struct {
	SessionName string
	Rig         string
	CrewName    string
}

type ompCrewRestartSkip struct {
	SessionName string
	Rig         string
	CrewName    string
	Reason      string
}

type ompCrewRestartPlan struct {
	Eligible []ompCrewRestartTarget
	Skipped  []ompCrewRestartSkip
}

type ompCrewInspector interface {
	AgentRuntime(session string) (string, error)
	IsIdle(session string) bool
	HookedWork(target string) (string, error)
}

func buildOmpCrewRestartPlan(agents []*AgentSession, inspector ompCrewInspector, rigFilter string) ompCrewRestartPlan {
	plan := ompCrewRestartPlan{}
	for _, agent := range agents {
		if agent == nil || agent.Type != AgentCrew {
			continue
		}
		if rigFilter != "" && agent.Rig != rigFilter {
			continue
		}

		runtimeName, err := inspector.AgentRuntime(agent.Name)
		if err != nil || runtimeName != string(config.AgentOmp) {
			continue
		}

		if !inspector.IsIdle(agent.Name) {
			plan.Skipped = append(plan.Skipped, ompCrewRestartSkip{
				SessionName: agent.Name,
				Rig:         agent.Rig,
				CrewName:    agent.AgentName,
				Reason:      "busy",
			})
			continue
		}

		target := fmt.Sprintf("%s/crew/%s", agent.Rig, agent.AgentName)
		hookedWork, err := inspector.HookedWork(target)
		if err != nil {
			plan.Skipped = append(plan.Skipped, ompCrewRestartSkip{
				SessionName: agent.Name,
				Rig:         agent.Rig,
				CrewName:    agent.AgentName,
				Reason:      "hook-check-failed",
			})
			continue
		}
		if hookedWork != "" {
			plan.Skipped = append(plan.Skipped, ompCrewRestartSkip{
				SessionName: agent.Name,
				Rig:         agent.Rig,
				CrewName:    agent.AgentName,
				Reason:      "hooked:" + hookedWork,
			})
			continue
		}

		plan.Eligible = append(plan.Eligible, ompCrewRestartTarget{
			SessionName: agent.Name,
			Rig:         agent.Rig,
			CrewName:    agent.AgentName,
		})
	}
	return plan
}

func parseOmpUpdateCheckOutput(output string) (current string, next string, updateAvailable bool) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Current version:") {
			current = strings.TrimSpace(strings.TrimPrefix(line, "Current version:"))
			current = strings.TrimPrefix(current, "omp/")
			continue
		}
		if strings.HasPrefix(line, "New version available:") {
			next = strings.TrimSpace(strings.TrimPrefix(line, "New version available:"))
			next = strings.TrimPrefix(next, "omp/")
			updateAvailable = next != ""
		}
	}
	return current, next, updateAvailable
}

type liveOmpCrewInspector struct {
	townRoot string
	tmux     *tmux.Tmux
}

func (i liveOmpCrewInspector) AgentRuntime(sessionName string) (string, error) {
	if agent, err := i.tmux.GetEnvironment(sessionName, "GT_AGENT"); err == nil && strings.TrimSpace(agent) != "" {
		return strings.TrimSpace(agent), nil
	}
	cmd, err := i.tmux.GetPaneCommand(sessionName)
	if err != nil {
		return "", err
	}
	if filepath.Base(strings.TrimSpace(cmd)) == string(config.AgentOmp) {
		return string(config.AgentOmp), nil
	}
	return strings.TrimSpace(cmd), nil
}

func (i liveOmpCrewInspector) IsIdle(sessionName string) bool {
	return i.tmux.IsIdle(sessionName)
}

func (i liveOmpCrewInspector) HookedWork(target string) (string, error) {
	return findHookedWorkIDForTarget(i.townRoot, target)
}

func findHookedWorkIDForTarget(townRoot, target string) (string, error) {
	if townRoot == "" {
		return "", fmt.Errorf("town root is required")
	}

	var workDirs []string
	if isTownLevelRole(target) {
		workDirs = append(workDirs, filepath.Join(townRoot, ".beads"))
	} else {
		parts := strings.Split(target, "/")
		if len(parts) == 0 || parts[0] == "" {
			return "", fmt.Errorf("invalid agent target %q", target)
		}
		rigName := parts[0]
		fallbackPath := filepath.Join(townRoot, rigName)
		agentBeadID := buildAgentBeadID(target, RoleUnknown, townRoot)
		if agentBeadID != "" {
			if resolved := beads.ResolveHookDir(townRoot, agentBeadID, fallbackPath); resolved != "" {
				workDirs = append(workDirs, resolved)
			}
		}
		workDirs = appendUniqueString(workDirs, fallbackPath)
		workDirs = appendUniqueString(workDirs, filepath.Join(townRoot, ".beads"))
	}

	for _, workDir := range workDirs {
		id, err := findHookedWorkInDir(workDir, target)
		if err != nil {
			return "", err
		}
		if id != "" {
			return id, nil
		}
	}
	return "", nil
}

func findHookedWorkInDir(workDir, target string) (string, error) {
	b := beads.New(workDir)
	for _, status := range []string{beads.StatusHooked, "in_progress"} {
		issues, err := b.List(beads.ListOptions{
			Status:   status,
			Assignee: target,
			Priority: -1,
		})
		if err != nil {
			return "", fmt.Errorf("listing %s work for %s in %s: %w", status, target, workDir, err)
		}
		if len(issues) > 0 {
			return issues[0].ID, nil
		}
	}
	return "", nil
}

func appendUniqueString(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func runOmpUpgrade(cmd *cobra.Command, args []string) error {
	if !ompUpgradeDryRun {
		if currentAgent := os.Getenv("GT_AGENT"); currentAgent == string(config.AgentOmp) {
			return fmt.Errorf("refusing to run OMP upgrade from an OMP-backed session; run from Claude/Gemini/Codex or a normal terminal")
		}
	}

	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	checkOut, err := runOmpCommand("update", "--check")
	if err != nil {
		return fmt.Errorf("checking OMP update: %w\n%s", err, checkOut)
	}
	current, next, updateAvailable := parseOmpUpdateCheckOutput(checkOut)

	agents, err := getAgentSessions(false)
	if err != nil {
		return fmt.Errorf("listing Gas Town sessions: %w", err)
	}
	inspector := liveOmpCrewInspector{townRoot: townRoot, tmux: tmux.NewTmux()}
	plan := buildOmpCrewRestartPlan(agents, inspector, ompUpgradeRig)

	printOmpUpgradePlan(current, next, updateAvailable, plan)

	if ompUpgradeDryRun {
		fmt.Printf("\n%s Dry run: no OMP update or session restart performed\n", style.WarningPrefix)
		return nil
	}

	if !updateAvailable && !ompUpgradeForce {
		fmt.Printf("\n%s OMP is already up to date; no sessions restarted\n", style.SuccessPrefix)
		return nil
	}

	updateArgs := []string{"update"}
	if ompUpgradeForce {
		updateArgs = append(updateArgs, "--force")
	}
	updateOut, err := runOmpCommand(updateArgs...)
	if strings.TrimSpace(updateOut) != "" {
		fmt.Println(strings.TrimSpace(updateOut))
	}
	if err != nil {
		return fmt.Errorf("updating OMP: %w", err)
	}

	// Re-evaluate after replacing the binary. A crew member can receive work,
	// start generating while omp update runs; the restart gate must reflect the
	// latest observed state, not the pre-update dry-run plan.
	agents, err = getAgentSessions(false)
	if err != nil {
		return fmt.Errorf("re-listing Gas Town sessions after OMP update: %w", err)
	}
	plan = buildOmpCrewRestartPlan(agents, inspector, ompUpgradeRig)
	if len(plan.Eligible) == 0 {
		fmt.Printf("\n%s OMP updated; no idle unhooked OMP crew sessions eligible for restart\n", style.WarningPrefix)
		return nil
	}

	return restartEligibleOmpCrew(plan.Eligible)
}

func runOmpCommand(args ...string) (string, error) {
	cmd := exec.Command("omp", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func printOmpUpgradePlan(current, next string, updateAvailable bool, plan ompCrewRestartPlan) {
	fmt.Printf("\n%s OMP upgrade\n", style.Bold.Render("gt omp upgrade"))
	if current != "" {
		fmt.Printf("  Current: %s\n", current)
	}
	if updateAvailable {
		fmt.Printf("  Latest:  %s\n", next)
	} else {
		fmt.Printf("  Latest:  %s\n", style.Dim.Render("already installed"))
	}

	fmt.Printf("\nEligible idle unhooked OMP crew sessions: %d\n", len(plan.Eligible))
	for _, target := range plan.Eligible {
		fmt.Printf("  %s %s/crew/%s (%s)\n", style.SuccessPrefix, target.Rig, target.CrewName, target.SessionName)
	}
	if len(plan.Skipped) > 0 {
		fmt.Printf("\nSkipped OMP crew sessions: %d\n", len(plan.Skipped))
		for _, skipped := range plan.Skipped {
			fmt.Printf("  %s %s/crew/%s (%s): %s\n", style.Dim.Render("○"), skipped.Rig, skipped.CrewName, skipped.SessionName, skipped.Reason)
		}
	}
}

func restartEligibleOmpCrew(targets []ompCrewRestartTarget) error {
	fmt.Printf("\nRestarting %d idle unhooked OMP crew session(s)...\n\n", len(targets))
	var failed int
	var failures []string
	for _, target := range targets {
		crewMgr, _, err := getCrewManager(target.Rig)
		if err != nil {
			failed++
			failures = append(failures, fmt.Sprintf("%s/crew/%s: %v", target.Rig, target.CrewName, err))
			fmt.Printf("  %s %s/crew/%s\n", style.ErrorPrefix, target.Rig, target.CrewName)
			continue
		}

		err = crewMgr.Start(target.CrewName, crew.StartOptions{
			KillExisting:  true,
			Topic:         "omp-upgrade",
			AgentOverride: string(config.AgentOmp),
		})
		if err != nil {
			failed++
			failures = append(failures, fmt.Sprintf("%s/crew/%s: %v", target.Rig, target.CrewName, err))
			fmt.Printf("  %s %s/crew/%s\n", style.ErrorPrefix, target.Rig, target.CrewName)
		} else {
			fmt.Printf("  %s %s/crew/%s\n", style.SuccessPrefix, target.Rig, target.CrewName)
		}
		time.Sleep(constants.ShutdownNotifyDelay)
	}
	if failed > 0 {
		for _, failure := range failures {
			fmt.Printf("  %s\n", style.Dim.Render(failure))
		}
		return fmt.Errorf("%d OMP crew restart(s) failed", failed)
	}
	fmt.Printf("\n%s Restarted %d OMP crew session(s)\n", style.SuccessPrefix, len(targets))
	return nil
}
