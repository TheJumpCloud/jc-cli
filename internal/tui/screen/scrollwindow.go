package screen

import (
	"fmt"
	"strings"

	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// defaultWindowBudget is used when the terminal height is unknown
// (budget <= 0). bubbletea does not always deliver an initial
// WindowSizeMsg before the first key events — most notably on some
// terminals it only arrives on the first resize — so height can be 0
// well past the first render. Returning ALL lines in that case (the
// original design) reproduces the exact field-report bug: on a tall
// screen the cursor row scrolls off and keypresses act invisibly.
// Windowing to a conservative default instead GUARANTEES the cursor
// stays visible; the moment a real WindowSizeMsg arrives it corrects
// to the true height.
const defaultWindowBudget = 20

// windowLines slices pre-rendered body lines so focusLine stays
// visible within budget, appending "… N more" markers on the clipped
// edges. The user-reported failure mode this prevents (KLA-480 field
// report): a screen taller than the terminal renders its cursor row
// off the top, so keypresses appear to do nothing while acting on an
// invisible row.
//
// budget is the number of body lines the screen may use (height minus
// its fixed header/footer chrome). budget <= 0 means unknown height —
// fall back to defaultWindowBudget rather than dumping everything.
func windowLines(lines []string, focusLine, budget int) []string {
	if budget <= 0 {
		budget = defaultWindowBudget
	}
	if len(lines) <= budget {
		return lines
	}
	if focusLine < 0 {
		focusLine = 0
	}
	if focusLine >= len(lines) {
		focusLine = len(lines) - 1
	}

	// Reserve marker lines: clipping above and/or below each consume
	// one line of the budget.
	body := budget
	start := 0
	// Slide the window so the focus line sits inside it, biased to
	// keep context above the cursor when possible.
	if focusLine >= body-1 {
		start = focusLine - body + 2 // leave room for the top marker
	}
	if start > 0 {
		body-- // top marker takes a line
	}
	end := start + body
	if end < len(lines) {
		body-- // bottom marker takes a line
		end = start + body
	}
	if end > len(lines) {
		end = len(lines)
	}
	// Re-check: shrinking for the bottom marker may have pushed the
	// focus line out; slide once more.
	if focusLine >= end {
		start += focusLine - end + 1
		end = focusLine + 1
	}

	var out []string
	if start > 0 {
		out = append(out, style.Subtitle.Render(fmt.Sprintf("  … %d more above", start)))
	}
	out = append(out, lines[start:end]...)
	if end < len(lines) {
		out = append(out, style.Subtitle.Render(fmt.Sprintf("  … %d more below", len(lines)-end)))
	}
	return out
}

// renderWindowed joins windowed body lines for a View. chromeLines is
// the fixed line count the caller renders around the body (headers +
// footers + blanks); the body gets whatever remains of the height.
func renderWindowed(lines []string, focusLine, height, chromeLines int) string {
	return strings.Join(windowLines(lines, focusLine, height-chromeLines), "\n")
}
