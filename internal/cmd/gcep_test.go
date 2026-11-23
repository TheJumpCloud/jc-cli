package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/klaassen-consulting/jc/internal/gcep"
)

const (
	gcepTestSystemID = "5f0c1a2b3c4d5e6f7a8b9c0d"
	gcepTestConfig   = `{"organizationObjectId":"5ec71e8e96bfda0611fc6c5b","customerId":"C0abc123","googleUserEmail":"admin@example.com","state":"configuration_state_enabled","connectedState":"connected_state_connected","applicableOsFamilies":["mac_os","windows"],"configuredByEmail":"admin@example.com","configuredByName":"Ada Admin","configuredByUserId":"5ec71e8e96bfda0611fc6c5c","registeredAt":"2026-11-01T10:00:00Z"}`
)

type gcepRequest struct {
	Method string
	Path   string
	Body   string
}

// gcepServer serves the GCEP endpoints and the V1 /systems list for name
// resolution. config is the GET /gcep/configuration answer: "" means the
// not-configured 404. A PUT stores its body as the new configuration unless
// ignorePut is set, so a read-back can be made to disagree.
type gcepServer struct {
	*httptest.Server
	mu        sync.Mutex
	config    string
	ignorePut bool
	requests  []gcepRequest
}

func (s *gcepServer) log() []gcepRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]gcepRequest{}, s.requests...)
}

func (s *gcepServer) writes() []gcepRequest {
	var out []gcepRequest
	for _, r := range s.log() {
		if r.Method != http.MethodGet {
			out = append(out, r)
		}
	}
	return out
}

func startGCEPServer(t *testing.T, config string) *gcepServer {
	t.Helper()
	s := &gcepServer{config: config}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.requests = append(s.requests, gcepRequest{r.Method, r.URL.Path, string(body)})
		w.Header().Set("Content-Type", "application/json")
		switch p := r.URL.Path; {
		case p == "/gcep/configuration" && r.Method == http.MethodGet:
			if s.config == "" {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"code":5,"message":"configuration not found"}`))
				return
			}
			w.Write([]byte(s.config))
		case p == "/gcep/configuration" && r.Method == http.MethodPut:
			if !s.ignorePut {
				s.config = string(body)
			}
			w.Write([]byte(`{}`))
		case p == "/gcep/configuration" && r.Method == http.MethodDelete:
			s.config = ""
			w.Write([]byte(`{}`))
		case p == "/gcep/systems/"+gcepTestSystemID+"/device-state":
			w.Write([]byte(`{"registrationStatus":"registered","managementStatus":"managed","deviceUsers":[{"deviceUserId":"du-1","googleEmail":"ada@example.com","localDeviceUsername":"ada","lastSuccessfulReportedTime":"2026-11-20T09:00:00Z"}]}`))
		case strings.HasPrefix(p, "/gcep/systems/"):
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"system not found"}`))
		case p == "/systems":
			w.Write([]byte(`{"totalCount":1,"results":[{"_id":"` + gcepTestSystemID + `","hostname":"ada-macbook"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.Close)
	overrideV2Client(t, s.URL)
	overrideV1Client(t, s.URL)
	return s
}

func runGCEP(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	setupUsersTest(t)
	cmd := NewRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{"gcep"}, args...))
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	return ToCLIError(err).ExitCode()
}

// AC1
func TestGCEP_HelpListsSubcommands(t *testing.T) {
	root := NewRootCmd()
	g, _, err := root.Find([]string{"gcep"})
	if err != nil || g.Name() != "gcep" {
		t.Fatalf("gcep not registered: %v", err)
	}
	names := map[string]bool{}
	for _, c := range g.Commands() {
		names[c.Name()] = true
	}
	if !names["config"] || !names["device-state"] {
		t.Fatalf("gcep subcommands: %v", names)
	}
	cfg, _, _ := root.Find([]string{"gcep", "config"})
	names = map[string]bool{}
	for _, c := range cfg.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"get", "set", "delete"} {
		if !names[want] {
			t.Errorf("gcep config missing %s: %v", want, names)
		}
	}
}

// AC2: a configuration is printed unchanged.
func TestGCEPConfigGet_PrintsConfiguration(t *testing.T) {
	startGCEPServer(t, gcepTestConfig)
	out, _, err := runGCEP(t, "config", "get")
	if err != nil {
		t.Fatal(err)
	}
	var got, want map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	json.Unmarshal([]byte(gcepTestConfig), &want)
	gb, _ := json.Marshal(got)
	wb, _ := json.Marshal(want)
	if string(gb) != string(wb) {
		t.Fatalf("configuration changed:\n got %s\nwant %s", gb, wb)
	}
}

// AC2: every plausible not-configured answer gets the friendly message. The
// live answer was not probed (see internal/gcep), so all are covered.
func TestGCEPConfigGet_NotConfigured(t *testing.T) {
	for name, config := range map[string]string{
		"404":        "",
		"empty":      `{}`,
		"zero-value": `{"organizationObjectId":"5ec71e8e96bfda0611fc6c5b","customerId":"","googleUserEmail":"","state":"configuration_state_unspecified","connectedState":"connected_state_unspecified","applicableOsFamilies":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			startGCEPServer(t, config)
			out, _, err := runGCEP(t, "config", "get")
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if strings.TrimSpace(out) != gcep.NotConfiguredMessage {
				t.Fatalf("got %q", out)
			}
		})
	}
}

