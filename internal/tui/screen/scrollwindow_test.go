package screen

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func mkLines(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = strings.Repeat("x", 3)
	}
	return lines
}

// TestWindowLines_CursorAlwaysVisible is the regression for the
// KLA-480 field report: a 40-line policy screen in a short terminal
// rendered the cursor off the top, so space/enter acted invisibly.
func TestWindowLines_CursorAlwaysVisible(t *testing.T) {
	lines := mkLines(40)
	lines[35] = "CURSOR"

	out := windowLines(lines, 35, 20)
	if len(out) > 20 {
		t.Fatalf("window emitted %d lines, budget 20", len(out))
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "CURSOR") {
		t.Fatalf("cursor line clipped out:\n%s", joined)
	}
	if !strings.Contains(joined, "more above") {
		t.Errorf("top clip marker missing")
	}
}

func TestWindowLines_TopOfListNoTopMarker(t *testing.T) {
	lines := mkLines(40)
	lines[0] = "CURSOR"
	out := windowLines(lines, 0, 15)
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "CURSOR") {
		t.Fatal("cursor at top must be visible")
	}
	if strings.Contains(joined, "more above") {
		t.Error("no top marker when nothing is clipped above")
	}
	if !strings.Contains(joined, "more below") {
		t.Error("bottom clip marker missing")
	}
}

func TestWindowLines_FitsUnchanged(t *testing.T) {
	lines := mkLines(10)
	if got := windowLines(lines, 3, 20); len(got) != 10 {
		t.Errorf("fitting content must pass through, got %d lines", len(got))
	}
	// Unknown height with content that fits the default budget: all lines.
	if got := windowLines(lines, 3, 0); len(got) != 10 {
		t.Errorf("unknown height, fitting content: got %d lines", len(got))
	}
}

// TestWindowLines_UnknownHeightTallListWindows is the real field-report
// mechanism: when bubbletea never delivered a WindowSizeMsg (height 0),
// a tall screen must STILL window so the cursor stays visible, rather
// than dumping every line and scrolling the cursor off.
func TestWindowLines_UnknownHeightTallListWindows(t *testing.T) {
	lines := mkLines(40)
	lines[0] = "CURSOR"
	out := windowLines(lines, 0, 0) // 0 = unknown height
	if len(out) > defaultWindowBudget+1 {
		t.Fatalf("unknown height did not window: %d lines", len(out))
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "CURSOR") {
		t.Fatalf("cursor lost under unknown height:\n%s", joined)
	}
	if !strings.Contains(joined, "more below") {
		t.Errorf("clip marker missing under unknown height")
	}
}

func TestWindowLines_LastLineFocus(t *testing.T) {
	lines := mkLines(40)
	lines[39] = "CURSOR"
	out := windowLines(lines, 39, 12)
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "CURSOR") {
		t.Fatalf("cursor at bottom clipped:\n%s", joined)
	}
	if strings.Contains(joined, "more below") {
		t.Error("no bottom marker at end of list")
	}
	if len(out) > 12 {
		t.Errorf("budget exceeded: %d", len(out))
	}
}

// TestPasswordPolicyScreen_ShortTerminalKeepsCursorVisible drives the
// actual reported scenario: a short terminal, cursor moved deep into
// the list — the selected row must appear in View().
func TestPasswordPolicyScreen_ShortTerminalKeepsCursorVisible(t *testing.T) {
	var putBody []byte
	srv := startOrgServer(t, &putBody)
	overridePasswordPolicyClient(t, srv.URL)

	s := loadPasswordPolicyScreen(t)
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 7}) // budget 3: fixture rows + group headers overflow
	// Move to the last row.
	for i := 0; i < len(s.rows); i++ {
		s.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	view := s.View()
	if !strings.Contains(view, "> ") {
		t.Fatalf("selected row not visible in short terminal:\n%s", view)
	}
	if !strings.Contains(view, "more above") {
		t.Errorf("clip marker missing in short terminal:\n%s", view)
	}
	// And the footer help stays on screen.
	if !strings.Contains(view, "Ctrl+S save") {
		t.Errorf("footer clipped:\n%s", view)
	}
}
