package mcp

import (
	"context"
	"fmt"
	"strings"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
)

// The Apple MDM device actions. THIRTEEN tools for fourteen operations.
//
// The missing one is lockwithgeneratedpin, and it is missing for the reason
// `recovery-lock password` is (apple_mdm_devices.go): it locks the machine
// AND returns the PIN that unlocks it. A destructive tool can be gated —
// plan-by-default, step-up auth, a signed manifest — but none of that
// un-writes a credential from a transcript, so it stays CLI-only and
// no_secret_tools_test.go enforces the absence.
//
// Every tool here reads the device BEFORE it does anything, which is what
// makes the plan worth reading: it carries the serial number, so an agent
// that resolved the wrong id shows a plan naming a machine the operator does
// not recognise, rather than a 24-character string they cannot check.
//
// None of these tools reports that an action happened. See
// apple_mdm.AcceptedIsNotCompleted — the API answers 204, background_tasks
// stays empty, and nothing in this surface says what became of the command.
// The result says "accepted".

type appleMDMDeviceActionInput struct {
	AppleMDMID string `json:"apple_mdm_id" jsonschema:"the Apple MDM configuration's id, from apple_mdm_list"`
	DeviceID   string `json:"device_id" jsonschema:"the managed device's id, from apple_mdm_devices — a DEVICE id, not an Apple MDM id"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to send the command. Without this the tool reads the device and returns a plan, and nothing is sent."`
}

type appleMDMDevicePINActionInput struct {
	AppleMDMID string `json:"apple_mdm_id" jsonschema:"the Apple MDM configuration's id, from apple_mdm_list"`
	DeviceID   string `json:"device_id" jsonschema:"the managed device's id, from apple_mdm_devices"`
	PIN        string `json:"pin,omitempty" jsonschema:"Six digits. The API documents this as REQUIRED FOR MACOS while declaring it optional, and on a Mac it is the only way past the resulting lock screen. Omitted means omitted — jc does not invent one."`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to send the command. Without this the tool returns a plan and nothing is sent."`
}

type appleMDMDeviceRestartInput struct {
	AppleMDMID string   `json:"apple_mdm_id" jsonschema:"the Apple MDM configuration's id, from apple_mdm_list"`
	DeviceID   string   `json:"device_id" jsonschema:"the managed device's id, from apple_mdm_devices"`
	KextPaths  []string `json:"kext_paths,omitempty" jsonschema:"Kext paths whose kernel cache should be rebuilt during the restart."`
	Execute    bool     `json:"execute,omitempty" jsonschema:"Set to true to send the command. Without this the tool returns a plan and nothing is sent."`
}

type appleMDMScheduleOSUpdateInput struct {
	AppleMDMID       string `json:"apple_mdm_id" jsonschema:"the Apple MDM configuration's id, from apple_mdm_list"`
	DeviceID         string `json:"device_id" jsonschema:"the managed device's id, from apple_mdm_devices"`
	InstallAction    string `json:"install_action" jsonschema:"One of DOWNLOAD_ONLY, INSTALL_LATER, INSTALL_ASAP, INSTALL_FORCE_RESTART. INSTALL_FORCE_RESTART restarts the device without asking the person using it."`
	ProductKey       string `json:"product_key" jsonschema:"Apple software update product key. REQUIRED, and nothing in the JumpCloud API will tell you a valid value: there is no available-updates read in the Apple MDM surface, and the API accepts any string without validating it. A wrong key is accepted and then does nothing. Do not guess one."`
	MaxUserDeferrals *int   `json:"max_user_deferrals,omitempty" jsonschema:"How many times the user may defer. Omit to leave unset; 0 means zero deferrals."`
	Execute          bool   `json:"execute,omitempty" jsonschema:"Set to true to schedule the update. Without this the tool returns a plan and nothing is sent."`
}

