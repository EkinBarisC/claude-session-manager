// Package cli wires up the csm command tree.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/EkinBarisC/claude-session-manager/internal/tui"
)

const Version = "0.4.0"

// exitCode carries non-zero exits (e.g. runner's auth/rate-limit codes)
// out of cobra's error-based flow.
var exitCode int

func fail(format string, a ...any) error {
	exitCode = 1
	return fmt.Errorf(format, a...)
}

const examples = `  csm                                         launch the interactive TUI
  csm init                                    set up ~/.csm
  csm add "Fix the failing tests in src/"     queue a task in the current directory
  csm add "Refactor auth" -C ../other-project --effort high --mode plan
  csm list                                    pending and failed items
  csm run --max-items 1                       run one item now
  csm run --until 08:00                       run until 08:00
  csm config set default_effort low           change a setting
  csm requeue --all                           retry everything that failed
  csm doctor                                  check the installation`

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "csm",
		Version: Version,
		Short:   "Claude Session Manager - spend leftover Claude Pro quota via Claude Code headless runs",
		Long: "Claude Session Manager - spend leftover Claude Pro quota via Claude Code\n" +
			"headless runs (subscription auth only). Run without arguments for the TUI.",
		Example:       examples,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tui.Run()
		},
	}
	root.AddCommand(
		newInitCmd(),
		newAddCmd(),
		newListCmd(),
		newShowCmd(),
		newEditCmd(),
		newRmCmd(),
		newClearCmd(),
		newRequeueCmd(),
		newStatusCmd(),
		newRunCmd(),
		newReportCmd(),
		newScheduleCmd(),
		newConfigCmd(),
		newDoctorCmd(),
		newUsageCmd(),
		newTuiCmd(),
	)
	return root
}

func newTuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "launch the interactive TUI (same as running csm with no arguments)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return tui.Run()
		},
	}
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "csm:", err)
		if exitCode == 0 {
			exitCode = 1
		}
	}
	return exitCode
}
