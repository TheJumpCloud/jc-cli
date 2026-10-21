package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/config"
)

// Automated Device Enrollment, Apple Business Manager and Account-Driven User
// Enrollment tools. Eleven, matching the CLI's reads one for one.
//
// Every one reads, and none returns a secret. The two secret-bearing reads in
// this area are NOT here and will not be: the device recovery-lock password
// and the device lock PIN are the credentials that unlock a Mac, and a tool
// returning either would put it in a transcript while passing any
// mutation-based gate, because the call that harms is not the call that
// mutates. Same reasoning that keeps the agent credential writes and Password
// Vault's secret reads CLI-only.
//
// Two reads in the wider area are unreachable rather than withheld: depkey
// and the single enrollment-profile read answer HTTP 406, because they serve
// plists and every jc request carries Accept: application/json from the auth
// transport — which cannot be overridden from outside, by design.

type adeTokenInput struct {
	TokenID string `json:"token_id" jsonschema:"the ADE token's objectId, from agents of the ade_tokens list"`
}

type adeSettingInput struct {
	TokenID  string `json:"token_id" jsonschema:"the ADE token's objectId"`
	OSFamily string `json:"os_family" jsonschema:"which OS family: DARWIN (macOS) or IOS. macos and ios are accepted spellings"`
}

type abmDevicesInput struct {
	AppleMDMID string `json:"apple_mdm_id,omitempty" jsonschema:"optional Apple MDM id to scope the list to; omit for the whole organization"`
}

type deviceTasksInput struct {
	DeviceID string `json:"device_id" jsonschema:"the managed device's id — a DEVICE id, not an Apple MDM id"`
}

type appleMDMIDInput struct {
	AppleMDMID string `json:"apple_mdm_id" jsonschema:"the Apple MDM configuration's id"`
}

