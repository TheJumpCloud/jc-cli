package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// --- recipe create tests ---

// multiLineInput implements InputReader for tests that need multiple line responses.
type multiLineInput struct {
	lines []string
	index int
}

func (m *multiLineInput) ReadAPIKey() (string, error) {
	return m.ReadLine()
}

func (m *multiLineInput) ReadLine() (string, error) {
	if m.index >= len(m.lines) {
		return "", fmt.Errorf("no more input")
	}
	line := m.lines[m.index]
	m.index++
	return line, nil
}

func overrideRecipeInputReader(t *testing.T, input InputReader) {
	t.Helper()
	orig := recipeInputReader
	recipeInputReader = input
	t.Cleanup(func() { recipeInputReader = orig })
}

func TestRecipeCreate_Success(t *testing.T) {
	setupRecipeTest(t)

	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)

	// Simulate: name, desc, param(name, desc, required), empty param, step(name, cmd), empty step
	overrideRecipeInputReader(t, &multiLineInput{lines: []string{
		"my-new-recipe",      // name
		"A brand new recipe", // description
		"target",             // param name
		"Target user",        // param desc
		"y",                  // param required
		"",                   // empty param name (stop params)
		"greet",              // step name
		"users list",         // step command
		"",                   // empty step name (stop steps)
	}})

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"recipe", "create"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stdout := out.String()
	if !strings.Contains(stdout, "Recipe saved to") {
		t.Errorf("should confirm save, got: %q", stdout)
	}

	// Verify the file was created.
	savedPath := filepath.Join(recipeDir, "my-new-recipe.yaml")
	data, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("recipe file not created: %v", err)
	}

	// Verify the YAML can be parsed back.
	r, err := recipe.ParseFile(savedPath)
	if err != nil {
		t.Fatalf("saved recipe invalid: %v", err)
	}
	if r.Name != "my-new-recipe" {
		t.Errorf("name = %q, want my-new-recipe", r.Name)
	}
	if r.Description != "A brand new recipe" {
		t.Errorf("description = %q, want A brand new recipe", r.Description)
	}
	if len(r.Parameters) != 1 {
		t.Fatalf("parameters count = %d, want 1", len(r.Parameters))
	}
	if r.Parameters[0].Name != "target" {
		t.Errorf("param name = %q, want target", r.Parameters[0].Name)
	}
	if !r.Parameters[0].Required {
		t.Error("param should be required")
	}
	if len(r.Steps) != 1 {
		t.Fatalf("steps count = %d, want 1", len(r.Steps))
	}
	if r.Steps[0].Name != "greet" {
		t.Errorf("step name = %q, want greet", r.Steps[0].Name)
	}
	if r.Steps[0].Command != "users list" {
		t.Errorf("step command = %q, want users list", r.Steps[0].Command)
	}
	_ = data
}

func TestRecipeCreate_EmptyName(t *testing.T) {
	setupRecipeTest(t)

	overrideRecipeInputReader(t, &multiLineInput{lines: []string{""}})

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "create"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("error should mention name required, got: %q", err.Error())
	}
}

func TestRecipeCreate_NoSteps(t *testing.T) {
	setupRecipeTest(t)

	overrideRecipeInputReader(t, &multiLineInput{lines: []string{
		"empty-recipe", // name
		"No steps",     // description
		"",             // no params
		"",             // no steps
	}})

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "create"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no steps")
	}
	if !strings.Contains(err.Error(), "at least one step") {
		t.Errorf("error should mention steps required, got: %q", err.Error())
	}
}

func TestRecipeCreate_OverwriteConfirm(t *testing.T) {
	setupRecipeTest(t)

	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)

	// Pre-create the file.
	_ = os.WriteFile(filepath.Join(recipeDir, "existing.yaml"),
		[]byte("name: existing\nsteps:\n  - name: s1\n    command: version\n"), 0600)

	overrideRecipeInputReader(t, &multiLineInput{lines: []string{
		"existing",       // name (conflicts)
		"Updated recipe", // description
		"",               // no params
		"new-step",       // step name
		"users list",     // step command
		"",               // no more steps
		"y",              // overwrite confirm
	}})

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "create"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(out.String(), "Recipe saved") {
		t.Errorf("should confirm save after overwrite, got: %q", out.String())
	}
}

func TestRecipeCreate_OverwriteCancel(t *testing.T) {
	setupRecipeTest(t)

	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)

	// Pre-create the file.
	_ = os.WriteFile(filepath.Join(recipeDir, "existing.yaml"),
		[]byte("name: existing\nsteps:\n  - name: s1\n    command: version\n"), 0600)

	overrideRecipeInputReader(t, &multiLineInput{lines: []string{
		"existing",       // name (conflicts)
		"Updated recipe", // description
		"",               // no params
		"new-step",       // step name
		"users list",     // step command
		"",               // no more steps
		"n",              // cancel overwrite
	}})

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"recipe", "create"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(errOut.String(), "Cancelled") {
		t.Errorf("should show cancelled, got stderr: %q", errOut.String())
	}
}

