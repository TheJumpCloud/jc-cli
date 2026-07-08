package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

func newWindowsMDMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "windows-mdm",
		Short: "Create Windows custom MDM policies (OMA-URI and registry)",
		Long: `Create JumpCloud custom policies for Windows devices from raw
OMA-URI settings (Policy CSP) or registry keys — the Windows analog of
` + "`jc apple-mdm payloads create-policy`" + `.

Unlike the Apple side there is no schema catalog (yet — that's the
KLA-460 follow-up): both JumpCloud templates accept arbitrary entries,
so you supply the OMA-URI path / registry key directly, exactly like
Intune's "Custom" OMA-URI profile. For guidance on building OMA-URI
paths, see Microsoft's Policy CSP reference:
https://learn.microsoft.com/en-us/windows/client-management/mdm/policy-configuration-service-provider

Both policy shapes are device-scoped (JumpCloud does not expose
user-scoped variants of these templates today).`,
	}
	cmd.AddCommand(newWindowsMDMOMAURICmd())
	cmd.AddCommand(newWindowsMDMRegistryCmd())
	return cmd
}

func newWindowsMDMOMAURICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "oma-uri",
		Short: "Custom MDM (OMA-URI) policies from Policy CSP settings",
	}
	cmd.AddCommand(newWindowsMDMOMAURICreatePolicyCmd())
	return cmd
}

func newWindowsMDMRegistryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Custom Registry Keys policies (HKLM)",
	}
	cmd.AddCommand(newWindowsMDMRegistryCreatePolicyCmd())
	return cmd
}

