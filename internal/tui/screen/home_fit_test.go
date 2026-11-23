package screen

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/TheJumpCloud/jc-cli/internal/tui"
)

// selectedLine returns the one view line holding the selected entry, failing
// when there is none or more than one.
func selectedLine(t *testing.T, view, name string) string {
	t.Helper()
	var found []string
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, "> "+name) {
			found = append(found, l)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one line with the selected %q, got %d:\n%s", name, len(found), view)
	}
	return found[0]
}

// On a short terminal the home grid scrolls instead of overflowing: the
// title stays, the view fits above the status bar, the selected entry is on
// screen, and the "more" markers take lines of their own instead of covering
// one (which used to hide the cursor at small heights).
func TestHomeFitsShortTerminal(t *testing.T) {
	for _, tc := range []struct {
		name          string
		height, downs int
		bookmarks     []string
		wantAbove     bool
	}{
		{name: "height 20, scrolled", height: 20, downs: 12, wantAbove: true},
		{name: "height 20, top", height: 20},
		{name: "height 5", height: 5, downs: 7, wantAbove: true},
		{name: "height 6, every row", height: 6, downs: 30, wantAbove: true},
		{name: "unknown height", height: 0, downs: 12, wantAbove: true},
		{name: "cursor in bookmarks", height: 12, bookmarks: []string{"users", "devices"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			overrideBookmarks(t, tc.bookmarks)
			h := NewHomeScreen(tui.BuildRegistry())
			if tc.height > 0 {
				h.Update(tea.WindowSizeMsg{Width: 140, Height: tc.height})
			}
			for i := 0; i < tc.downs; i++ {
				h.Update(tea.KeyMsg{Type: tea.KeyDown})
			}
			var sel *tui.ResourceEntry
			if h.inBookmarks {
				bm := h.bookmarkedEntries()
				sel = &bm[h.cursor]
			} else {
				sel = h.currentGridEntry()
			}
			if sel == nil {
				t.Fatal("no selected entry")
			}

			view := h.View()
			lines := strings.Split(view, "\n")
			limit := tc.height - appChromeReserve
			if tc.height <= 0 {
				limit = 1 + defaultWindowBudget
			}
			if tc.height > 0 && limit < 1+minWindowBudget {
				limit = 1 + minWindowBudget
			}
			if len(lines) > limit {
				t.Errorf("view is %d lines, more than the %d available", len(lines), limit)
			}
			if !strings.Contains(lines[0], "JumpCloud TUI") {
				t.Errorf("title scrolled away: %q", lines[0])
			}
			line := selectedLine(t, view, sel.DisplayName)
			if strings.Contains(line, "more above") || strings.Contains(line, "more below") {
				t.Errorf("a marker shares the selected line: %q", line)
			}
			if got := strings.Contains(view, "more above"); got != tc.wantAbove {
				t.Errorf("'more above' shown=%v, want %v:\n%s", got, tc.wantAbove, view)
			}
		})
	}
}

// Moving through every entry keeps the cursor on screen at a small height.
func TestHomeCursorAlwaysVisible(t *testing.T) {
	overrideBookmarks(t, nil)
	h := NewHomeScreen(tui.BuildRegistry())
	h.Update(tea.WindowSizeMsg{Width: 140, Height: 7})
	for i := 0; i < 40; i++ {
		sel := h.currentGridEntry()
		selectedLine(t, h.View(), sel.DisplayName)
		h.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
}
