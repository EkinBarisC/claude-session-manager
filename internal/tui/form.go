package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
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
	"Prompt (enter = newline, tab = next field)", "Project dir (tab completes)",
	"Model (empty = config)", "Effort (empty = config)", "Mode (empty = config)",
	"Priority",
}

// form is the add/edit task dialog. The prompt is a word-wrapping textarea
// (long prompts stay fully visible); the remaining fields are single-line
// inputs, so inputs[fieldPrompt] is unused.
type form struct {
	prompt textarea.Model
	inputs [fieldCount]textinput.Model
	focus  int
	errMsg string
	hint   string
	editID string // "" = adding a new item
}

func newForm() *form {
	f := &form{}
	ta := textarea.New()
	ta.Placeholder = "what should claude do?"
	ta.CharLimit = 4000
	ta.SetWidth(72)
	ta.SetHeight(5)
	ta.ShowLineNumbers = false
	ta.Focus()
	f.prompt = ta
	for i := fieldProject; i < fieldCount; i++ {
		in := textinput.New()
		in.CharLimit = 500
		in.Width = 60
		f.inputs[i] = in
	}
	f.inputs[fieldProject].SetValue(".")
	f.inputs[fieldEffort].Placeholder = strings.Join(config.EffortLevels, "|")
	f.inputs[fieldMode].Placeholder = strings.Join(config.RunModes, "|")
	f.inputs[fieldPriority].SetValue("0")
	return f
}

func newEditForm(item *queue.Item) *form {
	f := newForm()
	f.editID = item.ID
	f.prompt.SetValue(item.Prompt)
	f.inputs[fieldProject].SetValue(item.Project)
	f.inputs[fieldModel].SetValue(item.Model)
	f.inputs[fieldEffort].SetValue(item.Effort)
	f.inputs[fieldMode].SetValue(item.Mode)
	f.inputs[fieldPriority].SetValue(strconv.Itoa(item.Priority))
	return f
}

func (f *form) focusCmd() tea.Cmd {
	return textinput.Blink
}

// update handles a key while the form is open. It returns (done, added,
// cmd): done means the form should close, added is the new item id.
func (f *form) update(msg tea.KeyMsg) (bool, string, tea.Cmd) {
	key := msg.String()
	promptOwnsKey := f.focus == fieldPrompt &&
		(key == "enter" || key == "up" || key == "down")

	switch key {
	case "esc":
		return true, "", nil
	case "tab", "shift+tab", "enter", "up", "down":
		if promptOwnsKey {
			break // newline / cursor movement inside the textarea
		}
		if key == "tab" && f.focus == fieldProject {
			f.completeProject()
			return false, "", nil
		}
		f.hint = ""
		if key == "enter" && f.focus == fieldCount-1 {
			id, err := f.submit()
			if err != nil {
				f.errMsg = err.Error()
				return false, "", nil
			}
			return true, id, nil
		}
		delta := 1
		if key == "shift+tab" || key == "up" {
			delta = -1
		}
		return false, "", f.setFocus(f.focus + delta)
	}

	var cmd tea.Cmd
	if f.focus == fieldPrompt {
		f.prompt, cmd = f.prompt.Update(msg)
	} else {
		f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
	}
	return false, "", cmd
}

func (f *form) setFocus(idx int) tea.Cmd {
	f.focus = (idx + fieldCount) % fieldCount
	var cmds []tea.Cmd
	if f.focus == fieldPrompt {
		cmds = append(cmds, f.prompt.Focus())
	} else {
		f.prompt.Blur()
	}
	for i := fieldProject; i < fieldCount; i++ {
		if i == f.focus {
			cmds = append(cmds, f.inputs[i].Focus())
		} else {
			f.inputs[i].Blur()
		}
	}
	return tea.Batch(cmds...)
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
	prompt := strings.TrimSpace(f.prompt.Value())
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
	model := strings.TrimSpace(f.inputs[fieldModel].Value())

	if f.editID != "" {
		items := queue.Load()
		item, err := queue.Find(items, f.editID)
		if err != nil {
			return "", err
		}
		abs, err := filepath.Abs(project)
		if err != nil {
			return "", err
		}
		item.Prompt = prompt
		item.Project = abs
		item.Model = model
		item.Effort = effort
		item.Mode = mode
		item.Priority = priority
		return item.ID, queue.Save(items)
	}

	item, err := queue.Add(prompt, project, model, effort, mode, priority, false)
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
	title := "New task"
	if f.editID != "" {
		title = "Edit [" + f.editID + "]"
	}
	f.prompt.SetWidth(min(90, max(30, width-4)))
	var b strings.Builder
	b.WriteString(formTitleS.Render(title) + "\n\n")
	b.WriteString(" " + formLabelS.Render(formLabels[fieldPrompt]) + "\n" + f.prompt.View() + "\n")
	for i := fieldProject; i < fieldCount; i++ {
		b.WriteString(" " + formLabelS.Render(formLabels[i]) + "\n " + f.inputs[i].View() + "\n")
	}
	if f.hint != "" {
		b.WriteString("\n " + formLabelS.Render(f.hint))
	}
	if f.errMsg != "" {
		b.WriteString("\n" + formErrS.Render(f.errMsg))
	}
	return b.String()
}
