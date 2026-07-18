package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/bundle"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

// Security baseline bundle tools (KLA-472): the MCP face of `jc
// bundle`. list/show/status are read-only; apply is Execute-gated like
// the other create tools. Apply reuses bundle.BuildApplyPlan/Execute —
// the same orchestration the CLI runs — so the preview an agent sees
// is exactly the plan that executes.

// ── tool: bundle_list ──────────────────────────────────────────────

type bundleListInput struct{}

type bundleSummary struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Origin      string `json:"origin"`
	Platforms   string `json:"platforms"`
	Policies    int    `json:"policies"`
}

type bundleListResult struct {
	Bundles []bundleSummary `json:"bundles"`
}

// ── tool: bundle_show ──────────────────────────────────────────────

type bundleShowInput struct {
	Name string `json:"name" jsonschema:"The bundle name (from bundle_list)."`
}

// ── tool: bundle_status ────────────────────────────────────────────

type bundleStatusInput struct {
	Name string `json:"name" jsonschema:"The bundle name (from bundle_list). Compares the applied tenant state against the bundle definition."`
}

// ── tool: bundle_apply ─────────────────────────────────────────────

type bundleApplyInput struct {
	Name string `json:"name" jsonschema:"The bundle name (from bundle_list)."`
	// DeviceGroup mirrors the CLI --group flag.
	DeviceGroup string `json:"device_group,omitempty" jsonschema:"Optional device group (name or ID) to bind the bundle's policy group to."`
	// PolicyGroupName mirrors --policy-group-name.
	PolicyGroupName string `json:"policy_group_name,omitempty" jsonschema:"Override the default policy group name '<bundle> (v<version>)'."`
	Execute         bool   `json:"execute,omitempty" jsonschema:"Set true to create the policies/policy group/binding. Without it, returns the full step-by-step plan (read-only: validates, resolves templates, pre-flights name conflicts — never POSTs)."`
}

type bundleApplyResult struct {
	Bundle          string             `json:"bundle"`
	Version         string             `json:"version"`
	PolicyGroupName string             `json:"policy_group_name"`
	Steps           []bundle.ApplyStep `json:"steps"`
	Executed        bool               `json:"executed"`
	// Result is set on the execute path.
	Result *bundle.ApplyResult `json:"result,omitempty"`
	// Error carries Execute's failure text on partial failure so the
	// structured result (with every created ID in Result) still reaches
	// the agent for precise cleanup.
	Error string `json:"error,omitempty"`
}

// findBundle loads all bundles and resolves one by name.
func findBundle(name string) (*bundle.Bundle, error) {
	bundles, err := bundle.LoadAll()
	if err != nil {
		return nil, err
	}
	b := bundle.FindByName(bundles, name)
	if b == nil {
		return nil, fmt.Errorf("no bundle named %q — use bundle_list to see what's available", name)
	}
	return b, nil
}

