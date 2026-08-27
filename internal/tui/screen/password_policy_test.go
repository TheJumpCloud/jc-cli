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
		case r.Method == "GET" && r.URL.Path == "/passwordpolicies":
			// The V2 half. Deliberately out of precedence order so the
			// screen's sort is exercised.
			_, _ = w.Write([]byte(`{"results":[
				{"objectId":"pp-2","name":"Contractors","precedence":2,"default":false,"groupCount":1},
				{"objectId":"pp-1","name":"","precedence":1,"default":true,"groupCount":0}
			]}`))
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

// overridePasswordPolicyClient points BOTH clients at the stub. The screen
// reads two different APIs, and leaving the V2 one unstubbed would let a test
// reach the real network.
func overridePasswordPolicyClient(t *testing.T, url string) {
	t.Helper()
	origV1 := newV1ClientForPasswordPolicy
	newV1ClientForPasswordPolicy = func() (*api.V1Client, error) {
		c := api.NewV1ClientWithKey("test-key")
		c.BaseURL = url
		return c, nil
	}
	origV2 := newV2ClientForPasswordPolicy
	newV2ClientForPasswordPolicy = func() (*api.V2Client, error) {
		c := api.NewV2ClientWithKey("test-key")
		c.BaseURL = url
		return c, nil
	}
	t.Cleanup(func() {
		newV1ClientForPasswordPolicy = origV1
		newV2ClientForPasswordPolicy = origV2
	})
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

// The screen covers two different APIs that share the name "password policy":
// the V1 org-settings default and the V2 group-bound policies. Both must load.
func TestPasswordPolicyScreen_ShowsGroupBoundPolicies(t *testing.T) {
	var put []byte
	overridePasswordPolicyClient(t, startOrgServer(t, &put).URL)
	s := loadPasswordPolicyScreen(t)

	if len(s.groupPolicies) != 2 {
		t.Fatalf("expected 2 group-bound policies, got %d", len(s.groupPolicies))
	}
	// Rendered in precedence order, not the order the API returned them.
	if s.groupPolicies[0].ObjectID != "pp-1" || s.groupPolicies[1].ObjectID != "pp-2" {
		t.Errorf("policies not sorted by precedence: %+v", s.groupPolicies)
	}

	view := s.View()
	if !strings.Contains(view, "Group-bound policies") {
		t.Errorf("the group-bound section is missing:\n%s", view)
	}
	if !strings.Contains(view, "Contractors") {
		t.Errorf("a named policy should be listed:\n%s", view)
	}
	// The org default has no name in this API; it must not render blank.
	if !strings.Contains(view, "(unnamed)") {
		t.Errorf("the unnamed default policy needs a placeholder:\n%s", view)
	}
	if !strings.Contains(view, "g group policies") {
		t.Errorf("the footer should offer the drill-in when policies exist:\n%s", view)
	}
	// The org-default half must still be there and editable.
	if !strings.Contains(view, "Organization password policy") {
		t.Errorf("the org default section is missing:\n%s", view)
	}
}

// A failure reading the V2 half must not stop the org default from loading;
// they are independent APIs and the editable half is the more important one.
func TestPasswordPolicyScreen_GroupHalfFailureIsIsolated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/organizations":
			_, _ = w.Write([]byte(`{"totalCount":1,"results":[{"id":"org-1","displayName":"Test Org"}]}`))
		case r.URL.Path == "/organizations/org-1":
			_, _ = w.Write([]byte(`{"id":"org-1","settings":{"passwordPolicy":{"enableMinLength":true,"minLength":8}}}`))
		case r.URL.Path == "/passwordpolicies":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"nope"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	overridePasswordPolicyClient(t, srv.URL)

	s := NewPasswordPolicyScreen()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	runCmd(t, s, s.loadCmd())

	if s.stage != ppStageEdit {
		t.Fatalf("the org default must still load: stage=%v err=%q", s.stage, s.err)
	}
	if s.err != "" {
		t.Errorf("the group-bound failure must not surface as the screen error: %q", s.err)
	}
	if s.groupErr == "" {
		t.Error("the group-bound failure should be reported in its own section")
	}
	if !strings.Contains(s.View(), "403") {
		t.Errorf("the section should show what went wrong:\n%s", s.View())
	}
}

func TestPasswordPolicyScreen_NoGroupPolicies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/organizations":
			_, _ = w.Write([]byte(`{"totalCount":1,"results":[{"id":"org-1","displayName":"Test Org"}]}`))
		case r.URL.Path == "/organizations/org-1":
			_, _ = w.Write([]byte(`{"id":"org-1","settings":{"passwordPolicy":{"enableMinLength":true,"minLength":8}}}`))
		case r.URL.Path == "/passwordpolicies":
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	overridePasswordPolicyClient(t, srv.URL)

	s := NewPasswordPolicyScreen()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	runCmd(t, s, s.loadCmd())

	view := s.View()
	if !strings.Contains(view, "every user falls under the organization default") {
		t.Errorf("an empty group-bound list should explain what that means:\n%s", view)
	}
	// With nothing to open, the footer must not advertise the drill-in.
	if strings.Contains(view, "g group policies") {
		t.Errorf("the footer should not offer a drill-in with no policies:\n%s", view)
	}
}

func TestPasswordPolicyEntry_IsReadOnlyAndUnwrapsResults(t *testing.T) {
	e := passwordPolicyEntry()
	if e.ResponseKey != "results" {
		t.Errorf("ResponseKey = %q; the V2 list wraps its array in \"results\"", e.ResponseKey)
	}
	if e.Schema.IDField != "objectId" {
		t.Errorf("IDField = %q, want objectId", e.Schema.IDField)
	}
	for _, v := range e.Schema.Verbs {
		if v != "list" && v != "get" {
			t.Errorf("the drill-in is a viewer; unexpected verb %q", v)
		}
	}
}
