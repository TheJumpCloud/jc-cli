package cmd

// The ADE/ABM/ADUE writes: ADE enrollment settings, the ADUE service-discovery
// URL, and the per-token ABM re-sync. The wire contract, and the reasons for
// every refusal below, live in internal/apple_mdm/ade_writes.go.
//
// `ade-settings update` READS THE SETTING FIRST, for the same reason the
// device actions read the device: the plan can then say what each field goes
// from, not only what it goes to. It READS IT AGAIN in effect afterwards —
// the PATCH answers with the updated setting, and that answer is checked
// against what was asked, because this API has twice been seen to drop input
// with a 200.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

func newADESettingUpdateCmd() *cobra.Command {
	var (
		zeroTouch                      bool
		title, paragraph, button       string
		reset                          []string
		setupOptions, defaultGroups    []string
		clearSetupOptions, clearGroups bool
	)

	cmd := &cobra.Command{
		Use:   "update <token-id> <os-family>",
		Short: "Change the ADE enrollment settings for one OS family",
		Long: `Change the Automated Device Enrollment experience for one OS family on one
ADE token. Only the settings you name are changed.

That last sentence is jc's doing, not the API's. The API takes a "patch mask"
listing the fields to write, and a field in the mask with no value is RESET —
zero-touch to off, lists to empty, welcome text to JumpCloud's stock text. jc
builds the mask from the flags you pass and nothing else.

Two more things the API does that this command guards against:
  - An empty welcome string resets it to the stock text; it cannot be made
    blank. jc refuses "" and offers --reset-welcome instead.
  - A default-group id that belongs to nothing is accepted and stored. jc
    looks every group up before writing.

Setup options REPLACE the current list. Accepted values:
` + wrapWords(apple_mdm.SetupOptions, 76, "  ") + `
(The spec also lists UNKNOWN, which the API answers with HTTP 500.)`,
		Example: `  jc apple-mdm ade-settings update 6a7e1bb88cf0be0001a4e3cd macos --zero-touch=true
  jc apple-mdm ade-settings update 6a7e1bb88cf0be0001a4e3cd ios --welcome-title "Welcome to Acme"
  jc apple-mdm ade-settings update 6a7e1bb88cf0be0001a4e3cd macos --default-group "All Macs" --plan
  jc apple-mdm ade-settings update 6a7e1bb88cf0be0001a4e3cd ios --reset-welcome title,button`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			change := apple_mdm.ADESettingsChange{}
			f := cmd.Flags()
			if f.Changed("zero-touch") {
				change.ZeroTouch = &zeroTouch
			}
			if f.Changed("welcome-title") {
				change.WelcomeTitle = &title
			}
			if f.Changed("welcome-paragraph") {
				change.WelcomeText = &paragraph
			}
			if f.Changed("welcome-button") {
				change.WelcomeButton = &button
			}
			for _, r := range reset {
				switch strings.ToLower(strings.TrimSpace(r)) {
				case "title":
					change.ResetTitle = true
				case "paragraph":
					change.ResetText = true
				case "button":
					change.ResetButton = true
				default:
					return fmt.Errorf("--reset-welcome takes title, paragraph and/or button, not %q", r)
				}
			}
			if f.Changed("setup-options") && clearSetupOptions {
				return fmt.Errorf("--setup-options and --clear-setup-options contradict each other")
			}
			if f.Changed("setup-options") {
				change.SetupOptions = &setupOptions
			} else if clearSetupOptions {
				change.SetupOptions = &[]string{}
			}
			if f.Changed("default-group") && clearGroups {
				return fmt.Errorf("--default-group and --clear-default-groups contradict each other")
			}
			return runADESettingUpdate(cmd, args[0], args[1], change, f.Changed("default-group"), defaultGroups, clearGroups)
		},
	}

	f := cmd.Flags()
	f.BoolVar(&zeroTouch, "zero-touch", false, "Make enrollment mandatory (true) or skippable (false)")
	f.StringVar(&title, "welcome-title", "", "Welcome screen title (max 255)")
	f.StringVar(&paragraph, "welcome-paragraph", "", "Welcome screen text (max 255)")
	f.StringVar(&button, "welcome-button", "", "Welcome screen button label (max 255)")
	f.StringSliceVar(&reset, "reset-welcome", nil, "Reset welcome text to JumpCloud's stock text: title, paragraph, button")
	f.StringSliceVar(&setupOptions, "setup-options", nil, "Setup-assistant options; REPLACES the current list")
	f.BoolVar(&clearSetupOptions, "clear-setup-options", false, "Empty the setup-assistant options")
	f.StringSliceVar(&defaultGroups, "default-group", nil, "Device group (name or id) enrolled devices join; REPLACES the current list")
	f.BoolVar(&clearGroups, "clear-default-groups", false, "Remove every default device group")
	_ = cmd.RegisterFlagCompletionFunc("default-group", completeResourceNames(resolve.DeviceGroupConfig))
	return cmd
}

