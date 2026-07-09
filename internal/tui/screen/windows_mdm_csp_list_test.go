package screen

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// testCSPSettings is a tiny in-memory catalog slice — screen tests
// never touch the fetch-on-demand snapshot pipeline.
func testCSPSettings() []windows_mdm.Setting {
	return []windows_mdm.Setting{
		{
			Area: "Camera", Name: "AllowCamera",
			URI:    "./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera",
			Scope:  "device", Format: "int",
			Description:  "Disables or enables the camera.",
			DefaultValue: "1",
			AllowedValues: &windows_mdm.AllowedValues{
				Type: "ENUM",
				Enum: []windows_mdm.EnumValue{
					{Value: "0", Description: "Not allowed."},
					{Value: "1", Description: "Allowed."},
				},
			},
		},
		{
			Area: "DeviceLock", Name: "MaxDevicePasswordFailedAttempts",
			URI:   "./Device/Vendor/MSFT/Policy/Config/DeviceLock/MaxDevicePasswordFailedAttempts",
			Scope: "device", Format: "int",
			Description:   "Number of authentication failures before wipe.",
			DefaultValue:  "0",
			AllowedValues: &windows_mdm.AllowedValues{Type: "Range", Value: "[0-999]"},
		},
		{
			Area: "ADMX_Sample", Name: "LegacyThing",
			URI:   "./Device/Vendor/MSFT/Policy/Config/ADMX_Sample/LegacyThing",
			Scope: "device", Format: "chr",
			Description:   "An ADMX-backed sample.",
			ADMXBacked:    true,
			AllowedValues: &windows_mdm.AllowedValues{Type: "ADMX"},
		},
		{
			Area: "Sample", Name: "UserThing",
			URI:   "./User/Vendor/MSFT/Policy/Config/Sample/UserThing",
			Scope: "user", Format: "chr",
			Description: "A user-scoped sample.",
		},
	}
}

// stubCatalogLoader swaps the catalog loader for the test's lifetime.
func stubCatalogLoader(t *testing.T, settings []windows_mdm.Setting, err error) {
	t.Helper()
	orig := windowsCSPCatalogLoader
	windowsCSPCatalogLoader = func() ([]windows_mdm.Setting, string, error) {
		return settings, "TestSnapshot", err
	}
	t.Cleanup(func() { windowsCSPCatalogLoader = orig })
}

// loadedCSPList builds the list screen and drives the async load to
// completion.
func loadedCSPList(t *testing.T) *WindowsMDMCSPListScreen {
	t.Helper()
	stubCatalogLoader(t, testCSPSettings(), nil)
	s := NewWindowsMDMCSPListScreen()
	cmd := s.loadCmd()
	model, _ := s.Update(cmd())
	return model.(*WindowsMDMCSPListScreen)
}

func TestWindowsCSPList_LoadsAsync(t *testing.T) {
	s := loadedCSPList(t)
	if s.loading {
		t.Error("loading should clear after loadCatalogMsg")
	}
	if len(s.all) != 4 || len(s.filtered) != 4 {
		t.Errorf("expected 4 settings, got all=%d filtered=%d", len(s.all), len(s.filtered))
	}
	if s.snapshot != "TestSnapshot" {
		t.Errorf("snapshot = %q", s.snapshot)
	}
	view := s.View()
	for _, want := range []string{"Camera/AllowCamera", "ADMX_Sample/LegacyThing", "yes"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestWindowsCSPList_LoadErrorRendersRetryHint(t *testing.T) {
	stubCatalogLoader(t, nil, errors.New("download failed: dial tcp"))
	s := NewWindowsMDMCSPListScreen()
	model, _ := s.Update(s.loadCmd()())
	s = model.(*WindowsMDMCSPListScreen)
	view := s.View()
	if !strings.Contains(view, "download failed") || !strings.Contains(view, "r retry") {
		t.Errorf("error view should carry the error + retry hint:\n%s", view)
	}
}

func TestWindowsCSPList_FilterNarrowsAndResets(t *testing.T) {
	s := loadedCSPList(t)

	s.filter.SetValue("camera")
	s.applyFilter()
	if len(s.filtered) != 1 || s.filtered[0].Name != "AllowCamera" {
		t.Errorf("filter wrong: %+v", s.filtered)
	}

	// Description text matches too.
	s.filter.SetValue("authentication failures")
	s.applyFilter()
	if len(s.filtered) != 1 || s.filtered[0].Area != "DeviceLock" {
		t.Errorf("description filter wrong: %+v", s.filtered)
	}

	// Reset must produce a FRESH slice, not an alias of s.all — same
	// regression guard as the Apple list.
	s.filter.SetValue("")
	s.applyFilter()
	if len(s.filtered) != 4 {
		t.Fatalf("reset filter wrong: %d", len(s.filtered))
	}
	s.filtered[0] = windows_mdm.Setting{Name: "mutated"}
	if s.all[0].Name == "mutated" {
		t.Error("filtered slice aliases all — mutation leaked")
	}
}

func TestWindowsCSPList_NoFilterModeOnErrorScreen(t *testing.T) {
	// The error view renders instead of the list; `/` must not start
	// typing into a hidden filter box (CodeRabbit PR #67 review).
	stubCatalogLoader(t, nil, errors.New("boom"))
	s := NewWindowsMDMCSPListScreen()
	model, _ := s.Update(s.loadCmd()())
	s = model.(*WindowsMDMCSPListScreen)
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if s.filtering {
		t.Error("filter mode must not activate while the error view is shown")
	}
}

func TestWindowsCSPList_CursorClamps(t *testing.T) {
	s := loadedCSPList(t)
	s.cursor = 3
	s.filter.SetValue("camera")
	s.applyFilter()
	if s.cursor != 0 {
		t.Errorf("cursor should clamp to the narrowed list, got %d", s.cursor)
	}
}

func TestWindowsCSPList_EnterPushesDetail(t *testing.T) {
	s := loadedCSPList(t)
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should produce a push cmd")
	}
}

func TestWindowsCSPList_EmptyDraftFlashesOnAuthor(t *testing.T) {
	s := loadedCSPList(t)
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if cmd == nil {
		t.Fatal("c on empty draft should flash a hint")
	}
	// With a drafted setting it pushes the form instead.
	s.draft.add(testCSPSettings()[0])
	_, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if cmd == nil {
		t.Fatal("c with a draft should push the form")
	}
}

func TestWindowsMDMDraft_AddDedupesByURI(t *testing.T) {
	d := &windowsMDMDraft{}
	s := testCSPSettings()[0]
	if !d.add(s) {
		t.Fatal("first add should succeed")
	}
	if d.add(s) {
		t.Fatal("duplicate add should be refused")
	}
	if len(d.settings) != 1 {
		t.Errorf("draft should hold exactly 1, got %d", len(d.settings))
	}
}
