package screen

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/recipe"
	"github.com/klaassen-consulting/jc/internal/tui"
)

func TestRecipeRunScreen_PlanModeRendersPreview(t *testing.T) {
	r := &recipe.Recipe{
		Name: "t",
		Steps: []recipe.Step{
			{Name: "one", Command: "users list"},
			{Name: "two", Command: "devices list"},
		},
	}
	s := NewRecipeRunScreen(r, nil, true)
	s.Init()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	view := s.View()
	if !strings.Contains(view, "Plan: t") {
		t.Errorf("plan view should show 'Plan: t'; got:\n%s", view)
	}
	if !strings.Contains(view, "users list") {
		t.Errorf("plan view should include step commands; got:\n%s", view)
	}
	if !s.done {
		t.Error("plan mode should be marked done immediately")
	}
}

func TestRecipeRunScreen_DispatcherNotConfigured(t *testing.T) {
	// Ensure dispatcher is nil.
	orig := RecipeDispatcher
	RecipeDispatcher = nil
	t.Cleanup(func() { RecipeDispatcher = orig })

	r := &recipe.Recipe{Name: "t", Steps: []recipe.Step{{Name: "x", Command: "y"}}}
	s := NewRecipeRunScreen(r, nil, false)
	s.Init()
	if !strings.Contains(s.err, "dispatcher not configured") {
		t.Errorf("expected dispatcher error, got: %q", s.err)
	}
	if !s.done {
		t.Error("should be marked done when dispatcher missing")
	}
}

func TestRecipeRunScreen_ApplyProgressLineUpdatesStatus(t *testing.T) {
	r := &recipe.Recipe{
		Name: "t",
		Steps: []recipe.Step{
			{Name: "alpha", Command: "a"},
			{Name: "beta", Command: "b"},
			{Name: "gamma", Command: "c"},
		},
	}
	s := NewRecipeRunScreen(r, nil, false)

	s.applyProgressLine("[1/3] alpha... done")
	if s.steps[0].status != "done" {
		t.Errorf("step 0 = %q, want 'done'", s.steps[0].status)
	}
	if s.steps[1].status != "running" {
		t.Errorf("step 1 should advance to 'running', got %q", s.steps[1].status)
	}

	s.applyProgressLine("[2/3] beta... skipped")
	if s.steps[1].status != "skipped" {
		t.Errorf("step 1 = %q, want 'skipped'", s.steps[1].status)
	}
	if s.steps[2].status != "running" {
		t.Errorf("step 2 should advance to 'running', got %q", s.steps[2].status)
	}

	s.applyProgressLine("[3/3] gamma... failed")
	if s.steps[2].status != "failed" {
		t.Errorf("step 2 = %q, want 'failed'", s.steps[2].status)
	}
}

func TestRecipeRunScreen_ApplyProgressLineIgnoresGarbage(t *testing.T) {
	r := &recipe.Recipe{Name: "t", Steps: []recipe.Step{{Name: "x", Command: "y"}}}
	s := NewRecipeRunScreen(r, nil, false)

	// Malformed line shouldn't change state or panic.
	s.applyProgressLine("not a progress line")
	s.applyProgressLine("[99/1] out-of-range... done") // idx out of range
	if s.steps[0].status != "pending" {
		t.Errorf("step 0 = %q, want 'pending'", s.steps[0].status)
	}
}

func TestRecipeRunScreen_EscPops(t *testing.T) {
	r := &recipe.Recipe{Name: "t", Steps: []recipe.Step{{Name: "x", Command: "y"}}}
	s := NewRecipeRunScreen(r, nil, true)
	s.Init()

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should produce a command")
	}
	if _, ok := cmd().(tui.PopScreenMsg); !ok {
		t.Fatalf("expected PopScreenMsg, got %T", cmd())
	}
}

func TestRecipeRunScreen_EnterPopsWhenDone(t *testing.T) {
	r := &recipe.Recipe{Name: "t", Steps: []recipe.Step{{Name: "x", Command: "y"}}}
	s := NewRecipeRunScreen(r, nil, true) // plan mode marks done=true
	s.Init()

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should produce a command when done")
	}
	if _, ok := cmd().(tui.PopScreenMsg); !ok {
		t.Fatalf("expected PopScreenMsg, got %T", cmd())
	}
}

func TestRecipeRunScreen_DoneMsgSetsResult(t *testing.T) {
	r := &recipe.Recipe{Name: "t", Steps: []recipe.Step{{Name: "x", Command: "y"}}}
	s := NewRecipeRunScreen(r, nil, false)
	// Skip Init so we don't kick off a goroutine that has no dispatcher.
	s.Update(recipeDoneMsg{
		result: &recipe.ExecutionResult{Recipe: "t", Status: "success", Message: "all good"},
	})

	if !s.done {
		t.Error("done flag not set")
	}
	if s.result == nil || s.result.Message != "all good" {
		t.Errorf("result.Message = %v", s.result)
	}
	view := s.View()
	if !strings.Contains(view, "all good") {
		t.Errorf("view should show completion message; got:\n%s", view)
	}
}
