package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/config"
)

// Version is set at build time via -ldflags.
var Version = "dev"

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "jc",
		Short: "JumpCloud CLI — manage your JumpCloud organization from the terminal",
		Long: `jc is a modern, LLM-friendly CLI for JumpCloud.

It covers the full JumpCloud API surface (v1, v2, Directory Insights) with
built-in MCP server support, a recipe system, plan mode, and conversational
interface.`,
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.SetVersionTemplate("jc v{{.Version}}\n")

	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newCompletionCmd())

	// Persistent flags (global)
	rootCmd.PersistentFlags().StringP("output", "o", "json", "Output format: json, table, csv, human, yaml, ndjson")
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

	// Bind flags to Viper
	_ = viper.BindPFlag("output", rootCmd.PersistentFlags().Lookup("output"))
	_ = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
	_ = viper.BindPFlag("quiet", rootCmd.PersistentFlags().Lookup("quiet"))

	return rootCmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the jc version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "jc v%s\n", Version)
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

// Execute initializes config and runs the root command.
func Execute() {
	if err := config.Init(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	rootCmd := NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
