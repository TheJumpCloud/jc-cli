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

// startComplianceServer mounts the three V1 endpoints fetchComplianceData
// hits (/systemusers, /systems, /users for admins) on a single httptest
// server with deterministic fixtures.
func startComplianceServer(t *testing.T, users, devices, admins []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/systemusers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": users, "totalCount": len(users),
			})
		case "/api/systems":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": devices, "totalCount": len(devices),
			})
		case "/api/users":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": admins, "totalCount": len(admins),
			})
		default:
			w.WriteHeader(404)
			_ = json.NewEncoder(w).Encode(map[string]string{"path": r.URL.Path})
		}
	}))
}

// passwordDateDaysAgo formats a date `days` ago using the date-only
// layout — exercises the secondary parse path in aggregateUserCompliance
// alongside the primary RFC3339 path. We anchor "now" in the tests so
// the math stays deterministic.
func passwordDateDaysAgo(now time.Time, days int) string {
	return now.Add(-time.Duration(days) * 24 * time.Hour).Format("2006-01-02")
}

func TestAggregateUserCompliance_MFAAndPasswordBuckets(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	users := []map[string]any{
		// Active + MFA + 5d password → bucket 0
		{"_id": "u1", "username": "alice", "email": "a@x.com",
			"activated": true, "totp_enabled": true,
			"password_date": passwordDateDaysAgo(now, 5)},
		// Active + no MFA + 45d password → bucket 1, lands in WithoutMFA
		{"_id": "u2", "username": "bob", "email": "b@x.com",
			"activated": true, "totp_enabled": false,
			"password_date": passwordDateDaysAgo(now, 45)},
		// Active + MFA via mfa.configured + 75d → bucket 2
		{"_id": "u3", "username": "carol", "email": "c@x.com",
			"activated": true, "mfa": map[string]any{"configured": true},
			"password_date": passwordDateDaysAgo(now, 75)},
		// Active + no MFA + 120d → bucket 3, second offender
		{"_id": "u4", "username": "dave", "email": "d@x.com",
			"activated": true, "totp_enabled": false,
			"password_date": passwordDateDaysAgo(now, 120)},
		// Suspended user without MFA → excluded from MFA scope but
		// still contributes to password buckets (bucket 0).
		{"_id": "u5", "username": "evan", "email": "e@x.com",
			"activated": true, "suspended": true, "totp_enabled": false,
			"password_date": passwordDateDaysAgo(now, 10)},
		// Locked user without MFA → also excluded from MFA scope.
		{"_id": "u6", "username": "frank", "email": "f@x.com",
			"activated": true, "account_locked": true, "totp_enabled": false,
			"password_date": passwordDateDaysAgo(now, 20)},
		// Empty password_date → no_data, still counts toward MFA scope.
		{"_id": "u7", "username": "gina", "email": "g@x.com",
			"activated": true, "totp_enabled": true},
	}
	raws := make([]json.RawMessage, len(users))
	for i, u := range users {
		raws[i], _ = json.Marshal(u)
	}

	mfa, pwd := aggregateUserCompliance(raws, now)

	// MFA scope: 5 active users (u1, u2, u3, u4, u7); u5 suspended, u6
	// locked — both excluded so an inactive account isn't held against
	// the org's MFA percentage.
	if mfa.Total != 5 {
		t.Errorf("MFA total = %d, want 5 (excludes suspended/locked)", mfa.Total)
	}
	if mfa.Enrolled != 3 {
		t.Errorf("MFA enrolled = %d, want 3 (u1, u3 via mfa.configured, u7)", mfa.Enrolled)
	}
	// 3 / 5 = 60 %
	if mfa.Percentage < 59.9 || mfa.Percentage > 60.1 {
		t.Errorf("MFA percentage = %.2f, want ~60.0", mfa.Percentage)
	}
	// Without MFA: u2 (bob) and u4 (dave). u5/u6 excluded from scope.
	if len(mfa.WithoutMFA) != 2 {
		t.Fatalf("WithoutMFA = %+v, want 2 entries (bob, dave)", mfa.WithoutMFA)
	}
	// Order follows iteration order of the input slice, which is
	// deterministic for this test.
	if mfa.WithoutMFA[0].Username != "bob" || mfa.WithoutMFA[1].Username != "dave" {
		t.Errorf("WithoutMFA usernames = %+v, want [bob, dave]", mfa.WithoutMFA)
	}
	if mfa.WithoutMFALen != 2 {
		t.Errorf("WithoutMFALen = %d, want 2", mfa.WithoutMFALen)
	}

	// Password buckets: u1=5d, u5=10d, u6=20d → bucket 0 (3 users);
	// u2=45d → bucket 1; u3=75d → bucket 2; u4=120d → bucket 3.
	// u7 → no_data.
	wantBuckets := []int{3, 1, 1, 1}
	for i, want := range wantBuckets {
		if pwd.Buckets[i].Count != want {
			t.Errorf("bucket[%d] %q count = %d, want %d",
				i, pwd.Buckets[i].Label, pwd.Buckets[i].Count, want)
		}
	}
	if pwd.NoData != 1 {
		t.Errorf("NoData = %d, want 1 (u7)", pwd.NoData)
	}
	if pwd.Total != 6 {
		t.Errorf("password Total = %d, want 6 (excludes no_data)", pwd.Total)
	}
	if pwd.Buckets[0].Label != "<30d" {
		t.Errorf("bucket[0].Label = %q, want \"<30d\"", pwd.Buckets[0].Label)
	}
}

