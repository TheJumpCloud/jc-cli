package cmd

// The Apple MDM device reads: device detail, the push-certificate CSR, the
// recovery-lock pair and the device-lock pair.
//
// TWO OF THESE COMMANDS HAVE NO MCP EQUIVALENT. `recovery-lock password` and
// `device-lock pin` return the credentials that unlock a Mac, and the
// reasoning is the same as for the vault secret readers in
// password_vault_secrets.go: reading a secret mutates nothing, so it
// classifies as read-only and passes every gate the MCP server has, and the
// damage is done the moment the value is in a transcript. They follow the same
// convention too -- nothing is printed without --reveal or --clipboard,
// because "does this device have a recovery lock password?" is the common
// question and answering it should not put the password into scrollback.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

// appleMDMDeviceTarget resolves the pair of ids these routes need.
//
// The configuration accepts a name or an id, matching `jc apple-mdm devices`.
// The device is an id only: there is no device-name index to resolve against
// here, and it is validated for shape before it reaches a request path, the
// same rule the ADE reads follow.
func appleMDMDeviceTarget(ctx context.Context, client *api.V2Client, mdmArg, deviceArg string) (string, string, error) {
	mdmID, err := resolveAppleMDM(ctx, client, mdmArg)
	if err != nil {
		return "", "", err
	}
	if err := apple_mdm.RequireObjectID("a managed device", deviceArg); err != nil {
		return "", "", err
	}
	return mdmID, deviceArg, nil
}

// absenceOrMissingDevice turns an ambiguous 404 into a true statement.
//
// See apple_mdm.AbsenceAndTypoBothAnswer404: the recovery-lock and lock-pin
// reads answer 404 both for a device that has nothing set and for a device id
// that belongs to nothing, with identical bodies. Reporting the first without
// checking would tell an operator that a device they cannot even see has no
// recovery lock.
//
// So on a 404 -- and only then -- the device itself is read. If it exists the
// 404 meant absence and is reported as such; if it does not, the error says
// that instead. Any other failure is passed through untouched.
func absenceOrMissingDevice(ctx context.Context, client *api.V2Client, mdmID, deviceID, what string, err error) error {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		return err
	}

	if _, probeErr := client.Get(ctx, apple_mdm.DeviceEndpoint(mdmID, deviceID)); probeErr != nil {
		var probeAPIErr *api.APIError
		if errors.As(probeErr, &probeAPIErr) && probeAPIErr.StatusCode == http.StatusNotFound {
			return fmt.Errorf("no device %s is managed by Apple MDM configuration %s — "+
				"check the id against `jc apple-mdm devices %s`", deviceID, mdmID, mdmID)
		}
		// The device read failed for some other reason, so whether the device
		// exists is now unknown. Say that rather than picking an answer.
		return fmt.Errorf("%s returned 404 for device %s, and reading the device to find out "+
			"whether it exists also failed: %w", what, deviceID, probeErr)
	}

	// Message, not Err: Error() renders Message and Err is json:"-", so
	// setting only Err produces an error whose message is empty.
	// Reported through the exit code as well as the body, so a script can
	// branch on absence without parsing the output.
	return &CLIError{
		Code:       ErrCodeGeneral,
		Message:    fmt.Sprintf("device %s has no %s", deviceID, what),
		Suggestion: "the device exists and was read to confirm it, so this is an absence rather than a bad id",
	}
}

// ---------------------------------------------------------------- device get

func newAppleMDMDeviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device <mdm-name-or-id> <device-id>",
		Short: "Show one Apple MDM managed device",
		Long: `Show the full record for one device managed by an Apple MDM configuration.

` + "`jc apple-mdm devices`" + ` lists them; this reads one. The record carries more
than the listing shows, including deviceInformation (model, capacity, OS build)
and securityInfo (recovery lock, FileVault, passcode compliance).

Note that the record reports mdmOsFamily as "macOS", while the ADE settings
commands call the same platform "DARWIN". jc reports each as the API gives it
rather than translating between them.`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			mdmID, deviceID, err := appleMDMDeviceTarget(cmd.Context(), client, args[0], args[1])
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), apple_mdm.DeviceEndpoint(mdmID, deviceID))
			if err != nil {
				return absenceOrMissingDevice(cmd.Context(), client, mdmID, deviceID, "device record", err)
			}
			device, err := apple_mdm.ParseDevice(raw)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), device, output.CurrentOptions())
		},
	}
	return cmd
}

// ----------------------------------------------------------------------- csr