type appleMDMLostModeInput struct {
	AppleMDMID  string `json:"apple_mdm_id" jsonschema:"the Apple MDM configuration's id, from apple_mdm_list"`
	DeviceID    string `json:"device_id" jsonschema:"the managed device's id, from apple_mdm_devices"`
	Enable      bool   `json:"enable" jsonschema:"true puts the device into lost mode, false takes it out. Exactly one of the two is sent — the API accepts a body asking for both and silently discards the enable."`
	Message     string `json:"message,omitempty" jsonschema:"Message to show on the lost device. Enabling requires a message or a phone number; the API rejects enabling with neither."`
	PhoneNumber string `json:"phone_number,omitempty" jsonschema:"Phone number to show on the lost device. Enabling requires a message or a phone number."`
	Footnote    string `json:"footnote,omitempty" jsonschema:"Footnote to show. Accepted by the API — but so is any misspelled field, so acceptance is not evidence it is displayed."`
	Execute     bool   `json:"execute,omitempty" jsonschema:"Set to true to send the command. Without this the tool returns a plan and nothing is sent."`
}

type appleMDMRefreshDEPInput struct {
	AppleMDMID string `json:"apple_mdm_id" jsonschema:"the Apple MDM configuration's id, from apple_mdm_list"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to trigger the re-sync. Without this the tool returns a plan."`
}

// deviceActionResult is what every tool here returns. Accepted, never done.
type deviceActionResult struct {
	Action   string `json:"action"`
	MDMID    string `json:"apple_mdm_id"`
	DeviceID string `json:"device_id,omitempty"`
	Serial   string `json:"serial_number,omitempty"`
	OS       string `json:"os,omitempty"`
	Executed bool   `json:"executed"`
	Summary  string `json:"summary"`
	// Precondition is what the device record says about this action, when it
	// says anything. Present on both the plan and the execute path.
	Precondition string `json:"precondition,omitempty"`
}

// requireActionIDs validates both ids before either reaches a request path.
// Called from the preFlight closure as well as the handler: preFlight runs
// BEFORE the step-up gate, so a malformed id does not cost a Touch ID
// approval, and the handler check is what the id-guard test exercises.
func requireActionIDs(mdmID, deviceID string) error {
	if err := apple_mdm.RequireObjectID("an Apple MDM configuration", mdmID); err != nil {
		return err
	}
	return apple_mdm.RequireObjectID("a managed device", deviceID)
}

// runDeviceAction is the single path for every per-device action tool.
func (s *Server) runDeviceAction(ctx context.Context, tool, mdmID, deviceID, segment, label string,
	body any, execute bool, effects []string) (*mcp.CallToolResult, any, error) {

	if err := requireActionIDs(mdmID, deviceID); err != nil {
		return errorResult(err.Error()), nil, nil
	}
	if s.readOnly && execute {
		return errorResult(fmt.Sprintf("server is in read-only mode; %s with execute=true is not allowed", tool)), nil, nil
	}

	// The device read comes first for the same reason it does in the CLI: a
	// well-formed id belonging to no device must fail before anything is
	// sent, and the record is what lets the plan name the machine.
	raw, err := adeGet(ctx, apple_mdm.DeviceEndpoint(mdmID, deviceID))
	if err != nil {
		return errorResult(fmt.Sprintf("%s: reading device %s before acting on it: %v", tool, deviceID, err)), nil, nil
	}
	pre, err := apple_mdm.ParsePreconditions(raw)
	if err != nil {
		return errorResult(fmt.Sprintf("%s: %v", tool, err)), nil, nil
	}

	result := deviceActionResult{
		Action:       label,
		MDMID:        mdmID,
		DeviceID:     deviceID,
		Serial:       pre.SerialNumber,
		OS:           strings.TrimSpace(pre.OSFamily + " " + pre.OSVersion),
		Precondition: pre.PreconditionNote(segment),
	}

	if !execute {
		plan := map[string]any{
			"plan":          true,
			"action":        label,
			"apple_mdm_id":  mdmID,
			"device_id":     deviceID,
			"serial_number": pre.SerialNumber,
			"os":            result.OS,
			"effects": append(append([]string{}, effects...),
				"The API answers 204 and nothing in it reports whether the command completed — "+
					"it is queued for delivery at the device's next check-in, so an offline device "+
					"is not a safe target"),
			"message": fmt.Sprintf("This would %s device %s (serial %s). Pass execute=true to proceed.",
				label, deviceID, pre.SerialNumber),
		}
		if result.Precondition != "" {
			plan["precondition"] = result.Precondition
		}
		return jsonResultWith(plan)
	}

	client, err := newV2ClientFunc()
	if err != nil {
		return errorResult(fmt.Sprintf("%s: building v2 client: %v", tool, err)), nil, nil
	}
	if body == nil {
		// Never nil: see apple_mdm.NilBodyIsSentAsNull.
		body = apple_mdm.EmptyActionBody()
	}
	if _, err := client.Create(ctx, apple_mdm.DeviceActionEndpoint(mdmID, deviceID, segment), body); err != nil {
		return errorResult(fmt.Sprintf("%s: %v", tool, err)), nil, nil
	}

	result.Executed = true
	result.Summary = apple_mdm.Outcome{
		Action: label, DeviceID: deviceID, MDMID: mdmID, Accepted: true,
	}.Describe()
	res, err := jsonResult(result)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return res, nil, nil
}

