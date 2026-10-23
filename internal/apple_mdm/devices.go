package apple_mdm

// The Apple MDM device reads: device detail, the push-certificate CSR, the
// recovery-lock pair and the device-lock pair.
//
// Everything here was probed live against org 5ec71e8e96bfda0611fc6c5b on
// 2026-10-22, across seven enrolled devices chosen to cover three different
// recovery-lock states. The spec describes none of what follows accurately,
// and two of the findings change what the commands are allowed to say.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// Endpoints. The {deviceManagerObjectId} in the spec is the Apple MDM
// configuration's id -- the same value `jc apple-mdm list` returns -- and
// {apple_mdm_id} on the neighbouring routes is the same thing under a second
// name. Both spellings are preserved in the spec; only one object exists.
func DeviceEndpoint(mdmID, deviceID string) string {
	return "/applemdms/" + mdmID + "/devices/" + deviceID
}

func CSREndpoint(mdmID string) string {
	return "/applemdms/" + mdmID + "/csr"
}

func RecoveryLockConfigEndpoint(mdmID, deviceID string) string {
	return DeviceEndpoint(mdmID, deviceID) + "/getrecoverylockconfig"
}

func RecoveryLockPasswordEndpoint(mdmID, deviceID string) string {
	return DeviceEndpoint(mdmID, deviceID) + "/getrecoverylockpassword"
}

func LockStateEndpoint(mdmID, deviceID string) string {
	return DeviceEndpoint(mdmID, deviceID) + "/lockstate"
}

func LockPINEndpoint(mdmID, deviceID string) string {
	return DeviceEndpoint(mdmID, deviceID) + "/lockpin"
}

// AbsenceAndTypoBothAnswer404 is the finding that shapes every command in
// this file.
//
// getrecoverylockconfig, getrecoverylockpassword and lockpin all answer HTTP
// 404 for a device that simply has nothing set. A device id that does not
// exist answers HTTP 404 too, with a byte-identical body -- verified by asking
// for 000000000000000000000000, which is a well-formed object id belonging to
// no device. So the status alone cannot distinguish "this device has no
// recovery lock" from "you mistyped the id", and a command that guesses will
// confidently tell an operator a managed device has no recovery lock when in
// fact they are looking at nothing at all.
//
// The resolution is to read the device first and interpret the 404 only once
// the device is known to exist. That costs one extra request on a path that is
// already interactive, and it is the difference between a true answer and a
// plausible one.
const AbsenceAndTypoBothAnswer404 = "a 404 from the recovery-lock and lock-pin reads means " +
	"either 'nothing is set on this device' or 'no such device' -- the two are " +
	"indistinguishable on the wire, so the device is read first and the 404 is only " +
	"reported as absence once the device is known to exist"

// OSFamilySpelledTwoWays: the device record reports mdmOsFamily "macOS", while
// the ADE settings route for the same tenant takes and returns "DARWIN" for
// the same platform (see OSFamilies in ade.go). Neither accepts the other's
// spelling. Nothing in jc should translate between them silently -- a device
// listing says macOS because that is what the API said.
const OSFamilySpelledTwoWays = "the device record says mdmOsFamily 'macOS'; the ADE settings " +
	"route calls the same platform 'DARWIN'. Both are reported as received"

// ParseDevice decodes a single device record.
//
// Unlike every list in this area, the device detail read carries NO envelope:
// the body is the device object itself. decodeSingle would therefore be wrong
// here, and so would reaching for a {device:...} key that does not exist.
func ParseDevice(raw json.RawMessage) (json.RawMessage, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("decoding the device: %w", err)
	}
	if len(probe) == 0 {
		// Not "a device with no fields" -- an empty object here means the
		// response was not what this endpoint returns, and reporting it as a
		// blank device would read as a successful answer.
		return nil, fmt.Errorf("the device read returned an empty object; expected a device record")
	}
	if _, ok := probe["id"]; !ok {
		return nil, fmt.Errorf("the device read returned an object with no id field; " +
			"expected a device record")
	}
	return raw, nil
}

// CSRIsNotJSON: /applemdms/{id}/csr is the one read in this area whose body is
// not JSON. It answers HTTP 200 with a bare base64 string -- no envelope, no
// quotes, no object -- which decodes to an Apple plist carrying
// PushCertCertificateChain, PushCertRequestCSR and PushCertSignature.
//
// This is also why it works at all while depkey and the single
// enrollment-profile read answer 406 (see PlistReadsAreUnreachable): those two
// serve a plist directly and are refused by the forced Accept: application/json,
// whereas this one wraps the plist in base64 and so survives the same header.
const CSRIsNotJSON = "the csr read answers HTTP 200 with a bare base64 string rather than " +
	"JSON; it decodes to the Apple plist holding the push-certificate request"

