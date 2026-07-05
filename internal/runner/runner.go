// Package runner picks pending items, runs them, and parks on limits.
package runner

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/EkinBarisC/claude-session-manager/internal/claude"
	"github.com/EkinBarisC/claude-session-manager/internal/config"
	"github.com/EkinBarisC/claude-session-manager/internal/ledger"
	"github.com/EkinBarisC/claude-session-manager/internal/queue"
	"github.com/EkinBarisC/claude-session-manager/internal/report"
	"github.com/EkinBarisC/claude-session-manager/internal/sessions"
)

// Exit codes for `csm run`.
const (
	ExitOK        = 0
	ExitUsage     = 1
	ExitAuthError = 2
	ExitRateLimit = 3
)

// ResolveUntil turns "HH:MM" into the next occurrence of that local time.
func ResolveUntil(hhmm string) (*time.Time, error) {
	if hhmm == "" {
		return nil, nil
	}
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return nil, fmt.Errorf("invalid --until time %q (want HH:MM)", hhmm)
	}
	now := time.Now()
	candidate := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
	if !candidate.After(now) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return &candidate, nil
}

// Run processes pending items until the queue, --until, --max-items, or the
// weekly budget stops it. itemID restricts the run to a single item.
func Run(untilHHMM string, maxItems int, dryRun bool, itemID string) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Println("csm:", err)
		return ExitUsage
	}
	config.EnsureInit()
	until, err := ResolveUntil(untilHHMM)
	if err != nil {
		fmt.Println("csm:", err)
		return ExitUsage
	}
	registry := sessions.Load()
	env, removed := claude.StrippedEnv(os.Environ())
	if len(removed) > 0 {
		fmt.Printf("csm: stripped billing-capable env vars from claude subprocess: %s\n", strings.Join(removed, ", "))
	}

	items := queue.Load()
	var todo []*queue.Item
	if itemID != "" {
		item, err := queue.Find(items, itemID)
		if err != nil {
			fmt.Println("csm:", err)
			return ExitUsage
		}
		if item.Status != queue.Pending {
			fmt.Printf("csm: [%s] is %s, not pending (use `csm requeue %s` to run it again)\n",
				item.ID, item.Status, item.ID)
			return ExitUsage
		}
		todo = []*queue.Item{item}
	} else {
		todo = queue.PendingItems(items)
	}
	if len(todo) == 0 {
		fmt.Println("csm: queue is empty - nothing to run")
		return ExitOK
	}

	if !dryRun {
		trigger := "manual"
		if until != nil {
			trigger = "scheduled until " + until.Format("15:04")
		}
		report.AppendRunHeader(trigger)
	}

	ran := 0
	for _, item := range todo {
		if until != nil && !time.Now().Before(*until) {
			fmt.Printf("csm: reached --until %s, stopping\n", until.Format("15:04"))
			break
		}
		if maxItems > 0 && ran >= maxItems {
			fmt.Printf("csm: reached --max-items %d, stopping\n", maxItems)
			break
		}

		spend := ledger.WeeklySpend()
		if spend >= cfg.WeeklyTokenBudget {
			msg := fmt.Sprintf("weekly budget reached (%d / %d weighted tokens in the last 7 days)",
				spend, cfg.WeeklyTokenBudget)
			fmt.Printf("csm: %s, stopping\n", msg)
			if !dryRun {
				report.AppendNote(msg)
			}
			break
		}

		resumeID := ""
		if !item.ForceNewSession {
			resumeID = registry.Resumable(cfg, item.Project)
		}

		session := "new session (context.md pickup)"
		if resumeID != "" {
			session = "resume " + resumeID
		}
		model, effort, mode := claude.ItemSettings(cfg, item)
		if effort == "" {
			effort = "cli-default"
		}
		fmt.Printf("csm: [%s] %s | %s | model=%s effort=%s mode=%s\n",
			item.ID, item.Project, session, model, effort, mode)
		if dryRun {
			ran++
			continue
		}

		result := claude.RunItem(cfg, item, resumeID, env)
		ran++

		if result.AuthError {
			fmt.Printf("csm: %s - stopping the whole run (no quota burned on a broken login)\n", result.Error)
			report.AppendNote("run aborted: " + result.Error)
			return ExitAuthError
		}

		if result.RateLimited {
			fmt.Println("csm: usage limit reached")
			note := "usage limit reached"
			if result.ResetAt != nil {
				note += ", resets ~" + result.ResetAt.Format("15:04")
			}
			report.AppendNote(note)
			if result.ResetAt != nil && until != nil && result.ResetAt.Before(*until) {
				wait := time.Until(*result.ResetAt) + time.Minute
				if wait < time.Minute {
					wait = time.Minute
				}
				fmt.Printf("csm: sleeping %.0f min until reset (~%s)\n",
					wait.Minutes(), result.ResetAt.Format("15:04"))
				time.Sleep(wait)
				continue // item stays pending, retry after reset
			}
			fmt.Println("csm: no reset time inside the run window - stopping")
			return ExitRateLimit
		}

		RecordOutcome(cfg, items, item, result, registry)

		tag := "done"
		if !result.OK {
			tag = "NEEDS ATTENTION"
		}
		detail := result.Summary
		if detail == "" {
			detail = result.Error
		}
		fmt.Printf("csm: [%s] %s - %s\n", item.ID, tag, detail)
	}

	fmt.Printf("csm: done (%d item(s) processed)\n", ran)
	return ExitOK
}

// RecordOutcome persists everything one finished run produced: session
// registry, ledger, queue status, and the report block.
func RecordOutcome(cfg config.Config, items []*queue.Item, item *queue.Item,
	result claude.Result, registry sessions.Registry) {
	if result.SessionID != "" {
		registry.Record(item.Project, result.SessionID, result.ContextTokens())
	}

	model, _, _ := claude.ItemSettings(cfg, item)
	weighted := 0
	if len(result.Usage) > 0 {
		ledger.Append(item.ID, item.Project, model, result.Usage)
		weighted = ledger.Weighted(result.Usage)
	}
	spend := ledger.WeeklySpend()

	status := queue.Done
	if !result.OK {
		status = queue.NeedsAttention
	}
	queue.Finish(items, item, status, result.SessionID, result.Summary, result.Error, result.Usage)
	report.AppendItem(item, status, result.SessionID, result.Summary, result.Error,
		weighted, spend, cfg.WeeklyTokenBudget)
}
