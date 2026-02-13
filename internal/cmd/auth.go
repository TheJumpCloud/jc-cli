package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/keychain"
)

// ExitCodeAuthFailed is the exit code when authentication check fails.
const ExitCodeAuthFailed = 3

// ExitError wraps an error with a specific exit code.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit code %d", e.Code)
}

func (e *ExitError) Unwrap() error {
	return e.Err
}

// InputReader abstracts user input for testability.
// In production this reads from os.Stdin; tests inject a custom reader.
type InputReader interface {
	ReadAPIKey() (string, error)
	ReadLine() (string, error)
}

// stdinReader reads from the real terminal.
type stdinReader struct{}

func (s *stdinReader) ReadAPIKey() (string, error) {
	raw, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", fmt.Errorf("failed to read API key: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

func (s *stdinReader) ReadLine() (string, error) {
	buf := make([]byte, 1024)
	n, err := os.Stdin.Read(buf)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf[:n])), nil
}

// defaultInput is the production input reader.
var defaultInput InputReader = &stdinReader{}

// newAPIClient creates an API client with the given key. Overridable in tests.
var newAPIClient = func(key string) *api.Client {
	return api.NewClientWithKey(key)
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication credentials",
		Long:  "Login, logout, and check authentication status for JumpCloud.",
	}

	cmd.AddCommand(newAuthLoginCmd())
	cmd.AddCommand(newAuthStatusCmd())
	cmd.AddCommand(newAuthLogoutCmd())

	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	var profileFlag string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with JumpCloud",
		Long: `Authenticate with JumpCloud by providing an API key.

The API key is validated against the JumpCloud API, stored in the OS keychain,
and a reference is saved to the config file. If the keychain is unavailable,
the key is stored in the config file with a warning.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthLogin(cmd, profileFlag, defaultInput)
		},
	}

	cmd.Flags().StringVar(&profileFlag, "profile", "", "Profile name to create or update (default: active profile)")

	return cmd
}

func runAuthLogin(cmd *cobra.Command, profileFlag string, input InputReader) error {
	profile := profileFlag
	if profile == "" {
		profile = config.ActiveProfile()
	}

	// Check if running non-interactively.
	if viper.GetBool("non-interactive") {
		return fmt.Errorf("auth login requires interactive input. Remove --non-interactive or set JC_API_KEY")
	}

	// Prompt for API key with masked input.
	fmt.Fprint(cmd.ErrOrStderr(), "Enter JumpCloud API key: ")
	apiKey, err := input.ReadAPIKey()
	fmt.Fprintln(cmd.ErrOrStderr()) // newline after masked input
	if err != nil {
		return err
	}

	if apiKey == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	// Validate the API key.
	fmt.Fprintf(cmd.ErrOrStderr(), "Validating API key...")
	client := newAPIClient(apiKey)
	org, err := client.ValidateAPIKey()
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr())
		return fmt.Errorf("authentication failed: %w", err)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), " OK\n")

	// Optionally prompt for org ID if not already set.
	existingOrgID := config.OrgID()
	if existingOrgID == "" && org.ID != "" {
		if err := config.SetProfileField(profile, "org_id", org.ID); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not save org ID to config: %v\n", err)
		}
	}

	// Store the API key in the keychain.
	keychainAvailable := keychain.IsAvailable()
	if keychainAvailable {
		if err := keychain.Set(profile, apiKey); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not store key in keychain: %v\n", err)
			// Fall back to plaintext in config.
			if err := config.SetProfileField(profile, "api_key", apiKey); err != nil {
				return fmt.Errorf("failed to save API key to config: %w", err)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: API key stored as plaintext in config file\n")
		} else {
			// Write keychain reference to config.
			ref := keychain.URI(profile)
			if err := config.SetProfileField(profile, "api_key", ref); err != nil {
				return fmt.Errorf("failed to save keychain reference to config: %w", err)
			}
		}
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: OS keychain unavailable. Storing API key as plaintext in config\n")
		if err := config.SetProfileField(profile, "api_key", apiKey); err != nil {
			return fmt.Errorf("failed to save API key to config: %w", err)
		}
	}

	// Set as active profile if it's not already.
	if config.ActiveProfile() != profile {
		if err := config.SetActiveProfile(profile); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not set active profile: %v\n", err)
		}
	}

	orgName := org.DisplayName
	if orgName == "" {
		orgName = org.ID
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Logged in to %s (profile: %s)\n", orgName, profile)
	return nil
}

func newAuthStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check authentication status",
		Long:  "Display current authentication state, profile, and organization info.",
		RunE:  runAuthStatus,
	}
	return cmd
}

func runAuthStatus(cmd *cobra.Command, args []string) error {
	profile := config.ActiveProfile()
	apiKey := config.APIKey()
	outputFmt := config.Output()

	status := authStatusInfo{
		Authenticated: false,
		Profile:       profile,
	}

	if apiKey != "" {
		// Try to validate.
		client := newAPIClient(apiKey)
		org, err := client.ValidateAPIKey()
		if err == nil {
			status.Authenticated = true
			status.OrgName = org.DisplayName
			status.OrgID = org.ID
			status.APIKeyRedacted = api.RedactKey(apiKey)
		}
	}

	if outputFmt == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}

	// Human-readable output.
	if status.Authenticated {
		fmt.Fprintf(cmd.OutOrStdout(), "Authenticated: yes\n")
		fmt.Fprintf(cmd.OutOrStdout(), "Profile:       %s\n", status.Profile)
		fmt.Fprintf(cmd.OutOrStdout(), "Org Name:      %s\n", status.OrgName)
		fmt.Fprintf(cmd.OutOrStdout(), "Org ID:        %s\n", status.OrgID)
		fmt.Fprintf(cmd.OutOrStdout(), "API Key:       %s\n", status.APIKeyRedacted)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Authenticated: no\n")
		fmt.Fprintf(cmd.OutOrStdout(), "Profile:       %s\n", status.Profile)
		fmt.Fprintf(cmd.OutOrStdout(), "Run 'jc auth login' to authenticate.\n")
		return &ExitError{Code: ExitCodeAuthFailed}
	}

	return nil
}

type authStatusInfo struct {
	Authenticated bool   `json:"authenticated"`
	Profile       string `json:"profile"`
	OrgName       string `json:"org_name,omitempty"`
	OrgID         string `json:"org_id,omitempty"`
	APIKeyRedacted string `json:"api_key,omitempty"`
}

func newAuthLogoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		Long:  "Remove the API key from the keychain and clear the profile credentials.",
		RunE:  runAuthLogout,
	}
	return cmd
}

func runAuthLogout(cmd *cobra.Command, args []string) error {
	profile := config.ActiveProfile()

	// Try to remove from keychain.
	if err := keychain.Delete(profile); err != nil {
		// Not an error if key wasn't in keychain.
		fmt.Fprintf(cmd.ErrOrStderr(), "Note: %v\n", err)
	}

	// Clear profile credentials in config.
	if err := config.SetProfileField(profile, "api_key", ""); err != nil {
		return fmt.Errorf("failed to clear API key from config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Logged out (profile: %s)\n", profile)
	return nil
}
