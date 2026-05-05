package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/reaper"
	"github.com/steveyegge/gastown/internal/style"
)

var (
	reaperDB       string
	reaperHost     string
	reaperPort     int
	reaperMaxAge   string
	reaperPurgeAge string
	reaperMailAge  string
	reaperStaleAge string
	reaperAckAge   string
	reaperDryRun   bool
	reaperJSON     bool
)

var reaperCmd = &cobra.Command{
	Use:     "reaper",
	GroupID: GroupServices,
	Short:   "Wisp and issue cleanup operations (Dog-callable helpers)",
	Long: `Execute wisp reaper operations against Dolt databases.

These subcommands are the callable helper functions for the mol-dog-reaper
formula. They execute SQL operations but leave eligibility decisions to the
Dog agent or daemon orchestrator.

When run by a Dog:
  gt reaper scan --db=gastown          # Discover candidates
  gt reaper reap --db=gastown          # Close stale wisps
  gt reaper purge --db=gastown         # Delete old closed wisps + mail
  gt reaper auto-close --db=gastown    # Close stale issues`,
	RunE: requireSubcommand,
}

var reaperDatabasesCmd = &cobra.Command{
	Use:   "databases",
	Short: "List databases available for reaping",
	RunE: func(cmd *cobra.Command, args []string) error {
		dbs := reaper.DiscoverDatabases(reaperHost, reaperPort)
		if reaperJSON {
			fmt.Println(reaper.FormatJSON(dbs))
		} else {
			for _, db := range dbs {
				fmt.Println(db)
			}
		}
		return nil
	},
}

var reaperScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan databases for reaper candidates",
	Long: `Count reap, purge, auto-close, and mail candidates in databases.

When --db is provided, scans a single database. When omitted, auto-discovers
all databases on the Dolt server and scans each one, printing a summary.

Returns counts and anomaly detection results without modifying any data.
The Dog uses this to understand the state before deciding what to reap.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		maxAge, err := time.ParseDuration(reaperMaxAge)
		if err != nil {
			return fmt.Errorf("invalid --max-age: %w", err)
		}
		purgeAge, err := time.ParseDuration(reaperPurgeAge)
		if err != nil {
			return fmt.Errorf("invalid --purge-age: %w", err)
		}
		mailAge, err := time.ParseDuration(reaperMailAge)
		if err != nil {
			return fmt.Errorf("invalid --mail-age: %w", err)
		}
		staleAge, err := time.ParseDuration(reaperStaleAge)
		if err != nil {
			return fmt.Errorf("invalid --stale-age: %w", err)
		}

		databases := reaper.DiscoverDatabases(reaperHost, reaperPort)
		if reaperDB != "" {
			databases = strings.Split(reaperDB, ",")
		}

		var results []*reaper.ScanResult
		for _, dbName := range databases {
			if err := reaper.ValidateDBName(dbName); err != nil {
				fmt.Fprintf(os.Stderr, "skip invalid db: %s\n", dbName)
				continue
			}

			db, err := reaper.OpenDB(reaperHost, reaperPort, dbName, 10*time.Second, 10*time.Second)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: connect error: %v\n", dbName, err)
				continue
			}

			if ok, err := reaper.HasReaperSchema(db); err != nil {
				fmt.Fprintf(os.Stderr, "%s: schema check error: %v\n", dbName, err)
				db.Close()
				continue
			} else if !ok {
				db.Close()
				continue
			}

			result, err := reaper.Scan(db, dbName, maxAge, purgeAge, mailAge, staleAge)
			db.Close()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: scan error: %v\n", dbName, err)
				continue
			}
			results = append(results, result)
		}

		if reaperJSON {
			fmt.Println(reaper.FormatJSON(results))
		} else {
			var totalReap, totalPurge, totalMail, totalStale, totalOpen int
			for _, r := range results {
				fmt.Printf("Database: %s\n", r.Database)
				fmt.Printf("  Reap candidates:  %d\n", r.ReapCandidates)
				fmt.Printf("  Purge candidates: %d\n", r.PurgeCandidates)
				fmt.Printf("  Mail candidates:  %d\n", r.MailCandidates)
				fmt.Printf("  Stale candidates: %d\n", r.StaleCandidates)
				fmt.Printf("  Open wisps:       %d\n", r.OpenWisps)
				for _, a := range r.Anomalies {
					fmt.Printf("  %s %s\n", style.Warning.Render("ANOMALY:"), a.Message)
				}
				totalReap += r.ReapCandidates
				totalPurge += r.PurgeCandidates
				totalMail += r.MailCandidates
				totalStale += r.StaleCandidates
				totalOpen += r.OpenWisps
			}
			if len(results) > 1 {
				fmt.Printf("\nScan summary (%d databases):\n", len(results))
				fmt.Printf("  Reap candidates:  %d\n", totalReap)
				fmt.Printf("  Purge candidates: %d\n", totalPurge)
				fmt.Printf("  Mail candidates:  %d\n", totalMail)
				fmt.Printf("  Stale candidates: %d\n", totalStale)
				fmt.Printf("  Open wisps:       %d\n", totalOpen)
			}
		}
		return nil
	},
}

var reaperReapCmd = &cobra.Command{
	Use:   "reap",
	Short: "Close stale wisps past max-age",
	Long: `Close wisps that are past the max-age threshold and whose parent