func TestGCEPConfigGet_OtherErrorsSurface(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"forbidden"}`))
	}))
	t.Cleanup(ts.Close)
	overrideV2Client(t, ts.URL)
	_, _, err := runGCEP(t, "config", "get")
	if exitCodeOf(err) != ExitPermission {
		t.Fatalf("want exit %d, got %v", ExitPermission, err)
	}
}

// AC3
func TestGCEPDeviceState_ByID(t *testing.T) {
	s := startGCEPServer(t, gcepTestConfig)
	out, _, err := runGCEP(t, "device-state", gcepTestSystemID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"registrationStatus": "registered"`) {
		t.Fatalf("output: %s", out)
	}
	log := s.log()
	if len(log) != 1 || log[0].Method != http.MethodGet || log[0].Path != "/gcep/systems/"+gcepTestSystemID+"/device-state" {
		t.Fatalf("requests: %+v", log)
	}
}

func TestGCEPDeviceState_ByNameResolves(t *testing.T) {
	s := startGCEPServer(t, gcepTestConfig)
	if _, _, err := runGCEP(t, "device-state", "ada-macbook", "--no-cache"); err != nil {
		t.Fatal(err)
	}
	var stateGets int
	for _, r := range s.log() {
		if r.Method != http.MethodGet {
			t.Errorf("non-GET: %+v", r)
		}
		if strings.HasPrefix(r.Path, "/gcep/") {
			stateGets++
			if r.Path != "/gcep/systems/"+gcepTestSystemID+"/device-state" {
				t.Errorf("unexpected path %s", r.Path)
			}
		}
	}
	if stateGets != 1 {
		t.Fatalf("want one device-state GET, got %d: %+v", stateGets, s.log())
	}
}

func TestGCEPDeviceState_MalformedIDRejectedLocally(t *testing.T) {
	s := startGCEPServer(t, gcepTestConfig)
	_, _, err := runGCEP(t, "device-state", "5f0c1a2b3c4d5e6f7a8b9c")
	if err == nil || !strings.Contains(err.Error(), "24-character hex") {
		t.Fatalf("want local rejection, got %v", err)
	}
	if exitCodeOf(err) != ExitUsage {
		t.Errorf("want exit %d, got %d", ExitUsage, exitCodeOf(err))
	}
	if n := len(s.log()); n != 0 {
		t.Fatalf("%d requests sent: %+v", n, s.log())
	}
}

