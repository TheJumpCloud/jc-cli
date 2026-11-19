// Package direports covers Directory Insights reports (on the insights host):
// request a report, list the requests, download an artifact. Probed
// 2026-11-19:
//
//   - POST /reports/{type} answers 202 with the report record, status
//     PENDING; it completes (or FAILS — os-version failed on the probe
//     tenant) asynchronously, within about a minute.
//   - GET /reports is a bare array of records with id, type ("ods-" plus the
//     requested type), status, expiration (7 days) and artifacts, each
//     {id, format: CSV|JSON}.
//   - GET /reports/{id}/artifacts/{artifactId}/content is the file itself.
//     It carries personal data (usernames, emails, account state), so the CLI
//     writes it to a file and the MCP tools never return it.
//   - There is no delete; reports expire.
package direports

import (
	"encoding/json"
	"fmt"
	"strings"
)

const ListPath = "/reports"

// Types are the report types the API accepts.
var Types = []string{
	"browser-patch-policy", "os-patch-policy", "os-version", "software-inventory",
	"user-account-health", "users-to-devices", "users-to-directories",
	"users-to-ldap-servers", "users-to-radius-servers", "users-to-sso-applications",
	"users-to-user-groups",
}

// RequestPath validates a report type and builds its request path.
func RequestPath(reportType string) (string, error) {
	for _, t := range Types {
		if t == reportType {
			return "/reports/" + t, nil
		}
	}
	return "", fmt.Errorf("unknown report type %q; one of: %s", reportType, strings.Join(Types, ", "))
}

// ContentPath is the download path of one artifact.
func ContentPath(reportID, artifactID string) string {
	return "/reports/" + reportID + "/artifacts/" + artifactID + "/content"
}

// Artifact is one downloadable file of a report.
type Artifact struct {
	ID     string `json:"id"`
	Format string `json:"format"`
}

// Report is one report request.
type Report struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Status    string     `json:"status"`
	Artifacts []Artifact `json:"artifacts"`
}

// ParseList reads the bare-array report listing.
func ParseList(raw []byte) ([]json.RawMessage, error) {
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("the reports listing is not an array: %w", err)
	}
	return rows, nil
}

// Find returns one report from the listing.
func Find(rows []json.RawMessage, id string) (Report, error) {
	for _, r := range rows {
		var rep Report
		if json.Unmarshal(r, &rep) == nil && rep.ID == id {
			return rep, nil
		}
	}
	return Report{}, fmt.Errorf("no Directory Insights report %s (reports expire after 7 days)", id)
}

// PickArtifact chooses an artifact by id or by format (csv, json).
func PickArtifact(rep Report, which string) (Artifact, error) {
	if rep.Status != "COMPLETED" {
		return Artifact{}, fmt.Errorf("report %s is %s; only a COMPLETED report can be downloaded", rep.ID, rep.Status)
	}
	for _, a := range rep.Artifacts {
		if a.ID == which || strings.EqualFold(a.Format, which) {
			return a, nil
		}
	}
	formats := make([]string, 0, len(rep.Artifacts))
	for _, a := range rep.Artifacts {
		formats = append(formats, strings.ToLower(a.Format))
	}
	return Artifact{}, fmt.Errorf("report %s has no artifact %q (available: %s)", rep.ID, which, strings.Join(formats, ", "))
}
