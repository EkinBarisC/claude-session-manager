// Package sessions maps a project directory to its most recent Claude Code
// session and an estimate of that session's context size, so the runner can
// decide between resuming (`claude -r`) and rotating to a fresh session.
package sessions

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/EkinBarisC/claude-session-manager/internal/config"
)

type Entry struct {
	SessionID     string `json:"session_id"`
	ContextTokens int    `json:"context_tokens"`
	UpdatedAt     string `json:"updated_at"`
}

type Registry map[string]Entry

func Load() Registry {
	reg := Registry{}
	config.ReadJSON(config.SessionsPath(), &reg)
	return reg
}

func (r Registry) Save() error {
	return config.WriteJSON(config.SessionsPath(), r)
}

func key(project string) string {
	abs, err := filepath.Abs(project)
	if err != nil {
		abs = project
	}
	return strings.ToLower(abs)
}

// Resumable returns the session id to resume, or "" if a fresh session
// should be started.
func (r Registry) Resumable(cfg config.Config, project string) string {
	entry, ok := r[key(project)]
	if !ok || entry.SessionID == "" {
		return ""
	}
	threshold := cfg.ContextWindowTokens * cfg.ContextRotatePct / 100
	if entry.ContextTokens >= threshold {
		return "" // rotate: context.md carries the state forward
	}
	return entry.SessionID
}

func (r Registry) Record(project, sessionID string, contextTokens int) error {
	r[key(project)] = Entry{
		SessionID:     sessionID,
		ContextTokens: contextTokens,
		UpdatedAt:     time.Now().UTC().Format("2006-01-02T15:04:05+00:00"),
	}
	return r.Save()
}