// simpleActionTool registers a tool for an action that takes no body.
func simpleActionTool(s *Server, name, segment, label, description string, effects []string) {
	addTypedToolWithPreFlight(s, name, description,
		func(args appleMDMDeviceActionInput) error {
			if err := requireActionIDs(args.AppleMDMID, args.DeviceID); err != nil {
				return err
			}
			if s.readOnly && args.Execute {
				return fmt.Errorf("server is in read-only mode; %s with execute=true is not allowed", name)
			}
			return nil
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args appleMDMDeviceActionInput) (*mcp.CallToolResult, any, error) {
			return s.runDeviceAction(ctx, name, args.AppleMDMID, args.DeviceID, segment, label, nil, args.Execute, effects)
		},
	)
}

func (s *Server) registerAppleMDMDeviceActionTools() {
	// ---- the three that change nothing on the device -------------------
	simpleActionTool(s, "apple_mdm_device_os_update_status",
		apple_mdm.ActionOSUpdateStatus, "ask for OS update status",
		"Ask a managed Apple device to report the status of its OS update. This queues a QUERY, not a change. The answer does NOT come back here: the API returns 204 with no body and nothing in this surface reports the result — apple_mdm_background_tasks_list stays empty even after a verified queue. Set execute=true to send the query.",
		[]string{"Asks the device to report OS update status; the answer is not returned by this API"})

	simpleActionTool(s, "apple_mdm_device_refresh_activation_lock",
		apple_mdm.ActionRefreshActivationLock, "refresh activation lock information",
		"Refresh what JumpCloud knows about a managed device's activation lock. This changes nothing on the device — it updates JumpCloud's own view. Read apple_mdm_device_get afterwards to see the refreshed hasActivationLockBypassCodes and securityInfo.isActivationLockManageable. Set execute=true to send it.",
		[]string{"Refreshes JumpCloud's activation-lock information for the device"})

	// ---- the clears ---------------------------------------------------
	simpleActionTool(s, "apple_mdm_device_clear_passcode",
		apple_mdm.ActionClearPasscode, "clear the passcode of",
		"Remove the passcode from a managed Apple device, leaving the device with no passcode. This REDUCES the device's security posture; it is not a neutral act. Set execute=true to send the command.",
		[]string{"Removes the device passcode, leaving the device unprotected by one"})

	simpleActionTool(s, "apple_mdm_device_clear_restrictions_password",
		apple_mdm.ActionClearRestrictionsPasswd, "clear the restrictions password of",
		"Remove the restrictions (Screen Time) password from a managed Apple device. Set execute=true to send the command.",
		[]string{"Removes the restrictions password"})

	simpleActionTool(s, "apple_mdm_device_clear_activation_lock",
		apple_mdm.ActionClearActivationLock, "clear the activation lock of",
		"Clear the activation lock on a managed Apple device so it can be set up by someone other than the Apple ID that locked it. Whether this can work is visible beforehand in the device record: securityInfo.isActivationLockManageable and hasActivationLockBypassCodes, both reported in this tool's plan. The API may still answer 409. Set execute=true to send the command.",
		[]string{"Clears the activation lock, releasing the device from its Apple ID"})

	// ---- stop / start -------------------------------------------------
	simpleActionTool(s, "apple_mdm_device_shutdown",
		apple_mdm.ActionShutdown, "shut down",
		"Shut down a managed Apple device. A powered-off device receives no further MDM commands until someone physically turns it on, so this is the one action in this family that removes your own ability to undo it remotely. Set execute=true to send the command.",
		[]string{"Shuts the device down",
			"A powered-off device receives no further MDM commands until someone turns it on"})

	addTypedToolWithPreFlight(s, "apple_mdm_device_restart",
		"Restart a managed Apple device, interrupting whoever is using it. The command is queued and delivered at the device's next check-in, so a machine that is offline right now restarts when it comes back rather than not at all. Set execute=true to send the command.",
		func(args appleMDMDeviceRestartInput) error {
			if err := requireActionIDs(args.AppleMDMID, args.DeviceID); err != nil {
				return err
			}
			if s.readOnly && args.Execute {
				return fmt.Errorf("server is in read-only mode; apple_mdm_device_restart with execute=true is not allowed")
			}
			return nil
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args appleMDMDeviceRestartInput) (*mcp.CallToolResult, any, error) {
			return s.runDeviceAction(ctx, "apple_mdm_device_restart", args.AppleMDMID, args.DeviceID,
				apple_mdm.ActionRestart, "restart",
				apple_mdm.RestartBody{KextPaths: args.KextPaths}, args.Execute,
				[]string{"Restarts the device, interrupting whoever is using it"})
		},
	)

	// ---- lock and erase ------------------------------------------------
	addTypedToolWithPreFlight(s, "apple_mdm_device_lock",
		"Lock a managed Apple device with the MDM lock command. On a Mac this is a firmware-level lock: the machine stops at a PIN prompt and the six-digit PIN is the only way past it, so a lock sent without a PIN may leave a machine nobody can unlock. The API documents the PIN as required for macOS while declaring it optional. Set execute=true to send the command.",
		func(args appleMDMDevicePINActionInput) error {
			if err := requireActionIDs(args.AppleMDMID, args.DeviceID); err != nil {
				return err
			}
			if s.readOnly && args.Execute {
				return fmt.Errorf("server is in read-only mode; apple_mdm_device_lock with execute=true is not allowed")
			}
			return apple_mdm.ValidatePIN(args.PIN)
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args appleMDMDevicePINActionInput) (*mcp.CallToolResult, any, error) {
			if err := apple_mdm.ValidatePIN(args.PIN); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return s.runDeviceAction(ctx, "apple_mdm_device_lock", args.AppleMDMID, args.DeviceID,
				apple_mdm.ActionLock, "lock",
				apple_mdm.PINBody{PIN: args.PIN}, args.Execute,
				[]string{"Locks the device; on a Mac the PIN is the only way past the lock screen"})
		},
	)

	addTypedToolWithPreFlight(s, "apple_mdm_device_erase",
		"Erase (wipe) a managed Apple device. THIS IS EXTREMELY DESTRUCTIVE AND IRREVERSIBLE — every byte of user data goes. The command is queued and delivered at the device's next check-in, so a device that is powered off is not a safe target: it is erased when it comes back. The API documents the six-digit PIN as required for macOS. Set execute=true to erase; otherwise this returns a plan naming the machine by serial number.",
		func(args appleMDMDevicePINActionInput) error {
			if err := requireActionIDs(args.AppleMDMID, args.DeviceID); err != nil {
				return err
			}
			if s.readOnly && args.Execute {
				return fmt.Errorf("server is in read-only mode; apple_mdm_device_erase with execute=true is not allowed")
			}
			return apple_mdm.ValidatePIN(args.PIN)
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args appleMDMDevicePINActionInput) (*mcp.CallToolResult, any, error) {
			if err := apple_mdm.ValidatePIN(args.PIN); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return s.runDeviceAction(ctx, "apple_mdm_device_erase", args.AppleMDMID, args.DeviceID,
				apple_mdm.ActionErase, "erase",
				apple_mdm.PINBody{PIN: args.PIN}, args.Execute,
				[]string{"Wipes the device. Irreversible."})
		},
	)

	// ---- unenroll ------------------------------------------------------
	addTypedToolWithPreFlight(s, "apple_mdm_device_unenroll",
		"Remove a device's enrollment from an Apple MDM configuration. The device stops being managed and MDM-delivered profiles and policies stop being enforced; re-enrolling is not something this can undo for you. Unlike the rest of this family the route answers 200 with the removed device record. Set execute=true to unenroll.",
		func(args appleMDMDeviceActionInput) error {
			if err := requireActionIDs(args.AppleMDMID, args.DeviceID); err != nil {
				return err
			}
			if s.readOnly && args.Execute {
				return fmt.Errorf("server is in read-only mode; apple_mdm_device_unenroll with execute=true is not allowed")
			}
			return nil
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args appleMDMDeviceActionInput) (*mcp.CallToolResult, any, error) {
			if err := requireActionIDs(args.AppleMDMID, args.DeviceID); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if s.readOnly && args.Execute {
				return errorResult("server is in read-only mode; apple_mdm_device_unenroll with execute=true is not allowed"), nil, nil
			}
			raw, err := adeGet(ctx, apple_mdm.DeviceEndpoint(args.AppleMDMID, args.DeviceID))
			if err != nil {
				return errorResult(fmt.Sprintf("apple_mdm_device_unenroll: reading device %s before acting on it: %v",
					args.DeviceID, err)), nil, nil
			}
			pre, err := apple_mdm.ParsePreconditions(raw)
			if err != nil {
				return errorResult(fmt.Sprintf("apple_mdm_device_unenroll: %v", err)), nil, nil
			}
			if !args.Execute {
				return jsonResultWith(map[string]any{
					"plan":          true,
					"action":        "unenroll",
					"apple_mdm_id":  args.AppleMDMID,
					"device_id":     args.DeviceID,
					"serial_number": pre.SerialNumber,
					"os":            strings.TrimSpace(pre.OSFamily + " " + pre.OSVersion),
					"effects": []string{
						"Removes the device's Apple MDM enrollment",
						"MDM-delivered profiles and policies stop being enforced",
					},
					"message": fmt.Sprintf("This would unenroll device %s (serial %s) from Apple MDM. "+
						"Pass execute=true to proceed.", args.DeviceID, pre.SerialNumber),
				})
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("apple_mdm_device_unenroll: building v2 client: %v", err)), nil, nil
			}
			removed, err := client.Delete(ctx, apple_mdm.DeviceEndpoint(args.AppleMDMID, args.DeviceID))
			if err != nil {
				return errorResult(fmt.Sprintf("apple_mdm_device_unenroll: %v", err)), nil, nil
			}
			return jsonResultWith(map[string]any{
				"action":        "unenroll",
				"apple_mdm_id":  args.AppleMDMID,
				"device_id":     args.DeviceID,
				"serial_number": pre.SerialNumber,
				"executed":      true,
				"removed":       string(removed),
				"summary": apple_mdm.Outcome{
					Action: "unenroll", DeviceID: args.DeviceID, MDMID: args.AppleMDMID, Accepted: true,
				}.Describe(),
			})
		},
	)

	// ---- scheduled OS update -------------------------------------------
	addTypedToolWithPreFlight(s, "apple_mdm_device_schedule_os_update",
		"Schedule an OS update on a managed Apple device. product_key is required and NOTHING IN THE JUMPCLOUD API WILL TELL YOU A VALID VALUE — there is no available-updates read in the Apple MDM surface, and the API accepts any string without validating it, so a wrong key is accepted and then silently does nothing. Do not invent one; get it from Apple's software update catalog or the device itself. install_action is validated against the API's enum before the request leaves. INSTALL_FORCE_RESTART restarts the device without asking the person using it. Set execute=true to schedule.",
		func(args appleMDMScheduleOSUpdateInput) error {
			if err := requireActionIDs(args.AppleMDMID, args.DeviceID); err != nil {
				return err
			}
			if s.readOnly && args.Execute {
				return fmt.Errorf("server is in read-only mode; apple_mdm_device_schedule_os_update with execute=true is not allowed")
			}
			_, err := apple_mdm.NewOSUpdateBody(args.InstallAction, args.ProductKey, args.MaxUserDeferrals)
			return err
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args appleMDMScheduleOSUpdateInput) (*mcp.CallToolResult, any, error) {
			body, err := apple_mdm.NewOSUpdateBody(args.InstallAction, args.ProductKey, args.MaxUserDeferrals)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return s.runDeviceAction(ctx, "apple_mdm_device_schedule_os_update", args.AppleMDMID, args.DeviceID,
				apple_mdm.ActionScheduleOSUpdate, "schedule an OS update on", body, args.Execute,
				[]string{
					"Schedules an OS update with install action " + args.InstallAction,
					"The product key is not validated by the API; a wrong one is accepted and does nothing",
				})
		},
	)

	// ---- lost mode ------------------------------------------------------
	addTypedToolWithPreFlight(s, "apple_mdm_device_lost_mode",
		"Put a managed Apple device into lost mode, or take it out (enable=false). Lost mode locks the device and displays a message and phone number so a finder can return it. Enabling requires a message or a phone number — that rule is the API's and the spec does not mention it. Exactly one of enable/disable is ever sent: the API accepts a body asking for both, silently discards the enable, and does not say which command it ran. Set execute=true to send the command.",
		func(args appleMDMLostModeInput) error {
			if err := requireActionIDs(args.AppleMDMID, args.DeviceID); err != nil {
				return err
			}
			if s.readOnly && args.Execute {
				return fmt.Errorf("server is in read-only mode; apple_mdm_device_lost_mode with execute=true is not allowed")
			}
			_, err := lostModeBodyFor(args)
			return err
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args appleMDMLostModeInput) (*mcp.CallToolResult, any, error) {
			body, err := lostModeBodyFor(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			label, effect := "take out of lost mode", "Takes the device out of lost mode"
			if args.Enable {
				label = "put into lost mode"
				effect = "Locks the device and displays the message or phone number on it"
			}
			return s.runDeviceAction(ctx, "apple_mdm_device_lost_mode", args.AppleMDMID, args.DeviceID,
				apple_mdm.ActionLostMode, label, body, args.Execute, []string{effect})
		},
	)

	// ---- the one org-level action ---------------------------------------
	addTypedToolWithPreFlight(s, "apple_mdm_refresh_dep_devices",
		"Re-sync the DEP (Automated Device Enrollment) device list for an Apple MDM configuration. This is the only action in this area that is not per-device, and it changes nothing on any device. Set execute=true to trigger it.",
		func(args appleMDMRefreshDEPInput) error {
			if err := apple_mdm.RequireObjectID("an Apple MDM configuration", args.AppleMDMID); err != nil {
				return err
			}
			if s.readOnly && args.Execute {
				return fmt.Errorf("server is in read-only mode; apple_mdm_refresh_dep_devices with execute=true is not allowed")
			}
			return nil
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args appleMDMRefreshDEPInput) (*mcp.CallToolResult, any, error) {
			if err := apple_mdm.RequireObjectID("an Apple MDM configuration", args.AppleMDMID); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if s.readOnly && args.Execute {
				return errorResult("server is in read-only mode; apple_mdm_refresh_dep_devices with execute=true is not allowed"), nil, nil
			}
			if !args.Execute {
				return jsonResultWith(map[string]any{
					"plan":         true,
					"action":       "refresh-dep-devices",
					"apple_mdm_id": args.AppleMDMID,
					"effects":      []string{"Re-syncs the DEP device list for this configuration"},
					"message": fmt.Sprintf("This would re-sync the DEP device list for Apple MDM configuration %s. "+
						"Pass execute=true to proceed.", args.AppleMDMID),
				})
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("apple_mdm_refresh_dep_devices: building v2 client: %v", err)), nil, nil
			}
			if _, err := client.Create(ctx, apple_mdm.RefreshDEPDevicesEndpoint(args.AppleMDMID),
				apple_mdm.EmptyActionBody()); err != nil {
				return errorResult(fmt.Sprintf("apple_mdm_refresh_dep_devices: %v", err)), nil, nil
			}
			return jsonResultWith(map[string]any{
				"action":       "refresh-dep-devices",
				"apple_mdm_id": args.AppleMDMID,
				"executed":     true,
				"summary": apple_mdm.Outcome{
					Action: "refresh-dep-devices", MDMID: args.AppleMDMID, Accepted: true,
				}.Describe(),
			})
		},
	)
}

// lostModeBodyFor builds the oneof from the tool's boolean, which is the
// whole reason the tool takes a boolean rather than two optional objects: a
// bool cannot express "both".
func lostModeBodyFor(args appleMDMLostModeInput) (apple_mdm.LostModeBody, error) {
	if !args.Enable {
		body := apple_mdm.DisableLostMode()
		return body, body.Validate()
	}
	body, err := apple_mdm.EnableLostMode(args.Message, args.PhoneNumber, args.Footnote)
	if err != nil {
		return apple_mdm.LostModeBody{}, err
	}
	return body, body.Validate()
}
