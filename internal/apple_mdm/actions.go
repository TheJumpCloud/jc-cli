package apple_mdm

// The Apple MDM device actions: the fourteen operations that tell a managed
// Mac to do something, rather than asking it what it is.
//
// Everything here was probed live against org 5ec71e8e96bfda0611fc6c5b on
// 2026-10-23, across eight enrolled devices. THREE of the fourteen were
// actually fired -- osUpdateStatus, refreshActivationLockInformation and
// refreshdepdevices, none of which changes anything a user would notice. The
// other eleven were never aimed at a real device, and the reason is
// QueuedCommandsRunAtNextCheckIn below: there is no such thing as a safe
// target for an erase, not even an offline one.
//
// What made the gate possible anyway is ValidationRunsBeforeTheDeviceLookup:
// every one of these routes validates the request body BEFORE it resolves the
// device id, so a body can be probed to exhaustion against an id that belongs
// to no device and the action can never happen. That is how the lost-mode
// contract below was discovered without putting a single Mac into lost mode.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Endpoints. {apple_mdm_id} and {deviceManagerObjectId} are the same object
// under two spellings, exactly as in devices.go -- the lock/erase family uses
// the first, lockwithgeneratedpin and lostmode the second, and one id answers
// both.
func DeviceActionEndpoint(mdmID, deviceID, action string) string {
	return DeviceEndpoint(mdmID, deviceID) + "/" + action
}

// RefreshDEPDevicesEndpoint is the only action in this file that is not
// per-device: it re-syncs the whole configuration's DEP device list.
func RefreshDEPDevicesEndpoint(mdmID string) string {
	return "/applemdms/" + mdmID + "/refreshdepdevices"
}

// The action path segments, spelled as the API spells them -- camelCase in
// the lock/erase family, all-lowercase in the two {deviceManagerObjectId}
// routes. Not normalized, because the API does not normalize them.
const (
	ActionErase                   = "erase"
	ActionLock                    = "lock"
	ActionRestart                 = "restart"
	ActionShutdown                = "shutdown"
	ActionClearPasscode           = "clearPasscode"
	ActionClearRestrictionsPasswd = "clearRestrictionsPassword"
	ActionClearActivationLock     = "clearActivationLock"
	ActionRefreshActivationLock   = "refreshActivationLockInformation"
	ActionOSUpdateStatus          = "osUpdateStatus"
	ActionScheduleOSUpdate        = "scheduleOSUpdate"
	ActionLockWithGeneratedPIN    = "lockwithgeneratedpin"
	ActionLostMode                = "lostmode"
)

// ValidationRunsBeforeTheDeviceLookup is the finding that made this slice
// safe to probe at all.
//
// All fourteen routes answer in the same order, observed without exception:
// the API gateway validates the body against the OpenAPI schema, then the
// service applies its own rules, then the device id is resolved, and only
// then does anything reach the device. A malformed body answers 400 for an id
// that belongs to no device; a well-formed one answers 404. So the body
// contract can be mapped completely against 000000000000000000000000 with no
// possibility of acting on a real machine.
//
// The two validation layers are distinguishable and worth telling apart when
// reading an error: the gateway says "Request validation failed: Parameter
// (body) failed schema validation: …", the service says
// {"message":"…","status":"INVALID_ARGUMENT"}.
const ValidationRunsBeforeTheDeviceLookup = "these routes validate the body before they " +
	"resolve the device id: a bad body is 400 even for a device that does not exist, and a " +
	"good body is 404. The body contract is therefore probeable against a nonexistent device " +
	"without any risk of performing the action"

// QueuedCommandsRunAtNextCheckIn is why eleven of the fourteen were never
// fired at a real device, and why "it is offline, so this is safe" is wrong.
//
// An MDM command is queued by Apple and delivered when the device next checks
// in. A 204 therefore means the command was accepted for delivery, not that
// anything has happened yet -- and an erase sent to a machine that is powered
// off is not a no-op, it is an erase that happens later.
const QueuedCommandsRunAtNextCheckIn = "an MDM command is queued and delivered at the " +
	"device's next check-in, so an offline device is not a safe target -- the command runs " +
	"when it comes back"

