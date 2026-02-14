package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"

	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/recipe"
)

func setupRecipeTest(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	viper.Reset()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_ORG_ID", "")
	t.Setenv("JC_PROFILE", "")
	t.Setenv("JC_OUTPUT", "")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte("active_profile: default\nprofiles:\n  default:\n    api_key: test-key-1234\n"), 0600)

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}
}

// writeTempRecipe writes a recipe YAML to a temporary file and returns its path.
func writeTempRecipe(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writing temp recipe: %v", err)
	}
	return path
}

// overrideRecipesDir replaces the RecipesDir function to point to a temp directory.
func overrideRecipesDir(t *testing.T, dir string) {
	t.Helper()
	orig := recipe.RecipesDir
	recipe.RecipesDir = func() string { return dir }
	t.Cleanup(func() { recipe.RecipesDir = orig })
}

// overrideRootCmdForRecipe replaces the root cmd factory for recipe dispatch in tests.
func overrideRootCmdForRecipe(t *testing.T, fn func() recipe.CobraCommand) {
	t.Helper()
	orig := newRootCmdForRecipe
	newRootCmdForRecipe = fn
	t.Cleanup(func() { newRootCmdForRecipe = orig })
}

const validRecipeYAML = `name: test-recipe
description: A test recipe
author: Test
version: "1.0"
tags: [test, automation]
parameters:
  - name: greeting
    description: What to say
    required: true
    type: string
  - name: target
    description: Who to greet
    default: world
steps:
  - name: say-hello
    command: 'version'
on_success:
  message: "Said {{ .greeting }} to {{ .target }}"
`

const multiStepRecipeYAML = `name: multi-step
description: Multi-step recipe
steps:
  - name: step-one
    command: 'version'
  - name: step-two
    command: 'version'
  - name: step-three
    command: 'version'
`

// --- recipe list tests ---

func TestRecipeList_JSON(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Should parse as JSON array.
	var results []map[string]any
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatalf("parsing output: %v", err)
	}

	// Should have at least the built-in recipes (11).
	if len(results) < 10 {
		t.Errorf("expected at least 10 built-in recipes, got %d", len(results))
	}

	// Each recipe should have name and description.
	for _, r := range results {
		if r["name"] == nil || r["name"] == "" {
			t.Errorf("recipe missing name: %v", r)
		}
		if r["description"] == nil || r["description"] == "" {
			t.Errorf("recipe missing description: %v", r)
		}
	}
}

func TestRecipeList_Table(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "list", "-t"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	output := out.String()
	// Table should have column headers.
	if !strings.Contains(output, "NAME") {
		t.Errorf("table should have NAME header, got: %q", output)
	}
	if !strings.Contains(output, "DESCRIPTION") {
		t.Errorf("table should have DESCRIPTION header, got: %q", output)
	}
	// Should contain at least one built-in recipe name.
	if !strings.Contains(output, "onboard-user") {
		t.Errorf("table should contain onboard-user recipe, got: %q", output)
	}
}

func TestRecipeList_Footer(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	errOut := &bytes.Buffer{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"recipe", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errOut.String()
	if !strings.Contains(stderr, "recipes") {
		t.Errorf("footer should show recipe count, got stderr: %q", stderr)
	}
}

func TestRecipeList_IncludesUserDefined(t *testing.T) {
	setupRecipeTest(t)

	// Create a user-defined recipe directory with a custom recipe.
	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)

	customRecipe := `name: my-custom-recipe
description: A custom workflow
steps:
  - name: step1
    command: 'version'
`
	_ = os.WriteFile(filepath.Join(recipeDir, "custom.yaml"), []byte(customRecipe), 0600)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var results []map[string]any
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatalf("parsing output: %v", err)
	}

	found := false
	for _, r := range results {
		if r["name"] == "my-custom-recipe" {
			found = true
			break
		}
	}
	if !found {
		t.Error("user-defined recipe 'my-custom-recipe' should appear in list")
	}
}

func TestRecipeList_DefaultFields(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var results []map[string]any
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatalf("parsing output: %v", err)
	}

	// JSON output should include the summary fields.
	for _, r := range results {
		if _, ok := r["steps"]; !ok {
			t.Errorf("recipe list entry should include steps count: %v", r)
		}
	}
}

// --- recipe show tests ---

func TestRecipeShow_JSON(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "show", "onboard-user"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("parsing output: %v", err)
	}

	if result["name"] != "onboard-user" {
		t.Errorf("name = %q, want onboard-user", result["name"])
	}
	if result["description"] == nil || result["description"] == "" {
		t.Error("should have description")
	}

	// Should include full recipe details.
	steps, ok := result["steps"].([]any)
	if !ok || len(steps) == 0 {
		t.Error("should include steps array with entries")
	}

	params, ok := result["parameters"].([]any)
	if !ok || len(params) == 0 {
		t.Error("should include parameters array with entries")
	}
}

