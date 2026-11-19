package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startLiveLikeAppServer mimics the live v1 application PUT (2026-11-17):
//   - a body without both name and ssoUrl is answered 200 with the app
//     unchanged (nothing applied);
//   - otherwise the body is merged, except aiReady, which stays false. (Live,
//     a full body with aiReady:true and no MCP connection is a 400; keeping
//     false silently exercises jc's backstop check of the returned app.)
func startLiveLikeAppServer(t *testing.T, gotBody *map[string]any) map[string]any {
	t.Helper()
	app := map[string]any{"_id": "aabbccddee112233aabb2001", "name": "bookmark", "displayLabel": "zz",
		"ssoUrl": "https://example.invalid", "description": "d", "aiReady": false}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/applications/aabbccddee112233aabb2001" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(app)
		case http.MethodPut:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			*gotBody = body
			_, hasName := body["name"]
			_, hasURL := body["ssoUrl"]
			if hasName && hasURL {
				for k, v := range body {
					if k != "aiReady" {
						app[k] = v
					}
				}
			}
			json.NewEncoder(w).Encode(app)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(ts.Close)
	overrideV1Client(t, ts.URL)
	return app
}

func TestAppsUpdate_SendsFullBodySoTheChangeApplies(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	app := startLiveLikeAppServer(t, &body)
	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "update", "aabbccddee112233aabb2001", "--fields", `{"displayLabel":"renamed"}`})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "bookmark" || body["ssoUrl"] != "https://example.invalid" || body["description"] != "d" {
		t.Errorf("PUT body %v lacks the current app's fields", body)
	}
	if app["displayLabel"] != "renamed" {
		t.Errorf("change not applied: %v", app)
	}
}

func TestAppsUpdate_AIReadyIgnoredByServerFails(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	startLiveLikeAppServer(t, &body)
	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "update", "aabbccddee112233aabb2001", "--ai-ready=true"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "MCP connection") {
		t.Fatalf("want an error naming the MCP connection, got %v", err)
	}
	if body["aiReady"] != true {
		t.Errorf("PUT body = %v, want aiReady true", body)
	}
}

func TestAppsUpdate_AIReadyApplied(t *testing.T) {
	setupUsersTest(t)
	ts := setupAppsTest(t, sampleApps(), nil) // this mock merges every field
	defer ts.Close()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "update", "aabbccddee112233aabb2001", "--ai-ready=true"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out.Bytes(), &got)
	if got["aiReady"] != true {
		t.Errorf("output %s", out.String())
	}
}

func TestAppsUpdate_AIReadyFlagWinsOverConfig(t *testing.T) {
	setupUsersTest(t)
	var body map[string]any
	startLiveLikeAppServer(t, &body)
	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "update", "aabbccddee112233aabb2001", "--fields", `{"aiReady":true}`, "--ai-ready=false"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if body["aiReady"] != false {
		t.Errorf("PUT body = %v, want aiReady false from the flag", body)
	}
}

func TestAppsList_AIReadyFiltersLocally(t *testing.T) {
	setupUsersTest(t)
	apps := sampleApps()
	apps[0]["aiReady"] = true
	apps[1]["aiReady"] = false // apps[2] has no field: counts as false
	ts := setupAppsTest(t, apps, nil)
	defer ts.Close()
	for want, n := range map[string]int{"true": 1, "false": 2} {
		cmd := NewRootCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"apps", "list", "--ai-ready", want})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var got []map[string]any
		if err := json.Unmarshal(out.Bytes(), &got); err != nil || len(got) != n {
			t.Errorf("--ai-ready %s: %d apps (%v), want %d", want, len(got), err, n)
		}
	}
	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apps", "list", "--ai-ready", "yes"})
	if err := cmd.Execute(); err == nil {
		t.Error("--ai-ready yes accepted")
	}
}

// --config is the SSO configuration and becomes the app's "config" field;
// the old form, with an outer "config" key, is refused with the fix.
func TestAppsCreate_ConfigIsTheSSOConfiguration(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		want    string
		wantErr string
	}{
		{[]string{"--config", `{"idpCertificate":"x"}`}, `config: map[idpCertificate:x]`, ""},
		{[]string{"--fields", `{"description":"d"}`}, `description: d`, ""},
		{[]string{"--config", `{"config":{"idpCertificate":"x"}}`}, "", "outer \"config\" wrapper"},
		{[]string{"--fields", `{"config":{}}`}, "", "may not set \"config\""},
	} {
		cmd := NewRootCmd()
		var out strings.Builder
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(append([]string{"apps", "create", "--name", "bookmark", "--plan"}, tc.args...))
		err := cmd.Execute()
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%v: want error %q, got %v", tc.args, tc.wantErr, err)
			}
			continue
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Errorf("%v: plan lacks %q:\n%s", tc.args, tc.want, out.String())
		}
	}
}