molecule is already closed (or missing/orphaned).

When --db is provided, reaps a single database. When omitted, auto-discovers
all databases on the Dolt server and reaps each one.

Returns the count of reaped wisps. Use --dry-run to preview.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		maxAge, err := time.ParseDuration(reaperMaxAge)
		if err != nil {
			return fmt.Errorf("invalid --max-age: %w", err)
		}

		databases := reaper.DiscoverDatabases(reaperHost, reaperPort)
		if reaperDB != "" {
			databases = strings.Split(reaperDB, ",")
		}

		var results []*reaper.ReapResult
		for _, dbName := range databases {
			if err := reaper.ValidateDBName(dbName); err != nil {
				fmt.Fprintf(os.Stderr, "skip invalid db: %s\n", dbName)
				continue
			}

			db, err := reaper.OpenDB(reaperHost, reaperPort, dbName, 10*time.Second, 10*time.Second)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: connect error: %v\n", dbName, err)
				continue
			}

			if ok, err := reaper.HasReaperSchema(db); err != nil {
				fmt.Fprintf(os.Stderr, "%s: schema check error: %v\n", dbName, err)
				db.Close()
				continue
			} else if !ok {
				db.Close()
				continue
			}

			result, err := reaper.Reap(db, dbName, maxAge, reaperDryRun)
			db.Close()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: reap error: %v\n", dbName, err)
				continue
			}
			results = append(results, result)
		}

		if reaperJSON {
			fmt.Println(reaper.FormatJSON(results))
		} else {
			var totalReaped, totalOpen int
			for _, r := range results {
				prefix := ""
				if r.DryRun {
					prefix = "[DRY RUN] would "
				}
				fmt.Printf("%s: %sreaped %d wisps, %d open remain\n",
					r.Database, prefix, r.Reaped, r.OpenRemain)
				totalReaped += r.Reaped
				totalOpen += r.OpenRemain
			}
			if len(results) > 1 {
				prefix := ""
				if reaperDryRun {
					prefix = "[DRY RUN] "
				}
				fmt.Printf("\n%sReap summary (%d databases): reaped %d wisps, %d open remain\n",
					prefix, len(results), totalReaped, totalOpen)
				if totalOpen > 600 {
					fmt.Fprintf(os.Stderr, "WARNING: %d open wisps exceed alert threshold (600)\n", totalOpen)
				}
			}
		}
		return nil
	},
}

var reaperPurgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Delete old closed wisps and mail",
	Long: `Delete closed wisps past the purge-age threshold and closed mail
past the mail-age threshold. Irreversible operation.

When --db is provided, purges a single database. When omitted, auto-discovers
all databases on the Dolt server and purges each one.

Returns counts of purged rows. Use --dry-run to preview.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		purgeAge, err := time.ParseDuration(reaperPurgeAge)
		if err != nil {
			return fmt.Errorf("invalid --purge-age: %w", err)
		}
		mailAge, err := time.ParseDuration(reaperMailAge)
		if err != nil {
			return fmt.Errorf("invalid --mail-age: %w", err)
		}

		databases := reaper.DiscoverDatabases(reaperHost, reaperPort)
		if reaperDB != "" {
			databases = strings.Split(reaperDB, ",")
		}

		var results []*reaper.PurgeResult
		for _, dbName := range databases {
			if err := reaper.ValidateDBName(dbName); err != nil {
				fmt.Fprintf(os.Stderr, "skip invalid db: %s\n", dbName)
				continue
			}

			db, err := reaper.OpenDB(reaperHost, reaperPort, dbName, 30*time.Second, 30*time.Second)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: connect error: %v\n", dbName, err)
				continue
			}

			if ok, err := reaper.HasReaperSchema(db); err != nil {
				fmt.Fprintf(os.Stderr, "%s: schema check error: %v\n", dbName, err)
				db.Close()
				continue
			} else if !ok {
				db.Close()
				continue
			}

			result, err := reaper.Purge(db, dbName, purgeAge, mailAge, reaperDryRun)
			db.Close()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: purge error: %v\n", dbName, err)
				continue
			}
			results = append(results, result)
		}

		if reaperJSON {
			fmt.Println(reaper.FormatJSON(results))
		} else {
			var totalWisps, totalMail int
			for _, r := range results {
				prefix := ""
				if r.DryRun {
					prefix = "[DRY RUN] would "
				}
				fmt.Printf("%s: %spurged %d wisps, %d mail\n",
					r.Database, prefix, r.WispsPurged, r.MailPurged)
				for _, a := range r.Anomalies {
					fmt.Printf("  %s %s\n", style.Warning.Render("ANOMALY:"), a.Message)
				}
				totalWisps += r.WispsPurged
				totalMail += r.MailPurged
			}
			if len(results) > 1 {
				prefix := ""
				if reaperDryRun {
					prefix = "[DRY RUN] "
				}
				fmt.Printf("\n%sPurge summary (%d databases): purged %d wisps, %d mail\n",
					prefix, len(results), totalWisps, totalMail)
			}
		}
		return nil
	},
}

var reaperClosePluginAcksCmd = &cobra.Command{
	Use:   "close-plugin-acks",
	Short: "Close plugin receipts, dispatches, and reply-acks (fast-track)",
	Long: `Close ephemeral plugin wisps that accumulate between full reap cycles.

Targets three categories with a short ack-age threshold (default 1h):
  - Plugin receipts: outcome wisps from daemon plugin runs
  - Plugin dispatches: daemon→dog instruction beads
  - Plugin reply-acks: 'Re: Plugin:*' and 'Re: DOG_DONE:*' wisps from deacon/

These are high-volume ephemeral wisps (~30-50/hour in normal operation) that
would accumulate for 24h under the standard max-age. A 1h threshold keeps
the open wisp count manageable without affecting longer-lived work wisps.

When --db is provided, operates on a single database. When omitted,
auto-discovers all databases on the Dolt server.

