package privaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Writes. Everything below was established by probing the test org on
// 2026-11-18 and 2026-11-19.
//
// AN UPDATE IS A FULL REPLACE, AND ONE MISSING KEY LOCKS YOU OUT. PUT
// /servers with {server:{id, ...}} upserts by id. A partial body is a 500 and
// changes nothing; a full one replaces the record, clearing every key it
// leaves out. Leaving out accessPolicies answers 200 and wipes every
// permission on the object. From that moment GET, PUT, DELETE and archive all
// answer 400 "you need permissions Manage", the object disappears from the
// listing, and nothing in the API can bring it back. One server on the probe
// tenant was stranded that way.
//
// So an update here is always: read the edit view, change named keys on it,
// check that the access policy about to be sent is the one that was read and
// still grants Manage, send the whole object, then read it back and confirm
// the change landed and the policy did not move. The edit view is read raw —
// a database's connection string has to go back unchanged, and sending the
// redacted placeholder would overwrite the real one.
//
// Creates grant the caller Manage, View Detail and Connect, the only grant
// that cannot strand the object.
//
// Other facts:
//   - Websites are written with POST /websites, which also upserts by id.
//     Servers, databases, credentials and blocking rules use PUT.
//   - archive, unarchive and clones must carry a JSON body; with none the API
//     answers 400 `"null" is not valid JSON`. {} is accepted.
//   - Bulk archive (servers, databases, credentials) takes {"ids": [...]}.
//   - Archived objects leave the default listing. Servers and databases show
//     them under archivedFilter=true; websites and credentials under
//     filter=archived:eq:true.
//   - A clone is named "<name> - Copy" and carries the original's notes and
//     access policy.
//
// Not exposed, and why:
//   - Credential create and update. A credential's edit view carries no
//     secret fields, so a full-replace update built from it may clear the
//     secret, and checking would mean reading the secret back. Create needs a
//     secret on input; Password Vault's `credentials create` already does
//     that for the same backend.
//   - Blocking-rule create. It is not atomic (a 400 still saves the rule), the
//     criteria's numeric "type" has no documented meaning, and server rules
//     are refused outright. Update and delete of existing rules are exposed.
//   - Connectors, resource managers, group resources, rotation, backup key,
//     Excel import and export: not probed, or secret-bearing.

// Writer is the part of the v2 API client the write side needs.
type Writer interface {
	Getter
	Create(ctx context.Context, endpoint string, body any) (json.RawMessage, error)
	Update(ctx context.Context, endpoint string, body any) (json.RawMessage, error)
	Delete(ctx context.Context, endpoint string) (json.RawMessage, error)
}

// FullPermissions is the grant every create gives its caller.
var FullPermissions = []string{"Manage", "View Detail", "Connect"}

// LockoutWarning is shown on every write that sends an access policy.
const LockoutWarning = "an update replaces the whole record, and a record sent without an " +
	"access policy that grants Manage loses every permission and cannot afterwards be read, " +
	"changed or deleted through the API — so jc sends back the policy it read, unchanged, and " +
	"refuses if that policy grants Manage to nobody"

// SelfEndpoint is the caller's own PAM user record.
const SelfEndpoint = UsersEndpoint + "/self"

// SelfID returns the caller's integer PAM user id, which an access policy
// names. It is not the JumpCloud user object id.
func SelfID(ctx context.Context, c Getter) (int, error) {
	raw, err := c.Get(ctx, SelfEndpoint)
	if err != nil {
		return 0, err
	}
	var self struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(raw, &self); err != nil {
		return 0, fmt.Errorf("decoding your Privileged Access user record: %w", err)
	}
	if self.ID <= 0 {
		return 0, fmt.Errorf("your Privileged Access user record carries no id; " +
			"an access policy cannot be built without it")
	}
	return self.ID, nil
}

// OwnerPolicy is the access policy a create sends: the caller, with every
// permission.
func OwnerPolicy(userID int) map[string]any {
	return map[string]any{
		"isPrivate": false,
		"users":     []map[string]any{{"id": userID, "permissions": FullPermissions}},
		"groups":    []any{},
	}
}

