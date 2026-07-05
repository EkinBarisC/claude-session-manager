// Package tui is the interactive terminal UI: a queue browser with add /
// requeue / delete / run actions, plus report and config views.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/EkinBarisC/claude-session-manager/internal/claude"
	"github.com/EkinBarisC/claude-session-manager/internal/config"
	"github.com/EkinBarisC/claude-session-manager/internal/ledger"
	"github.com/EkinBarisC/claude-session-manager/internal/queue"
	"github.com/EkinBarisC/claude-session-manager/internal/report"
	"github.com/EkinBarisC/claude-session-manager/internal/runner"
	"github.com/EkinBarisC/claude-session-manager/internal/runstate"
	"github.com/EkinBarisC/claude-session-manager/internal/sessions"
)

type tab int

const (
	tabQueue tab = iota
	tabReport
	tabConfig
)

type mode int

const (
	modeTable mode = iota
	modeDetail
	modeForm
	modeConfirmDelete
)

var (
	accent     = lipgloss.AdaptiveColor{Light: "#7D56F4", Dark: "#A78BFA"}
	subtle     = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	okColor    = lipgloss.AdaptiveColor{Light: "#059669", Dark: "#34D399"}
	warnColor  = lipgloss.AdaptiveColor{Light: "#D97706", Dark: "#FBBF24"}
	errColor   = lipgloss.AdaptiveColor{Light: "#DC2626", Dark: "#F87171"}
	slashColor = lipgloss.AdaptiveColor{Light: "#0891B2", Dark: "#67E8F9"} // slash commands/skills
	tabStyle   = lipgloss.NewStyle().Padding(0, 2).Foreground(subtle)
	activeTabS = lipgloss.NewStyle().Padding(0, 2).Bold(true).Foreground(accent).Underline(true)
	statusBarS = lipgloss.NewStyle().Foreground(subtle)
	flashS     = lipgloss.NewStyle().Foreground(warnColor)
	helpS      = lipgloss.NewStyle().Foreground(subtle)
	detailKeyS = lipgloss.NewStyle().Bold(true).Width(10)
)

type model struct {
	cfg      config.Config
	items    []*queue.Item
	tab      tab
	mode     mode
	table    table.Model
	report   viewport.Model
	form     *form
	spinner  spinner.Model
	width    int
	height   int
	running  string // item id currently running, "" if idle
	extRun   *runstate.Lock
	limits   []claude.Limit // real plan usage, fetched async
	flash    string
	detailID string
	ready    bool

	// config tab state
	cfgCursor  int
	cfgEditing bool
	cfgInput   textinput.Model
	cfgErr     string
}

type runDoneMsg struct {
	id     string
	result claude.Result
}

type claudeDoneMsg struct {
	id  string
	err error
}

// Run starts the TUI and blocks until the user quits.
func Run() error {
	if _, err := config.EnsureInit(); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	m := model{cfg: cfg, spinner: sp}
	m.reload()
	_, err = tea.NewProgram(&m, tea.WithAltScreen()).Run()
	return err
}

type usageMsg struct {
	limits []claude.Limit
}

// fetchUsageCmd queries real plan usage in the background (free: /usage
// spends no tokens) so the status bar can show actual limit pressure.
func fetchUsageCmd(cfg config.Config) tea.Cmd {
	return func() tea.Msg {
		limits, _, err := claude.FetchUsage(cfg)
		if err != nil {
			return usageMsg{}
		}
		return usageMsg{limits: limits}
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, fetchUsageCmd(m.cfg))
}

func (m *model) reload() {
	m.items = queue.Load()
	if cfg, err := config.Load(); err == nil {
		m.cfg = cfg
	}
	// a lock held by another process (scheduled or manual run)
	m.extRun = runstate.Current()
	if m.extRun != nil && m.extRun.PID == os.Getpid() {
		m.extRun = nil
	}
	m.rebuildTable()
	m.setReportContent()
}

