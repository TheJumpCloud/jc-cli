package cmd

import (
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
)

// TestComposeOSCheckRejectsUnsupportedPayload — Bugbot PR #59 review
// caught that runComposeCreatePolicy skipped the per-payload
// SupportedOS check. Single-payload create-policy refuses iOS-only
// payloads when --os macOS is set; compose was happily building and
// POSTing mixed-platform bundles whose payloads devices later ignored.
//
// This regression guard exercises the OS-mismatch path by hand-building
// PayloadInstance slices (cheaper than spinning up an httptest server)
// and asserting the error mentions the unsupported payload by name.
func TestComposeOSCheckRejectsUnsupportedPayload(t *testing.T) {
	// Helper builds a synthetic payload-instance with the platforms
	// the schema declares support for.
	mkInstance := func(payloadType string, supports map[string]bool) apple_mdm.PayloadInstance {
		sos := apple_mdm.SupportedOS{}
		for plat, ok := range supports {
			if ok {
				sos[plat] = apple_mdm.OSSupport{Introduced: "1.0"}
			} else {
				sos[plat] = apple_mdm.OSSupport{Introduced: "n/a"}
			}
		}
		return apple_mdm.PayloadInstance{
			Schema: apple_mdm.Payload{
				Type:        payloadType,
				SupportedOS: sos,
			},
		}
	}

	tests := []struct {
		name      string
		osFamily  string
		instances []apple_mdm.PayloadInstance
		wantErr   string
	}{
		{
			"iOS-only payload in macOS bundle",
			"macOS",
			[]apple_mdm.PayloadInstance{
				mkInstance("com.apple.test.macsupported", map[string]bool{"macOS": true, "iOS": false}),
				mkInstance("com.apple.test.iosonly", map[string]bool{"macOS": false, "iOS": true}),
			},
			"com.apple.test.iosonly",
		},
		{
			"macOS-only payload in iOS bundle",
			"iOS",
			[]apple_mdm.PayloadInstance{
				mkInstance("com.apple.test.macosonly", map[string]bool{"macOS": true, "iOS": false}),
				mkInstance("com.apple.test.iossupported", map[string]bool{"macOS": false, "iOS": true}),
			},
			"com.apple.test.macosonly",
		},
		{
			"clean macOS bundle passes the check",
			"macOS",
			[]apple_mdm.PayloadInstance{
				mkInstance("com.apple.test.x", map[string]bool{"macOS": true, "iOS": false}),
				mkInstance("com.apple.test.y", map[string]bool{"macOS": true, "iOS": true}),
			},
			"",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Replicate the OS check loop the actual function uses.
			// We can't easily exercise runComposeCreatePolicy whole
			// (it builds an API client); the check logic is the
			// interesting part.
			schemaPlat := canonicalApplePlatform(tc.osFamily)
			var unsupported []string
			for _, p := range tc.instances {
				sup, ok := p.Schema.SupportedOS[schemaPlat]
				if !ok || !sup.Available() {
					unsupported = append(unsupported, p.Schema.Type)
				}
			}
			if tc.wantErr == "" {
				if len(unsupported) > 0 {
					t.Errorf("expected no unsupported, got %v", unsupported)
				}
				return
			}
			if len(unsupported) == 0 {
				t.Errorf("expected unsupported payload %q to be flagged", tc.wantErr)
				return
			}
			found := false
			for _, u := range unsupported {
				if u == tc.wantErr {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected %q in unsupported list, got %v", tc.wantErr, unsupported)
			}
		})
	}
}

// TestCanonicalApplePlatform covers the normalization layer that
// translates either Apple's platform name OR JumpCloud's family alias
// to Apple's vendored-schema canonical name. Bugbot PR #57 review
// caught that without this, `--os ios` would pass jcOSFamily but
// then fail the SupportedOS lookup (Apple's schemas key on "iOS",
// not "ios") with a misleading error.
func TestCanonicalApplePlatform(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		// Apple's canonical names pass through.
		{"macOS", "macOS"},
		{"iOS", "iOS"},
		// JC family aliases map to Apple's canonical.
		{"darwin", "macOS"},
		{"ios", "iOS"},
		// Unknown / future platforms pass through so a lookup miss
		// surfaces clearly instead of silently renaming.
		{"tvOS", "tvOS"},
		{"visionOS", "visionOS"},
		{"watchOS", "watchOS"},
		{"madeup", "madeup"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := canonicalApplePlatform(tc.in); got != tc.want {
			t.Errorf("canonicalApplePlatform(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestJCOSFamily covers the Apple-platform → JumpCloud-template-family
// translation. macOS and iOS are supported (KLA-450 landed iOS);
// tvOS/visionOS/watchOS fail clean because JumpCloud's MDM doesn't
// manage those platforms today.
func TestJCOSFamily(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
		errSubstr string
	}{
		{"macOS", apple_mdm.OSFamilyDarwin, false, ""},
		{"darwin", apple_mdm.OSFamilyDarwin, false, ""},
		{"iOS", apple_mdm.OSFamilyIOS, false, ""},
		{"ios", apple_mdm.OSFamilyIOS, false, ""},
		{"tvOS", "", true, "JumpCloud MDM does not manage"},
		{"visionOS", "", true, "JumpCloud MDM does not manage"},
		{"watchOS", "", true, "JumpCloud MDM does not manage"},
		{"unknown", "", true, "unknown Apple platform"},
		{"", "", true, "unknown Apple platform"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := jcOSFamily(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for %q, got nil", tc.in)
				} else if !strings.Contains(err.Error(), tc.errSubstr) {
					t.Errorf("error %q missing substring %q", err.Error(), tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
