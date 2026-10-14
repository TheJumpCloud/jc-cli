package cmd

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/schema"
)

// The public command catalog is the Commands list in
// schema.BuildCommandManifest. cmd/sitegen renders it into
// docs/site/commands.json, llms.txt and llms-full.txt — the last being what
// an agent reads to find out what jc can do.
//
// That list is hand-maintained rather than derived from the Cobra tree, and
// nothing checked it, so a whole command group could ship with commands, MCP
// tools, a schema entry and docs, and still be invisible to everything
// downstream of the catalog. The verify-site gate cannot catch it: a group
// with no entry is perfectly consistent, just absent.
//
// It had drifted. When this test was written, FIFTEEN shipped groups were
// missing — most of the KLA-485 coverage program.
//
// The entries carry hand-written prose that no generator could produce, which
// is why the fix is a lint plus a backfill rather than deriving the list.

// notInCatalog lists top-level command groups that deliberately have no
// CommandEntry, with the reason. Everything else must have one.
var notInCatalog = map[string]string{
	// Machinery with no public-facing surface worth cataloguing.
	"completion": "shell completion plumbing",
	"version":    "prints the build number",
	"setup":      "interactive first-run wizard; `jc auth` is the catalogued entry point",
	"tui":        "launches the interactive browser over every other command",
	"api":        "raw request passthrough; the catalog describes commands, not the whole API",
}

func TestEveryCommandGroupHasACatalogEntry(t *testing.T) {
	inCatalog := map[string]bool{}
	for _, e := range schema.BuildCommandManifest().Commands {
		inCatalog[strings.TrimPrefix(e.Path, "jc ")] = true
	}

	var missing []string
	for _, c := range NewRootCmd().Commands() {
		if c.Hidden || c.Name() == "help" {
			continue
		}
		name := c.Name()
		if _, exempt := notInCatalog[name]; exempt {
			continue
		}
		if !inCatalog[name] {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("these command groups have no entry in the public command catalog, so "+
			"they are absent from docs/site/commands.json and from llms-full.txt, which "+
			"is what agents read to discover what jc can do:\n  %s\n\nAdd a CommandEntry "+
			"to schema.BuildCommandManifest in internal/schema/schema.go (and put it in a "+
			"sidebar category in cmd/sitegen/main.go), then run `make site`. If the group "+
			"genuinely does not belong in the catalog, add it to notInCatalog in this file "+
			"with the reason.",
			strings.Join(missing, "\n  "))
	}
}

// TestCatalogEntriesAreReachable is the other direction: an entry for a
// command that no longer exists sends readers to something they cannot run.
func TestCatalogEntriesAreReachable(t *testing.T) {
	groups := map[string]bool{}
	for _, c := range NewRootCmd().Commands() {
		groups[c.Name()] = true
		for _, a := range c.Aliases {
			groups[a] = true
		}
	}

	var dangling []string
	for _, e := range schema.BuildCommandManifest().Commands {
		name := strings.TrimPrefix(e.Path, "jc ")
		if !groups[name] {
			dangling = append(dangling, e.Path)
		}
	}

	if len(dangling) > 0 {
		sort.Strings(dangling)
		t.Errorf("the public command catalog advertises commands that do not exist:\n  %s\n\n"+
			"Remove the entry from schema.BuildCommandManifest, or restore the command.",
			strings.Join(dangling, "\n  "))
	}
}

// TestCatalogSubcommandsExist keeps each entry's Subcommands honest. A
// catalogued subcommand that was renamed or removed is worse than an absent
// one: it reads as supported.
func TestCatalogSubcommandsExist(t *testing.T) {
	root := NewRootCmd()
	byName := map[string]bool{}
	for _, c := range root.Commands() {
		byName[c.Name()] = true
		for _, a := range c.Aliases {
			byName[a] = true
		}
	}

	var wrong []string
	for _, e := range schema.BuildCommandManifest().Commands {
		name := strings.TrimPrefix(e.Path, "jc ")
		if !byName[name] {
			continue // TestCatalogEntriesAreReachable reports this one.
		}
		group := findGroup(root, name)
		if group == nil {
			continue
		}
		for _, declared := range e.Subcommands {
			// A declared subcommand may be a path rather than a single name —
			// `jc groups` catalogues "user list" and "device create".
			if !resolveSubcommandPath(group, strings.Fields(declared)) {
				wrong = append(wrong, e.Path+" "+declared)
			}
		}
	}

	if len(wrong) > 0 {
		sort.Strings(wrong)
		t.Errorf("the catalog advertises subcommands that do not exist:\n  %s",
			strings.Join(wrong, "\n  "))
	}
}

// resolveSubcommandPath walks a space-separated subcommand path down from
// cmd. Named for what it walks: api.go owns resolvePath, which resolves an
// API version from a request path.
func resolveSubcommandPath(cmd *cobra.Command, parts []string) bool {
	if len(parts) == 0 {
		return true
	}
	next := findGroup(cmd, parts[0])
	if next == nil {
		return false
	}
	return resolveSubcommandPath(next, parts[1:])
}

func findGroup(root *cobra.Command, name string) *cobra.Command {
	for _, c := range root.Commands() {
		if c.Name() == name {
			return c
		}
		for _, a := range c.Aliases {
			if a == name {
				return c
			}
		}
	}
	return nil
}
