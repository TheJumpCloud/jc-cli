package component

import (
	"strings"
	"testing"
)

func TestStatusBar_FlashOverridesHelp(t *testing.T) {
	sb := StatusBar{
		Breadcrumbs: []string{"Home"},
		Help:        "q:quit",
		Flash:       "Copied: abc123",
		Width:       80,
	}

	view := sb.View()
	if !strings.Contains(view, "Copied: abc123") {
		t.Error("view should show flash message")
	}
}

func TestStatusBar_NoFlashShowsHelp(t *testing.T) {
	sb := StatusBar{
		Breadcrumbs: []string{"Home"},
		Help:        "q:quit",
		Width:       80,
	}

	view := sb.View()
	if !strings.Contains(view, "q:quit") {
		t.Error("view should show help when no flash")
	}
}

func TestStatusBar_LoadingOverridesHelp(t *testing.T) {
	sb := StatusBar{
		Breadcrumbs: []string{"Home"},
		Help:        "q:quit",
		Loading:     true,
		Width:       80,
	}

	view := sb.View()
	if !strings.Contains(view, "Loading...") {
		t.Error("view should show loading indicator")
	}
}

func TestStatusBar_FlashOverridesLoading(t *testing.T) {
	sb := StatusBar{
		Breadcrumbs: []string{"Home"},
		Help:        "q:quit",
		Flash:       "Done!",
		Loading:     true,
		Width:       80,
	}

	view := sb.View()
	if !strings.Contains(view, "Done!") {
		t.Error("flash should take priority over loading")
	}
	if strings.Contains(view, "Loading...") {
		t.Error("loading should not show when flash is active")
	}
}
