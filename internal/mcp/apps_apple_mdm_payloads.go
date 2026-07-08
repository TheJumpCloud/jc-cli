package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
)

// MCP App: Apple MDM payloads catalog + create-policy.
//
// Four tools, all backed by the same primitives the CLI uses:
//
//   apple_mdm_payloads_search       — list/filter the catalog
//   apple_mdm_payloads_show         — full schema for one payload
//   apple_mdm_payloads_template     — emit a .mobileconfig (offline)
//   apple_mdm_payloads_create_policy — emit + POST to JumpCloud
//
// The first three are read-only and bypass the step-up gate;
// create_policy carries the `Execute bool` field that triggers the
// existing destructive-op authorization flow (same shape as
// recipe_run / users_delete / etc.).
//
// Why a separate MCP App: the CLI surface is multi-step (search →
// show → template → create), but agents do better with one well-
// shaped tool per intent. Bundling them into one MCP App keeps the
// agent's planner short — one tool call per logical step — without
// forcing it to remember sub-command syntax.

// ── tool 1: search ─────────────────────────────────────────────────

type appleMDMPayloadsSearchInput struct {
	// OS narrows results to payloads supporting the given Apple
	// platform. Accepts Apple's canonical names (macOS/iOS/tvOS/
	// visionOS/watchOS). Empty = all platforms.
	OS string `json:"os,omitempty" jsonschema:"Restrict to payloads supporting this Apple platform (macOS, iOS, tvOS, visionOS, watchOS). Empty returns all."`
	// Search is a case-insensitive substring match against
	// payloadtype, title, and description. The agent typically uses
	// this to map a natural-language intent to candidate payloads
	// (e.g. \"airdrop\" → com.apple.applicationaccess).
	Search string `json:"search,omitempty" jsonschema:"Case-insensitive substring search over payloadtype/title/description."`
}

// appleMDMPayloadSummary is the per-row search result — trimmed
// enough that listing the whole catalog (125 entries) doesn't blow
// the context budget. Agents that want the full schema follow up
// with apple_mdm_payloads_show.
type appleMDMPayloadSummary struct {
	ID               string   `json:"id"`
	Type             string   `json:"type"`
	Title            string   `json:"title,omitempty"`
	Description      string   `json:"description,omitempty"`
	SupportedOS      []string `json:"supported_os"`
	KeyCount         int      `json:"key_count"`
	RequiredKeyCount int      `json:"required_key_count"`
}

type appleMDMPayloadsSearchResult struct {
	Release  string                    `json:"release"`
	Total    int                       `json:"total"`    // unfiltered catalog size
	Matched  int                       `json:"matched"`  // after applying filters
	Payloads []appleMDMPayloadSummary  `json:"payloads"`
}

// ── tool 2: show ───────────────────────────────────────────────────

type appleMDMPayloadsShowInput struct {
	// PayloadTypeOrID accepts either Apple's canonical payloadtype
	// (e.g. com.apple.wifi.managed) or the catalog ID for variant
	// disambiguation (e.g. com.apple.MCX(EnergySaver)).
	PayloadTypeOrID string `json:"payloadtype_or_id" jsonschema:"Apple PayloadType (e.g. com.apple.wifi.managed) OR catalog ID for variant disambiguation (e.g. com.apple.MCX(EnergySaver))."`
}

type appleMDMPayloadsShowResult struct {
	Release string             `json:"release"`
	Payload apple_mdm.Payload  `json:"payload"`
}

// ── tool 3: template ───────────────────────────────────────────────

type appleMDMPayloadsTemplateInput struct {
	// PayloadTypeOrID — same resolution as show.
	PayloadTypeOrID string `json:"payloadtype_or_id" jsonschema:"Apple PayloadType or catalog ID."`
	// Values maps Apple key names to user-supplied values. Same shape
	// CoerceAndValidate consumes elsewhere; may be nil if every
	// required key has a schema default.
	Values map[string]any `json:"values,omitempty" jsonschema:"Map of Apple payload key names to values. Validated against the schema."`
	// DisplayName overrides the inner-payload PayloadDisplayName.
	// Optional.
	DisplayName string `json:"display_name,omitempty" jsonschema:"Override the inner-payload display name. Defaults to the schema's Title."`
	// Identifier sets the envelope's PayloadIdentifier. Empty
	// auto-generates a jc.<uuid> form.
	Identifier string `json:"identifier,omitempty" jsonschema:"Profile reverse-DNS identifier. Empty auto-generates jc.<uuid>."`
	// Organization sets the envelope's PayloadOrganization. Optional.
	Organization string `json:"organization,omitempty" jsonschema:"Profile organization metadata. Optional."`
	// RemovalDisallowed sets PayloadRemovalDisallowed on the envelope.
	RemovalDisallowed bool `json:"removal_disallowed,omitempty" jsonschema:"Prevent end users from removing the profile via System Settings."`
}

