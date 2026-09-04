// Package pwm is the shared contract for JumpCloud Password Manager, used by
// the CLI, the MCP tools and the TUI so the three cannot drift.
//
// Everything here was established by probing a live tenant, because the
// OpenAPI spec is wrong about this area in ways that matter — id formats,
// envelope shapes, and which endpoints exist at all.
package pwm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Endpoints. Note the tree is /passwordmanager, one word.
const (
	Endpoint              = "/passwordmanager"
	UsersEndpoint         = Endpoint + "/users"
	GroupsEndpoint        = Endpoint + "/groups"
	SharedFoldersEndpoint = Endpoint + "/sharedfolders"
	ItemsEndpoint         = Endpoint + "/items"
	OverviewEndpoint      = Endpoint + "/overview"
	BackupKeysEndpoint    = Endpoint + "/backup/keys"
	FolderPolicyEndpoint  = Endpoint + "/policy/sharedfolders"
	CompanyPolicyEndpoint = Endpoint + "/company/policies"
)

// FolderEndpoint and friends address one shared folder.
//
// Note the identity is spelled THREE ways within this one resource: creating a
// folder returns `folderId`, listing them returns `uuid`, and the detail object
// uses its own shape. Callers join on the value, never on the key name.
func FolderEndpoint(id string) string       { return SharedFoldersEndpoint + "/" + id }
func FolderUsersEndpoint(id string) string  { return FolderEndpoint(id) + "/users" }
func FolderGroupsEndpoint(id string) string { return FolderEndpoint(id) + "/groups" }

// UserEndpoint and friends address one record.
func UserEndpoint(id string) string              { return UsersEndpoint + "/" + id }
func GroupEndpoint(id string) string             { return GroupsEndpoint + "/" + id }
func UserSharedFoldersEndpoint(id string) string { return UserEndpoint(id) + "/sharedFolders" }
func UserItemsEndpoint(id string) string         { return UserEndpoint(id) + "/items" }

// NotImplemented records endpoints this area advertises and does not serve.
//
// Every one was probed against a healthy tenant with real data. They are kept
// here rather than deleted so the next person reading the spec does not
// rediscover them one 404 at a time — and so a reviewer can see the surface is
// smaller than the spec by design, not by omission.
//
// The failures are not uniform, which matters: a 500 looks like an outage and a
// 404 looks like a typo, and neither says "this was never built".
var NotImplemented = map[string]string{
	Endpoint + "/appversions":                   "HTTP 500 — spec'd, never served",
	UsersEndpoint + "/{id}/apps":                "HTTP 404 — though `apps` IS a field on the user record",
	GroupsEndpoint + "/{id}":                    "HTTP 404 — groups are LIST-ONLY, there is no detail endpoint",
	GroupsEndpoint + "/{id}/users":              "HTTP 404",
	GroupsEndpoint + "/{id}/sharedFolders":      "HTTP 404",
	GroupsEndpoint + "/{id}/items":              "HTTP 404",
	SharedFoldersEndpoint + "/{id}/items":       "HTTP 404",
	SharedFoldersEndpoint + "/{id}/members":     "HTTP 404 — use /users and /groups instead",
	"DELETE " + SharedFoldersEndpoint + "/{id}": "HTTP 404 — a folder can be CREATED and never removed via the API",
}

// AppVersionsEndpoint is spec'd and NOT implemented server-side. Kept as a
// named constant so a search for it lands on this comment.
const AppVersionsEndpoint = Endpoint + "/appversions"

// idPattern matches the UUIDs Password Manager uses for its own records.
//
// This area does NOT use the 24-character hex ObjectIDs the rest of JumpCloud
// uses, so the shared resolver's IsID would reject every valid PWM id.
var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsID reports whether s looks like a Password Manager record id.
//
// Checking locally is not defensive tidiness, it is required: the API answers
// HTTP 500 for a JumpCloud 24-hex id, for a well-formed UUID that does not
// exist, AND presumably for a genuine server fault — all three produce the
// same "Unexpected error". Both were confirmed live. So an id that reaches the
// API unvalidated cannot be told apart from an outage, and a caller who
// mistypes gets a message suggesting JumpCloud is broken.
func IsID(s string) bool { return idPattern.MatchString(s) }

// ErrNotPWMID explains the id-shape mismatch in the terms a caller will hit.
func ErrNotPWMID(s string) error {
	if jcObjectID.MatchString(s) {
		return fmt.Errorf("%q is a JumpCloud object id, not a Password Manager id: "+
			"Password Manager keys its own records by UUID and maps back to JumpCloud through "+
			"externalId. Pass a name or username instead and it will be resolved", s)
	}
	return fmt.Errorf("%q is not a Password Manager id (expected a UUID) — pass a name or "+
		"username and it will be resolved", s)
}

