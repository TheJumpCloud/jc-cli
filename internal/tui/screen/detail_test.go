package screen

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/schema"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/fetch"
)

func testUserEntry() tui.ResourceEntry {
	return tui.ResourceEntry{
		Key:             "users",
		DisplayName:     "Users",
		Category:        tui.CategoryIdentity,
		ClientType:      tui.ClientV1,
		ListEndpoint:    "/systemusers",
		GraphSourceType: "user",
		Schema:          schema.Resources["users"],
	}
}

func testPolicyEntry() tui.ResourceEntry {
	return tui.ResourceEntry{
		Key:          "policies",
		DisplayName:  "Policies",
		Category:     tui.CategoryManagement,
		ClientType:   tui.ClientV2,
		ListEndpoint: "/policies",
		// No GraphSourceType — policies don't have associations.
		Schema: schema.Resources["policies"],
	}
}

func TestDetailScreen_Title(t *testing.T) {
	d := NewDetailScreen(testUserEntry(), "abc123", "John Doe")
	if d.Title() != "John Doe" {
		t.Errorf("Title = %q, want 'John Doe'", d.Title())
	}
}

func TestDetailScreen_TitleFallsBackToID(t *testing.T) {
	d := NewDetailScreen(testUserEntry(), "abc123", "")
	if d.Title() != "abc123" {
		t.Errorf("Title = %q, want 'abc123'", d.Title())
	}
}

func TestDetailScreen_HasAssocTargets(t *testing.T) {
	d := NewDetailScreen(testUserEntry(), "abc123", "John")
	if len(d.assocTargets) == 0 {
		t.Error("user entry should have association targets")
	}
}

func TestDetailScreen_NoAssocForPolicies(t *testing.T) {
	d := NewDetailScreen(testPolicyEntry(), "abc123", "My Policy")
	if len(d.assocTargets) != 0 {
		t.Errorf("policy entry should have no association targets, got %d", len(d.assocTargets))
	}
}

func TestDetailScreen_TabToggles(t *testing.T) {
	d := NewDetailScreen(testUserEntry(), "abc123", "John")
	d.data = json.RawMessage(`{"_id":"abc123","username":"john"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	if d.activeTab != 0 {
		t.Errorf("initial activeTab = %d, want 0", d.activeTab)
	}

	// Tab to associations.
	d.Update(tea.KeyMsg{Type: tea.KeyTab})
	if d.activeTab != 1 {
		t.Errorf("activeTab after Tab = %d, want 1", d.activeTab)
	}

	// Tab back to fields.
	d.Update(tea.KeyMsg{Type: tea.KeyTab})
	if d.activeTab != 0 {
		t.Errorf("activeTab after second Tab = %d, want 0", d.activeTab)
	}
}

func TestDetailScreen_TabIgnoredWithoutAssoc(t *testing.T) {
	d := NewDetailScreen(testPolicyEntry(), "abc123", "My Policy")
	d.data = json.RawMessage(`{"id":"abc123","name":"My Policy"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	d.Update(tea.KeyMsg{Type: tea.KeyTab})
	if d.activeTab != 0 {
		t.Errorf("activeTab should stay 0 for resources without associations, got %d", d.activeTab)
	}
}

func TestDetailScreen_AssocResultPopulates(t *testing.T) {
	d := NewDetailScreen(testUserEntry(), "abc123", "John")
	d.data = json.RawMessage(`{"_id":"abc123","username":"john"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Switch to associations tab.
	d.Update(tea.KeyMsg{Type: tea.KeyTab})
	gen := d.assocGen

	// Simulate associations result.
	assocData := []json.RawMessage{
		json.RawMessage(`{"type":"application","id":"app001"}`),
		json.RawMessage(`{"type":"application","id":"app002"}`),
	}
	d.Update(fetch.AssociationsResultMsg{
		ResourceKey: "users",
		TargetType:  d.assocTargets[0],
		Data:        assocData,
		Generation:  gen,
	})

	if d.assocLoading {
		t.Error("assocLoading should be false after result")
	}
	stored := d.assocData[d.assocTargets[0]]
	if len(stored) != 2 {
		t.Errorf("assocData has %d items, want 2", len(stored))
	}
	if len(d.assocTable.Rows) != 2 {
		t.Errorf("assocTable rows = %d, want 2", len(d.assocTable.Rows))
	}
}

func TestDetailScreen_TargetCycling(t *testing.T) {
	d := NewDetailScreen(testUserEntry(), "abc123", "John")
	d.data = json.RawMessage(`{"_id":"abc123","username":"john"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Switch to associations tab.
	d.Update(tea.KeyMsg{Type: tea.KeyTab})
	if d.assocTargetIdx != 0 {
		t.Fatalf("initial assocTargetIdx = %d, want 0", d.assocTargetIdx)
	}

	// Cycle right.
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if d.assocTargetIdx != 1 {
		t.Errorf("assocTargetIdx after l = %d, want 1", d.assocTargetIdx)
	}

	// Cycle left.
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	if d.assocTargetIdx != 0 {
		t.Errorf("assocTargetIdx after h = %d, want 0", d.assocTargetIdx)
	}
}

func TestDetailScreen_ViewShowsTabHeader(t *testing.T) {
	d := NewDetailScreen(testUserEntry(), "abc123", "John")
	d.data = json.RawMessage(`{"_id":"abc123","username":"john"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := d.View()
	if !strings.Contains(view, "Fields") {
		t.Error("view should show Fields tab")
	}
	if !strings.Contains(view, "Associations") {
		t.Error("view should show Associations tab")
	}
}

func TestDetailScreen_ViewNoTabsForNonGraphResource(t *testing.T) {
	d := NewDetailScreen(testPolicyEntry(), "abc123", "My Policy")
	d.data = json.RawMessage(`{"id":"abc123","name":"My Policy"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := d.View()
	if strings.Contains(view, "Associations") {
		t.Error("policies should not show Associations tab")
	}
}

func TestDetailScreen_StaleAssocResultIgnored(t *testing.T) {
	d := NewDetailScreen(testUserEntry(), "abc123", "John")
	d.data = json.RawMessage(`{"_id":"abc123","username":"john"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Switch to associations tab.
	d.Update(tea.KeyMsg{Type: tea.KeyTab})

	// Send stale result.
	d.Update(fetch.AssociationsResultMsg{
		ResourceKey: "users",
		TargetType:  d.assocTargets[0],
		Data:        []json.RawMessage{json.RawMessage(`{"type":"app","id":"x"}`)},
		Generation:  d.assocGen - 1,
	})

	if !d.assocLoading {
		t.Error("assocLoading should remain true (stale result)")
	}
}

func TestDetailScreen_AssocCacheAvoidsFetch(t *testing.T) {
	d := NewDetailScreen(testUserEntry(), "abc123", "John")
	d.data = json.RawMessage(`{"_id":"abc123","username":"john"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Pre-populate cache for first target.
	target := d.assocTargets[0]
	d.assocData[target] = []json.RawMessage{json.RawMessage(`{"type":"app","id":"x"}`)}

	// Switch to associations tab — should use cache, not fetch.
	d.Update(tea.KeyMsg{Type: tea.KeyTab})
	// assocLoading should NOT be set since data was cached.
	if d.assocLoading {
		t.Error("should not be loading — data was cached")
	}
	if len(d.assocTable.Rows) != 1 {
		t.Errorf("table rows = %d, want 1 (from cache)", len(d.assocTable.Rows))
	}
}