func TestAggregateDeviceCompliance_FDEBuckets(t *testing.T) {
	devices := []map[string]any{
		{"_id": "d1", "hostname": "mac-1", "os": "Mac OS X", "fde": map[string]any{"active": true}},
		{"_id": "d2", "hostname": "mac-2", "os": "Mac OS X", "fde": map[string]any{"active": true}},
		{"_id": "d3", "hostname": "win-1", "os": "Windows", "fde": map[string]any{"active": false}},
		{"_id": "d4", "hostname": "win-2", "os": "Windows", "fde": map[string]any{"active": true}},
		{"_id": "d5", "hostname": "linux-1", "os": "", "fde": map[string]any{"active": false}},
	}
	raws := make([]json.RawMessage, len(devices))
	for i, d := range devices {
		raws[i], _ = json.Marshal(d)
	}

	fde := aggregateDeviceCompliance(raws)

	if fde.Total != 5 || fde.Encrypted != 3 {
		t.Errorf("overall = %d/%d, want 3/5", fde.Encrypted, fde.Total)
	}
	if fde.Percentage < 59.9 || fde.Percentage > 60.1 {
		t.Errorf("percentage = %.2f, want 60.0", fde.Percentage)
	}
	// ByOS sorted by total desc, then alphabetical. Mac=2 and Win=2
	// tie on total, so alphabetical: Mac OS X before Windows. Unknown=1
	// comes after.
	if len(fde.ByOS) != 3 {
		t.Fatalf("ByOS = %+v, want 3 entries", fde.ByOS)
	}
	if fde.ByOS[0].OS != "Mac OS X" || fde.ByOS[0].Encrypted != 2 {
		t.Errorf("ByOS[0] = %+v, want Mac OS X 2/2", fde.ByOS[0])
	}
	if fde.ByOS[1].OS != "Windows" || fde.ByOS[1].Encrypted != 1 {
		t.Errorf("ByOS[1] = %+v, want Windows 1/2", fde.ByOS[1])
	}
	if fde.ByOS[2].OS != "Unknown" {
		t.Errorf("ByOS[2] should be Unknown bucket, got %+v", fde.ByOS[2])
	}
	// Unencrypted list: d3 + d5
	if len(fde.Unencrypted) != 2 {
		t.Errorf("Unencrypted = %+v, want 2 entries", fde.Unencrypted)
	}
	if fde.UnencryptedLen != 2 {
		t.Errorf("UnencryptedLen = %d, want 2", fde.UnencryptedLen)
	}
}