var jcObjectID = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// User is a Password Manager user.
//
// ExternalId is the bridge back to the directory: it holds the JumpCloud user
// id, which is how a name typed by an operator becomes a PWM record. Nothing
// else links the two.
type User struct {
	ID           string `json:"id"`
	ExternalID   string `json:"externalId"`
	EmployeeUUID string `json:"employeeUuid,omitempty"`
	Name         string `json:"name"`
	Username     string `json:"username,omitempty"`
	Email        string `json:"email,omitempty"`
	Status       string `json:"status,omitempty"`
	// Groups are objects, not names. Typing this as []string made every user
	// record fail to unmarshal, and the silent skip below turned that into
	// "0 users are enrolled" — a wrong answer with no error.
	Groups     []UserGroupRef `json:"groups,omitempty"`
	ItemsCount int            `json:"itemsCount,omitempty"`
	// Password hygiene counters, which are the point of the overview.
	PasswordsCount int `json:"passwordsCount,omitempty"`
	PasswordsScore int `json:"passwordsScore,omitempty"`
	WeakPasswords  int `json:"weakPasswords,omitempty"`
	OldPasswords   int `json:"oldPasswords,omitempty"`
}

// UserGroupRef is a group as it appears nested on a user record. Several of
// its fields come back empty there; the group list is authoritative.
type UserGroupRef struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ExternalID string `json:"externalId,omitempty"`
}

// Group is a Password Manager group, likewise bridged by ExternalID.
type Group struct {
	ID         string `json:"id"`
	ExternalID string `json:"externalId"`
	Name       string `json:"name"`
	UsersCount int    `json:"usersCount,omitempty"`
	CreatedAt  string `json:"createdAt,omitempty"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
}

// ParseList unwraps the {results, totalCount} envelope.
//
// Three envelope shapes exist in this one area, which is why parsing is
// centralised: most families use {results, totalCount}; overview,
// policy/sharedfolders and company/policies are BARE objects with no envelope
// at all; and /items returns results, totalCount AND a separate items key.
func ParseList(raw json.RawMessage) ([]json.RawMessage, int, error) {
	var env struct {
		Results    []json.RawMessage `json:"results"`
		TotalCount int               `json:"totalCount"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, 0, fmt.Errorf("parsing Password Manager list: %w", err)
	}
	return env.Results, env.TotalCount, nil
}

// ParseItems handles /items, which returns BOTH results and items.
//
// Both were empty on the tenant probed, so which one wins when populated is
// unknown. Returning results and reporting whether items differed keeps that
// honest rather than silently picking one.
func ParseItems(raw json.RawMessage) (results []json.RawMessage, items []json.RawMessage, total int, err error) {
	var env struct {
		Results    []json.RawMessage `json:"results"`
		Items      []json.RawMessage `json:"items"`
		TotalCount int               `json:"totalCount"`
	}
	if uerr := json.Unmarshal(raw, &env); uerr != nil {
		return nil, nil, 0, fmt.Errorf("parsing Password Manager items: %w", uerr)
	}
	return env.Results, env.Items, env.TotalCount, nil
}

// ParseUsers decodes the user list.
func ParseUsers(raw json.RawMessage) ([]User, error) {
	rows, _, err := ParseList(raw)
	if err != nil {
		return nil, err
	}
	out := make([]User, 0, len(rows))
	for i, row := range rows {
		var u User
		// Reported, never skipped. Skipping a record that will not decode
		// turns a schema mismatch into a smaller list, and a smaller list into
		// a confident wrong answer: a mistyped Groups field once made this
		// return zero users and the caller conclude nobody was enrolled.
		if err := json.Unmarshal(row, &u); err != nil {
			return nil, fmt.Errorf("Password Manager user %d of %d did not decode: %w — "+
				"the record shape has changed and jc would otherwise report fewer users than exist",
				i+1, len(rows), err)
		}
		out = append(out, u)
	}
	return out, nil
}

// ParseGroups decodes the group list.
func ParseGroups(raw json.RawMessage) ([]Group, error) {
	rows, _, err := ParseList(raw)
	if err != nil {
		return nil, err
	}
	out := make([]Group, 0, len(rows))
	for i, row := range rows {
		var g Group
		if err := json.Unmarshal(row, &g); err != nil {
			return nil, fmt.Errorf("Password Manager group %d of %d did not decode: %w",
				i+1, len(rows), err)
		}
		out = append(out, g)
	}
	return out, nil
}

