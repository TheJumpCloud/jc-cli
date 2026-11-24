package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type riskSettingsServer struct {
	*httptest.Server
	mu       sync.Mutex
	enabled  any
	puts     []string
	rawQuery []string
	putCode  int
	ignore   bool // accept the PUT without storing it
}

func startRiskSettingsServer(t *testing.T) *riskSettingsServer {
	t.Helper()
	s := &riskSettingsServer{enabled: true}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/access-risk-settings":
			s.rawQuery = append(s.rawQuery, r.URL.RawQuery)
			json.NewEncoder(w).Encode(map[string]any{"features": []any{map[string]any{
				"name": "identity_risk_detection", "category": "risk",
				"settings": map[string]any{"enabled": s.enabled, "trusted_ip_lists": []string{"203.0.113.0/24"}},
			}}})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/access-risk-settings/"):
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			b, _ := json.Marshal(body)
			s.puts = append(s.puts, r.URL.Path+" "+string(b))
			if s.putCode != 0 {
				w.WriteHeader(s.putCode)
				w.Write([]byte(`{"message":"server says no"}`))
				return
			}
			if !s.ignore && r.URL.Path == "/access-risk-settings/enabled" {
				s.enabled = body["value"]
			}
			json.NewEncoder(w).Encode(map[string]any{"name": "enabled", "value": body["value"]})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func runRisk(t *testing.T, s *riskSettingsServer, args ...string) (string, string, error) {
	t.Helper()
	setupUsersTest(t)
	overrideV2Client(t, s.URL)
	cmd := NewRootCmd()
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetArgs(append([]string{"identity-risk", "settings"}, args...))
	err := cmd.Execute()
	return out.String(), errb.String(), err
}

func riskExitOf(err error) int {
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	var ce *CLIError
	if errors.As(err, &ce) {
		return ce.ExitCode()
	}
	return -1
}

func TestRiskSettingsList_FiltersAndTable(t *testing.T) {
	s := startRiskSettingsServer(t)
	out, _, err := runRisk(t, s, "list", "-t",
		"--filter", "setting.name:eq:enabled", "--filter", "setting.name:eq:trusted_ip_lists")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.rawQuery) != 1 || strings.Count(s.rawQuery[0], "filter=") != 2 {
		t.Errorf("want one GET with two filter params, got %v", s.rawQuery)
	}
	for _, want := range []string{"enabled", "trusted_ip_lists", "203.0.113.0/24"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
}

func TestRiskSettingsSet_Plan(t *testing.T) {
	s := startRiskSettingsServer(t)
	_, errOut, err := runRisk(t, s, "set", "enabled", "--value", "false", "--plan")
	if riskExitOf(err) != 10 {
		t.Fatalf("want exit 10, got %v", err)
	}
	if len(s.puts) != 0 {
		t.Errorf("plan sent a PUT: %v", s.puts)
	}
	for _, want := range []string{"enabled", "true", "false"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("plan missing %q:\n%s", want, errOut)
		}
	}
}

func TestRiskSettingsSet_NoTTYRefuses(t *testing.T) {
	s := startRiskSettingsServer(t)
	_, _, err := runRisk(t, s, "set", "enabled", "--value", "false")
	if err == nil || len(s.puts) != 0 {
		t.Fatalf("want refusal without PUT, err=%v puts=%v", err, s.puts)
	}
}

func TestRiskSettingsSet_Rejections(t *testing.T) {
	cases := map[string][]string{
		"unknown name": {"set", "nope", "--value", "true", "--force"},
		"not json":     {"set", "enabled", "--value", "not-json", "--force"},
		"wrong type":   {"set", "enabled", "--value", `"yes"`, "--force"},
		"no value":     {"set", "enabled", "--force"},
		"empty file":   {"set", "enabled", "--value-file", "/dev/null", "--force"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			s := startRiskSettingsServer(t)
			_, _, err := runRisk(t, s, args...)
			if riskExitOf(err) != 2 {
				t.Fatalf("want exit 2, got %v", err)
			}
			if len(s.puts) != 0 {
				t.Errorf("PUT sent: %v", s.puts)
			}
		})
	}
	s := startRiskSettingsServer(t)
	_, _, err := runRisk(t, s, "set", "nope", "--value", "true", "--force")
	if !strings.Contains(err.Error(), "enabled") || !strings.Contains(err.Error(), "trusted_ip_lists") {
		t.Errorf("valid names not listed: %v", err)
	}
}

func TestRiskSettingsSet_ForceWritesAndVerifies(t *testing.T) {
	s := startRiskSettingsServer(t)
	_, _, err := runRisk(t, s, "set", "enabled", "--value", "false", "--force")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.puts) != 1 || s.puts[0] != `/access-risk-settings/enabled {"value":false}` {
		t.Errorf("unexpected PUTs: %v", s.puts)
	}
	if len(s.rawQuery) < 2 {
		t.Errorf("no read-back GET: %v", s.rawQuery)
	}
}

func TestRiskSettingsSet_ReadBackMismatchFails(t *testing.T) {
	s := startRiskSettingsServer(t)
	s.ignore = true
	_, _, err := runRisk(t, s, "set", "enabled", "--value", "false", "--force")
	if err == nil || !strings.Contains(err.Error(), "NOT changed") {
		t.Fatalf("want read-back failure, got %v", err)
	}
}

func TestRiskSettingsSet_ServerErrorsShown(t *testing.T) {
	for _, code := range []int{http.StatusConflict, http.StatusForbidden} {
		s := startRiskSettingsServer(t)
		s.putCode = code
		_, _, err := runRisk(t, s, "set", "enabled", "--value", "false", "--force")
		if err == nil || !strings.Contains(err.Error(), "server says no") {
			t.Errorf("%d: server message not shown: %v", code, err)
		}
	}
}
