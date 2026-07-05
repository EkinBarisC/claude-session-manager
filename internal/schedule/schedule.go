// Package schedule registers the nightly job: Windows Task Scheduler on
// Windows, a managed crontab entry on macOS/Linux. Platform selection
// happens at compile time via build tags (schedule_windows.go /
// schedule_unix.go).
package schedule

import (
	"fmt"
	"strings"
)

const (
	TaskName   = "csm-nightly"
	CronMarker = "# csm-nightly"
)

// CronLine builds the crontab entry for a nightly run. Pure so it can be
// unit-tested on any platform.
func CronLine(startHHMM, untilHHMM, pathEnv, executable, logPath string) (string, error) {
	var hour, minute int
	if _, err := fmt.Sscanf(startHHMM, "%d:%d", &hour, &minute); err != nil {
		return "", fmt.Errorf("invalid start time %q (want HH:MM)", startHHMM)
	}
	// cron runs with a minimal PATH; carry the current one so the claude
	// binary resolves the same way it does in an interactive shell.
	command := fmt.Sprintf("PATH=%s %s run --until %s >> %s 2>&1",
		posixQuote(pathEnv), posixQuote(executable), untilHHMM, posixQuote(logPath))
	// % is special in crontab lines (command terminator / stdin marker)
	command = strings.ReplaceAll(command, "%", `\%`)
	return fmt.Sprintf("%d %d * * * %s %s", minute, hour, command, CronMarker), nil
}

// posixQuote single-quotes s for POSIX sh unless it is already safe.
func posixQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\"'`$&|;<>()*?[]#~%!{}\\") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
