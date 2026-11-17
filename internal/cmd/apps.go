package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/aiready"
	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/appupdate"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

// appDefaultFields is the default field subset shown for apps list/table output.
var appDefaultFields = []string{"_id", "name", "displayLabel", "ssoType", "status"}

// resolveApp resolves an app name or ID to a JumpCloud application ID.
func resolveApp(ctx context.Context, client *api.V1Client, identifier string) (string, error) {
	r := resolve.NewResolver(client)
	return r.Resolve(ctx, identifier, resolve.ApplicationConfig)
}

func newAppsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apps",
		Short: "Manage JumpCloud SSO applications",
		Long:  "List, get, create, update, and delete SSO applications and view associated groups.",
	}

	cmd.AddCommand(newAppsListCmd())
	cmd.AddCommand(newAppsGetCmd())
	cmd.AddCommand(newAppsCreateCmd())
	cmd.AddCommand(newAppsUpdateCmd())
	cmd.AddCommand(newAppsDeleteCmd())

	return cmd
}

func newAppsListCmd() *cobra.Command {
	var (
		limitFlag   int
		sortFlag    string
		filterFlag  []string
		aiReadyFlag string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all SSO applications",
		Long: `List all JumpCloud SSO applications.

Default fields: _id, name, displayLabel, ssoType, status.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'name=AWS SSO'            Exact match
  --filter 'ssoType=saml'            Filter by SSO type
  --ai-ready true                    Apps shown as AI Ready in the User Portal

--ai-ready filters locally on each app's aiReady value, because the API's own
aiReady:eq:false filter only matches apps where the flag was explicitly stored
and misses most of them.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppsList(cmd, limitFlag, sortFlag, filterFlag, aiReadyFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -name)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=AWS SSO')")
	cmd.Flags().StringVar(&aiReadyFlag, "ai-ready", "", "Only apps whose aiReady is true or false (filtered locally)")

	return cmd
}

func runAppsList(cmd *cobra.Command, limit int, sort string, filters []string, aiReady string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}
	var wantAIReady *bool
	if aiReady != "" {
		v, perr := parseBoolWord(aiReady)
		if perr != nil {
			return fmt.Errorf("--ai-ready: %w", perr)
		}
		wantAIReady = &v
		if limit > 0 {
			return fmt.Errorf("--ai-ready cannot be combined with --limit: it filters locally, so every app has to be fetched first")
		}
	}

	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/applications", api.ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV1Queries(exprs),
	})
	if err != nil {
		return err
	}

	data, total := result.Data, result.TotalCount
	if wantAIReady != nil {
		data, err = aiready.Filter(data, *wantAIReady)
		if err != nil {
			return err
		}
		total = len(data)
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = appDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(data), total)
	}

	return nil
}

func newAppsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <app-name-or-id>",
		Short: "Get an application by name or ID",
		Long: `Get a single JumpCloud SSO application by name or ID.

Accepts an app name (e.g., "AWS SSO") or a 24-character hex application ID.
App names are resolved to IDs automatically with caching (use --no-cache to bypass).

The output includes associated user groups and device groups fetched via
the V2 graph associations API.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.ApplicationConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppsGet(cmd, args[0])
		},
	}

	return cmd
}

func runAppsGet(cmd *cobra.Command, identifier string) error {
	v1Client, err := newV1Client()
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	id, err := resolveApp(ctx, v1Client, identifier)
	if err != nil {
		return err
	}

	// Fetch the application via V1.
	appData, err := v1Client.Get(ctx, "/applications/"+id)
	if err != nil {
		return err
	}

	// Fetch associated user groups and device groups via V2 graph API.
	v2Client, err := newV2Client()
	if err != nil {
		return err
	}

	// V2 graph associations use ?targets=<type> as a query parameter (not a filter).
	// We embed it in the endpoint URL so buildV2ListURL preserves it alongside limit.
	userGroups, err := v2Client.ListAll(ctx, "/applications/"+id+"/associations?targets=user_group", api.V2ListOptions{})
	if err != nil {
		// Non-fatal: associations may not be available.
		userGroups = &api.V2ListResult{}
	}

	deviceGroups, err := v2Client.ListAll(ctx, "/applications/"+id+"/associations?targets=system_group", api.V2ListOptions{})
	if err != nil {
		deviceGroups = &api.V2ListResult{}
	}

	// Merge associations into the app data.
	enriched, err := enrichAppWithAssociations(appData, userGroups.Data, deviceGroups.Data)
	if err != nil {
		// Fall back to plain app data if enrichment fails.
		enriched = appData
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), enriched, opts)
}

