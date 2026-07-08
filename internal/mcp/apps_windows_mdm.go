package mcp

import (
	"context"
	"fmt"
	"strings"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// MCP App: Windows custom MDM policies (KLA-459).
//
// Two tools, both backed by the same primitives the CLI uses:
//
//   windows_mdm_oma_uri_create_policy  — Policy CSP settings → JC policy
//   windows_mdm_registry_create_policy — HKLM registry rows → JC policy
//
// Both carry the `Execute bool` destructive-gate field and a preFlight
// closure (validation BEFORE the step-up gate, per the KLA-452
// pattern). Unlike the Apple app there are no read-only catalog tools
// yet — the Policy CSP discovery catalog is the KLA-460 follow-up;
// until then the agent supplies OMA-URI paths from its own knowledge
// of Microsoft's Policy CSP reference.

// ── tool 1: oma-uri create_policy ──────────────────────────────────

type windowsMDMOMAURISetting struct {
	URI    string `json:"uri" jsonschema:"OMA-URI path, e.g. ./Device/Vendor/MSFT/Policy/Config/BitLocker/RequireDeviceEncryption. Must start with ./"`
	Format string `json:"format" jsonschema:"Wire data type: int, chr (string), float, bool, xml, or b64. Aliases integer/string/boolean/base64 accepted."`
	Value  string `json:"value" jsonschema:"The setting value, string-encoded regardless of format."`
}

type windowsMDMOMAURICreatePolicyInput struct {
	// PolicyName is the JC-side policy name.
	PolicyName string `json:"policy_name" jsonschema:"JumpCloud policy name."`
	// Settings is the list of OMA-URI triples the policy applies.
	Settings []windowsMDMOMAURISetting `json:"settings" jsonschema:"One or more OMA-URI settings. All are device-scoped (JumpCloud's template is device-scoped)."`
	// Execute — the destructive-gate signal. Without it the tool
	// returns a preview (validated settings + resolved JC template);
	// no POST happens.
	Execute bool `json:"execute,omitempty" jsonschema:"Set to true to actually POST the policy to JumpCloud. Without this the tool returns a preview (normalized settings, resolved JC template) and never creates anything. Execute: true routes through the step-up auth gate."`
}

// ── tool 2: registry create_policy ─────────────────────────────────

type windowsMDMRegistryKey struct {
	Location string `json:"location" jsonschema:"HKLM-relative key path (HKEY_LOCAL_MACHINE is implied — do NOT prefix it). JumpCloud recommends SOFTWARE\\Policies\\... paths. 255-char limit."`
	Name     string `json:"name" jsonschema:"Registry value name. 99-char limit."`
	Type     string `json:"type" jsonschema:"Value type: DWORD, expandString, multiString, String, or QWORD. Aliases REG_DWORD/EXPAND_SZ/MULTI_SZ/SZ/REG_QWORD accepted."`
	Data     string `json:"data" jsonschema:"The value data. Must be an unsigned integer for DWORD/QWORD."`
}

type windowsMDMRegistryCreatePolicyInput struct {
	PolicyName string                  `json:"policy_name" jsonschema:"JumpCloud policy name."`
	Keys       []windowsMDMRegistryKey `json:"keys" jsonschema:"One or more registry rows, all applied under HKEY_LOCAL_MACHINE."`
	Execute    bool                    `json:"execute,omitempty" jsonschema:"Set to true to actually POST the policy to JumpCloud. Without this the tool returns a preview and never creates anything. Execute: true routes through the step-up auth gate."`
}

// ── shared result shape ────────────────────────────────────────────

type windowsMDMCreatePolicyResult struct {
	// On execute: the new JC policy ID.
	PolicyID   string `json:"policy_id,omitempty"`
	PolicyName string `json:"policy_name"`
	// The resolved template — the most useful preview feedback the
	// agent gets, and resolving on preview catches a tenant without
	// Windows MDM before the destructive gate ever prompts.
	JCTemplateID   string `json:"jc_template_id"`
	JCTemplateName string `json:"jc_template_name"`
	// The normalized entries that did / would have shipped (formats
	// and reg types canonicalized to wire values).
	Settings []windows_mdm.OMAURISetting `json:"settings,omitempty"`
	Keys     []windows_mdm.RegistryKey   `json:"keys,omitempty"`
	// Executed reflects whether the POST actually fired.
	Executed bool `json:"executed"`
}

// ── conversion helpers ─────────────────────────────────────────────

func toOMAURISettings(in []windowsMDMOMAURISetting) []windows_mdm.OMAURISetting {
	out := make([]windows_mdm.OMAURISetting, len(in))
	for i, s := range in {
		out[i] = windows_mdm.OMAURISetting{URI: s.URI, Format: s.Format, Value: s.Value}
	}
	return out
}

func toRegistryKeys(in []windowsMDMRegistryKey) []windows_mdm.RegistryKey {
	out := make([]windows_mdm.RegistryKey, len(in))
	for i, k := range in {
		out[i] = windows_mdm.RegistryKey{Location: k.Location, ValueName: k.Name, RegType: k.Type, Data: k.Data}
	}
	return out
}

// ── registration ───────────────────────────────────────────────────

// registerWindowsMDMTools wires both Windows custom-policy tools onto
// the server. Both are Execute-gated with preflight validation, per
// the KLA-452 addTypedToolWithPreFlight pattern.
func (s *Server) registerWindowsMDMTools() {
	addTypedToolWithPreFlight(s, "windows_mdm_oma_uri_create_policy",
		"Create a JumpCloud 'Custom MDM (OMA-URI)' policy for Windows devices from one or more Policy CSP settings (OMA-URI path + format + value). "+
			"Use for Windows settings JumpCloud has no built-in policy for — the Windows analog of apple_mdm_payloads_create_policy, minus the catalog (supply OMA-URI paths from Microsoft's Policy CSP reference). "+
			"Formats: "+strings.Join(windows_mdm.OMAURIFormats(), ", ")+". Device-scoped only. "+
			"Without execute: true, returns a preview (normalized settings + resolved JC template); this reads the tenant's policy templates but never POSTs/creates anything. "+
			"With execute: true, POSTs the policy and returns the new policy ID via the step-up auth gate and audit log.",
		// preFlight runs BEFORE the step-up auth gate — a malformed
		// call (bad format enum, missing uri, empty policy_name) fails
		// without wasting a Touch ID approval.
		func(args windowsMDMOMAURICreatePolicyInput) error {
			if s.readOnly && args.Execute {
				return fmt.Errorf("server is in read-only mode; windows_mdm_oma_uri_create_policy with execute=true is not allowed")
			}
			if args.PolicyName == "" {
				return fmt.Errorf("windows_mdm_oma_uri_create_policy: 'policy_name' is required")
			}
			if _, err := windows_mdm.NormalizeAndValidateSettings(toOMAURISettings(args.Settings)); err != nil {
				return fmt.Errorf("windows_mdm_oma_uri_create_policy: %v", err)
			}
			return nil
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args windowsMDMOMAURICreatePolicyInput) (*mcp.CallToolResult, any, error) {
			normalized, err := windows_mdm.NormalizeAndValidateSettings(toOMAURISettings(args.Settings))
			if err != nil {
				return errorResult(fmt.Sprintf("windows_mdm_oma_uri_create_policy: %v", err)), nil, nil
			}

			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("windows_mdm_oma_uri_create_policy: building v2 client: %v", err)), nil, nil
			}
			tmpl, err := windows_mdm.ResolveOMAURITemplate(ctx, client)
			if err != nil {
				return errorResult(fmt.Sprintf("windows_mdm_oma_uri_create_policy: resolving template: %v", err)), nil, nil
			}

			result := windowsMDMCreatePolicyResult{
				PolicyName:     args.PolicyName,
				JCTemplateID:   tmpl.ID,
				JCTemplateName: tmpl.Name,
				Settings:       normalized,
			}

			if !args.Execute {
				res, err := jsonResult(result)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				return res, nil, nil
			}

			// Execute path — the step-up gate has already authorized
			// this call in the wrapper.
			body := windows_mdm.BuildOMAURIPolicyBody(args.PolicyName, tmpl, normalized)
			raw, err := client.Create(ctx, "/policies", body)
			if err != nil {
				return errorResult(fmt.Sprintf("windows_mdm_oma_uri_create_policy: POST /policies: %v", err)), nil, nil
			}
			result.PolicyID, result.PolicyName = extractPolicyIDName(raw)
			if result.PolicyName == "" {
				result.PolicyName = args.PolicyName
			}
			result.Executed = true
			res, err := jsonResult(result)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedToolWithPreFlight(s, "windows_mdm_registry_create_policy",
		"Create a JumpCloud 'Advanced: Custom Registry Keys' policy for Windows devices from one or more HKLM registry rows. "+
			"Every row lands under HKEY_LOCAL_MACHINE (the hive is implied — never include it in location). JumpCloud recommends SOFTWARE\\Policies\\... locations. "+
			"Types: "+strings.Join(windows_mdm.RegistryRegTypes(), ", ")+". Device-scoped only. "+
			"Without execute: true, returns a preview (normalized keys + resolved JC template); this reads the tenant's policy templates but never POSTs/creates anything. "+
			"With execute: true, POSTs the policy and returns the new policy ID via the step-up auth gate and audit log.",
		func(args windowsMDMRegistryCreatePolicyInput) error {
			if s.readOnly && args.Execute {
				return fmt.Errorf("server is in read-only mode; windows_mdm_registry_create_policy with execute=true is not allowed")
			}
			if args.PolicyName == "" {
				return fmt.Errorf("windows_mdm_registry_create_policy: 'policy_name' is required")
			}
			if _, err := windows_mdm.NormalizeAndValidateKeys(toRegistryKeys(args.Keys)); err != nil {
				return fmt.Errorf("windows_mdm_registry_create_policy: %v", err)
			}
			return nil
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args windowsMDMRegistryCreatePolicyInput) (*mcp.CallToolResult, any, error) {
			normalized, err := windows_mdm.NormalizeAndValidateKeys(toRegistryKeys(args.Keys))
			if err != nil {
				return errorResult(fmt.Sprintf("windows_mdm_registry_create_policy: %v", err)), nil, nil
			}

			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("windows_mdm_registry_create_policy: building v2 client: %v", err)), nil, nil
			}
			tmpl, err := windows_mdm.ResolveRegistryTemplate(ctx, client)
			if err != nil {
				return errorResult(fmt.Sprintf("windows_mdm_registry_create_policy: resolving template: %v", err)), nil, nil
			}

			result := windowsMDMCreatePolicyResult{
				PolicyName:     args.PolicyName,
				JCTemplateID:   tmpl.ID,
				JCTemplateName: tmpl.Name,
				Keys:           normalized,
			}

			if !args.Execute {
				res, err := jsonResult(result)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				return res, nil, nil
			}

			body := windows_mdm.BuildRegistryPolicyBody(args.PolicyName, tmpl, normalized)
			raw, err := client.Create(ctx, "/policies", body)
			if err != nil {
				return errorResult(fmt.Sprintf("windows_mdm_registry_create_policy: POST /policies: %v", err)), nil, nil
			}
			result.PolicyID, result.PolicyName = extractPolicyIDName(raw)
			if result.PolicyName == "" {
				result.PolicyName = args.PolicyName
			}
			result.Executed = true
			res, err := jsonResult(result)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)
}
