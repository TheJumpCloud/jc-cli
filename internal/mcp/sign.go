package mcp

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/keychain"
)

// manifestSigner produces a tamper-evident record for a destructive MCP
// op. Implementations are called from the chokepoint in addTypedTool
// after the underlying JumpCloud API call has succeeded — the manifest
// is a forensic attestation, not a precondition. A nil error means the
// manifest was emitted; a non-nil error is logged but does not roll the
// op back (the op already happened upstream).
type manifestSigner interface {
	sign(toolName string, args any) error
}

// noopSigner is the default when destructive-op signing is disabled.
// Adds zero overhead to the hot path.
type noopSigner struct{}

func (noopSigner) sign(string, any) error { return nil }

// signedManifest is the JSON object written one-per-line to the signed
// audit log. The signature covers the canonical encoding of every other
// field, so any tampering with tool/args/timestamp/etc. invalidates the
// chain. Verifiers reconstruct the canonical form and check against the
// stored public key.
type signedManifest struct {
	Tool            string          `json:"tool"`
	ArgsRedacted    json.RawMessage `json:"args_redacted"`
	Target          string          `json:"target,omitempty"`
	Timestamp       string          `json:"timestamp"`
	Nonce           string          `json:"nonce"`
	OperatorPubkey  string          `json:"operator_pubkey"`
	Signature       string          `json:"signature,omitempty"`
}

// canonicalForSigning returns the byte sequence the signature is computed
// over. We marshal a copy of the manifest with Signature cleared and rely
// on encoding/json's deterministic key order (alphabetical) for stable
// canonicalization. Verifiers do the same trick.
func (m signedManifest) canonicalForSigning() ([]byte, error) {
	cp := m
	cp.Signature = ""
	return json.Marshal(cp)
}

// ed25519Signer holds the key material for a profile and writes manifests
// to the signed audit log. Lazy keypair generation: the first sign call
// for a profile that has no signing key yet creates one, persists the
// private key in the keychain, and writes the pubkey into config.
type ed25519Signer struct {
	mu       sync.Mutex
	profile  string
	logPath  string
	priv     ed25519.PrivateKey // nil until lazy-loaded
	pub      ed25519.PublicKey  // nil until lazy-loaded
	pubB64   string             // cached for the manifest field
}

// newEd25519Signer constructs the signer for the named profile and the
// given audit log path. Construction does NOT touch the keychain — the
// keypair is materialized lazily on the first sign call so a server
// that runs read-only never generates one.
func newEd25519Signer(profile, logPath string) *ed25519Signer {
	return &ed25519Signer{
		profile: profile,
		logPath: logPath,
	}
}

func (s *ed25519Signer) sign(toolName string, args any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureKeyLoaded(); err != nil {
		return fmt.Errorf("loading signing key: %w", err)
	}

	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("generating nonce: %w", err)
	}

	rawArgs, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("marshaling args: %w", err)
	}
	rawArgs = redactParams(rawArgs)

	m := signedManifest{
		Tool:           toolName,
		ArgsRedacted:   rawArgs,
		Target:         destructiveTarget(args),
		Timestamp:      nowFunc().UTC().Format(time.RFC3339),
		Nonce:          base64.StdEncoding.EncodeToString(nonce),
		OperatorPubkey: s.pubB64,
	}

	canonical, err := m.canonicalForSigning()
	if err != nil {
		return fmt.Errorf("canonicalizing manifest: %w", err)
	}
	sig := ed25519.Sign(s.priv, canonical)
	m.Signature = base64.StdEncoding.EncodeToString(sig)

	return s.appendManifest(m)
}

