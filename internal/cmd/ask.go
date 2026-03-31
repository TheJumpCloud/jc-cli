package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/ask"
	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/recipe"
)

// askConfirmReader is used by the ask command for confirmation prompts.
// Overridable in tests.
var askConfirmReader *bufio.Reader

func getAskConfirmReader() *bufio.Reader {
	if askConfirmReader != nil {
		return askConfirmReader
	}
	return bufio.NewReader(os.Stdin)
}

// newAskClient creates an LLM client from config. Overridable in tests.
var newAskClient = func() (ask.Client, error) {
	provider := ask.Provider(viper.GetString("ask.provider"))
	apiKey := resolveAskAPIKey()
	model := viper.GetString("ask.model")
	url := viper.GetString("ask.url")
	return ask.NewClient(provider, apiKey, model, url)
}

// resolveAskAPIKey returns the LLM API key from env or config.
func resolveAskAPIKey() string {
	// JC_ASK_API_KEY env var takes priority.
	if key := os.Getenv("JC_ASK_API_KEY"); key != "" {
		return key
	}
	return viper.GetString("ask.api_key")
}

// askHistoryFile returns the path to the ask history log.
func askHistoryFile() string {
	return filepath.Join(config.ConfigDir(), "ask-history.log")
}

func newAskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ask <question...>",
		Short: "Translate natural language to jc commands",
		Long: `Ask translates natural language queries into jc CLI commands using an LLM.

The generated commands are shown for review before execution. You can
approve, reject, or modify them before they run.

The LLM never has direct API access — it only generates command strings
that are validated against the CLI schema before execution.

Configuration:
  jc config set ask.provider anthropic    # or: openai, ollama, disabled
  jc config set ask.api_key <your-key>    # LLM provider API key
  jc config set ask.model <model-name>    # optional model override
  jc config set ask.max_commands 10       # max commands per query (default 10)

Environment:
  JC_ASK_API_KEY    Override LLM API key

Examples:
  jc ask "which users haven't logged in for 90 days?"
  jc ask "show me all macOS devices"
  jc ask "find SSO auth failures in the last 24 hours"
  jc ask "list all user groups and their member count"`,
		Args: cobra.MinimumNArgs(1),
		RunE: runAsk,
	}
	return cmd
}

// askResult describes a proposed command and its execution outcome.
type askResult struct {
	Command  string `json:"command"`
	Status   string `json:"status"` // proposed, approved, rejected, executed, failed
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
}

func runAsk(cmd *cobra.Command, args []string) error {
	query := strings.Join(args, " ")
	if strings.TrimSpace(query) == "" {
		return NewCLIError(ErrCodeUsageError, "empty query", "Provide a question, e.g.: jc ask \"list suspended users\"")
	}

	// Check if non-interactive mode — no confirmation possible.
	nonInteractive := viper.GetBool("non-interactive")
	force := viper.GetBool("force")
	confirmBeforeExecute := true
	if force || nonInteractive {
		confirmBeforeExecute = false
	} else if isStdinPiped() {
		// Can't prompt on piped stdin — default to executing without confirmation
		// (ask commands are non-destructive read operations, unlike delete commands).
		confirmBeforeExecute = false
	}

	maxCommands := viper.GetInt("ask.max_commands")
	if maxCommands <= 0 {
		maxCommands = 10
	}

	// Create LLM client.
	client, err := newAskClient()
	if err != nil {
		return NewCLIError(ErrCodeConfigError, err.Error(),
			"Configure LLM provider: jc config set ask.provider anthropic")
	}

	// Translate the query.
	fmt.Fprintf(cmd.ErrOrStderr(), "Translating: %s\n", query)
	result, err := client.Translate(query, maxCommands)
	if err != nil {
		return NewCLIError(ErrCodeGeneral, fmt.Sprintf("LLM translation failed: %v", err),
			"Check your API key and provider configuration")
	}

	if len(result.Commands) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "No commands generated.")
		return nil
	}

	// Log the proposed commands.
	logAskHistory(query, result.Commands)

	// Display proposed commands.
	fmt.Fprintln(cmd.ErrOrStderr())
	fmt.Fprintln(cmd.ErrOrStderr(), "Proposed commands:")
	for i, c := range result.Commands {
		fmt.Fprintf(cmd.ErrOrStderr(), "  [%d] jc %s\n", i+1, c)
	}
	if result.Explanation != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "\n%s\n", result.Explanation)
	}
	fmt.Fprintln(cmd.ErrOrStderr())

	// JSON output mode: return structured result without executing.
	outputFlag := cmd.Root().PersistentFlags().Lookup("output")
	if outputFlag != nil && outputFlag.Changed && outputFlag.Value.String() == "json" {
		results := make([]askResult, len(result.Commands))
		for i, c := range result.Commands {
			results[i] = askResult{Command: c, Status: "proposed"}
		}
		out, _ := json.MarshalIndent(map[string]interface{}{
			"query":       query,
			"commands":    results,
			"explanation": result.Explanation,
		}, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	// Confirmation prompt.
	if confirmBeforeExecute {
		fmt.Fprintf(cmd.ErrOrStderr(), "Execute these commands? [y/N] ")
		reader := getAskConfirmReader()
		line, _ := reader.ReadString('\n')
		answer := strings.TrimSpace(strings.ToLower(line))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(cmd.ErrOrStderr(), "Aborted.")
			return nil
		}
	}

	// Execute the commands via the recipe dispatcher.
	getRootCmd := newRootCmdForRecipe
	if getRootCmd == nil {
		getRootCmd = func() recipe.CobraCommand { return NewRootCmd() }
	}
	dispatcher := recipe.NewDispatcher(getRootCmd)

	var results []askResult
	for i, cmdStr := range result.Commands {
		fmt.Fprintf(cmd.ErrOrStderr(), "[%d/%d] jc %s... ", i+1, len(result.Commands), cmdStr)

		cmdArgs := recipe.ParseCommandArgs(cmdStr)
		output, execErr := dispatcher(cmdArgs)

		r := askResult{Command: cmdStr}
		if execErr != nil {
			r.Status = "failed"
			r.Error = execErr.Error()
			fmt.Fprintf(cmd.ErrOrStderr(), "failed: %s\n", execErr.Error())
		} else {
			r.Status = "executed"
			r.Output = output
			fmt.Fprintln(cmd.ErrOrStderr(), "done")
			// Print command output to stdout.
			if output != "" {
				fmt.Fprint(cmd.OutOrStdout(), output)
			}
		}
		results = append(results, r)
	}

	// Summary.
	succeeded := 0
	failed := 0
	for _, r := range results {
		if r.Status == "executed" {
			succeeded++
		} else {
			failed++
		}
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "\n── %d executed, %d failed ──\n", succeeded, failed)
	return nil
}

// logAskHistory appends proposed commands to the history log.
func logAskHistory(query string, commands []string) {
	f, err := os.OpenFile(askHistoryFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return // non-fatal
	}
	defer f.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(f, "[%s] Query: %s\n", now, query)
	for _, c := range commands {
		fmt.Fprintf(f, "[%s]   jc %s\n", now, c)
	}
}