// FindUserByExternalID matches a Password Manager user to a JumpCloud user id.
//
// This is the bridge, and it is the only one: PWM records carry no username the
// directory recognises as a key, so resolving "the user called juergen" means
// resolving that name to a JumpCloud id first and matching it here.
func FindUserByExternalID(users []User, jcUserID string) (User, bool) {
	for _, u := range users {
		if u.ExternalID == jcUserID {
			return u, true
		}
	}
	return User{}, false
}

// FindUser resolves an identifier a person actually typed: a PWM UUID, or a
// name, username or email as it appears in Password Manager.
//
// The directory bridge is deliberately NOT done here — it needs an API client,
// and this package stays free of one so the three surfaces share the matching
// rules rather than reimplementing them.
func FindUser(users []User, identifier string) (User, error) {
	if IsID(identifier) {
		for _, u := range users {
			if u.ID == identifier {
				return u, nil
			}
		}
		return User{}, ErrNoMatch
	}
	want := strings.ToLower(identifier)

	// Username and email are checked before display name, and that
	// precedence is deliberate rather than ambiguity: an exact username
	// match SHOULD beat someone whose display name happens to collide.
	// Ambiguity is only ambiguity within one tier.
	if u, err := oneOf(users, "user", identifier, func(u User) bool {
		return strings.ToLower(u.Username) == want || strings.ToLower(u.Email) == want
	}, func(u User) (string, string) { return u.Username, u.ID }); err != ErrNoMatch {
		return u, err
	}
	return oneOf(users, "user", identifier, func(u User) bool {
		return strings.ToLower(u.Name) == want
	}, func(u User) (string, string) { return u.Name, u.ID })
}

func FindGroup(groups []Group, identifier string) (Group, error) {
	if IsID(identifier) {
		for _, g := range groups {
			if g.ID == identifier {
				return g, nil
			}
		}
		return Group{}, ErrNoMatch
	}
	want := strings.ToLower(identifier)
	return oneOf(groups, "group", identifier, func(g Group) bool {
		return strings.ToLower(g.Name) == want
	}, func(g Group) (string, string) { return g.Name, g.ID })
}

// Folder is a Password Manager shared folder as the LIST returns it.
//
// The id arrives as `uuid` here and as `folderId` from a create, so both tags
// are accepted rather than making every caller know which endpoint produced
// the record.
type Folder struct {
	ID          string `json:"uuid"`
	FolderID    string `json:"folderId,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ItemsCount  int    `json:"itemsInFolder,omitempty"`
	UsersCount  int    `json:"usersWithAccess,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
}

// Identity returns the folder's id whichever key carried it.
func (f Folder) Identity() string {
	if f.ID != "" {
		return f.ID
	}
	return f.FolderID
}

// ParseFolders decodes the shared-folder list.
func ParseFolders(raw json.RawMessage) ([]Folder, error) {
	rows, _, err := ParseList(raw)
	if err != nil {
		return nil, err
	}
	out := make([]Folder, 0, len(rows))
	for i, row := range rows {
		var f Folder
		if err := json.Unmarshal(row, &f); err != nil {
			return nil, fmt.Errorf("Password Manager folder %d of %d did not decode: %w",
				i+1, len(rows), err)
		}
		out = append(out, f)
	}
	return out, nil
}

// FindFolder resolves a shared folder by id or name.
func FindFolder(folders []Folder, identifier string) (Folder, error) {
	if IsID(identifier) {
		for _, f := range folders {
			if f.Identity() == identifier {
				return f, nil
			}
		}
		return Folder{}, ErrNoMatch
	}
	want := strings.ToLower(identifier)
	return oneOf(folders, "shared folder", identifier, func(f Folder) bool {
		return strings.ToLower(f.Name) == want
	}, func(f Folder) (string, string) { return f.Name, f.Identity() })
}

// ─── resolution ────────────────────────────────────────────────────

// Fetcher reads one Password Manager endpoint. Both surfaces already have
// a client; neither needs to hand this package one.
type Fetcher func(ctx context.Context, endpoint string) (json.RawMessage, error)

// DirectoryResolver turns a directory name into a JumpCloud user ID. It is
// a parameter rather than an import because users live on V1 and this
// package should not care which client the caller holds.
type DirectoryResolver func(ctx context.Context, name string) (string, error)

