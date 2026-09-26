// Package passwordvault is the shared contract for JumpCloud Password Vault,
// used by the CLI and the MCP tools so the two cannot drift.
//
// Password Vault replaces Password Manager. Both surfaces are live while
// customers migrate, they model resources differently — Password Manager is
// folders/items/backup-keys, Vault is credentials/folders/websites/groups —
// and so they share no code beyond this comment. See internal/pwm.
//
// Internally this is JumpCloud's PAM product: every schema in the spec is
// named jumpcloud.privileged_access.*, and the activation gate reports an
// isPam flag alongside isActive.
//
// Everything here was established by probing a live tenant on 2026-09-25,
// because the spec is wrong about this area in ways that would produce silent
// data loss: it marks nothing required on bodies that reject an omitted
// field with HTTP 500, and it documents query parameters the server does not
// accept while requiring ones it does not document.
package passwordvault

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Endpoints. The tree is /password-vault, hyphenated — unlike Password
// Manager's /passwordmanager, which is one word.
const (
	Endpoint                  = "/password-vault"
	StatusEndpoint            = Endpoint + "/status"
	TenantSettingsEndpoint    = Endpoint + "/tenant-settings"
	DefaultPermsEndpoint      = TenantSettingsEndpoint + "/default-permissions"
	DashboardOverviewEndpoint = Endpoint + "/dashboard-overview"
	UsersEndpoint             = Endpoint + "/users"
	UsersSelfEndpoint         = UsersEndpoint + "/self"
	UsersSelfJCEndpoint       = UsersSelfEndpoint + "/jc-user"
	ActivePWMTenantsEndpoint  = UsersEndpoint + "/active-pwm-tenants"
	GroupsEndpoint            = Endpoint + "/groups"
	GroupsAllEndpoint         = GroupsEndpoint + "/all"
	TagsEndpoint              = Endpoint + "/tags"
)

// GroupEndpoint and friends address one group. Group ids are INTEGERS here,
// not the 24-hex object ids the rest of JumpCloud uses and not the UUIDs
// Password Manager uses.
func GroupEndpoint(id int) string { return GroupsEndpoint + "/" + strconv.Itoa(id) }
func GroupMembersEndpoint(id int) string {
	return GroupEndpoint(id) + "/members"
}
func GroupResourcesEndpoint(id int) string {
	return GroupEndpoint(id) + "/resources"
}
func GroupAssignableEndpoint(id int) string {
	return GroupEndpoint(id) + "/assignable-resources"
}
func GroupDeactivateEndpoint(id int) string {
	return GroupEndpoint(id) + "/deactivate"
}

// --- Activation gate ------------------------------------------------------

// Status is the activation gate. It answers WITHOUT the caller being entitled
// to anything else in the tree, which is what makes it usable as a probe:
// before activation it returns 200 with isActive false while every other
// Vault path returns 404.
type Status struct {
	IsActive   bool   `json:"isActive"`
	IsPAM      bool   `json:"isPam"`
	SSOAppID   string `json:"jumpcloudSsoApplicationId"`
	ConsoleURL string `json:"vaultoneConsoleUrl"`
}

// ParseStatus decodes the gate. A body that will not decode is an error
// rather than an inactive status: reporting "not activated" because the
// response was unreadable would send an operator to enable something that is
// already on.
func ParseStatus(raw json.RawMessage) (Status, error) {
	var s Status
	if err := json.Unmarshal(raw, &s); err != nil {
		return Status{}, fmt.Errorf("decoding the Password Vault status: %w", err)
	}
	return s, nil
}

// ErrNotActivated is the message every surface shows when a Vault command is
// run against an org that has not activated. It names the gate, because a
// bare 404 from some other endpoint is what this exists to replace.
func ErrNotActivated() error {
	return fmt.Errorf("Password Vault is not active on this organization " +
		"(GET /password-vault/status reports isActive false). Every other Password " +
		"Vault endpoint returns 404 until it is activated, which is done from the " +
		"JumpCloud console")
}

// --- Ids ------------------------------------------------------------------

// objectIDPattern matches the 24-hex object ids used by credentials, folders
// and websites. Users and groups use integers instead, so this area carries
// two id shapes and a caller must know which resource it is addressing.
var objectIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// IsObjectID reports whether s is a 24-hex object id.
func IsObjectID(s string) bool { return objectIDPattern.MatchString(s) }

