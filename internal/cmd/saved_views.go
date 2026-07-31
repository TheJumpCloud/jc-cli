package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
	"github.com/klaassen-consulting/jc/internal/savedview"
)

func resolveSavedView(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.SavedViewConfig)
}

// fetchSavedView returns the saved-view object with the given id by reading it
// out of the list — /saved-views has no GET-by-id endpoint. Used by get and by
// the update read-modify-write.
func fetchSavedView(ctx context.Context, client *api.V2Client, id string) (map[string]any, error) {
	result, err := client.ListAll(ctx, "/saved-views", api.V2ListOptions{ResponseKey: "views"})
	if err != nil {
		return nil, err
	}
	for _, raw := range result.Data {
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}
		if s, _ := obj["id"].(string); s == id {
			return obj, nil
		}
	}
	return nil, fmt.Errorf("saved view %q not found", id)
}

func newSavedViewsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "saved-views",
		Aliases: []string{"saved-view", "views"},
		Short:   "Manage JumpCloud saved views (saved column/filter/sort presets for console lists)",
		Long: `List, get, create, update, and delete JumpCloud saved views.

A saved view is a named preset — columns, sort, and filters — for one of the
console's object lists. The 'source' field names which list the view belongs
to (e.g. devices, users, user_groups, systems, policies). A shared view is
visible to all admins in the organization.`,
	}
	cmd.AddCommand(newSavedViewsListCmd())
	cmd.AddCommand(newSavedViewsGetCmd())
	cmd.AddCommand(newSavedViewsCreateCmd())
	cmd.AddCommand(newSavedViewsUpdateCmd())
	cmd.AddCommand(newSavedViewsDeleteCmd())
	return cmd
}

func newSavedViewsListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
		sourceFlag string
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all saved views",
		Long: `List all JumpCloud saved views.

Default fields: id, name, source, shared, isDefault. Use --source to show only
views for one list (e.g. --source devices).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSavedViewsList(cmd, limitFlag, sortFlag, filterFlag, sourceFlag)
		},
	}
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'shared=true')")
	cmd.Flags().StringVar(&sourceFlag, "source", "", "Show only views for this list (e.g. devices, users)")
	return cmd
}

func runSavedViewsList(cmd *cobra.Command, limit int, sort string, filters []string, source string) error {
	if source != "" {
		filters = append(filters, "source="+source)
	}
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	result, err := client.ListAll(cmd.Context(), "/saved-views", api.V2ListOptions{
		Limit:       limit,
		Sort:        sort,
		Filter:      filter.ToV2Queries(exprs),
		ResponseKey: "views",
	})
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	opts.DefaultFields = savedview.DefaultFields
	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}
	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}
	return nil
}

func newSavedViewsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "get <name-or-id>",
		Short:             "Get a saved view by name or ID",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SavedViewConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSavedViewsGet(cmd, args[0])
		},
	}
	return cmd
}

func runSavedViewsGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveSavedView(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	// No GET /saved-views/{id}; read the object back out of the list.
	obj, err := fetchSavedView(cmd.Context(), client, id)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), raw, opts)
}

func newSavedViewsCreateCmd() *cobra.Command {
	var (
		name      string
		source    string
		columns   string
		sortCol   string
		shared    bool
		isDefault bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a saved view",
		Long: `Create a new JumpCloud saved view.

Required: --name and --source. --source names which console list the view
belongs to; common values: devices, users, user_groups, systems, policies.
--columns is a comma-separated column set and --sort is the sort field.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSavedViewsCreate(cmd, name, source, columns, sortCol, shared, isDefault)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Saved view name (required)")
	cmd.Flags().StringVar(&source, "source", "", "List the view belongs to, e.g. devices, users (required)")
	cmd.Flags().StringVar(&columns, "columns", "", "Comma-separated column set")
	cmd.Flags().StringVar(&sortCol, "sort", "", "Sort field for the view")
	cmd.Flags().BoolVar(&shared, "shared", false, "Make the view visible to all admins")
	cmd.Flags().BoolVar(&isDefault, "default", false, "Make this the default view for its source")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("source")
	return cmd
}