// IsDirectoryID reports whether s looks like a JumpCloud 24-character hex
// object ID — the format the rest of the directory uses, and the format
// externalId holds. Password Manager's own IDs are UUIDs; see IsID.
func IsDirectoryID(s string) bool {
	if len(s) != 24 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

// ResolveUser turns what an operator typed into a Password Manager user.
//
// Three routes, in the order that costs least: a name matching a Password
// Manager record is used as-is; a directory ID is matched through
// externalId; otherwise the name is resolved against the DIRECTORY and
// then matched here through externalId, which is the only link between
// the two systems.
//
// This lives in the contract package rather than in either surface
// because the bridge is the part most likely to drift: the CLI and the
// MCP server must agree on what "ada" means, or the same question gets
// two answers depending on who asks.
func ResolveUser(ctx context.Context, get Fetcher, resolveDirectory DirectoryResolver, identifier string) (User, error) {
	raw, err := get(ctx, UsersEndpoint)
	if err != nil {
		return User{}, err
	}
	users, err := ParseUsers(raw)
	if err != nil {
		return User{}, err
	}

	// An ambiguous name stops here rather than falling through to the
	// directory. Guessing which of two matching records was meant is the
	// failure this reports; trying a different lookup would only hide it.
	switch u, ferr := FindUser(users, identifier); {
	case ferr == nil:
		return u, nil
	case !errors.Is(ferr, ErrNoMatch):
		return User{}, ferr
	}

	jcID := identifier
	if !IsDirectoryID(identifier) {
		if resolveDirectory == nil {
			return User{}, fmt.Errorf("%q matches no Password Manager user (%s)",
				identifier, enrolledCount(len(users)))
		}
		jcID, err = resolveDirectory(ctx, identifier)
		if err != nil {
			return User{}, fmt.Errorf("%q matches no Password Manager user, and no JumpCloud "+
				"user either: %w", identifier, err)
		}
	}
	if u, ok := FindUserByExternalID(users, jcID); ok {
		return u, nil
	}
	return User{}, fmt.Errorf("%q is a JumpCloud user but is not enrolled in Password Manager (%s)",
		identifier, enrolledCount(len(users)))
}

// ResolveFolder turns a shared-folder name or ID into an ID.
func ResolveFolder(ctx context.Context, get Fetcher, identifier string) (string, error) {
	if IsID(identifier) {
		return identifier, nil
	}
	raw, err := get(ctx, SharedFoldersEndpoint)
	if err != nil {
		return "", err
	}
	folders, err := ParseFolders(raw)
	if err != nil {
		return "", err
	}
	f, ferr := FindFolder(folders, identifier)
	if errors.Is(ferr, ErrNoMatch) {
		return "", fmt.Errorf("no shared folder %q (%d exist)", identifier, len(folders))
	}
	if ferr != nil {
		return "", ferr
	}
	return f.Identity(), nil
}

// enrolledCount phrases the enrolment count so the error reads as a
// sentence. "1 users are" undercuts an otherwise precise message.
func enrolledCount(n int) string {
	switch n {
	case 0:
		return "nobody is enrolled"
	case 1:
		return "1 person is enrolled"
	default:
		return fmt.Sprintf("%d people are enrolled", n)
	}
}

// ─── ambiguity ─────────────────────────────────────────────────────

// ErrNoMatch means nothing in the list matched. It is a normal outcome,
// not a fault: ResolveUser treats it as "try the directory next".
var ErrNoMatch = errors.New("no match")

// AmbiguousError means a name matched more than one record.
//
// It exists because "who can see this folder?" is an access-review
// question, and answering it about one arbitrary folder of two — with no
// warning — is a confidently wrong answer to a question about who can
// read credentials. Shared folders make this permanent rather than
// transient: the API creates them and cannot delete them, so two folders
// with the same name stay that way.
type AmbiguousError struct {
	Kind       string
	Identifier string
	Candidates []Candidate
}

// Candidate is one record a name matched.
type Candidate struct {
	Name string
	ID   string
}

func (e *AmbiguousError) Error() string {
	lines := make([]string, len(e.Candidates))
	for i, c := range e.Candidates {
		lines[i] = "  " + c.Name + " (" + c.ID + ")"
	}
	return fmt.Sprintf("%s %q is ambiguous — it matches %d records:\n%s\n"+
		"Pass one of the IDs above instead of the name.",
		e.Kind, e.Identifier, len(e.Candidates), strings.Join(lines, "\n"))
}

// oneOf returns the single record matching pred, ErrNoMatch if none do,
// or an *AmbiguousError naming every candidate if several do.
func oneOf[T any](items []T, kind, identifier string, pred func(T) bool, describe func(T) (name, id string)) (T, error) {
	var found []T
	for _, it := range items {
		if pred(it) {
			found = append(found, it)
		}
	}
	var zero T
	switch len(found) {
	case 0:
		return zero, ErrNoMatch
	case 1:
		return found[0], nil
	default:
		cands := make([]Candidate, len(found))
		for i, it := range found {
			n, id := describe(it)
			cands[i] = Candidate{Name: n, ID: id}
		}
		return zero, &AmbiguousError{Kind: kind, Identifier: identifier, Candidates: cands}
	}
}
