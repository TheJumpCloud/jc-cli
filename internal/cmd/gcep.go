package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/gcep"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
)

// newGCEPCmd builds the `jc gcep` group: the organization's GCEP (Google
// device-trust) configuration and a device's GCEP trust state. The wire
// contract lives in internal/gcep, shared with the MCP read tools.
func newGCEPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gcep",
		Short: "Manage the GCEP Google device-trust integration",
		Long: `Read and manage the organization's GCEP integration, which reports JumpCloud
device trust to Google (Google customer id, Google user email, the OS families
it applies to), and read a device's GCEP trust state.

  jc gcep config get                       Show the configuration
  jc gcep config set --state disabled      Change fields (complete body is sent)
  jc gcep config delete                    Remove the integration (destructive)
  jc gcep device-state <device>            A device's registration and users

There is one configuration per organization.`,
	}
	cmd.AddCommand(newGCEPConfigCmd())
	cmd.AddCommand(newGCEPDeviceStateCmd())
	return cmd
}

func newGCEPConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "config",
		Aliases: []string{"configuration"},
		Short:   "The organization's GCEP configuration",
	}
	cmd.AddCommand(newGCEPConfigGetCmd())
	cmd.AddCommand(newGCEPConfigSetCmd())
	cmd.AddCommand(newGCEPConfigDeleteCmd())
	return cmd
}

// fetchGCEPConfig GETs the configuration. It returns (raw, nil, nil) when the
// organization has none, whether the API says so with an error or with an
// empty configuration.
func fetchGCEPConfig(ctx context.Context, client *api.V2Client) (json.RawMessage, *gcep.Configuration, error) {
	raw, err := client.Get(ctx, gcep.ConfigurationEndpoint)
	if err != nil {
		if gcep.IsNotConfiguredError(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	cfg, err := gcep.ParseConfiguration(raw)
	if err != nil {
		return nil, nil, err
	}
	return raw, cfg, nil
}

func newGCEPConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show the organization's GCEP configuration",
		Long: `Show the organization's GCEP configuration as the API returns it: customerId,
googleUserEmail, state, connectedState, applicableOsFamilies and who
configured it when.

An organization without a configuration gets a "not configured" message.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, cfg, err := fetchGCEPConfig(cmd.Context(), client)
			if err != nil {
				return err
			}
			if cfg == nil {
				if !output.CurrentOptions().Quiet {
					fmt.Fprintln(cmd.OutOrStdout(), gcep.NotConfiguredMessage)
				}
				return nil
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
}

func newGCEPConfigSetCmd() *cobra.Command {
	var (
		customerID string
		email      string
		state      string
		osFamilies []string
	)
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Create or change the organization's GCEP configuration",
		Long: `Create or change the organization's GCEP configuration.

Give only what should change. The current configuration is read first and the
complete body (customerId, googleUserEmail, state, applicableOsFamilies) is
sent with only the given fields replaced, so a full-replace PUT cannot clear
the rest. The configuration is read back afterwards and the command fails if
it does not match what was sent.

Creating a configuration (none exists yet) needs all four flags.

  jc gcep config set --state disabled
  jc gcep config set --os mac_os,windows
  jc gcep config set --customer-id C0abc123 --google-user-email admin@example.com \
      --state enabled --os mac_os

--state: enabled or disabled. --os: mac_os, windows (comma-separated).

This applies to the whole organization, so it prompts for confirmation. Use
--plan to preview, or --force to skip the prompt.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ch, err := gcepChangesFromFlags(cmd, customerID, email, state, osFamilies)
			if err != nil {
				return err
			}
			return runGCEPConfigSet(cmd, ch)
		},
	}
	cmd.Flags().StringVar(&customerID, "customer-id", "", "Google customer id")
	cmd.Flags().StringVar(&email, "google-user-email", "", "Google user email the integration acts as")
	cmd.Flags().StringVar(&state, "state", "", "Configuration state: enabled or disabled")
	cmd.Flags().StringSliceVar(&osFamilies, "os", nil, "OS families the integration applies to: mac_os, windows (comma-separated)")
	return cmd
}

