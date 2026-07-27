package tui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	background = lipgloss.Color("#000000")
	border     = lipgloss.Color("#5A5A5A")
	muted      = lipgloss.Color("#808080")
	text       = lipgloss.Color("#F2F2F2")
	accent     = lipgloss.Color("#8DE1C7")

	terminalBorder = lipgloss.Border{
		Top:         "-",
		Bottom:      "-",
		Left:        "|",
		Right:       "|",
		TopLeft:     "+",
		TopRight:    "+",
		BottomLeft:  "+",
		BottomRight: "+",
	}
)

type Model struct {
	width  int
	height int
	draft  string
	status string
	path   string
}

func NewModel() Model {
	return Model{
		status: "TUI shell · actions not connected",
		path:   workingDirectory(),
	}
}

func NewProgram() *tea.Program {
	return tea.NewProgram(NewModel(), tea.WithAltScreen())
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+q", "esc":
			return m, tea.Quit
		case "backspace":
			if len(m.draft) > 0 {
				m.draft = m.draft[:len(m.draft)-1]
			}
		case "enter":
			if strings.TrimSpace(m.draft) != "" {
				m.status = "Draft captured · provider actions will be connected next"
			}
		case "tab":
			m.status = "Chat surface focused · side panel is read-only for now"
		default:
			if len(msg.Runes) > 0 {
				m.draft += string(msg.Runes)
				m.status = "Drafting · provider actions will be connected next"
			}
		}
	}

	return m, nil
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Starting Dragon Sync…"
	}

	contentWidth := m.width - 2
	if contentWidth < 40 {
		contentWidth = 40
	}

	document := lipgloss.NewStyle().
		Background(background).
		Foreground(text).
		Width(contentWidth).
		Padding(0, 1)

	header := m.header(contentWidth)
	chatWidth := contentWidth - 34
	if chatWidth < 42 {
		chatWidth = contentWidth
	}

	chatHeight := m.height - 9
	if chatHeight < 14 {
		chatHeight = 14
	}

	chat := m.chatPanel(chatWidth, chatHeight)
	if contentWidth >= 92 {
		body := lipgloss.JoinHorizontal(lipgloss.Top, chat, m.sidePanel(32, chatHeight))
		return document.Render(lipgloss.JoinVertical(lipgloss.Left, header, body, m.footer(contentWidth)))
	}

	side := m.sidePanel(contentWidth, 18)
	return document.Render(lipgloss.JoinVertical(lipgloss.Left, header, chat, side, m.footer(contentWidth)))
}

func (m Model) header(width int) string {
	brand := lipgloss.NewStyle().Bold(true).Foreground(text).Render("DRAGON") + " " + lipgloss.NewStyle().Bold(true).Foreground(accent).Render("SYNC")
	mode := lipgloss.NewStyle().Foreground(muted).Render("/ CHAT")
	local := lipgloss.NewStyle().Foreground(muted).Render("LOCAL")
	left := lipgloss.JoinHorizontal(lipgloss.Center, brand, "  ", mode)
	right := lipgloss.NewStyle().Foreground(muted).Render("v0.1 · MVP shell")
	spacer := max(0, width-lipgloss.Width(left)-lipgloss.Width(right)-lipgloss.Width(local)-8)
	return lipgloss.NewStyle().Width(width).Padding(1, 1, 0).Render(
		lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", spacer), right, "  ", local),
	)
}

