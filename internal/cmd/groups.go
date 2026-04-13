package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

// userGroupDefaultFields is the default field subset shown for user group output.
var userGroupDefaultFields = []string{"id", "name", "description", "type"}

// deviceGroupDefaultFields is the default field subset shown for device group output.
var deviceGroupDefaultFields = []string{"id", "name", "description", "type"}

// newV2Client creates a V2 API client. Overridable in tests.
var newV2Client = func() (*api.V2Client, error) {
	return api.NewV2Client()
}

// resolveUserGroup resolves a group name or ID to a JumpCloud user group ID.
func resolveUserGroup(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.UserGroupConfig)
}

// resolveDeviceGroup resolves a group name or ID to a JumpCloud device (system) group ID.
func resolveDeviceGroup(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.DeviceGroupConfig)
}

func newGroupsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "groups",
		Aliases: []string{"g"},
		Short:   "Manage JumpCloud groups",
		Long:    "Manage JumpCloud user groups and device (system) groups.\n\nAliases: g, groups",
	}

	cmd.AddCommand(newGroupsUserCmd())
	cmd.AddCommand(newGroupsDeviceCmd())
	cmd.AddCommand(newGroupsAddMemberCmd())
	cmd.AddCommand(newGroupsRemoveMemberCmd())

	return cmd
}

func newGroupsUserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage user groups",
		Long:  "List, get, create, update, and delete JumpCloud user groups.",
	}

	cmd.AddCommand(newGroupsUserListCmd())
	cmd.AddCommand(newGroupsUserGetCmd())
	cmd.AddCommand(newGroupsUserCreateCmd())
	cmd.AddCommand(newGroupsUserUpdateCmd())
	cmd.AddCommand(newGroupsUserDeleteCmd())

	return cmd
}

func newGroupsUserListCmd() *cobra.Command {
	var (
		limitFlag   int
		sortFlag    string
		filterFlag  []string
		membersFlag bool
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all user groups",
		Long: `List all JumpCloud user groups.

Default fields: id, name, description, type.
Use --output table for a readable ASCII table.
Use --members to include a memberCount field (requires extra API calls).

Filter examples:
  --filter 'name=Engineering'     Exact match
  --filter 'type=custom'          Filter by type`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsUserList(cmd, limitFlag, sortFlag, filterFlag, membersFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -name)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=Engineering')")
	cmd.Flags().BoolVar(&membersFlag, "members", false, "Include memberCount field (extra API call per group)")

	return cmd
}

func runGroupsUserList(cmd *cobra.Command, limit int, sort string, filters []string, members bool) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/usergroups", api.V2ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	data := result.Data
	if members {
		data = enrichWithMemberCount(cmd.Context(), client, data, "/usergroups/%s/members")
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = userGroupDefaultFields
	if members {
		opts.DefaultFields = append(opts.DefaultFields, "memberCount")
	}

	if err := output.WriteList(cmd.OutOrStdout(), data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(data))
	}

	return nil
}

func newGroupsUserGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <group-name-or-id>",
		Short: "Get a user group by name or ID",
		Long: `Get a single JumpCloud user group by name or ID.

Accepts a group name (e.g., "Engineering") or a 24-character hex group ID.
Group names are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserGroupConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsUserGet(cmd, args[0])
		},
	}

	return cmd
}

func runGroupsUserGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveUserGroup(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/usergroups/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newGroupsUserCreateCmd() *cobra.Command {
	var (
		name        string
		description string
		ifNotExists bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new user group",
		Long: `Create a new JumpCloud user group.

Required field: --name.
The newly created group object is returned.

