package windows_mdm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture is HAND-WRITTEN (no Microsoft content — the real DDF
// drop is not redistributable) but byte-faithful to the DDF v2 shape:
// UTF-8 BOM, DOCTYPE with internal subset, MSFT-namespaced metadata,
// device + user roots, a nested group node, ENUM/Range/None/ADMX
// allowed values, and a Deprecated marker.

func loadFixtureSettings(t *testing.T) []Setting {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "Sample_AreaDDF.xml"))
	if err != nil {
		t.Fatal(err)
	}
	settings, err := ParseAreaDDF(data)
	if err != nil {
		t.Fatalf("ParseAreaDDF: %v", err)
	}
	return settings
}

func TestParseAreaDDF_FixtureShape(t *testing.T) {
	settings := loadFixtureSettings(t)

	// 4 device leaves (AllowWidget, WidgetTimeout, WidgetGroup/NestedName,
	// LegacyAdmxThing) + 1 user leaf (AllowWidget).
	if len(settings) != 5 {
		t.Fatalf("expected 5 settings, got %d: %+v", len(settings), settings)
	}

	byName := map[string]Setting{}
	for _, s := range settings {
		byName[s.Scope+":"+s.Name] = s
	}

	aw := byName["device:AllowWidget"]
	if aw.URI != "./Device/Vendor/MSFT/Policy/Config/Sample/AllowWidget" {
		t.Errorf("AllowWidget URI = %q", aw.URI)
	}
	if aw.Area != "Sample" || aw.Format != "int" || aw.DefaultValue != "1" {
		t.Errorf("AllowWidget core fields wrong: %+v", aw)
	}
	if aw.MinOSBuild != "10.0.10240" {
		t.Errorf("AllowWidget MinOSBuild = %q", aw.MinOSBuild)
	}
	if aw.AllowedValues == nil || aw.AllowedValues.Type != "ENUM" || len(aw.AllowedValues.Enum) != 2 {
		t.Fatalf("AllowWidget allowed values wrong: %+v", aw.AllowedValues)
	}
	if aw.AllowedValues.Enum[0].Value != "0" || aw.AllowedValues.Enum[0].Description != "Not allowed." {
		t.Errorf("AllowWidget enum[0] wrong: %+v", aw.AllowedValues.Enum[0])
	}

	// Range constraint.
	wt := byName["device:WidgetTimeout"]
	if wt.AllowedValues == nil || wt.AllowedValues.Type != "Range" || wt.AllowedValues.Value != "[0-730]" {
		t.Errorf("WidgetTimeout range wrong: %+v", wt.AllowedValues)
	}

	// Nested group node — Name is relative to the AREA root.
	nested := byName["device:WidgetGroup/NestedName"]
	if nested.URI != "./Device/Vendor/MSFT/Policy/Config/Sample/WidgetGroup/NestedName" {
		t.Errorf("nested URI = %q", nested.URI)
	}
	if nested.Format != "chr" {
		t.Errorf("nested format = %q", nested.Format)
	}

	// ADMX-backed + deprecated flags.
	admx := byName["device:LegacyAdmxThing"]
	if !admx.ADMXBacked {
		t.Error("LegacyAdmxThing should be flagged ADMX-backed")
	}
	if !admx.Deprecated {
		t.Error("LegacyAdmxThing should be flagged deprecated")
	}

	// User-scoped root parsed with the right scope + URI prefix.
	user := byName["user:AllowWidget"]
	if user.URI != "./User/Vendor/MSFT/Policy/Config/Sample/AllowWidget" {
		t.Errorf("user AllowWidget URI = %q", user.URI)
	}
}

func TestLoadCatalog_IndicesAndFilter(t *testing.T) {
	cat, err := LoadCatalog("testdata")
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if cat.Len() != 5 {
		t.Fatalf("Len = %d", cat.Len())
	}
	if got := cat.Areas(); len(got) != 1 || got[0] != "Sample" {
		t.Errorf("Areas = %v", got)
	}

	// ByRef is case-insensitive and prefers the device variant when
	// both scopes exist.
	s, ok := cat.ByRef("sample/allowwidget")
	if !ok {
		t.Fatal("ByRef miss")
	}
	if s.Scope != "device" {
		t.Errorf("ByRef should prefer device variant, got %q", s.Scope)
	}

	// Nested name lookup.
	if _, ok := cat.ByRef("Sample/WidgetGroup/NestedName"); !ok {
		t.Error("ByRef miss for nested name")
	}

	// Filter: search over description.
	if got := cat.Filter(FilterOpts{Search: "times out"}); len(got) != 1 || got[0].Name != "WidgetTimeout" {
		t.Errorf("search filter wrong: %+v", got)
	}
	// Filter: scope.
	if got := cat.Filter(FilterOpts{Scope: "user"}); len(got) != 1 || got[0].Scope != "user" {
		t.Errorf("scope filter wrong: %+v", got)
	}
	// Filter: ExcludeADMX drops the ADMX-backed entry.
	all := cat.Filter(FilterOpts{Scope: "device"})
	noADMX := cat.Filter(FilterOpts{Scope: "device", ExcludeADMX: true})
	if len(all)-len(noADMX) != 1 {
		t.Errorf("ExcludeADMX should drop exactly 1: all=%d noADMX=%d", len(all), len(noADMX))
	}
	// Filter: unknown area is empty, not an error.
	if got := cat.Filter(FilterOpts{Area: "NoSuchArea"}); got != nil {
		t.Errorf("unknown area should return nil, got %+v", got)
	}
}

