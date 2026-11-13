// Package accesscert is the shared contract for JumpCloud's Access
// Certification API area (/api/v2/access-certification/*), used by the CLI and
// the MCP tools so the two cannot drift.
//
// This package is READ-ONLY by construction: it only knows how to issue GETs
// through the Getter interface, which has no write method. The write
// operations of this area are deliberately not built here.
package accesscert

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Base is the path prefix of the area, relative to the v2 base URL.
const Base = "/access-certification"

const (
	SettingsEndpoint   = Base + "/settings"
	CampaignsEndpoint  = Base + "/campaigns"
	RunsEndpoint       = Base + "/campaign-runs"
	AdminQueueEndpoint = Base + "/admin-queue"
)

// MaxPageSize is the largest limit the API accepts.
const MaxPageSize = 100

// CampaignEndpoint addresses one campaign template.
func CampaignEndpoint(id string) string { return CampaignsEndpoint + "/" + id }

// RunEndpoint addresses one campaign run.
func RunEndpoint(id string) string { return RunsEndpoint + "/" + id }

// ReviewItemsEndpoint addresses the review items of one run.
func ReviewItemsEndpoint(runID string) string { return RunEndpoint(runID) + "/review-items" }

// AuditReportEndpoint addresses the audit report of one run.
func AuditReportEndpoint(runID string) string { return RunEndpoint(runID) + "/audit-report" }

// Getter is the only HTTP capability this package needs: a GET.
type Getter interface {
	Get(ctx context.Context, endpoint string) (json.RawMessage, error)
}

var objectIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// IsObjectID reports whether s is a 24-hex object id.
func IsObjectID(s string) bool { return objectIDPattern.MatchString(s) }

// ErrNotObjectID explains a malformed id before it reaches the API.
func ErrNotObjectID(kind, s string) error {
	return fmt.Errorf("%q is not a valid %s id — expected a 24-character hex object id "+
		"(there is no lookup by name)", s, kind)
}

// Statuses are the values --status / status accept for campaign runs.
var Statuses = []string{"active", "closed", "canceled", "all"}

// StatusFilters returns the status filter expressions to query for a status
// value. "" means no status filter: the API then returns ACTIVE runs only.
// "all" yields one entry per status, so the caller makes one paged query each.
func StatusFilters(status string) ([]string, error) {
	switch status {
	case "", "active":
		return []string{""}, nil
	case "closed", "canceled":
		return []string{"status:eq:" + status}, nil
	case "all":
		return []string{"", "status:eq:closed", "status:eq:canceled"}, nil
	}
	return nil, fmt.Errorf("invalid status %q — allowed values: %s", status, strings.Join(Statuses, ", "))
}

// Query describes one list request.
type Query struct {
	Sort     string
	Filters  []string
	Metadata bool
	// Limit caps the total rows returned; 0 means everything.
	Limit int
}

// Page is the result of a paged list.
type Page struct {
	Results    []json.RawMessage
	TotalCount int
	Metadata   map[string][]json.RawMessage
}

type envelope struct {
	Results    []json.RawMessage            `json:"results"`
	TotalCount *int                         `json:"totalCount"`
	Metadata   map[string][]json.RawMessage `json:"metadata"`
}

func buildPath(endpoint string, q Query, skip, limit int) string {
	v := url.Values{}
	v.Set("skip", strconv.Itoa(skip))
	v.Set("limit", strconv.Itoa(limit))
	if q.Sort != "" {
		v.Set("sort", q.Sort)
	}
	for _, f := range q.Filters {
		if f != "" {
			v.Add("filter", f)
		}
	}
	if q.Metadata {
		v.Set("metadata", "true")
	}
	return endpoint + "?" + v.Encode()
}

