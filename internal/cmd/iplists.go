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

// ipListDefaultFields is the default field subset shown for IP list output.
var ipListDefaultFields = []string{"id", "name", "description"}

// resolveIPList resolves an IP list name or ID to a JumpCloud IP list ID.
func resolveIPList(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.IPListConfig)
}

func newIPListsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "iplists",
		Short: "Manage JumpCloud IP lists",
		Long:  "List, get, create, update, and delete JumpCloud IP lists for use with authentication policies.",
	}

	cmd.AddCommand(newIPListsListCmd())
	cmd.AddCommand(newIPListsGetCmd())
	cmd.AddCommand(newIPListsCreateCmd())
	cmd.AddCommand(newIPListsUpdateCmd())
	cmd.AddCommand(newIPListsDeleteCmd())

	return cmd
}

func newIPListsListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all IP lists",
		Long: `List all JumpCloud IP lists.

Default fields: id, name, description.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'name=Office IPs'     Exact match`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIPListsList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -name)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=Office IPs')")

	return cmd
}

func runIPListsList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/iplists", api.V2ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = ipListDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newIPListsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get an IP list by name or ID",
		Long: `Get a single JumpCloud IP list by name or ID.

Accepts an IP list name (e.g., "Office IPs") or a 24-character hex ID.
Names are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.IPListConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIPListsGet(cmd, args[0])
		},
	}

	return cmd
}

func runIPListsGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveIPList(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/iplists/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newIPListsCreateCmd() *cobra.Command {
	var (
		name        string
		description string
		ips         string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new IP list",
		Long: `Create a new JumpCloud IP list.

Required fields: --name, --ips.
IP addresses can be single IPs, CIDR ranges, or IP ranges (e.g. "10.0.0.1,10.0.0.0/24,10.0.1.1-10.0.1.255").
The newly created IP list object is returned.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIPListsCreate(cmd, name, description, ips)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "IP list name (required)")
	cmd.Flags().StringVar(&description, "description", "", "IP list description")
	cmd.Flags().StringVar(&ips, "ips", "", "Comma-separated IP addresses, CIDRs, or ranges (required)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("ips")

	return cmd
}

func runIPListsCreate(cmd *cobra.Command, name, description, ips string) error {
	ipList := parseIPFlag(ips)

	if viper.GetBool("plan") {
		effects := []string{"name: " + name, fmt.Sprintf("ips: %d entries", len(ipList))}
		if description != "" {
			effects = append(effects, "description: "+description)
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "IP list",
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

	body := map[string]any{
		"name": name,
		"ips":  ipList,
	}
	if description != "" {
		body["description"] = description
	}

	result, err := client.Create(cmd.Context(), "/iplists", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newIPListsUpdateCmd() *cobra.Command {
	var (
		name        string
		description string
		ips         string
	)

	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Update an IP list",
		Long: `Update an existing JumpCloud IP list.

Accepts an IP list name or 24-character hex ID.
Specify only the fields you want to change. The updated IP list object is returned.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.IPListConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIPListsUpdate(cmd, args[0], name, description, ips)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "IP list name")
	cmd.Flags().StringVar(&description, "description", "", "IP list description")
	cmd.Flags().StringVar(&ips, "ips", "", "Comma-separated IP addresses, CIDRs, or ranges")

	return cmd
}

func runIPListsUpdate(cmd *cobra.Command, identifier, name, description, ips string) error {
	body := map[string]any{}

	if cmd.Flags().Changed("name") {
		body["name"] = name
	}
	if cmd.Flags().Changed("description") {
		body["description"] = description
	}
	if cmd.Flags().Changed("ips") {
		body["ips"] = parseIPFlag(ips)
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --name, --description, --ips)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "IP list",
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

	id, err := resolveIPList(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/iplists/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newIPListsDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <name-or-id>",
		Aliases: []string{"rm"},
		Short:   "Delete an IP list",
		Long: `Delete a JumpCloud IP list.

Accepts an IP list name or 24-character hex ID.
Shows the IP list name before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.IPListConfig),
		RunE: batchRunE("IP list", "delete", runIPListsDelete),
	}

	addBatchSourceFlags(cmd)
	return cmd
}

func runIPListsDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveIPList(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch the IP list first so we can show details in the confirmation/plan.
	ipListData, err := client.Get(cmd.Context(), "/iplists/"+id)
	if err != nil {
		return err
	}

	var ipList struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(ipListData, &ipList); err != nil {
		return fmt.Errorf("parsing IP list data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "IP list",
			Target:   fmt.Sprintf("%s (%s)", ipList.Name, id),
			Effects:  []string{"Remove IP list and all references from authentication policies"},
		}
		return renderPlan(cmd, p)
	}

	// Confirmation prompt (unless --force is set).
	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete IP list %q? [y/N] ", ipList.Name)
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

	_, err = client.Delete(cmd.Context(), "/iplists/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "IP list %q deleted successfully.\n", ipList.Name)
	return nil
}

// parseIPFlag splits a comma-separated IP string into a slice of trimmed entries.
func parseIPFlag(ips string) []string {
	parts := strings.Split(ips, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
