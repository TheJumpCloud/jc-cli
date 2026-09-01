package mcp

// A field vocabulary: what each JSON key MEANS, per emitter.
//
// A drift audit found `mfa` carrying two different facts. users_get returns
// {configured, exclusion}; user_view returned {totp_enabled, status}, with no
// overlapping sub-keys. A caller reading user.mfa.configured against a view
// payload got undefined — falsy — so an MFA check silently reported NOT
// CONFIGURED, on a security-relevant field.
//
// No comparison of field NAMES catches that: both tools have a key called
// `mfa`, so the names agree exactly. Only a statement of what each key means
// makes the collision visible, which is why this table exists rather than a
// tidier reflective check.
//
// It is also the artefact a caller needs. Two jc tools may legitimately spell
// one fact differently — a passthrough returns the API's `displayName`, a jc
// projection returns `display_name` — and that is a defensible split. What is
// never defensible is one spelling meaning two things.

// fieldFact records that an emitter's JSON key carries a particular fact about
// a particular kind of object.
//
// Object scoping is what keeps the collision check honest. `id` on a user and
// `id` on a device are not a collision — they are the same fact about different
// things, and flagging them would train everyone to ignore the check. `mfa`
// meaning two things about the SAME user is the real defect.
type fieldFact struct {
	// Emitter is the tool or payload the key appears in.
	Emitter string
	// Object is what the key describes: "user", "device", "template".
	Object string
	// Key is the JSON key, dotted for nesting.
	Key string
	// Fact is what it means, phrased so two emitters describing the same
	// thing produce the same string.
	Fact string
	// Note records a difference in how the fact is ENCODED, when two
	// emitters agree on the meaning but not the representation.
	//
	// This is deliberately separate from Fact. Folding the encoding into the
	// fact string makes two tools carrying one fact look like a collision,
	// which would fire the check on something that is not a defect and teach
	// everyone to ignore it. The `admin` case is exactly this: both tools
	// mean "admin role assignment", and only absence is spelled differently.
	Note string
}

