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

// --- MCP App tool registration ---

// addToolWithMeta wraps mcp.AddTool with rate limiting, audit logging, and
// tool filtering — same as addTool but also sets Meta on the tool definition.
// This is used for MCP App tools that need _meta.ui.resourceUri.
func (s *Server) addToolWithMeta(name, description string, meta mcp.Meta, handler func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error)) {
	if !s.toolFilter.isAllowed(name) {
		return
	}

	tool := &mcp.Tool{
		Name:        name,
		Description: description,
		Meta:        meta,
	}

	wrappedHandler := func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
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

	mcp.AddTool(s.mcpServer, tool, wrappedHandler)
	s.toolNames = append(s.toolNames, name)
}

// --- MCP App registration entry points ---

// registerAppTools registers MCP App tools (tools with _meta.ui.resourceUri).
func (s *Server) registerAppTools() {
	s.addToolWithMeta(
		"dashboard_view",
		"Show an interactive JumpCloud organization dashboard with user/device counts, MFA adoption, device OS breakdown, resource counts, and recent event activity. Returns structured data that renders as a rich HTML dashboard in MCP App-capable hosts.",
		mcp.Meta{
			"ui":              map[string]any{"resourceUri": "ui://jc/dashboard"},
			"ui/resourceUri":  "ui://jc/dashboard", // legacy key for older hosts
		},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			data, err := fetchDashboardData(ctx)
			if err != nil {
				return errorResult(fmt.Sprintf("fetching dashboard data: %v", err)), nil, nil
			}
			res, err := jsonResult(data)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)
}

// registerAppResources registers MCP App UI resources (ui:// scheme).
func (s *Server) registerAppResources() {
	s.mcpServer.AddResource(
		&mcp.Resource{
			URI:         "ui://jc/dashboard",
			Name:        "Dashboard App",
			Description: "Interactive JumpCloud organization dashboard",
			MIMEType:    "text/html;profile=mcp-app",
		},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{
					URI:      "ui://jc/dashboard",
					MIMEType: "text/html;profile=mcp-app",
					Text:     dashboardHTML,
				}},
			}, nil
		},
	)
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
		cmdResult, err := client.ListAll(ctx, "/commands", api.ListOptions{})
		if err != nil {
			mu.Lock()
			errs = append(errs, fmt.Sprintf("listing commands: %v", err))
			mu.Unlock()
		} else {
			cmds = cmdResult.TotalCount
		}
		appResult, err := client.ListAll(ctx, "/applications", api.ListOptions{})
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
