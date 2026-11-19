// Package privaccess reads JumpCloud Privileged Access (PAM): the privileged
// servers, databases and websites people connect to through a connector, the
// privileged credentials linked to them, the blocking rules that stop risky
// commands or queries, and the connectors and connections themselves. The CLI
// and the MCP tools share it so the two cannot drift.
//
// Reads are in this file; the writes, and the lockout that shapes every one
// of them, are in writes.go.
//
// Same backend as Password Vault (internal/passwordvault): the users are the
// same, and so are the defects. Everything below was established by probing
// the test org on 2026-11-18. The spec is wrong about this area in the ways
// listed here.
//
//   - Activation: GET /privileged-access/status → {isActive, isExpired, isPam,
//     isPwm, ...}. PAM is usable when isActive && isPam && !isExpired.
//   - THREE pagination models:
//     servers, databases, connectors, connections and blocking-rules page with
//     maxResultCount/skipCount and answer {items, totalCount}. The spec marks
//     maxResultCount optional; servers and databases reject a call without it
//     ("required, 1–100, fetch-all is not supported").
//     websites, credentials, resource-managers and users take limit/skip and
//     answer {results, totalCount}. Like Password Vault, the users listing
//     adds the calling user to the first page whether or not they match,
//     and sort is ignored. These are fetched unpaginated and the server's
//     count is reported.
//     A credential's history and linked servers page with a continuation
//     token.
//   - THE PLAIN DETAIL READ IS BROKEN. GET /{servers|databases|websites|
//     credentials}/{id} answers 400 "you need permissions View Detail" to a
//     caller holding Manage, View Detail and Connect on a record it has just
//     created. The edit views answer and return a superset, so those are
//     what this package reads: /{id}/edit for servers, databases and
//     websites, /{id}/edit-shell for credentials.
//   - ABSENCE HAS FIVE SHAPES. An unknown database, website, credential or
//     blocking rule gives 404. An unknown server's edit view gives 500, which
//     the API client retries. A credential's /linked-servers gives 500 and its
//     /managers 400. The connection status of a resource that does not exist
//     is 200 "Available". So every by-id read here confirms the object
//     exists, from a listing or from a read whose 404 is reliable, before it
//     asks anything else.
//   - A blocking rule's edit view needs ?criteriaType= in the SHORT form
//     (DATABASE). The long form the write side and the listing use
//     (BLOCKING_RULE_CRITERIA_TYPE_DATABASE) is rejected.
//
// SECRETS. Never read here: a credential's secret, MFA code and rotation
// script; the backup key; connector install files (they carry an enrollment
// token); session recordings, keylogs and the pre-signed video and download
// URLs. Among the reads that are made, a database's connection string can
// embed a password, and a credential listing carries card and identity-
// document fields. Redact handles the first; StripPersonalFields the second.
package privaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Endpoints, relative to the v2 API base.
const (
	Endpoint                  = "/privileged-access"
	StatusEndpoint            = Endpoint + "/status"
	DashboardEndpoint         = Endpoint + "/dashboard-overview"
	UsersEndpoint             = Endpoint + "/users"
	TagsEndpoint              = Endpoint + "/tags"
	GroupsAllEndpoint         = Endpoint + "/groups/all"
	ServersEndpoint           = Endpoint + "/servers"
	DatabasesEndpoint         = Endpoint + "/databases"
	WebsitesEndpoint          = Endpoint + "/websites"
	CredentialsEndpoint       = Endpoint + "/credentials"
	BlockingRulesEndpoint     = Endpoint + "/blocking-rules"
	ConnectorsEndpoint        = Endpoint + "/connectors"
	ConnectorVersionsEndpoint = ConnectorsEndpoint + "/versions"
	ConnectionsEndpoint       = Endpoint + "/connections"
	ResourceManagersEndpoint  = Endpoint + "/resource-managers"
)

// PageSize is the largest page the maxResultCount endpoints document.
const PageSize = 100

// maxPages bounds a paged listing so a server that never reaches its own
// count cannot loop forever.
const maxPages = 50

// Redacted replaces a value that may carry a secret.
const Redacted = "[redacted]"

// Getter is the part of the v2 API client this package needs.
type Getter interface {
	Get(ctx context.Context, endpoint string) (json.RawMessage, error)
}

