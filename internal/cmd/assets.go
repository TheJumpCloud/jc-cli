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

// Default fields per asset sub-resource (from flattened labels).
var deviceAssetDefaultFields = []string{"id", "Name", "Serial Number", "Status", "Model", "Type"}
var accessoryAssetDefaultFields = []string{"id", "Name", "Status"}
var locationAssetDefaultFields = []string{"id", "Name"}

// assetSubResource groups the configuration for one asset sub-resource type.
type assetSubResource struct {
	use           string
	noun          string
	endpoint      string
	defaultFields []string
	resolveConfig resolve.ResourceConfig
}

var assetSubResources = []assetSubResource{
	{
		use: "devices", noun: "device asset",
		endpoint:      "/assets/devices",
		defaultFields: deviceAssetDefaultFields,
		resolveConfig: resolve.DeviceAssetConfig,
	},
	{
		use: "accessories", noun: "accessory asset",
		endpoint:      "/assets/accessories",
		defaultFields: accessoryAssetDefaultFields,
		resolveConfig: resolve.AccessoryAssetConfig,
	},
	{
		use: "locations", noun: "location asset",
		endpoint:      "/assets/locations",
		defaultFields: locationAssetDefaultFields,
		resolveConfig: resolve.LocationAssetConfig,
	},
}

func newAssetsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assets",
		Short: "Manage JumpCloud assets",
		Long: `Manage JumpCloud assets (devices, accessories, locations).

Assets use a dynamic field structure. Use --field "Label=Value" for create/update.`,
	}

	for _, sub := range assetSubResources {
		cmd.AddCommand(newAssetSubCmd(sub))
	}

	return cmd
}

func newAssetSubCmd(sub assetSubResource) *cobra.Command {
	cmd := &cobra.Command{
		Use:   sub.use,
		Short: fmt.Sprintf("Manage %ss", sub.noun),
		Long:  fmt.Sprintf("List, get, create, update, and delete JumpCloud %ss.", sub.noun),
	}

	cmd.AddCommand(newAssetSubListCmd(sub))
	cmd.AddCommand(newAssetSubGetCmd(sub))
	cmd.AddCommand(newAssetSubCreateCmd(sub))
	cmd.AddCommand(newAssetSubUpdateCmd(sub))
	cmd.AddCommand(newAssetSubDeleteCmd(sub))

	return cmd
}

func newAssetSubListCmd(sub assetSubResource) *cobra.Command {
	var limitFlag int

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   fmt.Sprintf("List all %ss", sub.noun),
		Long: fmt.Sprintf(`List all JumpCloud %ss.

Output is flattened from the nested fields structure to top-level columns.
Use --output table for a readable ASCII table.`, sub.noun),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAssetSubList(cmd, sub, limitFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")

	return cmd
}

func runAssetSubList(cmd *cobra.Command, sub assetSubResource, limit int) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), sub.endpoint, api.V2ListOptions{
		Limit: limit,
	})
	if err != nil {
		return err
	}

	flattened := flattenAssetFields(result.Data)

	opts := output.CurrentOptions()
	opts.DefaultFields = sub.defaultFields

	if err := output.WriteList(cmd.OutOrStdout(), flattened, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(flattened))
	}

	return nil
}

func newAssetSubGetCmd(sub assetSubResource) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <name-or-id>",
		Short: fmt.Sprintf("Get a %s by name or ID", sub.noun),
		Long: fmt.Sprintf(`Get a single JumpCloud %s by name or ID.

Accepts a name or 24-character hex ID.
Names are resolved from the nested fields structure automatically.`, sub.noun),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAssetSubGet(cmd, sub, args[0])
		},
	}

	return cmd
}