// ParseCSR turns the csr response into the plist bytes it encodes.
//
// The body arrives as raw base64 rather than JSON, so this takes the bytes as
// read off the wire. Whitespace is tolerated because the transfer may wrap it;
// anything else is an error rather than a partial decode, since half a
// certificate request is worse than none.
func ParseCSR(body []byte) ([]byte, error) {
	trimmed := strings.Join(strings.Fields(string(body)), "")
	if trimmed == "" {
		return nil, fmt.Errorf("the csr read returned an empty body")
	}
	// A JSON error envelope would also be "a body" -- catch it before trying
	// to read it as base64, so the message names the real problem.
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return nil, fmt.Errorf("the csr read returned JSON, not the expected base64 plist: %s",
			truncate(trimmed, 120))
	}
	plist, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("decoding the csr: the body was not valid base64: %w", err)
	}
	if !strings.Contains(string(plist), "<plist") {
		return nil, fmt.Errorf("the csr decoded from base64 but is not a plist")
	}
	return plist, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// RecoveryLockThreeStatusFields is the trap in the recovery-lock config.
//
// The record carries THREE fields that each claim to describe the same thing,
// and on live devices they disagree. One device in the probe set reported
// isLockEnabled true, status SET_PENDING and uiStatus OFF simultaneously --
// and still returned a recovery-lock password when asked. So none of the three
// can be used on its own to decide whether a device has a usable password, and
// in particular `uiStatus: OFF` does not mean there is nothing to read.
//
// Worse, the device LISTING is wrong about it too: securityInfo.
// recoveryLockEnabled was absent on that device, and a reader who treats
// absence as false would skip a device that has a password.
const RecoveryLockThreeStatusFields = "isLockEnabled, status and uiStatus disagree on live " +
	"devices (true / SET_PENDING / OFF on the same device, which still had a password). " +
	"None of them, nor securityInfo.recoveryLockEnabled in the device listing, predicts " +
	"whether a password can be read -- only asking does"

// NextRotationAtMoves records an observation deliberately left unresolved.
//
// nextRotationAt on two devices was seen to change between reads, from
// passwordCreatedAt + rotationWindowDays to roughly an hour ahead, and the two
// devices it moved on were the two whose password had been read. That suggests
// reading the password schedules the rotation -- which would make a "read"
// command something quite different.
//
// It is NOT established. Reading the password a second time did not move the
// date again, which a per-read reschedule should have done, and devices whose
// password was never read carry rotation dates long past due, so a background
// job recalculating overdue rotations fits the evidence equally well. Settling
// it would mean rotating a recovery-lock password on somebody's real machine
// to watch what happens, with n=1 and no way to rule out a coincident job.
//
// So: treat nextRotationAt as advisory, never as a promise, and do not build
// anything that assumes a password read is free of server-side effects.
const NextRotationAtMoves = "nextRotationAt has been observed to change between reads; it is " +
	"advisory. Whether reading the password schedules a rotation is unproven -- see the " +
	"comment on this constant before relying on either answer"

// RecoveryLockConfig is the recovery-lock state for one device.
//
// Pointers on the timestamps because null is meaningful and distinct from
// absent: lastRotationAt is explicitly null on a device whose password has
// never rotated, and flattening that to the zero time would read as 1 January
// year 1 in output.
type RecoveryLockConfig struct {
	IsLockEnabled           bool    `json:"isLockEnabled"`
	IsPasswordAutoGenerated bool    `json:"isPasswordAutoGenerated"`
	Status                  string  `json:"status"`
	UIStatus                string  `json:"uiStatus"`
	RotationWindowDays      int     `json:"rotationWindowDays"`
	InitialSetAt            *string `json:"initialSetAt"`
	PasswordCreatedAt       *string `json:"passwordCreatedAt"`
	LastRotationAt          *string `json:"lastRotationAt"`
	NextRotationAt          *string `json:"nextRotationAt"`
	StatusReportedAt        *string `json:"statusReportedAt"`
}

// Describe summarises the config without pretending the three status fields
// agree. When they disagree it says so, because a one-word answer here would
// be a guess dressed as a fact.
func (c RecoveryLockConfig) Describe() string {
	if c.Status == "SET" && c.UIStatus == "ON" {
		return "recovery lock is set"
	}
	if c.Status == "" && c.UIStatus == "" {
		return "recovery lock state not reported"
	}
	return fmt.Sprintf("recovery lock is reported inconsistently "+
		"(status %s, console shows %s, enabled=%t) — a password may still be readable",
		orNone(c.Status), orNone(c.UIStatus), c.IsLockEnabled)
}

