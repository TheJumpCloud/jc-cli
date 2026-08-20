package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/devsettings"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
)

// newDevicesSettingsCmd builds the `jc devices settings` subtree, which manages
// the organization-wide device settings (not per-device configuration).
func newDevicesSettingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "settings",
		Aliases: []string{"org-settings"},
		Short:   "Manage organization-wide device settings",
		Long: `Manage the organization-wide device settings of a JumpCloud tenant.

These are org-level defaults, not per-device configuration:

  sign-in        Sign In with JumpCloud, per OS family (Windows, macOS)
  password-sync  Whether new devices sync passwords by default

Both are singletons: there is one value per organization.`,
	}

	cmd.AddCommand(newDevicesSettingsGetCmd())
	cmd.AddCommand(newDevicesSettingsSignInCmd())
	cmd.AddCommand(newDevicesSettingsPasswordSyncCmd())

	return cmd
}

func newDevicesSettingsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show all organization-wide device settings",
		Long: `Show every organization-wide device setting in one object.

Combines 'sign-in get' and 'password-sync get' so the whole org-level device
posture can be read in a single call.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}

			rawSignIn, err := client.Get(cmd.Context(), devsettings.SignInEndpoint)
			if err != nil {
				return err
			}
			signIn, err := devsettings.ParseSignIn(rawSignIn)
			if err != nil {
				return err
			}

			rawSync, err := client.Get(cmd.Context(), devsettings.PasswordSyncEndpoint)
			if err != nil {
				return err
			}
			var sync devsettings.PasswordSync
			if err := json.Unmarshal(rawSync, &sync); err != nil {
				return fmt.Errorf("decoding password sync setting: %w", err)
			}

			combined := map[string]any{
				"organizationObjectId": signIn.OrganizationObjectID,
				"signInWithJumpCloud":  signIn.Settings,
				"defaultPasswordSync":  sync.Enabled,
			}
			return output.WriteSingle(cmd.OutOrStdout(), mustMarshal(combined), output.CurrentOptions())
		},
	}
}

func newDevicesSettingsSignInCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sign-in",
		Aliases: []string{"signin", "sign-in-with-jumpcloud"},
		Short:   "Manage the Sign In with JumpCloud settings",
		Long: `Manage the organization's Sign In with JumpCloud settings.

There is one entry per OS family (Windows, macOS), each carrying whether the
feature is enabled and the default permission granted to users signing in.`,
	}
	cmd.AddCommand(newDevicesSettingsSignInGetCmd())
	cmd.AddCommand(newDevicesSettingsSignInSetCmd())
	return cmd
}

func newDevicesSettingsSignInGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show the Sign In with JumpCloud settings",
		Long: `Show the organization's Sign In with JumpCloud settings, one row per OS family.

Default fields: osFamily, enabled, defaultPermission.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), devsettings.SignInEndpoint)
			if err != nil {
				return err
			}
			settings, err := devsettings.ParseSignIn(raw)
			if err != nil {
				return err
			}

			items := make([]json.RawMessage, 0, len(settings.Settings))
			for _, s := range settings.Settings {
				items = append(items, mustMarshal(s))
			}

			opts := output.CurrentOptions()
			opts.DefaultFields = devsettings.SignInDefaultFields
			if err := output.WriteList(cmd.OutOrStdout(), items, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(items))
			}
			return nil
		},
	}
}

