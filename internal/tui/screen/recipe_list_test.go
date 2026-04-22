package screen

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/recipe"
	"github.com/klaassen-consulting/jc/internal/tui"
)

// withRecipeLoaders installs fake loaders for the duration of a test.
func withRecipeLoaders(t *testing.T, all, builtin []*recipe.Recipe) {
	t.Helper()
	origAll, origBuiltin := recipeLoader, builtInLoader
	recipeLoader = func() ([]*recipe.Recipe, error) { return all, nil }
	builtInLoader = func() ([]*recipe.Recipe, error) { return builtin, nil }
	t.Cleanup(func() {
		recipeLoader = origAll
		builtInLoader = origBuiltin
	})
}

func sampleRecipes() []*recipe.Recipe {
	return []*recipe.Recipe{
		{
			Name:        "security-audit",
			Description: "Scan recent auth failures",
			Tags:        []string{"security"},
			Steps:       []recipe.Step{{Name: "list failures", Command: "insights query --service sso"}},
		},
		{
			Name:        "onboard-user",
			Description: "Create a new user",
			Parameters: []recipe.Parameter{
				{Name: "username", Required: true},
				{Name: "email", Required: true},
			},
			Steps: []recipe.Step{{Name: "create", Command: "users create --username {{ .username }}"}},
		},
	}
}

func TestRecipeListScreen_Title(t *testing.T) {
	s := NewRecipeListScreen()
	if s.Title() != "Recipes" {
		t.Errorf("Title = %q, want 'Recipes'", s.Title())
	}
}

func TestRecipeListScreen_LoadShowsRecipes(t *testing.T) {
	all := sampleRecipes()
	withRecipeLoaders(t, all, all)
	s := NewRecipeListScreen()
	s.Init()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	view := s.View()
	if !strings.Contains(view, "security-audit") {
		t.Errorf("view should contain 'security-audit'; got:\n%s", view)
	}
	if !strings.Contains(view, "onboard-user") {
		t.Errorf("view should contain 'onboard-user'; got:\n%s", view)
	}
}

func TestRecipeListScreen_EnterPushesParamForm(t *testing.T) {
	all := sampleRecipes()
	withRecipeLoaders(t, all, all)
	s := NewRecipeListScreen()
	s.Init()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter should produce a command")
	}
	pushMsg, ok := cmd().(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("expected PushScreenMsg, got %T", cmd())
	}
	// Recipes are sorted alphabetically in LoadAll, but our fake bypasses that;
	// accept either as long as it's one of our recipes.
	title := pushMsg.Screen.Title()
	if title != "Configure: security-audit" && title != "Configure: onboard-user" {
		t.Errorf("unexpected pushed screen title %q", title)
	}
}

func TestRecipeListScreen_EscPops(t *testing.T) {
	withRecipeLoaders(t, sampleRecipes(), sampleRecipes())
	s := NewRecipeListScreen()
	s.Init()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc should produce a command")
	}
	if _, ok := cmd().(tui.PopScreenMsg); !ok {
		t.Fatalf("expected PopScreenMsg, got %T", cmd())
	}
}

func TestRecipeListScreen_FilterNarrowsList(t *testing.T) {
	withRecipeLoaders(t, sampleRecipes(), sampleRecipes())
	s := NewRecipeListScreen()
	s.Init()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Enter filter mode.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	// Type "sec" — should narrow to security-audit only.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})

	if len(s.filtered) != 1 {
		t.Fatalf("filtered len = %d, want 1", len(s.filtered))
	}
	if s.filtered[0].Name != "security-audit" {
		t.Errorf("filtered[0].Name = %q, want 'security-audit'", s.filtered[0].Name)
	}
}

func TestRecipeListScreen_SourceIdentification(t *testing.T) {
	all := []*recipe.Recipe{
		{Name: "builtin-one", Steps: []recipe.Step{{Name: "x", Command: "y"}}},
		{Name: "user-one", Steps: []recipe.Step{{Name: "x", Command: "y"}}},
	}
	builtin := []*recipe.Recipe{all[0]}
	withRecipeLoaders(t, all, builtin)
	s := NewRecipeListScreen()
	s.Init()

	if s.userRecipes["builtin-one"] {
		t.Errorf("builtin-one should not be marked user")
	}
	if !s.userRecipes["user-one"] {
		t.Errorf("user-one should be marked user")
	}
}

func TestRecipeListScreen_EmptyList(t *testing.T) {
	withRecipeLoaders(t, nil, nil)
	s := NewRecipeListScreen()
	s.Init()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	view := s.View()
	if !strings.Contains(view, "No recipes available") {
		t.Errorf("empty-list view should mention 'No recipes available'; got:\n%s", view)
	}
}

// TestRecipeListScreen_QDoesNotPop is a guard against resurrecting the
// unreachable `case "q":` handler. The app intercepts single-key "q" via
// GlobalKeyMap.Quit before screens see it, so our screen must not attempt
// to pop on "q" — if it did, it would hint at misleading behavior that
// never fires in the real app.
func TestRecipeListScreen_QDoesNotPop(t *testing.T) {
	withRecipeLoaders(t, sampleRecipes(), sampleRecipes())
	s := NewRecipeListScreen()
	s.Init()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		return // no action, as expected
	}
	if _, isPop := cmd().(tui.PopScreenMsg); isPop {
		t.Error("\"q\" must not produce PopScreenMsg at the screen level (app handles quit)")
	}
}

// TestRecipeListScreen_FilterFocusesOnce ensures pressing "/" enters filter
// mode and returns a single non-nil Cmd (the cursor-blink tick), not the
// double-Focus pattern that silently discarded the first return value.
func TestRecipeListScreen_FilterFocusesOnce(t *testing.T) {
	withRecipeLoaders(t, sampleRecipes(), sampleRecipes())
	s := NewRecipeListScreen()
	s.Init()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !s.filtering {
		t.Error("\"/\" should enter filtering mode")
	}
	if cmd == nil {
		t.Error("\"/\" should produce a focus/blink Cmd")
	}
}
