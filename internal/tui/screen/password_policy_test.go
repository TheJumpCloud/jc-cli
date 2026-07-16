package screen

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/api"
)

// startOrgServer stubs the V1 org endpoints. The settings object
// carries a sibling key (newSystemUserStateDefaults) that MUST survive
// the save untouched — the read-modify-write contract.
func startOrgServer(t *testing.T, putBody *[]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/organizations":
			_, _ = w.Write([]byte(`{"totalCount":1,"results":[{"id":"org-1","displayName":"Test Org"}]}`))
		case r.Method == "GET" && r.URL.Path == "/organizations/org-1":
			_, _ = w.Write([]byte(`{"id":"org-1","displayName":"Test Org","settings":{
				"newSystemUserStateDefaults":{"applicationImport":"ACTIVATED"},
				"passwordPolicy":{
					"enableMinLength":true,"minLength":8,
					"needsLowercase":false,
					"enableMaxLoginAttempts":true,"maxLoginAttempts":6,
					"effectiveDate":"2023-05-03T09:23:50.046Z"
				}}}`))
		case r.Method == "PUT" && r.URL.Path == "/organizations/org-1":
			body, _ := io.ReadAll(r.Body)
			*putBody = body
			_, _ = w.Write(body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func overridePasswordPolicyClient(t *testing.T, url string) {
	t.Helper()
	orig := newV1ClientForPasswordPolicy
	newV1ClientForPasswordPolicy = func() (*api.V1Client, error) {
		c := api.NewV1ClientWithKey("test-key")
		c.BaseURL = url
		return c, nil
	}
	t.Cleanup(func() { newV1ClientForPasswordPolicy = orig })
}

func loadPasswordPolicyScreen(t *testing.T) *PasswordPolicyScreen {
	t.Helper()
	s := NewPasswordPolicyScreen()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	runCmd(t, s, s.loadCmd())
	if s.stage != ppStageEdit || s.err != "" {
		t.Fatalf("load failed: stage=%v err=%q", s.stage, s.err)
	}
	return s
}

func TestPasswordPolicyScreen_LoadsAndRenders(t *testing.T) {
	var putBody []byte
	srv := startOrgServer(t, &putBody)
	overridePasswordPolicyClient(t, srv.URL)

	s := loadPasswordPolicyScreen(t)
	view := s.View()
	for _, want := range []string{
		"Test Org",
		"Complexity", "Lockout", // group headers
		"Minimum length", "8",
		"Max login attempts", "6",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	// effectiveDate is server-managed: never an editable row.
	if strings.Contains(view, "effectiveDate") {
		t.Error("effectiveDate must not be editable")
	}
}

func TestPasswordPolicyScreen_EditAndSavePreservesSiblings(t *testing.T) {
	var putBody []byte
	srv := startOrgServer(t, &putBody)
	overridePasswordPolicyClient(t, srv.URL)

	s := loadPasswordPolicyScreen(t)

	// Toggle the first bool row (enableMinLength: true → false).
	s.cursor = 0
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})

	// Edit minLength (row 1) 8 → 12 via inline input.
	s.cursor = 1
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.stage != ppStageEditingValue {
		t.Fatalf("stage = %v, want editing", s.stage)
	}
	s.input.SetValue("12")
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Ctrl+S → confirm shows both diffs → y saves.
	s.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if s.stage != ppStageConfirm {
		t.Fatalf("stage = %v, want confirm", s.stage)
	}
	view := s.View()
	for _, want := range []string{"Enforce minimum length: true → false", "Minimum length: 8 → 12"} {
		if !strings.Contains(view, want) {
			t.Errorf("confirm missing %q:\n%s", want, view)
		}
	}
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	runCmd(t, s, cmd)
	if s.stage != ppStageEdit || s.err != "" {
		t.Fatalf("save failed: stage=%v err=%q", s.stage, s.err)
	}

	// The PUT body: full settings (sibling key preserved), edited
	// values applied, effectiveDate passed through untouched.
	var body struct {
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(putBody, &body); err != nil {
		t.Fatalf("PUT body: %v\n%s", err, putBody)
	}
	if _, ok := body.Settings["newSystemUserStateDefaults"]; !ok {
		t.Error("sibling settings key was dropped — read-modify-write broken")
	}
	pp := body.Settings["passwordPolicy"].(map[string]any)
	if pp["enableMinLength"] != false || pp["minLength"] != float64(12) {
		t.Errorf("edited values wrong: %v", pp)
	}
	if pp["effectiveDate"] != "2023-05-03T09:23:50.046Z" {
		t.Errorf("effectiveDate must pass through: %v", pp["effectiveDate"])
	}
	if pp["maxLoginAttempts"] != float64(6) {
		t.Errorf("untouched value changed: %v", pp["maxLoginAttempts"])
	}
}

func TestPasswordPolicyScreen_NoChangesAndBadNumber(t *testing.T) {
	var putBody []byte
	srv := startOrgServer(t, &putBody)
	overridePasswordPolicyClient(t, srv.URL)

	s := loadPasswordPolicyScreen(t)

	// Ctrl+S with no edits: flash, no PUT, no confirm stage.
	s.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if s.stage != ppStageEdit || s.flash != "No changes." {
		t.Errorf("no-change save: stage=%v flash=%q", s.stage, s.flash)
	}
	if putBody != nil {
		t.Error("no-change save must not PUT")
	}

	// Non-numeric input on an int field: clear error, value untouched.
	s.cursor = 1
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	s.input.SetValue("lots")
	s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(s.err, "not a whole number") {
		t.Errorf("bad number err = %q", s.err)
	}
	if s.policy["minLength"] != float64(8) {
		t.Errorf("value must stay 8, got %v", s.policy["minLength"])
	}
}