// --- Activation -----------------------------------------------------------

// Status is the activation gate.
type Status struct {
	IsActive  bool `json:"isActive"`
	IsExpired bool `json:"isExpired"`
	IsPAM     bool `json:"isPam"`
	IsPWM     bool `json:"isPwm"`
}

// ParseStatus decodes the gate. An unreadable body is an error, never
// "inactive": that would send an operator to enable something that is on.
func ParseStatus(raw json.RawMessage) (Status, error) {
	var s Status
	if err := json.Unmarshal(raw, &s); err != nil {
		return Status{}, fmt.Errorf("decoding the Privileged Access status: %w", err)
	}
	return s, nil
}

// RequireActive fails with one clear sentence when PAM is not usable.
func RequireActive(ctx context.Context, c Getter) error {
	raw, err := c.Get(ctx, StatusEndpoint)
	if err != nil {
		return err
	}
	s, err := ParseStatus(raw)
	if err != nil {
		return err
	}
	switch {
	case !s.IsActive || !s.IsPAM:
		return fmt.Errorf("Privileged Access is not active on this organization " +
			"(GET /privileged-access/status reports it off). It is enabled from the JumpCloud console")
	case s.IsExpired:
		return fmt.Errorf("the Privileged Access subscription on this organization has expired " +
			"(GET /privileged-access/status reports isExpired)")
	}
	return nil
}

// --- Ids ------------------------------------------------------------------

var objectIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// IsObjectID reports whether s is a 24-hex object id. Every object here uses
// one, except users, which are numbered.
func IsObjectID(s string) bool { return objectIDPattern.MatchString(s) }

func errBadID(kind, s string) error {
	if _, err := strconv.Atoi(s); err == nil {
		return fmt.Errorf("%q is a number, but Privileged Access %ss are addressed by a "+
			"24-character hex id (only users are numbered)", s, kind)
	}
	return fmt.Errorf("%q is not a Privileged Access %s id: take the 24-character hex id from the listing", s, kind)
}

// --- Envelopes ------------------------------------------------------------

// strictEnvelope decodes an object and requires the named list key to be
// present and an array. A shape change must fail loudly, not list nothing.
func strictEnvelope(raw json.RawMessage, key, what string) ([]json.RawMessage, map[string]json.RawMessage, error) {
	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, nil, fmt.Errorf("decoding the %s response: %w", what, err)
	}
	list, ok := env[key]
	if !ok {
		return nil, nil, fmt.Errorf("the %s response has no %q field; the API's envelope has changed", what, key)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(list, &rows); err != nil || rows == nil {
		return nil, nil, fmt.Errorf("the %s response's %q field is not a list; the API's envelope has changed", what, key)
	}
	// Every row of every listing is redacted here, in one place: several of
	// these listings (connectors, connections) were empty on the probed
	// tenant, so their full shape has never been seen.
	for i, r := range rows {
		red, err := Redact(r)
		if err != nil {
			return nil, nil, err
		}
		rows[i] = red
	}
	return rows, env, nil
}

func totalOf(env map[string]json.RawMessage, what string) (int, error) {
	raw, ok := env["totalCount"]
	if !ok {
		return 0, fmt.Errorf("the %s response has no totalCount; the API's envelope has changed", what)
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, fmt.Errorf("the %s response's totalCount is not a number", what)
	}
	return n, nil
}

func idOf(row json.RawMessage) string {
	var v struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(row, &v) != nil {
		return ""
	}
	return strings.Trim(string(v.ID), `"`)
}

// ListPaged walks a maxResultCount/skipCount listing ({items, totalCount}),
// dropping rows whose id was already seen, until it holds totalCount rows.
func ListPaged(ctx context.Context, c Getter, endpoint, what string) ([]json.RawMessage, int, error) {
	var out []json.RawMessage
	seen := map[string]bool{}
	for page := 0; page < maxPages; page++ {
		v := url.Values{}
		v.Set("maxResultCount", strconv.Itoa(PageSize))
		v.Set("skipCount", strconv.Itoa(page*PageSize))
		sep := "?"
		if strings.Contains(endpoint, "?") {
			sep = "&"
		}
		raw, err := c.Get(ctx, endpoint+sep+v.Encode())
		if err != nil {
			return nil, 0, err
		}
		rows, env, err := strictEnvelope(raw, "items", what)
		if err != nil {
			return nil, 0, err
		}
		total, err := totalOf(env, what)
		if err != nil {
			return nil, 0, err
		}
		for _, r := range rows {
			id := idOf(r)
			if id != "" && seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, r)
		}
		if len(out) >= total || len(rows) == 0 {
			return out, total, nil
		}
	}
	return nil, 0, fmt.Errorf("listing %s did not finish within %d pages of %d", what, maxPages, PageSize)
}

