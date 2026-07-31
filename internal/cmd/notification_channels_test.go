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
	"testing"

	"github.com/spf13/viper"
)

// startNotificationChannelsServer mirrors the live /notifications/channels
// shape (probed 2026-07-31): list wrapped in {channels, count}, id under
// objectId, single GET/POST/PATCH wrapped in {channel}, DELETE → {}.
func startNotificationChannelsServer(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	channel := map[string]any{
		"objectId": "aaa111aaa111aaa111aaa111", "organizationObjectId": "org1",
		"name": "Ops Webhook", "description": "ops alerts", "type": "CHANNEL_TYPE_WEBHOOK",
		"enabled":   true,
		"config":    map[string]any{"webhook": map[string]any{"objectId": "wh1", "url": "https://example.com/hook", "sslVerification": true}},
		"createdAt": "2026-01-01T00:00:00Z", "createdBy": "admin1", "updatedAt": "2026-01-02T00:00:00Z", "updatedBy": "admin1",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case p == "/notifications/channels" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"channels": []map[string]any{channel}, "count": 1})
		case p == "/notifications/channels" && r.Method == http.MethodPost:
			if capture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, capture)
			}
			json.NewEncoder(w).Encode(map[string]any{"channel": channel})
		case strings.HasPrefix(p, "/notifications/channels/") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"channel": channel})
		case strings.HasPrefix(p, "/notifications/channels/") && r.Method == http.MethodPatch:
			if capture != nil {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, capture)
			}
			json.NewEncoder(w).Encode(map[string]any{"channel": channel})
		case strings.HasPrefix(p, "/notifications/channels/") && r.Method == http.MethodDelete:
			if capture != nil {
				(*capture)["deletedPath"] = p
			}
			w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected: %s %s", r.Method, p)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runChannels(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	viper.Set("cache.enabled", false)
	root := NewRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{"notification-channels"}, args...))
	err := root.Execute()
	return out.String(), errBuf.String(), err
}

func TestChannelsList(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startNotificationChannelsServer(t, nil).URL)
	out, errBuf, err := runChannels(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var rows []map[string]any
	if e := json.Unmarshal([]byte(out), &rows); e != nil || len(rows) != 1 {
		t.Fatalf("ResponseKey unwrap failed: %v\n%s", e, out)
	}
	if rows[0]["name"] != "Ops Webhook" {
		t.Errorf("row = %v", rows[0])
	}
	if !strings.Contains(errBuf, "1 items") {
		t.Errorf("footer missing: %s", errBuf)
	}
}

func TestChannelsList_IDs(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startNotificationChannelsServer(t, nil).URL)
	out, _, err := runChannels(t, "list", "--ids")
	if err != nil {
		t.Fatalf("list --ids: %v", err)
	}
	if strings.TrimSpace(out) != "aaa111aaa111aaa111aaa111" {
		t.Errorf("--ids must emit objectId, got %q", out)
	}
}

func TestChannelsGet_Unwraps(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startNotificationChannelsServer(t, nil).URL)
	out, _, err := runChannels(t, "get", "Ops Webhook")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var obj map[string]any
	if e := json.Unmarshal([]byte(out), &obj); e != nil {
		t.Fatalf("not JSON: %v", e)
	}
	if obj["name"] != "Ops Webhook" {
		t.Errorf("get did not unwrap channel: %v", obj)
	}
	if _, wrapped := obj["channel"]; wrapped {
		t.Error("response still wrapped in channel envelope")
	}
}

func TestChannelsCreate_WebhookBody(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startNotificationChannelsServer(t, &body).URL)
	if _, _, err := runChannels(t, "create", "--name", "Ops Webhook", "--type", "webhook",
		"--url", "https://example.com/hook", "--description", "ops alerts"); err != nil {
		t.Fatalf("create: %v", err)
	}
	ch, ok := body["channel"].(map[string]any)
	if !ok {
		t.Fatalf("POST body not wrapped in channel: %v", body)
	}
	if ch["name"] != "Ops Webhook" || ch["type"] != "CHANNEL_TYPE_WEBHOOK" {
		t.Errorf("channel name/type wrong: %v", ch)
	}
	cfg, _ := ch["config"].(map[string]any)
	wh, _ := cfg["webhook"].(map[string]any)
	if wh["url"] != "https://example.com/hook" {
		t.Errorf("webhook url not set: %v", ch["config"])
	}
	if ch["enabled"] != true {
		t.Errorf("enabled default should be true: %v", ch["enabled"])
	}
}