// AcceptedIsNotCompleted: every action in this family answers HTTP 204 with
// no body (confirmed on the wire for osUpdateStatus: 204, zero bytes). There
// is no handle in the response and nothing in this API surface reports what
// became of the command -- /applemdms/{id}/background_tasks stayed empty
// after a successful queue, with both the configuration id and the device id
// tried in that slot. So no surface may say "the device was restarted"; the
// true statement is that the restart was accepted.
const AcceptedIsNotCompleted = "a 204 means the command was accepted for delivery, not " +
	"performed: background_tasks stays empty and nothing in this API reports completion, so " +
	"jc says 'accepted' and never 'done'"

// NilBodyIsSentAsNull is a trap in jc's own client, not in the API.
//
// The no-body actions declare no request body, but they do not accept the
// absence of one. V2Client.Create marshals its argument unconditionally, so a
// nil body goes out as the four bytes `null` and the gateway answers 400
// `SyntaxError: Unexpected token 'n', "null" is not valid JSON` -- before the
// device is looked up, so the action never happens, but the command fails.
// EmptyActionBody is the smallest body they accept.
const NilBodyIsSentAsNull = "a nil request body is marshalled to `null`, which these routes " +
	"reject with a 400 SyntaxError; the no-body actions must be sent an empty JSON object"

// EmptyActionBody is what the no-body actions are sent. See
// NilBodyIsSentAsNull for why this is not simply nil.
func EmptyActionBody() any { return struct{}{} }

// UnknownFieldsAreSwallowed: these routes accept fields nobody declared.
// {"enableLostMode":{"bogusField":"zzz","message":"m"}} validates. That cuts
// both ways and the second way is the important one: because an unknown field
// is accepted in silence, the fact that a field is ACCEPTED is not evidence
// that it is honoured. enableLostMode.footnote passes validation; whether a
// footnote ever reaches the lock screen is unverified, and a typo in a flag
// name will not be reported by the API either.
const UnknownFieldsAreSwallowed = "unknown body fields are accepted silently, so acceptance " +
	"of a field is not evidence that it does anything -- and a misspelled field is not reported"

// ---------------------------------------------------------------- the PIN

// pinPattern is the gateway's own rule, ^[0-9]{6}$, applied here so the
// failure is a sentence instead of a schema dump. The spec declares it and
// the live gateway enforces it; this is a copy, not an invention.
var pinPattern = regexp.MustCompile(`^[0-9]{6}$`)

// ValidatePIN checks a lock/erase PIN against the gateway's pattern.
//
// The spec says the PIN is "required for MacOS" while declaring it optional
// in the schema, and which of those is true is UNVERIFIED: finding out means
// sending an erase without a PIN to a real Mac. Nothing here assumes either
// way -- an empty PIN is passed through to the API, which is the only thing
// that knows.
func ValidatePIN(pin string) error {
	if pin == "" {
		return nil
	}
	if !pinPattern.MatchString(pin) {
		return fmt.Errorf("a lock/erase PIN must be exactly six digits, got %q", pin)
	}
	return nil
}

// PINBody is the body shape for lock and erase.
type PINBody struct {
	PIN string `json:"pin,omitempty"`
}

// ------------------------------------------------------------------ restart

// RestartBody carries the optional kernel-cache rebuild paths. kextPaths is
// type-checked by the gateway (a string where an array belongs answers 400),
// so the field is typed here rather than passed through.
type RestartBody struct {
	KextPaths []string `json:"kextPaths,omitempty"`
}

// ---------------------------------------------------------------- lost mode

