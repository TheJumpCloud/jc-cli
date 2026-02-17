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

func testIPListEntry() tui.ResourceEntry {
	return tui.ResourceEntry{
		Key:          "iplists",
		DisplayName:  "IP Lists",
		Category:     tui.CategorySecurity,
		ClientType:   tui.ClientV2,
		ListEndpoint: "/iplists",
		Schema:       schema.Resources["iplists"],
	}
}

func TestFormScreen_CreateTitle(t *testing.T) {
	f := NewFormScreen(testIPListEntry(), "create", nil)
	if f.Title() != "New IP Lists" {
		t.Errorf("Title = %q, want 'New IP Lists'", f.Title())
	}
}

func TestFormScreen_EditTitle(t *testing.T) {
	data := json.RawMessage(`{"id":"abc123","name":"My List"}`)
	f := NewFormScreen(testIPListEntry(), "edit", data)
	if f.Title() != "Edit IP Lists" {
		t.Errorf("Title = %q, want 'Edit IP Lists'", f.Title())
	}
}

func TestFormScreen_SkipsIDField(t *testing.T) {
	f := NewFormScreen(testIPListEntry(), "create", nil)
	for _, ff := range f.fields {
		if ff.def.Name == "id" {
			t.Error("ID field should be skipped in form")
		}
	}
}

func TestFormScreen_SkipsArrayAndObjectFields(t *testing.T) {
	// Users schema has array fields (addresses, phoneNumbers, attributes)
	// and object fields (mfa).
	f := NewFormScreen(testUserEntry(), "create", nil)
	for _, ff := range f.fields {
		if ff.def.Type == "array" || ff.def.Type == "object" {
			t.Errorf("field %q of type %q should be skipped", ff.def.Name, ff.def.Type)
		}
	}
}

func TestFormScreen_RequiredFieldMarker(t *testing.T) {
	f := NewFormScreen(testIPListEntry(), "create", nil)
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	view := f.View()
	if !strings.Contains(view, "name *") {
		t.Error("view should show required marker for 'name' field")
	}
}

func TestFormScreen_EditPrePopulates(t *testing.T) {
	data := json.RawMessage(`{"id":"abc123","name":"Test List","description":"A test"}`)
	f := NewFormScreen(testIPListEntry(), "edit", data)

	found := false
	for _, ff := range f.fields {
		if ff.def.Name == "name" {
			if ff.input.Value() != "Test List" {
				t.Errorf("name field value = %q, want 'Test List'", ff.input.Value())
			}
			found = true
		}
		if ff.def.Name == "description" {
			if ff.input.Value() != "A test" {
				t.Errorf("description field value = %q, want 'A test'", ff.input.Value())
			}
		}
	}
	if !found {
		t.Error("expected to find 'name' field")
	}
}

func TestFormScreen_EscCancels(t *testing.T) {
	f := NewFormScreen(testIPListEntry(), "create", nil)
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	_, cmd := f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(tui.PopScreenMsg); !ok {
		t.Fatalf("expected PopScreenMsg, got %T", msg)
	}
}

func TestFormScreen_BoolToggle(t *testing.T) {
	// Users schema has bool fields like "activated", "suspended".
	f := NewFormScreen(testUserEntry(), "create", nil)
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Find a bool field index.
	boolIdx := -1
	for i, ff := range f.fields {
		if ff.def.Type == "bool" {
			boolIdx = i
			break
		}
	}
	if boolIdx < 0 {
		t.Fatal("expected at least one bool field in users schema")
	}

	// Navigate to the bool field.
	for f.focusIdx < boolIdx {
		f.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	initial := f.fields[boolIdx].boolVal

	// Press 'l' to toggle.
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if f.fields[boolIdx].boolVal == initial {
		t.Error("boolVal should have toggled after 'l'")
	}

	// Press 'h' to toggle back.
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	if f.fields[boolIdx].boolVal != initial {
		t.Error("boolVal should have toggled back after 'h'")
	}
}

func TestFormScreen_BuildBodyCreate(t *testing.T) {
	f := NewFormScreen(testIPListEntry(), "create", nil)
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Set the name field (first field should be "name" since "id" is skipped).
	for i := range f.fields {
		if f.fields[i].def.Name == "name" {
			f.fields[i].input.SetValue("New List")
		}
		// Leave description empty.
	}

	// Submit — this will set submitting=true and call the fetcher.
	// Since we have no real fetcher, we just check that submitting was set
	// and that no validation error occurred.
	f.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	if f.err != "" {
		t.Errorf("unexpected error: %s", f.err)
	}
	if !f.submitting {
		t.Error("expected submitting to be true after ctrl+s")
	}
}

func TestFormScreen_EditOnlyChanged(t *testing.T) {
	data := json.RawMessage(`{"id":"abc123","name":"Original","description":"Old desc"}`)
	f := NewFormScreen(testIPListEntry(), "edit", data)
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Change only the description.
	for i := range f.fields {
		if f.fields[i].def.Name == "description" {
			f.fields[i].input.SetValue("New desc")
		}
	}

	// Submit.
	f.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	if f.err != "" {
		t.Errorf("unexpected error: %s", f.err)
	}
	if !f.submitting {
		t.Error("expected submitting to be true")
	}
	// We can't directly inspect the body sent to the fetcher without a mock,
	// but we verify no validation error and submitting state was set.
}

func TestFormScreen_EditNoChangeSkipsAPI(t *testing.T) {
	data := json.RawMessage(`{"id":"abc123","name":"Original","description":"Old desc"}`)
	f := NewFormScreen(testIPListEntry(), "edit", data)
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Don't change anything. Submit.
	_, cmd := f.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if f.submitting {
		t.Error("should not be submitting when nothing changed")
	}
	if cmd == nil {
		t.Fatal("expected a batch command with flash + pop")
	}

	// Execute the batch and check for FlashMsg and PopScreenMsg.
	batchMsg := cmd()
	msgs, ok := batchMsg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg, got %T", batchMsg)
	}

	var hasFlash, hasPop bool
	for _, subCmd := range msgs {
		if subCmd == nil {
			continue
		}
		subMsg := subCmd()
		switch m := subMsg.(type) {
		case tui.FlashMsg:
			hasFlash = true
			if !strings.Contains(m.Text, "No changes") {
				t.Errorf("flash text = %q, want to contain 'No changes'", m.Text)
			}
		case tui.PopScreenMsg:
			hasPop = true
		}
	}
	if !hasFlash {
		t.Error("expected FlashMsg in batch")
	}
	if !hasPop {
		t.Error("expected PopScreenMsg in batch")
	}
}

