package screen

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/bundle"
	"github.com/klaassen-consulting/jc/internal/tui"
)

// isolateBundlesDirScreen keeps the developer's real user-bundles dir
// out of screen tests.
func isolateBundlesDirScreen(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	orig := bundle.BundlesDir
	bundle.BundlesDir = func() string { return dir }
	t.Cleanup(func() { bundle.BundlesDir = orig })
}

func overrideBundlesClient(t *testing.T, url string) {
	t.Helper()
	orig := newV2ClientForBundles
	newV2ClientForBundles = func() (*api.V2Client, error) {
		c := api.NewV2ClientWithKey("test-key")
		c.BaseURL = url + "/api/v2"
		return c, nil
	}
	t.Cleanup(func() { newV2ClientForBundles = orig })
}

// startBundleTUITenant stubs everything the apply + status screens
// touch. mutations records POSTs in order.
func startBundleTUITenant(t *testing.T, mutations *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == "/api/v2/policytemplates" && r.Method == "GET":
			filter := r.URL.Query().Get("filter")
			switch {
			case strings.Contains(filter, "custom_oma_uri_mdm_windows"):
				_, _ = w.Write([]byte(`[{"id":"tmpl-oma","name":"custom_oma_uri_mdm_windows"}]`))
			case strings.Contains(filter, "custom_registry_keys_policy_windows"):
				_, _ = w.Write([]byte(`[{"id":"tmpl-reg","name":"custom_registry_keys_policy_windows"}]`))
			default:
				_, _ = w.Write([]byte(`[]`))
			}
		case path == "/api/v2/policytemplates/tmpl-oma":
			_, _ = w.Write([]byte(`{"id":"tmpl-oma","name":"custom_oma_uri_mdm_windows","configFields":[{"id":"urifid","name":"uriList"}]}`))
		case path == "/api/v2/policytemplates/tmpl-reg":
			_, _ = w.Write([]byte(`{"id":"tmpl-reg","name":"custom_registry_keys_policy_windows","configFields":[{"id":"regfid","name":"customRegTable"}]}`))
		case path == "/api/v2/policies" && r.Method == "GET":
			_, _ = w.Write([]byte(`[]`))
		case path == "/api/v2/policygroups" && r.Method == "GET":
			_, _ = w.Write([]byte(`[]`))
		case path == "/api/v2/policies" && r.Method == "POST":
			var body struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			*mutations = append(*mutations, "policy:"+body.Name)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": fmt.Sprintf("pol-%d", len(*mutations)), "name": body.Name})
		case path == "/api/v2/policygroups" && r.Method == "POST":
			*mutations = append(*mutations, "group")
			_, _ = w.Write([]byte(`{"id":"pg-1"}`))
		case strings.HasSuffix(path, "/members") && r.Method == "POST":
			*mutations = append(*mutations, "member")
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// testTUIBundle is a small windows-only bundle so apply avoids the
// Apple catalog dependency in these screen tests.
func testTUIBundle(t *testing.T) *bundle.Bundle {
	t.Helper()
	b, err := bundle.Parse([]byte(`
name: tui-test
version: "1.0"
policies:
  - name: Camera off
    type: windows_oma_uri
    description: WN11-TEST-000001
    settings:
      - {uri: ./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera, format: int, value: "0"}
`))
	if err != nil {
		t.Fatal(err)
	}
	b.Source = &bundle.Source{Origin: "user", Attribution: "test content"}
	return b
}

// runCmd executes a tea.Cmd chain synchronously, feeding each result
// back into the screen, and returns the final model. Spinner ticks
// are dropped instead of fed back — following them would loop forever
// (each TickMsg's Update returns the next tick cmd).
func runCmd(t *testing.T, m tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		if _, isTick := msg.(spinner.TickMsg); isTick {
			break
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				m = runCmd(t, m, c)
			}
			return m
		}
		m, cmd = m.Update(msg)
	}
	return m
}

func TestBundlesListScreen_ShowsBuiltinsAndDrills(t *testing.T) {
	isolateBundlesDirScreen(t)
	s := NewBundlesListScreen()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	view := s.View()
	for _, want := range []string{"example-baseline", "macos-cis-lvl1", "macos-cis-lvl2", "windows-stig-cat1"} {
		if !strings.Contains(view, want) {
			t.Errorf("list view missing %q:\n%s", want, view)
		}
	}

	// Enter pushes the detail screen for the selected bundle.
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should push detail")
	}
	push, ok := cmd().(tui.PushScreenMsg)
	if !ok {
		t.Fatalf("enter should produce PushScreenMsg")
	}
	if _, ok := push.Screen.(*BundleDetailScreen); !ok {
		t.Fatalf("pushed screen is %T, want *BundleDetailScreen", push.Screen)
	}
}

