package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const featureFixture = `{"features":[{"name":"mcp","category":"ai","settings":[{"name":"enabled","value":true},{"name":"session_auto_revoke_days","value":30}]},{"name":"user_mcp","category":"ai","settings":[{"name":"enabled","value":false}]}]}`

// startFeatureServer serves /feature-settings, records the filters of the last
// request and fails the test on any non-GET.
func startFeatureServer(t *testing.T, filters *[]string, calls *int32) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		if r.Method != http.MethodGet {
			t.Errorf("non-GET request %s %s", r.Method, r.URL)
		}
		if r.URL.Path != "/feature-settings" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		*filters = r.URL.Query()["filter"]
		w.Header().Set("Content-Type", "application/json")
		if len(*filters) > 0 && *filters != nil && (*filters)[0] == "name:eq:nope" {
			w.Write([]byte(`{"features":[]}`))
			return
		}
		w.Write([]byte(featureFixture))
	}))
	t.Cleanup(ts.Close)
	overrideV2Client(t, ts.URL)
}

func runOrgFeaturesCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(append([]string{"org", "features"}, args...))
	err := cmd.Execute()
	return out.String(), err
}

func TestOrgFeaturesList_JSONKeepsGrouping(t *testing.T) {
	setupUsersTest(t)
	var filters []string
	var calls int32
	startFeatureServer(t, &filters, &calls)
	out, err := runOrgFeaturesCmd(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got) != 2 || got[0]["name"] != "mcp" || got[0]["settings"] == nil {
		t.Errorf("got %v", got)
	}
	if len(filters) != 0 {
		t.Errorf("unexpected filters %v", filters)
	}
}

func TestOrgFeaturesList_FiltersAreEqOnly(t *testing.T) {
	setupUsersTest(t)
	var filters []string
	var calls int32
	startFeatureServer(t, &filters, &calls)
	if _, err := runOrgFeaturesCmd(t, "list", "--name", "mcp", "--category", "ai"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(filters, ",") != "name:eq:mcp,category:eq:ai" {
		t.Errorf("filters = %v", filters)
	}
}

func TestOrgFeaturesList_TableFlattens(t *testing.T) {
	setupUsersTest(t)
	var filters []string
	var calls int32
	startFeatureServer(t, &filters, &calls)
	out, err := runOrgFeaturesCmd(t, "list", "-t")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"FEATURE", "CATEGORY", "SETTING", "VALUE", "session_auto_revoke_days", "user_mcp"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
}

func TestOrgFeaturesGet(t *testing.T) {
	setupUsersTest(t)
	var filters []string
	var calls int32
	startFeatureServer(t, &filters, &calls)
	out, err := runOrgFeaturesCmd(t, "get", "mcp")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["name"] != "mcp" {
		t.Errorf("get mcp = %s (%v)", out, err)
	}
	if _, err := runOrgFeaturesCmd(t, "get", "nope"); err == nil || !strings.Contains(err.Error(), "no feature named") {
		t.Errorf("unknown feature: %v", err)
	}
}

func TestOrgFeatures_InvalidNameSendsNoRequest(t *testing.T) {
	setupUsersTest(t)
	var filters []string
	var calls int32
	startFeatureServer(t, &filters, &calls)
	for _, args := range [][]string{{"get", "MCP"}, {"list", "--name", "mcp&x"}, {"list", "--category", "a:b"}} {
		if _, err := runOrgFeaturesCmd(t, args...); err == nil || !strings.Contains(err.Error(), "lower-case") {
			t.Errorf("%v: %v", args, err)
		}
	}
	if calls != 0 {
		t.Errorf("%d requests sent for invalid names", calls)
	}
}
