package agents

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The three credential writes. These are the calls that hand out an agent's
// way in, and the area's sharp edges are all here.
//
// Probed live on 2026-10-19 against throwaway agents.
//
// ONE CREDENTIAL PER AGENT. A second create answers "credential already
// exists". So there is no overlap-then-cut-over rotation: replacing a
// credential means deleting the only one and creating another, and between
// those two calls the agent cannot authenticate at all. That single fact
// shapes every decision below.
//
// THE SECRET IS RETURNED ONCE, by the create, and by nothing afterwards —
// there is not even a single-credential read (GET on one answers 404). So a
// surface that drops it has cost a credential, and one that prints it
// carelessly has leaked one. Both are easy; neither is recoverable.

// AgentCredentialEndpoint addresses one credential, by its client id.
//
// Note the id in the path is the clientId, not a separate object id, and
// there is no GET for it: this path accepts PATCH and DELETE only.
func AgentCredentialEndpoint(agentID, clientID string) string {
	return AgentCredentialsEndpoint(agentID) + "/" + clientID
}

// OneCredentialPerAgent is the constraint, quoted wherever a surface has to
// explain why rotation looks the way it does.
const OneCredentialPerAgent = "an agent can hold only one credential: creating a second is " +
	"refused, so replacing one means deleting it first — and the agent cannot authenticate " +
	"between the two calls"

// CredentialAlreadyExists recognises the refusal of a second create.
func CredentialAlreadyExists(err error) bool {
	return err != nil && strings.Contains(err.Error(), "credential already exists")
}

// --- Lifetimes ------------------------------------------------------------

// credentialLifetimes is the server-validated enum for lifetimeDays.
//
// Anything else is refused with "lifetimeDays must be one of: 0, 30, 60, 90,
// 365" — checked with 15, 400 and -1.
var credentialLifetimes = []int{0, 30, 60, 90, 365}

// DefaultCredentialLifetimeDays is what the server applies when lifetimeDays
// is omitted: a create with an empty body came back expiring in one year.
const DefaultCredentialLifetimeDays = 365

// LifetimeZeroNeverExpires records what 0 means, which is not "expire
// immediately" and is not documented.
//
// Verified by creating one: the credential comes back with NO expiresAt at
// all, and the listing shows it with none. It is a credential that never
// expires, which is a security decision rather than a convenience, so every
// surface says so before making one.
const LifetimeZeroNeverExpires = "a lifetime of 0 means the credential NEVER EXPIRES"

// CredentialLifetimes is for help text and validation messages.
func CredentialLifetimes() []int {
	out := append([]int(nil), credentialLifetimes...)
	sort.Ints(out)
	return out
}

// CredentialLifetimesList renders the enum for a message.
func CredentialLifetimesList() string {
	parts := make([]string, 0, len(credentialLifetimes))
	for _, v := range CredentialLifetimes() {
		parts = append(parts, strconv.Itoa(v))
	}
	return strings.Join(parts, ", ")
}

// BuildCredentialCreate validates a lifetime and returns the request body.
//
// A lifetime of -1 is the caller's way of saying "not specified", which
// sends no lifetimeDays and takes the server's default. 0 is a real value
// and means never expires, so it cannot double as the unset marker.
func BuildCredentialCreate(lifetimeDays int) (json.RawMessage, error) {
	if lifetimeDays < 0 {
		return json.Marshal(map[string]any{})
	}
	ok := false
	for _, v := range credentialLifetimes {
		if v == lifetimeDays {
			ok = true
			break
		}
	}
	if !ok {
		return nil, fmt.Errorf("a credential lifetime of %d days is not allowed: the API accepts "+
			"only %s, where 0 means it never expires", lifetimeDays, CredentialLifetimesList())
	}
	return json.Marshal(map[string]any{"lifetimeDays": lifetimeDays})
}

// --- Updating -------------------------------------------------------------

// tokenEndpointAuthMethods is the enum for the one field an update can
// change.
var tokenEndpointAuthMethods = []string{"client_secret_basic", "client_secret_post"}