// ParseGroupID turns an operator-supplied group id into the integer the API
// wants, rejecting the 24-hex and UUID shapes explicitly. Those are what a
// user coming from anywhere else in JumpCloud — or from Password Manager —
// will try first, and the server's own answer to them is unhelpful.
func ParseGroupID(s string) (int, error) {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n, nil
	}
	switch {
	case IsObjectID(s):
		return 0, fmt.Errorf("%q is a JumpCloud 24-character object id, but Password Vault "+
			"groups are numbered: take the integer id from `jc password-vault groups list`", s)
	case strings.Count(s, "-") == 4:
		return 0, fmt.Errorf("%q is a UUID, which is what Password MANAGER uses. Password "+
			"Vault groups are numbered: take the integer id from `jc password-vault groups list`", s)
	}
	return 0, fmt.Errorf("%q is not a Password Vault group id — groups are numbered, and the "+
		"id comes from `jc password-vault groups list`", s)
}

// --- Required filters -----------------------------------------------------
//
// Two endpoints reject a call that omits a filter the spec marks optional,
// and neither names a query parameter that actually works: /tags answers
// "targetKind must be CREDENTIAL or RESOURCE" to a request that sets
// ?targetKind=CREDENTIAL. The working form is jc's own filter syntax.

// TagTargetKinds are the values /tags accepts.
var TagTargetKinds = []string{"CREDENTIAL", "RESOURCE"}

// TagsQuery builds the query string /tags requires. Unlike the category
// filter below, this one IS validated by the server, so a bad value is
// rejected here to keep the message useful.
func TagsQuery(targetKind string) (url.Values, error) {
	k := strings.ToUpper(strings.TrimSpace(targetKind))
	for _, valid := range TagTargetKinds {
		if k == valid {
			v := url.Values{}
			v.Set("filter", "targetKind:eq:"+k)
			return v, nil
		}
	}
	return nil, fmt.Errorf("invalid target kind %q: expected %s",
		targetKind, strings.Join(TagTargetKinds, " or "))
}

// AssignableQuery builds the query string /groups/{id}/assignable-resources
// requires. The server rejects a call with no category and then accepts ANY
// category value, returning an empty list for one that means nothing — so a
// typo is indistinguishable from a genuinely empty result. Callers should
// surface the category they asked for alongside the answer.
func AssignableQuery(category string) (url.Values, error) {
	c := strings.ToUpper(strings.TrimSpace(category))
	if c == "" {
		return nil, fmt.Errorf("a resource category is required, e.g. CREDENTIAL or WEBSITE")
	}
	v := url.Values{}
	v.Set("filter", "category:eq:"+c)
	return v, nil
}

// CategoryUnvalidated documents the trap above for surfaces to repeat.
const CategoryUnvalidated = "the API does not validate the category: an unrecognised one " +
	"returns an empty list rather than an error, so an empty result here is not evidence " +
	"that the group has no assignable resources of that kind"

// --- Pagination -----------------------------------------------------------

// PaginationUnsafe records why no surface pages this area.
//
// Measured on a four-user tenant: `sort` is accepted and ignored — id, name,
// userName and -id all return byte-identical order — while limit and skip
// produce pages that both duplicate and omit records. One user appeared at
// skip=0, skip=2 and skip=3 of the same listing. The results are
// deterministic across runs, so this is a server defect rather than a race.
//
// Fetching unpaginated and trusting totalCount is the only correct approach.
const PaginationUnsafe = "Password Vault ignores sort, and its limit/skip paging both " +
	"duplicates and omits records, so jc fetches these lists unpaginated"

// --- Envelopes ------------------------------------------------------------
//
// Three shapes. Each parser reports a decode failure rather than an empty
// result, because a caller that cannot tell "none" from "unreadable" reports
// an empty vault when it cannot see one.

func decode(raw json.RawMessage, v any, what string) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("decoding the %s response: %w", what, err)
	}
	return nil
}

// ParseList reads {results, totalCount}, used by users, groups and the group
// sub-listings.
//
// totalCount is the SERVER'S match count and is the number to trust.
// len(results) is not: the users listing always includes the calling user
// whether or not they match the query, so a search with no matches returns
// one row with totalCount zero, and a limit of n returns n+1 rows. That is
// not a paging off-by-one — it is the caller being injected.
func ParseList(raw json.RawMessage, what string) ([]json.RawMessage, int, error) {
	var env struct {
		Results    []json.RawMessage `json:"results"`
		TotalCount int               `json:"totalCount"`
	}
	if err := decode(raw, &env, what); err != nil {
		return nil, 0, err
	}
	return env.Results, env.TotalCount, nil
}

