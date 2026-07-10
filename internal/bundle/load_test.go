package bundle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
)

// TestLoadBuiltIn_AllValid is the packaging gate: every embedded
// bundle must parse AND pass deep validation against the embedded
// Apple catalog — a builtin that fails at author time must never
// reach a release.
func TestLoadBuiltIn_AllValid(t *testing.T) {
	bundles, err := LoadBuiltIn()
	if err != nil {
		t.Fatalf("LoadBuiltIn: %v", err)
	}
	if len(bundles) == 0 {
		t.Fatal("no builtin bundles embedded")
	}
	cat, err := apple_mdm.Default()
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	for _, b := range bundles {
		if err := Validate(b, cat); err != nil {
			t.Errorf("builtin %q fails deep validation: %v", b.Name, err)
		}
		if b.Source == nil || b.Source.Origin != OriginBuiltin {
			t.Errorf("builtin %q must carry origin builtin, got %+v", b.Name, b.Source)
		}
		// The licensing gate in spirit: every builtin must say where
		// it came from.
		if b.Source.Attribution == "" {
			t.Errorf("builtin %q has no source.attribution — the licensing gate requires provenance", b.Name)
		}
	}
}

func overrideBundlesDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := BundlesDir
	BundlesDir = func() string { return dir }
	t.Cleanup(func() { BundlesDir = orig })
	return dir
}

func TestLoadAll_UserOverridesBuiltin(t *testing.T) {
	dir := overrideBundlesDir(t)

	override := `
name: example-baseline
version: "9.9.9"
policies:
  - name: Camera off
    type: windows_oma_uri
    settings: [{uri: ./Device/Vendor/MSFT/Policy/Config/Camera/AllowCamera, format: int, value: "0"}]
`
	if err := os.WriteFile(filepath.Join(dir, "override.yaml"), []byte(override), 0o600); err != nil {
		t.Fatal(err)
	}

	all, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	b := FindByName(all, "example-baseline")
	if b == nil {
		t.Fatal("example-baseline missing")
	}
	if b.Version != "9.9.9" {
		t.Errorf("user bundle must override builtin by name: got version %q", b.Version)
	}
	if b.Source.Origin != OriginUser {
		t.Errorf("override origin = %q, want user", b.Source.Origin)
	}
	// No duplicate entry for the overridden name.
	count := 0
	for _, x := range all {
		if x.Name == "example-baseline" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("override produced %d entries, want 1", count)
	}
}

// TestLoadFromDir_SkipsInvalid pins the warn-and-continue contract:
// one broken user file must not take down list.
func TestLoadFromDir_SkipsInvalid(t *testing.T) {
	dir := overrideBundlesDir(t)
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte("name: only-a-name\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	good := `
name: good
version: "1"
policies:
  - name: p
    type: windows_registry
    keys: [{location: SOFTWARE\X, name: v, type: DWORD, data: "1"}]
`
	if err := os.WriteFile(filepath.Join(dir, "good.yaml"), []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}

	bundles, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(bundles) != 1 || bundles[0].Name != "good" {
		t.Errorf("want only the good bundle, got %d: %+v", len(bundles), bundles)
	}
}

func TestLoadFromDir_MissingDirIsEmpty(t *testing.T) {
	bundles, err := LoadFromDir(filepath.Join(t.TempDir(), "nope"))
	if err != nil || bundles != nil {
		t.Errorf("missing dir: got %v, %v; want nil, nil", bundles, err)
	}
}

// TestStampOrigin_AuthoredOriginWins: an explicit origin (e.g.
// "imported" written by a converter) is never overwritten.
func TestStampOrigin_AuthoredOriginWins(t *testing.T) {
	b := &Bundle{Source: &Source{Origin: "imported"}}
	stampOrigin(b, OriginUser)
	if b.Source.Origin != "imported" {
		t.Errorf("origin = %q, want imported", b.Source.Origin)
	}
}