// TokenEndpointAuthMethods is for help text and MCP schemas.
func TokenEndpointAuthMethods() []string {
	return append([]string(nil), tokenEndpointAuthMethods...)
}

// CredentialPatchChangesOnlyTheAuthMethod records what an update can and
// cannot do, which is much less than its shape suggests.
//
// PATCH requires tokenEndpointAuthMethod — a body without it is refused,
// even an empty one — and that is the ONLY field it honours. status and
// lifetimeDays are accepted in the body and SILENTLY IGNORED: sending
// {"tokenEndpointAuthMethod":"client_secret_post","status":"inactive"}
// returns 200 with status still "active", and sending lifetimeDays leaves
// expiresAt untouched. Verified by reading the credential back after each.
//
// So there is no way to disable or re-date a credential through an update.
// The only way to stop one working is to delete it.
const CredentialPatchChangesOnlyTheAuthMethod = "a credential update can change only its " +
	"token endpoint auth method; status and lifetime are accepted in the request and ignored, " +
	"so the only way to stop a credential working is to delete it"

// BuildCredentialPatch validates an update and returns the body.
//
// It refuses the fields the server ignores rather than passing them on. A
// write that reports success for a change it discarded is the defect this
// area shares with Password Vault, and the honest response is to say the
// operation does not exist rather than to appear to perform it.
func BuildCredentialPatch(authMethod, status string, lifetimeDays int) (json.RawMessage, error) {
	if status != "" {
		return nil, fmt.Errorf("a credential's status cannot be changed: the API accepts status "+
			"in the request and ignores it, returning success while leaving the credential "+
			"active. Delete the credential instead — %s", OneCredentialPerAgent)
	}
	if lifetimeDays >= 0 {
		return nil, fmt.Errorf("a credential's lifetime cannot be changed after it is created: " +
			"the API accepts lifetimeDays in an update and ignores it. Delete the credential and " +
			"create a new one with the lifetime you want")
	}
	authMethod = strings.TrimSpace(authMethod)
	if authMethod == "" {
		return nil, fmt.Errorf("an update needs --auth-method: it is the only field this API "+
			"will change, and it accepts %s", strings.Join(tokenEndpointAuthMethods, " or "))
	}
	found := false
	for _, m := range tokenEndpointAuthMethods {
		if m == authMethod {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("invalid --auth-method %q: expected %s",
			authMethod, strings.Join(tokenEndpointAuthMethods, " or "))
	}
	return json.Marshal(map[string]any{"tokenEndpointAuthMethod": authMethod})
}

// --- The created credential ----------------------------------------------

// NewCredential is what a create returns.
//
// THE SECRET FIELD IS UNEXPORTED ON PURPOSE. encoding/json cannot reach an
// unexported field, so marshalling this struct — or handing it to any generic
// printer, logger or output formatter — cannot emit the secret. The only way
// to obtain it is Secret(), which a reader can grep for.
//
// This is not theoretical tidiness. While probing this very endpoint, a
// generic "print the response" helper in the probe script dumped a live
// clientSecret into a terminal transcript. The response shape invites it:
// the secret sits in an ordinary JSON field next to four harmless ones. A
// type that cannot be serialised wrongly is worth more here than a rule
// about remembering not to.
type NewCredential struct {
	ClientID                string `json:"clientId"`
	ExpiresAt               string `json:"expiresAt,omitempty"`
	TokenEndpointAuthMethod string `json:"tokenEndpointAuthMethod,omitempty"`
	AuthType                string `json:"authType,omitempty"`

	secret string
}

// Secret returns the client secret. It exists exactly once per credential
// and is not retrievable afterwards from any endpoint.
func (c NewCredential) Secret() string { return c.secret }

// HasSecret reports whether a secret was present in the response.
func (c NewCredential) HasSecret() bool { return c.secret != "" }

// NeverExpires reports whether the credential was created with no expiry,
// which a lifetime of 0 produces.
func (c NewCredential) NeverExpires() bool { return c.ExpiresAt == "" }

// String deliberately does not include the secret, so a %v or %s on this
// value cannot leak it either.
func (c NewCredential) String() string {
	expiry := c.ExpiresAt
	if expiry == "" {
		expiry = "never"
	}
	return fmt.Sprintf("credential %s (expires: %s)", c.ClientID, expiry)
}

// ParseNewCredential reads the create response.
//
// A response with no secret is an ERROR rather than a credential with an
// empty one. The create is the only call that returns a secret, so a missing
// one means either the shape changed or the credential is unusable — and
// reporting success would leave an operator believing they hold something
// they do not.
func ParseNewCredential(raw json.RawMessage) (NewCredential, error) {
	var wire struct {
		ClientID                string `json:"clientId"`
		ClientSecret            string `json:"clientSecret"`
		ExpiresAt               string `json:"expiresAt"`
		TokenEndpointAuthMethod string `json:"tokenEndpointAuthMethod"`
		AuthType                string `json:"authType"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return NewCredential{}, fmt.Errorf("decoding the new credential: %w — the credential may "+
			"have been created, so check with `jc agents credentials list` before trying again; "+
			"%s", err, OneCredentialPerAgent)
	}
	if wire.ClientID == "" {
		return NewCredential{}, fmt.Errorf("the API returned a credential with no client id, so " +
			"jc cannot tell you what was created — check with `jc agents credentials list`")
	}
	if wire.ClientSecret == "" {
		return NewCredential{}, fmt.Errorf("the API returned credential %s with no secret. The "+
			"creating call is the only one that ever returns it, so this credential cannot be "+
			"used: delete it and create another", wire.ClientID)
	}
	return NewCredential{
		ClientID:                wire.ClientID,
		ExpiresAt:               wire.ExpiresAt,
		TokenEndpointAuthMethod: wire.TokenEndpointAuthMethod,
		AuthType:                wire.AuthType,
		secret:                  wire.ClientSecret,
	}, nil
}

// CredentialNotFound recognises a delete or update aimed at a credential
// that is not there. A delete is NOT idempotent: the second one answers
// "not found".
func CredentialNotFound(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "not found")
}

// DeleteIsTheOnlyRevocation records why delete carries the weight it does in
// this area.
const DeleteIsTheOnlyRevocation = "deleting a credential is the only way to stop it working, " +
	"and it is the agent's only credential — the agent cannot authenticate until a new one is " +
	"created"

// --- Keeping secrets out of generic output -------------------------------

// secretBearingKeys are the field names that must never reach a generic
// output formatter, a table, a log or a clipboard by accident.
var secretBearingKeys = []string{
	"clientSecret", "client_secret", "secret", "password", "privateKey", "private_key",
}

// SanitizeCredential strips any secret-bearing field from a credential
// record before it is handed to generic output.
//
// Today no read returns a secret — verified against every credential on a
// live tenant, and the update response was checked field by field. This
// exists because that is an assumption about a server, and the whole design
// of this area rests on it: `credentials list` and `credentials update` both
// print the record through jc's ordinary output path, so if the API ever
// started including a secret there, every output format would carry it and
// no test of ours would notice.
//
// Enforcing the invariant costs one pass over a small object. Assuming it
// costs a disclosure.
//
// A record that will not decode is returned untouched rather than dropped —
// the caller still has something to show — but it cannot have been a JSON
// object, so there is no field to strip.
func SanitizeCredential(raw json.RawMessage) json.RawMessage {
	var rec map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rec); err != nil {
		return raw
	}
	stripped := false
	for _, k := range secretBearingKeys {
		if _, ok := rec[k]; ok {
			delete(rec, k)
			stripped = true
		}
	}
	if !stripped {
		return raw
	}
	out, err := json.Marshal(rec)
	if err != nil {
		// Marshalling a map that just came from Unmarshal should not fail,
		// but returning the original here would hand back the secret this
		// function exists to remove. An empty object is useless; it is not
		// harmful.
		return json.RawMessage(`{}`)
	}
	return out
}

// SanitizeCredentials applies SanitizeCredential to a listing.
func SanitizeCredentials(rows []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, SanitizeCredential(r))
	}
	return out
}