func TestRecipeShow_Table(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "show", "onboard-user", "-t"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "onboard-user") {
		t.Errorf("table should show recipe name, got: %q", output)
	}
}

func TestRecipeShow_NotFound(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "show", "nonexistent-recipe"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent recipe")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should say not found, got: %q", err.Error())
	}
	// Error should list available recipes.
	if !strings.Contains(err.Error(), "onboard-user") {
		t.Errorf("error should list available recipes, got: %q", err.Error())
	}
}

func TestRecipeShow_MissingArg(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "show"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

// --- recipe run tests ---

func TestRecipeRun_Success(t *testing.T) {
	setupRecipeTest(t)

	// Create a recipe dir with a simple recipe that uses 'version' command.
	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)

	recipeContent := `name: test-run
description: Test run recipe
steps:
  - name: check-version
    command: 'version'
on_success:
  message: "Recipe completed successfully"
`
	_ = os.WriteFile(filepath.Join(recipeDir, "test-run.yaml"), []byte(recipeContent), 0600)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"recipe", "run", "test-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errOut.String()
	// Should show progress.
	if !strings.Contains(stderr, "[1/1]") {
		t.Errorf("should show step progress, got stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "check-version") {
		t.Errorf("should show step name, got stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "done") {
		t.Errorf("should show done, got stderr: %q", stderr)
	}
	// Should show success message.
	if !strings.Contains(stderr, "Recipe completed successfully") {
		t.Errorf("should show on_success message, got stderr: %q", stderr)
	}
}

func TestRecipeRun_WithParams(t *testing.T) {
	setupRecipeTest(t)

	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)
	_ = os.WriteFile(filepath.Join(recipeDir, "test.yaml"), []byte(validRecipeYAML), 0600)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"recipe", "run", "test-recipe", "--param", "greeting=hello"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errOut.String()
	// Success message should have rendered template with params.
	if !strings.Contains(stderr, "Said hello to world") {
		t.Errorf("should show rendered success message, got stderr: %q", stderr)
	}
}

func TestRecipeRun_MissingRequiredParam(t *testing.T) {
	setupRecipeTest(t)

	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)
	_ = os.WriteFile(filepath.Join(recipeDir, "test.yaml"), []byte(validRecipeYAML), 0600)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "run", "test-recipe"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing required parameter")
	}
	if !strings.Contains(err.Error(), "greeting") {
		t.Errorf("error should mention missing param name, got: %q", err.Error())
	}
}

func TestRecipeRun_NotFound(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "run", "nonexistent-recipe"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent recipe")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should say not found, got: %q", err.Error())
	}
}

func TestRecipeRun_MissingArg(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "run"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

func TestRecipeRun_InvalidParamFormat(t *testing.T) {
	setupRecipeTest(t)

	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)
	_ = os.WriteFile(filepath.Join(recipeDir, "test.yaml"), []byte(validRecipeYAML), 0600)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "run", "test-recipe", "--param", "noequalssign"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid param format")
	}
	if !strings.Contains(err.Error(), "key=value") {
		t.Errorf("error should mention key=value format, got: %q", err.Error())
	}
}

