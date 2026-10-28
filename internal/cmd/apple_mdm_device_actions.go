package cmd

// The Apple MDM device actions: fourteen operations that tell a managed Mac
// to do something. Thirteen are per-device; refresh-dep-devices is not.
//
// THE DEVICE IS READ BEFORE EVERY ACTION, and not only to produce a better
// error. Three things come out of that one GET:
//
//   - A device id that belongs to nothing fails before anything is sent,
//     rather than after.
//   - The prompt can name the machine -- "ERASE device C02TD1PEGTDY
//     (macOS 12.7.6)?" instead of a 24-character id that nobody can check.
//   - The record's own precondition fields go into the plan, so an operator
//     sees what the device claims about itself before wiping it.
//
// ONE COMMAND HAS NO MCP EQUIVALENT: `device-lock with-generated-pin` locks
// the device AND returns the PIN that unlocks it, so it follows the rule the
// recovery-lock password reader follows (apple_mdm_devices.go) -- CLI only,
// nothing printed without --reveal or --clipboard.
//
// Nothing here reports that an action happened. See
// apple_mdm.AcceptedIsNotCompleted: these routes answer 204, and no read in
// this API surface says what became of the command.

import (
	"context"
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

// deviceAction is one action, described once and executed by a single path.
type deviceAction struct {
	// segment is the API path segment and the word used in messages.
	segment string
	// label is what a human reads: "erase", "clear the passcode of".
	label string
	// prompt is the verb in the confirmation question, capitalized.
	// Empty means the action is not destructive enough to confirm.
	prompt string
	// effects is what --plan lists.
	effects []string
	// reversible feeds the plan. False does not mean "it will succeed".
	reversible bool
}

// readDeviceForAction resolves both ids and reads the device.
//
// A 404 here is unambiguous -- unlike the sub-route 404s in
// apple_mdm.AbsenceAndTypoBothAnswer404, this IS the device read -- so it is
// reported directly.
func readDeviceForAction(ctx context.Context, client *api.V2Client, mdmArg, deviceArg string) (
	mdmID, deviceID string, pre apple_mdm.Preconditions, err error) {

	mdmID, deviceID, err = appleMDMDeviceTarget(ctx, client, mdmArg, deviceArg)
	if err != nil {
		return "", "", pre, err
	}
	raw, err := client.Get(ctx, apple_mdm.DeviceEndpoint(mdmID, deviceID))
	if err != nil {
		if apiErr, ok := asAPIError(err); ok && apiErr.StatusCode == http.StatusNotFound {
			return "", "", pre, fmt.Errorf("no device %s is managed by Apple MDM configuration %s — "+
				"check the id against `jc apple-mdm devices %s`", deviceID, mdmID, mdmID)
		}
		return "", "", pre, fmt.Errorf("reading device %s before acting on it: %w", deviceID, err)
	}
	pre, err = apple_mdm.ParsePreconditions(raw)
	if err != nil {
		return "", "", pre, err
	}
	return mdmID, deviceID, pre, nil
}

// planTarget is describeTarget trimmed to fit plan.RenderHuman's fixed-width
// box, which pads a target to 48 columns and does not wrap. The serial plus
// the id is what identifies the machine; the OS version is in the prompt,
// which is not boxed.
func planTarget(deviceID string, pre apple_mdm.Preconditions) string {
	if pre.SerialNumber == "" {
		return deviceID
	}
	return fmt.Sprintf("%s (%s)", pre.SerialNumber, deviceID)
}

// describeTarget names the machine in the words an operator can check against
// the thing on the desk.
func describeTarget(deviceID string, pre apple_mdm.Preconditions) string {
	parts := []string{}
	if pre.SerialNumber != "" {
		parts = append(parts, pre.SerialNumber)
	}
	if pre.OSFamily != "" || pre.OSVersion != "" {
		parts = append(parts, strings.TrimSpace(pre.OSFamily+" "+pre.OSVersion))
	}
	if len(parts) == 0 {
		return deviceID
	}
	return fmt.Sprintf("%s (%s)", strings.Join(parts, ", "), deviceID)
}

// runDeviceAction is the one path every per-device action takes.
func runDeviceAction(cmd *cobra.Command, mdmArg, deviceArg string, act deviceAction, body any) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	ctx := cmd.Context()

	mdmID, deviceID, pre, err := readDeviceForAction(ctx, client, mdmArg, deviceArg)
	if err != nil {
		return err
	}

	target := describeTarget(deviceID, pre)
	note := pre.PreconditionNote(act.segment)

	if viper.GetBool("plan") {
		effects := append([]string{}, act.effects...)
		effects = append(effects,
			"Queued until the device's next check-in",
			"This API never reports whether it ran")
		if note != "" {
			effects = append(effects, "Note: "+note)
		}
		return renderPlan(cmd, &plan.Plan{
			// act.label, not act.segment: the plan header is read by a human
			// and refreshActivationLockInformation does not fit the box. The
			// resource stays "apple-mdm" rather than "apple-mdm device" for
			// the same reason — the device is named in Target.
			Action:     act.label,
			Resource:   "apple-mdm",
			Target:     planTarget(deviceID, pre),
			Effects:    effects,
			Reversible: act.reversible,
		})
	}

	if act.prompt != "" {
		if mustAbortWithoutTTY() {
			fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
			return nil
		}
		if shouldConfirm() {
			if note != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "Note: %s\n", note)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "%s %s? [y/N] ", act.prompt, target)
			reader := getConfirmReader()
			answer, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading confirmation: %w", err)
			}
			if a := strings.TrimSpace(strings.ToLower(answer)); a != "y" && a != "yes" {
				fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
				return nil
			}
		}
	}

	// EmptyActionBody, never nil: see apple_mdm.NilBodyIsSentAsNull.
	if body == nil {
		body = apple_mdm.EmptyActionBody()
	}
	if _, err := client.Create(ctx, apple_mdm.DeviceActionEndpoint(mdmID, deviceID, act.segment), body); err != nil {
		return err
	}

	outcome := apple_mdm.Outcome{
		Action: act.label, DeviceID: deviceID, MDMID: mdmID, Accepted: true,
	}
	if !output.CurrentOptions().Quiet {
		fmt.Fprintln(cmd.OutOrStdout(), outcome.Describe())
	}
	return nil
}

