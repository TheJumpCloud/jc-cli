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

	"github.com/klaassen-consulting/jc/internal/plan"
)

type subServer struct {
	*httptest.Server
	mu      sync.Mutex
	methods []string
	putBody map[string][]map[string]any
	// ignore makes the PUT response echo the stored state unchanged.
	ignore bool
}

func startSubServer(t *testing.T, ignore bool) *subServer {
	t.Helper()
	state := []map[string]any{}
	for _, k := range []string{"a_one", "b_two", "c_three", "d_four", "e_five"} {
		state = append(state, map[string]any{"emailKey": k, "category": "cat", "title": "Title " + k, "subscribed": true})
	}
	s := &subServer{ignore: ignore}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.URL.Path != "/notifications/subscriptions/email" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		s.methods = append(s.methods, r.Method)
		if r.Method == http.MethodPut {
			var body map[string][]map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.putBody = body
			if !s.ignore {
				state = body["subscriptions"]
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"subscriptions": state})
	}))
	t.Cleanup(s.Close)
	return s
}

func runSub(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewRootCmd()
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errb.String(), err
}

func TestEmailSubscriptionsHelp(t *testing.T) {
	setupUsersTest(t)
	out, _, err := runSub(t, "admins", "email-subscriptions", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"list", "set", "calling administrator"} {
		if !strings.Contains(strings.ToLower(out), strings.ToLower(w)) {
			t.Errorf("help missing %q:\n%s", w, out)
		}
	}
}

func TestEmailSubscriptionsList(t *testing.T) {
	setupUsersTest(t)
	s := startSubServer(t, false)
	overrideV2Client(t, s.URL)
	out, _, err := runSub(t, "admins", "email-subscriptions", "list", "-t")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"emailKey", "category", "title", "subscribed", "a_one"} {
		if !strings.Contains(strings.ToLower(out), strings.ToLower(w)) {
			t.Errorf("table missing %q:\n%s", w, out)
		}
	}
	if len(s.methods) != 1 || s.methods[0] != http.MethodGet {
		t.Errorf("methods = %v, want one GET", s.methods)
	}
}

func TestEmailSubscriptionsSetSendsFullList(t *testing.T) {
	setupUsersTest(t)
	s := startSubServer(t, false)
	overrideV2Client(t, s.URL)
	if _, _, err := runSub(t, "admins", "email-subscriptions", "set", "c_three", "--subscribed=false", "--force"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.methods, ","); got != "GET,PUT" {
		t.Fatalf("methods = %s, want GET,PUT", got)
	}
	subs := s.putBody["subscriptions"]
	if len(subs) != 5 {
		t.Fatalf("PUT had %d entries, want 5", len(subs))
	}
	for _, e := range subs {
		want := e["emailKey"] != "c_three"
		if e["subscribed"] != want {
			t.Errorf("%v: subscribed = %v, want %v", e["emailKey"], e["subscribed"], want)
		}
	}
}

func TestEmailSubscriptionsSetUnknownKey(t *testing.T) {
	setupUsersTest(t)
	s := startSubServer(t, false)
	overrideV2Client(t, s.URL)
	_, _, err := runSub(t, "admins", "email-subscriptions", "set", "nope", "--subscribed=false", "--force")
	var cerr *CLIError
	if !errors.As(err, &cerr) || cerr.ExitCode() != ExitUsage {
		t.Fatalf("want usage error, got %v", err)
	}
	if !strings.Contains(cerr.Message, "nope") || !strings.Contains(cerr.Message, "a_one") {
		t.Errorf("message should name the key and list valid ones: %s", cerr.Message)
	}
	if len(s.methods) != 1 {
		t.Errorf("methods = %v, want only the GET", s.methods)
	}
}

func TestEmailSubscriptionsSetPlan(t *testing.T) {
	setupUsersTest(t)
	s := startSubServer(t, false)
	overrideV2Client(t, s.URL)
	_, errOut, err := runSub(t, "admins", "email-subscriptions", "set", "a_one", "--subscribed=false", "--plan")
	var xe *ExitError
	if !errors.As(err, &xe) || xe.Code != plan.ExitCodePlan {
		t.Fatalf("want exit 10, got %v", err)
	}
	if !strings.Contains(errOut, "a_one: subscribed true -> false") {
		t.Errorf("plan missing before/after:\n%s", errOut)
	}
	if len(s.methods) != 1 {
		t.Errorf("methods = %v, want only the GET", s.methods)
	}
}

func TestEmailSubscriptionsSetNoForceNoTTY(t *testing.T) {
	setupUsersTest(t)
	s := startSubServer(t, false)
	overrideV2Client(t, s.URL)
	_, errOut, _ := runSub(t, "admins", "email-subscriptions", "set", "a_one", "--subscribed=false")
	for _, m := range s.methods {
		if m == http.MethodPut {
			t.Fatalf("PUT sent without --force (stderr: %s)", errOut)
		}
	}
}

func TestEmailSubscriptionsSetVerifiesResponse(t *testing.T) {
	setupUsersTest(t)
	s := startSubServer(t, true)
	overrideV2Client(t, s.URL)
	_, _, err := runSub(t, "admins", "email-subscriptions", "set", "a_one", "--subscribed=false", "--force")
	if err == nil || !strings.Contains(err.Error(), "did not take effect") {
		t.Fatalf("want did-not-take-effect error, got %v", err)
	}
}

func TestEmailSubscriptionsNoAdminFlag(t *testing.T) {
	setupUsersTest(t)
	_, _, err := runSub(t, "admins", "email-subscriptions", "list", "--admin-id", "x")
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("want unknown flag, got %v", err)
	}
}
