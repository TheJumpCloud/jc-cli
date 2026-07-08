package windows_mdm

import (
	"strings"
	"testing"
)

func TestNormalizeAndValidateSettings_ValidAndAliases(t *testing.T) {
	in := []OMAURISetting{
		{URI: "./Device/Vendor/MSFT/Policy/Config/BitLocker/RequireDeviceEncryption", Format: "int", Value: "1"},
		{URI: "./User/Vendor/MSFT/Policy/Config/Foo/Bar", Format: "string", Value: "hello"},   // alias → chr
		{URI: "./Vendor/MSFT/Policy/Config/Foo/Baz", Format: "BOOLEAN", Value: "true"},        // alias, case-insensitive → bool
		{URI: "./Device/Vendor/MSFT/Policy/Config/Foo/Qux", Format: "base64", Value: "aGk="},  // alias → b64
		{URI: "./Device/Vendor/MSFT/Policy/Config/Foo/Xml", Format: "xml", Value: "<a/>"},
		{URI: "./Device/Vendor/MSFT/Policy/Config/Foo/Flt", Format: "float", Value: "1.5"},
	}
	out, err := NormalizeAndValidateSettings(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantFormats := []string{"int", "chr", "bool", "b64", "xml", "float"}
	for i, want := range wantFormats {
		if out[i].Format != want {
			t.Errorf("setting %d: format = %q, want %q", i+1, out[i].Format, want)
		}
	}
}

func TestNormalizeAndValidateSettings_AggregatesAllProblems(t *testing.T) {
	// One pass must report every offender — the aggregate-errors
	// convention — so the operator fixes everything in one edit cycle.
	in := []OMAURISetting{
		{URI: "", Format: "int", Value: "1"},                          // missing uri
		{URI: "Device/Vendor/MSFT/X", Format: "int", Value: "1"},      // missing ./ prefix
		{URI: "./Vendor/MSFT/X", Format: "dword", Value: "1"},         // bad format
		{URI: "./Vendor/MSFT/Y", Format: "int", Value: ""},            // missing value
		{URI: "./Vendor/MSFT/Z", Format: "int", Value: "notanumber"},  // type mismatch
		{URI: "./Vendor/MSFT/W", Format: "bool", Value: "yes"},        // bad bool
		{URI: "./Vendor/MSFT/V", Format: "float", Value: "abc"},       // bad float
	}
	_, err := NormalizeAndValidateSettings(in)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{
		"setting 1: uri is required",
		"setting 2", "must start with ./",
		"setting 3", `"dword" is not valid`,
		"setting 4: value is required",
		"setting 5", "not an integer",
		"setting 6", "not a boolean",
		"setting 7", "not a float",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("aggregated error missing %q:\n%s", want, msg)
		}
	}
}

func TestNormalizeAndValidateSettings_Empty(t *testing.T) {
	if _, err := NormalizeAndValidateSettings(nil); err == nil {
		t.Fatal("expected error for zero settings")
	}
}

func TestNormalizeAndValidateKeys_ValidAndAliases(t *testing.T) {
	in := []RegistryKey{
		{Location: `SOFTWARE\Policies\Microsoft\Windows\Explorer`, ValueName: "NoAutorun", RegType: "DWORD", Data: "1"},
		{Location: `SOFTWARE\Policies\X`, ValueName: "A", RegType: "EXPAND_SZ", Data: "%SystemRoot%"},   // alias → expandString
		{Location: `SOFTWARE\Policies\X`, ValueName: "B", RegType: "REG_MULTI_SZ", Data: "a\nb"},        // alias → multiString
		{Location: `SOFTWARE\Policies\X`, ValueName: "C", RegType: "sz", Data: "v"},                     // alias, case-insensitive → String
		{Location: `SOFTWARE\Policies\X`, ValueName: "D", RegType: "qword", Data: "42"},                 // case-insensitive wire name
	}
	out, err := NormalizeAndValidateKeys(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantTypes := []string{"DWORD", "expandString", "multiString", "String", "QWORD"}
	for i, want := range wantTypes {
		if out[i].RegType != want {
			t.Errorf("key %d: type = %q, want %q", i+1, out[i].RegType, want)
		}
	}
}

func TestNormalizeAndValidateKeys_AggregatesAllProblems(t *testing.T) {
	in := []RegistryKey{
		{Location: "", ValueName: "A", RegType: "DWORD", Data: "1"},                                   // missing location
		{Location: `HKEY_LOCAL_MACHINE\SOFTWARE\Policies\X`, ValueName: "B", RegType: "DWORD", Data: "1"}, // hive prefix
		{Location: `HKLM\SOFTWARE\X`, ValueName: "C", RegType: "DWORD", Data: "1"},                    // hive alias prefix
		{Location: `SOFTWARE\Policies\X`, ValueName: "", RegType: "DWORD", Data: "1"},                 // missing name
		{Location: `SOFTWARE\Policies\X`, ValueName: "E", RegType: "BINARY", Data: "1"},               // bad type
		{Location: `SOFTWARE\Policies\X`, ValueName: "F", RegType: "DWORD", Data: ""},                 // missing data
		{Location: `SOFTWARE\Policies\X`, ValueName: "G", RegType: "DWORD", Data: "-5"},               // not unsigned
		{Location: strings.Repeat("a", 256), ValueName: "H", RegType: "DWORD", Data: "1"},             // location too long
		{Location: `SOFTWARE\Policies\X`, ValueName: strings.Repeat("n", 100), RegType: "DWORD", Data: "1"}, // name too long
	}
	_, err := NormalizeAndValidateKeys(in)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{
		"key 1: location is required",
		"key 2", "HKEY_LOCAL_MACHINE is implied",
		"key 3", "hive prefix",
		"key 4: name is required",
		"key 5", `"BINARY" is not valid`,
		"key 6: data is required",
		"key 7", "not an unsigned integer",
		"key 8", "255-character limit",
		"key 9", "99-character limit",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("aggregated error missing %q:\n%s", want, msg)
		}
	}
}

func TestNormalizeAndValidateKeys_Empty(t *testing.T) {
	if _, err := NormalizeAndValidateKeys(nil); err == nil {
		t.Fatal("expected error for zero keys")
	}
}

func TestHasHiveRoot_DeepHKEYNotRejected(t *testing.T) {
	// Only the FIRST path segment counts as a hive prefix — a subkey
	// merely containing "HKEY" deeper in the path is legitimate.
	if hasHiveRoot(`SOFTWARE\Policies\HKEY_LOCAL_MACHINE_lookalike\X`) {
		t.Error("deep segment containing HKEY should not be treated as a hive prefix")
	}
	if !hasHiveRoot(`hklm\SOFTWARE`) {
		t.Error("lowercase hklm prefix should be rejected")
	}
	if !hasHiveRoot(`HKEY_CURRENT_USER/Software`) {
		t.Error("forward-slash separated hive prefix should be rejected")
	}
}
