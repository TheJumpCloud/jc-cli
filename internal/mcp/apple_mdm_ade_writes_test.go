package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The slice-4 write tools, against a server replaying the contract observed on
// 2026-10-28: the IOS setting as it was live, a PATCH that applies only the
// masked fields (and resets a masked field with no value, as the service
// does), device groups that 404 unless known, and the discovery-URL answers.

const knownGroupID = "609384d91f24757289141557"

const liveIOSSettingEnvelope = `{"adeSetting":{"objectId":"690bf92bc4816d00015b8c41",
"adeTokenObjectId":"6a7e1bb88cf0be0001a4e3cd","deviceManagerObjectId":"690b6c939c0bf5000100039a",
"osFamily":"IOS","ade":{"welcomeScreen":{"title":"Welcome","paragraph":"In just a few steps, you will be working securely from your Apple device.","button":"continue"},
"setupOptions":[],"enableZeroTouchEnrollment":false,"defaultDeviceGroupObjectIds":[]},
"organizationObjectId":"5ec71e8e96bfda0611fc6c5b"}}`

// startADEWriteRecorder records every request. dropWrites=true makes the
// PATCH answer 200 with the setting unchanged — the shape of a silently
// dropped write, which the tools must not report as success.
func startADEWriteRecorder(t *testing.T, dropWrites bool) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	var seen []recordedRequest
	var state map[string]any
	if err := json.Unmarshal([]byte(liveIOSSettingEnvelope), &state); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, recordedRequest{method: r.Method, path: r.URL.Path, body: string(body)})
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/systemgroups/"):
			if strings.HasSuffix(r.URL.Path, knownGroupID) {
				_, _ = w.Write([]byte(`{"id":"` + knownGroupID + `","name":"All Macs"}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		case r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(state)
		case r.Method == http.MethodPatch:
			if !dropWrites {
				applyMask(t, state, body)
			}
			_ = json.NewEncoder(w).Encode(state)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/service-discovery-url"):
			_, _ = w.Write([]byte(`{"serviceDiscoveryUrl":"https://apple.mdm.jumpcloud.com/account-driven-service-discovery/5ec71e8e96bfda0611fc6c5b"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// applyMask does what the service does: write each masked field from the
// body, and reset it when the body has no value.
func applyMask(t *testing.T, state map[string]any, raw []byte) {
	var b map[string]any
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("PATCH body is not JSON: %s", raw)
	}
	ade := state["adeSetting"].(map[string]any)["ade"].(map[string]any)
	ws := ade["welcomeScreen"].(map[string]any)
	bws, _ := b["welcomeScreen"].(map[string]any)
	stock := map[string]string{"title": "Welcome", "paragraph": "In just a few steps, you will be working securely from your Apple device.", "button": "continue"}
	for _, f := range b["patchMask"].(map[string]any)["fields"].([]any) {
		switch f := f.(string); f {
		case "enable_zero_touch_enrollment":
			v, _ := b["enableZeroTouchEnrollment"].(bool)
			ade["enableZeroTouchEnrollment"] = v
		case "setup_options":
			v, ok := b["setupOptions"]
			if !ok {
				v = []any{}
			}
			ade["setupOptions"] = v
		case "default_device_group_object_ids":
			v, ok := b["defaultDeviceGroupObjectIds"]
			if !ok {
				v = []any{}
			}
			ade["defaultDeviceGroupObjectIds"] = v
		default:
			k := strings.TrimPrefix(f, "welcome_screen.")
			if v, ok := bws[k].(string); ok && v != "" {
				ws[k] = v
			} else {
				ws[k] = stock[k]
			}
		}
	}
}

func settingsArgs(extra map[string]any) map[string]any {
	args := map[string]any{"token_id": "6a7e1bb88cf0be0001a4e3cd", "os_family": "ios"}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

var everyADEWriteTool = []struct {
	tool string
	args map[string]any
}{
	{"apple_mdm_ade_settings_update", settingsArgs(map[string]any{"zero_touch": true})},
	{"apple_mdm_ade_token_refresh_abm_devices", map[string]any{"token_id": "6a7e1bb88cf0be0001a4e3cd"}},
	{"apple_mdm_adue_enable", map[string]any{"apple_mdm_id": "690b6c939c0bf5000100039a"}},
	{"apple_mdm_adue_disable", map[string]any{"apple_mdm_id": "690b6c939c0bf5000100039a"}},
}

func TestADEWritePlansSendNoWrite(t *testing.T) {
	for _, tc := range everyADEWriteTool {
		t.Run(tc.tool, func(t *testing.T) {
			setupToolTest(t)
			srv, seen := startADEWriteRecorder(t, false)
			overrideV2ClientForTest(t, srv.URL)
			cs := connectToolTestServer(t, Options{})

			res := callTool(t, cs, tc.tool, tc.args)
			out := getResultText(t, res)
			if res.IsError {
				t.Fatalf("the plan path errored: %s", out)
			}
			if w := writesIn(*seen); len(w) != 0 {
				t.Errorf("%s sent %d write(s) without execute=true: %+v", tc.tool, len(w), w)
			}
			if !strings.Contains(out, `"plan": true`) {
				t.Errorf("%s without execute did not return a plan: %s", tc.tool, out)
			}
		})
	}
}

func TestADEWritesRefusedOnAReadOnlyServer(t *testing.T) {
	for _, tc := range everyADEWriteTool {
		t.Run(tc.tool, func(t *testing.T) {
			setupToolTest(t)
			srv, seen := startADEWriteRecorder(t, false)
			overrideV2ClientForTest(t, srv.URL)
			cs := connectToolTestServer(t, Options{ReadOnly: true})

			args := map[string]any{"execute": true}
			for k, v := range tc.args {
				args[k] = v
			}
			out := getResultText(t, callTool(t, cs, tc.tool, args))
			if !strings.Contains(out, "read-only") {
				t.Errorf("a read-only server did not refuse %s: %s", tc.tool, out)
			}
			if w := writesIn(*seen); len(w) != 0 {
				t.Errorf("a read-only server sent a write: %+v", w)
			}
		})
	}
}

// The central guard for the settings tool: a field the caller did not pass is
// not in the mask, because a masked field without a value is RESET.
func TestADESettingsUpdateMasksOnlyWhatWasPassed(t *testing.T) {
	setupToolTest(t)
	srv, seen := startADEWriteRecorder(t, false)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "apple_mdm_ade_settings_update",
		settingsArgs(map[string]any{"welcome_button": "Go", "execute": true}))
	if res.IsError {
		t.Fatalf("execute errored: %s", getResultText(t, res))
	}
	w := writesIn(*seen)
	if len(w) != 1 || w[0].method != http.MethodPatch {
		t.Fatalf("expected one PATCH, got %+v", w)
	}
	var body struct {
		PatchMask struct{ Fields []string } `json:"patchMask"`
	}
	if err := json.Unmarshal([]byte(w[0].body), &body); err != nil {
		t.Fatal(err)
	}
	if strings.Join(body.PatchMask.Fields, ",") != "welcome_screen.button" {
		t.Errorf("mask = %v; anything else in it would be reset", body.PatchMask.Fields)
	}
	if !strings.HasSuffix(w[0].path, "/ade_settings/IOS") {
		t.Errorf("patched %s; ios must become IOS", w[0].path)
	}
}

func TestADESettingsPlanShowsBeforeAndAfter(t *testing.T) {
	setupToolTest(t)
	srv, _ := startADEWriteRecorder(t, false)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "apple_mdm_ade_settings_update",
		settingsArgs(map[string]any{"zero_touch": true})))
	if !strings.Contains(out, "zero-touch: false → true") {
		t.Errorf("the plan should say what zero-touch goes from and to: %s", out)
	}
}

// Guards apple_mdm.UnknownGroupIDsAreStored.
func TestADESettingsRefusesAGroupThatDoesNotExist(t *testing.T) {
	setupToolTest(t)
	srv, seen := startADEWriteRecorder(t, false)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "apple_mdm_ade_settings_update", settingsArgs(map[string]any{
		"default_group_ids": []string{"000000000000000000000000"}, "execute": true}))
	if !res.IsError || !strings.Contains(getResultText(t, res), "does not exist") {
		t.Errorf("a nonexistent group was not refused: %s", getResultText(t, res))
	}
	if w := writesIn(*seen); len(w) != 0 {
		t.Errorf("wrote despite the missing group: %+v", w)
	}

	// A group that exists goes through.
	res = callTool(t, cs, "apple_mdm_ade_settings_update", settingsArgs(map[string]any{
		"default_group_ids": []string{knownGroupID}, "execute": true}))
	if res.IsError {
		t.Errorf("an existing group was refused: %s", getResultText(t, res))
	}
}

