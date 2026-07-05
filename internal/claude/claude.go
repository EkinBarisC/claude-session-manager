// Package claude runs one queue item through `claude -p` and interprets
// the outcome.
//
// Billing safety: the subprocess environment is stripped of every variable
// that could route the Claude Code CLI to pay-per-token API billing or a
// third-party provider. With those gone the CLI can only use the stored
// subscription (Pro) OAuth login. If it isn't logged in, the run fails with
// an auth error instead of spending money.
package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/EkinBarisC/claude-session-manager/internal/config"
	"github.com/EkinBarisC/claude-session-manager/internal/queue"
)

// StripEnv lists variables that could redirect the CLI to usage-based billing.
var StripEnv = []string{
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_PROFILE",
	"ANTHROPIC_MODEL",
	"CLAUDE_CODE_USE_BEDROCK",
	"CLAUDE_CODE_USE_VERTEX",
	"AWS_BEARER_TOKEN_BEDROCK",
}

// Protocol is appended to every queued prompt. It keeps sessions cheap to
// rotate: state always lives in ./context.md, so a fresh session can pick
// up mid-stream.
const Protocol = `
---
Automated run rules (csm):
1. If ./context.md exists in the project root, read it first for prior state.
2. Do all work on a git branch named csm/<short-task-slug> (create it if
   needed). Commit your changes. NEVER push, force-reset, or delete branches.
3. Before finishing, create or update ./context.md (max 150 lines): current
   state, key decisions, remaining work, and the active branch name.
4. End your reply with exactly one line: SUMMARY: <one sentence result>.
`

