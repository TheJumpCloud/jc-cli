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

func draftWith(settings ...windows_mdm.Setting) *windowsMDMDraft {
	d := &windowsMDMDraft{}
	for _, s := range settings {
		d.add(s)
	}
	return d
}

func TestBuildWindowsOMAURIRow_KindMapping(t *testing.T) {
	settings := testCSPSettings()

	// ENUM → pick-list seeded to the default value.
	enum := buildWindowsOMAURIRow(settings[0]) // Camera/AllowCamera default 1
	if enum.kind != windowsRowKindEnum {
		t.Errorf("ENUM setting kind = %d", enum.kind)
	}
	if enum.options[enum.selectedIdx].Value != "1" {
		t.Errorf("ENUM default not seeded: selected %q", enum.options[enum.selectedIdx].Value)
	}

	// Numeric with Range → text row seeded from default.
	num := buildWindowsOMAURIRow(settings[1])
	if num.kind != windowsRowKindText || num.text.Value() != "0" {
		t.Errorf("numeric row wrong: kind=%d value=%q", num.kind, num.text.Value())
	}

	// bool format → toggle.
	boolRow := buildWindowsOMAURIRow(windows_mdm.Setting{
		URI: "./Device/Vendor/MSFT/X", Format: "bool", DefaultValue: "true",
	})
	if boolRow.kind != windowsRowKindBool || !boolRow.boolValue {
		t.Errorf("bool row wrong: kind=%d value=%v", boolRow.kind, boolRow.boolValue)
	}
}

func TestWindowsOMAURIForm_RangeValidationInline(t *testing.T) {
	settings := testCSPSettings()
	s := NewWindowsMDMOMAURIFormScreen(draftWith(settings[1])) // Range [0-999]

	s.rows[0].text.SetValue("1200")
	if err := validateWindowsRowInline(&s.rows[0]); !strings.Contains(err, "maximum") {
		t.Errorf("expected above-maximum error, got %q", err)
	}
	s.rows[0].text.SetValue("abc")
	if err := validateWindowsRowInline(&s.rows[0]); !strings.Contains(err, "whole number") {
		t.Errorf("expected parse error, got %q", err)
	}
	s.rows[0].text.SetValue("500")
	if err := validateWindowsRowInline(&s.rows[0]); err != "" {
		t.Errorf("500 is in range, got %q", err)
	}
}

func TestWindowsOMAURIForm_SubmitValidToPreview(t *testing.T) {
	settings := testCSPSettings()
	s := NewWindowsMDMOMAURIFormScreen(draftWith(settings[0], settings[1]))
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	// Cycle the enum to 0 ("Not allowed") to prove pick-list state
	// flows into the built settings.
	s.rows[0].selectedIdx = 0
	s.submit()
	if s.stage != mdmFormStagePreview {
		t.Fatalf("valid submit should reach preview, stage=%d (row errs: %q %q)",
			s.stage, s.rows[0].err, s.rows[1].err)
	}
	if len(s.normalized) != 2 || s.normalized[0].Value != "0" {
		t.Errorf("normalized settings wrong: %+v", s.normalized)
	}
	// Blank name falls back to the suggestion.
	if s.policyName == "" {
		t.Error("policy name should fall back to the suggestion")
	}
}

func TestWindowsOMAURIForm_SubmitMapsAggregateErrorsToRows(t *testing.T) {
	settings := testCSPSettings()
	s := NewWindowsMDMOMAURIFormScreen(draftWith(settings[1])) // int row

	s.rows[0].text.SetValue("") // required — validator rejects empty value
	s.submit()
	if s.stage != mdmFormStageEdit {
		t.Fatal("invalid submit must stay on edit stage")
	}
	if s.rows[0].err == "" {
		t.Error("aggregate validator error should map onto the row")
	}

	// Fixing the value + resubmitting must clear the stale error
	// (the Apple form's Bugbot #53 guard, mirrored).
	s.rows[0].text.SetValue("5")
	s.submit()
	if s.stage != mdmFormStagePreview || s.rows[0].err != "" {
		t.Errorf("stale error not cleared: stage=%d err=%q", s.stage, s.rows[0].err)
	}
}

