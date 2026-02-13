package api

import (
	"bytes"
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
	// Sort is the field to sort by. Prefix with "-" for descending order.
	Sort string
}

// ListResult holds the results from a list operation along with metadata.
type ListResult struct {
	// Data is the list of result items.
	Data []json.RawMessage
	// TotalCount is the total number of items available on the server.
	TotalCount int
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
// Results are accumulated and returned as a ListResult with total count.
//
// Pagination stops when:
//   - All results have been fetched (skip >= totalCount)
//   - The Limit has been reached
//   - The context is cancelled
func (c *V1Client) ListAll(ctx context.Context, endpoint string, opts ListOptions) (*ListResult, error) {
	pageSize := opts.effectivePageSize()
	var allResults []json.RawMessage
	var totalCount int
	skip := 0

	for {
		// Check context cancellation before each page request.
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Build URL with pagination parameters.
		reqURL, err := c.buildListURL(endpoint, skip, pageSize, opts.Sort)
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

		totalCount = listResp.TotalCount

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

	return &ListResult{Data: allResults, TotalCount: totalCount}, nil
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

// Create sends a POST request to create a new resource at the given V1 endpoint.
// The body should be a JSON-serializable object. Returns the created resource.
func (c *V1Client) Create(ctx context.Context, endpoint string, body any) (json.RawMessage, error) {
	jsonBody, err := json.Marshal(body)
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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, NewAPIError(resp.StatusCode, endpoint, respBody)
	}

	return respBody, nil
}

// Update sends a PUT request to update an existing resource at the given V1 endpoint.
// The body should be a JSON-serializable object. Returns the updated resource.
func (c *V1Client) Update(ctx context.Context, endpoint string, body any) (json.RawMessage, error) {
	jsonBody, err := json.Marshal(body)
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

// Delete sends a DELETE request to remove a resource at the given V1 endpoint.
// Returns the response body (JumpCloud typically returns the deleted resource).
func (c *V1Client) Delete(ctx context.Context, endpoint string) (json.RawMessage, error) {
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

// Post sends a POST request to a V1 endpoint with an optional body.
// Used for action endpoints like /systemusers/{id}/resetmfa that don't return
// a meaningful resource. Returns the raw response body.
func (c *V1Client) Post(ctx context.Context, endpoint string, body any) (json.RawMessage, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshalling request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	reqURL := c.BaseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bodyReader)
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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return nil, NewAPIError(resp.StatusCode, endpoint, respBody)
	}

	return respBody, nil
}

// SearchOptions controls search behavior for V1 search endpoints.
type SearchOptions struct {
	// Limit is the maximum total number of results to return (0 = no limit).
	Limit int
	// Sort is the field to sort by. Prefix with "-" for descending order.
	Sort string
}

// Search sends a POST request to a V1 search endpoint and returns all matching results.
// The endpoint should be a path like "/search/systemusers" (appended to BaseURL).
// The searchBody is the JSON request body containing search filters.
// Results are accumulated with automatic pagination and returned as a ListResult.
func (c *V1Client) Search(ctx context.Context, endpoint string, searchBody any, opts SearchOptions) (*ListResult, error) {
	pageSize := DefaultPageSize
	if opts.Limit > 0 && opts.Limit < pageSize {
		pageSize = opts.Limit
	}

	var allResults []json.RawMessage
	var totalCount int
	skip := 0

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Marshal the search body and inject pagination fields.
		rawBody, err := json.Marshal(searchBody)
		if err != nil {
			return nil, fmt.Errorf("marshalling search body: %w", err)
		}

		// Parse into a map to inject pagination params.
		var bodyMap map[string]any
		if err := json.Unmarshal(rawBody, &bodyMap); err != nil {
			return nil, fmt.Errorf("parsing search body: %w", err)
		}
		bodyMap["skip"] = skip
		bodyMap["limit"] = pageSize
		if opts.Sort != "" {
			bodyMap["sort"] = opts.Sort
		}

		paginatedBody, err := json.Marshal(bodyMap)
		if err != nil {
			return nil, fmt.Errorf("marshalling paginated body: %w", err)
		}

		reqURL := c.BaseURL + endpoint
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(paginatedBody))
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

		if resp.StatusCode != http.StatusOK {
			return nil, NewAPIError(resp.StatusCode, endpoint, body)
		}

		var listResp v1ListResponse
		if err := json.Unmarshal(body, &listResp); err != nil {
			return nil, fmt.Errorf("parsing response: %w", err)
		}

		totalCount = listResp.TotalCount

		var pageItems []json.RawMessage
		if err := json.Unmarshal(listResp.Results, &pageItems); err != nil {
			return nil, fmt.Errorf("parsing results array: %w", err)
		}

		allResults = append(allResults, pageItems...)

		if opts.Limit > 0 && len(allResults) >= opts.Limit {
			allResults = allResults[:opts.Limit]
			break
		}

		skip += len(pageItems)
		if skip >= listResp.TotalCount || len(pageItems) == 0 {
			break
		}

		if opts.Limit > 0 {
			remaining := opts.Limit - len(allResults)
			if remaining < pageSize {
				pageSize = remaining
			}
		}
	}

	return &ListResult{Data: allResults, TotalCount: totalCount}, nil
}

// buildListURL constructs the full URL for a V1 list request with pagination params.
func (c *V1Client) buildListURL(endpoint string, skip, limit int, sort string) (string, error) {
	u, err := url.Parse(c.BaseURL + endpoint)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("skip", strconv.Itoa(skip))
	q.Set("limit", strconv.Itoa(limit))
	if sort != "" {
		q.Set("sort", sort)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
