package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// InsightsReportTypes are the report types Directory Insights accepts on
// POST /reports/{report_type}. The published spec spells user-account-health
// differently in its request and list enums; the request spelling is used here.
var InsightsReportTypes = []string{
	"browser-patch-policy",
	"os-patch-policy",
	"os-version",
	"software-inventory",
	"user-account-health",
	"users-to-devices",
	"users-to-directories",
	"users-to-ldap-servers",
	"users-to-radius-servers",
	"users-to-sso-applications",
	"users-to-user-groups",
}

// Directory Insights report statuses.
const (
	ReportStatusPending    = "PENDING"
	ReportStatusInProgress = "IN_PROGRESS"
	ReportStatusCompleted  = "COMPLETED"
	ReportStatusFailed     = "FAILED"
	ReportStatusDeleted    = "DELETED"
)

// InsightsReportArtifact is one downloadable file of a report.
type InsightsReportArtifact struct {
	ID     string `json:"id"`
	Format string `json:"format"`
}

// InsightsReport is a Directory Insights report's metadata.
type InsightsReport struct {
	ID        string                   `json:"id"`
	Type      string                   `json:"type"`
	Status    string                   `json:"status"`
	Artifacts []InsightsReportArtifact `json:"artifacts"`
}

// ValidateInsightsReportType rejects a report type the API does not offer.
func ValidateInsightsReportType(t string) error {
	for _, v := range InsightsReportTypes {
		if v == t {
			return nil
		}
	}
	return fmt.Errorf("unknown report type %q; valid types: %s", t, strings.Join(InsightsReportTypes, ", "))
}

// ListReports returns the raw report metadata objects. sort is passed through
// as the sort query parameter when non-empty.
func (c *InsightsClient) ListReports(ctx context.Context, sort string) ([]json.RawMessage, error) {
	endpoint := "/reports"
	if sort != "" {
		endpoint += "?sort=" + url.QueryEscape(sort)
	}
	body, _, err := c.do(ctx, http.MethodGet, endpoint, "/reports", http.StatusOK)
	if err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("parsing reports response: %w", err)
	}
	return items, nil
}

// RequestReport asks for a new report of the given type and returns the
// metadata of the pending report (HTTP 202).
func (c *InsightsClient) RequestReport(ctx context.Context, reportType string) (json.RawMessage, error) {
	body, _, err := c.do(ctx, http.MethodPost, "/reports/"+url.PathEscape(reportType), "/reports/"+reportType, http.StatusAccepted, http.StatusOK, http.StatusCreated)
	return body, err
}

// DownloadReportArtifact returns the body of a report artifact unchanged.
func (c *InsightsClient) DownloadReportArtifact(ctx context.Context, reportID, artifactID string) ([]byte, error) {
	endpoint := "/reports/" + url.PathEscape(reportID) + "/artifacts/" + url.PathEscape(artifactID) + "/content"
	body, _, err := c.do(ctx, http.MethodGet, endpoint, endpoint, http.StatusOK)
	return body, err
}

func (c *InsightsClient) do(ctx context.Context, method, endpoint, label string, okStatuses ...int) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+endpoint, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response: %w", err)
	}
	for _, s := range okStatuses {
		if resp.StatusCode == s {
			return body, resp.StatusCode, nil
		}
	}
	return nil, resp.StatusCode, NewAPIError(resp.StatusCode, label, body)
}
