package cmd

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The slice-4 commands against a server replaying the 2026-10-28 contract.
// The PATCH answers with the setting it was sent applied to the live IOS
// record, or — with dropWrites — unchanged, the shape of a write the API
// accepted and did nothing with.

const (
	testADETokenID    = "6a7e1bb88cf0be0001a4e3cd"
	testDeviceGroupID = "609384d91f24757289141557"
)

const iosSettingEnvelope = `{"adeSetting":{"objectId":"690bf92bc4816d00015b8c41","osFamily":"IOS",
"ade":{"welcomeScreen":{"title":"Welcome","paragraph":"In just a few steps, you will be working securely from your Apple device.","button":"continue"},
"setupOptions":[],"enableZeroTouchEnrollment":false,"defaultDeviceGroupObjectIds":[]}}}`

func startADEWriteServer(t *testing.T, dropWrites bool) (*httptest.Server, *[]seenRequest) {
	t.Helper()
	var seen []seenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, seenRequest{Method: r.Method, Path: r.URL.Path, Body: string(body)})
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/applemdms":
			_, _ = w.Write([]byte(`[{"id":"690b6c939c0bf5000100039a","name":"Klaassen Consulting"}]`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/systemgroups/"):
			if strings.HasSuffix(r.URL.Path, testDeviceGroupID) {
				_, _ = w.Write([]byte(`{"id":"` + testDeviceGroupID + `","name":"All Macs"}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(iosSettingEnvelope))
		case r.Method == http.MethodPatch:
			if dropWrites {
				_, _ = w.Write([]byte(iosSettingEnvelope))
				return
			}
			// Apply zero-touch only; enough for the tests below.
			var b struct {
				Z *bool `json:"enableZeroTouchEnrollment"`
			}
			_ = json.Unmarshal(body, &b)
			out := iosSettingEnvelope
			if b.Z != nil && *b.Z {
				out = strings.Replace(out, `"enableZeroTouchEnrollment":false`, `"enableZeroTouchEnrollment":true`, 1)
			}
			_, _ = w.Write([]byte(out))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/service-discovery-url"):
			_, _ = w.Write([]byte(`{"serviceDiscoveryUrl":"https://apple.mdm.jumpcloud.com/account-driven-service-discovery/5ec71e8e96bfda0611fc6c5b"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestADESettingsUpdatePlanShowsBeforeAfterAndWritesNothing(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startADEWriteServer(t, false)
	overrideV2Client(t, srv.URL)

	_, stderr, err := runActionCmd(t, "apple-mdm", "ade-settings", "update", testADETokenID, "ios",
		"--zero-touch=true", "--plan", "-o", "human")
	if err == nil {
		t.Fatal("--plan should exit with the plan code")
	}
	if w := writeRequests(*seen); len(w) != 0 {
		t.Errorf("--plan wrote: %+v", w)
	}
	if !strings.Contains(stderr, "zero-touch: false → true") {
		t.Errorf("the plan does not show before and after: %s", stderr)
	}
}

// The mask carries only the flags that were passed. Anything else in it would
// be reset by the service.
func TestADESettingsUpdateMasksOnlyTheFlagsPassed(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startADEWriteServer(t, false)
	overrideV2Client(t, srv.URL)

	if _, _, err := runActionCmd(t, "apple-mdm", "ade-settings", "update", testADETokenID, "ios",
		"--zero-touch=true"); err != nil {
		t.Fatal(err)
	}
	w := writeRequests(*seen)
	if len(w) != 1 || w[0].Method != http.MethodPatch {
		t.Fatalf("want one PATCH, got %+v", w)
	}
	if !strings.Contains(w[0].Body, `"patchMask":{"fields":["enable_zero_touch_enrollment"]}`) {
		t.Errorf("mask is not exactly zero-touch: %s", w[0].Body)
	}
	if !strings.HasSuffix(w[0].Path, "/ade_settings/IOS") {
		t.Errorf("patched %s", w[0].Path)
	}
}

func TestADESettingsUpdateRefusalsHappenBeforeAnyRequest(t *testing.T) {
	for name, args := range map[string][]string{
		"nothing":     {},
		"blank title": {"--welcome-title", ""},
		"UNKNOWN":     {"--setup-options", "UNKNOWN"},
		"contradicts": {"--setup-options", "SIRI_SETUP", "--clear-setup-options"},
		"bad reset":   {"--reset-welcome", "footer"},
		"bad token":   {"--zero-touch=true", "--token-placeholder"},
	} {
		t.Run(name, func(t *testing.T) {
			setupUsersTest(t)
			srv, seen := startADEWriteServer(t, false)
			overrideV2Client(t, srv.URL)

			token := testADETokenID
			if name == "bad token" {
				token, args = "../../systemusers", args[:1]
			}
			full := append([]string{"apple-mdm", "ade-settings", "update", token, "ios"}, args...)
			if _, _, err := runActionCmd(t, full...); err == nil {
				t.Error("was not refused")
			}
			if len(*seen) != 0 {
				t.Errorf("a request left before the refusal: %+v", *seen)
			}
		})
	}
}

// Guards apple_mdm.UnknownGroupIDsAreStored: a well-formed id belonging to no
// group is looked up and refused, and nothing is written.
func TestADESettingsUpdateRefusesAGroupThatDoesNotExist(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startADEWriteServer(t, false)
	overrideV2Client(t, srv.URL)

	_, _, err := runActionCmd(t, "apple-mdm", "ade-settings", "update", testADETokenID, "ios",
		"--default-group", "000000000000000000000000")
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err = %v, want a refusal naming the missing group", err)
	}
	if w := writeRequests(*seen); len(w) != 0 {
		t.Errorf("wrote despite the missing group: %+v", w)
	}
}

func TestADESettingsUpdateReportsASilentlyDroppedWrite(t *testing.T) {
	setupUsersTest(t)
	srv, _ := startADEWriteServer(t, true)
	overrideV2Client(t, srv.URL)

	_, _, err := runActionCmd(t, "apple-mdm", "ade-settings", "update", testADETokenID, "ios", "--zero-touch=true")
	if err == nil || !strings.Contains(err.Error(), "did not change: zero-touch") {
		t.Errorf("a 200 that changed nothing read as success: %v", err)
	}
}

func TestADUEEnablePrintsTheChosenURL(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startADEWriteServer(t, false)
	overrideV2Client(t, srv.URL)

	stdout, _, err := runActionCmd(t, "apple-mdm", "adue", "enable", testMDMID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "account-driven-service-discovery/5ec71e8e96bfda0611fc6c5b") {
		t.Errorf("the URL was not printed: %s", stdout)
	}
	if w := writeRequests(*seen); len(w) != 1 || strings.TrimSpace(w[0].Body) != "{}" {
		t.Errorf("want one POST with {}, got %+v", w)
	}
}

func TestADUEDisableCancelsOnNo(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startADEWriteServer(t, false)
	overrideV2Client(t, srv.URL)
	orig := confirmReader
	confirmReader = bufio.NewReader(strings.NewReader("n\n"))
	t.Cleanup(func() { confirmReader = orig })

	_, stderr, err := runActionCmd(t, "apple-mdm", "adue", "disable", testMDMID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "Cancelled") {
		t.Errorf("no cancellation reported: %s", stderr)
	}
	if w := writeRequests(*seen); len(w) != 0 {
		t.Errorf("a cancelled disable still wrote: %+v", w)
	}
}

func TestADUEDisableWithForceDeletes(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startADEWriteServer(t, false)
	overrideV2Client(t, srv.URL)

	if _, _, err := runActionCmd(t, "apple-mdm", "adue", "disable", testMDMID, "--force"); err != nil {
		t.Fatal(err)
	}
	w := writeRequests(*seen)
	if len(w) != 1 || w[0].Method != http.MethodDelete || !strings.HasSuffix(w[0].Path, "/service-discovery-url") {
		t.Errorf("want one DELETE of the discovery URL, got %+v", w)
	}
}

func TestRefreshABMDevicesPostsAnObject(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startADEWriteServer(t, false)
	overrideV2Client(t, srv.URL)

	if _, _, err := runActionCmd(t, "apple-mdm", "ade-tokens", "refresh-abm-devices", testADETokenID); err != nil {
		t.Fatal(err)
	}
	w := writeRequests(*seen)
	if len(w) != 1 || !strings.HasSuffix(w[0].Path, "/ade_tokens/"+testADETokenID+"/refresh_abm_devices") ||
		strings.TrimSpace(w[0].Body) != "{}" {
		t.Errorf("want one POST of {} to the token's refresh route, got %+v", w)
	}
}
