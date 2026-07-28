package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agent-sync/agent-sync/internal/chat"
	"github.com/agent-sync/agent-sync/pkg/types"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	background = lipgloss.Color("#000000")
	border     = lipgloss.Color("#5A5A5A")
	muted      = lipgloss.Color("#808080")
	text       = lipgloss.Color("#F2F2F2")
	accent     = lipgloss.Color("#8DE1C7")

	terminalBorder = lipgloss.Border{Top: "-", Bottom: "-", Left: "|", Right: "|", TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+"}
)

type pickerMode int

const (
	pickerNone pickerMode = iota
	pickerStart
	pickerSwitch
	pickerResume
	pickerImport
)

type choice struct{ label, value string }

type sentMessage struct {
	message *chat.Message
	err     error
}

type loadedChat struct {
	chat     *chat.Chat
	messages []chat.Message
	sessions []chat.AgentSession
	err      error
}

type importSessionsLoaded struct {
	sessions []types.Session
	err      error
}

type Model struct {
	service  *chat.Service
	width    int
	height   int
	draft    string
	status   string
	path     string
	project  string
	current  *chat.Chat
	messages []chat.Message
	sessions []chat.AgentSession
	busy     bool
	picker   pickerMode
	choices  []choice
	selected int
}

func NewModel(service *chat.Service) Model {
	project := currentDirectory()
	return Model{service: service, status: "Enter /start to choose a local agent", path: displayDirectory(project), project: project}
}

func NewProgram(service *chat.Service) *tea.Program {
	return tea.NewProgram(NewModel(service), tea.WithAltScreen())
}
func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case sentMessage:
		m.busy = false
		if msg.err != nil {
			m.status = "Agent error: " + oneLine(msg.err.Error())
			return m, nil
		}
		m.messages = append(m.messages, *msg.message)
		m.status = string(m.current.ActiveAgent) + " replied"
		return m, m.loadCurrent()
	case loadedChat:
		if msg.err != nil {
			m.status = "Storage error: " + oneLine(msg.err.Error())
			return m, nil
		}
		m.current, m.messages, m.sessions = msg.chat, msg.messages, msg.sessions
		return m, nil
	case importSessionsLoaded:
		m.busy = false
		if msg.err != nil {
			m.status = "Import scan failed: " + oneLine(msg.err.Error())
			return m, nil
		}
		if len(msg.sessions) == 0 {
			m.status = "No local tool sessions found to import"
			return m, nil
		}
		m.picker, m.selected, m.choices = pickerImport, 0, nil
		for _, item := range msg.sessions {
			m.choices = append(m.choices, choice{label: fmt.Sprintf("%s  ·  %s", trim(item.Title, 40), item.Provider), value: item.ID})
		}
		m.status = "Choose a local session to import"
		return m, nil
	case tea.KeyMsg:
		if m.busy {
			if msg.String() == "ctrl+c" || msg.String() == "ctrl+q" {
				return m, tea.Quit
			}
			return m, nil
		}
		if m.picker != pickerNone {
			return m.updatePicker(msg)
		}
		switch msg.String() {
		case "ctrl+c", "ctrl+q", "esc":
			return m, tea.Quit
		case "backspace":
			if len(m.draft) > 0 {
				m.draft = m.draft[:len(m.draft)-1]
			}
		case "enter":
			return m.submit()
		default:
			if len(msg.Runes) > 0 {
				m.draft += string(msg.Runes)
			}
		}
	}
	return m, nil
}