func (m *model) rebuildTable() {
	columns := []table.Column{
		{Title: "ID", Width: 8},
		{Title: "Status", Width: 15},
		{Title: "Pri", Width: 4},
		{Title: "Model/Effort/Mode", Width: 24},
		{Title: "Prompt", Width: max(20, m.width-60)},
	}
	rows := make([]table.Row, 0, len(m.items))
	for _, it := range sortedForDisplay(m.items) {
		mdl, eff, md := claude.ItemSettings(m.cfg, it)
		if eff == "" {
			eff = "default"
		}
		rows = append(rows, table.Row{
			it.ID, it.Status, fmt.Sprintf("%d", it.Priority),
			fmt.Sprintf("%s/%s/%s", mdl, eff, md),
			report.Short(it.Prompt, 200),
		})
	}
	cursor := 0
	if m.ready {
		cursor = m.table.Cursor()
	}
	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(max(3, m.height-6)),
	)
	styles := table.DefaultStyles()
	styles.Header = styles.Header.Bold(true).Foreground(accent).BorderStyle(lipgloss.NormalBorder()).BorderBottom(true)
	styles.Selected = styles.Selected.Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Bold(true)
	t.SetStyles(styles)
	if cursor < len(rows) {
		t.SetCursor(cursor)
	}
	m.table = t
	m.ready = true
}

func sortedForDisplay(items []*queue.Item) []*queue.Item {
	out := make([]*queue.Item, len(items))
	copy(out, items)
	// pending first (priority desc), then needs_attention, then done
	rank := func(s string) int {
		switch s {
		case queue.Pending:
			return 0
		case queue.NeedsAttention:
			return 1
		default:
			return 2
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			a, b := out[j-1], out[j]
			swap := false
			if rank(a.Status) != rank(b.Status) {
				swap = rank(a.Status) > rank(b.Status)
			} else if a.Priority != b.Priority {
				swap = a.Priority < b.Priority
			} else {
				swap = a.CreatedAt > b.CreatedAt
			}
			if swap {
				out[j-1], out[j] = out[j], out[j-1]
			} else {
				break
			}
		}
	}
	return out
}

func (m *model) selected() *queue.Item {
	row := m.table.SelectedRow()
	if row == nil {
		return nil
	}
	item, err := queue.Find(m.items, row[0])
	if err != nil {
		return nil
	}
	return item
}