func TestTemplateSetting_SeedsValue(t *testing.T) {
	cat, err := LoadCatalog("testdata")
	if err != nil {
		t.Fatal(err)
	}
	// Default value wins.
	s, _ := cat.ByRef("Sample/AllowWidget")
	tpl := TemplateSetting(s)
	if tpl.URI != s.URI || tpl.Format != "int" || tpl.Value != "1" {
		t.Errorf("template from default wrong: %+v", tpl)
	}
	// No default → first enum value → here NestedName has neither, so
	// empty value (operator fills it in).
	nested, _ := cat.ByRef("Sample/WidgetGroup/NestedName")
	if tpl := TemplateSetting(nested); tpl.Value != "" {
		t.Errorf("no-default no-enum template should have empty value: %+v", tpl)
	}
	// The emitted triple passes the create-path validation once a
	// value is present — the discover→author→create loop contract.
	if _, err := NormalizeAndValidateSettings([]OMAURISetting{TemplateSetting(s)}); err != nil {
		t.Errorf("template triple should validate: %v", err)
	}
}

// TestEnsureSnapshot_UsesPrePlacedZip covers the air-gapped path with
// a locally built zip: EnsureSnapshot must extract a pre-placed
// archive without any network access. SHA verification is exercised
// via the mismatch branch (our zip is deliberately NOT the pinned
// Microsoft drop).
func TestEnsureSnapshot_PrePlacedZipFailsChecksum(t *testing.T) {
	dir := filepath.Join(t.TempDir(), SnapshotName)
	zipPath := dir + ".zip"
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zipPath, []byte("not the pinned drop"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := EnsureSnapshot(t.Context(), dir, nil)
	if err == nil {
		t.Fatal("expected SHA-256 mismatch error for a non-pinned zip")
	}
	if !strings.Contains(err.Error(), "SHA-256") {
		t.Errorf("error should mention SHA-256 verification: %v", err)
	}
}

// TestDefaultCatalog_RetriesAfterTransientFailure guards the
// CodeRabbit PR #65 catch: a failed load must NOT be memoized. A
// long-lived MCP server whose first fetch hits a network blip has to
// recover on the next tool call, not stay broken until restart.
func TestDefaultCatalog_RetriesAfterTransientFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	defaultCatalogMu.Lock()
	defaultCatalog = nil // isolate from other tests in this binary
	defaultCatalogMu.Unlock()

	// First call: a corrupt pre-placed zip fails SHA verification.
	dir := CacheDir()
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+".zip", []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := DefaultCatalog(t.Context(), nil); err == nil {
		t.Fatal("expected first call to fail on the corrupt zip")
	}

	// Operator fixes the cache (here: a valid extracted snapshot).
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "Sample_AreaDDF.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Sample_AreaDDF.xml"), fixture, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, snapshotMarker), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second call must retry and succeed — no permanently cached error.
	cat, err := DefaultCatalog(t.Context(), nil)
	if err != nil {
		t.Fatalf("second call should retry after transient failure: %v", err)
	}
	if cat.Len() == 0 {
		t.Error("retried catalog is empty")
	}

	defaultCatalogMu.Lock()
	defaultCatalog = nil // don't leak fixture catalog to other tests
	defaultCatalogMu.Unlock()
}

func TestEnsureSnapshot_MarkerShortCircuits(t *testing.T) {
	// A dir carrying the completion marker must be used as-is with no
	// zip and no network.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, snapshotMarker), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := EnsureSnapshot(t.Context(), dir, nil)
	if err != nil || got != dir {
		t.Fatalf("marker short-circuit failed: %v %q", err, got)
	}
}
