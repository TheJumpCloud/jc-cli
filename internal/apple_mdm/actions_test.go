package apple_mdm

import (
	"encoding/json"
	"strings"
	"testing"
)

// Every status and body in this file was observed live on 2026-10-23 against
// org 5ec71e8e96bfda0611fc6c5b. The bodies marked "accepted" were accepted by
// the API against device id 000000000000000000000000, which belongs to no
// device -- see ValidationRunsBeforeTheDeviceLookup. None of them was ever
// sent to a real machine.

func TestDeviceActionEndpoints(t *testing.T) {
	const mdm, dev = "690b6c939c0bf5000100039a", "69c45aabb695320001aff063"

	if got, want := DeviceActionEndpoint(mdm, dev, ActionErase),
		"/applemdms/690b6c939c0bf5000100039a/devices/69c45aabb695320001aff063/erase"; got != want {
		t.Errorf("erase endpoint = %q, want %q", got, want)
	}
	// The camelCase segments are the API's spelling and must survive
	// untouched; a lowercased clearpasscode is a 404.
	if got := DeviceActionEndpoint(mdm, dev, ActionClearPasscode); !strings.HasSuffix(got, "/clearPasscode") {
		t.Errorf("clear-passcode endpoint = %q, want the camelCase segment preserved", got)
	}
	if got := DeviceActionEndpoint(mdm, dev, ActionRefreshActivationLock); !strings.HasSuffix(got, "/refreshActivationLockInformation") {
		t.Errorf("refresh endpoint = %q", got)
	}
	// lockwithgeneratedpin and lostmode are all-lowercase in the API, while
	// their neighbours are camelCase. Preserved, not normalized.
	if got := DeviceActionEndpoint(mdm, dev, ActionLockWithGeneratedPIN); !strings.HasSuffix(got, "/lockwithgeneratedpin") {
		t.Errorf("lock-with-generated-pin endpoint = %q", got)
	}
	if got, want := RefreshDEPDevicesEndpoint(mdm),
		"/applemdms/690b6c939c0bf5000100039a/refreshdepdevices"; got != want {
		t.Errorf("refreshdepdevices endpoint = %q, want %q", got, want)
	}
}

// Guards NilBodyIsSentAsNull. The second half is the point: it records the
// behaviour that makes the first half necessary, so this test still fails if
// someone "simplifies" EmptyActionBody back to nil.
func TestEmptyActionBodyIsAnObjectNotNull(t *testing.T) {
	got, err := json.Marshal(EmptyActionBody())
	if err != nil {
		t.Fatalf("marshalling the empty action body: %v", err)
	}
	if string(got) != "{}" {
		t.Errorf("EmptyActionBody marshals to %s, want {}", got)
	}

	var nilBody any
	nullBytes, err := json.Marshal(nilBody)
	if err != nil {
		t.Fatalf("marshalling nil: %v", err)
	}
	if string(nullBytes) != "null" {
		t.Fatalf("a nil body marshals to %s; this test's premise is stale", nullBytes)
	}
}

func TestValidatePIN(t *testing.T) {
	// Empty is allowed through deliberately: whether macOS requires the PIN
	// is unverified, and the API is the only thing that knows.
	if err := ValidatePIN(""); err != nil {
		t.Errorf("an empty PIN must be passed through, got %v", err)
	}
	if err := ValidatePIN("123456"); err != nil {
		t.Errorf("ValidatePIN(123456) = %v, want nil", err)
	}
	// Each of these was rejected live by the gateway with
	// `must match pattern "^[0-9]{6}$"`.
	for _, bad := range []string{"abc", "1", "1234567", "12345a", " 123456", "123456 "} {
		err := ValidatePIN(bad)
		if err == nil {
			t.Errorf("ValidatePIN(%q) = nil, want an error", bad)
			continue
		}
		// The rejected value must not appear in the message: an error string
		// travels into logs and transcripts, and a near-miss of a PIN is
		// still a PIN.
		if strings.Contains(err.Error(), bad) {
			t.Errorf("ValidatePIN(%q) echoed the rejected value: %v", bad, err)
		}
	}
}

func TestRestartBodyOmitsKextPathsWhenEmpty(t *testing.T) {
	got, err := json.Marshal(RestartBody{})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if string(got) != "{}" {
		t.Errorf("an empty restart body marshals to %s, want {}", got)
	}
	got, err = json.Marshal(RestartBody{KextPaths: []string{"/a/b.kext"}})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if string(got) != `{"kextPaths":["/a/b.kext"]}` {
		t.Errorf("restart body = %s", got)
	}
}

