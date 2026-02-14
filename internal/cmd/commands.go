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
		Long:  "List, get, create, update, delete, run, and view results for JumpCloud commands.",
	}

	cmd.AddCommand(newCommandsListCmd())
	cmd.AddCommand(newCommandsGetCmd())
	cmd.AddCommand(newCommandsCreateCmd())
	cmd.AddCommand(newCommandsUpdateCmd())
	cmd.AddCommand(newCommandsDeleteCmd())
	cmd.AddCommand(newCommandsRunCmd())
	cmd.AddCommand(newCommandsResultsCmd())

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

// commandResultDefaultFields is the default field subset shown for command results.
var commandResultDefaultFields = []string{"system", "exitCode", "requestTime", "responseTime", "stdout", "stderr"}

func newCommandsRunCmd() *cobra.Command {
	var onFlag string

	cmd := &cobra.Command{
		Use:   "run <command-name-or-id>",
		Short: "Trigger command execution on a device or device group",
		Long: `Trigger execution of a JumpCloud command on a specific device or device group.

Accepts a command name or 24-character hex command ID.
The --on flag specifies the target device (hostname or ID) or device group (name or ID).

Examples:
  jc commands run "Update Agents" --on JDOE-MBP
  jc commands run "Update Agents" --on "macOS Fleet"
  jc commands run aaa111aaa111aaa111aaa111 --on bbb222bbb222bbb222bbb222 --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCommandsRun(cmd, args[0], onFlag)
		},
	}

	cmd.Flags().StringVar(&onFlag, "on", "", "Target device (hostname or ID) or device group (name or ID) (required)")
	_ = cmd.MarkFlagRequired("on")

	return cmd
}

func runCommandsRun(cmd *cobra.Command, commandIdentifier, onTarget string) error {
	v1Client, err := newV1Client()
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	commandID, err := resolveCommand(ctx, v1Client, commandIdentifier)
	if err != nil {
		return err
	}

	// Resolve the target: try device first, then device group.
	// When the target is already an ID (24-char hex), the resolver returns it
	// immediately without verifying it exists. We validate with a GET call so
	// that a device-group ID correctly falls through to V2 resolution.
	var systemIDs []string
	var systemGroupIDs []string
	var targetDesc string

	deviceResolved := false
	deviceID, deviceErr := resolveDevice(ctx, v1Client, onTarget)
	if deviceErr == nil {
		// If this was a raw ID, verify the device actually exists.
		if resolve.IsID(onTarget) {
			_, verifyErr := v1Client.Get(ctx, "/systems/"+deviceID)
			if verifyErr == nil {
				deviceResolved = true
			}
		} else {
			deviceResolved = true
		}
	}

	if deviceResolved {
		systemIDs = []string{deviceID}
		targetDesc = fmt.Sprintf("1 device (%s)", onTarget)
	} else {
		// Device resolution failed — try device group via V2 API.
		v2Client, err := newV2Client()
		if err != nil {
			return err
		}
		groupID, groupErr := resolveDeviceGroup(ctx, v2Client, onTarget)
		if groupErr != nil {
			return fmt.Errorf("could not resolve %q as a device or device group", onTarget)
		}
		systemGroupIDs = []string{groupID}
		targetDesc = fmt.Sprintf("device group %q", onTarget)
	}

	// Confirmation prompt (unless --force is set).
	if !viper.GetBool("force") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Run command %q on %s? [y/N] ", commandIdentifier, targetDesc)
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

	// Build the trigger request body.
	body := map[string]any{
		"command": commandID,
	}
	if len(systemIDs) > 0 {
		body["systems"] = systemIDs
	}
	if len(systemGroupIDs) > 0 {
		body["systemGroups"] = systemGroupIDs
	}

	_, err = v1Client.Post(ctx, "/runcommand", body)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Command %q triggered on %s.\n", commandIdentifier, targetDesc)
	return nil
}

func newCommandsResultsCmd() *cobra.Command {
	var (
		limitFlag int
		sortFlag  string
	)

	cmd := &cobra.Command{
		Use:   "results <command-name-or-id>",
		Short: "List execution results for a command",
		Long: `List execution results for a JumpCloud command.

Accepts a command name or 24-character hex command ID.
Results show the device, exit code, stdout, stderr, and timestamp for each execution.

Default fields: system, exitCode, requestTime, responseTime, stdout, stderr.
Use --output table for quick scanning of results.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCommandsResults(cmd, args[0], limitFlag, sortFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -requestTime)")

	return cmd
}

func runCommandsResults(cmd *cobra.Command, commandIdentifier string, limit int, sort string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	commandID, err := resolveCommand(ctx, client, commandIdentifier)
	if err != nil {
		return err
	}

	// Fetch command results filtered by command ID.
	result, err := client.ListAll(ctx, "/commandresults", api.ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: []string{"command:$eq:" + commandID},
	})
	if err != nil {
		return err
	}

	// Flatten nested response fields for readability.
	flattened := flattenCommandResults(result.Data)

	opts := output.CurrentOptions()
	opts.DefaultFields = commandResultDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), flattened, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(flattened), result.TotalCount)
	}

	return nil
}

// flattenCommandResults extracts nested fields (response.data.output, response.error,
// system, systemId) into top-level fields for display.
func flattenCommandResults(data []json.RawMessage) []json.RawMessage {
	flattened := make([]json.RawMessage, 0, len(data))
	for _, raw := range data {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			flattened = append(flattened, raw)
			continue
		}

		// Extract response.data.output → stdout.
		// Extract response.error → stderr.
		if respRaw, ok := obj["response"]; ok {
			var resp struct {
				Data  *struct{ Output string } `json:"data"`
				Error string                   `json:"error"`
			}
			if err := json.Unmarshal(respRaw, &resp); err == nil {
				if resp.Data != nil {
					stdoutJSON, _ := json.Marshal(resp.Data.Output)
					obj["stdout"] = stdoutJSON
				}
				if resp.Error != "" {
					stderrJSON, _ := json.Marshal(resp.Error)
					obj["stderr"] = stderrJSON
				}
			}
		}

		out, err := json.Marshal(obj)
		if err != nil {
			flattened = append(flattened, raw)
			continue
		}
		flattened = append(flattened, out)
	}
	return flattened
}
