package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// insightsSuffix is the path the regional insights base URL ends in. The
// data-export (SIEM pull) API lives on the same regional host.
const insightsSuffix = "/insights/directory/v1"

// DataExportBaseURL is the SIEM pull API next to an insights base URL:
// https://api[.eu|.in].jumpcloud.com/data-export/v2. Data export has no
// override of its own; it follows the insights host (and so a
// JC_INSIGHTS_BASE_URL override). A base that does not end in
// /insights/directory/v1 is refused rather than guessed at.
func DataExportBaseURL(insightsBase string) (string, error) {
	trimmed := strings.TrimSuffix(strings.TrimRight(insightsBase, "/"), insightsSuffix)
	if trimmed == strings.TrimRight(insightsBase, "/") {
		return "", fmt.Errorf("cannot derive the SIEM data-export host from the insights base %q: "+
			"it does not end in %s", insightsBase, insightsSuffix)
	}
	return trimmed + "/data-export/v2", nil
}

// Raw sends one request to base+path with the client's auth, retry and
// logging transport, and returns the body. body is JSON-encoded when not nil.
// Any status other than 200, 201, 202 or 204 is an *APIError.
//
// A redirect to another host is not followed: the transport puts the org's
// credential on every round trip, so following a 302 to, say, a presigned
// storage URL would hand it to that host. The 3xx is returned as an error
// instead (see PutPresigned for the same rule on uploads).
func (c *Client) Raw(ctx context.Context, method, base, path string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encoding the request body: %w", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := *c.HTTP
	hc.CheckRedirect = sameHostRedirectsOnly
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent:
		return out, nil
	}
	return nil, NewAPIError(resp.StatusCode, path, out)
}

// sameHostRedirectsOnly follows a redirect only within the original host.
func sameHostRedirectsOnly(r *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	if r.URL.Host != via[0].URL.Host {
		return http.ErrUseLastResponse
	}
	return nil
}
