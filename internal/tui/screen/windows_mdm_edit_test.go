package screen

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// decodedOMAURIPolicy builds a DecodedPolicy carrying one setting the
// fixture catalog knows (Sample/AllowWidget — the ENUM one) and one
// it doesn't (a standalone-CSP URI).
func decodedOMAURIPolicy() windows_mdm.DecodedPolicy {
	return windows_mdm.DecodedPolicy{
		PolicyID:     "pol-oma",
		PolicyName:   "Widget lockdown",
		Kind:         windows_mdm.PolicyKindOMAURI,
		TemplateName: windows_mdm.TemplateNameOMAURI,
		Settings: []windows_mdm.OMAURISetting{
			{URI: "./Device/Vendor/MSFT/Policy/Config/Sample/AllowWidget", Format: "int", Value: "0"},
			{URI: "./Device/Vendor/MSFT/BitLocker/SomethingCustom", Format: "chr", Value: "hand-authored"},
		},
	}
}

func TestOMAURIFormEdit_RehydratesFromCatalog(t *testing.T) {
	cat := catalogFromSettings(t, nil) // fixture catalog (Sample area)
	s := NewWindowsMDMOMAURIFormScreenForEdit(decodedOMAURIPolicy(), cat)

	if s.mode != formModeEdit || s.editPolicyID != "pol-oma" {
		t.Fatalf("edit mode not set: %+v", s.mode)
	}
	if s.nameInput.Value() != "Widget lockdown" {
		t.Errorf("name not prepopulated: %q", s.nameInput.Value())
	}

	// Row 0: catalog hit → ENUM pick-list, selected at the stored "0".
	if s.rows[0].kind != windowsRowKindEnum {
		t.Fatalf("catalog-hit row should be an enum, kind=%d", s.rows[0].kind)
	}
	if s.rows[0].options[s.rows[0].selectedIdx].Value != "0" {
		t.Errorf("stored value not selected: %q", s.rows[0].options[s.rows[0].selectedIdx].Value)
	}

	// Row 1: catalog miss → editable text row carrying the stored
	// value verbatim, never dropped.
	if s.rows[1].kind != windowsRowKindText || s.rows[1].text.Value() != "hand-authored" {
		t.Errorf("catalog-miss fallback wrong: kind=%d value=%q", s.rows[1].kind, s.rows[1].text.Value())
	}
	if !strings.Contains(s.Title(), "Edit") {
		t.Errorf("title should say Edit: %q", s.Title())
	}
}

// TestOMAURIFormEdit_EnumDriftPreservesStoredValue guards the
// CodeRabbit PR #68 catch: a stored value absent from the catalog's
// enum options (catalog drift) must NOT silently land on the default
// option — that would mutate the policy on save. It degrades to a
// text row carrying the stored value verbatim.
func TestOMAURIFormEdit_EnumDriftPreservesStoredValue(t *testing.T) {
	cat := catalogFromSettings(t, nil)
	decoded := decodedOMAURIPolicy()
	decoded.Settings[0].Value = "42" // not in the fixture's {0,1} enum
	s := NewWindowsMDMOMAURIFormScreenForEdit(decoded, cat)

	if s.rows[0].kind != windowsRowKindText {
		t.Fatalf("drifted enum should degrade to text, kind=%d", s.rows[0].kind)
	}
	if s.rows[0].text.Value() != "42" {
		t.Errorf("stored value must survive verbatim: %q", s.rows[0].text.Value())
	}
}

func TestOMAURIFormEdit_NilCatalogFallsBackToText(t *testing.T) {
	s := NewWindowsMDMOMAURIFormScreenForEdit(decodedOMAURIPolicy(), nil)
	for i, r := range s.rows {
		if r.kind != windowsRowKindText {
			t.Errorf("row %d should be text with nil catalog, kind=%d", i, r.kind)
		}
	}
	if s.rows[0].text.Value() != "0" {
		t.Errorf("stored value lost: %q", s.rows[0].text.Value())
	}
}

