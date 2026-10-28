package apple_mdm

// The writes in the ADE/ABM/ADUE corner of the area: ADE enrollment settings,
// the ADUE service-discovery URL, and the per-token ABM re-sync.
//
// Probed live on 2026-10-28. Every route validates its body before resolving
// its id, so the body contract came out of requests against ids that belong to
// nothing. The semantics below came out of real writes, made only against the
// IOS settings of a token with no ADE devices and the discovery URL of a
// configuration that had it switched off, and each was restored and diffed
// identical to the snapshot taken before.
//
// THE PATCH MASK IS THE DANGEROUS PART of this file. A field named in the mask
// is written whether or not the body carries a value for it, and a missing
// value is not "leave alone" but "reset": zero-touch becomes false, the lists
// become empty, the welcome-screen strings revert to JumpCloud's stock text.
// So the mask and the body are never built separately here — ADESettingsChange
// produces both from one set of fields, and a field it was not given is in
// neither.
//
// Two writes in this corner are NOT here:
//
//   - ADE token create and renew upload an Apple Business Manager server token.
//     The body contract is known (see EncryptedServerTokenIsCamelCase) but the
//     encoding of the file is not, and the only way to learn it is a real token
//     from ABM. Held back rather than guessed.
//   - POST /microsoft-mdm/configuration-files returns a zip that enrolls
//     Windows devices in bulk — an enrollment credential — and jc's forced
//     JSON Accept header stands between it and the client, like the two plist
//     reads in PlistReadsAreUnreachable.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// --- ADE settings ---------------------------------------------------------

// The six patch-mask fields the service accepts. Snake case with dotted
// sub-paths, although the body beside them is camelCase — the mask names
// protobuf fields and the body is the JSON rendering of the same message.
// "welcome_screen" on its own is rejected, and so is every camelCase spelling.
const (
	MaskZeroTouch       = "enable_zero_touch_enrollment"
	MaskWelcomeTitle    = "welcome_screen.title"
	MaskWelcomeText     = "welcome_screen.paragraph"
	MaskWelcomeButton   = "welcome_screen.button"
	MaskSetupOptions    = "setup_options"
	MaskDefaultGroupIDs = "default_device_group_object_ids"
)

// MaskIsSnakeCaseBodyIsCamelCase records the casing split.
const MaskIsSnakeCaseBodyIsCamelCase = "the patch mask names fields in snake_case with dotted " +
	"sub-paths (welcome_screen.title) while the body is camelCase (welcomeScreen.title); " +
	"camelCase mask entries and the bare welcome_screen are both rejected"

// MaskedButAbsentIsReset records the behaviour that shapes ADESettingsChange.
const MaskedButAbsentIsReset = "a field in the patch mask with no value in the body is " +
	"RESET, not skipped: zero-touch to false, setup options and default groups to [], and " +
	"each welcome-screen string to JumpCloud's stock text"

// UnmaskedFieldsAreIgnored is the other half: a value in the body whose field
// is not in the mask is dropped without an error.
const UnmaskedFieldsAreIgnored = "a body value whose field is not in the patch mask is " +
	"dropped silently — the request succeeds and the field does not change"

// WelcomeTextCannotBeBlanked: an explicit "" is treated as absent, and absent
// means the stock text. There is no way to show an empty title.
const WelcomeTextCannotBeBlanked = "an empty welcome-screen string resets it to JumpCloud's " +
	"stock text (\"Welcome\", the stock paragraph, \"continue\"); it cannot be made blank"

// UnknownGroupIDsAreStored is why the callers look every group up first.
const UnknownGroupIDsAreStored = "a default-group id that belongs to nothing is accepted " +
	"and stored — enrollment would then place devices in a group that does not exist"

// SetupOptionUnknownIs500 is why UNKNOWN is absent from SetupOptions.
const SetupOptionUnknownIs500 = "setupOptions UNKNOWN passes the gateway's enum check and " +
	"then fails in the service with HTTP 500"

// WelcomeTextMax is the service's limit on each welcome-screen string. Not in
// the spec; the service answers "welcome_screen_title can't be longer than 255
// chars".
const WelcomeTextMax = 255