// simpleActionCmd builds a command for an action that takes no body.
func simpleActionCmd(use, short, long string, act deviceAction) *cobra.Command {
	return &cobra.Command{
		Use:               use,
		Short:             short,
		Long:              long,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceAction(cmd, args[0], args[1], act, nil)
		},
	}
}

// ------------------------------------------------------------------- erase

func newAppleMDMEraseCmd() *cobra.Command {
	var pin string

	cmd := &cobra.Command{
		Use:   "erase <mdm-name-or-id> <device-id>",
		Short: "Erase a managed Apple device (destructive)",
		Long: `Send the MDM erase command to a device managed by Apple MDM.

WARNING: this wipes the device. It is irreversible, and --confirm-erase is
required as well as the usual confirmation prompt.

The API documents the six-digit PIN as REQUIRED FOR MACOS while declaring it
optional in the schema. jc does not guess: an omitted PIN is sent as omitted
and the API decides. The PIN is checked against the API's own pattern
(six digits) before the request leaves, so a typo costs nothing.

The command is queued and delivered at the device's next check-in. A device
that is powered off is NOT a safe target — it is erased when it returns.`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Before --plan, matching `jc devices erase`: a plan that renders
			// without the safety flag teaches the flag is optional.
			confirmErase, _ := cmd.Flags().GetBool("confirm-erase")
			if !confirmErase {
				return fmt.Errorf("erasing a device is irreversible. You must pass --confirm-erase to proceed")
			}
			if err := apple_mdm.ValidatePIN(pin); err != nil {
				return err
			}
			return runDeviceAction(cmd, args[0], args[1], deviceAction{
				segment: apple_mdm.ActionErase,
				label:   "erase",
				prompt:  "ERASE (wipe all data on)",
				effects: []string{"Wipes the device. Irreversible."},
			}, apple_mdm.PINBody{PIN: pin})
		},
	}
	cmd.Flags().Bool("confirm-erase", false, "Required safety flag to confirm the erase")
	cmd.Flags().StringVar(&pin, "pin", "", "Six-digit PIN (the API documents this as required for macOS)")
	return cmd
}

// -------------------------------------------------------------------- lock

