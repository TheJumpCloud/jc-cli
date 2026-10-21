package apple_mdm

// Automated Device Enrollment (ADE), Apple Business Manager (ABM) and
// Account-Driven User Enrollment (ADUE): the wire contract, shared by the CLI
// and the MCP server so the two cannot drift.
//
// Every shape here was read off the live API, not the spec. The spec is right
// about the paths and wrong or silent about almost everything else that
// matters: it does not say that the list and single reads use different
// envelope keys, that one of its three osFamily values is rejected, that an
// absent service-discovery URL arrives as HTTP 200, or that the ADUE config
// is JSON encoded inside JSON.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Endpoints that take no identifier.
const (
	ADETokensEndpoint        = "/applemdms/ade_tokens"
	ADETokensStatsEndpoint   = "/applemdms/ade_tokens_stats"
	ADETokenOrgNamesEndpoint = "/applemdms/ade_token_org_names"

	// ABMDevicesEndpoint is the org-wide ABM device list. There is also a
	// per-device-manager list at ManagerABMDevicesEndpoint; on a tenant with
	// one Apple MDM the two answered identically, so neither is a projection
	// of the other as far as anyone can tell from one tenant.
	ABMDevicesEndpoint = "/applemdms/abm_devices"
)

// ADETokenEndpoint is one ADE token by its objectId.
func ADETokenEndpoint(tokenID string) string {
	return ADETokensEndpoint + "/" + tokenID
}

// ADESettingsEndpoint lists every OS family's enrollment settings for a token.
func ADESettingsEndpoint(tokenID string) string {
	return ADETokenEndpoint(tokenID) + "/ade_settings"
}

// ADESettingEndpoint is one OS family's settings for a token.
func ADESettingEndpoint(tokenID, osFamily string) string {
	return ADESettingsEndpoint(tokenID) + "/" + osFamily
}

// ManagerABMDevicesEndpoint is the ABM device list scoped to one Apple MDM.
func ManagerABMDevicesEndpoint(managerID string) string {
	return "/applemdms/" + managerID + "/abm_devices"
}

// BackgroundTasksEndpoint lists the MDM background tasks queued for a device.
// The id is a DEVICE id, not an Apple MDM id, despite sitting at the same
// position in the path as one — the spec calls it deviceObjectId while the
// sibling routes call the same position deviceManagerObjectId.
func BackgroundTasksEndpoint(deviceID string) string {
	return "/applemdms/" + deviceID + "/background_tasks"
}

// ADUEEndpoint returns the Account-Driven User Enrollment redirect for an org.
func ADUEEndpoint(orgID string) string {
	return "/applemdms/" + orgID + "/account-driven-service-discovery"
}

// ADUEConfigEndpoint returns the ADUE server configuration for an org.
func ADUEConfigEndpoint(orgID string) string {
	return ADUEEndpoint(orgID) + "/config"
}

// ValidateServiceDiscoveryURLEndpoint checks whether an Apple MDM has a
// usable service-discovery URL.
func ValidateServiceDiscoveryURLEndpoint(managerID string) string {
	return "/applemdms/" + managerID + "/validate-service-discovery-url"
}

// OSFamilies are the osFamily values an ADE settings read accepts.
//
// The spec's enum is UNKNOWN, DARWIN and IOS. UNKNOWN is a protobuf zero
// value that leaked into the published enum: asking for it returns HTTP 400,
// so offering all three to an operator would hand them a guaranteed error for
// one of the three. Only these two are real.
var OSFamilies = []string{"DARWIN", "IOS"}

// OSFamilyUnknownIsRejected records why UNKNOWN is absent from OSFamilies.
const OSFamilyUnknownIsRejected = "the osFamily enum includes UNKNOWN, but asking for it " +
	"returns HTTP 400 — it is a protobuf zero value that leaked into the published enum"

// osFamilyAliases maps what an operator will type onto what the API calls it.
// "DARWIN" is the kernel, not the product: nobody administering Macs thinks of
// the OS family as Darwin, and the obvious guess — macos — is not accepted.
var osFamilyAliases = map[string]string{
	"darwin": "DARWIN",
	"macos":  "DARWIN",
	"mac":    "DARWIN",
	"osx":    "DARWIN",
	"ios":    "IOS",
	"ipados": "IOS",
}

// NormalizeOSFamily maps an operator's spelling onto the API's, or explains
// what the accepted values are.
func NormalizeOSFamily(s string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(s))
	if key == "" {
		return "", fmt.Errorf("which OS family? one of %s", strings.Join(OSFamilies, ", "))
	}
	if key == "unknown" {
		return "", fmt.Errorf("UNKNOWN is not a usable OS family (the API answers 400); use %s",
			strings.Join(OSFamilies, " or "))
	}
	if v, ok := osFamilyAliases[key]; ok {
		return v, nil
	}
	return "", fmt.Errorf("%q is not an OS family; use %s", s, strings.Join(OSFamilies, " or "))
}

