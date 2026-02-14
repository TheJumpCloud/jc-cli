package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/version"
)

// validOutputFormats lists the accepted --output values.
var validOutputFormats = []string{"json", "table", "csv", "human", "yaml", "ndjson"}

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "jc",
		Short: "JumpCloud CLI — manage your JumpCloud organization from the terminal",
		Long: `jc is a modern, LLM-friendly CLI for JumpCloud.

It covers the full JumpCloud API surface (v1, v2, Directory Insights) with
built-in MCP server support, a recipe system, plan mode, and conversational
interface.`,
		Version:       version.Number,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// --org overrides the active profile for this command only.
			if org := viper.GetString("org"); org != "" {
				if !config.ProfileExists(org) {
					available := strings.Join(config.ProfileNames(), ", ")
					return NewCLIError(ErrCodeConfigError,
						fmt.Sprintf("profile %q not found. Available profiles: %s", org, available),
						"Use 'jc auth login --profile <name>' to create a profile")
				}
				config.OverrideActiveProfile(org)
			}

			// -t is a convenience shorthand for --output table.
			if t, _ := cmd.Flags().GetBool("table"); t {
				viper.Set("defaults.output", "table")
			}

			// Validate the output format.
			format := viper.GetString("defaults.output")
			validFormat := false
			for _, valid := range validOutputFormats {
				if format == valid {
					validFormat = true
					break
				}
			}
			if !validFormat {
				return NewCLIError(ErrCodeValidationError,
					fmt.Sprintf("unknown output format %q. Valid formats: %s",
						format, strings.Join(validOutputFormats, ", ")),
					"Use one of: json, table, csv, human, yaml, ndjson")
			}

			// Validate --fields and --exclude are mutually exclusive.
			if viper.GetString("fields") != "" && viper.GetString("exclude") != "" {
				return NewCLIError(ErrCodeValidationError,
					"--fields and --exclude are mutually exclusive",
					"Use either --fields to include specific fields or --exclude to remove them")
			}

			return nil
		},
	}

	rootCmd.SetVersionTemplate("jc v{{.Version}}\n")

	// Provide helpful suggestions when unknown flags are used.
	rootCmd.SetFlagErrorFunc(flagErrorWithSuggestion)

	// Register --version with -V shorthand before Cobra's auto-creation.
	// Cobra skips its own version flag when Lookup("version") already exists.
	// We use -V (uppercase) because -v is taken by --verbose.
	rootCmd.Flags().BoolP("version", "V", false, "Print version information")

	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newCompletionCmd())
	rootCmd.AddCommand(newAuthCmd())
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(newUsersCmd())
	rootCmd.AddCommand(newDevicesCmd())
	rootCmd.AddCommand(newGroupsCmd())
	rootCmd.AddCommand(newCommandsCmd())
	rootCmd.AddCommand(newPoliciesCmd())
	rootCmd.AddCommand(newAppsCmd())
	rootCmd.AddCommand(newGraphCmd())
	rootCmd.AddCommand(newAdminsCmd())
	rootCmd.AddCommand(newBulkCmd())
	rootCmd.AddCommand(newInsightsCmd())
	rootCmd.AddCommand(newRecipeCmd())
	rootCmd.AddCommand(newMcpCmd())
	rootCmd.AddCommand(newSchemaCmd())
	rootCmd.AddCommand(newExplainCmd())
	rootCmd.AddCommand(newAskCmd())

	// Persistent flags (global)
	rootCmd.PersistentFlags().StringP("output", "o", "json", "Output format: json, table, csv, human, yaml, ndjson")
	rootCmd.PersistentFlags().BoolP("table", "t", false, "Shorthand for --output table")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose HTTP logging")
	rootCmd.PersistentFlags().Bool("debug", false, "Enable debug logging")
	rootCmd.PersistentFlags().BoolP("quiet", "q", false, "Suppress output, exit code only")
	rootCmd.PersistentFlags().BoolP("force", "f", false, "Skip confirmation prompts")
	rootCmd.PersistentFlags().Bool("non-interactive", false, "Disable all interactive prompts")
	rootCmd.PersistentFlags().Bool("no-cache", false, "Bypass name-to-ID cache")
	rootCmd.PersistentFlags().Bool("no-color", false, "Disable color output")
	rootCmd.PersistentFlags().Bool("plan", false, "Preview changes without executing")
	rootCmd.PersistentFlags().String("org", "", "Override active profile for this command")
	rootCmd.PersistentFlags().String("api-key", "", "Override API key for this command")
	rootCmd.PersistentFlags().Bool("ids", false, "Output one ID per line (for piping)")
	rootCmd.PersistentFlags().String("fields", "", "Comma-separated list of fields to include (e.g. 'username,email,department')")
	rootCmd.PersistentFlags().String("exclude", "", "Comma-separated list of fields to exclude (e.g. 'password_date,totp_enabled')")
	rootCmd.PersistentFlags().Bool("all", false, "Include all available fields in output")
	rootCmd.PersistentFlags().String("query", "", "JMESPath expression to filter/transform output (e.g. \"[?department=='Engineering'].{name:username,email:email}\")")

	// Register flag completion functions for flags with a fixed set of values.
	_ = rootCmd.RegisterFlagCompletionFunc("output", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return validOutputFormats, cobra.ShellCompDirectiveNoFileComp
	})
	_ = rootCmd.RegisterFlagCompletionFunc("org", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return config.ProfileNames(), cobra.ShellCompDirectiveNoFileComp
	})

	// Bind flags to Viper so the priority chain works:
	// flags > env vars > config file > built-in defaults.
	//
	// Note: "output" flag maps to "defaults.output" in Viper. We bind
	// both the flag key and the nested config key so they stay in sync.
	_ = viper.BindPFlag("defaults.output", rootCmd.PersistentFlags().Lookup("output"))
	_ = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
	_ = viper.BindPFlag("quiet", rootCmd.PersistentFlags().Lookup("quiet"))
	_ = viper.BindPFlag("force", rootCmd.PersistentFlags().Lookup("force"))
	_ = viper.BindPFlag("non-interactive", rootCmd.PersistentFlags().Lookup("non-interactive"))
	_ = viper.BindPFlag("no-cache", rootCmd.PersistentFlags().Lookup("no-cache"))
	_ = viper.BindPFlag("no-color", rootCmd.PersistentFlags().Lookup("no-color"))
	_ = viper.BindPFlag("plan", rootCmd.PersistentFlags().Lookup("plan"))
	_ = viper.BindPFlag("org", rootCmd.PersistentFlags().Lookup("org"))
	_ = viper.BindPFlag("api_key", rootCmd.PersistentFlags().Lookup("api-key"))
	_ = viper.BindPFlag("ids", rootCmd.PersistentFlags().Lookup("ids"))
	_ = viper.BindPFlag("fields", rootCmd.PersistentFlags().Lookup("fields"))
	_ = viper.BindPFlag("exclude", rootCmd.PersistentFlags().Lookup("exclude"))
	_ = viper.BindPFlag("all", rootCmd.PersistentFlags().Lookup("all"))
	_ = viper.BindPFlag("query", rootCmd.PersistentFlags().Lookup("query"))

	return rootCmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the jc version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "jc v%s\n", version.Number)
		},
	}
}

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for jc.

