package claude

import "testing"

// Real /usage output shape from claude 2.1.x.
const usageReport = `You are currently using your subscription to power your Claude Code usage

Current session: 38% used · resets Jul 6, 1:29am (Europe/Istanbul)
Current week (all models): 23% used · resets Jul 12, 4:59am (Europe/Istanbul)
Current week (Fable): 31% used · resets Jul 12, 4:59am (Europe/Istanbul)

What's contributing to your limits usage?
Last 24h · 169 requests · 3 sessions`

func TestParseUsageReport(t *testing.T) {
	limits := ParseUsageReport(usageReport)
	if len(limits) != 3 {
		t.Fatalf("want 3 limits, got %d: %+v", len(limits), limits)
	}
	session := limits[0]
	if session.Scope != "session" || session.Pct != 38 ||
		session.Resets != "Jul 6, 1:29am (Europe/Istanbul)" {
		t.Errorf("session limit wrong: %+v", session)
	}
	week := limits[1]
	if week.Scope != "week (all models)" || week.Pct != 23 {
		t.Errorf("week limit wrong: %+v", week)
	}
	if limits[2].Scope != "week (Fable)" || limits[2].Pct != 31 {
		t.Errorf("per-model week limit wrong: %+v", limits[2])
	}

	if got := FormatLimits(limits[:2]); got != "session 38%, week (all models) 23%" {
		t.Errorf("FormatLimits wrong: %q", got)
	}
}

func TestParseUsageReportGarbage(t *testing.T) {
	if limits := ParseUsageReport("no usage lines here"); len(limits) != 0 {
		t.Errorf("garbage should yield no limits, got %+v", limits)
	}
}
