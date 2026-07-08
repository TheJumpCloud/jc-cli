package mcp

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed apps_html/dashboard.html
var dashboardHTML string

//go:embed apps_html/compliance.html
var complianceHTML string

//go:embed apps_html/recipe_runner.html
var recipeRunnerHTML string

//go:embed apps_html/common.js
var appCommonJS string

const (
	mcpAppMIMEType = "text/html;profile=mcp-app"
	// appCommonMarker in an app's HTML is replaced at registration time with a
	// <script> tag containing common.js. Keeps postMessage plumbing out of each
	// app's content.
	appCommonMarker = "<!--JC_APP_COMMON-->"
)

// appSpec describes a single MCP App (tool + ui:// resource pair).
// Adding a new app means: one new .html file under apps_html/, one entry in
// appSpecs, no other Go wiring required.
type appSpec struct {
	// Name is the MCP tool name (e.g. "dashboard_view").
	Name string
	// Description is the MCP tool description shown to LLMs.
	Description string
	// ResourceURI is the ui:// resource the host fetches to render the app
	// (e.g. "ui://jc/dashboard"). Must be unique across the server.
	ResourceURI string
	// ResourceName and ResourceDescription populate the MCP Resource
	// metadata; ResourceName is what users see in resource listings.
	ResourceName        string
	ResourceDescription string
	// HTML is the embedded app template. It may include the appCommonMarker
	// comment, which the registrar replaces with common.js at registration.
	HTML string
	// Handler returns the JSON-serializable payload pushed to the app as the
	// initial tool result. Server wraps the returned value in an MCP
	// CallToolResult (text content block) automatically.
	Handler func(ctx context.Context) (any, error)
}

// appSpecs lists every MCP App this server exposes. Each entry wires a tool,
// a ui:// resource, the _meta.ui.resourceUri link, and server-side data
// aggregation. Add a new app by appending here + dropping its HTML in
// apps_html/.
var appSpecs = []appSpec{
	{
		Name:                "dashboard_view",
		Description:         "Show an interactive JumpCloud organization dashboard with user/device counts, MFA adoption, device OS breakdown, resource counts, and recent event activity. Returns structured data that renders as a rich HTML dashboard in MCP App-capable hosts.",
		ResourceURI:         "ui://jc/dashboard",
		ResourceName:        "Dashboard App",
		ResourceDescription: "Interactive JumpCloud organization dashboard",
		HTML:                dashboardHTML,
		Handler: func(ctx context.Context) (any, error) {
			return fetchDashboardData(ctx)
		},
	},
	{
		Name:                "compliance_view",
		Description:         "Show a JumpCloud compliance snapshot scoped to audit-friendly metrics: MFA adoption (% enrolled + list of users without MFA), device encryption (% FDE-enabled, segmented by OS, with unencrypted-device drill-down), password-age histogram (<30d / 30-60d / 60-90d / >90d), and admin inventory (per-admin email, role, MFA status, last login). Renders as a 4-card report in MCP App-capable hosts; returns the same data as JSON when rendering isn't supported.",
		ResourceURI:         "ui://jc/compliance",
		ResourceName:        "Compliance Snapshot App",
		ResourceDescription: "Audit-friendly JumpCloud compliance snapshot (MFA, FDE, password age, admins)",
		HTML:                complianceHTML,
		Handler: func(ctx context.Context) (any, error) {
			return fetchComplianceData(ctx)
		},
	},
	{
		Name:                "recipe_runner_view",
		Description:         "Show an interactive JumpCloud recipe runner: browse built-in and user recipes, fill in parameters via an auto-generated form, preview the plan, and (with operator approval) execute the recipe end-to-end. Initial payload is the recipe catalog (same shape as recipe_list); the iframe drives subsequent plan/execute calls via recipe_run.",
		ResourceURI:         "ui://jc/recipe-runner",
		ResourceName:        "Recipe Runner App",
		ResourceDescription: "Interactive jc recipe runner (browse → parameter form → plan/execute)",
		HTML:                recipeRunnerHTML,
		Handler: func(ctx context.Context) (any, error) {
			return fetchRecipeListData()
		},
	},
}

