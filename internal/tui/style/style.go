// Package style provides lipgloss styles and colors for the TUI.
// This is a leaf package imported by both tui/ and its sub-packages
// to avoid circular imports.
package style

import "github.com/charmbracelet/lipgloss"

// Colors used throughout the TUI.
var (
	ColorPrimary   = lipgloss.Color("#7C3AED") // Purple
	ColorSecondary = lipgloss.Color("#06B6D4") // Cyan
	ColorSuccess   = lipgloss.Color("#22C55E") // Green
	ColorWarning   = lipgloss.Color("#F59E0B") // Amber
	ColorError     = lipgloss.Color("#EF4444") // Red
	ColorMuted     = lipgloss.Color("#6B7280") // Gray
	ColorBorder    = lipgloss.Color("#374151") // Dark gray
	ColorHighlight = lipgloss.Color("#A78BFA") // Light purple
	ColorText      = lipgloss.Color("#F9FAFB") // Near-white
	ColorDimText   = lipgloss.Color("#9CA3AF") // Light gray
)

// Styles for various UI elements.
var (
	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		MarginBottom(1)

	Subtitle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true)

	TableHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorSecondary).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(ColorBorder)

	SelectedRow = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorText).
			Background(lipgloss.Color("#4C1D95"))

	NormalRow = lipgloss.NewStyle().
			Foreground(ColorText)

	DimRow = lipgloss.NewStyle().
		Foreground(ColorDimText)

	FilterInput = lipgloss.NewStyle().
			Foreground(ColorText).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary).
			Padding(0, 1)

	FilterChip = lipgloss.NewStyle().
			Foreground(ColorText).
			Background(lipgloss.Color("#4C1D95")).
			Padding(0, 1).
			MarginRight(1)

	Breadcrumb = lipgloss.NewStyle().
			Foreground(ColorDimText)

	BreadcrumbActive = lipgloss.NewStyle().
				Foreground(ColorHighlight).
				Bold(true)

	StatusBar = lipgloss.NewStyle().
			Foreground(ColorDimText).
			BorderTop(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(ColorBorder)

	Help = lipgloss.NewStyle().
		Foreground(ColorMuted)

	Error = lipgloss.NewStyle().
		Foreground(ColorError).
		Bold(true)

	Success = lipgloss.NewStyle().
		Foreground(ColorSuccess)

	Category = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true).
			MarginTop(1)

	ResourceName = lipgloss.NewStyle().
			Foreground(ColorText)

	ResourceVerbs = lipgloss.NewStyle().
			Foreground(ColorMuted)

	FieldKey = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true)

	FieldValue = lipgloss.NewStyle().
			Foreground(ColorText)

	SortIndicator = lipgloss.NewStyle().
			Foreground(ColorWarning)

	Spinner = lipgloss.NewStyle().
		Foreground(ColorPrimary)

	SectionHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(ColorBorder).
			MarginTop(1)
)
