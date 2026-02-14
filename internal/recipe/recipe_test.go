package recipe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Parse and Validate ---

func TestParse_ValidRecipe(t *testing.T) {
	yaml := `
name: test-recipe
description: A test recipe
author: Test Author
version: "1.0"
tags: [test, example]
parameters:
  - name: username
    description: The username
    required: true
    type: string
  - name: notify
    description: Send notification
    required: false
    type: bool
    default: "true"
steps:
  - name: list users
    command: users list --limit 10
  - name: get user
    command: "users get {{ .username }}"
on_success:
  message: "Recipe completed for {{ .username }}"
on_failure:
  message: "Failed at step {{ .failed_step }}"
`
	r, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if r.Name != "test-recipe" {
		t.Errorf("Name = %q, want %q", r.Name, "test-recipe")
	}
	if r.Description != "A test recipe" {
		t.Errorf("Description = %q, want %q", r.Description, "A test recipe")
	}
	if r.Author != "Test Author" {
		t.Errorf("Author = %q, want %q", r.Author, "Test Author")
	}
	if r.Version != "1.0" {
		t.Errorf("Version = %q, want %q", r.Version, "1.0")
	}
	if len(r.Tags) != 2 || r.Tags[0] != "test" || r.Tags[1] != "example" {
		t.Errorf("Tags = %v, want [test, example]", r.Tags)
	}
	if len(r.Parameters) != 2 {
		t.Fatalf("Parameters length = %d, want 2", len(r.Parameters))
	}
	if r.Parameters[0].Name != "username" || !r.Parameters[0].Required {
		t.Errorf("Parameter 0 = %+v", r.Parameters[0])
	}
	if r.Parameters[1].Default != "true" {
		t.Errorf("Parameter 1 default = %q, want %q", r.Parameters[1].Default, "true")
	}
	if len(r.Steps) != 2 {
		t.Fatalf("Steps length = %d, want 2", len(r.Steps))
	}
	if r.OnSuccess == nil || r.OnFailure == nil {
		t.Error("Expected on_success and on_failure hooks")
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	// Use a YAML tab indentation error that yaml.v3 rejects.
	_, err := Parse([]byte("name: test\n\t- bad indent"))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "invalid recipe YAML") {
		t.Errorf("error = %q, expected 'invalid recipe YAML'", err.Error())
	}
}

func TestParse_MissingName(t *testing.T) {
	yaml := `
steps:
  - name: step1
    command: users list
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected validation error for missing name")
	}
	if !strings.Contains(err.Error(), "recipe name is required") {
		t.Errorf("error = %q, expected 'recipe name is required'", err.Error())
	}
}

func TestParse_NoSteps(t *testing.T) {
	yaml := `
name: empty-recipe
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected validation error for no steps")
	}
	if !strings.Contains(err.Error(), "at least one step") {
		t.Errorf("error = %q, expected 'at least one step'", err.Error())
	}
}

func TestParse_StepMissingName(t *testing.T) {
	yaml := `
name: test
steps:
  - command: users list
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected validation error for step missing name")
	}
	if !strings.Contains(err.Error(), "step 1: name is required") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestParse_StepMissingCommand(t *testing.T) {
	yaml := `
name: test
steps:
  - name: empty step
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected validation error for step missing command")
	}
	if !strings.Contains(err.Error(), "command is required") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestParse_InvalidParameterType(t *testing.T) {
	yaml := `
name: test
parameters:
  - name: bad
    type: float
steps:
  - name: step1
    command: users list
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected validation error for invalid parameter type")
	}
	if !strings.Contains(err.Error(), "invalid type \"float\"") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestParse_ParameterMissingName(t *testing.T) {
	yaml := `
name: test
parameters:
  - type: string
steps:
  - name: step1
    command: users list
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected validation error for parameter missing name")
	}
	if !strings.Contains(err.Error(), "parameter 1: name is required") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestParse_ValidParameterTypes(t *testing.T) {
	for _, typ := range []string{"string", "bool", "int", ""} {
		yaml := fmt.Sprintf(`
name: test
parameters:
  - name: p
    type: %s
steps:
  - name: step1
    command: users list
`, typ)
		_, err := Parse([]byte(yaml))
		if err != nil {
			t.Errorf("type %q should be valid, got error: %v", typ, err)
		}
	}
}