func TestWindowsOMAURIForm_RemoveRowSyncsDraft(t *testing.T) {
	settings := testCSPSettings()
	draft := draftWith(settings[0], settings[1])
	s := NewWindowsMDMOMAURIFormScreen(draft)

	s.focusIdx = 1 // first row
	s.removeFocusedRow()
	if len(s.rows) != 1 || len(draft.settings) != 1 {
		t.Fatalf("row removal must sync the draft: rows=%d draft=%d", len(s.rows), len(draft.settings))
	}
	if draft.settings[0].URI != settings[1].URI {
		t.Errorf("wrong setting removed: %+v", draft.settings)
	}

	// Removing the last row pops back to browse.
	s.focusIdx = 1
	_, cmd := s.removeFocusedRow()
	if len(s.rows) != 0 || cmd == nil {
		t.Error("removing the last row should pop the screen")
	}
}

// startWindowsPolicyServer stubs the template lookup + POST /policies,
// mirroring the windows_mdm package's own test stub.
func startWindowsPolicyServer(t *testing.T, onCreate func()) *httptest.Server {
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
		case r.URL.Path == "/api/v2/policies" && r.Method == "POST":
			if onCreate != nil {
				onCreate()
			}
			_, _ = w.Write([]byte(`{"id":"pol-9999","name":"TUI Created Policy"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func stubWindowsMDMClient(t *testing.T, baseURL string) {
	t.Helper()
	orig := newV2ClientForWindowsMDM
	newV2ClientForWindowsMDM = func() (*api.V2Client, error) {
		c := api.NewV2ClientWithKey("test-key")
		c.BaseURL = baseURL + "/api/v2"
		return c, nil
	}
	t.Cleanup(func() { newV2ClientForWindowsMDM = orig })
}

func TestWindowsOMAURIForm_CreateFlow(t *testing.T) {
	posted := false
	srv := startWindowsPolicyServer(t, func() { posted = true })
	stubWindowsMDMClient(t, srv.URL)

	settings := testCSPSettings()
	draft := draftWith(settings[0])
	s := NewWindowsMDMOMAURIFormScreen(draft)
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	s.submit()
	if s.stage != mdmFormStagePreview {
		t.Fatalf("expected preview, got stage %d", s.stage)
	}

	// `c` starts the create; run the returned batch's create cmd by
	// executing the message pipeline manually.
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if s.stage != mdmFormStageCreating || cmd == nil {
		t.Fatalf("c should enter creating stage with a cmd, stage=%d", s.stage)
	}
	msg := s.createCmd()()
	model, _ := s.Update(msg)
	s = model.(*WindowsMDMOMAURIFormScreen)

	if !posted {
		t.Fatal("create flow never POSTed")
	}
	if s.stage != mdmFormStageSuccess || s.policyID != "pol-9999" {
		t.Errorf("success state wrong: stage=%d id=%q err=%q", s.stage, s.policyID, s.createErr)
	}
	if len(draft.settings) != 0 {
		t.Error("draft should clear after a successful create")
	}
	if !strings.Contains(s.View(), "pol-9999") {
		t.Error("success view should show the policy ID")
	}
}

func TestWindowsOMAURIForm_CreateFailureRendersError(t *testing.T) {
	settings := testCSPSettings()
	s := NewWindowsMDMOMAURIFormScreen(draftWith(settings[0]))
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	model, _ := s.Update(windowsMDMCreateMsg{err: errFake})
	s = model.(*WindowsMDMOMAURIFormScreen)
	if s.stage != mdmFormStageFailed || !strings.Contains(s.View(), "fake failure") {
		t.Errorf("failure state wrong: stage=%d", s.stage)
	}
}

type fakeErr struct{}

func (fakeErr) Error() string { return "fake failure" }

var errFake = fakeErr{}
