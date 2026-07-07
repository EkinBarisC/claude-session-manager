package runstate

import (
	"strings"
	"testing"

	"github.com/EkinBarisC/claude-session-manager/internal/config"
)

func TestAcquireReleaseCycle(t *testing.T) {
	t.Setenv("CSM_HOME", t.TempDir())

	if Current() != nil {
		t.Fatal("no lock expected in a fresh state dir")
	}
	held, err := Acquire("manual")
	if err != nil {
		t.Fatal(err)
	}
	lock := Current()
	if lock == nil || lock.Trigger != "manual" {
		t.Fatalf("live lock expected, got %+v", lock)
	}

	held.SetItem("abcd1234")
	if got := Current(); got.ItemID != "abcd1234" {
		t.Errorf("item id not recorded, got %+v", got)
	}

	if _, err := Acquire("tui"); err == nil {
		t.Fatal("second acquire must fail while lock is live")
	} else if !strings.Contains(err.Error(), "in progress") {
		t.Errorf("unhelpful error: %v", err)
	}

	held.Release()
	if Current() != nil {
		t.Fatal("lock should be gone after release")
	}
	if _, err := Acquire("manual"); err != nil {
		t.Fatalf("acquire after release should work: %v", err)
	}
}

func TestStaleLockIsReplaced(t *testing.T) {
	t.Setenv("CSM_HOME", t.TempDir())

	// pids on every platform are far below this; the process cannot exist
	config.WriteJSON(config.LockPath(), Lock{PID: 1 << 30, StartedAt: "2026-01-01T00:00:00Z", Trigger: "scheduled"})
	if Current() != nil {
		t.Fatal("dead pid must not count as a live lock")
	}
	if _, err := Acquire("manual"); err != nil {
		t.Fatalf("stale lock should be silently replaced: %v", err)
	}
}

func TestLastRunRoundTrip(t *testing.T) {
	t.Setenv("CSM_HOME", t.TempDir())

	if ReadLastRun() != nil {
		t.Fatal("no lastrun expected in a fresh state dir")
	}
	WriteLastRun("2026-07-05T00:30:00Z", "scheduled until 07:30", 4, "weekly budget reached")
	lr := ReadLastRun()
	if lr == nil {
		t.Fatal("lastrun not written")
	}
	if lr.Processed != 4 || lr.Outcome != "weekly budget reached" || lr.FinishedAt == "" {
		t.Errorf("round trip mangled: %+v", lr)
	}
	if lr.Age() <= 0 {
		t.Errorf("age should be positive, got %v", lr.Age())
	}
}
