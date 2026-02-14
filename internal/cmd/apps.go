package cmd

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
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
		Long:  "List SSO applications, get app details, and view associated groups.",
	}

	cmd.AddCommand(newAppsListCmd())
	cmd.AddCommand(newAppsGetCmd())

	return cmd
}

func newAppsListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all SSO applications",
		Long: `List all JumpCloud SSO applications.

Default fields: _id, name, displayLabel, ssoType, status.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'name=AWS SSO'            Exact match
  --filter 'ssoType=saml'            Filter by SSO type`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppsList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -name)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=AWS SSO')")

	return cmd
}

func runAppsList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
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

	opts := output.CurrentOptions()
	opts.DefaultFields = appDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(result.Data), result.TotalCount)
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
		Args: cobra.ExactArgs(1),
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
