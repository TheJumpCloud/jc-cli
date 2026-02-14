package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// InsightsBaseURL is the JumpCloud Directory Insights API base URL.
	InsightsBaseURL = "https://console.jumpcloud.com/insights/directory/v1"
)

// nowFunc is used to get the current time. Tests can override this.
var insightsNowFunc = time.Now

// InsightsClient is a JumpCloud Directory Insights API client.
// It handles POST-based event queries with automatic pagination.
type InsightsClient struct {
	*Client
}

// NewInsightsClient creates a new Insights API client using the currently configured API key.
func NewInsightsClient() (*InsightsClient, error) {
	c, err := NewClient()
	if err != nil {
		return nil, err
	}
	c.BaseURL = InsightsBaseURL
	return &InsightsClient{Client: c}, nil
}

// NewInsightsClientWithKey creates a new Insights API client with the given API key.
func NewInsightsClientWithKey(apiKey string) *InsightsClient {
	c := NewClientWithKey(apiKey)
	c.BaseURL = InsightsBaseURL
	return &InsightsClient{Client: c}
}

// InsightsQuery represents a Directory Insights event query.
type InsightsQuery struct {
	// Service is the event service to query (e.g., "sso", "ldap", "all").
	// Multiple services can be comma-separated.
	Service string `json:"service"`
	// StartTime is the beginning of the time range (RFC 3339).
	StartTime string `json:"start_time"`
	// EndTime is the end of the time range (RFC 3339).
	EndTime string `json:"end_time,omitempty"`
	// Fields is a list of fields to include in the response.
	Fields []string `json:"fields,omitempty"`
	// SearchTermFilter is a map of field→value filters to apply.
	SearchTermFilter map[string]any `json:"search_term_filter,omitempty"`
	// Sort is the sort order (e.g., "timestamp" or "-timestamp").
	Sort string `json:"sort,omitempty"`
	// Limit is the maximum number of events to return per page.
	Limit int `json:"limit,omitempty"`
	// Skip is the number of events to skip (for pagination).
	Skip int `json:"skip,omitempty"`
}

// InsightsQueryOptions controls pagination and limits for event queries.
type InsightsQueryOptions struct {
	// Limit is the maximum total number of results to return (0 = no limit).
	Limit int
	// Sort is the field to sort by. Prefix with "-" for descending order.
	Sort string
}

// InsightsResult holds the results from an event query.
type InsightsResult struct {
	// Data is the list of event items.
	Data []json.RawMessage
}

// QueryEvents sends a POST query to the /events endpoint and returns all matching events.
// Pagination is handled automatically via skip/limit in the request body.
func (c *InsightsClient) QueryEvents(ctx context.Context, query InsightsQuery, opts InsightsQueryOptions) (*InsightsResult, error) {
	pageSize := DefaultPageSize
	if opts.Limit > 0 && opts.Limit < pageSize {
		pageSize = opts.Limit
	}

	var allResults []json.RawMessage
	skip := 0

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Build request body with pagination.
		body := c.buildQueryBody(query, skip, pageSize, opts)
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshalling query body: %w", err)
		}

		reqURL := c.BaseURL + "/events"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}

		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, NewAPIError(resp.StatusCode, "/events", respBody)
		}

		// Directory Insights returns a bare JSON array.
		var pageItems []json.RawMessage
		if err := json.Unmarshal(respBody, &pageItems); err != nil {
			return nil, fmt.Errorf("parsing response: %w", err)
		}

		allResults = append(allResults, pageItems...)

		// Check if we've reached the user-specified limit.
		if opts.Limit > 0 && len(allResults) >= opts.Limit {
			allResults = allResults[:opts.Limit]
			break
		}

		// Stop if this page returned fewer items than requested (last page).
		if len(pageItems) < pageSize {
			break
		}

		skip += len(pageItems)

		// Adjust page size for final page if limit is set.
		if opts.Limit > 0 {
			remaining := opts.Limit - len(allResults)
			if remaining < pageSize {
				pageSize = remaining
			}
		}
	}

	return &InsightsResult{Data: allResults}, nil
}

// CountEvents sends a POST query to the /events/count endpoint and returns the event count.
func (c *InsightsClient) CountEvents(ctx context.Context, query InsightsQuery) (int, error) {
	body := map[string]any{
		"service":    query.Service,
		"start_time": query.StartTime,
	}
	if query.EndTime != "" {
		body["end_time"] = query.EndTime
	}
	if query.SearchTermFilter != nil {
		body["search_term_filter"] = query.SearchTermFilter
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("marshalling count body: %w", err)
	}

	reqURL := c.BaseURL + "/events/count"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(jsonBody))
	if err != nil {
		return 0, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, NewAPIError(resp.StatusCode, "/events/count", respBody)
	}

	// Response is {"count": N} or just a number.
	var countResp struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(respBody, &countResp); err != nil {
		return 0, fmt.Errorf("parsing count response: %w", err)
	}

	return countResp.Count, nil
}

