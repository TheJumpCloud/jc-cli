package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// installFixtureSnapshot points the catalog cache at a temp dir
// pre-populated with the hand-written test fixture + completion
// marker, so csp commands run offline. DefaultCatalog memoizes per
// process, so every test in this binary shares the fixture catalog —
// which is exactly what we want.
func installFixtureSnapshot(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	dir := filepath.Join(root, "jc", "windows-mdm-ddf", windows_mdm.SnapshotName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join("..", "windows_mdm", "testdata", "Sample_AreaDDF.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Sample_AreaDDF.xml"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".complete"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runCSP(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestCSPListShowTemplate_OfflineOverFixture(t *testing.T) {
	installFixtureSnapshot(t)

	// list — human table contains the fixture setting.
	out, _, err := runCSP(t, "windows-mdm", "csp", "list", "--search", "widget", "-o", "human")
	if err != nil {
		t.Fatalf("csp list: %v", err)
	}
	if !strings.Contains(out, "Sample/AllowWidget") {
		t.Errorf("list output missing Sample/AllowWidget:\n%s", out)
	}

	// list --exclude-admx drops the ADMX-backed fixture entry.
	out, _, err = runCSP(t, "windows-mdm", "csp", "list", "--exclude-admx", "-o", "human")
	if err != nil {
		t.Fatalf("csp list --exclude-admx: %v", err)
	}
	if strings.Contains(out, "LegacyAdmxThing") {
		t.Errorf("--exclude-admx should drop LegacyAdmxThing:\n%s", out)
	}

	// show — human render carries the OMA-URI + enum.
	out, _, err = runCSP(t, "windows-mdm", "csp", "show", "Sample/AllowWidget", "-o", "human")
	if err != nil {
		t.Fatalf("csp show: %v", err)
	}
	for _, want := range []string{
		"./Device/Vendor/MSFT/Policy/Config/Sample/AllowWidget",
		"Not allowed.",
		"csp template Sample/AllowWidget",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}

	// show miss — actionable error.
	_, _, err = runCSP(t, "windows-mdm", "csp", "show", "Sample/NoSuchThing")
	if err == nil || !strings.Contains(err.Error(), "csp list --search") {
		t.Errorf("show miss should suggest a search, got %v", err)
	}

	// Zero matches in JSON must render [] not null — the documented
	// jq pipeline depends on it (CodeRabbit PR #65 review).
	out, _, err = runCSP(t, "windows-mdm", "csp", "list", "--search", "zzz-no-match", "-o", "json")
	if err != nil {
		t.Fatalf("csp list zero-match: %v", err)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("zero-match JSON should be [], got %q", strings.TrimSpace(out))
	}
}

func TestCSPTemplate_FeedsCreatePolicy(t *testing.T) {
	installFixtureSnapshot(t)

	// template emits the exact settings-file shape collectOMAURISettings
	// parses — the discover→author→create contract.
	out, stderr, err := runCSP(t, "windows-mdm", "csp", "template", "Sample/AllowWidget")
	if err != nil {
		t.Fatalf("csp template: %v", err)
	}
	var triples []struct {
		URI    string `json:"uri"`
		Format string `json:"format"`
		Value  string `json:"value"`
	}
	if err := json.Unmarshal([]byte(out), &triples); err != nil {
		t.Fatalf("template output is not the settings-file JSON shape: %v\n%s", err, out)
	}
	if len(triples) != 1 || triples[0].Format != "int" || triples[0].Value != "1" {
		t.Errorf("template triple wrong: %+v", triples)
	}

	// Round-trip through the create path's file loader + validator.
	dir := t.TempDir()
	file := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(file, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	settings, err := collectOMAURISettings(nil, file)
	if err != nil {
		t.Fatalf("create path could not consume template output: %v", err)
	}
	if _, err := windows_mdm.NormalizeAndValidateSettings(settings); err != nil {
		t.Fatalf("template output failed create-path validation: %v", err)
	}
	_ = stderr

	// ADMX-backed + user-scoped settings warn on stderr.
	_, stderr, err = runCSP(t, "windows-mdm", "csp", "template", "Sample/LegacyAdmxThing")
	if err != nil {
		t.Fatalf("csp template admx: %v", err)
	}
	if !strings.Contains(stderr, "ADMX-backed") {
		t.Errorf("expected ADMX warning on stderr, got: %s", stderr)
	}
}
