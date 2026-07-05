package claude

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/EkinBarisC/claude-session-manager/internal/config"
)

// Limit is one line of Claude Code's /usage report: how much of a rolling
// quota window is used and when it resets.
type Limit struct {
	Scope  string // "session" (5h window) or "week (all models)" etc.
	Pct    int
	Resets string // human text as claude prints it, may be ""
}

var (
	sessionLimitRe = regexp.MustCompile(`(?im)^current session:\s*(\d+)%\s*used(?:\s*·\s*resets\s*(.+))?$`)
	weekLimitRe    = regexp.MustCompile(`(?im)^current week \(([^)]+)\):\s*(\d+)%\s*used(?:\s*·\s*resets\s*(.+))?$`)
)

// FetchUsage asks the claude CLI for real plan usage. The /usage slash
// command works in print mode and reports zero token cost, so this is free
// to call. Returns the parsed limits plus the raw report text.
func FetchUsage(cfg config.Config) ([]Limit, string, error) {
	binary, err := exec.LookPath(cfg.ClaudeBinary)
	if err != nil {
		return nil, "", fmt.Errorf("'%s' not found on PATH", cfg.ClaudeBinary)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-p", "/usage", "--output-format", "json")
	cmd.Dir = config.StateDir() // neutral cwd: no project trust involved
	cmd.Env, _ = StrippedEnv(os.Environ())
	cmd.Stdin = strings.NewReader("") // avoid the CLI waiting on piped stdin
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, "", fmt.Errorf("claude /usage failed: %w", err)
	}
	payload := parseJSONPayload(stdout.String())
	if payload == nil || payload.Result == "" {
		return nil, "", fmt.Errorf("claude /usage returned no report")
	}
	return ParseUsageReport(payload.Result), payload.Result, nil
}

// FormatLimits renders limits on one line: "session 38%, week (all models) 23%".
func FormatLimits(limits []Limit) string {
	parts := make([]string, len(limits))
	for i, l := range limits {
		parts[i] = fmt.Sprintf("%s %d%%", l.Scope, l.Pct)
	}
	return strings.Join(parts, ", ")
}

// ParseUsageReport pulls the limit lines out of the /usage text.
func ParseUsageReport(text string) []Limit {
	var limits []Limit
	if m := sessionLimitRe.FindStringSubmatch(text); m != nil {
		pct, _ := strconv.Atoi(m[1])
		limits = append(limits, Limit{Scope: "session", Pct: pct, Resets: strings.TrimSpace(m[2])})
	}
	for _, m := range weekLimitRe.FindAllStringSubmatch(text, -1) {
		pct, _ := strconv.Atoi(m[2])
		limits = append(limits, Limit{
			Scope:  "week (" + m[1] + ")",
			Pct:    pct,
			Resets: strings.TrimSpace(m[3]),
		})
	}
	return limits
}
