package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

// maxPackageSize is the per-file upload cap for JumpCloud's private repo
// (https://jumpcloud.com/support/manage-software-with-jumpcloud-private-repo).
const maxPackageSize = 5 * 1024 * 1024 * 1024 // 5 GB

// supportedPackageExtensions maps JumpCloud-supported package file extensions
// to a human-readable label. JumpCloud's private repo only accepts MSI, PKG,
// and IPA — DMG/EXE/DEB are explicitly not supported.
var supportedPackageExtensions = map[string]string{
	".msi": "Windows MSI",
	".pkg": "macOS PKG",
	".ipa": "iOS IPA",
}

// uploadPollInterval and uploadPollTimeout bound the wait for server-side
// validation after a binary is uploaded. Validation typically lands in a few
// seconds; 2 minutes is generous for occasional slow paths.
const (
	uploadPollInterval = 3 * time.Second
	uploadPollTimeout  = 2 * time.Minute
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
		filePath       string
		noWait         bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new software app",
		Long: `Create a new JumpCloud software app.

Required fields: --name.

Package settings can be specified in three ways:

1. Individual flags (recommended for single-package apps):
   --package-id firefox --package-manager CHOCOLATEY

2. File upload to JumpCloud's private repo (APPLE_CUSTOM, WINDOWS_MDM, etc.):
   --file ./MyApp.pkg --package-manager APPLE_CUSTOM

3. Raw JSON (for advanced/multi-package use):
   --settings '[{"packageId":"firefox","packageManager":"CHOCOLATEY"}]'

Valid package managers: APPLE_CUSTOM, APPLE_VPP, CHOCOLATEY, GOOGLE_ANDROID,
MICROSOFT_STORE, WINDOWS_MDM, WINGET.

File uploads (--file): accepts .msi/.pkg/.ipa up to 5 GB. Packages must be
digitally signed with a trusted, unexpired cert — unsigned packages will be
rejected by JumpCloud after upload. See
https://jumpcloud.com/support/manage-software-with-jumpcloud-private-repo

If --settings is provided, it takes precedence over individual package flags.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSoftwareCreate(cmd, softwareCreateOpts{
				name:           name,
				settings:       settings,
				packageID:      packageID,
				packageManager: packageManager,
				desiredState:   desiredState,
				location:       location,
				description:    description,
				autoUpdate:     autoUpdate,
				filePath:       filePath,
				noWait:         noWait,
			})
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
	cmd.Flags().StringVar(&filePath, "file", "", "Path to a .msi/.pkg/.ipa package to upload (≤5 GB)")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "Skip polling for server-side package validation after upload")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

// softwareCreateOpts groups create flags so the signature stays manageable.
type softwareCreateOpts struct {
	name           string
	settings       string
	packageID      string
	packageManager string
	desiredState   string
	location       string
	description    string
	autoUpdate     bool
	filePath       string
	noWait         bool
}

func runSoftwareCreate(cmd *cobra.Command, o softwareCreateOpts) error {
	// Flag-combination validation first (cheap, no I/O).
	if o.filePath != "" {
		if o.settings != "" {
			return fmt.Errorf("--file cannot be combined with --settings")
		}
		if o.packageID != "" {
			return fmt.Errorf("--file cannot be combined with --package-id (--file uploads to JumpCloud object storage; --package-id is for externally hosted packages)")
		}
		if o.packageManager == "" {
			return fmt.Errorf("--package-manager is required with --file (typically APPLE_CUSTOM for .pkg, WINDOWS_MDM for .msi, APPLE_VPP for .ipa)")
		}
	}
	if o.packageID != "" && o.packageManager == "" {
		return fmt.Errorf("--package-manager is required when using --package-id")
	}
	if o.packageManager != "" {
		var err error
		o.packageManager, err = validatePackageManager(o.packageManager)
		if err != nil {
			return err
		}
	}

	// Stat the file to validate extension/size early, even in plan mode.
	var fileInfo os.FileInfo
	if o.filePath != "" {
		if err := validatePackageExtension(o.filePath); err != nil {
			return err
		}
		info, err := os.Stat(o.filePath)
		if err != nil {
			return fmt.Errorf("reading --file: %w", err)
		}
		if info.IsDir() {
			return fmt.Errorf("--file must be a regular file, not a directory: %s", o.filePath)
		}
		if info.Size() > maxPackageSize {
			return fmt.Errorf("--file is %d bytes; JumpCloud's per-file limit is 5 GB", info.Size())
		}
		if info.Size() == 0 {
			return fmt.Errorf("--file is empty: %s", o.filePath)
		}
		fileInfo = info
	}

	if viper.GetBool("plan") {
		effects := []string{"displayName: " + o.name}
		switch {
		case o.settings != "":
			effects = append(effects, "settings: (raw JSON)")
		case o.filePath != "":
			effects = append(effects,
				"packageManager: "+o.packageManager,
				"desiredState: "+o.desiredState,
				fmt.Sprintf("file: %s (%d bytes)", o.filePath, fileInfo.Size()),
				"upload: POST /softwareapps then PUT binary to presigned S3 URL",
			)
		case o.packageID != "":
			effects = append(effects,
				"packageId: "+o.packageID,
				"packageManager: "+o.packageManager,
				"desiredState: "+o.desiredState,
			)
			if o.location != "" {
				effects = append(effects, "location: "+o.location)
			}
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "software app",
			Target:     o.name,
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
		"displayName": o.name,
	}

	switch {
	case o.settings != "":
		if !json.Valid([]byte(o.settings)) {
			return fmt.Errorf("parsing --settings: invalid JSON")
		}
		body["settings"] = json.RawMessage(o.settings)
	case o.filePath != "":
		fileName := filepath.Base(o.filePath)
		fmt.Fprintf(cmd.ErrOrStderr(), "Hashing %s (%d bytes)...\n", fileName, fileInfo.Size())
		sha, err := hashFileSHA256(o.filePath)
		if err != nil {
			return err
		}
		pkg := map[string]any{
			"packageManager": o.packageManager,
			"desiredState":   o.desiredState,
			"storedPackage": map[string]any{
				"versions": []any{
					map[string]any{
						"name":      fileName,
						"version":   1,
						"size":      fileInfo.Size(),
						"sha256sum": sha,
					},
				},
			},
		}
		if o.location != "" {
			pkg["location"] = o.location
		}
		if o.description != "" {
			pkg["description"] = o.description
		}
		if o.autoUpdate {
			pkg["autoUpdate"] = true
		}
		body["settings"] = []any{pkg}
	case o.packageID != "":
		pkg := map[string]any{
			"packageId":      o.packageID,
			"packageManager": o.packageManager,
			"desiredState":   o.desiredState,
		}
		if o.location != "" {
			pkg["location"] = o.location
		}
		if o.description != "" {
			pkg["description"] = o.description
		}
		if o.autoUpdate {
			pkg["autoUpdate"] = true
		}
		body["settings"] = []any{pkg}
	}

	result, err := client.Create(cmd.Context(), "/softwareapps", body)
	if err != nil {
		return err
	}

	// For file uploads, extract uploadUrl, PUT the file, and optionally poll.
	if o.filePath != "" {
		var createResp struct {
			ID        string `json:"id"`
			UploadURL string `json:"uploadUrl"`
		}
		if err := json.Unmarshal(result, &createResp); err != nil {
			return fmt.Errorf("parsing create response: %w", err)
		}
		if createResp.UploadURL == "" {
			return fmt.Errorf("server did not return uploadUrl; verify the package manager supports custom binary uploads")
		}

		fmt.Fprintf(cmd.ErrOrStderr(), "Uploading %s...\n", filepath.Base(o.filePath))
		if err := uploadFileToPresigned(cmd.Context(), o.filePath, createResp.UploadURL, fileInfo.Size()); err != nil {
			return err
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "Upload complete.")

		if o.noWait {
			fmt.Fprintf(cmd.ErrOrStderr(), "Skipped polling. Check validation status with: jc software get %s\n", createResp.ID)
		} else {
			finalResult, err := waitForPackageValidation(cmd.Context(), client, createResp.ID, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			result = finalResult
		}
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

// validatePackageExtension returns an error if the file's extension is not one
// of JumpCloud's supported private-repo formats (.msi/.pkg/.ipa).
func validatePackageExtension(path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := supportedPackageExtensions[ext]; !ok {
		return fmt.Errorf("unsupported package format %q: JumpCloud's private repo accepts .msi, .pkg, and .ipa only (see https://jumpcloud.com/support/manage-software-with-jumpcloud-private-repo)", ext)
	}
	return nil
}

// hashFileSHA256 streams the file through SHA-256 and returns the lowercase
// hex digest. Streams rather than buffering to stay memory-bounded for large
// packages.
func hashFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening file for hashing: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing file: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// uploadFileToPresigned opens the file and streams it to the presigned URL.
// Re-opens the file (it was already read once for hashing); streaming rather
// than buffering avoids loading a 5 GB file into memory.
func uploadFileToPresigned(ctx context.Context, path, url string, size int64) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening file for upload: %w", err)
	}
	defer f.Close()
	return api.PutPresigned(ctx, url, f, size)
}

// waitForPackageValidation polls the software app until the stored package
// leaves PENDING/VALIDATING. Returns the latest app JSON. Surfaces
// rejectedReason verbatim if the server rejects the package (typically due to
// an unsigned binary or invalid cert).
func waitForPackageValidation(ctx context.Context, client *api.V2Client, appID string, progressOut io.Writer) (json.RawMessage, error) {
	fmt.Fprint(progressOut, "Waiting for server-side validation")
	defer fmt.Fprintln(progressOut)

	deadline := time.Now().Add(uploadPollTimeout)
	var last json.RawMessage
	for {
		fmt.Fprint(progressOut, ".")

		data, err := client.Get(ctx, "/softwareapps/"+appID)
		if err != nil {
			return nil, err
		}
		last = data

		var parsed struct {
			Settings []struct {
				StoredPackage struct {
					Versions []struct {
						Status         string `json:"status"`
						RejectedReason string `json:"rejectedReason"`
					} `json:"versions"`
				} `json:"storedPackage"`
			} `json:"settings"`
		}
		if err := json.Unmarshal(data, &parsed); err == nil && len(parsed.Settings) > 0 && len(parsed.Settings[0].StoredPackage.Versions) > 0 {
			v := parsed.Settings[0].StoredPackage.Versions[0]
			switch v.Status {
			case "AVAILABLE":
				fmt.Fprint(progressOut, " AVAILABLE")
				return last, nil
			case "REJECTED":
				fmt.Fprint(progressOut, " REJECTED")
				return last, fmt.Errorf("package rejected by JumpCloud: %s", v.RejectedReason)
			}
		}

		if time.Now().After(deadline) {
			fmt.Fprint(progressOut, " timeout")
			return last, fmt.Errorf("validation did not complete within %s; check status with: jc software get %s", uploadPollTimeout, appID)
		}

		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(uploadPollInterval):
		}
	}
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
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.SoftwareAppConfig),
		RunE: batchRunE("software app", "delete", runSoftwareDelete),
	}

	addBatchSourceFlags(cmd)
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
