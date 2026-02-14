// Package plan provides the plan mode engine for previewing mutating commands.
// Plan mode shows what a command would do without executing it.
package plan

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
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
	fmt.Fprintf(w, "┌%s┐\n", border)
	fmt.Fprintf(w, "│ %-48s │\n", fmt.Sprintf("Plan: %s %s", p.Action, p.Resource))
	fmt.Fprintf(w, "├%s┤\n", border)
	fmt.Fprintf(w, "│ %-48s │\n", fmt.Sprintf("Target: %s", p.Target))

	if len(p.Effects) > 0 {
		fmt.Fprintf(w, "│ %-48s │\n", "Effects:")
		for _, e := range p.Effects {
			fmt.Fprintf(w, "│   %-46s │\n", e)
		}
	}

	rev := "yes"
	if !p.Reversible {
		rev = "no (irreversible)"
	}
	fmt.Fprintf(w, "│ %-48s │\n", fmt.Sprintf("Reversible: %s", rev))
	fmt.Fprintf(w, "└%s┘\n", border)
	fmt.Fprintln(w, "No changes made (plan mode).")
	return nil
}