// --- ParseFile ---

func TestParseFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	content := `
name: file-recipe
description: from file
steps:
  - name: step1
    command: users list
`
	os.WriteFile(path, []byte(content), 0600)

	r, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	if r.Name != "file-recipe" {
		t.Errorf("Name = %q, want %q", r.Name, "file-recipe")
	}
}

func TestParseFile_NotFound(t *testing.T) {
	_, err := ParseFile("/nonexistent/path/recipe.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "cannot read recipe file") {
		t.Errorf("error = %q", err.Error())
	}
}

// --- ResolveParams ---

func TestResolveParams_AllProvided(t *testing.T) {
	r := &Recipe{
		Parameters: []Parameter{
			{Name: "user", Required: true},
			{Name: "email", Required: true},
		},
	}
	params, err := r.ResolveParams(map[string]string{
		"user":  "jdoe",
		"email": "jdoe@acme.com",
	})
	if err != nil {
		t.Fatalf("ResolveParams failed: %v", err)
	}
	if params["user"] != "jdoe" || params["email"] != "jdoe@acme.com" {
		t.Errorf("params = %v", params)
	}
}

func TestResolveParams_WithDefaults(t *testing.T) {
	r := &Recipe{
		Parameters: []Parameter{
			{Name: "user", Required: true},
			{Name: "role", Default: "member"},
		},
	}
	params, err := r.ResolveParams(map[string]string{"user": "jdoe"})
	if err != nil {
		t.Fatalf("ResolveParams failed: %v", err)
	}
	if params["role"] != "member" {
		t.Errorf("role = %q, want %q", params["role"], "member")
	}
}

func TestResolveParams_DefaultOverridden(t *testing.T) {
	r := &Recipe{
		Parameters: []Parameter{
			{Name: "role", Default: "member"},
		},
	}
	params, err := r.ResolveParams(map[string]string{"role": "admin"})
	if err != nil {
		t.Fatalf("ResolveParams failed: %v", err)
	}
	if params["role"] != "admin" {
		t.Errorf("role = %q, want %q", params["role"], "admin")
	}
}

func TestResolveParams_MissingRequired(t *testing.T) {
	r := &Recipe{
		Parameters: []Parameter{
			{Name: "user", Required: true},
			{Name: "email", Required: true},
		},
	}
	_, err := r.ResolveParams(map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing required parameters")
	}
	if !strings.Contains(err.Error(), "missing required parameters") {
		t.Errorf("error = %q", err.Error())
	}
	if !strings.Contains(err.Error(), "user") || !strings.Contains(err.Error(), "email") {
		t.Errorf("error should list missing params: %q", err.Error())
	}
}

// --- Template Rendering ---

func TestRenderTemplate_NoTemplating(t *testing.T) {
	result, err := renderTemplate("users list --limit 10", nil)
	if err != nil {
		t.Fatalf("renderTemplate failed: %v", err)
	}
	if result != "users list --limit 10" {
		t.Errorf("result = %q", result)
	}
}

func TestRenderTemplate_WithParams(t *testing.T) {
	result, err := renderTemplate("users get {{ .username }}", map[string]string{"username": "jdoe"})
	if err != nil {
		t.Fatalf("renderTemplate failed: %v", err)
	}
	if result != "users get jdoe" {
		t.Errorf("result = %q, want %q", result, "users get jdoe")
	}
}

