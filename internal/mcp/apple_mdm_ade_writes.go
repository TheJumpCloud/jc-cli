package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/apple_mdm"
)

// The ADE/ABM/ADUE writes: four tools for the CLI's four commands. All plan by
// default and act only with execute=true. None carries a secret.
//
// The ADE token create and renew writes are not here, and not in the CLI
// either: they upload an Apple Business Manager server token whose encoding
// has never been verified (apple_mdm/ade_writes.go). When they land they
// belong with the other credential-bearing operations — CLI only.

type adeSettingsUpdateInput struct {
	TokenID            string   `json:"token_id" jsonschema:"the ADE token's objectId, from apple_mdm_ade_tokens_list"`
	OSFamily           string   `json:"os_family" jsonschema:"DARWIN (macOS) or IOS; macos and ios are accepted"`
	ZeroTouch          *bool    `json:"zero_touch,omitempty" jsonschema:"true makes enrollment mandatory, false skippable. Omit to leave unchanged."`
	WelcomeTitle       *string  `json:"welcome_title,omitempty" jsonschema:"Welcome screen title, max 255. Cannot be blank — use reset_welcome to restore the stock text."`
	WelcomeParagraph   *string  `json:"welcome_paragraph,omitempty" jsonschema:"Welcome screen text, max 255."`
	WelcomeButton      *string  `json:"welcome_button,omitempty" jsonschema:"Welcome screen button label, max 255."`
	ResetWelcome       []string `json:"reset_welcome,omitempty" jsonschema:"Welcome strings to restore to JumpCloud's stock text: any of title, paragraph, button."`
	SetupOptions       []string `json:"setup_options,omitempty" jsonschema:"Setup-assistant options. REPLACES the current list. UNKNOWN is refused (the API answers 500)."`
	ClearSetupOptions  bool     `json:"clear_setup_options,omitempty" jsonschema:"Empty the setup-assistant options."`
	DefaultGroupIDs    []string `json:"default_group_ids,omitempty" jsonschema:"Device group ids enrolled devices join. REPLACES the current list. Each is checked to exist first, because the API stores ids that belong to nothing."`
	ClearDefaultGroups bool     `json:"clear_default_groups,omitempty" jsonschema:"Remove every default device group."`
	Execute            bool     `json:"execute,omitempty" jsonschema:"Set to true to write. Without this the tool reads the current setting and returns a before/after plan."`
}

type adeTokenActionInput struct {
	TokenID string `json:"token_id" jsonschema:"the ADE token's objectId, from apple_mdm_ade_tokens_list"`
	Execute bool   `json:"execute,omitempty" jsonschema:"Set to true to trigger the re-sync. Without this the tool returns a plan."`
}

type adueWriteInput struct {
	AppleMDMID string `json:"apple_mdm_id" jsonschema:"the Apple MDM configuration's id, from apple_mdm_list"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to make the change. Without this the tool returns a plan."`
}

// adeSettingsChangeFrom turns the tool's arguments into the contract's change,
// and validates it offline. Called from preFlight and from the handler.
func adeSettingsChangeFrom(args adeSettingsUpdateInput) (apple_mdm.ADESettingsChange, string, error) {
	if err := apple_mdm.RequireObjectID("an ADE token", args.TokenID); err != nil {
		return apple_mdm.ADESettingsChange{}, "", err
	}
	osFamily, err := apple_mdm.NormalizeOSFamily(args.OSFamily)
	if err != nil {
		return apple_mdm.ADESettingsChange{}, "", err
	}
	c := apple_mdm.ADESettingsChange{
		ZeroTouch:     args.ZeroTouch,
		WelcomeTitle:  args.WelcomeTitle,
		WelcomeText:   args.WelcomeParagraph,
		WelcomeButton: args.WelcomeButton,
	}
	for _, r := range args.ResetWelcome {
		switch strings.ToLower(strings.TrimSpace(r)) {
		case "title":
			c.ResetTitle = true
		case "paragraph":
			c.ResetText = true
		case "button":
			c.ResetButton = true
		default:
			return c, "", fmt.Errorf("reset_welcome takes title, paragraph and/or button, not %q", r)
		}
	}
	switch {
	case len(args.SetupOptions) > 0 && args.ClearSetupOptions:
		return c, "", fmt.Errorf("setup_options and clear_setup_options contradict each other")
	case len(args.SetupOptions) > 0:
		opts := args.SetupOptions
		c.SetupOptions = &opts
	case args.ClearSetupOptions:
		c.SetupOptions = &[]string{}
	}
	switch {
	case len(args.DefaultGroupIDs) > 0 && args.ClearDefaultGroups:
		return c, "", fmt.Errorf("default_group_ids and clear_default_groups contradict each other")
	case len(args.DefaultGroupIDs) > 0:
		ids := args.DefaultGroupIDs
		c.DefaultGroupIDs = &ids
	case args.ClearDefaultGroups:
		c.DefaultGroupIDs = &[]string{}
	}
	if _, err := c.Body(); err != nil {
		return c, "", err
	}
	return c, osFamily, nil
}

