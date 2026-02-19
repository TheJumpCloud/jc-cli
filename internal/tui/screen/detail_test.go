package screen

import (
	"encoding/json"
	"fmt"
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
		Key:             "policies",
		DisplayName:     "Policies",
		Category:        tui.CategoryManagement,
		ClientType:      tui.ClientV2,
		ListEndpoint:    "/policies",
		GraphSourceType: "policy",
		Schema:          schema.Resources["policies"],
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

func TestDetailScreen_HasAssocForPolicies(t *testing.T) {
	d := NewDetailScreen(testPolicyEntry(), "abc123", "My Policy")
	if len(d.assocTargets) != 3 {
		t.Errorf("policy entry should have 3 association targets, got %d", len(d.assocTargets))
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
	entry := tui.ResourceEntry{
		Key:          "org",
		DisplayName:  "Organization",
		Category:     tui.CategoryManagement,
		ClientType:   tui.ClientV1,
		ListEndpoint: "/organizations",
		Schema:       schema.Resources["org"],
	}
	d := NewDetailScreen(entry, "abc123", "My Org")
	d.data = json.RawMessage(`{"_id":"abc123","displayName":"My Org"}`)
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
	entry := tui.ResourceEntry{
		Key:          "org",
		DisplayName:  "Organization",
		Category:     tui.CategoryManagement,
		ClientType:   tui.ClientV1,
		ListEndpoint: "/organizations",
		Schema:       schema.Resources["org"],
	}
	d := NewDetailScreen(entry, "abc123", "My Org")
	d.data = json.RawMessage(`{"_id":"abc123","displayName":"My Org"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := d.View()
	if strings.Contains(view, "Associations") {
		t.Error("org should not show Associations tab")
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

func testUserGroupEntry() tui.ResourceEntry {
	return tui.ResourceEntry{
		Key:             "user-groups",
		DisplayName:     "User Groups",
		Category:        tui.CategoryIdentity,
		ClientType:      tui.ClientV2,
		ListEndpoint:    "/usergroups",
		GraphSourceType: "user_group",
		Schema:          schema.Resources["groups"],
	}
}

func TestDetailScreen_AssocEnterNavigatesToDetail(t *testing.T) {
	d := NewDetailScreen(testUserGroupEntry(), "grp001", "Developers")
	d.data = json.RawMessage(`{"id":"grp001","name":"Developers"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Switch to associations tab.
	d.Update(tea.KeyMsg{Type: tea.KeyTab})
	gen := d.assocGen

	// Simulate association data with a user member.
	assocData := []json.RawMessage{
		json.RawMessage(`{"type":"system","id":"aaa111bbb222ccc333ddd444"}`),
		json.RawMessage(`{"type":"system","id":"eee555fff666aaa777bbb888"}`),
	}
	d.Update(fetch.AssociationsResultMsg{
		ResourceKey: "user-groups",
		TargetType:  d.assocTargets[d.assocTargetIdx],
		Data:        assocData,
		Generation:  gen,
	})

	// Press Enter on the first association row.
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command from Enter on association row, got nil")
	}

	msg := cmd()
	pushMsg, ok := msg.(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("expected PushScreenMsg, got %T", msg)
	}

	detail, ok := pushMsg.Screen.(*DetailScreen)
	if !ok {
		t.Fatalf("expected *DetailScreen, got %T", pushMsg.Screen)
	}

	// "system" graph type should navigate to "devices" registry key.
	if detail.entry.Key != "devices" {
		t.Errorf("assoc drill-down key = %q, want 'devices'", detail.entry.Key)
	}
	if detail.id != "aaa111bbb222ccc333ddd444" {
		t.Errorf("assoc drill-down id = %q, want 'aaa111bbb222ccc333ddd444'", detail.id)
	}
}

func TestDetailScreen_AssocEnterWithUserType(t *testing.T) {
	d := NewDetailScreen(testUserGroupEntry(), "grp001", "Developers")
	d.data = json.RawMessage(`{"id":"grp001","name":"Developers"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Switch to associations tab.
	d.Update(tea.KeyMsg{Type: tea.KeyTab})

	// Navigate to a target type that has "user" associations.
	// user_group targets: application, system, system_group
	// We need to find the right target index, but let's just populate data directly.
	d.assocLoading = false
	d.assocTable.Rows = []json.RawMessage{
		json.RawMessage(`{"type":"application","id":"aaa111bbb222ccc333ddd444"}`),
	}

	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command from Enter on application association")
	}

	msg := cmd()
	pushMsg, ok := msg.(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("expected PushScreenMsg, got %T", msg)
	}

	detail := pushMsg.Screen.(*DetailScreen)
	if detail.entry.Key != "apps" {
		t.Errorf("assoc drill-down key = %q, want 'apps'", detail.entry.Key)
	}
}

func TestDetailScreen_AssocEnterNoRowsIsNoop(t *testing.T) {
	d := NewDetailScreen(testUserGroupEntry(), "grp001", "Developers")
	d.data = json.RawMessage(`{"id":"grp001","name":"Developers"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Switch to associations tab.
	d.Update(tea.KeyMsg{Type: tea.KeyTab})

	// No data loaded — Enter should be a no-op.
	d.assocLoading = false
	d.assocTable.Rows = nil

	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("expected nil command when no association rows exist")
	}
}

func TestDetailScreen_AssocEnterUnknownTypeIsNoop(t *testing.T) {
	d := NewDetailScreen(testUserGroupEntry(), "grp001", "Developers")
	d.data = json.RawMessage(`{"id":"grp001","name":"Developers"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Switch to associations tab and populate with unknown type.
	d.Update(tea.KeyMsg{Type: tea.KeyTab})
	d.assocLoading = false
	d.assocTable.Rows = []json.RawMessage{
		json.RawMessage(`{"type":"unknown_thing","id":"aaa111bbb222ccc333ddd444"}`),
	}

	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("expected nil command for unknown graph type")
	}
}

func TestDetailScreen_AssocNamesEnrichRows(t *testing.T) {
	d := NewDetailScreen(testUserGroupEntry(), "grp001", "Developers")
	d.data = json.RawMessage(`{"id":"grp001","name":"Developers"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Switch to associations tab.
	d.Update(tea.KeyMsg{Type: tea.KeyTab})
	gen := d.assocGen

	// Simulate association data.
	assocData := []json.RawMessage{
		json.RawMessage(`{"type":"system","id":"aaa111bbb222ccc333ddd444"}`),
		json.RawMessage(`{"type":"system","id":"eee555fff666aaa777bbb888"}`),
	}
	d.Update(fetch.AssociationsResultMsg{
		ResourceKey: "user-groups",
		TargetType:  d.assocTargets[d.assocTargetIdx],
		Data:        assocData,
		Generation:  gen,
	})

	// Simulate names resolved.
	d.Update(fetch.AssocNamesResolvedMsg{
		Names:      map[string]string{"aaa111bbb222ccc333ddd444": "MacBook Pro", "eee555fff666aaa777bbb888": "iMac"},
		Generation: gen,
	})

	// Columns should now include "name".
	if len(d.assocTable.Columns) != 3 {
		t.Fatalf("columns = %v, want 3 columns", d.assocTable.Columns)
	}
	if d.assocTable.Columns[1] != "name" {
		t.Errorf("columns[1] = %q, want 'name'", d.assocTable.Columns[1])
	}

	// Rows should contain the name field.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(d.assocTable.Rows[0], &obj); err != nil {
		t.Fatal(err)
	}
	var name string
	json.Unmarshal(obj["name"], &name)
	if name != "MacBook Pro" {
		t.Errorf("row[0].name = %q, want 'MacBook Pro'", name)
	}
}

func TestDetailScreen_AssocNamesStaleIgnored(t *testing.T) {
	d := NewDetailScreen(testUserGroupEntry(), "grp001", "Developers")
	d.data = json.RawMessage(`{"id":"grp001","name":"Developers"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Switch to associations tab.
	d.Update(tea.KeyMsg{Type: tea.KeyTab})

	// Simulate stale names result.
	d.Update(fetch.AssocNamesResolvedMsg{
		Names:      map[string]string{"aaa111bbb222ccc333ddd444": "Stale"},
		Generation: d.assocGen - 1,
	})

	// assocNames should be empty — stale result ignored.
	if len(d.assocNames) != 0 {
		t.Errorf("assocNames should be empty after stale result, got %d", len(d.assocNames))
	}
}

func TestDetailScreen_AssocNamesPersistAcrossTargets(t *testing.T) {
	d := NewDetailScreen(testUserGroupEntry(), "grp001", "Developers")
	d.data = json.RawMessage(`{"id":"grp001","name":"Developers"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Pre-populate a name.
	d.assocNames["aaa111bbb222ccc333ddd444"] = "MacBook Pro"

	// Switch to associations tab.
	d.Update(tea.KeyMsg{Type: tea.KeyTab})
	gen := d.assocGen

	// Simulate data that includes the pre-resolved ID.
	assocData := []json.RawMessage{
		json.RawMessage(`{"type":"system","id":"aaa111bbb222ccc333ddd444"}`),
	}
	d.Update(fetch.AssociationsResultMsg{
		ResourceKey: "user-groups",
		TargetType:  d.assocTargets[d.assocTargetIdx],
		Data:        assocData,
		Generation:  gen,
	})

	// Name should already be enriched from the persistent cache.
	if d.assocTable.Columns[1] != "name" {
		t.Errorf("columns should include name from persistent cache, got %v", d.assocTable.Columns)
	}

	var obj map[string]json.RawMessage
	json.Unmarshal(d.assocTable.Rows[0], &obj)
	var name string
	json.Unmarshal(obj["name"], &name)
	if name != "MacBook Pro" {
		t.Errorf("row name = %q, want 'MacBook Pro'", name)
	}
}

func TestDetailScreen_ExportModeToggle(t *testing.T) {
	origClip := clipboardWriteFunc
	clipboardWriteFunc = func(s string) error { return nil }
	defer func() { clipboardWriteFunc = origClip }()

	d := NewDetailScreen(testUserEntry(), "abc123def456abc123def456", "John Doe")
	d.data = json.RawMessage(`{"_id":"abc123def456abc123def456","username":"john"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Press 'e' to enter export mode.
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if !d.exporting {
		t.Error("expected exporting to be true after 'e'")
	}

	// Press 'esc' to cancel.
	d.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if d.exporting {
		t.Error("expected exporting to be false after 'esc'")
	}
}

func TestDetailScreen_ExportJSONToClipboard(t *testing.T) {
	var clipped string
	origClip := clipboardWriteFunc
	clipboardWriteFunc = func(s string) error { clipped = s; return nil }
	defer func() { clipboardWriteFunc = origClip }()

	d := NewDetailScreen(testUserEntry(), "abc123def456abc123def456", "John Doe")
	d.data = json.RawMessage(`{"_id":"abc123def456abc123def456","username":"john"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Press 'e' then 'j' to export JSON to clipboard.
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if cmd == nil {
		t.Fatal("expected command from 'j' export")
	}

	msg := cmd()
	flash, ok := msg.(tui.FlashMsg)
	if !ok {
		t.Fatalf("expected FlashMsg, got %T", msg)
	}
	if !strings.Contains(flash.Text, "JSON") {
		t.Errorf("flash = %q, want to contain 'JSON'", flash.Text)
	}
	if clipped == "" {
		t.Error("clipboard should not be empty")
	}
	if d.exporting {
		t.Error("exporting should be false after export completes")
	}
}

func TestDetailScreen_ExportNoDataIsNoop(t *testing.T) {
	d := NewDetailScreen(testUserEntry(), "abc123def456abc123def456", "John Doe")
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Press 'e' with no data — should not enter export mode.
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if d.exporting {
		t.Error("should not enter export mode with no data")
	}
}

func TestDetailScreen_CopyIDProducesFlash(t *testing.T) {
	var copied string
	origClip := clipboardWriteFunc
	clipboardWriteFunc = func(s string) error { copied = s; return nil }
	defer func() { clipboardWriteFunc = origClip }()

	d := NewDetailScreen(testUserEntry(), "abc123def456abc123def456", "John Doe")
	d.data = json.RawMessage(`{"_id":"abc123def456abc123def456","username":"john"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if cmd == nil {
		t.Fatal("'c' should return a command")
	}

	msg := cmd()
	flash, ok := msg.(tui.FlashMsg)
	if !ok {
		t.Fatalf("expected FlashMsg, got %T", msg)
	}
	if flash.Text != "Copied: abc123def456abc123def456" {
		t.Errorf("flash text = %q, want 'Copied: abc123def456abc123def456'", flash.Text)
	}
	if copied != "abc123def456abc123def456" {
		t.Errorf("clipboard = %q, want 'abc123def456abc123def456'", copied)
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

// --- Delete tests ---

func TestDetailScreen_DeleteConfirm(t *testing.T) {
	d := NewDetailScreen(testUserEntry(), "abc123def456abc123def456", "alice")
	d.data = json.RawMessage(`{"_id":"abc123def456abc123def456","username":"alice"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Press 'd' to enter confirmation mode.
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if !d.confirming {
		t.Error("expected confirming to be true after 'd'")
	}

	// View should show confirmation prompt.
	view := d.View()
	if !strings.Contains(view, "Delete") {
		t.Error("view should contain 'Delete' prompt")
	}
}

func TestDetailScreen_DeleteCancel(t *testing.T) {
	d := NewDetailScreen(testUserEntry(), "abc123def456abc123def456", "alice")
	d.data = json.RawMessage(`{"_id":"abc123def456abc123def456","username":"alice"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Press 'd' to enter confirmation, then 'n' to cancel.
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if d.confirming {
		t.Error("expected confirming to be false after cancellation")
	}
}

func TestDetailScreen_DeleteNoVerb(t *testing.T) {
	// policy-templates don't have "delete" verb.
	entry := tui.ResourceEntry{
		Key:          "policy-templates",
		DisplayName:  "Policy Templates",
		Category:     tui.CategoryApplications,
		ClientType:   tui.ClientV2,
		ListEndpoint: "/policytemplates",
		Schema:       schema.Resources["policy-templates"],
	}
	d := NewDetailScreen(entry, "abc123def456abc123def456", "My Template")
	d.data = json.RawMessage(`{"id":"abc123def456abc123def456","name":"My Template"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if d.confirming {
		t.Error("should not enter confirming for resource without delete verb")
	}
}

func TestDetailScreen_DeleteSuccess(t *testing.T) {
	d := NewDetailScreen(testUserEntry(), "abc123def456abc123def456", "alice")
	d.data = json.RawMessage(`{"_id":"abc123def456abc123def456","username":"alice"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Enter confirmation and confirm.
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	gen := d.deleteGen

	// Simulate successful delete.
	_, cmd := d.Update(fetch.MutationResultMsg{
		ResourceKey: "users",
		Generation:  gen,
	})
	if cmd == nil {
		t.Fatal("expected batch command from successful delete")
	}
	if d.confirming {
		t.Error("confirming should be false after mutation result")
	}
}

func TestDetailScreen_DeleteError(t *testing.T) {
	d := NewDetailScreen(testUserEntry(), "abc123def456abc123def456", "alice")
	d.data = json.RawMessage(`{"_id":"abc123def456abc123def456","username":"alice"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Enter confirmation and confirm.
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	gen := d.deleteGen

	// Simulate failed delete.
	d.Update(fetch.MutationResultMsg{
		ResourceKey: "users",
		Generation:  gen,
		Err:         fmt.Errorf("not found"),
	})
	if d.err != "not found" {
		t.Errorf("err = %q, want 'not found'", d.err)
	}
}

// --- Edit tests ---

func TestDetailScreen_EditPushesForm(t *testing.T) {
	d := NewDetailScreen(testUserEntry(), "abc123def456abc123def456", "alice")
	d.data = json.RawMessage(`{"_id":"abc123def456abc123def456","username":"alice","email":"alice@example.com"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	if cmd == nil {
		t.Fatal("expected command from 'E'")
	}

	msg := cmd()
	pushMsg, ok := msg.(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("expected PushScreenMsg, got %T", msg)
	}

	form, ok := pushMsg.Screen.(*FormScreen)
	if !ok {
		t.Fatalf("expected *FormScreen, got %T", pushMsg.Screen)
	}
	if form.mode != "edit" {
		t.Errorf("form mode = %q, want 'edit'", form.mode)
	}
}

func TestDetailScreen_EditNoVerb(t *testing.T) {
	// policy-templates don't have "update" verb.
	entry := tui.ResourceEntry{
		Key:          "policy-templates",
		DisplayName:  "Policy Templates",
		Category:     tui.CategoryApplications,
		ClientType:   tui.ClientV2,
		ListEndpoint: "/policytemplates",
		Schema:       schema.Resources["policy-templates"],
	}
	d := NewDetailScreen(entry, "abc123def456abc123def456", "My Template")
	d.data = json.RawMessage(`{"id":"abc123def456abc123def456","name":"My Template"}`)
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	if cmd != nil {
		t.Error("should not push form for resource without update verb")
	}
}

func TestDetailScreen_EditNoDataIsNoop(t *testing.T) {
	d := NewDetailScreen(testUserEntry(), "abc123def456abc123def456", "alice")
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	if cmd != nil {
		t.Error("should not push form when data is nil")
	}
}