// decodeEnvelope pulls a named array out of a response, treating a missing or
// undecodable key as an error rather than as an empty result. An ABM device
// list that failed to decode must never read as "this org has no ABM devices".
func decodeEnvelope(raw json.RawMessage, key, what string) ([]json.RawMessage, int, error) {
	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, 0, fmt.Errorf("decoding the %s response: %w", what, err)
	}
	body, ok := env[key]
	if !ok {
		return nil, 0, fmt.Errorf("the %s response carried no %q field", what, key)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, 0, fmt.Errorf("decoding %s: %w", what, err)
	}
	total := len(rows)
	if tc, ok := env["totalCount"]; ok {
		var n int
		if err := json.Unmarshal(tc, &n); err == nil {
			total = n
		}
	}
	return rows, total, nil
}

// decodeSingle pulls a named object out of a response.
func decodeSingle(raw json.RawMessage, key, what string) (json.RawMessage, error) {
	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decoding the %s response: %w", what, err)
	}
	body, ok := env[key]
	if !ok {
		return nil, fmt.Errorf("the %s response carried no %q field", what, key)
	}
	return body, nil
}

// SingularAndPluralDisagree records the envelope rule for this area: a list
// read answers under the plural key and a single read under the singular one,
// so neither decoder can serve both.
const SingularAndPluralDisagree = "a list read answers {adeTokens:[…]} and a single read " +
	"{adeToken:{…}} — same for adeSettings/adeSetting, so the two cannot share a decoder"

// ParseADETokens reads the ADE token list.
func ParseADETokens(raw json.RawMessage) ([]json.RawMessage, int, error) {
	return decodeEnvelope(raw, "adeTokens", "ADE token list")
}

// ParseADEToken reads a single ADE token.
func ParseADEToken(raw json.RawMessage) (json.RawMessage, error) {
	return decodeSingle(raw, "adeToken", "ADE token")
}

// ParseADESettings reads every OS family's settings for one token. The
// envelope also repeats organizationObjectId, adeTokenObjectId and
// deviceManagerObjectId alongside the rows; the rows carry them too, so
// nothing here depends on the outer copies.
func ParseADESettings(raw json.RawMessage) ([]json.RawMessage, int, error) {
	return decodeEnvelope(raw, "adeSettings", "ADE settings list")
}

// ParseADESetting reads one OS family's settings.
func ParseADESetting(raw json.RawMessage) (json.RawMessage, error) {
	return decodeSingle(raw, "adeSetting", "ADE settings")
}

// ParseABMDevices reads an ABM device list, at either scope.
func ParseABMDevices(raw json.RawMessage) ([]json.RawMessage, int, error) {
	return decodeEnvelope(raw, "abmDevices", "ABM device list")
}

// ParseBackgroundTasks reads a device's MDM background tasks.
func ParseBackgroundTasks(raw json.RawMessage) ([]json.RawMessage, int, error) {
	return decodeEnvelope(raw, "backgroundTasks", "background task list")
}

// ParseOrgNames reads the ABM organization names behind an org's ADE tokens.
func ParseOrgNames(raw json.RawMessage) ([]string, int, error) {
	rows, total, err := decodeEnvelope(raw, "orgNames", "ADE token org name list")
	if err != nil {
		return nil, 0, err
	}
	names := make([]string, 0, len(rows))
	for i, r := range rows {
		var s string
		if err := json.Unmarshal(r, &s); err != nil {
			return nil, 0, fmt.Errorf("decoding org name %d: %w", i, err)
		}
		names = append(names, s)
	}
	return names, total, nil
}

// ADETokenStats is the ADE token health summary.
type ADETokenStats struct {
	ExpiredCount  int `json:"expiredCount"`
	ExpiringCount int `json:"expiringCount"`
	TotalCount    int `json:"totalCount"`
	ValidCount    int `json:"validCount"`
}

// TotalIsNotHealth records the trap in this summary. On a tenant with one
// token the API answered totalCount 1, expiringCount 1 and validCount 0 — so
// a token can be counted, be expiring, and not be valid, all at once.
// Reporting "1 ADE token" without the breakdown says nothing about whether
// enrollment currently works.
const TotalIsNotHealth = "totalCount counts tokens, not working ones: a tenant with one " +
	"token read totalCount 1, expiringCount 1, validCount 0"

// NeedsAttention is true when any token is expired or expiring, or when no
// token is valid despite tokens existing.
func (s ADETokenStats) NeedsAttention() bool {
	if s.ExpiredCount > 0 || s.ExpiringCount > 0 {
		return true
	}
	return s.TotalCount > 0 && s.ValidCount == 0
}