// LostModeOneofIsNotAField is the finding that costs the most time to anyone
// reading the error literally.
//
// An empty body answers 400 {"message":"command is required",
// "status":"INVALID_ARGUMENT"}. There is no field called `command`: the
// message names a protobuf oneof on the service's own request type, and every
// shape of {"command":…} fails with exactly the same error. The real body is
// the one the spec documents -- {"enableLostMode":{…}} or
// {"disableLostMode":{}} -- and "command is required" means "set one of
// those".
const LostModeOneofIsNotAField = "the lostmode 400 'command is required' names a protobuf " +
	"oneof, not a body field: there is no `command` key. Send {\"enableLostMode\":{…}} or " +
	"{\"disableLostMode\":{}}"

// LostModeEnableNeedsAWayToBeReached: enabling lost mode with an empty object
// answers 400 "message or phone number must be provided to enable Lost Mode"
// -- a service-level rule the spec does not state at all, since it declares
// enableLostMode as an object with no properties.
const LostModeEnableNeedsAWayToBeReached = "enableLostMode requires at least one of message " +
	"or phoneNumber; the spec declares the object as having no properties and says nothing " +
	"about this"

// LostModeBothMembersAreAccepted is the reason this package refuses to build
// such a body.
//
// {"enableLostMode":{},"disableLostMode":{}} is ACCEPTED -- 404 for a
// nonexistent device, meaning validation passed -- in both key orders, and in
// both orders the enable-side "message or phone number" rule does not fire.
// The enable is therefore being dropped. Which command actually reaches the
// device is UNVERIFIED, because confirming it means putting a real Mac into
// lost mode; what is certain is that the request is accepted, that half of it
// is silently discarded, and that a client assembling the body from a map
// would pick a winner by Go's randomized map iteration order.
//
// So LostModeBody can only be built by EnableLostMode or DisableLostMode, and
// Validate rejects both-set and neither-set before anything is sent.
const LostModeBothMembersAreAccepted = "setting both enableLostMode and disableLostMode is " +
	"accepted and silently drops the enable, in either key order; which command reaches the " +
	"device is unverified, so jc refuses to send both"

// LostModeEnable is the enable side of the oneof.
type LostModeEnable struct {
	Message     string `json:"message,omitempty"`
	PhoneNumber string `json:"phoneNumber,omitempty"`
	// Footnote validates, but see UnknownFieldsAreSwallowed: so does
	// `bogusField`. Whether a footnote is displayed is unverified.
	Footnote string `json:"footnote,omitempty"`
}

// LostModeDisable is the disable side. It carries nothing; the key's presence
// IS the command.
type LostModeDisable struct{}

// LostModeBody is the oneof, expressed so that exactly one member can be set.
// Build it with EnableLostMode or DisableLostMode -- the fields are exported
// only because they have to be for encoding/json, and Validate is the gate.
type LostModeBody struct {
	Enable  *LostModeEnable  `json:"enableLostMode,omitempty"`
	Disable *LostModeDisable `json:"disableLostMode,omitempty"`
}

// EnableLostMode builds an enable body, applying the undeclared service rule
// locally so the operator gets a sentence rather than an INVALID_ARGUMENT.
func EnableLostMode(message, phoneNumber, footnote string) (LostModeBody, error) {
	if strings.TrimSpace(message) == "" && strings.TrimSpace(phoneNumber) == "" {
		return LostModeBody{}, fmt.Errorf("enabling lost mode needs a message or a phone number " +
			"to show on the locked screen — pass at least one of them")
	}
	return LostModeBody{Enable: &LostModeEnable{
		Message:     message,
		PhoneNumber: phoneNumber,
		Footnote:    footnote,
	}}, nil
}

// DisableLostMode builds a disable body.
func DisableLostMode() LostModeBody {
	return LostModeBody{Disable: &LostModeDisable{}}
}

// Validate enforces the oneof. See LostModeBothMembersAreAccepted: the API
// will not do this for us -- it accepts both and discards one.
func (b LostModeBody) Validate() error {
	switch {
	case b.Enable != nil && b.Disable != nil:
		return fmt.Errorf("lost mode takes either enable or disable, never both: the API accepts " +
			"a body with both and silently drops the enable")
	case b.Enable == nil && b.Disable == nil:
		return fmt.Errorf("lost mode needs either enable or disable")
	}
	return nil
}