func TestGCEPDeviceState_404ShowsServerMessage(t *testing.T) {
	startGCEPServer(t, gcepTestConfig)
	_, _, err := runGCEP(t, "device-state", "000000000000000000000000")
	if err == nil || !strings.Contains(err.Error(), "system not found") {
		t.Fatalf("got %v", err)
	}
	if exitCodeOf(err) == 0 {
		t.Fatal("want non-zero exit")
	}
}

func TestGCEPDeviceState_TableListsUsers(t *testing.T) {
	startGCEPServer(t, gcepTestConfig)
	out, _, err := runGCEP(t, "device-state", gcepTestSystemID, "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"DEVICEUSERID", "GOOGLEEMAIL", "LOCALDEVICEUSERNAME", "du-1", "ada@example.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %s:\n%s", want, out)
		}
	}
}

// AC4: GET first, then a complete PUT with only state changed, then read back.
func TestGCEPConfigSet_SendsCompleteBody(t *testing.T) {
	s := startGCEPServer(t, gcepTestConfig)
	if _, _, err := runGCEP(t, "config", "set", "--state", "disabled", "--force"); err != nil {
		t.Fatal(err)
	}
	log := s.log()
	if len(log) != 3 || log[0].Method != http.MethodGet || log[1].Method != http.MethodPut || log[2].Method != http.MethodGet {
		t.Fatalf("want GET, PUT, GET; got %+v", log)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(log[1].Body), &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"customerId":           "C0abc123",
		"googleUserEmail":      "admin@example.com",
		"state":                "configuration_state_disabled",
		"applicableOsFamilies": []any{"mac_os", "windows"},
	}
	gb, _ := json.Marshal(body)
	wb, _ := json.Marshal(want)
	if string(gb) != string(wb) {
		t.Fatalf("PUT body:\n got %s\nwant %s", gb, wb)
	}
}

func TestGCEPConfigSet_ReadBackMismatchFails(t *testing.T) {
	s := startGCEPServer(t, gcepTestConfig)
	s.ignorePut = true
	_, _, err := runGCEP(t, "config", "set", "--state", "disabled", "--force")
	if err == nil || !strings.Contains(err.Error(), "does not match") || !strings.Contains(err.Error(), "state") {
		t.Fatalf("want read-back mismatch, got %v", err)
	}
	if exitCodeOf(err) == 0 {
		t.Fatal("want non-zero exit")
	}
}

func TestGCEPConfigSet_CreateNeedsAllFields(t *testing.T) {
	s := startGCEPServer(t, "")
	_, _, err := runGCEP(t, "config", "set", "--state", "enabled", "--force")
	if err == nil || !strings.Contains(err.Error(), "--customer-id") || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("got %v", err)
	}
	if w := s.writes(); len(w) != 0 {
		t.Fatalf("writes sent: %+v", w)
	}

	if _, _, err := runGCEP(t, "config", "set", "--customer-id", "C0new", "--google-user-email", "a@example.com",
		"--state", "enabled", "--os", "mac_os", "--force"); err != nil {
		t.Fatal(err)
	}
	if w := s.writes(); len(w) != 1 || !strings.Contains(w[0].Body, `"customerId":"C0new"`) {
		t.Fatalf("writes: %+v", w)
	}
}

// AC5
func TestGCEPConfig_PlanSendsNoWrite(t *testing.T) {
	for name, args := range map[string][]string{
		"set":    {"config", "set", "--state", "disabled", "--plan", "-o", "json"},
		"delete": {"config", "delete", "--plan"},
	} {
		t.Run(name, func(t *testing.T) {
			s := startGCEPServer(t, gcepTestConfig)
			stdout, stderr, err := runGCEP(t, args...)
			if exitCodeOf(err) != 10 {
				t.Fatalf("want exit 10, got %v", err)
			}
			if w := s.writes(); len(w) != 0 {
				t.Fatalf("writes sent: %+v", w)
			}
			planText := stdout + stderr
			if !strings.Contains(planText, "C0abc123") {
				t.Errorf("plan does not name the customer id:\n%s", planText)
			}
			if name == "set" {
				for _, want := range []string{"configuration_state_enabled → configuration_state_disabled", "applicableOsFamilies: mac_os,windows (unchanged)", "googleUserEmail: admin@example.com (unchanged)"} {
					if !strings.Contains(stdout, want) {
						t.Errorf("plan missing %q:\n%s", want, stdout)
					}
				}
			}
		})
	}
}

