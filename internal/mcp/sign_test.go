package mcp

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/klaassen-consulting/jc/internal/keychain"
)

// signerFixture overrides the keychain/config seams with in-memory state
// so tests don't touch the user's real OS keychain or write to their
// config file. Returns a cleanup function.
func signerFixture(t *testing.T) (logPath string, cleanup func()) {
	t.Helper()

	tmpDir := t.TempDir()
	logPath = filepath.Join(tmpDir, "mcp-audit-signed.log")

	prevGet := keychainGetSigningKey
	prevSet := keychainSetSigningKey
	prevDel := keychainDeleteSigningKey
	prevPub := configSetSigningPubkey

	store := map[string]string{}
	pubkeys := map[string]string{}

	keychainGetSigningKey = func(profile string) (string, error) {
		v, ok := store[profile]
		if !ok {
			// Return the sentinel so ensureKeyLoaded distinguishes
			// "no entry yet, bootstrap" from "transient keychain
			// failure, fail closed".
			return "", keychain.ErrNotFound
		}
		return v, nil
	}
	keychainSetSigningKey = func(profile, encoded string) error {
		store[profile] = encoded
		return nil
	}
	keychainDeleteSigningKey = func(profile string) error {
		delete(store, profile)
		return nil
	}
	configSetSigningPubkey = func(profile, pubB64 string) error {
		pubkeys[profile] = pubB64
		return nil
	}

	cleanup = func() {
		keychainGetSigningKey = prevGet
		keychainSetSigningKey = prevSet
		keychainDeleteSigningKey = prevDel
		configSetSigningPubkey = prevPub
	}
	return logPath, cleanup
}

func TestNoopSigner_AlwaysOK(t *testing.T) {
	if err := (noopSigner{}).sign("users_delete", destructiveInput{Identifier: "alice", Execute: true}); err != nil {
		t.Errorf("noopSigner.sign returned %v, want nil", err)
	}
}

func TestNewSigner_DisabledReturnsNoop(t *testing.T) {
	if _, ok := newSigner(false, "default").(noopSigner); !ok {
		t.Errorf("newSigner(false) = %T, want noopSigner", newSigner(false, "default"))
	}
}

func TestNewSigner_EnabledReturnsEd25519(t *testing.T) {
	if _, ok := newSigner(true, "default").(*ed25519Signer); !ok {
		t.Errorf("newSigner(true) = %T, want *ed25519Signer", newSigner(true, "default"))
	}
}

func TestEd25519Signer_RoundTrip(t *testing.T) {
	logPath, cleanup := signerFixture(t)
	defer cleanup()

	s := newEd25519Signer("default", logPath)
	if err := s.sign("users_delete", destructiveInput{Identifier: "alice", Execute: true}); err != nil {
		t.Fatalf("sign() error: %v", err)
	}

	// Manifest written?
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	if !strings.Contains(string(data), `"tool":"users_delete"`) {
		t.Errorf("log missing tool name: %s", data)
	}
	if !strings.Contains(string(data), `"target":"alice"`) {
		t.Errorf("log missing target: %s", data)
	}

	// Verify signature against the recorded pubkey.
	var m signedManifest
	if err := json.Unmarshal(bytes.TrimSpace(data), &m); err != nil {
		t.Fatalf("decoding manifest: %v", err)
	}
	pubBytes, _ := base64.StdEncoding.DecodeString(m.OperatorPubkey)
	canonical, _ := m.canonicalForSigning()
	sig, _ := base64.StdEncoding.DecodeString(m.Signature)
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), canonical, sig) {
		t.Error("signature did not verify against the manifest's own pubkey")
	}
}

func TestEd25519Signer_MultipleOpsReuseKey(t *testing.T) {
	logPath, cleanup := signerFixture(t)
	defer cleanup()

	s := newEd25519Signer("default", logPath)
	for i := 0; i < 3; i++ {
		if err := s.sign("users_delete", destructiveInput{Identifier: "alice", Execute: true}); err != nil {
			t.Fatalf("sign() #%d error: %v", i, err)
		}
	}

	data, _ := os.ReadFile(logPath)
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	// All three manifests must share the same pubkey (we only generate
	// once per signer's lifetime).
	var pubkeys []string
	for _, line := range lines {
		var m signedManifest
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("decode: %v", err)
		}
		pubkeys = append(pubkeys, m.OperatorPubkey)
	}
	if pubkeys[0] != pubkeys[1] || pubkeys[1] != pubkeys[2] {
		t.Errorf("pubkeys differ across ops: %v", pubkeys)
	}
}