// gcepChangesFromFlags validates every given flag before any request is made.
func gcepChangesFromFlags(cmd *cobra.Command, customerID, email, state string, osFamilies []string) (gcep.Changes, error) {
	var ch gcep.Changes
	usage := func(err error) error {
		return NewCLIError(ErrCodeUsageError, err.Error(), "")
	}
	if cmd.Flags().Changed("customer-id") {
		v := strings.TrimSpace(customerID)
		if v == "" {
			return ch, usage(fmt.Errorf("--customer-id must not be empty"))
		}
		ch.CustomerID = &v
	}
	if cmd.Flags().Changed("google-user-email") {
		v := strings.TrimSpace(email)
		if v == "" || !strings.Contains(v, "@") {
			return ch, usage(fmt.Errorf("--google-user-email %q is not an email address", email))
		}
		ch.GoogleUserEmail = &v
	}
	if cmd.Flags().Changed("state") {
		v, err := gcep.NormalizeState(state)
		if err != nil {
			return ch, usage(err)
		}
		ch.State = &v
	}
	if cmd.Flags().Changed("os") {
		v, err := gcep.NormalizeOSFamilies(osFamilies)
		if err != nil {
			return ch, usage(err)
		}
		ch.ApplicableOsFamilies = v
	}
	if ch.Empty() {
		return ch, NewCLIError(ErrCodeUsageError, "no changes requested",
			"Specify at least one of --customer-id, --google-user-email, --state, --os")
	}
	return ch, nil
}

func runGCEPConfigSet(cmd *cobra.Command, ch gcep.Changes) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	_, current, err := fetchGCEPConfig(cmd.Context(), client)
	if err != nil {
		return err
	}

	missing := []string(nil)
	if current == nil {
		missing = gcep.MissingForCreate(ch)
	}
	body := gcep.Merge(current, ch)
	changes := gcep.Compare(current, body)

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:     "update",
			Resource:   "GCEP configuration",
			Target:     "organization (Google customer " + changes[0].Before + ")",
			Reversible: true,
		}
		if current == nil {
			p.Action = "create"
			p.Target = "organization (" + gcep.NotConfiguredMessage + ")"
			p.Effects = append(p.Effects, gcep.NotConfiguredMessage+" This would create it.")
		}
		p.Effects = append(p.Effects, "before → after:")
		for _, c := range changes {
			p.Effects = append(p.Effects, c.Describe())
		}
		if len(missing) > 0 {
			p.Effects = append(p.Effects, "Cannot run as given: creating needs "+strings.Join(missing, ", "))
		}
		p.Effects = append(p.Effects, "Applies to the whole organization")
		return renderPlan(cmd, p)
	}

	if len(missing) > 0 {
		return NewCLIError(ErrCodeUsageError,
			gcep.NotConfiguredMessage+" Creating the configuration needs "+strings.Join(missing, ", "),
			"Give --customer-id, --google-user-email, --state and --os")
	}

	if current != nil && !gcep.AnyChanged(changes) {
		fmt.Fprintln(cmd.ErrOrStderr(), "The GCEP configuration already has these values; no change made.")
		return nil
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		var lines []string
		for _, c := range changes {
			if c.Changed {
				lines = append(lines, "  "+c.Describe())
			}
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Change the GCEP configuration for the whole organization?\n%s\n[y/N] ",
			strings.Join(lines, "\n"))
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

	if _, err := client.Update(cmd.Context(), gcep.ConfigurationEndpoint, body); err != nil {
		return err
	}

	// The PUT returns an empty object, so read back to prove what took effect.
	raw, got, err := fetchGCEPConfig(cmd.Context(), client)
	if err != nil {
		return fmt.Errorf("the GCEP configuration was sent but could not be read back: %w", err)
	}
	if mismatches := gcep.Mismatches(body, got); len(mismatches) > 0 {
		return fmt.Errorf("the GCEP configuration read back after the update does not match what was sent:\n  %s",
			strings.Join(mismatches, "\n  "))
	}
	return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
}

func newGCEPConfigDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete",
		Short: "Remove the organization's GCEP integration (destructive)",
		Long: `Remove the organization's GCEP configuration. This removes the integration
for the whole organization; devices stop reporting GCEP device trust to Google.
The configuration_state_pending_delete state suggests removal completes
asynchronously; check with 'jc gcep config get'.

Shows the configuration before prompting for confirmation. Use --plan to
preview, or --force to skip the prompt; a non-interactive run needs --force.`,
		Args: cobra.NoArgs,
		RunE: runGCEPConfigDelete,
	}
}

func runGCEPConfigDelete(cmd *cobra.Command, args []string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	_, current, err := fetchGCEPConfig(cmd.Context(), client)
	if err != nil {
		return err
	}

	if current == nil {
		if viper.GetBool("plan") {
			return renderPlan(cmd, &plan.Plan{
				Action:   "delete",
				Resource: "GCEP configuration",
				Target:   "none",
				Effects:  []string{gcep.NotConfiguredMessage, "Nothing to remove"},
			})
		}
		fmt.Fprintln(cmd.ErrOrStderr(), gcep.NotConfiguredMessage+" Nothing to delete.")
		return nil
	}

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:   "delete",
			Resource: "GCEP configuration",
			Target:   fmt.Sprintf("Google customer %s (%s)", current.CustomerID, current.GoogleUserEmail),
			Effects: []string{
				"Remove the GCEP integration for the whole organization",
				"state: " + current.State,
				"applicableOsFamilies: " + strings.Join(current.ApplicableOsFamilies, ","),
				"Devices stop reporting GCEP device trust to Google",
			},
		})
	}

	// Removing the integration for the whole organization needs a person at a
	// terminal or an explicit --force: --non-interactive alone is not consent.
	if !viper.GetBool("force") && (isStdinPiped() || viper.GetBool("non-interactive")) {
		return NewCLIError(ErrCodeUsageError,
			"refusing to delete the GCEP configuration without confirmation (no terminal); nothing was sent",
			"Re-run with --force to delete it for the whole organization, or --plan to preview")
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete the GCEP configuration for Google customer %q? "+
			"This removes the integration for the whole organization. [y/N] ", current.CustomerID)
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

	if _, err := client.Delete(cmd.Context(), gcep.ConfigurationEndpoint); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "GCEP configuration for Google customer %q deleted. "+
		"Removal may complete asynchronously; check with: jc gcep config get\n", current.CustomerID)
	return nil
}

func newGCEPDeviceStateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "device-state <device>",
		Short: "Show a device's GCEP trust state",
		Long: `Show a device's GCEP trust state: registrationStatus, managementStatus and the
device users reported to Google (deviceUserId, googleEmail,
localDeviceUsername, lastSuccessfulReportedTime).

Accepts a device hostname or 24-character hex device id. Table and CSV output
list the device users.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if gcep.LooksLikeMalformedID(args[0]) {
				return NewCLIError(ErrCodeUsageError, gcep.ErrNotObjectID(args[0]).Error(), "")
			}
			id := args[0]
			if !gcep.IsObjectID(id) {
				v1, err := newV1Client()
				if err != nil {
					return err
				}
				if id, err = resolveDevice(cmd.Context(), v1, id); err != nil {
					return err
				}
				if !gcep.IsObjectID(id) {
					return gcep.ErrNotObjectID(id)
				}
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), gcep.DeviceStateEndpoint(id))
			if err != nil {
				return err
			}

			opts := output.CurrentOptions()
			if opts.Format != output.FormatTable && opts.Format != output.FormatCSV {
				return output.WriteSingle(cmd.OutOrStdout(), raw, opts)
			}
			state, err := gcep.ParseDeviceState(raw)
			if err != nil {
				return err
			}
			if !opts.Quiet {
				fmt.Fprintf(cmd.ErrOrStderr(), "registrationStatus: %s  managementStatus: %s\n",
					state.RegistrationStatus, state.ManagementStatus)
			}
			opts.DefaultFields = gcep.DeviceUserFields
			return output.WriteList(cmd.OutOrStdout(), state.DeviceUsers, opts)
		},
	}
}