// --- recipe import tests ---

func TestRecipeImport_LocalFile(t *testing.T) {
	setupRecipeTest(t)

	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)

	// Write a recipe to a temp file.
	sourcePath := writeTempRecipe(t, "import-me.yaml", validRecipeYAML)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "import", sourcePath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stdout := out.String()
	if !strings.Contains(stdout, "imported") {
		t.Errorf("should confirm import, got: %q", stdout)
	}
	if !strings.Contains(stdout, "test-recipe") {
		t.Errorf("should show recipe name, got: %q", stdout)
	}

	// Verify the file was saved.
	saved := filepath.Join(recipeDir, "test-recipe.yaml")
	if _, err := os.Stat(saved); err != nil {
		t.Errorf("imported recipe file not found: %v", err)
	}
}

func TestRecipeImport_InvalidFile(t *testing.T) {
	setupRecipeTest(t)

	// Write an invalid recipe file.
	path := writeTempRecipe(t, "bad.yaml", "name: \"\"\nsteps: []\n")

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "import", path})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid recipe")
	}
	if !strings.Contains(err.Error(), "invalid recipe") {
		t.Errorf("error should mention invalid recipe, got: %q", err.Error())
	}
}

func TestRecipeImport_NonexistentFile(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "import", "/nonexistent/path/recipe.yaml"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "reading recipe file") {
		t.Errorf("error should mention reading file, got: %q", err.Error())
	}
}

func TestRecipeImport_URL(t *testing.T) {
	setupRecipeTest(t)

	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)

	// Start a test server that serves recipe YAML.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		w.Write([]byte(validRecipeYAML))
	}))
	defer srv.Close()

	// Confirm import.
	overrideRecipeInputReader(t, &multiLineInput{lines: []string{"y"}})

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"recipe", "import", srv.URL + "/recipe.yaml"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stdout := out.String()
	if !strings.Contains(stdout, "imported") {
		t.Errorf("should confirm import, got: %q", stdout)
	}

	// Verify the recipe details were shown.
	stderr := errOut.String()
	if !strings.Contains(stderr, "test-recipe") {
		t.Errorf("should show recipe name, got stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "Steps: 1") {
		t.Errorf("should show step count, got stderr: %q", stderr)
	}

	// Verify file saved.
	if _, err := os.Stat(filepath.Join(recipeDir, "test-recipe.yaml")); err != nil {
		t.Errorf("imported recipe file not found: %v", err)
	}
}

func TestRecipeImport_URL_Cancel(t *testing.T) {
	setupRecipeTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(validRecipeYAML))
	}))
	defer srv.Close()

	overrideRecipeInputReader(t, &multiLineInput{lines: []string{"n"}})

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"recipe", "import", srv.URL + "/recipe.yaml"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(errOut.String(), "cancelled") {
		t.Errorf("should show cancelled, got stderr: %q", errOut.String())
	}

	// File should NOT be saved.
	recipeDir := recipe.RecipesDir()
	if _, err := os.Stat(filepath.Join(recipeDir, "test-recipe.yaml")); err == nil {
		t.Error("recipe should not have been saved after cancel")
	}
}

func TestRecipeImport_URL_HTTPError(t *testing.T) {
	setupRecipeTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "import", srv.URL + "/notfound.yaml"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for HTTP 404")
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("error should mention HTTP status, got: %q", err.Error())
	}
}

func TestRecipeImport_URL_OversizedBody(t *testing.T) {
	setupRecipeTest(t)

	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)

	totalSent := 0
	// Serve a body larger than maxRecipeBodySize (10 MB).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		// Write invalid YAML that is larger than the limit.
		// After LimitReader truncates, this will be invalid YAML.
		chunk := []byte("- invalid: [unclosed\n")
		for totalSent < 11<<20 { // 11 MB
			n, err := w.Write(chunk)
			if err != nil {
				return
			}
			totalSent += n
		}
	}))
	defer srv.Close()

	overrideRecipeInputReader(t, &multiLineInput{lines: []string{"y"}})

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "import", srv.URL + "/huge.yaml"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for oversized body")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error should mention size limit, got: %q", err.Error())
	}
}

func TestRecipeImport_URL_OversizedBodyValidPrefixRejected(t *testing.T) {
	setupRecipeTest(t)

	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)

	validPrefix := []byte("name: oversized-valid\nsteps:\n  - name: step1\n    command: version\n")
	padding := bytes.Repeat([]byte(" "), int(maxRecipeBodySize)+1024)
	body := append(validPrefix, padding...)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	overrideRecipeInputReader(t, &multiLineInput{lines: []string{"y"}})

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "import", srv.URL + "/huge-valid-prefix.yaml"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for oversized body with valid YAML prefix")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error should mention size limit, got: %q", err.Error())
	}

	if _, statErr := os.Stat(filepath.Join(recipeDir, "oversized-valid.yaml")); statErr == nil {
		t.Fatal("recipe should not be saved when body exceeds maxRecipeBodySize")
	}
}

