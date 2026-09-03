package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

// Password Manager is read-only, and the reason is a property of the API
// rather than caution: a shared folder can be CREATED through the API and
// there is no route to delete one — four path variations all return 404 while
// GET on the same path returns 200. A write here cannot be undone, so none is
// exposed.
//
// The four cloud-backup key writes are held back for a different reason: they
// manage encryption key material.
func TestPWM_ExposesNoWrites(t *testing.T) {
	var leaves func(c *cobra.Command) []string
	leaves = func(c *cobra.Command) []string {
		if len(c.Commands()) == 0 {
			return []string{c.Name()}
		}
		var out []string
		for _, sub := range c.Commands() {
			out = append(out, leaves(sub)...)
		}
		return out
	}

	found := leaves(newPasswordManagerCmd())
	if len(found) < 10 {
		t.Fatalf("only %d leaves — the group did not build: %v", len(found), found)
	}
	for _, leaf := range found {
		for _, verb := range []string{"create", "update", "delete", "remove", "add", "set", "rename"} {
			if leaf == verb {
				t.Errorf("password-manager exposes a %q leaf; this group is read-only because "+
					"the API has no delete route for a shared folder", verb)
			}
		}
	}
}

// The group's help has to carry the two facts that make this area surprising,
// because a caller who does not know them reads a 500 as an outage.
func TestPWM_HelpNamesTheTwoTraps(t *testing.T) {
	long := newPasswordManagerCmd().Long
	for _, want := range []string{
		"UUID",       // ids are not the usual 24-hex
		"externalId", // the only bridge to the directory
		"500",        // every bad id looks like a server fault
		"READ-ONLY",
	} {
		if !contains(long, want) {
			t.Errorf("the group help should mention %q: it is what stops a typo reading as an outage", want)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