func TestRecipeRun_Plan(t *testing.T) {
	setupRecipeTest(t)

	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)
	_ = os.WriteFile(filepath.Join(recipeDir, "multi.yaml"), []byte(multiStepRecipeYAML), 0600)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"recipe", "run", "multi-step", "--plan"})

	err := cmd.Execute()
	// Plan mode should return ExitError with code 10.
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %v", err)
	}
	if exitErr.Code != 10 {
		t.Errorf("exit code = %d, want 10", exitErr.Code)
	}

	stderr := errOut.String()
	// Human plan should show recipe name and step count.
	if !strings.Contains(stderr, "Recipe: multi-step") {
		t.Errorf("plan should show recipe name, got stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "Steps: 3") {
		t.Errorf("plan should show step count, got stderr: %q", stderr)
	}
	// Should list each step.
	if !strings.Contains(stderr, "step-one") {
		t.Errorf("plan should list step-one, got stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "step-two") {
		t.Errorf("plan should list step-two, got stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "step-three") {
		t.Errorf("plan should list step-three, got stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "No changes made") {
		t.Errorf("plan should say no changes made, got stderr: %q", stderr)
	}
}

func TestRecipeRun_PlanJSON(t *testing.T) {
	setupRecipeTest(t)

	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)
	_ = os.WriteFile(filepath.Join(recipeDir, "multi.yaml"), []byte(multiStepRecipeYAML), 0600)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "run", "multi-step", "--plan", "--output", "json"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %v", err)
	}

	// JSON plan should be valid JSON.
	var plans []map[string]any
	if err := json.Unmarshal(out.Bytes(), &plans); err != nil {
		t.Fatalf("parsing plan JSON: %v", err)
	}
	if len(plans) != 3 {
		t.Fatalf("expected 3 steps in plan, got %d", len(plans))
	}
	for _, p := range plans {
		if p["name"] == nil || p["name"] == "" {
			t.Error("plan step should have name")
		}
		if p["command"] == nil || p["command"] == "" {
			t.Error("plan step should have command")
		}
		if p["would_run"] != true {
			t.Errorf("plan step should have would_run=true, got %v", p["would_run"])
		}
	}
}

func TestRecipeRun_StepFailure(t *testing.T) {
	setupRecipeTest(t)

	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)

	// This recipe runs a command that will fail (nonexistent subcommand).
	failRecipe := `name: fail-recipe
description: Recipe that fails
steps:
  - name: will-fail
    command: 'nonexistent-command-xyz'
on_failure:
  message: "Failed at: {{ .failed_step }}"
`
	_ = os.WriteFile(filepath.Join(recipeDir, "fail.yaml"), []byte(failRecipe), 0600)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"recipe", "run", "fail-recipe"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for failed recipe step")
	}
	if !strings.Contains(err.Error(), "fail-recipe") {
		t.Errorf("error should mention recipe name, got: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "will-fail") {
		t.Errorf("error should mention failed step, got: %q", err.Error())
	}

	// On-failure message should be shown.
	stderr := errOut.String()
	if !strings.Contains(stderr, "Failed at: will-fail") {
		t.Errorf("on_failure message should be shown, got stderr: %q", stderr)
	}
}

func TestRecipeRun_Progress(t *testing.T) {
	setupRecipeTest(t)

	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)
	_ = os.WriteFile(filepath.Join(recipeDir, "multi.yaml"), []byte(multiStepRecipeYAML), 0600)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"recipe", "run", "multi-step"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errOut.String()
	// Should show [N/M] step-name... done for each step.
	if !strings.Contains(stderr, "[1/3] step-one... done") {
		t.Errorf("should show progress for step 1, got stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "[2/3] step-two... done") {
		t.Errorf("should show progress for step 2, got stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "[3/3] step-three... done") {
		t.Errorf("should show progress for step 3, got stderr: %q", stderr)
	}
}

// --- recipe validate tests ---

func TestRecipeValidate_Valid(t *testing.T) {
	setupRecipeTest(t)

	path := writeTempRecipe(t, "valid.yaml", validRecipeYAML)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "validate", path})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "is valid") {
		t.Errorf("should say recipe is valid, got: %q", output)
	}
}

func TestRecipeValidate_Invalid(t *testing.T) {
	setupRecipeTest(t)

	invalidYAML := `name: ""
steps: []
`
	path := writeTempRecipe(t, "invalid.yaml", invalidYAML)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "validate", path})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid recipe")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("error should say validation failed, got: %q", err.Error())
	}
}

func TestRecipeValidate_NonexistentFile(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "validate", "/nonexistent/path/recipe.yaml"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "cannot read") {
		t.Errorf("error should mention cannot read, got: %q", err.Error())
	}
}

func TestRecipeValidate_MissingArg(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "validate"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

func TestRecipeValidate_InvalidYAML(t *testing.T) {
	setupRecipeTest(t)

	path := writeTempRecipe(t, "bad.yaml", "{{{{not valid yaml")

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "validate", path})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

// --- help tests ---

func TestRecipeCmd_Help(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	output := out.String()
	// Should list subcommands.
	if !strings.Contains(output, "list") {
		t.Errorf("help should mention list subcommand, got: %q", output)
	}
	if !strings.Contains(output, "show") {
		t.Errorf("help should mention show subcommand, got: %q", output)
	}
	if !strings.Contains(output, "run") {
		t.Errorf("help should mention run subcommand, got: %q", output)
	}
	if !strings.Contains(output, "validate") {
		t.Errorf("help should mention validate subcommand, got: %q", output)
	}
}

func TestRecipeRunCmd_Help(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "run", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "--param") {
		t.Errorf("run help should mention --param flag, got: %q", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("run help should mention key=value format, got: %q", output)
	}
}

func TestRootCmd_IncludesRecipe(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "recipe") {
		t.Errorf("root help should include recipe command, got: %q", output)
	}
}
