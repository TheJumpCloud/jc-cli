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
}

// fieldVocabulary is the declared meaning of keys that more than one emitter
// uses, plus the ones a collision would be dangerous in.
//
// Add an entry when a key is shared, or when getting it wrong would be silent.
// This is deliberately not exhaustive: a table nobody maintains is worse than a
// short one that is true.
var fieldVocabulary = []fieldFact{
	// Identity, shared everywhere.
	{"users_get", "user", "id", "own id"},
	{"users_search", "user", "id", "own id"},
	{"user_view", "user", "user.id", "own id"},
	{"devices_search", "device", "id", "own id"},
	{"device_view", "device", "device.id", "own id"},

	// The collision that prompted this table. users_get keeps `mfa`;
	// user_view uses `mfa_enrollment`, matching the API's own mfaEnrollment.
	{"users_get", "user", "mfa", "mfa configuration state"},
	{"users_search", "user", "mfa", "mfa configuration state"},
	{"user_view", "user", "mfa_enrollment", "mfa enrollment state"},

	// Case-convention split: passthrough tools return the API's camelCase,
	// jc projections return snake_case. Same facts, different spellings —
	// allowed, and recorded so it reads as a decision rather than an
	// accident.
	{"devices_search", "device", "displayName", "display name"},
	{"device_view", "device", "device.display_name", "display name"},
	{"devices_search", "device", "serialNumber", "serial number"},
	{"device_view", "device", "device.serial_number", "serial number"},
	{"devices_search", "device", "lastContact", "last contact time"},
	{"device_view", "device", "device.last_contact", "last contact time"},
	{"devices_search", "device", "agentVersion", "agent version"},
	{"device_view", "device", "device.agent_version", "agent version"},
	{"devices_search", "device", "version", "os version"},
	{"device_view", "device", "device.os_version", "os version"},

	// Group membership, after the resolver fix: both view tools emit the
	// same shape from the same helper.
	{"user_view", "user", "groups", "group memberships with resolved names"},
	{"device_view", "device", "groups", "group memberships with resolved names"},

	// Lint and the template list agree on where a template came from.
	{"workflows_lint", "template", "source", "who authored it"},
	{"workflows_templates_list", "template", "source", "who authored it"},
	{"workflows_lint", "template", "corrected_by", "id of jc's corrected copy"},
	{"workflows_templates_list", "template", "corrected_by", "id of jc's corrected copy"},
	{"workflows_lint", "template", "corrects", "name of the template this replaces"},
	{"workflows_templates_list", "template", "corrects", "name of the template this replaces"},
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
