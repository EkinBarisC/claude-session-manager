// Package ledger records token usage per run. The weekly budget guard works
// on a rolling 7-day sum of *weighted* tokens:
//
//	input + cache_creation + output + 0.1 * cache_read
//
// This is a heuristic for subscription quota pressure, not billing (nothing
// is billed - subscription auth only).
package ledger

import (
	"math"
	"time"

	"github.com/EkinBarisC/claude-session-manager/internal/config"
)

type Record struct {
	TS       string         `json:"ts"`
	ItemID   string         `json:"item_id"`
	Project  string         `json:"project"`
	Model    string         `json:"model"`
	Usage    map[string]int `json:"usage"`
	Weighted int            `json:"weighted"`
}

func Load() []Record {
	var records []Record
	config.ReadJSON(config.LedgerPath(), &records)
	return records
}

func Weighted(usage map[string]int) int {
	return int(math.Round(
		float64(usage["input_tokens"]) +
			float64(usage["cache_creation_input_tokens"]) +
			float64(usage["output_tokens"]) +
			0.1*float64(usage["cache_read_input_tokens"])))
}

func Append(itemID, project, model string, usage map[string]int) error {
	records := append(Load(), Record{
		TS:       time.Now().UTC().Format("2006-01-02T15:04:05+00:00"),
		ItemID:   itemID,
		Project:  project,
		Model:    model,
		Usage:    usage,
		Weighted: Weighted(usage),
	})
	return config.WriteJSON(config.LedgerPath(), records)
}

// WeeklySpend sums weighted tokens over the last 7 days.
func WeeklySpend() int {
	cutoff := time.Now().UTC().AddDate(0, 0, -7)
	total := 0
	for _, rec := range Load() {
		ts, err := time.Parse(time.RFC3339, rec.TS)
		if err != nil {
			continue
		}
		if !ts.Before(cutoff) {
			total += rec.Weighted
		}
	}
	return total
}