func TestRenderTemplate_MultipleParams(t *testing.T) {
	tmpl := "users create --username {{ .username }} --email {{ .email }}"
	data := map[string]string{"username": "jdoe", "email": "jdoe@acme.com"}
	result, err := renderTemplate(tmpl, data)
	if err != nil {
		t.Fatalf("renderTemplate failed: %v", err)
	}
	expected := "users create --username jdoe --email jdoe@acme.com"
	if result != expected {
		t.Errorf("result = %q, want %q", result, expected)
	}
}

func TestRenderTemplate_InvalidSyntax(t *testing.T) {
	_, err := renderTemplate("{{ .bad }", map[string]string{})
	if err == nil {
		t.Fatal("expected template parse error")
	}
	if !strings.Contains(err.Error(), "template parse error") {
		t.Errorf("error = %q", err.Error())
	}
}

// --- When Conditions ---

func TestEvaluateWhen_EmptyCondition(t *testing.T) {
	result, err := evaluateWhen("", nil)
	if err != nil {
		t.Fatalf("evaluateWhen failed: %v", err)
	}
	if !result {
		t.Error("empty when should return true")
	}
}

func TestEvaluateWhen_TruthyValue(t *testing.T) {
	result, err := evaluateWhen("{{ .enabled }}", map[string]string{"enabled": "true"})
	if err != nil {
		t.Fatalf("evaluateWhen failed: %v", err)
	}
	if !result {
		t.Error("expected truthy result")
	}
}

func TestEvaluateWhen_FalsyValues(t *testing.T) {
	cases := []struct {
		when string
		data map[string]string
	}{
		{"{{ .enabled }}", map[string]string{"enabled": "false"}},
		{"{{ .enabled }}", map[string]string{"enabled": "0"}},
		{"{{ .enabled }}", map[string]string{"enabled": ""}},
		{"{{ .missing }}", map[string]string{}},
	}

	for _, tc := range cases {
		result, err := evaluateWhen(tc.when, tc.data)
		if err != nil {
			t.Fatalf("evaluateWhen(%q, %v) failed: %v", tc.when, tc.data, err)
		}
		if result {
			t.Errorf("evaluateWhen(%q, %v) = true, want false", tc.when, tc.data)
		}
	}
}

func TestEvaluateWhen_InvalidTemplate(t *testing.T) {
	_, err := evaluateWhen("{{ .bad }", nil)
	if err == nil {
		t.Fatal("expected error for invalid when template")
	}
}

// --- parseCommandArgs ---

