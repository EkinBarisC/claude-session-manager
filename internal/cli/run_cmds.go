package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/EkinBarisC/claude-session-manager/internal/claude"
	"github.com/EkinBarisC/claude-session-manager/internal/config"
	"github.com/EkinBarisC/claude-session-manager/internal/ledger"
	"github.com/EkinBarisC/claude-session-manager/internal/queue"
	"github.com/EkinBarisC/claude-session-manager/internal/report"
	"github.com/EkinBarisC/claude-session-manager/internal/runner"
	"github.com/EkinBarisC/claude-session-manager/internal/runstate"
	"github.com/EkinBarisC/claude-session-manager/internal/schedule"
	"github.com/EkinBarisC/claude-session-manager/internal/sessions"
)

// localTime renders an RFC3339 timestamp in the machine's local zone.
func localTime(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	return t.Local().Format("2006-01-02 15:04")
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "create ~/.csm with default config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.EnsureInit()
			if err != nil {
				return fail("%v", err)
			}
			fmt.Printf("csm: initialized. Config: %s\n", path)
			fmt.Println("csm: make sure Claude Code is logged in with your Pro account " +
				"(run `claude` once and use /login).")
			return nil
		},
	}
}

func newRunCmd() *cobra.Command {
	var until, itemID string
	var maxItems int
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "run",
		Short: "run pending items now (manual burn or scheduled)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			exitCode = runner.Run(until, maxItems, dryRun, itemID)
			if exitCode != 0 {
				return fmt.Errorf("run stopped early (exit %d)", exitCode)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&until, "until", "", "stop starting new items at this local time (HH:MM)")
	cmd.Flags().IntVar(&maxItems, "max-items", 0, "stop after this many items")
	cmd.Flags().StringVar(&itemID, "id", "", "run only this item (id or unique prefix)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would run without invoking claude")
	return cmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "queue overview, weekly spend, session registry",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fail("%v", err)
			}
			if lock := runstate.Current(); lock != nil {
				line := fmt.Sprintf("Run in progress: pid %d, %s, started %s",
					lock.PID, lock.Trigger, localTime(lock.StartedAt))
				if lock.ItemID != "" {
					line += fmt.Sprintf(", on [%s]", lock.ItemID)
				}
				fmt.Println(line)
			}
			if lr := runstate.ReadLastRun(); lr != nil {
				fmt.Printf("Last run: %s (%s) - %d item(s), %s\n",
					localTime(lr.FinishedAt), lr.Trigger, lr.Processed, lr.Outcome)
			}
			items := queue.Load()
			byStatus := map[string]int{}
			for _, it := range items {
				byStatus[it.Status]++
			}
			if len(items) == 0 {
				fmt.Println("Queue: empty")
			} else {
				var parts []string
				for _, s := range []string{queue.Done, queue.NeedsAttention, queue.Pending} {
					if byStatus[s] > 0 {
						parts = append(parts, fmt.Sprintf("%s=%d", s, byStatus[s]))
					}
				}
				fmt.Printf("Queue (%d items): %s\n", len(items), strings.Join(parts, ", "))
			}
			for _, it := range queue.PendingItems(items) {
				model, effort, mode := claude.ItemSettings(cfg, it)
				if effort == "" {
					effort = "default"
				}
				fmt.Printf("  [%s] p%d %s/%s/%s %s :: %s\n",
					it.ID, it.Priority, model, effort, mode, it.Project, report.Short(it.Prompt, 60))
			}
			for _, it := range items {
				if it.Status != queue.NeedsAttention {
					continue
				}
				line := fmt.Sprintf("  [%s] NEEDS ATTENTION: %s", it.ID, it.Error)
				if it.SessionID != "" {
					line += fmt.Sprintf(" (claude -r %s)", it.SessionID)
				}
				fmt.Println(line)
			}

			spend := ledger.WeeklySpend()
			pct := 0.0
			if cfg.WeeklyTokenBudget > 0 {
				pct = 100 * float64(spend) / float64(cfg.WeeklyTokenBudget)
			}
			fmt.Printf("Weekly spend: %d / %d weighted tokens (%.0f%%)\n",
				spend, cfg.WeeklyTokenBudget, pct)

			registry := sessions.Load()
			if len(registry) > 0 {
				threshold := cfg.ContextWindowTokens * cfg.ContextRotatePct / 100
				fmt.Println("Sessions:")
				for project, entry := range registry {
					state := "resumable"
					if entry.ContextTokens >= threshold {
						state = "will rotate"
					}
					fmt.Printf("  %s: %d ctx tokens (%s)\n", project, entry.ContextTokens, state)
				}
			}
			return nil
		},
	}
}

