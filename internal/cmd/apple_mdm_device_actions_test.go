package cmd

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The device-action commands, against a server that answers the way the live
// API answers: the device read returns a record, every action returns 204
// with no body, and the unenroll DELETE returns the removed record.

const actionDeviceRecord = `{
  "appleMdmId": "690b6c939c0bf5000100039a",
  "id": "6adb8d357ec0ff0001d101ea",
  "serialNumber": "C02TD1PEGTDY",
  "mdmOsFamily": "macOS",
  "osVersion": "12.7.6",
  "enrolled": true,
  "lostMode": false,
  "hasActivationLockBypassCodes": false,
  "securityInfo": {"isActivationLockManageable": false}
}`

type seenRequest struct {
	Method string
	Path   string
	Body   string
}

// startDeviceActionServer records every request. deviceFound=false makes the
// device read 404, which is what a well-formed id belonging to no device
// does.
func startDeviceActionServer(t *testing.T, deviceFound bool) (*httptest.Server, *[]seenRequest) {
	t.Helper()
	var seen []seenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, seenRequest{Method: r.Method, Path: r.URL.Path, Body: string(body)})
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/applemdms":
			_, _ = w.Write([]byte(`[{"id":"690b6c939c0bf5000100039a","name":"Klaassen Consulting"}]`))
		case r.Method == http.MethodGet:
			if !deviceFound {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
			_, _ = w.Write([]byte(actionDeviceRecord))
		case r.Method == http.MethodDelete:
			_, _ = w.Write([]byte(actionDeviceRecord))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func writeRequests(seen []seenRequest) []seenRequest {
	var out []seenRequest
	for _, r := range seen {
		if r.Method != http.MethodGet {
			out = append(out, r)
		}
	}
	return out
}

func runActionCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errBuf.String(), err
}

const (
	testMDMID    = "690b6c939c0bf5000100039a"
	testDeviceID = "6adb8d357ec0ff0001d101ea"
)

// --plan must make no write, and must say which machine it is talking about.
func TestAppleMDMEraseePlanMakesNoWrite(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startDeviceActionServer(t, true)
	overrideV2Client(t, srv.URL)

	_, stderr, err := runActionCmd(t, "apple-mdm", "erase", testMDMID, testDeviceID,
		"--confirm-erase", "--pin", "123456", "--plan")
	if err == nil {
		t.Fatal("--plan should exit with the plan code, got nil")
	}
	if w := writeRequests(*seen); len(w) != 0 {
		t.Errorf("--plan sent %d write(s): %+v", len(w), w)
	}
	if !strings.Contains(stderr, "C02TD1PEGTDY") {
		t.Errorf("the plan does not name the machine: %s", stderr)
	}
	if !strings.Contains(stderr, "Irreversible") {
		t.Errorf("the plan does not say the erase is irreversible: %s", stderr)
	}
}

// The safety flag is checked BEFORE anything else, so no request is made at
// all — not even the device read. (The same ordering bug was caught on
// `jc devices erase` in PR #72: a plan rendered before the gate ran.)
func TestAppleMDMEraseRequiresConfirmFlagBeforeAnyRequest(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startDeviceActionServer(t, true)
	overrideV2Client(t, srv.URL)

	_, _, err := runActionCmd(t, "apple-mdm", "erase", testMDMID, testDeviceID, "--plan")
	if err == nil || !strings.Contains(err.Error(), "--confirm-erase") {
		t.Fatalf("erase without --confirm-erase: err = %v, want it to demand the flag", err)
	}
	if len(*seen) != 0 {
		t.Errorf("a refused erase still reached the API: %+v", *seen)
	}
}

// A malformed PIN is refused locally, so a typo never becomes a request.
func TestAppleMDMLockRefusesABadPINBeforeAnyRequest(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startDeviceActionServer(t, true)
	overrideV2Client(t, srv.URL)

	_, _, err := runActionCmd(t, "apple-mdm", "device-lock", "send", testMDMID, testDeviceID, "--pin", "abc")
	if err == nil || !strings.Contains(err.Error(), "six digits") {
		t.Fatalf("a non-numeric PIN: err = %v, want a six-digit complaint", err)
	}
	if len(*seen) != 0 {
		t.Errorf("a refused lock still reached the API: %+v", *seen)
	}
}

