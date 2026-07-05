package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/EkinBarisC/claude-session-manager/internal/claude"
	"github.com/EkinBarisC/claude-session-manager/internal/config"
	"github.com/EkinBarisC/claude-session-manager/internal/ledger"
	"github.com/EkinBarisC/claude-session-manager/internal/queue"
	"github.com/EkinBarisC/claude-session-manager/internal/report"
)

func validateChoice(name, value string, choices []string) error {
	if value != "" && !slices.Contains(choices, value) {
		return fail("--%s must be one of %s", name, strings.Join(choices, ", "))
	}
	return nil
}

func newAddCmd() *cobra.Command {
	var project, model, effort, mode string
	var priority int
	var newSession bool
	cmd := &cobra.Command{
		Use:   "add <prompt>",
		Short: "queue a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateChoice("effort", effort, config.EffortLevels); err != nil {
				return err
			}
			if err := validateChoice("mode", mode, config.RunModes); err != nil {
				return err
			}
			info, err := os.Stat(project)
			if err != nil || !info.IsDir() {
				return fail("project directory not found: %s", project)
			}
			if model != "" && strings.Contains(strings.ToLower(model), "opus") {
				fmt.Println("csm: warning - Pro accounts have no Opus access in Claude Code; " +
					"this item will likely fail. Queued anyway.")
			}
			if mode == "full" {
				fmt.Println("csm: warning - mode 'full' skips ALL permission checks in that " +
					"project; use only for sandboxed/throwaway directories.")
			}
			if _, err := config.EnsureInit(); err != nil {
				return fail("%v", err)
			}
			item, err := queue.Add(args[0], project, model, effort, mode, priority, newSession)
			if err != nil {
				return fail("%v", err)
			}
			fmt.Printf("csm: queued [%s] for %s\n", item.ID, item.Project)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "C", ".", "project directory the task runs in")
	cmd.Flags().StringVarP(&model, "model", "m", "", "model override (default: config default_model)")
	cmd.Flags().StringVarP(&effort, "effort", "e", "", "reasoning effort: low|medium|high|xhigh|max")
	cmd.Flags().StringVar(&mode, "mode", "", "plan = read-only planning, safe = allowlisted edits, full = skip all permission checks")
	cmd.Flags().IntVarP(&priority, "priority", "p", 0, "higher runs first")
	cmd.Flags().BoolVar(&newSession, "new-session", false, "force a fresh session even if one is resumable")
	return cmd
}

func itemRow(cfg config.Config, item *queue.Item) string {
	model, effort, mode := claude.ItemSettings(cfg, item)
	if effort == "" {
		effort = "default"
	}
	head := fmt.Sprintf("  [%s] %-15s p%d %s/%s/%s", item.ID, item.Status, item.Priority, model, effort, mode)
	line := fmt.Sprintf("%s  %s :: %s", head, item.Project, report.Short(item.Prompt, 70))
	tail := item.Summary
	if tail == "" {
		tail = item.Error
	}
	if tail != "" {
		line += "\n      -> " + report.Short(tail, 70)
	}
	return line
}

func newListCmd() *cobra.Command {
	var status string
	var all bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "list queue items",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateChoice("status", status,
				[]string{queue.Pending, queue.Done, queue.NeedsAttention}); err != nil {
				return err
			}
			cfg, err := config.Load()
			if err != nil {
				return fail("%v", err)
			}
			items := queue.Load()
			var shown []*queue.Item
			for _, it := range items {
				if status != "" && it.Status != status {
					continue
				}
				if status == "" && !all && it.Status == queue.Done {
					continue
				}
				shown = append(shown, it)
			}
			if len(shown) == 0 {
				fmt.Println("csm: nothing to list (try --all)")
				return nil
			}
			slices.SortStableFunc(shown, func(a, b *queue.Item) int {
				if (a.Status == queue.Pending) != (b.Status == queue.Pending) {
					if a.Status == queue.Pending {
						return -1
					}
					return 1
				}
				if a.Priority != b.Priority {
					return b.Priority - a.Priority
				}
				return strings.Compare(a.CreatedAt, b.CreatedAt)
			})
			for _, it := range shown {
				fmt.Println(itemRow(cfg, it))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "only items with this status")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "include done items")
	return cmd
}

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <item-id>",
		Short: "show one item in full (id may be a unique prefix)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			items := queue.Load()
			item, err := queue.Find(items, args[0])
			if err != nil {
				return fail("%v", err)
			}
			cfg, err := config.Load()
			if err != nil {
				return fail("%v", err)
			}
			model, effort, mode := claude.ItemSettings(cfg, item)
			fromConfig := func(own string) string {
				if own == "" {
					return " (config default)"
				}
				return ""
			}
			if effort == "" {
				effort = "cli default"
			}
			fmt.Printf("id:        %s\n", item.ID)
			fmt.Printf("status:    %s\n", item.Status)
			fmt.Printf("project:   %s\n", item.Project)
			if item.Branch != "" {
				fmt.Printf("branch:    %s\n", item.Branch)
			}
			fmt.Printf("model:     %s%s\n", model, fromConfig(item.Model))
			fmt.Printf("effort:    %s%s\n", effort, fromConfig(item.Effort))
			fmt.Printf("mode:      %s%s\n", mode, fromConfig(item.Mode))
			fmt.Printf("priority:  %d\n", item.Priority)
			fmt.Printf("created:   %s\n", item.CreatedAt)
			if item.FinishedAt != "" {
				fmt.Printf("finished:  %s\n", item.FinishedAt)
			}
			if item.SessionID != "" {
				fmt.Printf("session:   %s  (resume with `claude -r %s`)\n", item.SessionID, item.SessionID)
			}
			if item.Summary != "" {
				fmt.Printf("summary:   %s\n", item.Summary)
			}
			if item.Error != "" {
				fmt.Printf("error:     %s\n", item.Error)
			}
			if len(item.Tokens) > 0 {
				fmt.Printf("tokens:    %d weighted (in %d, out %d, cache-read %d)\n",
					ledger.Weighted(item.Tokens),
					claude.UsageInt(item.Tokens, "input_tokens"),
					claude.UsageInt(item.Tokens, "output_tokens"),
					claude.UsageInt(item.Tokens, "cache_read_input_tokens"))
			}
			logPath := filepath.Join(config.LogsDir(), item.ID+".md")
			if _, err := os.Stat(logPath); err == nil {
				fmt.Printf("transcript: %s\n", logPath)
			}
			fmt.Printf("prompt:\n%s\n", item.Prompt)
			return nil
		},
	}
}

