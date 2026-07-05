// Package config holds settings and state-file paths. All state lives in
// ~/.csm (override with CSM_HOME). Files are JSON and remain compatible
// with state written by the earlier Python implementation.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var (
	EffortLevels = []string{"low", "medium", "high", "xhigh", "max"}
	RunModes     = []string{"plan", "safe", "full"}

	hhmmRe = regexp.MustCompile(`^([01]?\d|2[0-3]):[0-5]\d$`)
)

type Config struct {
	// Model for queue items that don't specify one. Pro has no Opus in
	// Claude Code; sonnet stretches each 5h window across more items.
	DefaultModel string `json:"default_model"`
	// Effort passed to `claude --effort` (low|medium|high|xhigh|max).
	// Claude Code's own default is xhigh; medium stretches quota further.
	// Empty string means use the CLI default.
	DefaultEffort string `json:"default_effort"`
	// Run mode for items without --mode:
	//   plan  -> read-only planning (--permission-mode plan)
	//   safe  -> allowlisted edits/tests/commits, push blocked (default)
	//   full  -> --dangerously-skip-permissions (use only for sandboxed dirs)
	DefaultRunMode string `json:"default_run_mode"`
	// Rolling 7-day budget of weighted tokens the bot may spend
	// (input + cache_creation + output + 0.1 * cache_read).
	WeeklyTokenBudget int `json:"weekly_token_budget"`
	// Context rotation: when a session's estimated context exceeds
	// ContextRotatePct % of ContextWindowTokens, the next task for that
	// project starts a fresh session (which reads context.md).
	ContextWindowTokens int `json:"context_window_tokens"`
	ContextRotatePct    int `json:"context_rotate_pct"`
	// Hard timeout per queue item.
	ItemTimeoutMinutes int `json:"item_timeout_minutes"`
	// Used by `csm schedule` for the nightly job.
	QuietHoursStart string `json:"quiet_hours_start"`
	QuietHoursEnd   string `json:"quiet_hours_end"`
	ClaudeBinary    string `json:"claude_binary"`
	// Passed to `claude -p` via --allowedTools / --disallowedTools.
	// Edits, tests, and branch-local git are allowed; push and destructive
	// operations are blocked.
	AllowedTools    []string `json:"allowed_tools"`
	DisallowedTools []string `json:"disallowed_tools"`
}

func Defaults() Config {
	return Config{
		DefaultModel:        "sonnet",
		DefaultEffort:       "medium",
		DefaultRunMode:      "safe",
		WeeklyTokenBudget:   1_000_000,
		ContextWindowTokens: 200_000,
		ContextRotatePct:    40,
		ItemTimeoutMinutes:  30,
		QuietHoursStart:     "00:30",
		QuietHoursEnd:       "07:30",
		ClaudeBinary:        "claude",
		AllowedTools: []string{
			"Read", "Edit", "Write", "Glob", "Grep", "WebSearch", "WebFetch",
			"Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)",
			"Bash(git add:*)", "Bash(git commit:*)", "Bash(git branch:*)",
			"Bash(git checkout:*)", "Bash(git switch:*)", "Bash(git stash:*)",
			"Bash(mkdir:*)", "Bash(npm test:*)", "Bash(npm run:*)",
			"Bash(npx:*)", "Bash(node:*)", "Bash(python:*)", "Bash(py:*)",
			"Bash(pytest:*)", "Bash(pip install:*)",
		},
		DisallowedTools: []string{
			"Bash(git push:*)", "Bash(git reset:*)", "Bash(git rebase:*)",
			"Bash(git clean:*)", "Bash(git remote:*)",
			"Bash(rm:*)", "Bash(del:*)", "Bash(rmdir:*)",
		},
	}
}

func StateDir() string {
	if dir := os.Getenv("CSM_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".csm"
	}
	return filepath.Join(home, ".csm")
}

func ConfigPath() string   { return filepath.Join(StateDir(), "config.json") }
func QueuePath() string    { return filepath.Join(StateDir(), "queue.json") }
func SessionsPath() string { return filepath.Join(StateDir(), "sessions.json") }
func LedgerPath() string   { return filepath.Join(StateDir(), "ledger.json") }
func ReportPath() string   { return filepath.Join(StateDir(), "report.md") }

// Load returns defaults overlaid with whatever the config file sets.
func Load() (Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(ConfigPath())
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("cannot read %s: %w", ConfigPath(), err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("cannot parse %s: %w", ConfigPath(), err)
	}
	return cfg, nil
}

