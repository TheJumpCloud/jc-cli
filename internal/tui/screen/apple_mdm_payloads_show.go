package screen

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// AppleMDMPayloadsShowScreen renders the schema for one payload in a
// scrollable viewport. Required keys appear first so the operator can
// see at a glance what must be supplied; optional keys follow with
// declared defaults and constraints.
type AppleMDMPayloadsShowScreen struct {
	payload  apple_mdm.Payload
	viewport viewport.Model
	ready    bool
	width    int
	height   int
}

// NewAppleMDMPayloadsShowScreen wraps a parsed Payload in a viewport.
func NewAppleMDMPayloadsShowScreen(p apple_mdm.Payload) *AppleMDMPayloadsShowScreen {
	return &AppleMDMPayloadsShowScreen{payload: p}
}

func (s *AppleMDMPayloadsShowScreen) Title() string {
	if s.payload.Title != "" {
		return s.payload.Title + " — " + s.payload.Type
	}
	return s.payload.Type
}

func (s *AppleMDMPayloadsShowScreen) Init() tea.Cmd { return nil }

func (s *AppleMDMPayloadsShowScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = m.Width
		s.height = m.Height
		// 3 rows for the chrome (title + breadcrumbs + bottom hint).
		const chrome = 3
		body := s.renderBody()
		if !s.ready {
			s.viewport = viewport.New(m.Width, m.Height-chrome)
			s.viewport.SetContent(body)
			s.ready = true
		} else {
			s.viewport.Width = m.Width
			s.viewport.Height = m.Height - chrome
			s.viewport.SetContent(body)
		}
		return s, nil
	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		case "n":
			// Hand off to the editor + create-policy flow.
			// Stubbed in PR4 (KLA-453); will route to a `prompt for
			// policy name` screen → $EDITOR handoff in a follow-up.
			return s, func() tea.Msg {
				return tui.FlashMsg{Text: "Editor handoff coming in the next milestone — use `jc apple-mdm payloads create-policy` from the CLI for now."}
			}
		}
		var cmd tea.Cmd
		s.viewport, cmd = s.viewport.Update(msg)
		return s, cmd
	}
	return s, nil
}

// renderBody assembles the detail block as a single string the
// viewport scrolls over. Layout matches `jc apple-mdm payloads show`
// (the CLI) so the TUI and CLI stay aligned visually.
func (s *AppleMDMPayloadsShowScreen) renderBody() string {
	var b strings.Builder

	// Header — payload identity + prose description.
	fmt.Fprintf(&b, "%s\n", style.ResourceName.Render(s.payload.Type))
	if s.payload.Title != "" {
		fmt.Fprintf(&b, "  %s\n", s.payload.Title)
	}
	if s.payload.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", s.payload.Description)
	}

	// Supported-OS table.
	fmt.Fprint(&b, "\n", style.SectionHeader.Render("Supported platforms"), "\n")
	if len(s.payload.SupportedOS) == 0 {
		fmt.Fprintln(&b, "  (none declared)")
	} else {
		fmt.Fprintln(&b, "  "+style.TableHeader.Render(
			fmt.Sprintf("%-10s  %-12s  %-8s  %-14s  %-12s  %-11s  %s",
				"PLATFORM", "INTRODUCED", "MULTIPLE", "DEVICECHANNEL", "USERCHANNEL", "SUPERVISED", "REQUIRESDEP")))
		for _, plat := range appleListPlatforms {
			sup, ok := s.payload.SupportedOS[plat]
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "  %-10s  %-12s  %-8s  %-14s  %-12s  %-11s  %s\n",
				plat, defaultStrTUI(sup.Introduced, "—"),
				ynBoolTUI(sup.Multiple), ynBoolTUI(sup.DeviceChannel), ynBoolTUI(sup.UserChannel),
				ynBoolTUI(sup.Supervised), ynBoolTUI(sup.RequiresDEP))
		}
	}

	required, optional, other := groupKeysByPresenceTUI(s.payload.Keys)
	if len(required) > 0 {
		fmt.Fprint(&b, "\n", style.SectionHeader.Render("Required keys"), "\n")
		writeKeyTableTUI(&b, required)
	}
	if len(optional) > 0 {
		fmt.Fprint(&b, "\n", style.SectionHeader.Render("Optional keys"), "\n")
		writeKeyTableTUI(&b, optional)
	}
	if len(other) > 0 {
		fmt.Fprint(&b, "\n", style.SectionHeader.Render("Other keys"), "\n")
		writeKeyTableTUI(&b, other)
	}

	return b.String()
}

func writeKeyTableTUI(b *strings.Builder, keys []apple_mdm.Key) {
	fmt.Fprintln(b, "  "+style.TableHeader.Render(
		fmt.Sprintf("%-32s  %-12s  %-10s  %-30s  %s",
			"KEY", "TYPE", "DEFAULT", "CONSTRAINTS", "DESCRIPTION")))
	for _, k := range keys {
		fmt.Fprintf(b, "  %-32s  %-12s  %-10s  %-30s  %s\n",
			truncateTUI(k.Name, 32),
			defaultStrTUI(k.Type, "—"),
			truncateTUI(formatDefaultTUI(k.Default), 10),
			truncateTUI(formatConstraintsTUI(k), 30),
			truncateTUI(firstLineTUI(k.Content), 70))
	}
}

func (s *AppleMDMPayloadsShowScreen) View() string {
	if !s.ready {
		return "Loading…"
	}
	hint := style.Subtitle.Render("j/k scroll · n create policy · Esc back")
	return s.viewport.View() + "\n" + hint
}

// ── Small helpers (mirror the CLI's version in internal/cmd/apple_mdm_payloads.go).
// Duplicated rather than imported to keep the TUI package free of cmd-package deps.

func groupKeysByPresenceTUI(keys []apple_mdm.Key) (required, optional, other []apple_mdm.Key) {
	for _, k := range keys {
		switch strings.ToLower(k.Presence) {
		case "required":
			required = append(required, k)
		case "optional", "":
			optional = append(optional, k)
		default:
			other = append(other, k)
		}
	}
	return
}

func ynBoolTUI(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func defaultStrTUI(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func formatDefaultTUI(v any) string {
	if v == nil {
		return "—"
	}
	switch x := v.(type) {
	case string:
		if x == "" {
			return `""`
		}
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func formatConstraintsTUI(k apple_mdm.Key) string {
	var parts []string
	if k.ValueType != "" {
		parts = append(parts, k.ValueType)
	}
	if k.Range != nil {
		parts = append(parts, fmt.Sprintf("range [%v..%v]", k.Range.Min, k.Range.Max))
	}
	if len(k.RangeList) > 0 {
		vals := make([]string, 0, len(k.RangeList))
		for _, v := range k.RangeList {
			vals = append(vals, fmt.Sprintf("%v", v))
		}
		parts = append(parts, "enum{"+strings.Join(vals, ",")+"}")
	}
	if len(k.Subkeys) > 0 {
		parts = append(parts, fmt.Sprintf("nested(%d)", len(k.Subkeys)))
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " · ")
}

func firstLineTUI(s string) string {
	if s == "" {
		return "—"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