// SetupOptions are the setup-assistant options an ADE setting accepts: the
// spec's enum without UNKNOWN, which is a protobuf zero value and answers 500.
var SetupOptions = []string{
	"ACCESSIBILITY_SETUP", "ANALYTICS", "APPEARANCE_SETUP", "APPLE_ID_SIGN_IN",
	"APPLE_PAY_SETUP", "BACKUP_RESTORE", "DISPLAY_TONE", "FILEVAULT",
	"ICLOUD_ANALYTICS", "ICLOUD_DOCUMENTS_DESKTOP_SETUP", "LOCATION_SERVICES_SETUP",
	"PRIVACY_PANE", "SCREEN_TIME", "SIRI_SETUP", "TERMS_AND_CONDITIONS",
	"TOUCH_ID_SETUP", "APP_STORE", "DISPLAY_ZOOM", "DEVICE_TO_DEVICE_MIGRATION",
	"HOME_BUTTON", "IMESSAGE_AND_FACETIME", "MESSAGING_ACTIVATION_USING_PHONE_NUMBER",
	"MOVE_FROM_ANDROID", "PASSCODE", "RESTORE_COMPLETE", "SET_UP_CELLULAR",
	"SOFTWARE_UPDATE", "UPDATE_COMPLETE", "UNLOCK_WITH_WATCH", "WATCH_MIGRATION",
	"WELCOME", "ACTION_BUTTON", "ENABLE_LOCKDOWN_MODE", "INTELLIGENCE", "SAFETY",
	"TERMS_OF_ADDRESS", "WALLPAPER", "CAMERA_BUTTON", "ADDITIONAL_PRIVACY_SETTINGS",
	"MULTITASKING", "OS_SHOWCASE", "WEB_CONTENT_FILTERING", "SAFETY_AND_HANDLING",
	"LIQUID_GLASS",
}

// NormalizeSetupOption upper-cases an option and checks it against
// SetupOptions, so a typo fails here rather than at the gateway.
func NormalizeSetupOption(s string) (string, error) {
	v := strings.ToUpper(strings.TrimSpace(s))
	if v == "UNKNOWN" {
		return "", fmt.Errorf("UNKNOWN is not a usable setup option (the API answers 500)")
	}
	for _, o := range SetupOptions {
		if o == v {
			return v, nil
		}
	}
	return "", fmt.Errorf("%q is not a setup-assistant option; see `jc apple-mdm ade-settings update --help` for the list", s)
}

// WelcomeScreen is the welcome-screen part of an ADE settings body. Each field
// is omitted when empty: an empty string and an absent one mean the same thing
// to the service, and only the mask decides whether either is written.
type WelcomeScreen struct {
	Title     string `json:"title,omitempty"`
	Paragraph string `json:"paragraph,omitempty"`
	Button    string `json:"button,omitempty"`
}

// PatchMask lists the fields an ADE settings PATCH writes.
type PatchMask struct {
	Fields []string `json:"fields"`
}

// ADESettingsBody is the wire body. Build it with ADESettingsChange.Body, never
// by hand: the mask and the values must come from the same decision.
type ADESettingsBody struct {
	EnableZeroTouchEnrollment *bool          `json:"enableZeroTouchEnrollment,omitempty"`
	WelcomeScreen             *WelcomeScreen `json:"welcomeScreen,omitempty"`
	SetupOptions              []string       `json:"setupOptions,omitempty"`
	DefaultDeviceGroupIDs     []string       `json:"defaultDeviceGroupObjectIds,omitempty"`
	PatchMask                 PatchMask      `json:"patchMask"`
}

// ADESettingsChange is what an operator asked to change. A nil field is not
// part of the change. A non-nil empty slice clears the list. Reset* restores a
// welcome-screen string to the stock text — the only thing an empty string
// could mean to the service, so it is spelled out rather than inferred.
type ADESettingsChange struct {
	ZeroTouch       *bool
	WelcomeTitle    *string
	WelcomeText     *string
	WelcomeButton   *string
	ResetTitle      bool
	ResetText       bool
	ResetButton     bool
	SetupOptions    *[]string
	DefaultGroupIDs *[]string
}

// ErrNothingToChange is returned for a change that names no field. The service
// would answer 400 for an empty mask; refusing here says which flags exist.
var ErrNothingToChange = fmt.Errorf("nothing to change — name at least one setting")

