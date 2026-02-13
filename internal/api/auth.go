package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Organization represents the response from GET /api/organizations.
type Organization struct {
	ID          string `json:"_id"`
	DisplayName string `json:"displayName"`
}

// ValidateAPIKey checks whether the configured API key is valid by calling
// GET /api/organizations. Returns the organization info on success.
func (c *Client) ValidateAPIKey() (*Organization, error) {
	resp, err := c.HTTP.Get(c.BaseURL + "/organizations")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to JumpCloud API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read API response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		// The organizations endpoint returns an object with a "results" array.
		var wrapper struct {
			Results []Organization `json:"results"`
		}
		if err := json.Unmarshal(body, &wrapper); err == nil && len(wrapper.Results) > 0 {
			return &wrapper.Results[0], nil
		}
		// Fallback: try parsing as a single org (API may return different shapes).
		var org Organization
		if err := json.Unmarshal(body, &org); err == nil && org.ID != "" {
			return &org, nil
		}
		return nil, fmt.Errorf("API key is valid but returned no organizations")

	case http.StatusUnauthorized:
		return nil, fmt.Errorf("invalid API key (HTTP 401). Check your key or run: jc auth login")

	case http.StatusForbidden:
		return nil, fmt.Errorf("API key lacks permission (HTTP 403). Verify the key has the correct scope")

	default:
		return nil, fmt.Errorf("JumpCloud API returned HTTP %d: %s", resp.StatusCode, truncateBody(body, 200))
	}
}

// truncateBody returns the body string, truncated to maxLen bytes.
func truncateBody(body []byte, maxLen int) string {
	if len(body) <= maxLen {
		return string(body)
	}
	return string(body[:maxLen]) + "..."
}