Use --if-not-exists to skip creation when the group name already exists
(returns the existing group instead of failing).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsUserCreate(cmd, name, description, ifNotExists)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Group name (required)")
	cmd.Flags().StringVar(&description, "description", "", "Group description")
	cmd.Flags().BoolVar(&ifNotExists, "if-not-exists", false, "Skip creation if group name already exists (idempotent)")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func runGroupsUserCreate(cmd *cobra.Command, name, description string, ifNotExists bool) error {
	if ifNotExists {
		client, err := newV2Client()
		if err != nil {
			return err
		}
		r := resolve.NewV2Resolver(client)
		id, resolveErr := r.Resolve(cmd.Context(), name, resolve.UserGroupConfig)
		if resolveErr == nil {
			existing, err := client.Get(cmd.Context(), "/usergroups/"+id)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			return output.WriteSingle(cmd.OutOrStdout(), existing, opts)
		}
		// Only proceed to creation if the error is "not found".
		// Surface network errors, ambiguous matches, etc.
		var resolveError *resolve.ResolveError
		if !errors.As(resolveErr, &resolveError) {
			return resolveErr
		}
	}

	if viper.GetBool("plan") {
		effects := []string{"name: " + name}
		if description != "" {
			effects = append(effects, "description: "+description)
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "user group",
			Target:     name,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	body := map[string]string{
		"name": name,
	}
	if description != "" {
		body["description"] = description
	}

	result, err := client.Create(cmd.Context(), "/usergroups", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newGroupsUserUpdateCmd() *cobra.Command {
	var (
		name        string
		description string
	)

	cmd := &cobra.Command{
		Use:   "update <group-name-or-id>",
		Short: "Update a user group",
		Long: `Update an existing JumpCloud user group.

Accepts a group name or 24-character hex group ID.
Specify only the fields you want to change. The updated group object is returned.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserGroupConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsUserUpdate(cmd, args[0], name, description)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Group name")
	cmd.Flags().StringVar(&description, "description", "", "Group description")

	return cmd
}

func runGroupsUserUpdate(cmd *cobra.Command, identifier, name, description string) error {
	body := map[string]string{}

	if cmd.Flags().Changed("name") {
		body["name"] = name
	}
	if cmd.Flags().Changed("description") {
		body["description"] = description
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --name, --description)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, k+": "+v)
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "user group",
			Target:     identifier,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveUserGroup(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/usergroups/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newGroupsUserDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete [group-name-or-id]",
		Aliases: []string{"rm"},
		Short:   "Delete a user group",
		Long: `Delete a JumpCloud user group.

Accepts a group name or 24-character hex group ID.
Shows the group name, member count, and associated resources before prompting
for confirmation. Use --force to skip the confirmation prompt.

Stdin mode:
  Use --stdin to read group names/IDs from stdin (one per line).
  When stdin is piped, --stdin is implied automatically.
  In stdin mode, --force is implied (no confirmation prompts).`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserGroupConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			useStdin, _ := cmd.Flags().GetBool("stdin")
			if useStdin || (len(args) == 0 && isStdinPiped()) {
				return runGroupsUserDeleteStdin(cmd)
			}
			if len(args) == 0 {
				return fmt.Errorf("requires a group name or ID argument (or use --stdin)")
			}
			return runGroupsUserDelete(cmd, args[0])
		},
	}

	cmd.Flags().Bool("stdin", false, "Read group names/IDs from stdin (one per line)")

	return cmd
}

func runGroupsUserDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveUserGroup(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch the group first so we can show details in the confirmation/plan.
	groupData, err := client.Get(cmd.Context(), "/usergroups/"+id)
	if err != nil {
		return err
	}

	var group struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(groupData, &group); err != nil {
		return fmt.Errorf("parsing group data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "user group",
			Target:   fmt.Sprintf("%s (%s)", group.Name, id),
			Effects:  []string{"Remove user group and all memberships"},
		}
		return renderPlan(cmd, p)
	}

	// Confirmation prompt (unless --force is set).
	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete user group %q? [y/N] ", group.Name)
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

	_, err = client.Delete(cmd.Context(), "/usergroups/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "User group %q deleted successfully.\n", group.Name)
	return nil
}

// runGroupsUserDeleteStdin reads group names/IDs from stdin and deletes each one.
func runGroupsUserDeleteStdin(cmd *cobra.Command) error {
	identifiers, err := readLinesFromStdin()
	if err != nil {
		return err
	}

	if len(identifiers) == 0 {
		return nil
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result := runStdinBatch(identifiers, "user group", "Deleting", cmd.ErrOrStderr(), func(identifier string) error {
		id, err := resolveUserGroup(cmd.Context(), client, identifier)
		if err != nil {
			return err
		}
		_, err = client.Delete(cmd.Context(), "/usergroups/"+id)
		return err
	})

	if result.Failed > 0 {
		return fmt.Errorf("%d of %d deletions failed", result.Failed, result.Succeeded+result.Failed)
	}
	return nil
}

// enrichWithMemberCount fetches member counts for each group in parallel and
// injects a "memberCount" field into each group's JSON. The endpointFmt should
// contain a %s placeholder for the group ID (e.g. "/usergroups/%s/members").
func enrichWithMemberCount(ctx context.Context, client *api.V2Client, groups []json.RawMessage, endpointFmt string) []json.RawMessage {
	result := make([]json.RawMessage, len(groups))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10) // bounded concurrency

	for i, raw := range groups {
		var g struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &g); err != nil || g.ID == "" {
			result[i] = raw
			continue
		}

		wg.Add(1)
		go func(idx int, groupID string, rawJSON json.RawMessage) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			endpoint := fmt.Sprintf(endpointFmt, groupID)
			members, err := client.ListAll(ctx, endpoint, api.V2ListOptions{})

			var obj map[string]any
			json.Unmarshal(rawJSON, &obj)
			if err != nil {
				obj["memberCount"] = -1
			} else {
				obj["memberCount"] = len(members.Data)
			}
			enriched, _ := json.Marshal(obj)
			result[idx] = enriched
		}(i, g.ID, raw)
	}

	wg.Wait()
	return result
}

// --- Device Groups ---

func newGroupsDeviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device",
		Short: "Manage device (system) groups",
		Long:  "List, get, create, update, and delete JumpCloud device (system) groups.",
	}

	cmd.AddCommand(newGroupsDeviceListCmd())
	cmd.AddCommand(newGroupsDeviceGetCmd())
	cmd.AddCommand(newGroupsDeviceCreateCmd())
	cmd.AddCommand(newGroupsDeviceUpdateCmd())
	cmd.AddCommand(newGroupsDeviceDeleteCmd())

	return cmd
}

func newGroupsDeviceListCmd() *cobra.Command {
	var (
		limitFlag   int
		sortFlag    string
		filterFlag  []string
		membersFlag bool
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all device groups",
		Long: `List all JumpCloud device (system) groups.

Default fields: id, name, description, type.
Use --output table for a readable ASCII table.
Use --members to include a memberCount field (requires extra API calls).

Filter examples:
  --filter 'name=macOS Fleet'     Exact match
  --filter 'type=custom'          Filter by type`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsDeviceList(cmd, limitFlag, sortFlag, filterFlag, membersFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -name)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=macOS Fleet')")
	cmd.Flags().BoolVar(&membersFlag, "members", false, "Include memberCount field (extra API call per group)")

	return cmd
}

func runGroupsDeviceList(cmd *cobra.Command, limit int, sort string, filters []string, members bool) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/systemgroups", api.V2ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	data := result.Data
	if members {
		data = enrichWithMemberCount(cmd.Context(), client, data, "/systemgroups/%s/membership")
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = deviceGroupDefaultFields
	if members {
		opts.DefaultFields = append(opts.DefaultFields, "memberCount")
	}

	if err := output.WriteList(cmd.OutOrStdout(), data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(data))
	}

	return nil
}

func newGroupsDeviceGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <group-name-or-id>",
		Short: "Get a device group by name or ID",
		Long: `Get a single JumpCloud device (system) group by name or ID.

Accepts a group name (e.g., "macOS Fleet") or a 24-character hex group ID.
Group names are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.DeviceGroupConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsDeviceGet(cmd, args[0])
		},
	}

	return cmd
}

func runGroupsDeviceGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveDeviceGroup(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/systemgroups/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newGroupsDeviceCreateCmd() *cobra.Command {
	var (
		name        string
		description string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new device group",
		Long: `Create a new JumpCloud device (system) group.

Required field: --name.
The newly created group object is returned.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsDeviceCreate(cmd, name, description)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Group name (required)")
	cmd.Flags().StringVar(&description, "description", "", "Group description")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func runGroupsDeviceCreate(cmd *cobra.Command, name, description string) error {
	if viper.GetBool("plan") {
		effects := []string{"name: " + name}
		if description != "" {
			effects = append(effects, "description: "+description)
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "device group",
			Target:     name,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	body := map[string]string{
		"name": name,
	}
	if description != "" {
		body["description"] = description
	}

	result, err := client.Create(cmd.Context(), "/systemgroups", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newGroupsDeviceUpdateCmd() *cobra.Command {
	var (
		name        string
		description string
	)

	cmd := &cobra.Command{
		Use:   "update <group-name-or-id>",
		Short: "Update a device group",
		Long: `Update an existing JumpCloud device (system) group.

Accepts a group name or 24-character hex group ID.
Specify only the fields you want to change. The updated group object is returned.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.DeviceGroupConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsDeviceUpdate(cmd, args[0], name, description)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Group name")
	cmd.Flags().StringVar(&description, "description", "", "Group description")

	return cmd
}

func runGroupsDeviceUpdate(cmd *cobra.Command, identifier, name, description string) error {
	body := map[string]string{}

	if cmd.Flags().Changed("name") {
		body["name"] = name
	}
	if cmd.Flags().Changed("description") {
		body["description"] = description
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --name, --description)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, k+": "+v)
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "device group",
			Target:     identifier,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveDeviceGroup(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/systemgroups/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newGroupsDeviceDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete [group-name-or-id]",
		Aliases: []string{"rm"},
		Short:   "Delete a device group",
		Long: `Delete a JumpCloud device (system) group.

Accepts a group name or 24-character hex group ID.
Shows the group name before prompting for confirmation.
Use --force to skip the confirmation prompt.

Stdin mode:
  Use --stdin to read group names/IDs from stdin (one per line).
  When stdin is piped, --stdin is implied automatically.
  In stdin mode, --force is implied (no confirmation prompts).`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.DeviceGroupConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			useStdin, _ := cmd.Flags().GetBool("stdin")
			if useStdin || (len(args) == 0 && isStdinPiped()) {
				return runGroupsDeviceDeleteStdin(cmd)
			}
			if len(args) == 0 {
				return fmt.Errorf("requires a group name or ID argument (or use --stdin)")
			}
			return runGroupsDeviceDelete(cmd, args[0])
		},
	}

	cmd.Flags().Bool("stdin", false, "Read group names/IDs from stdin (one per line)")

	return cmd
}

func runGroupsDeviceDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveDeviceGroup(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch the group first so we can show details in the confirmation/plan.
	groupData, err := client.Get(cmd.Context(), "/systemgroups/"+id)
	if err != nil {
		return err
	}

	var group struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(groupData, &group); err != nil {
		return fmt.Errorf("parsing group data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "device group",
			Target:   fmt.Sprintf("%s (%s)", group.Name, id),
			Effects:  []string{"Remove device group and all memberships"},
		}
		return renderPlan(cmd, p)
	}

	// Confirmation prompt (unless --force is set).
	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete device group %q? [y/N] ", group.Name)
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

	_, err = client.Delete(cmd.Context(), "/systemgroups/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Device group %q deleted successfully.\n", group.Name)
	return nil
}

// runGroupsDeviceDeleteStdin reads group names/IDs from stdin and deletes each one.
func runGroupsDeviceDeleteStdin(cmd *cobra.Command) error {
	identifiers, err := readLinesFromStdin()
	if err != nil {
		return err
	}

	if len(identifiers) == 0 {
		return nil
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result := runStdinBatch(identifiers, "device group", "Deleting", cmd.ErrOrStderr(), func(identifier string) error {
		id, err := resolveDeviceGroup(cmd.Context(), client, identifier)
		if err != nil {
			return err
		}
		_, err = client.Delete(cmd.Context(), "/systemgroups/"+id)
		return err
	})

	if result.Failed > 0 {
		return fmt.Errorf("%d of %d deletions failed", result.Failed, result.Succeeded+result.Failed)
	}
	return nil
}

// --- Group Membership Management ---

func newGroupsAddMemberCmd() *cobra.Command {
	var (
		userFlag   string
		deviceFlag string
	)

	cmd := &cobra.Command{
		Use:   "add-member <group-name-or-id>",
		Short: "Add a member to a group",
		Long: `Add a user or device to a JumpCloud group.

Use --user to add a user to a user group, or --device to add a device to a device group.
Exactly one of --user or --device must be specified.

Examples:
  jc groups add-member Engineering --user jdoe
  jc groups add-member "macOS Fleet" --device JDOE-MBP`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserGroupConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsAddMember(cmd, args[0], userFlag, deviceFlag)
		},
	}

	cmd.Flags().StringVar(&userFlag, "user", "", "Username or user ID to add to a user group")
	cmd.Flags().StringVar(&deviceFlag, "device", "", "Hostname or device ID to add to a device group")

	return cmd
}

func runGroupsAddMember(cmd *cobra.Command, groupIdentifier, user, device string) error {
	if user == "" && device == "" {
		return fmt.Errorf("specify --user or --device")
	}
	if user != "" && device != "" {
		return fmt.Errorf("specify only one of --user or --device, not both")
	}

	if viper.GetBool("plan") {
		memberType := "user"
		memberID := user
		if device != "" {
			memberType = "device"
			memberID = device
		}
		p := &plan.Plan{
			Action:     "add member",
			Resource:   memberType + " group",
			Target:     groupIdentifier,
			Effects:    []string{fmt.Sprintf("Add %s %s to group", memberType, memberID)},
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	v2Client, err := newV2Client()
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	if user != "" {
		return addUserToGroup(ctx, cmd, v2Client, groupIdentifier, user)
	}
	return addDeviceToGroup(ctx, cmd, v2Client, groupIdentifier, device)
}

func addUserToGroup(ctx context.Context, cmd *cobra.Command, v2Client *api.V2Client, groupIdentifier, userIdentifier string) error {
	groupID, err := resolveUserGroup(ctx, v2Client, groupIdentifier)
	if err != nil {
		return fmt.Errorf("resolving group: %w", err)
	}

	// Resolve the user via V1 API.
	v1Client, err := newV1Client()
	if err != nil {
		return err
	}
	userID, err := resolveUser(ctx, v1Client, userIdentifier)
	if err != nil {
		return fmt.Errorf("resolving user: %w", err)
	}

	body := map[string]string{
		"op":   "add",
		"type": "user",
		"id":   userID,
	}

	_, err = v2Client.Create(ctx, "/usergroups/"+groupID+"/members", body)
	if err != nil {
		// JumpCloud returns 409 Conflict when member already exists.
		if apiErr, ok := asAPIError(err); ok && apiErr.StatusCode == 409 {
			fmt.Fprintf(cmd.OutOrStdout(), "User %q is already a member of group %q.\n", userIdentifier, groupIdentifier)
			return nil
		}
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Added user %q to group %q.\n", userIdentifier, groupIdentifier)
	return nil
}

func addDeviceToGroup(ctx context.Context, cmd *cobra.Command, v2Client *api.V2Client, groupIdentifier, deviceIdentifier string) error {
	groupID, err := resolveDeviceGroup(ctx, v2Client, groupIdentifier)
	if err != nil {
		return fmt.Errorf("resolving group: %w", err)
	}

	// Resolve the device via V1 API.
	v1Client, err := newV1Client()
	if err != nil {
		return err
	}
	deviceID, err := resolveDevice(ctx, v1Client, deviceIdentifier)
	if err != nil {
		return fmt.Errorf("resolving device: %w", err)
	}

	body := map[string]string{
		"op":   "add",
		"type": "system",
		"id":   deviceID,
	}

	_, err = v2Client.Create(ctx, "/systemgroups/"+groupID+"/membership", body)
	if err != nil {
		if apiErr, ok := asAPIError(err); ok && apiErr.StatusCode == 409 {
			fmt.Fprintf(cmd.OutOrStdout(), "Device %q is already a member of group %q.\n", deviceIdentifier, groupIdentifier)
			return nil
		}
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Added device %q to group %q.\n", deviceIdentifier, groupIdentifier)
	return nil
}

func newGroupsRemoveMemberCmd() *cobra.Command {
	var (
		userFlag   string
		deviceFlag string
		allFlag    bool
	)

	cmd := &cobra.Command{
		Use:   "remove-member <group-name-or-id>",
		Short: "Remove a member from a group",
		Long: `Remove a user or device from a JumpCloud group.

Use --user to remove a user from a user group, or --device to remove a device from a device group.
Exactly one of --user or --device must be specified.

Use --all with --user to remove the user from ALL user groups.
When --all is used, the positional group argument is not required.

Examples:
  jc groups remove-member Engineering --user jdoe
  jc groups remove-member "macOS Fleet" --device JDOE-MBP
  jc groups remove-member --all --user jdoe`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserGroupConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			if allFlag {
				if deviceFlag != "" {
					return fmt.Errorf("--all is only supported with --user")
				}
				if userFlag == "" {
					return fmt.Errorf("--all requires --user")
				}
				return runGroupsRemoveUserFromAll(cmd, userFlag)
			}
			if len(args) == 0 {
				return fmt.Errorf("requires a group name or ID argument (or use --all)")
			}
			return runGroupsRemoveMember(cmd, args[0], userFlag, deviceFlag)
		},
	}

	cmd.Flags().StringVar(&userFlag, "user", "", "Username or user ID to remove from a user group")
	cmd.Flags().StringVar(&deviceFlag, "device", "", "Hostname or device ID to remove from a device group")
	cmd.Flags().BoolVar(&allFlag, "all", false, "Remove user from ALL groups (requires --user)")

	return cmd
}

func runGroupsRemoveMember(cmd *cobra.Command, groupIdentifier, user, device string) error {
	if user == "" && device == "" {
		return fmt.Errorf("specify --user or --device")
	}
	if user != "" && device != "" {
		return fmt.Errorf("specify only one of --user or --device, not both")
	}

	if viper.GetBool("plan") {
		memberType := "user"
		memberID := user
		if device != "" {
			memberType = "device"
			memberID = device
		}
		p := &plan.Plan{
			Action:     "remove member",
			Resource:   memberType + " group",
			Target:     groupIdentifier,
			Effects:    []string{fmt.Sprintf("Remove %s %s from group", memberType, memberID)},
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	v2Client, err := newV2Client()
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	if user != "" {
		return removeUserFromGroup(ctx, cmd, v2Client, groupIdentifier, user)
	}
	return removeDeviceFromGroup(ctx, cmd, v2Client, groupIdentifier, device)
}

func removeUserFromGroup(ctx context.Context, cmd *cobra.Command, v2Client *api.V2Client, groupIdentifier, userIdentifier string) error {
	groupID, err := resolveUserGroup(ctx, v2Client, groupIdentifier)
	if err != nil {
		return fmt.Errorf("resolving group: %w", err)
	}

	v1Client, err := newV1Client()
	if err != nil {
		return err
	}
	userID, err := resolveUser(ctx, v1Client, userIdentifier)
	if err != nil {
		return fmt.Errorf("resolving user: %w", err)
	}

	body := map[string]string{
		"op":   "remove",
		"type": "user",
		"id":   userID,
	}

	_, err = v2Client.Create(ctx, "/usergroups/"+groupID+"/members", body)
	if err != nil {
		// Not a member — treat as informative, not error.
		if apiErr, ok := asAPIError(err); ok && apiErr.StatusCode == 404 {
			fmt.Fprintf(cmd.OutOrStdout(), "User %q is not a member of group %q.\n", userIdentifier, groupIdentifier)
			return nil
		}
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Removed user %q from group %q.\n", userIdentifier, groupIdentifier)
	return nil
}

func removeDeviceFromGroup(ctx context.Context, cmd *cobra.Command, v2Client *api.V2Client, groupIdentifier, deviceIdentifier string) error {
	groupID, err := resolveDeviceGroup(ctx, v2Client, groupIdentifier)
	if err != nil {
		return fmt.Errorf("resolving group: %w", err)
	}

	v1Client, err := newV1Client()
	if err != nil {
		return err
	}
	deviceID, err := resolveDevice(ctx, v1Client, deviceIdentifier)
	if err != nil {
		return fmt.Errorf("resolving device: %w", err)
	}

	body := map[string]string{
		"op":   "remove",
		"type": "system",
		"id":   deviceID,
	}

	_, err = v2Client.Create(ctx, "/systemgroups/"+groupID+"/membership", body)
	if err != nil {
		if apiErr, ok := asAPIError(err); ok && apiErr.StatusCode == 404 {
			fmt.Fprintf(cmd.OutOrStdout(), "Device %q is not a member of group %q.\n", deviceIdentifier, groupIdentifier)
			return nil
		}
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Removed device %q from group %q.\n", deviceIdentifier, groupIdentifier)
	return nil
}

func runGroupsRemoveUserFromAll(cmd *cobra.Command, userIdentifier string) error {
	v2Client, err := newV2Client()
	if err != nil {
		return err
	}

	v1Client, err := newV1Client()
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	userID, err := resolveUser(ctx, v1Client, userIdentifier)
	if err != nil {
		return fmt.Errorf("resolving user: %w", err)
	}

	// List all user groups, then remove the user from each.
	result, err := v2Client.ListAll(ctx, "/usergroups", api.V2ListOptions{})
	if err != nil {
		return err
	}

	var removed int
	for _, raw := range result.Data {
		var g struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &g); err != nil {
			continue
		}

		body := map[string]string{
			"op":   "remove",
			"type": "user",
			"id":   userID,
		}

		_, err := v2Client.Create(ctx, "/usergroups/"+g.ID+"/members", body)
		if err != nil {
			// 404 means not a member — skip silently.
			if apiErr, ok := asAPIError(err); ok && apiErr.StatusCode == 404 {
				continue
			}
			return fmt.Errorf("removing from group %q: %w", g.Name, err)
		}
		removed++
		fmt.Fprintf(cmd.OutOrStdout(), "Removed user %q from group %q.\n", userIdentifier, g.Name)
	}

	if removed == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "User %q was not a member of any groups.\n", userIdentifier)
	}

	return nil
}

// asAPIError checks if an error is an *api.APIError and returns it.
func asAPIError(err error) (*api.APIError, bool) {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}