// ListResults reads an unpaginated {results, totalCount} listing. totalCount
// is the number to trust: the users listing adds the caller to results
// without counting them.
func ListResults(ctx context.Context, c Getter, endpoint, what string) ([]json.RawMessage, int, error) {
	raw, err := c.Get(ctx, endpoint)
	if err != nil {
		return nil, 0, err
	}
	rows, env, err := strictEnvelope(raw, "results", what)
	if err != nil {
		return nil, 0, err
	}
	total, err := totalOf(env, what)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ListUncounted reads a listing with no count: {results} for tags and groups,
// {items} for connector versions.
func ListUncounted(ctx context.Context, c Getter, endpoint, key, what string) ([]json.RawMessage, error) {
	raw, err := c.Get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	rows, _, err := strictEnvelope(raw, key, what)
	return rows, err
}

// SelfInjected is the users-listing caveat for surfaces to repeat.
const SelfInjected = "the users listing always includes the calling user, matched or not, " +
	"while its count excludes them; trust the count, not the number of rows"

// GroupProducts are the values groups/all accepts as productFilter.
var GroupProducts = map[string]string{
	"PAM": "GROUP_PRODUCT_PAM",
	"PWM": "GROUP_PRODUCT_PWM",
}

// GroupsEndpoint builds the groups/all request for a product ("" = all).
func GroupsEndpoint(product string) (string, error) {
	p := strings.ToUpper(strings.TrimSpace(product))
	if p == "" {
		return GroupsAllEndpoint, nil
	}
	full, ok := GroupProducts[p]
	if !ok {
		return "", fmt.Errorf("invalid product %q: expected PAM or PWM", product)
	}
	return GroupsAllEndpoint + "?productFilter=" + full, nil
}

// --- Resources: servers, databases, websites --------------------------------

// Kind is a resource family addressed through its edit view.
type Kind struct {
	Name     string // singular, for messages
	Endpoint string
	Paged    bool // maxResultCount/skipCount, else unpaginated limit/skip
}

var (
	Servers   = Kind{Name: "server", Endpoint: ServersEndpoint, Paged: true}
	Databases = Kind{Name: "database", Endpoint: DatabasesEndpoint, Paged: true}
	Websites  = Kind{Name: "website", Endpoint: WebsitesEndpoint}
)

// List returns every resource of a kind and the server's count.
func List(ctx context.Context, c Getter, k Kind) ([]json.RawMessage, int, error) {
	if k.Paged {
		return ListPaged(ctx, c, k.Endpoint, k.Name+"s")
	}
	return ListResults(ctx, c, k.Endpoint, k.Name+"s")
}

// Find picks one row by id or by exact name. A name shared by several rows is
// an error naming their ids.
func Find(rows []json.RawMessage, kind, ident string) (json.RawMessage, error) {
	var byName []json.RawMessage
	for _, r := range rows {
		var v struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(r, &v)
		if strings.EqualFold(idOf(r), ident) {
			return r, nil
		}
		if v.Name == ident {
			byName = append(byName, r)
		}
	}
	switch len(byName) {
	case 1:
		return byName[0], nil
	case 0:
		return nil, fmt.Errorf("no Privileged Access %s with id or name %q", kind, ident)
	}
	ids := make([]string, len(byName))
	for i, r := range byName {
		ids[i] = idOf(r)
	}
	return nil, fmt.Errorf("%d Privileged Access %ss are named %q (%s); use an id",
		len(byName), kind, ident, strings.Join(ids, ", "))
}

// Get resolves a resource by id or name from its listing, then reads its edit
// view. Resolving first is what keeps an unknown server from becoming a
// retried 500, and is what makes names work.
func Get(ctx context.Context, c Getter, k Kind, ident string) (json.RawMessage, error) {
	rows, _, err := List(ctx, c, k)
	if err != nil {
		return nil, err
	}
	row, err := Find(rows, k.Name, ident)
	if err != nil {
		return nil, err
	}
	raw, err := c.Get(ctx, k.Endpoint+"/"+idOf(row)+"/edit")
	if err != nil {
		return nil, err
	}
	return Redact(raw)
}

// --- Credentials ----------------------------------------------------------

// PersonalFields are credential-listing fields drawn from cards and identity
// documents. They are not secrets, but they are personal data and are not
// needed to manage access.
var PersonalFields = []string{"last4Digits", "cardholderName", "idType", "firstName"}

// StripPersonalFields removes PersonalFields from each row.
func StripPersonalFields(rows []json.RawMessage) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(rows))
	for _, r := range rows {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(r, &m); err != nil {
			return nil, fmt.Errorf("decoding a credential row: %w", err)
		}
		for _, k := range PersonalFields {
			delete(m, k)
		}
		b, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// ListCredentials returns the privileged credentials (only those: a Password
// Vault credential that is not privileged is not listed here).
func ListCredentials(ctx context.Context, c Getter) ([]json.RawMessage, int, error) {
	return ListResults(ctx, c, CredentialsEndpoint, "credentials")
}

// credentialShellKeys is the allow-list for the credential edit-shell view.
// Anything else the endpoint starts returning is dropped, not shown.
var credentialShellKeys = map[string]bool{
	"id": true, "name": true, "credentialType": true, "accessPolicies": true,
	"canViewSecret": true, "credentialRotationId": true,
}

// GetCredential reads a credential's edit shell: its name, type, access
// policies and whether the caller may view the secret. Never the secret. An
// unknown id is a reliable 404 here.
func GetCredential(ctx context.Context, c Getter, id string) (json.RawMessage, error) {
	if !IsObjectID(id) {
		return nil, errBadID("credential", id)
	}
	raw, err := c.Get(ctx, CredentialsEndpoint+"/"+id+"/edit-shell")
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("decoding the credential: %w", err)
	}
	for k := range m {
		if !credentialShellKeys[k] {
			delete(m, k)
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return Redact(b)
}

// CredentialManagers lists who manages a credential. The endpoint reports an
// unknown id as 400, so existence is confirmed through the edit shell first.
func CredentialManagers(ctx context.Context, c Getter, id string) ([]json.RawMessage, int, error) {
	if _, err := GetCredential(ctx, c, id); err != nil {
		return nil, 0, err
	}
	return ListResults(ctx, c, CredentialsEndpoint+"/"+id+"/managers", "credential managers")
}

// TokenPage is one page of a continuation-token listing.
type TokenPage struct {
	Items []json.RawMessage
	Next  string
}

func tokenList(ctx context.Context, c Getter, endpoint, key, token, what string) (TokenPage, error) {
	if token != "" {
		endpoint += "?continuationToken=" + url.QueryEscape(token)
	}
	raw, err := c.Get(ctx, endpoint)
	if err != nil {
		return TokenPage{}, err
	}
	rows, env, err := strictEnvelope(raw, key, what)
	if err != nil {
		return TokenPage{}, err
	}
	var next string
	if t, ok := env["continuationToken"]; ok {
		_ = json.Unmarshal(t, &next)
	}
	return TokenPage{Items: rows, Next: next}, nil
}

// CredentialHistory lists a credential's versions ({continuationToken, items}).
func CredentialHistory(ctx context.Context, c Getter, id, token string) (TokenPage, error) {
	if !IsObjectID(id) {
		return TokenPage{}, errBadID("credential", id)
	}
	return tokenList(ctx, c, CredentialsEndpoint+"/"+id+"/history", "items", token, "credential history")
}

// CredentialLinkedServers lists the servers a credential is linked to
// ({continuationToken, results}). An unknown id is a 500 here, so existence
// is confirmed through the edit shell first.
func CredentialLinkedServers(ctx context.Context, c Getter, id, token string) (TokenPage, error) {
	if _, err := GetCredential(ctx, c, id); err != nil {
		return TokenPage{}, err
	}
	return tokenList(ctx, c, CredentialsEndpoint+"/"+id+"/linked-servers", "results", token, "linked servers")
}

// --- Blocking rules ---------------------------------------------------------

// criteriaPrefix is what the listing and the write side put in front of the
// criteria type, and what the edit view refuses.
const criteriaPrefix = "BLOCKING_RULE_CRITERIA_TYPE_"

// ListBlockingRules returns every blocking rule.
func ListBlockingRules(ctx context.Context, c Getter) ([]json.RawMessage, int, error) {
	return ListPaged(ctx, c, BlockingRulesEndpoint, "blocking rules")
}

// GetBlockingRule resolves a rule by id or name and reads its edit view with
// the short criteria type the view requires.
func GetBlockingRule(ctx context.Context, c Getter, ident string) (json.RawMessage, error) {
	rows, _, err := ListBlockingRules(ctx, c)
	if err != nil {
		return nil, err
	}
	row, err := Find(rows, "blocking rule", ident)
	if err != nil {
		return nil, err
	}
	var v struct {
		CriteriaType string `json:"criteriaType"`
	}
	_ = json.Unmarshal(row, &v)
	short := strings.TrimPrefix(v.CriteriaType, criteriaPrefix)
	if short == "" {
		return nil, fmt.Errorf("blocking rule %s has no criteria type in the listing, which its detail read requires", idOf(row))
	}
	raw, err := c.Get(ctx, BlockingRulesEndpoint+"/"+idOf(row)+"/edit?criteriaType="+url.QueryEscape(short))
	if err != nil {
		return nil, err
	}
	return Redact(raw)
}

// --- Connections ------------------------------------------------------------

// ResourceConnectionStatus reports who is connected to a server, database or
// website. The endpoint answers "Available" for an id that matches nothing,
// so the resource is looked up in the three listings first and its kind is
// returned alongside.
func ResourceConnectionStatus(ctx context.Context, c Getter, ident string) (json.RawMessage, string, error) {
	for _, k := range []Kind{Servers, Databases, Websites} {
		rows, _, err := List(ctx, c, k)
		if err != nil {
			return nil, "", err
		}
		row, ferr := Find(rows, k.Name, ident)
		if ferr != nil {
			if strings.Contains(ferr.Error(), "use an id") {
				return nil, "", ferr
			}
			continue
		}
		raw, err := c.Get(ctx, ConnectionsEndpoint+"/status/by-resource/"+idOf(row))
		if err != nil {
			return nil, "", err
		}
		if raw, err = Redact(raw); err != nil {
			return nil, "", err
		}
		return raw, k.Name, nil
	}
	return nil, "", fmt.Errorf("no Privileged Access server, database or website with id or name %q", ident)
}

// --- Redaction --------------------------------------------------------------

// secretKeyParts mark a key whose string or structured value may carry a
// secret. A database's connectionString can embed a password; the rest are
// defence in depth against fields the views may start returning, including
// pre-signed recording and download links and connector enrollment values.
// Keys are compared lower-cased with "_", "-" and "." removed.
var secretKeyParts = []string{"password", "passwd", "secret", "passphrase", "privatekey",
	"connectionstring", "dsn", "token", "mfakey", "otp", "totp", "apikey", "cardnumber",
	"securitycode", "cvv", "pincode", "enrollment", "presigned", "signedurl",
	"downloadurl", "videourl", "recordingurl"}

// secretKeyExact are short keys that would match too much as substrings.
var secretKeyExact = map[string]bool{"pass": true, "pin": true, "key": true, "pwd": true}

func secretKey(k string) bool {
	l := strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(k))
	if secretKeyExact[l] {
		return true
	}
	for _, p := range secretKeyParts {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}

// Redact replaces the value of every secret-looking key, at any depth, that
// holds a non-empty string or any object or array. Booleans, numbers, nulls
// and empty strings say nothing secret (canViewSecret is a flag) and are kept.
func Redact(raw json.RawMessage) (json.RawMessage, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("decoding a Privileged Access response: %w", err)
	}
	b, err := json.Marshal(redactValue(v))
	if err != nil {
		return nil, err
	}
	return b, nil
}

func redactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if secretKey(k) {
				switch y := x.(type) {
				case string:
					if y != "" {
						t[k] = Redacted
					}
				case map[string]any, []any:
					t[k] = Redacted
				}
				continue
			}
			t[k] = redactValue(x)
		}
		return t
	case []any:
		for i, x := range t {
			t[i] = redactValue(x)
		}
		return t
	}
	return v
}