Returns counts of closed wisps per category. Use --dry-run to preview.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ackAge, err := time.ParseDuration(reaperAckAge)
		if err != nil {
			return fmt.Errorf("invalid --ack-age: %w", err)
		}

		databases := reaper.DiscoverDatabases(reaperHost, reaperPort)
		if reaperDB != "" {
			databases = strings.Split(reaperDB, ",")
		}

		type dbResult struct {
			Database  string `json:"database"`
			Receipts  int    `json:"receipts_closed"`
			Dispatches int   `json:"dispatches_closed"`
			Acks      int    `json:"acks_closed"`
			DryRun    bool   `json:"dry_run,omitempty"`
		}

		var results []dbResult
		for _, dbName := range databases {
			if err := reaper.ValidateDBName(dbName); err != nil {
				fmt.Fprintf(os.Stderr, "skip invalid db: %s\n", dbName)
				continue
			}
			db, err := reaper.OpenDB(reaperHost, reaperPort, dbName, 10*time.Second, 10*time.Second)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: connect error: %v\n", dbName, err)
				continue
			}
			ok, schemaErr := reaper.HasReaperSchema(db)
			if schemaErr != nil {
				db.Close()
				continue
			} else if !ok {
				db.Close()
				continue
			}

			r, _ := reaper.ClosePluginReceipts(db, dbName, ackAge, reaperDryRun)
			d, _ := reaper.ClosePluginDispatches(db, dbName, ackAge, reaperDryRun)
			a, _ := reaper.ClosePluginAcks(db, dbName, ackAge, reaperDryRun)
			db.Close()

			receipts, dispatches, acks := 0, 0, 0
			if r != nil {
				receipts = r.Closed
			}
			if d != nil {
				dispatches = d.Closed
			}
			if a != nil {
				acks = a.Closed
			}
			if receipts+dispatches+acks == 0 {
				continue
			}
			results = append(results, dbResult{
				Database:   dbName,
				Receipts:   receipts,
				Dispatches: dispatches,
				Acks:       acks,
				DryRun:     reaperDryRun,
			})
		}

		if reaperJSON {
			fmt.Println(reaper.FormatJSON(results))
		} else {
			var totalReceipts, totalDispatches, totalAcks int
			for _, r := range results {
				prefix := ""
				if r.DryRun {
					prefix = "[DRY RUN] would close "
				} else {
					prefix = "closed "
				}
				fmt.Printf("%s: %s%d receipts, %d dispatches, %d acks\n",
					r.Database, prefix, r.Receipts, r.Dispatches, r.Acks)
				totalReceipts += r.Receipts
				totalDispatches += r.Dispatches
				totalAcks += r.Acks
			}
			if len(results) > 1 || (len(results) == 1 && results[0].Receipts+results[0].Dispatches+results[0].Acks > 0) {
				prefix := ""
				if reaperDryRun {
					prefix = "[DRY RUN] "
				}
				fmt.Printf("\n%sTotal: %d receipts, %d dispatches, %d acks closed\n",
					prefix, totalReceipts, totalDispatches, totalAcks)
			}
			if len(results) == 0 {
				fmt.Println("No plugin acks to close")
			}
		}
		return nil
	},
}

