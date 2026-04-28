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

// SetClientSecret stores an OAuth client secret in the OS keychain for the given profile.
// Uses a different account name (<profile>:client_secret) to avoid collisions with API keys.
func SetClientSecret(profile, secret string) error {
	account := profile + ":client_secret"
	if err := keyring.Set(ServiceName, account, secret); err != nil {
		return fmt.Errorf("failed to store client secret in keychain for profile %q: %w", profile, err)
	}
	return nil
}

// GetClientSecret retrieves an OAuth client secret from the OS keychain for the given profile.
func GetClientSecret(profile string) (string, error) {
	account := profile + ":client_secret"
	secret, err := keyring.Get(ServiceName, account)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve client secret from keychain for profile %q: %w", profile, err)
	}
	return secret, nil
}

// DeleteClientSecret removes an OAuth client secret from the OS keychain for the given profile.
func DeleteClientSecret(profile string) error {
	account := profile + ":client_secret"
	if err := keyring.Delete(ServiceName, account); err != nil {
		return fmt.Errorf("failed to remove client secret from keychain for profile %q: %w", profile, err)
	}
	return nil
}

// ClientSecretURI returns the keychain reference URI for a profile's client secret.
func ClientSecretURI(profile string) string {
	return URIPrefix + profile + ":client_secret"
}

// SetSigningKey stores an Ed25519 signing private key (raw 64-byte seed+pub
// blob, base64-encoded) in the OS keychain for the given profile. Used by
// the MCP destructive-op signer (KLA-411) to attest each destructive call
// without ever writing the private key to disk.
func SetSigningKey(profile, encodedKey string) error {
	account := profile + ":signing_key"
	if err := keyring.Set(ServiceName, account, encodedKey); err != nil {
		return fmt.Errorf("failed to store signing key in keychain for profile %q: %w", profile, err)
	}
	return nil
}

// GetSigningKey retrieves the Ed25519 signing private key (base64-encoded)
// from the OS keychain for the given profile.
func GetSigningKey(profile string) (string, error) {
	account := profile + ":signing_key"
	secret, err := keyring.Get(ServiceName, account)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve signing key from keychain for profile %q: %w", profile, err)
	}
	return secret, nil
}

// DeleteSigningKey removes the signing key from the OS keychain.
func DeleteSigningKey(profile string) error {
	account := profile + ":signing_key"
	if err := keyring.Delete(ServiceName, account); err != nil {
		return fmt.Errorf("failed to remove signing key from keychain for profile %q: %w", profile, err)
	}
	return nil
}
