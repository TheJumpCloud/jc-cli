package apple_mdm

import (
	"strings"
	"testing"
)

// These tests use Default() which reads the actual vendored schemas.
// They lock in load-time invariants — if a refresh drops below the
// minimum count or breaks well-known payloads, the suite catches it.

func TestCatalog_Default_Loads(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatalf("Default(): %v", err)
	}
	// As of Release-v26.4 the catalog ships 125 parseable payloads
	// (127 files − 2 self-referential YAML anchors). Lock in a lower
	// bound rather than the exact count so a future Apple release
	// adding payloads doesn't churn this test on every refresh.
	if c.Len() < 100 {
		t.Errorf("catalog has only %d payloads (expected >= 100); did an embed directive break?", c.Len())
	}
	if c.Release != SchemaRelease {
		t.Errorf("Release = %q, want %q", c.Release, SchemaRelease)
	}
}

func TestCatalog_WellKnownPayloads(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatalf("Default(): %v", err)
	}
	// Spot-check three payloads that have been stable in Apple's
	// catalog for many releases. If any of these go missing the
	// rename/removal should at least be a conscious schema bump.
	for _, want := range []string{
		"com.apple.wifi.managed",
		"com.apple.applicationaccess",
		"com.apple.security.firewall",
	} {
		if _, ok := c.ByType(want); !ok {
			t.Errorf("expected payload %q in catalog", want)
		}
	}
}

func TestCatalog_VariantsOf_MCX(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatalf("Default(): %v", err)
	}
	// com.apple.MCX ships in 6 variant files (Accounts, EnergySaver,
	// FileVault2, Mobililty, TimeServer, WiFi). The variant collection
	// is what makes per-variant rendering possible despite their
	// shared PayloadType.
	variants := c.VariantsOf("com.apple.MCX")
	if len(variants) < 2 {
		t.Errorf("expected multiple com.apple.MCX variants, got %d", len(variants))
	}
	// All variants must share the canonical PayloadType.
	for _, v := range variants {
		if v.Type != "com.apple.MCX" {
			t.Errorf("variant %q has Type %q, want com.apple.MCX", v.ID, v.Type)
		}
	}
	// IDs must be distinct (this is the property that lets ByID
	// resolve the ambiguity).
	seen := make(map[string]bool)
	for _, v := range variants {
		if seen[v.ID] {
			t.Errorf("duplicate variant ID %q", v.ID)
		}
		seen[v.ID] = true
	}
}

func TestCatalog_Filter_OS(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatalf("Default(): %v", err)
	}
	macOS := c.Filter(FilterOpts{OS: "macOS"})
	iOS := c.Filter(FilterOpts{OS: "iOS"})
	if len(macOS) == 0 || len(iOS) == 0 {
		t.Fatal("expected non-empty macOS and iOS sets")
	}
	// Apple's tvOS and watchOS catalogs are much smaller than iOS, so
	// macOS-only and iOS-only sets should both fall below the total.
	if len(macOS) >= c.Len() {
		t.Errorf("macOS filter returned %d, catalog has %d — filter not narrowing", len(macOS), c.Len())
	}
	// All filtered entries must declare the target OS as available.
	for _, p := range macOS {
		if !p.SupportedOS["macOS"].Available() {
			t.Errorf("filter leak: %s has macOS unavailable", p.ID)
		}
	}
}

func TestCatalog_Filter_Search(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatalf("Default(): %v", err)
	}
	wifi := c.Filter(FilterOpts{Search: "wifi"})
	if len(wifi) == 0 {
		t.Fatal("expected matches for 'wifi'")
	}
	for _, p := range wifi {
		hay := strings.ToLower(p.Type + " " + p.Title + " " + p.Description)
		if !strings.Contains(hay, "wifi") {
			t.Errorf("filter leak: %s contains no 'wifi'", p.ID)
		}
	}
}

func TestCatalog_ByID_DistinguishesMCXVariants(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatalf("Default(): %v", err)
	}
	// These specific IDs are the user-facing disambiguators in the
	// `show` command's error message. Pin them so a future refresh
	// renaming one surfaces clearly in the test diff.
	for _, id := range []string{"com.apple.MCX(FileVault2)", "com.apple.MCX(WiFi)"} {
		if _, ok := c.ByID(id); !ok {
			t.Errorf("ByID(%q) miss — has Apple renamed the variant file?", id)
		}
	}
}
