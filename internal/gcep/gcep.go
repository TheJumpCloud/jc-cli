// Package gcep is the shared contract for JumpCloud's GCEP API area
// (/api/v2/gcep/*), a Google device-trust integration, used by the CLI and the
// MCP tools so the two cannot drift. The spec does not expand the acronym.
//
// The shapes here come from the published spec and were NOT probed live
// before this package was written (JCCLI-9). In particular the spec documents
// no 404 for GET /gcep/configuration, so what an organization without the
// integration gets back is unknown. NotConfigured and IsNotConfiguredError
// therefore accept every plausible "nothing there" answer: a 404, an empty
// body, and a zero-valued configuration (gRPC-gateway style APIs, which the
// configuration_state_* enum names suggest, return one). Replace this with
// the recorded answer once someone has probed the test org.
//
// The PUT "creates or updates" the configuration and was not verified to
// merge, so callers must send the complete body (Merge) and read it back
// (Diff): that is correct whether the PUT replaces or merges.
package gcep

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/klaassen-consulting/jc/internal/api"
)

// ConfigurationEndpoint is the organization's GCEP configuration singleton,
// relative to the v2 base URL.
const ConfigurationEndpoint = "/gcep/configuration"

// DeviceStateEndpoint addresses one device's GCEP trust state.
func DeviceStateEndpoint(systemID string) string {
	return "/gcep/systems/" + systemID + "/device-state"
}

// NotConfiguredMessage is what both surfaces say for an organization without
// a GCEP configuration.
const NotConfiguredMessage = "GCEP is not configured for this organization."

// Configuration is the GET /gcep/configuration response.
type Configuration struct {
	OrganizationObjectID string   `json:"organizationObjectId,omitempty"`
	CustomerID           string   `json:"customerId"`
	GoogleUserEmail      string   `json:"googleUserEmail"`
	State                string   `json:"state"`
	ConnectedState       string   `json:"connectedState,omitempty"`
	ApplicableOsFamilies []string `json:"applicableOsFamilies"`
	ConfiguredByEmail    string   `json:"configuredByEmail,omitempty"`
	ConfiguredByName     string   `json:"configuredByName,omitempty"`
	ConfiguredByUserID   string   `json:"configuredByUserId,omitempty"`
	RegisteredAt         string   `json:"registeredAt,omitempty"`
}

// UpdateBody is the PUT /gcep/configuration request: the four writable fields.
type UpdateBody struct {
	CustomerID           string   `json:"customerId"`
	GoogleUserEmail      string   `json:"googleUserEmail"`
	State                string   `json:"state"`
	ApplicableOsFamilies []string `json:"applicableOsFamilies"`
}

// Body returns the complete PUT body for c.
func (c Configuration) Body() UpdateBody {
	os := c.ApplicableOsFamilies
	if os == nil {
		os = []string{}
	}
	return UpdateBody{
		CustomerID:           c.CustomerID,
		GoogleUserEmail:      c.GoogleUserEmail,
		State:                c.State,
		ApplicableOsFamilies: os,
	}
}

// ParseConfiguration decodes a GET /gcep/configuration response. It returns
// (nil, nil) when the response is a not-configured answer (see NotConfigured).
func ParseConfiguration(raw json.RawMessage) (*Configuration, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var c Configuration
	if err := json.Unmarshal(trimmed, &c); err != nil {
		return nil, fmt.Errorf("decoding GCEP configuration: %w", err)
	}
	if c.CustomerID == "" && c.GoogleUserEmail == "" &&
		(c.State == "" || c.State == StateUnspecified) && len(c.ApplicableOsFamilies) == 0 {
		return nil, nil
	}
	return &c, nil
}

// NotConfigured reports whether a successful GET response means the
// organization has no GCEP configuration.
func NotConfigured(raw json.RawMessage) bool {
	c, err := ParseConfiguration(raw)
	return err == nil && c == nil
}

// IsNotConfiguredError reports whether a GET /gcep/configuration error means
// the organization has no GCEP configuration: a 404, or a 400 whose message
// says the configuration was not found.
func IsNotConfiguredError(err error) bool {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.StatusCode == 404 {
		return true
	}
	if apiErr.StatusCode != 400 {
		return false
	}
	msg := strings.ToLower(apiErr.Message)
	for _, s := range []string{"not found", "not configured", "does not exist", "no configuration"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// Configuration states.
const (
	StateEnabled       = "configuration_state_enabled"
	StateDisabled      = "configuration_state_disabled"
	StatePendingDelete = "configuration_state_pending_delete"
	StateUnspecified   = "configuration_state_unspecified"
)

// StateValues are the values --state / state accept, in help order.
var StateValues = []string{"enabled", "disabled"}

// OSFamilyValues are the values --os / applicableOsFamilies accept.
var OSFamilyValues = []string{"mac_os", "windows"}

// NormalizeState maps enabled|disabled (or the full enum name) to the API
// enum. The error names the allowed values and never echoes an enum built
// from the input.
func NormalizeState(v string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case "enabled", StateEnabled:
		return StateEnabled, nil
	case "disabled", StateDisabled:
		return StateDisabled, nil
	}
	return "", fmt.Errorf("invalid --state %q — allowed values: %s", v, strings.Join(StateValues, ", "))
}

// NormalizeOSFamilies validates a list of OS families and returns them
// deduplicated in input order. Every invalid value is reported at once.
func NormalizeOSFamilies(values []string) ([]string, error) {
	var out, bad []string
	seen := map[string]bool{}
	for _, v := range values {
		s := strings.ToLower(strings.TrimSpace(v))
		switch s {
		case "mac_os", "windows":
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		default:
			bad = append(bad, fmt.Sprintf("%q", v))
		}
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("invalid --os value %s — allowed values: %s (comma-separated)",
			strings.Join(bad, ", "), strings.Join(OSFamilyValues, ", "))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--os needs at least one value — allowed values: %s (comma-separated)",
			strings.Join(OSFamilyValues, ", "))
	}
	return out, nil
}