func newAppleMDMCSRCmd() *cobra.Command {
	var asBase64 bool

	cmd := &cobra.Command{
		Use:   "csr <mdm-name-or-id>",
		Short: "Read the Apple push-certificate signing request",
		Long: `Read the certificate signing request for this configuration's Apple push
certificate — the document you upload to Apple to renew MDM push.

The API answers this one with a bare base64 string rather than JSON; it decodes
to an Apple plist holding the certificate chain, the CSR and its signature. jc
prints the decoded plist by default, or the base64 exactly as received with
--base64, which is the form to save to a file.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			mdmID, err := resolveAppleMDM(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			body, err := client.Get(cmd.Context(), apple_mdm.CSREndpoint(mdmID))
			if err != nil {
				return err
			}
			if asBase64 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), strings.TrimSpace(string(body)))
				return err
			}
			plist, err := apple_mdm.ParseCSR(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.TrimRight(string(plist), "\n"))
			return err
		},
	}
	cmd.Flags().BoolVar(&asBase64, "base64", false,
		"Print the base64 body exactly as the API returned it, instead of the decoded plist")
	return cmd
}

// -------------------------------------------------------------- recovery lock

func newAppleMDMRecoveryLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recovery-lock",
		Short: "Recovery lock state and password for a managed Mac",
		Long: `Read the recovery lock configuration for a managed Mac, and — with
` + "`password --reveal`" + ` — the recovery lock password itself.

A recovery lock password is what a Mac asks for before it will boot to
recoveryOS, so it is the credential that stands between a stolen machine and a
reinstall. ` + "`password`" + ` prints nothing unless asked.`,
	}
	cmd.AddCommand(newAppleMDMRecoveryLockConfigCmd())
	cmd.AddCommand(newAppleMDMRecoveryLockPasswordCmd())
	return cmd
}

func newAppleMDMRecoveryLockConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "config <mdm-name-or-id> <device-id>",
		Aliases: []string{"get"},
		Short:   "Show the recovery lock configuration for a device",
		Long: `Show whether a managed Mac has a recovery lock, when its password was last
rotated and when it is due to rotate again.

Three fields here claim to describe the same thing and do not always agree. A
device on the probe tenant reported isLockEnabled true, status SET_PENDING and
uiStatus OFF at once — and still had a readable password. jc prints all three
and says so rather than resolving them to one confident answer, and the
summary line never claims a device has nothing when a password may still be
readable.

nextRotationAt is advisory: it has been seen to change between reads.`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			mdmID, deviceID, err := appleMDMDeviceTarget(cmd.Context(), client, args[0], args[1])
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), apple_mdm.RecoveryLockConfigEndpoint(mdmID, deviceID))
			if err != nil {
				return absenceOrMissingDevice(cmd.Context(), client, mdmID, deviceID,
					"recovery lock configured", err)
			}
			cfg, err := apple_mdm.ParseRecoveryLockConfig(raw)
			if err != nil {
				return err
			}

			opts := output.CurrentOptions()
			if err := output.WriteSingle(cmd.OutOrStdout(), raw, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				fmt.Fprintf(cmd.ErrOrStderr(), "── %s ──\n", cfg.Describe())
			}
			return nil
		},
	}
}

func newAppleMDMRecoveryLockPasswordCmd() *cobra.Command {
	var out secretOutput

	cmd := &cobra.Command{
		Use:   "password <mdm-name-or-id> <device-id>",
		Short: "Read a device's recovery lock password (CLI only)",
		Long: `Read the recovery lock password for a managed Mac.

NOTHING IS PRINTED without --reveal or --clipboard. Without them this reports
only whether a password exists, which is the question usually being asked and
does not put the credential into terminal scrollback, a CI log or a screen
recording.

There is no MCP tool for this command, deliberately. Reading a secret changes
nothing, so it would classify as read-only and pass every gate that server
has, and no confirmation undoes a value already written into a transcript.

A device with no recovery lock password and a device id that does not exist
both answer 404 here, so jc reads the device before deciding which happened.`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			mdmID, deviceID, err := appleMDMDeviceTarget(cmd.Context(), client, args[0], args[1])
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), apple_mdm.RecoveryLockPasswordEndpoint(mdmID, deviceID))
			if err != nil {
				return absenceOrMissingDevice(cmd.Context(), client, mdmID, deviceID,
					"recovery lock password", err)
			}
			password, err := apple_mdm.ParseRecoveryLockPassword(raw)
			if err != nil {
				return err
			}
			return emitDeviceSecret(cmd, out, "recovery lock password", deviceID, password)
		},
	}
	addDeviceSecretFlags(cmd, &out)
	return cmd
}

// ---------------------------------------------------------------- device lock

func newAppleMDMDeviceLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device-lock",
		Short: "Device lock state and PIN for a managed device",
		Long: `Read the MDM device-lock state for a managed device, and — with
` + "`pin --reveal`" + ` — the PIN that was generated when it was locked.

This is the lock applied by MDM (lost mode and the lock command), not the
recovery lock that guards recoveryOS on a Mac. See ` + "`jc apple-mdm recovery-lock`" + `
for that.`,
	}
	cmd.AddCommand(newAppleMDMLockStateCmd())
	cmd.AddCommand(newAppleMDMLockPINCmd())
	return cmd
}

func newAppleMDMLockStateCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "state <mdm-name-or-id> <device-id>",
		Aliases: []string{"get"},
		Short:   "Show the MDM device-lock state",
		Long: `Show whether a managed device is locked by MDM, and why a previous lock
failed if one did.

Every string in this record carries a protobuf-style sentinel rather than an
empty value: failureReason reads "lock_failure_reason_unspecified" when there
is nothing to report, which means nothing is wrong. jc folds those away in the
summary line rather than presenting them as findings.`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			mdmID, deviceID, err := appleMDMDeviceTarget(cmd.Context(), client, args[0], args[1])
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), apple_mdm.LockStateEndpoint(mdmID, deviceID))
			if err != nil {
				return absenceOrMissingDevice(cmd.Context(), client, mdmID, deviceID,
					"device lock state", err)
			}
			state, err := apple_mdm.ParseLockState(raw)
			if err != nil {
				return err
			}

			opts := output.CurrentOptions()
			if err := output.WriteSingle(cmd.OutOrStdout(), raw, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				fmt.Fprintf(cmd.ErrOrStderr(), "── %s ──\n", state.Describe())
			}
			return nil
		},
	}
}

func newAppleMDMLockPINCmd() *cobra.Command {
	var out secretOutput

	cmd := &cobra.Command{
		Use:   "pin <mdm-name-or-id> <device-id>",
		Short: "Read a device's MDM lock PIN (CLI only)",
		Long: `Read the PIN generated when a device was locked by MDM — the code that
unlocks it.

NOTHING IS PRINTED without --reveal or --clipboard, and there is no MCP tool
for this command, for the same reasons as ` + "`recovery-lock password`" + `.

No device on any tenant jc has probed has a lock PIN set, so only the "no PIN"
path of this read is verified against the live API. The success body is
therefore passed through as received rather than decoded against a guessed
field name: reporting "no PIN" because a guess missed would be a false
negative about device security.`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			mdmID, deviceID, err := appleMDMDeviceTarget(cmd.Context(), client, args[0], args[1])
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), apple_mdm.LockPINEndpoint(mdmID, deviceID))
			if err != nil {
				return absenceOrMissingDevice(cmd.Context(), client, mdmID, deviceID,
					"MDM lock PIN", err)
			}
			body, err := apple_mdm.ParseLockPIN(raw)
			if err != nil {
				return err
			}
			// Unverified shape, so the whole body is the secret: it is not
			// known which field carries the PIN, and printing "the parts that
			// are not secret" would mean guessing which those are.
			return emitDeviceSecret(cmd, out, "MDM lock PIN", deviceID,
				strings.TrimSpace(string(body)))
		},
	}
	addDeviceSecretFlags(cmd, &out)
	return cmd
}

// ------------------------------------------------------------ secret plumbing

// addDeviceSecretFlags registers --reveal and --clipboard.
//
// Deliberately NOT addSecretFlags from password_vault_secrets.go: that one
// also registers --field, which exists because a vault credential holds
// several named secrets. These reads return a single value, and a --field flag
// with nothing to select would be a flag that cannot be used correctly.
func addDeviceSecretFlags(cmd *cobra.Command, o *secretOutput) {
	cmd.Flags().BoolVar(&o.reveal, "reveal", false,
		"Print the value to stdout. Without this, only whether one exists is reported.")
	cmd.Flags().BoolVarP(&o.toClip, "clipboard", "c", false,
		"Copy the value to the clipboard instead of printing it")
}

// emitDeviceSecret honours the output choice, printing nothing by default.
func emitDeviceSecret(cmd *cobra.Command, o secretOutput, what, deviceID, value string) error {
	switch {
	case o.toClip:
		if err := clipboardWrite(value); err != nil {
			return fmt.Errorf("copying the %s to the clipboard: %w", what, err)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "%s for device %s copied to the clipboard\n", what, deviceID)
		return nil
	case o.reveal:
		_, err := fmt.Fprintln(cmd.OutOrStdout(), value)
		return err
	default:
		fmt.Fprintf(cmd.ErrOrStderr(),
			"device %s has a %s. Pass --reveal to print it, or --clipboard to copy it.\n",
			deviceID, what)
		return nil
	}
}