// ---------------------------------------------------------------- lost mode

// The bodies here are exactly the ones the API accepted or rejected.
func TestLostModeBodiesMatchWhatTheAPIAccepted(t *testing.T) {
	enable, err := EnableLostMode("Lost — call IT", "", "")
	if err != nil {
		t.Fatalf("EnableLostMode: %v", err)
	}
	got, err := json.Marshal(enable)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	// Live: {"enableLostMode":{"message":"probe"}} -> 404, i.e. the body
	// passed validation.
	if string(got) != `{"enableLostMode":{"message":"Lost — call IT"}}` {
		t.Errorf("enable body = %s", got)
	}

	// Live: {"disableLostMode":{}} -> 404. The key's presence is the command,
	// so the empty object must survive marshalling.
	got, err = json.Marshal(DisableLostMode())
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if string(got) != `{"disableLostMode":{}}` {
		t.Errorf("disable body = %s, want the empty object to be sent", got)
	}
}

// Guards LostModeEnableNeedsAWayToBeReached -- the rule the spec omits.
func TestEnableLostModeNeedsMessageOrPhone(t *testing.T) {
	if _, err := EnableLostMode("", "", ""); err == nil {
		t.Error("EnableLostMode with neither message nor phone = nil, want an error " +
			"(live: 400 'message or phone number must be provided to enable Lost Mode')")
	}
	// A footnote alone is NOT enough: live, {"enableLostMode":{"footnote":"x"}}
	// still answered 400 with the same message.
	if _, err := EnableLostMode("", "", "footnote only"); err == nil {
		t.Error("a footnote alone must not satisfy the rule — the API rejects it")
	}
	if _, err := EnableLostMode("", "+15551234567", ""); err != nil {
		t.Errorf("a phone number alone is accepted live, got %v", err)
	}
	// Whitespace is not a message. The API would take it; it would also be
	// useless on a lock screen.
	if _, err := EnableLostMode("   ", "", ""); err == nil {
		t.Error("a whitespace-only message must not satisfy the rule")
	}
}

// Guards LostModeBothMembersAreAccepted. This is the one guard here standing
// between a user and an action the API performs without saying which.
func TestLostModeRefusesBothMembers(t *testing.T) {
	both := LostModeBody{Enable: &LostModeEnable{Message: "m"}, Disable: &LostModeDisable{}}
	if err := both.Validate(); err == nil {
		t.Error("a body with both oneof members = nil, want an error: the API accepts it, " +
			"drops the enable, and never says so")
	}
	if err := (LostModeBody{}).Validate(); err == nil {
		t.Error("an empty lost-mode body = nil, want an error (live: 400 'command is required')")
	}

	enable, err := EnableLostMode("m", "", "")
	if err != nil {
		t.Fatalf("EnableLostMode: %v", err)
	}
	if err := enable.Validate(); err != nil {
		t.Errorf("a valid enable body: %v", err)
	}
	if !enable.Enabling() {
		t.Error("Enabling() = false for an enable body")
	}
	disable := DisableLostMode()
	if err := disable.Validate(); err != nil {
		t.Errorf("a valid disable body: %v", err)
	}
	if disable.Enabling() {
		t.Error("Enabling() = true for a disable body")
	}
}

// The constructors are the only way in, so there is no path by which a caller
// assembles both members from flags and lets Go's map ordering decide.
func TestLostModeConstructorsSetExactlyOneMember(t *testing.T) {
	enable, err := EnableLostMode("m", "p", "f")
	if err != nil {
		t.Fatalf("EnableLostMode: %v", err)
	}
	if enable.Disable != nil {
		t.Error("EnableLostMode also set the disable member")
	}
	if DisableLostMode().Enable != nil {
		t.Error("DisableLostMode also set the enable member")
	}
}

// -------------------------------------------------------------- OS updates

