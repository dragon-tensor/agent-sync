package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/agent-sync/agent-sync/internal/context"
	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/internal/groups"
	"github.com/agent-sync/agent-sync/internal/sync"
	"github.com/agent-sync/agent-sync/pkg/types"
)

type tab int

const (
	dashboardTab tab = iota
	sessionsTab
	contextTab
	groupsTab
	providersTab
)

type focusMode int

const (
	mainFocus focusMode = iota
	detailFocus
	inputFocus
	helpFocus
)

type model struct {
	activeTab tab
	focus     focusMode
	help      help.Model
	keys      keyMap

	db       *db.DB
	registry *sync.Registry
	store    *context.Store
	mergeEng *context.MergeEngine
	groupMgr *groups.Manager
	width    int
	height   int

	stats  map[string]interface{}
	err    error
	loaded bool

	sessions      []types.Session
	selectedRow   int
	sessionMsg    []types.Message
	showSession   bool

	contextEntries []types.ContextEntry
	ctxSelected    int
	showCtxDetail  bool

	groups []types.AgentGroup
	providers  []types.Provider

	inputMode  bool
	input      textinput.Model
	inputLabel string
	inputFn    func(string)

	statusMsg string
}

type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Enter    key.Binding
	Escape   key.Binding
	Back     key.Binding
	Tab      key.Binding
	Help     key.Binding
	Quit     key.Binding
	Refresh  key.Binding
	Sync     key.Binding
	Delete   key.Binding
	Save     key.Binding
	Search   key.Binding
	Number1  key.Binding
	Number2  key.Binding
	Number3  key.Binding
	Number4  key.Binding
	Number5  key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		Escape:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Back:    key.NewBinding(key.WithKeys("backspace", "q"), key.WithHelp("q", "back")),
		Tab:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next tab")),
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:    key.NewBinding(key.WithKeys("ctrl+c", "ctrl+q"), key.WithHelp("ctrl+c", "quit")),
		Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Sync:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sync")),
		Delete:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		Save:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		Search:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Number1: key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "dashboard")),
		Number2: key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "sessions")),
		Number3: key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "context")),
		Number4: key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "groups")),
		Number5: key.NewBinding(key.WithKeys("5"), key.WithHelp("5", "providers")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Quit, k.Tab, k.Up, k.Down, k.Enter, k.Help}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Number1, k.Number2, k.Number3, k.Number4, k.Number5},
		{k.Up, k.Down, k.Enter, k.Escape, k.Back},
		{k.Tab, k.Refresh, k.Sync, k.Delete, k.Save, k.Search},
		{k.Help, k.Quit},
	}
}

func New(database *db.DB, reg *sync.Registry, store *context.Store, mergeEng *context.MergeEngine, groupMgr *groups.Manager) *model {
	ti := textinput.New()
	ti.Placeholder = "type here..."
	ti.CharLimit = 500
	ti.Width = 50

	return &model{
		activeTab: 0,
		focus:     mainFocus,
		help:      help.New(),
		keys:      defaultKeyMap(),
		db:        database,
		registry:  reg,
		store:     store,
		mergeEng:  mergeEng,
		groupMgr:  groupMgr,
		input:     ti,
		statusMsg: "ready",
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.loadStatsCmd(), m.loadAllCmd())
}

func (m *model) loadStatsCmd() tea.Cmd {
	return func() tea.Msg {
		return statsMsg{stats: m.db.GetStats()}
	}
}

func (m *model) loadAllCmd() tea.Cmd {
	return func() tea.Msg {
		sessions, err := m.db.ListSessions("", 200, 0)
		if err != nil {
			return errMsg{err}
		}
		entries, err := m.store.List(200, 0)
		if err != nil {
			return errMsg{err}
		}
		groups, err := m.groupMgr.List()
		if err != nil {
			return errMsg{err}
		}
		providers, err := m.db.ListProviders()
		if err != nil {
			return errMsg{err}
		}
		return loadedMsg{sessions, entries, groups, providers}
	}
}

type statsMsg struct {
	stats map[string]interface{}
}

type loadedMsg struct {
	sessions  []types.Session
	entries   []types.ContextEntry
	groups    []types.AgentGroup
	providers []types.Provider
}

type errMsg struct {
	err error
}

type statusMsg struct {
	msg string
}