func newAppleMDMDeviceLockSendCmd() *cobra.Command {
	var pin string

	cmd := &cobra.Command{
		Use:   "send <mdm-name-or-id> <device-id>",
		Short: "Send the MDM lock command to a device",
		Long: `Lock a managed Apple device with the MDM lock command.

On a Mac this is a firmware-level lock: the machine stops at a PIN prompt and
the six-digit PIN is the only way past it. The API documents the PIN as
required for macOS while declaring it optional, so jc sends what you give it
and lets the API decide — but it checks the six-digit shape first.

To have JumpCloud generate the PIN instead, use ` + "`with-generated-pin`" + `.
To read the PIN of a device already locked, use ` + "`pin --reveal`" + `.`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := apple_mdm.ValidatePIN(pin); err != nil {
				return err
			}
			return runDeviceAction(cmd, args[0], args[1], deviceAction{
				segment: apple_mdm.ActionLock,
				label:   "lock",
				prompt:  "LOCK",
				effects: []string{"Locks the device behind the PIN"},
			}, apple_mdm.PINBody{PIN: pin})
		},
	}
	cmd.Flags().StringVar(&pin, "pin", "", "Six-digit PIN (the API documents this as required for macOS)")
	return cmd
}

// lock with a PIN JumpCloud generates -- CLI only, because the response
// carries the PIN.
func newAppleMDMLockWithGeneratedPINCmd() *cobra.Command {
	var (
		out     secretOutput
		message string
		phone   string
	)

	cmd := &cobra.Command{
		Use:   "with-generated-pin <mdm-name-or-id> <device-id>",
		Short: "Lock a device with a JumpCloud-generated PIN (CLI only)",
		Long: `Lock a managed device with a PIN that JumpCloud generates and returns.

NOTHING IS PRINTED without --reveal or --clipboard: the response carries the
PIN that unlocks the device, and a locked Mac whose PIN is in a terminal
transcript is not locked. ` + "`jc apple-mdm device-lock pin`" + ` reads it back later.

There is no MCP tool for this command, deliberately — it locks a machine and
hands back the credential that unlocks it, and no confirmation undoes a value
already written into a transcript.

The response shape has never been observed: no device on the probed tenant
had a lock PIN, so the body is passed through as the API returns it rather
than being decoded against a guessed key.`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			mdmID, deviceID, pre, err := readDeviceForAction(ctx, client, args[0], args[1])
			if err != nil {
				return err
			}
			target := describeTarget(deviceID, pre)

			if viper.GetBool("plan") {
				return renderPlan(cmd, &plan.Plan{
					Action:   "lock-with-generated-pin",
					Resource: "apple-mdm",
					Target:   planTarget(deviceID, pre),
					Effects: []string{
						"Locks the device with a generated PIN",
						"Prints nothing without --reveal/--clipboard",
					},
					Reversible: false,
				})
			}
			if mustAbortWithoutTTY() {
				fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
				return nil
			}
			if shouldConfirm() {
				fmt.Fprintf(cmd.ErrOrStderr(), "LOCK %s with a generated PIN? [y/N] ", target)
				reader := getConfirmReader()
				answer, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading confirmation: %w", err)
				}
				if a := strings.TrimSpace(strings.ToLower(answer)); a != "y" && a != "yes" {
					fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
					return nil
				}
			}

			body := map[string]string{}
			if message != "" {
				body["message"] = message
			}
			if phone != "" {
				body["phoneNumber"] = phone
			}
			raw, err := client.Create(ctx,
				apple_mdm.DeviceActionEndpoint(mdmID, deviceID, apple_mdm.ActionLockWithGeneratedPIN), body)
			if err != nil {
				return err
			}
			pin, err := apple_mdm.ParseLockPIN(raw)
			if err != nil {
				return err
			}
			return emitDeviceSecret(cmd, out, "generated lock PIN", deviceID, strings.TrimSpace(string(pin)))
		},
	}
	cmd.Flags().StringVar(&message, "message", "", "Message to show on the locked screen")
	cmd.Flags().StringVar(&phone, "phone", "", "Phone number to show on the locked screen")
	addDeviceSecretFlags(cmd, &out)
	return cmd
}

// ----------------------------------------------------- restart / shutdown

