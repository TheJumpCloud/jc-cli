package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	// V2BaseURL is the JumpCloud API v2 base URL.
	V2BaseURL = "https://console.jumpcloud.com/api/v2"

	// DefaultV2PageSize is the default number of results per page for V2 pagination.
	DefaultV2PageSize = 100
)

// V2Client is a JumpCloud V2 API client with built-in Link-header pagination,
// rate limiting (via retryTransport), and error handling.
type V2Client struct {
	*Client
}

// NewV2Client creates a new V2 API client using the currently configured API key.
func NewV2Client() (*V2Client, error) {
	c, err := NewClient()
	if err != nil {
		return nil, err
	}
	c.BaseURL = V2BaseURL
	return &V2Client{Client: c}, nil
}

// NewV2ClientWithKey creates a new V2 API client with the given API key.
func NewV2ClientWithKey(apiKey string) *V2Client {
	c := NewClientWithKey(apiKey)
	c.BaseURL = V2BaseURL
	return &V2Client{Client: c}
}

// V2ListOptions controls pagination and result limits for V2 list operations.
type V2ListOptions struct {
	// Limit is the maximum total number of results to return (0 = no limit).
	Limit int
	// Sort is the field to sort by. Prefix with "-" for descending order.
	Sort string
	// Filter is a list of query parameter filter expressions for V2 (e.g., "filter=name:eq:Engineering").
	Filter []string
	// Search is a full-text search term.
	Search string
	// ResponseKey extracts the array from a wrapped object key (e.g. "identityProviders").
	ResponseKey string
}

// V2ListResult holds the results from a V2 list operation.
// V2 does not return a totalCount in the response body, so TotalCount
// represents the number of items actually fetched.
type V2ListResult struct {
	// Data is the list of result items.
	Data []json.RawMessage
}

// ListAll fetches all results from a V2 list endpoint with automatic Link-header pagination.
// The endpoint should be a path like "/usergroups" (appended to BaseURL).
//
// V2 API differences from V1:
//   - Response body is a bare JSON array (not wrapped in {"results": ..., "totalCount": ...})
//   - Pagination uses Link headers with rel="next" (RFC 5988)
//   - No totalCount is available; pagination stops when there is no "next" link
//
// Pagination stops when:
//   - There is no "next" Link header
//   - The Limit has been reached
//   - The context is cancelled
func (c *V2Client) ListAll(ctx context.Context, endpoint string, opts V2ListOptions) (*V2ListResult, error) {
	var allResults []json.RawMessage

	// Build initial URL with query parameters.
	reqURL, err := c.buildV2ListURL(endpoint, opts)
	if err != nil {
		return nil, fmt.Errorf("building URL: %w", err)
	}

	for {
		// Check context cancellation before each page request.
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
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

		// Handle non-200 responses.
		if resp.StatusCode != http.StatusOK {
			return nil, NewAPIError(resp.StatusCode, endpoint, body)
		}

		// V2 response is typically a bare JSON array, but some endpoints
		// return a wrapped object like {"results": [...]}.
		var pageItems []json.RawMessage
		if err := json.Unmarshal(body, &pageItems); err != nil {
			// Try explicit ResponseKey first, then fall back to "results".
			var parsed bool
			if opts.ResponseKey != "" {
				var obj map[string]json.RawMessage
				if err2 := json.Unmarshal(body, &obj); err2 == nil {
					if arr, ok := obj[opts.ResponseKey]; ok {
						if err3 := json.Unmarshal(arr, &pageItems); err3 == nil {
							parsed = true
						}
					}
				}
			}
			if !parsed {
				var wrapped struct {
					Results []json.RawMessage `json:"results"`
				}
				if err2 := json.Unmarshal(body, &wrapped); err2 != nil {
					return nil, fmt.Errorf("parsing response: %w", err)
				}
				pageItems = wrapped.Results
			}
		}

		allResults = append(allResults, pageItems...)

		// Check if we've reached the user-specified limit.
		if opts.Limit > 0 && len(allResults) >= opts.Limit {
			allResults = allResults[:opts.Limit]
			break
		}

		// Follow the "next" Link header for pagination.
		nextURL := parseLinkNext(resp.Header.Get("Link"))
		if nextURL == "" || len(pageItems) == 0 {
			break
		}

		// Validate the next URL's host matches our base URL to prevent
		// credential exfiltration via malicious Link headers.
		if !isSameOrigin(nextURL, c.BaseURL) {
			return nil, fmt.Errorf("pagination link host mismatch: refusing to follow %q", nextURL)
		}

		reqURL = nextURL
	}

	return &V2ListResult{Data: allResults}, nil
}

