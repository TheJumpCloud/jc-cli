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

// softwareDefaultFields is the default field subset shown for software app output.
var softwareDefaultFields = []string{"id", "displayName", "createdAt", "updatedAt"}

// validPackageManagers is the set of accepted package manager values.
var validPackageManagers = []string{
	"APPLE_CUSTOM", "APPLE_VPP", "CHOCOLATEY",
	"GOOGLE_ANDROID", "MICROSOFT_STORE", "WINDOWS_MDM", "WINGET",
}

func validatePackageManager(value string) (string, error) {
	upper := strings.ToUpper(value)
	for _, v := range validPackageManagers {
		if upper == v {
			return v, nil
		}
	}
	return "", fmt.Errorf("invalid package manager %q, must be one of: %s", value, strings.Join(validPackageManagers, ", "))
}

// resolveSoftwareApp resolves a software app name or ID to a JumpCloud software app ID.
func resolveSoftwareApp(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.SoftwareAppConfig)
}

func newSoftwareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "software",
		Short: "Manage JumpCloud software apps",
		Long:  "List, get, create, update, and delete JumpCloud software application deployments.",
	}

	cmd.AddCommand(newSoftwareListCmd())
	cmd.AddCommand(newSoftwareGetCmd())
	cmd.AddCommand(newSoftwareCreateCmd())
	cmd.AddCommand(newSoftwareUpdateCmd())
	cmd.AddCommand(newSoftwareDeleteCmd())
	cmd.AddCommand(newSoftwareStatusesCmd())
	cmd.AddCommand(newSoftwareAssociationsCmd())
	cmd.AddCommand(newSoftwareReclaimLicenseCmd())

	return cmd
}

func newSoftwareListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all software apps",
		Long: `List all JumpCloud software apps.

Default fields: id, displayName, createdAt, updatedAt.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'displayName=Firefox'     Exact match`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSoftwareList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -displayName)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'displayName=Firefox')")

	return cmd
}

func runSoftwareList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/softwareapps", api.V2ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = softwareDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newSoftwareGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get a software app by name or ID",
		Long: `Get a single JumpCloud software app by name or ID.

Accepts a software app displayName (e.g., "Firefox") or a 24-character hex ID.
Names are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SoftwareAppConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSoftwareGet(cmd, args[0])
		},
	}

	return cmd
}

func runSoftwareGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveSoftwareApp(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/softwareapps/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newSoftwareCreateCmd() *cobra.Command {
	var (
		name           string
		settings       string
		packageID      string
		packageManager string
		desiredState   string
		location       string
		description    string
		autoUpdate     bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new software app",
		Long: `Create a new JumpCloud software app.

Required fields: --name.

Package settings can be specified in two ways:

1. Individual flags (recommended for single-package apps):
   --package-id firefox --package-manager CHOCOLATEY

2. Raw JSON (for advanced/multi-package use):
   --settings '[{"packageId":"firefox","packageManager":"CHOCOLATEY"}]'

Valid package managers: APPLE_CUSTOM, APPLE_VPP, CHOCOLATEY, GOOGLE_ANDROID,
MICROSOFT_STORE, WINDOWS_MDM, WINGET.

