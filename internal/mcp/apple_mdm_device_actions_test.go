package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The device-action tools, tested against a server that replays the observed
// contract: the device detail read answers 200 with a real record shape, and
// every action answers 204 with no body, which is what the live API does.

// deviceRecordForActions is a trimmed live record (device
// 6adb8d357ec0ff0001d101ea on org 5ec71e8e96bfda0611fc6c5b, 2026-10-23).
const deviceRecordForActions = `{
  "appleMdmId": "690b6c939c0bf5000100039a",
  "id": "6adb8d357ec0ff0001d101ea",
  "serialNumber": "C02TD1PEGTDY",
  "mdmOsFamily": "macOS",
  "osVersion": "12.7.6",
  "enrolled": true,
  "lostMode": false,
  "hasUnlockToken": false,
  "hasActivationLockBypassCodes": false,
  "securityInfo": {"isActivationLockManageable": false, "passcodeCompliant": false}
}`

type recordedRequest struct {
	method string
	path   string
	body   string
}

// startActionRecorder records every request and answers the way the live API
// answers. deviceFound=false makes the device detail read 404, which is how a
// well-formed id belonging to no device behaves.
func startActionRecorder(t *testing.T, deviceFound bool) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	var seen []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, recordedRequest{method: r.Method, path: r.URL.Path, body: string(body)})

		switch {
		case r.Method == http.MethodGet:
			if !deviceFound {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(deviceRecordForActions))
		case r.Method == http.MethodDelete:
			// The unenroll route is the one that answers 200 with the record.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(deviceRecordForActions))
		default:
			// Every action: 204, no body.
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func writesIn(seen []recordedRequest) []recordedRequest {
	var out []recordedRequest
	for _, r := range seen {
		if r.method != http.MethodGet {
			out = append(out, r)
		}
	}
	return out
}

// everyDeviceActionTool is every tool in this file with the minimum arguments
// that reach its handler. Kept complete by
// TestEveryAppleMDMToolTakingAnIDValidatesIt, which fails when a tool is
// added without being registered there.
var everyDeviceActionTool = []struct {
	tool string
	args map[string]any
}{
	{"apple_mdm_device_erase", nil},
	{"apple_mdm_device_lock", nil},
	{"apple_mdm_device_restart", nil},
	{"apple_mdm_device_shutdown", nil},
	{"apple_mdm_device_unenroll", nil},
	{"apple_mdm_device_clear_passcode", nil},
	{"apple_mdm_device_clear_restrictions_password", nil},
	{"apple_mdm_device_clear_activation_lock", nil},
	{"apple_mdm_device_refresh_activation_lock", nil},
	{"apple_mdm_device_os_update_status", nil},
	{"apple_mdm_device_schedule_os_update", map[string]any{
		"install_action": "DOWNLOAD_ONLY", "product_key": "MSU_UPDATE_24A348_patch_15.1"}},
	{"apple_mdm_device_lost_mode", map[string]any{"enable": false}},
}

func actionArgs(extra map[string]any) map[string]any {
	args := map[string]any{
		"apple_mdm_id": "690b6c939c0bf5000100039a",
		"device_id":    "6adb8d357ec0ff0001d101ea",
	}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

// THE guard on this file. Without execute, not one of these tools may send
// anything that is not a read -- an agent that calls apple_mdm_device_erase
// to find out what it would do must not erase anything.
func TestDeviceActionPlansSendNoWrite(t *testing.T) {
	for _, tc := range everyDeviceActionTool {
		t.Run(tc.tool, func(t *testing.T) {
			setupToolTest(t)
			srv, seen := startActionRecorder(t, true)
			overrideV2ClientForTest(t, srv.URL)
			cs := connectToolTestServer(t, Options{})

			res := callTool(t, cs, tc.tool, actionArgs(tc.args))
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

	// The org-level action takes no device id and gets the same treatment.
	t.Run("apple_mdm_refresh_dep_devices", func(t *testing.T) {
		setupToolTest(t)
		srv, seen := startActionRecorder(t, true)
		overrideV2ClientForTest(t, srv.URL)
		cs := connectToolTestServer(t, Options{})

		res := callTool(t, cs, "apple_mdm_refresh_dep_devices",
			map[string]any{"apple_mdm_id": "690b6c939c0bf5000100039a"})
		if res.IsError {
			t.Fatalf("the plan path errored: %s", getResultText(t, res))
		}
		if w := writesIn(*seen); len(w) != 0 {
			t.Errorf("refresh_dep_devices sent %d write(s) without execute=true: %+v", len(w), w)
		}
	})
}

// The plan has to name the machine, not just its id: an agent that resolved
// the wrong device produces a plan an operator can actually catch.
func TestDeviceActionPlanNamesTheMachine(t *testing.T) {
	setupToolTest(t)
	srv, _ := startActionRecorder(t, true)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "apple_mdm_device_erase", actionArgs(nil)))
	if !strings.Contains(out, "C02TD1PEGTDY") {
		t.Errorf("the erase plan does not name the serial number: %s", out)
	}
	if !strings.Contains(out, "next check-in") {
		t.Errorf("the plan does not disclose that the command is queued: %s", out)
	}
}

// Guards apple_mdm.NilBodyIsSentAsNull. A nil body goes out as `null`, which
// the gateway rejects with a 400 SyntaxError before it looks at the device --
// so the action silently never happens. The body on the wire must be {}.
func TestDeviceActionSendsAnEmptyObjectNotNull(t *testing.T) {
	setupToolTest(t)
	srv, seen := startActionRecorder(t, true)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "apple_mdm_device_clear_passcode", actionArgs(map[string]any{"execute": true}))
	if res.IsError {
		t.Fatalf("execute errored: %s", getResultText(t, res))
	}
	writes := writesIn(*seen)
	if len(writes) != 1 {
		t.Fatalf("expected exactly one write, got %+v", writes)
	}
	if !strings.HasSuffix(writes[0].path, "/clearPasscode") {
		t.Errorf("posted to %q, want the camelCase action segment", writes[0].path)
	}
	if got := strings.TrimSpace(writes[0].body); got != "{}" {
		t.Errorf("the body on the wire was %q, want {} — `null` is refused with a 400 "+
			"SyntaxError and the action never happens", got)
	}
}

// Guards apple_mdm.LostModeBothMembersAreAccepted. The API takes a body with
// both members, drops the enable, and says nothing; so exactly one key may
// ever appear on the wire.
func TestLostModeSendsExactlyOneMember(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enable  bool
		present string
		absent  string
	}{
		{"enable", true, "enableLostMode", "disableLostMode"},
		{"disable", false, "disableLostMode", "enableLostMode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupToolTest(t)
			srv, seen := startActionRecorder(t, true)
			overrideV2ClientForTest(t, srv.URL)
			cs := connectToolTestServer(t, Options{})

			args := actionArgs(map[string]any{"enable": tc.enable, "execute": true})
			if tc.enable {
				args["message"] = "Lost — call IT"
			}
			res := callTool(t, cs, "apple_mdm_device_lost_mode", args)
			if res.IsError {
				t.Fatalf("execute errored: %s", getResultText(t, res))
			}
			writes := writesIn(*seen)
			if len(writes) != 1 {
				t.Fatalf("expected exactly one write, got %+v", writes)
			}
			if !strings.Contains(writes[0].body, tc.present) {
				t.Errorf("body %q does not carry %s", writes[0].body, tc.present)
			}
			if strings.Contains(writes[0].body, tc.absent) {
				t.Errorf("body %q carries BOTH oneof members; the API would accept it and "+
					"silently drop one", writes[0].body)
			}
		})
	}
}