func TestParseCommandArgs_Simple(t *testing.T) {
	args := parseCommandArgs("users list --limit 10")
	expected := []string{"users", "list", "--limit", "10"}
	if len(args) != len(expected) {
		t.Fatalf("args = %v, want %v", args, expected)
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("args[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestParseCommandArgs_DoubleQuoted(t *testing.T) {
	args := parseCommandArgs(`users list --filter "os=Mac OS X"`)
	expected := []string{"users", "list", "--filter", "os=Mac OS X"}
	if len(args) != len(expected) {
		t.Fatalf("args = %v, want %v", args, expected)
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("args[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestParseCommandArgs_SingleQuoted(t *testing.T) {
	args := parseCommandArgs("users list --filter 'os=Mac OS X'")
	expected := []string{"users", "list", "--filter", "os=Mac OS X"}
	if len(args) != len(expected) {
		t.Fatalf("args = %v, want %v", args, expected)
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("args[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestParseCommandArgs_Empty(t *testing.T) {
	args := parseCommandArgs("")
	if len(args) != 0 {
		t.Errorf("args = %v, want empty", args)
	}
}

func TestParseCommandArgs_ExtraSpaces(t *testing.T) {
	args := parseCommandArgs("  users   list   ")
	expected := []string{"users", "list"}
	if len(args) != len(expected) {
		t.Fatalf("args = %v, want %v", args, expected)
	}
}

// --- Execute ---

func mockDispatcher(responses map[string]struct {
	output string
	err    error
}) CommandDispatcher {
	return func(args []string) (string, error) {
		key := strings.Join(args, " ")
		if r, ok := responses[key]; ok {
			return r.output, r.err
		}
		return "", fmt.Errorf("unexpected command: %s", key)
	}
}

func TestExecute_AllStepsSuccess(t *testing.T) {
	r := &Recipe{
		Name: "test",
		Steps: []Step{
			{Name: "step1", Command: "users list"},
			{Name: "step2", Command: "devices list"},
		},
		OnSuccess: &Hook{Message: "All done!"},
	}

	dispatcher := mockDispatcher(map[string]struct {
		output string
		err    error
	}{
		"users list":   {"[{\"username\":\"jdoe\"}]", nil},
		"devices list": {"[{\"hostname\":\"mac1\"}]", nil},
	})

	var progress bytes.Buffer
	result, err := r.Execute(dispatcher, nil, &progress)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Status != "success" {
		t.Errorf("Status = %q, want %q", result.Status, "success")
	}
	if len(result.Steps) != 2 {
		t.Fatalf("Steps count = %d, want 2", len(result.Steps))
	}
	if result.Steps[0].Status != "success" || result.Steps[1].Status != "success" {
		t.Error("Expected all steps to succeed")
	}
	if result.Message != "All done!" {
		t.Errorf("Message = %q, want %q", result.Message, "All done!")
	}

	// Check progress output.
	progStr := progress.String()
	if !strings.Contains(progStr, "[1/2] step1... done") {
		t.Errorf("progress missing step1: %q", progStr)
	}
	if !strings.Contains(progStr, "[2/2] step2... done") {
		t.Errorf("progress missing step2: %q", progStr)
	}
}

func TestExecute_StepFailure_StopsExecution(t *testing.T) {
	r := &Recipe{
		Name: "test",
		Steps: []Step{
			{Name: "step1", Command: "users list"},
			{Name: "step2", Command: "failing command"},
			{Name: "step3", Command: "devices list"},
		},
		OnFailure: &Hook{Message: "Failed at {{ .failed_step }}"},
	}

	dispatcher := mockDispatcher(map[string]struct {
		output string
		err    error
	}{
		"users list":      {"[]", nil},
		"failing command": {"", errors.New("command failed")},
	})

	var progress bytes.Buffer
	result, err := r.Execute(dispatcher, nil, &progress)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if result.Status != "failed" {
		t.Errorf("Status = %q, want %q", result.Status, "failed")
	}
	if len(result.Steps) != 2 {
		t.Errorf("Steps count = %d, want 2 (step3 should not run)", len(result.Steps))
	}
	if result.Steps[1].Error != "command failed" {
		t.Errorf("Step 2 error = %q", result.Steps[1].Error)
	}
	if result.Message != "Failed at step2" {
		t.Errorf("Message = %q, want %q", result.Message, "Failed at step2")
	}
}

func TestExecute_ContinueOnError(t *testing.T) {
	r := &Recipe{
		Name: "test",
		Steps: []Step{
			{Name: "step1", Command: "failing command", ContinueOnError: true},
			{Name: "step2", Command: "users list"},
		},
	}

	dispatcher := mockDispatcher(map[string]struct {
		output string
		err    error
	}{
		"failing command": {"", errors.New("oops")},
		"users list":      {"[]", nil},
	})

	var progress bytes.Buffer
	result, err := r.Execute(dispatcher, nil, &progress)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// With continue_on_error, execution should continue.
	if result.Status != "success" {
		t.Errorf("Status = %q, want %q (continue_on_error)", result.Status, "success")
	}
	if len(result.Steps) != 2 {
		t.Fatalf("Steps count = %d, want 2", len(result.Steps))
	}
	if result.Steps[0].Status != "failed" {
		t.Errorf("Step 1 status = %q, want %q", result.Steps[0].Status, "failed")
	}
	if result.Steps[1].Status != "success" {
		t.Errorf("Step 2 status = %q, want %q", result.Steps[1].Status, "success")
	}
}

func TestExecute_WithTemplateParams(t *testing.T) {
	r := &Recipe{
		Name: "test",
		Parameters: []Parameter{
			{Name: "username", Required: true},
		},
		Steps: []Step{
			{Name: "get user", Command: "users get {{ .username }}"},
		},
	}

	var dispatched []string
	dispatcher := func(args []string) (string, error) {
		dispatched = append(dispatched, strings.Join(args, " "))
		return "{}", nil
	}

	var progress bytes.Buffer
	_, err := r.Execute(dispatcher, map[string]string{"username": "jdoe"}, &progress)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(dispatched) != 1 || dispatched[0] != "users get jdoe" {
		t.Errorf("dispatched = %v, want [users get jdoe]", dispatched)
	}
}

func TestExecute_CaptureOutput(t *testing.T) {
	r := &Recipe{
		Name: "test",
		Steps: []Step{
			{Name: "get id", Command: "users list --ids", Capture: "user_id"},
			{Name: "use id", Command: "users get {{ .user_id }}"},
		},
	}

	var dispatched []string
	dispatcher := func(args []string) (string, error) {
		dispatched = append(dispatched, strings.Join(args, " "))
		if strings.Contains(strings.Join(args, " "), "--ids") {
			return "abc123def456\n", nil
		}
		return "{}", nil
	}

	var progress bytes.Buffer
	_, err := r.Execute(dispatcher, nil, &progress)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(dispatched) != 2 {
		t.Fatalf("dispatched count = %d, want 2", len(dispatched))
	}
	// The second command should use the captured output (trimmed).
	if dispatched[1] != "users get abc123def456" {
		t.Errorf("dispatched[1] = %q, want %q", dispatched[1], "users get abc123def456")
	}
}

func TestExecute_WhenCondition_Skips(t *testing.T) {
	r := &Recipe{
		Name: "test",
		Parameters: []Parameter{
			{Name: "do_notify", Default: "false"},
		},
		Steps: []Step{
			{Name: "always", Command: "users list"},
			{Name: "notify", Command: "notify sent", When: "{{ .do_notify }}"},
		},
	}

	var dispatched []string
	dispatcher := func(args []string) (string, error) {
		dispatched = append(dispatched, strings.Join(args, " "))
		return "", nil
	}

	var progress bytes.Buffer
	result, err := r.Execute(dispatcher, nil, &progress)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(dispatched) != 1 {
		t.Errorf("dispatched = %v, want only 1 command (notify skipped)", dispatched)
	}
	if result.Steps[1].Status != "skipped" {
		t.Errorf("step 2 status = %q, want %q", result.Steps[1].Status, "skipped")
	}
	if !strings.Contains(progress.String(), "skipped") {
		t.Errorf("progress should show 'skipped': %q", progress.String())
	}
}

func TestExecute_WhenCondition_Runs(t *testing.T) {
	r := &Recipe{
		Name: "test",
		Parameters: []Parameter{
			{Name: "do_notify", Default: "true"},
		},
		Steps: []Step{
			{Name: "notify", Command: "users list", When: "{{ .do_notify }}"},
		},
	}

	dispatched := false
	dispatcher := func(args []string) (string, error) {
		dispatched = true
		return "", nil
	}

	var progress bytes.Buffer
	_, err := r.Execute(dispatcher, nil, &progress)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !dispatched {
		t.Error("step with truthy when should run")
	}
}

func TestExecute_MissingRequiredParam(t *testing.T) {
	r := &Recipe{
		Name: "test",
		Parameters: []Parameter{
			{Name: "user", Required: true},
		},
		Steps: []Step{
			{Name: "step1", Command: "users list"},
		},
	}

	_, err := r.Execute(nil, nil, io.Discard)
	if err == nil {
		t.Fatal("expected error for missing required parameter")
	}
	if !strings.Contains(err.Error(), "missing required parameters") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestExecute_OnSuccessTemplate(t *testing.T) {
	r := &Recipe{
		Name: "test",
		Parameters: []Parameter{
			{Name: "user"},
		},
		Steps: []Step{
			{Name: "step1", Command: "users list"},
		},
		OnSuccess: &Hook{Message: "Done for {{ .user }}"},
	}

	dispatcher := func(args []string) (string, error) { return "", nil }

	result, err := r.Execute(dispatcher, map[string]string{"user": "jdoe"}, io.Discard)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Message != "Done for jdoe" {
		t.Errorf("Message = %q, want %q", result.Message, "Done for jdoe")
	}
}

func TestExecute_OnFailureTemplate(t *testing.T) {
	r := &Recipe{
		Name: "test",
		Steps: []Step{
			{Name: "failing step", Command: "bad"},
		},
		OnFailure: &Hook{Message: "Step {{ .failed_step }} broke"},
	}

	dispatcher := func(args []string) (string, error) {
		return "", errors.New("error")
	}

	result, err := r.Execute(dispatcher, nil, io.Discard)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Message != "Step failing step broke" {
		t.Errorf("Message = %q, want %q", result.Message, "Step failing step broke")
	}
}

// --- Plan ---

func TestPlan_RendersCommands(t *testing.T) {
	r := &Recipe{
		Name: "test",
		Parameters: []Parameter{
			{Name: "user", Required: true},
		},
		Steps: []Step{
			{Name: "get user", Command: "users get {{ .user }}"},
			{Name: "list devices", Command: "devices list"},
		},
	}

	plans, err := r.Plan(map[string]string{"user": "jdoe"})
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if len(plans) != 2 {
		t.Fatalf("plans count = %d, want 2", len(plans))
	}
	if plans[0].Command != "users get jdoe" {
		t.Errorf("plans[0].Command = %q, want %q", plans[0].Command, "users get jdoe")
	}
	if !plans[0].WouldRun || !plans[1].WouldRun {
		t.Error("expected all steps would_run=true")
	}
}

func TestPlan_WithWhenCondition(t *testing.T) {
	r := &Recipe{
		Name: "test",
		Parameters: []Parameter{
			{Name: "notify", Default: "false"},
		},
		Steps: []Step{
			{Name: "always", Command: "users list"},
			{Name: "conditional", Command: "notify", When: "{{ .notify }}"},
		},
	}

	plans, err := r.Plan(nil)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if !plans[0].WouldRun {
		t.Error("step 1 should would_run=true")
	}
	if plans[1].WouldRun {
		t.Error("step 2 should would_run=false (notify=false)")
	}
	if plans[1].When != "{{ .notify }}" {
		t.Errorf("step 2 When = %q", plans[1].When)
	}
}

func TestPlan_MissingRequiredParam(t *testing.T) {
	r := &Recipe{
		Name: "test",
		Parameters: []Parameter{
			{Name: "user", Required: true},
		},
		Steps: []Step{
			{Name: "step1", Command: "users get {{ .user }}"},
		},
	}

	_, err := r.Plan(nil)
	if err == nil {
		t.Fatal("expected error for missing required parameter")
	}
}

// --- Plan Rendering ---

func TestRenderPlanJSON(t *testing.T) {
	plans := []StepPlan{
		{Name: "step1", Command: "users list", WouldRun: true},
		{Name: "step2", Command: "notify", When: "{{ .notify }}", WouldRun: false},
	}

	var buf bytes.Buffer
	err := RenderPlanJSON(&buf, plans)
	if err != nil {
		t.Fatalf("RenderPlanJSON failed: %v", err)
	}

	var parsed []StepPlan
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(parsed) != 2 {
		t.Errorf("parsed length = %d, want 2", len(parsed))
	}
}

func TestRenderPlanHuman(t *testing.T) {
	plans := []StepPlan{
		{Name: "step1", Command: "users list", WouldRun: true},
		{Name: "step2", Command: "notify", When: "{{ .notify }}", WouldRun: false},
	}

	var buf bytes.Buffer
	RenderPlanHuman(&buf, "test-recipe", plans)
	output := buf.String()

	if !strings.Contains(output, "Recipe: test-recipe") {
		t.Errorf("missing recipe name in output: %q", output)
	}
	if !strings.Contains(output, "will run") {
		t.Errorf("missing 'will run' in output: %q", output)
	}
	if !strings.Contains(output, "will skip") {
		t.Errorf("missing 'will skip' in output: %q", output)
	}
	if !strings.Contains(output, "No changes made (plan mode)") {
		t.Errorf("missing plan mode notice: %q", output)
	}
}

// --- LoadFromDir ---

func TestLoadFromDir_Success(t *testing.T) {
	dir := t.TempDir()

	// Write valid recipe files.
	r1 := `name: recipe1
steps:
  - name: s1
    command: users list`
	r2 := `name: recipe2
steps:
  - name: s1
    command: devices list`
	os.WriteFile(filepath.Join(dir, "r1.yaml"), []byte(r1), 0600)
	os.WriteFile(filepath.Join(dir, "r2.yml"), []byte(r2), 0600)

	// Write a non-yaml file (should be ignored).
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a recipe"), 0600)

	recipes, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}
	if len(recipes) != 2 {
		t.Errorf("loaded %d recipes, want 2", len(recipes))
	}
}

func TestLoadFromDir_NonexistentDir(t *testing.T) {
	recipes, err := LoadFromDir("/nonexistent/path")
	if err != nil {
		t.Fatalf("LoadFromDir should not error for nonexistent dir: %v", err)
	}
	if recipes != nil {
		t.Errorf("expected nil recipes for nonexistent dir, got %v", recipes)
	}
}

func TestLoadFromDir_InvalidRecipeSkipped(t *testing.T) {
	dir := t.TempDir()

	// Write an invalid recipe (no name).
	invalid := `steps:
  - name: s1
    command: users list`
	os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(invalid), 0600)

	// Write a valid recipe.
	valid := `name: good
steps:
  - name: s1
    command: users list`
	os.WriteFile(filepath.Join(dir, "good.yaml"), []byte(valid), 0600)

	recipes, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}
	if len(recipes) != 1 {
		t.Errorf("loaded %d recipes, want 1 (invalid should be skipped)", len(recipes))
	}
	if recipes[0].Name != "good" {
		t.Errorf("recipe name = %q, want %q", recipes[0].Name, "good")
	}
}

func TestLoadFromDir_SkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "subdir"), 0700)
	os.WriteFile(filepath.Join(dir, "subdir", "recipe.yaml"), []byte(`name: sub
steps:
  - name: s1
    command: x`), 0600)

	recipes, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}
	if len(recipes) != 0 {
		t.Errorf("loaded %d recipes, want 0 (subdirs should not be scanned)", len(recipes))
	}
}

