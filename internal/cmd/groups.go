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
		Use:   "groups",
		Short: "Manage JumpCloud groups",
		Long:  "Manage JumpCloud user groups and device (system) groups.",
	}

	cmd.AddCommand(newGroupsUserCmd())
	cmd.AddCommand(newGroupsDeviceCmd())

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
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all user groups",
		Long: `List all JumpCloud user groups.

Default fields: id, name, description, type.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'name=Engineering'     Exact match
  --filter 'type=custom'          Filter by type`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsUserList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -name)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=Engineering')")

	return cmd
}

func runGroupsUserList(cmd *cobra.Command, limit int, sort string, filters []string) error {
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

	opts := output.CurrentOptions()
	opts.DefaultFields = userGroupDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
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
		Args: cobra.ExactArgs(1),
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
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new user group",
		Long: `Create a new JumpCloud user group.

Required field: --name.
The newly created group object is returned.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsUserCreate(cmd, name, description)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Group name (required)")
	cmd.Flags().StringVar(&description, "description", "", "Group description")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func runGroupsUserCreate(cmd *cobra.Command, name, description string) error {
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
		Args: cobra.ExactArgs(1),
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
		Use:   "delete <group-name-or-id>",
		Short: "Delete a user group",
		Long: `Delete a JumpCloud user group.

Accepts a group name or 24-character hex group ID.
Shows the group name, member count, and associated resources before prompting
for confirmation. Use --force to skip the confirmation prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsUserDelete(cmd, args[0])
		},
	}

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

	// Fetch the group first so we can show details in the confirmation prompt.
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

	// Confirmation prompt (unless --force is set).
	if !viper.GetBool("force") {
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
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all device groups",
		Long: `List all JumpCloud device (system) groups.

Default fields: id, name, description, type.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'name=macOS Fleet'     Exact match
  --filter 'type=custom'          Filter by type`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsDeviceList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -name)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=macOS Fleet')")

	return cmd
}

func runGroupsDeviceList(cmd *cobra.Command, limit int, sort string, filters []string) error {
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

	opts := output.CurrentOptions()
	opts.DefaultFields = deviceGroupDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
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
		Args: cobra.ExactArgs(1),
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
		Args: cobra.ExactArgs(1),
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
		Use:   "delete <group-name-or-id>",
		Short: "Delete a device group",
		Long: `Delete a JumpCloud device (system) group.

Accepts a group name or 24-character hex group ID.
Shows the group name before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupsDeviceDelete(cmd, args[0])
		},
	}

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

	// Fetch the group first so we can show details in the confirmation prompt.
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

	// Confirmation prompt (unless --force is set).
	if !viper.GetBool("force") {
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
