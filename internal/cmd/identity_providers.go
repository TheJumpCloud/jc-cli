package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var identityProviderDefaultFields = []string{"id", "name", "type", "clientId", "url"}

func resolveIdentityProvider(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.IdentityProviderConfig)
}

// flattenIdentityProvider promotes oidc sub-fields to top level for display.
func flattenIdentityProvider(raw json.RawMessage) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	oidcRaw, ok := obj["oidc"]
	if !ok {
		return raw
	}
	var oidc map[string]json.RawMessage
	if err := json.Unmarshal(oidcRaw, &oidc); err != nil {
		return raw
	}
	if v, ok := oidc["clientId"]; ok {
		obj["clientId"] = v
	}
	if v, ok := oidc["url"]; ok {
		obj["url"] = v
	}
	delete(obj, "oidc")
	result, _ := json.Marshal(obj)
	return result
}

func flattenIdentityProviders(data []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, len(data))
	for i, raw := range data {
		out[i] = flattenIdentityProvider(raw)
	}
	return out
}

func newIdentityProvidersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "identity-providers",
		Aliases: []string{"idp"},
		Short:   "Manage JumpCloud identity providers",
		Long:    "Manage JumpCloud identity providers for SSO/OIDC federation (types: OIDC, GOOGLE, OKTA, AZURE).",
	}
	cmd.AddCommand(newIdentityProvidersListCmd())
	cmd.AddCommand(newIdentityProvidersGetCmd())
	cmd.AddCommand(newIdentityProvidersCreateCmd())
	cmd.AddCommand(newIdentityProvidersUpdateCmd())
	cmd.AddCommand(newIdentityProvidersDeleteCmd())
	return cmd
}

func newIdentityProvidersListCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List identity providers",
		Long:  "List all JumpCloud identity providers configured for your organization.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIdentityProvidersList(cmd, limit)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of results")
	return cmd
}

func runIdentityProvidersList(cmd *cobra.Command, limit int) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := client.ListAll(ctx, "/identity-providers", api.V2ListOptions{
		Limit:       limit,
		ResponseKey: "identityProviders",
	})
	if err != nil {
		return err
	}

	data := flattenIdentityProviders(result.Data)

	opts := output.CurrentOptions()
	opts.DefaultFields = identityProviderDefaultFields
	if err := output.WriteList(cmd.OutOrStdout(), data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(data))
	}
	return nil
}

func newIdentityProvidersGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [name-or-id]",
		Short: "Get an identity provider",
		Long:  "Get a JumpCloud identity provider by name or ID.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.IdentityProviderConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIdentityProvidersGet(cmd, args[0])
		},
	}
	return cmd
}

func runIdentityProvidersGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	id, err := resolveIdentityProvider(ctx, client, identifier)
	if err != nil {
		return err
	}

	data, err := client.Get(ctx, "/identity-providers/"+id)
	if err != nil {
		return err
	}

	data = flattenIdentityProvider(data)

	opts := output.CurrentOptions()
	opts.DefaultFields = identityProviderDefaultFields
	return output.WriteSingle(cmd.OutOrStdout(), data, opts)
}

func newIdentityProvidersCreateCmd() *cobra.Command {
	var name, idpType, clientID, clientSecret, url string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an identity provider",
		Long:  "Create a new JumpCloud identity provider. Valid types: OIDC, GOOGLE, OKTA, AZURE.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIdentityProvidersCreate(cmd, name, idpType, clientID, clientSecret, url)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Identity provider name")
	cmd.Flags().StringVar(&idpType, "type", "", "Provider type (OIDC, GOOGLE, OKTA, AZURE)")
	cmd.Flags().StringVar(&clientID, "client-id", "", "OIDC client ID")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "OIDC client secret")
	cmd.Flags().StringVar(&url, "url", "", "OIDC issuer URL")
	cmd.MarkFlagRequired("name")
	cmd.MarkFlagRequired("type")
	cmd.MarkFlagRequired("client-id")
	cmd.MarkFlagRequired("client-secret")
	cmd.MarkFlagRequired("url")
	return cmd
}

