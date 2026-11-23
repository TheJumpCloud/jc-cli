// Package siem reads the SIEM pull API (/data-export/v2 on the regional
// api host), shared by the CLI and the MCP tools. Probed 2026-11-19:
//
//   - GET /events needs since, until and event_types. The window may span at
//     most one hour ("events pull window exceeds 1 hour maximum").
//   - event_types is "all" or up to 25 comma-separated partition names; an
//     unknown name is a 400.
//   - Data lags real time by roughly two hours. A window past the writer
//     answers 422 with available_until; that is reported, not retried.
//   - Paging is by an opaque next_cursor.
//   - POST /siem/connection-test answers {ok, org_id} and returns no events.
package siem

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	EventsPath         = "/events"
	ConnectionTestPath = "/siem/connection-test"
	MaxWindow          = time.Hour
	MaxTypes           = 25
	MaxLimit           = 1000
)

// Query is one events pull.
type Query struct {
	Since, Until time.Time
	Types        []string // empty means all
	Limit        int
	Cursor       string
}

// Path validates q and builds the events path.
func (q Query) Path() (string, error) {
	if q.Since.IsZero() || q.Until.IsZero() {
		return "", fmt.Errorf("a window start and end are required")
	}
	if !q.Until.After(q.Since) {
		return "", fmt.Errorf("the window end must be after its start")
	}
	if q.Until.Sub(q.Since) > MaxWindow {
		return "", fmt.Errorf("the SIEM API serves at most one hour per request; this window is %s",
			q.Until.Sub(q.Since).Round(time.Minute))
	}
	types := []string{}
	for _, t := range q.Types {
		if t = strings.TrimSpace(t); t != "" {
			types = append(types, t)
		}
	}
	if len(types) == 0 {
		types = []string{"all"}
	}
	if len(types) > MaxTypes {
		return "", fmt.Errorf("at most %d event types per request (or \"all\")", MaxTypes)
	}
	for _, t := range types {
		if t == "all" && len(types) > 1 {
			return "", fmt.Errorf("\"all\" must be the only event type")
		}
	}
	if q.Limit < 0 || q.Limit > MaxLimit {
		return "", fmt.Errorf("limit must be between 1 and %d", MaxLimit)
	}
	v := url.Values{}
	v.Set("since", q.Since.UTC().Format(time.RFC3339))
	v.Set("until", q.Until.UTC().Format(time.RFC3339))
	v.Set("event_types", strings.Join(types, ","))
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Cursor != "" {
		v.Set("next_cursor", q.Cursor)
	}
	return EventsPath + "?" + v.Encode(), nil
}

// Page is one page of events.
type Page struct {
	Events     []json.RawMessage `json:"events"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

// ParsePage reads an events page strictly.
func ParsePage(raw []byte) (Page, error) {
	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		return Page{}, fmt.Errorf("the SIEM events response is not an object: %w", err)
	}
	ev, ok := env["events"]
	if !ok {
		return Page{}, fmt.Errorf("the SIEM events response has no events field")
	}
	var p Page
	if string(ev) != "null" {
		if err := json.Unmarshal(ev, &p.Events); err != nil {
			return Page{}, fmt.Errorf("SIEM events is not an array: %w", err)
		}
	}
	if c, ok := env["next_cursor"]; ok && string(c) != "null" {
		if err := json.Unmarshal(c, &p.NextCursor); err != nil {
			return Page{}, fmt.Errorf("SIEM next_cursor is not a string: %w", err)
		}
	}
	return p, nil
}

// LagHint explains a 422 from a window the writer has not reached yet.
const LagHint = "SIEM event data lags real time by about two hours; pull an earlier window " +
	"(the error says how far data is available)"

// NewQuery builds a query from user input, shared by the CLI and MCP. Without
// since, it is the hour ending hoursAgo before now; without until, the hour
// after since.
func NewQuery(since, until string, hoursAgo time.Duration, types string, limit int, cursor string) (Query, error) {
	q := Query{Limit: limit, Cursor: cursor, Types: strings.Split(types, ",")}
	if since == "" {
		if until != "" {
			return q, fmt.Errorf("an end time needs a start time")
		}
		end := time.Now().UTC().Add(-hoursAgo).Truncate(time.Minute)
		q.Since, q.Until = end.Add(-time.Hour), end
		return q, nil
	}
	s, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return q, fmt.Errorf("the start must be RFC 3339, e.g. 2026-11-19T10:00:00Z")
	}
	q.Since = s
	if until == "" {
		q.Until = s.Add(time.Hour)
		return q, nil
	}
	u, err := time.Parse(time.RFC3339, until)
	if err != nil {
		return q, fmt.Errorf("the end must be RFC 3339")
	}
	q.Until = u
	return q, nil
}