// Enabling lost mode with no way to reach anyone is refused locally, before a
// request is made -- the API's own rule, which its spec does not state.
func TestLostModeEnableWithoutMessageOrPhoneIsRefusedBeforeAnyRequest(t *testing.T) {
	setupToolTest(t)
	srv, seen := startActionRecorder(t, true)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "apple_mdm_device_lost_mode",
		actionArgs(map[string]any{"enable": true, "execute": true}))
	if !res.IsError && !strings.Contains(getResultText(t, res), "message or a phone number") {
		t.Errorf("enabling lost mode with neither message nor phone was not refused: %s",
			getResultText(t, res))
	}
	if len(*seen) != 0 {
		t.Errorf("a refused call still reached the API: %+v", *seen)
	}
}

// The device is read BEFORE the action, so a well-formed id that belongs to
// no device fails without anything being sent. The reverse order would send
// an erase and then explain that the device could not be found.
func TestDeviceActionReadsTheDeviceBeforeActing(t *testing.T) {
	setupToolTest(t)
	srv, seen := startActionRecorder(t, false) // the device read 404s
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "apple_mdm_device_erase", actionArgs(map[string]any{"execute": true}))
	out := getResultText(t, res)
	if !strings.Contains(out, "reading device") {
		t.Errorf("the failure does not say the device read is what failed: %s", out)
	}
	if w := writesIn(*seen); len(w) != 0 {
		t.Errorf("an erase was sent for a device that could not be read: %+v", w)
	}
}

