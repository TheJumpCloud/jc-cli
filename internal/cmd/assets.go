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

// assetDefaultFields is the default field subset shown for asset output.
var assetDefaultFields = []string{"id", "name", "serialNumber", "assetTag", "status"}

// resolveAsset resolves an asset name or ID to a JumpCloud asset ID.
func resolveAsset(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.AssetConfig)
}

func newAssetsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assets",
		Short: "Manage JumpCloud assets",
		Long:  "List, get, create, update, and delete JumpCloud hardware assets.",
	}

	cmd.AddCommand(newAssetsListCmd())
	cmd.AddCommand(newAssetsGetCmd())
	cmd.AddCommand(newAssetsCreateCmd())
	cmd.AddCommand(newAssetsUpdateCmd())
	cmd.AddCommand(newAssetsDeleteCmd())

	return cmd
}

func newAssetsListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all assets",
		Long: `List all JumpCloud assets.

Default fields: id, name, serialNumber, assetTag, status.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'name=MacBook Pro'     Exact match
  --filter 'status=Assigned'      Filter by status`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAssetsList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -name)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=MacBook Pro')")

	return cmd
}

func runAssetsList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/assets", api.V2ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = assetDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newAssetsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get an asset by name or ID",
		Long: `Get a single JumpCloud asset by name or ID.

Accepts an asset name (e.g., "MacBook Pro") or a 24-character hex ID.
Names are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAssetsGet(cmd, args[0])
		},
	}

	return cmd
}

func runAssetsGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveAsset(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/assets/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAssetsCreateCmd() *cobra.Command {
	var (
		name         string
		serialNumber string
		assetTag     string
		status       string
		assetType    string
		systemID     string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new asset",
		Long: `Create a new JumpCloud asset.

Required fields: --name.
The newly created asset object is returned.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAssetsCreate(cmd, name, serialNumber, assetTag, status, assetType, systemID)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Asset name (required)")
	cmd.Flags().StringVar(&serialNumber, "serial-number", "", "Hardware serial number")
	cmd.Flags().StringVar(&assetTag, "asset-tag", "", "Organization asset tag")
	cmd.Flags().StringVar(&status, "status", "", "Asset status (e.g. In Stock, Assigned, In Repair, Retired, Lost)")
	cmd.Flags().StringVar(&assetType, "type", "", "Asset type (e.g. laptop, desktop, mobile, peripheral)")
	cmd.Flags().StringVar(&systemID, "system-id", "", "Linked JumpCloud system ID")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func runAssetsCreate(cmd *cobra.Command, name, serialNumber, assetTag, status, assetType, systemID string) error {
	if viper.GetBool("plan") {
		effects := []string{"name: " + name}
		if serialNumber != "" {
			effects = append(effects, "serialNumber: "+serialNumber)
		}
		if assetTag != "" {
			effects = append(effects, "assetTag: "+assetTag)
		}
		if status != "" {
			effects = append(effects, "status: "+status)
		}
		if assetType != "" {
			effects = append(effects, "type: "+assetType)
		}
		if systemID != "" {
			effects = append(effects, "systemId: "+systemID)
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "asset",
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
	}
	if serialNumber != "" {
		body["serialNumber"] = serialNumber
	}
	if assetTag != "" {
		body["assetTag"] = assetTag
	}
	if status != "" {
		body["status"] = status
	}
	if assetType != "" {
		body["type"] = assetType
	}
	if systemID != "" {
		body["systemId"] = systemID
	}

	result, err := client.Create(cmd.Context(), "/assets", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAssetsUpdateCmd() *cobra.Command {
	var (
		name         string
		serialNumber string
		assetTag     string
		status       string
		assetType    string
		systemID     string
	)

	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Update an asset",
		Long: `Update an existing JumpCloud asset.

Accepts an asset name or 24-character hex ID.
Specify only the fields you want to change. The updated asset object is returned.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAssetsUpdate(cmd, args[0], name, serialNumber, assetTag, status, assetType, systemID)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Asset name")
	cmd.Flags().StringVar(&serialNumber, "serial-number", "", "Hardware serial number")
	cmd.Flags().StringVar(&assetTag, "asset-tag", "", "Organization asset tag")
	cmd.Flags().StringVar(&status, "status", "", "Asset status")
	cmd.Flags().StringVar(&assetType, "type", "", "Asset type")
	cmd.Flags().StringVar(&systemID, "system-id", "", "Linked JumpCloud system ID")

	return cmd
}

func runAssetsUpdate(cmd *cobra.Command, identifier, name, serialNumber, assetTag, status, assetType, systemID string) error {
	body := map[string]any{}

	if cmd.Flags().Changed("name") {
		body["name"] = name
	}
	if cmd.Flags().Changed("serial-number") {
		body["serialNumber"] = serialNumber
	}
	if cmd.Flags().Changed("asset-tag") {
		body["assetTag"] = assetTag
	}
	if cmd.Flags().Changed("status") {
		body["status"] = status
	}
	if cmd.Flags().Changed("type") {
		body["type"] = assetType
	}
	if cmd.Flags().Changed("system-id") {
		body["systemId"] = systemID
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --name, --status)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "asset",
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

	id, err := resolveAsset(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/assets/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAssetsDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <name-or-id>",
		Aliases: []string{"rm"},
		Short:   "Delete an asset",
		Long: `Delete a JumpCloud asset.

Accepts an asset name or 24-character hex ID.
Shows the asset name before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAssetsDelete(cmd, args[0])
		},
	}

	return cmd
}

func runAssetsDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveAsset(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch the asset first so we can show details in the confirmation/plan.
	assetData, err := client.Get(cmd.Context(), "/assets/"+id)
	if err != nil {
		return err
	}

	var asset struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(assetData, &asset); err != nil {
		return fmt.Errorf("parsing asset data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "asset",
			Target:   fmt.Sprintf("%s (%s)", asset.Name, id),
			Effects:  []string{"Remove asset"},
		}
		return renderPlan(cmd, p)
	}

	// Confirmation prompt (unless --force is set).
	if !viper.GetBool("force") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete asset %q? [y/N] ", asset.Name)
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

	_, err = client.Delete(cmd.Context(), "/assets/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Asset %q deleted successfully.\n", asset.Name)
	return nil
}
