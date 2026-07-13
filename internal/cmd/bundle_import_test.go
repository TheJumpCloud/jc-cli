package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/bundle"
	"github.com/klaassen-consulting/jc/internal/mscp"
)

// seedMSCPCache pre-populates the mSCP cache dir (via XDG_CACHE_HOME)
// with a hand-written snapshot and completion marker, so the import
// command never touches the network in tests.
func seedMSCPCache(t *testing.T) {
	t.Helper()
	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)
	snap := filepath.Join(cacheRoot, "jc", "mscp", mscp.SnapshotTag)
	for path, content := range map[string]string{
		"rules/os/fixture_firewall.yaml": `
id: fixture_firewall
title: Fixture firewall rule
mobileconfig: true
mobileconfig_info:
  com.apple.security.firewall:
    EnableFirewall: true
`,
		"rules/os/fixture_shell.yaml": `
id: fixture_shell
title: Fixture shell rule
mobileconfig: false
`,
		"baselines/fixturebase.yaml": `
title: "Fixture baseline (jc test content)"
parent_values: fixturebase
profile:
  - section: s
    rules: [fixture_firewall, fixture_shell]
`,
		".complete.v1": "seeded\n",
	} {
		dest := filepath.Join(snap, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBundleImportMSCP_WritesUsableBundle(t *testing.T) {
	setupUsersTest(t)
	dir := isolateBundlesDir(t)
	seedMSCPCache(t)

	stdout, stderr, err := runBundleCmd(t, "import", "mscp", "--baseline", "fixturebase")
	if err != nil {
		t.Fatalf("import: %v\n%s", err, stderr)
	}
	// Default name derivation + default destination (the bundles dir).
	wantPath := filepath.Join(dir, "macos-fixturebase.yaml")
	if !strings.Contains(stdout, wantPath) {
		t.Errorf("output should name %s:\n%s", wantPath, stdout)
	}
	if !strings.Contains(stderr, "1 profile-enforceable (converted), 1 shell-only") {
		t.Errorf("report summary wrong:\n%s", stderr)
	}

	// The written bundle is immediately usable: it loads via LoadAll
	// (user dir), carries provenance, and deep-validates.
	b, err := bundle.ParseFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if b.Name != "macos-fixturebase" || b.Version != mscp.SnapshotTag ||
		b.Source.Origin != "imported" || b.Source.License != "CC BY 4.0" {
		t.Errorf("imported bundle wrong: %+v", b)
	}
	listOut, _, err := runBundleCmd(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listOut, "macos-fixturebase") {
		t.Errorf("imported bundle missing from list:\n%s", listOut)
	}

	// Unknown baseline: names the available ones.
	_, _, err = runBundleCmd(t, "import", "mscp", "--baseline", "nope")
	if err == nil || !strings.Contains(err.Error(), "fixturebase") {
		t.Errorf("unknown baseline should list available: %v", err)
	}
}

// TestBuiltinBaselines_Regenerable pins the shipped baselines' key
// facts so a converter change that silently alters them fails here
// (the full regeneration equality check needs the real snapshot, which
// CI doesn't download — headers document the regen command).
func TestBuiltinBaselines_Regenerable(t *testing.T) {
	builtins, err := bundle.LoadBuiltIn()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]struct{ units int }{
		"macos-cis-lvl1": {units: 14},
		"macos-cis-lvl2": {units: 16},
	} {
		b := bundle.FindByName(builtins, name)
		if b == nil {
			t.Errorf("builtin %s missing", name)
			continue
		}
		if len(b.Policies) != want.units {
			t.Errorf("%s: %d units, want %d", name, len(b.Policies), want.units)
		}
		if b.Version != mscp.SnapshotTag {
			t.Errorf("%s: version %q, want %q", name, b.Version, mscp.SnapshotTag)
		}
		if !strings.Contains(b.Source.Attribution, "NIST macOS Security Compliance Project") {
			t.Errorf("%s: attribution must name mSCP", name)
		}
		// Every unit is a macOS apple_profile with exactly one payload.
		for _, u := range b.Policies {
			if u.Type != bundle.UnitAppleProfile || u.OS != "macOS" || len(u.Profile.Payloads) != 1 {
				t.Errorf("%s/%s: unexpected unit shape", name, u.Name)
			}
		}
	}
}