// Changes are the fields a `config set` asks to change; nil means unchanged.
type Changes struct {
	CustomerID           *string
	GoogleUserEmail      *string
	State                *string
	ApplicableOsFamilies []string
}

// Empty reports whether no field was given.
func (ch Changes) Empty() bool {
	return ch.CustomerID == nil && ch.GoogleUserEmail == nil && ch.State == nil && ch.ApplicableOsFamilies == nil
}

// Merge returns the complete PUT body: current (nil when not configured) with
// only the given fields replaced.
func Merge(current *Configuration, ch Changes) UpdateBody {
	var body UpdateBody
	if current != nil {
		body = current.Body()
	} else {
		body.ApplicableOsFamilies = []string{}
	}
	if ch.CustomerID != nil {
		body.CustomerID = *ch.CustomerID
	}
	if ch.GoogleUserEmail != nil {
		body.GoogleUserEmail = *ch.GoogleUserEmail
	}
	if ch.State != nil {
		body.State = *ch.State
	}
	if ch.ApplicableOsFamilies != nil {
		body.ApplicableOsFamilies = append([]string{}, ch.ApplicableOsFamilies...)
	}
	return body
}

// MissingForCreate names the flags a create (no current configuration) still
// needs. Creating sends all four fields, so all four must be given.
func MissingForCreate(ch Changes) []string {
	var missing []string
	if ch.CustomerID == nil || *ch.CustomerID == "" {
		missing = append(missing, "--customer-id")
	}
	if ch.GoogleUserEmail == nil || *ch.GoogleUserEmail == "" {
		missing = append(missing, "--google-user-email")
	}
	if ch.State == nil {
		missing = append(missing, "--state")
	}
	if ch.ApplicableOsFamilies == nil {
		missing = append(missing, "--os")
	}
	return missing
}

// FieldChange is one writable field before and after a `config set`.
type FieldChange struct {
	Field   string
	Before  string
	After   string
	Changed bool
}

// Describe renders the change as one plan line.
func (f FieldChange) Describe() string {
	if !f.Changed {
		return fmt.Sprintf("%s: %s (unchanged)", f.Field, f.Before)
	}
	return fmt.Sprintf("%s: %s → %s", f.Field, f.Before, f.After)
}

func osKey(os []string) string {
	s := append([]string{}, os...)
	sort.Strings(s)
	return strings.Join(s, ",")
}

func display(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func displayOS(os []string) string {
	if len(os) == 0 {
		return "(none)"
	}
	return strings.Join(os, ",")
}

// Compare lists the four writable fields of before (nil when not configured)
// against after. OS families compare as a set.
func Compare(before *Configuration, after UpdateBody) []FieldChange {
	var b UpdateBody
	if before != nil {
		b = before.Body()
	}
	return []FieldChange{
		{"customerId", display(b.CustomerID), display(after.CustomerID), b.CustomerID != after.CustomerID},
		{"googleUserEmail", display(b.GoogleUserEmail), display(after.GoogleUserEmail), b.GoogleUserEmail != after.GoogleUserEmail},
		{"state", display(b.State), display(after.State), b.State != after.State},
		{"applicableOsFamilies", displayOS(b.ApplicableOsFamilies), displayOS(after.ApplicableOsFamilies),
			osKey(b.ApplicableOsFamilies) != osKey(after.ApplicableOsFamilies)},
	}
}

// AnyChanged reports whether any field in changes differs.
func AnyChanged(changes []FieldChange) bool {
	for _, c := range changes {
		if c.Changed {
			return true
		}
	}
	return false
}

// Mismatches compares what was sent with what was read back after a PUT and
// returns one line per field that differs; empty means they match.
func Mismatches(sent UpdateBody, got *Configuration) []string {
	if got == nil {
		return []string{"the configuration read back is empty (" + NotConfiguredMessage + ")"}
	}
	var out []string
	for _, c := range Compare(got, sent) {
		if c.Changed {
			out = append(out, fmt.Sprintf("%s: sent %s, read back %s", c.Field, c.After, c.Before))
		}
	}
	return out
}

var objectIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// IsObjectID reports whether s is a 24-hex object id.
func IsObjectID(s string) bool { return objectIDPattern.MatchString(s) }

var hexOnly = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// LooksLikeMalformedID reports whether s was evidently meant as an object id
// but is not one: a run of 16 or more hex characters that is not 24 long.
// Shorter or non-hex strings are treated as device names.
func LooksLikeMalformedID(s string) bool {
	return len(s) >= 16 && len(s) != 24 && hexOnly.MatchString(s)
}

// ErrNotObjectID explains a malformed device id before it reaches the API.
func ErrNotObjectID(s string) error {
	return fmt.Errorf("%q is not a valid device id — expected a 24-character hex object id or a device hostname", s)
}

// DeviceState is the GET /gcep/systems/{id}/device-state response.
type DeviceState struct {
	RegistrationStatus string            `json:"registrationStatus"`
	ManagementStatus   string            `json:"managementStatus"`
	DeviceUsers        []json.RawMessage `json:"deviceUsers"`
}

// DeviceUserFields are the default fields of a device-state table row.
var DeviceUserFields = []string{"deviceUserId", "googleEmail", "localDeviceUsername", "lastSuccessfulReportedTime"}

// ParseDeviceState decodes a device-state response.
func ParseDeviceState(raw json.RawMessage) (*DeviceState, error) {
	var d DeviceState
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("decoding GCEP device state: %w", err)
	}
	return &d, nil
}
