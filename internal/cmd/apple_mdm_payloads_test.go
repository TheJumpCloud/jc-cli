package cmd

import (
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
)

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
