package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/devsettings"
)

type devSettingsSignInSetInput struct {
	OS                string `json:"os" jsonschema:"OS family to change: windows or macos"`
	Enabled           *bool  `json:"enabled,omitempty" jsonschema:"Whether Sign In with JumpCloud is enabled for this OS family"`
	DefaultPermission string `json:"default_permission,omitempty" jsonschema:"Default permission granted on sign-in: standard or admin"`
	Execute           bool   `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type devSettingsPasswordSyncSetInput struct {
	Enabled bool `json:"enabled" jsonschema:"Whether devices sync passwords by default"`
	Execute bool `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

func (s *Server) registerDeviceSettingsTools() {
	addTypedTool(s, "devices_settings_get", "Get every organization-wide JumpCloud device setting in one call: the Sign In with JumpCloud settings (one entry per OS family, with enabled and defaultPermission) and the default password sync boolean. These are org-level defaults, not per-device configuration.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			rawSignIn, err := client.Get(ctx, devsettings.SignInEndpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("getting sign-in settings: %v", err)), nil, nil
			}
			signIn, err := devsettings.ParseSignIn(rawSignIn)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			rawSync, err := client.Get(ctx, devsettings.PasswordSyncEndpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("getting password sync setting: %v", err)), nil, nil
			}
			var sync devsettings.PasswordSync
			if err := json.Unmarshal(rawSync, &sync); err != nil {
				return errorResult(fmt.Sprintf("decoding password sync setting: %v", err)), nil, nil
			}
			res, err := jsonResult(map[string]any{
				"organizationObjectId": signIn.OrganizationObjectID,
				"signInWithJumpCloud":  signIn.Settings,
				"defaultPasswordSync":  sync.Enabled,
			})
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "devices_settings_signin_set", "Change the organization's Sign In with JumpCloud setting for ONE OS family (windows or macos). The current settings are fetched first and the complete array is sent back with only the named OS family changed, so the other OS family is never disturbed. This affects every device in the organization. Set execute=true to apply; otherwise returns a plan showing the before/after.",
		func(ctx context.Context, req *mcp.CallToolRequest, args devSettingsSignInSetInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			osFamily, err := devsettings.NormalizeOSFamily(args.OS)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			var permission *string
			if args.DefaultPermission != "" {
				p, err := devsettings.NormalizePermission(args.DefaultPermission)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				permission = &p
			}
			if args.Enabled == nil && permission == nil {
				return errorResult("no changes requested: specify enabled and/or default_permission"), nil, nil
			}

			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Get(ctx, devsettings.SignInEndpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("getting sign-in settings: %v", err)), nil, nil
			}
			current, err := devsettings.ParseSignIn(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}

			merged := devsettings.MergeSignInSetting(current.Settings, osFamily, args.Enabled, permission)
			after, _ := devsettings.FindSignIn(merged, osFamily)

			if !args.Execute {
				effects := map[string]any{
					"to":    after.Describe(),
					"scope": "every device in the organization",
				}
				if before, ok := devsettings.FindSignIn(current.Settings, osFamily); ok {
					effects["from"] = before.Describe()
				}
				return planResult("update", "Sign In with JumpCloud setting", osFamily, "", effects)
			}

			if _, err := client.Update(ctx, devsettings.SignInEndpoint, devsettings.SignInBody(merged)); err != nil {
				return errorResult(fmt.Sprintf("updating sign-in settings: %v", err)), nil, nil
			}
			// The PUT returns no body, so re-read to report the new state.
			raw, err = client.Get(ctx, devsettings.SignInEndpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("re-reading sign-in settings: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "devices_settings_password_sync_set", "Change the organization's default password sync setting (whether devices sync passwords by default). This applies to the whole organization. Set execute=true to apply; otherwise returns a plan showing the before/after.",
		func(ctx context.Context, req *mcp.CallToolRequest, args devSettingsPasswordSyncSetInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Get(ctx, devsettings.PasswordSyncEndpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("getting password sync setting: %v", err)), nil, nil
			}
			var current devsettings.PasswordSync
			if err := json.Unmarshal(raw, &current); err != nil {
				return errorResult(fmt.Sprintf("decoding password sync setting: %v", err)), nil, nil
			}

			if !args.Execute {
				return planResult("update", "default password sync setting", "organization", "", map[string]any{
					"from":  current.Enabled,
					"to":    args.Enabled,
					"scope": "every device in the organization",
				})
			}
			if current.Enabled == args.Enabled {
				return textResult(fmt.Sprintf("Default password sync is already %t; no change made.", args.Enabled)), nil, nil
			}
			if _, err := client.Update(ctx, devsettings.PasswordSyncEndpoint, devsettings.PasswordSyncBody(args.Enabled)); err != nil {
				return errorResult(fmt.Sprintf("updating password sync setting: %v", err)), nil, nil
			}
			raw, err = client.Get(ctx, devsettings.PasswordSyncEndpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("re-reading password sync setting: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)
}