// ensureKeyLoaded materializes the per-profile keypair from the keychain,
// generating + persisting a new one only if no entry exists yet. Caller
// must hold s.mu.
//
// Critical: any non-not-found error from the keychain (locked, permission
// denied, corrupted entry, transient I/O) propagates rather than falling
// through to keypair regeneration. Otherwise a transient keychain glitch
// would overwrite an existing key and permanently break verification of
// every previously signed manifest in the audit log.
func (s *ed25519Signer) ensureKeyLoaded() error {
	if s.priv != nil {
		return nil
	}

	encoded, err := keychainGetSigningKey(s.profile)
	switch {
	case err == nil && encoded != "":
		raw, decErr := base64.StdEncoding.DecodeString(encoded)
		if decErr != nil {
			return fmt.Errorf("decoding stored signing key: %w", decErr)
		}
		if len(raw) != ed25519.PrivateKeySize {
			return fmt.Errorf("stored signing key is %d bytes, want %d", len(raw), ed25519.PrivateKeySize)
		}
		s.priv = ed25519.PrivateKey(raw)
		s.pub = s.priv.Public().(ed25519.PublicKey)
		s.pubB64 = base64.StdEncoding.EncodeToString(s.pub)
		return nil
	case err == nil && encoded == "":
		// Empty value with no error — treat as not-found and bootstrap.
	case keychain.IsNotFound(err):
		// Genuine not-found — bootstrap.
	default:
		// Locked, permission denied, corrupted, transient I/O — fail
		// closed. Generating a new key would overwrite the existing
		// entry once the keychain comes back, severing the audit chain.
		return fmt.Errorf("retrieving signing key: %w", err)
	}

	// Generate fresh keypair.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generating keypair: %w", err)
	}
	encodedPriv := base64.StdEncoding.EncodeToString(priv)
	if err := keychainSetSigningKey(s.profile, encodedPriv); err != nil {
		return fmt.Errorf("storing signing key in keychain: %w", err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	if err := configSetSigningPubkey(s.profile, pubB64); err != nil {
		// Roll back the keychain write so the next attempt regenerates
		// rather than leaving a key whose pubkey isn't recorded.
		_ = keychainDeleteSigningKey(s.profile)
		return fmt.Errorf("persisting public key to config: %w", err)
	}
	s.priv = priv
	s.pub = pub
	s.pubB64 = pubB64
	return nil
}

// appendManifest writes one JSON line to the signed audit log. Caller
// must hold s.mu so concurrent destructive ops don't interleave bytes.
// File mode 0600 to match the existing audit log.
func (s *ed25519Signer) appendManifest(m signedManifest) error {
	if err := os.MkdirAll(filepath.Dir(s.logPath), 0700); err != nil {
		return fmt.Errorf("creating signed-audit-log dir: %w", err)
	}
	f, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("opening signed-audit-log: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	if err := enc.Encode(m); err != nil {
		return fmt.Errorf("writing signed manifest: %w", err)
	}
	return nil
}

// signedAuditLogPath returns the conventional location, mirroring the
// existing audit log next to it.
func signedAuditLogPath() string {
	return filepath.Join(config.ConfigDir(), "mcp-audit-signed.log")
}

// newSigner returns the manifestSigner a Server should use given the
// requested configuration. Disabled → noop (zero overhead path).
func newSigner(enabled bool, profile string) manifestSigner {
	if !enabled {
		return noopSigner{}
	}
	return newEd25519Signer(profile, signedAuditLogPath())
}

// --- Test seams (overridden by stepup_test.go siblings) ---
// Wrapping these calls in package-level vars lets tests inject in-memory
// keychain + config writes without touching the user's real OS keychain.

var keychainGetSigningKey = func(profile string) (string, error) {
	return keychain.GetSigningKey(profile)
}
var keychainSetSigningKey = func(profile, encoded string) error {
	return keychain.SetSigningKey(profile, encoded)
}
var keychainDeleteSigningKey = func(profile string) error {
	return keychain.DeleteSigningKey(profile)
}
var configSetSigningPubkey = func(profile, pubB64 string) error {
	return config.SetSigningPubkey(profile, pubB64)
}

// VerifyManifestStream reads JSON-encoded signedManifests from r and
// returns nil if every record's signature checks out against the
// supplied trusted public key. The first invalid record (or read error)
// short-circuits with an error naming the offending record's nonce so
// operators can grep the file.
//
// Exposed for the `jc audit verify` CLI command and tests.
func VerifyManifestStream(r io.Reader, trustedPubkey ed25519.PublicKey) (int, error) {
	dec := json.NewDecoder(r)
	verified := 0
	for dec.More() {
		var m signedManifest
		if err := dec.Decode(&m); err != nil {
			return verified, fmt.Errorf("decoding manifest #%d: %w", verified+1, err)
		}
		canonical, err := m.canonicalForSigning()
		if err != nil {
			return verified, fmt.Errorf("canonicalizing manifest #%d (nonce=%s): %w", verified+1, m.Nonce, err)
		}
		sig, err := base64.StdEncoding.DecodeString(m.Signature)
		if err != nil {
			return verified, fmt.Errorf("decoding signature on manifest #%d (nonce=%s): %w", verified+1, m.Nonce, err)
		}
		if !ed25519.Verify(trustedPubkey, canonical, sig) {
			return verified, fmt.Errorf("signature mismatch on manifest #%d (nonce=%s, tool=%s)", verified+1, m.Nonce, m.Tool)
		}
		verified++
	}
	return verified, nil
}
