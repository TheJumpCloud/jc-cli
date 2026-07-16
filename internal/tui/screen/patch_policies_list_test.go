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

func overridePatchPoliciesClient(t *testing.T, url string) {
	t.Helper()
	orig := newV2ClientForPatchPolicies
	newV2ClientForPatchPolicies = func() (*api.V2Client, error) {
		c := api.NewV2ClientWithKey("test-key")
		c.BaseURL = url + "/api/v2"
		return c, nil
	}
	t.Cleanup(func() { newV2ClientForPatchPolicies = orig })
}

// TestPatchPolicyTemplates_Pinned is the drift guard the KLA-481
// ticket demands: the 12 template names were verified on the live
// tenant (2026-07-16). If JumpCloud renames one, this inventory —
// not a silently empty screen — is where it surfaces.
func TestPatchPolicyTemplates_Pinned(t *testing.T) {
	if len(patchPolicyTemplates) != 12 {
		t.Fatalf("template family has %d entries, want 12 (deliberate re-curation only)", len(patchPolicyTemplates))
	}
	byOS := map[string]int{}
	for tmpl, os := range patchPolicyTemplates {
		byOS[os]++
		found := false
		for _, known := range patchOSOrder {
			if os == known {
				found = true
			}
		}
		if !found {
			t.Errorf("template %s maps to unknown OS %q", tmpl, os)
		}
	}
	want := map[string]int{"macOS": 6, "iOS": 2, "Windows": 2, "Linux": 1, "Android": 1}
	for os, n := range want {
		if byOS[os] != n {
			t.Errorf("%s templates = %d, want %d", os, byOS[os], n)
		}
	}
}

func TestPatchPoliciesListScreen_FiltersGroupsAndDrills(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/policies":
			_, _ = w.Write([]byte(`[
				{"id":"p1","name":"Win updates","template":{"name":"system_updates_windows"}},
				{"id":"p2","name":"Mac DDM enforce","template":{"name":"software_update_enforcement_specific_darwin_ddm"}},
				{"id":"p3","name":"Not patch","template":{"name":"custom_oma_uri_mdm_windows"}},
				{"id":"p4","name":"Ubuntu updates","template":{"name":"system_update_ubuntu_linux"}}
			]`))
		case "/api/v2/policies/p2":
			_, _ = w.Write([]byte(`{"id":"p2","name":"Mac DDM enforce",
				"values":[
					{"configFieldName":"TargetOSVersion","value":"26.1"},
					{"configFieldName":"details","value":{"nested":true}}
				]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	overridePatchPoliciesClient(t, srv.URL)

	s := NewPatchPoliciesListScreen()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	runCmd(t, s, s.loadCmd())
	if s.loading || s.err != "" {
		t.Fatalf("load failed: %q", s.err)
	}

	view := s.View()
	// Non-patch policy filtered out; groups in fixed OS order
	// (macOS before Windows before Linux).
	if strings.Contains(view, "Not patch") {
		t.Error("non-patch policy leaked into the list")
	}
	for _, want := range []string{"3 OS-update policies", "macOS", "Windows", "Linux", "Mac DDM enforce", "Win updates", "Ubuntu updates"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "macOS") > strings.Index(view, "Windows") {
		t.Error("group order wrong: macOS must precede Windows")
	}

	// Rows are sorted macOS-first, so cursor 0 = Mac DDM enforce.
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	push, ok := cmd().(tui.PushScreenMsg)
	if !ok {
		t.Fatal("enter should push detail")
	}
	detail, ok := push.Screen.(*PatchPolicyDetailScreen)
	if !ok {
		t.Fatalf("pushed %T", push.Screen)
	}
	runCmd(t, detail, detail.loadCmd())
	dview := detail.View()
	for _, want := range []string{"Mac DDM enforce", "TargetOSVersion", "26.1", `{"nested":true}`} {
		if !strings.Contains(dview, want) {
			t.Errorf("detail missing %q:\n%s", want, dview)
		}
	}
}

func TestPatchPoliciesListScreen_EmptyTenant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	overridePatchPoliciesClient(t, srv.URL)

	s := NewPatchPoliciesListScreen()
	runCmd(t, s, s.loadCmd())
	view := s.View()
	if !strings.Contains(view, "No OS-update policies") || !strings.Contains(view, "system_updates_windows") {
		t.Errorf("empty state should guide creation:\n%s", view)
	}
}
