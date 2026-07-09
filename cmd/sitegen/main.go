// Command sitegen renders the jc CLI command manifest into the static
// showcase site artifacts under docs/site/:
//
//   - commands.json   — the manifest, augmented with sidebar categories
//   - llms.txt        — short agent-facing index (https://llmstxt.org/)
//   - llms-full.txt   — full agent-facing reference
//
// The single source of truth is schema.BuildCommandManifest() — this tool
// is just a renderer. To regenerate, run `make site`. The CI gate
// `make verify-site` diffs the committed artifacts against a fresh
// regeneration and fails if they drifted.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/klaassen-consulting/jc/internal/schema"
)

// category groups command paths under a sidebar heading on the site.
// Order matters — it controls sidebar order. A command path that doesn't
// match any category falls under "Other" at the bottom.
type category struct {
	Name     string
	Commands []string
}

var categories = []category{
	{
		Name: "Identity & Access",
		Commands: []string{
			"jc users", "jc groups", "jc admins",
			"jc auth-policies", "jc iplists", "jc identity-providers",
		},
	},
	{
		Name:     "Devices & MDM",
		Commands: []string{"jc devices", "jc apple-mdm", "jc windows-mdm"},
	},
	{
		Name:     "Apps & SSO",
		Commands: []string{"jc apps", "jc app-templates", "jc saas-management"},
	},
	{
		Name: "Policies & Commands",
		Commands: []string{
			"jc policies", "jc policy-templates", "jc policy-groups", "jc commands",
		},
	},
	{
		Name:     "Insights",
		Commands: []string{"jc insights", "jc system-insights"},
	},
	{
		Name: "Integrations",
		Commands: []string{
			"jc ad", "jc gsuite", "jc office365", "jc duo",
			"jc ldap", "jc radius", "jc software", "jc assets",
		},
	},
	{
		Name: "Org & Lifecycle",
		Commands: []string{
			"jc org", "jc user-states", "jc access-requests",
			"jc custom-emails", "jc bulk", "jc graph",
		},
	},
	{
		Name: "AI & Automation",
		Commands: []string{
			"jc recipe", "jc multi", "jc mcp", "jc ask", "jc explain", "jc schema",
		},
	},
	{
		Name:     "Setup",
		Commands: []string{"jc auth", "jc config"},
	},
	{
		Name:     "Diagnostics",
		Commands: []string{"jc audit", "jc doctor"},
	},
}

type enrichedCommand struct {
	Path        string             `json:"path"`
	Description string             `json:"description"`
	Long        string             `json:"long,omitempty"`
	Category    string             `json:"category"`
	Subcommands []string           `json:"subcommands,omitempty"`
	Flags       []schema.FlagEntry `json:"flags,omitempty"`
}

type output struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Categories  []string           `json:"categories"`
	Commands    []enrichedCommand  `json:"commands"`
	GlobalFlags []schema.FlagEntry `json:"global_flags"`
	Resources   []string           `json:"resources"`
}

func main() {
	outDir := flag.String("out", "docs/site", "output directory")
	flag.Parse()

	if err := run(*outDir); err != nil {
		fmt.Fprintln(os.Stderr, "sitegen:", err)
		os.Exit(1)
	}
}

func run(outDir string) error {
	manifest := schema.BuildCommandManifest()
	o := enrich(manifest)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	if err := writeJSON(filepath.Join(outDir, "commands.json"), o); err != nil {
		return fmt.Errorf("commands.json: %w", err)
	}
	if err := writeFile(filepath.Join(outDir, "llms.txt"), renderLLMs(o)); err != nil {
		return fmt.Errorf("llms.txt: %w", err)
	}
	if err := writeFile(filepath.Join(outDir, "llms-full.txt"), renderLLMsFull(o)); err != nil {
		return fmt.Errorf("llms-full.txt: %w", err)
	}

	fmt.Fprintf(os.Stderr, "sitegen: wrote %d commands under %d categories to %s\n",
		len(o.Commands), len(o.Categories), outDir)
	return nil
}

