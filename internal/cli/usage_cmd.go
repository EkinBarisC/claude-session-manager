package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/EkinBarisC/claude-session-manager/internal/claude"
	"github.com/EkinBarisC/claude-session-manager/internal/config"
	"github.com/EkinBarisC/claude-session-manager/internal/ledger"
)

func newUsageCmd() *cobra.Command {
	var raw bool
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "show real Claude plan usage (5h session + weekly limits)",
		Long: "Asks the claude CLI for actual plan usage via its /usage command\n" +
			"(free - no tokens are spent). This is account-level truth including\n" +
			"your interactive sessions; the ledger numbers in `csm status` cover\n" +
			"only what csm itself spent.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fail("%v", err)
			}
			limits, reportText, err := claude.FetchUsage(cfg)
			if err != nil {
				return fail("%v", err)
			}
			if raw {
				fmt.Println(reportText)
				return nil
			}
			if len(limits) == 0 {
				fmt.Println("csm: could not find limit lines in the /usage report; try --raw")
				return nil
			}
			for _, l := range limits {
				line := fmt.Sprintf("%-20s %3d%% used", l.Scope, l.Pct)
				if l.Resets != "" {
					line += "   resets " + l.Resets
				}
				fmt.Println(line)
			}
			fmt.Printf("%-20s %d / %d weighted tokens (csm's own runs, rolling 7d)\n",
				"csm ledger", ledger.WeeklySpend(), cfg.WeeklyTokenBudget)
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "print claude's full /usage report")
	return cmd
}
