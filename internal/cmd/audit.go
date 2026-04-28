package cmd

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/mcp"
)

func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Inspect and verify MCP audit logs",
		Long:  "Subcommands for working with MCP audit logs, including signed-manifest verification (KLA-411).",
	}

	cmd.AddCommand(newAuditVerifyCmd())
	return cmd
}

func newAuditVerifyCmd() *cobra.Command {
	var profileFlag, logPath, pubkeyOverride string

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify signatures in the signed MCP audit log",
		Long: `Read every signed manifest in the MCP signed-audit-log and verify the
chain against the configured per-profile public key.

Used to catch tampering with the audit trail. A successful run reports
the count of verified records and exits 0; any signature mismatch,
truncation, or decode error exits with a non-zero code.

By default, reads ~/.config/jc/mcp-audit-signed.log and uses the active
profile's signing pubkey from config (set the first time
'mcp.sign_destructive_ops' is enabled and a destructive op fires).

Pass --pubkey to verify against an externally-attested key — useful when
investigating a host whose config might itself be tampered with.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := profileFlag
			if profile == "" {
				profile = config.ActiveProfile()
			}

			pubkeyB64 := pubkeyOverride
			if pubkeyB64 == "" {
				pubkeyB64 = config.SigningPubkey(profile)
			}
			if pubkeyB64 == "" {
				return fmt.Errorf("no signing public key for profile %q. Either run a signed destructive op first to bootstrap, or pass --pubkey explicitly", profile)
			}
			pubBytes, err := base64.StdEncoding.DecodeString(pubkeyB64)
			if err != nil {
				return fmt.Errorf("decoding stored pubkey: %w", err)
			}
			if len(pubBytes) != ed25519.PublicKeySize {
				return fmt.Errorf("stored pubkey is %d bytes, want %d", len(pubBytes), ed25519.PublicKeySize)
			}

			path := logPath
			if path == "" {
				path = filepath.Join(config.ConfigDir(), "mcp-audit-signed.log")
			}
			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("opening signed audit log %s: %w", path, err)
			}
			defer f.Close()

			verified, verr := mcp.VerifyManifestStream(f, ed25519.PublicKey(pubBytes))
			if verr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Verified %d manifest(s) before failure: %v\n", verified, verr)
				return &ExitError{Code: 1, Err: verr}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "OK — verified %d signed manifest(s) for profile %q.\n", verified, profile)
			return nil
		},
	}

	cmd.Flags().StringVar(&profileFlag, "profile", "", "Profile whose signing pubkey to use (defaults to active profile)")
	cmd.Flags().StringVar(&logPath, "log", "", "Path to the signed audit log (defaults to ~/.config/jc/mcp-audit-signed.log)")
	cmd.Flags().StringVar(&pubkeyOverride, "pubkey", "", "Verify against this base64 Ed25519 pubkey instead of the configured one (override when investigating a possibly-tampered host)")

	return cmd
}