// Enabling lost mode with no way to reach anyone is the API's rule, applied
// locally so it costs nothing.
func TestAppleMDMLostModeEnableNeedsMessageOrPhone(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startDeviceActionServer(t, true)
	overrideV2Client(t, srv.URL)

	_, _, err := runActionCmd(t, "apple-mdm", "lost-mode", "enable", testMDMID, testDeviceID)
	if err == nil || !strings.Contains(err.Error(), "message or a phone number") {
		t.Fatalf("lost-mode enable with neither: err = %v", err)
	}
	if len(*seen) != 0 {
		t.Errorf("a refused lost-mode enable still reached the API: %+v", *seen)
	}
}

// Answering anything but yes cancels, and nothing is sent. The prompt names
// the machine, which is the point of reading the device first: an operator
// who sees a serial they do not recognise can stop here.
func TestAppleMDMDestructiveActionCancelsOnNo(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startDeviceActionServer(t, true)
	overrideV2Client(t, srv.URL)

	orig := confirmReader
	confirmReader = bufio.NewReader(strings.NewReader("n\n"))
	t.Cleanup(func() { confirmReader = orig })

	_, stderr, err := runActionCmd(t, "apple-mdm", "restart", testMDMID, testDeviceID)
	if err != nil {
		t.Fatalf("expected a clean cancellation, got %v", err)
	}
	if !strings.Contains(stderr, "Cancelled") {
		t.Errorf("stderr = %q, want a cancellation", stderr)
	}
	if !strings.Contains(stderr, "C02TD1PEGTDY") {
		t.Errorf("the prompt does not name the machine: %q", stderr)
	}
	if w := writeRequests(*seen); len(w) != 0 {
		t.Errorf("a restart was sent after being declined: %+v", w)
	}
}

// And with no answer available at all, the command fails rather than
// proceeding. Silence is not consent: this is the path a cron job or a
// detached process takes when it forgets --force.
func TestAppleMDMDestructiveActionWithNoAnswerSendsNothing(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startDeviceActionServer(t, true)
	overrideV2Client(t, srv.URL)

	orig := confirmReader
	confirmReader = bufio.NewReader(strings.NewReader(""))
	t.Cleanup(func() { confirmReader = orig })

	if _, _, err := runActionCmd(t, "apple-mdm", "restart", testMDMID, testDeviceID); err == nil {
		t.Error("a restart with no answer available succeeded")
	}
	if w := writeRequests(*seen); len(w) != 0 {
		t.Errorf("a restart was sent with no confirmation available: %+v", w)
	}
}

// Guards apple_mdm.NilBodyIsSentAsNull on the CLI path: the body on the wire
// must be {} and not `null`, which the gateway refuses with a 400 before it
// looks at the device.
func TestAppleMDMActionSendsAnEmptyObjectNotNull(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startDeviceActionServer(t, true)
	overrideV2Client(t, srv.URL)

	stdout, _, err := runActionCmd(t, "apple-mdm", "clear-passcode", testMDMID, testDeviceID, "--force")
	if err != nil {
		t.Fatalf("clear-passcode: %v", err)
	}
	writes := writeRequests(*seen)
	if len(writes) != 1 {
		t.Fatalf("expected exactly one write, got %+v", writes)
	}
	if !strings.HasSuffix(writes[0].Path, "/clearPasscode") {
		t.Errorf("posted to %q, want the camelCase segment", writes[0].Path)
	}
	if got := strings.TrimSpace(writes[0].Body); got != "{}" {
		t.Errorf("body on the wire = %q, want {}", got)
	}
	if !strings.Contains(stdout, "accepted") {
		t.Errorf("stdout = %q, want it to report acceptance", stdout)
	}
	if !strings.Contains(stdout, "nothing in this API reports whether it completed") {
		t.Errorf("stdout = %q, want the disclosure that completion is not reported", stdout)
	}
}