func newReportCmd() *cobra.Command {
	var tail int
	cmd := &cobra.Command{
		Use:   "report",
		Short: "show the run report",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(config.ReportPath())
			if err != nil {
				fmt.Println("csm: no report yet")
				return nil
			}
			lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
			if tail < len(lines) {
				lines = lines[len(lines)-tail:]
			}
			fmt.Printf("--- %s (last %d lines) ---\n", config.ReportPath(), len(lines))
			fmt.Println(strings.Join(lines, "\n"))
			return nil
		},
	}
	cmd.Flags().IntVar(&tail, "tail", 60, "lines to show")
	return cmd
}

func newScheduleCmd() *cobra.Command {
	var start, until string
	var remove bool
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "register/remove the nightly job (Task Scheduler on Windows, cron elsewhere)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if remove {
				if err := schedule.Remove(); err != nil {
					return fail("%v", err)
				}
				return nil
			}
			cfg, err := config.Load()
			if err != nil {
				return fail("%v", err)
			}
			if start == "" {
				start = cfg.QuietHoursStart
			}
			if until == "" {
				until = cfg.QuietHoursEnd
			}
			if err := schedule.Install(start, until); err != nil {
				return fail("%v", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&start, "start", "", "nightly start HH:MM (default: config quiet_hours_start)")
	cmd.Flags().StringVar(&until, "until", "", "nightly end HH:MM (default: config quiet_hours_end)")
	cmd.Flags().BoolVar(&remove, "remove", false, "remove the scheduled job")
	return cmd
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "check claude login, config, and the nightly job",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			problems := 0
			check := func(ok bool, label, detail string) {
				mark := "ok  "
				if !ok {
					mark = "FAIL"
					problems++
				}
				if detail != "" {
					detail = " - " + detail
				}
				fmt.Printf("  [%s] %s%s\n", mark, label, detail)
			}

			cfg, cfgErr := config.Load()
			fmt.Printf("csm %s doctor\n", Version)
			fmt.Printf("  state dir: %s\n", config.StateDir())

			if binary, err := exec.LookPath(cfg.ClaudeBinary); err == nil {
				out, verr := exec.Command(binary, "--version").CombinedOutput()
				version := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
				check(verr == nil, "claude binary: "+binary, version)
			} else {
				check(false, fmt.Sprintf("claude binary '%s' not on PATH", cfg.ClaudeBinary),
					"install Claude Code and log in with your Pro account")
			}

			var configIssues []string
			if cfgErr != nil {
				configIssues = append(configIssues, cfgErr.Error())
			}
			for key := range config.Overrides() {
				if err := config.Validate(key, config.Overrides()[key]); err != nil {
					configIssues = append(configIssues, err.Error())
				}
			}
			check(len(configIssues) == 0, "config valid", strings.Join(configIssues, "; "))

			var billing []string
			for _, name := range claude.StripEnv {
				if os.Getenv(name) != "" {
					billing = append(billing, name)
				}
			}
			detail := "none set"
			if len(billing) > 0 {
				detail = strings.Join(billing, ", ") + " set in your shell - csm strips them from every run"
			}
			check(true, "billing env vars", detail)

			// A registered job whose runs stopped happening is the silent
			// failure mode (expired login, machine asleep, task removed
			// behind our back) - that one deserves a FAIL.
			lastRun := runstate.ReadLastRun()
			switch {
			case !schedule.Exists():
				check(true, "nightly job", "not registered (run `csm schedule`)")
			case lastRun == nil:
				check(true, "nightly job", "registered; no run recorded yet")
			case lastRun.Age() > 48*time.Hour:
				check(false, "nightly job",
					fmt.Sprintf("registered but the last run finished %s ago (%s) - "+
						"is the machine awake during quiet hours?",
						lastRun.Age().Truncate(time.Hour), localTime(lastRun.FinishedAt)))
			default:
				check(true, "nightly job",
					fmt.Sprintf("registered; last run %s (%s)",
						localTime(lastRun.FinishedAt), lastRun.Outcome))
			}

			if lock := runstate.Current(); lock != nil {
				check(true, "run in progress",
					fmt.Sprintf("pid %d since %s", lock.PID, localTime(lock.StartedAt)))
			}

			items := queue.Load()
			counts := map[string]int{}
			for _, it := range items {
				counts[it.Status]++
			}
			queueDetail := "empty"
			if len(items) > 0 {
				var parts []string
				for _, s := range []string{queue.Done, queue.NeedsAttention, queue.Pending} {
					if counts[s] > 0 {
						parts = append(parts, fmt.Sprintf("%s=%d", s, counts[s]))
					}
				}
				queueDetail = strings.Join(parts, ", ")
			}
			check(true, "queue", queueDetail)
			check(true, "weekly spend",
				fmt.Sprintf("%d / %d weighted tokens", ledger.WeeklySpend(), cfg.WeeklyTokenBudget))

			if problems == 0 {
				fmt.Println("csm: all checks passed")
				return nil
			}
			return fail("%d problem(s) found", problems)
		},
	}
}