func TestGCEPConfig_PlanWhenNotConfigured(t *testing.T) {
	for _, args := range [][]string{
		{"config", "set", "--state", "disabled", "--plan"},
		{"config", "delete", "--plan"},
	} {
		s := startGCEPServer(t, "")
		_, stderr, err := runGCEP(t, args...)
		if exitCodeOf(err) != 10 {
			t.Fatalf("%v: want exit 10, got %v", args, err)
		}
		if !strings.Contains(stderr, "not configured") {
			t.Errorf("%v: plan does not say not configured:\n%s", args, stderr)
		}
		if w := s.writes(); len(w) != 0 {
			t.Fatalf("writes sent: %+v", w)
		}
	}
}

// AC6: a non-terminal without --force refuses and sends nothing, even with
// --non-interactive.
func TestGCEPConfigDelete_NonInteractiveNeedsForce(t *testing.T) {
	for name, args := range map[string][]string{
		"piped":           {"config", "delete"},
		"non-interactive": {"config", "delete", "--non-interactive"},
	} {
		t.Run(name, func(t *testing.T) {
			s := startGCEPServer(t, gcepTestConfig)
			overrideIsStdinPiped(t, name == "piped")
			_, _, err := runGCEP(t, args...)
			if err == nil || !strings.Contains(err.Error(), "refusing") {
				t.Fatalf("want refusal, got %v", err)
			}
			if w := s.writes(); len(w) != 0 {
				t.Fatalf("writes sent: %+v", w)
			}
		})
	}
}

func TestGCEPConfigDelete_PromptsInTerminal(t *testing.T) {
	for answer, wantDelete := range map[string]bool{"y\n": true, "n\n": false} {
		s := startGCEPServer(t, gcepTestConfig)
		overrideIsStdinPiped(t, false)
		overrideConfirmReader(t, answer)
		_, stderr, err := runGCEP(t, "config", "delete")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stderr, `"C0abc123"`) || !strings.Contains(stderr, "whole organization") {
			t.Errorf("prompt: %q", stderr)
		}
		w := s.writes()
		if wantDelete != (len(w) == 1 && w[0].Method == http.MethodDelete && w[0].Path == "/gcep/configuration") {
			t.Fatalf("answer %q: writes %+v", answer, w)
		}
	}
}

func TestGCEPConfigDelete_Force(t *testing.T) {
	s := startGCEPServer(t, gcepTestConfig)
	overrideIsStdinPiped(t, true)
	if _, _, err := runGCEP(t, "config", "delete", "--force"); err != nil {
		t.Fatal(err)
	}
	if w := s.writes(); len(w) != 1 || w[0].Method != http.MethodDelete {
		t.Fatalf("writes: %+v", w)
	}
}

// AC7
func TestGCEPConfigSet_InvalidValuesExit2(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want []string
	}{
		"state":    {[]string{"--state", "bogus"}, []string{"enabled", "disabled"}},
		"os":       {[]string{"--os", "linux"}, []string{"mac_os", "windows", "linux"}},
		"os-mixed": {[]string{"--os", "mac_os,solaris"}, []string{"mac_os", "windows", "solaris"}},
	} {
		t.Run(name, func(t *testing.T) {
			s := startGCEPServer(t, gcepTestConfig)
			_, _, err := runGCEP(t, append([]string{"config", "set", "--plan"}, tc.args...)...)
			if exitCodeOf(err) != ExitUsage {
				t.Fatalf("want exit 2, got %v", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q missing %q", err, w)
				}
			}
			if strings.Contains(err.Error(), "configuration_state_bogus") {
				t.Errorf("error echoes a built enum: %q", err)
			}
			if n := len(s.log()); n != 0 {
				t.Fatalf("%d requests sent", n)
			}
		})
	}
}