func orNone(s string) string {
	if s == "" {
		return "(unreported)"
	}
	return s
}

func ParseRecoveryLockConfig(raw json.RawMessage) (RecoveryLockConfig, error) {
	var c RecoveryLockConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return RecoveryLockConfig{}, fmt.Errorf("decoding the recovery lock config: %w", err)
	}
	return c, nil
}

// ParseRecoveryLockPassword pulls the password out of {"password": "..."}.
//
// The value is returned rather than printed, logged or wrapped in anything.
// An empty or missing key is an error: a command that printed nothing here
// would look exactly like a device with no password, which is the one thing
// this read must never be ambiguous about.
func ParseRecoveryLockPassword(raw json.RawMessage) (string, error) {
	var body struct {
		Password *string `json:"password"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", fmt.Errorf("decoding the recovery lock password: %w", err)
	}
	if body.Password == nil {
		return "", fmt.Errorf("the recovery lock password read returned no password field")
	}
	if *body.Password == "" {
		return "", fmt.Errorf("the recovery lock password read returned an empty password")
	}
	return *body.Password, nil
}

// LockPINShapeUnverified: every device on the probe tenant answered 404 for
// lockpin, including devices with an active recovery lock. The PIN is set by
// lockwithgeneratedpin, which is a write and out of scope here, so the
// SUCCESS shape of this read has never been seen.
//
// That is why ParseLockPIN does not assert a key. Guessing {"pin": …} or
// {"lockPin": …} from the endpoint's name and then reporting "no PIN set" when
// the guess missed would turn a decode failure into a false negative about
// device security -- the exact failure this area's rule against treating
// absent data as evidence exists to prevent.
const LockPINShapeUnverified = "no device on any probed tenant has a lock PIN, so only the " +
	"404 path of this read is verified. The success body is passed through as received " +
	"rather than decoded against a guessed key"

// ParseLockPIN validates that the body is a non-empty JSON object and returns
// it unchanged. See LockPINShapeUnverified for why it does no more than that.
func ParseLockPIN(raw json.RawMessage) (json.RawMessage, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("decoding the lock pin: %w", err)
	}
	if len(probe) == 0 {
		return nil, fmt.Errorf("the lock pin read returned an empty object")
	}
	return raw, nil
}

// LockState is the device-lock state. Every string field here carries
// protobuf-style sentinels rather than empty values -- "..._unspecified"
// means "nothing to report", not a state called unspecified.
type LockState struct {
	State         string  `json:"state"`
	FailureReason string  `json:"failureReason"`
	MessageResult string  `json:"messageResult"`
	LockMessage   string  `json:"lockMessage"`
	PhoneNumber   string  `json:"phoneNumber"`
	StatusUpdated *string `json:"statusUpdatedAt"`
}

// Sentinel suffixes and prefixes observed live:
//
//	state:         device_lock_console_state_unlocked
//	failureReason: lock_failure_reason_unspecified
//	messageResult: message_result_unspecified
const lockStatePrefix = "device_lock_console_state_"

// IsUnspecified reports whether a protobuf sentinel means "nothing to report".
func IsUnspecified(s string) bool { return strings.HasSuffix(s, "_unspecified") }

// Locked reports whether the device is locked.
//
// Matched against the known locked state rather than "not unlocked": a new
// sentinel, or an empty string from a device that has never reported, would
// otherwise read as locked and send somebody looking for a lock that does not
// exist.
func (s LockState) Locked() bool {
	return strings.TrimPrefix(s.State, lockStatePrefix) == "locked"
}

// Describe renders the state in words, folding the sentinels away.
func (s LockState) Describe() string {
	state := strings.TrimPrefix(s.State, lockStatePrefix)
	if state == "" || IsUnspecified(s.State) {
		return "the device has not reported a lock state"
	}
	out := "device lock state: " + strings.ReplaceAll(state, "_", " ")
	if s.FailureReason != "" && !IsUnspecified(s.FailureReason) {
		out += fmt.Sprintf(" (last failure: %s)", strings.ReplaceAll(s.FailureReason, "_", " "))
	}
	return out
}

func ParseLockState(raw json.RawMessage) (LockState, error) {
	var s LockState
	if err := json.Unmarshal(raw, &s); err != nil {
		return LockState{}, fmt.Errorf("decoding the lock state: %w", err)
	}
	return s, nil
}