var reaperAutoCloseCmd = &cobra.Command{
	Use:   "auto-close",
	Short: "Close stale issues past stale-age",
	Long: `Close issues open with no updates past the stale-age threshold.
Excludes P0/P1 priority, epics, and issues with active dependencies.

When --db is provided, auto-closes in a single database. When omitted,
auto-discovers all databases on the Dolt server and auto-closes in each one.

Returns the count of closed issues. Use --dry-run to preview.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		staleAge, err := time.ParseDuration(reaperStaleAge)
		if err != nil {
			return fmt.Errorf("invalid --stale-age: %w", err)
		}

		databases := reaper.DiscoverDatabases(reaperHost, reaperPort)
		if reaperDB != "" {
			databases = strings.Split(reaperDB, ",")
		}

		var results []*reaper.AutoCloseResult
		for _, dbName := range databases {
			if err := reaper.ValidateDBName(dbName); err != nil {
				fmt.Fprintf(os.Stderr, "skip invalid db: %s\n", dbName)
				continue
			}

			db, err := reaper.OpenDB(reaperHost, reaperPort, dbName, 10*time.Second, 10*time.Second)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: connect error: %v\n", dbName, err)
				continue
			}

			if ok, err := reaper.HasReaperSchema(db); err != nil {
				fmt.Fprintf(os.Stderr, "%s: schema check error: %v\n", dbName, err)
				db.Close()
				continue
			} else if !ok {
				db.Close()
				continue
			}

			result, err := reaper.AutoClose(db, dbName, staleAge, reaperDryRun)
			db.Close()
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: auto-close error: %v\n", dbName, err)
				continue
			}
			results = append(results, result)
		}

		if reaperJSON {
			fmt.Println(reaper.FormatJSON(results))
		} else {
			var totalClosed int
			for _, r := range results {
				prefix := ""
				if r.DryRun {
					prefix = "[DRY RUN] would "
				}
				for _, entry := range r.ClosedEntries {
					fmt.Printf("  %s %s (%dd stale, db:%s)\n",
						entry.ID, entry.Title, entry.AgeDays, entry.Database)
				}
				fmt.Printf("%s: %sauto-closed %d stale issues\n",
					r.Database, prefix, r.Closed)
				totalClosed += r.Closed
			}
			if len(results) > 1 {
				prefix := ""
				if reaperDryRun {
					prefix = "[DRY RUN] "
				}
				fmt.Printf("\n%sAuto-close summary (%d databases): auto-closed %d stale issues\n",
					prefix, len(results), totalClosed)
			}
		}
		return nil
	},
}

var reaperRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run full reaper cycle across all databases",
	Long: `Execute a full reaper cycle: scan → reap → purge → auto-close → report.

This is the inline fallback for when Dog dispatch is unavailable.
Normally the daemon dispatches a Dog to execute the mol-dog-reaper formula.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		databases := reaper.DiscoverDatabases(reaperHost, reaperPort)
		if reaperDB != "" {
			databases = strings.Split(reaperDB, ",")
		}

		maxAge, err := time.ParseDuration(reaperMaxAge)
		if err != nil {
			return fmt.Errorf("invalid --max-age: %w", err)
		}
		purgeAge, err := time.ParseDuration(reaperPurgeAge)
		if err != nil {
			return fmt.Errorf("invalid --purge-age: %w", err)
		}
		mailAge, err := time.ParseDuration(reaperMailAge)
		if err != nil {
			return fmt.Errorf("invalid --mail-age: %w", err)
		}
		staleAge, err := time.ParseDuration(reaperStaleAge)
		if err != nil {
			return fmt.Errorf("invalid --stale-age: %w", err)
		}

		var totalReaped, totalPurged, totalMailPurged, totalClosed, totalOpen int

		for _, dbName := range databases {
			if err := reaper.ValidateDBName(dbName); err != nil {
				fmt.Printf("skip invalid db: %s\n", dbName)
				continue
			}

			db, err := reaper.OpenDB(reaperHost, reaperPort, dbName, 30*time.Second, 30*time.Second)
			if err != nil {
				fmt.Printf("%s: connect error: %v\n", dbName, err)
				continue
			}

			if ok, err := reaper.HasReaperSchema(db); err != nil {
				fmt.Printf("%s: schema check error: %v\n", dbName, err)
				db.Close()
				continue
			} else if !ok {
				fmt.Printf("%s: skipped (no reaper schema)\n", dbName)
				db.Close()
				continue
			}

			// Scan
			scanResult, err := reaper.Scan(db, dbName, maxAge, purgeAge, mailAge, staleAge)
			if err != nil {
				fmt.Printf("%s: scan error: %v\n", dbName, err)
				db.Close()
				continue
			}
			for _, a := range scanResult.Anomalies {
				fmt.Printf("%s: %s %s\n", dbName, style.Warning.Render("ANOMALY:"), a.Message)
			}

			// Reap
			reapResult, err := reaper.Reap(db, dbName, maxAge, reaperDryRun)
			if err != nil {
				fmt.Printf("%s: reap error: %v\n", dbName, err)
			} else {
				totalReaped += reapResult.Reaped
				totalOpen += reapResult.OpenRemain
			}

			// Purge
			purgeResult, err := reaper.Purge(db, dbName, purgeAge, mailAge, reaperDryRun)
			if err != nil {
				fmt.Printf("%s: purge error: %v\n", dbName, err)
			} else {
				totalPurged += purgeResult.WispsPurged
				totalMailPurged += purgeResult.MailPurged
			}

			// Auto-close
			closeResult, err := reaper.AutoClose(db, dbName, staleAge, reaperDryRun)
			if err != nil {
				fmt.Printf("%s: auto-close error: %v\n", dbName, err)
			} else {
				for _, entry := range closeResult.ClosedEntries {
					fmt.Printf("  %s %s (%dd stale, db:%s)\n",
						entry.ID, entry.Title, entry.AgeDays, entry.Database)
				}
				totalClosed += closeResult.Closed
			}

			db.Close()
		}

		// Report
		prefix := ""
		if reaperDryRun {
			prefix = "[DRY RUN] "
		}
		fmt.Printf("\n%sReaper cycle complete:\n", prefix)
		fmt.Printf("  Databases: %d\n", len(databases))
		fmt.Printf("  Reaped:    %d\n", totalReaped)
		fmt.Printf("  Purged:    %d wisps, %d mail\n", totalPurged, totalMailPurged)
		fmt.Printf("  Closed:    %d stale issues\n", totalClosed)
		fmt.Printf("  Open:      %d wisps remain\n", totalOpen)

		return nil
	},
}