func TestRecipeImport_DuplicateOverwrite(t *testing.T) {
	setupRecipeTest(t)

	recipeDir := t.TempDir()
	overrideRecipesDir(t, recipeDir)

	// Pre-create the recipe file.
	_ = os.WriteFile(filepath.Join(recipeDir, "test-recipe.yaml"),
		[]byte("name: test-recipe\nsteps:\n  - name: old\n    command: version\n"), 0600)

	sourcePath := writeTempRecipe(t, "import-me.yaml", validRecipeYAML)

	// Confirm overwrite.
	overrideRecipeInputReader(t, &multiLineInput{lines: []string{"y"}})

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "import", sourcePath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(out.String(), "imported") {
		t.Errorf("should confirm import, got: %q", out.String())
	}
}

func TestRecipeImport_MissingArg(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "import"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

// --- recipe export tests ---

func TestRecipeExport_Stdout(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "export", "onboard-user"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stdout := out.String()
	// Should be valid YAML with recipe name.
	if !strings.Contains(stdout, "name: onboard-user") {
		t.Errorf("YAML should contain recipe name, got: %q", stdout)
	}
	if !strings.Contains(stdout, "steps:") {
		t.Errorf("YAML should contain steps, got: %q", stdout)
	}
	if !strings.Contains(stdout, "parameters:") {
		t.Errorf("YAML should contain parameters, got: %q", stdout)
	}

	// The exported YAML should be parseable.
	_, err := recipe.Parse([]byte(stdout))
	if err != nil {
		t.Fatalf("exported YAML not valid: %v", err)
	}
}

func TestRecipeExport_ToFile(t *testing.T) {
	setupRecipeTest(t)

	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "exported.yaml")

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"recipe", "export", "onboard-user", "--file", outPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Confirmation on stderr.
	if !strings.Contains(errOut.String(), "exported") {
		t.Errorf("should confirm export, got stderr: %q", errOut.String())
	}

	// File should exist and be valid YAML.
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("exported file not found: %v", err)
	}

	r, err := recipe.Parse(data)
	if err != nil {
		t.Fatalf("exported file not valid: %v", err)
	}
	if r.Name != "onboard-user" {
		t.Errorf("name = %q, want onboard-user", r.Name)
	}
}

func TestRecipeExport_NotFound(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "export", "nonexistent-recipe"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent recipe")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should say not found, got: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "onboard-user") {
		t.Errorf("error should list available recipes, got: %q", err.Error())
	}
}

func TestRecipeExport_MissingArg(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "export"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

func TestRecipeExport_RoundTrip(t *testing.T) {
	setupRecipeTest(t)

	// Export a recipe, then validate the output can be re-imported.
	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "export", "offboard-user"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Parse the exported YAML.
	r, err := recipe.Parse(out.Bytes())
	if err != nil {
		t.Fatalf("exported YAML not valid: %v", err)
	}
	if r.Name != "offboard-user" {
		t.Errorf("round-trip name = %q, want offboard-user", r.Name)
	}
}

// --- help tests for new commands ---

func TestRecipeCmd_Help_IncludesNewCommands(t *testing.T) {
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
	if !strings.Contains(output, "create") {
		t.Errorf("help should mention create subcommand, got: %q", output)
	}
	if !strings.Contains(output, "import") {
		t.Errorf("help should mention import subcommand, got: %q", output)
	}
	if !strings.Contains(output, "export") {
		t.Errorf("help should mention export subcommand, got: %q", output)
	}
}

func TestRecipeExportCmd_Help(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "export", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "--file") {
		t.Errorf("export help should mention --file flag, got: %q", output)
	}
}

func TestRecipeImportCmd_Help(t *testing.T) {
	setupRecipeTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"recipe", "import", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "url-or-path") {
		t.Errorf("import help should mention url-or-path usage, got: %q", output)
	}
}

// TestResetViperForRecipeStep ensures the reset restores the compiled defaults
// for keys the root PersistentPreRunE can Set. Regression guard for the -t
// leakage bug: step N's -t (which Sets defaults.output=table) used to persist
// into step N+1 and break JSON-capturing steps.
func TestResetViperForRecipeStep(t *testing.T) {
	viper.Set("defaults.output", "table")
	viper.Set("plan", true)

	resetViperForRecipeStep()

	if got := viper.GetString("defaults.output"); got != "json" {
		t.Errorf("defaults.output = %q after reset, want 'json'", got)
	}
	if viper.GetBool("plan") {
		t.Error("plan flag still true after reset")
	}
}