func runSavedViewsCreate(cmd *cobra.Command, name, source, columns, sortCol string, shared, isDefault bool) error {
	cols := splitScopes(columns) // reuse the comma-split/trim/empty-free helper
	body, err := savedview.CreateBody(name, source, cols, sortCol, shared, isDefault)
	if err != nil {
		return err
	}
	if viper.GetBool("plan") {
		effects := []string{"source: " + source}
		if len(cols) > 0 {
			effects = append(effects, fmt.Sprintf("columns: %s", strings.Join(cols, ", ")))
		}
		if sortCol != "" {
			effects = append(effects, "sort: "+sortCol)
		}
		if shared {
			effects = append(effects, "shared with all admins")
		}
		if isDefault {
			effects = append(effects, "set as default for its source")
		}
		return renderPlan(cmd, &plan.Plan{
			Action:     "create",
			Resource:   "saved view",
			Target:     name,
			Effects:    effects,
			Reversible: true,
		})
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	result, err := client.Create(cmd.Context(), "/saved-views", body)
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newSavedViewsUpdateCmd() *cobra.Command {
	var (
		name    string
		columns string
		sortCol string
		shared  bool
	)
	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Update a saved view",
		Long: `Update an existing JumpCloud saved view.

Specify only the fields you want to change; all others are preserved. The
saved-view PUT is a full-object replace, so this command reads the current
view, applies your changes, and writes it back.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SavedViewConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSavedViewsUpdate(cmd, args[0], name, columns, sortCol, shared)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "New saved view name")
	cmd.Flags().StringVar(&columns, "columns", "", "New comma-separated column set (replaces the set)")
	cmd.Flags().StringVar(&sortCol, "sort", "", "New sort field")
	cmd.Flags().BoolVar(&shared, "shared", false, "Set the shared flag")
	return cmd
}

func runSavedViewsUpdate(cmd *cobra.Command, identifier, name, columns, sortCol string, shared bool) error {
	changedName := cmd.Flags().Changed("name")
	changedColumns := cmd.Flags().Changed("columns")
	changedSort := cmd.Flags().Changed("sort")
	changedShared := cmd.Flags().Changed("shared")
	if !changedName && !changedColumns && !changedSort && !changedShared {
		return fmt.Errorf("no fields to update. Specify at least one of --name, --columns, --sort, --shared")
	}
	if viper.GetBool("plan") {
		var effects []string
		if changedName {
			effects = append(effects, "name: "+name)
		}
		if changedColumns {
			effects = append(effects, "columns: "+columns)
		}
		if changedSort {
			effects = append(effects, "sort: "+sortCol)
		}
		if changedShared {
			effects = append(effects, fmt.Sprintf("shared: %t", shared))
		}
		return renderPlan(cmd, &plan.Plan{
			Action:     "update",
			Resource:   "saved view",
			Target:     identifier,
			Effects:    effects,
			Reversible: true,
		})
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveSavedView(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	// Read-modify-write: PUT /saved-views/{id} is a full-object replace, and
	// there is no GET-by-id, so read the current object out of the list, apply
	// only the changed fields, then PUT it back wrapped in {savedView}.
	obj, err := fetchSavedView(cmd.Context(), client, id)
	if err != nil {
		return err
	}
	if changedName {
		obj["name"] = name
	}
	if changedColumns {
		obj["columns"] = splitScopes(columns)
	}
	if changedSort {
		config, _ := obj["configuration"].(map[string]any)
		if config == nil {
			config = map[string]any{}
		}
		config["sort"] = sortCol
		obj["configuration"] = config
	}
	if changedShared {
		obj["shared"] = shared
	}
	savedview.StripServerManaged(obj)
	result, err := client.Update(cmd.Context(), "/saved-views/"+id, savedview.PutBody(obj))
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newSavedViewsDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "delete <name-or-id>",
		Aliases:           []string{"rm"},
		Short:             "Delete a saved view",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SavedViewConfig),
		RunE:              batchRunE("saved view", "delete", runSavedViewsDelete),
	}
	addBatchSourceFlags(cmd)
	return cmd
}

func runSavedViewsDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveSavedView(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	// Read the object out of the list for the confirmation/success message
	// (no GET-by-id endpoint).
	name := identifier
	if obj, err := fetchSavedView(cmd.Context(), client, id); err == nil {
		if n, _ := obj["name"].(string); n != "" {
			name = n
		}
	}

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:   "delete",
			Resource: "saved view",
			Target:   fmt.Sprintf("%s (%s)", name, id),
			Effects:  []string{"The saved column/filter/sort preset is removed"},
		})
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete saved view %q? [y/N] ", name)
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
	if _, err := client.Delete(cmd.Context(), "/saved-views/"+id); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Saved view %q deleted successfully.\n", name)
	return nil
}