To load completions:

Bash:
  $ source <(jc completion bash)
  # To install permanently, add to your .bashrc:
  $ echo 'source <(jc completion bash)' >> ~/.bashrc

Zsh:
  $ source <(jc completion zsh)
  # To install permanently:
  $ jc completion zsh > "${fpath[1]}/_jc"

Fish:
  $ jc completion fish | source
  # To install permanently:
  $ jc completion fish > ~/.config/fish/completions/jc.fish
`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	}
	return cmd
}

// flagErrorWithSuggestion wraps Cobra's flag parsing errors to suggest
// similar valid flags when an unknown flag is used.
func flagErrorWithSuggestion(cmd *cobra.Command, err error) error {
	msg := err.Error()
	// Extract the unknown flag name from error messages like:
	// "unknown flag: --foo" or "unknown shorthand flag: 'x' in -x"
	var unknown string
	if strings.HasPrefix(msg, "unknown flag: --") {
		unknown = strings.TrimPrefix(msg, "unknown flag: --")
	} else if strings.Contains(msg, "unknown shorthand flag") {
		// Can't suggest for single-char shorthand misses.
		return err
	} else {
		return err
	}

	// Collect all persistent flag names from the command and its parents.
	var candidates []string
	cmd.Root().PersistentFlags().VisitAll(func(f *pflag.Flag) {
		candidates = append(candidates, f.Name)
	})
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		candidates = append(candidates, f.Name)
	})

	// Find closest matches (edit distance <= 3).
	var suggestions []string
	for _, c := range candidates {
		if levenshtein(unknown, c) <= 3 {
			suggestions = append(suggestions, "--"+c)
		}
	}

	if len(suggestions) > 0 {
		return fmt.Errorf("%s\n\nDid you mean one of these?\n\t%s", msg, strings.Join(suggestions, "\n\t"))
	}
	return err
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)

	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

// builtinCommands lists all built-in command names. Built-in commands
// always take precedence over user-defined aliases.
var builtinCommands = map[string]bool{
	"version": true, "completion": true, "auth": true, "config": true,
	"users": true, "devices": true, "groups": true, "commands": true,
	"policies": true, "apps": true, "graph": true, "admins": true,
	"bulk": true, "insights": true, "recipe": true, "mcp": true,
	"schema": true, "explain": true, "ask": true, "help": true,
	// Short aliases for resource commands.
	"u": true, "d": true, "g": true, "i": true,
}

// expandAliases checks if the first positional argument matches a
// user-defined alias and expands it. Returns the (possibly expanded)
// args and a warning string if the alias shadows a built-in command.
func expandAliases(args []string) ([]string, string) {
	if len(args) == 0 {
		return args, ""
	}

	// Find the first positional arg (skip flags).
	firstArgIdx := -1
	for i, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			firstArgIdx = i
			break
		}
		// Skip flag values (e.g., --output json).
		if (arg == "--output" || arg == "-o" || arg == "--org" || arg == "--api-key" ||
			arg == "--fields" || arg == "--exclude") && i+1 < len(args) {
			continue
		}
	}
	if firstArgIdx < 0 {
		return args, ""
	}

	name := args[firstArgIdx]
	aliases := config.Aliases()
	expansion, ok := aliases[name]
	if !ok {
		return args, ""
	}

	// Warn if the alias shadows a built-in command.
	if builtinCommands[name] {
		return args, fmt.Sprintf("Warning: alias %q conflicts with built-in command and will be ignored\n", name)
	}

	// Expand: replace the alias name with the aliased command tokens.
	// Use the recipe package's ParseCommandArgs for proper quote handling.
	tokens := strings.Fields(expansion)

	expanded := make([]string, 0, len(args)-1+len(tokens))
	expanded = append(expanded, args[:firstArgIdx]...)
	expanded = append(expanded, tokens...)
	expanded = append(expanded, args[firstArgIdx+1:]...)
	return expanded, ""
}

// Execute initializes config and runs the root command.
func Execute() {
	if err := config.Init(); err != nil {
		cliErr := WrapCLIError(ErrCodeConfigError, err.Error(),
			"Check your config file at ~/.config/jc/config.yaml", err)
		writeError(os.Stderr, cliErr, "json")
		os.Exit(ExitGeneral)
	}

	// Expand user-defined aliases before Cobra parses commands.
	osArgs := os.Args[1:]
	expanded, warning := expandAliases(osArgs)
	if warning != "" {
		fmt.Fprint(os.Stderr, warning)
	}

	rootCmd := NewRootCmd()
	rootCmd.SetArgs(expanded)
	if err := rootCmd.Execute(); err != nil {
		// Check for ExitError with a specific exit code (legacy path).
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}

		// Convert to structured CLIError and render.
		cliErr := ToCLIError(err)
		format := viper.GetString("defaults.output")
		writeError(os.Stderr, cliErr, format)
		os.Exit(cliErr.ExitCode())
	}
}

// writeError renders a CLIError to w. When the output format is JSON,
// the error is written as structured JSON. Otherwise, it's written as
// human-readable text.
func writeError(w io.Writer, cliErr *CLIError, format string) {
	if format == "json" {
		_ = cliErr.WriteJSON(w)
	} else {
		cliErr.WritePlain(w)
	}
}