func runAssetSubGet(cmd *cobra.Command, sub assetSubResource, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveAssetSub(cmd.Context(), client, identifier, sub.resolveConfig)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), sub.endpoint+"/"+id)
	if err != nil {
		return err
	}

	flattened := flattenAssetFields([]json.RawMessage{result})

	opts := output.CurrentOptions()
	if len(flattened) > 0 {
		return output.WriteSingle(cmd.OutOrStdout(), flattened[0], opts)
	}
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAssetSubCreateCmd(sub assetSubResource) *cobra.Command {
	var fieldFlags []string

	cmd := &cobra.Command{
		Use:   "create",
		Short: fmt.Sprintf("Create a new %s", sub.noun),
		Long: fmt.Sprintf(`Create a new JumpCloud %s.

Use --field "Label=Value" (repeatable) to set field values.
Example: --field "Name=JDOE-MBP" --field "Serial Number=C02X1234"`, sub.noun),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAssetSubCreate(cmd, sub, fieldFlags)
		},
	}

	cmd.Flags().StringArrayVar(&fieldFlags, "field", nil, `Set a field value as "Label=Value" (repeatable)`)

	return cmd
}

func runAssetSubCreate(cmd *cobra.Command, sub assetSubResource, fieldFlags []string) error {
	body, effects, err := buildAssetBody(fieldFlags)
	if err != nil {
		return err
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields specified. Use --field \"Label=Value\" to set field values")
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:     "create",
			Resource:   sub.noun,
			Target:     effectTarget(body),
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.Create(cmd.Context(), sub.endpoint, map[string]any{"fields": body})
	if err != nil {
		return err
	}

	flattened := flattenAssetFields([]json.RawMessage{result})
	opts := output.CurrentOptions()
	if len(flattened) > 0 {
		return output.WriteSingle(cmd.OutOrStdout(), flattened[0], opts)
	}
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAssetSubUpdateCmd(sub assetSubResource) *cobra.Command {
	var fieldFlags []string

	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: fmt.Sprintf("Update a %s", sub.noun),
		Long: fmt.Sprintf(`Update an existing JumpCloud %s.

Accepts a name or 24-character hex ID.
Use --field "Label=Value" (repeatable) to set field values.`, sub.noun),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAssetSubUpdate(cmd, sub, args[0], fieldFlags)
		},
	}

	cmd.Flags().StringArrayVar(&fieldFlags, "field", nil, `Set a field value as "Label=Value" (repeatable)`)

	return cmd
}

func runAssetSubUpdate(cmd *cobra.Command, sub assetSubResource, identifier string, fieldFlags []string) error {
	body, effects, err := buildAssetBody(fieldFlags)
	if err != nil {
		return err
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Use --field \"Label=Value\" to set field values")
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:     "update",
			Resource:   sub.noun,
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

	id, err := resolveAssetSub(cmd.Context(), client, identifier, sub.resolveConfig)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), sub.endpoint+"/"+id, map[string]any{"fields": body})
	if err != nil {
		return err
	}

	flattened := flattenAssetFields([]json.RawMessage{result})
	opts := output.CurrentOptions()
	if len(flattened) > 0 {
		return output.WriteSingle(cmd.OutOrStdout(), flattened[0], opts)
	}
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAssetSubDeleteCmd(sub assetSubResource) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <name-or-id>",
		Aliases: []string{"rm"},
		Short:   fmt.Sprintf("Delete a %s", sub.noun),
		Long: fmt.Sprintf(`Delete a JumpCloud %s.

Accepts a name or 24-character hex ID.
Use --force to skip the confirmation prompt.`, sub.noun),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAssetSubDelete(cmd, sub, args[0])
		},
	}

	return cmd
}

