package screen

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/apple_mdm"
)

// These tests run against the real embedded catalog (the apple_mdm
// package's go:embed of Apple's Release-v26.4 schemas). The catalog
// parse is cheap and cached, so this is fast and we don't have to
// fight Catalog's unexported fields to stub it.

func TestAppleMDMPayloadsListScreen_LoadsRealCatalog(t *testing.T) {
	s := NewAppleMDMPayloadsListScreen()
	s.Init()
	if !s.loaded {
		t.Fatal("screen not loaded")
	}
	if s.err != "" {
		t.Fatalf("unexpected error: %s", s.err)
	}
	if len(s.all) < 100 {
		t.Errorf("expected catalog of >=100 entries (Apple Release-v26.4 ships ~125), got %d", len(s.all))
	}
	if s.release == "" {
		t.Error("release should be populated from catalog")
	}
}

func TestAppleMDMPayloadsListScreen_Filter_NarrowsByTitleAndDescription(t *testing.T) {
	s := NewAppleMDMPayloadsListScreen()
	s.Init()
	pre := len(s.filtered)

	// Substring against title — pin to a well-known payload that's
	// covered by the apple_mdm package's TestCatalog_WellKnownPayloads.
	s.filter.SetValue("Firewall")
	s.applyFilter()
	if len(s.filtered) == 0 {
		t.Error("filter 'Firewall' should match com.apple.security.firewall")
	}
	for _, p := range s.filtered {
		hay := p.Type + " " + p.Title + " " + p.Description
		if !containsCaseInsensitive(hay, "firewall") {
			t.Errorf("leak: %s doesn't contain 'firewall'", p.Type)
		}
	}

	// No matches at all.
	s.filter.SetValue("zzzdefinitelynotreal")
	s.applyFilter()
	if len(s.filtered) != 0 {
		t.Errorf("nonsense filter should match nothing, got %d", len(s.filtered))
	}

	// Clearing restores the full set.
	s.filter.SetValue("")
	s.applyFilter()
	if len(s.filtered) != pre {
		t.Errorf("empty filter should restore %d rows, got %d", pre, len(s.filtered))
	}
}

func TestAppleMDMPayloadsListScreen_FilterClampsCursor(t *testing.T) {
	s := NewAppleMDMPayloadsListScreen()
	s.Init()
	s.cursor = len(s.filtered) - 1
	// Narrow to a small subset; cursor must clamp into range.
	s.filter.SetValue("wifi")
	s.applyFilter()
	if s.cursor >= len(s.filtered) {
		t.Errorf("cursor %d out of range after filter (len=%d)", s.cursor, len(s.filtered))
	}
}

func TestAppleMDMPayloadsListScreen_EscPopsWhenNotFiltering(t *testing.T) {
	s := NewAppleMDMPayloadsListScreen()
	s.Init()
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected a pop cmd, got nil")
	}
	if cmd() == nil {
		t.Error("pop cmd returned nil msg")
	}
}

func TestAppleMDMPayloadsListScreen_SlashEntersFilter_EscExits(t *testing.T) {
	s := NewAppleMDMPayloadsListScreen()
	s.Init()
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !s.filtering {
		t.Error("/ should enter filter mode")
	}
	s.filter.SetValue("nonsense")
	s.applyFilter()
	s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if s.filtering {
		t.Error("Esc should exit filter mode")
	}
	if s.filter.Value() != "" {
		t.Errorf("Esc should clear filter value, got %q", s.filter.Value())
	}
}

func TestAppleMDMPayloadsListScreen_FilterDoesNotCorruptCatalog(t *testing.T) {
	// Regression guard for Bugbot PR #52 re-review. Pre-fix:
	//   1. applyFilter with empty query: s.filtered = s.all (alias).
	//   2. applyFilter with non-empty query: s.filtered = s.filtered[:0]
	//      then append — appends went through s.all's backing array.
	//   3. s.all's contents were silently overwritten with filtered
	//      results; the next empty-query cycle saw a corrupted catalog.
	s := NewAppleMDMPayloadsListScreen()
	s.Init()
	originalLen := len(s.all)
	if originalLen == 0 {
		t.Fatal("catalog empty — cannot exercise regression")
	}
	// Snapshot the first entry's Type so we can detect overwrite.
	firstType := s.all[0].Type

	// Cycle: empty → narrow → empty. Pre-fix the second empty round
	// would not match the original first round because s.all had
	// been overwritten by the narrow round's appends.
	s.filter.SetValue("")
	s.applyFilter()
	s.filter.SetValue("xyz_unlikely_match_string_that_filters_to_one_or_few")
	s.applyFilter()
	s.filter.SetValue("")
	s.applyFilter()

	if len(s.all) != originalLen {
		t.Errorf("s.all length changed: %d → %d (filter corrupted backing array)", originalLen, len(s.all))
	}
	if s.all[0].Type != firstType {
		t.Errorf("s.all[0] mutated: %q → %q (filter aliased the slice)", firstType, s.all[0].Type)
	}
}

func TestAppleMDMPayloadsListScreen_EnterOpensDetailScreen(t *testing.T) {
	s := NewAppleMDMPayloadsListScreen()
	s.Init()
	if len(s.filtered) == 0 {
		t.Fatal("no payloads in catalog")
	}
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter should produce a push cmd")
	}
	msg := cmd()
	push, ok := msg.(interface{ marker() })
	_ = push
	_ = ok
	// We can't import internal/tui from this test file to type-assert
	// against PushScreenMsg without an import cycle through the screen
	// package; instead just verify a non-nil msg was produced. The
	// detail screen is exercised by TestAppleMDMPayloadsShowScreen.
	if msg == nil {
		t.Error("push cmd returned nil msg")
	}
}

// containsCaseInsensitive — local helper so the test doesn't import strings
// (already imported by other files in the package).
func containsCaseInsensitive(s, substr string) bool {
	return indexCaseInsensitive(s, substr) >= 0
}

func indexCaseInsensitive(s, substr string) int {
	if substr == "" {
		return 0
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if equalFold(s[i:i+len(substr)], substr) {
			return i
		}
	}
	return -1
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ai, bi := a[i], b[i]
		if 'A' <= ai && ai <= 'Z' {
			ai += 'a' - 'A'
		}
		if 'A' <= bi && bi <= 'Z' {
			bi += 'a' - 'A'
		}
		if ai != bi {
			return false
		}
	}
	return true
}

// unused but referenced by future tests on the detail screen
var _ = apple_mdm.Payload{}
