package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

// keychainIsAvailable checks if the OS keychain is usable. Overridable in tests.
var keychainIsAvailable = keychain.IsAvailable

// getAllowPlaintext returns true if the --allow-plaintext flag is set on this
// command or any of its parent commands (it's a persistent flag on auth).
func getAllowPlaintext(cmd *cobra.Command) bool {
	f := cmd.Flag("allow-plaintext")
	return f != nil && f.Value.String() == "true"
}

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

	cmd.PersistentFlags().Bool("allow-plaintext", false, "Allow storing credentials as plaintext in the config file when the OS keychain is unavailable. SECURITY RISK: anyone reading ~/.config/jc/config.yaml (backups, sync clients, malware) recovers the credential. Prefer fixing the keychain or running on a host where it works.")

	cmd.AddCommand(newAuthLoginCmd())
	cmd.AddCommand(newAuthStatusCmd())
	cmd.AddCommand(newAuthLogoutCmd())
	cmd.AddCommand(newAuthSwitchCmd())

	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	var profileFlag string
	var serviceAccountFlag bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with JumpCloud",
		Long: `Authenticate with JumpCloud by providing service account credentials or an API key.

Recommended — service account (--service-account):
  Prompts for client ID and client secret (OAuth 2.0 client credentials).
  The client secret is stored in the OS keychain. A short-lived bearer
  token is obtained from the OAuth token endpoint and refreshed
  automatically. Service accounts are easier to rotate, revoke, and
  scope than personal API keys, so this is the recommended path for new
  deployments.

Alternative — API key (default for backwards compatibility):
  The API key is validated, stored in the OS keychain, and a reference
  is saved to the config file. API keys are long-lived bearer secrets;
  prefer service account auth for production use.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if serviceAccountFlag {
				return runAuthLoginServiceAccount(cmd, profileFlag, defaultInput)
			}
			// Auto-detect: if the profile is configured as a service account, use that flow.
			profile := profileFlag
			if profile == "" {
				profile = config.ActiveProfile()
			}
			method := viper.GetString("profiles." + profile + ".auth_method")
			if method == "service_account" {
				return runAuthLoginServiceAccount(cmd, profileFlag, defaultInput)
			}
			return runAuthLogin(cmd, profileFlag, defaultInput)
		},
	}

	cmd.Flags().StringVar(&profileFlag, "profile", "", "Profile name to create or update (default: active profile)")
	cmd.Flags().BoolVar(&serviceAccountFlag, "service-account", false, "Authenticate using OAuth 2.0 service account (client ID + client secret)")

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

	// One-line nudge toward the recommended path. Operators who explicitly
	// want API key auth see it once and continue; new operators learn
	// service account exists. Printed to stderr so it doesn't pollute any
	// stdout-piping setup.
	fmt.Fprintln(cmd.ErrOrStderr(),
		"Tip: service account auth (jc auth login --service-account) is recommended over personal API keys. "+
			"See docs/AUTH.md for details.")

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
	allowPlaintext := getAllowPlaintext(cmd)
	if keychainIsAvailable() {
		if err := keychain.Set(profile, apiKey); err != nil {
			if !allowPlaintext {
				return fmt.Errorf("could not store key in keychain: %w. Use --allow-plaintext to store credentials in config file", err)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not store key in keychain: %v\n", err)
			// Fall back to plaintext in config.
			if err := config.SetProfileField(profile, "api_key", apiKey); err != nil {
				return fmt.Errorf("failed to save API key to config: %w", err)
			}
			fmt.Fprintf(cmd.ErrOrStderr(),
				"Warning: API key stored as plaintext in %s. "+
					"Anyone with read access to that file recovers the credential. "+
					"Fix your keychain setup and re-run 'jc auth login' as soon as possible.\n",
				config.ConfigPath())
		} else {
			// Write keychain reference to config.
			ref := keychain.URI(profile)
			if err := config.SetProfileField(profile, "api_key", ref); err != nil {
				return fmt.Errorf("failed to save keychain reference to config: %w", err)
			}
		}
	} else {
		if !allowPlaintext {
			return fmt.Errorf("OS keychain unavailable. Use --allow-plaintext to store credentials in config file, or fix your keychain setup")
		}
		fmt.Fprintf(cmd.ErrOrStderr(),
			"Warning: OS keychain unavailable. Storing API key as plaintext in %s. "+
				"Anyone with read access to that file recovers the credential. "+
				"Fix your keychain setup and re-run 'jc auth login' as soon as possible.\n",
			config.ConfigPath())
		if err := config.SetProfileField(profile, "api_key", apiKey); err != nil {
			return fmt.Errorf("failed to save API key to config: %w", err)
		}
	}

	// Clear any service account fields — this profile is now API key auth.
	_ = config.SetProfileField(profile, "auth_method", "")
	_ = config.SetProfileField(profile, "client_id", "")
	_ = config.SetProfileField(profile, "client_secret", "")

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

// newOAuthClient creates a bearer-token-authenticated API client. Overridable in tests.
var newOAuthClient = func(tc *api.TokenCache) *api.Client {
	return api.NewClientWithToken(tc)
}

func runAuthLoginServiceAccount(cmd *cobra.Command, profileFlag string, input InputReader) error {
	profile := profileFlag
	if profile == "" {
		profile = config.ActiveProfile()
	}

	// If credentials are already stored and valid, skip prompting.
	existingID := config.ClientID()
	existingSecret := config.ClientSecret()
	if existingID != "" && existingSecret != "" {
		tc := api.NewTokenCache(existingID, existingSecret)
		if _, err := tc.Token(cmd.Context()); err == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "Already authenticated as service account (profile: %s)\n", profile)
			return nil
		}
		// Token fetch failed — fall through to re-prompt.
	}

	// Check if running non-interactively.
	if viper.GetBool("non-interactive") {
		return fmt.Errorf("auth login --service-account requires interactive input. Remove --non-interactive")
	}

	// Prompt for client ID.
	fmt.Fprint(cmd.ErrOrStderr(), "Enter client ID: ")
	clientID, err := input.ReadLine()
	if err != nil {
		return fmt.Errorf("failed to read client ID: %w", err)
	}
	if clientID == "" {
		return fmt.Errorf("client ID cannot be empty")
	}

	// Prompt for client secret with masked input.
	fmt.Fprint(cmd.ErrOrStderr(), "Enter client secret: ")
	clientSecret, err := input.ReadAPIKey()
	fmt.Fprintln(cmd.ErrOrStderr()) // newline after masked input
	if err != nil {
		return err
	}
	if clientSecret == "" {
		return fmt.Errorf("client secret cannot be empty")
	}

	// Validate by obtaining a token.
	fmt.Fprintf(cmd.ErrOrStderr(), "Obtaining bearer token...")
	tc := api.NewTokenCache(clientID, clientSecret)
	_, err = tc.Token(cmd.Context())
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr())
		return fmt.Errorf("authentication failed: %w", err)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), " OK\n")

	// Fetch org info (best-effort — service accounts may lack /organizations access).
	fmt.Fprintf(cmd.ErrOrStderr(), "Fetching organization info...")
	client := newOAuthClient(tc)
	org, err := client.ValidateAPIKey()
	if err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
			fmt.Fprintf(cmd.ErrOrStderr(), " skipped (insufficient permissions)\n")
			org = &api.Organization{}
		} else {
			fmt.Fprintln(cmd.ErrOrStderr())
			return fmt.Errorf("token validation failed: %w", err)
		}
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), " OK\n")
	}

	// Store the client secret in the keychain.
	allowPlaintext := getAllowPlaintext(cmd)
	if keychainIsAvailable() {
		if err := keychain.SetClientSecret(profile, clientSecret); err != nil {
			if !allowPlaintext {
				return fmt.Errorf("could not store client secret in keychain: %w. Use --allow-plaintext to store credentials in config file", err)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not store client secret in keychain: %v\n", err)
			// Fall back to plaintext in config.
			if err := config.SetProfileField(profile, "client_secret", clientSecret); err != nil {
				return fmt.Errorf("failed to save client secret to config: %w", err)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: client secret stored as plaintext in config file\n")
		} else {
			// Write keychain reference to config.
			ref := keychain.ClientSecretURI(profile)
			if err := config.SetProfileField(profile, "client_secret", ref); err != nil {
				return fmt.Errorf("failed to save keychain reference to config: %w", err)
			}
		}
	} else {
		if !allowPlaintext {
			return fmt.Errorf("OS keychain unavailable. Use --allow-plaintext to store credentials in config file, or fix your keychain setup")
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: OS keychain unavailable. Storing client secret as plaintext in config\n")
		if err := config.SetProfileField(profile, "client_secret", clientSecret); err != nil {
			return fmt.Errorf("failed to save client secret to config: %w", err)
		}
	}

	// Save auth method and client ID to config.
	if err := config.SetProfileField(profile, "auth_method", "service_account"); err != nil {
		return fmt.Errorf("failed to save auth method to config: %w", err)
	}
	if err := config.SetProfileField(profile, "client_id", clientID); err != nil {
		return fmt.Errorf("failed to save client ID to config: %w", err)
	}

	// Clear any leftover api_key field.
	_ = config.SetProfileField(profile, "api_key", "")

	// Set org_id if not already set.
	existingOrgID := config.OrgID()
	if existingOrgID == "" && org.ID != "" {
		if err := config.SetProfileField(profile, "org_id", org.ID); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not save org ID to config: %v\n", err)
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
	if orgName != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Logged in to %s via service account (profile: %s)\n", orgName, profile)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Logged in via service account (profile: %s)\n", profile)
	}
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
	authMethod := config.AuthMethod()
	outputFmt := config.Output()

	status := authStatusInfo{
		Authenticated: false,
		Profile:       profile,
		AuthMethod:    authMethod,
		ProfileRole:   config.ProfileRole(profile),
	}

	if authMethod == "service_account" {
		clientID := config.ClientID()
		clientSecret := config.ClientSecret()

		if clientID != "" && clientSecret != "" {
			tc := api.NewTokenCache(clientID, clientSecret)
			_, tokenErr := tc.Token(cmd.Context())
			if tokenErr == nil {
				status.Authenticated = true
				status.ClientID = clientID
				expiresAt := tc.ExpiresAt()
				if !expiresAt.IsZero() {
					status.TokenExpiry = expiresAt.UTC().Format("2006-01-02T15:04:05Z")
				}

				client := newOAuthClient(tc)
				org, err := client.ValidateAPIKey()
				if err == nil {
					status.OrgName = org.DisplayName
					status.OrgID = org.ID
				} else {
					status.OrgID = config.OrgID()
				}
			}
		}
	}

	// Fall back to API key if not yet authenticated (covers the case where
	// auth_method is service_account but an API key was stored via auth login).
	if !status.Authenticated {
		apiKey := config.APIKey()
		if apiKey != "" {
			client := newAPIClient(apiKey)
			org, err := client.ValidateAPIKey()
			if err == nil {
				status.Authenticated = true
				status.AuthMethod = "api_key"
				status.OrgName = org.DisplayName
				status.OrgID = org.ID
				status.APIKeyRedacted = api.RedactKey(apiKey)
			}
		}
	}

	if viper.GetBool("quiet") {
		if !status.Authenticated {
			return &ExitError{Code: ExitCodeAuthFailed}
		}
		return nil
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
		if status.ProfileRole != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Profile Role:  %s\n", status.ProfileRole)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Auth Method:   %s\n", status.AuthMethod)
		fmt.Fprintf(cmd.OutOrStdout(), "Org Name:      %s\n", status.OrgName)
		fmt.Fprintf(cmd.OutOrStdout(), "Org ID:        %s\n", status.OrgID)
		if status.AuthMethod == "service_account" {
			fmt.Fprintf(cmd.OutOrStdout(), "Client ID:     %s\n", status.ClientID)
			if status.TokenExpiry != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Token Expiry:  %s\n", status.TokenExpiry)
			}
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "API Key:       %s\n", status.APIKeyRedacted)
		}
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Authenticated: no\n")
		fmt.Fprintf(cmd.OutOrStdout(), "Profile:       %s\n", status.Profile)
		if status.ProfileRole != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Profile Role:  %s\n", status.ProfileRole)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Auth Method:   %s\n", status.AuthMethod)
		if status.AuthMethod == "service_account" {
			fmt.Fprintf(cmd.OutOrStdout(), "Run 'jc auth login --service-account' to re-authenticate.\n")
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Run 'jc auth login' to authenticate.\n")
		}
		return &ExitError{Code: ExitCodeAuthFailed}
	}

	return nil
}

type authStatusInfo struct {
	Authenticated  bool   `json:"authenticated"`
	Profile        string `json:"profile"`
	ProfileRole    string `json:"profile_role,omitempty"`
	AuthMethod     string `json:"auth_method"`
	OrgName        string `json:"org_name,omitempty"`
	OrgID          string `json:"org_id,omitempty"`
	APIKeyRedacted string `json:"api_key,omitempty"`
	ClientID       string `json:"client_id,omitempty"`
	TokenExpiry    string `json:"token_expiry,omitempty"`
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
	authMethod := config.AuthMethod()

	if authMethod == "service_account" {
		// Remove client secret from keychain.
		if err := keychain.DeleteClientSecret(profile); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Note: %v\n", err)
		}

		// Clear service account fields from config.
		_ = config.SetProfileField(profile, "auth_method", "")
		_ = config.SetProfileField(profile, "client_id", "")
		if err := config.SetProfileField(profile, "client_secret", ""); err != nil {
			return fmt.Errorf("failed to clear client secret from config: %w", err)
		}
	} else {
		// Try to remove API key from keychain.
		if err := keychain.Delete(profile); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Note: %v\n", err)
		}

		// Clear profile credentials in config.
		if err := config.SetProfileField(profile, "api_key", ""); err != nil {
			return fmt.Errorf("failed to clear API key from config: %w", err)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Logged out (profile: %s)\n", profile)
	return nil
}

func newAuthSwitchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "switch [profile-name]",
		Short: "Switch the active profile",
		Long: `Switch the active profile to a different named profile.