func setStatus(s string) tea.Cmd {
	return func() tea.Msg {
		return statusMsg{msg: s}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = msg.Width
		m.input.Width = msg.Width - 20
		return m, nil

	case tea.KeyMsg:
		if m.inputMode {
			return m.handleInputKey(msg)
		}
		return m.handleMainKey(msg)

	case statsMsg:
		m.stats = msg.stats
		m.loaded = true

	case loadedMsg:
		m.sessions = msg.sessions
		m.contextEntries = msg.entries
		m.groups = msg.groups
		m.providers = msg.providers

	case errMsg:
		m.err = msg.err
		m.statusMsg = fmt.Sprintf("error: %v", msg.err)

	case statusMsg:
		m.statusMsg = msg.msg
	}

	return m, tea.Batch(cmds...)
}

func (m *model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		val := m.input.Value()
		m.inputMode = false
		m.input.SetValue("")
		if m.inputFn != nil {
			m.inputFn(val)
		}
		return m, tea.Batch(m.loadStatsCmd(), m.loadAllCmd())

	case "esc":
		m.inputMode = false
		m.input.SetValue("")
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *model) handleMainKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Quit) {
		return m, tea.Quit
	}

	if key.Matches(msg, m.keys.Help) {
		m.help.ShowAll = !m.help.ShowAll
		return m, nil
	}

	if key.Matches(msg, m.keys.Escape) {
		if m.showSession {
			m.showSession = false
			return m, nil
		}
		if m.showCtxDetail {
			m.showCtxDetail = false
			return m, nil
		}
		return m, nil
	}

	if key.Matches(msg, m.keys.Tab) {
		m.activeTab = (m.activeTab + 1) % 5
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Number1):
		m.activeTab = dashboardTab
	case key.Matches(msg, m.keys.Number2):
		m.activeTab = sessionsTab
	case key.Matches(msg, m.keys.Number3):
		m.activeTab = contextTab
	case key.Matches(msg, m.keys.Number4):
		m.activeTab = groupsTab
	case key.Matches(msg, m.keys.Number5):
		m.activeTab = providersTab
	}

	if m.showSession {
		return m.handleSessionKey(msg)
	}
	if m.showCtxDetail {
		return m, nil
	}

	switch m.activeTab {
	case sessionsTab:
		return m.handleSessionsKey(msg)
	case contextTab:
		return m.handleContextKey(msg)
	case groupsTab:
		return m.handleGroupsKey(msg)
	case providersTab:
		return m.handleProvidersKey(msg)
	}

	return m, nil
}

func (m *model) handleSessionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Escape, m.keys.Back) {
		m.showSession = false
	}
	return m, nil
}

func (m *model) handleSessionsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		if m.selectedRow > 0 {
			m.selectedRow--
		}
	case key.Matches(msg, m.keys.Down):
		if m.selectedRow < len(m.sessions)-1 {
			m.selectedRow++
		}
	case key.Matches(msg, m.keys.Enter):
		if len(m.sessions) > 0 && m.selectedRow < len(m.sessions) {
			s := m.sessions[m.selectedRow]
			msgs, err := m.db.GetSessionMessages(s.ID)
			if err == nil {
				m.sessionMsg = msgs
				m.showSession = true
			}
		}
	case key.Matches(msg, m.keys.Refresh):
		return m, tea.Batch(m.loadStatsCmd(), m.loadAllCmd())
	}
	return m, nil
}

func (m *model) handleContextKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		if m.ctxSelected > 0 {
			m.ctxSelected--
		}
	case key.Matches(msg, m.keys.Down):
		if m.ctxSelected < len(m.contextEntries)-1 {
			m.ctxSelected++
		}
	case key.Matches(msg, m.keys.Enter):
		if len(m.contextEntries) > 0 && m.ctxSelected < len(m.contextEntries) {
			m.showCtxDetail = true
		}
	case key.Matches(msg, m.keys.Save):
		m.startInput("Context content: ", func(val string) {
			_, err := m.store.Save(val, "", "tui", "", nil)
			if err != nil {
				m.statusMsg = fmt.Sprintf("save error: %v", err)
				return
			}
			m.statusMsg = "context saved"
		})
	case key.Matches(msg, m.keys.Delete):
		if len(m.contextEntries) > 0 && m.ctxSelected < len(m.contextEntries) {
			id := m.contextEntries[m.ctxSelected].ID
			m.store.Delete(id)
			m.statusMsg = "context deleted"
			return m, tea.Batch(m.loadStatsCmd(), m.loadAllCmd())
		}
	case key.Matches(msg, m.keys.Refresh):
		return m, tea.Batch(m.loadStatsCmd(), m.loadAllCmd())
	}
	return m, nil
}