// CheckPolicy refuses an access policy that would strand the object: absent,
// not an object, or granting Manage to no user and no group.
func CheckPolicy(policy any) error {
	m, ok := policy.(map[string]any)
	if !ok {
		return fmt.Errorf("refusing to write: the record has no access policy, and sending it " +
			"without one would remove every permission on it irrecoverably")
	}
	for _, key := range []string{"users", "groups"} {
		list, _ := m[key].([]any)
		for _, e := range list {
			em, _ := e.(map[string]any)
			perms, _ := em["permissions"].([]any)
			for _, p := range perms {
				if p == "Manage" {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("refusing to write: the record's access policy grants Manage to nobody, " +
		"so after this write nobody could change or delete it through the API")
}

// --- Kinds ------------------------------------------------------------------

// wrapKey is the key a kind's record sits under in its edit view and in a
// write body.
func (k Kind) wrapKey() string { return k.Name }

// postWrites reports how a kind is written: websites are POSTed, the rest PUT.
func (k Kind) postWrites() bool { return k.Endpoint == WebsitesEndpoint }

// hasBulkArchive reports whether a kind has bulk-archive endpoints.
func (k Kind) hasBulkArchive() bool { return k.Paged }

// KindByName returns server, database or website.
func KindByName(name string) (Kind, error) {
	switch strings.ToLower(strings.TrimSuffix(name, "s")) {
	case "server":
		return Servers, nil
	case "database", "db":
		return Databases, nil
	case "website", "site":
		return Websites, nil
	}
	return Kind{}, fmt.Errorf("unknown Privileged Access resource kind %q: expected server, database or website", name)
}

// archivedEndpoint is the listing of a kind's archived objects.
func archivedEndpoint(endpoint string, paged bool) string {
	if paged {
		return endpoint + "?archivedFilter=true"
	}
	return endpoint + "?filter=" + url.QueryEscape("archived:eq:true")
}

// ListArchived returns the archived objects of a kind.
func ListArchived(ctx context.Context, c Getter, k Kind) ([]json.RawMessage, int, error) {
	if k.Paged {
		return ListPaged(ctx, c, archivedEndpoint(k.Endpoint, true), "archived "+k.Name+"s")
	}
	return ListResults(ctx, c, archivedEndpoint(k.Endpoint, false), "archived "+k.Name+"s")
}

// resolve finds one object by id or name in the active listing, or in the
// archived one when archived is true.
func resolve(ctx context.Context, c Getter, k Kind, ident string, archived bool) (string, error) {
	var rows []json.RawMessage
	var err error
	if archived {
		rows, _, err = ListArchived(ctx, c, k)
	} else {
		rows, _, err = List(ctx, c, k)
	}
	if err != nil {
		return "", err
	}
	row, err := Find(rows, k.Name, ident)
	if err != nil {
		if archived {
			return "", fmt.Errorf("%w among archived %ss", err, k.Name)
		}
		return "", err
	}
	return idOf(row), nil
}

// resolveAny finds one object whether or not it is archived.
func resolveAny(ctx context.Context, c Getter, k Kind, ident string) (string, error) {
	id, err := resolve(ctx, c, k, ident, false)
	if err == nil || strings.Contains(err.Error(), "use an id") {
		return id, err
	}
	if aid, aerr := resolve(ctx, c, k, ident, true); aerr == nil {
		return aid, nil
	}
	return "", err
}

func present(ctx context.Context, c Getter, k Kind, id string, archived bool) (bool, error) {
	var rows []json.RawMessage
	var err error
	if archived {
		rows, _, err = ListArchived(ctx, c, k)
	} else {
		rows, _, err = List(ctx, c, k)
	}
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		if strings.EqualFold(idOf(r), id) {
			return true, nil
		}
	}
	return false, nil
}

func createdID(raw json.RawMessage, what string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.ID == "" {
		return "", fmt.Errorf("the %s write returned no id, so it cannot be confirmed — "+
			"check the listing before retrying", what)
	}
	return out.ID, nil
}

// --- Create -----------------------------------------------------------------

// ResourceSpec is what a create sends. Fields that do not apply to a kind
// must be empty.
type ResourceSpec struct {
	Name  string
	URI   string
	Notes string
	Tags  string
	// Server
	OS       string // linux or windows
	Protocol string // ssh, rdp, ...
	// Server and database
	Port int
	// Database
	Provider     string
	DatabaseName string
}

// CreateBody builds a create body for a kind, with the caller as owner.
func CreateBody(k Kind, s ResourceSpec, selfID int) (map[string]any, error) {
	if strings.TrimSpace(s.Name) == "" {
		return nil, fmt.Errorf("a name is required")
	}
	if strings.TrimSpace(s.URI) == "" {
		return nil, fmt.Errorf("a uri (host, address or URL) is required")
	}
	rec := map[string]any{"name": s.Name, "uri": s.URI, "accessPolicies": OwnerPolicy(selfID)}
	if s.Notes != "" {
		rec["notes"] = s.Notes
	}
	if s.Tags != "" {
		rec["tags"] = s.Tags
	}
	switch k.Endpoint {
	case ServersEndpoint:
		if s.Provider != "" || s.DatabaseName != "" {
			return nil, fmt.Errorf("provider and database name apply to databases, not servers")
		}
		if s.OS == "" || s.Protocol == "" || s.Port <= 0 {
			return nil, fmt.Errorf("a server needs an OS, a protocol and a port")
		}
		rec["os"] = strings.ToLower(s.OS)
		rec["protocol"] = map[string]any{"category": strings.ToLower(s.Protocol), "port": s.Port}
	case DatabasesEndpoint:
		if s.OS != "" || s.Protocol != "" {
			return nil, fmt.Errorf("OS and protocol apply to servers, not databases")
		}
		if s.Provider == "" || s.Port <= 0 {
			return nil, fmt.Errorf("a database needs a provider and a port")
		}
		rec["provider"] = strings.ToLower(s.Provider)
		rec["port"] = s.Port
		if s.DatabaseName != "" {
			rec["databaseName"] = s.DatabaseName
		}
	case WebsitesEndpoint:
		if s.OS != "" || s.Protocol != "" || s.Port != 0 || s.Provider != "" || s.DatabaseName != "" {
			return nil, fmt.Errorf("a website takes only a name, a URL, notes and tags")
		}
	}
	return map[string]any{k.wrapKey(): rec}, nil
}

// CreateResource creates a server, database or website owned by the caller
// and confirms it is listed. A name already in use is refused, so the new
// object can always be addressed by name.
func CreateResource(ctx context.Context, c Writer, k Kind, s ResourceSpec) (string, error) {
	rows, _, err := List(ctx, c, k)
	if err != nil {
		return "", err
	}
	if _, ferr := Find(rows, k.Name, s.Name); ferr == nil || strings.Contains(ferr.Error(), "use an id") {
		return "", fmt.Errorf("a Privileged Access %s named %q already exists", k.Name, s.Name)
	}
	self, err := SelfID(ctx, c)
	if err != nil {
		return "", err
	}
	body, err := CreateBody(k, s, self)
	if err != nil {
		return "", err
	}
	var raw json.RawMessage
	if k.postWrites() {
		raw, err = c.Create(ctx, k.Endpoint, body)
	} else {
		raw, err = c.Update(ctx, k.Endpoint, body)
	}
	if err != nil {
		return "", err
	}
	id, err := createdID(raw, k.Name)
	if err != nil {
		return "", err
	}
	ok, err := present(ctx, c, k, id, false)
	if err != nil {
		return id, err
	}
	if !ok {
		return id, fmt.Errorf("the API returned %s %s, but it is not in the listing afterwards", k.Name, id)
	}
	return id, nil
}

// --- Update -----------------------------------------------------------------

// UpdatableKeys are the record keys an update may change, per kind. Anything
// else — the access policy above all — is sent back exactly as read.
var UpdatableKeys = map[string][]string{
	ServersEndpoint:   {"name", "uri", "notes", "tags", "os"},
	DatabasesEndpoint: {"name", "uri", "notes", "tags", "port", "databaseName"},
	WebsitesEndpoint:  {"name", "uri", "notes", "tags"},
}

// ApplyChanges sets the changed keys on a record read from the edit view and
// checks the access policy it will carry. It changes nothing else.
func ApplyChanges(k Kind, rec map[string]any, changes map[string]any) error {
	if len(changes) == 0 {
		return fmt.Errorf("nothing to change")
	}
	allowed := map[string]bool{}
	for _, key := range UpdatableKeys[k.Endpoint] {
		allowed[key] = true
	}
	for key, v := range changes {
		if !allowed[key] {
			keys := append([]string(nil), UpdatableKeys[k.Endpoint]...)
			sort.Strings(keys)
			return fmt.Errorf("%q cannot be changed on a %s here (changeable: %s)",
				key, k.Name, strings.Join(keys, ", "))
		}
		if key == "name" && strings.TrimSpace(fmt.Sprint(v)) == "" {
			return fmt.Errorf("the name cannot be empty")
		}
		rec[key] = v
	}
	return CheckPolicy(rec["accessPolicies"])
}

func editRecord(ctx context.Context, c Getter, endpoint, key string) (map[string]any, map[string]any, error) {
	raw, err := c.Get(ctx, endpoint)
	if err != nil {
		return nil, nil, err
	}
	var view map[string]any
	if err := json.Unmarshal(raw, &view); err != nil {
		return nil, nil, fmt.Errorf("decoding the edit view: %w", err)
	}
	rec, ok := view[key].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("the edit view carries no %q record, so nothing can be "+
			"sent back safely", key)
	}
	return view, rec, nil
}

// valueEqual compares a value read back from JSON (float64) with one set by
// the caller (int or string).
func valueEqual(got, want any) bool {
	if reflect.DeepEqual(got, want) {
		return true
	}
	return fmt.Sprint(got) == fmt.Sprint(want)
}

// UpdateResource changes named keys on a server, database or website and
// returns the redacted edit view read back afterwards.
func UpdateResource(ctx context.Context, c Writer, k Kind, ident string, changes map[string]any) (json.RawMessage, error) {
	id, err := resolveAny(ctx, c, k, ident)
	if err != nil {
		return nil, err
	}
	edit := k.Endpoint + "/" + id + "/edit"
	_, rec, err := editRecord(ctx, c, edit, k.wrapKey())
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(fmt.Sprint(rec["id"]), id) {
		return nil, fmt.Errorf("the edit view for %s %s describes a different record; not writing", k.Name, id)
	}
	before := rec["accessPolicies"]
	if err := ApplyChanges(k, rec, changes); err != nil {
		return nil, err
	}
	body := map[string]any{k.wrapKey(): rec}
	if k.postWrites() {
		_, err = c.Create(ctx, k.Endpoint, body)
	} else {
		_, err = c.Update(ctx, k.Endpoint, body)
	}
	if err != nil {
		return nil, err
	}
	view, after, err := editRecord(ctx, c, edit, k.wrapKey())
	if err != nil {
		return nil, fmt.Errorf("the update was sent but could not be read back: %w", err)
	}
	for key, want := range changes {
		if !valueEqual(after[key], want) {
			return nil, fmt.Errorf("the API accepted the update, but %s on %s %s reads back as %v",
				key, k.Name, id, after[key])
		}
	}
	if !reflect.DeepEqual(before, after["accessPolicies"]) {
		return nil, fmt.Errorf("the update changed the access policy on %s %s, which it must not — "+
			"check it in the console", k.Name, id)
	}
	b, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	return Redact(b)
}

// --- Delete, clone ----------------------------------------------------------

// DeleteResource deletes a server, database or website and
// confirms it is gone from both listings.
//
// An archived SERVER cannot be deleted: the API answers 500 and keeps it
// (archived databases and websites delete normally). That is refused here
// with the fix, rather than surfacing as a retried server error.
func DeleteResource(ctx context.Context, c Writer, k Kind, ident string) (string, error) {
	id, err := resolve(ctx, c, k, ident, false)
	if err != nil && !strings.Contains(err.Error(), "use an id") {
		aid, aerr := resolve(ctx, c, k, ident, true)
		if aerr != nil {
			return "", err
		}
		if k.Endpoint == ServersEndpoint {
			return "", fmt.Errorf("server %s is archived, and the API cannot delete an archived "+
				"server (it answers 500 and keeps it): unarchive it first", aid)
		}
		id, err = aid, nil
	}
	if err != nil {
		return "", err
	}
	if _, err := c.Delete(ctx, k.Endpoint+"/"+id); err != nil {
		return id, err
	}
	for _, archived := range []bool{false, true} {
		ok, err := present(ctx, c, k, id, archived)
		if err != nil {
			return id, err
		}
		if ok {
			return id, fmt.Errorf("the API accepted the delete, but %s %s is still listed", k.Name, id)
		}
	}
	return id, nil
}

// CloneResource duplicates a server, database or website. The copy is named
// "<name> - Copy" and carries the original's access policy.
func CloneResource(ctx context.Context, c Writer, k Kind, ident string) (string, error) {
	id, err := resolve(ctx, c, k, ident, false)
	if err != nil {
		return "", err
	}
	raw, err := c.Create(ctx, k.Endpoint+"/"+id+"/clones", map[string]any{})
	if err != nil {
		return "", err
	}
	newID, err := createdID(raw, k.Name+" clone")
	if err != nil {
		return "", err
	}
	ok, err := present(ctx, c, k, newID, false)
	if err != nil {
		return newID, err
	}
	if !ok {
		return newID, fmt.Errorf("the API returned clone %s, but it is not in the listing afterwards", newID)
	}
	return newID, nil
}

// --- Archive ----------------------------------------------------------------

// IDsBody is the bulk body. An empty list is refused: the API would accept it
// and do nothing.
func IDsBody(ids []string) (map[string][]string, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("no ids given")
	}
	for _, id := range ids {
		if !IsObjectID(id) {
			return nil, errBadID("object", id)
		}
	}
	return map[string][]string{"ids": ids}, nil
}

// SetArchived archives (or restores) servers, databases or websites by id or
// name, and confirms each one moved between the listings. Several servers or
// databases go in one bulk call; websites have no bulk endpoint.
func SetArchived(ctx context.Context, c Writer, k Kind, idents []string, archive bool) ([]string, error) {
	if len(idents) == 0 {
		return nil, fmt.Errorf("no %ss given", k.Name)
	}
	ids := make([]string, 0, len(idents))
	for _, ident := range idents {
		id, err := resolve(ctx, c, k, ident, !archive)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	verb := "archive"
	if !archive {
		verb = "unarchive"
	}
	if len(ids) > 1 && k.hasBulkArchive() {
		body, err := IDsBody(ids)
		if err != nil {
			return nil, err
		}
		if _, err := c.Create(ctx, k.Endpoint+"/bulk-"+verb, body); err != nil {
			return nil, err
		}
	} else {
		for _, id := range ids {
			if _, err := c.Create(ctx, k.Endpoint+"/"+id+"/"+verb, map[string]any{}); err != nil {
				return nil, fmt.Errorf("%s %s: %w", verb, id, err)
			}
		}
	}
	return ids, confirmArchived(ctx, c, k.Name, ids, archive, func(ctx context.Context) ([]json.RawMessage, error) {
		rows, _, err := ListArchived(ctx, c, k)
		return rows, err
	})
}

func confirmArchived(ctx context.Context, c Getter, what string, ids []string, archive bool,
	archived func(context.Context) ([]json.RawMessage, error)) error {
	rows, err := archived(ctx)
	if err != nil {
		return err
	}
	in := map[string]bool{}
	for _, r := range rows {
		in[strings.ToLower(idOf(r))] = true
	}
	for _, id := range ids {
		if in[strings.ToLower(id)] != archive {
			state := "not archived"
			if !archive {
				state = "still archived"
			}
			return fmt.Errorf("the API accepted the request, but %s %s is %s afterwards", what, id, state)
		}
	}
	return nil
}

// --- Credentials --------------------------------------------------------------

func listArchivedCredentials(ctx context.Context, c Getter) ([]json.RawMessage, error) {
	rows, _, err := ListResults(ctx, c, archivedEndpoint(CredentialsEndpoint, false), "archived credentials")
	return rows, err
}

func credentialListed(ctx context.Context, c Getter, id string, archived bool) (bool, error) {
	var rows []json.RawMessage
	var err error
	if archived {
		rows, err = listArchivedCredentials(ctx, c)
	} else {
		rows, _, err = ListCredentials(ctx, c)
	}
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		if strings.EqualFold(idOf(r), id) {
			return true, nil
		}
	}
	return false, nil
}

// DeleteCredential deletes a privileged credential, archived or not, and
// confirms it is gone from both listings.
func DeleteCredential(ctx context.Context, c Writer, id string) error {
	if !IsObjectID(id) {
		return errBadID("credential", id)
	}
	active, err := credentialListed(ctx, c, id, false)
	if err != nil {
		return err
	}
	archived := false
	if !active {
		if archived, err = credentialListed(ctx, c, id, true); err != nil {
			return err
		}
	}
	if !active && !archived {
		return fmt.Errorf("no privileged credential %s (active or archived)", id)
	}
	if _, err := c.Delete(ctx, CredentialsEndpoint+"/"+id); err != nil {
		return err
	}
	for _, a := range []bool{false, true} {
		ok, err := credentialListed(ctx, c, id, a)
		if err != nil {
			return err
		}
		if ok {
			return fmt.Errorf("the API accepted the delete, but credential %s is still listed", id)
		}
	}
	return nil
}

// SetCredentialsArchived archives or restores privileged credentials by id.
func SetCredentialsArchived(ctx context.Context, c Writer, ids []string, archive bool) error {
	body, err := IDsBody(ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		ok, err := credentialListed(ctx, c, id, !archive)
		if err != nil {
			return err
		}
		if !ok {
			where := "among active privileged credentials"
			if !archive {
				where = "among archived privileged credentials"
			}
			return fmt.Errorf("no credential %s %s", id, where)
		}
	}
	verb := "archive"
	if !archive {
		verb = "unarchive"
	}
	if _, err := c.Create(ctx, CredentialsEndpoint+"/"+verb, body); err != nil {
		return err
	}
	return confirmArchived(ctx, c, "credential", ids, archive, func(ctx context.Context) ([]json.RawMessage, error) {
		return listArchivedCredentials(ctx, c)
	})
}

// CloneCredential duplicates a privileged credential, secret included.
func CloneCredential(ctx context.Context, c Writer, id string) (string, error) {
	if !IsObjectID(id) {
		return "", errBadID("credential", id)
	}
	ok, err := credentialListed(ctx, c, id, false)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("no active privileged credential %s", id)
	}
	raw, err := c.Create(ctx, CredentialsEndpoint+"/"+id+"/clones", map[string]any{})
	if err != nil {
		return "", err
	}
	newID, err := createdID(raw, "credential clone")
	if err != nil {
		return "", err
	}
	if ok, err := credentialListed(ctx, c, newID, false); err != nil {
		return newID, err
	} else if !ok {
		return newID, fmt.Errorf("the API returned clone %s, but it is not in the listing afterwards", newID)
	}
	return newID, nil
}