If --settings is provided, it takes precedence over individual package flags.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSoftwareCreate(cmd, name, settings, packageID, packageManager, desiredState, location, description, autoUpdate)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Software app display name (required)")
	cmd.Flags().StringVar(&settings, "settings", "", "Settings as raw JSON string (advanced)")
	cmd.Flags().StringVar(&packageID, "package-id", "", "Package identifier (e.g. firefox, com.1password.1password)")
	cmd.Flags().StringVar(&packageManager, "package-manager", "", "Package manager: CHOCOLATEY, APPLE_CUSTOM, APPLE_VPP, WINGET, MICROSOFT_STORE, WINDOWS_MDM, GOOGLE_ANDROID")
	cmd.Flags().StringVar(&desiredState, "desired-state", "INSTALL", "Desired state (default: INSTALL)")
	cmd.Flags().StringVar(&location, "location", "", "Download URL for custom packages")
	cmd.Flags().StringVar(&description, "description", "", "Package description")
	cmd.Flags().BoolVar(&autoUpdate, "auto-update", false, "Enable automatic updates")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func runSoftwareCreate(cmd *cobra.Command, name, settings, packageID, packageManager, desiredState, location, description string, autoUpdate bool) error {
	// Validate package manager if individual flags are used.
	if packageID != "" && packageManager == "" {
		return fmt.Errorf("--package-manager is required when using --package-id")
	}
	if packageManager != "" {
		var err error
		packageManager, err = validatePackageManager(packageManager)
		if err != nil {
			return err
		}
	}

	if viper.GetBool("plan") {
		effects := []string{"displayName: " + name}
		if settings != "" {
			effects = append(effects, "settings: (raw JSON)")
		} else if packageID != "" {
			effects = append(effects, "packageId: "+packageID)
			effects = append(effects, "packageManager: "+packageManager)
			effects = append(effects, "desiredState: "+desiredState)
			if location != "" {
				effects = append(effects, "location: "+location)
			}
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "software app",
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
		"displayName": name,
	}

	if settings != "" {
		if !json.Valid([]byte(settings)) {
			return fmt.Errorf("parsing --settings: invalid JSON")
		}
		body["settings"] = json.RawMessage(settings)
	} else if packageID != "" {
		pkg := map[string]any{
			"packageId":      packageID,
			"packageManager": packageManager,
			"desiredState":   desiredState,
		}
		if location != "" {
			pkg["location"] = location
		}
		if description != "" {
			pkg["description"] = description
		}
		if autoUpdate {
			pkg["autoUpdate"] = true
		}
		body["settings"] = []any{pkg}
	}

	result, err := client.Create(cmd.Context(), "/softwareapps", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newSoftwareUpdateCmd() *cobra.Command {
	var (
		name           string
		settings       string
		packageID      string
		packageManager string
		desiredState   string
		location       string
		description    string
		autoUpdate     bool
	)

	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Update a software app",
		Long: `Update an existing JumpCloud software app.

Accepts a software app displayName or 24-character hex ID.
Specify only the fields you want to change.

Package settings can be updated via individual flags or raw JSON (--settings).
If --settings is provided, it replaces the entire settings array.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SoftwareAppConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSoftwareUpdate(cmd, args[0], name, settings, packageID, packageManager, desiredState, location, description, autoUpdate)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Software app display name")
	cmd.Flags().StringVar(&settings, "settings", "", "Settings as raw JSON string (replaces entire settings array)")
	cmd.Flags().StringVar(&packageID, "package-id", "", "Package identifier")
	cmd.Flags().StringVar(&packageManager, "package-manager", "", "Package manager: CHOCOLATEY, APPLE_CUSTOM, APPLE_VPP, WINGET, MICROSOFT_STORE, WINDOWS_MDM, GOOGLE_ANDROID")
	cmd.Flags().StringVar(&desiredState, "desired-state", "", "Desired state (e.g. INSTALL)")
	cmd.Flags().StringVar(&location, "location", "", "Download URL for custom packages")
	cmd.Flags().StringVar(&description, "description", "", "Package description")
	cmd.Flags().BoolVar(&autoUpdate, "auto-update", false, "Enable automatic updates")

	return cmd
}

func runSoftwareUpdate(cmd *cobra.Command, identifier, name, settings, packageID, packageManager, desiredState, location, description string, autoUpdate bool) error {
	if packageManager != "" {
		var err error
		packageManager, err = validatePackageManager(packageManager)
		if err != nil {
			return err
		}
	}

	body := map[string]any{}

	if cmd.Flags().Changed("name") {
		body["displayName"] = name
	}
	if cmd.Flags().Changed("settings") {
		if !json.Valid([]byte(settings)) {
			return fmt.Errorf("parsing --settings: invalid JSON")
		}
		body["settings"] = json.RawMessage(settings)
	} else {
		// Build settings from individual flags if any were provided.
		pkg := map[string]any{}
		if cmd.Flags().Changed("package-id") {
			pkg["packageId"] = packageID
		}
		if cmd.Flags().Changed("package-manager") {
			pkg["packageManager"] = packageManager
		}
		if cmd.Flags().Changed("desired-state") {
			pkg["desiredState"] = desiredState
		}
		if cmd.Flags().Changed("location") {
			pkg["location"] = location
		}
		if cmd.Flags().Changed("description") {
			pkg["description"] = description
		}
		if cmd.Flags().Changed("auto-update") {
			pkg["autoUpdate"] = autoUpdate
		}
		if len(pkg) > 0 {
			body["settings"] = []any{pkg}
		}
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --name, --package-id, --settings)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "software app",
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

	id, err := resolveSoftwareApp(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/softwareapps/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newSoftwareDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <name-or-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a software app",
		Long: `Delete a JumpCloud software app.

Accepts a software app displayName or 24-character hex ID.
Shows the software app name before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SoftwareAppConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSoftwareDelete(cmd, args[0])
		},
	}

	return cmd
}

func runSoftwareDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveSoftwareApp(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch the software app first so we can show details in the confirmation/plan.
	appData, err := client.Get(cmd.Context(), "/softwareapps/"+id)
	if err != nil {
		return err
	}

	var app struct {
		DisplayName string `json:"displayName"`
	}
	if err := json.Unmarshal(appData, &app); err != nil {
		return fmt.Errorf("parsing software app data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "software app",
			Target:   fmt.Sprintf("%s (%s)", app.DisplayName, id),
			Effects:  []string{"Remove software app deployment"},
		}
		return renderPlan(cmd, p)
	}

	// Confirmation prompt (unless --force is set).
	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete software app %q? [y/N] ", app.DisplayName)
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

	_, err = client.Delete(cmd.Context(), "/softwareapps/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Software app %q deleted successfully.\n", app.DisplayName)
	return nil
}

// softwareStatusDefaultFields is the default field subset shown for software status output.
var softwareStatusDefaultFields = []string{"systemId", "status", "lastUpdate"}

func newSoftwareStatusesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "statuses <name-or-id>",
		Short: "List deployment statuses for a software app",
		Long: `List deployment statuses for a JumpCloud software app.

Accepts a software app displayName or 24-character hex ID.
Returns per-device deployment status information.

Default fields: systemId, status, lastUpdate.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SoftwareAppConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSoftwareStatuses(cmd, args[0])
		},
	}

	return cmd
}

func runSoftwareStatuses(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveSoftwareApp(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/softwareapps/"+id+"/statuses", api.V2ListOptions{})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = softwareStatusDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newSoftwareAssociationsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "associations <name-or-id>",
		Short: "List associations for a software app",
		Long: `List system associations for a JumpCloud software app.

Accepts a software app displayName or 24-character hex ID.
Returns the list of systems associated with the software app.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SoftwareAppConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSoftwareAssociations(cmd, args[0])
		},
	}

	return cmd
}

