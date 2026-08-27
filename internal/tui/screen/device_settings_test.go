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

// startDeviceSettingsSrv stubs both singletons faithfully: the live PUTs answer
// 204 with an empty body, and the sign-in PUT is treated as a FULL REPLACE, so
// a partial body visibly drops the other OS family.
func startDeviceSettingsSrv(t *testing.T, putBodies *[][]byte) *httptest.Server {
	t.Helper()
	signIn := map[string]any{
		"organizationObjectId": "5ec71e8e96bfda0611fc6c5b",
		"settings": []map[string]any{
			{"osFamily": "WINDOWS", "enabled": true, "defaultPermission": "STANDARD"},
			{"osFamily": "MACOS", "enabled": true, "defaultPermission": "STANDARD"},
		},
	}
	sync := map[string]any{"enabled": true}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/devices/settings/signinwithjumpcloud" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(signIn)
		case r.URL.Path == "/devices/settings/signinwithjumpcloud" && r.Method == http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			if putBodies != nil {
				*putBodies = append(*putBodies, body)
			}
			var in struct {
				Settings []map[string]any `json:"settings"`
			}
			json.Unmarshal(body, &in)
			signIn["settings"] = in.Settings // full replace
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/devices/settings/defaultpasswordsync" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(sync)
		case r.URL.Path == "/devices/settings/defaultpasswordsync" && r.Method == http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			if putBodies != nil {
				*putBodies = append(*putBodies, body)
			}
			var in map[string]any
			json.Unmarshal(body, &in)
			sync["enabled"] = in["enabled"]
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func overrideDeviceSettingsClient(t *testing.T, url string) {
	t.Helper()
	orig := newV2ClientForDeviceSettings
	newV2ClientForDeviceSettings = func() (*api.V2Client, error) {
		c := api.NewV2ClientWithKey("test-key")
		c.BaseURL = url
		return c, nil
	}
	t.Cleanup(func() { newV2ClientForDeviceSettings = orig })
}

func loadDeviceSettings(t *testing.T, url string) *DeviceSettingsScreen {
	t.Helper()
	overrideDeviceSettingsClient(t, url)
	s := NewDeviceSettingsScreen()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	runCmd(t, s, s.loadCmd())
	if s.stage != dsStageBrowse || s.err != "" {
		t.Fatalf("load failed: stage=%v err=%q", s.stage, s.err)
	}
	return s
}

func TestDeviceSettings_ShowsBothSingletons(t *testing.T) {
	s := loadDeviceSettings(t, startDeviceSettingsSrv(t, nil).URL)

	// Two rows per OS family (enabled + permission), plus password sync.
	if len(s.rows) != 5 {
		t.Fatalf("expected 5 rows, got %d: %+v", len(s.rows), s.rows)
	}
	view := s.View()
	for _, want := range []string{"Sign In with JumpCloud", "Password sync", "WINDOWS", "MACOS"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

// The whole reason this screen goes through devsettings.MergeSignInSetting:
// the PUT may be a full replace, so changing one OS family must not drop the
// other. Live probing could not establish which it is.
func TestDeviceSettings_SaveKeepsTheOtherOSFamily(t *testing.T) {
	var puts [][]byte
	s := loadDeviceSettings(t, startDeviceSettingsSrv(t, &puts).URL)

	// Turn MACOS off, leave WINDOWS alone.
	for i, r := range s.rows {
		if r.OSFamily == "MACOS" && !r.Permission {
			s.cursor = i
			break
		}
	}
	s.toggleCurrent()

	runCmd(t, s, s.saveCmd())
	if len(puts) != 1 {
		t.Fatalf("expected exactly one PUT (sign-in only), got %d", len(puts))
	}

	var body struct {
		Settings []struct {
			OSFamily string `json:"osFamily"`
			Enabled  bool   `json:"enabled"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(puts[0], &body); err != nil {
		t.Fatalf("PUT body: %v", err)
	}
	if len(body.Settings) != 2 {
		t.Fatalf("the complete array must go back, got %d entries: %s", len(body.Settings), puts[0])
	}
	for _, st := range body.Settings {
		switch st.OSFamily {
		case "MACOS":
			if st.Enabled {
				t.Error("MACOS should have been disabled")
			}
		case "WINDOWS":
			if !st.Enabled {
				t.Error("WINDOWS was not touched and must survive the write")
			}
		}
	}
}

// Only the singleton that changed is written; they are separate endpoints.
func TestDeviceSettings_OnlyWritesWhatChanged(t *testing.T) {
	var puts [][]byte
	s := loadDeviceSettings(t, startDeviceSettingsSrv(t, &puts).URL)

	s.cursor = len(s.rows) - 1 // the password sync row
	s.toggleCurrent()
	runCmd(t, s, s.saveCmd())

	if len(puts) != 1 {
		t.Fatalf("only password sync changed, so expected 1 PUT, got %d", len(puts))
	}
	var body map[string]any
	json.Unmarshal(puts[0], &body)
	if _, isSignIn := body["settings"]; isSignIn {
		t.Errorf("the sign-in singleton must not be rewritten when unchanged: %s", puts[0])
	}
	if body["enabled"] != false {
		t.Errorf("password sync should have been turned off: %s", puts[0])
	}
}

func TestDeviceSettings_PermissionRowCycles(t *testing.T) {
	s := loadDeviceSettings(t, startDeviceSettingsSrv(t, nil).URL)
	for i, r := range s.rows {
		if r.Permission {
			s.cursor = i
			break
		}
	}
	before := s.rows[s.cursor].Value
	s.toggleCurrent()
	if s.rows[s.cursor].Value == before {
		t.Error("a permission row should cycle, not stay put")
	}
	if s.rows[s.cursor].Value != "ADMIN" && s.rows[s.cursor].Value != "STANDARD" {
		t.Errorf("permission cycled to an invalid value: %q", s.rows[s.cursor].Value)
	}
}

// An org-wide write must be confirmed, and the confirmation must say so.
func TestDeviceSettings_SaveRequiresConfirmation(t *testing.T) {
	var puts [][]byte
	s := loadDeviceSettings(t, startDeviceSettingsSrv(t, &puts).URL)

	s.cursor = 0
	s.toggleCurrent()
	s.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if s.stage != dsStageConfirm {
		t.Fatalf("Ctrl+S should ask first, stage=%v", s.stage)
	}
	if !strings.Contains(s.View(), "every device in the organization") {
		t.Errorf("the confirmation should say what the blast radius is:\n%s", s.View())
	}
	if len(puts) != 0 {
		t.Error("nothing should be written before confirming")
	}

	// Declining goes back without writing.
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if s.stage != dsStageBrowse || len(puts) != 0 {
		t.Errorf("declining must not write: stage=%v puts=%d", s.stage, len(puts))
	}
}

func TestDeviceSettings_NoChangesSaysSo(t *testing.T) {
	s := loadDeviceSettings(t, startDeviceSettingsSrv(t, nil).URL)
	s.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if s.stage == dsStageConfirm {
		t.Error("with nothing changed there is nothing to confirm")
	}
	if !strings.Contains(s.flash, "No changes") {
		t.Errorf("flash = %q", s.flash)
	}
}

func TestADTranslationRulesEntry(t *testing.T) {
	e := adTranslationRulesEntry("ad-1", "example.com")
	if e.ListEndpoint != "/activedirectories/ad-1/translation-rules" {
		t.Errorf("endpoint = %q", e.ListEndpoint)
	}
	// The list wraps its array in "rules"; getting this wrong yields an empty
	// list rather than an error.
	if e.ResponseKey != "rules" {
		t.Errorf("ResponseKey = %q, want rules", e.ResponseKey)
	}
	if !strings.Contains(e.DisplayName, "example.com") {
		t.Errorf("the title should name which directory: %q", e.DisplayName)
	}
	for _, v := range e.Schema.Verbs {
		if v != "list" && v != "get" {
			t.Errorf("translation rules are read-only in the TUI; got verb %q", v)
		}
	}
}