func TestNewOSUpdateBody(t *testing.T) {
	// Live: {"install_action":"BOGUS","product_key":"x"} -> 400
	// "no enum match for: BOGUS".
	if _, err := NewOSUpdateBody("BOGUS", "x", nil); err == nil {
		t.Error("an unlisted install action = nil, want an error")
	}
	// Live: {} -> 400 "must have required property 'install_action',
	// must have required property 'product_key'".
	if _, err := NewOSUpdateBody(InstallActionDownloadOnly, "", nil); err == nil {
		t.Error("a missing product key = nil, want an error")
	}
	body, err := NewOSUpdateBody(InstallActionDownloadOnly, "MSU_UPDATE_24A348_patch_15.1", nil)
	if err != nil {
		t.Fatalf("NewOSUpdateBody: %v", err)
	}
	got, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if string(got) != `{"install_action":"DOWNLOAD_ONLY","product_key":"MSU_UPDATE_24A348_patch_15.1"}` {
		t.Errorf("os update body = %s", got)
	}

	// Zero deferrals is a real instruction and must not be dropped as if it
	// had never been asked for.
	zero := 0
	body, err = NewOSUpdateBody(InstallActionInstallASAP, "k", &zero)
	if err != nil {
		t.Fatalf("NewOSUpdateBody: %v", err)
	}
	got, err = json.Marshal(body)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if !strings.Contains(string(got), `"max_user_deferrals":0`) {
		t.Errorf("os update body = %s, want max_user_deferrals:0 to be sent", got)
	}

	if len(InstallActions()) != 4 {
		t.Errorf("InstallActions() returned %d values, want the 4 the gateway enum declares",
			len(InstallActions()))
	}
}

// ------------------------------------------------------------ preconditions

// A device record observed live: the oldest Mac on the tenant, which reports
// activation lock as neither manageable nor bypassable.
const deviceNotActivationLockManageable = `{
  "appleMdmId": "690b6c939c0bf5000100039a",
  "id": "6adb8d357ec0ff0001d101ea",
  "serialNumber": "C02TD1PEGTDY",
  "mdmOsFamily": "macOS",
  "osVersion": "12.7.6",
  "enrolled": true,
  "lostMode": false,
  "hasUnlockToken": false,
  "hasActivationLockBypassCodes": false,
  "securityInfo": {
    "isActivationLockManageable": false,
    "passcodeCompliant": false,
    "filevaultEnabled": false,
    "recoveryLockEnabled": false
  }
}`

// And one that reports it available -- six of the eight did.
const deviceActivationLockManageable = `{
  "appleMdmId": "690b6c939c0bf5000100039a",
  "id": "69c45aabb695320001aff063",
  "serialNumber": "F60GG20MC2",
  "mdmOsFamily": "macOS",
  "osVersion": "27.2",
  "enrolled": true,
  "lostMode": false,
  "hasUnlockToken": false,
  "hasActivationLockBypassCodes": true,
  "securityInfo": {
    "isActivationLockManageable": true,
    "passcodeCompliant": false,
    "recoveryLockEnabled": true
  }
}`

func TestParsePreconditions(t *testing.T) {
	p, err := ParsePreconditions(json.RawMessage(deviceNotActivationLockManageable))
	if err != nil {
		t.Fatalf("ParsePreconditions: %v", err)
	}
	if p.SerialNumber != "C02TD1PEGTDY" {
		t.Errorf("serial = %q", p.SerialNumber)
	}
	if p.OSFamily != "macOS" {
		t.Errorf("mdmOsFamily = %q, want it reported as received", p.OSFamily)
	}
	if p.HasActivationLockBypassCodes {
		t.Error("hasActivationLockBypassCodes = true, want false")
	}
	if p.SecurityInfo.IsActivationLockManageable {
		t.Error("isActivationLockManageable = true, want false")
	}

	p2, err := ParsePreconditions(json.RawMessage(deviceActivationLockManageable))
	if err != nil {
		t.Fatalf("ParsePreconditions: %v", err)
	}
	if !p2.SecurityInfo.IsActivationLockManageable || !p2.HasActivationLockBypassCodes {
		t.Error("the manageable device decoded as not manageable")
	}
}

