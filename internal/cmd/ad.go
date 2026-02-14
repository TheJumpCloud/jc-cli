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

// adDefaultFields is the default field subset shown for Active Directory output.
var adDefaultFields = []string{"id", "domain", "useCase", "groupsEnabled", "delegationState"}

// resolveAD resolves an Active Directory domain or ID to a JumpCloud AD ID.
func resolveAD(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.ActiveDirectoryConfig)
}

func newADCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ad",
		Short: "Manage JumpCloud Active Directory integrations",
		Long:  "List, get, create, update, and delete JumpCloud Active Directory domain integrations.",
	}

	cmd.AddCommand(newADListCmd())
	cmd.AddCommand(newADGetCmd())
	cmd.AddCommand(newADCreateCmd())
	cmd.AddCommand(newADUpdateCmd())
	cmd.AddCommand(newADDeleteCmd())

	return cmd
}

func newADListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all Active Directory integrations",
		Long: `List all JumpCloud Active Directory integrations.

Default fields: id, domain, useCase, groupsEnabled, delegationState.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'domain=corp.example.com'     Exact match`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runADList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -domain)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'domain=corp.example.com')")

	return cmd
}

func runADList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/activedirectories", api.V2ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = adDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newADGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <domain-or-id>",
		Short: "Get an Active Directory integration by domain or ID",
		Long: `Get a single JumpCloud Active Directory integration by domain or ID.

Accepts a domain name (e.g., "corp.example.com") or a 24-character hex ID.
Names are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runADGet(cmd, args[0])
		},
	}

	return cmd
}

func runADGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveAD(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/activedirectories/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newADCreateCmd() *cobra.Command {
	var (
		domain  string
		useCase string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new Active Directory integration",
		Long: `Create a new JumpCloud Active Directory integration.

Required fields: --domain.
The newly created Active Directory object is returned.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runADCreate(cmd, domain, useCase)
		},
	}

	cmd.Flags().StringVar(&domain, "domain", "", "Active Directory domain (required)")
	cmd.Flags().StringVar(&useCase, "use-case", "", "Use case (e.g. ADASAUTHORITY)")
	_ = cmd.MarkFlagRequired("domain")

	return cmd
}

func runADCreate(cmd *cobra.Command, domain, useCase string) error {
	if viper.GetBool("plan") {
		effects := []string{"domain: " + domain}
		if useCase != "" {
			effects = append(effects, "useCase: "+useCase)
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "Active Directory",
			Target:     domain,
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
		"domain": domain,
	}
	if useCase != "" {
		body["useCase"] = useCase
	}

	result, err := client.Create(cmd.Context(), "/activedirectories", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newADUpdateCmd() *cobra.Command {
	var (
		useCase       string
		groupsEnabled bool
	)

	cmd := &cobra.Command{
		Use:   "update <domain-or-id>",
		Short: "Update an Active Directory integration",
		Long: `Update an existing JumpCloud Active Directory integration.

Accepts an Active Directory domain or 24-character hex ID.
Specify only the fields you want to change. The updated Active Directory object is returned.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runADUpdate(cmd, args[0], useCase, groupsEnabled)
		},
	}

	cmd.Flags().StringVar(&useCase, "use-case", "", "Use case (e.g. ADASAUTHORITY)")
	cmd.Flags().BoolVar(&groupsEnabled, "groups-enabled", false, "Enable or disable group management")

	return cmd
}

func runADUpdate(cmd *cobra.Command, identifier, useCase string, groupsEnabled bool) error {
	body := map[string]any{}

	if cmd.Flags().Changed("use-case") {
		body["useCase"] = useCase
	}
	if cmd.Flags().Changed("groups-enabled") {
		body["groupsEnabled"] = groupsEnabled
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --use-case, --groups-enabled)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "Active Directory",
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

	id, err := resolveAD(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/activedirectories/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newADDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <domain-or-id>",
		Aliases: []string{"rm"},
		Short:   "Delete an Active Directory integration",
		Long: `Delete a JumpCloud Active Directory integration.

Accepts an Active Directory domain or 24-character hex ID.
Shows the AD domain before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runADDelete(cmd, args[0])
		},
	}

	return cmd
}

func runADDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveAD(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch the AD first so we can show details in the confirmation/plan.
	adData, err := client.Get(cmd.Context(), "/activedirectories/"+id)
	if err != nil {
		return err
	}

	var ad struct {
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(adData, &ad); err != nil {
		return fmt.Errorf("parsing AD data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "Active Directory",
			Target:   fmt.Sprintf("%s (%s)", ad.Domain, id),
			Effects:  []string{"Remove Active Directory integration"},
		}
		return renderPlan(cmd, p)
	}

	// Confirmation prompt (unless --force is set).
	if !viper.GetBool("force") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete AD %q? [y/N] ", ad.Domain)
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

	_, err = client.Delete(cmd.Context(), "/activedirectories/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "AD %q deleted successfully.\n", ad.Domain)
	return nil
}