var (
	rateLimitRe = regexp.MustCompile(`(?i)usage limit|rate limit|limit reached|limit will reset|out of extra usage`)
	authErrorRe = regexp.MustCompile(`(?i)/login|not logged in|invalid api key|api key not found|oauth token|authentication_error|please log in`)
	// Claude Code prints e.g. "Claude AI usage limit reached|1712345678"
	epochRe   = regexp.MustCompile(`\|(\d{10})\b`)
	resetAtRe = regexp.MustCompile(`(?i)reset(?:s)?\s+(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
)

type Result struct {
	OK          bool
	RateLimited bool
	AuthError   bool
	TimedOut    bool
	ResetAt     *time.Time
	SessionID   string
	Usage       map[string]int
	ResultText  string
	Summary     string
	Error       string
}

// ContextTokens estimates the session's context size after this run.
func (r Result) ContextTokens() int {
	return r.Usage["input_tokens"] +
		r.Usage["cache_creation_input_tokens"] +
		r.Usage["cache_read_input_tokens"] +
		r.Usage["output_tokens"]
}

// StrippedEnv returns base without billing-capable variables, plus the
// names that were removed.
func StrippedEnv(base []string) (env []string, removed []string) {
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		stripped := false
		for _, bad := range StripEnv {
			if strings.EqualFold(name, bad) {
				removed = append(removed, name)
				stripped = true
				break
			}
		}
		if !stripped {
			env = append(env, kv)
		}
	}
	return env, removed
}

// ItemSettings resolves (model, effort, mode) for an item, falling back
// to config.
func ItemSettings(cfg config.Config, item *queue.Item) (model, effort, mode string) {
	model = item.Model
	if model == "" {
		model = cfg.DefaultModel
	}
	effort = item.Effort
	if effort == "" {
		effort = cfg.DefaultEffort
	}
	mode = item.Mode
	if mode == "" {
		mode = cfg.DefaultRunMode
	}
	if mode == "" {
		mode = "safe"
	}
	return model, effort, mode
}

// BuildCommand assembles the argv for one item run.
func BuildCommand(cfg config.Config, item *queue.Item, resumeID string) ([]string, error) {
	binary, err := exec.LookPath(cfg.ClaudeBinary)
	if err != nil {
		return nil, fmt.Errorf("'%s' not found on PATH. Install Claude Code and log in with your Pro account first", cfg.ClaudeBinary)
	}
	model, effort, mode := ItemSettings(cfg, item)
	cmd := []string{binary}
	if resumeID != "" {
		cmd = append(cmd, "--resume", resumeID)
	}
	cmd = append(cmd,
		"-p", item.Prompt+Protocol,
		"--output-format", "json",
		"--model", model,
	)
	if effort != "" {
		cmd = append(cmd, "--effort", effort)
	}
	switch mode {
	case "plan":
		cmd = append(cmd, "--permission-mode", "plan")
	case "full":
		cmd = append(cmd, "--dangerously-skip-permissions")
	default: // safe
		cmd = append(cmd, "--permission-mode", "acceptEdits")
		if len(cfg.AllowedTools) > 0 {
			cmd = append(cmd, "--allowedTools", strings.Join(cfg.AllowedTools, ","))
		}
	}
	if mode != "plan" && len(cfg.DisallowedTools) > 0 {
		cmd = append(cmd, "--disallowedTools", strings.Join(cfg.DisallowedTools, ","))
	}
	return cmd, nil
}

// ParseResetTime extracts a usage-limit reset time from CLI output.
func ParseResetTime(text string, now time.Time) *time.Time {
	if m := epochRe.FindStringSubmatch(text); m != nil {
		if secs, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			t := time.Unix(secs, 0)
			return &t
		}
	}
	if m := resetAtRe.FindStringSubmatch(text); m != nil {
		hour, _ := strconv.Atoi(m[1])
		minute := 0
		if m[2] != "" {
			minute, _ = strconv.Atoi(m[2])
		}
		meridiem := strings.ToLower(m[3])
		if meridiem == "pm" && hour < 12 {
			hour += 12
		}
		if meridiem == "am" && hour == 12 {
			hour = 0
		}
		if hour > 23 || minute > 59 {
			return nil
		}
		candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
		if !candidate.After(now) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		return &candidate
	}
	return nil
}

type payload struct {
	SessionID string         `json:"session_id"`
	Usage     map[string]int `json:"usage"`
	Result    string         `json:"result"`
	IsError   bool           `json:"is_error"`
}

// RunItem executes one queue item and interprets the outcome.
func RunItem(cfg config.Config, item *queue.Item, resumeID string, env []string) Result {
	var res Result
	argv, err := BuildCommand(cfg, item, resumeID)
	if err != nil {
		res.Error = err.Error()
		return res
	}

	timeout := time.Duration(cfg.ItemTimeoutMinutes) * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = item.Project
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.TimedOut = true
		res.Error = fmt.Sprintf("timed out after %d minutes", cfg.ItemTimeoutMinutes)
		return res
	}

	combined := stdout.String() + "\n" + stderr.String()

	if p := parseJSONPayload(stdout.String()); p != nil {
		res.SessionID = p.SessionID
		res.Usage = p.Usage
		res.ResultText = p.Result
	}

	if authErrorRe.MatchString(combined) {
		res.AuthError = true
		res.Error = "auth error - claude is not logged in with the subscription account"
		return res
	}
	if rateLimitRe.MatchString(combined) {
		res.RateLimited = true
		res.ResetAt = ParseResetTime(combined, time.Now())
		res.Error = "usage limit reached"
		return res
	}

	isError := false
	if p := parseJSONPayload(stdout.String()); p != nil {
		isError = p.IsError
	}
	if runErr != nil || isError {
		fallback := res.ResultText
		if fallback == "" {
			fallback = stderr.String()
		}
		if fallback == "" {
			fallback = stdout.String()
		}
		if strings.TrimSpace(fallback) == "" {
			fallback = "unknown error"
		}
		res.Error = firstLine(fallback, 200)
		return res
	}

	res.OK = true
	res.Summary = ExtractSummary(res.ResultText)
	// A run that ends by asking us something needs a human, not the queue.
	lastLine := lastNonEmptyLine(res.ResultText)
	if res.Summary == "" && strings.HasSuffix(lastLine, "?") {
		res.OK = false
		res.Error = "ended with a question: " + firstLine(lastLine, 200)
	}
	return res
}

// ExtractSummary finds the trailing "SUMMARY: ..." line of a result.
func ExtractSummary(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(strings.ToUpper(line), "SUMMARY:") {
			return strings.TrimSpace(line[len("SUMMARY:"):])
		}
	}
	return ""
}

func parseJSONPayload(stdout string) *payload {
	text := strings.TrimSpace(stdout)
	start := strings.Index(text, "{")
	if start == -1 {
		return nil
	}
	var p payload
	if err := json.Unmarshal([]byte(text[start:]), &p); err != nil {
		return nil
	}
	return &p
}

func firstLine(text string, limit int) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	runes := []rune(line)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return line
}

func lastNonEmptyLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}