func newEditCmd() *cobra.Command {
	var prompt, model, effort, mode string
	var priority int
	cmd := &cobra.Command{
		Use:   "edit <item-id>",
		Short: "change a queued item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateChoice("effort", effort, config.EffortLevels); err != nil {
				return err
			}
			if err := validateChoice("mode", mode, config.RunModes); err != nil {
				return err
			}
			items := queue.Load()
			item, err := queue.Find(items, args[0])
			if err != nil {
				return fail("%v", err)
			}
			var changes []string
			apply := func(name string, target *string, value string) {
				if value != "" {
					*target = value
					changes = append(changes, fmt.Sprintf("%s=%s", name, value))
				}
			}
			apply("prompt", &item.Prompt, prompt)
			apply("model", &item.Model, model)
			apply("effort", &item.Effort, effort)
			apply("mode", &item.Mode, mode)
			if cmd.Flags().Changed("priority") {
				item.Priority = priority
				changes = append(changes, fmt.Sprintf("priority=%d", priority))
			}
			if len(changes) == 0 {
				return fail("nothing to change (pass --prompt/--model/--effort/--mode/--priority)")
			}
			if err := queue.Save(items); err != nil {
				return fail("%v", err)
			}
			fmt.Printf("csm: [%s] updated: %s\n", item.ID, strings.Join(changes, ", "))
			if item.Status != queue.Pending {
				fmt.Printf("csm: note - item is %s; `csm requeue %s` to run it\n", item.Status, item.ID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&prompt, "prompt", "", "new prompt")
	cmd.Flags().StringVarP(&model, "model", "m", "", "new model")
	cmd.Flags().StringVarP(&effort, "effort", "e", "", "new effort")
	cmd.Flags().StringVar(&mode, "mode", "", "new run mode")
	cmd.Flags().IntVarP(&priority, "priority", "p", 0, "new priority")
	return cmd
}

func newRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rm <item-id>...",
		Aliases: []string{"remove"},
		Short:   "delete items from the queue",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			items := queue.Load()
			targets := map[*queue.Item]bool{}
			for _, token := range args {
				item, err := queue.Find(items, token)
				if err != nil {
					return fail("%v", err)
				}
				targets[item] = true
			}
			var remaining []*queue.Item
			for _, it := range items {
				if !targets[it] {
					remaining = append(remaining, it)
				}
			}
			if err := queue.Save(remaining); err != nil {
				return fail("%v", err)
			}
			for it := range targets {
				fmt.Printf("csm: removed [%s] %s\n", it.ID, report.Short(it.Prompt, 70))
			}
			return nil
		},
	}
}

func newClearCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "remove done items from the queue",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			items := queue.Load()
			var kept []*queue.Item
			removed := 0
			for _, it := range items {
				if all || it.Status == queue.Done {
					removed++
				} else {
					kept = append(kept, it)
				}
			}
			if removed == 0 {
				fmt.Println("csm: nothing to clear")
				return nil
			}
			if err := queue.Save(kept); err != nil {
				return fail("%v", err)
			}
			fmt.Printf("csm: cleared %d item(s), %d left\n", removed, len(kept))
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "remove every item regardless of status")
	return cmd
}

func newRequeueCmd() *cobra.Command {
	var all, newSession bool
	cmd := &cobra.Command{
		Use:   "requeue [<item-id>...]",
		Short: "set items back to pending",
		RunE: func(cmd *cobra.Command, args []string) error {
			items := queue.Load()
			var targets []*queue.Item
			switch {
			case all:
				for _, it := range items {
					if it.Status == queue.NeedsAttention {
						targets = append(targets, it)
					}
				}
				if len(targets) == 0 {
					fmt.Println("csm: no needs_attention items")
					return nil
				}
			case len(args) > 0:
				for _, token := range args {
					item, err := queue.Find(items, token)
					if err != nil {
						return fail("%v", err)
					}
					targets = append(targets, item)
				}
			default:
				return fail("pass item ids or --all")
			}
			for _, it := range targets {
				it.Status = queue.Pending
				it.Error = ""
				if newSession {
					it.ForceNewSession = true
				}
				fmt.Printf("csm: [%s] back to pending\n", it.ID)
			}
			return queue.Save(items)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "requeue every needs_attention item")
	cmd.Flags().BoolVar(&newSession, "new-session", false, "also force a fresh session on the retry")
	return cmd
}
