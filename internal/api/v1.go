package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

const (
	// DefaultPageSize is the default number of results per page for V1 pagination.
	DefaultPageSize = 100
)

// V1Client is a JumpCloud V1 API client with built-in pagination,
// rate limiting (via retryTransport), and error handling.
type V1Client struct {
	*Client
}

// NewV1Client creates a new V1 API client using the currently configured API key.
func NewV1Client() (*V1Client, error) {
	c, err := NewClient()
	if err != nil {
		return nil, err
	}
	return &V1Client{Client: c}, nil
}

// NewV1ClientWithKey creates a new V1 API client with the given API key.
func NewV1ClientWithKey(apiKey string) *V1Client {
	return &V1Client{Client: NewClientWithKey(apiKey)}
}

// ListOptions controls pagination and result limits for list operations.
type ListOptions struct {
	// Limit is the maximum total number of results to return (0 = no limit).
	Limit int
	// PageSize is the number of results per API request (default 100).
	PageSize int
}

// effectivePageSize returns the page size to use, considering both PageSize
// and Limit constraints.
func (o *ListOptions) effectivePageSize() int {
	ps := o.PageSize
	if ps <= 0 {
		ps = DefaultPageSize
	}
	// If limit is set and smaller than page size, only request what we need.
	if o.Limit > 0 && o.Limit < ps {
		return o.Limit
	}
	return ps
}

// v1ListResponse is the standard wrapper for V1 list endpoints.
// V1 returns {"results": [...], "totalCount": N}.
type v1ListResponse struct {
	Results    json.RawMessage `json:"results"`
	TotalCount int             `json:"totalCount"`
}

// ListAll fetches all results from a V1 list endpoint with automatic pagination.
// The endpoint should be a path like "/systemusers" (appended to BaseURL).
// Results are accumulated and returned as a JSON array.
//
// Pagination stops when:
//   - All results have been fetched (skip >= totalCount)
//   - The Limit has been reached
//   - The context is cancelled
func (c *V1Client) ListAll(ctx context.Context, endpoint string, opts ListOptions) ([]json.RawMessage, error) {
	pageSize := opts.effectivePageSize()
	var allResults []json.RawMessage
	skip := 0

	for {
		// Check context cancellation before each page request.
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Build URL with pagination parameters.
		reqURL, err := c.buildListURL(endpoint, skip, pageSize)
		if err != nil {
			return nil, fmt.Errorf("building URL: %w", err)
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

		// Parse the V1 list response.
		var listResp v1ListResponse
		if err := json.Unmarshal(body, &listResp); err != nil {
			return nil, fmt.Errorf("parsing response: %w", err)
		}

		// Unmarshal the results array into individual items.
		var pageItems []json.RawMessage
		if err := json.Unmarshal(listResp.Results, &pageItems); err != nil {
			return nil, fmt.Errorf("parsing results array: %w", err)
		}

		allResults = append(allResults, pageItems...)

		// Check if we've reached the user-specified limit.
		if opts.Limit > 0 && len(allResults) >= opts.Limit {
			allResults = allResults[:opts.Limit]
			break
		}

		// Check if we've fetched all available results.
		skip += len(pageItems)
		if skip >= listResp.TotalCount || len(pageItems) == 0 {
			break
		}

		// Adjust page size for the last page if limit is set.
		if opts.Limit > 0 {
			remaining := opts.Limit - len(allResults)
			if remaining < pageSize {
				pageSize = remaining
			}
		}
	}

	return allResults, nil
}

// Get fetches a single resource from a V1 endpoint.
// The endpoint should include the resource ID, e.g. "/systemusers/{id}".
func (c *V1Client) Get(ctx context.Context, endpoint string) (json.RawMessage, error) {
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

// buildListURL constructs the full URL for a V1 list request with pagination params.
func (c *V1Client) buildListURL(endpoint string, skip, limit int) (string, error) {
	u, err := url.Parse(c.BaseURL + endpoint)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("skip", strconv.Itoa(skip))
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()
	return u.String(), nil
}
