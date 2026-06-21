package cmd

import (
	"sort"

	"github.com/spf13/cobra"
)

// Command annotations — KLA-444.
//
// Every leaf Cobra command carries exactly ONE `jc:class` annotation
// declaring its mutation class. The classification is centralized in
// commandClass (classifications.go) and applied to the command tree at
// init time by applyClassifications.
//
// The annotation is a single source of truth that future work can lean
// on without re-deriving it from command names. KLA-444's minimal scope
// (this PR) defines + enforces the annotation; follow-ups will use it
// to drive MCP filtering by capability (`mcp.blocked_tools: ["tag:destructive"]`)
// and lighten the reflection-based destructive-op gate in internal/mcp/.
//
// Why centralize rather than co-locate the annotation with each command:
// 222 leaves spread across ~48 files made co-location a 222-edit PR with
// no behavioral change. A single classification map is easier to review,
// easier to audit ("show me every destructive op in one place"), and the
// lint test (TestEveryLeafIsClassified) catches drift the moment a new
// command lands without a classification entry.

// AnnotationClass is the Cobra annotation key carrying the mutation class.
const AnnotationClass = "jc:class"

// Mutation classes.
const (
	// ClassReadOnly — GETs only. Never writes to the JC API.
	// Safe to expose in a read-only MCP server, safe to run unattended.
	ClassReadOnly = "read-only"

	// ClassMutating — writes to the JC API. Reversible or low-impact.
	// `users create`, `groups update`, `policies create`, etc.
	ClassMutating = "mutating"

	// ClassDestructive — writes that are hard or impossible to reverse.
	// `users delete`, `devices erase`, `devices lock`, `access-requests revoke`.
	// Also commands whose downstream effects can be destructive even if the
	// API call itself isn't — `commands run` runs arbitrary code on devices;
	// `recipe run` executes a multi-step workflow that may include deletes;
	// `bulk users` can delete rows from CSV. Classed by worst-case capability,
	// not per-invocation intent.
	ClassDestructive = "destructive"

	// ClassInternal — never touches the JC API.
	// Local file ops (`audit verify`, `recipe validate`, `recipe import`),
	// credential management (`auth login`), help machinery (`version`,
	// `explain`, `schema commands`), CLI introspection (`mcp tools`,
	// `doctor`, `config view`), interactive launchers (`tui`, `setup`),
	// and LLM-only flows (`ask` — calls the LLM provider, not JC).
	ClassInternal = "internal"
)

// classIsValid reports whether s is one of the four declared mutation
// classes. Used by the lint test and by anyone querying the annotation.
func classIsValid(s string) bool {
	switch s {
	case ClassReadOnly, ClassMutating, ClassDestructive, ClassInternal:
		return true
	}
	return false
}

// applyClassifications walks the Cobra tree under root and stamps each
// leaf command's Annotations map with its declared jc:class. Leaves
// without a classifications.go entry are left unannotated — the lint
// test (TestEveryLeafIsClassified) catches them.
//
// Called once from NewRootCmd after the tree is fully built.
func applyClassifications(root *cobra.Command) {
	walkLeaves(root, func(c *cobra.Command) {
		class, ok := commandClass[c.CommandPath()]
		if !ok {
			return
		}
		if c.Annotations == nil {
			c.Annotations = make(map[string]string)
		}
		c.Annotations[AnnotationClass] = class
	})
}

// walkLeaves invokes fn on every leaf command in the tree rooted at c.
// Skips help and completion (Cobra-generated, no operator semantics).
//
// A leaf is a command with no subcommands. Walking parents would force
// us to declare a class for `jc users` itself, which is meaningless
// (it's a group, not an action).
func walkLeaves(c *cobra.Command, fn func(*cobra.Command)) {
	if c.Name() == "help" || c.Name() == "completion" {
		return
	}
	if !c.HasSubCommands() {
		fn(c)
		return
	}
	for _, sub := range c.Commands() {
		walkLeaves(sub, fn)
	}
}

// listLeafPaths returns every leaf command path under root, sorted.
// Helpful for tests + tools that audit the command tree.
func listLeafPaths(root *cobra.Command) []string {
	var paths []string
	walkLeaves(root, func(c *cobra.Command) {
		paths = append(paths, c.CommandPath())
	})
	sort.Strings(paths)
	return paths
}
