package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SIEM data export lives on the Insights host family but not under the
// /insights/directory/v1 prefix the rest of the Insights client uses, so the
// base URL is trimmed back to the host. Going through the Insights client
// keeps --region and JC_INSIGHTS_BASE_URL in effect.
const insightsPathPrefix = "/insights/directory/v1"

const (
	// SIEMMaxWindow is the longest since..until window the endpoint accepts.
	SIEMMaxWindow = time.Hour
	// SIEMMaxEventTypes is the most event_types slugs one request may name.
	SIEMMaxEventTypes = 25
	// SIEMMaxPageSize is the largest page the endpoint returns.
	SIEMMaxPageSize = 1000
)

// DataExportBaseURL returns the data-export v2 base URL for this client.
func (c *InsightsClient) DataExportBaseURL() string {
	return strings.TrimSuffix(c.BaseURL, insightsPathPrefix) + "/data-export/v2"
}

// SIEMConnectionResult is the connection-test response.
type SIEMConnectionResult struct {
	OK    bool   `json:"ok"`
	OrgID string `json:"org_id"`
}

// SIEMEventsQuery describes one SIEM events pull.
type SIEMEventsQuery struct {
	Since      time.Time
	Until      time.Time
	EventTypes []string
	Limit      int // page size; 0 = server default
	OCSF       bool
}

// SIEMUnavailableError is the 422 body returned when until is too recent.
type SIEMUnavailableError struct {
	*APIError
	AvailableUntil string
}

func (c *InsightsClient) doDataExport(ctx context.Context, method, path string, q url.Values, accept string) ([]byte, error) {
	u := c.DataExportBaseURL() + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		apiErr := NewAPIError(resp.StatusCode, "/data-export/v2"+path, body)
		if resp.StatusCode == http.StatusUnprocessableEntity {
			var e struct {
				AvailableUntil string `json:"available_until"`
			}
			if json.Unmarshal(body, &e) == nil && e.AvailableUntil != "" {
				return nil, &SIEMUnavailableError{APIError: apiErr, AvailableUntil: e.AvailableUntil}
			}
		}
		return nil, apiErr
	}
	return body, nil
}

// SIEMConnectionTest POSTs to /siem/connection-test (no body).
func (c *InsightsClient) SIEMConnectionTest(ctx context.Context) (*SIEMConnectionResult, error) {
	body, err := c.doDataExport(ctx, http.MethodPost, "/siem/connection-test", nil, "")
	if err != nil {
		return nil, err
	}
	var res SIEMConnectionResult
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	return &res, nil
}

// SIEMEventsRequestQuery builds the query string for one page.
func SIEMEventsRequestQuery(q SIEMEventsQuery, cursor string) url.Values {
	v := url.Values{}
	v.Set("since", q.Since.UTC().Format(time.RFC3339))
	v.Set("until", q.Until.UTC().Format(time.RFC3339))
	v.Set("event_types", strings.Join(q.EventTypes, ","))
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if cursor != "" {
		v.Set("next_cursor", cursor)
	}
	return v
}

// SIEMEventsAccept is the Accept header for the requested format.
func SIEMEventsAccept(ocsf bool) string {
	if ocsf {
		return "application/ocsf+json"
	}
	return "application/json"
}

// SIEMEvents follows next_cursor until it is absent or max events (max > 0)
// have been collected. The endpoint ignores skip/offset, so this never uses
// the generic offset helpers. A repeated cursor is treated as an error so a
// misbehaving server cannot make the loop unbounded.
func (c *InsightsClient) SIEMEvents(ctx context.Context, q SIEMEventsQuery, max int) ([]json.RawMessage, error) {
	var all []json.RawMessage
	cursor := ""
	seen := map[string]bool{}
	pageQ := q
	if max > 0 && (pageQ.Limit == 0 || pageQ.Limit > max) {
		pageQ.Limit = max
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		body, err := c.doDataExport(ctx, http.MethodGet, "/events", SIEMEventsRequestQuery(pageQ, cursor), SIEMEventsAccept(q.OCSF))
		if err != nil {
			return nil, err
		}
		var page struct {
			Events     []json.RawMessage `json:"events"`
			NextCursor string            `json:"next_cursor"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("parsing response: %w", err)
		}
		all = append(all, page.Events...)
		if max > 0 && len(all) >= max {
			return all[:max], nil
		}
		if page.NextCursor == "" {
			return all, nil
		}
		if seen[page.NextCursor] {
			return nil, fmt.Errorf("SIEM events: server repeated next_cursor %q; stopping to avoid an endless loop", page.NextCursor)
		}
		seen[page.NextCursor] = true
		cursor = page.NextCursor
	}
}
