package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
)

// validSystemInsightsTables lists all supported System Insights osquery table names.
var validSystemInsightsTables = []string{
	"alf", "alf_exceptions", "alf_explicit_auths", "apps", "authorized_keys",
	"azure_instance_metadata", "azure_instance_tags", "battery", "bitlocker_info",
	"browser_plugins", "certificates", "chassis_info", "chrome_extensions",
	"connectivity", "crashes", "cups_destinations", "disk_encryption", "disk_info",
	"dns_resolvers", "etc_hosts", "firefox_addons", "groups",
	"ie_extensions", "interface_addresses", "interface_details", "kernel_info",
	"launchd", "linux_packages", "logged_in_users", "logical_drives",
	"managed_policies", "mounts", "os_version", "patches", "programs",
	"python_packages", "safari_extensions", "scheduled_tasks", "secureboot",
	"services", "shadow", "shared_folders", "shared_resources",
	"sharing_preferences", "sip_config", "startup_items", "system_controls",
	"system_info", "tpm_info", "uptime", "usb_devices", "user_assist",
	"user_groups", "user_ssh_keys", "users", "wifi_networks", "wifi_status",
	"windows_security_center", "windows_security_products",
}

func newSystemInsightsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system-insights",
		Short: "Query System Insights osquery tables",
		Long:  "Query JumpCloud System Insights data across 60+ osquery tables. Use the 'tables' subcommand to list available table names.",
	}

	cmd.AddCommand(newSystemInsightsListCmd())
	cmd.AddCommand(newSystemInsightsTablesCmd())

	return cmd
}

func newSystemInsightsListCmd() *cobra.Command {
	var (
		limitFlag    int
		sortFlag     string
		filterFlag   []string
		systemIDFlag string
	)

	cmd := &cobra.Command{
		Use:   "list <table>",
		Short: "Query a System Insights table",
		Long: `Query a JumpCloud System Insights osquery table.

Specify the table name as a positional argument (e.g., "os_version", "disk_encryption").
Use "jc system-insights tables" to see all available table names.

Optionally filter by device using --system-id (accepts hostname or device ID).

Examples:
  jc system-insights list os_version
  jc system-insights list disk_encryption --system-id JDOE-MBP
  jc system-insights list chrome_extensions --limit 50
  jc system-insights list apps --filter 'name=Slack'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSystemInsightsList(cmd, args[0], limitFlag, sortFlag, filterFlag, systemIDFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=Slack')")
	cmd.Flags().StringVar(&systemIDFlag, "system-id", "", "Filter by device hostname or ID")

	return cmd
}

func runSystemInsightsList(cmd *cobra.Command, table string, limit int, sortField string, filters []string, systemID string) error {
	if !isValidInsightsTable(table) {
		return NewCLIError(ErrCodeValidationError,
			fmt.Sprintf("unknown System Insights table %q", table),
			"Use 'jc system-insights tables' to see available table names")
	}

	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}
	v2Filters := filter.ToV2Queries(exprs)

	// If --system-id is provided, resolve it and prepend a system_id filter.
	if systemID != "" {
		v1Client, err := newV1Client()
		if err != nil {
			return err
		}
		resolvedID, err := resolveDevice(cmd.Context(), v1Client, systemID)
		if err != nil {
			return err
		}
		v2Filters = append([]string{"system_id:eq:" + resolvedID}, v2Filters...)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	endpoint := "/systeminsights/" + table
	result, err := client.ListAll(cmd.Context(), endpoint, api.V2ListOptions{
		Limit:  limit,
		Sort:   sortField,
		Filter: v2Filters,
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	// System Insights tables vary widely, so show all fields by default.
	opts.DefaultFields = nil

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newSystemInsightsTablesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tables",
		Short: "List available System Insights table names",
		Long:  "Print all available JumpCloud System Insights osquery table names, sorted alphabetically.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSystemInsightsTables(cmd)
		},
	}
}

func runSystemInsightsTables(cmd *cobra.Command) error {
	sorted := make([]string, len(validSystemInsightsTables))
	copy(sorted, validSystemInsightsTables)
	sort.Strings(sorted)

	for _, name := range sorted {
		fmt.Fprintln(cmd.OutOrStdout(), name)
	}
	return nil
}

func isValidInsightsTable(name string) bool {
	for _, t := range validSystemInsightsTables {
		if strings.EqualFold(t, name) {
			return true
		}
	}
	return false
}
