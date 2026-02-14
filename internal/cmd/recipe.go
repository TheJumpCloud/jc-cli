package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/recipe"
)

// recipeDefaultFields is the default field subset shown for recipe list output.
var recipeDefaultFields = []string{"name", "description", "version", "tags"}

// recipeInputReader is used by recipe create for interactive input.
// Overridable in tests.
var recipeInputReader InputReader

func getRecipeInputReader() InputReader {
	if recipeInputReader != nil {
		return recipeInputReader
	}
	return defaultInput
}

// recipeHTTPGet fetches a URL. Overridable in tests.
var recipeHTTPGet = http.Get

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
	cmd.AddCommand(newRecipeCreateCmd())
	cmd.AddCommand(newRecipeImportCmd())
	cmd.AddCommand(newRecipeExportCmd())

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

// --- recipe create ---

func newRecipeCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create",
		Short: "Interactively create a new recipe",
		Long: `Interactively build a new recipe by prompting for name, description,
parameters, and steps. The resulting YAML is saved to ~/.config/jc/recipes/.`,
		RunE: runRecipeCreate,
	}
}

func runRecipeCreate(cmd *cobra.Command, args []string) error {
	input := getRecipeInputReader()
	w := cmd.ErrOrStderr()

	// Prompt for name.
	fmt.Fprint(w, "Recipe name: ")
	name, err := input.ReadLine()
	if err != nil {
		return fmt.Errorf("reading name: %w", err)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("recipe name is required")
	}

	// Prompt for description.
	fmt.Fprint(w, "Description: ")
	desc, err := input.ReadLine()
	if err != nil {
		return fmt.Errorf("reading description: %w", err)
	}

	// Build the recipe.
	r := &recipe.Recipe{
		Name:        name,
		Description: strings.TrimSpace(desc),
		Version:     "1.0",
	}

	// Prompt for parameters (loop until empty name).
	fmt.Fprintln(w, "\nAdd parameters (press Enter with empty name to finish):")
	for {
		fmt.Fprint(w, "  Parameter name: ")
		pName, err := input.ReadLine()
		if err != nil {
			return fmt.Errorf("reading parameter name: %w", err)
		}
		pName = strings.TrimSpace(pName)
		if pName == "" {
			break
		}

		fmt.Fprint(w, "  Description: ")
		pDesc, err := input.ReadLine()
		if err != nil {
			return fmt.Errorf("reading parameter description: %w", err)
		}

		fmt.Fprint(w, "  Required (y/n): ")
		pReq, err := input.ReadLine()
		if err != nil {
			return fmt.Errorf("reading parameter required: %w", err)
		}

		p := recipe.Parameter{
			Name:        pName,
			Description: strings.TrimSpace(pDesc),
			Required:    strings.TrimSpace(strings.ToLower(pReq)) == "y",
			Type:        "string",
		}
		r.Parameters = append(r.Parameters, p)
	}

	// Prompt for steps (loop until empty name).
	fmt.Fprintln(w, "\nAdd steps (press Enter with empty name to finish):")
	for {
		fmt.Fprint(w, "  Step name: ")
		sName, err := input.ReadLine()
		if err != nil {
			return fmt.Errorf("reading step name: %w", err)
		}
		sName = strings.TrimSpace(sName)
		if sName == "" {
			break
		}

		fmt.Fprint(w, "  Command: ")
		sCmd, err := input.ReadLine()
		if err != nil {
			return fmt.Errorf("reading step command: %w", err)
		}

		s := recipe.Step{
			Name:    sName,
			Command: strings.TrimSpace(sCmd),
		}
		r.Steps = append(r.Steps, s)
	}

	if len(r.Steps) == 0 {
		return fmt.Errorf("recipe must have at least one step")
	}

	// Validate the recipe.
	if err := r.Validate(); err != nil {
		return err
	}

	// Marshal to YAML.
	data, err := recipe.MarshalYAML(r)
	if err != nil {
		return fmt.Errorf("marshaling recipe: %w", err)
	}

	// Ensure recipes directory exists.
	dir := recipe.RecipesDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating recipes directory: %w", err)
	}

	// Write the recipe file.
	outPath := filepath.Join(dir, name+".yaml")

	// Check for existing file and prompt for overwrite.
	if _, err := os.Stat(outPath); err == nil {
		fmt.Fprintf(w, "\nRecipe %q already exists. Overwrite? [y/N]: ", name)
		confirm, err := input.ReadLine()
		if err != nil {
			return fmt.Errorf("reading confirmation: %w", err)
		}
		if strings.TrimSpace(strings.ToLower(confirm)) != "y" {
			fmt.Fprintln(w, "Cancelled.")
			return nil
		}
	}

	if err := os.WriteFile(outPath, data, 0600); err != nil {
		return fmt.Errorf("writing recipe: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Recipe saved to %s\n", outPath)
	return nil
}