func TestBundleDetailScreen_RendersProvenanceAndUnits(t *testing.T) {
	isolateBundlesDirScreen(t)
	builtins, err := bundle.LoadBuiltIn()
	if err != nil {
		t.Fatal(err)
	}
	b := bundle.FindByName(builtins, "windows-stig-cat1")
	if b == nil {
		t.Fatal("windows-stig-cat1 missing")
	}

	s := NewBundleDetailScreen(b)
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := s.View()
	for _, want := range []string{
		"windows-stig-cat1", "DISA", "public domain", // provenance
		"Anonymous access and NTLM hardening", "5 registry key(s)", // units
		"WN11-SO-000145", // per-unit STIG ids
		"a apply · s status",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("detail view missing %q", want)
		}
	}
}

func TestBundleApplyScreen_PlanThenExecute(t *testing.T) {
	isolateBundlesDirScreen(t)
	var mutations []string
	srv := startBundleTUITenant(t, &mutations)
	overrideBundlesClient(t, srv.URL)

	s := NewBundleApplyScreen(testTUIBundle(t))
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Stage 1: skip the device group (empty input), Enter → plan.
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should start planning")
	}
	runCmd(t, s, cmd)
	if s.stage != bundleApplyStagePlan {
		t.Fatalf("stage = %v, err = %q", s.stage, s.err)
	}
	if len(mutations) != 0 {
		t.Fatalf("planning must not write: %v", mutations)
	}
	view := s.View()
	if !strings.Contains(view, "tui-test/Camera off") || !strings.Contains(view, "nothing created yet") {
		t.Errorf("plan view wrong:\n%s", view)
	}

	// Stage 2: y → execute.
	_, cmd = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	runCmd(t, s, cmd)
	if s.stage != bundleApplyStageDone || s.err != "" {
		t.Fatalf("stage = %v, err = %q", s.stage, s.err)
	}
	// policy + group + member = 3 writes.
	if len(mutations) != 3 || mutations[0] != "policy:tui-test/Camera off" {
		t.Errorf("mutations = %v", mutations)
	}
	if !strings.Contains(s.View(), "2 objects created") {
		t.Errorf("done view wrong:\n%s", s.View())
	}
}

func TestBundleApplyScreen_EscCancelsAtPlan(t *testing.T) {
	isolateBundlesDirScreen(t)
	var mutations []string
	srv := startBundleTUITenant(t, &mutations)
	overrideBundlesClient(t, srv.URL)

	s := NewBundleApplyScreen(testTUIBundle(t))
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	runCmd(t, s, cmd)
	if s.stage != bundleApplyStagePlan {
		t.Fatalf("stage = %v (%s)", s.stage, s.err)
	}
	_, cmd = s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc at plan should pop")
	}
	if len(mutations) != 0 {
		t.Fatalf("cancelled apply must not write: %v", mutations)
	}
}

func TestBundleStatusScreen_RendersDrift(t *testing.T) {
	isolateBundlesDirScreen(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v2/policygroups":
			_, _ = w.Write([]byte(`[{"id":"pg-1","name":"renamed by hand","description":"bundle:tui-test@1.0"}]`))
		case r.URL.Path == "/api/v2/policygroups/pg-1/members":
			_, _ = w.Write([]byte(`[{"to":{"id":"pol-1","type":"policy"}}]`))
		case r.URL.Path == "/api/v2/policies/pol-1":
			_, _ = w.Write([]byte(`{"id":"pol-1","name":"tui-test/Camera off",
				"template":{"name":"custom_oma_uri_mdm_windows"},
				"values":[{"configFieldName":"uriList","value":[
					{"uri":"./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera","format":"int","value":"1"}
				]}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	overrideBundlesClient(t, srv.URL)

	s := NewBundleStatusScreen(testTUIBundle(t))
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	runCmd(t, s, s.statusCmd())
	if s.loading || s.err != "" {
		t.Fatalf("status failed: loading=%v err=%q", s.loading, s.err)
	}
	view := s.View()
	for _, want := range []string{
		"renamed by hand", "provenance marker", // rename-proof lookup
		"drifted", `tenant int="1", bundle int="0"`, // value-level diff
		"Drift detected.",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("drift view missing %q:\n%s", want, view)
		}
	}
}