func (s *Server) registerAppleMDMADETools() {
	addTypedTool(s, "apple_mdm_ade_tokens_list",
		"Is Automated Device Enrollment working? Lists the ADE (DEP) tokens linking this organization to Apple Business Manager, with each token's ABM org name, expiry and device count. A token is what lets a device bought through ABM enroll itself, so when one expires zero-touch enrollment stops for every device behind it. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			raw, err := adeGet(ctx, apple_mdm.ADETokensEndpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("listing ADE tokens: %v", err)), nil, nil
			}
			rows, _, err := apple_mdm.ParseADETokens(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return rawListPage(rows)
		},
	)

	addTypedTool(s, "apple_mdm_ade_token_get",
		"Get one Automated Device Enrollment token by its objectId — ABM organization name, server name, expiry, device count and last sync time. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adeTokenInput) (*mcp.CallToolResult, any, error) {
			if args.TokenID == "" {
				return errorResult("token_id is required"), nil, nil
			}
			raw, err := adeGet(ctx, apple_mdm.ADETokenEndpoint(args.TokenID))
			if err != nil {
				return errorResult(fmt.Sprintf("getting ADE token: %v", err)), nil, nil
			}
			one, err := apple_mdm.ParseADEToken(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return rawSingleResult(one)
		},
	)

	addTypedTool(s, "apple_mdm_ade_tokens_stats",
		"Are any ADE tokens about to expire? Counts the organization's Automated Device Enrollment tokens by state: expired, expiring, valid, total. IMPORTANT when reading the answer: the total counts tokens, not working ones — a tenant with one token can report totalCount 1, expiringCount 1 and validCount 0 at the same time, meaning enrollment is about to break. Judge by validCount and the expiry counts, never by the total. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			raw, err := adeGet(ctx, apple_mdm.ADETokensStatsEndpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("getting ADE token stats: %v", err)), nil, nil
			}
			stats, err := apple_mdm.ParseADETokenStats(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return jsonResultWith(map[string]any{
				"expiredCount":   stats.ExpiredCount,
				"expiringCount":  stats.ExpiringCount,
				"validCount":     stats.ValidCount,
				"totalCount":     stats.TotalCount,
				"needsAttention": stats.NeedsAttention(),
			})
		},
	)

	addTypedTool(s, "apple_mdm_ade_token_org_names",
		"Which Apple Business Manager organizations are linked here? Lists the ABM organization names behind this organization's ADE tokens. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			raw, err := adeGet(ctx, apple_mdm.ADETokenOrgNamesEndpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("listing ADE token org names: %v", err)), nil, nil
			}
			names, total, err := apple_mdm.ParseOrgNames(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return jsonResultWith(map[string]any{"orgNames": names, "totalCount": total})
		},
	)

	addTypedTool(s, "apple_mdm_ade_settings_list",
		"What enrollment experience do ADE devices get? Lists the Automated Device Enrollment settings held for each OS family on one ADE token — the setup-assistant panes that are skipped, the welcome screen text, whether enrollment is mandatory. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adeTokenInput) (*mcp.CallToolResult, any, error) {
			if args.TokenID == "" {
				return errorResult("token_id is required"), nil, nil
			}
			raw, err := adeGet(ctx, apple_mdm.ADESettingsEndpoint(args.TokenID))
			if err != nil {
				return errorResult(fmt.Sprintf("listing ADE settings: %v", err)), nil, nil
			}
			rows, _, err := apple_mdm.ParseADESettings(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return rawListPage(rows)
		},
	)

	addTypedTool(s, "apple_mdm_ade_settings_get",
		"Get the Automated Device Enrollment settings for one OS family on one ADE token. The API names the families DARWIN (for macOS) and IOS; macos and ios are accepted here and translated. The spec also lists UNKNOWN, which the API rejects with HTTP 400, so it is refused before the call. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adeSettingInput) (*mcp.CallToolResult, any, error) {
			if args.TokenID == "" {
				return errorResult("token_id is required"), nil, nil
			}
			osFamily, err := apple_mdm.NormalizeOSFamily(args.OSFamily)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := adeGet(ctx, apple_mdm.ADESettingEndpoint(args.TokenID, osFamily))
			if err != nil {
				return errorResult(fmt.Sprintf("getting ADE settings: %v", err)), nil, nil
			}
			one, err := apple_mdm.ParseADESetting(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return rawSingleResult(one)
		},
	)

	addTypedTool(s, "apple_mdm_abm_devices_list",
		"Which devices has Apple Business Manager assigned to us? Lists ABM device assignments, organization-wide or scoped to one Apple MDM with apple_mdm_id. These are ASSIGNMENTS, not enrolled devices: a device appears here once ABM assigns it to a JumpCloud MDM server, whether or not it has ever been switched on. For devices that actually enrolled, use apple_mdm_devices_list. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args abmDevicesInput) (*mcp.CallToolResult, any, error) {
			endpoint := apple_mdm.ABMDevicesEndpoint
			if args.AppleMDMID != "" {
				endpoint = apple_mdm.ManagerABMDevicesEndpoint(args.AppleMDMID)
			}
			raw, err := adeGet(ctx, endpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("listing ABM devices: %v", err)), nil, nil
			}
			rows, _, err := apple_mdm.ParseABMDevices(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return rawListPage(rows)
		},
	)

	addTypedTool(s, "apple_mdm_background_tasks_list",
		"What MDM background tasks are queued on this device? Lists the launch daemons and agents MDM has queued for one managed device, with each task's identifier, label and state. The id is a DEVICE id, not an Apple MDM id, despite sitting in the same path position as one. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args deviceTasksInput) (*mcp.CallToolResult, any, error) {
			if args.DeviceID == "" {
				return errorResult("device_id is required"), nil, nil
			}
			raw, err := adeGet(ctx, apple_mdm.BackgroundTasksEndpoint(args.DeviceID))
			if err != nil {
				return errorResult(fmt.Sprintf("listing background tasks: %v", err)), nil, nil
			}
			rows, _, err := apple_mdm.ParseBackgroundTasks(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return rawListPage(rows)
		},
	)

	addTypedTool(s, "apple_mdm_adue_get",
		"How do users enroll a personal device? Returns this organization's Account-Driven User Enrollment (ADUE) URL — the BYOD path where a user enrolls by signing in with their JumpCloud account instead of going through Apple Business Manager. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			orgID, err := adeOrgID()
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := adeGet(ctx, apple_mdm.ADUEEndpoint(orgID))
			if err != nil {
				return errorResult(fmt.Sprintf("getting the ADUE URL: %v", err)), nil, nil
			}
			url, err := apple_mdm.ParseADUERedirect(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return jsonResultWith(map[string]any{"redirectUrl": url})
		},
	)

	addTypedTool(s, "apple_mdm_adue_config",
		"Returns this organization's Account-Driven User Enrollment service configuration — the servers an Apple device is pointed at when a user enrolls by signing in. The API delivers this as a JSON document inside a JSON string; it is unwrapped here, so what comes back is the configuration rather than an escaped blob. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			orgID, err := adeOrgID()
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := adeGet(ctx, apple_mdm.ADUEConfigEndpoint(orgID))
			if err != nil {
				return errorResult(fmt.Sprintf("getting the ADUE config: %v", err)), nil, nil
			}
			inner, err := apple_mdm.ParseADUEConfig(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return rawSingleResult(inner)
		},
	)

	addTypedTool(s, "apple_mdm_adue_validate",
		"Does this Apple MDM have a usable service discovery URL? IMPORTANT: the API reports a MISSING URL as HTTP 200 with responseCode NOT_FOUND, so the status code proves nothing — read the ok field in the answer, not the absence of an error. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args appleMDMIDInput) (*mcp.CallToolResult, any, error) {
			if args.AppleMDMID == "" {
				return errorResult("apple_mdm_id is required"), nil, nil
			}
			raw, err := adeGet(ctx, apple_mdm.ValidateServiceDiscoveryURLEndpoint(args.AppleMDMID))
			if err != nil {
				return errorResult(fmt.Sprintf("validating the service discovery URL: %v", err)), nil, nil
			}
			res, err := apple_mdm.ParseServiceDiscovery(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return jsonResultWith(map[string]any{
				"ok":           res.OK(),
				"responseCode": res.ResponseCode,
				"summary":      res.Describe(),
			})
		},
	)
}

