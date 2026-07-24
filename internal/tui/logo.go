package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Wave colors: seafoam ↔ light blue. Glyphs stay put; only hues travel.
var dragonWave = []string{
	"#9FE2BF", // seafoam mint
	"#7FDBCA", // seafoam aqua
	"#5EC4B0", // seafoam teal
	"#7EC8E3", // light blue
	"#A8D8EA", // pale light blue
	"#7FDBCA", // seafoam aqua
}

// ansi_shadow "dragon" — CLI block letters (brand is lowercase; font renders solid blocks).
var dragonArt = []string{
	`██████╗ ██████╗  █████╗  ██████╗  ██████╗ ███╗   ██╗`,
	`██╔══██╗██╔══██╗██╔══██╗██╔════╝ ██╔═══██╗████╗  ██║`,
	`██║  ██║██████╔╝███████║██║  ███╗██║   ██║██╔██╗ ██║`,
	`██║  ██║██╔══██╗██╔══██║██║   ██║██║   ██║██║╚██╗██║`,
	`██████╔╝██║  ██║██║  ██║╚██████╔╝╚██████╔╝██║ ╚████║`,
	`╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝ ╚═════╝  ╚═════╝ ╚═╝  ╚═══╝`,
}

// ansi_shadow "SYNC" — same block size as dragon, pure white.
var syncArt = []string{
	`███████╗██╗   ██╗███╗   ██╗ ██████╗`,
	`██╔════╝╚██╗ ██╔╝████╗  ██║██╔════╝`,
	`███████╗ ╚████╔╝ ██╔██╗ ██║██║     `,
	`╚════██║  ╚██╔╝  ██║╚██╗██║██║     `,
	`███████║   ██║   ██║ ╚████║╚██████╗`,
	`╚══════╝   ╚═╝   ╚═╝  ╚═══╝ ╚═════╝`,
}

var (
	logoSyncStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF"))

	logoBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("#3A9B8E")).
			Padding(0, 2)

	logoTaglineStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8AA39A"))
)

// LogoWordmark is the compact chrome mark (tabs / narrow).
// Still CLI-flavored: first row of the block art + white SYNC.
func LogoWordmark(phase int) string {
	top := waveLine(dragonArt[0], phase)
	sync := logoSyncStyle.Render("  SYNC")
	return top + sync
}

// LogoBlock is the canonical splash: block "dragon" (wave) + block "SYNC" (white).
func LogoBlock(phase int) string {
	dragon := colorArt(dragonArt, phase, true)
	sync := colorArt(syncArt, 0, false)
	tagline := logoTaglineStyle.Render("local context · every agent · one store")
	inner := lipgloss.JoinVertical(lipgloss.Center,
		dragon,
		"",
		sync,
		"",
		tagline,
	)
	return logoBoxStyle.Render(inner)
}

func colorArt(lines []string, phase int, wave bool) string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if wave {
			out = append(out, waveLine(line, phase))
		} else {
			out = append(out, logoSyncStyle.Render(line))
		}
	}
	return strings.Join(out, "\n")
}

func waveLine(line string, phase int) string {
	n := len(dragonWave)
	var b strings.Builder
	for i, r := range line {
		if r == ' ' {
			b.WriteRune(r)
			continue
		}
		c := dragonWave[(i+phase)%n]
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render(string(r)))
	}
	return b.String()
}