// Enabling reports which side of the oneof is set. Call after Validate.
func (b LostModeBody) Enabling() bool { return b.Enable != nil }

// ------------------------------------------------------------- OS updates

// InstallAction values, as the gateway's enum declares them. An unlisted
// value answers 400 "no enum match for: …".
const (
	InstallActionDownloadOnly        = "DOWNLOAD_ONLY"
	InstallActionInstallLater        = "INSTALL_LATER"
	InstallActionInstallASAP         = "INSTALL_ASAP"
	InstallActionInstallForceRestart = "INSTALL_FORCE_RESTART"
)

// InstallActions returns the enum in the spec's order, for flag help and for
// the MCP tool description.
func InstallActions() []string {
	return []string{
		InstallActionDownloadOnly,
		InstallActionInstallLater,
		InstallActionInstallASAP,
		InstallActionInstallForceRestart,
	}
}

// ProductKeyHasNoSourceInThisAPI: scheduleOSUpdate requires product_key, the
// gateway does not validate it (the string "x" is accepted), and no endpoint
// in the Apple MDM surface lists the valid keys for a device -- there is no
// available-updates read anywhere in the three MDM tags. So the required
// field has no discoverable source here, and a wrong key is accepted by the
// API and then silently does nothing on the device.
const ProductKeyHasNoSourceInThisAPI = "scheduleOSUpdate requires a product_key that nothing " +
	"in this API will tell you: there is no available-updates read, and any string is accepted"

// OSUpdateBody is the scheduleOSUpdate body. install_action and product_key
// are required by the gateway; max_user_deferrals is a pointer so that
// omitting it is distinguishable from asking for zero deferrals.
type OSUpdateBody struct {
	InstallAction    string `json:"install_action"`
	ProductKey       string `json:"product_key"`
	MaxUserDeferrals *int   `json:"max_user_deferrals,omitempty"`
}

// NewOSUpdateBody validates the enum locally so a typo costs nothing, and
// requires the product key because the gateway does.
func NewOSUpdateBody(installAction, productKey string, maxUserDeferrals *int) (OSUpdateBody, error) {
	var valid bool
	for _, a := range InstallActions() {
		if installAction == a {
			valid = true
			break
		}
	}
	if !valid {
		return OSUpdateBody{}, fmt.Errorf("install action %q is not one of %s",
			installAction, strings.Join(InstallActions(), ", "))
	}
	if strings.TrimSpace(productKey) == "" {
		return OSUpdateBody{}, fmt.Errorf("scheduleOSUpdate requires a product key — %s",
			ProductKeyHasNoSourceInThisAPI)
	}
	return OSUpdateBody{
		InstallAction:    installAction,
		ProductKey:       productKey,
		MaxUserDeferrals: maxUserDeferrals,
	}, nil
}

// ------------------------------------------------------------ preconditions

// Preconditions are the fields of the device record that say whether an
// action can work at all. Read before a destructive action, they turn "the
// API said 409" into something an operator could have known in advance.
//
// On the probed tenant, six of eight devices reported activation lock
// manageable with bypass codes present, and the two oldest (macOS 12.7.6 and
// 14.8.4) reported neither -- so clearActivationLock is not uniformly
// available even within one configuration.
type Preconditions struct {
	SerialNumber string `json:"serialNumber,omitempty"`
	OSFamily     string `json:"mdmOsFamily,omitempty"`
	OSVersion    string `json:"osVersion,omitempty"`

	// LostMode is kept raw. Every probed device reported the bare boolean
	// false; the shape when lost mode is ACTIVE has never been observed, and
	// decoding into a bool would turn an unseen object into a parse failure
	// on exactly the device an operator most needs to look at.
	LostMode json.RawMessage `json:"lostMode,omitempty"`

	HasUnlockToken               bool `json:"hasUnlockToken,omitempty"`
	HasActivationLockBypassCodes bool `json:"hasActivationLockBypassCodes,omitempty"`

	SecurityInfo struct {
		IsActivationLockManageable bool `json:"isActivationLockManageable,omitempty"`
		PasscodeCompliant          bool `json:"passcodeCompliant,omitempty"`
		FilevaultEnabled           bool `json:"filevaultEnabled,omitempty"`
	} `json:"securityInfo,omitempty"`
}