// --- FindByName ---

func TestFindByName_Found(t *testing.T) {
	recipes := []*Recipe{
		{Name: "recipe-a"},
		{Name: "recipe-b"},
	}
	r := FindByName(recipes, "recipe-b")
	if r == nil {
		t.Fatal("expected to find recipe-b")
	}
	if r.Name != "recipe-b" {
		t.Errorf("found = %q", r.Name)
	}
}

func TestFindByName_NotFound(t *testing.T) {
	recipes := []*Recipe{
		{Name: "recipe-a"},
	}
	r := FindByName(recipes, "nonexistent")
	if r != nil {
		t.Errorf("expected nil, got %+v", r)
	}
}

// --- NewDispatcher ---

func TestNewDispatcher_CapturesOutput(t *testing.T) {
	// Mock a CobraCommand that writes to its configured stdout.
	dispatcher := NewDispatcher(func() CobraCommand {
		return &mockCobraCmd{
			executeFunc: func(out io.Writer) error {
				fmt.Fprint(out, "hello world")
				return nil
			},
		}
	})

	output, err := dispatcher([]string{"test"})
	if err != nil {
		t.Fatalf("dispatcher error: %v", err)
	}
	if output != "hello world" {
		t.Errorf("output = %q, want %q", output, "hello world")
	}
}

