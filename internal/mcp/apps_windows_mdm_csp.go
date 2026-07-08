package mcp

import (
	"context"
	"fmt"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// Windows Policy CSP catalog tools (KLA-460) — the read-only discovery
// half of the windows_mdm app, joining the two create tools from
// KLA-459 (5 tools total, symmetric with apple_mdm_payloads_*).
//
// None of these touch the JumpCloud API. The catalog is Microsoft's
// DDF v2 snapshot, fetched once from Microsoft's official URL
// (SHA-256-pinned) into the local cache — see
// internal/windows_mdm/catalog.go for why it is not vendored.

// ── tool: csp_search ───────────────────────────────────────────────

type windowsMDMCSPSearchInput struct {
	Area string `json:"area,omitempty" jsonschema:"Restrict to one Policy CSP area (e.g. Camera, Update, DeviceLock, ADMX_AppCompat). Empty searches all ~230 areas."`
	// Search mirrors the CLI: substring over area, name, URI, and
	// description — the agent's natural-language-to-OMA-URI bridge.
	Search      string `json:"search,omitempty" jsonschema:"Case-insensitive substring over area, name, URI, and description (e.g. 'screen capture', 'bitlocker')."`
	Scope       string `json:"scope,omitempty" jsonschema:"Restrict to 'device' or 'user' scoped settings. JumpCloud's Custom MDM (OMA-URI) template is device-scoped."`
	ExcludeADMX bool   `json:"exclude_admx,omitempty" jsonschema:"Drop ADMX-backed settings (their values need ADMX-style XML, not plain scalars)."`
	Limit       int    `json:"limit,omitempty" jsonschema:"Maximum results to return (default 50, max 200). The catalog holds ~3700 settings — narrow with area/search rather than raising the limit."`
}

// windowsMDMCSPSettingSummary trims a Setting for search results so a
// broad query doesn't blow the agent's context budget.
type windowsMDMCSPSettingSummary struct {
	Setting     string `json:"setting"` // "Area/Name" — the ref show/template take
	URI         string `json:"uri"`
	Format      string `json:"format"`
	Scope       string `json:"scope"`
	ADMXBacked  bool   `json:"admx_backed,omitempty"`
	Description string `json:"description,omitempty"`
}

type windowsMDMCSPSearchResult struct {
	Snapshot string `json:"snapshot"`
	Total    int    `json:"total"`   // full catalog size
	Matched  int    `json:"matched"` // after filters
	Returned int    `json:"returned"`
	// Truncated is set when matched > returned — never a silent cap;
	// the agent should narrow with area/search.
	Truncated bool                          `json:"truncated,omitempty"`
	Settings  []windowsMDMCSPSettingSummary `json:"settings"`
}

// ── tool: csp_show ─────────────────────────────────────────────────

type windowsMDMCSPShowInput struct {
	Setting string `json:"setting" jsonschema:"The setting ref as Area/PolicyName (e.g. Camera/AllowCamera, DeviceLock/MaxDevicePasswordFailedAttempts). Case-insensitive."`
}

type windowsMDMCSPShowResult struct {
	Snapshot string              `json:"snapshot"`
	Setting  windows_mdm.Setting `json:"setting"`
}

// ── tool: csp_template ─────────────────────────────────────────────

type windowsMDMCSPTemplateInput struct {
	Settings []string `json:"settings" jsonschema:"One or more setting refs (Area/PolicyName). The result feeds windows_mdm_oma_uri_create_policy's settings directly."`
}

type windowsMDMCSPTemplateResult struct {
	Snapshot string `json:"snapshot"`
	// Settings is the ready-to-edit list windows_mdm_oma_uri_create_policy
	// consumes as its `settings` argument — values seeded from the
	// schema default (or first allowed value); adjust before executing.
	Settings []windows_mdm.OMAURISetting `json:"settings"`
	// Warnings flags ADMX-backed refs (value must be ADMX-style XML)
	// and user-scoped refs (JC's template is device-scoped).
	Warnings []string `json:"warnings,omitempty"`
}

// ── registration ───────────────────────────────────────────────────

const cspSearchLimitMax = 200

// registerWindowsMDMCSPTools wires the three read-only catalog tools.
// Called from registerWindowsMDMTools so the whole windows_mdm app
// registers from one place.
func (s *Server) registerWindowsMDMCSPTools() {
	addToolWithMetaTyped(s, "windows_mdm_csp_search",
		"Search Microsoft's Windows Policy CSP settings catalog (~230 areas, ~3700 settings) to map a natural-language Windows-management intent ('disable the camera', 'require BitLocker-era encryption', 'lock screen timeout') to the exact OMA-URI + format + allowed values. "+
			"Data is Microsoft's pinned DDF v2 snapshot, auto-fetched once from Microsoft's official URL (SHA-256-verified) into the local cache — never the JumpCloud API. "+
			"Pair with windows_mdm_csp_show for full metadata and windows_mdm_csp_template → windows_mdm_oma_uri_create_policy to ship a policy.",
		nil,
		func(ctx context.Context, req *mcp.CallToolRequest, args windowsMDMCSPSearchInput) (*mcp.CallToolResult, any, error) {
			if args.Scope != "" && args.Scope != "device" && args.Scope != "user" {
				return errorResult(fmt.Sprintf("windows_mdm_csp_search: scope %q: want device or user", args.Scope)), nil, nil
			}
			cat, err := windows_mdm.DefaultCatalog(ctx, nil)
			if err != nil {
				return errorResult(fmt.Sprintf("windows_mdm_csp_search: loading catalog: %v", err)), nil, nil
			}
			matches := cat.Filter(windows_mdm.FilterOpts{
				Area: args.Area, Search: args.Search, Scope: args.Scope, ExcludeADMX: args.ExcludeADMX,
			})
			limit := args.Limit
			if limit <= 0 {
				limit = 50
			}
			if limit > cspSearchLimitMax {
				limit = cspSearchLimitMax
			}
			returned := matches
			if len(returned) > limit {
				returned = returned[:limit]
			}
			summaries := make([]windowsMDMCSPSettingSummary, 0, len(returned))
			for _, m := range returned {
				summaries = append(summaries, windowsMDMCSPSettingSummary{
					Setting:     m.Area + "/" + m.Name,
					URI:         m.URI,
					Format:      m.Format,
					Scope:       m.Scope,
					ADMXBacked:  m.ADMXBacked,
					Description: m.Description,
				})
			}
			res, err := jsonResult(windowsMDMCSPSearchResult{
				Snapshot:  cat.Snapshot,
				Total:     cat.Len(),
				Matched:   len(matches),
				Returned:  len(summaries),
				Truncated: len(matches) > len(summaries),
				Settings:  summaries,
			})
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addToolWithMetaTyped(s, "windows_mdm_csp_show",
		"Show one Windows Policy CSP setting in full: OMA-URI, wire format, description, default value, allowed values (enum with per-value descriptions / range / regex), minimum OS build, scope, ADMX-backed and deprecated flags. "+
			"This is everything needed to author the value before creating a policy. Never calls the JumpCloud API.",
		nil,
		func(ctx context.Context, req *mcp.CallToolRequest, args windowsMDMCSPShowInput) (*mcp.CallToolResult, any, error) {
			cat, err := windows_mdm.DefaultCatalog(ctx, nil)
			if err != nil {
				return errorResult(fmt.Sprintf("windows_mdm_csp_show: loading catalog: %v", err)), nil, nil
			}
			setting, ok := cat.ByRef(args.Setting)
			if !ok {
				return errorResult(fmt.Sprintf(
					"windows_mdm_csp_show: no Policy CSP setting %q in snapshot %s — use windows_mdm_csp_search to find the right Area/PolicyName ref",
					args.Setting, cat.Snapshot)), nil, nil
			}
			res, err := jsonResult(windowsMDMCSPShowResult{Snapshot: cat.Snapshot, Setting: setting})
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addToolWithMetaTyped(s, "windows_mdm_csp_template",
		"Emit the ready-to-edit settings list ({uri, format, value} triples) for one or more Policy CSP settings — the exact shape windows_mdm_oma_uri_create_policy consumes as its `settings` argument. "+
			"Values are seeded from each setting's default (or first allowed value); adjust them before executing the create tool. Never calls the JumpCloud API. "+
			"Warnings flag ADMX-backed settings (value must be ADMX-style XML like <enabled/>) and user-scoped settings (JumpCloud's template is device-scoped).",
		nil,
		func(ctx context.Context, req *mcp.CallToolRequest, args windowsMDMCSPTemplateInput) (*mcp.CallToolResult, any, error) {
			if len(args.Settings) == 0 {
				return errorResult("windows_mdm_csp_template: 'settings' requires at least one Area/PolicyName ref"), nil, nil
			}
			cat, err := windows_mdm.DefaultCatalog(ctx, nil)
			if err != nil {
				return errorResult(fmt.Sprintf("windows_mdm_csp_template: loading catalog: %v", err)), nil, nil
			}
			out := windowsMDMCSPTemplateResult{Snapshot: cat.Snapshot}
			for _, ref := range args.Settings {
				setting, ok := cat.ByRef(ref)
				if !ok {
					return errorResult(fmt.Sprintf(
						"windows_mdm_csp_template: no Policy CSP setting %q in snapshot %s", ref, cat.Snapshot)), nil, nil
				}
				if setting.ADMXBacked {
					out.Warnings = append(out.Warnings, fmt.Sprintf(
						"%s is ADMX-backed — its value must be ADMX-style XML (<enabled/>, <disabled/>, or <enabled/><data .../>), not a plain scalar", ref))
				}
				if setting.Scope == "user" {
					out.Warnings = append(out.Warnings, fmt.Sprintf(
						"%s is user-scoped; JumpCloud's Custom MDM (OMA-URI) template is device-scoped and may not apply it", ref))
				}
				out.Settings = append(out.Settings, windows_mdm.TemplateSetting(setting))
			}
			res, err := jsonResult(out)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)
}