func init() {
	// Shared flags
	// GH#2601: Default host/port from env vars for non-localhost setups.
	defaultHost := "127.0.0.1"
	if h := os.Getenv("GT_DOLT_HOST"); h != "" {
		defaultHost = h
	} else if h := os.Getenv("BEADS_DOLT_SERVER_HOST"); h != "" {
		defaultHost = h
	}
	defaultPort := 3307
	if p := os.Getenv("GT_DOLT_PORT"); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			defaultPort = v
		}
	} else if p := os.Getenv("BEADS_DOLT_SERVER_PORT"); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			defaultPort = v
		}
	}

	for _, cmd := range []*cobra.Command{reaperScanCmd, reaperReapCmd, reaperPurgeCmd, reaperAutoCloseCmd, reaperClosePluginAcksCmd, reaperRunCmd, reaperDatabasesCmd} {
		cmd.Flags().StringVar(&reaperDB, "db", "", "Database name (required for single-db commands)")
		cmd.Flags().StringVar(&reaperHost, "host", defaultHost, "Dolt server host (env: GT_DOLT_HOST)")
		cmd.Flags().IntVar(&reaperPort, "port", defaultPort, "Dolt server port (env: GT_DOLT_PORT)")
		cmd.Flags().BoolVar(&reaperDryRun, "dry-run", false, "Report what would happen without acting")
	}

	// JSON output flag for single-db commands
	for _, cmd := range []*cobra.Command{reaperScanCmd, reaperReapCmd, reaperPurgeCmd, reaperAutoCloseCmd, reaperClosePluginAcksCmd, reaperDatabasesCmd} {
		cmd.Flags().BoolVar(&reaperJSON, "json", false, "Output as JSON")
	}

	// Threshold flags
	for _, cmd := range []*cobra.Command{reaperScanCmd, reaperReapCmd, reaperRunCmd} {
		cmd.Flags().StringVar(&reaperMaxAge, "max-age", "24h", "Max wisp age before reaping")
	}
	for _, cmd := range []*cobra.Command{reaperScanCmd, reaperPurgeCmd, reaperRunCmd} {
		cmd.Flags().StringVar(&reaperPurgeAge, "purge-age", "168h", "Max closed wisp age before purging (7d)")
		cmd.Flags().StringVar(&reaperMailAge, "mail-age", "168h", "Max closed mail age before purging (7d)")
	}
	for _, cmd := range []*cobra.Command{reaperScanCmd, reaperAutoCloseCmd, reaperRunCmd} {
		cmd.Flags().StringVar(&reaperStaleAge, "stale-age", "720h", "Max issue staleness before auto-close (30d)")
	}

	reaperClosePluginAcksCmd.Flags().StringVar(&reaperAckAge, "ack-age", "1h", "Max plugin ack/receipt age before closing")

	reaperCmd.AddCommand(reaperDatabasesCmd)
	reaperCmd.AddCommand(reaperScanCmd)
	reaperCmd.AddCommand(reaperReapCmd)
	reaperCmd.AddCommand(reaperPurgeCmd)
	reaperCmd.AddCommand(reaperClosePluginAcksCmd)
	reaperCmd.AddCommand(reaperAutoCloseCmd)
	reaperCmd.AddCommand(reaperRunCmd)

	rootCmd.AddCommand(reaperCmd)
}
