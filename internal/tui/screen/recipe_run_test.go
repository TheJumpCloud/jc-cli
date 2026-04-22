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

func TestRecipeRunScreen_ShowsStepOutput(t *testing.T) {
	r := &recipe.Recipe{
		Name: "audit",
		Steps: []recipe.Step{
			{Name: "list-devices", Command: "devices list -t"},
		},
	}
	s := NewRecipeRunScreen(r, nil, false)
	// Mark the step done and inject a done message with captured output.
	s.steps[0].status = "done"
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	s.Update(recipeDoneMsg{
		result: &recipe.ExecutionResult{
			Recipe: "audit",
			Status: "success",
			Steps: []recipe.StepResult{
				{Name: "list-devices", Status: "success",
					Output: "HOSTNAME  OS\nfoo       Mac\nbar       Windows\n"},
			},
			Message: "done",
		},
	})

	view := s.View()
	if !strings.Contains(view, "HOSTNAME") {
		t.Errorf("view should contain captured output header 'HOSTNAME'; got:\n%s", view)
	}
	if !strings.Contains(view, "foo") || !strings.Contains(view, "bar") {
		t.Errorf("view should contain output rows; got:\n%s", view)
	}
}

func TestRecipeRunScreen_ScrollOffsetJKMoves(t *testing.T) {
	// Build an output large enough to force scrolling.
	var many []string
	for i := 0; i < 50; i++ {
		many = append(many, "line"+string(rune('A'+i%26)))
	}
	longOutput := strings.Join(many, "\n")

	r := &recipe.Recipe{
		Name:  "t",
		Steps: []recipe.Step{{Name: "s", Command: "c"}},
	}
	s := NewRecipeRunScreen(r, nil, false)
	s.steps[0].status = "done"
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 20}) // viewport ~16 lines
	s.Update(recipeDoneMsg{
		result: &recipe.ExecutionResult{
			Recipe: "t",
			Status: "success",
			Steps:  []recipe.StepResult{{Name: "s", Status: "success", Output: longOutput}},
		},
	})

	initial := s.scrollOffset
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if s.scrollOffset != initial+1 {
		t.Errorf("after j, scrollOffset = %d, want %d", s.scrollOffset, initial+1)
	}
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if s.scrollOffset != initial {
		t.Errorf("after k, scrollOffset = %d, want %d", s.scrollOffset, initial)
	}
	// Scroll to bottom with G, then ensure offset is clamped (View calls clampScroll).
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	_ = s.View() // triggers clampScroll
	if s.scrollOffset > 50 {
		t.Errorf("scrollOffset after G+View should be clamped; got %d", s.scrollOffset)
	}
	// Back to top with g.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if s.scrollOffset != 0 {
		t.Errorf("after g, scrollOffset = %d, want 0", s.scrollOffset)
	}
}

func TestRecipeRunScreen_SkippedStepHasNoOutput(t *testing.T) {
	r := &recipe.Recipe{
		Name: "t",
		Steps: []recipe.Step{
			{Name: "conditional", Command: "c", When: "{{ .flag }}"},
		},
	}
	s := NewRecipeRunScreen(r, nil, false)
	s.steps[0].status = "skipped"
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	s.Update(recipeDoneMsg{
		result: &recipe.ExecutionResult{
			Recipe: "t",
			Status: "success",
			Steps:  []recipe.StepResult{{Name: "conditional", Status: "skipped"}},
		},
	})

	view := s.View()
	if !strings.Contains(view, "conditional") {
		t.Errorf("view should still list skipped step name; got:\n%s", view)
	}
}
