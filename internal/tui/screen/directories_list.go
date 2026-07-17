package screen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// newV2ClientForDirectories is overridable for tests.
var newV2ClientForDirectories = api.NewV2Client

// DirectoriesListScreen is the unified directory-integrations view
// (KLA-479): every integration type in one list with OAuth health —
// a broken grant (expired/revoked consent) shows red here long before
// anyone notices sync failures.
type DirectoriesListScreen struct {
	rows    []directoryRow
	cursor  int
	loading bool
	err     string
	spinner spinner.Model

	width, height int
}

// directoryRow is one integration, with health pre-derived.
type directoryRow struct {
	ID     string
	Name   string
	Type   string
	Health string // "ok" or "error: <code> — <message>"
	// Expiry is the OAuth token expiry (unix seconds as string on the
	// wire) — informational only when present.
	Raw map[string]any
}

// loadDirectoriesMsg carries the fetch result.
type loadDirectoriesMsg struct {
	rows []directoryRow
	err  error
}

func NewDirectoriesListScreen() *DirectoriesListScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner
	return &DirectoriesListScreen{spinner: sp, loading: true}
}

func (s *DirectoriesListScreen) Title() string { return "Directories" }

func (s *DirectoriesListScreen) Init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, s.loadCmd())
}

func (s *DirectoriesListScreen) loadCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := newV2ClientForDirectories()
		if err != nil {
			return loadDirectoriesMsg{err: fmt.Errorf("building v2 client: %w", err)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := client.ListAll(ctx, "/directories", api.V2ListOptions{})
		if err != nil {
			return loadDirectoriesMsg{err: err}
		}
		rows := make([]directoryRow, 0, len(result.Data))
		for _, raw := range result.Data {
			var obj map[string]any
			if err := json.Unmarshal(raw, &obj); err != nil {
				continue
			}
			name, _ := obj["name"].(string)
			id, _ := obj["id"].(string)
			typ, _ := obj["type"].(string)
			rows = append(rows, directoryRow{
				ID: id, Name: name, Type: typ,
				Health: directoryRowHealth(obj),
				Raw:    obj,
			})
		}
		return loadDirectoriesMsg{rows: rows}
	}
}

// directoryRowHealth mirrors the CLI's health derivation (see
// internal/cmd/directories.go): "ok" unless oAuthStatus carries an
// error.
func directoryRowHealth(obj map[string]any) string {
	status, ok := obj["oAuthStatus"].(map[string]any)
	if !ok {
		return "ok"
	}
	errCode, _ := status["error"].(string)
	if errCode == "" {
		return "ok"
	}
	if msg, _ := status["errorMessage"].(string); msg != "" {
		const maxMsg = 60
		if len(msg) > maxMsg {
			msg = msg[:maxMsg] + "…"
		}
		return "error: " + errCode + " — " + msg
	}
	return "error: " + errCode
}

func (s *DirectoriesListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		return s, nil
	case spinner.TickMsg:
		if s.loading {
			var cmd tea.Cmd
			s.spinner, cmd = s.spinner.Update(m)
			return s, cmd
		}
		return s, nil
	case loadDirectoriesMsg:
		s.loading = false
		if m.err != nil {
			s.err = m.err.Error()
			return s, nil
		}
		s.rows = m.rows
		return s, nil
	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		case "r":
			if !s.loading {
				s.loading, s.err = true, ""
				return s, tea.Batch(s.spinner.Tick, s.loadCmd())
			}
		case "up", "k":
			if s.cursor > 0 {
				s.cursor--
			}
		case "down", "j":
			if s.cursor < len(s.rows)-1 {
				s.cursor++
			}
		case "enter":
			if s.cursor < len(s.rows) {
				row := s.rows[s.cursor]
				return s, func() tea.Msg {
					return tui.PushScreenMsg{Screen: NewDirectoryDetailScreen(row)}
				}
			}
		}
	}
	return s, nil
}

func (s *DirectoriesListScreen) View() string {
	var b strings.Builder
	switch {
	case s.loading:
		fmt.Fprintln(&b, s.spinner.View()+" Loading directory integrations...")
	case s.err != "":
		fmt.Fprintln(&b, style.Error.Render("Error: "+s.err))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("r retry · Esc back"))
	default:
		broken := 0
		for _, r := range s.rows {
			if r.Health != "ok" {
				broken++
			}
		}
		summary := fmt.Sprintf("%d integrations", len(s.rows))
		if broken > 0 {
			summary += fmt.Sprintf(" — %d with OAuth errors", broken)
		}
		fmt.Fprintln(&b, style.Subtitle.Render(summary))
		fmt.Fprintln(&b)
		fmt.Fprintf(&b, "  %-28s %-18s %s\n", "NAME", "TYPE", "HEALTH")
		var lines []string
		for i, r := range s.rows {
			health := style.Success.Render("ok")
			if r.Health != "ok" {
				health = style.Error.Render(r.Health)
			}
			line := fmt.Sprintf("%-28s %-18s ", truncTUI(r.Name, 28), r.Type)
			if i == s.cursor {
				lines = append(lines, style.SelectedRow.Render("> "+line)+health)
			} else {
				lines = append(lines, "  "+line+health)
			}
		}
		fmt.Fprintln(&b, renderWindowed(lines, s.cursor, s.height, 5))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, style.Subtitle.Render("Enter detail · r refresh · Esc back"))
	}
	return b.String()
}

// truncTUI clips a string to width with an ellipsis.
func truncTUI(s string, width int) string {
	if len(s) <= width {
		return s
	}
	if width <= 1 {
		return s[:width]
	}
	return s[:width-1] + "…"
}

// DirectoryDetailScreen renders one integration in full — every field
// the list response carries, incl. the complete OAuth error text.
type DirectoryDetailScreen struct {
	row           directoryRow
	width, height int
}

func NewDirectoryDetailScreen(row directoryRow) *DirectoryDetailScreen {
	return &DirectoryDetailScreen{row: row}
}

func (s *DirectoryDetailScreen) Title() string { return "Directory: " + s.row.Name }

func (s *DirectoryDetailScreen) Init() tea.Cmd { return nil }

func (s *DirectoryDetailScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		return s, nil
	case tea.KeyMsg:
		if m.String() == "esc" {
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		}
	}
	return s, nil
}

func (s *DirectoryDetailScreen) View() string {
	var b strings.Builder
	fmt.Fprintln(&b, style.Subtitle.Render(fmt.Sprintf("%s (%s)", s.row.Name, s.row.Type)))
	fmt.Fprintln(&b)
	if s.row.Health == "ok" {
		fmt.Fprintln(&b, "  Health: "+style.Success.Render("ok"))
	} else {
		fmt.Fprintln(&b, "  Health: "+style.Error.Render(s.row.Health))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "  Fix: re-authorize this integration in the Admin Portal")
		fmt.Fprintln(&b, "  (Directory Integrations → "+s.row.Name+").")
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, style.SectionHeader.Render("Raw fields"))
	pretty, err := json.MarshalIndent(s.row.Raw, "  ", "  ")
	if err == nil {
		fmt.Fprintln(&b, "  "+string(pretty))
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, style.Subtitle.Render("Esc back"))
	return b.String()
}
