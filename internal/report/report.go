// Package report appends the per-item run report to ~/.csm/report.md.
package report

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EkinBarisC/claude-session-manager/internal/config"
	"github.com/EkinBarisC/claude-session-manager/internal/queue"
)

func AppendRunHeader(trigger string) {
	appendText(fmt.Sprintf("\n## Run %s (%s)\n", time.Now().Format("2006-01-02 15:04"), trigger))
}

func AppendItem(item *queue.Item, status, sessionID, summary, errMsg string, weightedTokens, weeklySpend, budget int, logPath string) {
	lines := []string{
		fmt.Sprintf("### [%s] %s - %s", status, item.ID, Short(item.Prompt, 80)),
		fmt.Sprintf("- project: `%s`", item.Project),
	}
	if item.Branch != "" {
		lines = append(lines, fmt.Sprintf("- branch: `%s`", item.Branch))
	}
	if sessionID != "" {
		lines = append(lines, fmt.Sprintf("- session: `%s` (resume with `claude -r %s`)", sessionID, sessionID))
	}
	if summary != "" {
		lines = append(lines, "- summary: "+summary)
	}
	if logPath != "" {
		lines = append(lines, fmt.Sprintf("- transcript: `%s`", logPath))
	}
	if errMsg != "" {
		lines = append(lines, "- error: "+errMsg)
	}
	lines = append(lines, fmt.Sprintf("- tokens (weighted): %d | week: %d / %d", weightedTokens, weeklySpend, budget))
	appendText(strings.Join(lines, "\n") + "\n")
}

func AppendNote(text string) {
	appendText("- " + text + "\n")
}

func appendText(text string) {
	path := config.ReportPath()
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(text)
}

// Short returns the first line of text, truncated to limit runes.
func Short(text string, limit int) string {
	line := ""
	for _, l := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimSpace(l)
		break
	}
	runes := []rune(line)
	if len(runes) > limit {
		return string(runes[:limit]) + "..."
	}
	return line
}
