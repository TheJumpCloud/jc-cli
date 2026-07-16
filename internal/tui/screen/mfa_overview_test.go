package screen

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/klaassen-consulting/jc/internal/api"
)

func overrideMFAOverviewClient(t *testing.T, url string) {
	t.Helper()
	orig := newV1ClientForMFAOverview
	newV1ClientForMFAOverview = func() (*api.V1Client, error) {
		c := api.NewV1ClientWithKey("test-key")
		c.BaseURL = url
		return c, nil
	}
	t.Cleanup(func() { newV1ClientForMFAOverview = orig })
}

func startMFAOverviewServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/organizations":
			_, _ = w.Write([]byte(`{"totalCount":1,"results":[{"id":"org-1"}]}`))
		case "/organizations/org-1":
			_, _ = w.Write([]byte(`{"id":"org-1","settings":{
				"requireAdminMFA":true,
				"passwordPolicy":{"allowUnenrolledMFAPasswordReset":false}}}`))
		case "/systemusers":
			_, _ = w.Write([]byte(`{"totalCount":3,"results":[
				{"username":"alice","mfa":{"exclusion":false},
				 "mfaEnrollment":{"overallStatus":"ENROLLED","totpStatus":"ENROLLED","webAuthnStatus":"ENROLLED","pushStatus":"NOT_ENROLLED","jcGoStatus":"NOT_ENROLLED","smsStatus":"NOT_ENROLLED"}},
				{"username":"bob","mfa":{"exclusion":true},
				 "mfaEnrollment":{"overallStatus":"NOT_ENROLLED","totpStatus":"NOT_ENROLLED","webAuthnStatus":"NOT_ENROLLED","pushStatus":"NOT_ENROLLED","jcGoStatus":"NOT_ENROLLED","smsStatus":"NOT_ENROLLED"}},
				{"username":"carol","mfa":{"exclusion":false},
				 "mfaEnrollment":{"overallStatus":"ENROLLED","totpStatus":"ENROLLED","webAuthnStatus":"NOT_ENROLLED","pushStatus":"ENROLLED","jcGoStatus":"NOT_ENROLLED","smsStatus":"NOT_ENROLLED"}}
			]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMFAOverviewScreen_Aggregates(t *testing.T) {
	srv := startMFAOverviewServer(t)
	overrideMFAOverviewClient(t, srv.URL)

	s := NewMFAOverviewScreen()
	s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	runCmd(t, s, s.loadCmd())
	if s.loading || s.err != "" {
		t.Fatalf("load failed: %q", s.err)
	}

	d := s.data
	if !d.RequireAdminMFA || d.AllowUnenrolledPWR {
		t.Errorf("org knobs wrong: %+v", d)
	}
	if d.TotalUsers != 3 || d.Enrolled != 2 || d.Excluded != 1 {
		t.Errorf("aggregate wrong: %+v", d)
	}
	if d.FactorCounts["totpStatus"] != 2 || d.FactorCounts["webAuthnStatus"] != 1 ||
		d.FactorCounts["pushStatus"] != 1 || d.FactorCounts["smsStatus"] != 0 {
		t.Errorf("factor counts wrong: %v", d.FactorCounts)
	}
	if len(d.NotEnrolled) != 1 || d.NotEnrolled[0] != "bob" {
		t.Errorf("not-enrolled wrong: %v", d.NotEnrolled)
	}

	view := s.View()
	for _, want := range []string{
		"Require MFA for administrators", "on",
		"2 of 3 users enrolled",
		"1 user(s) excluded",
		"TOTP (authenticator app)", "2/3",
		"WebAuthn (security key / passkey)", "1/3",
		"Not enrolled (1)", "bob",
		"Admin Portal-only",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestMFAOverviewScreen_LoadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	t.Cleanup(srv.Close)
	overrideMFAOverviewClient(t, srv.URL)

	s := NewMFAOverviewScreen()
	runCmd(t, s, s.loadCmd())
	if s.err == "" || !strings.Contains(s.View(), "r retry") {
		t.Errorf("error path wrong: err=%q", s.err)
	}
}