type appleMDMPayloadsTemplateResult struct {
	PayloadType        string         `json:"payload_type"`
	ValidatedValues    map[string]any `json:"validated_values"`
	MobileconfigBytes  int            `json:"mobileconfig_bytes"`
	Mobileconfig       string         `json:"mobileconfig"`
}

// ── tool 4: create_policy ──────────────────────────────────────────

// appleMDMPayloadsCreatePolicyInput extends Template with the
// destructive-gate Execute field plus the JC-specific fields.
type appleMDMPayloadsCreatePolicyInput struct {
	// PayloadTypeOrID — same resolution as the other tools.
	PayloadTypeOrID string `json:"payloadtype_or_id" jsonschema:"Apple PayloadType or catalog ID."`
	// Values — same as template.
	Values map[string]any `json:"values,omitempty" jsonschema:"Map of Apple payload key names to values."`
	// PolicyName is the JC-side policy name. Also used as the
	// envelope's PayloadDisplayName.
	PolicyName string `json:"policy_name" jsonschema:"JumpCloud policy name. Also serves as the profile display name."`
	// OS picks the JumpCloud template family. macOS or iOS as of
	// KLA-450; tvOS/visionOS/watchOS not supported by JC.
	OS string `json:"os,omitempty" jsonschema:"Apple platform — macOS (default) or iOS. tvOS/visionOS/watchOS not supported by JumpCloud MDM."`
	// Identifier / Organization / RemovalDisallowed — same as template.
	Identifier        string `json:"identifier,omitempty" jsonschema:"Profile reverse-DNS identifier. Empty auto-generates jc.<uuid>."`
	Organization      string `json:"organization,omitempty" jsonschema:"Profile organization metadata."`
	RemovalDisallowed bool   `json:"removal_disallowed,omitempty" jsonschema:"Prevent end users from removing the profile via System Settings."`
	// Redispatch matches JumpCloud's Admin Portal default. Ignored
	// for iOS templates (which don't expose the field).
	Redispatch bool `json:"redispatch,omitempty" jsonschema:"Re-apply policy on every OS update. Default false to keep the wire shape conservative; the agent should set true when it wants Admin Portal parity."`
	// Execute — the destructive-gate signal. Without it the tool
	// returns the same validated values + mobileconfig the template
	// tool would, plus the resolved JC template ID; no POST happens.
	// With it, the gate runs and the policy is created.
	Execute bool `json:"execute,omitempty" jsonschema:"Set to true to actually POST the policy to JumpCloud. Without this the tool returns a preview (validated values, mobileconfig, resolved JC template) and never calls the JumpCloud API. Execute: true routes through the step-up auth gate."`
}

type appleMDMPayloadsCreatePolicyResult struct {
	// On execute: the JC policy ID + name + JC template ID.
	PolicyID         string `json:"policy_id,omitempty"`
	PolicyName       string `json:"policy_name"`
	JCTemplateID     string `json:"jc_template_id"`
	JCTemplateName   string `json:"jc_template_name"`
	// Always: the inner Apple payloadtype + validated values + the
	// emitted mobileconfig bytes (the body that did / would have shipped).
	PayloadType       string         `json:"payload_type"`
	ValidatedValues   map[string]any `json:"validated_values"`
	MobileconfigBytes int            `json:"mobileconfig_bytes"`
	Mobileconfig      string         `json:"mobileconfig"`
	// Executed reflects whether the POST actually fired. False on
	// preview (Execute: false); true when the JC policy was created.
	Executed bool `json:"executed"`
}

// ── handler logic ──────────────────────────────────────────────────

// resolvePayloadForMCP runs the same ID-then-Type lookup the CLI uses,
// returning a precise error so the agent can correct its input rather
// than reasoning about silently-wrong defaults.
func resolvePayloadForMCP(ref string) (apple_mdm.Payload, error) {
	cat, err := apple_mdm.Default()
	if err != nil {
		return apple_mdm.Payload{}, fmt.Errorf("loading catalog: %w", err)
	}
	if p, ok := cat.ByID(ref); ok {
		return p, nil
	}
	variants := cat.VariantsOf(ref)
	switch len(variants) {
	case 0:
		return apple_mdm.Payload{}, fmt.Errorf(
			"no payload with ID or payloadtype %q in catalog (release %s)", ref, cat.Release)
	case 1:
		return variants[0], nil
	default:
		ids := make([]string, 0, len(variants))
		for _, v := range variants {
			ids = append(ids, v.ID)
		}
		return apple_mdm.Payload{}, fmt.Errorf(
			"payloadtype %q is ambiguous (%d variants); pass the catalog ID instead: %s",
			ref, len(variants), strings.Join(ids, ", "))
	}
}

