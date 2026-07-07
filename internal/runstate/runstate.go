// Package runstate answers "is a run happening?" and "how did the last run
// go?" via two small files in the state dir.
//
// run.lock exists while a run is in progress and doubles as mutual
// exclusion: a second runner refuses to start, so the nightly job and a
// manual `csm run` can never mutate queue.json concurrently. A lock whose
// pid is no longer alive (crash, power loss) is treated as stale and
// silently replaced.
//
// lastrun.json is a heartbeat written at the end of every run so `csm
// status` and `csm doctor` can report when the queue was last worked on
// and why the run stopped.
package runstate

import (
	"fmt"
	"os"
	"time"

	"github.com/EkinBarisC/claude-session-manager/internal/config"
)

type Lock struct {
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
	Trigger   string `json:"trigger"`
	ItemID    string `json:"item_id"`
}

// Held is an acquired lock; release it when the run ends.
type Held struct {
	lock Lock
}

// Acquire takes the run lock, failing if another live run holds it.
func Acquire(trigger string) (*Held, error) {
	if current := Current(); current != nil {
		return nil, fmt.Errorf("another csm run is in progress (pid %d, started %s, trigger %s); "+
			"wait for it or remove %s if it is not real",
			current.PID, current.StartedAt, current.Trigger, config.LockPath())
	}
	h := &Held{lock: Lock{
		PID:       os.Getpid(),
		StartedAt: now(),
		Trigger:   trigger,
	}}
	if err := config.WriteJSON(config.LockPath(), h.lock); err != nil {
		return nil, err
	}
	return h, nil
}

// SetItem records which queue item the run is currently on.
func (h *Held) SetItem(id string) {
	h.lock.ItemID = id
	config.WriteJSON(config.LockPath(), h.lock)
}

func (h *Held) Release() {
	os.Remove(config.LockPath())
}

// Current returns the live lock, or nil when no run is in progress.
// A lock left behind by a dead process does not count.
func Current() *Lock {
	var lock Lock
	config.ReadJSON(config.LockPath(), &lock)
	if lock.PID == 0 {
		return nil
	}
	if !pidAlive(lock.PID) {
		return nil
	}
	return &lock
}

type LastRun struct {
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Trigger    string `json:"trigger"`
	Processed  int    `json:"items_processed"`
	Outcome    string `json:"outcome"`
}

func WriteLastRun(startedAt, trigger string, processed int, outcome string) {
	config.WriteJSON(config.LastRunPath(), LastRun{
		StartedAt:  startedAt,
		FinishedAt: now(),
		Trigger:    trigger,
		Processed:  processed,
		Outcome:    outcome,
	})
}

// ReadLastRun returns the most recent run summary, or nil if none recorded.
func ReadLastRun() *LastRun {
	var lr LastRun
	config.ReadJSON(config.LastRunPath(), &lr)
	if lr.FinishedAt == "" {
		return nil
	}
	return &lr
}

// Age returns how long ago the run finished, or 0 if unparseable.
func (lr *LastRun) Age() time.Duration {
	t, err := time.Parse(time.RFC3339, lr.FinishedAt)
	if err != nil {
		return 0
	}
	return time.Since(t)
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
