package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// startUserViewServer mounts V1 + V2 + Insights endpoints used by
// fetchUserViewData on a single test server, with deterministic fixtures.
func startUserViewServer(t *testing.T, user map[string]any, groups []map[string]any, sshKeys []map[string]any, events []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		// V1: user search by username (resolver path)
		case r.URL.Path == "/api/search/systemusers" && r.Method == "POST":
			// Resolver POSTs a search; return our single fixture user as the match.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results":    []map[string]any{user},
				"totalCount": 1,
			})
		case r.URL.Path == "/api/systemusers" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results":    []map[string]any{user},
				"totalCount": 1,
			})
		case strings.HasPrefix(r.URL.Path, "/api/systemusers/") && strings.HasSuffix(r.URL.Path, "/sshkeys") && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results":    sshKeys,
				"totalCount": len(sshKeys),
			})
		case strings.HasPrefix(r.URL.Path, "/api/systemusers/") && r.Method == "GET":
			// User detail by ID.
			_ = json.NewEncoder(w).Encode(user)

		// V2: user → group memberships
		case strings.HasPrefix(r.URL.Path, "/api/v2/users/") && strings.HasSuffix(r.URL.Path, "/memberof") && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(groups)

		// Insights: events query (POST) — the user_view filters by initiated_by.username.
		case r.URL.Path == "/insights/directory/v1/events" && r.Method == "POST":
			_ = json.NewEncoder(w).Encode(events)

		default:
			w.WriteHeader(404)
			_ = json.NewEncoder(w).Encode(map[string]string{"path": r.URL.Path, "method": r.Method})
		}
	}))
}

func TestPreviewSSHKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIabcdefghijklmnop user@host", "ssh-ed25519 AAAAC3NzaC1l…"},
		{"ssh-rsa SHORT", "ssh-rsa SHORT…"},
		{"justabunchoftext", "justabunchoftext"},
	}
	for _, c := range cases {
		got := previewSSHKey(c.in)
		if got != c.want {
			t.Errorf("previewSSHKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFetchUserViewData_Aggregates(t *testing.T) {
	setupToolTest(t)

	// Anchor "now" so the recent-events window is deterministic.
	origNow := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFunc = origNow })

	user := map[string]any{
		"_id":            "aabbccddee112233aabbcc01",
		"username":       "alice",
		"email":          "alice@acme.com",
		"firstname":      "Alice",
		"lastname":       "Anderson",
		"department":     "Engineering",
		"activated":      true,
		"suspended":      false,
		"account_locked": false,
		"totp_enabled":   true,
		"created":        "2025-01-15T10:00:00Z",
	}
	groups := []map[string]any{
		{"id": "gg-eng", "attributes": map[string]any{"name": "Engineering"}},
		{"id": "gg-onc", "attributes": map[string]any{"name": "Oncall"}},
	}
	sshKeys := []map[string]any{
		{"id": "k1", "name": "laptop", "public_key": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIabcdef alice@laptop", "create_date": "2026-01-10T00:00:00Z"},
	}
	events := []map[string]any{
		{"timestamp": "2026-04-27T11:00:00Z", "service": "sso", "event_type": "sso_auth", "success": true},
		{"timestamp": "2026-04-26T18:00:00Z", "service": "ldap", "event_type": "ldap_bind", "success": true},
	}

	ts := startUserViewServer(t, user, groups, sshKeys, events)
	t.Cleanup(ts.Close)
	overrideV1ClientForTest(t, ts.URL)
	overrideV2ClientForTest(t, ts.URL)
	overrideInsightsClientForTest(t, ts.URL)

	data, err := fetchUserViewData(context.Background(), userViewArgs{User: "alice"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	// Header
	if data.User.Username != "alice" {
		t.Errorf("username = %q, want alice", data.User.Username)
	}
	if data.User.Email != "alice@acme.com" {
		t.Errorf("email = %q", data.User.Email)
	}
	if !data.User.Activated || data.User.Locked || data.User.Suspended {
		t.Errorf("status flags wrong: %+v", data.User)
	}
	// MFA
	if !data.MFA.TOTPEnabled || data.MFA.Status != "ENROLLED" {
		t.Errorf("mfa = %+v, want enrolled+totp", data.MFA)
	}
	// Groups (sorted alphabetically)
	if len(data.Groups) != 2 {
		t.Fatalf("groups len = %d, want 2 (%+v)", len(data.Groups), data.Groups)
	}
	if data.Groups[0].Name != "Engineering" || data.Groups[1].Name != "Oncall" {
		t.Errorf("groups = %+v, want sorted Engineering, Oncall", data.Groups)
	}
	// SSH keys
	if len(data.SSHKeys) != 1 || data.SSHKeys[0].Name != "laptop" {
		t.Errorf("ssh keys = %+v", data.SSHKeys)
	}
	if !strings.HasPrefix(data.SSHKeys[0].PublicKeyPreview, "ssh-ed25519 ") {
		t.Errorf("preview = %q, want algo-prefixed", data.SSHKeys[0].PublicKeyPreview)
	}
	// Events
	if len(data.RecentEvents) != 2 {
		t.Errorf("events len = %d, want 2", len(data.RecentEvents))
	}
	// last_login derived from first event
	if data.User.LastLogin != "2026-04-27T11:00:00Z" {
		t.Errorf("last_login = %q, want first event timestamp", data.User.LastLogin)
	}
}

func TestFetchUserViewData_RequiresUser(t *testing.T) {
	_, err := fetchUserViewData(context.Background(), userViewArgs{})
	if err == nil || !strings.Contains(err.Error(), "user is required") {
		t.Errorf("expected user-required error, got: %v", err)
	}
}

func TestUserView_HasUIMetadata(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	result, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var found *mcp.Tool
	for _, tool := range result.Tools {
		if tool.Name == "user_view" {
			found = tool
			break
		}
	}
	if found == nil {
		t.Fatal("user_view tool missing")
	}
	meta := map[string]any(found.Meta)
	ui, ok := meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("expected _meta.ui to be a map, got %T", meta["ui"])
	}
	if uri, _ := ui["resourceUri"].(string); uri != userViewResourceURI {
		t.Errorf("resourceUri = %q, want %q", uri, userViewResourceURI)
	}
}

func TestUserViewResource_ServesHTMLWithInjection(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	result, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: userViewResourceURI})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(result.Contents) == 0 {
		t.Fatal("empty resource contents")
	}
	c := result.Contents[0]
	if c.MIMEType != mcpAppMIMEType {
		t.Errorf("MIME = %q, want %q", c.MIMEType, mcpAppMIMEType)
	}
	if !strings.Contains(c.Text, "window.jcApp") {
		t.Error("served HTML missing common.js injection")
	}
	if strings.Contains(c.Text, appCommonMarker) {
		t.Error("served HTML still contains injection marker")
	}
	if !strings.Contains(c.Text, "JumpCloud User Profile") {
		t.Error("served HTML missing page title")
	}
}
