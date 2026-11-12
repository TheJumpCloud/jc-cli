// Package plan provides the plan mode engine for previewing mutating commands.
// Plan mode shows what a command would do without executing it.
package plan

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// ExitCodePlan is the exit code for plan mode (no changes made).
const ExitCodePlan = 10

// Plan describes a pending mutation that would be performed.
type Plan struct {
	Action     string   `json:"action"`
	Resource   string   `json:"resource"`
	Target     string   `json:"target"`
	Effects    []string `json:"effects"`
	Reversible bool     `json:"reversible"`
}

// Render writes the plan to w in the specified format.
// JSON format produces structured output; all other formats use human-readable rendering.
func (p *Plan) Render(w io.Writer, format string) error {
	if format == "json" {
		return p.RenderJSON(w)
	}
	return p.RenderHuman(w)
}

// RenderJSON writes the plan as structured JSON.
func (p *Plan) RenderJSON(w io.Writer) error {
	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}

// RenderHuman writes a visual text plan to w.
func (p *Plan) RenderHuman(w io.Writer) error {
	border := strings.Repeat("─", 50)
	line := func(indent string, width int, text string) {
		for _, l := range wrap(text, width, width) {
			fmt.Fprintf(w, "│ %s%-*s │\n", indent, width, l)
		}
	}
	fmt.Fprintf(w, "┌%s┐\n", border)
	line("", 48, fmt.Sprintf("Plan: %s %s", p.Action, p.Resource))
	fmt.Fprintf(w, "├%s┤\n", border)
	line("", 48, fmt.Sprintf("Target: %s", p.Target))

	if len(p.Effects) > 0 {
		line("", 48, "Effects:")
		for _, e := range p.Effects {
			// Continuation lines sit two spaces further in, under the effect text.
			for i, l := range wrap(e, 46, 44) {
				if i > 0 {
					l = "  " + l
				}
				fmt.Fprintf(w, "│   %-46s │\n", l)
			}
		}
	}

	rev := "yes"
	if !p.Reversible {
		rev = "no (irreversible)"
	}
	line("", 48, fmt.Sprintf("Reversible: %s", rev))
	fmt.Fprintf(w, "└%s┘\n", border)
	fmt.Fprintln(w, "No changes made (plan mode).")
	return nil
}

// wrap word-wraps text to first runes on the first line and rest runes on
// later lines. Widths are measured in runes. A word longer than a whole line
// is hard-split. Text that already fits is returned unchanged.
func wrap(text string, first, rest int) []string {
	if utf8.RuneCountInString(text) <= first {
		return []string{text}
	}
	var lines []string
	var cur []rune
	width := first
	flush := func() {
		lines = append(lines, string(cur))
		cur = cur[:0]
		width = rest
	}
	for _, word := range strings.Fields(text) {
		wr := []rune(word)
		if len(cur) > 0 && len(cur)+1+len(wr) <= width {
			cur = append(cur, ' ')
			cur = append(cur, wr...)
			continue
		}
		if len(cur) > 0 {
			flush()
		}
		for len(wr) > width {
			cur = append(cur, wr[:width]...)
			wr = wr[width:]
			flush()
		}
		cur = append(cur, wr...)
	}
	if len(cur) > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}