func (m *model) setReportContent() {
	data, err := os.ReadFile(config.ReportPath())
	content := "no report yet"
	if err == nil {
		content = highlightReport(string(data))
	}
	vp := viewport.New(max(20, m.width-2), max(3, m.height-5))
	vp.SetContent(content)
	vp.GotoBottom()
	m.report = vp
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.rebuildTable()
		m.setReportContent()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case usageMsg:
		m.limits = msg.limits
		return m, nil

	case claudeDoneMsg:
		m.reload()
		if msg.err != nil {
			m.flash = fmt.Sprintf("claude session exited: %v", msg.err)
		} else {
			m.flash = fmt.Sprintf("[%s] session closed - press r to requeue if resolved", msg.id)
		}
		return m, fetchUsageCmd(m.cfg)

	case runDoneMsg:
		m.running = ""
		m.reload()
		switch {
		case msg.result.AuthError, msg.result.RateLimited:
			m.flash = fmt.Sprintf("[%s] stopped: %s", msg.id, msg.result.Error)
		case msg.result.OK:
			m.flash = fmt.Sprintf("[%s] done: %s", msg.id, msg.result.Summary)
		default:
			m.flash = fmt.Sprintf("[%s] needs attention: %s", msg.id, msg.result.Error)
		}
		return m, fetchUsageCmd(m.cfg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeForm {
		return m.updateForm(msg)
	}

	key := msg.String()
	if m.mode == modeConfirmDelete {
		if key == "y" {
			m.deleteSelected()
		} else {
			m.flash = "delete cancelled"
		}
		m.mode = modeTable
		return m, nil
	}
	if m.mode == modeDetail {
		if key == "esc" || key == "q" || key == "enter" {
			m.mode = modeTable
		}
		return m, nil
	}
	// a config value being edited owns every key (q, 1-3, etc. are text)
	if m.tab == tabConfig && m.cfgEditing {
		return m.handleConfigEditKey(msg)
	}

	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "ctrl+z":
		// unix job control: fg brings the TUI back. No-op on Windows.
		return m, tea.Suspend
	case "tab":
		m.tab = (m.tab + 1) % 3
		return m, nil
	case "1":
		m.tab = tabQueue
		return m, nil
	case "2":
		m.tab = tabReport
		return m, nil
	case "3":
		m.tab = tabConfig
		return m, nil
	case "u":
		m.reload()
		m.flash = "refreshed"
		return m, fetchUsageCmd(m.cfg)
	}

	switch m.tab {
	case tabReport:
		var cmd tea.Cmd
		m.report, cmd = m.report.Update(msg)
		return m, cmd
	case tabConfig:
		return m.handleConfigKey(msg)
	}

	// queue tab
	switch key {
	case "n":
		m.form = newForm()
		m.mode = modeForm
		return m, m.form.focusCmd()
	case "enter":
		if it := m.selected(); it != nil {
			m.detailID = it.ID
			m.mode = modeDetail
		}
		return m, nil
	case "e":
		if it := m.selected(); it != nil {
			m.form = newEditForm(it)
			m.mode = modeForm
			return m, m.form.focusCmd()
		}
		return m, nil
	case "d":
		if m.selected() != nil {
			m.mode = modeConfirmDelete
		}
		return m, nil
	case "r":
		if it := m.selected(); it != nil {
			it.Status = queue.Pending
			it.Error = ""
			queue.Save(m.items)
			m.reload()
			m.flash = fmt.Sprintf("[%s] back to pending", it.ID)
		}
		return m, nil
	case "R":
		return m.startRun()
	case "c":
		return m.openClaudeSession()
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	editing := m.form.editID != ""
	done, added, cmd := m.form.update(msg)
	if done {
		m.mode = modeTable
		if added != "" {
			m.reload()
			if editing {
				m.flash = fmt.Sprintf("updated [%s]", added)
			} else {
				m.flash = fmt.Sprintf("queued [%s]", added)
			}
		}
	}
	return m, cmd
}

func (m *model) deleteSelected() {
	it := m.selected()
	if it == nil {
		return
	}
	var remaining []*queue.Item
	for _, other := range m.items {
		if other != it {
			remaining = append(remaining, other)
		}
	}
	queue.Save(remaining)
	m.reload()
	m.flash = fmt.Sprintf("removed [%s]", it.ID)
}

// openClaudeSession suspends the TUI and resumes the selected item's Claude
// Code session interactively (`claude -r`) in the item's project directory,
// so a needs_attention question can be answered in place. The billing env
// vars are stripped just like headless runs.
func (m *model) openClaudeSession() (tea.Model, tea.Cmd) {
	if m.running != "" {
		m.flash = "a run is in progress - wait for it to finish first"
		return m, nil
	}
	it := m.selected()
	if it == nil {
		return m, nil
	}
	if it.SessionID == "" {
		m.flash = fmt.Sprintf("[%s] has no recorded session to resume", it.ID)
		return m, nil
	}
	binary, err := exec.LookPath(m.cfg.ClaudeBinary)
	if err != nil {
		m.flash = fmt.Sprintf("'%s' not found on PATH", m.cfg.ClaudeBinary)
		return m, nil
	}
	cmd := exec.Command(binary, "-r", it.SessionID)
	cmd.Dir = it.Project
	cmd.Env, _ = claude.StrippedEnv(os.Environ())
	id := it.ID
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return claudeDoneMsg{id: id, err: err}
	})
}

