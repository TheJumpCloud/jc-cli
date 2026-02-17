package cmd

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/keychain"
)

// setupInputReader overrides the input reader used by the setup wizard.
// nil means use defaultInput (production stdin).
var setupInputReader InputReader

func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactive onboarding wizard",
		Long: `Walk through first-time JumpCloud CLI configuration interactively.

The wizard guides you through:
  - Selecting or creating a profile
  - Authenticating with an API key or service account
  - Setting your organization ID
  - Choosing default output format, color, and list limit

On re-run, existing settings are shown and can be kept by pressing Enter.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if viper.GetBool("non-interactive") {
				return fmt.Errorf("setup requires interactive input. Remove --non-interactive to run the wizard")
			}

			input := setupInputReader
			if input == nil {
				input = defaultInput
			}

			wiz := &setupWizard{
				cmd:   cmd,
				input: input,
				w:     cmd.ErrOrStderr(),
				out:   cmd.OutOrStdout(),
			}
			return wiz.run()
		},
	}
	return cmd
}

// setupWizard orchestrates the interactive setup flow.
type setupWizard struct {
	cmd   *cobra.Command
	input InputReader
	w     io.Writer // stderr — prompts and progress
	out   io.Writer // stdout — final summary
}

func (wiz *setupWizard) run() error {
	wiz.printWelcome()

	profile, err := wiz.stepProfile()
	if err != nil {
		return err
	}

	org, err := wiz.stepAuth(profile)
	if err != nil {
		return err
	}

	if err := wiz.stepOrgID(profile, org); err != nil {
		return err
	}

	if err := wiz.stepOutputFormat(); err != nil {
		return err
	}

	if err := wiz.stepColor(); err != nil {
		return err
	}

	if err := wiz.stepLimit(); err != nil {
		return err
	}

	wiz.stepMultiProfileGuidance()

	wiz.printSummary(profile)
	return nil
}

func (wiz *setupWizard) printWelcome() {
	fmt.Fprintln(wiz.w)
	fmt.Fprintln(wiz.w, "Welcome to JumpCloud CLI setup!")
	fmt.Fprintln(wiz.w, "This wizard will walk you through the initial configuration.")
	fmt.Fprintln(wiz.w)

	profiles := config.ProfileNames()
	if len(profiles) > 0 {
		active := config.ActiveProfile()
		fmt.Fprintf(wiz.w, "Existing profiles: %s (active: %s)\n", strings.Join(profiles, ", "), active)
	}
	fmt.Fprintln(wiz.w)
}

// stepProfile lets the user select or create a profile.
func (wiz *setupWizard) stepProfile() (string, error) {
	active := config.ActiveProfile()
	profiles := config.ProfileNames()

	if len(profiles) > 0 {
		fmt.Fprintf(wiz.w, "Profile [%s]: ", active)
	} else {
		fmt.Fprintf(wiz.w, "Profile name [default]: ", )
		active = "default"
	}

	line, err := wiz.input.ReadLine()
	if err != nil {
		return "", fmt.Errorf("failed to read profile name: %w", err)
	}
	line = strings.TrimSpace(line)

	if line == "" {
		return active, nil
	}

	// If the user typed a new profile name, set it as active.
	if !config.ProfileExists(line) {
		// Create the profile by setting a placeholder field.
		if err := config.SetProfileField(line, "api_key", ""); err != nil {
			return "", fmt.Errorf("failed to create profile %q: %w", line, err)
		}
	}
	if err := config.SetActiveProfile(line); err != nil {
		return "", fmt.Errorf("failed to set active profile: %w", err)
	}
	return line, nil
}

// stepAuth handles authentication — API key or service account.
func (wiz *setupWizard) stepAuth(profile string) (*api.Organization, error) {
	// Detect existing auth.
	authMethod := config.AuthMethod()
	hasExistingAuth := false

	if authMethod == "service_account" {
		if config.ClientID() != "" && config.ClientSecret() != "" {
			hasExistingAuth = true
		}
	} else {
		if config.APIKey() != "" {
			hasExistingAuth = true
		}
	}

	if hasExistingAuth {
		// Validate existing credentials.
		fmt.Fprintf(wiz.w, "Validating existing credentials...")
		org, err := wiz.validateExistingAuth(profile)
		if err == nil {
			fmt.Fprintf(wiz.w, " OK (%s)\n", org.DisplayName)
			keep, kerr := wiz.promptYesNo("Keep current credentials?", true)
			if kerr != nil {
				return nil, kerr
			}
			if keep {
				return org, nil
			}
		} else {
			fmt.Fprintf(wiz.w, " FAILED\n")
			fmt.Fprintf(wiz.w, "Existing credentials are invalid. Let's set up new ones.\n")
		}
	}

	// Choose auth method.
	fmt.Fprintln(wiz.w, "Authentication method:")
	fmt.Fprintln(wiz.w, "  1) API Key")
	fmt.Fprintln(wiz.w, "  2) Service Account (OAuth 2.0)")
	fmt.Fprintf(wiz.w, "Select [1]: ")

	choice, err := wiz.input.ReadLine()
	if err != nil {
		return nil, fmt.Errorf("failed to read auth method: %w", err)
	}
	choice = strings.TrimSpace(choice)

	if choice == "" || choice == "1" {
		return wiz.authAPIKey(profile)
	} else if choice == "2" {
		return wiz.authServiceAccount(profile)
	}
	return nil, fmt.Errorf("invalid auth method selection: %s", choice)
}

func (wiz *setupWizard) validateExistingAuth(profile string) (*api.Organization, error) {
	authMethod := config.AuthMethod()
	if authMethod == "service_account" {
		clientID := config.ClientID()
		clientSecret := config.ClientSecret()
		tc := api.NewTokenCache(clientID, clientSecret)
		client := newOAuthClient(tc)
		return client.ValidateAPIKey()
	}
	apiKey := config.APIKey()
	client := newAPIClient(apiKey)
	return client.ValidateAPIKey()
}

func (wiz *setupWizard) authAPIKey(profile string) (*api.Organization, error) {
	fmt.Fprint(wiz.w, "Enter JumpCloud API key: ")
	apiKey, err := wiz.input.ReadAPIKey()
	fmt.Fprintln(wiz.w) // newline after masked input
	if err != nil {
		return nil, err
	}
	if apiKey == "" {
		return nil, fmt.Errorf("API key cannot be empty")
	}

	// Validate.
	fmt.Fprintf(wiz.w, "Validating API key...")
	client := newAPIClient(apiKey)
	org, err := client.ValidateAPIKey()
	if err != nil {
		fmt.Fprintln(wiz.w)
		return nil, fmt.Errorf("authentication failed: %w", err)
	}
	fmt.Fprintf(wiz.w, " OK\n")

	// Store in keychain.
	if err := wiz.storeAPIKeyInKeychain(profile, apiKey); err != nil {
		return nil, err
	}

	// Clear any leftover service account fields.
	_ = config.SetProfileField(profile, "auth_method", "")
	_ = config.SetProfileField(profile, "client_id", "")
	_ = config.SetProfileField(profile, "client_secret", "")

	return org, nil
}

func (wiz *setupWizard) authServiceAccount(profile string) (*api.Organization, error) {
	fmt.Fprint(wiz.w, "Enter client ID: ")
	clientID, err := wiz.input.ReadLine()
	if err != nil {
		return nil, fmt.Errorf("failed to read client ID: %w", err)
	}
	if clientID == "" {
		return nil, fmt.Errorf("client ID cannot be empty")
	}

	fmt.Fprint(wiz.w, "Enter client secret: ")
	clientSecret, err := wiz.input.ReadAPIKey()
	fmt.Fprintln(wiz.w) // newline after masked input
	if err != nil {
		return nil, err
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("client secret cannot be empty")
	}

	// Obtain token.
	fmt.Fprintf(wiz.w, "Obtaining bearer token...")
	tc := api.NewTokenCache(clientID, clientSecret)
	_, err = tc.Token()
	if err != nil {
		fmt.Fprintln(wiz.w)
		return nil, fmt.Errorf("authentication failed: %w", err)
	}
	fmt.Fprintf(wiz.w, " OK\n")

	// Validate.
	fmt.Fprintf(wiz.w, "Validating credentials...")
	client := newOAuthClient(tc)
	org, err := client.ValidateAPIKey()
	if err != nil {
		fmt.Fprintln(wiz.w)
		return nil, fmt.Errorf("token validation failed: %w", err)
	}
	fmt.Fprintf(wiz.w, " OK\n")

	// Store client secret in keychain.
	if err := wiz.storeClientSecretInKeychain(profile, clientSecret); err != nil {
		return nil, err
	}

	// Save auth method and client ID.
	if err := config.SetProfileField(profile, "auth_method", "service_account"); err != nil {
		return nil, fmt.Errorf("failed to save auth method: %w", err)
	}
	if err := config.SetProfileField(profile, "client_id", clientID); err != nil {
		return nil, fmt.Errorf("failed to save client ID: %w", err)
	}

	// Clear any leftover api_key field.
	_ = config.SetProfileField(profile, "api_key", "")

	return org, nil
}

func (wiz *setupWizard) storeAPIKeyInKeychain(profile, apiKey string) error {
	if keychain.IsAvailable() {
		if err := keychain.Set(profile, apiKey); err != nil {
			fmt.Fprintf(wiz.w, "Warning: could not store key in keychain: %v\n", err)
			if err := config.SetProfileField(profile, "api_key", apiKey); err != nil {
				return fmt.Errorf("failed to save API key to config: %w", err)
			}
			fmt.Fprintf(wiz.w, "Warning: API key stored as plaintext in config file\n")
		} else {
			ref := keychain.URI(profile)
			if err := config.SetProfileField(profile, "api_key", ref); err != nil {
				return fmt.Errorf("failed to save keychain reference: %w", err)
			}
		}
	} else {
		fmt.Fprintf(wiz.w, "Warning: OS keychain unavailable. Storing API key as plaintext in config\n")
		if err := config.SetProfileField(profile, "api_key", apiKey); err != nil {
			return fmt.Errorf("failed to save API key to config: %w", err)
		}
	}
	return nil
}

func (wiz *setupWizard) storeClientSecretInKeychain(profile, secret string) error {
	if keychain.IsAvailable() {
		if err := keychain.SetClientSecret(profile, secret); err != nil {
			fmt.Fprintf(wiz.w, "Warning: could not store client secret in keychain: %v\n", err)
			if err := config.SetProfileField(profile, "client_secret", secret); err != nil {
				return fmt.Errorf("failed to save client secret to config: %w", err)
			}
			fmt.Fprintf(wiz.w, "Warning: client secret stored as plaintext in config file\n")
		} else {
			ref := keychain.ClientSecretURI(profile)
			if err := config.SetProfileField(profile, "client_secret", ref); err != nil {
				return fmt.Errorf("failed to save keychain reference: %w", err)
			}
		}
	} else {
		fmt.Fprintf(wiz.w, "Warning: OS keychain unavailable. Storing client secret as plaintext in config\n")
		if err := config.SetProfileField(profile, "client_secret", secret); err != nil {
			return fmt.Errorf("failed to save client secret to config: %w", err)
		}
	}
	return nil
}

// stepOrgID confirms or overrides the auto-detected org ID.
func (wiz *setupWizard) stepOrgID(profile string, org *api.Organization) error {
	current := config.OrgID()
	detected := org.ID

	if current == "" && detected != "" {
		current = detected
	}

	if current != "" {
		fmt.Fprintf(wiz.w, "Organization ID [%s]: ", current)
	} else {
		fmt.Fprint(wiz.w, "Organization ID: ")
	}

	line, err := wiz.input.ReadLine()
	if err != nil {
		return fmt.Errorf("failed to read org ID: %w", err)
	}
	line = strings.TrimSpace(line)

	if line == "" {
		line = current
	}

	if line != "" {
		if err := config.SetProfileField(profile, "org_id", line); err != nil {
			return fmt.Errorf("failed to save org ID: %w", err)
		}
	}
	return nil
}

// stepOutputFormat lets the user pick a default output format.
func (wiz *setupWizard) stepOutputFormat() error {
	current := config.Output()
	fmt.Fprintf(wiz.w, "Default output format (%s) [%s]: ",
		strings.Join(validOutputFormats, "/"), current)

	line, err := wiz.input.ReadLine()
	if err != nil {
		return fmt.Errorf("failed to read output format: %w", err)
	}
	line = strings.TrimSpace(line)

	if line == "" {
		return nil // keep current
	}

	// Validate.
	valid := false
	for _, f := range validOutputFormats {
		if line == f {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid output format %q. Valid formats: %s",
			line, strings.Join(validOutputFormats, ", "))
	}

	if err := config.SetConfigValue("defaults.output", line); err != nil {
		return fmt.Errorf("failed to save output format: %w", err)
	}
	return nil
}

// stepColor lets the user toggle color output.
func (wiz *setupWizard) stepColor() error {
	current := viper.GetBool("defaults.color")
	keep, err := wiz.promptYesNo("Enable color output?", current)
	if err != nil {
		return err
	}
	val := "true"
	if !keep {
		val = "false"
	}
	if err := config.SetConfigValue("defaults.color", val); err != nil {
		return fmt.Errorf("failed to save color setting: %w", err)
	}
	return nil
}

// stepLimit lets the user set the default list limit.
func (wiz *setupWizard) stepLimit() error {
	current := viper.GetInt("defaults.limit")
	fmt.Fprintf(wiz.w, "Default list limit [%d]: ", current)

	line, err := wiz.input.ReadLine()
	if err != nil {
		return fmt.Errorf("failed to read limit: %w", err)
	}
	line = strings.TrimSpace(line)

	if line == "" {
		return nil // keep current
	}

	n, err := strconv.Atoi(line)
	if err != nil || n <= 0 {
		return fmt.Errorf("invalid limit %q: must be a positive integer", line)
	}

	if err := config.SetConfigValue("defaults.limit", line); err != nil {
		return fmt.Errorf("failed to save limit: %w", err)
	}
	return nil
}

// stepMultiProfileGuidance prints guidance about managing multiple profiles.
func (wiz *setupWizard) stepMultiProfileGuidance() {
	fmt.Fprintln(wiz.w)
	fmt.Fprintln(wiz.w, "Tip: To add more profiles later, run:")
	fmt.Fprintln(wiz.w, "  jc auth login --profile <name>")
	fmt.Fprintln(wiz.w, "  jc auth switch <name>")
	fmt.Fprintln(wiz.w)
}

// printSummary outputs the final configuration to stdout.
func (wiz *setupWizard) printSummary(profile string) {
	fmt.Fprintln(wiz.out, "Setup complete!")
	fmt.Fprintln(wiz.out)
	fmt.Fprintf(wiz.out, "Profile:       %s\n", profile)

	authMethod := config.AuthMethod()
	fmt.Fprintf(wiz.out, "Auth Method:   %s\n", authMethod)

	if authMethod == "service_account" {
		fmt.Fprintf(wiz.out, "Client ID:     %s\n", config.ClientID())
	} else {
		apiKey := config.APIKey()
		fmt.Fprintf(wiz.out, "API Key:       %s\n", api.RedactKey(apiKey))
	}

	orgID := config.OrgID()
	if orgID != "" {
		fmt.Fprintf(wiz.out, "Org ID:        %s\n", orgID)
	}

	fmt.Fprintf(wiz.out, "Output:        %s\n", config.Output())
	fmt.Fprintf(wiz.out, "Color:         %v\n", viper.GetBool("defaults.color"))
	fmt.Fprintf(wiz.out, "Limit:         %d\n", viper.GetInt("defaults.limit"))
}

// promptYesNo displays a yes/no prompt with a default.
func (wiz *setupWizard) promptYesNo(prompt string, defaultYes bool) (bool, error) {
	suffix := " (Y/n): "
	if !defaultYes {
		suffix = " (y/N): "
	}
	fmt.Fprint(wiz.w, prompt+suffix)

	line, err := wiz.input.ReadLine()
	if err != nil {
		return false, fmt.Errorf("failed to read response: %w", err)
	}
	line = strings.TrimSpace(strings.ToLower(line))

	if line == "" {
		return defaultYes, nil
	}
	return line == "y" || line == "yes", nil
}