// renderAppHTML returns the app's HTML with the common.js scaffolding injected
// in place of appCommonMarker. If the marker is absent the HTML is returned
// unchanged, letting apps opt out of the shared scaffolding.
func renderAppHTML(raw string) string {
	if !strings.Contains(raw, appCommonMarker) {
		return raw
	}
	return strings.Replace(raw, appCommonMarker, "<script>"+appCommonJS+"</script>", 1)
}

// addToolWithMetaTyped wraps mcp.AddTool with rate limiting, audit logging,
// tool filtering, and Meta support — the typed-input variant of
// addToolWithMeta for MCP Apps that accept parameters (service filters,
// time ranges, target IDs, etc.). The In type parameter is used by the SDK
// to auto-derive the tool's JSON input schema.
func addToolWithMetaTyped[In any](s *Server, name, description string, meta mcp.Meta, handler func(ctx context.Context, req *mcp.CallToolRequest, args In) (*mcp.CallToolResult, any, error)) {
	if !s.toolFilter.isAllowed(name) {
		return
	}

	tool := &mcp.Tool{
		Name:        name,
		Description: description,
		Meta:        meta,
	}

	wrapped := func(ctx context.Context, req *mcp.CallToolRequest, args In) (*mcp.CallToolResult, any, error) {
		if err := s.limiter.allow(); err != nil {
			s.auditLog.log(name, req.Params.Arguments, false, err.Error())
			return errorResult(err.Error()), nil, nil
		}

		result, out, err := handler(ctx, req, args)

		if err != nil {
			s.auditLog.log(name, req.Params.Arguments, false, err.Error())
		} else if result != nil && result.IsError {
			errMsg := "tool error"
			if len(result.Content) > 0 {
				if tc, ok := result.Content[0].(*mcp.TextContent); ok {
					errMsg = tc.Text
				}
			}
			s.auditLog.log(name, req.Params.Arguments, false, errMsg)
		} else {
			s.auditLog.log(name, req.Params.Arguments, true, "")
		}

		return result, out, err
	}

	mcp.AddTool(s.mcpServer, tool, wrapped)
	s.toolNames = append(s.toolNames, name)
}

// addToolWithMeta is the no-args convenience wrapper for addToolWithMetaTyped.
// Kept as a method on *Server for call-site ergonomics (s.addToolWithMeta(...))
// since no-args App tools are the common case.
func (s *Server) addToolWithMeta(name, description string, meta mcp.Meta, handler func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error)) {
	addToolWithMetaTyped[struct{}](s, name, description, meta, handler)
}

// --- MCP App registration entry points ---

// registerAppTools registers the tool half of every MCP App in appSpecs,
// plus any apps with typed inputs that live outside the no-args slice.
// Each tool carries _meta.ui.resourceUri pointing at its matching ui:// resource.
func (s *Server) registerAppTools() {
	for _, spec := range appSpecs {
		s.registerAppTool(spec)
	}
	// Apps that accept parameters are registered individually because they
	// need typed handler generics. Each call registers both tool and resource
	// (the resource half is no different from a no-args app).
	s.registerInsightsView()
	s.registerUserView()
	s.registerDeviceView()
	s.registerAppleMDMPayloadsTools()
	s.registerWindowsMDMTools()
}

// registerAppResources registers the ui:// resource half of every MCP App.
// Resource bodies are HTML with common.js injected in place of the marker.
func (s *Server) registerAppResources() {
	for _, spec := range appSpecs {
		s.registerAppResource(spec)
	}
}

