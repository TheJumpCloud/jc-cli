package component

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// ProgressRing renders a single-metric progress bar with percentage.
type ProgressRing struct {
	Title   string
	Current int
	Total   int
	Color   lipgloss.Color
	Width   int
}

// View renders the progress indicator.
func (p ProgressRing) View() string {
	w := p.Width
	if w <= 0 {
		w = 30
	}

	color := p.Color
	if color == "" {
		color = style.ColorSuccess
	}

	var sb strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(style.ColorSecondary)
	sb.WriteString(titleStyle.Render(p.Title))
	sb.WriteString("\n")

	// Percentage line
	pct := 0
	if p.Total > 0 {
		pct = (p.Current * 100) / p.Total
	}

	pctStyle := lipgloss.NewStyle().Bold(true).Foreground(color)
	dimStyle := lipgloss.NewStyle().Foreground(style.ColorDimText)
	sb.WriteString(pctStyle.Render(fmt.Sprintf("◉ %d%%", pct)))
	sb.WriteString(dimStyle.Render(fmt.Sprintf(" enrolled")))
	sb.WriteString("\n")

	// Progress bar
	barWidth := w - 8 // space for "[", "]", " X/Y"
	if barWidth < 5 {
		barWidth = 5
	}

	filled := 0
	if p.Total > 0 {
		filled = (p.Current * barWidth) / p.Total
	}
	empty := barWidth - filled

	filledStyle := lipgloss.NewStyle().Foreground(color)
	emptyStyle := lipgloss.NewStyle().Foreground(style.ColorBorder)

	bar := "[" +
		filledStyle.Render(strings.Repeat("█", filled)) +
		emptyStyle.Render(strings.Repeat("░", empty)) +
		"]"

	ratio := fmt.Sprintf(" %d/%d", p.Current, p.Total)
	sb.WriteString(bar + dimStyle.Render(ratio) + "\n")

	return sb.String()
}