// Body validates the change and builds the body and mask together.
func (c ADESettingsChange) Body() (ADESettingsBody, error) {
	var b ADESettingsBody
	var mask []string

	if c.ZeroTouch != nil {
		v := *c.ZeroTouch
		// Always sent, false included. A masked zero-touch with no value
		// resets to false — which happens to be what false means, but sending
		// it explicitly keeps the body honest about what was asked.
		b.EnableZeroTouchEnrollment = &v
		mask = append(mask, MaskZeroTouch)
	}

	ws := WelcomeScreen{}
	type welcomeField struct {
		name  string
		value *string
		reset bool
		mask  string
		dst   *string
	}
	for _, f := range []welcomeField{
		{"title", c.WelcomeTitle, c.ResetTitle, MaskWelcomeTitle, &ws.Title},
		{"paragraph", c.WelcomeText, c.ResetText, MaskWelcomeText, &ws.Paragraph},
		{"button", c.WelcomeButton, c.ResetButton, MaskWelcomeButton, &ws.Button},
	} {
		switch {
		case f.value != nil && f.reset:
			return ADESettingsBody{}, fmt.Errorf("the welcome %s is both set and reset — pick one", f.name)
		case f.reset:
			mask = append(mask, f.mask)
		case f.value != nil:
			v := *f.value
			if strings.TrimSpace(v) == "" {
				return ADESettingsBody{}, fmt.Errorf("the welcome %s cannot be blank: the API "+
					"treats an empty value as a reset to JumpCloud's stock text, so ask for the reset "+
					"explicitly if that is what you want", f.name)
			}
			if len(v) > WelcomeTextMax {
				return ADESettingsBody{}, fmt.Errorf("the welcome %s is %d characters; the API's limit is %d",
					f.name, len(v), WelcomeTextMax)
			}
			*f.dst = v
			mask = append(mask, f.mask)
		}
	}
	if ws != (WelcomeScreen{}) {
		b.WelcomeScreen = &ws
	}

	if c.SetupOptions != nil {
		opts := make([]string, 0, len(*c.SetupOptions))
		seen := map[string]bool{}
		for _, o := range *c.SetupOptions {
			v, err := NormalizeSetupOption(o)
			if err != nil {
				return ADESettingsBody{}, err
			}
			if !seen[v] {
				seen[v] = true
				opts = append(opts, v)
			}
		}
		b.SetupOptions = opts
		mask = append(mask, MaskSetupOptions)
	}

	if c.DefaultGroupIDs != nil {
		ids := make([]string, 0, len(*c.DefaultGroupIDs))
		for _, id := range *c.DefaultGroupIDs {
			if !IsObjectID(id) {
				return ADESettingsBody{}, ErrNotObjectID("a device group", id)
			}
			ids = append(ids, id)
		}
		b.DefaultDeviceGroupIDs = ids
		mask = append(mask, MaskDefaultGroupIDs)
	}

	if len(mask) == 0 {
		return ADESettingsBody{}, ErrNothingToChange
	}
	b.PatchMask = PatchMask{Fields: mask}
	return b, nil
}

// ADESettingValues is the readable part of an ADE setting, for the plan's
// before/after.
type ADESettingValues struct {
	EnableZeroTouchEnrollment bool          `json:"enableZeroTouchEnrollment"`
	WelcomeScreen             WelcomeScreen `json:"welcomeScreen"`
	SetupOptions              []string      `json:"setupOptions"`
	DefaultDeviceGroupIDs     []string      `json:"defaultDeviceGroupObjectIds"`
}

// ParseADESettingValues reads the values out of a single ADE setting
// (the object inside the adeSetting envelope).
func ParseADESettingValues(setting json.RawMessage) (ADESettingValues, error) {
	var env struct {
		ADE *ADESettingValues `json:"ade"`
	}
	if err := json.Unmarshal(setting, &env); err != nil {
		return ADESettingValues{}, fmt.Errorf("decoding the ADE setting: %w", err)
	}
	if env.ADE == nil {
		return ADESettingValues{}, fmt.Errorf("the ADE setting carried no \"ade\" field")
	}
	return *env.ADE, nil
}

// effectValueWidth keeps one effect line inside plan.RenderHuman's fixed box
// (46 columns for an effect). Values are shortened, never wrapped.
const effectValueWidth = 12

func shorten(s string) string {
	r := []rune(s)
	if len(r) <= effectValueWidth {
		return s
	}
	return string(r[:effectValueWidth-1]) + "…"
}

// Effects describes the change against the current values, one short line per
// masked field — so the plan says what each field goes FROM as well as to.
func (c ADESettingsChange) Effects(cur ADESettingValues) []string {
	var out []string
	if c.ZeroTouch != nil {
		out = append(out, fmt.Sprintf("zero-touch: %t → %t", cur.EnableZeroTouchEnrollment, *c.ZeroTouch))
	}
	welcome := func(name, curV string, v *string, reset bool) {
		switch {
		case reset:
			out = append(out, fmt.Sprintf("%s: %q → stock text", name, shorten(curV)))
		case v != nil:
			out = append(out, fmt.Sprintf("%s: %q → %q", name, shorten(curV), shorten(*v)))
		}
	}
	welcome("title", cur.WelcomeScreen.Title, c.WelcomeTitle, c.ResetTitle)
	welcome("paragraph", cur.WelcomeScreen.Paragraph, c.WelcomeText, c.ResetText)
	welcome("button", cur.WelcomeScreen.Button, c.WelcomeButton, c.ResetButton)
	if c.SetupOptions != nil {
		out = append(out, fmt.Sprintf("setup options: %d → %d", len(cur.SetupOptions), len(*c.SetupOptions)))
	}
	if c.DefaultGroupIDs != nil {
		out = append(out, fmt.Sprintf("default groups: %d → %d", len(cur.DefaultDeviceGroupIDs), len(*c.DefaultGroupIDs)))
	}
	return out
}

