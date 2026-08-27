package cmd

import (
	"sort"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/schema"
)

// notAResource lists top-level command groups that legitimately have no
// schema.Resources entry, with the reason. Everything else must be registered.
//
// This exists because of a real regression: nine API areas shipped to the CLI
// and MCP without a schema entry, and because internal/tui builds its menu
// from schema.Resources, none of them ever appeared in the TUI. Nothing caught
// it. Now a new command group fails this test until it is either registered or
// consciously exempted here.
//
// The test lives in internal/cmd rather than internal/schema because it needs
// the Cobra tree, and internal/cmd already imports internal/schema — the
// dependency cannot run the other way.
var notAResource = map[string]string{
	// Tooling and meta-commands: no backing resource collection.
	"ask":      "natural-language translation, not a resource",
	"bulk":     "CSV-driven operations over other resources",
	"explain":  "explains a command, not a resource",
	"mcp":      "MCP server",
	"multi":    "runs another command across profiles",
	"recipe":   "local multi-step workflows, not an API resource",
	"schema":   "emits this registry",
	"config":   "local configuration",
	"setup":    "local configuration",
	"profile":  "local configuration",
	"version":  "build metadata",
	"doctor":   "environment diagnostics",
	"login":    "authentication",
	"logout":   "authentication",
	"tui":      "launches the browser over every other resource",
	"complete": "shell completion",
	"audit":    "runs health checks across other resources; produces findings, not a collection",
	"auth":     "login, logout, and auth status",

	// Resources reached another way in the TUI.
	"graph":     "association traversal, exposed per-resource rather than as its own list",
	"search":    "a capability wired onto users/devices/commands via SearchEndpoint, not a menu entry",
	"office365": "folded into the cloud-directories sub-menu",
	"directories": "a unified read-only view over the other directory integrations, " +
		"with its own TUI screen",
	"bundle":      "security baseline bundles have a bespoke TUI screen",
	"windows-mdm": "has bespoke TUI screens per policy type",

	// Reached in the TUI without a schema entry, deliberately.
	"reports": "five families with different envelopes and writability; the TUI " +
		"sub-menu is derived from report.Families so there is one source of truth",
	"password-policies": "one TUI screen covering both the V1 org-settings default " +
		"and the V2 group-bound policies; the two have different shapes",

	"service-accounts-keys": "sub-resource of service-accounts",
}

func TestEveryCommandGroupHasASchemaEntry(t *testing.T) {
	root := NewRootCmd()

	var missing []string
	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		name := c.Name()
		if _, exempt := notAResource[name]; exempt {
			continue
		}
		if _, ok := schema.Resources[name]; !ok {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("these command groups have no schema.Resources entry, so they are "+
			"invisible to the TUI (which builds its menu from that map) and to "+
			"`jc schema`:\n  %s\n\nAdd an entry to internal/schema/schema.go, or — if the "+
			"group genuinely has no listable resource — add it to notAResource in this "+
			"file with the reason.",
			strings.Join(missing, "\n  "))
	}
}

// TestSchemaEntriesAreReachable is the other direction: a schema entry that no
// command group and no TUI-only resource backs is dead weight.
func TestSchemaEntriesAreReachable(t *testing.T) {
	root := NewRootCmd()
	groups := map[string]bool{}
	for _, c := range root.Commands() {
		groups[c.Name()] = true
		for _, a := range c.Aliases {
			groups[a] = true
		}
	}

	// Resources with no top-level command of the same name, reached through a
	// sibling command or exposed only in the TUI.
	viaOtherCommand := map[string]bool{
		"workflow-runs":         true, // jc workflows runs
		"workflow-templates":    true, // jc workflows templates
		"health-rules":          true, // jc alerts / health monitoring
		"health-rule-templates": true,
		"user-states":           true,
		"system-insights":       true,
		"access-requests":       true,
		"app-templates":         true,
		"policy-templates":      true,
		"policy-groups":         true,
		"identity-providers":    true,
		"saas-management":       true,
		"custom-emails":         true,
		"apple-mdm":             true,
		"auth-policies":         true,
		"service-accounts":      true,
	}

	var orphaned []string
	for name := range schema.Resources {
		if groups[name] || viaOtherCommand[name] {
			continue
		}
		orphaned = append(orphaned, name)
	}

	if len(orphaned) > 0 {
		sort.Strings(orphaned)
		t.Errorf("these schema.Resources entries are backed by no command group:\n  %s",
			strings.Join(orphaned, "\n  "))
	}
}
