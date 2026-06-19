package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
)

// applePlatforms is the curated list of Apple platform names the CLI
// surfaces (must match the keys Apple uses in `supportedOS:`).
// Kept canonical so `--os` validation and list-table headers stay in
// sync — adding a future platform (e.g. xrOS-2) means adding it here.
var applePlatforms = []string{"iOS", "macOS", "tvOS", "visionOS", "watchOS"}

func newAppleMDMPayloadsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "payloads",
		Short: "Browse Apple MDM Configuration Profile payload schemas",
		Long: `Browse the vendored catalog of Apple Configuration Profile schemas
(from github.com/apple/device-management). Use these schemas to discover
which keys an Apple payload supports before turning it into a JumpCloud
Custom MDM Configuration Profile policy.

This subcommand tree is fully offline — the schemas are embedded in the
binary at build time. Refresh on Apple's release cadence via the bumped
vendor directory (` + "see internal/apple_mdm/schemas/<tag>/NOTICE.md" + `).

Roadmap: ` + "`payloads template`" + ` will emit a starter mobileconfig from
a chosen schema (PR2), and ` + "`payloads create-policy`" + ` will round-trip
it directly to JumpCloud (PR3).`,
	}
	cmd.AddCommand(newAppleMDMPayloadsListCmd())
	cmd.AddCommand(newAppleMDMPayloadsShowCmd())
	cmd.AddCommand(newAppleMDMPayloadsTemplateCmd())
	cmd.AddCommand(newAppleMDMPayloadsCreatePolicyCmd())
	return cmd
}