// ParseResultsOnly reads {results} with NO count field — groups/all and tags.
func ParseResultsOnly(raw json.RawMessage, what string) ([]json.RawMessage, error) {
	var env struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := decode(raw, &env, what); err != nil {
		return nil, err
	}
	return env.Results, nil
}

// SelfInjected documents the users-listing behaviour for surfaces to repeat.
const SelfInjected = "the users listing always includes the calling user, matched or not, " +
	"while the count excludes them — trust the count, not the number of rows"

// ParseWrapped unwraps a single-key envelope — tenant-settings nests
// everything under "values" and default-permissions under
// "defaultPermissions", where every sibling single-object read in this area
// returns the object directly.
func ParseWrapped(raw json.RawMessage, key, what string) (json.RawMessage, error) {
	var env map[string]json.RawMessage
	if err := decode(raw, &env, what); err != nil {
		return nil, err
	}
	inner, ok := env[key]
	if !ok {
		return nil, fmt.Errorf("the %s response has no %q field; the envelope has changed", what, key)
	}
	return inner, nil
}

// TenantSettingsBody wraps an updated settings object back into the envelope
// the PUT expects. The GET nests under "values" and the PUT takes the same
// shape, which is easy to get wrong in the direction that silently writes
// nothing.
func TenantSettingsBody(values json.RawMessage) (json.RawMessage, error) {
	b, err := json.Marshal(map[string]json.RawMessage{"values": values})
	if err != nil {
		return nil, fmt.Errorf("encoding the tenant settings body: %w", err)
	}
	return b, nil
}

// --- Credentials, folders and websites ------------------------------------

const (
	CredentialsEndpoint   = Endpoint + "/credentials"
	CredentialColumnsPath = CredentialsEndpoint + "/excel/columns"
	FoldersEndpoint       = Endpoint + "/folders"
	WebsitesEndpoint      = Endpoint + "/websites"
	WebsiteColumnsPath    = WebsitesEndpoint + "/excel/columns"
)

func CredentialEndpoint(id string) string   { return CredentialsEndpoint + "/" + id }
func CredentialActivities(id string) string { return CredentialEndpoint(id) + "/activities" }
func CredentialHistory(id string) string    { return CredentialEndpoint(id) + "/history" }
func CredentialManagers(id string) string   { return CredentialEndpoint(id) + "/managers" }

func FolderEndpoint(id string) string      { return FoldersEndpoint + "/" + id }
func FolderEditEndpoint(id string) string  { return FolderEndpoint(id) + "/edit" }
func FolderItemsEndpoint(id string) string { return FolderEndpoint(id) + "/items" }
func FolderManagers(id string) string      { return FolderEndpoint(id) + "/managers" }

func WebsiteEndpoint(id string) string     { return WebsitesEndpoint + "/" + id }
func WebsiteEditEndpoint(id string) string { return WebsiteEndpoint(id) + "/edit" }
func WebsiteActivities(id string) string   { return WebsiteEndpoint(id) + "/activities" }
func WebsiteManagers(id string) string     { return WebsiteEndpoint(id) + "/managers" }
func WebsiteConnectLinks(id string) string { return WebsiteEndpoint(id) + "/connect-links" }
func WebsiteParamsExtension(id string) string {
	return WebsiteEndpoint(id) + "/parameters-extension"
}

// ErrNotVaultObjectID explains a malformed id before it reaches the API.
// Credentials, folders and websites use 24-hex object ids — unlike users and
// groups in the same area, which are numbered.
func ErrNotVaultObjectID(kind, s string) error {
	if _, err := strconv.Atoi(s); err == nil {
		return fmt.Errorf("%q is a number, and Password Vault %ss are addressed by a "+
			"24-character hex id — only users and groups are numbered in this area", s, kind)
	}
	return fmt.Errorf("%q is not a Password Vault %s id — take the 24-character hex id "+
		"from the listing", s, kind)
}