// The three that change nothing on the device do not prompt at all, since
// there is nothing to undo.
func TestAppleMDMSafeActionsDoNotPrompt(t *testing.T) {
	for _, args := range [][]string{
		{"apple-mdm", "os-update", "status", testMDMID, testDeviceID},
		{"apple-mdm", "refresh-activation-lock", testMDMID, testDeviceID},
		{"apple-mdm", "refresh-dep-devices", testMDMID},
	} {
		t.Run(strings.Join(args[1:3], "-"), func(t *testing.T) {
			setupUsersTest(t)
			srv, seen := startDeviceActionServer(t, true)
			overrideV2Client(t, srv.URL)

			stdout, stderr, err := runActionCmd(t, args...)
			if err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			if strings.Contains(stderr, "Cancelled") {
				t.Errorf("a non-destructive action prompted and cancelled: %s", stderr)
			}
			if len(writeRequests(*seen)) != 1 {
				t.Errorf("expected exactly one write, got %+v", writeRequests(*seen))
			}
			if !strings.Contains(stdout, "accepted") {
				t.Errorf("stdout = %q", stdout)
			}
		})
	}
}

// A well-formed device id belonging to no device fails on the read, before
// anything is sent.
func TestAppleMDMActionOnAMissingDeviceSendsNothing(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startDeviceActionServer(t, false)
	overrideV2Client(t, srv.URL)

	_, _, err := runActionCmd(t, "apple-mdm", "shutdown", testMDMID, "000000000000000000000000", "--force")
	if err == nil {
		t.Fatal("a shutdown on a nonexistent device succeeded")
	}
	if !strings.Contains(err.Error(), "no device 000000000000000000000000 is managed by") {
		t.Errorf("err = %v, want it to name the device and the configuration", err)
	}
	if w := writeRequests(*seen); len(w) != 0 {
		t.Errorf("a shutdown was sent for a device that does not exist: %+v", w)
	}
}

// The unenroll route answers 200 with the removed record rather than 204, so
// the record is what gets printed.
func TestAppleMDMUnenrollPrintsTheRemovedRecord(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startDeviceActionServer(t, true)
	overrideV2Client(t, srv.URL)

	stdout, _, err := runActionCmd(t, "apple-mdm", "unenroll", testMDMID, testDeviceID, "--force")
	if err != nil {
		t.Fatalf("unenroll: %v", err)
	}
	writes := writeRequests(*seen)
	if len(writes) != 1 || writes[0].Method != http.MethodDelete {
		t.Fatalf("expected one DELETE, got %+v", writes)
	}
	if !strings.Contains(stdout, "C02TD1PEGTDY") {
		t.Errorf("stdout = %q, want the removed device record", stdout)
	}
}

// The precondition the device reports about itself reaches the plan.
func TestAppleMDMClearActivationLockPlanCarriesThePrecondition(t *testing.T) {
	setupUsersTest(t)
	srv, _ := startDeviceActionServer(t, true)
	overrideV2Client(t, srv.URL)

	_, stderr, err := runActionCmd(t, "apple-mdm", "clear-activation-lock", testMDMID, testDeviceID, "--plan")
	if err == nil {
		t.Fatal("--plan should exit with the plan code")
	}
	if !strings.Contains(stderr, "isActivationLockManageable") {
		t.Errorf("the plan does not carry the precondition: %s", stderr)
	}
}

// The schedule command refuses an install action the API's enum does not
// list, and demands the product key the API requires.
func TestAppleMDMScheduleOSUpdateValidatesLocally(t *testing.T) {
	setupUsersTest(t)
	srv, seen := startDeviceActionServer(t, true)
	overrideV2Client(t, srv.URL)

	_, _, err := runActionCmd(t, "apple-mdm", "os-update", "schedule", testMDMID, testDeviceID,
		"--install-action", "BOGUS", "--product-key", "x")
	if err == nil || !strings.Contains(err.Error(), "BOGUS") {
		t.Fatalf("a bad install action: err = %v", err)
	}

	_, _, err = runActionCmd(t, "apple-mdm", "os-update", "schedule", testMDMID, testDeviceID,
		"--install-action", "DOWNLOAD_ONLY")
	if err == nil || !strings.Contains(err.Error(), "product key") {
		t.Fatalf("a missing product key: err = %v", err)
	}
	if len(*seen) != 0 {
		t.Errorf("a locally-refused schedule reached the API: %+v", *seen)
	}
}