func newAppleMDMRestartCmd() *cobra.Command {
	var kextPaths []string

	cmd := &cobra.Command{
		Use:   "restart <mdm-name-or-id> <device-id>",
		Short: "Restart a managed Apple device",
		Long: `Restart a managed Apple device.

The command is queued and delivered at the device's next check-in, so a
machine that is currently offline restarts when it comes back rather than not
at all.

--kext-path rebuilds the kernel cache for the named kexts as part of the
restart.`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceAction(cmd, args[0], args[1], deviceAction{
				segment:    apple_mdm.ActionRestart,
				label:      "restart",
				prompt:     "RESTART",
				effects:    []string{"Restarts the device, interrupting its user"},
				reversible: true,
			}, apple_mdm.RestartBody{KextPaths: kextPaths})
		},
	}
	cmd.Flags().StringSliceVar(&kextPaths, "kext-path", nil,
		"Kext path to rebuild the kernel cache for (repeatable)")
	return cmd
}

func newAppleMDMShutdownCmd() *cobra.Command {
	return simpleActionCmd(
		"shutdown <mdm-name-or-id> <device-id>",
		"Shut down a managed Apple device",
		`Shut down a managed Apple device.

The command is queued and delivered at the device's next check-in. Note that
a shut-down device cannot receive the next command — including a restart — so
this is the one action in this family that removes your own ability to undo
it remotely.`,
		deviceAction{
			segment: apple_mdm.ActionShutdown,
			label:   "shutdown",
			prompt:  "SHUT DOWN",
			effects: []string{
				"Shuts the device down",
				"No further MDM commands until powered on",
			},
		})
}

// --------------------------------------------------------------- the clears

func newAppleMDMClearPasscodeCmd() *cobra.Command {
	return simpleActionCmd(
		"clear-passcode <mdm-name-or-id> <device-id>",
		"Clear a managed device's passcode",
		`Remove the passcode from a managed Apple device.

The device is left with no passcode, which is a reduction in its security
posture, not a neutral act.`,
		deviceAction{
			segment: apple_mdm.ActionClearPasscode,
			label:   "clear-passcode",
			prompt:  "CLEAR THE PASSCODE of",
			effects: []string{"Removes the passcode, leaving none set"},
		})
}

func newAppleMDMClearRestrictionsPasswordCmd() *cobra.Command {
	return simpleActionCmd(
		"clear-restrictions-password <mdm-name-or-id> <device-id>",
		"Clear a managed device's restrictions password",
		`Remove the restrictions (Screen Time) password from a managed Apple device.`,
		deviceAction{
			segment: apple_mdm.ActionClearRestrictionsPasswd,
			label:   "clear-restrictions-password",
			prompt:  "CLEAR THE RESTRICTIONS PASSWORD of",
			effects: []string{"Removes the restrictions password"},
		})
}

func newAppleMDMClearActivationLockCmd() *cobra.Command {
	return simpleActionCmd(
		"clear-activation-lock <mdm-name-or-id> <device-id>",
		"Clear a managed device's activation lock",
		`Clear the activation lock on a managed Apple device, so it can be set up by
someone other than the Apple ID that locked it.

Whether this can work is visible in the device record before you try:
securityInfo.isActivationLockManageable and hasActivationLockBypassCodes. jc
reports what the device claims in the plan and in the prompt — on the probed
tenant six of eight devices reported the lock manageable and the two oldest
reported it not. The API may still answer 409.`,
		deviceAction{
			segment: apple_mdm.ActionClearActivationLock,
			label:   "clear-activation-lock",
			prompt:  "CLEAR THE ACTIVATION LOCK of",
			effects: []string{"Releases the device from its Apple ID"},
		})
}

// ----------------------------------------------------------- the safe three

func newAppleMDMRefreshActivationLockCmd() *cobra.Command {
	return simpleActionCmd(
		"refresh-activation-lock <mdm-name-or-id> <device-id>",
		"Refresh a device's activation lock information",
		`Ask JumpCloud to refresh what it knows about a device's activation lock.

This changes nothing on the device — it updates JumpCloud's own view, and is
one of the three actions in this family that was verified live rather than
mocked.`,
		deviceAction{
			segment:    apple_mdm.ActionRefreshActivationLock,
			label:      "refresh-activation-lock",
			effects:    []string{"Refreshes JumpCloud's own lock information"},
			reversible: true,
		})
}

func newAppleMDMOSUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "os-update",
		Short: "Query or schedule an OS update on a managed device",
	}
	cmd.AddCommand(newAppleMDMOSUpdateStatusCmd())
	cmd.AddCommand(newAppleMDMScheduleOSUpdateCmd())
	return cmd
}

