package mcp

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// The check the mfa collision needed: one key name must not mean two things.
//
// A field-name comparison passes that case — both tools have `mfa` — so this
// asserts on the declared FACT instead. When it fails, the fix is to rename
// one of the keys, not to relax the table.
func TestVocabulary_OneKeyMeansOneThing(t *testing.T) {
	for key, facts := range factsByKey() {
		if len(facts) <= 1 {
			continue
		}
		var lines []string
		for fact, emitters := range facts {
			sort.Strings(emitters)
			lines = append(lines, "  "+strings.Join(emitters, ", ")+" → "+fact)
		}
		sort.Strings(lines)
		t.Errorf("key %q carries %d different facts; a caller reading it cannot know which:\n%s",
			key, len(facts), strings.Join(lines, "\n"))
	}
}

// Two emitters may spell one fact differently — a passthrough returns the
// API's camelCase, a jc projection returns snake_case. That split is allowed.
// This asserts it is DECLARED, so it reads as a decision rather than an
// accident, and so a caller has the mapping.
func TestVocabulary_SharedFactsAreMapped(t *testing.T) {
	byFact := map[string][]string{}
	for _, f := range fieldVocabulary {
		byFact[f.Object+"/"+f.Fact] = append(byFact[f.Object+"/"+f.Fact], f.Emitter+":"+f.Key)
	}

	shared := 0
	for fact, refs := range byFact {
		if len(refs) < 2 {
			continue
		}
		shared++
		// One tool may emit a fact under two keys — _id duplicates id on the
		// V1 passthrough tools, and jc leaves that as the API sends it. What
		// must not happen is UNDECLARED duplication: the second key has to
		// carry a Note saying why it is there, or the table is hiding it.
		seen := map[string]bool{}
		for _, r := range refs {
			parts := strings.SplitN(r, ":", 2)
			emitter, key := parts[0], parts[1]
			if !seen[emitter] {
				seen[emitter] = true
				continue
			}
			if noteFor(emitter, key) == "" {
				t.Errorf("%s emits fact %q under a second key %q with no note explaining why: %v",
					emitter, fact, key, refs)
			}
		}
	}
	if shared == 0 {
		t.Error("no fact is shared between emitters, so this table is not testing anything")
	}
}

// The vocabulary must describe what the code actually emits. A key that was
// renamed without updating the table would leave the table lying, which is
// worse than not having one.
func TestVocabulary_MatchesWhatUserViewEmits(t *testing.T) {
	raw, err := json.Marshal(userViewData{})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}

	for _, f := range fieldVocabulary {
		if f.Emitter != "user_view" {
			continue
		}
		top := strings.SplitN(f.Key, ".", 2)[0]
		if _, ok := m[top]; !ok {
			t.Errorf("vocabulary claims user_view emits %q, but the payload has no %q: %v",
				f.Key, top, keysOfMap(m))
		}
	}

	// And the specific one that was wrong.
	if _, collides := m["mfa"]; collides {
		t.Error("user_view emits `mfa` again — it must be mfa_enrollment")
	}
}

func TestVocabulary_MatchesWhatDeviceViewEmits(t *testing.T) {
	raw, err := json.Marshal(deviceViewData{})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, f := range fieldVocabulary {
		if f.Emitter != "device_view" {
			continue
		}
		top := strings.SplitN(f.Key, ".", 2)[0]
		if _, ok := m[top]; !ok {
			t.Errorf("vocabulary claims device_view emits %q, but the payload has no %q: %v",
				f.Key, top, keysOfMap(m))
		}
	}
}

// noteFor returns the declared encoding note for one emitter's key.
func noteFor(emitter, key string) string {
	for _, f := range fieldVocabulary {
		if f.Emitter == emitter && f.Key == key {
			return f.Note
		}
	}
	return ""
}

// Where two emitters agree on a fact but encode it differently, at least one
// must say so. `admin` means the same thing in users_get and users_search and
// differs only in how a non-admin is represented — a difference that is
// invisible unless it is written down.
func TestVocabulary_EncodingDifferencesAreNoted(t *testing.T) {
	byFact := map[string][]fieldFact{}
	for _, f := range fieldVocabulary {
		k := f.Object + "/" + f.Fact
		byFact[k] = append(byFact[k], f)
	}

	shared := byFact["user/admin role assignment"]
	if len(shared) < 2 {
		t.Fatal("the admin case should be declared by both users tools")
	}
	for _, f := range shared {
		if f.Note == "" {
			t.Errorf("%s declares `admin` with no note; the presence trap is the whole point", f.Emitter)
		}
	}
	// The trap must be spelled out, not merely hinted.
	var mentionsPresence bool
	for _, f := range shared {
		if strings.Contains(f.Note, "key presence") || strings.Contains(f.Note, "in user") {
			mentionsPresence = true
		}
	}
	if !mentionsPresence {
		t.Error("no admin note warns against testing key presence, which is the actual failure mode")
	}
}

func keysOfMap(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// unionKeys and commonKeys describe a whole list response rather than one
// record of it.
//
// The audit found field sets varying BETWEEN records inside a single response:
// one user had no public_key key at all while the next had public_key: null,
// and one device carried domainInfo.domainSid while another did not. That is
// upstream omitempty leaking through. A parity assertion that samples the first
// record therefore passes or fails on which record it happened to draw — flaky,
// not wrong, which is worse because it gets retried away.
//
// So a list is described by two sets: every key that appears anywhere, and
// every key that appears in all of them. Comparing unions is what a caller
// consuming the payload needs; comparing intersections tells them which keys
// they may rely on without a presence check.
func unionKeys(records []map[string]any) []string {
	seen := map[string]bool{}
	for _, r := range records {
		for k := range r {
			seen[k] = true
		}
	}
	return sortedKeys(seen)
}

func commonKeys(records []map[string]any) []string {
	if len(records) == 0 {
		return nil
	}
	counts := map[string]int{}
	for _, r := range records {
		for k := range r {
			counts[k]++
		}
	}
	seen := map[string]bool{}
	for k, n := range counts {
		if n == len(records) {
			seen[k] = true
		}
	}
	return sortedKeys(seen)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// A one-record sample would call these two responses identical, because record
// zero matches. They are not: the second record of each carries a key the
// first does not, which is precisely the shape the audit observed.
func TestVocabulary_ListParityUsesEveryRecord(t *testing.T) {
	toolA := []map[string]any{
		{"id": "1", "username": "a"},
		{"id": "2", "username": "b", "public_key": nil},
	}
	toolB := []map[string]any{
		{"id": "1", "username": "a"},
		{"id": "2", "username": "b"},
	}

	if got, want := len(unionKeys(toolA)), 3; got != want {
		t.Errorf("union of A = %v, want %d keys", unionKeys(toolA), want)
	}
	if diff := len(unionKeys(toolA)) - len(unionKeys(toolB)); diff != 1 {
		t.Errorf("the union must expose the extra key: A=%v B=%v",
			unionKeys(toolA), unionKeys(toolB))
	}

	// The intersection tells a caller what is safe to read unconditionally.
	common := commonKeys(toolA)
	if len(common) != 2 || common[0] != "id" || common[1] != "username" {
		t.Errorf("common keys = %v, want [id username]", common)
	}
	for _, k := range common {
		if k == "public_key" {
			t.Error("public_key is absent from one record and must not be reported as always present")
		}
	}

	// Sampling record zero alone would have found no difference at all —
	// the failure mode this exists to prevent.
	if len(sortedKeys(map[string]bool{"id": true, "username": true})) != len(commonKeys(toolB)) {
		t.Error("fixture assumption broken")
	}
}
