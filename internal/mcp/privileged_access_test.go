package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrivilegedAccessTools(t *testing.T) {
	setupToolTest(t)
	const db = "dddddddddddddddddddddddd"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/privileged-access/status":
			w.Write([]byte(`{"isActive":true,"isPam":true,"isExpired":false}`))
		case "/api/v2/privileged-access/databases":
			w.Write([]byte(`{"items":[{"id":"` + db + `","name":"Prod DB"}],"totalCount":1}`))
		case "/api/v2/privileged-access/databases/" + db + "/edit":
			w.Write([]byte(`{"database":{"id":"` + db + `","connectionString":"Host=x;Password=hunter2"}}`))
		case "/api/v2/privileged-access/credentials":
			w.Write([]byte(`{"results":[{"id":"cccccccccccccccccccccccc","name":"Corp card","last4Digits":"4242","cardholderName":"J Doe"}],"totalCount":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	defer ts.Close()
	overrideV2ClientForTest(t, ts.URL)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "privileged_access_database_get", map[string]any{"resource": "Prod DB"})
	if text := getResultText(t, res); res.IsError || strings.Contains(text, "hunter2") || !strings.Contains(text, "[redacted]") {
		t.Errorf("database get: %s", text)
	}

	res = callTool(t, cs, "privileged_access_credentials_list", map[string]any{})
	if text := getResultText(t, res); res.IsError || strings.Contains(text, "4242") || strings.Contains(text, "J Doe") || !strings.Contains(text, "Corp card") {
		t.Errorf("credentials list: %s", text)
	}

	res = callTool(t, cs, "privileged_access_credential_get", map[string]any{"id": "../status"})
	if !res.IsError {
		t.Error("a non-id must be rejected")
	}
}
