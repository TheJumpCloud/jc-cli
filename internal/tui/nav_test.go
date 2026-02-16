package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// mockScreen is a minimal Screen implementation for testing.
type mockScreen struct {
	title string
}

func (m *mockScreen) Title() string                           { return m.title }
func (m *mockScreen) Init() tea.Cmd                           { return nil }
func (m *mockScreen) Update(tea.Msg) (tea.Model, tea.Cmd)     { return m, nil }
func (m *mockScreen) View() string                            { return m.title }

func TestNavStack_PushAndCurrent(t *testing.T) {
	nav := &NavStack{}
	s1 := &mockScreen{title: "Home"}
	nav.Push(s1)

	if nav.Current() != s1 {
		t.Error("Current should return pushed screen")
	}
	if nav.Depth() != 1 {
		t.Errorf("Depth = %d, want 1", nav.Depth())
	}
}

func TestNavStack_PushMultiple(t *testing.T) {
	nav := &NavStack{}
	s1 := &mockScreen{title: "Home"}
	s2 := &mockScreen{title: "Users"}
	s3 := &mockScreen{title: "jdoe"}

	nav.Push(s1)
	nav.Push(s2)
	nav.Push(s3)

	if nav.Current().Title() != "jdoe" {
		t.Errorf("Current = %q, want 'jdoe'", nav.Current().Title())
	}
	if nav.Depth() != 3 {
		t.Errorf("Depth = %d, want 3", nav.Depth())
	}
}

func TestNavStack_Pop(t *testing.T) {
	nav := &NavStack{}
	s1 := &mockScreen{title: "Home"}
	s2 := &mockScreen{title: "Users"}

	nav.Push(s1)
	nav.Push(s2)

	popped := nav.Pop()
	if popped.Title() != "Users" {
		t.Errorf("Pop returned %q, want 'Users'", popped.Title())
	}
	if nav.Current().Title() != "Home" {
		t.Errorf("Current after pop = %q, want 'Home'", nav.Current().Title())
	}
}

func TestNavStack_PopLastReturnsNil(t *testing.T) {
	nav := &NavStack{}
	nav.Push(&mockScreen{title: "Home"})

	popped := nav.Pop()
	if popped != nil {
		t.Error("Pop on single-item stack should return nil")
	}
	if nav.Depth() != 1 {
		t.Errorf("Depth = %d, want 1 after failed pop", nav.Depth())
	}
}

func TestNavStack_Breadcrumbs(t *testing.T) {
	nav := &NavStack{}
	nav.Push(&mockScreen{title: "Home"})
	nav.Push(&mockScreen{title: "Users"})
	nav.Push(&mockScreen{title: "jdoe"})

	crumbs := nav.Breadcrumbs()
	want := []string{"Home", "Users", "jdoe"}
	if len(crumbs) != len(want) {
		t.Fatalf("Breadcrumbs = %v, want %v", crumbs, want)
	}
	for i, c := range crumbs {
		if c != want[i] {
			t.Errorf("Breadcrumbs[%d] = %q, want %q", i, c, want[i])
		}
	}
}

func TestNavStack_Replace(t *testing.T) {
	nav := &NavStack{}
	nav.Push(&mockScreen{title: "Home"})
	nav.Push(&mockScreen{title: "Users"})

	nav.Replace(&mockScreen{title: "Devices"})
	if nav.Current().Title() != "Devices" {
		t.Errorf("Current = %q, want 'Devices'", nav.Current().Title())
	}
	if nav.Depth() != 2 {
		t.Errorf("Depth = %d, want 2 after replace", nav.Depth())
	}
}

func TestNavStack_EmptyStack(t *testing.T) {
	nav := &NavStack{}
	if nav.Current() != nil {
		t.Error("Current on empty stack should return nil")
	}
	if nav.Depth() != 0 {
		t.Errorf("Depth = %d, want 0", nav.Depth())
	}
	crumbs := nav.Breadcrumbs()
	if len(crumbs) != 0 {
		t.Errorf("Breadcrumbs = %v, want empty", crumbs)
	}
}