// A 200 that changed nothing is not success.
func TestADESettingsReportsASilentlyDroppedWrite(t *testing.T) {
	setupToolTest(t)
	srv, _ := startADEWriteRecorder(t, true)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "apple_mdm_ade_settings_update",
		settingsArgs(map[string]any{"zero_touch": true, "execute": true}))
	if !res.IsError || !strings.Contains(getResultText(t, res), "did not change: zero-touch") {
		t.Errorf("a dropped write read as success: %s", getResultText(t, res))
	}
}

func TestADESettingsRefusalsHappenBeforeAnyRequest(t *testing.T) {
	for name, extra := range map[string]map[string]any{
		"nothing":     {},
		"blank":       {"welcome_title": ""},
		"UNKNOWN":     {"setup_options": []string{"UNKNOWN"}},
		"contradicts": {"setup_options": []string{"SIRI_SETUP"}, "clear_setup_options": true},
		"bad reset":   {"reset_welcome": []string{"footer"}},
		"bad family":  {"os_family": "UNKNOWN", "zero_touch": true},
	} {
		t.Run(name, func(t *testing.T) {
			setupToolTest(t)
			srv, seen := startADEWriteRecorder(t, false)
			overrideV2ClientForTest(t, srv.URL)
			cs := connectToolTestServer(t, Options{})

			args := settingsArgs(map[string]any{"execute": true})
			for k, v := range extra {
				args[k] = v
			}
			res := callTool(t, cs, "apple_mdm_ade_settings_update", args)
			if !res.IsError {
				t.Errorf("was not refused: %s", getResultText(t, res))
			}
			if len(*seen) != 0 {
				t.Errorf("a request left before the refusal: %+v", *seen)
			}
		})
	}
}

