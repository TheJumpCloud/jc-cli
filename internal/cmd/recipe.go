package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/recipe"
)

// recipeDefaultFields is the default field subset shown for recipe list output.
var recipeDefaultFields = []string{"name", "description", "version", "tags"}

// newRootCmdForRecipe creates a fresh root command for recipe step dispatch.
// This is a package-level var so tests can override it.
// Default is nil; when nil, falls back to NewRootCmd() at runtime to avoid
// initialization cycle (NewRootCmd → newRecipeCmd → newRootCmdForRecipe).
var newRootCmdForRecipe func() recipe.CobraCommand

func newRecipeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recipe",
		Short: "Manage and run recipes (multi-step workflows)",
		Long: `Recipes are YAML-defined multi-step workflows that automate common
JumpCloud operations like user onboarding, offboarding, and auditing.

Built-in recipes are bundled with jc. User-defined recipes can be
placed in ~/.config/jc/recipes/.`,
	}

	cmd.AddCommand(newRecipeListCmd())
	cmd.AddCommand(newRecipeShowCmd())
	cmd.AddCommand(newRecipeRunCmd())
	cmd.AddCommand(newRecipeValidateCmd())

	return cmd
}

func newRecipeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all available recipes (built-in + user-defined)",
		RunE:  runRecipeList,
	}
}

func runRecipeList(cmd *cobra.Command, args []string) error {
	recipes, err := recipe.LoadAll()
	if err != nil {
		return err
	}

	// Convert recipes to json.RawMessage for the output engine.
	var data []json.RawMessage
	for _, r := range recipes {
		// Build a summary object (not the full recipe with all steps).
		summary := map[string]interface{}{
			"name":        r.Name,
			"description": r.Description,
		}
		if r.Version != "" {
			summary["version"] = r.Version
		}
		if r.Author != "" {
			summary["author"] = r.Author
		}
		if len(r.Tags) > 0 {
			summary["tags"] = strings.Join(r.Tags, ", ")
		}
		if len(r.Parameters) > 0 {
			summary["parameters"] = len(r.Parameters)
		}
		summary["steps"] = len(r.Steps)

		raw, err := json.Marshal(summary)
		if err != nil {
			return err
		}
		data = append(data, raw)
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = recipeDefaultFields
	if err := output.WriteList(cmd.OutOrStdout(), data, opts); err != nil {
		return err
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "── %d recipes ──\n", len(data))
	return nil
}

func newRecipeShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Display full recipe details: steps, parameters, tags",
		Args:  cobra.ExactArgs(1),
		RunE:  runRecipeShow,
	}
}

func runRecipeShow(cmd *cobra.Command, args []string) error {
	name := args[0]

	recipes, err := recipe.LoadAll()
	if err != nil {
		return err
	}

	r := recipe.FindByName(recipes, name)
	if r == nil {
		available := make([]string, 0, len(recipes))
		for _, rec := range recipes {
			available = append(available, rec.Name)
		}
		return fmt.Errorf("recipe %q not found. Available recipes: %s", name, strings.Join(available, ", "))
	}

	// Marshal the full recipe struct for display.
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), raw, opts)
}

func newRecipeRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <name>",
		Short: "Execute a recipe with parameters",
		Long: `Execute a recipe with parameters. Parameters are passed as --param key=value.

Examples:
  jc recipe run onboard-user --param username=jdoe --param email=jdoe@acme.com
  jc recipe run onboard-user --param username=jdoe --plan`,
		Args: cobra.ExactArgs(1),
		RunE: runRecipeRun,
	}

	cmd.Flags().StringArray("param", nil, "Recipe parameter as key=value (repeatable)")

	return cmd
}

func runRecipeRun(cmd *cobra.Command, args []string) error {
	name := args[0]

	recipes, err := recipe.LoadAll()
	if err != nil {
		return err
	}

	r := recipe.FindByName(recipes, name)
	if r == nil {
		available := make([]string, 0, len(recipes))
		for _, rec := range recipes {
			available = append(available, rec.Name)
		}
		return fmt.Errorf("recipe %q not found. Available recipes: %s", name, strings.Join(available, ", "))
	}

	// Parse --param flags into a map.
	params, err := parseParamFlags(cmd)
	if err != nil {
		return err
	}

	// Plan mode: preview steps without executing.
	if viper.GetBool("plan") {
		plans, err := r.Plan(params)
		if err != nil {
			return err
		}

		// Check if user explicitly requested JSON output.
		outputFlag := cmd.Root().PersistentFlags().Lookup("output")
		if outputFlag != nil && outputFlag.Changed && viper.GetString("defaults.output") == "json" {
			if err := recipe.RenderPlanJSON(cmd.OutOrStdout(), plans); err != nil {
				return err
			}
		} else {
			recipe.RenderPlanHuman(cmd.ErrOrStderr(), r.Name, plans)
		}

		return &ExitError{Code: plan.ExitCodePlan}
	}

	// Execute the recipe.
	rootCmdFn := newRootCmdForRecipe
	if rootCmdFn == nil {
		rootCmdFn = func() recipe.CobraCommand { return NewRootCmd() }
	}
	dispatcher := recipe.NewDispatcher(rootCmdFn)
	result, err := r.Execute(dispatcher, params, cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	// Show the completion message to stderr.
	if result.Message != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), result.Message)
	}

	// If there was a failure, show an error.
	if result.Status == "failed" {
		// Find the failed step for a useful error message.
		for _, s := range result.Steps {
			if s.Status == "failed" {
				return fmt.Errorf("recipe %q failed at step %q: %s", name, s.Name, s.Error)
			}
		}
		return fmt.Errorf("recipe %q failed", name)
	}

	return nil
}

// parseParamFlags parses --param key=value flags into a string map.
func parseParamFlags(cmd *cobra.Command) (map[string]string, error) {
	rawParams, err := cmd.Flags().GetStringArray("param")
	if err != nil {
		return nil, err
	}

	params := make(map[string]string)
	for _, p := range rawParams {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return nil, fmt.Errorf("invalid parameter format %q: expected key=value", p)
		}
		params[k] = v
	}
	return params, nil
}

func newRecipeValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <file.yaml>",
		Short: "Validate a recipe YAML file for syntax and semantic errors",
		Args:  cobra.ExactArgs(1),
		RunE:  runRecipeValidate,
	}
}

func runRecipeValidate(cmd *cobra.Command, args []string) error {
	path := args[0]

	_, err := recipe.ParseFile(path)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Recipe %s is valid.\n", path)
	return nil
}