// --- Blocking rules -----------------------------------------------------------

// RuleUpdatableKeys are the blocking-rule keys an update may change. The
// criteria and the resources the rule applies to are not among them.
var RuleUpdatableKeys = []string{"name", "notes", "tags", "priority"}

func ruleEdit(ctx context.Context, c Getter, ident string) (string, string, error) {
	rows, _, err := ListBlockingRules(ctx, c)
	if err != nil {
		return "", "", err
	}
	row, err := Find(rows, "blocking rule", ident)
	if err != nil {
		return "", "", err
	}
	var v struct {
		CriteriaType string `json:"criteriaType"`
	}
	_ = json.Unmarshal(row, &v)
	short := strings.TrimPrefix(v.CriteriaType, criteriaPrefix)
	if short == "" {
		return "", "", fmt.Errorf("blocking rule %s has no criteria type in the listing", idOf(row))
	}
	id := idOf(row)
	return id, BlockingRulesEndpoint + "/" + id + "/edit?criteriaType=" + url.QueryEscape(short), nil
}

// UpdateBlockingRule changes named keys on a blocking rule and returns the
// redacted edit view read back afterwards.
func UpdateBlockingRule(ctx context.Context, c Writer, ident string, changes map[string]any) (json.RawMessage, error) {
	if len(changes) == 0 {
		return nil, fmt.Errorf("nothing to change")
	}
	allowed := map[string]bool{}
	for _, k := range RuleUpdatableKeys {
		allowed[k] = true
	}
	for k := range changes {
		if !allowed[k] {
			return nil, fmt.Errorf("%q cannot be changed on a blocking rule here (changeable: %s)",
				k, strings.Join(RuleUpdatableKeys, ", "))
		}
	}
	if p, ok := changes["priority"]; ok {
		n, err := strconv.Atoi(fmt.Sprint(p))
		if err != nil || n < 1 {
			return nil, fmt.Errorf("priority must be a positive whole number")
		}
		changes["priority"] = n
	}
	id, edit, err := ruleEdit(ctx, c, ident)
	if err != nil {
		return nil, err
	}
	_, rec, err := editRecord(ctx, c, edit, "blockingRule")
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(fmt.Sprint(rec["id"]), id) {
		return nil, fmt.Errorf("the edit view for blocking rule %s describes a different record; not writing", id)
	}
	before := rec["accessPolicies"]
	for k, v := range changes {
		if k == "name" && strings.TrimSpace(fmt.Sprint(v)) == "" {
			return nil, fmt.Errorf("the name cannot be empty")
		}
		rec[k] = v
	}
	if err := CheckPolicy(rec["accessPolicies"]); err != nil {
		return nil, err
	}
	if _, err := c.Update(ctx, BlockingRulesEndpoint, map[string]any{"blockingRule": rec}); err != nil {
		return nil, err
	}
	view, after, err := editRecord(ctx, c, edit, "blockingRule")
	if err != nil {
		return nil, fmt.Errorf("the update was sent but could not be read back: %w", err)
	}
	for k, want := range changes {
		if !valueEqual(after[k], want) {
			return nil, fmt.Errorf("the API accepted the update, but %s on blocking rule %s reads back as %v",
				k, id, after[k])
		}
	}
	if !reflect.DeepEqual(before, after["accessPolicies"]) {
		return nil, fmt.Errorf("the update changed the access policy on blocking rule %s — check it in the console", id)
	}
	b, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	return Redact(b)
}

// DeleteBlockingRule deletes a blocking rule and confirms it is gone.
func DeleteBlockingRule(ctx context.Context, c Writer, ident string) (string, error) {
	rows, _, err := ListBlockingRules(ctx, c)
	if err != nil {
		return "", err
	}
	row, err := Find(rows, "blocking rule", ident)
	if err != nil {
		return "", err
	}
	id := idOf(row)
	if _, err := c.Delete(ctx, BlockingRulesEndpoint+"/"+id); err != nil {
		return id, err
	}
	rows, _, err = ListBlockingRules(ctx, c)
	if err != nil {
		return id, err
	}
	for _, r := range rows {
		if strings.EqualFold(idOf(r), id) {
			return id, fmt.Errorf("the API accepted the delete, but blocking rule %s is still listed", id)
		}
	}
	return id, nil
}
