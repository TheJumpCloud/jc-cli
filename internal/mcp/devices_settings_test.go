package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startDeviceSettingsV2Server mocks the org device-settings singletons with a
// faithful full-replace PUT that answers 204 and an empty body, as the live
// API does.
func startDeviceSettingsV2Server(t *testing.T) *httptest.Server {
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
		p := strings.TrimPrefix(r.URL.Path, "/api/v2")
		switch {
		case p == "/devices/settings/signinwithjumpcloud" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(signIn)

		case p == "/devices/settings/signinwithjumpcloud" && r.Method == http.MethodPut:
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

		case p == "/devices/settings/defaultpasswordsync" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(sync)

		case p == "/devices/settings/defaultpasswordsync" && r.Method == http.MethodPut:
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			sync["enabled"] = in["enabled"]
			w.WriteHeader(http.StatusNoContent)

		default:
			t.Errorf("unexpected: %s %s", r.Method, p)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMCPDeviceSettings_Get(t *testing.T) {
	overrideV2ClientForTest(t, startDeviceSettingsV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "devices_settings_get", map[string]any{}))
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if m["defaultPasswordSync"] != true {
		t.Errorf("defaultPasswordSync = %v", m["defaultPasswordSync"])
	}
	list, _ := m["signInWithJumpCloud"].([]any)
	if len(list) != 2 {
		t.Errorf("expected both OS families: %s", out)
	}
}

// Without execute=true the tool reports the before/after and writes nothing.
func TestMCPDeviceSettings_SignInSetPlan(t *testing.T) {
	overrideV2ClientForTest(t, startDeviceSettingsV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "devices_settings_signin_set", map[string]any{
		"os": "macos", "enabled": false,
	}))
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if m["plan"] != true {
		t.Fatalf("expected a plan, got %s", out)
	}
	effects, _ := m["effects"].(map[string]any)
	if effects["from"] != "MACOS: enabled, default permission STANDARD" {
		t.Errorf("plan should show the current value, got %v", effects["from"])
	}
	if effects["to"] != "MACOS: disabled, default permission STANDARD" {
		t.Errorf("plan should show the new value, got %v", effects["to"])
	}

	// Nothing may have been written.
	out = getResultText(t, callTool(t, cs, "devices_settings_get", map[string]any{}))
	json.Unmarshal([]byte(out), &m)
	list, _ := m["signInWithJumpCloud"].([]any)
	for _, item := range list {
		s, _ := item.(map[string]any)
		if s["osFamily"] == "MACOS" && s["enabled"] != true {
			t.Errorf("plan mode must not write, but MACOS changed: %v", s)
		}
	}
}

// A single-OS change must send the complete array so the full-replace PUT
// cannot drop the other OS family.
func TestMCPDeviceSettings_SignInSetPreservesOtherOS(t *testing.T) {
	overrideV2ClientForTest(t, startDeviceSettingsV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	callTool(t, cs, "devices_settings_signin_set", map[string]any{
		"os": "macos", "enabled": false, "execute": true,
	})

	out := getResultText(t, callTool(t, cs, "devices_settings_get", map[string]any{}))
	var m map[string]any
	json.Unmarshal([]byte(out), &m)
	list, _ := m["signInWithJumpCloud"].([]any)
	if len(list) != 2 {
		t.Fatalf("full-replace PUT dropped an OS family: %s", out)
	}
	for _, item := range list {
		s, _ := item.(map[string]any)
		if s["osFamily"] == "WINDOWS" && s["enabled"] != true {
			t.Errorf("WINDOWS entry was disturbed: %v", s)
		}
		if s["osFamily"] == "MACOS" && s["enabled"] != false {
			t.Errorf("MACOS should be disabled: %v", s)
		}
	}
}

func TestMCPDeviceSettings_SignInSetValidation(t *testing.T) {
	overrideV2ClientForTest(t, startDeviceSettingsV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "devices_settings_signin_set", map[string]any{
		"os": "linux", "enabled": false, "execute": true,
	}))
	if !strings.Contains(out, "invalid OS family") {
		t.Errorf("expected OS validation error, got %s", out)
	}

	out = getResultText(t, callTool(t, cs, "devices_settings_signin_set", map[string]any{
		"os": "macos", "execute": true,
	}))
	if !strings.Contains(out, "no changes requested") {
		t.Errorf("expected no-changes error, got %s", out)
	}
}

func TestMCPDeviceSettings_PasswordSyncSet(t *testing.T) {
	overrideV2ClientForTest(t, startDeviceSettingsV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	// Already true → short-circuits instead of writing.
	out := getResultText(t, callTool(t, cs, "devices_settings_password_sync_set", map[string]any{
		"enabled": true, "execute": true,
	}))
	if !strings.Contains(out, "already true") {
		t.Errorf("expected no-op notice, got %s", out)
	}

	out = getResultText(t, callTool(t, cs, "devices_settings_password_sync_set", map[string]any{
		"enabled": false, "execute": true,
	}))
	var sync map[string]any
	if err := json.Unmarshal([]byte(out), &sync); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if sync["enabled"] != false {
		t.Errorf("expected updated state, got %s", out)
	}
}

func TestMCPDeviceSettings_ReadOnlyBlocksMutations(t *testing.T) {
	overrideV2ClientForTest(t, startDeviceSettingsV2Server(t).URL)
	cs := connectToolTestServer(t, Options{ReadOnly: true})

	out := getResultText(t, callTool(t, cs, "devices_settings_signin_set", map[string]any{
		"os": "macos", "enabled": false, "execute": true,
	}))
	if !strings.Contains(out, "read-only") {
		t.Errorf("signin_set should be blocked in read-only mode, got %s", out)
	}

	out = getResultText(t, callTool(t, cs, "devices_settings_password_sync_set", map[string]any{
		"enabled": false, "execute": true,
	}))
	if !strings.Contains(out, "read-only") {
		t.Errorf("password_sync_set should be blocked in read-only mode, got %s", out)
	}
}