// ParsePreconditions reads the precondition fields out of a device record.
func ParsePreconditions(raw json.RawMessage) (Preconditions, error) {
	var p Preconditions
	if err := json.Unmarshal(raw, &p); err != nil {
		return Preconditions{}, fmt.Errorf("decoding device record: %w", err)
	}
	return p, nil
}

// LostModeActive reports whether lost mode is on, and whether that is known.
// known is false when lostMode arrives in a shape this code has never seen,
// which is every shape except the boolean -- see Preconditions.LostMode.
func (p Preconditions) LostModeActive() (active, known bool) {
	switch strings.TrimSpace(string(p.LostMode)) {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

// PreconditionNote returns what the device record says about this action
// before it is attempted, or "" when there is nothing useful to say.
//
// It is a warning, never a veto: every one of these fields has been seen to
// disagree with reality somewhere in this API (see
// RecoveryLockThreeStatusFields), so the device gets asked rather than
// second-guessed.
func (p Preconditions) PreconditionNote(action string) string {
	// Kept under 40 characters: these render inside plan.RenderHuman's
	// fixed-width box, which pads to 46 and does not wrap, so a longer note
	// breaks the border it is printed in.
	switch action {
	case ActionClearActivationLock:
		if !p.SecurityInfo.IsActivationLockManageable {
			return "isActivationLockManageable is false"
		}
		if !p.HasActivationLockBypassCodes {
			return "no activation lock bypass codes"
		}
	case ActionLostMode:
		if active, known := p.LostModeActive(); known && active {
			return "lostMode is already true"
		} else if !known {
			shape := strings.TrimSpace(string(p.LostMode))
			if shape == "" {
				shape = "absent"
			}
			if len(shape) > 12 {
				shape = shape[:12] + "…"
			}
			return "lostMode shape unrecognized: " + shape
		}
	case ActionErase, ActionLock:
		if strings.EqualFold(p.OSFamily, "macOS") {
			return "a six-digit PIN is required on macOS"
		}
	}
	return ""
}

// ------------------------------------------------------------------ outcome

// Outcome is what every action surface reports. It says accepted, never done
// -- see AcceptedIsNotCompleted.
type Outcome struct {
	Action   string `json:"action"`
	DeviceID string `json:"device_id,omitempty"`
	MDMID    string `json:"apple_mdm_id"`
	Accepted bool   `json:"accepted"`
	Detail   string `json:"detail,omitempty"`
}

// Describe renders the outcome for a human, in the words the finding allows.
//
// The two branches are not cosmetic. QueuedCommandsRunAtNextCheckIn is a fact
// about commands sent TO A DEVICE; refreshdepdevices is a server-side re-sync
// with Apple and has no device and no check-in. One sentence covering both
// would be wrong about one of them.
func (o Outcome) Describe() string {
	if o.DeviceID == "" {
		if !o.Accepted {
			return fmt.Sprintf("%s was NOT sent for Apple MDM configuration %s", o.Action, o.MDMID)
		}
		return fmt.Sprintf("%s accepted for Apple MDM configuration %s — nothing in this API "+
			"reports whether it completed", o.Action, o.MDMID)
	}
	if !o.Accepted {
		return fmt.Sprintf("%s was NOT sent to device %s", o.Action, o.DeviceID)
	}
	return fmt.Sprintf("%s accepted for device %s — queued for delivery at the device's next "+
		"check-in; nothing in this API reports whether it completed", o.Action, o.DeviceID)
}
