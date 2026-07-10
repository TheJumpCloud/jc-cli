package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/bundle"
)

// makeApplePolicyJSON builds a realistic GET /policies/{id} response
// for a firewall profile by emitting a real mobileconfig — the same
// bytes apply would have POSTed — so the decode-and-diff path is
// exercised against genuine plist content, not a hand-rolled stub.
func makeApplePolicyJSON(t *testing.T, id, name string, enableFirewall, redispatch bool) string {
	t.Helper()
	cat, err := apple_mdm.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &apple_mdm.ComposeConfig{
		Name: name,
		Payloads: []apple_mdm.ComposePayload{{
			Type:   "com.apple.security.firewall",
			Values: map[string]any{"EnableFirewall": enableFirewall},
		}},
	}
	instances, env, err := cfg.BuildPayloadInstances(cat)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := apple_mdm.EmitMobileconfig(&buf, env, instances); err != nil {
		t.Fatal(err)
	}
	policy := map[string]any{
		"id":       id,
		"name":     name,
		"template": map[string]string{"name": "custom_mdm_profile_darwin"},
		"values": []map[string]any{
			{"configFieldName": "payload", "value": base64.StdEncoding.EncodeToString(buf.Bytes())},
			{"configFieldName": "redispatchPolicy", "value": redispatch},
		},
	}
	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func makeOMAURIPolicyJSON(t *testing.T, id, name, cameraValue string) string {
	t.Helper()
	policy := map[string]any{
		"id":       id,
		"name":     name,
		"template": map[string]string{"name": "custom_oma_uri_mdm_windows"},
		"values": []map[string]any{{
			"configFieldName": "uriList",
			"value": []map[string]string{{
				"uri": "./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera", "format": "int", "value": cameraValue,
			}},
		}},
	}
	data, _ := json.Marshal(policy)
	return string(data)
}

func makeRegistryPolicyJSON(t *testing.T, id, name, data string) string {
	t.Helper()
	policy := map[string]any{
		"id":       id,
		"name":     name,
		"template": map[string]string{"name": "custom_registry_keys_policy_windows"},
		"values": []map[string]any{{
			"configFieldName": "customRegTable",
			"value": []map[string]string{{
				"customLocation":  `SOFTWARE\Policies\Microsoft\Windows\Explorer`,
				"customValueName": "NoAutorun", "customRegType": "DWORD", "customData": data,
			}},
		}},
	}
	raw, _ := json.Marshal(policy)
	return string(raw)
}

// bundleStatusServer stubs the read-only endpoints status touches.
type bundleStatusServer struct {
	groups   []map[string]string // policy groups: id/name/description
	members  map[string][]string // group id → member policy ids
	policies map[string]string   // policy id → raw JSON response
}

func startBundleStatusServer(t *testing.T, s *bundleStatusServer) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case r.Method == "GET" && path == "/policygroups":
			_ = json.NewEncoder(w).Encode(s.groups)
		case r.Method == "GET" && strings.HasSuffix(path, "/members"):
			gid := strings.TrimSuffix(strings.TrimPrefix(path, "/policygroups/"), "/members")
			out := []map[string]any{}
			for _, pid := range s.members[gid] {
				out = append(out, map[string]any{"to": map[string]string{"id": pid, "type": "policy"}})
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == "GET" && strings.HasPrefix(path, "/policies/"):
			pid := strings.TrimPrefix(path, "/policies/")
			raw, ok := s.policies[pid]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(raw))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	overrideV2Client(t, srv.URL)
}

// statusBundle writes the canonical 3-unit test bundle and returns its
// path (same shape the apply tests use).
func statusBundle(t *testing.T) string {
	t.Helper()
	return writeApplyBundle(t)
}