func newWindowsMDMOMAURICreatePolicyCmd() *cobra.Command {
	var (
		policyName   string
		settingFlags []string
		settingsFile string
	)

	cmd := &cobra.Command{
		Use:   "create-policy",
		Short: "Create a JumpCloud Custom MDM (OMA-URI) policy",
		Long: `Create a JumpCloud "Custom MDM (OMA-URI)" policy from one or more
OMA-URI settings. Each setting is a triple: the OMA-URI path, the wire
data type, and the value.

Formats: ` + strings.Join(windows_mdm.OMAURIFormats(), ", ") + ` (the Admin Portal
display names integer/string/boolean/base64 are accepted as aliases).

The policy template is resolved dynamically by name
(` + "`" + windows_mdm.TemplateNameOMAURI + "`" + `) so this works on any tenant
without hardcoded IDs. ` + "`--plan`" + ` previews the resolved template and
the full settings list without making the POST.`,
		Example: `  jc windows-mdm oma-uri create-policy --name "Require BitLocker" \
      --setting 'uri=./Device/Vendor/MSFT/Policy/Config/BitLocker/RequireDeviceEncryption,format=int,value=1'

  # Multiple settings in one policy
  jc windows-mdm oma-uri create-policy --name "Camera + Bluetooth lockdown" \
      --setting 'uri=./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera,format=int,value=0' \
      --setting 'uri=./Device/Vendor/MSFT/Policy/Config/Bluetooth/AllowDiscoverableMode,format=int,value=0'

  # From a JSON file (array of {uri, format, value}), preview first
  jc windows-mdm oma-uri create-policy --name "Baseline" \
      --settings-file baseline.json --plan`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if policyName == "" {
				return fmt.Errorf("--name is required for the new JumpCloud policy")
			}
			settings, err := collectOMAURISettings(settingFlags, settingsFile)
			if err != nil {
				return err
			}
			normalized, err := windows_mdm.NormalizeAndValidateSettings(settings)
			if err != nil {
				return err
			}

			// Resolve the template up front (not gated on --plan) so a
			// tenant without Windows MDM fails fast — same rationale as
			// the apple-mdm create-policy path (Bugbot PR #51 review).
			client, err := newV2Client()
			if err != nil {
				return fmt.Errorf("building v2 client: %w", err)
			}
			ctx := cmd.Context()
			tmpl, err := windows_mdm.ResolveOMAURITemplate(ctx, client)
			if err != nil {
				return fmt.Errorf("resolving Custom MDM (OMA-URI) template: %w", err)
			}

			if viper.GetBool("plan") {
				effects := make([]string, 0, len(normalized)+2)
				effects = append(effects,
					"JumpCloud template: "+tmpl.Name+" ("+tmpl.ID+")",
					fmt.Sprintf("OMA-URI settings (%d):", len(normalized)))
				for _, s := range normalized {
					effects = append(effects, fmt.Sprintf("  %s = %s (%s)", s.URI, s.Value, s.Format))
				}
				p := &plan.Plan{
					Action:     "create",
					Resource:   "JumpCloud Custom MDM (OMA-URI) policy",
					Target:     policyName,
					Effects:    effects,
					Reversible: true,
				}
				return renderPlan(cmd, p)
			}

			body := windows_mdm.BuildOMAURIPolicyBody(policyName, tmpl, normalized)
			result, err := client.Create(ctx, "/policies", body)
			if err != nil {
				return fmt.Errorf("creating policy: %w", err)
			}
			opts := output.CurrentOptions()
			return output.WriteSingle(cmd.OutOrStdout(), result, opts)
		},
	}

	cmd.Flags().StringVar(&policyName, "name", "", "JumpCloud policy name (required)")
	cmd.Flags().StringArrayVar(&settingFlags, "setting", nil,
		"One OMA-URI setting as 'uri=...,format=...,value=...' (repeatable). Commas inside the value are preserved.")
	cmd.Flags().StringVar(&settingsFile, "settings-file", "",
		"Path to a JSON file with an array of {uri, format, value} objects. Combines with --setting.")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func newWindowsMDMRegistryCreatePolicyCmd() *cobra.Command {
	var (
		policyName string
		keyFlags   []string
		keysFile   string
	)

	cmd := &cobra.Command{
		Use:   "create-policy",
		Short: "Create a JumpCloud Custom Registry Keys policy",
		Long: `Create a JumpCloud "Advanced: Custom Registry Keys" policy from one
or more registry rows. Every row lands under HKEY_LOCAL_MACHINE — the
hive is implied and must not be included in the location. JumpCloud
recommends locations under SOFTWARE\Policies.

Types: ` + strings.Join(windows_mdm.RegistryRegTypes(), ", ") + ` (the classic
REG_*/EXPAND_SZ/MULTI_SZ/SZ spellings are accepted as aliases).

The policy template is resolved dynamically by name
(` + "`" + windows_mdm.TemplateNameRegistry + "`" + `). ` + "`--plan`" + ` previews without POSTing.`,
		Example: `  jc windows-mdm registry create-policy --name "Disable Autorun" \
      --key 'location=SOFTWARE\Policies\Microsoft\Windows\Explorer,name=NoAutorun,type=DWORD,data=1'

  # From a JSON file (array of {location, name, type, data})
  jc windows-mdm registry create-policy --name "Chrome baseline" \
      --keys-file chrome-baseline.json --plan`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if policyName == "" {
				return fmt.Errorf("--name is required for the new JumpCloud policy")
			}
			keys, err := collectRegistryKeys(keyFlags, keysFile)
			if err != nil {
				return err
			}
			normalized, err := windows_mdm.NormalizeAndValidateKeys(keys)
			if err != nil {
				return err
			}

			client, err := newV2Client()
			if err != nil {
				return fmt.Errorf("building v2 client: %w", err)
			}
			ctx := cmd.Context()
			tmpl, err := windows_mdm.ResolveRegistryTemplate(ctx, client)
			if err != nil {
				return fmt.Errorf("resolving Custom Registry Keys template: %w", err)
			}

			if viper.GetBool("plan") {
				effects := make([]string, 0, len(normalized)+2)
				effects = append(effects,
					"JumpCloud template: "+tmpl.Name+" ("+tmpl.ID+")",
					fmt.Sprintf("Registry keys (%d, all under HKLM):", len(normalized)))
				for _, k := range normalized {
					effects = append(effects, fmt.Sprintf("  %s\\%s = %s (%s)", k.Location, k.ValueName, k.Data, k.RegType))
				}
				p := &plan.Plan{
					Action:     "create",
					Resource:   "JumpCloud Custom Registry Keys policy",
					Target:     policyName,
					Effects:    effects,
					Reversible: true,
				}
				return renderPlan(cmd, p)
			}

			body := windows_mdm.BuildRegistryPolicyBody(policyName, tmpl, normalized)
			result, err := client.Create(ctx, "/policies", body)
			if err != nil {
				return fmt.Errorf("creating policy: %w", err)
			}
			opts := output.CurrentOptions()
			return output.WriteSingle(cmd.OutOrStdout(), result, opts)
		},
	}

	cmd.Flags().StringVar(&policyName, "name", "", "JumpCloud policy name (required)")
	cmd.Flags().StringArrayVar(&keyFlags, "key", nil,
		"One registry row as 'location=...,name=...,type=...,data=...' (repeatable). Commas inside the data are preserved.")
	cmd.Flags().StringVar(&keysFile, "keys-file", "",
		"Path to a JSON file with an array of {location, name, type, data} objects. Combines with --key.")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

