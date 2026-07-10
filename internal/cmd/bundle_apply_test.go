package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/bundle"
	"github.com/klaassen-consulting/jc/internal/plan"
)

const applyBundleYAML = `
name: test-baseline
version: "1.2.0"
policies:
  - name: Firewall
    type: apple_profile
    profile:
      payloads:
        - type: com.apple.security.firewall
          values: {EnableFirewall: true}
  - name: Camera off
    type: windows_oma_uri
    settings:
      - {uri: ./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera, format: int, value: "0"}
  - name: Autorun off
    type: windows_registry
    keys:
      - {location: SOFTWARE\Policies\Microsoft\Windows\Explorer, name: NoAutorun, type: DWORD, data: "1"}
`

// bundleApplyServer is a fake tenant covering every endpoint apply
// touches. mutations records POSTs in order; existingPolicies /
// existingGroups seed the pre-flight lists; failPolicyName makes any
// policy POST with that name return 409 (a 4xx so the client's retry
// transport doesn't mask the failure the way a 500 would).
type bundleApplyServer struct {
	mutations        []string
	existingPolicies []string
	existingGroups   []string
	failPolicyName   string

	policyCreates int
}

func (s *bundleApplyServer) handler(t *testing.T) http.HandlerFunc {
	nameList := func(names []string, prefix string) []map[string]string {
		out := make([]map[string]string, 0, len(names))
		for i, n := range names {
			out = append(out, map[string]string{"id": fmt.Sprintf("%s-%d", prefix, i), "name": n})
		}
		return out
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path, q := r.URL.Path, r.URL.Query()
		switch {
		case r.Method == "GET" && path == "/policytemplates":
			// Filtered by name — return the matching template summary.
			name := strings.TrimPrefix(q.Get("filter"), "name:eq:")
			id := map[string]string{
				"custom_mdm_profile_darwin":           "tmpl-darwin",
				"custom_mdm_profile_ios":              "tmpl-ios",
				"custom_oma_uri_mdm_windows":          "tmpl-oma",
				"custom_registry_keys_policy_windows": "tmpl-reg",
			}[name]
			if id == "" {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]string{{"id": id, "name": name}})
		case r.Method == "GET" && strings.HasPrefix(path, "/policytemplates/"):
			id := strings.TrimPrefix(path, "/policytemplates/")
			fields := map[string][]map[string]string{
				"tmpl-darwin": {{"id": "f-payload", "name": "payload"}, {"id": "f-redispatch", "name": "redispatchPolicy"}},
				"tmpl-ios":    {{"id": "f-payload", "name": "payload"}},
				"tmpl-oma":    {{"id": "f-urilist", "name": "uriList"}},
				"tmpl-reg":    {{"id": "f-regtable", "name": "customRegTable"}},
			}[id]
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "name": id, "configFields": fields})
		case r.Method == "GET" && path == "/policies":
			_ = json.NewEncoder(w).Encode(nameList(s.existingPolicies, "existing-pol"))
		case r.Method == "GET" && path == "/policygroups":
			_ = json.NewEncoder(w).Encode(nameList(s.existingGroups, "existing-pg"))
		case r.Method == "GET" && path == "/systemgroups":
			_ = json.NewEncoder(w).Encode([]map[string]string{{"id": "dg-1", "name": "Corp Devices"}})
		case r.Method == "POST" && path == "/policies":
			var body struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if s.failPolicyName != "" && body.Name == s.failPolicyName {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"message":"boom"}`))
				return
			}
			s.policyCreates++
			id := fmt.Sprintf("pol-%d", s.policyCreates)
			s.mutations = append(s.mutations, "POST /policies "+body.Name)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "name": body.Name})
		case r.Method == "POST" && path == "/policygroups":
			var body struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mutations = append(s.mutations, "POST /policygroups "+body.Name+" ["+body.Description+"]")
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "pg-1", "name": body.Name})
		case r.Method == "POST" && strings.HasPrefix(path, "/policygroups/") && strings.HasSuffix(path, "/members"):
			var body struct {
				Op, Type, ID string
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mutations = append(s.mutations, fmt.Sprintf("POST %s %s %s %s", path, body.Op, body.Type, body.ID))
			_, _ = w.Write([]byte(`{}`))
		case r.Method == "POST" && strings.HasPrefix(path, "/systemgroups/") && strings.HasSuffix(path, "/associations"):
			var body struct {
				Op, Type, ID string
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mutations = append(s.mutations, fmt.Sprintf("POST %s %s %s %s", path, body.Op, body.Type, body.ID))
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func startBundleApplyServer(t *testing.T, s *bundleApplyServer) {
	t.Helper()
	srv := httptest.NewServer(s.handler(t))
	t.Cleanup(srv.Close)
	overrideV2Client(t, srv.URL)
}

func writeApplyBundle(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "b.yaml")
	if err := os.WriteFile(path, []byte(applyBundleYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBundleApply_FullSequenceWithGroup(t *testing.T) {
	setupUsersTest(t)
	isolateBundlesDir(t)
	srv := &bundleApplyServer{}
	startBundleApplyServer(t, srv)

	stdout, stderr, err := runBundleCmd(t, "apply", "--file", writeApplyBundle(t), "--group", "Corp Devices")
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, stderr)
	}

	want := []string{
		"POST /policies test-baseline/Firewall",
		"POST /policies test-baseline/Camera off",
		"POST /policies test-baseline/Autorun off",
		"POST /policygroups test-baseline (v1.2.0) [bundle:test-baseline@1.2.0]",
		"POST /policygroups/pg-1/members add policy pol-1",
		"POST /policygroups/pg-1/members add policy pol-2",
		"POST /policygroups/pg-1/members add policy pol-3",
		"POST /systemgroups/dg-1/associations add policy_group pg-1",
	}
	if len(srv.mutations) != len(want) {
		t.Fatalf("mutation count = %d, want %d:\n%s", len(srv.mutations), len(want), strings.Join(srv.mutations, "\n"))
	}
	for i, w := range want {
		if srv.mutations[i] != w {
			t.Errorf("mutation[%d] = %q, want %q", i, srv.mutations[i], w)
		}
	}

	var result bundle.ApplyResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("result not JSON: %v\n%s", err, stdout)
	}
	if len(result.Created) != 4 || result.PolicyGroupID != "pg-1" || !result.Bound {
		t.Errorf("result wrong: %+v", result)
	}
	if !strings.Contains(stderr, `Applied bundle test-baseline v1.2.0: 3 policies`) ||
		!strings.Contains(stderr, `bound to device group "Corp Devices"`) {
		t.Errorf("summary wrong: %s", stderr)
	}
}

func TestBundleApply_PlanMakesNoWrites(t *testing.T) {
	setupUsersTest(t)
	isolateBundlesDir(t)
	srv := &bundleApplyServer{}
	startBundleApplyServer(t, srv)

	_, stderr, err := runBundleCmd(t, "apply", "--file", writeApplyBundle(t), "--group", "Corp Devices", "--plan")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != plan.ExitCodePlan {
		t.Fatalf("want plan exit code %d, got %v", plan.ExitCodePlan, err)
	}
	if len(srv.mutations) != 0 {
		t.Fatalf("--plan must not write: %v", srv.mutations)
	}
	// The preview names every step kind.
	for _, want := range []string{"policy \"test-baseline/Firewall\"", "policy_group", "binding"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("plan preview missing %q:\n%s", want, stderr)
		}
	}
}

func TestBundleApply_ConflictPreflightBlocksEverything(t *testing.T) {
	setupUsersTest(t)
	isolateBundlesDir(t)
	srv := &bundleApplyServer{
		existingPolicies: []string{"test-baseline/Camera off"},
		existingGroups:   []string{"test-baseline (v1.2.0)"},
	}
	startBundleApplyServer(t, srv)

	_, _, err := runBundleCmd(t, "apply", "--file", writeApplyBundle(t))
	if err == nil {
		t.Fatal("expected conflict error")
	}
	for _, want := range []string{
		`policy "test-baseline/Camera off" already exists`,
		`policy group "test-baseline (v1.2.0)" already exists`,
		"create-only",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("conflict error missing %q:\n%v", want, err)
		}
	}
	if len(srv.mutations) != 0 {
		t.Fatalf("pre-flight failure must precede writes: %v", srv.mutations)
	}
}

func TestBundleApply_MidSequenceFailureReportsCleanup(t *testing.T) {
	setupUsersTest(t)
	isolateBundlesDir(t)
	srv := &bundleApplyServer{failPolicyName: "test-baseline/Camera off"}
	startBundleApplyServer(t, srv)

	stdout, _, err := runBundleCmd(t, "apply", "--file", writeApplyBundle(t))
	if err == nil {
		t.Fatal("expected mid-sequence failure")
	}
	// Stopped at the failure: only the first policy was created.
	if len(srv.mutations) != 1 {
		t.Fatalf("must stop at first failure: %v", srv.mutations)
	}
	// The error names the failed step, reports no rollback, and gives
	// the exact cleanup command for what WAS created.
	for _, want := range []string{
		`creating policy "test-baseline/Camera off"`,
		"NOT rolled back",
		"jc policies delete pol-1",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("failure report missing %q:\n%v", want, err)
		}
	}
	_ = stdout
}

func TestBundleApply_TemplateMissingFailsBeforeCreates(t *testing.T) {
	setupUsersTest(t)
	isolateBundlesDir(t)
	srv := &bundleApplyServer{}
	// A tenant with no Windows OMA-URI template: reuse the handler but
	// intercept the filtered template list for that name.
	inner := srv.handler(t)
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/policytemplates" && strings.Contains(r.URL.Query().Get("filter"), "custom_oma_uri_mdm_windows") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		}
		inner(w, r)
	}))
	t.Cleanup(hs.Close)
	overrideV2Client(t, hs.URL)

	_, _, err := runBundleCmd(t, "apply", "--file", writeApplyBundle(t))
	if err == nil || !strings.Contains(err.Error(), "custom_oma_uri_mdm_windows") {
		t.Fatalf("want template-missing error, got %v", err)
	}
	if len(srv.mutations) != 0 {
		t.Fatalf("template resolution failure must precede all writes: %v", srv.mutations)
	}
}

func TestBundleApply_NoGroupSkipsBinding(t *testing.T) {
	setupUsersTest(t)
	isolateBundlesDir(t)
	srv := &bundleApplyServer{}
	startBundleApplyServer(t, srv)

	stdout, _, err := runBundleCmd(t, "apply", "--file", writeApplyBundle(t), "--policy-group-name", "Pilot")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, m := range srv.mutations {
		if strings.Contains(m, "/systemgroups/") {
			t.Errorf("no --group must mean no binding: %v", m)
		}
	}
	if srv.mutations[3] != "POST /policygroups Pilot [bundle:test-baseline@1.2.0]" {
		t.Errorf("--policy-group-name not honored: %v", srv.mutations[3])
	}
	var result bundle.ApplyResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result.Bound {
		t.Error("Bound must be false without --group")
	}
}