func (m Model) submit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.draft)
	if input == "" {
		return m, nil
	}
	m.draft = ""
	switch input {
	case "/start":
		m.openAgentPicker(pickerStart)
		return m, nil
	case "/resume":
		chats, err := m.service.ListChats()
		if err != nil {
			m.status = "Storage error: " + oneLine(err.Error())
			return m, nil
		}
		if len(chats) == 0 {
			m.status = "No Dragon Sync chats yet — use /start"
			return m, nil
		}
		m.picker, m.selected = pickerResume, 0
		m.choices = make([]choice, 0, len(chats))
		for _, item := range chats {
			m.choices = append(m.choices, choice{label: fmt.Sprintf("%s  ·  %s", trim(item.Title, 42), item.ActiveAgent), value: item.ID})
		}
		m.status = "Choose a Dragon Sync chat to resume"
		return m, nil
	case "/switch":
		if m.current == nil {
			m.status = "Start or resume a chat first"
			return m, nil
		}
		m.openAgentPicker(pickerSwitch)
		return m, nil
	case "/import":
		m.busy, m.status = true, "Scanning local tool histories…"
		return m, func() tea.Msg {
			sessions, err := m.service.ScanImportableSessions()
			return importSessionsLoaded{sessions: sessions, err: err}
		}
	}
	if m.current == nil {
		m.status = "Use /start to create a chat before sending a message"
		return m, nil
	}
	m.busy, m.status = true, "Sending to "+string(m.current.ActiveAgent)+"…"
	chatID := m.current.ID
	return m, func() tea.Msg {
		reply, err := m.service.Send(context.Background(), chatID, input)
		return sentMessage{message: reply, err: err}
	}
}

func (m *Model) openAgentPicker(mode pickerMode) {
	agents := chat.AvailableAgents()
	m.picker, m.selected, m.choices = mode, 0, nil
	for _, agent := range agents {
		m.choices = append(m.choices, choice{label: strings.ToUpper(string(agent)), value: string(agent)})
	}
	if len(m.choices) == 0 {
		m.picker = pickerNone
		m.status = "No supported local CLI found in PATH"
		return
	}
	if mode == pickerStart {
		m.status = "Choose the first agent for this chat"
	} else {
		m.status = "Choose the next agent; it will receive only unseen work"
	}
}

func (m Model) updatePicker(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.picker, m.choices, m.status = pickerNone, nil, "Selection cancelled"
		return m, nil
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
	case "down", "j":
		if m.selected < len(m.choices)-1 {
			m.selected++
		}
	case "enter":
		if len(m.choices) == 0 {
			return m, nil
		}
		picked := m.choices[m.selected]
		mode := m.picker
		m.picker, m.choices = pickerNone, nil
		switch mode {
		case pickerStart:
			created, err := m.service.Start(m.project, chat.Agent(picked.value))
			if err != nil {
				m.status = "Could not start chat: " + oneLine(err.Error())
				return m, nil
			}
			m.current, m.messages, m.sessions = created, nil, nil
			m.status = "Chat started with " + strings.ToUpper(picked.value)
		case pickerSwitch:
			updated, err := m.service.Switch(m.current.ID, chat.Agent(picked.value))
			if err != nil {
				m.status = "Could not switch: " + oneLine(err.Error())
				return m, nil
			}
			m.current = updated
			m.status = "Switched to " + strings.ToUpper(picked.value) + " — next message will include unseen work"
			return m, m.loadCurrent()
		case pickerResume:
			m.status = "Resuming chat…"
			return m, m.loadChat(picked.value)
		case pickerImport:
			available := chat.AvailableAgents()
			if len(available) == 0 {
				m.status = "No supported local CLI available to continue the imported chat"
				return m, nil
			}
			imported, err := m.service.ImportLegacySession(picked.value, available[0])
			if err != nil {
				m.status = "Could not import: " + oneLine(err.Error())
				return m, nil
			}
			m.current, m.status = imported, "Imported into Dragon Sync; switch agents whenever you want"
			return m, m.loadCurrent()
		}
	}
	return m, nil
}

func (m Model) loadCurrent() tea.Cmd {
	if m.current == nil {
		return nil
	}
	return m.loadChat(m.current.ID)
}

func (m Model) loadChat(id string) tea.Cmd {
	return func() tea.Msg {
		item, err := m.service.Resume(id)
		if err != nil {
			return loadedChat{err: err}
		}
		messages, err := m.service.Messages(id)
		if err != nil {
			return loadedChat{err: err}
		}
		sessions, err := m.service.AgentSessions(id)
		if err != nil {
			return loadedChat{err: err}
		}
		return loadedChat{chat: item, messages: messages, sessions: sessions}
	}
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Starting Dragon Sync…"
	}
	contentWidth := max(40, m.width-2)
	document := lipgloss.NewStyle().Background(background).Foreground(text).Width(contentWidth).Padding(0, 1)
	chatWidth := contentWidth - 34
	if chatWidth < 42 {
		chatWidth = contentWidth
	}
	chatHeight := max(14, m.height-9)
	chatView := m.chatPanel(chatWidth, chatHeight)
	if contentWidth >= 92 {
		return document.Render(lipgloss.JoinVertical(lipgloss.Left, m.header(contentWidth), lipgloss.JoinHorizontal(lipgloss.Top, chatView, m.sidePanel(32, chatHeight)), m.footer(contentWidth)))
	}
	return document.Render(lipgloss.JoinVertical(lipgloss.Left, m.header(contentWidth), chatView, m.sidePanel(contentWidth, 18), m.footer(contentWidth)))
}

