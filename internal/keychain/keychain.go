package keychain

import (
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	// ServiceName is the keychain service name used for all jc credentials.
	ServiceName = "jc"

	// URIPrefix is the prefix for keychain reference URIs stored in config.
	URIPrefix = "keychain://jc/"
)

// Set stores an API key in the OS keychain for the given profile.
func Set(profile, apiKey string) error {
	if err := keyring.Set(ServiceName, profile, apiKey); err != nil {
		return fmt.Errorf("failed to store key in keychain for profile %q: %w", profile, err)
	}
	return nil
}

// Get retrieves an API key from the OS keychain for the given profile.
func Get(profile string) (string, error) {
	secret, err := keyring.Get(ServiceName, profile)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve key from keychain for profile %q: %w", profile, err)
	}
	return secret, nil
}

// Delete removes an API key from the OS keychain for the given profile.
func Delete(profile string) error {
	if err := keyring.Delete(ServiceName, profile); err != nil {
		return fmt.Errorf("failed to remove key from keychain for profile %q: %w", profile, err)
	}
	return nil
}

// IsAvailable checks whether the OS keychain is usable by attempting
// a set/get/delete cycle with a temporary test entry.
func IsAvailable() bool {
	const testAccount = "__jc_keychain_test__"
	const testSecret = "test"

	if err := keyring.Set(ServiceName, testAccount, testSecret); err != nil {
		return false
	}
	// Clean up the test entry.
	_ = keyring.Delete(ServiceName, testAccount)
	return true
}

// URI returns the keychain reference URI for the given profile name.
// This URI is stored in the config file's api_key field to indicate
// that the actual key lives in the keychain.
func URI(profile string) string {
	return URIPrefix + profile
}

// IsKeychainRef returns true if the value is a keychain reference URI.
func IsKeychainRef(value string) bool {
	return strings.HasPrefix(value, URIPrefix)
}

// ProfileFromURI extracts the profile name from a keychain reference URI.
// Returns empty string if the value is not a valid keychain URI.
func ProfileFromURI(value string) string {
	if !IsKeychainRef(value) {
		return ""
	}
	return strings.TrimPrefix(value, URIPrefix)
}

// Resolve takes a value that may be a plaintext key or a keychain://jc/<profile>
// URI and returns the actual API key. If the value is a keychain reference,
// it retrieves the key from the keychain. Otherwise, it returns the value as-is.
func Resolve(value string) (string, error) {
	if !IsKeychainRef(value) {
		return value, nil
	}
	profile := ProfileFromURI(value)
	if profile == "" {
		return "", fmt.Errorf("invalid keychain reference: %s", value)
	}
	return Get(profile)
}
