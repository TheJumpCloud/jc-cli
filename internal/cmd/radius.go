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

var radiusDefaultFields = []string{"_id", "name", "networkSourceIp", "authPort", "accountingPort"}

func resolveRADIUSServer(ctx context.Context, client *api.V1Client, identifier string) (string, error) {
	r := resolve.NewResolver(client)
	return r.Resolve(ctx, identifier, resolve.RADIUSServerConfig)
}

func newRADIUSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "radius",
		Short: "Manage RADIUS servers",
		Long:  "List, get, create, update, and delete JumpCloud RADIUS server configurations.",
	}

	cmd.AddCommand(newRADIUSListCmd())
	cmd.AddCommand(newRADIUSGetCmd())
	cmd.AddCommand(newRADIUSCreateCmd())
	cmd.AddCommand(newRADIUSUpdateCmd())
	cmd.AddCommand(newRADIUSDeleteCmd())

	return cmd
}

func newRADIUSListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all RADIUS servers",
		Long: `List all JumpCloud RADIUS server configurations.

Default fields: _id, name, networkSourceIp, authPort, accountingPort.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'name=Office RADIUS'     Exact match`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRADIUSList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=Office RADIUS')")

	return cmd
}

func runRADIUSList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/radiusservers", api.ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV1Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = radiusDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(result.Data), result.TotalCount)
	}

	return nil
}

func newRADIUSGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get a RADIUS server by name or ID",
		Long: `Get a single JumpCloud RADIUS server by name or ID.

Accepts a server name (e.g., "Office RADIUS") or a 24-character hex ID.
Names are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRADIUSGet(cmd, args[0])
		},
	}
}

func runRADIUSGet(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveRADIUSServer(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/radiusservers/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newRADIUSCreateCmd() *cobra.Command {
	var (
		name           string
		sharedSecret   string
		authPort       int
		accountingPort int
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new RADIUS server",
		Long: `Create a new JumpCloud RADIUS server configuration.

Required fields: --name, --shared-secret.
The newly created RADIUS server object is returned.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRADIUSCreate(cmd, name, sharedSecret, authPort, accountingPort)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "RADIUS server name (required)")
	cmd.Flags().StringVar(&sharedSecret, "shared-secret", "", "RADIUS shared secret (required)")
	cmd.Flags().IntVar(&authPort, "auth-port", 1812, "Authentication port")
	cmd.Flags().IntVar(&accountingPort, "accounting-port", 1813, "Accounting port")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("shared-secret")

	return cmd
}

func runRADIUSCreate(cmd *cobra.Command, name, sharedSecret string, authPort, accountingPort int) error {
	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "create",
			Resource: "RADIUS server",
			Target:   name,
			Effects: []string{
				"name: " + name,
				fmt.Sprintf("authPort: %d", authPort),
				fmt.Sprintf("accountingPort: %d", accountingPort),
			},
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV1Client()
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":           name,
		"sharedSecret":   sharedSecret,
		"authPort":       authPort,
		"accountingPort": accountingPort,
	}

	result, err := client.Create(cmd.Context(), "/radiusservers", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newRADIUSUpdateCmd() *cobra.Command {
	var (
		name           string
		sharedSecret   string
		authPort       int
		accountingPort int
	)

	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Update a RADIUS server",
		Long: `Update an existing JumpCloud RADIUS server configuration.

Accepts a server name or 24-character hex ID.
Specify only the fields you want to change. The updated RADIUS server object is returned.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRADIUSUpdate(cmd, args[0], name, sharedSecret, authPort, accountingPort)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "RADIUS server name")
	cmd.Flags().StringVar(&sharedSecret, "shared-secret", "", "RADIUS shared secret")
	cmd.Flags().IntVar(&authPort, "auth-port", 0, "Authentication port")
	cmd.Flags().IntVar(&accountingPort, "accounting-port", 0, "Accounting port")

	return cmd
}

func runRADIUSUpdate(cmd *cobra.Command, identifier, name, sharedSecret string, authPort, accountingPort int) error {
	body := map[string]any{}

	if cmd.Flags().Changed("name") {
		body["name"] = name
	}
	if cmd.Flags().Changed("shared-secret") {
		body["sharedSecret"] = sharedSecret
	}
	if cmd.Flags().Changed("auth-port") {
		body["authPort"] = authPort
	}
	if cmd.Flags().Changed("accounting-port") {
		body["accountingPort"] = accountingPort
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --name, --shared-secret)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "RADIUS server",
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

	id, err := resolveRADIUSServer(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/radiusservers/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newRADIUSDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <name-or-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a RADIUS server",
		Long: `Delete a JumpCloud RADIUS server configuration.

Accepts a server name or 24-character hex ID.
Shows the server name before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRADIUSDelete(cmd, args[0])
		},
	}
}

func runRADIUSDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveRADIUSServer(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	serverData, err := client.Get(cmd.Context(), "/radiusservers/"+id)
	if err != nil {
		return err
	}

	var server struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(serverData, &server); err != nil {
		return fmt.Errorf("parsing RADIUS server data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "RADIUS server",
			Target:   fmt.Sprintf("%s (%s)", server.Name, id),
			Effects:  []string{"Remove RADIUS server configuration"},
		}
		return renderPlan(cmd, p)
	}

	if !viper.GetBool("force") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete RADIUS server %q? [y/N] ", server.Name)
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

	_, err = client.Delete(cmd.Context(), "/radiusservers/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "RADIUS server %q deleted successfully.\n", server.Name)
	return nil
}