// registerAppTool wires one app's tool side: adds an MCP tool with
// _meta.ui.resourceUri so capable hosts render the app inline, and a
// wrapped handler that runs the spec's Handler and marshals its return
// value into a text-content tool result.
func (s *Server) registerAppTool(spec appSpec) {
	handler := spec.Handler
	s.addToolWithMeta(
		spec.Name,
		spec.Description,
		mcp.Meta{
			"ui":             map[string]any{"resourceUri": spec.ResourceURI},
			"ui/resourceUri": spec.ResourceURI, // legacy key for older hosts
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			data, err := handler(ctx)
			if err != nil {
				return errorResult(fmt.Sprintf("fetching %s data: %v", spec.Name, err)), nil, nil
			}
			res, err := jsonResult(data)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)
}

// registerAppResource wires one app's UI resource side: serves its HTML (with
// common.js injected) at the ui:// URI with the MCP App MIME type. The final
// HTML is precomputed once at registration.
func (s *Server) registerAppResource(spec appSpec) {
	registerUIResource(s, spec.ResourceURI, spec.ResourceName, spec.ResourceDescription, spec.HTML)
}

// registerUIResource serves an HTML body at a ui:// URI with the MCP App
// MIME type. The HTML's appCommonMarker (if present) is replaced with
// common.js once at registration so the per-request handler stays cheap.
//
// Factored out of registerAppResource so the typed-app path (see
// registerTypedAppTool) can reuse it without recreating the appSpec
// envelope. Pre-KLA-419 each typed app inlined the same 18 lines.
func registerUIResource(s *Server, uri, name, description, htmlBody string) {
	html := renderAppHTML(htmlBody)
	s.mcpServer.AddResource(
		&mcp.Resource{
			URI:         uri,
			Name:        name,
			Description: description,
			MIMEType:    mcpAppMIMEType,
		},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{
					URI:      uri,
					MIMEType: mcpAppMIMEType,
					Text:     html,
				}},
			}, nil
		},
	)
}

// typedAppSpec mirrors appSpec for MCP Apps whose tool accepts typed
// input parameters (e.g. user_view, device_view, insights_view). The
// generic In type parameter is what the SDK uses to auto-derive the
// tool's JSON input schema.
//
// Cannot live in a []typedAppSpec slice the way appSpecs does, because
// each entry's In differs; instead, callers construct one literal per
// register{Foo}View() entry point and pass it to registerTypedAppTool.
type typedAppSpec[In any] struct {
	Name                string
	Description         string
	ResourceURI         string
	ResourceName        string
	ResourceDescription string
	HTML                string
	// Handler returns the JSON-serializable payload pushed to the app as
	// the initial tool result. The wrapper marshals it via jsonResult
	// and surfaces errors via errorResult(spec.Name + ": <err>") so
	// every typed app reports its tool name on failure.
	Handler func(ctx context.Context, args In) (any, error)
}

