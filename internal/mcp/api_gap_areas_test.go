package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/TheJumpCloud/jc-cli/internal/siem"
)

var gapWriteTools = []struct {
	tool string
	args map[string]any
}{
	{"insights_report_request", map[string]any{"type": "users-to-user-groups"}},
	{"email_subscription_set", map[string]any{"email_key": "lockout-notice-admin", "subscribed": false}},
}

func TestGapAreaWriteTools_ReadOnlyAndPlan(t *testing.T) {
	setupToolTest(t)
	overrideV2ClientForTest(t, readOnlyTripwire(t).URL)

	ro := connectToolTestServer(t, Options{ReadOnly: true})
	for _, c := range gapWriteTools {
		if out := getResultText(t, callTool(t, ro, c.tool, gapArgs(c.args, true))); !strings.Contains(out, "read-only") {
			t.Errorf("%s not refused in read-only mode: %s", c.tool, out)
		}
	}
	rw := connectToolTestServer(t, Options{})
	for _, c := range gapWriteTools {
		if out := getResultText(t, callTool(t, rw, c.tool, gapArgs(c.args, false))); !strings.Contains(out, `"plan"`) {
			t.Errorf("%s did not plan: %s", c.tool, out)
		}
	}
	if out := getResultText(t, callTool(t, rw, "insights_report_request", map[string]any{"type": "nope"})); strings.Contains(out, `"plan"`) {
		t.Errorf("an unknown report type must be refused before planning: %s", out)
	}
	if out := getResultText(t, callTool(t, ro, "identity_risk_setting_set",
		map[string]any{"name": "enabled", "value": "false", "execute": true})); !strings.Contains(out, "read-only") {
		t.Errorf("identity_risk_setting_set not refused in read-only mode: %s", out)
	}
}

func gapArgs(args map[string]any, execute bool) map[string]any {
	out := map[string]any{"execute": execute}
	for k, v := range args {
		out[k] = v
	}
	return out
}

// gapMCPServer stands in for the insights, data-export and v2 hosts.
type gapMCPServer struct {
	mu                 sync.Mutex
	subscribed         map[string]bool
	emailIgnoresWrites bool
	risk               map[string]any
	riskIgnoresWrites  bool
	riskPuts           []string
	siemQueries        []url.Values
}

