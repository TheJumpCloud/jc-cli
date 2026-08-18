// Package report holds the JumpCloud Reports wire-contract helpers shared by
// the CLI (internal/cmd) and the MCP server (internal/mcp), so the two surfaces
// can never drift on the per-family list/get envelopes.
//
// The Reports area (GET /api/v2/reports/*) spans several families, each with
// its own list envelope key. Verified live against org 5ec71e8e96bfda0611fc6c5b
// on 2026-08-18 (KLA-485 reports probe): every list endpoint returns
// {<key>: [...], totalCount}. Only the built-in jumpcloud templates were
// populated on the tenant (20); the custom / custom-reports / saved / scheduled
// families exist but were empty, so this package + the `jc reports` group cover
// the read AND write operations. The writable families (custom, builder,
// scheduled) create/update via a {<GetKey>: obj} body — verified live on
// 2026-08-18 by cloning a built-in template's searchRequest: create returns the
// bare object, get wraps it in {<GetKey>}, delete returns empty. Scheduled
// reports require at least one recipient ({channelType,value}) and a
// scheduleFrequency ∈ {daily,weekly,monthly,quarterly}; their ids are UUIDs
// (not 24-hex), so LooksLikeID also accepts the UUID form. POST /reports/export
// takes {exportType,notifyByEmail,reportName,searchRequest} and returns a
// presigned {downloadUrl}.
//
// Only jumpcloud's single-get envelope ({reportTemplate}) was confirmed for the
// read-only families; their GetKey follows the singular-of-ListKey convention
// and Unwrap falls back to the raw body if the key is absent.
package report

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
)

// Family describes one report family under /reports.
type Family struct {
	// Name is the CLI subcommand / MCP suffix (e.g. "templates").
	Name string
	// ListEndpoint is the v2 list path (e.g. "/reports/jumpcloud").
	ListEndpoint string
	// ListKey is the list-response envelope key (e.g. "reportTemplates").
	ListKey string
	// GetKey is the single-get envelope key (singular; only "reportTemplate"
	// is live-confirmed, the rest follow convention with graceful fallback).
	GetKey string
	// IDField is the JSON field carrying the item id.
	IDField string
	// NameField is the JSON field used for name resolution.
	NameField string
	// DefaultFields is the default output column subset.
	DefaultFields []string
	// Writable reports whether the family supports create/update/delete. GetKey
	// doubles as the create/update request-body envelope key.
	Writable bool
}

// BodyKey is the request-body envelope key for create/update (same as GetKey).
func (f Family) BodyKey() string { return f.GetKey }

// WrapBody wraps a report object in the family's {<key>: obj} request envelope.
func (f Family) WrapBody(obj json.RawMessage) map[string]json.RawMessage {
	return map[string]json.RawMessage{f.GetKey: obj}
}

// Families is the set of covered report families, keyed by Name.
var Families = map[string]Family{
	"templates": {
		Name:          "templates",
		ListEndpoint:  "/reports/jumpcloud",
		ListKey:       "reportTemplates",
		GetKey:        "reportTemplate",
		IDField:       "id",
		NameField:     "displayName",
		DefaultFields: []string{"id", "displayName", "description"},
	},
	"saved": {
		Name:          "saved",
		ListEndpoint:  "/reports/saved-reports",
		ListKey:       "savedReports",
		GetKey:        "savedReport",
		IDField:       "id",
		NameField:     "displayName",
		DefaultFields: []string{"id", "reportType"},
	},
	"custom": {
		Name:          "custom",
		ListEndpoint:  "/reports/custom",
		ListKey:       "reportViews",
		GetKey:        "reportView",
		IDField:       "id",
		NameField:     "displayName",
		DefaultFields: []string{"id", "displayName", "description", "updatedAt"},
		Writable:      true,
	},
	"builder": {
		Name:          "builder",
		ListEndpoint:  "/reports/custom-reports",
		ListKey:       "customReports",
		GetKey:        "customReport",
		IDField:       "id",
		NameField:     "displayName",
		DefaultFields: []string{"id", "displayName", "description", "primaryObject", "updatedAt"},
		Writable:      true,
	},
	"scheduled": {
		Name:          "scheduled",
		ListEndpoint:  "/reports/scheduled",
		ListKey:       "scheduledReports",
		GetKey:        "scheduledReport",
		IDField:       "id",
		NameField:     "displayName",
		DefaultFields: []string{"id", "displayName", "exportType", "cronExpression", "isActive", "lastRunStatus", "nextRunAt"},
		Writable:      true,
	},
}

// RunsListKey / RunsGetKey / RunsDefaultFields describe the scheduled-report run
// history (GET /reports/scheduled/runs, /reports/scheduled/runs/{id}, and
// /reports/scheduled/{id}/runs), which is a sub-resource of the scheduled family.
const (
	RunsListKey = "runs"
	RunsGetKey  = "run"
)

// RunsDefaultFields is the default output column subset for run records.
var RunsDefaultFields = []string{"id", "displayName", "status", "startedAt", "completedAt", "rowCount", "artifactUrl"}

// FamilyNames returns the covered family names in stable sorted order.
func FamilyNames() []string {
	names := make([]string, 0, len(Families))
	for n := range Families {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Unwrap returns raw[key] when the response is a single-key envelope. Falls back
// to raw untouched if the key is absent or the body isn't an object.
func Unwrap(raw json.RawMessage, key string) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	if inner, ok := obj[key]; ok {
		return inner
	}
	return raw
}

// idPattern matches a 24-hex JumpCloud ObjectId; uuidPattern matches the UUID
// form scheduled-report ids use.
var (
	idPattern   = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// LooksLikeID reports whether s is already a report id (24-hex ObjectId or a
// UUID). Report families mix the two — scheduled ids are UUIDs — so a plain
// name-resolver's 24-hex-only check would fail on a scheduled id.
func LooksLikeID(s string) bool {
	return idPattern.MatchString(s) || uuidPattern.MatchString(s)
}

// ParseReportFile decodes a --report-file payload into the object to place in
// the {<GetKey>} envelope. Accepts either a bare report object or one already
// wrapped in {"<GetKey>": …}, so a body captured from `get` round-trips.
func (f Family) ParseReportFile(raw []byte) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("parsing report JSON: %w", err)
	}
	if inner, ok := obj[f.GetKey]; ok {
		return inner, nil
	}
	return json.RawMessage(raw), nil
}

// ExportBody builds the POST /reports/export request body. exportType defaults
// to "csv" when empty.
func ExportBody(reportName, exportType string, notifyByEmail bool, searchRequest json.RawMessage) map[string]any {
	if exportType == "" {
		exportType = "csv"
	}
	return map[string]any{
		"reportName":    reportName,
		"exportType":    exportType,
		"notifyByEmail": notifyByEmail,
		"searchRequest": searchRequest,
	}
}
