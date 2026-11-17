package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
//
// The SSO type is not a top-level field: it lives in sso.type, which the table
// renderer cannot show as a column. Use --sso-type to filter on it, or
// --query "[].{id:_id,label:displayLabel,type:sso.type}" to show it.
var appDefaultFields = []string{"_id", "name", "displayLabel", "active", "aiReady"}

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
		ssoTypeFlag string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all SSO applications",
		Long: `List all JumpCloud SSO applications.

Default fields: _id, name, displayLabel, active, aiReady. name is the catalog
connector key (bookmark, aws-sso, ...); displayLabel is the title users see.

Filter examples:
  --filter 'name=bookmark'           Exact match on the connector key
  --sso-type saml                    By SSO type: saml, oidc or bookmark
  --ai-ready true                    Apps shown as AI Ready in the User Portal

--sso-type and --ai-ready filter locally. The API rejects every filter on the
SSO type (it lives in sso.type, not in a top-level field), and its own
aiReady:eq:false filter only matches apps where the flag was explicitly stored
and misses most of them.

To show the SSO type as a column:
  jc apps list --query "[].{id:_id,label:displayLabel,type:sso.type}" -t`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppsList(cmd, limitFlag, sortFlag, filterFlag, aiReadyFlag, ssoTypeFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -name)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=AWS SSO')")
	cmd.Flags().StringVar(&aiReadyFlag, "ai-ready", "", "Only apps whose aiReady is true or false (filtered locally)")
	cmd.Flags().StringVar(&ssoTypeFlag, "sso-type", "", "Only apps of this SSO type: saml, oidc or bookmark (filtered locally)")

	return cmd
}

