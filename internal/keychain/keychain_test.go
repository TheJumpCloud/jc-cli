package keychain

import (
	"fmt"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestSetAndGet(t *testing.T) {
	keyring.MockInit()

	if err := Set("default", "my-secret-key"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}

	got, err := Get("default")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got != "my-secret-key" {
		t.Errorf("Get() = %q, want %q", got, "my-secret-key")
	}
}

func TestGetNonExistent(t *testing.T) {
	keyring.MockInit()

	_, err := Get("no-such-profile")
	if err == nil {
		t.Fatal("Get() should return error for non-existent profile")
	}
}

func TestDelete(t *testing.T) {
	keyring.MockInit()

	if err := Set("myprofile", "key123"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}

	if err := Delete("myprofile"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	_, err := Get("myprofile")
	if err == nil {
		t.Fatal("Get() should return error after Delete()")
	}
}

func TestDeleteNonExistent(t *testing.T) {
	keyring.MockInit()

	err := Delete("no-such-profile")
	if err == nil {
		t.Fatal("Delete() should return error for non-existent profile")
	}
}

func TestIsAvailable(t *testing.T) {
	keyring.MockInit()

	if !IsAvailable() {
		t.Error("IsAvailable() should return true with mock backend")
	}
}

func TestIsAvailableWhenUnavailable(t *testing.T) {
	keyring.MockInitWithError(fmt.Errorf("keychain unavailable"))

	if IsAvailable() {
		t.Error("IsAvailable() should return false when keychain errors")
	}
}

func TestURI(t *testing.T) {
	tests := []struct {
		profile string
		want    string
	}{
		{"default", "keychain://jc/default"},
		{"production", "keychain://jc/production"},
		{"my-org", "keychain://jc/my-org"},
	}
	for _, tt := range tests {
		got := URI(tt.profile)
		if got != tt.want {
			t.Errorf("URI(%q) = %q, want %q", tt.profile, got, tt.want)
		}
	}
}

func TestIsKeychainRef(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"keychain://jc/default", true},
		{"keychain://jc/production", true},
		{"keychain://jc/", true},
		{"keychain://other/default", false},
		{"plaintext-api-key", false},
		{"", false},
	}
	for _, tt := range tests {
		got := IsKeychainRef(tt.value)
		if got != tt.want {
			t.Errorf("IsKeychainRef(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestProfileFromURI(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{"keychain://jc/default", "default"},
		{"keychain://jc/production", "production"},
		{"keychain://jc/my-org", "my-org"},
		{"keychain://jc/", ""},
		{"plaintext-api-key", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := ProfileFromURI(tt.value)
		if got != tt.want {
			t.Errorf("ProfileFromURI(%q) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestResolve_PlaintextKey(t *testing.T) {
	keyring.MockInit()

	got, err := Resolve("plaintext-api-key-1234")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if got != "plaintext-api-key-1234" {
		t.Errorf("Resolve() = %q, want %q", got, "plaintext-api-key-1234")
	}
}

func TestResolve_KeychainRef(t *testing.T) {
	keyring.MockInit()

	// Store a key in the mock keychain.
	if err := Set("production", "secret-from-keychain"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}

	got, err := Resolve("keychain://jc/production")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if got != "secret-from-keychain" {
		t.Errorf("Resolve() = %q, want %q", got, "secret-from-keychain")
	}
}

func TestResolve_KeychainRefNotFound(t *testing.T) {
	keyring.MockInit()

	_, err := Resolve("keychain://jc/no-such-profile")
	if err == nil {
		t.Fatal("Resolve() should return error for missing keychain entry")
	}
}

func TestResolve_InvalidKeychainRef(t *testing.T) {
	keyring.MockInit()

	_, err := Resolve("keychain://jc/")
	if err == nil {
		t.Fatal("Resolve() should return error for empty profile in URI")
	}
}

func TestResolve_EmptyValue(t *testing.T) {
	keyring.MockInit()

	got, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if got != "" {
		t.Errorf("Resolve() = %q, want empty string", got)
	}
}

func TestMultipleProfiles(t *testing.T) {
	keyring.MockInit()

	profiles := map[string]string{
		"default":    "key-default-abc",
		"production": "key-prod-xyz",
		"staging":    "key-staging-123",
	}

	for profile, key := range profiles {
		if err := Set(profile, key); err != nil {
			t.Fatalf("Set(%q) error: %v", profile, err)
		}
	}

	for profile, wantKey := range profiles {
		got, err := Get(profile)
		if err != nil {
			t.Fatalf("Get(%q) error: %v", profile, err)
		}
		if got != wantKey {
			t.Errorf("Get(%q) = %q, want %q", profile, got, wantKey)
		}
	}
}

func TestSetOverwritesExisting(t *testing.T) {
	keyring.MockInit()

	if err := Set("default", "old-key"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}
	if err := Set("default", "new-key"); err != nil {
		t.Fatalf("Set() error on overwrite: %v", err)
	}

	got, err := Get("default")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got != "new-key" {
		t.Errorf("Get() = %q, want %q (overwritten value)", got, "new-key")
	}
}

func TestErrorBackend(t *testing.T) {
	keyring.MockInitWithError(fmt.Errorf("simulated keychain error"))

	if err := Set("default", "key"); err == nil {
		t.Fatal("Set() should return error with error backend")
	}
	if _, err := Get("default"); err == nil {
		t.Fatal("Get() should return error with error backend")
	}
	if err := Delete("default"); err == nil {
		t.Fatal("Delete() should return error with error backend")
	}
}
