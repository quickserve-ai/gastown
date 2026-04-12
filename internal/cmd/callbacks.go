package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/deacon"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

var callbacksCmd = &cobra.Command{
	Use:     "callbacks",
	GroupID: GroupAgents,
	Short:   "Handle agent callbacks",
	Long: `Handle callbacks from agents during Deacon patrol.

Callbacks are messages sent to the Mayor from:
- Witnesses reporting polecat status
- Refineries reporting merge results
- Polecats requesting help or escalation
- External triggers (webhooks, timers)

This command processes the Mayor's inbox and handles each message
appropriately, routing to other agents or updating state as needed.`,
}

var callbacksProcessCmd = &cobra.Command{
	Use:   "process",
	Short: "Process pending callbacks",
	Long: `Process all pending callbacks in the Mayor's inbox.

Reads unread messages from the Mayor's inbox and handles each based on
its type:

  POLECAT_DONE       - Log completion, update stats
  MERGE_COMPLETED    - Notify worker, close source issue
  MERGE_REJECTED     - Notify worker of rejection reason
  HELP:              - Route to human or handle if possible
  ESCALATION:        - Log and route to human
  SLING_REQUEST:     - Spawn polecat for the work

Note: Witnesses and Refineries handle routine operations autonomously.
They only send escalations for genuine problems, not status reports.

Unknown message types are logged but left unprocessed.`,
	RunE: runCallbacksProcess,
}

var (
	callbacksDryRun  bool
	callbacksVerbose bool
)

func init() {
	callbacksProcessCmd.Flags().BoolVar(&callbacksDryRun, "dry-run", false, "Show what would be processed without taking action")
	callbacksProcessCmd.Flags().BoolVarP(&callbacksVerbose, "verbose", "v", false, "Show detailed processing info")

	callbacksCmd.AddCommand(callbacksProcessCmd)
	rootCmd.AddCommand(callbacksCmd)
}

func runCallbacksProcess(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	result, err := deacon.ProcessCallbacks(townRoot, callbacksDryRun)
	if err != nil {
		return err
	}

	if len(result.ProcessResults) == 0 {
		fmt.Printf("%s No pending callbacks\n", style.Dim.Render("○"))
		return nil
	}

	fmt.Printf("%s Processing %d callback(s)\n", style.Bold.Render("●"), len(result.ProcessResults)+result.Skipped)

	for _, pr := range result.ProcessResults {
		if pr.Error != "" {
			fmt.Printf("  %s %s: %s\n",
				style.Error.Render("✗"),
				pr.Subject,
				pr.Error)
		} else if pr.Handled {
			fmt.Printf("  %s [%s] %s\n",
				style.Bold.Render("✓"),
				pr.CallbackType,
				pr.Action)
		} else {
			fmt.Printf("  %s [%s] %s\n",
				style.Dim.Render("○"),
				pr.CallbackType,
				pr.Action)
		}

		if callbacksVerbose {
			fmt.Printf("      From: %s\n", pr.From)
			fmt.Printf("      Subject: %s\n", pr.Subject)
		}
	}

	fmt.Println()
	if callbacksDryRun {
		fmt.Printf("%s Dry run: would process %d/%d callbacks\n",
			style.Dim.Render("○"), result.Processed, result.Processed+result.Failed)
	} else {
		fmt.Printf("%s Processed %d/%d callbacks",
			style.Bold.Render("✓"), result.Processed, result.Processed+result.Failed)
		if result.Failed > 0 {
			fmt.Printf(" (%d errors)", result.Failed)
		}
		fmt.Println()
	}

	return nil
}