func newAppsCreateCmd() *cobra.Command {
	var (
		name    string
		ssoType string
		config  string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new SSO application",
		Long: `Create a new JumpCloud SSO application.

Required: --name and --sso-type.
Optional: --config for SSO-specific configuration as raw JSON.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppsCreate(cmd, name, ssoType, config)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Application name (required)")
	cmd.Flags().StringVar(&ssoType, "sso-type", "", "SSO type, e.g. saml, oidc, bookmark (required)")
	cmd.Flags().StringVar(&config, "config", "", "SSO-specific configuration as raw JSON")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("sso-type")

	return cmd
}

func runAppsCreate(cmd *cobra.Command, name, ssoType, config string) error {
	if viper.GetBool("plan") {
		effects := []string{"name: " + name, "ssoType: " + ssoType}
		if config != "" {
			effects = append(effects, "config: "+config)
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "app",
			Target:     name,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	body := map[string]any{
		"name":    name,
		"ssoType": ssoType,
	}

	// Merge optional config JSON into the body.
	if config != "" {
		var extra map[string]any
		if err := json.Unmarshal([]byte(config), &extra); err != nil {
			return fmt.Errorf("parsing --config JSON: %w", err)
		}
		for k, v := range extra {
			body[k] = v
		}
	}

	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.Create(cmd.Context(), "/applications", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAppsUpdateCmd() *cobra.Command {
	var (
		name    string
		config  string
		aiReady bool
	)

	cmd := &cobra.Command{
		Use:   "update <app-name-or-id>",
		Short: "Update an SSO application",
		Long: `Update an existing JumpCloud SSO application.

Accepts an app name or 24-character hex application ID.
Specify only the fields you want to change. The updated app object is returned.

--ai-ready marks the app as AI Ready in the User Portal (informational only; it
grants no access). JumpCloud accepts true only for an app that has an MCP
connection, i.e. an AI Gateway server assigned to it (Access > AI Gateway >
Servers), and rejects the whole update otherwise.

jc reads the app and sends it back in full with your changes: JumpCloud
answers a partial update with success while changing nothing. The returned
app is then checked, and jc fails if a requested value did not take effect.`,
		Example: `  jc apps update "Bookmark" --ai-ready=true
  jc apps update 6504ef0130b63a30acf199d8 --ai-ready=false`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.ApplicationConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppsUpdate(cmd, args[0], name, config, aiReady)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Application name")
	cmd.Flags().StringVar(&config, "config", "", "SSO-specific configuration as raw JSON")
	cmd.Flags().BoolVar(&aiReady, "ai-ready", false, "Show the app as AI Ready in the User Portal (true needs an AI Gateway MCP connection; use --ai-ready=false to clear)")

	return cmd
}

func runAppsUpdate(cmd *cobra.Command, identifier, name, config string, aiReady bool) error {
	body := map[string]any{}

	if cmd.Flags().Changed("name") {
		body["name"] = name
	}

	// Merge optional config JSON into the body.
	if cmd.Flags().Changed("config") {
		var extra map[string]any
		if err := json.Unmarshal([]byte(config), &extra); err != nil {
			return fmt.Errorf("parsing --config JSON: %w", err)
		}
		for k, v := range extra {
			body[k] = v
		}
	}

	// Set after --config so the explicit flag wins over a config key.
	setAIReady := cmd.Flags().Changed("ai-ready")
	if setAIReady {
		body["aiReady"] = aiReady
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --name, --config, --ai-ready)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "app",
			Target:     identifier,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveApp(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// JumpCloud ignores a PUT without name and ssoUrl (200, nothing applied),
	// so send the whole app with the changes overlaid. See internal/appupdate.
	current, err := client.Get(cmd.Context(), "/applications/"+id)
	if err != nil {
		return err
	}
	full, err := appupdate.Merge(current, body)
	if err != nil {
		return err
	}
	result, err := client.Update(cmd.Context(), "/applications/"+id, full)
	if err != nil {
		return err
	}
	if setAIReady {
		if err := aiready.CheckApplied(result, aiReady); err != nil {
			return err
		}
	}
	missing, err := appupdate.Unapplied(result, body, aiready.Field)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return appupdate.ErrUnapplied(missing)
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAppsDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <app-name-or-id>",
		Aliases: []string{"rm"},
		Short:   "Delete an SSO application",
		Long: `Delete a JumpCloud SSO application.

Accepts an app name or 24-character hex application ID.
Shows the app name before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.ApplicationConfig),
		RunE:              batchRunE("app", "delete", runAppsDelete),
	}

	addBatchSourceFlags(cmd)
	return cmd
}

func runAppsDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveApp(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch the app first so we can show details in the confirmation prompt.
	appData, err := client.Get(cmd.Context(), "/applications/"+id)
	if err != nil {
		return err
	}

	var app struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(appData, &app); err != nil {
		return fmt.Errorf("parsing app data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "app",
			Target:   fmt.Sprintf("%s (%s)", app.Name, id),
			Effects:  []string{"Remove application permanently", "Users will lose SSO access"},
		}
		return renderPlan(cmd, p)
	}

	// Confirmation prompt (unless --force is set).
	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete app %q? [y/N] ", app.Name)
		reader := getConfirmReader()
		answer, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading confirmation: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
			return nil
		}
	}

	_, err = client.Delete(cmd.Context(), "/applications/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "App %q deleted successfully.\n", app.Name)
	return nil
}

// enrichAppWithAssociations adds userGroups and deviceGroups arrays to the app JSON.
func enrichAppWithAssociations(appData json.RawMessage, userGroups, deviceGroups []json.RawMessage) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(appData, &obj); err != nil {
		return nil, err
	}

	// Ensure nil slices marshal as [] not null.
	if userGroups == nil {
		userGroups = []json.RawMessage{}
	}
	if deviceGroups == nil {
		deviceGroups = []json.RawMessage{}
	}

	ugJSON, err := json.Marshal(userGroups)
	if err != nil {
		return nil, err
	}
	obj["associatedUserGroups"] = ugJSON

	dgJSON, err := json.Marshal(deviceGroups)
	if err != nil {
		return nil, err
	}
	obj["associatedDeviceGroups"] = dgJSON

	return json.Marshal(obj)
}

// parseBoolWord accepts exactly "true" or "false".
func parseBoolWord(s string) (bool, error) {
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("%q is not true or false", s)
}