// fieldVocabulary is the declared meaning of keys that more than one emitter
// uses, plus the ones a collision would be dangerous in.
//
// Add an entry when a key is shared, or when getting it wrong would be silent.
// This is deliberately not exhaustive: a table nobody maintains is worse than a
// short one that is true.
var fieldVocabulary = []fieldFact{
	// Identity, shared everywhere.
	{"users_get", "user", "id", "own id", ""},
	{"users_search", "user", "id", "own id", ""},
	{"user_view", "user", "user.id", "own id", ""},
	{"devices_search", "device", "id", "own id", ""},
	{"device_view", "device", "device.id", "own id", ""},

	// The collision that prompted this table. users_get keeps `mfa`;
	// user_view uses `mfa_enrollment`, matching the API's own mfaEnrollment.
	{"users_get", "user", "mfa", "mfa configuration state", ""},
	{"users_search", "user", "mfa", "mfa configuration state", ""},
	{"user_view", "user", "mfa_enrollment", "mfa enrollment state", ""},

	// Case-convention split: passthrough tools return the API's camelCase,
	// jc projections return snake_case. Same facts, different spellings —
	// allowed, and recorded so it reads as a decision rather than an
	// accident.
	{"devices_search", "device", "displayName", "display name",
		"users spell the same fact displayname, lower-case n — an upstream inconsistency, not jc's"},
	{"device_view", "device", "device.display_name", "display name", ""},
	{"devices_search", "device", "serialNumber", "serial number", ""},
	{"device_view", "device", "device.serial_number", "serial number", ""},
	{"devices_search", "device", "lastContact", "last contact time", ""},
	{"device_view", "device", "device.last_contact", "last contact time", ""},
	{"devices_search", "device", "agentVersion", "agent version", ""},
	{"device_view", "device", "device.agent_version", "agent version", ""},
	{"devices_search", "device", "version", "os version", ""},
	{"device_view", "device", "device.os_version", "os version", ""},

	// Upstream drift jc deliberately does NOT normalise.
	//
	// users_get and users_search are different endpoints with different
	// projections. Normalising would mean either fabricating fields the
	// search endpoint never returned, or an N+1 fetch to fill them — both
	// worse than the drift. The passthrough tools' value is that they are
	// faithful; the fix is to make the trap visible, not to hide it.
	//
	// `admin` is the sharp one: users_get returns an empty object for a
	// non-admin, users_search omits the key. So `"admin" in user` answers
	// differently per tool FOR THE SAME USER, while every shared value
	// agrees — invisible to a value comparison, which is the shape of every
	// bug this table exists for.
	{"users_get", "user", "admin", "admin role assignment",
		"returns an empty object for a non-admin, so the key is always present"},
	{"users_search", "user", "admin", "admin role assignment",
		"OMITS the key for a non-admin, so `\"admin\" in user` answers differently here than in users_get for the same user — read admin.id / admin.roleName, never key presence"},
	{"users_get", "user", "recoveryEmail", "recovery email",
		"users_search omits this field entirely; users_get is authoritative for one user"},

	// _id duplicates id on the V1 passthrough tools. Left alone: removing
	// either is a breaking change bought only for tidiness, and jc rewriting
	// a passthrough payload is a worse precedent than the duplication.
	{"users_get", "user", "_id", "own id", "duplicates id; left as the API sends it"},
	{"users_search", "user", "_id", "own id", "duplicates id; left as the API sends it"},
	{"devices_search", "device", "_id", "own id", "duplicates id; left as the API sends it"},

	// account_locked: one fact, one name, on every emitter.
	//
	// user_view called it `locked` — both snake_case, so the documented
	// camelCase/snake_case split did not explain it. It was a bare rename, and
	// a dangerous one on a security-relevant boolean: a caller who learned
	// account_locked from users_get read undefined here, which is falsy, so a
	// lock check reported NOT LOCKED on a locked account. Renamed rather than
	// aliased, for the reason the user_view.mfa precedent gives — an alias
	// would have preserved the exact bug it was meant to remove.
	{"users_get", "user", "account_locked", "whether the account is locked", ""},
	{"users_search", "user", "account_locked", "whether the account is locked", ""},
	{"user_view", "user", "user.account_locked", "whether the account is locked", ""},

	// Upstream naming differences jc passes through rather than normalising.
	// Recorded so they read as known rather than as accidents.
	{"users_get", "user", "organization", "owning organization id",
		"the groups endpoints call the same fact organizationObjectId; both are the API's own spelling"},

	// Group membership, after the resolver fix: both view tools emit the
	// same shape from the same helper.
	{"user_view", "user", "groups", "group memberships with resolved names", ""},
	{"device_view", "device", "groups", "group memberships with resolved names", ""},

	// Lint and the template list agree on where a template came from.
	{"workflows_lint", "template", "source", "who authored it", ""},
	{"workflows_templates_list", "template", "source", "who authored it", ""},
	{"workflows_lint", "template", "corrected_by", "id of jc's corrected copy", ""},
	{"workflows_templates_list", "template", "corrected_by", "id of jc's corrected copy", ""},
	{"workflows_lint", "template", "corrects", "name of the template this replaces", ""},
	{"workflows_templates_list", "template", "corrects", "name of the template this replaces", ""},
}

// factsByKey groups the vocabulary by (object, key), so a key meaning two
// things about the same object is visible as a key with two facts.
func factsByKey() map[string]map[string][]string {
	out := map[string]map[string][]string{}
	for _, f := range fieldVocabulary {
		scoped := f.Object + "." + f.Key
		if out[scoped] == nil {
			out[scoped] = map[string][]string{}
		}
		out[scoped][f.Fact] = append(out[scoped][f.Fact], f.Emitter)
	}
	return out
}