// Get fetches a single resource from a V2 endpoint.
// The endpoint should include the resource ID, e.g. "/usergroups/{id}".
func (c *V2Client) Get(ctx context.Context, endpoint string) (json.RawMessage, error) {
	reqURL := c.BaseURL + endpoint

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, NewAPIError(resp.StatusCode, endpoint, body)
	}

	return body, nil
}

// Create sends a POST request to create a new resource at the given V2 endpoint.
// The body should be a JSON-serializable object. Returns the created resource.
func (c *V2Client) Create(ctx context.Context, endpoint string, reqBody any) (json.RawMessage, error) {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshalling request body: %w", err)
	}

	reqURL := c.BaseURL + endpoint
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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return nil, NewAPIError(resp.StatusCode, endpoint, respBody)
	}

	return respBody, nil
}

// Update sends a PUT request to update an existing resource at the given V2 endpoint.
// The body should be a JSON-serializable object. Returns the updated resource.
func (c *V2Client) Update(ctx context.Context, endpoint string, reqBody any) (json.RawMessage, error) {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshalling request body: %w", err)
	}

	reqURL := c.BaseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, bytes.NewReader(jsonBody))
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
		return nil, NewAPIError(resp.StatusCode, endpoint, respBody)
	}

	return respBody, nil
}

// Delete sends a DELETE request to remove a resource at the given V2 endpoint.
// Returns the response body (may be empty for 204 responses).
func (c *V2Client) Delete(ctx context.Context, endpoint string) (json.RawMessage, error) {
	reqURL := c.BaseURL + endpoint

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return nil, NewAPIError(resp.StatusCode, endpoint, body)
	}

	return body, nil
}

// Patch sends a PATCH request to partially update a resource at the given V2 endpoint.
// The body should be a JSON-serializable object. Returns the updated resource.
func (c *V2Client) Patch(ctx context.Context, endpoint string, reqBody any) (json.RawMessage, error) {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshalling request body: %w", err)
	}

	reqURL := c.BaseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, reqURL, bytes.NewReader(jsonBody))
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
		return nil, NewAPIError(resp.StatusCode, endpoint, respBody)
	}

	return respBody, nil
}

// buildV2ListURL constructs the full URL for a V2 list request with query parameters.
func (c *V2Client) buildV2ListURL(endpoint string, opts V2ListOptions) (string, error) {
	u, err := url.Parse(c.BaseURL + endpoint)
	if err != nil {
		return "", err
	}

	q := u.Query()

	// V2 uses "limit" query param for page size.
	pageSize := DefaultV2PageSize
	if opts.Limit > 0 && opts.Limit < pageSize {
		pageSize = opts.Limit
	}
	q.Set("limit", fmt.Sprintf("%d", pageSize))

	if opts.Sort != "" {
		q.Set("sort", opts.Sort)
	}
	for _, f := range opts.Filter {
		q.Add("filter", f)
	}
	if opts.Search != "" {
		q.Set("search", opts.Search)
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}

// isSameOrigin checks that a URL shares the same scheme and host as the base URL.
// This prevents the authenticated client from following cross-origin pagination links.
func isSameOrigin(rawURL, baseURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, base.Scheme) && strings.EqualFold(u.Host, base.Host)
}

// parseLinkNext extracts the URL for rel="next" from an HTTP Link header.
// Link header format (RFC 5988):
//
//	<https://console.jumpcloud.com/api/v2/usergroups?limit=100&skip=100>; rel="next"
//
// Returns empty string if no "next" link is found.
func parseLinkNext(header string) string {
	if header == "" {
		return ""
	}

	// Link headers can contain multiple links separated by commas.
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)

		// Each part is: <url>; rel="relation"
		segments := strings.SplitN(part, ";", 2)
		if len(segments) != 2 {
			continue
		}

		urlPart := strings.TrimSpace(segments[0])
		relPart := strings.TrimSpace(segments[1])

		// Check if this is rel="next".
		if !strings.Contains(relPart, `rel="next"`) {
			continue
		}

		// Extract URL from angle brackets.
		if strings.HasPrefix(urlPart, "<") && strings.HasSuffix(urlPart, ">") {
			return urlPart[1 : len(urlPart)-1]
		}
	}

	return ""
}