// EnsureInit creates the state dir and a default config file if missing.
func EnsureInit() (string, error) {
	if err := os.MkdirAll(StateDir(), 0o755); err != nil {
		return "", err
	}
	path := ConfigPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := WriteJSON(path, Defaults()); err != nil {
			return "", err
		}
	}
	return path, nil
}

// Overrides returns the raw config file contents (no defaults merged in).
func Overrides() map[string]any {
	out := map[string]any{}
	ReadJSON(ConfigPath(), &out)
	return out
}

// ParseValue turns a CLI string into a typed value: JSON if it parses,
// plain string otherwise.
func ParseValue(raw string) any {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		return v
	}
	return raw
}

// KnownKeys lists every config key, in a stable order.
func KnownKeys() []string {
	return []string{
		"allowed_tools", "claude_binary", "context_rotate_pct",
		"context_window_tokens", "default_effort", "default_model",
		"default_run_mode", "disallowed_tools", "item_timeout_minutes",
		"quiet_hours_end", "quiet_hours_start", "weekly_token_budget",
	}
}

// Validate returns an error if (key, value) is not a valid setting.
// value is the loosely-typed result of ParseValue.
func Validate(key string, value any) error {
	if !slices.Contains(KnownKeys(), key) {
		return fmt.Errorf("unknown key '%s' (known: %s)", key, strings.Join(KnownKeys(), ", "))
	}
	switch key {
	case "default_effort":
		if s, ok := value.(string); (ok && s != "" && !slices.Contains(EffortLevels, s)) || (!ok && value != nil) {
			return fmt.Errorf("default_effort must be one of %s, or \"\" for the CLI default", strings.Join(EffortLevels, ", "))
		}
	case "default_run_mode":
		if s, ok := value.(string); !ok || !slices.Contains(RunModes, s) {
			return fmt.Errorf("default_run_mode must be one of %s", strings.Join(RunModes, ", "))
		}
	case "weekly_token_budget", "context_window_tokens", "item_timeout_minutes":
		if n, ok := asInt(value); !ok || n <= 0 {
			return fmt.Errorf("%s must be a positive integer", key)
		}
	case "context_rotate_pct":
		if n, ok := asInt(value); !ok || n < 1 || n > 100 {
			return fmt.Errorf("context_rotate_pct must be an integer between 1 and 100")
		}
	case "quiet_hours_start", "quiet_hours_end":
		if s, ok := value.(string); !ok || !hhmmRe.MatchString(s) {
			return fmt.Errorf("%s must be HH:MM (24h), e.g. 00:30", key)
		}
	case "allowed_tools", "disallowed_tools":
		list, ok := value.([]any)
		if !ok {
			return fmt.Errorf(`%s must be a JSON list of strings, e.g. '["Read", "Edit"]'`, key)
		}
		for _, v := range list {
			if _, ok := v.(string); !ok {
				return fmt.Errorf("%s must contain only strings", key)
			}
		}
	case "default_model", "claude_binary":
		if s, ok := value.(string); !ok || strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s must be a non-empty string", key)
		}
	}
	return nil
}

func asInt(v any) (int, bool) {
	f, ok := v.(float64) // encoding/json decodes all numbers as float64
	if !ok || f != float64(int(f)) {
		return 0, false
	}
	return int(f), true
}

// SetValue writes one key into the config file, preserving other overrides.
func SetValue(key string, value any) error {
	if _, err := EnsureInit(); err != nil {
		return err
	}
	data := Overrides()
	data[key] = value
	return WriteJSON(ConfigPath(), data)
}

// UnsetValue resets a key to its default by removing it from the file.
func UnsetValue(key string) error {
	if _, err := EnsureInit(); err != nil {
		return err
	}
	data := Overrides()
	delete(data, key)
	return WriteJSON(ConfigPath(), data)
}

// DefaultFor returns the default value of a key as it appears in JSON.
func DefaultFor(key string) any {
	raw, _ := json.Marshal(Defaults())
	m := map[string]any{}
	json.Unmarshal(raw, &m)
	return m[key]
}

// ReadJSON loads path into out; missing or unparseable files leave out as-is.
func ReadJSON(path string, out any) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	json.Unmarshal(data, out)
}

// WriteJSON writes v as indented JSON via a temp file + rename.
func WriteJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
