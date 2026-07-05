//go:build !windows

package schedule

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/EkinBarisC/claude-session-manager/internal/config"
)

// Install adds (or replaces) the marker-tagged crontab entry.
func Install(startHHMM, untilHHMM string) error {
	if _, err := exec.LookPath("crontab"); err != nil {
		exe, _ := os.Executable()
		return fmt.Errorf("'crontab' not found - install cron or schedule `%s run --until %s` yourself",
			exe, untilHHMM)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := filepath.Join(config.StateDir(), "cron.log")
	line, err := CronLine(startHHMM, untilHHMM, os.Getenv("PATH"), exe, logPath)
	if err != nil {
		return err
	}
	kept := withoutMarker(readCrontab())
	kept = append(kept, line)
	if err := writeCrontab(strings.Join(kept, "\n") + "\n"); err != nil {
		return err
	}
	fmt.Printf("Registered cron entry %s: daily at %s, runs until %s. Output: %s\n",
		CronMarker, startHHMM, untilHHMM, logPath)
	fmt.Println("csm: note - cron does not wake a sleeping machine; keep it awake " +
		"or set a wake alarm (`pmset repeat wakeorpoweron` on macOS, `rtcwake` on Linux).")
	return nil
}

func Remove() error {
	current := readCrontab()
	if !strings.Contains(strings.Join(current, "\n"), CronMarker) {
		fmt.Printf("csm: no %s cron entry found\n", CronMarker)
		return nil
	}
	kept := withoutMarker(current)
	content := ""
	if len(kept) > 0 {
		content = strings.Join(kept, "\n") + "\n"
	}
	if err := writeCrontab(content); err != nil {
		return err
	}
	fmt.Printf("Removed cron entry %s.\n", CronMarker)
	return nil
}

func Exists() bool {
	return strings.Contains(strings.Join(readCrontab(), "\n"), CronMarker)
}

func readCrontab() []string {
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		return nil // no crontab yet
	}
	text := strings.TrimRight(string(out), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func withoutMarker(lines []string) []string {
	var kept []string
	for _, l := range lines {
		if !strings.Contains(l, CronMarker) {
			kept = append(kept, l)
		}
	}
	return kept
}

func writeCrontab(content string) error {
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(content)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("scheduler error: %s", strings.SplitN(msg, "\n", 2)[0])
	}
	return nil
}