// lostMode was a bare boolean on all eight devices. The shape when it is
// ACTIVE has never been seen, so an unrecognized shape must read as unknown
// rather than as "not in lost mode".
func TestLostModeActiveDistinguishesUnknownFromFalse(t *testing.T) {
	p, err := ParsePreconditions(json.RawMessage(deviceNotActivationLockManageable))
	if err != nil {
		t.Fatalf("ParsePreconditions: %v", err)
	}
	active, known := p.LostModeActive()
	if active || !known {
		t.Errorf("lostMode false -> active=%v known=%v, want false/true", active, known)
	}

	p.LostMode = json.RawMessage(`true`)
	if active, known = p.LostModeActive(); !active || !known {
		t.Errorf("lostMode true -> active=%v known=%v, want true/true", active, known)
	}

	// The shape nobody has seen: an object. It must not read as "false".
	p.LostMode = json.RawMessage(`{"enabled":true,"message":"call IT"}`)
	if active, known = p.LostModeActive(); known {
		t.Errorf("an unseen lostMode shape reported known=true (active=%v); "+
			"an unrecognized shape is not evidence of absence", active)
	}

	// Absent entirely is also not "false".
	p.LostMode = nil
	if _, known = p.LostModeActive(); known {
		t.Error("an absent lostMode reported known=true")
	}
}

func TestPreconditionNote(t *testing.T) {
	notManageable, err := ParsePreconditions(json.RawMessage(deviceNotActivationLockManageable))
	if err != nil {
		t.Fatalf("ParsePreconditions: %v", err)
	}
	note := notManageable.PreconditionNote(ActionClearActivationLock)
	if !strings.Contains(note, "isActivationLockManageable") {
		t.Errorf("clear-activation-lock note = %q, want the field named", note)
	}

	manageable, err := ParsePreconditions(json.RawMessage(deviceActivationLockManageable))
	if err != nil {
		t.Fatalf("ParsePreconditions: %v", err)
	}
	if note := manageable.PreconditionNote(ActionClearActivationLock); note != "" {
		t.Errorf("a manageable device produced a warning: %q", note)
	}

	// macOS devices get told about the PIN, because the spec says it is
	// required for macOS while declaring it optional.
	if note := manageable.PreconditionNote(ActionErase); !strings.Contains(note, "PIN") {
		t.Errorf("erase note = %q, want the macOS PIN requirement mentioned", note)
	}

	// An unrecognized lostMode shape is surfaced rather than ignored.
	manageable.LostMode = json.RawMessage(`{"enabled":true}`)
	if note := manageable.PreconditionNote(ActionLostMode); !strings.Contains(note, "unrecognized") {
		t.Errorf("lost-mode note for an unseen shape = %q", note)
	}
}

// ------------------------------------------------------------------ outcome

// Guards AcceptedIsNotCompleted. A 204 is the only thing these routes return,
// nothing reports completion afterwards, and so no surface is allowed to
// claim the device did anything.
func TestOutcomeSaysAcceptedAndDisclosesThatNothingConfirms(t *testing.T) {
	o := Outcome{Action: "erase", DeviceID: "69c45aabb695320001aff063",
		MDMID: "690b6c939c0bf5000100039a", Accepted: true}
	got := o.Describe()
	if !strings.Contains(got, "accepted") {
		t.Errorf("Describe() = %q, want it to say the command was accepted", got)
	}
	if !strings.Contains(got, "nothing in this API reports whether it completed") {
		t.Errorf("Describe() = %q, want the disclosure that completion is not reported — "+
			"background_tasks stays empty, so 'accepted' is the strongest true statement", got)
	}
	if !strings.Contains(got, "check-in") {
		t.Errorf("Describe() = %q, want the queue-until-check-in semantics stated", got)
	}

	// The org-level action has no device, so it must not borrow the per-device
	// wording: refreshdepdevices is a server-side re-sync with Apple, and
	// there is no device whose check-in it waits for. The first version of
	// this test only looked for "device " and passed on output that said "the
	// device's next check-in" — true of twelve of the fourteen actions and
	// false of this one.
	orgLevel := Outcome{Action: "refresh-dep-devices", MDMID: "690b6c939c0bf5000100039a", Accepted: true}
	got = orgLevel.Describe()
	// Not a bare "device" check: the action is itself called
	// refresh-dep-devices, so the word legitimately appears in its name.
	if strings.Contains(got, "for device ") {
		t.Errorf("Describe() = %q, want the configuration named rather than a device", got)
	}
	if strings.Contains(got, "check-in") {
		t.Errorf("Describe() = %q, want no check-in claim: this action waits on no device", got)
	}
	if !strings.Contains(got, "nothing in this API reports whether it completed") {
		t.Errorf("Describe() = %q, want the completion disclosure here too", got)
	}

	if got := (Outcome{Action: "erase", DeviceID: "x", Accepted: false}).Describe(); !strings.Contains(got, "NOT sent") {
		t.Errorf("a non-accepted outcome = %q", got)
	}
}
