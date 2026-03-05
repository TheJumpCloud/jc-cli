package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

// duoAccountDefaultFields is the default field subset shown for Duo account output.
var duoAccountDefaultFields = []string{"id", "name"}

// duoAppDefaultFields is the default field subset shown for Duo application output.
var duoAppDefaultFields = []string{"id", "name", "apiHost"}

// resolveDuoAccount resolves a Duo account name or ID to a JumpCloud Duo account ID.
func resolveDuoAccount(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.DuoAccountConfig)
}

func newDuoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "duo",
		Short: "Manage JumpCloud Duo accounts and applications",
		Long:  "List, get, create, and delete JumpCloud Duo accounts and their applications.",
	}

	cmd.AddCommand(newDuoListCmd())
	cmd.AddCommand(newDuoGetCmd())
	cmd.AddCommand(newDuoCreateCmd())
	cmd.AddCommand(newDuoDeleteCmd())
	cmd.AddCommand(newDuoAppsCmd())
	cmd.AddCommand(newDuoAppGetCmd())
	cmd.AddCommand(newDuoAppCreateCmd())
	cmd.AddCommand(newDuoAppDeleteCmd())

	return cmd
}

func newDuoListCmd() *cobra.Command {
	var limitFlag int

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all Duo accounts",
		Long: `List all JumpCloud Duo accounts.

Default fields: id, name.
Use --output table for a readable ASCII table.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDuoList(cmd, limitFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")

	return cmd
}

func runDuoList(cmd *cobra.Command, limit int) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/duo/accounts", api.V2ListOptions{
		Limit: limit,
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = duoAccountDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newDuoGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get a Duo account by name or ID",
		Long: `Get a single JumpCloud Duo account by name or ID.

Accepts a Duo account name or a 24-character hex ID.
Names are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.DuoAccountConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDuoGet(cmd, args[0])
		},
	}

	return cmd
}

func runDuoGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveDuoAccount(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/duo/accounts/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newDuoCreateCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new Duo account",
		Long: `Create a new JumpCloud Duo account.

Required fields: --name.
The newly created Duo account object is returned.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDuoCreate(cmd, name)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Duo account name (required)")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func runDuoCreate(cmd *cobra.Command, name string) error {
	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:     "create",
			Resource:   "Duo account",
			Target:     name,
			Effects:    []string{"name: " + name},
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
	}

	result, err := client.Create(cmd.Context(), "/duo/accounts", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newDuoDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <name-or-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a Duo account",
		Long: `Delete a JumpCloud Duo account.

Accepts a Duo account name or 24-character hex ID.
Shows the Duo account name before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.DuoAccountConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDuoDelete(cmd, args[0])
		},
	}

	return cmd
}

func runDuoDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveDuoAccount(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch the Duo account first so we can show details in the confirmation/plan.
	accountData, err := client.Get(cmd.Context(), "/duo/accounts/"+id)
	if err != nil {
		return err
	}

	var account struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(accountData, &account); err != nil {
		return fmt.Errorf("parsing Duo account data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "Duo account",
			Target:   fmt.Sprintf("%s (%s)", account.Name, id),
			Effects:  []string{"Remove Duo account"},
		}
		return renderPlan(cmd, p)
	}

	// Confirmation prompt (unless --force is set).
	if !viper.GetBool("force") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete Duo account %q? [y/N] ", account.Name)
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

	_, err = client.Delete(cmd.Context(), "/duo/accounts/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Duo account %q deleted successfully.\n", account.Name)
	return nil
}

func newDuoAppsCmd() *cobra.Command {
	var limitFlag int

	cmd := &cobra.Command{
		Use:   "apps <account-name-or-id>",
		Short: "List Duo applications for an account",
		Long: `List all Duo applications for a JumpCloud Duo account.

Default fields: id, name, apiHost.
Use --output table for a readable ASCII table.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.DuoAccountConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDuoApps(cmd, args[0], limitFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")

	return cmd
}

func runDuoApps(cmd *cobra.Command, identifier string, limit int) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	accountID, err := resolveDuoAccount(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), fmt.Sprintf("/duo/accounts/%s/applications", accountID), api.V2ListOptions{
		Limit: limit,
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = duoAppDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newDuoAppGetCmd() *cobra.Command {
	var appID string

	cmd := &cobra.Command{
		Use:   "app-get <account-name-or-id>",
		Short: "Get a specific Duo application",
		Long: `Get a specific Duo application for a JumpCloud Duo account.

Requires --app-id to specify which application to retrieve.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.DuoAccountConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDuoAppGet(cmd, args[0], appID)
		},
	}

	cmd.Flags().StringVar(&appID, "app-id", "", "Duo application ID (required)")
	_ = cmd.MarkFlagRequired("app-id")

	return cmd
}

func runDuoAppGet(cmd *cobra.Command, identifier, appID string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	accountID, err := resolveDuoAccount(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), fmt.Sprintf("/duo/accounts/%s/applications/%s", accountID, appID))
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newDuoAppCreateCmd() *cobra.Command {
	var (
		name    string
		apiHost string
	)

	cmd := &cobra.Command{
		Use:   "app-create <account-name-or-id>",
		Short: "Create a Duo application",
		Long: `Create a new Duo application for a JumpCloud Duo account.

Required fields: --name, --api-host.
The newly created Duo application object is returned.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.DuoAccountConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDuoAppCreate(cmd, args[0], name, apiHost)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Duo application name (required)")
	cmd.Flags().StringVar(&apiHost, "api-host", "", "Duo API host (required)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("api-host")

	return cmd
}

func runDuoAppCreate(cmd *cobra.Command, identifier, name, apiHost string) error {
	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "create",
			Resource: "Duo application",
			Target:   name,
			Effects: []string{
				"name: " + name,
				"apiHost: " + apiHost,
			},
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	accountID, err := resolveDuoAccount(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":    name,
		"apiHost": apiHost,
	}

	result, err := client.Create(cmd.Context(), fmt.Sprintf("/duo/accounts/%s/applications", accountID), body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newDuoAppDeleteCmd() *cobra.Command {
	var appID string

	cmd := &cobra.Command{
		Use:     "app-delete <account-name-or-id>",
		Aliases: []string{"app-rm"},
		Short:   "Delete a Duo application",
		Long: `Delete a Duo application from a JumpCloud Duo account.

Requires --app-id to specify which application to delete.
Shows the application name before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.DuoAccountConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDuoAppDelete(cmd, args[0], appID)
		},
	}

	cmd.Flags().StringVar(&appID, "app-id", "", "Duo application ID (required)")
	_ = cmd.MarkFlagRequired("app-id")

	return cmd
}

func runDuoAppDelete(cmd *cobra.Command, identifier, appID string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	accountID, err := resolveDuoAccount(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch the Duo application first so we can show details in the confirmation/plan.
	appData, err := client.Get(cmd.Context(), fmt.Sprintf("/duo/accounts/%s/applications/%s", accountID, appID))
	if err != nil {
		return err
	}

	var app struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(appData, &app); err != nil {
		return fmt.Errorf("parsing Duo application data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "Duo application",
			Target:   fmt.Sprintf("%s (%s)", app.Name, appID),
			Effects:  []string{"Remove Duo application"},
		}
		return renderPlan(cmd, p)
	}

	// Confirmation prompt (unless --force is set).
	if !viper.GetBool("force") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete Duo application %q? [y/N] ", app.Name)
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

	_, err = client.Delete(cmd.Context(), fmt.Sprintf("/duo/accounts/%s/applications/%s", accountID, appID))
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Duo application %q deleted successfully.\n", app.Name)
	return nil
}
