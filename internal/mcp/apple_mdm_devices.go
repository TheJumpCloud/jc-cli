package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/apple_mdm"
)

// The Apple MDM device reads. FOUR tools for SIX CLI commands, and the gap is
// the point.
//
// `jc apple-mdm recovery-lock password` and `jc apple-mdm device-lock pin`
// have no tool here and will not get one. They return the credentials that
// unlock a Mac: the recovery-lock password is what stands between a stolen
// machine and a reinstall, and the lock PIN is what releases a locked device.
// Both are GETs, so both classify as read-only and would pass every gate this
// server has — the step-up prompt, plan-by-default, the destructive
// classification are all about undoing a change, and nothing undoes a value
// already written into a transcript. internal/mcp/no_secret_tools_test.go
// enforces the absence rather than leaving it to whoever edits this file next.
//
// The four that are here answer the questions an agent should be able to ask:
// what is this device, is it locked, does it have a recovery lock configured,
// and what is the push-certificate request.

type mdmDeviceInput struct {
	AppleMDMID string `json:"apple_mdm_id" jsonschema:"the Apple MDM configuration's id, from apple_mdm_list"`
	DeviceID   string `json:"device_id" jsonschema:"the managed device's id, from the Apple MDM configuration's device list — a DEVICE id, not an Apple MDM id"`
}

func (s *Server) registerAppleMDMDeviceTools() {
	addTypedTool(s, "apple_mdm_device_get",
		"Get one Apple MDM managed device by id — model, OS build, capacity, enrollment state, and securityInfo covering recovery lock, FileVault and passcode compliance. Note the record reports mdmOsFamily as 'macOS' while the ADE settings tools call the same platform 'DARWIN'; both are reported as the API gives them. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args mdmDeviceInput) (*mcp.CallToolResult, any, error) {
			raw, err := mdmDeviceGet(ctx, args, apple_mdm.DeviceEndpoint)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			device, err := apple_mdm.ParseDevice(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return rawSingleResult(device)
		},
	)

	addTypedTool(s, "apple_mdm_device_lock_state",
		"Is this managed device locked by MDM? Returns the device-lock state, the message shown on the lock screen, and why a previous lock attempt failed if one did. This is the MDM lock (lost mode and the lock command), not the recovery lock that guards recoveryOS on a Mac. Fields carrying a value ending in '_unspecified' mean there is nothing to report, not a state called unspecified. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args mdmDeviceInput) (*mcp.CallToolResult, any, error) {
			raw, err := mdmDeviceGet(ctx, args, apple_mdm.LockStateEndpoint)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			state, err := apple_mdm.ParseLockState(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return jsonResultWith(map[string]any{
				"locked":        state.Locked(),
				"summary":       state.Describe(),
				"state":         state.State,
				"failureReason": state.FailureReason,
				"messageResult": state.MessageResult,
				"lockMessage":   state.LockMessage,
				"phoneNumber":   state.PhoneNumber,
				"statusUpdatedAt": func() any {
					if state.StatusUpdated == nil {
						return nil
					}
					return *state.StatusUpdated
				}(),
			})
		},
	)

	addTypedTool(s, "apple_mdm_recovery_lock_config",
		"Does this Mac have a recovery lock, and when does its password rotate? Returns the recovery-lock configuration: whether it is enabled, when it was set, when the password last rotated and when it is next due. IMPORTANT: three fields here disagree on live devices — a device has been seen reporting isLockEnabled=true, status=SET_PENDING and uiStatus=OFF at once while still holding a readable password — so none of them, nor recoveryLockEnabled in the device listing, reliably says whether a password exists. The summary field says when they conflict. nextRotationAt is advisory and has been observed to change between reads. The recovery-lock password itself is not available through this server at all — that read is CLI-only. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args mdmDeviceInput) (*mcp.CallToolResult, any, error) {
			raw, err := mdmDeviceGet(ctx, args, apple_mdm.RecoveryLockConfigEndpoint)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			cfg, err := apple_mdm.ParseRecoveryLockConfig(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return jsonResultWith(map[string]any{
				"summary":                 cfg.Describe(),
				"isLockEnabled":           cfg.IsLockEnabled,
				"status":                  cfg.Status,
				"uiStatus":                cfg.UIStatus,
				"isPasswordAutoGenerated": cfg.IsPasswordAutoGenerated,
				"rotationWindowDays":      cfg.RotationWindowDays,
				"initialSetAt":            derefOrNil(cfg.InitialSetAt),
				"passwordCreatedAt":       derefOrNil(cfg.PasswordCreatedAt),
				"lastRotationAt":          derefOrNil(cfg.LastRotationAt),
				"nextRotationAt":          derefOrNil(cfg.NextRotationAt),
				"statusReportedAt":        derefOrNil(cfg.StatusReportedAt),
			})
		},
	)

	addTypedTool(s, "apple_mdm_csr_get",
		"Read the certificate signing request for this Apple MDM configuration's push certificate — the document uploaded to Apple to renew MDM push. The API answers with a bare base64 string rather than JSON; it decodes to an Apple plist carrying the certificate chain, the request and its signature. Returns the decoded plist and its length. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args appleMDMIDInput) (*mcp.CallToolResult, any, error) {
			if err := apple_mdm.RequireObjectID("an Apple MDM configuration", args.AppleMDMID); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body, err := adeGet(ctx, apple_mdm.CSREndpoint(args.AppleMDMID))
			if err != nil {
				return errorResult(fmt.Sprintf("reading the push-certificate request: %v", err)), nil, nil
			}
			plist, err := apple_mdm.ParseCSR(body)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return jsonResultWith(map[string]any{
				"plist":  string(plist),
				"bytes":  len(plist),
				"base64": base64.StdEncoding.EncodeToString(plist),
			})
		},
	)
}

// mdmDeviceGet validates both ids, performs the read, and turns the ambiguous
// 404 into a true statement.
//
// See apple_mdm.AbsenceAndTypoBothAnswer404: these routes answer 404 both for
// a device with nothing set and for a device id belonging to nothing. An agent
// told "this device has no recovery lock" when the id was wrong would carry
// that into whatever it does next, so the device is read before the 404 is
// interpreted — the same two-step the CLI does.
func mdmDeviceGet(ctx context.Context, args mdmDeviceInput, endpoint func(string, string) string) (json.RawMessage, error) {
	if err := apple_mdm.RequireObjectID("an Apple MDM configuration", args.AppleMDMID); err != nil {
		return nil, err
	}
	if err := apple_mdm.RequireObjectID("a managed device", args.DeviceID); err != nil {
		return nil, err
	}

	raw, err := adeGet(ctx, endpoint(args.AppleMDMID, args.DeviceID))
	if err == nil {
		return raw, nil
	}

	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		return nil, err
	}

	if _, probeErr := adeGet(ctx, apple_mdm.DeviceEndpoint(args.AppleMDMID, args.DeviceID)); probeErr != nil {
		var probeAPIErr *api.APIError
		if errors.As(probeErr, &probeAPIErr) && probeAPIErr.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("no device %s is managed by Apple MDM configuration %s",
				args.DeviceID, args.AppleMDMID)
		}
		return nil, fmt.Errorf("the read returned 404 for device %s, and checking whether that "+
			"device exists also failed: %w", args.DeviceID, probeErr)
	}

	return nil, fmt.Errorf("device %s exists but has nothing set for this read "+
		"(the API answers 404 for both absence and a bad id, so the device was read to tell "+
		"them apart)", args.DeviceID)
}

func derefOrNil(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
