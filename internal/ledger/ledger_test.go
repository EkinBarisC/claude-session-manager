package ledger

import (
	"testing"
	"time"

	"github.com/EkinBarisC/claude-session-manager/internal/config"
)

func TestWeighted(t *testing.T) {
	usage := map[string]int{
		"input_tokens":                100,
		"cache_creation_input_tokens": 200,
		"output_tokens":               300,
		"cache_read_input_tokens":     1000,
	}
	if got := Weighted(usage); got != 700 {
		t.Errorf("want 700 (100+200+300+0.1*1000), got %d", got)
	}
	if got := Weighted(map[string]int{}); got != 0 {
		t.Errorf("empty usage should weigh 0, got %d", got)
	}
}

func TestWeeklySpendWindow(t *testing.T) {
	t.Setenv("CSM_HOME", t.TempDir())

	old := Record{
		TS:       time.Now().UTC().AddDate(0, 0, -8).Format(time.RFC3339),
		Weighted: 500,
	}
	recent := Record{
		TS:       time.Now().UTC().AddDate(0, 0, -1).Format(time.RFC3339),
		Weighted: 300,
	}
	pythonFormat := Record{ // timestamps written by the old Python version
		TS:       time.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02T15:04:05+00:00"),
		Weighted: 200,
	}
	if err := config.WriteJSON(config.LedgerPath(), []Record{old, recent, pythonFormat}); err != nil {
		t.Fatal(err)
	}
	if got := WeeklySpend(); got != 500 {
		t.Errorf("want 500 (300 recent + 200 python-format, 500 too old excluded), got %d", got)
	}
}
