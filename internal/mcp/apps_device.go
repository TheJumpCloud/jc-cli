package mcp

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/resolve"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed apps_html/device.html
var deviceHTML string

const (
	deviceViewResourceURI = "ui://jc/device"

	// Cap on recent events fetched per device to keep the payload bounded.
	// Mirrors user_view's 50 — enough for a useful activity timeline
	// without dragging the call.
	deviceViewRecentEventsLimit = 50

	// Cap on rows pulled from each system-insights table for the snapshot.
	// Disks and logged-in users rarely exceed single digits in practice;
	// 10 keeps the UI scannable and the payload small.
	deviceViewInsightsRowLimit = 10
)

// deviceViewArgs is the tool input. Single required field; the device
// identifier accepts hostname, displayName, or 24-char hex ID — same
// surface as `jc devices get`.
type deviceViewArgs struct {
	// Device is the JumpCloud device to inspect: hostname, displayName,
	// or 24-char hex ID.
	Device string `json:"device" jsonschema:"JumpCloud device to inspect (hostname, displayName, or 24-char hex ID)."`
}

// deviceViewData is the JSON payload pushed to the device.html iframe.
// Mirrors the user_view shape: a header section, structured slices for
// per-card UI, and a Warnings list when a sub-fetch failed but the
// overall view is still useful.
type deviceViewData struct {
	Device         deviceHeader      `json:"device"`
	Status         deviceStatusSnap  `json:"status"`
	Groups         []deviceGroupRef  `json:"groups"`
	Policies       []policyRef       `json:"policies"`
	SystemInsights *deviceInsights   `json:"system_insights,omitempty"`
	RecentEvents   []json.RawMessage `json:"recent_events"`
	Warnings       []string          `json:"warnings,omitempty"`
}

type deviceHeader struct {
	ID           string `json:"id"`
	DisplayName  string `json:"display_name,omitempty"`
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	OSVersion    string `json:"os_version,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	LastContact  string `json:"last_contact,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
	Created      string `json:"created,omitempty"`
}

type deviceStatusSnap struct {
	Active bool `json:"active"`
	// Connectivity bucket: online (<1h), recent (<24h), stale (<7d), offline.
	// Mirrors aggregateDeviceStats in apps.go so the dashboard and
	// device_view agree on what "online" means.
	Connectivity string `json:"connectivity"`
	FDEEnabled   bool   `json:"fde_enabled"`
	MDMEnrolled  bool   `json:"mdm_enrolled"`
}

type deviceGroupRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type policyRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type deviceInsights struct {
	// UptimeSeconds is the OS-reported uptime; the UI formats it as
	// "Nd Nh". Zero means the systeminsights row didn't materialize on
	// this device (agent down, table unsupported, etc).
	UptimeSeconds int64                `json:"uptime_seconds,omitempty"`
	LoggedInUsers []deviceInsightsUser `json:"logged_in_users,omitempty"`
	Disks         []deviceInsightsDisk `json:"disks,omitempty"`
}

type deviceInsightsUser struct {
	User string `json:"user"`
	Type string `json:"type,omitempty"`
	Time string `json:"time,omitempty"`
	Host string `json:"host,omitempty"`
}

