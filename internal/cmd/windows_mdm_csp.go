package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// jc windows-mdm csp — the Policy CSP discovery catalog (KLA-460).
// Read-only: list/show/template never touch the JumpCloud API. The
// catalog data is Microsoft's DDF v2 snapshot, fetched on demand from
// Microsoft's official URL (SHA-256-pinned) and cached locally —
// never vendored into the binary, because Microsoft's download terms
// don't permit redistribution (unlike Apple's MIT schema repo).

func newWindowsMDMCSPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "csp",
		Short: "Browse the Windows Policy CSP settings catalog",
		Long: `Browse Microsoft's Policy CSP settings catalog — the discovery half
of ` + "`jc windows-mdm`" + `. Turns "I want BitLocker-style device control" into
the exact OMA-URI path, wire format, and allowed values, then
` + "`csp template`" + ` emits the {uri, format, value} triple that
` + "`oma-uri create-policy --settings-file`" + ` consumes.

The catalog is Microsoft's DDF v2 snapshot (` + windows_mdm.SnapshotName + `),
downloaded once from Microsoft's official URL (SHA-256-verified) into
the local cache — run ` + "`csp update`" + ` to prefetch. Air-gapped hosts can
place the zip at <cache>/` + windows_mdm.SnapshotName + `.zip manually.

Covers all Policy CSP areas (~230, ~3700 settings) including
ADMX-backed ones (flagged — their values need ADMX-style XML, not
plain scalars). Standalone CSPs (BitLocker CSP, Firewall CSP, VPNv2)
are NOT in this catalog; their OMA-URIs can still be used with
` + "`oma-uri create-policy`" + ` directly.`,
	}
	cmd.AddCommand(newWindowsMDMCSPListCmd())
	cmd.AddCommand(newWindowsMDMCSPShowCmd())
	cmd.AddCommand(newWindowsMDMCSPTemplateCmd())
	cmd.AddCommand(newWindowsMDMCSPUpdateCmd())
	return cmd
}

// cspProgress returns a progress printer that notes snapshot download
// steps on stderr, keeping stdout clean for the actual output.
func cspProgress(cmd *cobra.Command) func(string) {
	return func(msg string) {
		fmt.Fprintln(cmd.ErrOrStderr(), msg)
	}
}

func newWindowsMDMCSPListCmd() *cobra.Command {
	var (
		area        string
		search      string
		scope       string
		excludeADMX bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Policy CSP settings",
		Example: `  jc windows-mdm csp list --area Camera
  jc windows-mdm csp list --search bitlocker
  jc windows-mdm csp list --search "screen capture" --scope device --exclude-admx
  jc windows-mdm csp list --area Update -o json | jq '.[].uri'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if scope != "" && scope != "device" && scope != "user" {
				return fmt.Errorf("--scope %q: want device or user", scope)
			}
			cat, err := windows_mdm.DefaultCatalog(cmd.Context(), cspProgress(cmd))
			if err != nil {
				return err
			}
			matches := cat.Filter(windows_mdm.FilterOpts{
				Area: area, Search: search, Scope: scope, ExcludeADMX: excludeADMX,
			})
			return renderCSPList(cmd.OutOrStdout(), matches, cat.Len())
		},
	}
	cmd.Flags().StringVar(&area, "area", "", "Restrict to one Policy CSP area (e.g. Camera, Update, ADMX_AppCompat)")
	cmd.Flags().StringVar(&search, "search", "", "Case-insensitive substring filter over area, name, URI, and description")
	cmd.Flags().StringVar(&scope, "scope", "", "Restrict to device- or user-scoped settings (JumpCloud's template is device-scoped)")
	cmd.Flags().BoolVar(&excludeADMX, "exclude-admx", false, "Drop ADMX-backed settings (their values need ADMX-style XML)")
	return cmd
}

func newWindowsMDMCSPShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <Area/PolicyName>",
		Short: "Show one Policy CSP setting in detail",
		Long: `Render one setting with its OMA-URI, wire format, description,
default, allowed values, OS-build applicability, and scope — everything
needed to author the value for ` + "`oma-uri create-policy`" + `.`,
		Example: `  jc windows-mdm csp show Camera/AllowCamera
  jc windows-mdm csp show DeviceLock/MaxDevicePasswordFailedAttempts -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cat, err := windows_mdm.DefaultCatalog(cmd.Context(), cspProgress(cmd))
			if err != nil {
				return err
			}
			s, ok := cat.ByRef(args[0])
			if !ok {
				return fmt.Errorf(
					"no Policy CSP setting %q in snapshot %s — try `jc windows-mdm csp list --search %s`",
					args[0], cat.Snapshot, lastPathSegment(args[0]))
			}
			return renderCSPShow(cmd.OutOrStdout(), s)
		},
	}
	return cmd
}

