package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/bundle"
	"github.com/klaassen-consulting/jc/internal/mscp"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
)

// bundleDefaultFields is the field subset for bundle list table output.
var bundleDefaultFields = []string{"name", "version", "origin", "platforms", "policies"}

func newBundleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Manage security baseline bundles (pre-configured policy sets)",
		Long: `Bundles are versioned YAML artifacts grouping multiple policies —
Apple multi-payload profiles and Windows OMA-URI / registry policies —
into one named baseline.

Built-in bundles are compiled into jc. User-defined bundles live in
~/.config/jc/bundles/; a user bundle with the same name overrides the
built-in one. Every bundle carries a source block recording where its
content came from (licensing provenance).`,
	}

	cmd.AddCommand(newBundleListCmd())
	cmd.AddCommand(newBundleShowCmd())
	cmd.AddCommand(newBundleValidateCmd())
	cmd.AddCommand(newBundleExportCmd())
	cmd.AddCommand(newBundleApplyCmd())
	cmd.AddCommand(newBundleStatusCmd())
	cmd.AddCommand(newBundleImportCmd())

	return cmd
}

func newBundleImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Generate bundles from external baseline sources",
	}
	cmd.AddCommand(newBundleImportMSCPCmd())
	return cmd
}

func newBundleImportMSCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mscp",
		Short: "Generate a bundle from a NIST mSCP baseline (cis_lvl1, cis_lvl2, DISA-STIG, ...)",
		Long: `Convert a NIST macOS Security Compliance Project baseline into a jc
bundle. The pinned mSCP release (` + mscp.SnapshotTag + `) is downloaded once from
GitHub (SHA-256-verified) into the local cache; air-gapped hosts can
pre-place the zip — the error message shows where.

Only the configuration-profile-enforceable subset of a baseline
converts (mSCP enforces the rest via shell scripts, outside MDM's
reach) — the generated bundle's description records the exact
coverage. mSCP is CC BY 4.0; the generated bundle carries the
attribution in its source block.

The bundle is written to ~/.config/jc/bundles/ (or --output) and is
then usable like any other: jc bundle apply/status/export.

Examples:
  jc bundle import mscp --baseline cis_lvl1
  jc bundle import mscp --baseline cis_lvl2 --output ./cis2.yaml
  jc bundle import mscp --baseline DISA-STIG --name macos-stig`,
		RunE: runBundleImportMSCP,
	}
	cmd.Flags().String("baseline", "", "mSCP baseline manifest name (required; e.g. cis_lvl1, cis_lvl2, DISA-STIG, 800-53r5_moderate)")
	cmd.Flags().String("name", "", `Bundle name (default "macos-<baseline>", lowercased)`)
	cmd.Flags().String("output", "", "Write the bundle YAML here instead of ~/.config/jc/bundles/<name>.yaml")
	cmd.Flags().Bool("refresh", false, "Re-download the mSCP snapshot even if cached")
	_ = cmd.MarkFlagRequired("baseline")
	return cmd
}

