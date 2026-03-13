package component

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// StatCard renders a bordered card with a label and a value.
type StatCard struct {
	Label    string
	Value    string
	Color    lipgloss.Color
	Width    int
	Selected bool
}

// View renders the stat card as a bordered box.
func (s StatCard) View() string {
	w := s.Width
	if w <= 0 {
		w = 20
	}

	color := s.Color
	if color == "" {
		color = style.ColorPrimary
	}

	labelStyle := lipgloss.NewStyle().
		Foreground(style.ColorDimText)

	valueStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(color)

	content := labelStyle.Render(s.Label) + "\n" + valueStyle.Render(s.Value)

	borderStyle := lipgloss.RoundedBorder()
	if s.Selected {
		borderStyle = lipgloss.DoubleBorder()
	}

	box := lipgloss.NewStyle().
		BorderStyle(borderStyle).
		BorderForeground(color).
		Padding(0, 1).
		Width(w - 2). // account for border
		Render(content)

	return box
}