func readOnlyRefusal(tool string) string {
	return fmt.Sprintf("server is in read-only mode; %s with execute=true is not allowed", tool)
}

func (s *Server) registerAppleMDMADEWriteTools() {
	const settingsTool = "apple_mdm_ade_settings_update"
	addTypedToolWithPreFlight(s, settingsTool,
		"Change the Automated Device Enrollment experience for one OS family on one ADE token: whether enrollment is mandatory (zero-touch), the welcome screen, the setup-assistant options, the default device groups. ONLY the arguments you pass are changed — the API resets any field it is told to write without a value, so this tool never tells it to write a field you did not pass. Without execute=true it reads the current setting and returns what each field would go from and to.",
		func(args adeSettingsUpdateInput) error {
			if _, _, err := adeSettingsChangeFrom(args); err != nil {
				return err
			}
			if s.readOnly && args.Execute {
				return errors.New(readOnlyRefusal(settingsTool))
			}
			return nil
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args adeSettingsUpdateInput) (*mcp.CallToolResult, any, error) {
			change, osFamily, err := adeSettingsChangeFrom(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if s.readOnly && args.Execute {
				return errorResult(readOnlyRefusal(settingsTool)), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("%s: building v2 client: %v", settingsTool, err)), nil, nil
			}
			if change.DefaultGroupIDs != nil {
				for _, id := range *change.DefaultGroupIDs {
					if _, err := client.Get(ctx, apple_mdm.DeviceGroupEndpoint(id)); err != nil {
						var apiErr *api.APIError
						if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
							return errorResult(fmt.Sprintf("device group %s does not exist; the API would store it anyway, "+
								"and devices would enroll into nothing", id)), nil, nil
						}
						return errorResult(fmt.Sprintf("checking device group %s: %v", id, err)), nil, nil
					}
				}
			}
			endpoint := apple_mdm.ADESettingEndpoint(args.TokenID, osFamily)
			raw, err := client.Get(ctx, endpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("reading the current ADE settings: %v", err)), nil, nil
			}
			setting, err := apple_mdm.ParseADESetting(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			before, err := apple_mdm.ParseADESettingValues(setting)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return jsonResultWith(map[string]any{
					"plan":      true,
					"token_id":  args.TokenID,
					"os_family": osFamily,
					"effects":   change.Effects(before),
					"message":   "Nothing was written. Pass execute=true to apply these changes.",
				})
			}
			body, err := change.Body()
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			resp, err := client.Patch(ctx, endpoint, body)
			if err != nil {
				return errorResult(fmt.Sprintf("%s: %v", settingsTool, err)), nil, nil
			}
			updated, err := apple_mdm.ParseADESetting(resp)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			after, err := apple_mdm.ParseADESettingValues(updated)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if miss := change.Unapplied(after); len(miss) > 0 {
				return errorResult(fmt.Sprintf("the API answered success, but these did not change: %s",
					strings.Join(miss, ", "))), nil, nil
			}
			return rawSingleResult(updated)
		},
	)

	const refreshTool = "apple_mdm_ade_token_refresh_abm_devices"
	addTypedToolWithPreFlight(s, refreshTool,
		"Re-sync the Apple Business Manager device assignments behind one ADE token. Changes nothing on any device. A token that cannot sync (an expired one, presumably) answers HTTP 412. Set execute=true to trigger it.",
		func(args adeTokenActionInput) error {
			if err := apple_mdm.RequireObjectID("an ADE token", args.TokenID); err != nil {
				return err
			}
			if s.readOnly && args.Execute {
				return errors.New(readOnlyRefusal(refreshTool))
			}
			return nil
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args adeTokenActionInput) (*mcp.CallToolResult, any, error) {
			if err := apple_mdm.RequireObjectID("an ADE token", args.TokenID); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if s.readOnly && args.Execute {
				return errorResult(readOnlyRefusal(refreshTool)), nil, nil
			}
			if !args.Execute {
				return jsonResultWith(map[string]any{
					"plan":     true,
					"token_id": args.TokenID,
					"effects":  []string{"Re-syncs the ABM devices behind this token"},
					"message":  "Nothing was sent. Pass execute=true to proceed.",
				})
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("%s: building v2 client: %v", refreshTool, err)), nil, nil
			}
			if _, err := client.Create(ctx, apple_mdm.RefreshABMDevicesEndpoint(args.TokenID),
				apple_mdm.EmptyActionBody()); err != nil {
				return errorResult(fmt.Sprintf("%s: %v", refreshTool, err)), nil, nil
			}
			return jsonResultWith(map[string]any{
				"token_id": args.TokenID,
				"executed": true,
				"summary":  "ABM re-sync requested for ADE token " + args.TokenID,
			})
		},
	)

	adueTool := func(name, description string, enable bool) {
		addTypedToolWithPreFlight(s, name, description,
			func(args adueWriteInput) error {
				if err := apple_mdm.RequireObjectID("an Apple MDM configuration", args.AppleMDMID); err != nil {
					return err
				}
				if s.readOnly && args.Execute {
					return errors.New(readOnlyRefusal(name))
				}
				return nil
			},
			func(ctx context.Context, req *mcp.CallToolRequest, args adueWriteInput) (*mcp.CallToolResult, any, error) {
				if err := apple_mdm.RequireObjectID("an Apple MDM configuration", args.AppleMDMID); err != nil {
					return errorResult(err.Error()), nil, nil
				}
				if s.readOnly && args.Execute {
					return errorResult(readOnlyRefusal(name)), nil, nil
				}
				endpoint := apple_mdm.ServiceDiscoveryURLEndpoint(args.AppleMDMID)
				if !args.Execute {
					effect := "Publishes a JumpCloud-chosen discovery URL"
					if !enable {
						effect = "Removes the discovery URL; new sign-in enrollments stop"
					}
					return jsonResultWith(map[string]any{
						"plan":         true,
						"apple_mdm_id": args.AppleMDMID,
						"effects":      []string{effect},
						"message":      "Nothing was changed. Pass execute=true to proceed.",
					})
				}
				client, err := newV2ClientFunc()
				if err != nil {
					return errorResult(fmt.Sprintf("%s: building v2 client: %v", name, err)), nil, nil
				}
				if !enable {
					if _, err := client.Delete(ctx, endpoint); err != nil {
						return errorResult(fmt.Sprintf("%s: %v", name, err)), nil, nil
					}
					return jsonResultWith(map[string]any{
						"apple_mdm_id": args.AppleMDMID,
						"executed":     true,
						"summary":      "ADUE service discovery turned off",
					})
				}
				resp, err := client.Create(ctx, endpoint, apple_mdm.EmptyActionBody())
				if err != nil {
					return errorResult(fmt.Sprintf("%s: %v", name, err)), nil, nil
				}
				url, err := apple_mdm.ParseServiceDiscoveryURL(resp)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				return jsonResultWith(map[string]any{
					"apple_mdm_id":        args.AppleMDMID,
					"executed":            true,
					"serviceDiscoveryUrl": url,
				})
			},
		)
	}
	adueTool("apple_mdm_adue_enable",
		"Turn on the service-discovery URL Account-Driven User Enrollment uses to find this Apple MDM. JumpCloud chooses the URL — there is nothing to supply — and it is returned. Idempotent. Set execute=true to make the change.",
		true)
	adueTool("apple_mdm_adue_disable",
		"Turn off the ADUE service-discovery URL for this Apple MDM. Users can no longer start an Account-Driven User Enrollment by signing in until it is turned on again. Idempotent. Set execute=true to make the change.",
		false)
}
