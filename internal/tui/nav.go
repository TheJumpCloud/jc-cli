package tui

import tea "github.com/charmbracelet/bubbletea"

// Screen is a navigable TUI view.
type Screen interface {
	tea.Model
	Title() string
}

// HelpProvider is an optional interface a Screen can implement to contribute
// its own keys to the status bar.
//
// The bar was a fixed string chosen only by nav depth, so a key that exists on
// one screen — n to create on a list, t for an AD's translation rules — was
// never advertised anywhere the operator looks. Screens that put a hint in
// their own body were working around that, and only the ones that remembered.
type HelpProvider interface {
	// HelpKeys returns "key:action" pairs for this screen, already joined.
	// Empty means the screen adds nothing.
	HelpKeys() string
}

// NavStack manages a stack of screens with browser-like navigation.
type NavStack struct {
	screens []Screen
}

// Push adds a new screen to the stack.
func (n *NavStack) Push(s Screen) {
	n.screens = append(n.screens, s)
}

// Pop removes the top screen and returns it. Returns nil if only one screen remains.
func (n *NavStack) Pop() Screen {
	if len(n.screens) <= 1 {
		return nil
	}
	top := n.screens[len(n.screens)-1]
	n.screens = n.screens[:len(n.screens)-1]
	return top
}

// Current returns the active screen, or nil if the stack is empty.
func (n *NavStack) Current() Screen {
	if len(n.screens) == 0 {
		return nil
	}
	return n.screens[len(n.screens)-1]
}

// Replace replaces the current screen with a new one.
func (n *NavStack) Replace(s Screen) {
	if len(n.screens) == 0 {
		n.screens = append(n.screens, s)
		return
	}
	n.screens[len(n.screens)-1] = s
}

// Breadcrumbs returns the title trail from the stack.
func (n *NavStack) Breadcrumbs() []string {
	titles := make([]string, len(n.screens))
	for i, s := range n.screens {
		titles[i] = s.Title()
	}
	return titles
}

// Depth returns the number of screens on the stack.
func (n *NavStack) Depth() int {
	return len(n.screens)
}

// TextInputScreen is an optional interface for screens with text input.
// When TextInputActive returns true, the app skips single-key shortcuts
// (q to quit, ? for help) so they can be typed into form fields.
type TextInputScreen interface {
	TextInputActive() bool
}

// Navigation messages for the app to handle.

// PushScreenMsg tells the app to push a new screen.
type PushScreenMsg struct {
	Screen Screen
}

// PopScreenMsg tells the app to go back.
type PopScreenMsg struct{}

// ReplaceScreenMsg tells the app to replace the current screen.
type ReplaceScreenMsg struct {
	Screen Screen
}

// FlashMsg shows a transient message in the status bar.
type FlashMsg struct {
	Text string
}

// ClearFlashMsg clears the flash message.
type ClearFlashMsg struct{}

// RefreshListMsg tells a list screen to re-fetch its data after a mutation.
type RefreshListMsg struct{}
