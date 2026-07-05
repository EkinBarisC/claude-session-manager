// csm - Claude Session Manager.
//
// Spends leftover Claude Pro subscription quota by running queued prompts
// through Claude Code headless mode. Subscription auth only - never the
// pay-per-token API.
package main

import (
	"os"

	"github.com/EkinBarisC/claude-session-manager/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
