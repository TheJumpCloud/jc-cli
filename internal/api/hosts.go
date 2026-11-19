package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// insightsSuffix is the path the regional insights base URL ends in. The
// data-export (SIEM pull) API lives on the same regional host.
const insightsSuffix = "/insights/directory/v1"

// ResolveDataExportBaseURL is the SIEM pull API for the current region:
// https://api[.eu|.in].jumpcloud.com/data-export/v2. It is derived from the
// insights base so a region (or JC_INSIGHTS_BASE_URL override, used by tests)
// applies to both.
func ResolveDataExportBaseURL() string {
	return strings.TrimSuffix(ResolveInsightsBaseURL(), insightsSuffix) + "/data-export/v2"
}

// Raw sends one request to base+path with the client's auth, retry and
// logging transport, and returns the body and its content type. body is
// JSON-encoded when not nil. Any status other than 200, 201, 202 or 204 is
// an *APIError.
func (c *Client) Raw(ctx context.Context, method, base, path string, body any) ([]byte, string, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, "", fmt.Errorf("encoding the request body: %w", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading response: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent:
		return out, resp.Header.Get("Content-Type"), nil
	}
	return nil, "", NewAPIError(resp.StatusCode, path, out)
}

// IntervalBody builds a Directory Insights /events/interval request. service
// is comma-separated; interval is a number and m, h, d or w; an empty end
// means now. Shared by the CLI and the MCP tools.
func IntervalBody(service, startTime, endTime, interval, timezone string) (map[string]any, error) {
	if err := ValidateService(service); err != nil {
		return nil, err
	}
	if startTime == "" {
		return nil, fmt.Errorf("a start time is required")
	}
	if endTime == "" {
		endTime = time.Now().UTC().Format(time.RFC3339)
	}
	interval = strings.TrimSpace(interval)
	if len(interval) < 2 || !strings.ContainsAny(interval[len(interval)-1:], "mhdw") {
		return nil, fmt.Errorf("the interval must be a number and a unit (m, h, d or w), e.g. 1d")
	}
	n, err := strconv.Atoi(interval[:len(interval)-1])
	if err != nil || n < 1 {
		return nil, fmt.Errorf("the interval must start with a positive whole number, e.g. 1d")
	}
	services := []string{}
	for _, s := range strings.Split(service, ",") {
		if s = strings.TrimSpace(s); s != "" {
			services = append(services, s)
		}
	}
	body := map[string]any{
		"service": services, "start_time": startTime, "end_time": endTime,
		"interval_unit": interval[len(interval)-1:], "interval_value": strconv.Itoa(n),
	}
	if timezone != "" {
		body["timezone"] = timezone
	}
	return body, nil
}

// ParseIntervalBuckets reads the {buckets: [...]} interval response strictly.
func ParseIntervalBuckets(raw []byte) ([]json.RawMessage, error) {
	var env struct {
		Buckets *[]json.RawMessage `json:"buckets"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("the interval response is not an object: %w", err)
	}
	if env.Buckets == nil {
		return nil, fmt.Errorf("the interval response has no buckets")
	}
	return *env.Buckets, nil
}
