package screen

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// startWindowsPoliciesListServer stubs GET /policies (list) + GET
// /policies/{id} (drill-in) with a mixed tenant: one OMA-URI policy,
// one registry policy, one Apple policy (must be filtered out), one
// built-in (must be filtered out).
func startWindowsPoliciesListServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/policies":
			_, _ = w.Write([]byte(`[
				{"id":"pol-oma","name":"Camera lockdown","template":{"name":"custom_oma_uri_mdm_windows"}},
				{"id":"pol-reg","name":"Disable Autorun","template":{"name":"custom_registry_keys_policy_windows"}},
				{"id":"pol-apple","name":"Mac thing","template":{"name":"custom_mdm_profile_darwin"}},
				{"id":"pol-builtin","name":"Builtin","template":{"name":"windows_bitlocker"}}
			]`))
		case "/api/v2/policies/pol-oma":
			_, _ = w.Write([]byte(`{
				"id":"pol-oma","name":"Camera lockdown",
				"template":{"name":"custom_oma_uri_mdm_windows"},
				"values":[{"configFieldName":"uriList","value":[
					{"uri":"./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera","format":"int","value":"0"},
					{"uri":"./Device/Vendor/MSFT/BitLocker/SomethingCustom","format":"chr","value":"x"}
				]}]
			}`))
		case "/api/v2/policies/pol-reg":
			_, _ = w.Write([]byte(`{
				"id":"pol-reg","name":"Disable Autorun",
				"template":{"name":"custom_registry_keys_policy_windows"},
				"values":[{"configFieldName":"customRegTable","value":[
					{"customLocation":"SOFTWARE\\Policies\\X","customValueName":"NoAutorun","customRegType":"expandString","customData":"1"}
				]}]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// stubEditCatalog swaps the edit-rehydration catalog loader for a
// catalog built from the shared fixture settings.
func stubEditCatalog(t *testing.T) {
	t.Helper()
	orig := windowsEditCatalogLoader
	windowsEditCatalogLoader = func() *windows_mdm.Catalog {
		return catalogFromSettings(t, testCSPSettings())
	}
	t.Cleanup(func() { windowsEditCatalogLoader = orig })
}

// catalogFromSettings builds a real Catalog from in-memory settings by
// writing a minimal snapshot dir. Uses the windows_mdm test fixture
// file to stay faithful to the parser.
func catalogFromSettings(t *testing.T, _ []windows_mdm.Setting) *windows_mdm.Catalog {
	t.Helper()
	// LoadCatalog needs area files on disk; reuse the package fixture,
	// then rely on ByURI misses falling back for URIs outside it. For
	// the Camera URI used in tests we add a matching lookup via the
	// fixture-independent path below.
	cat, err := windows_mdm.LoadCatalog("../../windows_mdm/testdata")
	if err != nil {
		t.Fatalf("fixture catalog: %v", err)
	}
	return cat
}

func loadedWindowsPoliciesList(t *testing.T) *WindowsMDMPoliciesListScreen {
	t.Helper()
	srv := startWindowsPoliciesListServer(t)
	stubWindowsMDMClient(t, srv.URL)
	stubEditCatalog(t)
	s := NewWindowsMDMPoliciesListScreen()
	model, _ := s.Update(s.loadCmd()())
	return model.(*WindowsMDMPoliciesListScreen)
}

func TestWindowsPoliciesList_FiltersToWindowsTemplates(t *testing.T) {
	s := loadedWindowsPoliciesList(t)
	if len(s.all) != 2 {
		t.Fatalf("expected exactly the 2 Windows custom policies, got %d: %+v", len(s.all), s.all)
	}
	view := s.View()
	if strings.Contains(view, "Mac thing") || strings.Contains(view, "Builtin") {
		t.Errorf("non-Windows policies leaked into the list:\n%s", view)
	}
}

func TestWindowsPoliciesList_DrillInOMAURI(t *testing.T) {
	s := loadedWindowsPoliciesList(t)
	s.cursor = 0 // pol-oma

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !s.drilling || cmd == nil {
		t.Fatal("enter should start the drill-in")
	}
	// Double-Enter guard (Apple Bugbot #54 pattern).
	_, dup := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if dup != nil {
		t.Error("second Enter during drill-in must be ignored")
	}

	msg := s.decodeCmd("pol-oma")()
	model, pushCmd := s.Update(msg)
	s = model.(*WindowsMDMPoliciesListScreen)
	if s.drillingError != "" || pushCmd == nil {
		t.Fatalf("drill-in failed: %q", s.drillingError)
	}
}

func TestWindowsPoliciesList_DrillInRegistry(t *testing.T) {
	s := loadedWindowsPoliciesList(t)
	msg := s.decodeCmd("pol-reg")()
	m, ok := msg.(decodeWindowsPolicyMsg)
	if !ok || m.err != nil {
		t.Fatalf("decode msg wrong: %+v", msg)
	}
	if m.decoded.Kind != windows_mdm.PolicyKindRegistry || len(m.decoded.Keys) != 1 {
		t.Errorf("registry decode wrong: %+v", m.decoded)
	}
}