If no profile name is given, an interactive picker is shown.
Each profile has its own API key, org ID, and optional defaults.

Examples:
  jc auth switch production
  jc auth switch              # interactive picker`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeProfileNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthSwitch(cmd, args, defaultInput)
		},
	}
	return cmd
}

func runAuthSwitch(cmd *cobra.Command, args []string, input InputReader) error {
	profiles := config.ProfileNames()
	if len(profiles) == 0 {
		return fmt.Errorf("no profiles configured. Run 'jc auth login --profile <name>' to create one")
	}

	var target string

	if len(args) == 1 {
		// Explicit profile name given.
		target = args[0]
	} else {
		// Interactive picker.
		if viper.GetBool("non-interactive") {
			return fmt.Errorf("profile name required in non-interactive mode")
		}

		active := config.ActiveProfile()
		fmt.Fprintf(cmd.ErrOrStderr(), "Available profiles:\n")
		for i, p := range profiles {
			marker := "  "
			if p == active {
				marker = "* "
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "  %s%d) %s\n", marker, i+1, p)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "\nSelect profile [1-%d]: ", len(profiles))

		line, err := input.ReadLine()
		if err != nil {
			return fmt.Errorf("failed to read selection: %w", err)
		}

		var idx int
		if _, err := fmt.Sscanf(line, "%d", &idx); err != nil || idx < 1 || idx > len(profiles) {
			return fmt.Errorf("invalid selection: %s", line)
		}
		target = profiles[idx-1]
	}

	if !config.ProfileExists(target) {
		available := strings.Join(config.ProfileNames(), ", ")
		return fmt.Errorf("profile %q not found. Available profiles: %s", target, available)
	}

	if err := config.SetActiveProfile(target); err != nil {
		return fmt.Errorf("failed to switch profile: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Switched to profile: %s\n", target)
	return nil
}

// completeProfileNames provides tab completion for profile names.
func completeProfileNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return config.ProfileNames(), cobra.ShellCompDirectiveNoFileComp
}
