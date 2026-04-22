package screen

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/recipe"
	"github.com/klaassen-consulting/jc/internal/tui"
)

func TestRecipeParamFormScreen_TitleIncludesRecipe(t *testing.T) {
	r := &recipe.Recipe{Name: "onboard-user", Steps: []recipe.Step{{Name: "x", Command: "y"}}}
	s := NewRecipeParamFormScreen(r)
	if s.Title() != "Run: onboard-user" {
		t.Errorf("Title = %q", s.Title())
	}
}

func TestRecipeParamFormScreen_RendersFieldsWithDefaults(t *testing.T) {
	r := &recipe.Recipe{
		Name: "test",
		Parameters: []recipe.Parameter{
			{Name: "username", Required: true, Description: "user to create"},
			{Name: "group", Default: "Engineering"},
		},
		Steps: []recipe.Step{{Name: "x", Command: "y"}},
	}
	s := NewRecipeParamFormScreen(r)
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := s.View()

	if !strings.Contains(view, "username") {
		t.Errorf("view should contain 'username'; got:\n%s", view)
	}
	if !strings.Contains(view, "group") {
		t.Errorf("view should contain 'group'; got:\n%s", view)
	}
	// Default should be pre-populated in the input.
	if s.fields[1].input.Value() != "Engineering" {
		t.Errorf("group default not applied: %q", s.fields[1].input.Value())
	}
}

func TestRecipeParamFormScreen_RequiredValidation(t *testing.T) {
	r := &recipe.Recipe{
		Name:       "test",
		Parameters: []recipe.Parameter{{Name: "username", Required: true}},
		Steps:      []recipe.Step{{Name: "x", Command: "y"}},
	}
	s := NewRecipeParamFormScreen(r)
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Submit without filling in the required field.
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		// Submit should NOT push a screen when validation fails.
		if _, ok := cmd().(tui.PushScreenMsg); ok {
			t.Fatal("submit should not push when required param missing")
		}
	}
	if !strings.Contains(s.err, "missing required") {
		t.Errorf("err should mention 'missing required', got: %q", s.err)
	}
}

func TestRecipeParamFormScreen_IntTypeValidation(t *testing.T) {
	r := &recipe.Recipe{
		Name:       "test",
		Parameters: []recipe.Parameter{{Name: "days", Type: "int", Default: "7"}},
		Steps:      []recipe.Step{{Name: "x", Command: "y"}},
	}
	s := NewRecipeParamFormScreen(r)
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Replace default with a non-integer.
	s.fields[0].input.SetValue("abc")
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if _, ok := cmd().(tui.PushScreenMsg); ok {
			t.Fatal("submit should not push when int validation fails")
		}
	}
	if !strings.Contains(s.err, "must be an integer") {
		t.Errorf("err should mention int validation, got: %q", s.err)
	}
}

func TestRecipeParamFormScreen_SubmitPushesRunScreen(t *testing.T) {
	r := &recipe.Recipe{
		Name:       "test",
		Parameters: []recipe.Parameter{{Name: "username", Default: "jdoe"}},
		Steps:      []recipe.Step{{Name: "x", Command: "users get {{ .username }}"}},
	}
	s := NewRecipeParamFormScreen(r)
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should produce a command on valid submit")
	}
	msg := cmd()
	push, ok := msg.(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("expected PushScreenMsg, got %T", msg)
	}
	if !strings.HasPrefix(push.Screen.Title(), "Run: ") {
		t.Errorf("pushed title = %q, want prefix 'Run: '", push.Screen.Title())
	}
}

func TestRecipeParamFormScreen_TabCyclesFocus(t *testing.T) {
	r := &recipe.Recipe{
		Name: "test",
		Parameters: []recipe.Parameter{
			{Name: "a"}, {Name: "b"}, {Name: "c"},
		},
		Steps: []recipe.Step{{Name: "x", Command: "y"}},
	}
	s := NewRecipeParamFormScreen(r)
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if s.focused != 0 {
		t.Errorf("initial focus = %d, want 0", s.focused)
	}
	s.Update(tea.KeyMsg{Type: tea.KeyTab})
	if s.focused != 1 {
		t.Errorf("after tab focus = %d, want 1", s.focused)
	}
	s.Update(tea.KeyMsg{Type: tea.KeyTab})
	s.Update(tea.KeyMsg{Type: tea.KeyTab})
	if s.focused != 0 {
		t.Errorf("after 3 tabs focus = %d, want 0 (wrapped)", s.focused)
	}
}