// summarizePayload trims a Payload to the search-result shape.
func summarizePayload(p apple_mdm.Payload) appleMDMPayloadSummary {
	required := 0
	for _, k := range p.Keys {
		if strings.EqualFold(k.Presence, "required") {
			required++
		}
	}
	supported := []string{}
	for _, plat := range []string{"iOS", "macOS", "tvOS", "visionOS", "watchOS"} {
		if sup, ok := p.SupportedOS[plat]; ok && sup.Available() {
			supported = append(supported, plat)
		}
	}
	return appleMDMPayloadSummary{
		ID:               p.ID,
		Type:             p.Type,
		Title:            p.Title,
		Description:      p.Description,
		SupportedOS:      supported,
		KeyCount:         len(p.Keys),
		RequiredKeyCount: required,
	}
}

// validateAndEmit is the shared template/create-policy backend: looks
// up the schema, validates values, emits the mobileconfig. Returns
// the typed values + the plist bytes; the caller decides whether to
// POST.
func validateAndEmit(ref string, values map[string]any, displayName, identifier, organization string, removalDisallowed bool) (apple_mdm.Payload, map[string]any, []byte, error) {
	payload, err := resolvePayloadForMCP(ref)
	if err != nil {
		return apple_mdm.Payload{}, nil, nil, err
	}
	typed, err := apple_mdm.CoerceAndValidate(payload, values)
	if err != nil {
		return apple_mdm.Payload{}, nil, nil, err
	}
	// Fall back to the schema's title when display_name is empty —
	// matches the single-payload CLI behavior.
	if displayName == "" {
		displayName = payload.Title
	}
	var buf bytes.Buffer
	err = apple_mdm.EmitMobileconfig(&buf, apple_mdm.EnvelopeOpts{
		DisplayName:       displayName,
		Identifier:        identifier,
		Organization:      organization,
		RemovalDisallowed: removalDisallowed,
	}, []apple_mdm.PayloadInstance{{
		Schema:      payload,
		Values:      typed,
		DisplayName: displayName,
	}})
	if err != nil {
		return apple_mdm.Payload{}, nil, nil, fmt.Errorf("emitting mobileconfig: %w", err)
	}
	return payload, typed, buf.Bytes(), nil
}

// jcOSFamilyForMCP mirrors the CLI's jcOSFamily but lives in the
// mcp package to avoid a cross-package dep. Same semantics: accepts
// Apple's canonical name OR the JC family alias; rejects
// tvOS/visionOS/watchOS.
func jcOSFamilyForMCP(s string) (string, error) {
	switch s {
	case "", "macOS", apple_mdm.OSFamilyDarwin:
		return apple_mdm.OSFamilyDarwin, nil
	case "iOS", apple_mdm.OSFamilyIOS:
		return apple_mdm.OSFamilyIOS, nil
	case "tvOS", "visionOS", "watchOS":
		return "", fmt.Errorf("--os %q: JumpCloud MDM does not manage this Apple platform", s)
	default:
		return "", fmt.Errorf("--os %q: unknown Apple platform; supported: macOS, iOS", s)
	}
}

// canonicalApplePlatformForMCP normalizes the OS string for
// SupportedOS lookup. Same semantics as the CLI's helper.
func canonicalApplePlatformForMCP(s string) string {
	switch s {
	case "macOS", apple_mdm.OSFamilyDarwin:
		return "macOS"
	case "iOS", apple_mdm.OSFamilyIOS:
		return "iOS"
	}
	return s
}

// ── registration ───────────────────────────────────────────────────

