package mscp

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/bundle"
)

const fixtureDir = "testdata/snapshot"

func loadFixture(t *testing.T, baseline string) (map[string]*Rule, *Baseline) {
	t.Helper()
	rules, err := LoadRules(fixtureDir)
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	b, err := LoadBaseline(fixtureDir, baseline)
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}
	return rules, b
}

func TestConvert_FullPipeline(t *testing.T) {
	rules, manifest := loadFixture(t, "testbase")
	b, report, err := Convert(rules, manifest, "test-bundle", "imported")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	// 5 rules: 4 mobileconfig (one shell-only skipped), merged into 3
	// payload-type units.
	if report.TotalRules != 5 || report.Converted != 4 || len(report.ShellOnly) != 1 {
		t.Errorf("report wrong: %+v", report)
	}
	if report.ShellOnly[0] != "test_shell_only" {
		t.Errorf("shell-only = %v", report.ShellOnly)
	}
	if len(b.Policies) != 3 {
		t.Fatalf("units = %d, want 3: %+v", len(b.Policies), report.Units)
	}

	// Unit order is manifest order: firewall, screensaver, prefdomain.
	byName := map[string]*bundle.PolicyUnit{}
	for i := range b.Policies {
		byName[b.Policies[i].Name] = &b.Policies[i]
	}

	// Firewall: catalog-checked (single variant, values valid), and
	// the agreeing EnableFirewall overlap merged with stealth mode.
	fw := byName["com.apple.security.firewall"]
	if fw == nil {
		t.Fatal("firewall unit missing")
	}
	if fw.Profile.Payloads[0].Raw {
		t.Error("firewall should be catalog-checked, not raw")
	}
	vals := fw.Profile.Payloads[0].Values
	if vals["EnableFirewall"] != true || vals["EnableStealthMode"] != true {
		t.Errorf("firewall merge wrong: %v", vals)
	}

	// Screensaver: $ODV resolved from the baseline's parent_values
	// column (900, not the 1200 recommended); raw because the vendored
	// schema demands moduleName.
	ss := byName["com.apple.screensaver"]
	if ss == nil {
		t.Fatal("screensaver unit missing")
	}
	if got := ss.Profile.Payloads[0].Values["idleTime"]; got != 900 {
		t.Errorf("ODV substitution = %v (%T), want 900", got, got)
	}
	if !ss.Profile.Payloads[0].Raw {
		t.Error("screensaver (no moduleName) must be raw")
	}

	// Unknown preference domain: raw.
	pd := byName["com.example.prefdomain"]
	if pd == nil || !pd.Profile.Payloads[0].Raw {
		t.Errorf("pref domain must be raw: %+v", pd)
	}
	found := false
	for _, rt := range report.RawTypes {
		if rt == "com.example.prefdomain" {
			found = true
		}
	}
	if !found {
		t.Errorf("RawTypes missing pref domain: %v", report.RawTypes)
	}

	// Provenance: attribution names the tag, the baseline title, and
	// the honest subset; license recorded.
	src := b.Source
	if src.Origin != "imported" || src.License != "CC BY 4.0" ||
		!strings.Contains(src.Attribution, SnapshotTag) ||
		!strings.Contains(src.Attribution, "4 of 5") {
		t.Errorf("source wrong: %+v", src)
	}
	if b.Version != SnapshotTag {
		t.Errorf("version = %q", b.Version)
	}

	// The generated bundle round-trips and deep-validates.
	data, err := bundle.MarshalYAML(b)
	if err != nil {
		t.Fatal(err)
	}
	again, err := bundle.Parse(data)
	if err != nil {
		t.Fatalf("generated YAML does not re-parse: %v\n%s", err, data)
	}
	cat, err := apple_mdm.Default()
	if err != nil {
		t.Fatal(err)
	}
	if err := bundle.Validate(again, cat); err != nil {
		t.Errorf("round-tripped bundle fails validation: %v", err)
	}
}

func TestConvert_ConflictingValuesFail(t *testing.T) {
	rules, manifest := loadFixture(t, "conflictbase")
	_, _, err := Convert(rules, manifest, "x", "imported")
	if err == nil {
		t.Fatal("conflicting values must fail")
	}
	for _, want := range []string{"test_firewall_enable", "test_firewall_conflict", "EnableFirewall"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("conflict error missing %q: %v", want, err)
		}
	}
}

func TestConvert_UnresolvableODVFails(t *testing.T) {
	rules, manifest := loadFixture(t, "odvmissing")
	_, _, err := Convert(rules, manifest, "x", "imported")
	if err == nil || !strings.Contains(err.Error(), "$ODV") {
		t.Fatalf("unresolvable $ODV must fail loudly: %v", err)
	}
}

func TestLoadBaseline_UnknownListsAvailable(t *testing.T) {
	_, err := LoadBaseline(fixtureDir, "nope")
	if err == nil || !strings.Contains(err.Error(), "testbase") {
		t.Errorf("unknown baseline should list available ones: %v", err)
	}
}

func TestEnsureSnapshot_MarkerShortCircuits(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, snapshotMarker), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := EnsureSnapshot(context.Background(), dir, nil)
	if err != nil || got != dir {
		t.Errorf("marker short-circuit failed: %v %v", got, err)
	}
}

func TestVerifySHA256_Mismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.zip")
	if err := os.WriteFile(path, []byte("not the pinned archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := verifySHA256(path)
	if err == nil || !strings.Contains(err.Error(), SnapshotSHA256) {
		t.Errorf("mismatch error must name the pinned hash: %v", err)
	}
}

// TestExtract_StripsTopDirAndFilters pins the archive-layout handling:
// GitHub tag archives nest everything under macos_security-<tag>/, and
// only rules/ + baselines/ YAML should come out.
func TestExtract_StripsTopDirAndFilters(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"macos_security-tahoe_rev3/rules/os/r.yaml":        "id: r\ntitle: t\n",
		"macos_security-tahoe_rev3/baselines/b.yaml":       "title: b\nprofile: [{section: s, rules: [r]}]\n",
		"macos_security-tahoe_rev3/scripts/generate.py":    "print('skip me')",
		"macos_security-tahoe_rev3/includes/800-53.yaml":   "skip: true",
		"macos_security-tahoe_rev3/templates/x.yaml.jinja": "skip",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "snap.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(t.TempDir(), "out")
	if err := extract(zipPath, dir); err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, want := range []string{"rules/os/r.yaml", "baselines/b.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(want))); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
	for _, skip := range []string{"scripts", "includes", "templates"} {
		if _, err := os.Stat(filepath.Join(dir, skip)); err == nil {
			t.Errorf("%s should not be extracted", skip)
		}
	}
}
