package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/googleemm"
)

// Google EMM (Android Enterprise) tools. READS ONLY: six of this area's
// writes are device commands including erase-device, and DELETE on an
// enterprise unbinds Android Enterprise for the whole organization.

type gemmEnterpriseInput struct {
	Enterprise string `json:"enterprise" jsonschema:"The Android Enterprise: its display name, Google resource name (enterprises/XXXX), bare Google id, or JumpCloud 24-character object id. Only the object id is accepted by the API; jc matches the others and sends the right one."`
}

type gemmDeviceInput struct {
	DeviceID string `json:"device_id" jsonschema:"The enrolled device's id, as it appears in an enterprise's device list"`
}

type gemmTokenInput struct {
	Enterprise string `json:"enterprise" jsonschema:"The Android Enterprise the token belongs to — display name, Google id, or object id"`
	TokenID    string `json:"token_id" jsonschema:"The enrollment token id. These are opaque strings, not 24-character hex ids."`
}

func gemmFetcher(client *api.V2Client) googleemm.Fetcher {
	return func(ctx context.Context, endpoint string) (json.RawMessage, error) {
		return client.Get(ctx, endpoint)
	}
}

// gemmResolve confirms the enterprise through the shared resolver, so this
// server and the CLI agree about what an operator typed — and so a device
// count is never reported for an enterprise nobody has confirmed exists.
func gemmResolve(ctx context.Context, identifier string) (googleemm.Enterprise, error) {
	client, err := newV2ClientFunc()
	if err != nil {
		return googleemm.Enterprise{}, err
	}
	return googleemm.ResolveEnterprise(ctx, gemmFetcher(client), identifier)
}

func gemmRead(ctx context.Context, endpoint, what string) (*mcp.CallToolResult, any, error) {
	client, err := newV2ClientFunc()
	if err != nil {
		return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
	}
	raw, err := client.Get(ctx, endpoint)
	if err != nil {
		return errorResult(fmt.Sprintf("reading %s: %v", what, err)), nil, nil
	}
	return textResult(string(raw)), nil, nil
}