func runBundleImportMSCP(cmd *cobra.Command, args []string) error {
	baseline, _ := cmd.Flags().GetString("baseline")
	name, _ := cmd.Flags().GetString("name")
	outPath, _ := cmd.Flags().GetString("output")
	refresh, _ := cmd.Flags().GetBool("refresh")

	if name == "" {
		name = "macos-" + strings.ToLower(strings.ReplaceAll(baseline, "_", "-"))
	}

	if refresh {
		if err := os.RemoveAll(mscp.CacheDir()); err != nil {
			return fmt.Errorf("clearing mSCP cache: %w", err)
		}
		// EnsureSnapshot re-extracts a pre-placed sibling <tag>.zip
		// without re-downloading, so removing only the extracted dir
		// leaves --refresh re-extracting the stale archive. Drop the
		// zip too so a genuine re-download happens.
		if err := os.Remove(mscp.CacheDir() + ".zip"); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clearing mSCP cache archive: %w", err)
		}
	}

	progress := func(msg string) { fmt.Fprintln(cmd.ErrOrStderr(), msg) }
	dir, err := mscp.EnsureSnapshot(cmd.Context(), "", progress)
	if err != nil {
		return err
	}

	manifest, err := mscp.LoadBaseline(dir, baseline)
	if err != nil {
		return err
	}
	rules, err := mscp.LoadRules(dir)
	if err != nil {
		return err
	}

	b, report, err := mscp.Convert(rules, manifest, name, "imported")
	if err != nil {
		return err
	}

	data, err := bundle.MarshalYAML(b)
	if err != nil {
		return err
	}
	if outPath == "" {
		dir := bundle.BundlesDir()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("creating bundles directory: %w", err)
		}
		outPath = filepath.Join(dir, name+".yaml")
	}
	if err := os.WriteFile(outPath, data, 0o600); err != nil {
		return fmt.Errorf("writing bundle: %w", err)
	}

	fmt.Fprint(cmd.ErrOrStderr(), report.Summary())
	fmt.Fprintf(cmd.OutOrStdout(), "Bundle %q written to %s\n", name, outPath)
	fmt.Fprintf(cmd.ErrOrStderr(), "Preview with `jc bundle show %s`, then `jc bundle apply %s --plan`.\n", name, name)
	return nil
}

func newBundleStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [name]",
		Short: "Compare an applied bundle against the tenant (drift detection)",
		Long: `Check whether the tenant still matches a bundle's definition.

Finds the bundle's policy group (by its bundle:<name>@<version>
description marker, falling back to the default group name), lists its
member policies, decodes each one, and diffs values against the bundle.

Per-unit states: in-sync, drifted (with value-level diffs), missing
(policy deleted or renamed). Member policies that match no bundle unit
are reported as orphans. Read-only — nothing is changed.

Examples:
  jc bundle status example-baseline
  jc bundle status --file my-baseline.yaml`,
		Args: cobra.MaximumNArgs(1),
		RunE: runBundleStatus,
	}
	cmd.Flags().String("file", "", "Check a bundle file instead of a named bundle")
	return cmd
}

func runBundleStatus(cmd *cobra.Command, args []string) error {
	file, _ := cmd.Flags().GetString("file")

	var b *bundle.Bundle
	var err error
	switch {
	case file != "" && len(args) > 0:
		return fmt.Errorf("pass a bundle name or --file, not both")
	case file != "":
		b, err = bundle.ParseFile(file)
	case len(args) == 1:
		b, err = findBundleByName(args[0])
	default:
		return fmt.Errorf("pass a bundle name or --file")
	}
	if err != nil {
		return err
	}

	cat, err := apple_mdm.Default()
	if err != nil {
		return err
	}
	client, err := newV2Client()
	if err != nil {
		return fmt.Errorf("building v2 client: %w", err)
	}

	report, err := bundle.Status(cmd.Context(), client, b, cat)
	if err != nil {
		return err
	}

	raw, err := json.Marshal(report)
	if err != nil {
		return err
	}
	if err := output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions()); err != nil {
		return err
	}

	// Human summary on stderr: one line per unit + verdict.
	w := cmd.ErrOrStderr()
	for _, u := range report.Units {
		fmt.Fprintf(w, "  %-9s %s\n", u.State, u.PolicyName)
		for _, d := range u.Diffs {
			fmt.Fprintf(w, "            ↳ %s\n", d)
		}
	}
	for _, o := range report.Orphans {
		fmt.Fprintf(w, "  %-9s %s (member of the policy group but not in the bundle)\n", "orphan", o)
	}
	if report.InSync {
		fmt.Fprintf(w, "Bundle %s v%s is in sync (%d units).\n", report.Bundle, report.Version, len(report.Units))
	} else {
		fmt.Fprintf(w, "Bundle %s v%s has drifted.\n", report.Bundle, report.Version)
	}
	return nil
}

func newBundleApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply [name]",
		Short: "Apply a bundle: create its policies, a policy group, and optionally bind a device group",
		Long: `Apply a bundle to the tenant. Creates one policy per unit (named
"<bundle>/<unit>"), a policy group holding them all (named
"<bundle> (v<version>)" unless overridden), and — with --group — binds
that policy group to a device group so every device in it receives the
baseline.

Apply is create-only: it refuses to run when any of the names it would
create already exist (delete the previous apply, bump the bundle
version, or pass --policy-group-name). On a mid-apply failure nothing
is rolled back; the exact cleanup commands are printed instead.

Use --plan to preview every step without any writes.

Examples:
  jc bundle apply example-baseline --plan
  jc bundle apply example-baseline --group "Corp Macs"
  jc bundle apply --file my-baseline.yaml --policy-group-name "Pilot baseline"`,
		Args: cobra.MaximumNArgs(1),
		RunE: runBundleApply,
	}
	cmd.Flags().String("file", "", "Apply a bundle file instead of a named bundle")
	cmd.Flags().String("group", "", "Device group (name or ID) to bind the policy group to")
	cmd.Flags().String("policy-group-name", "", `Policy group name (default "<bundle> (v<version>)")`)
	return cmd
}

func runBundleApply(cmd *cobra.Command, args []string) error {
	file, _ := cmd.Flags().GetString("file")

	var b *bundle.Bundle
	var err error
	switch {
	case file != "" && len(args) > 0:
		return fmt.Errorf("pass a bundle name or --file, not both")
	case file != "":
		b, err = bundle.ParseFile(file)
	case len(args) == 1:
		b, err = findBundleByName(args[0])
	default:
		return fmt.Errorf("pass a bundle name or --file")
	}
	if err != nil {
		return err
	}

	cat, err := apple_mdm.Default()
	if err != nil {
		return err
	}
	client, err := newV2Client()
	if err != nil {
		return fmt.Errorf("building v2 client: %w", err)
	}
	ctx := cmd.Context()

	opts := bundle.ApplyOptions{}
	opts.PolicyGroupName, _ = cmd.Flags().GetString("policy-group-name")
	if group, _ := cmd.Flags().GetString("group"); group != "" {
		id, err := resolveDeviceGroup(ctx, client, group)
		if err != nil {
			return fmt.Errorf("resolving device group %q: %w", group, err)
		}
		opts.DeviceGroupID, opts.DeviceGroupName = id, group
	}

	applyPlan, err := bundle.BuildApplyPlan(ctx, client, b, cat, opts)
	if err != nil {
		return err
	}

	if viper.GetBool("plan") {
		effects := make([]string, 0, len(applyPlan.Steps))
		for _, s := range applyPlan.Steps {
			effects = append(effects, fmt.Sprintf("%s %q — %s", s.Kind, s.Name, s.Detail))
		}
		p := &plan.Plan{
			Action:   "apply",
			Resource: "security baseline bundle",
			Target:   fmt.Sprintf("%s (v%s)", b.Name, b.Version),
			Effects:  effects,
			// Reversible via the printed cleanup path, but multi-object.
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	result, err := applyPlan.Execute(ctx, client)
	if err != nil {
		// Partial-failure contract: the error already carries the
		// created-objects report + cleanup commands.
		return err
	}

	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if err := output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions()); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Applied bundle %s v%s: %d policies, policy group %q",
		b.Name, b.Version, len(result.Created)-1, applyPlan.PolicyGroupName)
	if result.Bound {
		fmt.Fprintf(cmd.ErrOrStderr(), ", bound to device group %q", applyPlan.DeviceGroupName)
	}
	fmt.Fprintln(cmd.ErrOrStderr())
	return nil
}

func newBundleListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all available bundles (built-in + user-defined)",
		RunE:    runBundleList,
	}
}

