package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startDeviceSettingsServer mocks the org device-settings singletons. The PUT
// handlers are faithful to the live API: they return 204 with no body, and the
// sign-in PUT REPLACES the whole settings array, so a partial body would
// visibly drop the other OS family on the following GET.
func startDeviceSettingsServer(t *testing.T) *httptest.Server {
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
			var in struct {
				Settings []map[string]any `json:"settings"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			if len(in.Settings) == 0 {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"message":"settings required"}`))
				return
			}
			signIn["settings"] = in.Settings // full replace
			w.WriteHeader(http.StatusNoContent)

		case r.URL.Path == "/devices/settings/defaultpasswordsync" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(sync)

		case r.URL.Path == "/devices/settings/defaultpasswordsync" && r.Method == http.MethodPut:
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
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

func runSettingsCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errBuf.String(), err
}

func TestDeviceSettings_Get(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startDeviceSettingsServer(t).URL)

	out, _, err := runSettingsCmd(t, "devices", "settings", "get")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("JSON parse error: %v\n%s", err, out)
	}
	if got["defaultPasswordSync"] != true {
		t.Errorf("defaultPasswordSync = %v", got["defaultPasswordSync"])
	}
	list, _ := got["signInWithJumpCloud"].([]any)
	if len(list) != 2 {
		t.Errorf("expected both OS families, got %v", got["signInWithJumpCloud"])
	}
}

func TestDeviceSettings_SignInGet(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startDeviceSettingsServer(t).URL)

	out, _, err := runSettingsCmd(t, "devices", "settings", "sign-in", "get")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("JSON parse error: %v\n%s", err, out)
	}
	if len(got) != 2 || got[0]["osFamily"] != "WINDOWS" {
		t.Errorf("unexpected sign-in settings: %s", out)
	}
}

// Changing one OS family must not disturb the other — the mock PUT is a full
// replace, so a partial body would drop WINDOWS here.
func TestDeviceSettings_SignInSetPreservesOtherOS(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startDeviceSettingsServer(t).URL)

	out, _, err := runSettingsCmd(t, "devices", "settings", "sign-in", "set",
		"--os", "macos", "--enabled=false", "--force")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("JSON parse error: %v\n%s", err, out)
	}
	if got["enabled"] != false {
		t.Errorf("macOS should be disabled, got %v", got["enabled"])
	}

	// The Windows entry must have survived the full-replace PUT.
	out, _, err = runSettingsCmd(t, "devices", "settings", "sign-in", "get")
	if err != nil {
		t.Fatal(err)
	}
	var after []map[string]any
	json.Unmarshal([]byte(out), &after)
	if len(after) != 2 {
		t.Fatalf("full-replace PUT dropped an OS family: %s", out)
	}
	for _, s := range after {
		if s["osFamily"] == "WINDOWS" && s["enabled"] != true {
			t.Errorf("WINDOWS entry was disturbed: %v", s)
		}
	}
}

func TestDeviceSettings_SignInSetPermissionOnly(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startDeviceSettingsServer(t).URL)

	out, _, err := runSettingsCmd(t, "devices", "settings", "sign-in", "set",
		"--os", "windows", "--default-permission", "admin", "--force")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	if got["defaultPermission"] != "ADMIN" {
		t.Errorf("defaultPermission = %v", got["defaultPermission"])
	}
	if got["enabled"] != true {
		t.Errorf("enabled must be preserved when only permission changes, got %v", got["enabled"])
	}
}

func TestDeviceSettings_SignInSetRequiresAChange(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startDeviceSettingsServer(t).URL)

	_, _, err := runSettingsCmd(t, "devices", "settings", "sign-in", "set", "--os", "macos", "--force")
	if err == nil || !strings.Contains(err.Error(), "no changes requested") {
		t.Fatalf("expected no-changes error, got %v", err)
	}
}

func TestDeviceSettings_SignInSetRejectsBadOS(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startDeviceSettingsServer(t).URL)

	_, _, err := runSettingsCmd(t, "devices", "settings", "sign-in", "set",
		"--os", "linux", "--enabled=false", "--force")
	if err == nil || !strings.Contains(err.Error(), "invalid OS family") {
		t.Fatalf("expected OS validation error, got %v", err)
	}
}

func TestDeviceSettings_PasswordSyncGetAndSet(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startDeviceSettingsServer(t).URL)

	out, _, err := runSettingsCmd(t, "devices", "settings", "password-sync", "get")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(out, "true") {
		t.Errorf("unexpected get output: %s", out)
	}

	out, _, err = runSettingsCmd(t, "devices", "settings", "password-sync", "set", "--enabled=false", "--force")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	if got["enabled"] != false {
		t.Errorf("enabled = %v", got["enabled"])
	}
}

// Setting a value that is already current should short-circuit rather than
// issue a pointless org-wide write.
func TestDeviceSettings_PasswordSyncSetNoOp(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startDeviceSettingsServer(t).URL)

	_, errOut, err := runSettingsCmd(t, "devices", "settings", "password-sync", "set", "--enabled=true", "--force")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(errOut, "already true") {
		t.Errorf("expected no-op notice, got stderr: %s", errOut)
	}
}

func TestDeviceSettings_SignInSetPlan(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startDeviceSettingsServer(t).URL)

	_, errOut, _ := runSettingsCmd(t, "devices", "settings", "sign-in", "set",
		"--os", "macos", "--enabled=false", "--plan")

	// Nothing may have changed.
	out, _, err := runSettingsCmd(t, "devices", "settings", "sign-in", "get")
	if err != nil {
		t.Fatal(err)
	}
	var after []map[string]any
	json.Unmarshal([]byte(out), &after)
	for _, s := range after {
		if s["osFamily"] == "MACOS" && s["enabled"] != true {
			t.Errorf("plan mode must not write, but MACOS changed: %v (stderr: %s)", s, errOut)
		}
	}
}
