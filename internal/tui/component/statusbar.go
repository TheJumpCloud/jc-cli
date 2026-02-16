package component

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// StatusBar renders breadcrumbs, help hints, and loading state.
type StatusBar struct {
	Breadcrumbs []string
	Help        string
	Loading     bool
	Width       int
}

// View renders the status bar.
func (s StatusBar) View() string {
	if s.Width <= 0 {
		return ""
	}

	// Left: breadcrumbs.
	var crumbs strings.Builder
	for i, b := range s.Breadcrumbs {
		if i > 0 {
			crumbs.WriteString(style.Breadcrumb.Render(" > "))
		}
		if i == len(s.Breadcrumbs)-1 {
			crumbs.WriteString(style.BreadcrumbActive.Render(b))
		} else {
			crumbs.WriteString(style.Breadcrumb.Render(b))
		}
	}

	// Right: help or loading.
	right := s.Help
	if s.Loading {
		right = style.Spinner.Render("Loading...")
	}

	left := crumbs.String()
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)

	gap := s.Width - leftWidth - rightWidth
	if gap < 1 {
		gap = 1
	}

	bar := left + strings.Repeat(" ", gap) + right

	return style.StatusBar.Width(s.Width).Render(bar)
}