// DetailReadRefused reports whether an error is the server refusing a detail
// read for a permission the caller demonstrably holds.
//
// Reproduced on 2026-09-25 against a freshly created credential whose access
// policy granted the calling user Manage, View Detail and Connect:
// GET /credentials/{id} and GET /credentials/{id}/activities both answered
// 400 {"message":"For this resource , you need permissions View Detail."}
// while /history and /managers answered 200. The same refusal appears on
// GET /websites/{id}. It is not a permission problem the operator can fix.
func DetailReadRefused(err error) bool {
	return err != nil && strings.Contains(err.Error(), "you need permissions View Detail")
}

// ErrDetailReadRefused turns that refusal into something actionable, naming
// the read that does work for the family in question.
func ErrDetailReadRefused(kind, alternative string) error {
	msg := fmt.Sprintf("the API refused this %s detail read, asking for a \"View Detail\" "+
		"permission that the calling user holds — reproduced against a freshly created "+
		"record granting it explicitly, so it is a defect in the endpoint rather than "+
		"something to fix in the access policy", kind)
	if alternative != "" {
		msg += ". " + alternative
	}
	return fmt.Errorf("%s", msg)
}

// FolderNotFoundIs400 records that a folder that does not exist is reported
// with 400 INVALID_ARGUMENT "Folder not found." rather than 404, while an
// absent credential or website gives 404 — and an absent credential's
// /managers gives 400 "Credential not found." So three absence signals across
// three families, and two within one of them.
func FolderNotFoundIs400(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Folder not found")
}

// ParseWrappedFolder unwraps {folder:{...}}, which both the folder detail and
// the folder edit view return where every sibling read returns the object
// directly.
func ParseWrappedFolder(raw json.RawMessage) (json.RawMessage, error) {
	return ParseWrapped(raw, "folder", "folder")
}

// ParseColumns unwraps {columns:{...}} from the Excel column metadata.
func ParseColumns(raw json.RawMessage) (json.RawMessage, error) {
	return ParseWrapped(raw, "columns", "import/export columns")
}

// ParseHistory reads {continuationToken, items} — the one endpoint in this
// area that pages with a token rather than skip/limit, and the only paging
// here that can be trusted.
func ParseHistory(raw json.RawMessage) ([]json.RawMessage, string, error) {
	var env struct {
		Items             []json.RawMessage `json:"items"`
		ContinuationToken string            `json:"continuationToken"`
	}
	if err := decode(raw, &env, "credential history"); err != nil {
		return nil, "", err
	}
	return env.Items, env.ContinuationToken, nil
}

// --- Writes ---------------------------------------------------------------
//
// THE CENTRAL FACT ABOUT WRITING TO THIS API: a write returns 200 with an
// empty body whether or not it did anything.
//
// Measured on 2026-09-25. POST /credentials/archive with a body of {} returns
// 200 and archives nothing; only {"ids":[...]} takes effect. POST
// /folders/{id}/items with {"ids":[...]} returns 200 and adds nothing; the key
// is "credentialIds". In both cases the wrong body is indistinguishable from
// the right one by its response.
//
// So a surface here must not report success from a 200. It reads the object
// back and reports what the server actually holds. That doubles the calls on a
// write, which is the correct trade when the alternative is telling somebody
// their credentials are archived when they are not.
const WriteReportsSuccessRegardless = "this API returns 200 with an empty body whether or " +
	"not a write took effect, so jc reads the record back and reports the state the server " +
	"actually holds rather than trusting the response"

// ArchiveBody builds the body for the credential archive and unarchive
// endpoints. The key is "ids"; an empty list is refused here because the
// server would accept it, answer 200 and do nothing.
func ArchiveBody(ids []string) (json.RawMessage, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("no credential ids given — the API would accept this and " +
			"report success without archiving anything")
	}
	for _, id := range ids {
		if !IsObjectID(id) {
			return nil, ErrNotVaultObjectID("credential", id)
		}
	}
	b, err := json.Marshal(map[string][]string{"ids": ids})
	if err != nil {
		return nil, fmt.Errorf("encoding the archive body: %w", err)
	}
	return b, nil
}

// FolderItemsBody builds the body for adding items to a folder. The key is
// "credentialIds" — "ids", which is what the archive endpoints take, returns
// 200 here and adds nothing.
func FolderItemsBody(credentialIDs []string) (json.RawMessage, error) {
	if len(credentialIDs) == 0 {
		return nil, fmt.Errorf("no credential ids given — the API would accept this and " +
			"report success without adding anything")
	}
	for _, id := range credentialIDs {
		if !IsObjectID(id) {
			return nil, ErrNotVaultObjectID("credential", id)
		}
	}
	b, err := json.Marshal(map[string][]string{"credentialIds": credentialIDs})
	if err != nil {
		return nil, fmt.Errorf("encoding the folder items body: %w", err)
	}
	return b, nil
}