func newAppleMDMPayloadsCreatePolicyCmd() *cobra.Command {
	var (
		valuePairs   []string
		valuesFile   string
		policyName   string
		osFamily     string
		identifier   string
		organization string
		removeLock   bool
		redispatch   bool
	)

	cmd := &cobra.Command{
		Use:   "create-policy <payloadtype-or-id>",
		Short: "Create a JumpCloud Custom MDM Configuration Profile policy from an Apple schema",
		Long: `Build a .mobileconfig from the chosen Apple schema (same shape as
` + "`payloads template`" + `) and POST it as a JumpCloud Custom MDM
Configuration Profile policy. The Custom MDM policy template is
resolved dynamically by name (` + "`custom_mdm_profile_<osfamily>`" + `) so
this works on any tenant without hardcoded IDs.

Workflow:

  1. Resolve the JumpCloud template + its configField IDs from
     /policytemplates (one filtered list call + one detail GET).
  2. Build the .mobileconfig in-memory using the same emitter as
     ` + "`payloads template`" + ` (values are coerced + validated against
     Apple's schema).
  3. Base64-encode the plist and assemble the wire body matching the
     exact shape JumpCloud's Admin Portal produces.
  4. POST /policies. Returns the new policy ID and name.

` + "`--plan`" + ` mode previews every step (resolve, build, base64, body
shape) without making the POST.`,
		Example: `  jc apple-mdm payloads create-policy com.apple.wifi.managed \
      --name "Corp WiFi (MDM)" \
      --values SSID_STR=CorpWiFi --values AutoJoin=true --values EncryptionType=WPA2

  jc apple-mdm payloads create-policy com.apple.security.firewall \
      --name "Firewall — enforce" --values-file firewall.json --plan`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if policyName == "" {
				return fmt.Errorf("--name is required for the new JumpCloud policy")
			}
			// Translate the Apple platform name (what the rest of the
			// catalog uses — "macOS"/"iOS"/etc.) into JumpCloud's
			// template family name ("darwin"/"iphone"). Pre-fix the
			// CLI accepted only the JumpCloud-side name verbatim,
			// which conflicted with `payloads list/show --os macOS`
			// and forced the operator to pass the cryptic --os darwin
			// (Bugbot PR #51 review).
			resolvedFamily, err := jcOSFamily(osFamily)
			if err != nil {
				return err
			}

			cat, err := apple_mdm.Default()
			if err != nil {
				return fmt.Errorf("loading catalog: %w", err)
			}
			payload, err := resolvePayload(cat, args[0])
			if err != nil {
				return err
			}

			fileValues := map[string]any{}
			if valuesFile != "" {
				b, err := os.ReadFile(valuesFile)
				if err != nil {
					return fmt.Errorf("reading --values-file: %w", err)
				}
				if err := json.Unmarshal(b, &fileValues); err != nil {
					return fmt.Errorf("parsing --values-file: %w", err)
				}
			}
			pairValues, err := apple_mdm.ParseValuePairs(valuePairs)
			if err != nil {
				return err
			}
			merged := apple_mdm.MergeValues(fileValues, pairValues)
			typed, err := apple_mdm.CoerceAndValidate(payload, merged)
			if err != nil {
				return err
			}

			// Emit the mobileconfig to a buffer (not to disk — we
			// only need the bytes for base64 + POST).
			var plistBuf bytes.Buffer
			err = apple_mdm.EmitMobileconfig(&plistBuf,
				apple_mdm.EnvelopeOpts{
					DisplayName:       policyName,
					Identifier:        identifier,
					Organization:      organization,
					RemovalDisallowed: removeLock,
				},
				[]apple_mdm.PayloadInstance{{
					Schema:      payload,
					Values:      typed,
					DisplayName: policyName,
				}},
			)
			if err != nil {
				return fmt.Errorf("emitting mobileconfig: %w", err)
			}

			// Resolve the JumpCloud template up front (not gated on
			// --plan) so a misconfigured tenant fails fast — pre-fix
			// the resolution only ran in the live POST path, so
			// --plan would happily print a "looks good" report while
			// the actual create-policy invocation would fail at the
			// template lookup (Bugbot PR #51 review). The two calls
			// are cheap and read-only.
			client, err := newV2Client()
			if err != nil {
				return fmt.Errorf("building v2 client: %w", err)
			}
			ctx := cmd.Context()
			tmpl, err := apple_mdm.ResolveCustomMDMTemplate(ctx, client, resolvedFamily)
			if err != nil {
				return fmt.Errorf("resolving Custom MDM template: %w", err)
			}

			if viper.GetBool("plan") {
				p := &plan.Plan{
					Action:   "create",
					Resource: "JumpCloud Custom MDM Configuration Profile policy",
					Target:   policyName,
					Effects: []string{
						"Apple payloadtype: " + payload.Type,
						"JumpCloud template: " + tmpl.Name + " (" + tmpl.ID + ")",
						"Apple platform: " + osFamily + " (family: " + resolvedFamily + ")",
						"Mobileconfig bytes: " + fmt.Sprintf("%d", plistBuf.Len()),
						"Re-apply on OS update: " + boolToYN(redispatch),
						"Removal disallowed: " + boolToYN(removeLock),
					},
					Reversible: true,
				}
				return renderPlan(cmd, p)
			}

			body := apple_mdm.BuildCustomMDMPolicyBody(policyName, tmpl, plistBuf.Bytes(), redispatch)
			result, err := client.Create(ctx, "/policies", body)
			if err != nil {
				return fmt.Errorf("creating policy: %w", err)
			}

			opts := output.CurrentOptions()
			return output.WriteSingle(cmd.OutOrStdout(), result, opts)
		},
	}

	cmd.Flags().StringSliceVar(&valuePairs, "values", nil,
		"Set a scalar key (key=value, repeatable). Use --values-file for nested structures.")
	cmd.Flags().StringVar(&valuesFile, "values-file", "",
		"Path to a JSON file mapping Apple payload key names to values (supports nested dicts/arrays)")
	cmd.Flags().StringVar(&policyName, "name", "",
		"JumpCloud policy name AND profile display name (required)")
	cmd.Flags().StringVar(&osFamily, "os", "macOS",
		"Apple platform: macOS. iOS planned for KLA-450; tvOS/visionOS/watchOS are not supported by JumpCloud MDM.")
	cmd.Flags().StringVar(&identifier, "identifier", "",
		"Profile reverse-DNS identifier (default: auto-generated jc.<uuid>)")
	cmd.Flags().StringVar(&organization, "organization", "",
		"Profile organization name (optional metadata)")
	cmd.Flags().BoolVar(&removeLock, "removal-disallowed", false,
		"Prevent end users from removing the profile via System Settings (requires MDM unenroll)")
	cmd.Flags().BoolVar(&redispatch, "redispatch", true,
		"Re-apply policy on every OS update (matches JumpCloud's Admin Portal default)")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func boolToYN(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// jcOSFamily maps Apple's platform name (macOS/iOS/etc., as used by
// the catalog and the rest of the apple-mdm subcommand tree) to
// JumpCloud's policy-template family name (darwin/iphone/etc.). v1
// supports macOS only; everything else returns a clear error pointing
// at the relevant follow-up ticket. Without this translation
// `--os macOS` is rejected even though the rest of the CLI uses Apple's
// naming exclusively.
func jcOSFamily(applePlatform string) (string, error) {
	switch applePlatform {
	case "macOS", apple_mdm.OSFamilyDarwin:
		return apple_mdm.OSFamilyDarwin, nil
	case "iOS", apple_mdm.OSFamilyIOS:
		return "", fmt.Errorf("--os %q: iOS support is tracked in KLA-450; not validated against a tenant yet", applePlatform)
	case "tvOS", "visionOS", "watchOS":
		return "", fmt.Errorf("--os %q: JumpCloud MDM does not manage this Apple platform", applePlatform)
	default:
		return "", fmt.Errorf("--os %q: unknown Apple platform; supported: macOS", applePlatform)
	}
}

func newAppleMDMPayloadsTemplateCmd() *cobra.Command {
	var (
		valuePairs   []string
		valuesFile   string
		outputFile   string
		displayName  string
		identifier   string
		organization string
		removeLock   bool
	)

	cmd := &cobra.Command{
		Use:   "template <payloadtype-or-id>",
		Short: "Emit a .mobileconfig from one Apple MDM payload schema",
		Long: `Emit a valid Apple Configuration Profile (.mobileconfig XML) containing
a single payload of the named type, with user-supplied values coerced
and validated against Apple's schema.

This is the offline shape: parse Apple's schema → emit plist → write to
file or stdout. Pair with the upcoming ` + "`payloads create-policy`" + ` (PR3)
to round-trip directly into a JumpCloud Custom MDM Configuration
Profile policy.

Value supply:

  --values key=value (repeatable) for scalar keys; coercion infers the
  right type from the schema (boolean→true/false, integer→int, etc.).

  --values-file path.json for the full structured shape, including
  nested dicts and arrays. The file is a flat JSON object keyed by
  Apple payload key names. Useful for any non-trivial profile.

Both can be combined — command-line --values overrides the file on
collision (more specific by convention).

The emitted profile carries auto-generated UUIDs and a 'jc.<uuid>'
identifier by default so consecutive emissions never collide with each
other on a device.`,
		Example: `  # Minimal WiFi profile from CLI scalars
  jc apple-mdm payloads template com.apple.wifi.managed \
      --values SSID_STR=CorpWiFi \
      --values AutoJoin=true \
      --values EncryptionType=WPA2 \
      --values HIDDEN_NETWORK=false \
      --name "Corp WiFi"

  # Complex profile from JSON file
  cat <<EOF > wifi.json
  { "SSID_STR": "CorpWiFi", "EncryptionType": "WPA3", "AutoJoin": true,
    "ProxyType": "Auto", "QoSMarkingPolicy": { "QoSMarkingEnabled": true } }
  EOF
  jc apple-mdm payloads template com.apple.wifi.managed \
      --values-file wifi.json -o corp-wifi.mobileconfig`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cat, err := apple_mdm.Default()
			if err != nil {
				return fmt.Errorf("loading catalog: %w", err)
			}
			payload, err := resolvePayload(cat, args[0])
			if err != nil {
				return err
			}

			fileValues := map[string]any{}
			if valuesFile != "" {
				b, err := os.ReadFile(valuesFile)
				if err != nil {
					return fmt.Errorf("reading --values-file: %w", err)
				}
				if err := json.Unmarshal(b, &fileValues); err != nil {
					return fmt.Errorf("parsing --values-file: %w", err)
				}
			}

			pairValues, err := apple_mdm.ParseValuePairs(valuePairs)
			if err != nil {
				return err
			}
			merged := apple_mdm.MergeValues(fileValues, pairValues)

			typed, err := apple_mdm.CoerceAndValidate(payload, merged)
			if err != nil {
				return err
			}

			// File-output path uses a temp-file + atomic rename so a
			// mid-emit failure can never leave a truncated .mobileconfig
			// on disk for a caller to accidentally upload. Pre-fix
			// (Bugbot PR #50 review):
			//   1. os.Create truncated the target up front, so an
			//      emission error left a half-written file.
			//   2. closer.Close() return value was discarded, so a
			//      delayed flush failure could leave incomplete data
			//      while the CLI reported "Wrote …" success.
			if outputFile != "" {
				return emitMobileconfigToFile(cmd, outputFile, payload, typed,
					displayName, identifier, organization, removeLock)
			}
			return apple_mdm.EmitMobileconfig(cmd.OutOrStdout(),
				apple_mdm.EnvelopeOpts{
					DisplayName:       displayName,
					Identifier:        identifier,
					Organization:      organization,
					RemovalDisallowed: removeLock,
				},
				[]apple_mdm.PayloadInstance{{
					Schema:      payload,
					Values:      typed,
					DisplayName: displayName,
				}},
			)
		},
	}

	cmd.Flags().StringSliceVar(&valuePairs, "values", nil,
		"Set a scalar key (key=value, repeatable). Use --values-file for nested structures.")
	cmd.Flags().StringVar(&valuesFile, "values-file", "",
		"Path to a JSON file mapping Apple payload key names to values (supports nested dicts/arrays)")
	cmd.Flags().StringVar(&outputFile, "output-file", "",
		"Write the .mobileconfig to this path (default: stdout)")
	cmd.Flags().StringVar(&displayName, "name", "",
		"Profile display name (shown in System Settings → Profiles and used as the policy name in PR3)")
	cmd.Flags().StringVar(&identifier, "identifier", "",
		"Profile reverse-DNS identifier (default: auto-generated jc.<uuid>)")
	cmd.Flags().StringVar(&organization, "organization", "",
		"Profile organization name (optional metadata)")
	cmd.Flags().BoolVar(&removeLock, "removal-disallowed", false,
		"Prevent end users from removing the profile via System Settings (requires MDM unenroll)")

	return cmd
}

