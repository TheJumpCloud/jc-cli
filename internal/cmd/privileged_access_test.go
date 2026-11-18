package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const pamDB = "dddddddddddddddddddddddd"

// startPAMServer serves the Privileged Access endpoints the CLI reads. active
// controls the activation gate.
func startPAMServer(t *testing.T, active bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case p == "/privileged-access/status":
			if active {
				w.Write([]byte(`{"isActive":true,"isPam":true,"isPwm":true,"isExpired":false}`))
			} else {
				w.Write([]byte(`{"isActive":false,"isPam":false}`))
			}
		case p == "/privileged-access/databases" && r.URL.Query().Get("maxResultCount") != "":
			w.Write([]byte(`{"items":[{"id":"` + pamDB + `","name":"Prod DB","provider":"postgresql"}],"totalCount":1}`))
		case p == "/privileged-access/databases/"+pamDB+"/edit":
			w.Write([]byte(`{"database":{"id":"` + pamDB + `","name":"Prod DB","connectionString":"Host=db;Password=hunter2"}}`))
		case p == "/privileged-access/servers":
			w.Write([]byte(`{"items":[],"totalCount":0}`))
		case p == "/privileged-access/websites":
			w.Write([]byte(`{"results":[],"totalCount":0}`))
		case p == "/privileged-access/credentials":
			w.Write([]byte(`{"results":[{"id":"cccccccccccccccccccccccc","name":"Corp card","credentialType":"CREDIT_CARD","last4Digits":"4242","cardholderName":"J Doe"}],"totalCount":1}`))
		case p == "/privileged-access/connections/status/by-resource/"+pamDB:
			w.Write([]byte(`{"statusDescription":"Available","numberConnectedUsers":0}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
}

func TestPAM_GateNamesTheProduct(t *testing.T) {
	setupUsersTest(t)
	ts := startPAMServer(t, false)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	_, err := runJC(t, "privileged-access", "servers", "list")
	if err == nil || !strings.Contains(err.Error(), "Privileged Access is not active") {
		t.Errorf("got %v", err)
	}
	if out, err := runJC(t, "pam", "status"); err != nil || !strings.Contains(out, "isActive") {
		t.Errorf("status must answer before activation: %v %s", err, out)
	}
}

func TestPAM_DatabaseGetByNameRedactsConnectionString(t *testing.T) {
	setupUsersTest(t)
	ts := startPAMServer(t, true)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	out, err := runJC(t, "pam", "databases", "get", "Prod DB")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "hunter2") || !strings.Contains(out, "[redacted]") {
		t.Errorf("connection string not redacted:\n%s", out)
	}
	if _, err := runJC(t, "pam", "databases", "get", "nope"); err == nil {
		t.Error("an unknown database must be an error")
	}
}

func TestPAM_CredentialsListHidesCardFieldsByDefault(t *testing.T) {
	setupUsersTest(t)
	ts := startPAMServer(t, true)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	out, err := runJC(t, "pam", "credentials", "list")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "4242") || strings.Contains(out, "J Doe") || !strings.Contains(out, "Corp card") {
		t.Errorf("default columns wrong:\n%s", out)
	}
}

func TestPAM_ConnectionStatusUnknownResource(t *testing.T) {
	setupUsersTest(t)
	ts := startPAMServer(t, true)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	if out, err := runJC(t, "pam", "connections", "status", "Prod DB"); err != nil || !strings.Contains(out, "Available") {
		t.Errorf("known resource: %v %s", err, out)
	}
	if _, err := runJC(t, "pam", "connections", "status", "bbbbbbbbbbbbbbbbbbbbbbbb"); err == nil {
		t.Error("an unknown resource must be an error, not \"Available\"")
	}
}

func TestPAM_CredentialsShowPersonalOptIn(t *testing.T) {
	setupUsersTest(t)
	ts := startPAMServer(t, true)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	out, err := runJC(t, "pam", "credentials", "list", "--show-personal")
	if err != nil || !strings.Contains(out, "4242") {
		t.Errorf("--show-personal: %v\n%s", err, out)
	}
}
