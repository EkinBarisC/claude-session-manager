package queue

import (
	"testing"
)

func item(id string, priority int, created string) *Item {
	return &Item{ID: id, Status: Pending, Priority: priority, CreatedAt: created}
}

func TestFind(t *testing.T) {
	items := []*Item{item("abcd1234", 0, "t1"), item("abff5678", 0, "t2")}

	if it, err := Find(items, "abcd1234"); err != nil || it.ID != "abcd1234" {
		t.Fatalf("exact match failed: %v", err)
	}
	if it, err := Find(items, "abc"); err != nil || it.ID != "abcd1234" {
		t.Fatalf("unique prefix failed: %v", err)
	}
	if _, err := Find(items, "ab"); err == nil {
		t.Fatal("ambiguous prefix should error")
	}
	if _, err := Find(items, "zz"); err == nil {
		t.Fatal("missing id should error")
	}
}

func TestPendingItemsOrder(t *testing.T) {
	items := []*Item{
		item("low-old", 0, "2026-01-01T00:00:00+00:00"),
		{ID: "done", Status: Done, CreatedAt: "2026-01-01T00:00:00+00:00"},
		item("high", 5, "2026-01-03T00:00:00+00:00"),
		item("low-new", 0, "2026-01-02T00:00:00+00:00"),
	}
	got := PendingItems(items)
	want := []string{"high", "low-old", "low-new"}
	if len(got) != len(want) {
		t.Fatalf("want %d items, got %d", len(want), len(got))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("position %d: want %s, got %s", i, id, got[i].ID)
		}
	}
}

func TestAddAndLoadRoundTrip(t *testing.T) {
	t.Setenv("CSM_HOME", t.TempDir())

	added, err := Add("do the thing", ".", "sonnet", "high", "plan", 3, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(added.ID) != 8 {
		t.Errorf("id should be 8 hex chars, got %q", added.ID)
	}

	items := Load()
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	it := items[0]
	if it.Prompt != "do the thing" || it.Effort != "high" || it.Mode != "plan" ||
		it.Priority != 3 || !it.ForceNewSession || it.Status != Pending {
		t.Errorf("round trip mangled item: %+v", it)
	}
}