func (m *model) handleGroupsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Save):
		m.startInput("Group name: ", func(val string) {
			_, err := m.groupMgr.Create(val, "created from tui", nil)
			if err != nil {
				m.statusMsg = fmt.Sprintf("group error: %v", err)
				return
			}
			m.statusMsg = fmt.Sprintf("group %q created", val)
		})
	case key.Matches(msg, m.keys.Delete):
		if len(m.groups) > 0 {
			id := m.groups[0].ID
			m.groupMgr.Delete(id)
			m.statusMsg = "group deleted"
			return m, tea.Batch(m.loadStatsCmd(), m.loadAllCmd())
		}
	case key.Matches(msg, m.keys.Refresh):
		return m, tea.Batch(m.loadStatsCmd(), m.loadAllCmd())
	}
	return m, nil
}

func (m *model) handleProvidersKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Sync):
		m.statusMsg = "syncing..."
		for _, p := range m.registry.List() {
			stats, err := p.Sync()
			if err != nil {
				m.statusMsg = fmt.Sprintf("sync error: %v", err)
				continue
			}
			m.statusMsg = fmt.Sprintf("synced %s: %d sessions", p.Name(), stats.SessionsFound)
		}
		return m, tea.Batch(m.loadStatsCmd(), m.loadAllCmd())
	case key.Matches(msg, m.keys.Refresh):
		return m, tea.Batch(m.loadStatsCmd(), m.loadAllCmd())
	}
	return m, nil
}

func (m *model) startInput(label string, fn func(string)) {
	m.inputMode = true
	m.inputLabel = label
	m.inputFn = fn
	m.input.Focus()
	m.input.SetValue("")
}

func (m *model) View() string {
	if !m.loaded {
		return lipgloss.NewStyle().Padding(4, 4).Render("Loading...")
	}

	if m.inputMode {
		return m.inputView()
	}

	if m.showSession {
		return m.sessionDetailView()
	}

	if m.showCtxDetail && m.ctxSelected < len(m.contextEntries) {
		return m.contextDetailView()
	}

	var body string
	switch m.activeTab {
	case dashboardTab:
		body = m.dashboardView()
	case sessionsTab:
		body = m.sessionsView()
	case contextTab:
		body = m.contextView()
	case groupsTab:
		body = m.groupsView()
	case providersTab:
		body = m.providersView()
	}

	return appStyle.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			m.headerView(),
			body,
			m.statusView(),
		),
	)
}

func (m *model) headerView() string {
	tabs := []struct {
		label string
		t     tab
	}{
		{"1:Dashboard", dashboardTab},
		{"2:Sessions", sessionsTab},
		{"3:Context", contextTab},
		{"4:Groups", groupsTab},
		{"5:Providers", providersTab},
	}

	var rendered []string
	for _, t := range tabs {
		if t.t == m.activeTab {
			rendered = append(rendered, activeTabStyle.Render(t.label))
		} else {
			rendered = append(rendered, tabStyle.Render(t.label))
		}
	}

	return tabBarStyle.Render(
		lipgloss.JoinHorizontal(lipgloss.Top, rendered...),
	)
}

func (m *model) statusView() string {
	helpText := "tab/1-5:navigate • enter:select • s:sync • n:new • d:delete • /:search • r:refresh • ?:keys • q/esc:back • ctrl+c:quit"
	if m.help.ShowAll {
		helpText = m.help.View(m.keys)
	}
	return statusBarStyle.Render(
		lipgloss.JoinHorizontal(lipgloss.Left,
			infoStyle.Render(" "+m.statusMsg+" "),
			subtleStyle.Render("  "),
			subtleStyle.Render(helpText),
		),
	)
}