func newAppleMDMOSUpdateStatusCmd() *cobra.Command {
	return simpleActionCmd(
		"status <mdm-name-or-id> <device-id>",
		"Ask a device to report its OS update status",
		`Ask a managed device to report the status of its OS update.

This queues a query, not a change, and is one of the three actions in this
family verified live. The answer does not come back here: the API returns 204
with no body, and nothing in this surface reports the result — not even
`+"`jc apple-mdm background-tasks`"+`, which stayed empty across a verified queue.`,
		deviceAction{
			segment: apple_mdm.ActionOSUpdateStatus,
			label:   "os-update-status",
			effects: []string{
				"Asks the device for its OS update status",
				"The answer is not returned by this API",
			},
			reversible: true,
		})
}

func newAppleMDMScheduleOSUpdateCmd() *cobra.Command {
	var (
		installAction string
		productKey    string
		maxDeferrals  int
	)

	cmd := &cobra.Command{
		Use:   "schedule <mdm-name-or-id> <device-id>",
		Short: "Schedule an OS update on a managed device",
		Long: `Schedule an OS update on a managed Apple device.

--product-key is required by the API and NOTHING IN THIS API WILL TELL YOU
WHAT TO PUT IN IT: there is no available-updates read anywhere in the Apple
MDM surface, and the API accepts any string without validating it. A wrong
key is accepted and then does nothing on the device.

--install-action is validated against the API's own enum before the request
leaves.

INSTALL_FORCE_RESTART restarts the device without asking the person using it.`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			var deferrals *int
			if cmd.Flags().Changed("max-deferrals") {
				deferrals = &maxDeferrals
			}
			body, err := apple_mdm.NewOSUpdateBody(installAction, productKey, deferrals)
			if err != nil {
				return err
			}
			return runDeviceAction(cmd, args[0], args[1], deviceAction{
				segment: apple_mdm.ActionScheduleOSUpdate,
				label:   "schedule-os-update",
				prompt:  "SCHEDULE AN OS UPDATE on",
				effects: []string{
					"Schedules an OS update: " + installAction,
					"The product key is not validated by the API",
				},
			}, body)
		},
	}
	cmd.Flags().StringVar(&installAction, "install-action", "",
		"One of "+strings.Join(apple_mdm.InstallActions(), ", "))
	cmd.Flags().StringVar(&productKey, "product-key", "",
		"Apple software update product key (required; the API offers no way to list them)")
	cmd.Flags().IntVar(&maxDeferrals, "max-deferrals", 0,
		"How many times the user may defer the update")
	return cmd
}

// ---------------------------------------------------------------- lost mode

func newAppleMDMLostModeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lost-mode",
		Short: "Enable or disable lost mode on a managed device",
		Long: `Put a managed Apple device into lost mode, or take it out.

Lost mode locks the device and displays a message and phone number so whoever
finds it can return it.

The API's own error here is misleading and worth knowing about: an empty body
answers 400 "command is required", but there is no field called ` + "`command`" + `
— the message names an internal oneof, and the real body is enableLostMode or
disableLostMode. jc builds it so that exactly one of the two can ever be set,
because the API accepts a body with both, silently discards the enable, and
does not say which command it ran.`,
	}
	cmd.AddCommand(newAppleMDMLostModeEnableCmd())
	cmd.AddCommand(newAppleMDMLostModeDisableCmd())
	return cmd
}

func newAppleMDMLostModeEnableCmd() *cobra.Command {
	var message, phone, footnote string

	cmd := &cobra.Command{
		Use:   "enable <mdm-name-or-id> <device-id>",
		Short: "Put a managed device into lost mode",
		Long: `Put a managed Apple device into lost mode.

At least one of --message or --phone is required. That rule is the API's, not
jc's: the API answers 400 "message or phone number must be provided to enable
Lost Mode", and the spec does not mention it at all.

--footnote is accepted by the API, but so is any misspelled field: unknown
body fields are swallowed silently, so acceptance is not evidence that a
footnote reaches the screen.`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := apple_mdm.EnableLostMode(message, phone, footnote)
			if err != nil {
				return err
			}
			if err := body.Validate(); err != nil {
				return err
			}
			return runDeviceAction(cmd, args[0], args[1], deviceAction{
				segment: apple_mdm.ActionLostMode,
				label:   "lost-mode enable",
				prompt:  "PUT INTO LOST MODE",
				effects: []string{"Locks the device and shows your message"},
			}, body)
		},
	}
	cmd.Flags().StringVar(&message, "message", "", "Message to display on the lost device")
	cmd.Flags().StringVar(&phone, "phone", "", "Phone number to display on the lost device")
	cmd.Flags().StringVar(&footnote, "footnote", "", "Footnote to display (acceptance is not proof it is shown)")
	return cmd
}

func newAppleMDMLostModeDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <mdm-name-or-id> <device-id>",
		Short: "Take a managed device out of lost mode",
		Long: `Take a managed Apple device out of lost mode.

The device record reports lostMode, and jc shows it in the plan. Every device
on the probed tenant reported the bare boolean false; the shape it takes when
lost mode is ACTIVE has never been observed, so an unrecognized value is
reported as unknown rather than as "not in lost mode".`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := apple_mdm.DisableLostMode()
			if err := body.Validate(); err != nil {
				return err
			}
			return runDeviceAction(cmd, args[0], args[1], deviceAction{
				segment:    apple_mdm.ActionLostMode,
				label:      "lost-mode disable",
				prompt:     "TAKE OUT OF LOST MODE",
				effects:    []string{"Takes the device out of lost mode"},
				reversible: true,
			}, body)
		},
	}
}

// ---------------------------------------------------------------- unenroll

func newAppleMDMUnenrollCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unenroll <mdm-name-or-id> <device-id>",
		Short: "Remove a device's Apple MDM enrollment (destructive)",
		Long: `Remove a device's enrollment from an Apple MDM configuration.

The device stops being managed: profiles and policies delivered over MDM are
no longer enforced, and re-enrolling it is not something this command can
undo for you.

Unlike the rest of this family, this route answers 200 with the removed
device record rather than 204, so the record is printed.`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			mdmID, deviceID, pre, err := readDeviceForAction(ctx, client, args[0], args[1])
			if err != nil {
				return err
			}
			target := describeTarget(deviceID, pre)

			if viper.GetBool("plan") {
				return renderPlan(cmd, &plan.Plan{
					Action:   "unenroll",
					Resource: "apple-mdm",
					Target:   planTarget(deviceID, pre),
					Effects: []string{
						"Removes the device's Apple MDM enrollment",
						"MDM profiles and policies stop applying",
					},
					Reversible: false,
				})
			}
			if mustAbortWithoutTTY() {
				fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
				return nil
			}
			if shouldConfirm() {
				fmt.Fprintf(cmd.ErrOrStderr(), "UNENROLL %s from Apple MDM? [y/N] ", target)
				reader := getConfirmReader()
				answer, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading confirmation: %w", err)
				}
				if a := strings.TrimSpace(strings.ToLower(answer)); a != "y" && a != "yes" {
					fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
					return nil
				}
			}

			raw, err := client.Delete(ctx, apple_mdm.DeviceEndpoint(mdmID, deviceID))
			if err != nil {
				return err
			}
			// The route is documented as returning the removed record. If a
			// tenant ever answers 204 instead, say what happened rather than
			// failing on an empty decode.
			if len(strings.TrimSpace(string(raw))) == 0 {
				if !output.CurrentOptions().Quiet {
					fmt.Fprintln(cmd.OutOrStdout(), apple_mdm.Outcome{
						Action: "unenroll", DeviceID: deviceID, MDMID: mdmID, Accepted: true,
					}.Describe())
				}
				return nil
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

// ------------------------------------------------------- refresh dep devices

func newAppleMDMRefreshDEPDevicesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh-dep-devices <mdm-name-or-id>",
		Short: "Re-sync a configuration's DEP device list",
		Long: `Ask JumpCloud to re-sync the DEP (Automated Device Enrollment) device list for
an Apple MDM configuration.

This is the only action in this area that is not per-device, and one of the
three verified live. It changes nothing on any device.`,
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
			if viper.GetBool("plan") {
				return renderPlan(cmd, &plan.Plan{
					Action:     "refresh-dep-devices",
					Resource:   "apple-mdm",
					Target:     mdmID,
					Effects:    []string{"Re-syncs this configuration's DEP devices"},
					Reversible: true,
				})
			}
			if _, err := client.Create(cmd.Context(),
				apple_mdm.RefreshDEPDevicesEndpoint(mdmID), apple_mdm.EmptyActionBody()); err != nil {
				return err
			}
			if !output.CurrentOptions().Quiet {
				fmt.Fprintln(cmd.OutOrStdout(), apple_mdm.Outcome{
					Action: "refresh-dep-devices", MDMID: mdmID, Accepted: true,
				}.Describe())
			}
			return nil
		},
	}
}
