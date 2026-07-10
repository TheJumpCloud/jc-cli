package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/bundle"
)

func runBundleCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"bundle"}, args...))
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

// isolateBundlesDir points the user-bundles dir at a temp dir so the
// developer's real ~/.config/jc/bundles never leaks into tests.
func isolateBundlesDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := bundle.BundlesDir
	bundle.BundlesDir = func() string { return dir }
	t.Cleanup(func() { bundle.BundlesDir = orig })
	return dir
}

func TestBundleList_ShowsBuiltin(t *testing.T) {
	setupUsersTest(t)
	isolateBundlesDir(t)

	stdout, stderr, err := runBundleCmd(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("list output not JSON: %v\n%s", err, stdout)
	}
	found := false
	for _, r := range rows {
		if r["name"] == "example-baseline" {
			found = true
			if r["origin"] != "builtin" {
				t.Errorf("origin = %v, want builtin", r["origin"])
			}
			if r["policies"] != float64(3) {
				t.Errorf("policies = %v, want 3", r["policies"])
			}
			if !strings.Contains(r["platforms"].(string), "macOS") || !strings.Contains(r["platforms"].(string), "windows") {
				t.Errorf("platforms = %v", r["platforms"])
			}
		}
	}
	if !found {
		t.Errorf("example-baseline missing from list:\n%s", stdout)
	}
	if !strings.Contains(stderr, "bundles ──") {
		t.Errorf("count footer missing: %s", stderr)
	}
}

func TestBundleShow_FullBundleAndNotFound(t *testing.T) {
	setupUsersTest(t)
	isolateBundlesDir(t)

	stdout, _, err := runBundleCmd(t, "show", "example-baseline")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	var b bundle.Bundle
	if err := json.Unmarshal([]byte(stdout), &b); err != nil {
		t.Fatalf("show output not a bundle: %v\n%s", err, stdout)
	}
	if b.Name != "example-baseline" || len(b.Policies) != 3 || b.Source == nil || b.Source.Attribution == "" {
		t.Errorf("show lost detail: %+v", b)
	}

	_, _, err = runBundleCmd(t, "show", "no-such-bundle")
	if err == nil || !strings.Contains(err.Error(), "Available bundles:") {
		t.Errorf("not-found should list available bundles: %v", err)
	}
}

func TestBundleValidate_NameFileAndFailure(t *testing.T) {
	setupUsersTest(t)
	isolateBundlesDir(t)

	// By name (the builtin).
	stdout, _, err := runBundleCmd(t, "validate", "example-baseline")
	if err != nil {
		t.Fatalf("validate builtin: %v", err)
	}
	if !strings.Contains(stdout, "is valid (3 policy units)") {
		t.Errorf("validate output wrong: %s", stdout)
	}

	// By file, with a deep error: valid structure, bad payload key +
	// bad DWORD — both must be reported together.
	bad := `
name: bad
version: "1"
policies:
  - name: bad key
    type: apple_profile
    profile:
      payloads:
        - type: com.apple.security.firewall
          values: {NotARealKey: true}
  - name: bad dword
    type: windows_registry
    keys: [{location: SOFTWARE\X, name: v, type: DWORD, data: "zzz"}]
`
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = runBundleCmd(t, "validate", "--file", path)
	if err == nil {
		t.Fatal("expected deep validation failure")
	}
	for _, want := range []string{"bad key", "bad dword"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("aggregated deep error missing %q: %v", want, err)
		}
	}

	// Argument contract.
	if _, _, err := runBundleCmd(t, "validate"); err == nil || !strings.Contains(err.Error(), "name or --file") {
		t.Errorf("no-arg validate should error: %v", err)
	}
	if _, _, err := runBundleCmd(t, "validate", "x", "--file", path); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Errorf("both name and --file should error: %v", err)
	}
}

func TestBundleExport_RoundTripsAndForks(t *testing.T) {
	setupUsersTest(t)
	dir := isolateBundlesDir(t)

	// Export to stdout: parseable YAML.
	stdout, _, err := runBundleCmd(t, "export", "example-baseline")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	exported, err := bundle.Parse([]byte(stdout))
	if err != nil {
		t.Fatalf("exported YAML does not re-parse: %v", err)
	}
	if exported.Name != "example-baseline" || len(exported.Policies) != 3 {
		t.Errorf("export lost content: %+v", exported)
	}

	// Export to file, then the fork workflow: edit name, drop into the
	// user dir, and see it appear alongside the builtin.
	path := filepath.Join(t.TempDir(), "fork.yaml")
	if _, _, err := runBundleCmd(t, "export", "example-baseline", "--file", path); err != nil {
		t.Fatalf("export --file: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	forked := strings.Replace(string(data), "name: example-baseline", "name: my-fork", 1)
	if err := os.WriteFile(filepath.Join(dir, "my-fork.yaml"), []byte(forked), 0o600); err != nil {
		t.Fatal(err)
	}

	listOut, _, err := runBundleCmd(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listOut, "my-fork") || !strings.Contains(listOut, "example-baseline") {
		t.Errorf("fork workflow broken:\n%s", listOut)
	}
}