func TestAggregateAdmins_SortAndShape(t *testing.T) {
	admins := []map[string]any{
		{"_id": "a2", "email": "zane@acme.com", "roleName": "Administrator",
			"enableMultiFactor": true, "lastLogin": "2026-05-20T10:00:00Z"},
		{"_id": "a1", "email": "alice@acme.com", "roleName": "Read-Only",
			"enableMultiFactor": false, "lastLogin": ""},
	}
	raws := make([]json.RawMessage, len(admins))
	for i, a := range admins {
		raws[i], _ = json.Marshal(a)
	}

	got := aggregateAdmins(raws)
	if got.Total != 2 {
		t.Errorf("total = %d, want 2", got.Total)
	}
	// Sorted alphabetically by email.
	if got.List[0].Email != "alice@acme.com" {
		t.Errorf("list[0] = %q, want alice@acme.com (alphabetical sort)", got.List[0].Email)
	}
	if got.List[1].EnableMultiFactor != true {
		t.Errorf("list[1].EnableMultiFactor = %v, want true", got.List[1].EnableMultiFactor)
	}
}

func TestFetchComplianceData_EndToEnd(t *testing.T) {
	setupToolTest(t)

	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	origNow := nowFunc
	nowFunc = func() time.Time { return now }
	t.Cleanup(func() { nowFunc = origNow })

	users := []map[string]any{
		{"_id": "u1", "username": "alice", "email": "a@x.com",
			"activated": true, "totp_enabled": true,
			"password_date": passwordDateDaysAgo(now, 5)},
		{"_id": "u2", "username": "bob", "email": "b@x.com",
			"activated": true, "totp_enabled": false,
			"password_date": passwordDateDaysAgo(now, 100)},
	}
	devices := []map[string]any{
		{"_id": "d1", "hostname": "mac-1", "os": "Mac OS X", "fde": map[string]any{"active": true}},
		{"_id": "d2", "hostname": "mac-2", "os": "Mac OS X", "fde": map[string]any{"active": false}},
	}
	admins := []map[string]any{
		{"_id": "a1", "email": "admin@acme.com", "roleName": "Administrator",
			"enableMultiFactor": true, "lastLogin": "2026-05-20T10:00:00Z"},
	}

	ts := startComplianceServer(t, users, devices, admins)
	t.Cleanup(ts.Close)
	overrideV1ClientForTest(t, ts.URL)

	data, err := fetchComplianceData(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	if data.MFA == nil || data.FDE == nil || data.Passwords == nil || data.Admins == nil {
		t.Fatalf("missing sections: %+v", data)
	}
	if data.MFA.Total != 2 || data.MFA.Enrolled != 1 {
		t.Errorf("MFA = %+v, want 2 active / 1 enrolled", data.MFA)
	}
	if data.FDE.Total != 2 || data.FDE.Encrypted != 1 {
		t.Errorf("FDE = %+v, want 2 total / 1 encrypted", data.FDE)
	}
	if data.Admins.Total != 1 || data.Admins.List[0].Email != "admin@acme.com" {
		t.Errorf("admins = %+v", data.Admins)
	}
	if data.Timestamp != now.Format(time.RFC3339) {
		t.Errorf("timestamp = %q, want %q", data.Timestamp, now.Format(time.RFC3339))
	}
}

func TestComplianceView_HasUIMetadata(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	result, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var found *mcp.Tool
	for _, tool := range result.Tools {
		if tool.Name == "compliance_view" {
			found = tool
			break
		}
	}
	if found == nil {
		t.Fatal("compliance_view tool missing")
	}
	meta := map[string]any(found.Meta)
	ui, ok := meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("expected _meta.ui to be a map, got %T", meta["ui"])
	}
	if uri, _ := ui["resourceUri"].(string); uri != "ui://jc/compliance" {
		t.Errorf("resourceUri = %q, want ui://jc/compliance", uri)
	}
}

func TestComplianceViewResource_ServesHTMLWithInjection(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	result, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "ui://jc/compliance"})
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
	if !strings.Contains(c.Text, "JumpCloud Compliance Snapshot") {
		t.Error("served HTML missing page title")
	}
}
