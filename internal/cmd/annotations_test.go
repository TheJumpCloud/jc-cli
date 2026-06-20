package cmd

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestEveryLeafIsClassified is the lint check for KLA-444 — every leaf
// command in the Cobra tree MUST carry a jc:class annotation.
//
// When a new leaf lands without a classifications.go entry, this test
// fails with the missing paths listed in sorted order so the diff to
// apply is mechanical. The error message names the file to edit and
// the four valid class constants, so the contributor doesn't need to
// re-read this test to figure out what to do.
func TestEveryLeafIsClassified(t *testing.T) {
	root := NewRootCmd()
	var missing, invalid []string

	walkLeaves(root, func(c *cobra.Command) {
		class, ok := c.Annotations[AnnotationClass]
		if !ok || class == "" {
			missing = append(missing, c.CommandPath())
			return
		}
		if !classIsValid(class) {
			invalid = append(invalid, c.CommandPath()+" = "+class)
		}
	})

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("the following leaf commands are missing a jc:class annotation. "+
			"Add an entry to commandClass in internal/cmd/classifications.go (one of: %s, %s, %s, %s):\n  %s",
			ClassReadOnly, ClassMutating, ClassDestructive, ClassInternal,
			strings.Join(missing, "\n  "))
	}
	if len(invalid) > 0 {
		sort.Strings(invalid)
		t.Errorf("the following leaf commands have an invalid jc:class value "+
			"(must be one of: %s, %s, %s, %s):\n  %s",
			ClassReadOnly, ClassMutating, ClassDestructive, ClassInternal,
			strings.Join(invalid, "\n  "))
	}
}

// TestClassificationMapHasNoStaleEntries catches the opposite drift:
// a commandClass entry whose path no longer exists in the Cobra tree
// (renamed or removed). Without this guard, stale entries would
// silently rot and quietly disagree with reality.
func TestClassificationMapHasNoStaleEntries(t *testing.T) {
	root := NewRootCmd()
	live := make(map[string]bool, 256)
	walkLeaves(root, func(c *cobra.Command) {
		live[c.CommandPath()] = true
	})
	var stale []string
	for path := range commandClass {
		if !live[path] {
			stale = append(stale, path)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("commandClass contains paths that no longer exist as Cobra leaves "+
			"(remove from internal/cmd/classifications.go):\n  %s",
			strings.Join(stale, "\n  "))
	}
}

// TestClassificationMapValuesAreValid is a guard for the map itself —
// regardless of whether the path is live, every declared class must
// be one of the four constants. Catches typos at lint time.
func TestClassificationMapValuesAreValid(t *testing.T) {
	for path, class := range commandClass {
		if !classIsValid(class) {
			t.Errorf("commandClass[%q] = %q is not a valid class", path, class)
		}
	}
}

// TestApplyClassificationsAttachesAnnotation confirms the wiring from
// commandClass → cobra.Command.Annotations actually fires. A canary
// against a future refactor that builds the root tree without calling
// applyClassifications.
func TestApplyClassificationsAttachesAnnotation(t *testing.T) {
	root := NewRootCmd()
	var found bool
	walkLeaves(root, func(c *cobra.Command) {
		if c.CommandPath() == "jc users delete" {
			class := c.Annotations[AnnotationClass]
			if class != ClassDestructive {
				t.Errorf("jc users delete annotation = %q, want %q", class, ClassDestructive)
			}
			found = true
		}
	})
	if !found {
		t.Error("could not find 'jc users delete' leaf; the test fixture or command tree is stale")
	}
}