// --- The lockout rule -----------------------------------------------------

// Permission vocabularies, per family. They are not interchangeable, they are
// nowhere in the spec, and they were recovered from 400 messages.
var (
	// CredentialPermissions also govern websites, which use the credential
	// vocabulary rather than a namespaced one of their own.
	CredentialPermissions = []string{"Manage", "View Detail", "Connect"}
	// FolderPermissions are namespaced. Passing credential-vocabulary values
	// to a folder is ACCEPTED at create time and then grants nothing.
	FolderPermissions = []string{"Folder.Manage", "Folder.Item.Manage", "Folder.View", "Folder.Connect"}
)

// LockoutWarning is why every create must grant the caller full management.
//
// Deleting needs Manage (or Folder.Manage); changing an access policy needs
// the same. So an object created without it cannot be managed OR repaired
// through the API — there is no recovery path. An object created with
// isPrivate and no users is worse: invisible to the listing, unreadable and
// undeletable. Three such objects were stranded on the probe tenant before
// this was understood.
const LockoutWarning = "a Password Vault object created without full management permission " +
	"for its creator cannot afterwards be deleted or have its permissions changed through " +
	"the API — there is no recovery path, so jc always grants them"

// AccessPolicyFor builds an access policy granting one user the full
// management set for a family, which is the only shape that cannot strand the
// object. userID is the INTEGER vault id, not a JumpCloud object id.
func AccessPolicyFor(userID int, family string) (map[string]any, error) {
	var perms []string
	switch strings.ToLower(family) {
	case "credential", "website":
		perms = CredentialPermissions
	case "folder":
		perms = FolderPermissions
	default:
		return nil, fmt.Errorf("unknown Password Vault family %q: expected credential, website or folder", family)
	}
	if userID <= 0 {
		return nil, fmt.Errorf("a vault user id is required — take it from `jc password-vault users self`")
	}
	return map[string]any{
		"users": []map[string]any{{"id": userID, "permissions": perms}},
	}, nil
}

// HeldBackWrites records the write operations this area serves that jc does
// NOT expose, and why each one is held.
//
// The common reason is the one at the top of this section: a write here
// answers 200 with an empty body whether or not it did anything. An operation
// whose effect cannot be read back afterwards therefore cannot be reported
// honestly — jc would be passing on a success it has no evidence for. Where
// that is the reason, the entry says what would have to be observable before
// the operation could ship.
var HeldBackWrites = map[string]string{
	"DELETE /credentials/bulk-delete": "takes a body rather than a path id, and the correct " +
		"key was not established. A wrong key here answers 200 and deletes nothing, which is " +
		"survivable — but the right key on a wrong list deletes credentials, which is not. " +
		"`credentials delete` addresses one record by path and is confirmed by read-back.",

	"DELETE /folders/{id}/items": "the add path takes credentialIds while the archive " +
		"endpoints take ids; which key the remove path wants was not established, and the " +
		"wrong one answers 200 and removes nothing. Probe it against a folder with known " +
		"contents and confirm the item count changes before shipping.",

	"POST /folders/{id}/items/move": "moves credentials between folders, which changes who " +
		"can reach them — folder membership is an access grant here. Unverified, and the " +
		"failure mode is silent.",

	"POST /folders/{id}/items/move-to-default": "same, with no target to name: it moves " +
		"everything out of a folder in one call.",

	"PUT /folders/{id}/policies": "body shape unobserved. This writes the access policies, " +
		"which is the one write that can strand an object beyond recovery — see " +
		"LockoutWarning. It should not ship on a guess.",

	"DELETE /folders/{id}/users/self": "removes the caller's own access to a folder. If the " +
		"caller holds the only Folder.Manage grant, this strands the folder permanently: " +
		"there is no API path back in. It is the lockout trap as a single call.",

	"POST /websites/{id}/links":                        "link bodies unobserved; a website with no linked credentials answers 400 with an empty message, so the failure shape is not readable either.",
	"POST /websites/{id}/links/{linkId}/confirmations": "depends on a link, which cannot be created yet.",
}