func (m *model) dashboardView() string {
	m.stats = m.db.GetStats()

	statCards := []struct {
		label string
		value interface{}
		sub   string
	}{
		{"Sessions", m.stats["total_sessions"], "synced conversations"},
		{"Messages", m.stats["total_messages"], fmt.Sprintf("%v tokens", m.stats["total_tokens"])},
		{"Context", m.stats["total_context_entries"], "shared entries"},
		{"Providers", m.stats["total_providers"], fmt.Sprintf("%v active", m.stats["active_providers"])},
	}

	var cards []string
	for _, c := range statCards {
		rendered := cardStyle.Render(
			lipgloss.JoinVertical(lipgloss.Center,
				subtleStyle.Render(strings.ToUpper(c.label)),
				accentStyle.Render(fmt.Sprintf("%v", c.value)),
				subtleStyle.Render(c.sub),
			),
		)
		cards = append(cards, rendered)
	}

	statsRow := lipgloss.JoinHorizontal(lipgloss.Top, cards...)

	recent := ""
	if len(m.sessions) > 0 {
		limit := 5
		if limit > len(m.sessions) {
			limit = len(m.sessions)
		}
		var lines []string
		for i := 0; i < limit; i++ {
			s := m.sessions[i]
			title := s.Title
			if len(title) > 40 {
				title = title[:40] + "..."
			}
			provider := yellowStyle.Render(string(s.Provider))
			timeStr := s.StartedAt.Format("Jan 02 15:04")
			lines = append(lines, fmt.Sprintf("  %s [%s] %s — %d msgs",
				timeStr, provider, title, s.MessageCount))
		}
		recent = lipgloss.JoinVertical(lipgloss.Left,
			headerStyle.Render("Recent Sessions"),
			strings.Join(lines, "\n"),
		)
	} else {
		recent = subtleStyle.Render("No sessions yet. Go to Providers tab and press 's' to sync.")
	}

	detected := subtleStyle.Render("")
	detectedList := m.registry.DetectAll()
	if len(detectedList) > 0 {
		var names []string
		for _, p := range detectedList {
			names = append(names, greenStyle.Render(p.Name()))
		}
		detected = headerStyle.Render("Detected: ") +
			lipgloss.JoinHorizontal(lipgloss.Left, strings.Join(names, "  "))
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		headerStyle.Render("Dashboard"),
		"",
		statsRow,
		"",
		recent,
		"",
		detected,
	)
}

func (m *model) sessionsView() string {
	if len(m.sessions) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			headerStyle.Render("Sessions"),
			"",
			subtleStyle.Render("No sessions found. Use Providers tab to sync (press 's')."),
		)
	}

	columns := []table.Column{
		{Title: "Provider", Width: 14},
		{Title: "Title", Width: 50},
		{Title: "Date", Width: 14},
		{Title: "Msgs", Width: 6},
		{Title: "Tokens", Width: 10},
	}

	var rows []table.Row
	for _, s := range m.sessions {
		title := s.Title
		if len(title) > 48 {
			title = title[:48] + "..."
		}
		rows = append(rows, table.Row{
			string(s.Provider),
			title,
			s.StartedAt.Format("2006-01-02"),
			fmt.Sprintf("%d", s.MessageCount),
			fmt.Sprintf("%d", s.TokenCount),
		})
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(min(15, len(rows))),
	)

	t.SetStyles(table.Styles{
		Selected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#58a6ff")).
			Bold(true).
			Background(lipgloss.Color("#161b22")),
		Header: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8b949e")).
			Bold(true).
			Padding(0, 1),
		Cell: lipgloss.NewStyle().
			Padding(0, 1),
	})

	return lipgloss.JoinVertical(lipgloss.Left,
		headerStyle.Render(fmt.Sprintf("Sessions (%d)", len(m.sessions))),
		"",
		t.View(),
		"",
		subtleStyle.Render("↑/↓ navigate · enter view messages · esc back · r refresh"),
	)
}

func (m *model) sessionDetailView() string {
	s := m.sessions[m.selectedRow]
	var msgLines []string
	msgLines = append(msgLines, headerStyle.Render(s.Title))
	msgLines = append(msgLines, fmt.Sprintf("Provider: %s  |  Model: %s  |  Date: %s  |  Messages: %d",
		yellowStyle.Render(string(s.Provider)),
		s.Model,
		s.StartedAt.Format("Jan 02 2006 15:04"),
		len(m.sessionMsg),
	))
	msgLines = append(msgLines, "")

	for _, msg := range m.sessionMsg {
		role := greenStyle.Render("USER")
		if msg.Role == "assistant" {
			role = accentStyle.Render("ASSISTANT")
		}
		content := msg.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		msgLines = append(msgLines, lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#30363d")).
			Padding(0, 1).
			Width(m.width-10).
			Render(
				lipgloss.JoinVertical(lipgloss.Left,
					role,
					content,
				),
			))
		msgLines = append(msgLines, "")
	}

	msgLines = append(msgLines, subtleStyle.Render("esc: back"))

	return lipgloss.JoinVertical(lipgloss.Left, msgLines...)
}