func (m *model) startRun() (tea.Model, tea.Cmd) {
	if m.running != "" {
		m.flash = "a run is already in progress"
		return m, nil
	}
	it := m.selected()
	if it == nil {
		return m, nil
	}
	if it.Status != queue.Pending {
		m.flash = fmt.Sprintf("[%s] is %s - press r to requeue first", it.ID, it.Status)
		return m, nil
	}
	if spend := ledger.WeeklySpend(); spend >= m.cfg.WeeklyTokenBudget {
		m.flash = fmt.Sprintf("weekly budget reached (%d / %d) - not running", spend, m.cfg.WeeklyTokenBudget)
		return m, nil
	}
	held, err := runstate.Acquire("tui")
	if err != nil {
		m.reload() // pick up the external lock for the status bar
		m.flash = "another csm run is already in progress"
		return m, nil
	}
	held.SetItem(it.ID)
	m.running = it.ID
	m.flash = ""
	cfg := m.cfg
	id := it.ID
	return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
		defer held.Release()
		items := queue.Load()
		item, err := queue.Find(items, id)
		if err != nil {
			return runDoneMsg{id: id, result: claude.Result{Error: err.Error()}}
		}
		registry := sessions.Load()
		env, _ := claude.StrippedEnv(os.Environ())
		resumeID := ""
		if !item.ForceNewSession {
			resumeID = registry.Resumable(cfg, item.Project)
		}
		report.AppendRunHeader("tui")
		result := claude.RunItem(cfg, item, resumeID, env)
		if result.AuthError || result.RateLimited {
			report.AppendNote("run aborted: " + result.Error)
		} else {
			runner.RecordOutcome(cfg, items, item, result, registry)
		}
		return runDoneMsg{id: id, result: result}
	})
}

