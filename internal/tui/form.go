package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/EkinBarisC/claude-session-manager/internal/config"
	"github.com/EkinBarisC/claude-session-manager/internal/queue"
)

const (
	fieldPrompt = iota
	fieldProject
	fieldModel
	fieldEffort
	fieldMode
	fieldPriority
	fieldCount
)

var formLabels = [fieldCount]string{
	"Prompt", "Project dir (tab completes)", "Model (empty = config)",
	"Effort (empty = config)", "Mode (empty = config)", "Priority",
}

type form struct {
	inputs [fieldCount]textinput.Model
	focus  int
	errMsg string
	hint   string
}

func newForm() *form {
	f := &form{}
	for i := range f.inputs {
		in := textinput.New()
		in.CharLimit = 500
		in.Width = 60
		f.inputs[i] = in
	}
	f.inputs[fieldPrompt].Placeholder = "what should claude do?"
	f.inputs[fieldProject].SetValue(".")
	f.inputs[fieldEffort].Placeholder = strings.Join(config.EffortLevels, "|")
	f.inputs[fieldMode].Placeholder = strings.Join(config.RunModes, "|")
	f.inputs[fieldPriority].SetValue("0")
	f.inputs[fieldPrompt].Focus()
	return f
}

func (f *form) focusCmd() tea.Cmd {
	return textinput.Blink
}

// update handles a key while the form is open. It returns (done, added,
// cmd): done means the form should close, added is the new item id.
func (f *form) update(msg tea.KeyMsg) (bool, string, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return true, "", nil
	case "tab", "shift+tab", "enter", "up", "down":
		if msg.String() == "tab" && f.focus == fieldProject {
			f.completeProject()
			return false, "", nil
		}
		f.hint = ""
		if msg.String() == "enter" && f.focus == fieldCount-1 {
			id, err := f.submit()
			if err != nil {
				f.errMsg = err.Error()
				return false, "", nil
			}
			return true, id, nil
		}
		if msg.String() == "shift+tab" || msg.String() == "up" {
			f.focus--
		} else {
			f.focus++
		}
		f.focus = (f.focus + fieldCount) % fieldCount
		cmds := make([]tea.Cmd, 0, fieldCount)
		for i := range f.inputs {
			if i == f.focus {
				cmds = append(cmds, f.inputs[i].Focus())
			} else {
				f.inputs[i].Blur()
			}
		}
		return false, "", tea.Batch(cmds...)
	}
	var cmd tea.Cmd
	f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
	return false, "", cmd
}

// completeProject tab-completes the project dir field against directories
// on disk, filling in the longest unambiguous prefix and listing candidates
// in the hint line when there is more than one.
func (f *form) completeProject() {
	in := &f.inputs[fieldProject]
	val := in.Value()
	path := val
	if path == "" {
		path = "."
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			path = home + path[1:]
		}
	}
	dir, prefix := filepath.Split(path)
	readDir := dir
	if readDir == "" {
		readDir = "."
	}
	entries, err := os.ReadDir(readDir)
	if err != nil {
		f.hint = "cannot read " + readDir
		return
	}
	var matches []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(strings.ToLower(e.Name()), strings.ToLower(prefix)) {
			matches = append(matches, e.Name())
		}
	}
	switch len(matches) {
	case 0:
		f.hint = "no matching directory"
	case 1:
		in.SetValue(dir + matches[0] + string(filepath.Separator))
		in.CursorEnd()
		f.hint = ""
	default:
		in.SetValue(dir + commonPrefix(matches))
		in.CursorEnd()
		f.hint = strings.Join(matches, "  ")
		if len(f.hint) > 200 {
			f.hint = f.hint[:200] + "..."
		}
	}
}

func commonPrefix(names []string) string {
	prefix := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(strings.ToLower(n), strings.ToLower(prefix)) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

func (f *form) submit() (string, error) {
	prompt := strings.TrimSpace(f.inputs[fieldPrompt].Value())
	if prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}
	project := strings.TrimSpace(f.inputs[fieldProject].Value())
	if project == "" {
		project = "."
	}
	effort := strings.TrimSpace(f.inputs[fieldEffort].Value())
	if effort != "" && !slices.Contains(config.EffortLevels, effort) {
		return "", fmt.Errorf("effort must be one of %s", strings.Join(config.EffortLevels, ", "))
	}
	mode := strings.TrimSpace(f.inputs[fieldMode].Value())
	if mode != "" && !slices.Contains(config.RunModes, mode) {
		return "", fmt.Errorf("mode must be one of %s", strings.Join(config.RunModes, ", "))
	}
	priority := 0
	if v := strings.TrimSpace(f.inputs[fieldPriority].Value()); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return "", fmt.Errorf("priority must be an integer")
		}
		priority = n
	}
	item, err := queue.Add(prompt, project,
		strings.TrimSpace(f.inputs[fieldModel].Value()), effort, mode, priority, false)
	if err != nil {
		return "", err
	}
	return item.ID, nil
}

var (
	formTitleS = lipgloss.NewStyle().Bold(true).Foreground(accent).Padding(0, 1)
	formLabelS = lipgloss.NewStyle().Foreground(subtle)
	formErrS   = lipgloss.NewStyle().Foreground(errColor).Padding(0, 1)
)

func (f *form) view(width int) string {
	var b strings.Builder
	b.WriteString(formTitleS.Render("New task") + "\n\n")
	for i, in := range f.inputs {
		b.WriteString(" " + formLabelS.Render(formLabels[i]) + "\n " + in.View() + "\n")
	}
	if f.hint != "" {
		b.WriteString("\n " + formLabelS.Render(f.hint))
	}
	if f.errMsg != "" {
		b.WriteString("\n" + formErrS.Render(f.errMsg))
	}
	return b.String()
}
