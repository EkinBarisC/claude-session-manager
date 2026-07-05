package schedule

import (
	"strings"
	"testing"
)

func TestCronLine(t *testing.T) {
	line, err := CronLine("00:30", "07:30",
		"/usr/local/bin:/home/u/.local/bin", "/usr/local/bin/csm", "/home/u/.csm/cron.log")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "30 0 * * * ") {
		t.Errorf("wrong schedule prefix: %s", line)
	}
	if !strings.HasSuffix(line, CronMarker) {
		t.Errorf("marker missing: %s", line)
	}
	for _, want := range []string{
		"PATH=/usr/local/bin:/home/u/.local/bin",
		"/usr/local/bin/csm run --until 07:30",
		">> /home/u/.csm/cron.log 2>&1",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("missing %q in: %s", want, line)
		}
	}
}

func TestCronLineQuotesAndEscapes(t *testing.T) {
	line, err := CronLine("01:15", "06:00",
		"/usr/bin", "/opt/my tools/csm", "/logs/100%.log")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "'/opt/my tools/csm'") {
		t.Errorf("path with spaces not quoted: %s", line)
	}
	if strings.Contains(line, "100%.log") || !strings.Contains(line, `100\%`) {
		t.Errorf("%% not escaped for cron: %s", line)
	}
	if !strings.HasPrefix(line, "15 1 * * * ") {
		t.Errorf("wrong schedule prefix: %s", line)
	}
}

func TestCronLineRejectsBadTime(t *testing.T) {
	if _, err := CronLine("late", "07:30", "p", "x", "l"); err == nil {
		t.Error("invalid start time should error")
	}
}
