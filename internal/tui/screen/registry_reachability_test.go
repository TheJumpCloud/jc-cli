package screen

import (
	"testing"

	"github.com/TheJumpCloud/jc-cli/internal/tui"
)

// Every leaf on the home screen must be reachable: it lists through an
// endpoint or its own ListFunc, or it opens a bespoke screen. An entry with
// none of the three opens the generic list with nothing to fetch and shows
// "No results" on an org that has data — which is how eight areas (agents,
// access certification, Password Vault, ...) shipped broken while the schema
// parity test passed.
func TestEveryHomeEntryHasAReadPath(t *testing.T) {
	h := NewHomeScreen(tui.BuildRegistry())
	var walk func([]tui.ResourceEntry)
	walk = func(es []tui.ResourceEntry) {
		for _, e := range es {
			if len(e.SubMenu) > 0 {
				walk(e.SubMenu)
				continue
			}
			if e.ListEndpoint != "" || e.ListFunc != nil {
				continue
			}
			cmd := h.openEntry(e)
			if cmd == nil {
				t.Errorf("%s (%s): no list path and opens nothing", e.Key, e.DisplayName)
				continue
			}
			msg, ok := cmd().(tui.PushScreenMsg)
			if !ok {
				t.Errorf("%s: opening it pushes no screen", e.Key)
				continue
			}
			if _, generic := msg.Screen.(*ListScreen); generic {
				t.Errorf("%s (%s): no list endpoint or ListFunc, yet it opens the generic list, "+
					"which will show \"No results\"", e.Key, e.DisplayName)
			}
		}
	}
	walk(tui.BuildRegistry())
}

// No home entry may show a raw schema key as its name.
func TestHomeEntriesHaveDisplayNames(t *testing.T) {
	for _, e := range tui.BuildRegistry() {
		if e.DisplayName == e.Key && e.DisplayName != "" && e.DisplayName[0] >= 'a' && e.DisplayName[0] <= 'z' {
			t.Errorf("%s has no display name", e.Key)
		}
	}
}