func runBundleList(cmd *cobra.Command, args []string) error {
	bundles, err := bundle.LoadAll()
	if err != nil {
		return err
	}

	var data []json.RawMessage
	for _, b := range bundles {
		summary := map[string]interface{}{
			"name":      b.Name,
			"version":   b.Version,
			"origin":    b.Source.Origin,
			"platforms": strings.Join(b.Platforms(), ", "),
			"policies":  len(b.Policies),
		}
		if b.Description != "" {
			summary["description"] = b.Description
		}
		raw, err := json.Marshal(summary)
		if err != nil {
			return err
		}
		data = append(data, raw)
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = bundleDefaultFields
	if err := output.WriteList(cmd.OutOrStdout(), data, opts); err != nil {
		return err
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "── %d bundles ──\n", len(data))
	return nil
}

func newBundleShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Display a bundle in full: metadata, source, and every policy unit",
		Args:  cobra.ExactArgs(1),
		RunE:  runBundleShow,
	}
}

// findBundleByName loads all bundles and resolves one by name, listing
// the available names on a miss (recipe show precedent).
func findBundleByName(name string) (*bundle.Bundle, error) {
	bundles, err := bundle.LoadAll()
	if err != nil {
		return nil, err
	}
	b := bundle.FindByName(bundles, name)
	if b == nil {
		available := make([]string, 0, len(bundles))
		for _, x := range bundles {
			available = append(available, x.Name)
		}
		return nil, fmt.Errorf("bundle %q not found. Available bundles: %s", name, strings.Join(available, ", "))
	}
	return b, nil
}

func runBundleShow(cmd *cobra.Command, args []string) error {
	b, err := findBundleByName(args[0])
	if err != nil {
		return err
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
}

func newBundleValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate [name]",
		Short: "Deep-validate a bundle offline (Apple payloads + Windows settings)",
		Long: `Validate a bundle by name or from a file. Structural checks and deep
validation both run: Apple payloads validate against the embedded
schema catalog, Windows OMA-URI settings and registry keys through the
same rules the create-policy commands enforce. Everything is offline —
no JumpCloud API calls, no catalog downloads.

Examples:
  jc bundle validate example-baseline
  jc bundle validate --file my-baseline.yaml`,
		Args: cobra.MaximumNArgs(1),
		RunE: runBundleValidate,
	}
	cmd.Flags().String("file", "", "Validate a bundle file instead of a named bundle")
	return cmd
}

func runBundleValidate(cmd *cobra.Command, args []string) error {
	file, _ := cmd.Flags().GetString("file")

	var b *bundle.Bundle
	var err error
	var what string
	switch {
	case file != "" && len(args) > 0:
		return fmt.Errorf("pass a bundle name or --file, not both")
	case file != "":
		b, err = bundle.ParseFile(file)
		what = file
	case len(args) == 1:
		b, err = findBundleByName(args[0])
		what = args[0]
	default:
		return fmt.Errorf("pass a bundle name or --file")
	}
	if err != nil {
		return err
	}

	cat, err := apple_mdm.Default()
	if err != nil {
		return err
	}
	if err := bundle.Validate(b, cat); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Bundle %s is valid (%d policy units).\n", what, len(b.Policies))
	return nil
}

func newBundleExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <name>",
		Short: "Export a bundle as YAML (fork a builtin into your own baseline)",
		Long: `Export a bundle's YAML to stdout or a file. The exported file is a
complete, portable bundle: edit it and drop it into ~/.config/jc/bundles/
(same name overrides the builtin) or apply it directly with --file.

Examples:
  jc bundle export example-baseline
  jc bundle export example-baseline --file my-baseline.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: runBundleExport,
	}
	cmd.Flags().String("file", "", "Write the bundle to a file instead of stdout")
	return cmd
}

func runBundleExport(cmd *cobra.Command, args []string) error {
	b, err := findBundleByName(args[0])
	if err != nil {
		return err
	}
	data, err := bundle.MarshalYAML(b)
	if err != nil {
		return fmt.Errorf("marshaling bundle: %w", err)
	}

	outFile, _ := cmd.Flags().GetString("file")
	if outFile != "" {
		if err := os.WriteFile(outFile, data, 0o600); err != nil {
			return fmt.Errorf("writing file: %w", err)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Bundle %q exported to %s\n", args[0], outFile)
		return nil
	}
	_, err = cmd.OutOrStdout().Write(data)
	return err
}
