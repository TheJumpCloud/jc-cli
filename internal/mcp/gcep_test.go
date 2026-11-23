package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const gcepMCPSystemID = "5f0c1a2b3c4d5e6f7a8b9c0d"

// startGCEPMCPServer fails the test on any non-GET request. notConfigured
// makes GET /gcep/configuration answer 404.
func startGCEPMCPServer(t *testing.T, notConfigured bool) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("non-GET request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch p := r.URL.Path; {
		case p == "/api/v2/gcep/configuration" && notConfigured:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"configuration not found"}`))
		case p == "/api/v2/gcep/configuration":
			w.Write([]byte(`{"customerId":"C0abc123","googleUserEmail":"admin@example.com","state":"configuration_state_enabled","applicableOsFamilies":["mac_os"]}`))
		case p == "/api/v2/gcep/systems/"+gcepMCPSystemID+"/device-state":
			w.Write([]byte(`{"registrationStatus":"registered","managementStatus":"managed","deviceUsers":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"system not found"}`))
		}
	}))
	t.Cleanup(ts.Close)
	overrideV2ClientForTest(t, ts.URL)
}

func TestGCEP_OnlyReadToolsRegistered(t *testing.T) {
	startGCEPMCPServer(t, false)
	cs := connectToolTestServer(t, Options{})
	tools, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tool := range tools.Tools {
		if strings.HasPrefix(tool.Name, "gcep") {
			got = append(got, tool.Name)
		}
	}
	if strings.Join(got, ",") != "gcep_config_get,gcep_device_state_get" &&
		strings.Join(got, ",") != "gcep_device_state_get,gcep_config_get" {
		t.Fatalf("gcep tools: %v", got)
	}
}

func TestGCEP_ToolsGETOnly(t *testing.T) {
	startGCEPMCPServer(t, false)
	cs := connectToolTestServer(t, Options{})
	res := callTool(t, cs, "gcep_config_get", map[string]any{})
	if res.IsError || !strings.Contains(getResultText(t, res), "C0abc123") {
		t.Fatalf("config: %s", getResultText(t, res))
	}
	res = callTool(t, cs, "gcep_device_state_get", map[string]any{"systemId": gcepMCPSystemID})
	if res.IsError || !strings.Contains(getResultText(t, res), "registered") {
		t.Fatalf("device state: %s", getResultText(t, res))
	}
}

func TestGCEPConfigGet_NotConfigured(t *testing.T) {
	startGCEPMCPServer(t, true)
	cs := connectToolTestServer(t, Options{})
	res := callTool(t, cs, "gcep_config_get", map[string]any{})
	text := getResultText(t, res)
	if res.IsError || !strings.Contains(text, `"configured": false`) {
		t.Fatalf("got %v %s", res.IsError, text)
	}
}

func TestGCEPDeviceState_Errors(t *testing.T) {
	startGCEPMCPServer(t, false)
	cs := connectToolTestServer(t, Options{})
	res := callTool(t, cs, "gcep_device_state_get", map[string]any{"systemId": "bad-id"})
	if !res.IsError || !strings.Contains(getResultText(t, res), "24-character hex") {
		t.Fatalf("got %s", getResultText(t, res))
	}
	res = callTool(t, cs, "gcep_device_state_get", map[string]any{"systemId": "000000000000000000000000"})
	if !res.IsError || !strings.Contains(getResultText(t, res), "system not found") {
		t.Fatalf("got %s", getResultText(t, res))
	}
}