func (m Model) header(width int) string {
	brand := lipgloss.NewStyle().Bold(true).Foreground(text).Render("DRAGON") + " " + lipgloss.NewStyle().Bold(true).Foreground(accent).Render("SYNC")
	mode := lipgloss.NewStyle().Foreground(muted).Render("/ CHAT")
	right := lipgloss.NewStyle().Foreground(muted).Render("v0.1 · LOCAL")
	left := lipgloss.JoinHorizontal(lipgloss.Center, brand, "  ", mode)
	return lipgloss.NewStyle().Width(width).Padding(1, 1, 0).Render(lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", max(0, width-lipgloss.Width(left)-lipgloss.Width(right)-4)), right))
}

func (m Model) chatPanel(width, height int) string {
	inner := max(20, width-4)
	title, meta := "Conversation", "use /start · /resume · /switch"
	if m.current != nil {
		title, meta = m.current.Title, "active · "+strings.ToUpper(string(m.current.ActiveAgent))
	}
	header := lipgloss.JoinHorizontal(lipgloss.Center, lipgloss.NewStyle().Bold(true).Foreground(text).Render(trim(title, 36)), "  ", lipgloss.NewStyle().Foreground(muted).Render(meta))
	content := m.conversation(inner - 4)
	if m.picker != pickerNone {
		content = m.pickerView(inner - 4)
	}
	lines := lipgloss.Height(content)
	blank := strings.Repeat("\n", max(0, height-lipgloss.Height(header)-lines-5))
	prompt := lipgloss.NewStyle().Foreground(accent).Bold(true).Render("› ")
	draft := m.draft
	if draft == "" {
		draft = lipgloss.NewStyle().Foreground(muted).Render(m.placeholder())
	} else {
		draft = lipgloss.NewStyle().Foreground(text).Render(draft)
	}
	input := lipgloss.NewStyle().Border(terminalBorder).BorderForeground(border).Padding(0, 1).Width(max(20, inner-2)).Render(lipgloss.JoinHorizontal(lipgloss.Center, prompt, draft))
	return lipgloss.NewStyle().Border(terminalBorder).BorderForeground(border).Padding(1, 1).Width(max(20, width-2)).Height(height).Render(lipgloss.JoinVertical(lipgloss.Left, header, "", content, blank, input))
}

func (m Model) conversation(width int) string {
	if len(m.messages) == 0 {
		body := "Dragon Sync stores the canonical conversation locally. Choose an agent with /start; use /switch at any time to transfer only work that agent has not already seen."
		return lipgloss.NewStyle().Foreground(muted).Width(width).Render(body)
	}
	start := max(0, len(m.messages)-8)
	var lines []string
	for _, message := range m.messages[start:] {
		who, color := "YOU", text
		if message.Role == "assistant" {
			who, color = strings.ToUpper(string(message.Agent)), accent
		}
		lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(color).Render(who), lipgloss.NewStyle().Foreground(text).Width(width).Render(message.Content), "")
	}
	if m.busy {
		lines = append(lines, lipgloss.NewStyle().Foreground(muted).Render(strings.ToUpper(string(m.current.ActiveAgent))+" IS WORKING…"))
	}
	return strings.Join(lines, "\n")
}

func (m Model) pickerView(width int) string {
	title := "SELECT AGENT"
	if m.picker == pickerResume {
		title = "RESUME CHAT"
	} else if m.picker == pickerImport {
		title = "IMPORT SESSION"
	}
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(accent).Render(title), lipgloss.NewStyle().Foreground(muted).Render("↑/↓ choose · enter confirm · esc cancel"), ""}
	for index, item := range m.choices {
		marker, style := "  ", lipgloss.NewStyle().Foreground(text)
		if index == m.selected {
			marker, style = "› ", lipgloss.NewStyle().Foreground(accent).Bold(true)
		}
		lines = append(lines, style.Render(marker+item.label))
	}
	return lipgloss.NewStyle().Border(terminalBorder).BorderForeground(border).Padding(1).Width(max(20, width-2)).Render(strings.Join(lines, "\n"))
}