func (m *model) contextView() string {
	if len(m.contextEntries) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			headerStyle.Render(fmt.Sprintf("Context (%d)", len(m.contextEntries))),
			"",
			subtleStyle.Render("No context entries. Press 'n' to create one."),
		)
	}

	var items []string
	for i, e := range m.contextEntries {
		summary := e.Summary
		if summary == "" {
			content := e.Content
			if len(content) > 80 {
				content = content[:80] + "..."
			}
			summary = content
		}
		summary = strings.ReplaceAll(summary, "\n", " ")
		prefix := "  "
		if i == m.ctxSelected {
			prefix = "▸ "
		}

		entry := fmt.Sprintf("%s%s [%s] %s", prefix, e.ID[:8], e.Source, summary)
		if i == m.ctxSelected {
			items = append(items, selectedItemStyle.Render(entry))
		} else {
			items = append(items, entry)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		headerStyle.Render(fmt.Sprintf("Context (%d)", len(m.contextEntries))),
		"",
		strings.Join(items, "\n"),
		"",
		subtleStyle.Render("↑/↓ navigate · enter view · n new · d delete · r refresh"),
	)
}

func (m *model) contextDetailView() string {
	e := m.contextEntries[m.ctxSelected]
	content := e.Content
	if len(content) > 1000 {
		content = content[:1000] + "..."
	}

	tags := ""
	if len(e.Tags) > 0 {
		tags = strings.Join(e.Tags, ", ")
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		headerStyle.Render("Context Detail"),
		"",
		lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#30363d")).Padding(1, 2).Width(m.width-10).Render(
			lipgloss.JoinVertical(lipgloss.Left,
				fmt.Sprintf("ID:     %s", e.ID),
				fmt.Sprintf("Source: %s", e.Source),
				fmt.Sprintf("Tags:   %s", tags),
				fmt.Sprintf("Updated: %s", e.UpdatedAt.Format("Jan 02 2006 15:04")),
				"",
				content,
			),
		),
		"",
		subtleStyle.Render("esc: back"),
	)
}

func (m *model) groupsView() string {
	if len(m.groups) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			headerStyle.Render("Agent Groups (selected-universal)"),
			"",
			subtleStyle.Render("No groups. Press 'n' to create one."),
			"",
			infoStyle.Render("Groups share context across selected agents (selected-universal)."),
		)
	}

	var items []string
	for _, grp := range m.groups {
		providers := strings.Join(grp.ProviderIDs, ", ")
		if providers == "" {
			providers = "all"
		}
		items = append(items, fmt.Sprintf("  %s — %s  (%d context entries)",
			accentStyle.Render(grp.Name),
			providers,
			len(grp.ContextIDs),
		))
		if grp.Description != "" {
			items = append(items, fmt.Sprintf("    %s", subtleStyle.Render(grp.Description)))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		headerStyle.Render(fmt.Sprintf("Agent Groups (%d)", len(m.groups))),
		"",
		strings.Join(items, "\n"),
		"",
		subtleStyle.Render("n: new group · d: delete · r: refresh"),
	)
}

func (m *model) providersView() string {
	if len(m.providers) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			headerStyle.Render("Providers"),
			"",
			subtleStyle.Render("No providers configured. Press 's' to sync (auto-detects providers)."),
		)
	}

	columns := []table.Column{
		{Title: "Status", Width: 8},
		{Title: "Name", Width: 16},
		{Title: "Type", Width: 14},
		{Title: "Last Sync", Width: 18},
	}

	var rows []table.Row
	for _, p := range m.providers {
		status := greenStyle.Render("●")
		if !p.Enabled {
			status = redStyle.Render("○")
		}
		lastSync := "never"
		if p.LastSync != nil {
			lastSync = p.LastSync.Format("2006-01-02 15:04")
		}
		rows = append(rows, table.Row{
			status,
			p.Name,
			string(p.Type),
			lastSync,
		})
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(len(rows)),
	)
	t.SetStyles(table.Styles{
		Selected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#58a6ff")).
			Background(lipgloss.Color("#161b22")),
		Header: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8b949e")).
			Bold(true).Padding(0, 1),
		Cell: lipgloss.NewStyle().Padding(0, 1),
	})

	return lipgloss.JoinVertical(lipgloss.Left,
		headerStyle.Render(fmt.Sprintf("Providers (%d)", len(m.providers))),
		"",
		t.View(),
		"",
		subtleStyle.Render("s: sync all · r: refresh"),
	)
}

func (m *model) inputView() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		headerStyle.Render(m.inputLabel),
		"",
		m.input.View(),
		"",
		subtleStyle.Render("enter: confirm · esc: cancel"),
	)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