// DistinctEvents sends a POST query to the /events/distinct endpoint
// and returns distinct values for a given field.
func (c *InsightsClient) DistinctEvents(ctx context.Context, query InsightsQuery, field string) ([]json.RawMessage, error) {
	body := map[string]any{
		"service":    query.Service,
		"start_time": query.StartTime,
		"field":      field,
	}
	if query.EndTime != "" {
		body["end_time"] = query.EndTime
	}
	if query.SearchTermFilter != nil {
		body["search_term_filter"] = query.SearchTermFilter
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling distinct body: %w", err)
	}

	reqURL := c.BaseURL + "/events/distinct"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, NewAPIError(resp.StatusCode, "/events/distinct", respBody)
	}

	var items []json.RawMessage
	if err := json.Unmarshal(respBody, &items); err != nil {
		return nil, fmt.Errorf("parsing distinct response: %w", err)
	}

	return items, nil
}

// buildQueryBody constructs the request body for event queries with pagination.
func (c *InsightsClient) buildQueryBody(query InsightsQuery, skip, limit int, opts InsightsQueryOptions) map[string]any {
	body := map[string]any{
		"service":    query.Service,
		"start_time": query.StartTime,
		"limit":      limit,
		"skip":       skip,
	}
	if query.EndTime != "" {
		body["end_time"] = query.EndTime
	}
	if len(query.Fields) > 0 {
		body["fields"] = query.Fields
	}
	if query.SearchTermFilter != nil {
		body["search_term_filter"] = query.SearchTermFilter
	}
	if opts.Sort != "" {
		body["sort"] = opts.Sort
	}
	return body
}

// ValidInsightsServices is the list of valid Directory Insights service names.
var ValidInsightsServices = []string{
	"all",
	"sso",
	"radius",
	"ldap",
	"user_portal",
	"admin",
	"mdm",
	"directory",
	"software",
	"systems",
	"password_manager",
}

// ValidateService checks if a service name (or comma-separated list) is valid.
// Returns an error listing valid services if any name is invalid.
func ValidateService(services string) error {
	valid := make(map[string]bool, len(ValidInsightsServices))
	for _, s := range ValidInsightsServices {
		valid[s] = true
	}
	for _, s := range strings.Split(services, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !valid[s] {
			return fmt.Errorf("invalid service %q; valid services: %s", s, strings.Join(ValidInsightsServices, ", "))
		}
	}
	return nil
}

// durationRegexp matches time duration shortcuts like "24h", "7d", "30d", "1h".
var durationRegexp = regexp.MustCompile(`^(\d+)\s*(h|d|m)$`)

// ParseTimeRange parses a human-friendly time range string into a start/end time pair.
// Supported formats:
//   - Duration shortcut: "24h", "7d", "30d", "1h" (relative to now)
//   - Date: "2006-01-02" (midnight UTC)
//   - Datetime: "2006-01-02T15:04:05Z" (RFC 3339)
//   - Keyword: "last 24h", "last 7d", "last 30d"
func ParseTimeRange(input string) (time.Time, error) {
	input = strings.TrimSpace(input)

	// Handle "last Xd" / "last Xh" prefixed format.
	if strings.HasPrefix(input, "last ") {
		input = strings.TrimPrefix(input, "last ")
		input = strings.TrimSpace(input)
	}

	// Try duration shortcut (e.g., "24h", "7d", "30d").
	if matches := durationRegexp.FindStringSubmatch(input); matches != nil {
		n, _ := strconv.Atoi(matches[1])
		now := insightsNowFunc()
		switch matches[2] {
		case "h":
			return now.Add(-time.Duration(n) * time.Hour), nil
		case "d":
			return now.AddDate(0, 0, -n), nil
		case "m":
			return now.AddDate(0, -n, 0), nil
		}
	}

	// Try RFC 3339 datetime.
	if t, err := time.Parse(time.RFC3339, input); err == nil {
		return t, nil
	}

	// Try date-only format.
	if t, err := time.Parse("2006-01-02", input); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("invalid time format %q; expected: 24h, 7d, 30d, 2006-01-02, or RFC 3339", input)
}
