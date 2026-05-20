package cmd

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// writeTestManifest writes one JSON-encoded signed manifest line to path.
// The shape mirrors mcp.signedManifest; we hand-roll it here to avoid an
// import cycle with the mcp package's tests.
func writeTestManifest(t *testing.T, path string, priv ed25519.PrivateKey) {
	t.Helper()
	pub := priv.Public().(ed25519.PublicKey)

	type manifest struct {
		Tool           string          `json:"tool"`
		ArgsRedacted   json.RawMessage `json:"args_redacted"`
		Target         string          `json:"target,omitempty"`
		Timestamp      string          `json:"timestamp"`
		Nonce          string          `json:"nonce"`
		OperatorPubkey string          `json:"operator_pubkey"`
		Signature      string          `json:"signature,omitempty"`
	}

	m := manifest{
		Tool:           "users_delete",
		ArgsRedacted:   json.RawMessage(`{"identifier":"alice"}`),
		Target:         "alice",
		Timestamp:      "2026-04-28T20:00:00Z",
		Nonce:          base64.StdEncoding.EncodeToString([]byte("nonce-fixture-12345678901234567")),
		OperatorPubkey: base64.StdEncoding.EncodeToString(pub),
	}
	canonical, _ := json.Marshal(m)
	m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, canonical))

	out, _ := json.Marshal(m)
	out = append(out, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestAuditVerify_OK(t *testing.T) {
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "mcp-audit-signed.log")
	writeTestManifest(t, logPath, priv)

	rootCmd := NewRootCmd()
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs([]string{"audit", "verify", "--log", logPath, "--pubkey", pubB64})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("audit verify failed: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OK") {
		t.Errorf("expected OK message, got: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "1 signed manifest") {
		t.Errorf("expected count=1 in output, got: %s", stdout.String())
	}
}

func TestAuditVerify_DetectsTamper(t *testing.T) {
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "mcp-audit-signed.log")
	writeTestManifest(t, logPath, priv)

	// Tamper with the on-disk manifest.
	data, _ := os.ReadFile(logPath)
	tampered := bytes.Replace(data, []byte(`"target":"alice"`), []byte(`"target":"bob"`), 1)
	_ = os.WriteFile(logPath, tampered, 0600)

	rootCmd := NewRootCmd()
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"audit", "verify", "--log", logPath, "--pubkey", pubB64})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected verify to fail on tampered manifest, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T: %v", err, err)
	}
	if exitErr.Code == 0 {
		t.Errorf("expected non-zero exit code on tamper, got %d", exitErr.Code)
	}
}

func TestAuditVerify_NoPubkey(t *testing.T) {
	setupTestConfig(t, `active_profile: default
profiles:
  default:
    api_key: ""
`)

	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "mcp-audit-signed.log")
	if err := os.WriteFile(logPath, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}

	rootCmd := NewRootCmd()
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"audit", "verify", "--log", logPath})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when no pubkey configured and no --pubkey passed, got nil")
	}
	if !strings.Contains(err.Error(), "no signing public key") {
		t.Errorf("error should mention missing pubkey, got: %v", err)
	}
}

func TestAuditCommandRegistered(t *testing.T) {
	rootCmd := NewRootCmd()
	var found *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Use == "audit" {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatal("expected 'audit' command to be registered")
	}
	var verifyFound bool
	for _, sub := range found.Commands() {
		if sub.Name() == "verify" {
			verifyFound = true
			break
		}
	}
	if !verifyFound {
		t.Error("expected 'verify' subcommand under 'audit'")
	}
}