func (m *model) View() string {
	if !m.ready {
		return "loading..."
	}
	var body string
	switch m.mode {
	case modeForm:
		body = m.form.view(m.width)
	case modeDetail:
		body = m.detailView()
	default:
		switch m.tab {
		case tabReport:
			body = m.report.View()
		case tabConfig:
			body = m.configView()
		default:
			body = m.table.View()
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.tabsView(), body, m.statusView(), m.helpView())
}

func (m *model) tabsView() string {
	labels := []string{"1 Queue", "2 Report", "3 Config"}
	parts := make([]string, len(labels))
	for i, l := range labels {
		if tab(i) == m.tab && m.mode != modeForm {
			parts[i] = activeTabS.Render(l)
		} else {
			parts[i] = tabStyle.Render(l)
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func (m *model) statusView() string {
	spend := ledger.WeeklySpend()
	pct := 0
	if m.cfg.WeeklyTokenBudget > 0 {
		pct = 100 * spend / m.cfg.WeeklyTokenBudget
	}
	budgetS := lipgloss.NewStyle().Foreground(okColor)
	if pct >= 90 {
		budgetS = budgetS.Foreground(errColor)
	} else if pct >= 60 {
		budgetS = budgetS.Foreground(warnColor)
	}
	status := budgetS.Render(fmt.Sprintf("bot week %d/%d (%d%%)", spend, m.cfg.WeeklyTokenBudget, pct))
	for _, l := range m.limits {
		label := l.Scope
		if label == "session" {
			label = "5h"
		} else if strings.HasPrefix(label, "week (all") {
			label = "wk"
		} else {
			continue // per-model week lines stay in `csm usage`
		}
		limitS := lipgloss.NewStyle().Foreground(okColor)
		if l.Pct >= 90 {
			limitS = limitS.Foreground(errColor)
		} else if l.Pct >= 60 {
			limitS = limitS.Foreground(warnColor)
		}
		status += "  " + limitS.Render(fmt.Sprintf("%s %d%%", label, l.Pct))
	}
	if m.running != "" {
		status += "  " + m.spinner.View() + fmt.Sprintf("running [%s]...", m.running)
	}
	if m.extRun != nil {
		note := fmt.Sprintf("external run in progress (pid %d, %s)", m.extRun.PID, m.extRun.Trigger)
		if m.extRun.ItemID != "" {
			note += fmt.Sprintf(" on [%s]", m.extRun.ItemID)
		}
		status += "  " + flashS.Render(note)
	}
	if m.flash != "" {
		status += "  " + flashS.Render(m.flash)
	}
	return statusBarS.Render(" ") + status
}

func (m *model) helpView() string {
	var help string
	switch m.mode {
	case modeForm:
		help = "tab next field - shift+tab back - enter: newline in prompt, submit on priority - esc cancel"
	case modeDetail:
		help = "esc back"
	case modeConfirmDelete:
		if it := m.selected(); it != nil {
			return helpS.Render(fmt.Sprintf(" delete [%s]? y/n", it.ID))
		}
		help = "y/n"
	default:
		switch m.tab {
		case tabQueue:
			help = "n new - e edit - enter detail - R run - c claude session - r requeue - d delete - u refresh - tab switch - q quit"
		case tabConfig:
			if m.cfgEditing {
				help = "enter save - esc cancel"
			} else {
				help = "up/down select - enter edit - d reset to default - tab switch - q quit"
			}
		default:
			help = "up/down scroll - tab switch - q quit"
		}
	}
	return helpS.Render(" " + help)
}

func (m *model) detailView() string {
	item, err := queue.Find(m.items, m.detailID)
	if err != nil {
		return "item vanished"
	}
	mdl, eff, md := claude.ItemSettings(m.cfg, item)
	if eff == "" {
		eff = "cli default"
	}
	row := func(k, v string) string {
		if v == "" {
			return ""
		}
		return detailKeyS.Render(k) + " " + v + "\n"
	}
	tokens := ""
	if len(item.Tokens) > 0 {
		tokens = fmt.Sprintf("%d weighted (in %d, out %d, cache-read %d)",
			ledger.Weighted(item.Tokens),
			claude.UsageInt(item.Tokens, "input_tokens"),
			claude.UsageInt(item.Tokens, "output_tokens"),
			claude.UsageInt(item.Tokens, "cache_read_input_tokens"))
	}
	out := row("id", item.ID) +
		row("status", item.Status) +
		row("project", item.Project) +
		row("branch", item.Branch) +
		row("model", mdl) +
		row("effort", eff) +
		row("mode", md) +
		row("priority", fmt.Sprintf("%d", item.Priority)) +
		row("created", item.CreatedAt) +
		row("finished", item.FinishedAt) +
		row("session", item.SessionID) +
		row("summary", item.Summary) +
		row("error", item.Error) +
		row("tokens", tokens) +
		row("transcript", transcriptPath(item.ID)) +
		"\n" + detailKeyS.Render("prompt") + "\n" + renderPrompt(item.Prompt, max(20, m.width-4))
	return lipgloss.NewStyle().Padding(0, 1).Render(out)
}

// renderPrompt word-wraps a prompt; when it invokes a slash command/skill
// the command token is highlighted so it reads as such.
func renderPrompt(prompt string, width int) string {
	wrapped := wordwrap(prompt, width)
	if !claude.IsSlashPrompt(prompt) {
		return wrapped
	}
	token := strings.Fields(prompt)[0]
	styled := lipgloss.NewStyle().Foreground(slashColor).Bold(true).Render(token)
	return strings.Replace(wrapped, token, styled, 1)
}

// transcriptPath returns the saved run transcript for an item, or "".
func transcriptPath(itemID string) string {
	path := filepath.Join(config.LogsDir(), itemID+".md")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

func wordwrap(text string, width int) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		for len(line) > width {
			cut := strings.LastIndex(line[:width], " ")
			if cut <= 0 {
				cut = width
			}
			out = append(out, line[:cut])
			line = strings.TrimLeft(line[cut:], " ")
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