func (s *Server) registerGoogleEMMTools() {
	addTypedTool(s, "google_emm_enterprises_list", "Every Android Enterprise binding this organization has, one row each, with its display name, enterprise type, linked device group and whether device enrollment is allowed. Most orgs have zero or one. Each row carries TWO identifiers and they are not interchangeable: objectId is JumpCloud's 24-character id and the only one any endpoint accepts, while name holds Google's own resource id and is display-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Get(ctx, googleemm.EnterprisesEndpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("listing enterprises: %v", err)), nil, nil
			}
			list, perr := googleemm.ParseEnterprises(raw)
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			rows := make([]json.RawMessage, 0, len(list))
			for _, e := range list {
				b, merr := json.Marshal(e)
				if merr != nil {
					return errorResult(merr.Error()), nil, nil
				}
				rows = append(rows, b)
			}
			total := len(rows)
			return listEnvelope(rows, &total)
		},
	)

	addTypedTool(s, "google_emm_enterprise_get", "One Android Enterprise binding, by display name, Google resource name, bare Google id or JumpCloud object id. JumpCloud serves no single-enterprise endpoint — a direct fetch is a 404 — so this lists and filters, which also means an identifier that matches nothing says so rather than returning an empty record.",
		func(ctx context.Context, req *mcp.CallToolRequest, args gemmEnterpriseInput) (*mcp.CallToolResult, any, error) {
			e, err := gemmResolve(ctx, args.Enterprise)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			out, merr := json.Marshal(e)
			if merr != nil {
				return errorResult(merr.Error()), nil, nil
			}
			return textResult(string(out)), nil, nil
		},
	)

	addTypedTool(s, "google_emm_connection_status", "Whether an Android Enterprise binding is still live against Google, and which organization it belongs to. Use it when enrollment or policy delivery has stopped working and you need to know whether the link itself is the problem. Returns a bare object, not a list envelope.",
		func(ctx context.Context, req *mcp.CallToolRequest, args gemmEnterpriseInput) (*mcp.CallToolResult, any, error) {
			e, err := gemmResolve(ctx, args.Enterprise)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return gemmRead(ctx, googleemm.ConnectionStatusEndpoint(e.ObjectID), "connection status")
		},
	)

	addTypedTool(s, "google_emm_devices_list", "How many Android devices are enrolled in one enterprise and which ones, one row per device. The enterprise is confirmed against the binding list before the count is read, deliberately: JumpCloud answers this endpoint with an empty device list for ANY well-formed 24-character id, including one that belongs to no enterprise, so an unconfirmed count would be a confident zero about something that does not exist.",
		func(ctx context.Context, req *mcp.CallToolRequest, args gemmEnterpriseInput) (*mcp.CallToolResult, any, error) {
			e, err := gemmResolve(ctx, args.Enterprise)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			client, cerr := newV2ClientFunc()
			if cerr != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", cerr)), nil, nil
			}
			raw, gerr := client.Get(ctx, googleemm.EnterpriseDevicesEndpoint(e.ObjectID))
			if gerr != nil {
				return errorResult(fmt.Sprintf("listing devices: %v", gerr)), nil, nil
			}
			rows, total, perr := googleemm.ParseDevices(raw)
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			return listEnvelope(rows, &total)
		},
	)

	addTypedTool(s, "google_emm_device_get", "One enrolled Android device in full, by its device id. Take the id from an enterprise's device listing — there is no organization-wide Android device index, so a device is only findable through the enterprise it enrolled into.",
		func(ctx context.Context, req *mcp.CallToolRequest, args gemmDeviceInput) (*mcp.CallToolResult, any, error) {
			return gemmRead(ctx, googleemm.DeviceEndpoint(args.DeviceID), "the device")
		},
	)

	addTypedTool(s, "google_emm_device_policy_results", "How the assigned policies actually landed on one enrolled Android device — the per-setting outcome, rather than what the policy asked for. This is the difference between a policy being assigned and a policy being in force, which is the question worth asking when a device looks compliant on paper.",
		func(ctx context.Context, req *mcp.CallToolRequest, args gemmDeviceInput) (*mcp.CallToolResult, any, error) {
			return gemmRead(ctx, googleemm.DevicePolicyResultsEndpoint(args.DeviceID), "policy results")
		},
	)

	addTypedTool(s, "google_emm_enrollment_tokens_list", "Every enrollment token an enterprise currently holds, one row each, with its expiry, enrollment link, QR code and whether it is single-use. These are the credentials that let a device join, so the roster is worth reviewing for tokens that have outlived their purpose. Note this response uses results and totalCount, unlike the other listings in this area.",
		func(ctx context.Context, req *mcp.CallToolRequest, args gemmEnterpriseInput) (*mcp.CallToolResult, any, error) {
			e, err := gemmResolve(ctx, args.Enterprise)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			client, cerr := newV2ClientFunc()
			if cerr != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", cerr)), nil, nil
			}
			raw, gerr := client.Get(ctx, googleemm.EnterpriseTokensEndpoint(e.ObjectID))
			if gerr != nil {
				return errorResult(fmt.Sprintf("listing enrollment tokens: %v", gerr)), nil, nil
			}
			rows, total, perr := googleemm.ParseTokens(raw)
			if perr != nil {
				return errorResult(perr.Error()), nil, nil
			}
			return listEnvelope(rows, &total)
		},
	)

	addTypedTool(s, "google_emm_enrollment_token_get", "One enrollment token in full, including the enrollment link and QR code image a device would use to join. Token ids are opaque strings rather than 24-character hex, so take the id from the enterprise's token listing rather than constructing one.",
		func(ctx context.Context, req *mcp.CallToolRequest, args gemmTokenInput) (*mcp.CallToolResult, any, error) {
			e, err := gemmResolve(ctx, args.Enterprise)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return gemmRead(ctx, googleemm.EnterpriseTokenEndpoint(e.ObjectID, args.TokenID), "the enrollment token")
		},
	)
}