func newWindowsMDMCSPTemplateCmd() *cobra.Command {
	var outputFile string

	cmd := &cobra.Command{
		Use:   "template <Area/PolicyName> [<Area/PolicyName> ...]",
		Short: "Emit a settings-file stub for oma-uri create-policy",
		Long: `Emit the JSON array of {uri, format, value} triples that
` + "`jc windows-mdm oma-uri create-policy --settings-file`" + ` consumes —
closing the discover→author→create loop. Values are seeded from the
setting's default (or first allowed value); edit before creating.

User-scoped settings emit a warning: JumpCloud's Custom MDM (OMA-URI)
template is device-scoped, so user-scoped URIs may not apply.`,
		Example: `  jc windows-mdm csp template Camera/AllowCamera > camera.json
  jc windows-mdm csp template Camera/AllowCamera Bluetooth/AllowDiscoverableMode \
      --output-file lockdown.json
  # then:
  jc windows-mdm oma-uri create-policy --name "Lockdown" --settings-file lockdown.json --plan`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cat, err := windows_mdm.DefaultCatalog(cmd.Context(), cspProgress(cmd))
			if err != nil {
				return err
			}
			triples := make([]windows_mdm.OMAURISetting, 0, len(args))
			for _, ref := range args {
				s, ok := cat.ByRef(ref)
				if !ok {
					return fmt.Errorf("no Policy CSP setting %q in snapshot %s", ref, cat.Snapshot)
				}
				if s.ADMXBacked {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"Warning: %s is ADMX-backed — its value must be ADMX-style XML (<enabled/>, <disabled/>, or <enabled/><data .../>), not a plain scalar.\n", ref)
				}
				if s.Scope == "user" {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"Warning: %s is user-scoped; JumpCloud's Custom MDM (OMA-URI) template is device-scoped and may not apply it.\n", ref)
				}
				triples = append(triples, windows_mdm.TemplateSetting(s))
			}

			data, err := json.MarshalIndent(triples, "", "  ")
			if err != nil {
				return err
			}
			data = append(data, '\n')
			if outputFile != "" {
				if err := os.WriteFile(outputFile, data, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %s (%d settings)\n", outputFile, len(triples))
				return nil
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
	cmd.Flags().StringVar(&outputFile, "output-file", "", "Write the settings-file JSON to this path instead of stdout")
	return cmd
}

func newWindowsMDMCSPUpdateCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Fetch (or re-verify) the pinned Policy CSP snapshot",
		Long: `Download the pinned Microsoft DDF v2 snapshot into the local cache
so csp list/show/template work offline afterwards. Verifies the
SHA-256 pin. No-op when the snapshot is already cached; --force
deletes the cache and re-downloads.

The snapshot is pinned per jc release (` + windows_mdm.SnapshotName + `) —
newer Microsoft drops arrive via a jc upgrade, not this command.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := windows_mdm.CacheDir()
			if force {
				if err := os.RemoveAll(dir); err != nil {
					return err
				}
				if err := os.Remove(dir + ".zip"); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
			got, err := windows_mdm.EnsureSnapshot(cmd.Context(), "", cspProgress(cmd))
			if err != nil {
				return err
			}
			cat, err := windows_mdm.LoadCatalog(got)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Snapshot %s ready at %s (%d areas, %d settings)\n",
				windows_mdm.SnapshotName, got, len(cat.Areas()), cat.Len())
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Delete the cached snapshot and re-download")
	return cmd
}

// ── rendering ──────────────────────────────────────────────────────

func renderCSPList(w io.Writer, settings []windows_mdm.Setting, total int) error {
	opts := output.CurrentOptions()
	if opts.Format == output.FormatJSON || opts.Format == output.FormatNDJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(settings)
	}
	if len(settings) == 0 {
		fmt.Fprintln(w, "No settings match.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SETTING\tFORMAT\tSCOPE\tADMX\tDESCRIPTION")
	for _, s := range settings {
		fmt.Fprintf(tw, "%s/%s\t%s\t%s\t%s\t%s\n",
			s.Area, s.Name, s.Format, s.Scope, boolToYN(s.ADMXBacked), truncateDesc(s.Description, 70))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(w, "\n%d of %d settings\n", len(settings), total)
	return nil
}

func renderCSPShow(w io.Writer, s windows_mdm.Setting) error {
	opts := output.CurrentOptions()
	if opts.Format == output.FormatJSON || opts.Format == output.FormatNDJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	}
	fmt.Fprintf(w, "%s/%s\n\n", s.Area, s.Name)
	fmt.Fprintf(w, "  OMA-URI:     %s\n", s.URI)
	fmt.Fprintf(w, "  Format:      %s\n", s.Format)
	fmt.Fprintf(w, "  Scope:       %s\n", s.Scope)
	if s.DefaultValue != "" {
		fmt.Fprintf(w, "  Default:     %s\n", s.DefaultValue)
	}
	if s.MinOSBuild != "" {
		fmt.Fprintf(w, "  Min OS:      %s\n", s.MinOSBuild)
	}
	if s.ADMXBacked {
		fmt.Fprintf(w, "  ADMX-backed: yes (value must be ADMX-style XML, not a plain scalar)\n")
	}
	if s.Deprecated {
		fmt.Fprintf(w, "  Deprecated:  yes\n")
	}
	if s.Description != "" {
		fmt.Fprintf(w, "\n  %s\n", s.Description)
	}
	if av := s.AllowedValues; av != nil {
		switch {
		case len(av.Enum) > 0:
			fmt.Fprintf(w, "\n  Allowed values (%s):\n", av.Type)
			for _, e := range av.Enum {
				fmt.Fprintf(w, "    %s\t%s\n", e.Value, e.Description)
			}
		case av.Value != "":
			fmt.Fprintf(w, "\n  Allowed values (%s): %s\n", av.Type, av.Value)
		}
	}
	fmt.Fprintf(w, "\nNext: jc windows-mdm csp template %s/%s\n", s.Area, s.Name)
	return nil
}

func truncateDesc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func lastPathSegment(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}