func TestEd25519Signer_NoncesAreUnique(t *testing.T) {
	logPath, cleanup := signerFixture(t)
	defer cleanup()

	s := newEd25519Signer("default", logPath)
	for i := 0; i < 5; i++ {
		if err := s.sign("users_delete", destructiveInput{Identifier: "alice", Execute: true}); err != nil {
			t.Fatal(err)
		}
	}

	data, _ := os.ReadFile(logPath)
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	seen := map[string]bool{}
	for _, line := range lines {
		var m signedManifest
		_ = json.Unmarshal(line, &m)
		if seen[m.Nonce] {
			t.Errorf("duplicate nonce %s", m.Nonce)
		}
		seen[m.Nonce] = true
	}
}

func TestVerifyManifestStream_HappyPath(t *testing.T) {
	logPath, cleanup := signerFixture(t)
	defer cleanup()

	s := newEd25519Signer("default", logPath)
	for i := 0; i < 3; i++ {
		if err := s.sign("users_delete", destructiveInput{Identifier: "alice", Execute: true}); err != nil {
			t.Fatal(err)
		}
	}

	data, _ := os.ReadFile(logPath)
	verified, err := VerifyManifestStream(bytes.NewReader(data), s.pub)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified != 3 {
		t.Errorf("verified = %d, want 3", verified)
	}
}

func TestVerifyManifestStream_TamperDetected(t *testing.T) {
	logPath, cleanup := signerFixture(t)
	defer cleanup()

	s := newEd25519Signer("default", logPath)
	if err := s.sign("users_delete", destructiveInput{Identifier: "alice", Execute: true}); err != nil {
		t.Fatal(err)
	}

	// Tamper with the on-disk manifest: swap the target.
	data, _ := os.ReadFile(logPath)
	tampered := bytes.Replace(data, []byte(`"target":"alice"`), []byte(`"target":"bob"`), 1)

	verified, err := VerifyManifestStream(bytes.NewReader(tampered), s.pub)
	if err == nil {
		t.Fatal("expected verification to fail after target swap, got nil")
	}
	if verified != 0 {
		t.Errorf("verified should stop at first failure; got %d", verified)
	}
	if !strings.Contains(err.Error(), "signature mismatch") {
		t.Errorf("error should mention signature mismatch, got: %v", err)
	}
}

func TestVerifyManifestStream_WrongPubkey(t *testing.T) {
	logPath, cleanup := signerFixture(t)
	defer cleanup()

	s := newEd25519Signer("default", logPath)
	if err := s.sign("users_delete", destructiveInput{Identifier: "alice", Execute: true}); err != nil {
		t.Fatal(err)
	}

	// Fresh, unrelated pubkey.
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)

	data, _ := os.ReadFile(logPath)
	if _, err := VerifyManifestStream(bytes.NewReader(data), otherPub); err == nil {
		t.Fatal("expected verification to fail with wrong pubkey, got nil")
	}
}

func TestEd25519Signer_TransientKeychainErrorDoesNotRegenerate(t *testing.T) {
	// Regression for the Bugbot finding on PR #25: a transient,
	// non-not-found keychain error (locked, permission denied, etc.)
	// must NOT fall through to keypair regeneration. Doing so would
	// overwrite an existing entry once the keychain comes back, breaking
	// verification of every previously-signed manifest.
	logPath, cleanup := signerFixture(t)
	defer cleanup()

	// Override the seam to simulate a transient failure.
	prevGet := keychainGetSigningKey
	defer func() { keychainGetSigningKey = prevGet }()

	var setCalled atomic.Int64
	prevSet := keychainSetSigningKey
	keychainSetSigningKey = func(profile, encoded string) error {
		setCalled.Add(1)
		return prevSet(profile, encoded)
	}
	defer func() { keychainSetSigningKey = prevSet }()

	keychainGetSigningKey = func(profile string) (string, error) {
		return "", fmt.Errorf("keychain locked: %w", errors.New("user not authenticated"))
	}

	s := newEd25519Signer("default", logPath)
	err := s.sign("users_delete", destructiveInput{Identifier: "alice", Execute: true})
	if err == nil {
		t.Fatal("expected sign() to fail on transient keychain error, got nil")
	}
	if !strings.Contains(err.Error(), "retrieving signing key") {
		t.Errorf("error should name the retrieval step, got: %v", err)
	}
	if setCalled.Load() != 0 {
		t.Errorf("keychain Set called %d times — must NOT regenerate on transient errors", setCalled.Load())
	}
	// Manifest log must not have been written.
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Errorf("signed log should not exist after failed sign attempt; stat err = %v", err)
	}
}