func inSyncStatusServer(t *testing.T) *bundleStatusServer {
	t.Helper()
	return &bundleStatusServer{
		groups: []map[string]string{
			{"id": "pg-1", "name": "test-baseline (v1.2.0)", "description": "bundle:test-baseline@1.2.0"},
		},
		members: map[string][]string{"pg-1": {"pol-1", "pol-2", "pol-3"}},
		policies: map[string]string{
			"pol-1": makeApplePolicyJSON(t, "pol-1", "test-baseline/Firewall", true, true),
			"pol-2": makeOMAURIPolicyJSON(t, "pol-2", "test-baseline/Camera off", "0"),
			"pol-3": makeRegistryPolicyJSON(t, "pol-3", "test-baseline/Autorun off", "1"),
		},
	}
}

func runBundleStatus_(t *testing.T, args ...string) (bundle.StatusReport, string, error) {
	t.Helper()
	stdout, stderr, err := runBundleCmd(t, append([]string{"status"}, args...)...)
	var report bundle.StatusReport
	if err == nil {
		if jerr := json.Unmarshal([]byte(stdout), &report); jerr != nil {
			t.Fatalf("status output not JSON: %v\n%s", jerr, stdout)
		}
	}
	return report, stderr, err
}

func TestBundleStatus_InSync(t *testing.T) {
	setupUsersTest(t)
	isolateBundlesDir(t)
	startBundleStatusServer(t, inSyncStatusServer(t))

	report, stderr, err := runBundleStatus_(t, "--file", statusBundle(t))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !report.InSync || !report.MatchedByMarker || report.PolicyGroupID != "pg-1" {
		t.Errorf("report wrong: %+v", report)
	}
	for _, u := range report.Units {
		if u.State != bundle.StateInSync {
			t.Errorf("unit %s state = %s, want in-sync (diffs: %v)", u.Unit, u.State, u.Diffs)
		}
	}
	if !strings.Contains(stderr, "is in sync (3 units)") {
		t.Errorf("summary wrong:\n%s", stderr)
	}
}

func TestBundleStatus_DriftAcrossAllThreeKinds(t *testing.T) {
	setupUsersTest(t)
	isolateBundlesDir(t)
	srv := inSyncStatusServer(t)
	// Apple: firewall disabled on tenant. Windows OMA-URI: camera
	// allowed. Registry: autorun data changed.
	srv.policies["pol-1"] = makeApplePolicyJSON(t, "pol-1", "test-baseline/Firewall", false, true)
	srv.policies["pol-2"] = makeOMAURIPolicyJSON(t, "pol-2", "test-baseline/Camera off", "1")
	srv.policies["pol-3"] = makeRegistryPolicyJSON(t, "pol-3", "test-baseline/Autorun off", "0")
	startBundleStatusServer(t, srv)

	report, stderr, err := runBundleStatus_(t, "--file", statusBundle(t))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if report.InSync {
		t.Fatal("drift not detected")
	}
	wantDiffs := map[string]string{
		"Firewall":    "values differ",
		"Camera off":  `tenant int="1", bundle int="0"`,
		"Autorun off": `tenant DWORD="0", bundle DWORD="1"`,
	}
	for _, u := range report.Units {
		if u.State != bundle.StateDrifted {
			t.Errorf("unit %s state = %s, want drifted", u.Unit, u.State)
			continue
		}
		if want := wantDiffs[u.Unit]; len(u.Diffs) != 1 || !strings.Contains(u.Diffs[0], want) {
			t.Errorf("unit %s diffs = %v, want mention of %q", u.Unit, u.Diffs, want)
		}
	}
	if !strings.Contains(stderr, "has drifted") {
		t.Errorf("summary wrong:\n%s", stderr)
	}
}