func newDevicesSettingsSignInSetCmd() *cobra.Command {
	var (
		osFlag         string
		enabledFlag    bool
		permissionFlag string
	)

	cmd := &cobra.Command{
		Use:   "set --os <windows|macos>",
		Short: "Change the Sign In with JumpCloud setting for one OS family",
		Long: `Change the organization's Sign In with JumpCloud setting for one OS family.

Specify only what should change; the current settings are fetched first and the
complete array is sent back with the named OS family updated, so the setting
for the other OS family is never disturbed.

  jc devices settings sign-in set --os macos --enabled=false
  jc devices settings sign-in set --os windows --default-permission admin

This changes how users sign in to every device in the organization, so it
prompts for confirmation. Use --plan to preview, or --force to skip the prompt.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevicesSettingsSignInSet(cmd, osFlag, enabledFlag, permissionFlag)
		},
	}

	cmd.Flags().StringVar(&osFlag, "os", "", "OS family to change: windows or macos (required)")
	cmd.Flags().BoolVar(&enabledFlag, "enabled", false, "Whether Sign In with JumpCloud is enabled for this OS family")
	cmd.Flags().StringVar(&permissionFlag, "default-permission", "", "Default permission granted on sign-in: standard or admin")
	_ = cmd.MarkFlagRequired("os")

	return cmd
}

func runDevicesSettingsSignInSet(cmd *cobra.Command, osFlag string, enabledFlag bool, permissionFlag string) error {
	osFamily, err := devsettings.NormalizeOSFamily(osFlag)
	if err != nil {
		return err
	}

	var (
		enabled    *bool
		permission *string
	)
	if cmd.Flags().Changed("enabled") {
		enabled = &enabledFlag
	}
	if cmd.Flags().Changed("default-permission") {
		p, err := devsettings.NormalizePermission(permissionFlag)
		if err != nil {
			return err
		}
		permission = &p
	}
	if enabled == nil && permission == nil {
		return fmt.Errorf("no changes requested. Specify --enabled and/or --default-permission")
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	raw, err := client.Get(cmd.Context(), devsettings.SignInEndpoint)
	if err != nil {
		return err
	}
	current, err := devsettings.ParseSignIn(raw)
	if err != nil {
		return err
	}

	merged := devsettings.MergeSignInSetting(current.Settings, osFamily, enabled, permission)
	after, _ := devsettings.FindSignIn(merged, osFamily)

	var effects []string
	if before, ok := devsettings.FindSignIn(current.Settings, osFamily); ok {
		effects = append(effects, "from  "+before.Describe())
	} else {
		effects = append(effects, "from  "+osFamily+": (no existing entry)")
	}
	effects = append(effects, "to    "+after.Describe())
	effects = append(effects, "Applies to every device in the organization")

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:     "update",
			Resource:   "Sign In with JumpCloud setting",
			Target:     osFamily,
			Effects:    effects,
			Reversible: true,
		})
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Change Sign In with JumpCloud for %s to %q? This affects every device in the organization. [y/N] ",
			osFamily, after.Describe())
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

	if _, err := client.Update(cmd.Context(), devsettings.SignInEndpoint, devsettings.SignInBody(merged)); err != nil {
		return err
	}

	// The PUT returns 204/{} with no body, so re-read to show the new state.
	raw, err = client.Get(cmd.Context(), devsettings.SignInEndpoint)
	if err != nil {
		return err
	}
	updated, err := devsettings.ParseSignIn(raw)
	if err != nil {
		return err
	}
	if entry, ok := devsettings.FindSignIn(updated.Settings, osFamily); ok {
		return output.WriteSingle(cmd.OutOrStdout(), mustMarshal(entry), output.CurrentOptions())
	}
	return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
}

func newDevicesSettingsPasswordSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "password-sync",
		Aliases: []string{"passwordsync", "default-password-sync"},
		Short:   "Manage the default password sync setting",
		Long: `Manage the organization's default password sync setting.

This controls whether devices sync passwords by default. It is a single
boolean for the whole organization.`,
	}
	cmd.AddCommand(newDevicesSettingsPasswordSyncGetCmd())
	cmd.AddCommand(newDevicesSettingsPasswordSyncSetCmd())
	return cmd
}

func newDevicesSettingsPasswordSyncGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show the default password sync setting",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), devsettings.PasswordSyncEndpoint)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
}

func newDevicesSettingsPasswordSyncSetCmd() *cobra.Command {
	var enabledFlag bool

	cmd := &cobra.Command{
		Use:   "set --enabled=<true|false>",
		Short: "Change the default password sync setting",
		Long: `Change the organization's default password sync setting.

  jc devices settings password-sync set --enabled=false

This applies to the whole organization, so it prompts for confirmation.
Use --plan to preview, or --force to skip the prompt.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevicesSettingsPasswordSyncSet(cmd, enabledFlag)
		},
	}

	cmd.Flags().BoolVar(&enabledFlag, "enabled", false, "Whether devices sync passwords by default (required)")
	_ = cmd.MarkFlagRequired("enabled")

	return cmd
}

func runDevicesSettingsPasswordSyncSet(cmd *cobra.Command, enabled bool) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	raw, err := client.Get(cmd.Context(), devsettings.PasswordSyncEndpoint)
	if err != nil {
		return err
	}
	var current devsettings.PasswordSync
	if err := json.Unmarshal(raw, &current); err != nil {
		return fmt.Errorf("decoding password sync setting: %w", err)
	}

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:   "update",
			Resource: "default password sync setting",
			Target:   "organization",
			Effects: []string{
				fmt.Sprintf("enabled: %t → %t", current.Enabled, enabled),
				"Applies to every device in the organization",
			},
			Reversible: true,
		})
	}

	if current.Enabled == enabled {
		fmt.Fprintf(cmd.ErrOrStderr(), "Default password sync is already %t; no change made.\n", enabled)
		return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Set default password sync to %t for the whole organization? [y/N] ", enabled)
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

	if _, err := client.Update(cmd.Context(), devsettings.PasswordSyncEndpoint, devsettings.PasswordSyncBody(enabled)); err != nil {
		return err
	}

	// The PUT returns 204/{} with no body, so re-read to show the new state.
	raw, err = client.Get(cmd.Context(), devsettings.PasswordSyncEndpoint)
	if err != nil {
		return err
	}
	return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
}
