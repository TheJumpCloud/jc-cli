package tui

import "github.com/charmbracelet/bubbles/key"

// GlobalKeys are available on all screens.
type GlobalKeys struct {
	Quit key.Binding
	Back key.Binding
	Help key.Binding
}

var GlobalKeyMap = GlobalKeys{
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
}

// NavigationKeys are used on screens with selectable lists.
type NavigationKeys struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Top    key.Binding
	Bottom key.Binding
}

var NavKeyMap = NavigationKeys{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("k/up", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("j/down", "down"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "open"),
	),
	Top: key.NewBinding(
		key.WithKeys("g", "home"),
		key.WithHelp("g", "top"),
	),
	Bottom: key.NewBinding(
		key.WithKeys("G", "end"),
		key.WithHelp("G", "bottom"),
	),
}

// ListKeys are for the list screen.
type ListKeys struct {
	Filter    key.Binding
	Sort      key.Binding
	SortDir   key.Binding
	Refresh   key.Binding
	AllFields key.Binding
	Copy      key.Binding
	Export    key.Binding
}

var ListKeyMap = ListKeys{
	Filter: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "filter"),
	),
	Sort: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "sort"),
	),
	SortDir: key.NewBinding(
		key.WithKeys("S"),
		key.WithHelp("S", "sort dir"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	AllFields: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "all fields"),
	),
	Copy: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "copy id"),
	),
	Export: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "export"),
	),
}

// DetailKeys are for the detail screen.
type DetailKeys struct {
	Tab       key.Binding
	AllFields key.Binding
	Refresh   key.Binding
	Copy      key.Binding
	Export    key.Binding
}

var DetailKeyMap = DetailKeys{
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "associations"),
	),
	AllFields: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "all fields"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	Copy: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "copy id"),
	),
	Export: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "export"),
	),
}