// ParseADETokenStats reads the ADE token stats summary. It is a bare object
// with no envelope, unlike every list in this area.
func ParseADETokenStats(raw json.RawMessage) (ADETokenStats, error) {
	var s ADETokenStats
	if err := json.Unmarshal(raw, &s); err != nil {
		return ADETokenStats{}, fmt.Errorf("decoding the ADE token stats response: %w", err)
	}
	return s, nil
}

// ParseADUERedirect reads the ADUE redirect URL for an org.
func ParseADUERedirect(raw json.RawMessage) (string, error) {
	var env struct {
		RedirectURL string `json:"redirectUrl"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("decoding the ADUE response: %w", err)
	}
	if env.RedirectURL == "" {
		return "", fmt.Errorf("the ADUE response carried no redirectUrl")
	}
	return env.RedirectURL, nil
}

// ADUEConfigIsDoubleEncoded records the shape of the ADUE config response.
const ADUEConfigIsDoubleEncoded = "the ADUE config arrives as {jsonConfig:\"<a JSON document " +
	"as a string>\"} — the payload has to be unmarshalled twice, and emitting the outer " +
	"object hands the caller an escaped blob instead of a document"

// ParseADUEConfig reads the ADUE server configuration, unwrapping the inner
// JSON document. The caller gets the configuration itself, not a string
// containing it.
func ParseADUEConfig(raw json.RawMessage) (json.RawMessage, error) {
	var env struct {
		JSONConfig string `json:"jsonConfig"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decoding the ADUE config response: %w", err)
	}
	if strings.TrimSpace(env.JSONConfig) == "" {
		return nil, fmt.Errorf("the ADUE config response carried no jsonConfig")
	}
	inner := json.RawMessage(env.JSONConfig)
	if !json.Valid(inner) {
		return nil, fmt.Errorf("the ADUE config's jsonConfig field is not valid JSON")
	}
	return inner, nil
}

// ServiceDiscoveryResult is the answer from a service-discovery URL check.
type ServiceDiscoveryResult struct {
	ResponseCode string `json:"responseCode"`
}

// AbsenceArrivesAsHTTP200 records why this check needs its own type. The
// endpoint reports a missing URL as HTTP 200 with responseCode NOT_FOUND, so
// an implementation that trusts the status code reports success for a tenant
// that has no service-discovery URL at all.
const AbsenceArrivesAsHTTP200 = "a missing service-discovery URL is reported as HTTP 200 " +
	"with {responseCode:\"NOT_FOUND\"} — the status code says nothing"

// responseCodeOK is the one value that means the URL resolves. Matched
// exactly: the other observed value is NOT_FOUND, and treating "anything that
// is not NOT_FOUND" as success would call an unrecognised code a pass.
const responseCodeOK = "OK"

// OK reports whether the service-discovery URL resolves.
func (r ServiceDiscoveryResult) OK() bool { return r.ResponseCode == responseCodeOK }

// Describe renders the result for human output.
func (r ServiceDiscoveryResult) Describe() string {
	switch {
	case r.OK():
		return "the service discovery URL resolves"
	case r.ResponseCode == "NOT_FOUND":
		return "no service discovery URL is configured"
	default:
		return "the service discovery URL check answered " + r.ResponseCode
	}
}

// ParseServiceDiscovery reads a service-discovery URL check.
func ParseServiceDiscovery(raw json.RawMessage) (ServiceDiscoveryResult, error) {
	var r ServiceDiscoveryResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return ServiceDiscoveryResult{}, fmt.Errorf("decoding the service discovery response: %w", err)
	}
	if r.ResponseCode == "" {
		return ServiceDiscoveryResult{}, fmt.Errorf("the service discovery response carried no responseCode")
	}
	return r, nil
}

// PlistReadsAreUnreachable records the two reads this package deliberately
// does not expose.
//
// GET /applemdms/{id}/depkey and GET /applemdms/{id}/enrollmentprofiles/{id}
// both answer HTTP 406 Not Acceptable: they serve plists, and every jc
// request carries Accept: application/json, set by the auth transport. That
// header cannot be overridden from outside — extra headers are wrapped
// OUTSIDE the auth transports on purpose, so that nothing can override an
// auth-critical header — which means reaching these two needs a deliberate
// client capability rather than a flag. Weakening a security property for two
// reads is the wrong trade, so they stay unreachable and recorded.
//
// GET /applemdms/{id}/csr is the near miss: same plist content, but it answers
// 200 with a base64 body despite the JSON Accept.
const PlistReadsAreUnreachable = "depkey and the single enrollment-profile read answer 406: " +
	"they serve plists and jc always sends Accept: application/json"