func runAppsList(cmd *cobra.Command, limit int, sort string, filters []string, aiReady, ssoType string) error {
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
	ssoType = strings.ToLower(strings.TrimSpace(ssoType))
	if ssoType != "" && limit > 0 {
		return fmt.Errorf("--sso-type cannot be combined with --limit: it filters locally, so every app has to be fetched first")
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
	if ssoType != "" {
		data, err = filterAppsBySSOType(data, ssoType)
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
		name         string
		displayLabel string
		ssoURL       string
		ssoType      string
		config       string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new SSO application",
		Long: `Create a new JumpCloud SSO application from a catalog connector.

--name is the connector's catalog key, not the title users see: "bookmark"
for a plain link, or a connector such as "slack" (see ` + "`jc app-templates list`" + `).
JumpCloud rejects any other value with "Could not find the service provider".
The title shown in the User Portal is --display-label, and --sso-url is the
URL a bookmark opens.

The SSO type (saml, oidc, bookmark) follows from the connector and cannot be
chosen; --sso-type is accepted for compatibility and ignored.

--config is raw JSON merged into the request body, for any other field.`,
		Example: `  jc apps create --name bookmark --display-label "Team Wiki" --sso-url https://wiki.example.com
  jc apps create --name bookmark --display-label "Team Wiki" --sso-url https://wiki.example.com --plan`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("sso-type") {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: --sso-type is ignored; the SSO type follows from the connector named by --name")
			}
			return runAppsCreate(cmd, name, displayLabel, ssoURL, config)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Catalog connector key, e.g. bookmark (required)")
	cmd.Flags().StringVar(&displayLabel, "display-label", "", "Title shown in the User Portal")
	cmd.Flags().StringVar(&ssoURL, "sso-url", "", "URL the app opens (bookmark apps)")
	cmd.Flags().StringVar(&config, "config", "", "Extra fields as raw JSON, merged into the request body")
	cmd.Flags().StringVar(&ssoType, "sso-type", "", "Ignored: the SSO type follows from the connector")
	// Hidden rather than MarkDeprecated: cobra prints the deprecation notice
	// on stdout, which would corrupt JSON output.
	_ = cmd.Flags().MarkHidden("sso-type")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func runAppsCreate(cmd *cobra.Command, name, displayLabel, ssoURL, config string) error {
	body := map[string]any{"name": name}

	// Merge optional config JSON first so the explicit flags win.
	if config != "" {
		var extra map[string]any
		if err := json.Unmarshal([]byte(config), &extra); err != nil {
			return fmt.Errorf("parsing --config JSON: %w", err)
		}
		for k, v := range extra {
			body[k] = v
		}
	}
	if displayLabel != "" {
		body["displayLabel"] = displayLabel
	}
	if ssoURL != "" {
		body["ssoUrl"] = ssoURL
	}

	if viper.GetBool("plan") {
		target := name
		if displayLabel != "" {
			target = displayLabel
		}
		var effects []string
		for _, k := range sortedKeys(body) {
			effects = append(effects, fmt.Sprintf("%s: %v", k, body[k]))
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "app",
			Target:     target,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.Create(cmd.Context(), "/applications", body)
	if err != nil {
		if appupdate.IsUnknownConnector(err) {
			hint := appupdate.UnknownConnectorHint(name, "--display-label") + " ('jc app-templates list')"
			if apiErr := (*api.APIError)(nil); errors.As(err, &apiErr) {
				e := CLIErrorFromAPI(apiErr)
				e.Suggestion = strings.ToUpper(hint[:1]) + hint[1:]
				return e
			}
		}
		return err
	}

	// The app exists now, so a field that did not land is a warning, not a
	// failure: exiting non-zero would invite a retry that creates a second app.
	if missing, err := appupdate.Unapplied(result, body); err == nil && len(missing) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: JumpCloud created the app but did not keep: %v\n", missing)
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAppsUpdateCmd() *cobra.Command {
	var (
		name         string
		displayLabel string
		config       string
		aiReady      bool
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
		Example: `  jc apps update "Team Wiki" --display-label "Engineering Wiki"
  jc apps update "Bookmark" --ai-ready=true
  jc apps update 6504ef0130b63a30acf199d8 --ai-ready=false`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.ApplicationConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppsUpdate(cmd, args[0], name, displayLabel, config, aiReady)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Not supported: the name is the catalog connector key and cannot change; use --display-label")
	_ = cmd.Flags().MarkHidden("name")
	cmd.Flags().StringVar(&displayLabel, "display-label", "", "Title shown in the User Portal")
	cmd.Flags().StringVar(&config, "config", "", "Fields as raw JSON, merged into the app; a \"config\" object is merged key by key")
	cmd.Flags().BoolVar(&aiReady, "ai-ready", false, "Show the app as AI Ready in the User Portal (true needs an AI Gateway MCP connection; use --ai-ready=false to clear)")

	return cmd
}

func runAppsUpdate(cmd *cobra.Command, identifier, name, displayLabel, config string, aiReady bool) error {
	body := map[string]any{}

	if cmd.Flags().Changed("name") {
		return fmt.Errorf("--name: %w (--display-label)", appupdate.ErrNameImmutable)
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

	// Set after --config so the explicit flags win over a config key.
	if cmd.Flags().Changed("display-label") {
		body["displayLabel"] = displayLabel
	}
	setAIReady := cmd.Flags().Changed("ai-ready")
	if setAIReady {
		body["aiReady"] = aiReady
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --display-label, --config, --ai-ready)")
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
	result, err := appupdate.Apply(cmd.Context(), client, id, body, aiready.Field)
	if err != nil {
		return err
	}
	if setAIReady {
		if err := aiready.CheckApplied(result, aiReady); err != nil {
			return err
		}
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

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// filterAppsBySSOType keeps the apps whose sso.type equals want. It returns a
// non-nil slice so an empty result renders as [] rather than null.
func filterAppsBySSOType(apps []json.RawMessage, want string) ([]json.RawMessage, error) {
	out := []json.RawMessage{}
	for i, raw := range apps {
		var app struct {
			SSO struct {
				Type string `json:"type"`
			} `json:"sso"`
		}
		if err := json.Unmarshal(raw, &app); err != nil {
			return nil, fmt.Errorf("reading application %d: %w", i, err)
		}
		if strings.EqualFold(app.SSO.Type, want) {
			out = append(out, raw)
		}
	}
	return out, nil
}