// startWindowsUpdateServer stubs template resolution + PUT
// /policies/{id}, capturing the PUT body.
func startWindowsUpdateServer(t *testing.T, captured *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v2/policytemplates" && r.Method == "GET":
			filter := r.URL.Query().Get("filter")
			switch {
			case strings.Contains(filter, "custom_oma_uri_mdm_windows"):
				_, _ = w.Write([]byte(`[{"id":"tmpl-oma","name":"custom_oma_uri_mdm_windows"}]`))
			case strings.Contains(filter, "custom_registry_keys_policy_windows"):
				_, _ = w.Write([]byte(`[{"id":"tmpl-reg","name":"custom_registry_keys_policy_windows"}]`))
			default:
				_, _ = w.Write([]byte(`[]`))
			}
		case r.URL.Path == "/api/v2/policytemplates/tmpl-oma":
			_, _ = w.Write([]byte(`{"id":"tmpl-oma","name":"custom_oma_uri_mdm_windows","configFields":[{"id":"urifid","name":"uriList"}]}`))
		case r.URL.Path == "/api/v2/policytemplates/tmpl-reg":
			_, _ = w.Write([]byte(`{"id":"tmpl-reg","name":"custom_registry_keys_policy_windows","configFields":[{"id":"regfid","name":"customRegTable"}]}`))
		case strings.HasPrefix(r.URL.Path, "/api/v2/policies/") && r.Method == "PUT":
			body := make([]byte, 0, 4096)
			buf := make([]byte, 1024)
			for {
				n, err := r.Body.Read(buf)
				body = append(body, buf[:n]...)
				if err != nil {
					break
				}
			}
			*captured = r.URL.Path + " " + string(body)
			_, _ = w.Write([]byte(`{"id":"pol-oma","name":"Widget lockdown v2"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOMAURIFormEdit_SubmitPUTs(t *testing.T) {
	var captured string
	srv := startWindowsUpdateServer(t, &captured)
	origClient := newV2ClientForWindowsMDM
	newV2ClientForWindowsMDM = func() (*api.V2Client, error) {
		c := api.NewV2ClientWithKey("test-key")
		c.BaseURL = srv.URL + "/api/v2"
		return c, nil
	}
	t.Cleanup(func() { newV2ClientForWindowsMDM = origClient })

	s := NewWindowsMDMOMAURIFormScreenForEdit(decodedOMAURIPolicy(), nil)
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	s.nameInput.SetValue("Widget lockdown v2")
	s.submit()
	if s.stage != mdmFormStagePreview {
		t.Fatalf("submit failed: %q %q", s.rows[0].err, s.rows[1].err)
	}

	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	msg := s.createCmd()()
	model, _ := s.Update(msg)
	s = model.(*WindowsMDMOMAURIFormScreen)

	if s.stage != mdmFormStageSuccess {
		t.Fatalf("update failed: %s", s.createErr)
	}
	if !strings.HasPrefix(captured, "/api/v2/policies/pol-oma ") {
		t.Errorf("PUT went to the wrong path: %s", captured[:60])
	}
	for _, want := range []string{`"Widget lockdown v2"`, `"hand-authored"`, `"uriList"`} {
		if !strings.Contains(captured, want) {
			t.Errorf("PUT body missing %s", want)
		}
	}
	if !strings.Contains(s.View(), "Policy updated.") {
		t.Error("success view should say updated, not created")
	}
}

func TestRegistryFormEdit_PrepopulatesAndPUTs(t *testing.T) {
	var captured string
	srv := startWindowsUpdateServer(t, &captured)
	origClient := newV2ClientForWindowsMDM
	newV2ClientForWindowsMDM = func() (*api.V2Client, error) {
		c := api.NewV2ClientWithKey("test-key")
		c.BaseURL = srv.URL + "/api/v2"
		return c, nil
	}
	t.Cleanup(func() { newV2ClientForWindowsMDM = origClient })

	decoded := windows_mdm.DecodedPolicy{
		PolicyID:   "pol-reg",
		PolicyName: "Disable Autorun",
		Kind:       windows_mdm.PolicyKindRegistry,
		Keys: []windows_mdm.RegistryKey{{
			Location: `SOFTWARE\Policies\X`, ValueName: "NoAutorun",
			RegType: "expandString", Data: "%SystemRoot%",
		}},
	}
	s := NewWindowsMDMRegistryFormScreenForEdit(decoded)
	if s.rows[0].location.Value() != `SOFTWARE\Policies\X` {
		t.Errorf("location not prepopulated: %q", s.rows[0].location.Value())
	}
	// expandString is index 1 in RegistryRegTypes().
	if types := windows_mdm.RegistryRegTypes(); types[s.rows[0].typeIdx] != "expandString" {
		t.Errorf("type cycle not set from wire value: %q", types[s.rows[0].typeIdx])
	}

	s.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	s.submit()
	if s.stage != mdmFormStagePreview {
		t.Fatalf("submit failed: %q", s.rows[0].err)
	}
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	msg := s.createCmd()()
	model, _ := s.Update(msg)
	s = model.(*WindowsMDMRegistryFormScreen)
	if s.stage != mdmFormStageSuccess {
		t.Fatalf("update failed: %s", s.createErr)
	}
	if !strings.HasPrefix(captured, "/api/v2/policies/pol-reg ") ||
		!strings.Contains(captured, `"customRegTable"`) {
		t.Errorf("registry PUT wrong: %s", captured[:80])
	}
}