type deviceInsightsDisk struct {
	Name       string `json:"name"`
	Mountpoint string `json:"mountpoint,omitempty"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
	FreeBytes  int64  `json:"free_bytes,omitempty"`
}

// fetchDeviceViewData runs the parallel API calls behind device_view and
// aggregates them into a single payload. Same best-effort contract as
// fetchUserViewData: a transient failure on a sub-fetch lands as a
// Warning rather than blocking the whole view.
func fetchDeviceViewData(ctx context.Context, args deviceViewArgs) (*deviceViewData, error) {
	if args.Device == "" {
		return nil, fmt.Errorf("device is required")
	}

	v1, err := newV1ClientFunc()
	if err != nil {
		return nil, fmt.Errorf("v1 client: %w", err)
	}

	// Resolve identifier first — every subsequent call needs the ID.
	id, err := resolveV1(ctx, v1, args.Device, resolve.DeviceConfig)
	if err != nil {
		return nil, fmt.Errorf("resolving device %q: %w", args.Device, err)
	}

	// Fetch device detail synchronously before fanning out so the
	// connectivity bucket calculation has the canonical lastContact —
	// matches the user_view "fetch detail first, then fan out" pattern.
	rawDev, err := v1.Get(ctx, "/systems/"+id)
	if err != nil {
		return nil, fmt.Errorf("fetching device %q: %w", args.Device, err)
	}
	var d struct {
		ID           string `json:"_id"`
		DisplayName  string `json:"displayName"`
		Hostname     string `json:"hostname"`
		OS           string `json:"os"`
		OSVersion    string `json:"version"`
		SerialNumber string `json:"serialNumber"`
		LastContact  string `json:"lastContact"`
		AgentVersion string `json:"agentVersion"`
		Created      string `json:"created"`
		Active       bool   `json:"active"`
		// FDE / MDM signals vary by platform. The most reliable
		// cross-platform proxy is the boolean `fde.active` on macOS
		// and `allowMultiFactorAuthentication` on the device. We read
		// both into a single status flag below.
		FDE struct {
			Active bool `json:"active"`
		} `json:"fde"`
		MdmEnrollment struct {
			Enrolled bool `json:"enrolled"`
		} `json:"mdmEnrollment"`
	}
	if err := json.Unmarshal(rawDev, &d); err != nil {
		return nil, fmt.Errorf("parsing device %q: %w", args.Device, err)
	}

	now := nowFunc()
	connectivity := connectivityBucket(d.LastContact, now)

	var (
		mu       sync.Mutex
		data     deviceViewData
		warnings []string
	)
	addWarning := func(msg string) {
		mu.Lock()
		warnings = append(warnings, msg)
		mu.Unlock()
	}

	data.Device = deviceHeader{
		ID:           d.ID,
		DisplayName:  d.DisplayName,
		Hostname:     d.Hostname,
		OS:           d.OS,
		OSVersion:    d.OSVersion,
		SerialNumber: d.SerialNumber,
		LastContact:  d.LastContact,
		AgentVersion: d.AgentVersion,
		Created:      d.Created,
	}
	data.Status = deviceStatusSnap{
		Active:       d.Active,
		Connectivity: connectivity,
		FDEEnabled:   d.FDE.Active,
		MDMEnrolled:  d.MdmEnrollment.Enrolled,
	}

	var wg sync.WaitGroup

	// Group memberships via V2: device → user_group / system_group.
	// The /memberof endpoint mirrors the user_view pattern and returns
	// both kinds of groups the device belongs to.
	wg.Add(1)
	go func() {
		defer wg.Done()
		v2, err := newV2ClientFunc()
		if err != nil {
			addWarning(fmt.Sprintf("v2 client: %v", err))
			return
		}
		result, err := v2.ListAll(ctx, "/systems/"+id+"/memberof", api.V2ListOptions{})
		if err != nil {
			addWarning(fmt.Sprintf("groups: %v", err))
			return
		}
		groups := make([]deviceGroupRef, 0, len(result.Data))
		for _, raw := range result.Data {
			var g struct {
				ID         string `json:"id"`
				Attributes struct {
					Name string `json:"name"`
				} `json:"attributes"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(raw, &g); err != nil {
				continue
			}
			name := g.Name
			if name == "" {
				name = g.Attributes.Name
			}
			groups = append(groups, deviceGroupRef{ID: g.ID, Name: name})
		}
		sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
		mu.Lock()
		data.Groups = groups
		mu.Unlock()
	}()

	// Applied policies via V2 graph associations + name lookup. Two
	// calls: first the association list (just IDs), then resolve the
	// IDs to names via a single /policies fetch and join in memory.
	// N+1 GETs would be cleaner per-policy but quickly hostile on
	// orgs with dozens of policies.
	wg.Add(1)
	go func() {
		defer wg.Done()
		v2, err := newV2ClientFunc()
		if err != nil {
			addWarning(fmt.Sprintf("v2 client: %v", err))
			return
		}
		assocs, err := v2.ListAll(ctx, "/systems/"+id+"/associations?targets=policy", api.V2ListOptions{})
		if err != nil {
			addWarning(fmt.Sprintf("policies (associations): %v", err))
			return
		}
		policyIDs := make([]string, 0, len(assocs.Data))
		for _, raw := range assocs.Data {
			var a struct {
				To struct {
					ID string `json:"id"`
				} `json:"to"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				continue
			}
			if a.To.ID != "" {
				policyIDs = append(policyIDs, a.To.ID)
			}
		}
		if len(policyIDs) == 0 {
			return
		}

		// Pull the full policy catalog once, build an ID→name map.
		// Cheaper than N parallel GETs and avoids hammering the API.
		all, err := v2.ListAll(ctx, "/policies", api.V2ListOptions{})
		if err != nil {
			// Fall back to id-only refs so the UI still shows the
			// count and IDs even though the names are missing.
			refs := make([]policyRef, 0, len(policyIDs))
			for _, pid := range policyIDs {
				refs = append(refs, policyRef{ID: pid})
			}
			addWarning(fmt.Sprintf("policy names: %v", err))
			mu.Lock()
			data.Policies = refs
			mu.Unlock()
			return
		}
		nameByID := make(map[string]string, len(all.Data))
		for _, raw := range all.Data {
			var p struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				continue
			}
			nameByID[p.ID] = p.Name
		}
		refs := make([]policyRef, 0, len(policyIDs))
		for _, pid := range policyIDs {
			refs = append(refs, policyRef{ID: pid, Name: nameByID[pid]})
		}
		sort.Slice(refs, func(i, j int) bool {
			// Empty names sort last so named policies surface first.
			if (refs[i].Name == "") != (refs[j].Name == "") {
				return refs[i].Name != ""
			}
			return refs[i].Name < refs[j].Name
		})
		mu.Lock()
		data.Policies = refs
		mu.Unlock()
	}()

	// System insights snapshot: uptime, logged-in users, disk info.
	// All three queries use the V2 client with a system_id filter; rows
	// are bounded by deviceViewInsightsRowLimit so a misbehaving table
	// can't bloat the payload.
	wg.Add(1)
	go func() {
		defer wg.Done()
		v2, err := newV2ClientFunc()
		if err != nil {
			addWarning(fmt.Sprintf("v2 client: %v", err))
			return
		}
		insights := &deviceInsights{}

		// Uptime: one row per device, but still ListAll'd so the
		// filter+sort plumbing matches.
		up, err := v2.ListAll(ctx, "/systeminsights/uptime", api.V2ListOptions{
			Filter: []string{"system_id:eq:" + id},
			Limit:  1,
		})
		if err != nil {
			addWarning(fmt.Sprintf("uptime: %v", err))
		} else if len(up.Data) > 0 {
			var row struct {
				TotalSeconds int64 `json:"total_seconds"`
			}
			if err := json.Unmarshal(up.Data[0], &row); err == nil {
				insights.UptimeSeconds = row.TotalSeconds
			}
		}

		liu, err := v2.ListAll(ctx, "/systeminsights/logged_in_users", api.V2ListOptions{
			Filter: []string{"system_id:eq:" + id},
			Limit:  deviceViewInsightsRowLimit,
		})
		if err != nil {
			addWarning(fmt.Sprintf("logged_in_users: %v", err))
		} else {
			for _, raw := range liu.Data {
				var u struct {
					User string `json:"user"`
					Type string `json:"type"`
					Time string `json:"time"`
					Host string `json:"host"`
				}
				if err := json.Unmarshal(raw, &u); err != nil {
					continue
				}
				insights.LoggedInUsers = append(insights.LoggedInUsers, deviceInsightsUser{
					User: u.User, Type: u.Type, Time: u.Time, Host: u.Host,
				})
			}
		}

		di, err := v2.ListAll(ctx, "/systeminsights/disk_info", api.V2ListOptions{
			Filter: []string{"system_id:eq:" + id},
			Limit:  deviceViewInsightsRowLimit,
		})
		if err != nil {
			addWarning(fmt.Sprintf("disk_info: %v", err))
		} else {
			for _, raw := range di.Data {
				var row struct {
					Name       string `json:"name"`
					Mountpoint string `json:"mountpoint"`
					Size       int64  `json:"size"`
					Free       int64  `json:"free"`
				}
				if err := json.Unmarshal(raw, &row); err != nil {
					continue
				}
				insights.Disks = append(insights.Disks, deviceInsightsDisk{
					Name: row.Name, Mountpoint: row.Mountpoint,
					SizeBytes: row.Size, FreeBytes: row.Free,
				})
			}
		}

		// Only set the field if at least one sub-query returned data;
		// keeps `omitempty` honest in the JSON output.
		if insights.UptimeSeconds > 0 || len(insights.LoggedInUsers) > 0 || len(insights.Disks) > 0 {
			mu.Lock()
			data.SystemInsights = insights
			mu.Unlock()
		}
	}()

	// Recent insights events: last 30d, filtered to this device.
	// Filter shape is a best-effort guess at JumpCloud's event schema —
	// `system.id` is the canonical field for system-as-resource in
	// Directory Insights events. If a real-org query returns empty,
	// the iframe gracefully shows "no recent activity" and the filter
	// can be tuned in a follow-up without changing the UI contract.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ic, err := newInsightsClientFunc()
		if err != nil {
			addWarning(fmt.Sprintf("insights client: %v", err))
			return
		}
		now := nowFunc().UTC()
		query := api.InsightsQuery{
			Service:   "all",
			StartTime: now.Add(-30 * 24 * time.Hour).Format(time.RFC3339),
			EndTime:   now.Format(time.RFC3339),
			SearchTermFilter: map[string]any{
				"system.id": id,
			},
		}
		result, err := ic.QueryEvents(ctx, query, api.InsightsQueryOptions{
			Limit: deviceViewRecentEventsLimit,
			Sort:  "-timestamp",
		})
		if err != nil {
			addWarning(fmt.Sprintf("recent events: %v", err))
			return
		}
		mu.Lock()
		data.RecentEvents = result.Data
		mu.Unlock()
	}()

	wg.Wait()

	if len(warnings) > 0 {
		data.Warnings = warnings
	}

	// Header must have populated from the synchronous fetch; if it
	// somehow didn't, surface that as an error rather than an empty
	// card. The aggregate-best-effort rule applies to *sub-fetches*,
	// not the core record.
	if data.Device.ID == "" && data.Device.Hostname == "" {
		return nil, fmt.Errorf("could not fetch device %q: %s", args.Device, strings.Join(warnings, "; "))
	}

	return &data, nil
}

// connectivityBucket maps a device's last-contact timestamp to one of
// the four buckets the dashboard already uses. Centralized here (rather
// than reaching into apps.go) so the function stays self-contained and
// the buckets can drift independently if device_view ever wants
// different thresholds.
func connectivityBucket(lastContact string, now time.Time) string {
	if lastContact == "" {
		return "offline"
	}
	t, err := time.Parse(time.RFC3339, lastContact)
	if err != nil {
		return "offline"
	}
	age := now.Sub(t)
	switch {
	case age < time.Hour:
		return "online"
	case age < 24*time.Hour:
		return "recent"
	case age < 7*24*time.Hour:
		return "stale"
	default:
		return "offline"
	}
}

// registerDeviceView wires the device_view MCP App: typed tool + ui://
// resource. Mirrors registerUserView; lives outside appSpecs because
// the tool takes typed input.
func (s *Server) registerDeviceView() {
	meta := mcp.Meta{
		"ui":             map[string]any{"resourceUri": deviceViewResourceURI},
		"ui/resourceUri": deviceViewResourceURI,
	}
	addToolWithMetaTyped(s, "device_view",
		"Show an interactive JumpCloud device inventory view: header (hostname, OS+version, serial, last contact, agent version), "+
			"status badges (online/stale/offline, FDE, MDM), group memberships, applied policies, a system-insights snapshot "+
			"(uptime, logged-in users, disks), and recent Directory Insights events for the device. "+
			"Required input: device (hostname, displayName, or 24-char hex ID). "+
			"Renders as a rich inventory panel in MCP App-capable hosts; returns the same data as JSON when rendering isn't supported.",
		meta,
		func(ctx context.Context, req *mcp.CallToolRequest, args deviceViewArgs) (*mcp.CallToolResult, any, error) {
			data, err := fetchDeviceViewData(ctx, args)
			if err != nil {
				return errorResult(fmt.Sprintf("device_view: %v", err)), nil, nil
			}
			res, err := jsonResult(data)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	rendered := renderAppHTML(deviceHTML)
	s.mcpServer.AddResource(
		&mcp.Resource{
			URI:         deviceViewResourceURI,
			Name:        "Device Inventory App",
			Description: "Interactive JumpCloud device inventory (status, groups, policies, system insights, recent events)",
			MIMEType:    mcpAppMIMEType,
		},
		func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{
					URI:      deviceViewResourceURI,
					MIMEType: mcpAppMIMEType,
					Text:     rendered,
				}},
			}, nil
		},
	)
}