func runAssetSubDelete(cmd *cobra.Command, sub assetSubResource, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveAssetSub(cmd.Context(), client, identifier, sub.resolveConfig)
	if err != nil {
		return err
	}

	// Fetch the asset to show details in confirmation/plan.
	assetData, err := client.Get(cmd.Context(), sub.endpoint+"/"+id)
	if err != nil {
		return err
	}

	assetName := extractAssetNameFromData(assetData)

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: sub.noun,
			Target:   fmt.Sprintf("%s (%s)", assetName, id),
			Effects:  []string{"Remove " + sub.noun},
		}
		return renderPlan(cmd, p)
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete %s %q? [y/N] ", sub.noun, assetName)
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

	_, err = client.Delete(cmd.Context(), sub.endpoint+"/"+id)
	if err != nil {
		return err
	}

	// Capitalize first letter for display.
	label := strings.ToUpper(sub.noun[:1]) + sub.noun[1:]
	fmt.Fprintf(cmd.OutOrStdout(), "%s %q deleted successfully.\n", label, assetName)
	return nil
}

// resolveAssetSub resolves an asset name or ID to a JumpCloud asset ID.
func resolveAssetSub(ctx context.Context, client *api.V2Client, identifier string, cfg resolve.ResourceConfig) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, cfg)
}

// flattenAssetFields converts nested asset field structures to flat objects.
//
// Input:  {"id": "...", "fields": {"Name": {"editable": true, "value": "JDOE-MBP"}, ...}}
// Output: {"id": "...", "Name": "JDOE-MBP", ...}
//
// Value types:
//   - scalar (string/number/bool/null) → use directly
//   - object with "name" key (select reference) → extract name string
//   - array of objects with "name" key → extract ["name1", "name2"]
func flattenAssetFields(data []json.RawMessage) []json.RawMessage {
	result := make([]json.RawMessage, 0, len(data))
	for _, raw := range data {
		var obj struct {
			ID     string `json:"id"`
			Fields map[string]struct {
				Value json.RawMessage `json:"value"`
			} `json:"fields"`
		}
		if err := json.Unmarshal(raw, &obj); err != nil {
			result = append(result, raw) // pass through on parse failure
			continue
		}

		flat := map[string]any{"id": obj.ID}
		for label, field := range obj.Fields {
			flat[label] = flattenAssetValue(field.Value)
		}

		b, err := json.Marshal(flat)
		if err != nil {
			result = append(result, raw)
			continue
		}
		result = append(result, b)
	}
	return result
}

// flattenAssetValue extracts a user-friendly value from an asset field value.
func flattenAssetValue(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	// Try as object with "name" key (select reference).
	var ref struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &ref); err == nil && ref.Name != "" && ref.Type == "select" {
		return ref.Name
	}

	// Try as array of objects with "name" key.
	var refs []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &refs); err == nil && len(refs) > 0 && refs[0].Name != "" {
		names := make([]string, len(refs))
		for i, r := range refs {
			names[i] = r.Name
		}
		return names
	}

	// Try as scalar string.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// Try as number/bool — return raw JSON.
	var v any
	if err := json.Unmarshal(raw, &v); err == nil {
		return v
	}

	return string(raw)
}

// buildAssetBody parses --field "Label=Value" flags into a fields map.
// Returns the fields map, a list of effects for plan mode, and any error.
func buildAssetBody(fieldFlags []string) (map[string]string, []string, error) {
	fields := make(map[string]string)
	var effects []string

	for _, f := range fieldFlags {
		idx := strings.Index(f, "=")
		if idx < 0 {
			return nil, nil, fmt.Errorf("invalid --field format %q: expected \"Label=Value\"", f)
		}
		label := f[:idx]
		value := f[idx+1:]
		if label == "" {
			return nil, nil, fmt.Errorf("invalid --field format %q: label cannot be empty", f)
		}
		fields[label] = value
		effects = append(effects, fmt.Sprintf("%s: %s", label, value))
	}

	return fields, effects, nil
}

// effectTarget extracts a Name value from a fields map for plan display.
func effectTarget(fields map[string]string) string {
	if name, ok := fields["Name"]; ok {
		return name
	}
	return "(new asset)"
}

// extractAssetNameFromData extracts the Name field from raw asset JSON data.
func extractAssetNameFromData(data json.RawMessage) string {
	name, err := resolve.ExtractAssetName(data)
	if err != nil {
		return "(unknown)"
	}
	return name
}
