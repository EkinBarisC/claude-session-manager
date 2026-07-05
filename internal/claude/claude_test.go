package claude

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/EkinBarisC/claude-session-manager/internal/config"
	"github.com/EkinBarisC/claude-session-manager/internal/queue"
)

func TestStrippedEnv(t *testing.T) {
	env, removed := StrippedEnv([]string{
		"PATH=/usr/bin",
		"ANTHROPIC_API_KEY=sk-secret",
		"CLAUDE_CODE_USE_BEDROCK=1",
		"HOME=/home/u",
	})
	if len(removed) != 2 {
		t.Fatalf("want 2 removed, got %v", removed)
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, "ANTHROPIC") || strings.HasPrefix(kv, "CLAUDE_CODE_USE") {
			t.Errorf("billing var leaked: %s", kv)
		}
	}
	if len(env) != 2 {
		t.Errorf("safe vars should survive, got %v", env)
	}
}

func TestItemSettings(t *testing.T) {
	cfg := config.Defaults()
	item := &queue.Item{}
	model, effort, mode := ItemSettings(cfg, item)
	if model != "sonnet" || effort != "medium" || mode != "safe" {
		t.Errorf("config fallback broken: %s/%s/%s", model, effort, mode)
	}

	item = &queue.Item{Model: "haiku", Effort: "max", Mode: "plan"}
	model, effort, mode = ItemSettings(cfg, item)
	if model != "haiku" || effort != "max" || mode != "plan" {
		t.Errorf("item overrides ignored: %s/%s/%s", model, effort, mode)
	}
}

func TestParseResetTimeEpoch(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.Local)
	got := ParseResetTime("Claude AI usage limit reached|1751719800", now)
	if got == nil {
		t.Fatal("epoch timestamp not parsed")
	}
	if got.Unix() != 1751719800 {
		t.Errorf("wrong epoch: %d", got.Unix())
	}
}

func TestParseResetTimeClock(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.Local)

	got := ParseResetTime("Your limit will reset at 3pm", now)
	if got == nil || got.Hour() != 15 || got.Day() != 5 {
		t.Fatalf("3pm same day expected, got %v", got)
	}

	got = ParseResetTime("resets 9am", now)
	if got == nil || got.Hour() != 9 || got.Day() != 6 {
		t.Fatalf("9am should roll to tomorrow, got %v", got)
	}

	if ParseResetTime("no times here", now) != nil {
		t.Error("garbage should yield nil")
	}
}

func TestExtractSummary(t *testing.T) {
	text := "did stuff\nmore stuff\nSUMMARY: fixed the tests"
	if got := ExtractSummary(text); got != "fixed the tests" {
		t.Errorf("got %q", got)
	}
	if got := ExtractSummary("no summary line"); got != "" {
		t.Errorf("want empty, got %q", got)
	}
	if got := ExtractSummary("summary: lowercase works"); got != "lowercase works" {
		t.Errorf("case-insensitive match failed, got %q", got)
	}
}

func TestBuildCommandModes(t *testing.T) {
	cfg := config.Defaults()
	cfg.ClaudeBinary = "go" // any binary that exists on PATH in CI
	base := &queue.Item{Prompt: "task", Project: "."}

	argvFor := func(mode string, resume string) []string {
		item := *base
		item.Mode = mode
		argv, err := BuildCommand(cfg, &item, resume)
		if err != nil {
			t.Fatal(err)
		}
		return argv
	}

	safe := argvFor("safe", "")
	if !slices.Contains(safe, "--allowedTools") || !slices.Contains(safe, "--disallowedTools") {
		t.Errorf("safe mode must pass tool lists: %v", safe)
	}
	if !slices.Contains(safe, "acceptEdits") {
		t.Errorf("safe mode must use acceptEdits: %v", safe)
	}

	plan := argvFor("plan", "")
	if slices.Contains(plan, "--allowedTools") || slices.Contains(plan, "--disallowedTools") {
		t.Errorf("plan mode must not pass tool lists: %v", plan)
	}
	if !slices.Contains(plan, "plan") {
		t.Errorf("plan mode must use --permission-mode plan: %v", plan)
	}

	full := argvFor("full", "")
	if !slices.Contains(full, "--dangerously-skip-permissions") {
		t.Errorf("full mode flag missing: %v", full)
	}
	if !slices.Contains(full, "--disallowedTools") {
		t.Errorf("full mode should still block disallowed tools: %v", full)
	}

	resumed := argvFor("safe", "sess-123")
	if !slices.Contains(resumed, "--resume") || !slices.Contains(resumed, "sess-123") {
		t.Errorf("resume flags missing: %v", resumed)
	}

	// the protocol must ride along with every prompt
	for i, arg := range safe {
		if arg == "-p" && !strings.Contains(safe[i+1], "SUMMARY:") {
			t.Error("protocol not appended to prompt")
		}
	}
}