// collectOMAURISettings merges --settings-file entries (first) with
// --setting flags (appended, in order).
func collectOMAURISettings(flags []string, file string) ([]windows_mdm.OMAURISetting, error) {
	var settings []windows_mdm.OMAURISetting
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("reading --settings-file: %w", err)
		}
		var fromFile []struct {
			URI    string `json:"uri"`
			Format string `json:"format"`
			Value  string `json:"value"`
		}
		if err := json.Unmarshal(b, &fromFile); err != nil {
			return nil, fmt.Errorf("parsing --settings-file (want a JSON array of {uri, format, value}): %w", err)
		}
		for _, s := range fromFile {
			settings = append(settings, windows_mdm.OMAURISetting{URI: s.URI, Format: s.Format, Value: s.Value})
		}
	}
	for _, raw := range flags {
		kv, err := parseKVSegments(raw, []string{"uri", "format", "value"})
		if err != nil {
			return nil, fmt.Errorf("--setting %q: %w", raw, err)
		}
		settings = append(settings, windows_mdm.OMAURISetting{
			URI: kv["uri"], Format: kv["format"], Value: kv["value"],
		})
	}
	if len(settings) == 0 {
		return nil, fmt.Errorf("no settings supplied; use --setting (repeatable) or --settings-file")
	}
	return settings, nil
}

// collectRegistryKeys merges --keys-file entries with --key flags.
// The file uses the friendly field names (location/name/type/data),
// matching the flag syntax — NOT the wire column names, which are a
// JumpCloud implementation detail the operator shouldn't need to know.
func collectRegistryKeys(flags []string, file string) ([]windows_mdm.RegistryKey, error) {
	var keys []windows_mdm.RegistryKey
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("reading --keys-file: %w", err)
		}
		var fromFile []struct {
			Location string `json:"location"`
			Name     string `json:"name"`
			Type     string `json:"type"`
			Data     string `json:"data"`
		}
		if err := json.Unmarshal(b, &fromFile); err != nil {
			return nil, fmt.Errorf("parsing --keys-file (want a JSON array of {location, name, type, data}): %w", err)
		}
		for _, k := range fromFile {
			keys = append(keys, windows_mdm.RegistryKey{
				Location: k.Location, ValueName: k.Name, RegType: k.Type, Data: k.Data,
			})
		}
	}
	for _, raw := range flags {
		kv, err := parseKVSegments(raw, []string{"location", "name", "type", "data"})
		if err != nil {
			return nil, fmt.Errorf("--key %q: %w", raw, err)
		}
		keys = append(keys, windows_mdm.RegistryKey{
			Location: kv["location"], ValueName: kv["name"], RegType: kv["type"], Data: kv["data"],
		})
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no keys supplied; use --key (repeatable) or --keys-file")
	}
	return keys, nil
}

// parseKVSegments parses a comma-separated k=v flag value where only
// the given keys start a new field — a comma-containing value (an xml
// blob, a multiString data list) keeps its commas because segments
// that don't begin with a known "key=" are re-attached to the previous
// field. This is why the flags use StringArrayVar rather than
// StringSliceVar: pflag's slice variant would eat the commas before we
// ever saw them.
func parseKVSegments(raw string, keys []string) (map[string]string, error) {
	out := map[string]string{}
	segments := strings.Split(raw, ",")
	current := ""
	for _, seg := range segments {
		matched := ""
		for _, k := range keys {
			if strings.HasPrefix(seg, k+"=") {
				matched = k
				break
			}
		}
		switch {
		case matched != "":
			if _, dup := out[matched]; dup {
				return nil, fmt.Errorf("duplicate field %q", matched)
			}
			out[matched] = seg[len(matched)+1:]
			current = matched
		case current != "":
			// Continuation of the previous field's value — restore the
			// comma the split consumed.
			out[current] += "," + seg
		default:
			return nil, fmt.Errorf("segment %q does not start with one of %s=", seg, strings.Join(keys, "=, "))
		}
	}
	var missing []string
	for _, k := range keys {
		if _, ok := out[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing field(s): %s", strings.Join(missing, ", "))
	}
	return out, nil
}
