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

// commandDefaultFields is the default field subset shown for command list/table output.
var commandDefaultFields = []string{"name", "commandType", "command", "schedule", "scheduleRepeatType"}

// resolveCommand resolves a command name or ID to a JumpCloud command ID.
func resolveCommand(ctx context.Context, client *api.V1Client, identifier string) (string, error) {
	r := resolve.NewResolver(client)
	return r.Resolve(ctx, identifier, resolve.CommandConfig)
}

func newCommandsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "commands",
		Short: "Manage JumpCloud commands",
		Long:  "List, get, create, update, and delete JumpCloud commands.",
	}

	cmd.AddCommand(newCommandsListCmd())
	cmd.AddCommand(newCommandsGetCmd())
	cmd.AddCommand(newCommandsCreateCmd())
	cmd.AddCommand(newCommandsUpdateCmd())
	cmd.AddCommand(newCommandsDeleteCmd())

	return cmd
}

func newCommandsListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
		searchFlag string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all commands",
		Long: `List all JumpCloud commands.

Default fields: name, commandType, command, schedule, scheduleRepeatType.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'commandType=linux'              Exact match
  --filter 'name=Update Agents'             Filter by name`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCommandsList(cmd, limitFlag, sortFlag, filterFlag, searchFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -name)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'field=value', 'field!=value')")
	cmd.Flags().StringVar(&searchFlag, "search", "", "Full-text search across fields")

	return cmd
}

func runCommandsList(cmd *cobra.Command, limit int, sort string, filters []string, search string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/commands", api.ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV1Queries(exprs),
		Search: search,
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = commandDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(result.Data), result.TotalCount)
	}

	return nil
}

func newCommandsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <command-name-or-id>",
		Short: "Get a command by name or ID",
		Long: `Get a single JumpCloud command by name or ID.

Accepts a command name (e.g., "Update Agents") or a 24-character hex command ID.
Command names are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCommandsGet(cmd, args[0])
		},
	}

	return cmd
}

func runCommandsGet(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveCommand(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/commands/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newCommandsCreateCmd() *cobra.Command {
	var (
		name        string
		commandBody string
		commandType string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new command",
		Long: `Create a new JumpCloud command.

Required fields: --name, --command, and --type.
The newly created command object is returned.

Supported types: linux, mac, windows.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCommandsCreate(cmd, name, commandBody, commandType)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Command name (required)")
	cmd.Flags().StringVar(&commandBody, "command", "", "Command body to execute (required)")
	cmd.Flags().StringVar(&commandType, "type", "", "Command type: linux, mac, windows (required)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("command")
	_ = cmd.MarkFlagRequired("type")

	return cmd
}

func runCommandsCreate(cmd *cobra.Command, name, commandBody, commandType string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	body := map[string]string{
		"name":        name,
		"command":     commandBody,
		"commandType": commandType,
	}

	result, err := client.Create(cmd.Context(), "/commands", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newCommandsUpdateCmd() *cobra.Command {
	var (
		name        string
		commandBody string
		commandType string
	)

	cmd := &cobra.Command{
		Use:   "update <command-name-or-id>",
		Short: "Update a command",
		Long: `Update an existing JumpCloud command.

Accepts a command name or 24-character hex command ID.
Specify only the fields you want to change. The updated command object is returned.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCommandsUpdate(cmd, args[0], name, commandBody, commandType)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Command name")
	cmd.Flags().StringVar(&commandBody, "command", "", "Command body to execute")
	cmd.Flags().StringVar(&commandType, "type", "", "Command type: linux, mac, windows")

	return cmd
}

func runCommandsUpdate(cmd *cobra.Command, identifier, name, commandBody, commandType string) error {
	body := map[string]string{}

	if cmd.Flags().Changed("name") {
		body["name"] = name
	}
	if cmd.Flags().Changed("command") {
		body["command"] = commandBody
	}
	if cmd.Flags().Changed("type") {
		body["commandType"] = commandType
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --name, --command, --type)")
	}

	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveCommand(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/commands/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newCommandsDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <command-name-or-id>",
		Short: "Delete a command",
		Long: `Delete a JumpCloud command.

Accepts a command name or 24-character hex command ID.
Shows the command name before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCommandsDelete(cmd, args[0])
		},
	}

	return cmd
}

func runCommandsDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveCommand(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch the command first so we can show details in the confirmation prompt.
	cmdData, err := client.Get(cmd.Context(), "/commands/"+id)
	if err != nil {
		return err
	}

	var jcCmd struct {
		Name        string `json:"name"`
		CommandType string `json:"commandType"`
	}
	if err := json.Unmarshal(cmdData, &jcCmd); err != nil {
		return fmt.Errorf("parsing command data: %w", err)
	}

	// Confirmation prompt (unless --force is set).
	if !viper.GetBool("force") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete command %q (%s)? [y/N] ", jcCmd.Name, jcCmd.CommandType)
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

	_, err = client.Delete(cmd.Context(), "/commands/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Command %q deleted successfully.\n", jcCmd.Name)
	return nil
}
