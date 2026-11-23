package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/gcep"
)

// GCEP tools: the two reads only. Changing or removing the organization's
// GCEP integration (PUT/DELETE /gcep/configuration) is deliberately CLI-only.

type gcepDeviceStateInput struct {
	SystemID string `json:"systemId" jsonschema:"The device's 24-character hex system object id."`
}

func (s *Server) registerGCEPTools() {
	addTypedTool(s, "gcep_config_get", "The organization's GCEP (Google device-trust) configuration: customerId, googleUserEmail, state (configuration_state_*), connectedState (connected_state_*), applicableOsFamilies (mac_os, windows) and who configured it when. Returns {configured:false} when the organization has none. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Get(ctx, gcep.ConfigurationEndpoint)
			if err != nil && !gcep.IsNotConfiguredError(err) {
				return errorResult(fmt.Sprintf("getting the GCEP configuration: %v", err)), nil, nil
			}
			if err != nil || gcep.NotConfigured(raw) {
				res, jerr := jsonResult(map[string]any{"configured": false, "message": gcep.NotConfiguredMessage})
				if jerr != nil {
					return errorResult(jerr.Error()), nil, nil
				}
				return res, nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "gcep_device_state_get", "One device's GCEP trust state, by its 24-character hex system id: registrationStatus (registered, not_registered), managementStatus (managed, not_managed, managed_unspecified) and deviceUsers (deviceUserId, googleEmail, localDeviceUsername, lastSuccessfulReportedTime). Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args gcepDeviceStateInput) (*mcp.CallToolResult, any, error) {
			if !gcep.IsObjectID(args.SystemID) {
				return errorResult(fmt.Sprintf("%q is not a valid system id — expected a 24-character hex object id", args.SystemID)), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Get(ctx, gcep.DeviceStateEndpoint(args.SystemID))
			if err != nil {
				return errorResult(fmt.Sprintf("getting the GCEP device state: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)
}
