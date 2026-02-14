package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

// Explanation describes what a jc command would do without executing it.
type Explanation struct {
	Command      string   `json:"command"`
	Action       string   `json:"action"`
	Resource     string   `json:"resource"`
	Description  string   `json:"description"`
	Reversible   bool     `json:"reversible"`
	Destructive  bool     `json:"destructive"`
	SideEffects  []string `json:"side_effects,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
	RequiresAuth bool     `json:"requires_auth"`
}

// commandInfo holds static metadata about a specific command verb.
type commandInfo struct {
	description  string
	reversible   bool
	destructive  bool
	sideEffects  []string
	warnings     []string
	requiresAuth bool
}

// commandMetadata maps resource → verb → metadata for all known commands.
var commandMetadata = map[string]map[string]commandInfo{
	"users": {
		"list":           {description: "List all JumpCloud system users with optional filtering and sorting.", reversible: true, requiresAuth: true},
		"get":            {description: "Retrieve a single JumpCloud user by username or ID.", reversible: true, requiresAuth: true},
		"create":         {description: "Create a new JumpCloud user account.", reversible: true, requiresAuth: true, sideEffects: []string{"A welcome email may be sent to the user"}},
		"update":         {description: "Update fields on an existing JumpCloud user.", reversible: true, requiresAuth: true},
		"delete":         {description: "Permanently delete a JumpCloud user.", reversible: false, destructive: true, requiresAuth: true, sideEffects: []string{"User is removed from all groups", "All device bindings are removed", "User loses access to all associated applications"}, warnings: []string{"This action is irreversible"}},
		"search":         {description: "Search for users by keyword across username, email, firstname, and lastname.", reversible: true, requiresAuth: true},
		"lock":           {description: "Lock a user account, preventing login.", reversible: true, requiresAuth: true, sideEffects: []string{"User is immediately prevented from logging in"}},
		"unlock":         {description: "Unlock a previously locked user account, restoring login access.", reversible: true, requiresAuth: true},
		"reset-mfa":      {description: "Reset MFA/TOTP enrollment for a user.", reversible: false, requiresAuth: true, sideEffects: []string{"User must re-enroll their authenticator device"}, warnings: []string{"The user will need to set up MFA again on next login"}},
		"reset-password": {description: "Trigger a password reset email to the user.", reversible: false, requiresAuth: true, sideEffects: []string{"A password reset email is sent to the user's email address"}},
	},
	"devices": {
		"list":    {description: "List all JumpCloud-managed devices (systems) with optional filtering.", reversible: true, requiresAuth: true},
		"get":     {description: "Retrieve a single device by hostname or ID.", reversible: true, requiresAuth: true},
		"delete":  {description: "Remove a device record from JumpCloud.", reversible: false, destructive: true, requiresAuth: true, sideEffects: []string{"The JumpCloud agent on the device is unlinked", "Device loses all managed configurations"}, warnings: []string{"The device will need to be re-enrolled to manage again"}},
		"lock":    {description: "Send an MDM lock command to the device.", reversible: true, requiresAuth: true, sideEffects: []string{"The device screen is locked immediately via MDM"}},
		"restart": {description: "Send an MDM restart command to the device.", reversible: true, requiresAuth: true, sideEffects: []string{"The device will restart immediately", "Unsaved work on the device may be lost"}},
		"erase":   {description: "Send an MDM erase (wipe) command to the device.", reversible: false, destructive: true, requiresAuth: true, sideEffects: []string{"ALL data on the device is permanently erased", "The device is factory reset"}, warnings: []string{"EXTREMELY DESTRUCTIVE — all data will be lost", "Requires --confirm-erase flag as a safety gate"}},
	},
	"groups": {
		"user":          {description: "Manage JumpCloud user groups (list, get, create, update, delete).", reversible: true, requiresAuth: true},
		"device":        {description: "Manage JumpCloud device groups (list, get, create, update, delete).", reversible: true, requiresAuth: true},
		"add-member":    {description: "Add a user or device to a group.", reversible: true, requiresAuth: true, sideEffects: []string{"The member inherits group policies and application access"}},
		"remove-member": {description: "Remove a user or device from a group.", reversible: true, requiresAuth: true, sideEffects: []string{"The member loses group policies and application access"}},
	},
	"insights": {
		"query":    {description: "Query Directory Insights events (audit log) for a given service and time range.", reversible: true, requiresAuth: true},
		"count":    {description: "Count events matching criteria without returning full records.", reversible: true, requiresAuth: true},
		"distinct": {description: "Get distinct values for a specific field across matching events.", reversible: true, requiresAuth: true},
		"save":     {description: "Save a frequently used insight query for later reuse.", reversible: true, requiresAuth: false},
		"run":      {description: "Execute a previously saved insight query.", reversible: true, requiresAuth: true},
		"saved":    {description: "List all saved insight queries.", reversible: true, requiresAuth: false},
	},
	"commands": {
		"list":    {description: "List all JumpCloud commands.", reversible: true, requiresAuth: true},
		"get":     {description: "Retrieve a single command by name or ID.", reversible: true, requiresAuth: true},
		"create":  {description: "Create a new JumpCloud command.", reversible: true, requiresAuth: true},
		"update":  {description: "Update an existing JumpCloud command.", reversible: true, requiresAuth: true},
		"delete":  {description: "Delete a JumpCloud command.", reversible: false, destructive: true, requiresAuth: true, warnings: []string{"This action is irreversible"}},
		"run":     {description: "Trigger a command to run on specified devices or device groups.", reversible: false, requiresAuth: true, sideEffects: []string{"The command executes on the target devices", "Command output is recorded in command results"}, warnings: []string{"The command will run immediately on target devices"}},
		"results": {description: "List execution results for a command.", reversible: true, requiresAuth: true},
	},
	"policies": {
		"list":    {description: "List all JumpCloud policies with name, type, and OS target.", reversible: true, requiresAuth: true},
		"get":     {description: "Retrieve a single policy by name or ID.", reversible: true, requiresAuth: true},
		"results": {description: "List policy application results per device.", reversible: true, requiresAuth: true},
	},
	"apps": {
		"list": {description: "List all SSO applications with name, type, and status.", reversible: true, requiresAuth: true},
		"get":  {description: "Retrieve a single application by name or ID.", reversible: true, requiresAuth: true},
	},
	"admins": {
		"list": {description: "List all JumpCloud administrators with email, role, and MFA status.", reversible: true, requiresAuth: true},
	},
	"graph": {
		"traverse": {description: "Traverse JumpCloud graph associations between resources (e.g., user→groups, device→groups).", reversible: true, requiresAuth: true},
	},
	"bulk": {
		"users": {description: "Process bulk user operations (create, update, delete) from a CSV file.", reversible: false, requiresAuth: true, sideEffects: []string{"Multiple users may be created, updated, or deleted"}, warnings: []string{"Review the CSV file carefully before executing", "Use --plan to preview all changes first"}},
	},
	"recipe": {
		"list":     {description: "List all available recipes (built-in and user-defined).", reversible: true, requiresAuth: false},
		"show":     {description: "Display full recipe details including steps and parameters.", reversible: true, requiresAuth: false},
		"run":      {description: "Execute a multi-step automated recipe with parameters.", reversible: false, requiresAuth: true, warnings: []string{"Recipe may perform multiple API operations", "Use --plan to preview all steps first"}},
		"validate": {description: "Validate a recipe YAML file for syntax and semantic errors.", reversible: true, requiresAuth: false},
		"create":   {description: "Interactively create a new recipe.", reversible: true, requiresAuth: false},
		"import":   {description: "Import a recipe from a URL or local file.", reversible: true, requiresAuth: false},
		"export":   {description: "Export a recipe as YAML.", reversible: true, requiresAuth: false},
	},
	"auth": {
		"login":  {description: "Authenticate to JumpCloud by providing an API key or service account credentials.", reversible: true, requiresAuth: false},
		"status": {description: "Show current authentication status, profile, and organization info.", reversible: true, requiresAuth: true},
		"logout": {description: "Remove stored credentials and clear the active profile.", reversible: true, requiresAuth: false, sideEffects: []string{"Credentials are removed from the OS keychain", "API key is cleared from the config file"}},
		"switch": {description: "Switch to a different named profile for multi-org management.", reversible: true, requiresAuth: false},
	},
	"config": {
		"view": {description: "Display the current configuration (secrets redacted).", reversible: true, requiresAuth: false},
		"set":  {description: "Set a configuration value using dot notation.", reversible: true, requiresAuth: false},
	},
	"schema": {
		"resources": {description: "List all resource types with their schema metadata.", reversible: true, requiresAuth: false},
		"commands":  {description: "Return full CLI command manifest as JSON.", reversible: true, requiresAuth: false},
	},
	"mcp": {
		"serve": {description: "Start the MCP (Model Context Protocol) server for AI agent integration.", reversible: true, requiresAuth: true},
	},
	"ask": {
		"": {description: "Translate a natural language query into jc CLI commands using an LLM.", reversible: true, requiresAuth: false, warnings: []string{"Commands are proposed for review before execution", "Requires LLM provider configuration (ask.provider)"}},
	},
}

// resourceDescriptions provides fallback descriptions when only a resource name is given.
var resourceDescriptions = map[string]string{
	"users":    "Manage JumpCloud system users (list, get, create, update, delete, search, lock, unlock, reset-mfa, reset-password).",
	"devices":  "Manage JumpCloud devices/systems (list, get, delete, lock, restart, erase).",
	"groups":   "Manage user groups and device groups (list, get, create, update, delete, add-member, remove-member).",
	"insights": "Query JumpCloud Directory Insights audit events (query, count, distinct, save, run).",
	"commands": "Manage and run JumpCloud commands (list, get, create, update, delete, run, results).",
	"policies": "Manage JumpCloud policies (list, get, results).",
	"apps":     "Manage SSO applications (list, get).",
	"admins":   "List JumpCloud administrators.",
	"graph":    "Traverse JumpCloud graph associations between resources.",
	"bulk":     "Perform bulk operations on resources from CSV files.",
	"recipe":   "Manage and run automation recipes.",
	"auth":     "Manage authentication and profiles.",
	"config":   "View and modify CLI configuration.",
	"schema":   "Inspect resource schemas and CLI command metadata.",
	"mcp":      "MCP server for AI agent integration.",
	"ask":      "Translate natural language queries into jc CLI commands using an LLM.",
}

// newExplainCmd creates the explain command.
func newExplainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain <command...>",
		Short: "Explain what a command would do in plain English",
		Long: `Explain describes what a jc command would do without executing it.

This is useful for understanding commands before running them,
especially when reviewing LLM-generated commands.

Unlike --plan, explain makes NO API calls and requires no authentication.
It provides a static description based on the command structure.

Examples:
  jc explain users delete jdoe
  jc explain devices erase MY-LAPTOP
  jc explain groups add-member Engineering --user jdoe
  jc explain "bulk users --file users.csv"
  jc explain users list --output json`,
		Args: cobra.MinimumNArgs(1),
		RunE: runExplain,
	}
	return cmd
}

func runExplain(cmd *cobra.Command, args []string) error {
	// Join all args into a single command string, then split on whitespace.
	// This handles both: jc explain users delete jdoe
	//               and: jc explain "users delete jdoe"
	commandStr := strings.Join(args, " ")
	parts := strings.Fields(commandStr)
	if len(parts) == 0 {
		return NewCLIError(ErrCodeUsageError, "empty command string", "Provide a command to explain, e.g.: jc explain users delete jdoe")
	}

	explanation := buildExplanation(parts)

	outputFlag := cmd.Root().PersistentFlags().Lookup("output")
	if outputFlag != nil && outputFlag.Changed && outputFlag.Value.String() == "json" {
		return renderExplanationJSON(cmd.OutOrStdout(), explanation)
	}
	return renderExplanationHuman(cmd.OutOrStdout(), explanation)
}

// buildExplanation constructs an Explanation from parsed command parts.
func buildExplanation(parts []string) *Explanation {
	resource := parts[0]

	// Check for groups subcommands: "groups user list", "groups device get", etc.
	if resource == "groups" && len(parts) > 1 {
		subgroup := parts[1]
		if subgroup == "user" || subgroup == "device" {
			if len(parts) > 2 {
				verb := parts[2]
				info := lookupGroupsSubcommand(subgroup, verb)
				if info != nil {
					return &Explanation{
						Command:      strings.Join(parts, " "),
						Action:       verb,
						Resource:     "groups " + subgroup,
						Description:  info.description,
						Reversible:   info.reversible,
						Destructive:  info.destructive,
						SideEffects:  info.sideEffects,
						Warnings:     info.warnings,
						RequiresAuth: info.requiresAuth,
					}
				}
			}
			// Just "groups user" or "groups device" — resource-level description.
			if info, ok := commandMetadata["groups"][subgroup]; ok {
				return &Explanation{
					Command:      strings.Join(parts, " "),
					Action:       "manage",
					Resource:     "groups " + subgroup,
					Description:  info.description,
					Reversible:   info.reversible,
					RequiresAuth: info.requiresAuth,
				}
			}
		}
		// Groups-level verbs like "add-member", "remove-member".
		verb := parts[1]
		if info, ok := commandMetadata["groups"][verb]; ok {
			return &Explanation{
				Command:      strings.Join(parts, " "),
				Action:       verb,
				Resource:     "groups",
				Description:  info.description,
				Reversible:   info.reversible,
				Destructive:  info.destructive,
				SideEffects:  info.sideEffects,
				Warnings:     info.warnings,
				RequiresAuth: info.requiresAuth,
			}
		}
	}

	// Look up resource + verb in metadata.
	if resourceMeta, ok := commandMetadata[resource]; ok {
		if len(parts) > 1 {
			verb := parts[1]
			if info, ok := resourceMeta[verb]; ok {
				return &Explanation{
					Command:      strings.Join(parts, " "),
					Action:       verb,
					Resource:     resource,
					Description:  info.description,
					Reversible:   info.reversible,
					Destructive:  info.destructive,
					SideEffects:  info.sideEffects,
					Warnings:     info.warnings,
					RequiresAuth: info.requiresAuth,
				}
			}
			// Unknown verb for known resource.
			return &Explanation{
				Command:      strings.Join(parts, " "),
				Action:       verb,
				Resource:     resource,
				Description:  fmt.Sprintf("Unknown subcommand '%s' for '%s'.", verb, resource),
				Reversible:   true,
				RequiresAuth: true,
			}
		}
		// Resource-only (e.g., "jc explain users").
		desc := resourceDescriptions[resource]
		if desc == "" {
			desc = fmt.Sprintf("Manage JumpCloud %s.", resource)
		}
		return &Explanation{
			Command:      resource,
			Action:       "manage",
			Resource:     resource,
			Description:  desc,
			Reversible:   true,
			RequiresAuth: true,
		}
	}

	// Completely unknown command.
	return &Explanation{
		Command:      strings.Join(parts, " "),
		Action:       "unknown",
		Resource:     resource,
		Description:  fmt.Sprintf("Unknown command: %s. Run 'jc --help' for available commands.", strings.Join(parts, " ")),
		Reversible:   true,
		RequiresAuth: false,
	}
}

// lookupGroupsSubcommand returns info for "groups user <verb>" or "groups device <verb>".
func lookupGroupsSubcommand(subgroup, verb string) *commandInfo {
	groupVerbs := map[string]commandInfo{
		"list":   {description: fmt.Sprintf("List all %s groups.", subgroup), reversible: true, requiresAuth: true},
		"get":    {description: fmt.Sprintf("Retrieve a single %s group by name or ID.", subgroup), reversible: true, requiresAuth: true},
		"create": {description: fmt.Sprintf("Create a new %s group.", subgroup), reversible: true, requiresAuth: true},
		"update": {description: fmt.Sprintf("Update an existing %s group.", subgroup), reversible: true, requiresAuth: true},
		"delete": {description: fmt.Sprintf("Delete a %s group.", subgroup), reversible: false, destructive: true, requiresAuth: true, sideEffects: []string{"All members are removed from the group", "Associated policies and app access are revoked"}, warnings: []string{"This action is irreversible"}},
	}
	if info, ok := groupVerbs[verb]; ok {
		return &info
	}
	return nil
}

// renderExplanationJSON writes the explanation as structured JSON.
func renderExplanationJSON(w io.Writer, e *Explanation) error {
	out, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}

// renderExplanationHuman writes a human-readable explanation.
func renderExplanationHuman(w io.Writer, e *Explanation) error {
	border := strings.Repeat("─", 60)
	fmt.Fprintf(w, "┌%s┐\n", border)
	fmt.Fprintf(w, "│ %-58s │\n", fmt.Sprintf("Explain: %s", truncate(e.Command, 48)))
	fmt.Fprintf(w, "├%s┤\n", border)
	fmt.Fprintf(w, "│ %-58s │\n", fmt.Sprintf("Action: %s %s", e.Action, e.Resource))

	// Word-wrap description to fit in box.
	for _, line := range wrapText(e.Description, 56) {
		fmt.Fprintf(w, "│  %-57s │\n", line)
	}

	if e.Destructive {
		fmt.Fprintf(w, "│ %-58s │\n", "*** DESTRUCTIVE OPERATION ***")
	}

	rev := "yes"
	if !e.Reversible {
		rev = "no (irreversible)"
	}
	fmt.Fprintf(w, "│ %-58s │\n", fmt.Sprintf("Reversible: %s", rev))

	if e.RequiresAuth {
		fmt.Fprintf(w, "│ %-58s │\n", "Requires authentication: yes")
	}

	if len(e.SideEffects) > 0 {
		fmt.Fprintf(w, "│ %-58s │\n", "Side effects:")
		for _, se := range e.SideEffects {
			for _, line := range wrapText("- "+se, 56) {
				fmt.Fprintf(w, "│  %-57s │\n", line)
			}
		}
	}

	if len(e.Warnings) > 0 {
		fmt.Fprintf(w, "│ %-58s │\n", "Warnings:")
		for _, w2 := range e.Warnings {
			for _, line := range wrapText("! "+w2, 56) {
				fmt.Fprintf(w, "│  %-57s │\n", line)
			}
		}
	}

	fmt.Fprintf(w, "└%s┘\n", border)
	fmt.Fprintln(w, "No action taken (explain mode).")
	return nil
}

// truncate shortens s to max length with "..." suffix if needed.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// wrapText breaks text into lines of at most maxWidth characters.
func wrapText(text string, maxWidth int) []string {
	if len(text) <= maxWidth {
		return []string{text}
	}

	var lines []string
	for len(text) > maxWidth {
		// Find the last space before maxWidth.
		idx := strings.LastIndex(text[:maxWidth], " ")
		if idx <= 0 {
			idx = maxWidth
		}
		lines = append(lines, text[:idx])
		text = strings.TrimLeft(text[idx:], " ")
	}
	if text != "" {
		lines = append(lines, text)
	}
	return lines
}