// emitMobileconfigToFile writes the emitted plist to outputFile via a
// temp-file + atomic rename so partial writes never reach the target
// path. The temp file lives in the same directory as the target so
// the rename stays on one filesystem and is atomic.
//
// On any failure (emit, close, rename) the temp file is removed and
// the original target — if it existed — is left untouched.
func emitMobileconfigToFile(cmd *cobra.Command, outputFile string,
	payload apple_mdm.Payload, typed map[string]any,
	displayName, identifier, organization string, removeLock bool) error {

	dir := filepath.Dir(outputFile)
	tmp, err := os.CreateTemp(dir, ".jc-mobileconfig-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	// On any error path we drop the temp file. Track success at the
	// end so the defer doesn't unlink the final renamed file.
	var succeeded bool
	defer func() {
		if !succeeded {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := apple_mdm.EmitMobileconfig(tmp,
		apple_mdm.EnvelopeOpts{
			DisplayName:       displayName,
			Identifier:        identifier,
			Organization:      organization,
			RemovalDisallowed: removeLock,
		},
		[]apple_mdm.PayloadInstance{{
			Schema:      payload,
			Values:      typed,
			DisplayName: displayName,
		}},
	); err != nil {
		tmp.Close()
		return fmt.Errorf("emitting mobileconfig: %w", err)
	}
	// Check the Close error — buffered writers and some filesystems
	// surface flush failures here, not in earlier Write calls.
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpPath, outputFile); err != nil {
		return fmt.Errorf("finalizing %s: %w", outputFile, err)
	}
	succeeded = true
	fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %s\n", outputFile)
	return nil
}

// resolvePayload is shared by `show` and `template` — resolve by ID
// first, then PayloadType; report all variants on ambiguity. Pulled
// up so both subcommands stay in lockstep on lookup semantics.
func resolvePayload(cat *apple_mdm.Catalog, ref string) (apple_mdm.Payload, error) {
	if p, ok := cat.ByID(ref); ok {
		return p, nil
	}
	variants := cat.VariantsOf(ref)
	switch len(variants) {
	case 0:
		return apple_mdm.Payload{}, fmt.Errorf(
			"no payload with ID or payloadtype %q in catalog (release %s)", ref, cat.Release)
	case 1:
		return variants[0], nil
	default:
		return apple_mdm.Payload{}, fmt.Errorf(
			"payloadtype %q has %d variants — disambiguate with the catalog ID:\n  %s",
			ref, len(variants), formatVariantList(variants))
	}
}

// payloadsListRow is the per-row shape used for both human and JSON
// output of `payloads list`. Trimmed-down view of Payload so list
// output isn't a wall of nested data.
type payloadsListRow struct {
	Type        string   `json:"type"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	SupportedOS []string `json:"supported_os"`
}

func newAppleMDMPayloadsListCmd() *cobra.Command {
	var osFilter, searchFilter string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Apple MDM payload schemas in the catalog",
		Long: `List every Apple Configuration Profile schema in the embedded
catalog. Filter by platform with --os (e.g. ` + "`--os macOS`" + `) and
narrow by name with --search (case-insensitive substring against
payloadtype, title, and description).`,
		Example: `  jc apple-mdm payloads list
  jc apple-mdm payloads list --os macOS
  jc apple-mdm payloads list --os iOS --search wifi
  jc apple-mdm payloads list --output json | jq '.[] | select(.supported_os | index("visionOS"))'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if osFilter != "" && !knownPlatform(osFilter) {
				return fmt.Errorf("unknown --os %q: want one of %s", osFilter, strings.Join(applePlatforms, ", "))
			}
			cat, err := apple_mdm.Default()
			if err != nil {
				return fmt.Errorf("loading catalog: %w", err)
			}
			matches := cat.Filter(apple_mdm.FilterOpts{OS: osFilter, Search: searchFilter})

			rows := make([]payloadsListRow, 0, len(matches))
			for _, p := range matches {
				rows = append(rows, payloadsListRow{
					Type:        p.Type,
					Title:       p.Title,
					Description: p.Description,
					SupportedOS: availablePlatforms(p.SupportedOS),
				})
			}

			return renderPayloadsList(cmd.OutOrStdout(), rows, cat.Len())
		},
	}
	cmd.Flags().StringVar(&osFilter, "os", "", "Restrict to payloads supported on this platform: iOS, macOS, tvOS, visionOS, watchOS")
	cmd.Flags().StringVar(&searchFilter, "search", "", "Case-insensitive substring filter against payloadtype, title, and description")
	return cmd
}