func (m Model) sidePanel(width, height int) string {
	current, previous := "Not started", "None"
	if m.current != nil {
		current = strings.ToUpper(string(m.current.ActiveAgent))
		if len(m.sessions) > 1 {
			previous = strings.ToUpper(string(m.sessions[1].Agent))
		}
	}
	section := func(label, value string) string {
		return lipgloss.JoinVertical(lipgloss.Left, lipgloss.NewStyle().Foreground(muted).Render(label), lipgloss.NewStyle().Foreground(text).Render(value))
	}
	chain := []string{chainRow("CURRENT", current, accent)}
	for index, session := range m.sessions {
		if m.current != nil && session.Agent == m.current.ActiveAgent {
			continue
		}
		chain = append(chain, chainLink(), chainRow(fmt.Sprintf("PREVIOUS · %d", index+1), strings.ToUpper(string(session.Agent)), muted))
	}
	content := lipgloss.JoinVertical(lipgloss.Left, lipgloss.NewStyle().Bold(true).Foreground(text).Render("Session info"), "", section("CURRENT AGENT", current), "", section("PREVIOUS AGENT", previous), "", section("LEDGER MESSAGES", fmt.Sprintf("%d", len(m.messages))), section("AGENT SESSIONS", fmt.Sprintf("%d", len(m.sessions))), section("THREADS", "01"), "\n"+lipgloss.NewStyle().Bold(true).Foreground(text).Render("Agent chain"), "", strings.Join(chain, "\n"), "\n"+lipgloss.NewStyle().Bold(true).Foreground(text).Render("Commands"), lipgloss.NewStyle().Foreground(muted).Render("/start /resume /switch /import"))
	return lipgloss.NewStyle().Border(terminalBorder).BorderForeground(border).Padding(1, 1).Width(max(20, width-2)).Height(height).Render(content)
}

func chainRow(label, value string, color lipgloss.Color) string {
	return lipgloss.JoinHorizontal(lipgloss.Center, lipgloss.NewStyle().Foreground(color).Render("*"), " ", lipgloss.JoinVertical(lipgloss.Left, lipgloss.NewStyle().Foreground(muted).Render(label), lipgloss.NewStyle().Foreground(color).Render(value)))
}
func chainLink() string { return lipgloss.NewStyle().Foreground(border).Render("  │") }

func (m Model) footer(width int) string {
	directory := lipgloss.NewStyle().Foreground(muted).Render("DIR ") + lipgloss.NewStyle().Foreground(text).Render(m.path)
	hints := lipgloss.NewStyle().Foreground(muted).Render("enter send  ·  esc quit")
	status := lipgloss.NewStyle().Foreground(muted).Render(m.status)
	if width < 100 {
		return lipgloss.NewStyle().Width(width).Padding(0, 1, 1).Render(lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Center, directory, "  ", status), hints))
	}
	return lipgloss.NewStyle().Width(width).Padding(0, 1, 1).Render(lipgloss.JoinHorizontal(lipgloss.Center, directory, "  ", status, strings.Repeat(" ", max(0, width-lipgloss.Width(directory)-lipgloss.Width(status)-lipgloss.Width(hints)-8)), hints))
}

func (m Model) placeholder() string {
	if m.current == nil {
		return "Use /start, /resume, or /import"
	}
	return "Message " + strings.ToUpper(string(m.current.ActiveAgent)) + "…"
}
func currentDirectory() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}
func displayDirectory(cwd string) string {
	home, err := os.UserHomeDir()
	if err == nil {
		if rel, relErr := filepath.Rel(home, cwd); relErr == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
		if cwd == home {
			return "~"
		}
	}
	return filepath.ToSlash(cwd)
}
func oneLine(value string) string { return trim(strings.ReplaceAll(value, "\n", " "), 110) }
func trim(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit-1] + "…"
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