// DeviceGroupEndpoint is one device group, for the existence check the
// service does not do (UnknownGroupIDsAreStored).
func DeviceGroupEndpoint(id string) string { return "/systemgroups/" + id }

// --- ADUE service discovery URL -------------------------------------------

// ServiceDiscoveryURLEndpoint sets (POST) or removes (DELETE) an Apple MDM's
// ADUE service-discovery URL.
func ServiceDiscoveryURLEndpoint(managerID string) string {
	return "/applemdms/" + managerID + "/service-discovery-url"
}

// ServiceDiscoveryURLIsServerChosen records that the POST takes no URL. The
// spec declares an empty object, the service accepts {}, and it answers with
// the URL it chose — so "set" is really "enable".
const ServiceDiscoveryURLIsServerChosen = "the service-discovery POST takes an empty body " +
	"and answers with the URL JumpCloud chose; there is no URL to supply"

// ServiceDiscoveryWritesAreIdempotent: both POST and DELETE answer the same
// way when repeated, so neither needs a read first to be safe.
const ServiceDiscoveryWritesAreIdempotent = "enabling twice answers the same URL twice, " +
	"and disabling twice answers {} twice"

// ParseServiceDiscoveryURL reads the URL out of an enable response.
func ParseServiceDiscoveryURL(raw json.RawMessage) (string, error) {
	var env struct {
		URL string `json:"serviceDiscoveryUrl"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("decoding the service discovery response: %w", err)
	}
	if env.URL == "" {
		return "", fmt.Errorf("the service discovery response carried no serviceDiscoveryUrl")
	}
	return env.URL, nil
}

// --- ABM re-sync ----------------------------------------------------------

// RefreshABMDevicesEndpoint re-syncs the ABM device list behind one ADE token.
func RefreshABMDevicesEndpoint(tokenID string) string {
	return ADETokenEndpoint(tokenID) + "/refresh_abm_devices"
}

// RefreshABMWasNotFiredLive records the verification gap. The body contract
// ({} accepted, 404 for a token that does not exist) is live; what a real
// token answers is not. The spec documents a 412 "precondition failed",
// presumably for a token that cannot sync.
const RefreshABMWasNotFiredLive = "refresh_abm_devices was probed only against a token that " +
	"does not exist; a real token's answer, and when it returns 412, are unverified"

// --- ADE tokens (held back) ------------------------------------------------

// EncryptedServerTokenIsCamelCase records the trap that would greet whoever
// implements ADE token create/renew. The service's error message and the
// spec's sample request both say encrypted_server_token; only
// encryptedServerToken is read, and the snake_case key is ignored — so a
// request built from the error message fails with the same error forever.
const EncryptedServerTokenIsCamelCase = "the error says \"encrypted_server_token is required\" " +
	"and the spec's sample sends that key, but only encryptedServerToken is read; the value " +
	"must be base64"

// Unapplied compares what was asked for with the setting the PATCH returned,
// and names every field that did not take. This API drops input without an
// error in two ways already seen (UnmaskedFieldsAreIgnored, and unknown body
// keys), so a 200 alone is not taken as proof. Resets are not checked: the
// stock text is the service's to choose.
func (c ADESettingsChange) Unapplied(after ADESettingValues) []string {
	var miss []string
	if c.ZeroTouch != nil && after.EnableZeroTouchEnrollment != *c.ZeroTouch {
		miss = append(miss, "zero-touch")
	}
	str := func(name string, want *string, got string) {
		if want != nil && got != *want {
			miss = append(miss, "welcome "+name)
		}
	}
	str("title", c.WelcomeTitle, after.WelcomeScreen.Title)
	str("paragraph", c.WelcomeText, after.WelcomeScreen.Paragraph)
	str("button", c.WelcomeButton, after.WelcomeScreen.Button)
	if c.SetupOptions != nil {
		want, _ := ADESettingsChange{SetupOptions: c.SetupOptions}.Body()
		if !sameSet(want.SetupOptions, after.SetupOptions) {
			miss = append(miss, "setup options")
		}
	}
	if c.DefaultGroupIDs != nil && !sameSet(*c.DefaultGroupIDs, after.DefaultDeviceGroupIDs) {
		miss = append(miss, "default groups")
	}
	return miss
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, s := range a {
		seen[strings.ToLower(s)]++
	}
	for _, s := range b {
		k := strings.ToLower(s)
		if seen[k] == 0 {
			return false
		}
		seen[k]--
	}
	return true
}
