package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/EkinBarisC/claude-session-manager/internal/config"
)

var (
	cfgCursorS = lipgloss.NewStyle().Bold(true).Foreground(accent)
	cfgKeyS    = lipgloss.NewStyle().Width(24)
)

// configValues returns the effective config as a key -> JSON value map.
func (m *model) configValues() map[string]any {
	raw, _ := json.Marshal(m.cfg)
	values := map[string]any{}
	json.Unmarshal(raw, &values)
	return values
}

func (m *model) configView() string {
	values := m.configValues()
	var b strings.Builder
	b.WriteString(" " + labelS.Render(config.ConfigPath()) + "\n\n")
	for i, key := range config.KnownKeys() {
		raw, _ := json.Marshal(values[key])
		val := string(raw)
		if limit := max(20, m.width-32); len(val) > limit {
			val = val[:limit] + "..."
		}
		cursor := "  "
		keyText := cfgKeyS.Render(key)
		if i == m.cfgCursor && m.mode == modeTable {
			cursor = cfgCursorS.Render("> ")
			keyText = cfgCursorS.Render(cfgKeyS.Render(key))
		}
		b.WriteString(cursor + keyText + " " + highlightJSON(val) + "\n")
	}
	if m.cfgEditing {
		key := config.KnownKeys()[m.cfgCursor]
		b.WriteString("\n " + labelS.Render("new value for "+key+" (JSON or plain string)") +
			"\n " + m.cfgInput.View() + "\n")
	}
	if m.cfgErr != "" {
		b.WriteString("\n " + flashS.Render(m.cfgErr))
	}
	return b.String()
}

// handleConfigEditKey owns every key while a config value is being edited,
// so typed characters (including q/1/2/3) reach the input.
func (m *model) handleConfigEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.cfgEditing = false
		m.cfgErr = ""
		return m, nil
	case "enter":
		key := config.KnownKeys()[m.cfgCursor]
		value := config.ParseValue(strings.TrimSpace(m.cfgInput.Value()))
		if err := config.Validate(key, value); err != nil {
			m.cfgErr = err.Error()
			return m, nil
		}
		if err := config.SetValue(key, value); err != nil {
			m.cfgErr = err.Error()
			return m, nil
		}
		m.cfgEditing = false
		m.cfgErr = ""
		m.reload()
		m.flash = fmt.Sprintf("%s saved", key)
		return m, nil
	}
	var cmd tea.Cmd
	m.cfgInput, cmd = m.cfgInput.Update(msg)
	return m, cmd
}

func (m *model) handleConfigKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	keys := config.KnownKeys()
	switch msg.String() {
	case "up", "k":
		if m.cfgCursor > 0 {
			m.cfgCursor--
		}
	case "down", "j":
		if m.cfgCursor < len(keys)-1 {
			m.cfgCursor++
		}
	case "enter", "e":
		key := keys[m.cfgCursor]
		in := textinput.New()
		in.CharLimit = 4000
		in.Width = max(40, m.width-6)
		in.SetValue(editableValue(m.configValues()[key]))
		in.Focus()
		in.CursorEnd()
		m.cfgInput = in
		m.cfgEditing = true
		m.cfgErr = ""
		return m, textinput.Blink
	case "d":
		key := keys[m.cfgCursor]
		if err := config.UnsetValue(key); err != nil {
			m.cfgErr = err.Error()
			return m, nil
		}
		m.reload()
		m.flash = fmt.Sprintf("%s reset to default", key)
	}
	return m, nil
}

// editableValue renders a config value the way a user would type it back:
// bare strings stay unquoted, everything else is compact JSON.
func editableValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}