func TestADUEEnableReturnsTheURLAndSendsAnObject(t *testing.T) {
	setupToolTest(t)
	srv, seen := startADEWriteRecorder(t, false)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "apple_mdm_adue_enable",
		map[string]any{"apple_mdm_id": "690b6c939c0bf5000100039a", "execute": true}))
	if !strings.Contains(out, "account-driven-service-discovery/5ec71e8e96bfda0611fc6c5b") {
		t.Errorf("the chosen URL was not returned: %s", out)
	}
	w := writesIn(*seen)
	if len(w) != 1 || w[0].method != http.MethodPost || strings.TrimSpace(w[0].body) != "{}" {
		t.Errorf("want one POST with body {}, got %+v", w)
	}
}

func TestADUEDisableDeletes(t *testing.T) {
	setupToolTest(t)
	srv, seen := startADEWriteRecorder(t, false)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "apple_mdm_adue_disable",
		map[string]any{"apple_mdm_id": "690b6c939c0bf5000100039a", "execute": true})
	if res.IsError {
		t.Fatalf("disable errored: %s", getResultText(t, res))
	}
	w := writesIn(*seen)
	if len(w) != 1 || w[0].method != http.MethodDelete || !strings.HasSuffix(w[0].path, "/service-discovery-url") {
		t.Errorf("want one DELETE of the discovery URL, got %+v", w)
	}
}
