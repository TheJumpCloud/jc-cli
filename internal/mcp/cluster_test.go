package mcp

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// allToolDescriptions parses every registered tool from every file in this
// package, not just tools.go. The earlier description invariants read one
// file, so ~100 tools defined in module files were never checked by them.
func allToolDescriptions(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var src strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatal(rerr)
		}
		src.Write(b)
	}
	out := map[string]string{}
	for _, pat := range []*regexp.Regexp{
		regexp.MustCompile(`addTypedTool\(s, "(\w+)", "((?:[^"\\]|\\.)*)"`),
		regexp.MustCompile(`s\.addTool\("(\w+)", "((?:[^"\\]|\\.)*)"`),
	} {
		for _, m := range pat.FindAllStringSubmatch(src.String(), -1) {
			out[m[1]] = m[2]
		}
	}
	if len(out) < 200 {
		t.Fatalf("only %d tools parsed — the pattern probably broke", len(out))
	}
	return out
}

// firstSentence is what the opening of a description amounts to for this
// check. The lead sentence is the part that decided the live cases.
func firstSentence(d string) string {
	if i := strings.IndexAny(d, ".?—:"); i >= 0 {
		return d[:i]
	}
	return d
}

// singularPersonMarkers are phrasings that place a description in the
// per-item vocabulary cluster. "each row" and a plural "their" are NOT
// here: both are ordinary list wording and flagging them would make this
// check noisy enough to be switched off.
var singularPersonMarkers = []string{
	"who is using", "one person", "this person", "the person",
	"a single", "one user", "one device", "one group",
}

// A live pass established that ranking turns on which vocabulary cluster a
// description sits in, not on phrase overlap. password_manager_users_list
// opened "Who is using the Password Manager?" — per-person wording on an
// org-wide tool — and lost its own exact phrase to two per-person tools
// while winning a paraphrase it did not contain. Rewritten in aggregate
// vocabulary, and changing nothing else, it took the aggregate query.
//
// So a list tool that opens in per-person wording is competing in the
// wrong cluster, against siblings that belong there and will win.
func TestMCP_ListToolsDoNotOpenInPerItemVocabulary(t *testing.T) {
	for name, desc := range allToolDescriptions(t) {
		if !strings.HasSuffix(name, "_list") {
			continue
		}
		opening := strings.ToLower(firstSentence(desc))
		for _, marker := range singularPersonMarkers {
			if strings.Contains(opening, marker) {
				t.Errorf("%s opens with %q — a list tool written in per-item vocabulary "+
					"competes in the wrong cluster and loses to the per-item sibling that "+
					"belongs there. Open with aggregate wording (how many, all, every, the "+
					"full list) and leave the per-item phrasing to the per-item tool.\n  %s",
					name, marker, firstSentence(desc))
			}
		}
	}
}

// The check has to be able to fail, and on the real wording rather than an
// invented one: this is the exact opening that lost the live query.
func TestMCP_PerItemVocabularyCheckCatchesTheRealCase(t *testing.T) {
	regressed := "Who is using the Password Manager? Lists the named people enrolled in the vault."
	opening := strings.ToLower(firstSentence(regressed))
	var caught bool
	for _, marker := range singularPersonMarkers {
		if strings.Contains(opening, marker) {
			caught = true
		}
	}
	if !caught {
		t.Error("the check would not have caught the wording that actually lost the query")
	}
	// And the replacement must pass, or the check bans the fix as well.
	fixed := "Vault enrolment roster — the full list of people enrolled in Password Manager."
	for _, marker := range singularPersonMarkers {
		if strings.Contains(strings.ToLower(firstSentence(fixed)), marker) {
			t.Errorf("the check rejects the wording that fixed the query (%q)", marker)
		}
	}
}