func TestFormScreen_SubmitSuccess(t *testing.T) {
	f := NewFormScreen(testIPListEntry(), "create", nil)
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Fill required field.
	for i := range f.fields {
		if f.fields[i].def.Name == "name" {
			f.fields[i].input.SetValue("Test")
		}
	}

	// Submit to set generation.
	f.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	gen := f.generation

	// Simulate successful mutation result.
	_, cmd := f.Update(fetch.MutationResultMsg{
		ResourceKey: "iplists",
		Data:        json.RawMessage(`{"id":"new123","name":"Test"}`),
		Generation:  gen,
	})

	if f.submitting {
		t.Error("submitting should be false after success")
	}
	if f.err != "" {
		t.Errorf("err should be empty, got %q", f.err)
	}
	if cmd == nil {
		t.Fatal("expected batch command after success")
	}

	// Execute the batch to collect all messages.
	// tea.Batch returns a function that when called returns a tea.BatchMsg.
	batchMsg := cmd()
	msgs, ok := batchMsg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg, got %T", batchMsg)
	}

	// Execute all sub-commands and collect message types.
	var hasFlash, hasPop, hasRefresh bool
	for _, subCmd := range msgs {
		if subCmd == nil {
			continue
		}
		subMsg := subCmd()
		switch m := subMsg.(type) {
		case tui.FlashMsg:
			hasFlash = true
			if !strings.Contains(m.Text, "Created") {
				t.Errorf("flash text = %q, want to contain 'Created'", m.Text)
			}
		case tui.PopScreenMsg:
			hasPop = true
		case tui.RefreshListMsg:
			hasRefresh = true
		}
	}
	if !hasFlash {
		t.Error("expected FlashMsg in batch")
	}
	if !hasPop {
		t.Error("expected PopScreenMsg in batch")
	}
	if !hasRefresh {
		t.Error("expected RefreshListMsg in batch")
	}
}

func TestFormScreen_SubmitError(t *testing.T) {
	f := NewFormScreen(testIPListEntry(), "create", nil)
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Fill required field.
	for i := range f.fields {
		if f.fields[i].def.Name == "name" {
			f.fields[i].input.SetValue("Test")
		}
	}

	// Submit.
	f.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	gen := f.generation

	// Simulate error result.
	f.Update(fetch.MutationResultMsg{
		ResourceKey: "iplists",
		Generation:  gen,
		Err:         fmt.Errorf("API error: 400"),
	})

	if f.submitting {
		t.Error("submitting should be false after error")
	}
	if f.err == "" {
		t.Error("err should be set after error result")
	}
	if !strings.Contains(f.err, "API error") {
		t.Errorf("err = %q, want to contain 'API error'", f.err)
	}

	// Verify the error shows in the view.
	view := f.View()
	if !strings.Contains(view, "API error") {
		t.Error("view should show the error message")
	}
}

func TestFormScreen_NavigateFields(t *testing.T) {
	f := NewFormScreen(testIPListEntry(), "create", nil)
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	if len(f.fields) < 2 {
		t.Fatalf("need at least 2 fields, got %d", len(f.fields))
	}

	if f.focusIdx != 0 {
		t.Fatalf("initial focusIdx = %d, want 0", f.focusIdx)
	}

	// Press down to move to next field.
	f.Update(tea.KeyMsg{Type: tea.KeyDown})
	if f.focusIdx != 1 {
		t.Errorf("focusIdx after down = %d, want 1", f.focusIdx)
	}

	// Press up to move back.
	f.Update(tea.KeyMsg{Type: tea.KeyUp})
	if f.focusIdx != 0 {
		t.Errorf("focusIdx after up = %d, want 0", f.focusIdx)
	}

	// Press up wraps to last.
	f.Update(tea.KeyMsg{Type: tea.KeyUp})
	if f.focusIdx != len(f.fields)-1 {
		t.Errorf("focusIdx after wrap up = %d, want %d", f.focusIdx, len(f.fields)-1)
	}
}