func runSoftwareAssociations(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveSoftwareApp(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/softwareapps/"+id+"/associations?targets=system", api.V2ListOptions{})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newSoftwareReclaimLicenseCmd() *cobra.Command {
	var deviceFlag string

	cmd := &cobra.Command{
		Use:   "reclaim-license <name-or-id>",
		Short: "Reclaim a software license from a device",
		Long: `Reclaim a software app license from a specific device.

Accepts a software app displayName or 24-character hex ID.
Requires --device with the target device hostname or ID.
Use --force to skip the confirmation prompt.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SoftwareAppConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSoftwareReclaimLicense(cmd, args[0], deviceFlag)
		},
	}

	cmd.Flags().StringVar(&deviceFlag, "device", "", "Device hostname or ID (required)")
	_ = cmd.MarkFlagRequired("device")

	return cmd
}

func runSoftwareReclaimLicense(cmd *cobra.Command, identifier, device string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveSoftwareApp(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Resolve device hostname or ID.
	v1Client, err := newV1Client()
	if err != nil {
		return err
	}
	deviceID, err := resolveDevice(cmd.Context(), v1Client, device)
	if err != nil {
		return err
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "reclaim-license",
			Resource: "software app",
			Target:   fmt.Sprintf("%s (device: %s)", id, deviceID),
			Effects:  []string{"Reclaim software license from device"},
		}
		return renderPlan(cmd, p)
	}

	// Confirmation prompt (unless --force is set).
	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Reclaim license for software app %q from device %q? [y/N] ", id, deviceID)
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

	body := map[string]any{
		"systemId": deviceID,
	}

	_, err = client.Create(cmd.Context(), "/softwareapps/"+id+"/reclaim-licenses", body)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "License reclaimed successfully for software app %s from device %s.\n", id, deviceID)
	return nil
}
