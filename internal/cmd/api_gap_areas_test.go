package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/siem"
)

// gapServer stands in for the insights host (/insights/directory/v1), the
// data-export host (/data-export/v2) and the v2 API, with the live shapes.
type gapServer struct {
	mu sync.Mutex
	// emailIgnoresWrites makes PUT succeed without changing anything.
	emailIgnoresWrites bool
	subscribed         map[string]bool
	// riskIgnoresWrites likewise for the risk settings.
	riskIgnoresWrites bool
	risk              map[string]any
	riskPuts          []string
	siemPaths         []string
}

func startGapServer(t *testing.T) (*gapServer, *httptest.Server) {
	t.Helper()
	g := &gapServer{
		subscribed: map[string]bool{"lockout-notice-admin": true},
		risk:       map[string]any{"enabled": true, "dormant_account_threshold_days": float64(60), "trusted_asns": []any{float64(13335)}},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case p == "/insights/directory/v1/events/interval":
			w.Write([]byte(`{"buckets":[{"key_as_string":"2026-11-18T00:00:00Z","doc_count":4}]}`))
		case p == "/insights/directory/v1/reports":
			w.Write([]byte(`[{"id":"r1","type":"users-to-devices","status":"COMPLETED","artifacts":[{"id":"a1","format":"csv"}]},
				{"id":"r2","type":"os-version","status":"PENDING","artifacts":[]}]`))
		case p == "/insights/directory/v1/reports/r1/artifacts/a1/content":
			w.Header().Set("Content-Type", "text/csv")
			w.Write([]byte("user,device\nalice,mbp\n"))
		case p == "/data-export/v2/events":
			g.siemPaths = append(g.siemPaths, r.URL.RawQuery)
			if strings.HasPrefix(r.URL.Query().Get("since"), "2099") {
				w.WriteHeader(http.StatusUnprocessableEntity)
				w.Write([]byte(`{"message":"window not yet available","available_until":"2026-11-19T08:00:00Z"}`))
				return
			}
			w.Write([]byte(`{"events":[{"id":"e1","event_type":"user_login_attempt"}],"next_cursor":"CUR"}`))
		case p == "/notifications/subscriptions/email" && r.Method == http.MethodGet:
			var subs []map[string]any
			for k, v := range g.subscribed {
				subs = append(subs, map[string]any{"emailKey": k, "subscribed": v})
			}
			json.NewEncoder(w).Encode(map[string]any{"subscriptions": subs})
		case p == "/notifications/subscriptions/email" && r.Method == http.MethodPut:
			var body struct {
				Subscriptions []struct {
					EmailKey   string `json:"emailKey"`
					Subscribed bool   `json:"subscribed"`
				} `json:"subscriptions"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if !g.emailIgnoresWrites {
				for _, s := range body.Subscriptions {
					g.subscribed[s.EmailKey] = s.Subscribed
				}
			}
			w.WriteHeader(http.StatusNoContent)
		case p == "/access-risk-settings" && r.Method == http.MethodGet:
			var settings []map[string]any
			for k, v := range g.risk {
				settings = append(settings, map[string]any{"name": k, "value": v})
			}
			json.NewEncoder(w).Encode(map[string]any{"features": []map[string]any{{"name": "identity_risk_detection", "settings": settings}}})
		case strings.HasPrefix(p, "/access-risk-settings/") && r.Method == http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			g.riskPuts = append(g.riskPuts, string(b))
			var body struct {
				Value any `json:"value"`
			}
			json.Unmarshal(b, &body)
			if !g.riskIgnoresWrites {
				g.risk[strings.TrimPrefix(p, "/access-risk-settings/")] = body.Value
			}
			w.Write(b)
		case strings.HasPrefix(p, "/gcep/"):
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"not found"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ts.Close)
	return g, ts
}

// overrideGapClients points insights at <url>/insights/directory/v1, so the
// data-export host is derived from it as in production, and v2 at <url>.
func overrideGapClients(t *testing.T, url string) {
	t.Helper()
	old := newInsightsClient
	newInsightsClient = func() (*api.InsightsClient, error) {
		c := api.NewInsightsClientWithKey("test-key-1234")
		c.BaseURL = url + "/insights/directory/v1"
		return c, nil
	}
	t.Cleanup(func() { newInsightsClient = old })
	overrideV2Client(t, url)
}

func runGap(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestInsightsInterval(t *testing.T) {
	setupUsersTest(t)
	_, ts := startGapServer(t)
	overrideGapClients(t, ts.URL)
	out, _, err := runGap(t, "insights", "interval", "--last", "7d", "--interval", "1d")
	if err != nil || !strings.Contains(out, `"doc_count": 4`) {
		t.Errorf("interval: %v\n%s", err, out)
	}
	if _, _, err := runGap(t, "insights", "interval", "--last", "7d", "--interval", "1x"); err == nil {
		t.Error("a bad interval must be refused")
	}
}

func TestInsightsReportsDownload(t *testing.T) {
	setupUsersTest(t)
	_, ts := startGapServer(t)
	overrideGapClients(t, ts.URL)
	dir := t.TempDir()

	path := filepath.Join(dir, "r.csv")
	if _, _, err := runGap(t, "insights", "reports", "download", "r1", "csv", "--out", path); err != nil {
		t.Fatalf("download: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("want a 0600 file, got %v %v", fi, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "user,device\nalice,mbp\n" {
		t.Errorf("content: %q", b)
	}

	// An existing file is not overwritten (it would keep its own mode).
	existing := filepath.Join(dir, "old.csv")
	os.WriteFile(existing, []byte("keep"), 0o644)
	if _, _, err := runGap(t, "insights", "reports", "download", "r1", "csv", "--out", existing); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Errorf("an existing file must be refused, got %v", err)
	}
	if b, _ := os.ReadFile(existing); string(b) != "keep" {
		t.Errorf("the existing file was changed: %q", b)
	}

	// A report that is not COMPLETED cannot be downloaded.
	if _, _, err := runGap(t, "insights", "reports", "download", "r2", "csv", "--out", filepath.Join(dir, "x")); err == nil ||
		!strings.Contains(err.Error(), "PENDING") {
		t.Errorf("a PENDING report must be refused, got %v", err)
	}

	// Stdout that is not a terminal (here a buffer) receives the content.
	out, _, err := runGap(t, "insights", "reports", "download", "r1", "a1")
	if err != nil || out != "user,device\nalice,mbp\n" {
		t.Errorf("stdout download: %q %v", out, err)
	}
}

func TestSIEMEvents(t *testing.T) {
	setupUsersTest(t)
	g, ts := startGapServer(t)
	overrideGapClients(t, ts.URL)

	// Default window, through the derived data-export host; the next-page
	// hint carries the window the cursor belongs to.
	_, errOut, err := runGap(t, "siem", "events")
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if !strings.Contains(errOut, "--since ") || !strings.Contains(errOut, "--until ") || !strings.Contains(errOut, "--cursor CUR") {
		t.Errorf("the next-page hint must carry the window: %q", errOut)
	}

	// --hours-ago takes a whole number of hours, as its help says.
	if _, _, err := runGap(t, "siem", "events", "--hours-ago", "5"); err != nil {
		t.Errorf("--hours-ago 5: %v", err)
	}

	// A cursor without its window is refused before any request.
	before := len(g.siemPaths)
	if _, _, err := runGap(t, "siem", "events", "--cursor", "CUR"); err == nil || !strings.Contains(err.Error(), "--since") {
		t.Errorf("a bare cursor must be refused, got %v", err)
	}
	if len(g.siemPaths) != before {
		t.Error("a bare cursor still reached the API")
	}

	// A 422 (window past the writer) carries the lag hint.
	_, _, err = runGap(t, "siem", "events", "--since", "2099-01-01T00:00:00Z")
	if err == nil || !strings.Contains(err.Error(), siem.LagHint) {
		t.Errorf("a 422 must carry the lag hint, got %v", err)
	}
}

func TestEmailSubscriptions(t *testing.T) {
	setupUsersTest(t)
	g, ts := startGapServer(t)
	overrideGapClients(t, ts.URL)

	if _, errOut, err := runGap(t, "email-subscriptions", "unsubscribe", "lockout-notice-admin", "--force"); err != nil ||
		!strings.Contains(errOut, "confirmed") {
		t.Fatalf("unsubscribe: %v %s", err, errOut)
	}
	if g.subscribed["lockout-notice-admin"] {
		t.Error("the subscription did not change")
	}

	// The read-back catches a write the API accepted but did not apply.
	g.emailIgnoresWrites = true
	if _, _, err := runGap(t, "email-subscriptions", "subscribe", "lockout-notice-admin", "--force"); err == nil ||
		!strings.Contains(err.Error(), "still reads subscribed=false") {
		t.Errorf("want a read-back mismatch, got %v", err)
	}

	// --plan (which exits 10 by design) reports the change as reversible.
	out, _, _ := runGap(t, "email-subscriptions", "subscribe", "lockout-notice-admin", "--plan", "-o", "json")
	if !strings.Contains(out, `"reversible": true`) {
		t.Errorf("the plan must say reversible:\n%s", out)
	}
}

func TestIdentityRiskSettingsSet(t *testing.T) {
	setupUsersTest(t)
	g, ts := startGapServer(t)
	overrideGapClients(t, ts.URL)

	if _, _, err := runGap(t, "identity-risk", "settings", "set", "dormant_account_threshold_days", "90", "--force"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := g.riskPuts[len(g.riskPuts)-1]; got != `{"value":90}` {
		t.Errorf("the number must be sent as a number, got %s", got)
	}
	if _, _, err := runGap(t, "identity-risk", "settings", "set", "trusted_asns", "13335,15169", "--force"); err != nil {
		t.Fatalf("set list: %v", err)
	}
	if got := g.riskPuts[len(g.riskPuts)-1]; got != `{"value":[13335,15169]}` {
		t.Errorf("a numeric list must be sent as numbers, got %s", got)
	}
	if _, _, err := runGap(t, "identity-risk", "settings", "set", "dormant_account_threshold_days", "ninety", "--force"); err == nil {
		t.Error("a non-number must be refused before the API")
	}

	g.riskIgnoresWrites = true
	if _, _, err := runGap(t, "identity-risk", "settings", "set", "enabled", "false", "--force"); err == nil ||
		!strings.Contains(err.Error(), "reads back") {
		t.Errorf("want a read-back mismatch, got %v", err)
	}
}

func TestGCEPNotConfigured(t *testing.T) {
	setupUsersTest(t)
	_, ts := startGapServer(t)
	overrideGapClients(t, ts.URL)
	if _, _, err := runGap(t, "gcep", "config"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "not configured") {
		t.Errorf("a 404 must read as not configured, got %v", err)
	}
}