func startGapMCPServer(t *testing.T) (*gapMCPServer, *httptest.Server) {
	t.Helper()
	g := &gapMCPServer{
		subscribed: map[string]bool{"lockout-notice-admin": true},
		risk:       map[string]any{"enabled": true, "dormant_account_threshold_days": float64(60)},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		p := strings.TrimPrefix(r.URL.Path, "/api/v2")
		switch {
		case p == "/insights/directory/v1/events/interval":
			w.Write([]byte(`{"buckets":[{"key_as_string":"2026-11-18T00:00:00Z","doc_count":4},{"doc_count":2}]}`))
		case p == "/data-export/v2/events":
			g.siemQueries = append(g.siemQueries, r.URL.Query())
			if strings.HasPrefix(r.URL.Query().Get("since"), "2099") {
				w.WriteHeader(http.StatusUnprocessableEntity)
				w.Write([]byte(`{"message":"not yet available"}`))
				return
			}
			w.Write([]byte(`{"events":null}`))
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

func TestGapAreaTools_Behaviour(t *testing.T) {
	setupToolTest(t)
	g, ts := startGapMCPServer(t)
	overrideV2ClientForTest(t, ts.URL)
	overrideInsightsClientForTest(t, ts.URL)
	cs := connectToolTestServer(t, Options{})
	text := func(name string, args map[string]any) (string, bool) {
		t.Helper()
		res := callTool(t, cs, name, args)
		return getResultText(t, res), res.IsError
	}

	t.Run("insights_event_histogram", func(t *testing.T) {
		out, isErr := text("insights_event_histogram", map[string]any{"last": "7d"})
		if isErr || !strings.Contains(out, `"returned": 2`) {
			t.Errorf("histogram: %s", out)
		}
	})

	t.Run("siem_events defaults and window", func(t *testing.T) {
		out, isErr := text("siem_events", map[string]any{})
		if isErr {
			t.Fatalf("siem_events: %s", out)
		}
		var m map[string]any
		json.Unmarshal([]byte(out), &m)
		if m["since"] == nil || m["until"] == nil || m["count"] != float64(0) {
			t.Errorf("the result must carry its window and an empty list: %s", out)
		}
		if ev, ok := m["events"].([]any); !ok || len(ev) != 0 {
			t.Errorf("null events must come back as []: %s", out)
		}
		q := g.siemQueries[len(g.siemQueries)-1]
		if q.Get("limit") != "100" || q.Get("event_types") != "all" {
			t.Errorf("defaults: %v", q)
		}
	})

	t.Run("siem_events 422 carries the lag hint", func(t *testing.T) {
		out, isErr := text("siem_events", map[string]any{"since": "2099-01-01T00:00:00Z"})
		if !isErr || !strings.Contains(out, siem.LagHint) {
			t.Errorf("want the lag hint: %s", out)
		}
	})

	t.Run("siem_events bare cursor", func(t *testing.T) {
		out, isErr := text("siem_events", map[string]any{"cursor": "CUR"})
		if !isErr || !strings.Contains(out, "since") {
			t.Errorf("a cursor without its window must be refused: %s", out)
		}
	})

	t.Run("identity_risk_setting_set sends a typed value", func(t *testing.T) {
		out, isErr := text("identity_risk_setting_set", map[string]any{"name": "dormant_account_threshold_days", "value": "90", "execute": true})
		if isErr || !strings.Contains(out, `"confirmed": true`) {
			t.Fatalf("set: %s", out)
		}
		if got := g.riskPuts[len(g.riskPuts)-1]; got != `{"value":90}` {
			t.Errorf("body: %s", got)
		}
	})

	t.Run("identity_risk_setting_set read-back mismatch", func(t *testing.T) {
		g.riskIgnoresWrites = true
		defer func() { g.riskIgnoresWrites = false }()
		out, isErr := text("identity_risk_setting_set", map[string]any{"name": "enabled", "value": "false", "execute": true})
		if !isErr || !strings.Contains(out, "reads back") {
			t.Errorf("want a read-back mismatch: %s", out)
		}
	})

	t.Run("email_subscription_set read-back mismatch", func(t *testing.T) {
		g.emailIgnoresWrites = true
		defer func() { g.emailIgnoresWrites = false }()
		out, isErr := text("email_subscription_set", map[string]any{"email_key": "lockout-notice-admin", "subscribed": false, "execute": true})
		if !isErr || !strings.Contains(out, "still reads subscribed=true") {
			t.Errorf("want a read-back mismatch: %s", out)
		}
	})

	t.Run("gcep_configuration not configured", func(t *testing.T) {
		out, isErr := text("gcep_configuration", map[string]any{})
		if !isErr || !strings.Contains(strings.ToLower(out), "not configured") {
			t.Errorf("a 404 must read as not configured: %s", out)
		}
	})
}

// The step-up prompt names what the write changes, not just the tool.
func TestGapAreaWriteTools_StepUpTarget(t *testing.T) {
	for _, c := range []struct {
		args any
		want string
	}{
		{riskSettingSetInput{Name: "enabled", Value: "false"}, "enabled=false"},
		{emailSubscriptionSetInput{EmailKey: "lockout-notice-admin", Subscribed: false}, "lockout-notice-admin subscribed=false"},
		{insightsReportRequestInput{Type: "users-to-devices"}, "report users-to-devices"},
	} {
		if got := destructiveTarget(c.args); got != c.want {
			t.Errorf("%T: got %q, want %q", c.args, got, c.want)
		}
	}
}
