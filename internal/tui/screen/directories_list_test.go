package screen

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/tui"
)

func overrideDirectoriesClient(t *testing.T, url string) {
	t.Helper()
	orig := newV2ClientForDirectories
	newV2ClientForDirectories = func() (*api.V2Client, error) {
		c := api.NewV2ClientWithKey("test-key")
		c.BaseURL = url + "/api/v2"
		return c, nil
	}
	t.Cleanup(func() { newV2ClientForDirectories = orig })
}

func TestDirectoriesListScreen_HealthAndDrill(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v2/directories" {
			_, _ = w.Write([]byte(`[
				{"id":"d1","type":"g_suite","name":"Workspace"},
				{"id":"d2","type":"office_365","name":"Broken O365",
				 "oAuthStatus":{"error":"invalid_grant","errorMessage":"AADSTS9002313: Invalid request"}}
			]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	overrideDirectoriesClient(t, srv.URL)

	s := NewDirectoriesListScreen()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	runCmd(t, s, s.loadCmd())
	if s.loading || s.err != "" {
		t.Fatalf("load failed: loading=%v err=%q", s.loading, s.err)
	}

	view := s.View()
	for _, want := range []string{
		"2 integrations — 1 with OAuth errors",
		"Workspace", "Broken O365",
		"error: invalid_grant",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}

	// Drill into the broken one: detail carries the full error + the
	// Admin Portal remediation pointer.
	s.cursor = 1
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should push detail")
	}
	push, ok := cmd().(tui.PushScreenMsg)
	if !ok {
		t.Fatal("want PushScreenMsg")
	}
	detail, ok := push.Screen.(*DirectoryDetailScreen)
	if !ok {
		t.Fatalf("pushed %T", push.Screen)
	}
	dview := detail.View()
	for _, want := range []string{"Broken O365", "invalid_grant", "re-authorize", "AADSTS9002313"} {
		if !strings.Contains(dview, want) {
			t.Errorf("detail missing %q:\n%s", want, dview)
		}
	}
}

func TestDirectoriesListScreen_LoadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	t.Cleanup(srv.Close)
	overrideDirectoriesClient(t, srv.URL)

	s := NewDirectoriesListScreen()
	runCmd(t, s, s.loadCmd())
	if s.err == "" {
		t.Fatal("load error should surface")
	}
	if !strings.Contains(s.View(), "r retry") {
		t.Errorf("error view missing retry hint:\n%s", s.View())
	}
}