// enrich joins the schema manifest with the hardcoded category map.
// The version field is deliberately dropped: it tracks git-describe and
// would churn commands.json on every commit, defeating `make verify-site`.
func enrich(m schema.CommandManifest) output {
	catByPath := make(map[string]string, len(categories)*4)
	var catNames []string
	for _, c := range categories {
		catNames = append(catNames, c.Name)
		for _, p := range c.Commands {
			catByPath[p] = c.Name
		}
	}
	const fallback = "Other"
	hasFallback := false

	enriched := make([]enrichedCommand, 0, len(m.Commands))
	for _, c := range m.Commands {
		cat, ok := catByPath[c.Path]
		if !ok {
			cat = fallback
			hasFallback = true
		}
		enriched = append(enriched, enrichedCommand{
			Path:        c.Path,
			Description: c.Description,
			Long:        c.Long,
			Category:    cat,
			Subcommands: c.Subcommands,
			Flags:       c.Flags,
		})
	}
	if hasFallback {
		catNames = append(catNames, fallback)
	}

	return output{
		Name:        m.Name,
		Description: m.Description,
		Categories:  catNames,
		Commands:    enriched,
		GlobalFlags: m.GlobalFlags,
		Resources:   m.Resources,
	}
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// renderLLMs emits the short llms.txt index per https://llmstxt.org/.
func renderLLMs(o output) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", o.Name)
	fmt.Fprintf(&b, "> %s\n\n", o.Description)
	fmt.Fprintln(&b, "The jc CLI is a Go-based command-line tool for JumpCloud — managing users, devices, groups, policies, applications, commands, insights, and more. It exposes every JumpCloud resource through a consistent verb-noun grammar with structured JSON output, an MCP server for AI agents, and a recipe system for declarative automation.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Command catalog")
	fmt.Fprintln(&b)

	byCategory := groupByCategory(o)
	for _, cat := range o.Categories {
		cmds := byCategory[cat]
		if len(cmds) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s\n\n", cat)
		for _, c := range cmds {
			fmt.Fprintf(&b, "- `%s` — %s\n", c.Path, c.Description)
		}
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b, "## See also")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "- [Full reference](llms-full.txt)")
	fmt.Fprintln(&b, "- [Machine-readable manifest](commands.json)")
	fmt.Fprintln(&b, "- [Source repository](https://github.com/TheJumpCloud/jc-cli)")
	return b.String()
}

// renderLLMsFull emits the long-form reference: every command, every flag,
// global flags, and the full resource list.
func renderLLMsFull(o output) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — full reference\n\n", o.Name)
	fmt.Fprintf(&b, "> %s\n\n", o.Description)

	fmt.Fprintln(&b, "## Global flags")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "These flags work on every `jc` subcommand:")
	fmt.Fprintln(&b)
	for _, f := range o.GlobalFlags {
		renderFlag(&b, f)
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Commands")
	fmt.Fprintln(&b)
	byCategory := groupByCategory(o)
	for _, cat := range o.Categories {
		cmds := byCategory[cat]
		if len(cmds) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s\n\n", cat)
		for _, c := range cmds {
			fmt.Fprintf(&b, "#### `%s`\n\n", c.Path)
			fmt.Fprintf(&b, "%s\n\n", c.Description)
			if c.Long != "" {
				fmt.Fprintf(&b, "%s\n\n", c.Long)
			}
			if len(c.Subcommands) > 0 {
				fmt.Fprintln(&b, "**Subcommands:**", "`"+strings.Join(c.Subcommands, "`, `")+"`")
				fmt.Fprintln(&b)
			}
			if len(c.Flags) > 0 {
				fmt.Fprintln(&b, "**Flags:**")
				fmt.Fprintln(&b)
				for _, f := range c.Flags {
					renderFlag(&b, f)
				}
				fmt.Fprintln(&b)
			}
		}
	}

	fmt.Fprintln(&b, "## Resource types")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Every resource has a JSON schema available via `jc schema <resource>`:")
	fmt.Fprintln(&b)
	for _, r := range o.Resources {
		fmt.Fprintf(&b, "- `%s`\n", r)
	}
	return b.String()
}

func renderFlag(b *strings.Builder, f schema.FlagEntry) {
	short := ""
	if f.Shorthand != "" {
		short = fmt.Sprintf(", `-%s`", f.Shorthand)
	}
	def := ""
	if f.Default != "" {
		def = fmt.Sprintf(" (default `%s`)", f.Default)
	}
	fmt.Fprintf(b, "- `--%s`%s `<%s>`%s — %s\n", f.Name, short, f.Type, def, f.Description)
}

func groupByCategory(o output) map[string][]enrichedCommand {
	m := make(map[string][]enrichedCommand, len(o.Categories))
	for _, c := range o.Commands {
		m[c.Category] = append(m[c.Category], c)
	}
	return m
}