func TestChannelsCreate_WebhookRequiresURL(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startNotificationChannelsServer(t, nil).URL)
	if _, _, err := runChannels(t, "create", "--name", "X", "--type", "webhook"); err == nil || !strings.Contains(err.Error(), "require --url") {
		t.Fatalf("expected url-required error, got %v", err)
	}
}

func TestChannelsCreate_InvalidType(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startNotificationChannelsServer(t, nil).URL)
	if _, _, err := runChannels(t, "create", "--name", "X", "--type", "sms"); err == nil || !strings.Contains(err.Error(), "invalid --type") {
		t.Fatalf("expected type error, got %v", err)
	}
}

func TestChannelsCreate_EmailNeedsConfigFile(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startNotificationChannelsServer(t, nil).URL)
	if _, _, err := runChannels(t, "create", "--name", "X", "--type", "email"); err == nil || !strings.Contains(err.Error(), "config-file") {
		t.Fatalf("expected config-file requirement for email, got %v", err)
	}
}

func TestChannelsCreate_ConfigFile(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startNotificationChannelsServer(t, &body).URL)
	dir := t.TempDir()
	cf := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(cf, []byte(`{"slack":{"slackChannel":[{"slackChannelId":"C123"}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runChannels(t, "create", "--name", "Team Slack", "--type", "slack", "--config-file", cf); err != nil {
		t.Fatalf("create with config-file: %v", err)
	}
	ch, _ := body["channel"].(map[string]any)
	cfg, _ := ch["config"].(map[string]any)
	sl, _ := cfg["slack"].(map[string]any)
	if sl == nil {
		t.Errorf("config-file slack config not applied: %v", ch["config"])
	}
	if ch["type"] != "CHANNEL_TYPE_SLACK" {
		t.Errorf("type not normalized: %v", ch["type"])
	}
}

// TestChannelsUpdate_RMW: partial update (only --enabled) must preserve
// name/type/config via RMW, PATCH wrapped {channel}, and strip server-managed
// timestamps/owner.
func TestChannelsUpdate_RMW(t *testing.T) {
	setupUsersTest(t)
	var patch map[string]any
	overrideV2Client(t, startNotificationChannelsServer(t, &patch).URL)
	if _, _, err := runChannels(t, "update", "aaa111aaa111aaa111aaa111", "--enabled=false"); err != nil {
		t.Fatalf("update: %v", err)
	}
	ch, ok := patch["channel"].(map[string]any)
	if !ok {
		t.Fatalf("PATCH body not wrapped in channel: %v", patch)
	}
	if ch["enabled"] != false {
		t.Errorf("enabled not applied: %v", ch["enabled"])
	}
	if ch["name"] != "Ops Webhook" || ch["type"] != "CHANNEL_TYPE_WEBHOOK" {
		t.Errorf("name/type clobbered by partial update: %v", ch)
	}
	if _, ok := ch["config"].(map[string]any); !ok {
		t.Errorf("config clobbered: %v", ch["config"])
	}
	for _, k := range []string{"createdAt", "createdBy", "updatedAt", "updatedBy", "organizationObjectId"} {
		if _, present := ch[k]; present {
			t.Errorf("server-managed %q must be stripped from PATCH body", k)
		}
	}
	// The channel's own objectId is kept (the update targets it).
	if ch["objectId"] != "aaa111aaa111aaa111aaa111" {
		t.Errorf("objectId should be preserved: %v", ch["objectId"])
	}
}

func TestChannels_PlanNoMutation(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	overrideV2Client(t, startNotificationChannelsServer(t, &body).URL)
	_, errBuf, err := runChannels(t, "create", "--name", "X", "--type", "webhook", "--url", "https://x.example", "--plan")
	var exitErr *ExitError
	if !errorAs(err, &exitErr) || exitErr.Code != 10 {
		t.Fatalf("expected plan ExitError(10), got: %v", err)
	}
	if body != nil {
		t.Errorf("--plan must not POST, captured: %v", body)
	}
	if !strings.Contains(errBuf, "Plan:") {
		t.Errorf("expected plan preview on stderr, got: %s", errBuf)
	}
}

func TestChannelsDelete(t *testing.T) {
	setupUsersTest(t)
	var cap = map[string]any{}
	overrideV2Client(t, startNotificationChannelsServer(t, &cap).URL)
	out, _, err := runChannels(t, "delete", "aaa111aaa111aaa111aaa111", "--force")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if cap["deletedPath"] != "/notifications/channels/aaa111aaa111aaa111aaa111" {
		t.Errorf("delete path: %v", cap["deletedPath"])
	}
	if !strings.Contains(out, "Ops Webhook") {
		t.Errorf("delete message should show resolved name, got: %q", out)
	}
}
