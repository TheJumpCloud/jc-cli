package component

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// sparkBlocks are the Unicode block characters used for sparkline bars,
// ordered from lowest to highest.
var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// Sparkline renders a sparkline chart from a series of data points.
type Sparkline struct {
	Title  string
	Data   []int
	Labels []string // optional x-axis labels (e.g. day names)
	Color  lipgloss.Color
	Width  int
}

// View renders the sparkline.
func (s Sparkline) View() string {
	if len(s.Data) == 0 {
		return ""
	}

	color := s.Color
	if color == "" {
		color = style.ColorSecondary
	}

	var sb strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(style.ColorSecondary)
	sb.WriteString(titleStyle.Render(s.Title))
	sb.WriteString("\n")

	// Find max for scaling.
	maxVal := 0
	for _, v := range s.Data {
		if v > maxVal {
			maxVal = v
		}
	}

	// Build sparkline string — each data point rendered as a repeated block
	// to match label width (label + 1 space).
	labelWidth := 3 // default: 2-char label + 1 space
	if len(s.Labels) > 0 && len(s.Labels[0]) > 0 {
		labelWidth = len(s.Labels[0]) + 1
	}

	sparkStyle := lipgloss.NewStyle().Foreground(color)
	var spark strings.Builder
	for _, v := range s.Data {
		idx := 0
		if maxVal > 0 {
			idx = (v * (len(sparkBlocks) - 1)) / maxVal
		}
		spark.WriteString(strings.Repeat(string(sparkBlocks[idx]), labelWidth))
	}
	sb.WriteString(sparkStyle.Render(spark.String()))
	sb.WriteString("\n")

	// Labels row.
	if len(s.Labels) > 0 {
		dimStyle := lipgloss.NewStyle().Foreground(style.ColorDimText)
		var labelRow strings.Builder
		for _, l := range s.Labels {
			labelRow.WriteString(fmt.Sprintf("%-*s", labelWidth, l))
		}
		sb.WriteString(dimStyle.Render(labelRow.String()))
		sb.WriteString("\n")
	}

	// Min/max annotation.
	dimStyle := lipgloss.NewStyle().Foreground(style.ColorDimText)
	minVal := s.Data[0]
	for _, v := range s.Data {
		if v < minVal {
			minVal = v
		}
	}
	sb.WriteString(dimStyle.Render(fmt.Sprintf("min:%d  max:%d", minVal, maxVal)))
	sb.WriteString("\n")

	return sb.String()
}