func TestFormScreen_KTypesInTextField(t *testing.T) {
	f := NewFormScreen(testIPListEntry(), "create", nil)
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Focus is on the first text field (name). Pressing 'k' should type 'k',
	// not navigate up.
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if f.focusIdx != 0 {
		t.Errorf("focusIdx changed to %d after 'k' on text field, want 0", f.focusIdx)
	}
	if f.fields[0].input.Value() != "k" {
		t.Errorf("text field value = %q after 'k', want 'k'", f.fields[0].input.Value())
	}
}

func TestFormScreen_KNavigatesOnBoolField(t *testing.T) {
	f := NewFormScreen(testUserEntry(), "create", nil)
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Find first bool field.
	boolIdx := -1
	for i, ff := range f.fields {
		if ff.def.Type == "bool" {
			boolIdx = i
			break
		}
	}
	if boolIdx < 0 {
		t.Fatal("expected at least one bool field")
	}

	// Navigate to the bool field.
	for f.focusIdx < boolIdx {
		f.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	// 'k' on a bool field should navigate up.
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if f.focusIdx != boolIdx-1 {
		t.Errorf("focusIdx = %d after 'k' on bool field, want %d", f.focusIdx, boolIdx-1)
	}
}

func TestFormScreen_BoolToggleArrowKeys(t *testing.T) {
	f := NewFormScreen(testUserEntry(), "create", nil)
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Find a bool field index.
	boolIdx := -1
	for i, ff := range f.fields {
		if ff.def.Type == "bool" {
			boolIdx = i
			break
		}
	}
	if boolIdx < 0 {
		t.Fatal("expected at least one bool field")
	}

	// Navigate to the bool field.
	for f.focusIdx < boolIdx {
		f.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	initial := f.fields[boolIdx].boolVal

	// Left arrow toggles.
	f.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if f.fields[boolIdx].boolVal == initial {
		t.Error("boolVal should toggle on left arrow")
	}

	// Right arrow toggles back.
	f.Update(tea.KeyMsg{Type: tea.KeyRight})
	if f.fields[boolIdx].boolVal != initial {
		t.Error("boolVal should toggle on right arrow")
	}

	// Space toggles.
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	if f.fields[boolIdx].boolVal == initial {
		t.Error("boolVal should toggle on space")
	}
}

func TestFormScreen_CreateOmitsUntouchedBools(t *testing.T) {
	f := NewFormScreen(testUserEntry(), "create", nil)
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Fill required username field.
	for i := range f.fields {
		if f.fields[i].def.Name == "username" {
			f.fields[i].input.SetValue("testuser")
		}
		if f.fields[i].def.Name == "email" {
			f.fields[i].input.SetValue("test@example.com")
		}
	}

	// Don't touch any bool fields. Verify boolTouched is false.
	for _, ff := range f.fields {
		if ff.def.Type == "bool" && ff.boolTouched {
			t.Errorf("bool field %q should not be touched before toggle", ff.def.Name)
		}
	}

	// Submit.
	f.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if f.err != "" {
		t.Fatalf("unexpected error: %s", f.err)
	}
	if !f.submitting {
		t.Error("expected submitting to be true")
	}
}

func TestFormScreen_CreateIncludesTouchedBools(t *testing.T) {
	f := NewFormScreen(testUserEntry(), "create", nil)
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Find a bool field and toggle it.
	boolIdx := -1
	for i, ff := range f.fields {
		if ff.def.Type == "bool" {
			boolIdx = i
			break
		}
	}
	if boolIdx < 0 {
		t.Fatal("expected at least one bool field")
	}

	// Navigate to bool field.
	for f.focusIdx < boolIdx {
		f.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	// Toggle it.
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	if !f.fields[boolIdx].boolTouched {
		t.Error("boolTouched should be true after toggle")
	}
}

func TestFormScreen_TextInputActive(t *testing.T) {
	f := NewFormScreen(testIPListEntry(), "create", nil)
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	if !f.TextInputActive() {
		t.Error("TextInputActive should return true when form is active")
	}

	// Set submitting — TextInputActive should return false.
	f.submitting = true
	if f.TextInputActive() {
		t.Error("TextInputActive should return false when submitting")
	}
}

func TestFormScreen_QTypesInTextField(t *testing.T) {
	// This tests that 'q' reaches the text input (the app-level fix ensures
	// 'q' is not intercepted when TextInputActive() is true).
	f := NewFormScreen(testIPListEntry(), "create", nil)
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	// Type 'q' into the first text field.
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if f.fields[0].input.Value() != "q" {
		t.Errorf("text field value = %q after 'q', want 'q'", f.fields[0].input.Value())
	}
}
