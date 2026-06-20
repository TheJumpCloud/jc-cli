package cmd

import (
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
)

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