func runIdentityProvidersCreate(cmd *cobra.Command, name, idpType, clientID, clientSecret, url string) error {
	body := map[string]any{
		"name": name,
		"type": idpType,
		"oidc": map[string]any{
			"clientId":     clientID,
			"clientSecret": clientSecret,
			"url":          url,
		},
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "create",
			Resource: "identity-provider",
			Target:   name,
			Effects:  []string{fmt.Sprintf("Create %s identity provider %q with URL %s", idpType, name, url)},
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	data, err := client.Create(ctx, "/identity-providers", body)
	if err != nil {
		return err
	}

	data = flattenIdentityProvider(data)

	opts := output.CurrentOptions()
	opts.DefaultFields = identityProviderDefaultFields
	return output.WriteSingle(cmd.OutOrStdout(), data, opts)
}

func newIdentityProvidersUpdateCmd() *cobra.Command {
	var name, idpType, clientID, clientSecret, url string
	cmd := &cobra.Command{
		Use:   "update [name-or-id]",
		Short: "Update an identity provider",
		Long:  "Update an existing JumpCloud identity provider.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.IdentityProviderConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIdentityProvidersUpdate(cmd, args[0], name, idpType, clientID, clientSecret, url)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "New name")
	cmd.Flags().StringVar(&idpType, "type", "", "New type (OIDC, GOOGLE, OKTA, AZURE)")
	cmd.Flags().StringVar(&clientID, "client-id", "", "New OIDC client ID")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "New OIDC client secret")
	cmd.Flags().StringVar(&url, "url", "", "New OIDC issuer URL")
	return cmd
}

func runIdentityProvidersUpdate(cmd *cobra.Command, identifier, name, idpType, clientID, clientSecret, url string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	id, err := resolveIdentityProvider(ctx, client, identifier)
	if err != nil {
		return err
	}

	// Fetch current state to merge changes (PUT requires full object).
	current, err := client.Get(ctx, "/identity-providers/"+id)
	if err != nil {
		return err
	}
	var obj map[string]any
	if err := json.Unmarshal(current, &obj); err != nil {
		return fmt.Errorf("parsing current state: %w", err)
	}

	if cmd.Flags().Changed("name") {
		obj["name"] = name
	}
	if cmd.Flags().Changed("type") {
		obj["type"] = idpType
	}
	oidc, _ := obj["oidc"].(map[string]any)
	if oidc == nil {
		oidc = map[string]any{}
	}
	if cmd.Flags().Changed("client-id") {
		oidc["clientId"] = clientID
	}
	if cmd.Flags().Changed("client-secret") {
		oidc["clientSecret"] = clientSecret
	}
	if cmd.Flags().Changed("url") {
		oidc["url"] = url
	}
	obj["oidc"] = oidc

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "update",
			Resource: "identity-provider",
			Target:   identifier,
			Effects:  []string{fmt.Sprintf("Update identity provider %q (ID: %s)", identifier, id)},
		}
		return renderPlan(cmd, p)
	}

	data, err := client.Update(ctx, "/identity-providers/"+id, obj)
	if err != nil {
		return err
	}

	data = flattenIdentityProvider(data)

	opts := output.CurrentOptions()
	opts.DefaultFields = identityProviderDefaultFields
	return output.WriteSingle(cmd.OutOrStdout(), data, opts)
}

func newIdentityProvidersDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete [name-or-id]",
		Short: "Delete an identity provider",
		Long:  "Delete a JumpCloud identity provider. This action is irreversible.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.IdentityProviderConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIdentityProvidersDelete(cmd, args[0])
		},
	}
	return cmd
}

func runIdentityProvidersDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	id, err := resolveIdentityProvider(ctx, client, identifier)
	if err != nil {
		return err
	}

	data, err := client.Get(ctx, "/identity-providers/"+id)
	if err != nil {
		return err
	}

	var obj map[string]any
	json.Unmarshal(data, &obj)
	displayName, _ := obj["name"].(string)
	if displayName == "" {
		displayName = id
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "identity-provider",
			Target:   displayName,
			Effects:  []string{fmt.Sprintf("Permanently delete identity provider %q (ID: %s)", displayName, id)},
		}
		return renderPlan(cmd, p)
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete identity provider %q? [y/N] ", displayName)
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

	if _, err := client.Delete(ctx, "/identity-providers/"+id); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleted identity provider %q (ID: %s)\n", displayName, id)
	return nil
}