// registerTypedAppTool is the typed-input counterpart to registerAppTool.
// Each typed app shrinks from ~40 lines of inline registration to a
// single typedAppSpec literal + this call. Future refactors of the
// wrap-data-fetch / errorResult / jsonResult shape (e.g. swapping in a
// wrapped-error pattern) become one site instead of one per app.
func registerTypedAppTool[In any](s *Server, spec typedAppSpec[In]) {
	meta := mcp.Meta{
		"ui":             map[string]any{"resourceUri": spec.ResourceURI},
		"ui/resourceUri": spec.ResourceURI, // legacy key for older hosts
	}
	addToolWithMetaTyped(s, spec.Name, spec.Description, meta,
		func(ctx context.Context, req *mcp.CallToolRequest, args In) (*mcp.CallToolResult, any, error) {
			data, err := spec.Handler(ctx, args)
			if err != nil {
				return errorResult(fmt.Sprintf("%s: %v", spec.Name, err)), nil, nil
			}
			res, err := jsonResult(data)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)
	registerUIResource(s, spec.ResourceURI, spec.ResourceName, spec.ResourceDescription, spec.HTML)
}

// --- Dashboard data fetching and aggregation ---

type dashboardData struct {
	Users     *userStats     `json:"users"`
	Devices   *deviceStats   `json:"devices"`
	Resources *resourceStats `json:"resources"`
	Events    *eventStats    `json:"events"`
	Timestamp string         `json:"timestamp"`
	Warnings  []string       `json:"warnings,omitempty"`
}

type userStats struct {
	Total         int     `json:"total"`
	Active        int     `json:"active"`
	Suspended     int     `json:"suspended"`
	Locked        int     `json:"locked"`
	MFAEnabled    int     `json:"mfa_enabled"`
	MFAPercentage float64 `json:"mfa_percentage"`
}

type deviceStats struct {
	Total        int            `json:"total"`
	OSBreakdown  map[string]int `json:"os_breakdown"`
	Connectivity connectivity   `json:"connectivity"`
}

type connectivity struct {
	Online  int `json:"online"`
	Recent  int `json:"recent"`
	Stale   int `json:"stale"`
	Offline int `json:"offline"`
}

type resourceStats struct {
	UserGroups   int `json:"user_groups"`
	DeviceGroups int `json:"device_groups"`
	Commands     int `json:"commands"`
	Policies     int `json:"policies"`
	Applications int `json:"applications"`
}

type eventStats struct {
	Last24h int `json:"last_24h"`
}

// fetchDashboardData makes parallel API calls and aggregates results.
func fetchDashboardData(ctx context.Context) (*dashboardData, error) {
	var (
		mu   sync.Mutex
		data dashboardData
		errs []string
	)
	data.Timestamp = nowFunc().UTC().Format(time.RFC3339)
	data.Resources = &resourceStats{} // shared across goroutines; fields set individually under mu

	var wg sync.WaitGroup

	// V1: users
	wg.Add(1)
	go func() {
		defer wg.Done()
		client, err := newV1ClientFunc()
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Sprintf("creating V1 client for users: %v", err))
			mu.Unlock()
			return
		}
		result, err := client.ListAll(ctx, "/systemusers", api.ListOptions{})
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Sprintf("listing users: %v", err))
			mu.Unlock()
			return
		}
		stats := aggregateUserStats(result.Data)
		mu.Lock()
		data.Users = stats
		mu.Unlock()
	}()

	// V1: devices
	wg.Add(1)
	go func() {
		defer wg.Done()
		client, err := newV1ClientFunc()
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Sprintf("creating V1 client for devices: %v", err))
			mu.Unlock()
			return
		}
		result, err := client.ListAll(ctx, "/systems", api.ListOptions{})
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Sprintf("listing devices: %v", err))
			mu.Unlock()
			return
		}
		stats := aggregateDeviceStats(result.Data, nowFunc())
		mu.Lock()
		data.Devices = stats
		mu.Unlock()
	}()

	// V2: resource counts (groups, policies)
	wg.Add(1)
	go func() {
		defer wg.Done()
		client, err := newV2ClientFunc()
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Sprintf("creating V2 client: %v", err))
			mu.Unlock()
			return
		}
		var ugCount, dgCount, polCount int

		ug, err := client.ListAll(ctx, "/usergroups", api.V2ListOptions{})
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Sprintf("listing user groups: %v", err))
			mu.Unlock()
		} else {
			ugCount = len(ug.Data)
		}
		dg, err := client.ListAll(ctx, "/systemgroups", api.V2ListOptions{})
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Sprintf("listing device groups: %v", err))
			mu.Unlock()
		} else {
			dgCount = len(dg.Data)
		}
		pol, err := client.ListAll(ctx, "/policies", api.V2ListOptions{})
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Sprintf("listing policies: %v", err))
			mu.Unlock()
		} else {
			polCount = len(pol.Data)
		}

		mu.Lock()
		data.Resources.UserGroups = ugCount
		data.Resources.DeviceGroups = dgCount
		data.Resources.Policies = polCount
		mu.Unlock()
	}()

	// V1: commands + applications counts
	wg.Add(1)
	go func() {
		defer wg.Done()
		client, err := newV1ClientFunc()
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Sprintf("creating V1 client for counts: %v", err))
			mu.Unlock()
			return
		}
		var cmds, apps int
		// Limit: 1 — we only need TotalCount from the first page, not all records.
		cmdResult, err := client.ListAll(ctx, "/commands", api.ListOptions{Limit: 1})
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Sprintf("listing commands: %v", err))
			mu.Unlock()
		} else {
			cmds = cmdResult.TotalCount
		}
		appResult, err := client.ListAll(ctx, "/applications", api.ListOptions{Limit: 1})
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Sprintf("listing applications: %v", err))
			mu.Unlock()
		} else {
			apps = appResult.TotalCount
		}

		mu.Lock()
		data.Resources.Commands = cmds
		data.Resources.Applications = apps
		mu.Unlock()
	}()

	// Insights: event count (last 24h)
	wg.Add(1)
	go func() {
		defer wg.Done()
		client, err := newInsightsClientFunc()
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Sprintf("creating Insights client: %v", err))
			mu.Unlock()
			return
		}
		now := nowFunc().UTC()
		query := api.InsightsQuery{
			Service:   "all",
			StartTime: now.Add(-24 * time.Hour).Format(time.RFC3339),
			EndTime:   now.Format(time.RFC3339),
		}
		count, err := client.CountEvents(ctx, query)
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Sprintf("counting events: %v", err))
			mu.Unlock()
			return
		}
		mu.Lock()
		data.Events = &eventStats{Last24h: count}
		mu.Unlock()
	}()

	wg.Wait()

	// Fill nil sections with zero-value structs so JSON output is consistent.
	if data.Users == nil {
		data.Users = &userStats{}
	}
	if data.Devices == nil {
		data.Devices = &deviceStats{OSBreakdown: map[string]int{}}
	}
	if data.Events == nil {
		data.Events = &eventStats{}
	}

	if len(errs) > 0 && data.Users.Total == 0 && data.Devices.Total == 0 {
		return nil, fmt.Errorf("all API calls failed: %s", strings.Join(errs, "; "))
	}
	if len(errs) > 0 {
		data.Warnings = errs
	}

	return &data, nil
}