func TestNewDispatcher_ReturnsError(t *testing.T) {
	dispatcher := NewDispatcher(func() CobraCommand {
		return &mockCobraCmd{
			executeFunc: func(out io.Writer) error {
				return errors.New("command failed")
			},
		}
	})

	_, err := dispatcher([]string{"test"})
	if err == nil {
		t.Fatal("expected error from dispatcher")
	}
	if !strings.Contains(err.Error(), "command failed") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestNewDispatcher_StderrInError(t *testing.T) {
	dispatcher := NewDispatcher(func() CobraCommand {
		return &mockCobraCmd{
			executeFunc: func(out io.Writer) error {
				return errors.New("generic error")
			},
			stderrFunc: func(errW io.Writer) {
				fmt.Fprint(errW, "detailed error message")
			},
		}
	})

	_, err := dispatcher([]string{"test"})
	if err == nil {
		t.Fatal("expected error from dispatcher")
	}
	if !strings.Contains(err.Error(), "detailed error message") {
		t.Errorf("error = %q, want stderr content", err.Error())
	}
}

// mockCobraCmd implements CobraCommand for testing.
type mockCobraCmd struct {
	args        []string
	out         io.Writer
	errW        io.Writer
	executeFunc func(out io.Writer) error
	stderrFunc  func(errW io.Writer)
}

func (m *mockCobraCmd) SetArgs(a []string) { m.args = a }
func (m *mockCobraCmd) SetOut(w io.Writer)  { m.out = w }
func (m *mockCobraCmd) SetErr(w io.Writer)  { m.errW = w }
func (m *mockCobraCmd) Execute() error {
	if m.stderrFunc != nil && m.errW != nil {
		m.stderrFunc(m.errW)
	}
	if m.executeFunc != nil {
		return m.executeFunc(m.out)
	}
	return nil
}

// --- RecipesDir ---

func TestRecipesDir(t *testing.T) {
	// Set JC_CONFIG to control config dir.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)

	expected := filepath.Join(dir, "recipes")
	if got := RecipesDir(); got != expected {
		t.Errorf("RecipesDir() = %q, want %q", got, expected)
	}
}

// --- Multiple validation errors ---

func TestValidate_MultipleErrors(t *testing.T) {
	r := &Recipe{
		// No name, no steps, invalid param type.
		Parameters: []Parameter{
			{Name: "p", Type: "float"},
		},
	}
	err := r.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "recipe name is required") {
		t.Errorf("missing name error in: %q", msg)
	}
	if !strings.Contains(msg, "at least one step") {
		t.Errorf("missing steps error in: %q", msg)
	}
	if !strings.Contains(msg, "invalid type") {
		t.Errorf("missing type error in: %q", msg)
	}
}

// --- Execute with template error in command ---

func TestExecute_TemplateErrorInCommand(t *testing.T) {
	r := &Recipe{
		Name: "test",
		Steps: []Step{
			{Name: "bad template", Command: "users get {{ .bad }"},
		},
	}

	result, err := r.Execute(nil, nil, io.Discard)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want %q", result.Status, "failed")
	}
	if result.Steps[0].Status != "failed" {
		t.Errorf("Step status = %q, want %q", result.Steps[0].Status, "failed")
	}
}

// --- Execute with when condition error ---

func TestExecute_WhenConditionError(t *testing.T) {
	r := &Recipe{
		Name: "test",
		Steps: []Step{
			{Name: "bad when", Command: "users list", When: "{{ .bad }"},
		},
		OnFailure: &Hook{Message: "Failed at {{ .failed_step }}"},
	}

	result, err := r.Execute(nil, nil, io.Discard)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want %q", result.Status, "failed")
	}
	if result.Message != "Failed at bad when" {
		t.Errorf("Message = %q", result.Message)
	}
}
