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

// realPayload mirrors the shape claude 2.1.x actually prints: usage mixes
// numbers with nested objects, strings, and the per-call iterations array.
// A strictly-typed usage map fails to unmarshal this whole object, which
// silently dropped tokens, session id, and summary (the bug this guards).
const realPayload = `{"type":"result","subtype":"success","is_error":false,
"result":"did the thing\nBRANCH: csm/fix-tests\nSUMMARY: fixed the tests",
"session_id":"b64f3847-6a02-426e-aba9-8ace766fe16f",
"usage":{"input_tokens":4692,"cache_creation_input_tokens":54219,
"cache_read_input_tokens":1635031,"output_tokens":23419,
"server_tool_use":{"web_search_requests":0},"service_tier":"standard",
"cache_creation":{"ephemeral_1h_input_tokens":54219,"ephemeral_5m_input_tokens":0},
"inference_geo":"not_available","speed":"standard",
"iterations":[
 {"input_tokens":2,"output_tokens":1809,"cache_read_input_tokens":61684,"cache_creation_input_tokens":1512,"type":"message"},
 {"input_tokens":5,"output_tokens":900,"cache_read_input_tokens":120000,"cache_creation_input_tokens":2000,"type":"message"}
]}}`

func TestParseRealPayload(t *testing.T) {
	p := parseJSONPayload(realPayload)
	if p == nil {
		t.Fatal("real claude output must parse")
	}
	if p.SessionID != "b64f3847-6a02-426e-aba9-8ace766fe16f" {
		t.Errorf("session id lost: %q", p.SessionID)
	}
	if UsageInt(p.Usage, "input_tokens") != 4692 || UsageInt(p.Usage, "output_tokens") != 23419 {
		t.Errorf("usage numbers lost: %v", p.Usage)
	}
	if got := ExtractSummary(p.Result); got != "fixed the tests" {
		t.Errorf("summary lost: %q", got)
	}
	if got := ExtractBranch(p.Result); got != "csm/fix-tests" {
		t.Errorf("branch lost: %q", got)
	}
}

func TestContextTokensUsesLastIteration(t *testing.T) {
	p := parseJSONPayload(realPayload)
	res := Result{Usage: p.Usage}
	// last iteration: 5 + 120000 + 2000 + 900, NOT the cumulative 1.7M
	if got := res.ContextTokens(); got != 122905 {
		t.Errorf("context should come from the last iteration, got %d", got)
	}

	// without iterations, fall back to the cumulative totals
	res = Result{Usage: map[string]any{"input_tokens": float64(100), "output_tokens": float64(50)}}
	if got := res.ContextTokens(); got != 150 {
		t.Errorf("fallback sum wrong: %d", got)
	}
}

func TestExtractBranchNone(t *testing.T) {
	if got := ExtractBranch("BRANCH: none\nSUMMARY: reviewed only"); got != "" {
		t.Errorf(`"none" should mean no branch, got %q`, got)
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
