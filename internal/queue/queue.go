// Package queue persists queue items in ~/.csm/queue.json.
package queue

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EkinBarisC/claude-session-manager/internal/config"
)

const (
	Pending        = "pending"
	Done           = "done"
	NeedsAttention = "needs_attention" // failed, timed out, or ended asking a question
)

type Item struct {
	ID              string         `json:"id"`
	Prompt          string         `json:"prompt"`
	Project         string         `json:"project"`
	Model           string         `json:"model"`
	Effort          string         `json:"effort"`
	Mode            string         `json:"mode"`
	Priority        int            `json:"priority"`
	ForceNewSession bool           `json:"force_new_session"`
	Status          string         `json:"status"`
	CreatedAt       string         `json:"created_at"`
	SessionID       string         `json:"session_id"`
	Summary         string         `json:"summary"`
	Error           string         `json:"error"`
	Tokens          map[string]int `json:"tokens"`
	FinishedAt      string         `json:"finished_at"`
}

func now() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05+00:00")
}

func Load() []*Item {
	var items []*Item
	config.ReadJSON(config.QueuePath(), &items)
	return items
}

func Save(items []*Item) error {
	if items == nil {
		items = []*Item{}
	}
	return config.WriteJSON(config.QueuePath(), items)
}

func Add(prompt, project, model, effort, mode string, priority int, forceNewSession bool) (*Item, error) {
	abs, err := filepath.Abs(project)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 4)
	rand.Read(buf)
	item := &Item{
		ID:              hex.EncodeToString(buf),
		Prompt:          prompt,
		Project:         abs,
		Model:           model,
		Effort:          effort,
		Mode:            mode,
		Priority:        priority,
		ForceNewSession: forceNewSession,
		Status:          Pending,
		CreatedAt:       now(),
	}
	items := append(Load(), item)
	return item, Save(items)
}

// Find looks up an item by id or unique id prefix.
func Find(items []*Item, token string) (*Item, error) {
	var matches []*Item
	for _, it := range items {
		if it.ID == token {
			return it, nil
		}
		if strings.HasPrefix(it.ID, token) {
			matches = append(matches, it)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, fmt.Errorf("no item matching '%s'", token)
	default:
		ids := make([]string, len(matches))
		for i, it := range matches {
			ids[i] = it.ID
		}
		return nil, fmt.Errorf("'%s' is ambiguous: %s", token, strings.Join(ids, ", "))
	}
}

// PendingItems returns pending items, highest priority first, oldest first
// within a priority.
func PendingItems(items []*Item) []*Item {
	var todo []*Item
	for _, it := range items {
		if it.Status == Pending {
			todo = append(todo, it)
		}
	}
	sort.SliceStable(todo, func(i, j int) bool {
		if todo[i].Priority != todo[j].Priority {
			return todo[i].Priority > todo[j].Priority
		}
		return todo[i].CreatedAt < todo[j].CreatedAt
	})
	return todo
}

// Finish records an item's outcome and persists the queue.
func Finish(items []*Item, item *Item, status string, sessionID, summary, errMsg string, tokens map[string]int) error {
	item.Status = status
	if sessionID != "" {
		item.SessionID = sessionID
	}
	item.Summary = summary
	item.Error = errMsg
	item.Tokens = tokens
	item.FinishedAt = now()
	return Save(items)
}
