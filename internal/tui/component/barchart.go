package component

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// BarItem represents a single bar in a horizontal bar chart.
type BarItem struct {
	Label string
	Value int
	Color lipgloss.Color
}

// BarChart renders a horizontal bar chart with labels and counts.
type BarChart struct {
	Title   string
	Items   []BarItem
	Width   int
	Focused bool
}

// View renders the bar chart.
func (b BarChart) View() string {
	if len(b.Items) == 0 {
		return ""
	}

	w := b.Width
	if w <= 0 {
		w = 30
	}

	var sb strings.Builder

	// Title
	titleColor := style.ColorSecondary
	if b.Focused {
		titleColor = style.ColorHighlight
	}
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(titleColor)
	focusMarker := ""
	if b.Focused {
		focusMarker = "▸ "
	}
	sb.WriteString(titleStyle.Render(focusMarker + b.Title))
	sb.WriteString("\n")

	// Find max value for scaling and max label width for alignment.
	maxVal := 0
	maxLabelWidth := 0
	for _, item := range b.Items {
		if item.Value > maxVal {
			maxVal = item.Value
		}
		if len(item.Label) > maxLabelWidth {
			maxLabelWidth = len(item.Label)
		}
	}

	// Bar width = total width - label - spacing - count display
	countWidth := len(fmt.Sprintf("%d", maxVal))
	barWidth := w - maxLabelWidth - countWidth - 4 // 4 = spaces around bar
	if barWidth < 5 {
		barWidth = 5
	}

	for _, item := range b.Items {
		color := item.Color
		if color == "" {
			color = style.ColorPrimary
		}

		label := fmt.Sprintf("%-*s", maxLabelWidth, item.Label)
		labelStyle := lipgloss.NewStyle().Foreground(style.ColorDimText)

		// Calculate filled/empty portions.
		filled := 0
		if maxVal > 0 {
			filled = (item.Value * barWidth) / maxVal
		}
		empty := barWidth - filled

		filledStyle := lipgloss.NewStyle().Foreground(color)
		emptyStyle := lipgloss.NewStyle().Foreground(style.ColorBorder)

		bar := filledStyle.Render(strings.Repeat("█", filled)) +
			emptyStyle.Render(strings.Repeat("░", empty))

		count := fmt.Sprintf("%*d", countWidth, item.Value)
		countStyle := lipgloss.NewStyle().Foreground(style.ColorText)

		sb.WriteString(labelStyle.Render(label) + " " + bar + " " + countStyle.Render(count) + "\n")
	}

	return sb.String()
}