// ListAll pages through a list endpoint with skip/limit (at most MaxPageSize
// per request) until totalCount is reached or q.Limit rows were collected.
func ListAll(ctx context.Context, g Getter, endpoint string, q Query) (*Page, error) {
	page := &Page{}
	for {
		size := MaxPageSize
		if q.Limit > 0 && q.Limit-len(page.Results) < size {
			size = q.Limit - len(page.Results)
		}
		raw, err := g.Get(ctx, buildPath(endpoint, q, len(page.Results), size))
		if err != nil {
			return nil, err
		}
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("unexpected response from %s: not a {results,totalCount} envelope: %w", endpoint, err)
		}
		page.Results = append(page.Results, env.Results...)
		if env.TotalCount != nil {
			page.TotalCount = *env.TotalCount
		}
		page.mergeMetadata(env.Metadata)
		if len(env.Results) == 0 || (q.Limit > 0 && len(page.Results) >= q.Limit) {
			break
		}
		if env.TotalCount != nil && len(page.Results) >= *env.TotalCount {
			break
		}
		if env.TotalCount == nil && len(env.Results) < size {
			break
		}
	}
	if page.TotalCount < len(page.Results) {
		page.TotalCount = len(page.Results)
	}
	return page, nil
}

// ListRuns lists campaign runs for a status value ("all" concatenates one
// paged query per status). q.Limit caps the combined total.
func ListRuns(ctx context.Context, g Getter, status string, q Query) (*Page, error) {
	filters, err := StatusFilters(status)
	if err != nil {
		return nil, err
	}
	out := &Page{}
	total := 0
	for _, sf := range filters {
		sub := q
		sub.Filters = append(append([]string{}, q.Filters...), sf)
		if q.Limit > 0 {
			sub.Limit = q.Limit - len(out.Results)
			if sub.Limit <= 0 {
				break
			}
		}
		p, err := ListAll(ctx, g, RunsEndpoint, sub)
		if err != nil {
			return nil, err
		}
		out.Results = append(out.Results, p.Results...)
		total += p.TotalCount
		out.mergeMetadata(p.Metadata)
	}
	out.TotalCount = total
	return out, nil
}

func (p *Page) mergeMetadata(m map[string][]json.RawMessage) {
	if len(m) == 0 {
		return
	}
	if p.Metadata == nil {
		p.Metadata = map[string][]json.RawMessage{}
	}
	for k, vals := range m {
		// Keep every key the server sends, so an empty facet marshals as [].
		if p.Metadata[k] == nil {
			p.Metadata[k] = []json.RawMessage{}
		}
		seen := map[string]bool{}
		for _, e := range p.Metadata[k] {
			seen[string(e)] = true
		}
		for _, e := range vals {
			if !seen[string(e)] {
				seen[string(e)] = true
				p.Metadata[k] = append(p.Metadata[k], e)
			}
		}
	}
}

// Envelope renders the page as the API's own list envelope.
func (p *Page) Envelope() (json.RawMessage, error) {
	res := p.Results
	if res == nil {
		res = []json.RawMessage{}
	}
	out := map[string]any{"results": res, "totalCount": p.TotalCount}
	if p.Metadata != nil {
		out["metadata"] = p.Metadata
	}
	return json.Marshal(out)
}

// StripURLs removes the pre-signed "url" from every artifact of an audit
// report, keeping each artifact's other fields (expiresAt).
func StripURLs(raw json.RawMessage) (json.RawMessage, error) {
	var report map[string]json.RawMessage
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, fmt.Errorf("audit report is not a JSON object: %w", err)
	}
	if a, ok := report["artifacts"]; ok {
		var artifacts map[string]map[string]json.RawMessage
		if err := json.Unmarshal(a, &artifacts); err != nil {
			return nil, fmt.Errorf("audit report artifacts is not an object keyed by format: %w", err)
		}
		for _, art := range artifacts {
			delete(art, "url")
		}
		b, err := json.Marshal(artifacts)
		if err != nil {
			return nil, err
		}
		report["artifacts"] = b
	}
	return json.Marshal(report)
}