// --- recipe import ---

func newRecipeImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import <url-or-path>",
		Short: "Import a recipe from a URL or local file",
		Long: `Import a recipe from a URL or local file path. The recipe is validated
before saving to ~/.config/jc/recipes/.

Examples:
  jc recipe import https://example.com/recipes/my-recipe.yaml
  jc recipe import /path/to/recipe.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: runRecipeImport,
	}
}

func runRecipeImport(cmd *cobra.Command, args []string) error {
	source := args[0]
	w := cmd.ErrOrStderr()

	var data []byte
	var err error

	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		// Fetch from URL.
		resp, err := recipeHTTPGet(source)
		if err != nil {
			return fmt.Errorf("fetching recipe: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("fetching recipe: HTTP %d", resp.StatusCode)
		}

		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("reading response: %w", err)
		}
	} else {
		// Read from local file.
		data, err = os.ReadFile(source)
		if err != nil {
			return fmt.Errorf("reading recipe file: %w", err)
		}
	}

	// Validate the recipe.
	r, err := recipe.Parse(data)
	if err != nil {
		return fmt.Errorf("invalid recipe: %w", err)
	}

	// For URL imports, show recipe details and prompt for confirmation.
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		fmt.Fprintf(w, "Recipe: %s\n", r.Name)
		if r.Description != "" {
			fmt.Fprintf(w, "Description: %s\n", r.Description)
		}
		fmt.Fprintf(w, "Steps: %d\n", len(r.Steps))
		for i, s := range r.Steps {
			fmt.Fprintf(w, "  [%d] %s: %s\n", i+1, s.Name, s.Command)
		}

		fmt.Fprint(w, "\nImport this recipe? [y/N]: ")
		input := getRecipeInputReader()
		confirm, err := input.ReadLine()
		if err != nil {
			return fmt.Errorf("reading confirmation: %w", err)
		}
		if strings.TrimSpace(strings.ToLower(confirm)) != "y" {
			fmt.Fprintln(w, "Import cancelled.")
			return nil
		}
	}

	// Ensure recipes directory exists.
	dir := recipe.RecipesDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating recipes directory: %w", err)
	}

	// Check for duplicate name.
	outPath := filepath.Join(dir, r.Name+".yaml")
	if _, err := os.Stat(outPath); err == nil {
		fmt.Fprintf(w, "Recipe %q already exists. Overwrite? [y/N]: ", r.Name)
		input := getRecipeInputReader()
		confirm, err := input.ReadLine()
		if err != nil {
			return fmt.Errorf("reading confirmation: %w", err)
		}
		if strings.TrimSpace(strings.ToLower(confirm)) != "y" {
			fmt.Fprintln(w, "Import cancelled.")
			return nil
		}
	}

	if err := os.WriteFile(outPath, data, 0600); err != nil {
		return fmt.Errorf("writing recipe: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Recipe %q imported to %s\n", r.Name, outPath)
	return nil
}

// --- recipe export ---

func newRecipeExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <name>",
		Short: "Export a recipe as YAML",
		Long: `Export a recipe as YAML to stdout or to a file.

Examples:
  jc recipe export onboard-user
  jc recipe export onboard-user --file onboard.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: runRecipeExport,
	}

	cmd.Flags().String("file", "", "Write recipe to file instead of stdout")

	return cmd
}

func runRecipeExport(cmd *cobra.Command, args []string) error {
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

	data, err := recipe.MarshalYAML(r)
	if err != nil {
		return fmt.Errorf("marshaling recipe: %w", err)
	}

	outFile, _ := cmd.Flags().GetString("file")
	if outFile != "" {
		if err := os.WriteFile(outFile, data, 0600); err != nil {
			return fmt.Errorf("writing file: %w", err)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Recipe %q exported to %s\n", name, outFile)
		return nil
	}

	// Write YAML to stdout.
	_, err = cmd.OutOrStdout().Write(data)
	return err
}