// registerBundleTools wires the four bundle tools.
func (s *Server) registerBundleTools() {
	addToolWithMetaTyped(s, "bundle_list",
		"List security baseline bundles: versioned YAML artifacts grouping Apple MDM profiles + Windows OMA-URI/registry policies into one applyable set. "+
			"Includes builtin bundles compiled into jc and user bundles from ~/.config/jc/bundles/ (same name = user overrides builtin). Local only — never calls the JumpCloud API.",
		nil,
		func(ctx context.Context, req *mcp.CallToolRequest, args bundleListInput) (*mcp.CallToolResult, any, error) {
			bundles, err := bundle.LoadAll()
			if err != nil {
				return errorResult(fmt.Sprintf("bundle_list: %v", err)), nil, nil
			}
			out := bundleListResult{Bundles: []bundleSummary{}}
			for _, b := range bundles {
				out.Bundles = append(out.Bundles, bundleSummary{
					Name:        b.Name,
					Version:     b.Version,
					Description: b.Description,
					Origin:      b.Source.Origin,
					Platforms:   joinComma(b.Platforms()),
					Policies:    len(b.Policies),
				})
			}
			res, err := jsonResult(out)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addToolWithMetaTyped(s, "bundle_show",
		"Show one bundle in full: metadata, source/licensing attribution, and every policy unit with its complete settings. Local only — never calls the JumpCloud API.",
		nil,
		func(ctx context.Context, req *mcp.CallToolRequest, args bundleShowInput) (*mcp.CallToolResult, any, error) {
			if args.Name == "" {
				return errorResult("bundle_show: 'name' is required"), nil, nil
			}
			b, err := findBundle(args.Name)
			if err != nil {
				return errorResult(fmt.Sprintf("bundle_show: %v", err)), nil, nil
			}
			res, err := jsonResult(b)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addToolWithMetaTyped(s, "bundle_status",
		"Drift detection: compare an applied bundle against the tenant. Finds the bundle's policy group (via its bundle:<name>@<version> description marker, falling back to the default group name), decodes every member policy, and reports per-unit in-sync / drifted (with value-level diffs) / missing, plus orphaned group members. Read-only.",
		nil,
		func(ctx context.Context, req *mcp.CallToolRequest, args bundleStatusInput) (*mcp.CallToolResult, any, error) {
			if args.Name == "" {
				return errorResult("bundle_status: 'name' is required"), nil, nil
			}
			b, err := findBundle(args.Name)
			if err != nil {
				return errorResult(fmt.Sprintf("bundle_status: %v", err)), nil, nil
			}
			cat, err := apple_mdm.Default()
			if err != nil {
				return errorResult(fmt.Sprintf("bundle_status: loading Apple catalog: %v", err)), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("bundle_status: building v2 client: %v", err)), nil, nil
			}
			report, err := bundle.Status(ctx, client, b, cat)
			if err != nil {
				return errorResult(fmt.Sprintf("bundle_status: %v", err)), nil, nil
			}
			res, err := jsonResult(report)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedToolWithPreFlight(s, "bundle_apply",
		"Apply a security baseline bundle to the tenant: create one policy per unit (named '<bundle>/<unit>'), a policy group holding them (description carries the bundle:<name>@<version> provenance marker), and optionally bind a device group so its devices receive the baseline. "+
			"Create-only: existing names are rejected up front with remediation. Mid-apply failures never roll back — the result lists every created ID. "+
			"Without execute: true, returns the full step plan (validates the bundle, resolves JC templates, pre-flights name conflicts — reads the tenant but never POSTs). "+
			"With execute: true, runs the plan via the step-up auth gate and audit log.",
		func(args bundleApplyInput) error {
			if s.readOnly && args.Execute {
				return fmt.Errorf("server is in read-only mode; bundle_apply with execute=true is not allowed")
			}
			if args.Name == "" {
				return fmt.Errorf("bundle_apply: 'name' is required")
			}
			// Full offline validation before the step-up gate — a
			// broken bundle must not waste a Touch ID approval.
			b, err := findBundle(args.Name)
			if err != nil {
				return fmt.Errorf("bundle_apply: %v", err)
			}
			cat, err := apple_mdm.Default()
			if err != nil {
				return fmt.Errorf("bundle_apply: loading Apple catalog: %v", err)
			}
			if err := bundle.Validate(b, cat); err != nil {
				return fmt.Errorf("bundle_apply: %v", err)
			}
			return nil
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args bundleApplyInput) (*mcp.CallToolResult, any, error) {
			b, err := findBundle(args.Name)
			if err != nil {
				return errorResult(fmt.Sprintf("bundle_apply: %v", err)), nil, nil
			}
			cat, err := apple_mdm.Default()
			if err != nil {
				return errorResult(fmt.Sprintf("bundle_apply: loading Apple catalog: %v", err)), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("bundle_apply: building v2 client: %v", err)), nil, nil
			}

			opts := bundle.ApplyOptions{PolicyGroupName: args.PolicyGroupName}
			if args.DeviceGroup != "" {
				id, err := resolve.NewV2Resolver(client).Resolve(ctx, args.DeviceGroup, resolve.DeviceGroupConfig)
				if err != nil {
					return errorResult(fmt.Sprintf("bundle_apply: resolving device group %q: %v", args.DeviceGroup, err)), nil, nil
				}
				opts.DeviceGroupID, opts.DeviceGroupName = id, args.DeviceGroup
			}

			applyPlan, err := bundle.BuildApplyPlan(ctx, client, b, cat, opts)
			if err != nil {
				return errorResult(fmt.Sprintf("bundle_apply: %v", err)), nil, nil
			}

			out := bundleApplyResult{
				Bundle:          b.Name,
				Version:         b.Version,
				PolicyGroupName: applyPlan.PolicyGroupName,
				Steps:           applyPlan.Steps,
			}
			if !args.Execute {
				res, err := jsonResult(out)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				return res, nil, nil
			}

			// Execute path — the step-up gate has already authorized
			// this call in the wrapper. On partial failure the result
			// (with every created ID) rides along with the error text
			// so the agent can clean up precisely.
			result, err := applyPlan.Execute(ctx, client)
			out.Result = result
			out.Executed = err == nil
			if err != nil {
				// Emit the structured result — including every created
				// ID in out.Result — as an error so the agent can clean
				// up precisely, not just the bare error text.
				out.Error = fmt.Sprintf("bundle_apply: %v", err)
				data, jerr := json.MarshalIndent(out, "", "  ")
				if jerr != nil {
					return errorResult(out.Error), nil, nil
				}
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
					IsError: true,
				}, nil, nil
			}
			res, jerr := jsonResult(out)
			if jerr != nil {
				return errorResult(jerr.Error()), nil, nil
			}
			return res, nil, nil
		},
	)
}

// joinComma is a tiny helper (strings.Join without importing strings
// solely for one call site would be silly — but we already avoid the
// import churn by keeping it here).
func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
