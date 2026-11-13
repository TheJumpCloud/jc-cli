package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const (
	acRunID      = "6ad0e800617e39fb0663b365"
	acCampaignID = "6ad0e800617e39fb0663b364"
	acSignedURL  = "https://bucket.example.test/report.csv?X-Amz-Signature=fixture"
)

type acServer struct {
	*httptest.Server
	mu      sync.Mutex
	queries []string
}

func (s *acServer) lastQueries() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.queries...)
}

// startAccessCertServer fails the test on any non-GET request and serves a
// 250-row campaign list, three runs (one per status) and an audit report
// whose artifacts field is an object keyed by format.
func startAccessCertServer(t *testing.T) *acServer {
	t.Helper()
	s := &acServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("non-GET request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/access-certification/") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		s.mu.Lock()
		s.queries = append(s.queries, r.URL.Path+"?"+r.URL.RawQuery)
		s.mu.Unlock()
		q := r.URL.Query()
		skip, _ := strconv.Atoi(q.Get("skip"))
		limit, _ := strconv.Atoi(q.Get("limit"))
		if limit > 100 {
			t.Errorf("limit %d exceeds 100", limit)
		}
		page := func(all []map[string]any) {
			end := skip + limit
			if end > len(all) {
				end = len(all)
			}
			if skip > end {
				skip = end
			}
			env := map[string]any{"results": all[skip:end], "totalCount": len(all)}
			if q.Get("metadata") == "true" {
				env["metadata"] = map[string]any{"applicationIds": []string{}, "userGroupIds": []string{"a"}}
			}
			json.NewEncoder(w).Encode(env)
		}
		switch p := r.URL.Path; {
		case p == "/access-certification/settings":
			w.Write([]byte(`{"featureEnabled":true,"aiSuggestionsEnabled":false,"platforms":[]}`))
		case p == "/access-certification/campaigns":
			var rows []map[string]any
			for i := 0; i < 250; i++ {
				rows = append(rows, map[string]any{"id": fmt.Sprintf("%024x", i), "name": fmt.Sprintf("c%d", i), "status": "active", "enabled": true, "scopeType": "group", "scheduleMode": "manual", "lastRunAt": nil})
			}
			page(rows)
		case p == "/access-certification/campaigns/"+acCampaignID:
			w.Write([]byte(`{"id":"` + acCampaignID + `","orgId":"o","status":"active","config":{"name":"Group Memberships"}}`))
		case strings.HasPrefix(p, "/access-certification/campaigns/"):
			w.WriteHeader(404)
			w.Write([]byte(`{"message":"campaign not found"}`))
		case p == "/access-certification/campaign-runs":
			status := "active"
			for _, f := range q["filter"] {
				if strings.HasPrefix(f, "status:eq:") {
					status = strings.TrimPrefix(f, "status:eq:")
				}
			}
			page([]map[string]any{{"runId": "run-" + status, "status": status}})
		case p == "/access-certification/campaign-runs/"+acRunID:
			w.Write([]byte(`{"runId":"` + acRunID + `","status":"closed"}`))
		case p == "/access-certification/campaign-runs/"+acRunID+"/review-items":
			var rows []map[string]any
			for i := 0; i < 6; i++ {
				rows = append(rows, map[string]any{"id": fmt.Sprintf("%024x", i), "finalDecision": "pending", "remediationAction": "no_action", "beforeSnapshot": map[string]any{"username": fmt.Sprintf("user%d", i)}})
			}
			page(rows)
		case p == "/access-certification/campaign-runs/"+acRunID+"/audit-report":
			w.Write([]byte(`{"campaignRunId":"` + acRunID + `","status":"exports_ready","documentHash":"abc123","artifacts":{"csv":{"url":"` + acSignedURL + `","expiresAt":"2026-11-13T10:00:00Z"},"json":{"url":"` + acSignedURL + `","expiresAt":"2026-11-13T10:00:00Z"},"pdf":{"url":"` + acSignedURL + `","expiresAt":"2026-11-13T10:00:00Z"}}}`))
		case strings.HasSuffix(p, "/audit-report"):
			w.WriteHeader(409)
			w.Write([]byte(`{"message":"audit report is only available for closed campaign runs"}`))
		case p == "/access-certification/admin-queue":
			page(nil)
		case strings.HasPrefix(p, "/access-certification/campaign-runs/"):
			w.WriteHeader(404)
			w.Write([]byte(`{"message":"campaign run not found"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(s.Close)
	overrideV2Client(t, s.URL)
	return s
}

func runAccessCert(t *testing.T, args ...string) (string, error) {
	t.Helper()
	setupUsersTest(t)
	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(append([]string{"access-cert"}, args...))
	err := cmd.Execute()
	return buf.String(), err
}

func TestAccessCertCampaignsList_PagingAllRows(t *testing.T) {
	s := startAccessCertServer(t)
	out, err := runAccessCert(t, "campaigns", "list")
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 250 {
		t.Fatalf("got %d rows, want 250", len(rows))
	}
	if q := s.lastQueries(); len(q) != 3 {
		t.Fatalf("want 3 pages, got %v", q)
	}
}

func TestAccessCertCampaignsList_LimitSortFilterPassThrough(t *testing.T) {
	s := startAccessCertServer(t)
	out, err := runAccessCert(t, "campaigns", "list", "--limit", "120", "--sort", "-name", "--filter", "name:eq:x", "--filter", "scopeType:eq:group")
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	json.Unmarshal([]byte(out), &rows)
	if len(rows) != 120 {
		t.Fatalf("got %d rows, want 120", len(rows))
	}
	qs := s.lastQueries()
	if len(qs) != 2 || !strings.Contains(qs[1], "skip=100") || !strings.Contains(qs[1], "limit=20") {
		t.Fatalf("unexpected paging: %v", qs)
	}
	for _, want := range []string{"sort=-name", "filter=name%3Aeq%3Ax", "filter=scopeType%3Aeq%3Agroup"} {
		if !strings.Contains(qs[0], want) {
			t.Errorf("query %q missing %q", qs[0], want)
		}
	}
}

func TestAccessCertCampaignsList_Table(t *testing.T) {
	startAccessCertServer(t)
	out, err := runAccessCert(t, "campaigns", "list", "--limit", "1", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"NAME", "ID", "STATUS", "ENABLED", "SCOPETYPE", "SCHEDULEMODE", "LASTRUNAT"} {
		if !strings.Contains(out, h) {
			t.Errorf("table missing %s:\n%s", h, out)
		}
	}
}

// TestAccessCertList_MetadataKeepsEmptyArrays: the live API sends
// applicationIds:[] next to a non-empty userGroupIds; both keys must survive.
func TestAccessCertList_MetadataKeepsEmptyArrays(t *testing.T) {
	for name, args := range map[string][]string{
		"campaigns": {"campaigns", "list", "--limit", "1", "--metadata"},
		"runs":      {"runs", "list", "--status", "all", "--metadata"},
	} {
		t.Run(name, func(t *testing.T) {
			startAccessCertServer(t)
			out, err := runAccessCert(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			var page struct {
				Metadata map[string]json.RawMessage `json:"metadata"`
			}
			if err := json.Unmarshal([]byte(out), &page); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if got := compactJSON(t, page.Metadata["applicationIds"]); got != "[]" {
				t.Errorf("applicationIds = %s, want []: %s", got, out)
			}
			if got := compactJSON(t, page.Metadata["userGroupIds"]); got != `["a"]` {
				t.Errorf(`userGroupIds = %s, want ["a"]: %s`, got, out)
			}
		})
	}
}

func compactJSON(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	if raw == nil {
		return "<missing>"
	}
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestAccessCertCampaignsGet_Unchanged(t *testing.T) {
	startAccessCertServer(t)
	out, err := runAccessCert(t, "campaigns", "get", acCampaignID)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	cfg, _ := got["config"].(map[string]any)
	if got["orgId"] != "o" || cfg["name"] != "Group Memberships" {
		t.Fatalf("unexpected: %s", out)
	}
}

func TestAccessCertRunsList_Status(t *testing.T) {
	cases := map[string][]string{
		"":         {"run-active"},
		"closed":   {"run-closed"},
		"all":      {"run-active", "run-closed", "run-canceled"},
		"canceled": {"run-canceled"},
	}
	for status, want := range cases {
		t.Run("status="+status, func(t *testing.T) {
			s := startAccessCertServer(t)
			args := []string{"runs", "list"}
			if status != "" {
				args = append(args, "--status", status)
			}
			out, err := runAccessCert(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			var rows []map[string]any
			json.Unmarshal([]byte(out), &rows)
			var got []string
			for _, r := range rows {
				got = append(got, r["runId"].(string))
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("got %v want %v", got, want)
			}
			qs := strings.Join(s.lastQueries(), " ")
			if status == "" && strings.Contains(qs, "filter=") {
				t.Errorf("default must send no status filter: %s", qs)
			}
			if status == "closed" && !strings.Contains(qs, "filter=status%3Aeq%3Aclosed") {
				t.Errorf("closed must filter: %s", qs)
			}
		})
	}
}

func TestAccessCertRunsList_InvalidStatusSendsNoRequest(t *testing.T) {
	s := startAccessCertServer(t)
	_, err := runAccessCert(t, "runs", "list", "--status", "bogus")
	if err == nil || !strings.Contains(err.Error(), "active, closed, canceled, all") {
		t.Fatalf("want status error, got %v", err)
	}
	if len(s.lastQueries()) != 0 {
		t.Fatal("request was sent")
	}
}

func TestAccessCertRunsList_HelpMentionsClosed(t *testing.T) {
	out, err := runAccessCert(t, "runs", "list", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "--status closed or --status all") {
		t.Fatalf("help missing closed guidance:\n%s", out)
	}
}

func TestAccessCert_InvalidIDSendsNoRequest(t *testing.T) {
	s := startAccessCertServer(t)
	for _, c := range [][]string{{"campaigns", "get"}, {"runs", "get"}, {"runs", "items"}, {"runs", "audit-report"}} {
		for _, id := range []string{"not-an-id", acRunID[:23], acRunID[:23] + "Z"} {
			_, err := runAccessCert(t, append(c, id)...)
			if err == nil || !strings.Contains(err.Error(), "24-character hex") {
				t.Errorf("%v %s: want id-format error, got %v", c, id, err)
			}
		}
	}
	if len(s.lastQueries()) != 0 {
		t.Fatal("request was sent")
	}
}

func TestAccessCertRunsItems_Table(t *testing.T) {
	startAccessCertServer(t)
	out, err := runAccessCert(t, "runs", "items", acRunID, "-o", "table", "--limit", "2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "USERNAME") || !strings.Contains(out, "user1") || strings.Contains(out, "user2") {
		t.Fatalf("unexpected table:\n%s", out)
	}
}

func TestAccessCertAuditReport_URLs(t *testing.T) {
	startAccessCertServer(t)
	out, err := runAccessCert(t, "runs", "audit-report", acRunID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, acSignedURL) {
		t.Fatalf("urls missing: %s", out)
	}
	out, err = runAccessCert(t, "runs", "audit-report", acRunID, "--no-urls")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "https://") || strings.Contains(out, `"url"`) {
		t.Fatalf("url leaked: %s", out)
	}
	if !strings.Contains(out, "expiresAt") || !strings.Contains(out, "abc123") {
		t.Fatalf("metadata lost: %s", out)
	}
}

func TestAccessCertAuditReport_409AndNotFound(t *testing.T) {
	startAccessCertServer(t)
	_, err := runAccessCert(t, "runs", "audit-report", "000000000000000000000001")
	if err == nil || !strings.Contains(err.Error(), "audit report is only available for closed campaign runs") {
		t.Fatalf("409: %v", err)
	}
	_, err = runAccessCert(t, "runs", "get", "000000000000000000000000")
	if err == nil || !strings.Contains(err.Error(), "campaign run not found") {
		t.Fatalf("404: %v", err)
	}
	_, err = runAccessCert(t, "campaigns", "get", "000000000000000000000000")
	if err == nil || !strings.Contains(err.Error(), "campaign not found") {
		t.Fatalf("404: %v", err)
	}
}

// TestAccessCert_EveryCommandOnlyGETs runs every command against a server
// that fails the test on any non-GET method.
func TestAccessCert_EveryCommandOnlyGETs(t *testing.T) {
	s := startAccessCertServer(t)
	for _, args := range [][]string{
		{"settings", "get"}, {"campaigns", "list"}, {"campaigns", "get", acCampaignID},
		{"runs", "list", "--status", "all"}, {"runs", "get", acRunID}, {"runs", "items", acRunID},
		{"runs", "audit-report", acRunID}, {"admin-queue"},
	} {
		if _, err := runAccessCert(t, args...); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
	if len(s.lastQueries()) == 0 {
		t.Fatal("no requests recorded")
	}
}

func TestAccessCert_AliasAndNoWriteCommands(t *testing.T) {
	setupUsersTest(t)
	root := NewRootCmd()
	c, _, err := root.Find([]string{"access-certification", "runs"})
	if err != nil || c.Name() != "runs" {
		t.Fatalf("alias: %v", err)
	}
	var names []string
	for _, sub := range c.Commands() {
		names = append(names, sub.Name())
	}
	if got := strings.Join(names, ","); got != "audit-report,get,items,list" {
		t.Fatalf("runs subcommands: %s", got)
	}
}