func TestBundleStatus_MissingAndOrphan(t *testing.T) {
	setupUsersTest(t)
	isolateBundlesDir(t)
	srv := inSyncStatusServer(t)
	// pol-2 vanishes from the group; a hand-made policy joins it.
	srv.members["pg-1"] = []string{"pol-1", "pol-3", "pol-9"}
	srv.policies["pol-9"] = makeOMAURIPolicyJSON(t, "pol-9", "hand-made policy", "1")
	delete(srv.policies, "pol-2")
	startBundleStatusServer(t, srv)

	report, _, err := runBundleStatus_(t, "--file", statusBundle(t))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if report.InSync {
		t.Fatal("missing+orphan must not be in-sync")
	}
	states := map[string]string{}
	for _, u := range report.Units {
		states[u.Unit] = u.State
	}
	if states["Camera off"] != bundle.StateMissing {
		t.Errorf("Camera off state = %s, want missing", states["Camera off"])
	}
	if states["Firewall"] != bundle.StateInSync || states["Autorun off"] != bundle.StateInSync {
		t.Errorf("untouched units must stay in-sync: %v", states)
	}
	if len(report.Orphans) != 1 || report.Orphans[0] != "hand-made policy" {
		t.Errorf("orphans = %v", report.Orphans)
	}
}

func TestBundleStatus_NameFallbackAndNotApplied(t *testing.T) {
	setupUsersTest(t)
	isolateBundlesDir(t)
	srv := inSyncStatusServer(t)
	// Marker wiped (e.g. someone edited the description) — the default
	// group name still matches.
	srv.groups[0]["description"] = "edited by hand"
	startBundleStatusServer(t, srv)

	report, _, err := runBundleStatus_(t, "--file", statusBundle(t))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if report.MatchedByMarker {
		t.Error("must report name-fallback match")
	}
	if !report.InSync {
		t.Errorf("still in sync via name fallback: %+v", report)
	}
}

func TestBundleStatus_NotAppliedErrors(t *testing.T) {
	setupUsersTest(t)
	isolateBundlesDir(t)
	startBundleStatusServer(t, &bundleStatusServer{
		groups: []map[string]string{{"id": "x", "name": "unrelated", "description": ""}},
	})

	_, _, err := runBundleCmd(t, "status", "--file", statusBundle(t))
	if err == nil || !strings.Contains(err.Error(), "was the bundle applied?") {
		t.Fatalf("want not-applied error, got %v", err)
	}
}

func TestBundleStatus_RedispatchDrift(t *testing.T) {
	setupUsersTest(t)
	isolateBundlesDir(t)
	srv := inSyncStatusServer(t)
	srv.policies["pol-1"] = makeApplePolicyJSON(t, "pol-1", "test-baseline/Firewall", true, false)
	startBundleStatusServer(t, srv)

	report, _, err := runBundleStatus_(t, "--file", statusBundle(t))
	if err != nil {
		t.Fatal(err)
	}
	var fw *bundle.UnitStatus
	for i := range report.Units {
		if report.Units[i].Unit == "Firewall" {
			fw = &report.Units[i]
		}
	}
	if fw == nil || fw.State != bundle.StateDrifted {
		t.Fatalf("firewall unit not drifted: %+v", fw)
	}
	if len(fw.Diffs) != 1 || !strings.Contains(fw.Diffs[0], "redispatch: tenant false, bundle true") {
		t.Errorf("redispatch diff wrong: %v", fw.Diffs)
	}
}

// Guard: the fixtures in this file must stay aligned with the shared
// apply bundle fixture — a rename there would silently turn every
// status test into a missing-unit test.
func TestBundleStatus_FixtureAlignment(t *testing.T) {
	b, err := bundle.ParseFile(statusBundle(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Firewall", "Camera off", "Autorun off"}
	if len(b.Policies) != len(want) {
		t.Fatalf("fixture has %d units, want %d", len(b.Policies), len(want))
	}
	for i, w := range want {
		if b.Policies[i].Name != w {
			t.Errorf("fixture unit[%d] = %q, want %q", i, b.Policies[i].Name, w)
		}
		if got := bundle.PolicyName(b, w); got != "test-baseline/"+w {
			t.Errorf("policy name = %q", got)
		}
	}
	_ = fmt.Sprintf // keep fmt imported if assertions change
}