func (m Model) chatPanel(width, height int) string {
	innerWidth := max(20, width-4)
	title := lipgloss.NewStyle().Bold(true).Foreground(text).Render("Conversation")
	meta := lipgloss.NewStyle().Foreground(muted).Render("portable chat · thread 01")
	header := lipgloss.JoinHorizontal(lipgloss.Center, title, "  ", meta)

	welcomeTitle := lipgloss.NewStyle().Bold(true).Foreground(text).Render("DRAGON SYNC READY")
	welcomeBody := lipgloss.NewStyle().Foreground(muted).Width(innerWidth - 4).Render(
		"This is the chat shell for your universal agent workspace. Provider switching, context transfer, and threads will be connected after the interface is approved.",
	)
	welcome := lipgloss.NewStyle().
		Border(terminalBorder).
		BorderForeground(border).
		Padding(1).
		Width(max(20, innerWidth-2)).
		Render(lipgloss.JoinVertical(lipgloss.Left, welcomeTitle, "", welcomeBody))

	space := max(1, height-lipgloss.Height(header)-lipgloss.Height(welcome)-5)
	blank := strings.Repeat("\n", space)

	prompt := lipgloss.NewStyle().Foreground(accent).Bold(true).Render("› ")
	draft := m.draft
	if draft == "" {
		draft = lipgloss.NewStyle().Foreground(muted).Render("Type a message when chat actions are connected…")
	} else {
		draft = lipgloss.NewStyle().Foreground(text).Render(draft)
	}
	input := lipgloss.NewStyle().
		Border(terminalBorder).
		BorderForeground(border).
		Padding(0, 1).
		Width(max(20, innerWidth-2)).
		Render(lipgloss.JoinHorizontal(lipgloss.Center, prompt, draft))

	return lipgloss.NewStyle().
		Border(terminalBorder).
		BorderForeground(border).
		Padding(1, 1).
		Width(max(20, width-2)).
		Height(height).
		Render(lipgloss.JoinVertical(lipgloss.Left, header, "", welcome, blank, input))
}

func (m Model) sidePanel(width, height int) string {
	section := func(label, value string) string {
		return lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Foreground(muted).Render(label),
			lipgloss.NewStyle().Foreground(text).Render(value),
		)
	}

	chain := lipgloss.JoinVertical(lipgloss.Left,
		chainRow("CURRENT", "— not connected", accent),
		chainLink(),
		chainRow("PREVIOUS", "—", muted),
		chainLink(),
		chainRow("PREVIOUS · 2", "—", muted),
		chainLink(),
		chainRow("PREVIOUS · 3", "—", muted),
	)

	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Foreground(text).Render("Session info"),
		"",
		section("CURRENT AGENT", "Not connected"),
		"",
		section("PREVIOUS AGENT", "None"),
		"",
		section("TOKENS USED", "0"),
		section("CONTEXT ITEMS", "0"),
		section("THREADS", "01"),
		"\n"+lipgloss.NewStyle().Bold(true).Foreground(text).Render("Agent chain"),
		"",
		chain,
		"\n"+lipgloss.NewStyle().Bold(true).Foreground(text).Render("Context"),
		lipgloss.NewStyle().Foreground(muted).Render("No context loaded"),
	)

	return lipgloss.NewStyle().
		Border(terminalBorder).
		BorderForeground(border).
		Padding(1, 1).
		Width(max(20, width-2)).
		Height(height).
		Render(content)
}

func chainRow(label, value string, color lipgloss.Color) string {
	marker := lipgloss.NewStyle().Foreground(color).Render("*")
	name := lipgloss.NewStyle().Foreground(muted).Render(label)
	state := lipgloss.NewStyle().Foreground(color).Render(value)
	return lipgloss.JoinHorizontal(lipgloss.Center, marker, " ", lipgloss.JoinVertical(lipgloss.Left, name, state))
}

func chainLink() string {
	return lipgloss.NewStyle().Foreground(border).Render("  │")
}

func (m Model) footer(width int) string {
	directory := lipgloss.NewStyle().Foreground(muted).Render("DIR ") + lipgloss.NewStyle().Foreground(text).Render(m.path)
	hints := lipgloss.NewStyle().Foreground(muted).Render("tab focus  ·  enter draft  ·  esc quit")
	status := lipgloss.NewStyle().Foreground(muted).Render(m.status)
	if width < 100 {
		return lipgloss.NewStyle().Width(width).Padding(0, 1, 1).Render(
			lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Center, directory, "  ", status), hints),
		)
	}
	spacer := max(0, width-lipgloss.Width(directory)-lipgloss.Width(status)-lipgloss.Width(hints)-8)
	line := lipgloss.JoinHorizontal(lipgloss.Center, directory, "  ", status, strings.Repeat(" ", spacer), hints)
	return lipgloss.NewStyle().Width(width).Padding(0, 1, 1).Render(line)
}

func workingDirectory() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