// adeGet is the one place these tools reach the API, so a client-construction
// failure reads the same way for all of them.
func adeGet(ctx context.Context, endpoint string) (json.RawMessage, error) {
	client, err := newV2ClientFunc()
	if err != nil {
		return nil, fmt.Errorf("creating API client: %w", err)
	}
	return client.Get(ctx, endpoint)
}

// adeOrgID returns the organization the two ADUE reads are scoped to. They
// take an organization id where their siblings take an Apple MDM id, so this
// is not interchangeable with apple_mdm_id.
func adeOrgID() (string, error) {
	orgID := config.OrgID()
	if orgID == "" {
		return "", fmt.Errorf("these reads are scoped to an organization and jc has no org id; " +
			"check `jc auth status`, or set JC_ORG_ID")
	}
	return orgID, nil
}

// jsonResultWith returns a computed object rather than a passthrough of the
// API's bytes, for the tools whose value is in the derived verdict —
// needsAttention and ok — not in the raw counts.
func jsonResultWith(v map[string]any) (*mcp.CallToolResult, any, error) {
	res, err := jsonResult(v)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return res, nil, nil
}

// rawSingleResult returns one record as the API sent it.
func rawSingleResult(raw json.RawMessage) (*mcp.CallToolResult, any, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return errorResult(fmt.Sprintf("decoding the record: %v", err)), nil, nil
	}
	res, err := jsonResult(v)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return res, nil, nil
}
