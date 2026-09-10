package workflow

import "testing"

// SuggestEventType exists to catch TYPOS. A live pass found it offering
// three "corrections" for feature_settings_change — syncro_settings_update,
// autotask_settings_update, association_change — at Levenshtein distances
// 11, 12 and 13. None is a typo of it; it is an event type the catalog
// omits, which is the opposite problem.
//
// That is worse than unhelpful. Acting on such a suggestion turns a correct
// event type into a wrong one, and a workflow triggering on a wrong-but-
// plausible type is the silent-never-fires failure this whole area exists
// to catch.
func TestSuggestEventType_DoesNotOfferDistantMatchesAsCorrections(t *testing.T) {
	got := SuggestEventType("feature_settings_change", 3)
	if len(got) != 0 {
		t.Errorf("a catalog omission was offered %d corrections (%v); the relative "+
			"threshold alone scaled with the name and allowed 13 edits on a "+
			"23-character type", len(got), got)
	}
}

// The counterpart, and the reason the threshold is 3 rather than 0: a real
// typo is one or two characters, and must still be caught.
func TestSuggestEventType_StillCatchesRealTypos(t *testing.T) {
	for typo, want := range map[string]string{
		"admin_login_atempt": "admin_login_attempt",
		"user_creat":         "user_create",
	} {
		got := SuggestEventType(typo, 3)
		var found bool
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("SuggestEventType(%q) = %v, want it to include %q — tightening "+
				"the threshold must not stop it doing its actual job", typo, got, want)
		}
	}
}
