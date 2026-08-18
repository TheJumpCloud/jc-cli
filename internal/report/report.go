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
// the READ operations (list/get across all families + scheduled runs). Write
// operations (create/update/delete/trigger/export) are a tracked follow-up —
// their request bodies are large report definitions that could not be
// live-exercised on an empty tenant.
//
// Only jumpcloud's single-get envelope ({reportTemplate}) was confirmed live;
// the other families' GetKey follows the singular-of-ListKey convention and
// Unwrap falls back to the raw body if the key is absent.
package report

import (
	"encoding/json"
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
	},
	"builder": {
		Name:          "builder",
		ListEndpoint:  "/reports/custom-reports",
		ListKey:       "customReports",
		GetKey:        "customReport",
		IDField:       "id",
		NameField:     "displayName",
		DefaultFields: []string{"id", "displayName", "description", "primaryObject", "updatedAt"},
	},
	"scheduled": {
		Name:          "scheduled",
		ListEndpoint:  "/reports/scheduled",
		ListKey:       "scheduledReports",
		GetKey:        "scheduledReport",
		IDField:       "id",
		NameField:     "displayName",
		DefaultFields: []string{"id", "displayName", "exportType", "cronExpression", "isActive", "lastRunStatus", "nextRunAt"},
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
