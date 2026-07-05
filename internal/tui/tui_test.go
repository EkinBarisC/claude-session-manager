package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditableValue(t *testing.T) {
	if got := editableValue("sonnet"); got != "sonnet" {
		t.Errorf("strings should stay bare, got %q", got)
	}
	if got := editableValue(float64(42)); got != "42" {
		t.Errorf("numbers as JSON, got %q", got)
	}
	if got := editableValue([]any{"a", "b"}); got != `["a","b"]` {
		t.Errorf("lists as compact JSON, got %q", got)
	}
}

func TestCommonPrefix(t *testing.T) {
	if got := commonPrefix([]string{"internal", "interface", "intern"}); got != "inter" {
		t.Errorf("want inter, got %q", got)
	}
	if got := commonPrefix([]string{"solo"}); got != "solo" {
		t.Errorf("single name is its own prefix, got %q", got)
	}
}

func TestCompleteProject(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"projects", "proto", "unrelated"} {
		os.Mkdir(filepath.Join(dir, d), 0o755)
	}

	f := newForm()
	f.inputs[fieldProject].SetValue(filepath.Join(dir, "pro"))
	f.completeProject()
	got := f.inputs[fieldProject].Value()
	if !strings.HasSuffix(got, "pro") || f.hint == "" {
		t.Errorf("ambiguous prefix should keep common prefix and hint candidates; value=%q hint=%q", got, f.hint)
	}

	f.inputs[fieldProject].SetValue(filepath.Join(dir, "proj"))
	f.completeProject()
	got = f.inputs[fieldProject].Value()
	if !strings.HasSuffix(got, "projects"+string(filepath.Separator)) {
		t.Errorf("unique prefix should complete fully, got %q", got)
	}

	f.inputs[fieldProject].SetValue(filepath.Join(dir, "zzz"))
	f.completeProject()
	if f.hint != "no matching directory" {
		t.Errorf("miss should hint, got %q", f.hint)
	}
}

func TestHighlightPreservesText(t *testing.T) {
	jsonDoc := `{"default_model": "sonnet", "weekly_token_budget": 1000000, "x": true}`
	plain := stripANSI(highlightJSON(jsonDoc))
	if plain != jsonDoc {
		t.Errorf("highlighting must not alter JSON text:\n%s\n%s", jsonDoc, plain)
	}

	reportDoc := "## Run 2026-07-05 (manual)\n### [done] abcd - task\n- project: `C:\\x`\n- error: boom"
	plain = stripANSI(highlightReport(reportDoc))
	if plain != reportDoc {
		t.Errorf("highlighting must not alter report text:\n%q\n%q", reportDoc, plain)
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case inEscape:
			if r == 'm' {
				inEscape = false
			}
		case r == '\x1b':
			inEscape = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
