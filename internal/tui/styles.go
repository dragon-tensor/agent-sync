package tui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	appStyle = lipgloss.NewStyle().
			Padding(1, 2)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7FDBCA")).
			Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7FDBCA"))

	subtleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8AA39A"))

	accentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7EC8E3"))

	greenStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9FE2BF"))

	redStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f85149"))

	yellowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d29922"))

	tabStyle = lipgloss.NewStyle().
			Padding(0, 3).
			Foreground(lipgloss.Color("#8AA39A"))

	activeTabStyle = lipgloss.NewStyle().
			Padding(0, 3).
			Foreground(lipgloss.Color("#9FE2BF")).
			Bold(true).
			Underline(true)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8AA39A")).
			Padding(0, 1).
			MarginTop(1)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#e6edf3")).
			Background(lipgloss.Color("#121A17")).
			Padding(0, 1)

	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3A9B8E")).
			Padding(1, 2).
			Width(25)

	selectedItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7EC8E3")).
				Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8AA39A")).
			Padding(0, 1)

	dialogBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7FDBCA")).
			Padding(1, 2).
			Width(50)

	inputStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3A9B8E")).
			Padding(0, 1).
			Width(40)

	focusedInputStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#7EC8E3")).
				Padding(0, 1).
				Width(40)

	tabBarStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(lipgloss.Color("#3A9B8E")).
			Padding(0, 1).
			MarginBottom(1)
)

func statusDot(ok bool) string {
	if ok {
		return greenStyle.Render("●")
	}
	return redStyle.Render("●")
}