// aggregateUserStats computes user status and MFA counts from raw JSON.
// Mirrors internal/tui/screen/dashboard.go:aggregateUsers.
func aggregateUserStats(data []json.RawMessage) *userStats {
	s := &userStats{}
	for _, raw := range data {
		var u struct {
			Activated     bool `json:"activated"`
			Suspended     bool `json:"suspended"`
			AccountLocked bool `json:"account_locked"`
			TOTPEnabled   bool `json:"totp_enabled"`
		}
		if err := json.Unmarshal(raw, &u); err != nil {
			continue
		}
		s.Total++
		switch {
		case u.AccountLocked:
			s.Locked++
		case u.Suspended:
			s.Suspended++
		default:
			s.Active++
		}
		if u.TOTPEnabled {
			s.MFAEnabled++
		}
	}
	if s.Total > 0 {
		s.MFAPercentage = float64(s.MFAEnabled) / float64(s.Total) * 100
	}
	return s
}

// aggregateDeviceStats computes OS distribution and connectivity buckets.
// Mirrors internal/tui/screen/dashboard.go:aggregateDevices.
func aggregateDeviceStats(data []json.RawMessage, now time.Time) *deviceStats {
	s := &deviceStats{
		OSBreakdown: make(map[string]int),
	}
	for _, raw := range data {
		var dev struct {
			OS          string `json:"os"`
			LastContact string `json:"lastContact"`
		}
		if err := json.Unmarshal(raw, &dev); err != nil {
			continue
		}
		s.Total++

		osName := dev.OS
		if osName == "" {
			osName = "Unknown"
		}
		s.OSBreakdown[osName]++

		if dev.LastContact != "" {
			if t, err := time.Parse(time.RFC3339, dev.LastContact); err == nil {
				age := now.Sub(t)
				switch {
				case age < time.Hour:
					s.Connectivity.Online++
				case age < 24*time.Hour:
					s.Connectivity.Recent++
				case age < 7*24*time.Hour:
					s.Connectivity.Stale++
				default:
					s.Connectivity.Offline++
				}
				continue
			}
		}
		s.Connectivity.Offline++
	}
	return s
}