func runADESettingUpdate(cmd *cobra.Command, tokenID, osArg string, change apple_mdm.ADESettingsChange,
	groupsChanged bool, groups []string, clearGroups bool) error {

	if err := apple_mdm.RequireObjectID("an ADE token", tokenID); err != nil {
		return err
	}
	osFamily, err := apple_mdm.NormalizeOSFamily(osArg)
	if err != nil {
		return err
	}
	// Validate everything that can be validated offline before any request,
	// with the groups standing in as well-formed placeholders: a typo in a
	// setup option should not cost a group lookup first.
	probe := change
	if groupsChanged || clearGroups {
		probe.DefaultGroupIDs = &[]string{}
	}
	if _, err := probe.Body(); err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}
	ctx := cmd.Context()

	if groupsChanged {
		ids := make([]string, 0, len(groups))
		for _, g := range groups {
			id, err := resolveDeviceGroup(ctx, client, g)
			if err != nil {
				return err
			}
			if err := requireDeviceGroupExists(ctx, client, g, id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		change.DefaultGroupIDs = &ids
	} else if clearGroups {
		change.DefaultGroupIDs = &[]string{}
	}

	body, err := change.Body()
	if err != nil {
		return err
	}

	endpoint := apple_mdm.ADESettingEndpoint(tokenID, osFamily)
	raw, err := client.Get(ctx, endpoint)
	if err != nil {
		return err
	}
	setting, err := apple_mdm.ParseADESetting(raw)
	if err != nil {
		return err
	}
	before, err := apple_mdm.ParseADESettingValues(setting)
	if err != nil {
		return err
	}

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:     "update",
			Resource:   "ADE settings",
			Target:     osFamily + " on token " + tokenID,
			Effects:    change.Effects(before),
			Reversible: true,
		})
	}

	resp, err := client.Patch(ctx, endpoint, body)
	if err != nil {
		return err
	}
	updated, err := apple_mdm.ParseADESetting(resp)
	if err != nil {
		return err
	}
	after, err := apple_mdm.ParseADESettingValues(updated)
	if err != nil {
		return err
	}
	if miss := change.Unapplied(after); len(miss) > 0 {
		return fmt.Errorf("the API answered success, but these did not change: %s. "+
			"The setting it returned is below; nothing was retried",
			strings.Join(miss, ", "))
	}
	return output.WriteSingle(cmd.OutOrStdout(), updated, output.CurrentOptions())
}

// requireDeviceGroupExists closes the hole apple_mdm.UnknownGroupIDsAreStored
// describes. The resolver passes an id through untouched, so an id that
// belongs to nothing would otherwise reach the API — which stores it.
func requireDeviceGroupExists(ctx context.Context, client *api.V2Client, given, id string) error {
	if _, err := client.Get(ctx, apple_mdm.DeviceGroupEndpoint(id)); err != nil {
		if apiErr, ok := asAPIError(err); ok && apiErr.StatusCode == http.StatusNotFound {
			return fmt.Errorf("device group %q does not exist; the API would store it anyway, "+
				"and devices would enroll into nothing", given)
		}
		return err
	}
	return nil
}

func newADETokenRefreshABMCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh-abm-devices <token-id>",
		Short: "Re-sync the Apple Business Manager devices behind one ADE token",
		Long: `Ask JumpCloud to re-sync the ABM device assignments for one ADE token, using
that token's sync cursor. Only devices behind this token are affected, and
nothing changes on any device.

Not verified against a real token: the API answered 404 for one that does not
exist, which is all that has been observed. The spec documents a 412
"precondition failed", presumably for a token that cannot sync — an expired one,
for instance.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tokenID := args[0]
			if err := apple_mdm.RequireObjectID("an ADE token", tokenID); err != nil {
				return err
			}
			if viper.GetBool("plan") {
				return renderPlan(cmd, &plan.Plan{
					Action:     "refresh-abm-devices",
					Resource:   "ADE token",
					Target:     tokenID,
					Effects:    []string{"Re-syncs this token's ABM devices"},
					Reversible: true,
				})
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			resp, err := client.Create(cmd.Context(), apple_mdm.RefreshABMDevicesEndpoint(tokenID),
				apple_mdm.EmptyActionBody())
			if err != nil {
				if apiErr, ok := asAPIError(err); ok && apiErr.StatusCode == http.StatusPreconditionFailed {
					return fmt.Errorf("the token cannot sync right now (HTTP 412) — check its expiry "+
						"with `jc apple-mdm ade-tokens get %s`: %w", tokenID, err)
				}
				return err
			}
			opts := output.CurrentOptions()
			if len(strings.TrimSpace(string(resp))) > 0 && json.Valid(resp) && string(resp) != "{}" {
				return output.WriteSingle(cmd.OutOrStdout(), resp, opts)
			}
			if !opts.Quiet {
				fmt.Fprintf(cmd.OutOrStdout(), "ABM re-sync requested for ADE token %s.\n", tokenID)
			}
			return nil
		},
	}
}

func newADUEEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <apple-mdm>",
		Short: "Turn on the ADUE service-discovery URL for an Apple MDM",
		Long: `Turn on the service-discovery URL that Account-Driven User Enrollment uses to
find this Apple MDM. JumpCloud chooses the URL; there is nothing to supply, and
the URL it chose is printed.

Running it again is harmless: the API answers with the same URL.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			id, err := resolveAppleMDM(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			if viper.GetBool("plan") {
				return renderPlan(cmd, &plan.Plan{
					Action:     "enable service discovery",
					Resource:   "apple-mdm",
					Target:     id,
					Effects:    []string{"Publishes a JumpCloud-chosen discovery URL", "Validate then answers SUCCESS"},
					Reversible: true,
				})
			}
			resp, err := client.Create(cmd.Context(), apple_mdm.ServiceDiscoveryURLEndpoint(id), apple_mdm.EmptyActionBody())
			if err != nil {
				return err
			}
			url, err := apple_mdm.ParseServiceDiscoveryURL(resp)
			if err != nil {
				return err
			}
			b, err := json.Marshal(map[string]string{"serviceDiscoveryUrl": url})
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), b, output.CurrentOptions())
		},
	}
}

func newADUEDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <apple-mdm>",
		Short: "Turn off the ADUE service-discovery URL for an Apple MDM",
		Long: `Remove the service-discovery URL for an Apple MDM. Users can no longer start an
Account-Driven User Enrollment by signing in until it is enabled again; devices
already enrolled are not affected as far as this API shows.

Running it again is harmless: the API answers the same way.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			id, err := resolveAppleMDM(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			if viper.GetBool("plan") {
				return renderPlan(cmd, &plan.Plan{
					Action:     "disable service discovery",
					Resource:   "apple-mdm",
					Target:     id,
					Effects:    []string{"Removes the ADUE discovery URL", "New sign-in enrollments stop"},
					Reversible: true,
				})
			}
			ok, err := askToProceed(cmd, fmt.Sprintf(
				"Turn off ADUE service discovery for Apple MDM %s? New sign-in enrollments will stop.", id))
			if err != nil || !ok {
				return err
			}
			if _, err := client.Delete(cmd.Context(), apple_mdm.ServiceDiscoveryURLEndpoint(id)); err != nil {
				return err
			}
			if !output.CurrentOptions().Quiet {
				fmt.Fprintf(cmd.OutOrStdout(), "ADUE service discovery turned off for Apple MDM %s.\n", id)
			}
			return nil
		},
	}
}

// askToProceed is the confirmation gate: no TTY cancels, --force skips, and
// anything but y/yes cancels. It reports false with a nil error for a
// cancellation, which is not a failure.
func askToProceed(cmd *cobra.Command, question string) (bool, error) {
	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return false, nil
	}
	if !shouldConfirm() {
		return true, nil
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s [y/N] ", question)
	answer, err := getConfirmReader().ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("reading confirmation: %w", err)
	}
	if a := strings.TrimSpace(strings.ToLower(answer)); a != "y" && a != "yes" {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
		return false, nil
	}
	return true, nil
}

// wrapWords joins items with ", " into lines no wider than width, each line
// prefixed with indent — so a 44-value enum reads as a paragraph in --help
// rather than one 600-column line.
func wrapWords(items []string, width int, indent string) string {
	var lines []string
	line := indent
	for i, it := range items {
		piece := it
		if i < len(items)-1 {
			piece += ","
		}
		if len(line) > len(indent) && len(line)+1+len(piece) > width {
			lines = append(lines, line)
			line = indent
		}
		if len(line) > len(indent) {
			line += " "
		}
		line += piece
	}
	return strings.Join(append(lines, line), "\n")
}
