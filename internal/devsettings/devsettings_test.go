package devsettings

import (
	"encoding/json"
	"testing"
)

func TestNormalizeEnums(t *testing.T) {
	for in, want := range map[string]string{
		"windows": "WINDOWS", "WINDOWS": "WINDOWS",
		"macos": "MACOS", "mac": "MACOS", " MacOS ": "MACOS",
	} {
		got, err := NormalizeOSFamily(in)
		if err != nil {
			t.Fatalf("NormalizeOSFamily(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("NormalizeOSFamily(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := NormalizeOSFamily("linux"); err == nil {
		t.Error("expected error for unsupported OS family")
	}
	if got, _ := NormalizePermission("Admin"); got != "ADMIN" {
		t.Errorf("NormalizePermission = %q", got)
	}
	if _, err := NormalizePermission("root"); err == nil {
		t.Error("expected error for unknown permission")
	}
}

func current() []SignInSetting {
	return []SignInSetting{
		{OSFamily: "WINDOWS", Enabled: true, DefaultPermission: "STANDARD"},
		{OSFamily: "MACOS", Enabled: true, DefaultPermission: "STANDARD"},
	}
}

// Changing one OS family must leave the other entry untouched, so the PUT is
// safe whether the API replaces the array or merges it.
func TestMergeSignInSetting_PreservesOtherOSFamily(t *testing.T) {
	disabled := false
	got := MergeSignInSetting(current(), "MACOS", &disabled, nil)

	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2 — the other OS family must survive", len(got))
	}
	mac, ok := FindSignIn(got, "MACOS")
	if !ok || mac.Enabled {
		t.Errorf("MACOS not disabled: %+v", mac)
	}
	if mac.DefaultPermission != "STANDARD" {
		t.Errorf("unspecified field must be preserved, got %q", mac.DefaultPermission)
	}
	win, ok := FindSignIn(got, "WINDOWS")
	if !ok || !win.Enabled || win.DefaultPermission != "STANDARD" {
		t.Errorf("WINDOWS entry was disturbed: %+v", win)
	}
}

func TestMergeSignInSetting_DoesNotMutateInput(t *testing.T) {
	cur := current()
	disabled := false
	MergeSignInSetting(cur, "MACOS", &disabled, nil)

	mac, _ := FindSignIn(cur, "MACOS")
	if !mac.Enabled {
		t.Error("merge must not mutate the caller's slice")
	}
}

func TestMergeSignInSetting_PermissionOnly(t *testing.T) {
	admin := "ADMIN"
	got := MergeSignInSetting(current(), "WINDOWS", nil, &admin)

	win, _ := FindSignIn(got, "WINDOWS")
	if win.DefaultPermission != "ADMIN" {
		t.Errorf("permission = %q", win.DefaultPermission)
	}
	if !win.Enabled {
		t.Error("enabled must be preserved when only permission changes")
	}
}

func TestMergeSignInSetting_AppendsMissingOSFamily(t *testing.T) {
	enabled := true
	got := MergeSignInSetting([]SignInSetting{{OSFamily: "WINDOWS", Enabled: true, DefaultPermission: "ADMIN"}},
		"MACOS", &enabled, nil)

	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	mac, ok := FindSignIn(got, "MACOS")
	if !ok || !mac.Enabled {
		t.Errorf("appended entry wrong: %+v", mac)
	}
	if mac.DefaultPermission != "STANDARD" {
		t.Errorf("appended entry should default to STANDARD, got %q", mac.DefaultPermission)
	}
}

// The sign-in GET returns a plain 24-hex org id despite the spec's
// `format: byte`, and the password-sync GET has no envelope at all.
func TestParseSignIn_LiveShape(t *testing.T) {
	raw := json.RawMessage(`{"organizationObjectId":"5ec71e8e96bfda0611fc6c5b","settings":[{"osFamily":"WINDOWS","enabled":true,"defaultPermission":"STANDARD"}]}`)
	got, err := ParseSignIn(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.OrganizationObjectID != "5ec71e8e96bfda0611fc6c5b" {
		t.Errorf("organizationObjectId = %q", got.OrganizationObjectID)
	}
	if len(got.Settings) != 1 || got.Settings[0].OSFamily != "WINDOWS" {
		t.Errorf("settings = %+v", got.Settings)
	}

	var sync PasswordSync
	if err := json.Unmarshal([]byte(`{"enabled":true}`), &sync); err != nil {
		t.Fatal(err)
	}
	if !sync.Enabled {
		t.Error("password sync should decode from a bare {enabled} body")
	}
}

func TestBodies(t *testing.T) {
	body := SignInBody(current())
	if _, ok := body["settings"]; !ok {
		t.Errorf("SignInBody missing settings: %v", body)
	}
	if got := PasswordSyncBody(false); got["enabled"] != false {
		t.Errorf("PasswordSyncBody = %v", got)
	}
}

func TestDescribe(t *testing.T) {
	s := SignInSetting{OSFamily: "MACOS", Enabled: false, DefaultPermission: "ADMIN"}
	if got := s.Describe(); got != "MACOS: disabled, default permission ADMIN" {
		t.Errorf("Describe = %q", got)
	}
}