// The execute path reports acceptance and discloses that nothing confirms
// completion. A tool that said "the device was erased" would be asserting
// something this API never tells it.
func TestDeviceActionExecuteReportsAcceptedNotDone(t *testing.T) {
	setupToolTest(t)
	srv, _ := startActionRecorder(t, true)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "apple_mdm_device_restart", actionArgs(map[string]any{"execute": true}))
	if res.IsError {
		t.Fatalf("execute errored: %s", getResultText(t, res))
	}
	var out struct {
		Executed bool   `json:"executed"`
		Summary  string `json:"summary"`
		Serial   string `json:"serial_number"`
	}
	if err := json.Unmarshal([]byte(getResultText(t, res)), &out); err != nil {
		t.Fatalf("result did not decode: %v", err)
	}
	if !out.Executed {
		t.Error("executed = false after a successful action")
	}
	if !strings.Contains(out.Summary, "accepted") {
		t.Errorf("summary = %q, want it to say accepted", out.Summary)
	}
	if !strings.Contains(out.Summary, "nothing in this API reports whether it completed") {
		t.Errorf("summary = %q, want the disclosure that completion is not reported", out.Summary)
	}
	if out.Serial != "C02TD1PEGTDY" {
		t.Errorf("serial_number = %q, want the machine named in the result too", out.Serial)
	}
}

// The precondition the device itself reports reaches the plan, so an operator
// sees "this will probably 409" before approving rather than after.
func TestClearActivationLockPlanCarriesThePrecondition(t *testing.T) {
	setupToolTest(t)
	srv, _ := startActionRecorder(t, true)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "apple_mdm_device_clear_activation_lock", actionArgs(nil)))
	if !strings.Contains(out, "isActivationLockManageable") {
		t.Errorf("the plan does not carry the precondition this device reports: %s", out)
	}
}

// A read-only server must refuse to execute, while still being allowed to
// produce a plan -- the plan is a GET.
func TestDeviceActionRefusedOnAReadOnlyServer(t *testing.T) {
	setupToolTest(t)
	srv, seen := startActionRecorder(t, true)
	overrideV2ClientForTest(t, srv.URL)
	cs := connectToolTestServer(t, Options{ReadOnly: true})

	res := callTool(t, cs, "apple_mdm_device_erase", actionArgs(map[string]any{"execute": true}))
	out := getResultText(t, res)
	if !strings.Contains(out, "read-only") {
		t.Errorf("a read-only server did not refuse an erase: %s", out)
	}
	if w := writesIn(*seen); len(w) != 0 {
		t.Errorf("a read-only server sent a write: %+v", w)
	}
}