func newAppleMDMPayloadsShowCmd() *cobra.Command {
	var osFilter string

	cmd := &cobra.Command{
		Use:   "show <payloadtype>",
		Short: "Show a single Apple MDM payload schema in detail",
		Long: `Render one Apple Configuration Profile schema with all keys, types,
defaults, value constraints, and platform availability.

Required keys are listed first so the operator can see at a glance what
must be supplied. Optional keys follow with their declared defaults.

` + "`--os`" + ` narrows the per-key support matrix to one platform so the
view isn't dominated by columns for platforms the operator doesn't
care about.`,
		Example: `  jc apple-mdm payloads show com.apple.wifi.managed
  jc apple-mdm payloads show com.apple.wifi.managed --os macOS
  jc apple-mdm payloads show com.apple.applicationaccess --output json | jq '.keys[] | select(.presence=="required")'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if osFilter != "" && !knownPlatform(osFilter) {
				return fmt.Errorf("unknown --os %q: want one of %s", osFilter, strings.Join(applePlatforms, ", "))
			}
			cat, err := apple_mdm.Default()
			if err != nil {
				return fmt.Errorf("loading catalog: %w", err)
			}

			// Resolve in order: catalog ID (exact filename match,
			// disambiguates MCX variants), then PayloadType (canonical
			// identifier). If PayloadType is ambiguous (multiple
			// variants share it), list the variants and surface the
			// ID-based lookup as the disambiguator. The user-facing
			// error is much more useful than silently returning a
			// first-wins variant they didn't ask for.
			id := args[0]
			if payload, ok := cat.ByID(id); ok {
				return renderPayloadShow(cmd.OutOrStdout(), payload, osFilter)
			}
			variants := cat.VariantsOf(id)
			switch len(variants) {
			case 0:
				return fmt.Errorf("no payload with ID or payloadtype %q in catalog (release %s)", id, cat.Release)
			case 1:
				return renderPayloadShow(cmd.OutOrStdout(), variants[0], osFilter)
			default:
				return fmt.Errorf("payloadtype %q has %d variants — disambiguate with the catalog ID:\n  %s",
					id, len(variants), formatVariantList(variants))
			}
		},
	}
	cmd.Flags().StringVar(&osFilter, "os", "", "Render the per-key support matrix only for this platform")
	return cmd
}

// renderPayloadsList writes the list either as JSON (one array of rows)
// or as a human-readable aligned table. NDJSON and CSV fall through to
// JSON for now — the data is structured enough that downstream piping
// works fine either way; tabular formats can be added if a real consumer
// asks for them.
func renderPayloadsList(w io.Writer, rows []payloadsListRow, total int) error {
	opts := output.CurrentOptions()
	if opts.Format == output.FormatJSON || opts.Format == output.FormatNDJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	if len(rows) == 0 {
		fmt.Fprintln(w, "No payloads match.")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TYPE\tTITLE\tPLATFORMS")
	for _, r := range rows {
		title := r.Title
		if title == "" {
			title = "—"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Type, title, strings.Join(r.SupportedOS, ","))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(w, "\n%d of %d payloads.\n", len(rows), total)
	return nil
}

// renderPayloadShow writes one payload in detail. JSON path emits the
// raw Payload struct verbatim — the schema is small enough that even a
// large profile (e.g. com.apple.applicationaccess with ~200 keys) is
// browseable. Human path groups by Presence ("required" first), aligns
// columns, and renders nested subkeys with indentation.
func renderPayloadShow(w io.Writer, p apple_mdm.Payload, osFilter string) error {
	opts := output.CurrentOptions()
	if opts.Format == output.FormatJSON || opts.Format == output.FormatNDJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(p)
	}

	fmt.Fprintf(w, "%s\n", p.Type)
	if p.Title != "" {
		fmt.Fprintf(w, "  %s\n", p.Title)
	}
	if p.Description != "" {
		fmt.Fprintf(w, "\n%s\n", p.Description)
	}

	// Platform support matrix
	fmt.Fprintln(w, "\n== Supported platforms ==")
	platforms := platformOrder(p.SupportedOS, osFilter)
	if len(platforms) == 0 {
		fmt.Fprintln(w, "  (none declared)")
	} else {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  PLATFORM\tINTRODUCED\tMULTIPLE\tDEVICECHANNEL\tUSERCHANNEL\tSUPERVISED\tREQUIRESDEP")
		for _, name := range platforms {
			s := p.SupportedOS[name]
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				name, defaultStr(s.Introduced, "—"),
				ynBool(s.Multiple), ynBool(s.DeviceChannel), ynBool(s.UserChannel),
				ynBool(s.Supervised), ynBool(s.RequiresDEP))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}

	// Keys, grouped by presence: required first, then optional, then anything else.
	required, optional, other := groupKeysByPresence(p.Keys)
	if len(required) > 0 {
		fmt.Fprintln(w, "\n== Required keys ==")
		writeKeyTable(w, required)
	}
	if len(optional) > 0 {
		fmt.Fprintln(w, "\n== Optional keys ==")
		writeKeyTable(w, optional)
	}
	if len(other) > 0 {
		fmt.Fprintln(w, "\n== Other keys ==")
		writeKeyTable(w, other)
	}

	return nil
}

func writeKeyTable(w io.Writer, keys []apple_mdm.Key) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  KEY\tTYPE\tDEFAULT\tCONSTRAINTS\tDESCRIPTION")
	for _, k := range keys {
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n",
			k.Name,
			defaultStr(k.Type, "—"),
			formatDefault(k.Default),
			formatConstraints(k),
			firstLine(k.Content))
	}
	_ = tw.Flush()
}

// groupKeysByPresence buckets keys for the detail render. We rely on
// Apple's "required"/"optional" strings verbatim; anything else lands
// in `other` so future presence values ("conditional", "deprecated")
// surface visibly instead of silently joining optional.
func groupKeysByPresence(keys []apple_mdm.Key) (required, optional, other []apple_mdm.Key) {
	for _, k := range keys {
		switch strings.ToLower(k.Presence) {
		case "required":
			required = append(required, k)
		case "optional", "":
			optional = append(optional, k)
		default:
			other = append(other, k)
		}
	}
	return
}

// platformOrder returns the platform names from p.SupportedOS sorted
// in Apple's display order (matches applePlatforms). When osFilter is
// set, returns just that platform if present.
func platformOrder(s apple_mdm.SupportedOS, osFilter string) []string {
	if osFilter != "" {
		if _, ok := s[osFilter]; ok {
			return []string{osFilter}
		}
		return nil
	}
	var out []string
	for _, plat := range applePlatforms {
		if _, ok := s[plat]; ok {
			out = append(out, plat)
		}
	}
	return out
}

// availablePlatforms returns the platforms where the payload is
// available (Available() == true, i.e. introduced and not n/a). Used
// for the list-row PLATFORMS column so unavailable entries don't
// clutter the table.
func availablePlatforms(s apple_mdm.SupportedOS) []string {
	var out []string
	for _, plat := range applePlatforms {
		sup, ok := s[plat]
		if !ok || !sup.Available() {
			continue
		}
		out = append(out, plat)
	}
	sort.Strings(out)
	return out
}

func knownPlatform(p string) bool {
	for _, plat := range applePlatforms {
		if plat == p {
			return true
		}
	}
	return false
}

func ynBool(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func defaultStr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func formatDefault(v any) string {
	if v == nil {
		return "—"
	}
	switch x := v.(type) {
	case string:
		if x == "" {
			return `""`
		}
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func formatConstraints(k apple_mdm.Key) string {
	var parts []string
	if k.ValueType != "" {
		parts = append(parts, k.ValueType)
	}
	if k.Range != nil {
		parts = append(parts, fmt.Sprintf("range [%v..%v]", k.Range.Min, k.Range.Max))
	}
	if len(k.RangeList) > 0 {
		vals := make([]string, 0, len(k.RangeList))
		for _, v := range k.RangeList {
			vals = append(vals, fmt.Sprintf("%v", v))
		}
		parts = append(parts, "enum{"+strings.Join(vals, ",")+"}")
	}
	if len(k.Subkeys) > 0 {
		parts = append(parts, fmt.Sprintf("nested(%d)", len(k.Subkeys)))
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " · ")
}

// formatVariantList renders the disambiguation hint when multiple
// schemas share a PayloadType (the MCX family). Each line shows the
// catalog ID and Title so the operator can pick. Indented two spaces
// to align with the parent error message.
func formatVariantList(vs []apple_mdm.Payload) string {
	var b strings.Builder
	for i, v := range vs {
		if i > 0 {
			b.WriteString("\n  ")
		}
		title := v.Title
		if title == "" {
			title = "(no title)"
		}
		fmt.Fprintf(&b, "%-40s  %s", v.ID, title)
	}
	return b.String()
}

func firstLine(s string) string {
	if s == "" {
		return "—"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		s = s[:77] + "..."
	}
	return s
}
