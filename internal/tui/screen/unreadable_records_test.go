package screen

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/TheJumpCloud/jc-cli/internal/api"
)

// These tests feed each list screen one good record and one whose field
// has the wrong type, and assert the screen shows the error instead of a
// list or count built from the records that happened to decode.

// serveJSON answers GET path with body and 404s everything else.
func serveJSON(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func overrideAppleMDMPoliciesClient(t *testing.T, url string) {
	t.Helper()
	orig := newV2ClientForMDMPolicies
	newV2ClientForMDMPolicies = func() (*api.V2Client, error) {
		c := api.NewV2ClientWithKey("test-key")
		c.BaseURL = url + "/api/v2"
		return c, nil
	}
	t.Cleanup(func() { newV2ClientForMDMPolicies = orig })
}

func TestMFAOverviewScreen_UnreadableUserRecordShowsError(t *testing.T) {
	srv := serveJSON(t, map[string]string{
		"/organizations":       `{"totalCount":1,"results":[{"id":"org-1"}]}`,
		"/organizations/org-1": `{"id":"org-1","settings":{"requireAdminMFA":true}}`,
		"/systemusers": `{"totalCount":2,"results":[
			{"username":"alice","mfa":{"exclusion":false},"mfaEnrollment":{"overallStatus":"ENROLLED"}},
			{"username":"bob","mfa":{"exclusion":"no"},"mfaEnrollment":{"overallStatus":"NOT_ENROLLED"}}
		]}`,
	})
	overrideMFAOverviewClient(t, srv.URL)

	s := NewMFAOverviewScreen()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	runCmd(t, s, s.loadCmd())
	if !strings.Contains(s.err, "user 2 of 2 did not decode") {
		t.Fatalf("err = %q, want the unreadable user named", s.err)
	}
	if s.data.TotalUsers != 0 || s.data.Enrolled != 0 {
		t.Errorf("coverage must not be computed from fewer users: %+v", s.data)
	}
	view := s.View()
	if !strings.Contains(view, "Error:") || strings.Contains(view, "alice") {
		t.Errorf("view must show the error, not a coverage figure:\n%s", view)
	}
}

func TestAppleMDMPoliciesListScreen_UnreadablePolicyShowsError(t *testing.T) {
	srv := serveJSON(t, map[string]string{
		"/api/v2/policies": `[
			{"id":"p1","name":"Wi-Fi","template":{"name":"custom_mdm_profile_darwin"}},
			{"id":"p2","name":123,"template":{"name":"custom_mdm_profile_darwin"}}
		]`,
	})
	overrideAppleMDMPoliciesClient(t, srv.URL)

	s := NewAppleMDMPoliciesListScreen()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	s.Update(s.loadCmd()())
	if !strings.Contains(s.err, "policy 2 of 2 did not decode") {
		t.Fatalf("err = %q, want the unreadable policy named", s.err)
	}
	if len(s.all) != 0 {
		t.Errorf("list must not be presented from fewer policies: %+v", s.all)
	}
	view := s.View()
	if !strings.Contains(view, "Error loading policies") || strings.Contains(view, "Wi-Fi") {
		t.Errorf("view must show the error, not a list:\n%s", view)
	}
}

func TestWindowsMDMPoliciesListScreen_UnreadablePolicyShowsError(t *testing.T) {
	srv := serveJSON(t, map[string]string{
		"/api/v2/policies": `[
			{"id":"pol-oma","name":"Camera lockdown","template":{"name":"custom_oma_uri_mdm_windows"}},
			{"id":"pol-reg","name":123,"template":{"name":"custom_registry_keys_policy_windows"}}
		]`,
	})
	stubWindowsMDMClient(t, srv.URL)

	s := NewWindowsMDMPoliciesListScreen()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	s.Update(s.loadCmd()())
	if !strings.Contains(s.err, "policy 2 of 2 did not decode") {
		t.Fatalf("err = %q, want the unreadable policy named", s.err)
	}
	if len(s.all) != 0 {
		t.Errorf("list must not be presented from fewer policies: %+v", s.all)
	}
	view := s.View()
	if !strings.Contains(view, "Error loading policies") || strings.Contains(view, "Camera lockdown") {
		t.Errorf("view must show the error, not a list:\n%s", view)
	}
}

func TestPatchPoliciesListScreen_UnreadablePolicyShowsError(t *testing.T) {
	srv := serveJSON(t, map[string]string{
		"/api/v2/policies": `[
			{"id":"p1","name":"Win updates","template":{"name":"system_updates_windows"}},
			{"id":"p2","name":123,"template":{"name":"system_update_ubuntu_linux"}}
		]`,
	})
	overridePatchPoliciesClient(t, srv.URL)

	s := NewPatchPoliciesListScreen()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	runCmd(t, s, s.loadCmd())
	if !strings.Contains(s.err, "policy 2 of 2 did not decode") {
		t.Fatalf("err = %q, want the unreadable policy named", s.err)
	}
	if len(s.rows) != 0 {
		t.Errorf("list must not be presented from fewer policies: %+v", s.rows)
	}
	view := s.View()
	if !strings.Contains(view, "Error:") || strings.Contains(view, "Win updates") {
		t.Errorf("view must show the error, not a list:\n%s", view)
	}
}

func TestDirectoriesListScreen_UnreadableDirectoryShowsError(t *testing.T) {
	// The screen decodes each record into a map, so a wrong-typed field
	// still decodes; the drift that cannot be read is a record that is
	// not an object at all.
	srv := serveJSON(t, map[string]string{
		"/api/v2/directories": `[
			{"id":"d1","type":"g_suite","name":"Workspace"},
			"d2"
		]`,
	})
	overrideDirectoriesClient(t, srv.URL)

	s := NewDirectoriesListScreen()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	runCmd(t, s, s.loadCmd())
	if !strings.Contains(s.err, "directory 2 of 2 did not decode") {
		t.Fatalf("err = %q, want the unreadable directory named", s.err)
	}
	if len(s.rows) != 0 {
		t.Errorf("list must not be presented from fewer directories: %+v", s.rows)
	}
	view := s.View()
	if !strings.Contains(view, "Error:") || strings.Contains(view, "Workspace") {
		t.Errorf("view must show the error, not a list:\n%s", view)
	}
}
