package config

import (
	"testing"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		key   string
		raw   string
		valid bool
	}{
		{"default_effort", `"medium"`, true},
		{"default_effort", `""`, true},
		{"default_effort", `"bogus"`, false},
		{"default_run_mode", `"plan"`, true},
		{"default_run_mode", `"yolo"`, false},
		{"weekly_token_budget", `2000000`, true},
		{"weekly_token_budget", `0`, false},
		{"weekly_token_budget", `"lots"`, false},
		{"weekly_token_budget", `1.5`, false},
		{"context_rotate_pct", `40`, true},
		{"context_rotate_pct", `101`, false},
		{"quiet_hours_start", `"00:30"`, true},
		{"quiet_hours_start", `"24:00"`, false},
		{"quiet_hours_start", `"7:5"`, false},
		{"allowed_tools", `["Read", "Edit"]`, true},
		{"allowed_tools", `"Read"`, false},
		{"allowed_tools", `[1, 2]`, false},
		{"default_model", `"sonnet"`, true},
		{"default_model", `""`, false},
		{"claude_binary", `"claude"`, true},
		{"no_such_key", `"x"`, false},
	}
	for _, c := range cases {
		err := Validate(c.key, ParseValue(c.raw))
		if c.valid && err != nil {
			t.Errorf("Validate(%s, %s): unexpected error %v", c.key, c.raw, err)
		}
		if !c.valid && err == nil {
			t.Errorf("Validate(%s, %s): expected error, got none", c.key, c.raw)
		}
	}
}

func TestParseValue(t *testing.T) {
	if v := ParseValue("2000000"); v != float64(2000000) {
		t.Errorf("numeric string should parse as JSON number, got %#v", v)
	}
	if v := ParseValue("sonnet"); v != "sonnet" {
		t.Errorf("bare word should stay a string, got %#v", v)
	}
	if v := ParseValue(`"quoted"`); v != "quoted" {
		t.Errorf("quoted JSON string should unquote, got %#v", v)
	}
	if _, ok := ParseValue(`["a","b"]`).([]any); !ok {
		t.Errorf("JSON list should parse as list")
	}
}

func TestLoadMergesOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CSM_HOME", dir)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "sonnet" || cfg.WeeklyTokenBudget != 1_000_000 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}

	if err := SetValue("default_model", "haiku"); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "haiku" {
		t.Fatalf("override not applied, got %s", cfg.DefaultModel)
	}
	if cfg.WeeklyTokenBudget != 1_000_000 {
		t.Fatalf("unrelated default lost, got %d", cfg.WeeklyTokenBudget)
	}

	if err := UnsetValue("default_model"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = Load()
	if cfg.DefaultModel != "sonnet" {
		t.Fatalf("unset did not restore default, got %s", cfg.DefaultModel)
	}
}