func TestEd25519Signer_NotFoundErrorBootstraps(t *testing.T) {
	// Counterpart to the transient-error test: a genuine ErrNotFound
	// (the expected first-run state) DOES trigger bootstrap.
	logPath, cleanup := signerFixture(t)
	defer cleanup()

	s := newEd25519Signer("default", logPath)
	if err := s.sign("users_delete", destructiveInput{Identifier: "alice", Execute: true}); err != nil {
		t.Fatalf("first-run sign should bootstrap and succeed, got: %v", err)
	}
	// Second op should reuse, not regenerate.
	pub1 := s.pubB64
	if err := s.sign("users_delete", destructiveInput{Identifier: "bob", Execute: true}); err != nil {
		t.Fatalf("second sign: %v", err)
	}
	if s.pubB64 != pub1 {
		t.Errorf("pubkey changed across ops: %q → %q", pub1, s.pubB64)
	}
}

func TestEd25519Signer_RedactsSensitiveArgs(t *testing.T) {
	logPath, cleanup := signerFixture(t)
	defer cleanup()

	type fakeArgs struct {
		Identifier string `json:"identifier"`
		APIKey     string `json:"api_key"`
		Execute    bool   `json:"execute"`
	}

	s := newEd25519Signer("default", logPath)
	if err := s.sign("admins_create", fakeArgs{Identifier: "alice", APIKey: "supersecret123456", Execute: true}); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(logPath)
	if bytes.Contains(data, []byte("supersecret123456")) {
		t.Errorf("manifest leaked the API key in args_redacted: %s", data)
	}
	if !bytes.Contains(data, []byte("REDACTED")) {
		t.Errorf("manifest should include REDACTED placeholder: %s", data)
	}
}

// Integration: confirm the chokepoint actually invokes the signer on a
// successful destructive op, and skips it when the signer is disabled.

type recordingSigner struct {
	calls    atomic.Int64
	lastTool string
}

func (r *recordingSigner) sign(toolName string, args any) error {
	r.calls.Add(1)
	r.lastTool = toolName
	return nil
}

func TestChokepoint_SignsSuccessfulDestructiveOps(t *testing.T) {
	setupToolTest(t)

	users := []map[string]any{
		{"_id": "aabbccddee112233aabbcc01", "username": "alice"},
	}
	ts := startV1Server(t, users, nil, nil)
	overrideV1ClientForTest(t, ts.URL)

	rec := &recordingSigner{}
	cs := connectToolTestServer(t, Options{signer: rec})

	result := callTool(t, cs, "users_delete", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
		"execute":    true,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getResultText(t, result))
	}
	if got := rec.calls.Load(); got != 1 {
		t.Errorf("signer calls = %d, want 1", got)
	}
	if rec.lastTool != "users_delete" {
		t.Errorf("signer tool = %q, want users_delete", rec.lastTool)
	}
}

func TestChokepoint_DoesNotSignFailedOps(t *testing.T) {
	setupToolTest(t)

	// No upstream server → users_delete will fail to reach the API.
	rec := &recordingSigner{}
	cs := connectToolTestServer(t, Options{signer: rec})

	result := callTool(t, cs, "users_delete", map[string]any{
		"identifier": "doesnotexist",
		"execute":    true,
	})
	if !result.IsError {
		t.Fatal("expected destructive call to fail without upstream API")
	}
	if got := rec.calls.Load(); got != 0 {
		t.Errorf("signer calls on failed op = %d, want 0", got)
	}
}

func TestChokepoint_DoesNotSignPlanMode(t *testing.T) {
	setupToolTest(t)

	rec := &recordingSigner{}
	cs := connectToolTestServer(t, Options{signer: rec})

	// Pre-resolved hex ID so the resolver doesn't hit the upstream API.
	// Plan mode (no execute=true) should short-circuit before any API
	// call and skip the signer entirely.
	result := callTool(t, cs, "users_delete", map[string]any{
		"identifier": "aabbccddee112233aabbcc01",
		// execute omitted → plan mode
	})
	if result.IsError {
		t.Fatalf("plan mode shouldn't error: %s", getResultText(t, result))
	}
	if got := rec.calls.Load(); got != 0 {
		t.Errorf("signer calls in plan mode = %d, want 0", got)
	}
}