// registerAppleMDMPayloadsTools wires all four tools onto the server.
// Read-only tools use the simple addToolWithMetaTyped wrapper; the
// destructive one uses addTypedTool so the Execute reflection check
// kicks in.
func (s *Server) registerAppleMDMPayloadsTools() {
	addToolWithMetaTyped(s, "apple_mdm_payloads_search",
		"Search Apple's MDM Configuration Profile schema catalog (vendored from github.com/apple/device-management, MIT-licensed, Release-v26.4). "+
			"Use this to map a natural-language MDM intent ('disable AirDrop on iPads', 'enforce FileVault on Macs') to candidate Apple PayloadTypes. "+
			"Returns up to 125 trimmed-down entries; pair with apple_mdm_payloads_show to fetch the full schema for one.",
		nil,
		func(ctx context.Context, req *mcp.CallToolRequest, args appleMDMPayloadsSearchInput) (*mcp.CallToolResult, any, error) {
			cat, err := apple_mdm.Default()
			if err != nil {
				return errorResult(fmt.Sprintf("apple_mdm_payloads_search: loading catalog: %v", err)), nil, nil
			}
			// Normalize OS alias to Apple's canonical name so agents
			// passing the JC family (`ios`, `darwin`) or the Apple form
			// (`iOS`, `macOS`) both hit. Without this, `os=ios` would
			// return an empty list because SupportedOS keys on `iOS`.
			// (Cursor Bugbot PR #60 catch.)
			osFilter := canonicalApplePlatformForMCP(args.OS)
			matches := cat.Filter(apple_mdm.FilterOpts{OS: osFilter, Search: args.Search})
			summaries := make([]appleMDMPayloadSummary, 0, len(matches))
			for _, p := range matches {
				summaries = append(summaries, summarizePayload(p))
			}
			res, err := jsonResult(appleMDMPayloadsSearchResult{
				Release:  cat.Release,
				Total:    cat.Len(),
				Matched:  len(summaries),
				Payloads: summaries,
			})
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addToolWithMetaTyped(s, "apple_mdm_payloads_show",
		"Show the full Apple MDM Configuration Profile schema for one payload — every key, type, default value, range, rangelist, valuetype, presence (required/optional), and per-platform support gate. "+
			"This is what the agent walks to know exactly which keys it must / may set when building a profile. "+
			"For ambiguous types (com.apple.MCX has 6 variants), pass the catalog ID instead.",
		nil,
		func(ctx context.Context, req *mcp.CallToolRequest, args appleMDMPayloadsShowInput) (*mcp.CallToolResult, any, error) {
			payload, err := resolvePayloadForMCP(args.PayloadTypeOrID)
			if err != nil {
				return errorResult(fmt.Sprintf("apple_mdm_payloads_show: %v", err)), nil, nil
			}
			cat, _ := apple_mdm.Default()
			res, err := jsonResult(appleMDMPayloadsShowResult{
				Release: cat.Release,
				Payload: payload,
			})
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addToolWithMetaTyped(s, "apple_mdm_payloads_template",
		"Emit a valid .mobileconfig for one Apple payload, offline. Values are validated against the schema (range / rangelist / presence / valuetype); the result is a plutil -lint-clean plist with the standard Configuration envelope. "+
			"This tool never calls the JumpCloud API — pair with apple_mdm_payloads_create_policy when the agent is ready to ship.",
		nil,
		func(ctx context.Context, req *mcp.CallToolRequest, args appleMDMPayloadsTemplateInput) (*mcp.CallToolResult, any, error) {
			payload, typed, plistBytes, err := validateAndEmit(args.PayloadTypeOrID, args.Values,
				args.DisplayName, args.Identifier, args.Organization, args.RemovalDisallowed)
			if err != nil {
				return errorResult(fmt.Sprintf("apple_mdm_payloads_template: %v", err)), nil, nil
			}
			res, err := jsonResult(appleMDMPayloadsTemplateResult{
				PayloadType:       payload.Type,
				ValidatedValues:   typed,
				MobileconfigBytes: len(plistBytes),
				Mobileconfig:      string(plistBytes),
			})
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedToolWithPreFlight(s, "apple_mdm_payloads_create_policy",
		"Create a JumpCloud Custom MDM Configuration Profile policy from one Apple payload. "+
			"Without execute: true, returns the same preview shape as apple_mdm_payloads_template (validated values + mobileconfig) plus the resolved JumpCloud template ID/name; this reads the tenant's policy templates but never POSTs/creates anything. "+
			"With execute: true, POSTs the policy to JumpCloud and returns the new policy ID. Execute: true routes through the step-up auth gate (Touch ID / TTY prompt) and the audit log, same as users_delete.",
		// preFlight runs BEFORE the step-up auth gate. Any error we
		// return here short-circuits the wrapper without prompting the
		// operator — so a malformed call (missing policy_name, OS that
		// doesn't manage the payload, payloadtype typo) doesn't waste a
		// Touch ID approval. (Cursor Bugbot PR #60 catch.)
		func(args appleMDMPayloadsCreatePolicyInput) error {
			if s.readOnly && args.Execute {
				return fmt.Errorf("server is in read-only mode; apple_mdm_payloads_create_policy with execute=true is not allowed")
			}
			if args.PolicyName == "" {
				return fmt.Errorf("apple_mdm_payloads_create_policy: 'policy_name' is required (used as the JumpCloud policy name + the inner-payload display name)")
			}
			payload, err := resolvePayloadForMCP(args.PayloadTypeOrID)
			if err != nil {
				return fmt.Errorf("apple_mdm_payloads_create_policy: %v", err)
			}
			osFamily := args.OS
			if osFamily == "" {
				osFamily = "macOS"
			}
			schemaPlat := canonicalApplePlatformForMCP(osFamily)
			if sup, ok := payload.SupportedOS[schemaPlat]; !ok || !sup.Available() {
				return fmt.Errorf(
					"apple_mdm_payloads_create_policy: payload %q does not declare support for %s",
					payload.Type, schemaPlat)
			}
			if _, err := jcOSFamilyForMCP(osFamily); err != nil {
				return fmt.Errorf("apple_mdm_payloads_create_policy: %v", err)
			}
			// Per-key value validation. Pulled into preflight so a bad
			// value also short-circuits the gate — same UX motivation
			// as the SupportedOS check.
			if _, err := apple_mdm.CoerceAndValidate(payload, args.Values); err != nil {
				return fmt.Errorf("apple_mdm_payloads_create_policy: %v", err)
			}
			return nil
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args appleMDMPayloadsCreatePolicyInput) (*mcp.CallToolResult, any, error) {
			// Validate values + emit the mobileconfig. preFlight has
			// already confirmed the payload exists and supports the OS,
			// so the only error class still possible here is per-key
			// validation (rangelist/presence/coercion).
			payload, typed, plistBytes, err := validateAndEmit(args.PayloadTypeOrID, args.Values,
				args.PolicyName, args.Identifier, args.Organization, args.RemovalDisallowed)
			if err != nil {
				return errorResult(fmt.Sprintf("apple_mdm_payloads_create_policy: %v", err)), nil, nil
			}
			osFamily := args.OS
			if osFamily == "" {
				osFamily = "macOS"
			}
			resolvedFamily, err := jcOSFamilyForMCP(osFamily)
			if err != nil {
				return errorResult(fmt.Sprintf("apple_mdm_payloads_create_policy: %v", err)), nil, nil
			}

			// Build the response shape both preview and execute share.
			result := appleMDMPayloadsCreatePolicyResult{
				PolicyName:        args.PolicyName,
				PayloadType:       payload.Type,
				ValidatedValues:   typed,
				MobileconfigBytes: len(plistBytes),
				Mobileconfig:      string(plistBytes),
			}

			// Build the v2 client + resolve the JC template for both
			// preview and execute — the template ID is one of the
			// most useful pieces of feedback the agent gets on
			// preview, and resolving on preview catches a misconfigured
			// tenant before the destructive gate prompts.
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("apple_mdm_payloads_create_policy: building v2 client: %v", err)), nil, nil
			}
			tmpl, err := apple_mdm.ResolveCustomMDMTemplate(ctx, client, resolvedFamily)
			if err != nil {
				return errorResult(fmt.Sprintf("apple_mdm_payloads_create_policy: resolving template: %v", err)), nil, nil
			}
			result.JCTemplateID = tmpl.ID
			result.JCTemplateName = tmpl.Name

			if !args.Execute {
				// Preview path. No POST, no step-up gate; the gate
				// fires automatically in the addTypedTool wrapper
				// when Execute is true.
				res, err := jsonResult(result)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				return res, nil, nil
			}

			// Execute path. The step-up gate has already authorized
			// this call by the time we get here (the addTypedTool
			// wrapper handles it before the handler fires).
			body := apple_mdm.BuildCustomMDMPolicyBody(args.PolicyName, tmpl, plistBytes, args.Redispatch)
			raw, err := client.Create(ctx, "/policies", body)
			if err != nil {
				return errorResult(fmt.Sprintf("apple_mdm_payloads_create_policy: POST /policies: %v", err)), nil, nil
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

// extractPolicyIDName pulls .id / .name from the JC create response.
// Tolerant — missing fields yield "" rather than an error because the
// creation itself succeeded; we only want to enrich the agent's reply.
func extractPolicyIDName(raw []byte) (string, string) {
	var resp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", ""
	}
	return resp.ID, resp.Name
}
