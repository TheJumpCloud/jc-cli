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
)

// resolveDevice resolves a hostname or ID to a JumpCloud device (system) ID.
func resolveDevice(ctx context.Context, client *api.V1Client, identifier string) (string, error) {
	r := resolve.NewResolver(client)
	return r.Resolve(ctx, identifier, resolve.DeviceConfig)
}

// deviceDefaultFields is the default field subset shown for device list/table output.
var deviceDefaultFields = []string{"displayName", "hostname", "os", "osVersion", "lastContact", "agentVersion"}

func newDevicesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "devices",
		Aliases: []string{"d"},
		Short:   "Manage JumpCloud devices",
		Long:    "List, get, update, delete, search, and send MDM commands to JumpCloud systems (devices).\n\nAliases: d, devices",
	}

	cmd.AddCommand(newDevicesListCmd())
	cmd.AddCommand(newDevicesGetCmd())
	cmd.AddCommand(newDevicesUpdateCmd())
	cmd.AddCommand(newDevicesDeleteCmd())
	cmd.AddCommand(newDevicesSearchCmd())
	cmd.AddCommand(newDevicesLockCmd())
	cmd.AddCommand(newDevicesRestartCmd())
	cmd.AddCommand(newDevicesEraseCmd())
	cmd.AddCommand(newDevicesFDEKeyCmd())

	return cmd
}

func newDevicesListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
		searchFlag string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all devices",
		Long: `List all JumpCloud systems (devices).

Default fields: displayName, hostname, os, osVersion, lastContact, agentVersion.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'os=Mac OS X'                  Exact match
  --filter 'active!=true'                 Inequality
  --filter 'lastContact>=2026-01-01'      Greater than or equal
  --filter 'os=Mac OS X' --filter 'active=true'   Multiple filters (AND)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevicesList(cmd, limitFlag, sortFlag, filterFlag, searchFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -lastContact)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'field=value', 'field!=value', 'field>=value')")
	cmd.Flags().StringVar(&searchFlag, "search", "", "Full-text search across fields")

	return cmd
}

func runDevicesList(cmd *cobra.Command, limit int, sort string, filters []string, search string) error {
	// Parse and validate filter expressions.
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/systems", api.ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV1Queries(exprs),
		Search: search,
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = deviceDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(result.Data), result.TotalCount)
	}

	return nil
}

func newDevicesGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <hostname-or-id>",
		Short: "Get a device by hostname or ID",
		Long: `Get a single JumpCloud system (device) by hostname or ID.

Accepts a hostname (e.g., "JDOE-MBP") or a 24-character hex system ID.
Hostnames are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args:               cobra.ExactArgs(1),
		ValidArgsFunction:  completeResourceNames(resolve.DeviceConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevicesGet(cmd, args[0])
		},
	}

	return cmd
}

func runDevicesGet(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveDevice(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/systems/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newDevicesDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete [hostname-or-id]",
		Aliases: []string{"rm"},
		Short:   "Delete a device",
		Long: `Delete a JumpCloud system (device).

Accepts a hostname or 24-character hex system ID.
Shows the device's hostname, OS, and last contact date before prompting for
confirmation. Use --force to skip the confirmation prompt.

Stdin mode:
  Use --from-file <path> or --stdin to read hostnames/IDs (one per
  line; blank lines and # comments ignored). Batch execution requires
  --force or --non-interactive; preview with --plan first.

  jc devices list --filter 'active!=true' --ids | jc devices delete --force
  jc devices delete --from-file stale-devices.txt --plan`,
		Args:               cobra.MaximumNArgs(1),
		ValidArgsFunction:  completeResourceNames(resolve.DeviceConfig),
		RunE:               batchRunE("device", "delete", runDevicesDelete),
	}

	addBatchSourceFlags(cmd)

	return cmd
}

func runDevicesDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveDevice(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch the device first so we can show details in the confirmation/plan.
	deviceData, err := client.Get(cmd.Context(), "/systems/"+id)
	if err != nil {
		return err
	}

	var device struct {
		Hostname    string `json:"hostname"`
		OS          string `json:"os"`
		LastContact string `json:"lastContact"`
	}
	if err := json.Unmarshal(deviceData, &device); err != nil {
		return fmt.Errorf("parsing device data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "device",
			Target:   fmt.Sprintf("%s (%s)", device.Hostname, id),
			Effects:  []string{"Remove device record from JumpCloud"},
		}
		return renderPlan(cmd, p)
	}

	// Confirmation prompt (unless --force is set).
	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		lastContact := device.LastContact
		if idx := strings.Index(lastContact, "T"); idx > 0 {
			lastContact = lastContact[:idx]
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete device %s (%s, last contact %s)? [y/N] ",
			device.Hostname, device.OS, lastContact)
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

	_, err = client.Delete(cmd.Context(), "/systems/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Device %s deleted successfully.\n", device.Hostname)
	return nil
}

// runDevicesDeleteStdin reads hostnames/IDs from stdin and deletes each one.

func newDevicesUpdateCmd() *cobra.Command {
	var (
		displayName                    string
		allowSshPasswordAuthentication bool
		allowMultiFactorAuthentication bool
		allowPublicKeyAuthentication   bool
	)

	cmd := &cobra.Command{
		Use:   "update <hostname-or-id>",
		Short: "Update a device",
		Long: `Update an existing JumpCloud device (system).

Accepts a hostname or 24-character hex system ID.
Specify only the fields you want to change. The updated device object is returned.`,
		Args:               cobra.ExactArgs(1),
		ValidArgsFunction:  completeResourceNames(resolve.DeviceConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevicesUpdate(cmd, args[0], displayName, allowSshPasswordAuthentication, allowMultiFactorAuthentication, allowPublicKeyAuthentication)
		},
	}

	cmd.Flags().StringVar(&displayName, "displayName", "", "Device display name")
	cmd.Flags().BoolVar(&allowSshPasswordAuthentication, "allowSshPasswordAuthentication", false, "Allow SSH password authentication")
	cmd.Flags().BoolVar(&allowMultiFactorAuthentication, "allowMultiFactorAuthentication", false, "Allow multi-factor authentication")
	cmd.Flags().BoolVar(&allowPublicKeyAuthentication, "allowPublicKeyAuthentication", false, "Allow public key authentication")

	return cmd
}

func runDevicesUpdate(cmd *cobra.Command, identifier, displayName string, allowSshPwd, allowMFA, allowPubKey bool) error {
	body := map[string]any{}

	if cmd.Flags().Changed("displayName") {
		body["displayName"] = displayName
	}
	if cmd.Flags().Changed("allowSshPasswordAuthentication") {
		body["allowSshPasswordAuthentication"] = allowSshPwd
	}
	if cmd.Flags().Changed("allowMultiFactorAuthentication") {
		body["allowMultiFactorAuthentication"] = allowMFA
	}
	if cmd.Flags().Changed("allowPublicKeyAuthentication") {
		body["allowPublicKeyAuthentication"] = allowPubKey
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --displayName)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "device",
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

	id, err := resolveDevice(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/systems/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newDevicesSearchCmd() *cobra.Command {
	var (
		limitFlag int
		sortFlag  string
	)

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search devices",
		Long: `Search JumpCloud systems (devices) using V1 POST search.

Performs a server-side search across device fields.
Mirrors the user search pattern using POST /search/systems.

Examples:
  jc devices search macbook
  jc devices search "alice-mbp" --limit 10`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevicesSearch(cmd, args[0], limitFlag, sortFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")

	return cmd
}

func runDevicesSearch(cmd *cobra.Command, query string, limit int, sort string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	searchBody := map[string]any{
		"searchFilter": map[string]any{
			"searchTerm": query,
		},
	}

	result, err := client.Search(cmd.Context(), "/search/systems", searchBody, api.SearchOptions{
		Limit: limit,
		Sort:  sort,
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = deviceDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(result.Data), result.TotalCount)
	}

	return nil
}

func newDevicesLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock <hostname-or-id>",
		Short: "Send MDM lock command to a device",
		Long:  "Send an MDM lock command to a JumpCloud device. Accepts a hostname or ID. The device will be locked remotely.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.DeviceConfig),
		RunE: batchRunE("device", "lock", func(cmd *cobra.Command, identifier string) error {
			return runDevicesMDMCommand(cmd, identifier, "lock")
		}),
	}
	addBatchSourceFlags(cmd)
	return cmd
}

func newDevicesRestartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restart <hostname-or-id>",
		Short: "Send MDM restart command to a device",
		Long:  "Send an MDM restart command to a JumpCloud device. Accepts a hostname or ID. The device will be restarted remotely.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.DeviceConfig),
		RunE: batchRunE("device", "restart", func(cmd *cobra.Command, identifier string) error {
			return runDevicesMDMCommand(cmd, identifier, "restart")
		}),
	}
	addBatchSourceFlags(cmd)
	return cmd
}

func newDevicesEraseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "erase <hostname-or-id>",
		Short: "Send MDM erase command to a device",
		Long: `Send an MDM erase command to a JumpCloud device.

Accepts a hostname or 24-character hex system ID.
WARNING: This will WIPE ALL DATA on the device. This action is irreversible.
The --confirm-erase flag is REQUIRED as a safety measure.`,
		Args:               cobra.MaximumNArgs(1),
		ValidArgsFunction:  completeResourceNames(resolve.DeviceConfig),
		RunE: batchRunE("device", "erase", func(cmd *cobra.Command, identifier string) error {
			confirmErase, _ := cmd.Flags().GetBool("confirm-erase")
			if !confirmErase {
				return fmt.Errorf("device erase is extremely destructive and irreversible. You must pass --confirm-erase to proceed")
			}
			return runDevicesMDMCommand(cmd, identifier, "erase")
		}),
	}
	cmd.Flags().Bool("confirm-erase", false, "Required safety flag to confirm device erase")
	addBatchSourceFlags(cmd)
	return cmd
}

// runDevicesMDMCommand sends an MDM command (lock, restart, erase) to a device.
func runDevicesMDMCommand(cmd *cobra.Command, identifier string, action string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveDevice(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch device to get hostname for confirmation/plan message.
	deviceData, err := client.Get(cmd.Context(), "/systems/"+id)
	if err != nil {
		return err
	}

	var device struct {
		Hostname string `json:"hostname"`
	}
	if err := json.Unmarshal(deviceData, &device); err != nil {
		return fmt.Errorf("parsing device data: %w", err)
	}

	if viper.GetBool("plan") {
		reversible := action != "erase"
		p := &plan.Plan{
			Action:     action,
			Resource:   "device",
			Target:     fmt.Sprintf("%s (%s)", device.Hostname, id),
			Effects:    []string{fmt.Sprintf("Send MDM %s command", action)},
			Reversible: reversible,
		}
		return renderPlan(cmd, p)
	}

	// Confirmation prompt (unless --force is set).
	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		promptVerb := strings.ToUpper(action[:1]) + action[1:]
		if action == "erase" {
			promptVerb = "ERASE (wipe all data on)"
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "%s device %s? [y/N] ", promptVerb, device.Hostname)
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

	_, err = client.Post(cmd.Context(), "/systems/"+id+"/command/builtin/"+action, nil)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Device %s %s command sent successfully.\n", device.Hostname, action)
	return nil
}

func newDevicesFDEKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fde-key <hostname-or-id>",
		Short: "Retrieve the FDE recovery key for a device",
		Long: `Retrieve the Full Disk Encryption (FileVault/BitLocker) recovery key for a device.

Accepts a hostname or 24-character hex system ID.
The recovery key is returned as a JSON object with a "key" field.`,
		Args:               cobra.ExactArgs(1),
		ValidArgsFunction:  completeResourceNames(resolve.DeviceConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevicesFDEKey(cmd, args[0])
		},
	}
}

func runDevicesFDEKey(cmd *cobra.Command, identifier string) error {
	// Resolve device using V1 client.
	v1Client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveDevice(cmd.Context(), v1Client, identifier)
	if err != nil {
		return err
	}

	// FDE key endpoint is V2.
	v2Client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := v2Client.Get(cmd.Context(), "/systems/"+id+"/fdekey")
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}
