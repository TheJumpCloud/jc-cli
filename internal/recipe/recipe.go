// Package recipe provides the recipe engine for parsing YAML recipe definitions,
// rendering Go templates in step commands, and executing steps sequentially
// by dispatching them to the existing CLI command tree.
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
	"text/template"

	"go.yaml.in/yaml/v3"

	"github.com/klaassen-consulting/jc/internal/config"
)

// Recipe represents a parsed recipe definition.
type Recipe struct {
	Name        string      `yaml:"name" json:"name"`
	Description string      `yaml:"description" json:"description"`
	Author      string      `yaml:"author,omitempty" json:"author,omitempty"`
	Version     string      `yaml:"version,omitempty" json:"version,omitempty"`
	Tags        []string    `yaml:"tags,omitempty" json:"tags,omitempty"`
	Parameters  []Parameter `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Steps       []Step      `yaml:"steps" json:"steps"`
	OnSuccess   *Hook       `yaml:"on_success,omitempty" json:"on_success,omitempty"`
	OnFailure   *Hook       `yaml:"on_failure,omitempty" json:"on_failure,omitempty"`
}

// Parameter defines an input parameter for a recipe.
type Parameter struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool   `yaml:"required,omitempty" json:"required,omitempty"`
	Type        string `yaml:"type,omitempty" json:"type,omitempty"` // string, bool, int
	Default     string `yaml:"default,omitempty" json:"default,omitempty"`
}

// Step defines a single step in a recipe.
type Step struct {
	Name            string `yaml:"name" json:"name"`
	Command         string `yaml:"command" json:"command"`
	When            string `yaml:"when,omitempty" json:"when,omitempty"`
	Capture         string `yaml:"capture,omitempty" json:"capture,omitempty"`
	ContinueOnError bool   `yaml:"continue_on_error,omitempty" json:"continue_on_error,omitempty"`
}

// Hook defines success/failure handlers.
type Hook struct {
	Message string `yaml:"message" json:"message"`
}

// StepResult captures the outcome of a single step execution.
type StepResult struct {
	Name   string `json:"name"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
	Status string `json:"status"` // "success", "failed", "skipped"
}

// ExecutionResult captures the outcome of running a full recipe.
type ExecutionResult struct {
	Recipe  string       `json:"recipe"`
	Status  string       `json:"status"` // "success", "failed"
	Steps   []StepResult `json:"steps"`
	Message string       `json:"message,omitempty"`
}

// CommandDispatcher is the function signature for dispatching a command string
// to the CLI command tree. It returns stdout output and any error.
// The dispatcher receives parsed command args (not a raw shell string).
type CommandDispatcher func(args []string) (string, error)

// Parse reads and parses a recipe from YAML bytes.
func Parse(data []byte) (*Recipe, error) {
	var r Recipe
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("invalid recipe YAML: %w", err)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

// ParseFile reads and parses a recipe from a YAML file.
func ParseFile(path string) (*Recipe, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read recipe file %s: %w", path, err)
	}
	return Parse(data)
}

// Validate checks that a recipe has all required fields.
func (r *Recipe) Validate() error {
	var errs []string
	if r.Name == "" {
		errs = append(errs, "recipe name is required")
	}
	if len(r.Steps) == 0 {
		errs = append(errs, "recipe must have at least one step")
	}
	for i, s := range r.Steps {
		if s.Name == "" {
			errs = append(errs, fmt.Sprintf("step %d: name is required", i+1))
		}
		if s.Command == "" {
			errs = append(errs, fmt.Sprintf("step %d (%s): command is required", i+1, s.Name))
		}
	}
	for i, p := range r.Parameters {
		if p.Name == "" {
			errs = append(errs, fmt.Sprintf("parameter %d: name is required", i+1))
		}
		if p.Type != "" && p.Type != "string" && p.Type != "bool" && p.Type != "int" {
			errs = append(errs, fmt.Sprintf("parameter %q: invalid type %q (must be string, bool, or int)", p.Name, p.Type))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("recipe validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// ResolveParams merges provided parameter values with defaults and validates
// that all required parameters are present. Returns the merged parameter map.
func (r *Recipe) ResolveParams(provided map[string]string) (map[string]string, error) {
	resolved := make(map[string]string)

	// Apply defaults first.
	for _, p := range r.Parameters {
		if p.Default != "" {
			resolved[p.Name] = p.Default
		}
	}

	// Override with provided values.
	for k, v := range provided {
		resolved[k] = v
	}

	// Check required parameters.
	var missing []string
	for _, p := range r.Parameters {
		if p.Required {
			if _, ok := resolved[p.Name]; !ok {
				missing = append(missing, p.Name)
			}
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required parameters: %s", strings.Join(missing, ", "))
	}

	return resolved, nil
}

// renderTemplate renders a Go text/template string with the given data.
// The data map contains parameter values and captured step outputs.
func renderTemplate(tmplStr string, data map[string]string) (string, error) {
	if !strings.Contains(tmplStr, "{{") {
		return tmplStr, nil // fast path: no template syntax
	}
	t, err := template.New("").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template render error: %w", err)
	}
	return buf.String(), nil
}

// evaluateWhen evaluates a step's "when" condition. The condition is a
// Go template expression that should render to a truthy value.
// Empty string, "false", "0", and "<no value>" are falsy; everything else is truthy.
func evaluateWhen(when string, data map[string]string) (bool, error) {
	if when == "" {
		return true, nil // no condition means always run
	}
	result, err := renderTemplate(when, data)
	if err != nil {
		return false, fmt.Errorf("when condition error: %w", err)
	}
	result = strings.TrimSpace(result)
	switch result {
	case "", "false", "0", "<no value>":
		return false, nil
	default:
		return true, nil
	}
}

// parseCommandArgs splits a rendered command string into arguments.
// It handles quoted strings (single and double quotes).
func parseCommandArgs(cmdStr string) []string {
	var args []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	for i := 0; i < len(cmdStr); i++ {
		c := cmdStr[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == ' ' && !inSingle && !inDouble:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

// Execute runs all steps of a recipe sequentially using the provided dispatcher.
// It writes progress to progressW and returns the execution result.
func (r *Recipe) Execute(dispatcher CommandDispatcher, params map[string]string, progressW io.Writer) (*ExecutionResult, error) {
	resolved, err := r.ResolveParams(params)
	if err != nil {
		return nil, err
	}

	// Data map starts with resolved params; captured outputs are added as steps run.
	data := make(map[string]string)
	for k, v := range resolved {
		data[k] = v
	}

	result := &ExecutionResult{
		Recipe: r.Name,
		Status: "success",
		Steps:  make([]StepResult, 0, len(r.Steps)),
	}

	for i, step := range r.Steps {
		stepNum := i + 1
		totalSteps := len(r.Steps)

		// Evaluate the "when" condition.
		shouldRun, err := evaluateWhen(step.When, data)
		if err != nil {
			result.Steps = append(result.Steps, StepResult{
				Name:   step.Name,
				Status: "failed",
				Error:  err.Error(),
			})
			result.Status = "failed"
			if r.OnFailure != nil {
				data["failed_step"] = step.Name
				msg, _ := renderTemplate(r.OnFailure.Message, data)
				result.Message = msg
			}
			return result, nil
		}
		if !shouldRun {
			fmt.Fprintf(progressW, "[%d/%d] %s... skipped\n", stepNum, totalSteps, step.Name)
			result.Steps = append(result.Steps, StepResult{
				Name:   step.Name,
				Status: "skipped",
			})
			continue
		}

		// Render the command template.
		rendered, err := renderTemplate(step.Command, data)
		if err != nil {
			result.Steps = append(result.Steps, StepResult{
				Name:   step.Name,
				Status: "failed",
				Error:  err.Error(),
			})
			result.Status = "failed"
			if r.OnFailure != nil {
				data["failed_step"] = step.Name
				msg, _ := renderTemplate(r.OnFailure.Message, data)
				result.Message = msg
			}
			return result, nil
		}

		// Parse into args and dispatch.
		args := parseCommandArgs(rendered)
		fmt.Fprintf(progressW, "[%d/%d] %s...", stepNum, totalSteps, step.Name)

		output, err := dispatcher(args)
		if err != nil {
			fmt.Fprintf(progressW, " failed\n")
			result.Steps = append(result.Steps, StepResult{
				Name:   step.Name,
				Output: output,
				Status: "failed",
				Error:  err.Error(),
			})

			if step.ContinueOnError {
				continue
			}

			result.Status = "failed"
			if r.OnFailure != nil {
				data["failed_step"] = step.Name
				msg, _ := renderTemplate(r.OnFailure.Message, data)
				result.Message = msg
			}
			return result, nil
		}

		fmt.Fprintf(progressW, " done\n")

		// Capture output if requested.
		if step.Capture != "" {
			data[step.Capture] = strings.TrimSpace(output)
		}

		result.Steps = append(result.Steps, StepResult{
			Name:   step.Name,
			Output: output,
			Status: "success",
		})
	}

	// All steps completed successfully.
	if r.OnSuccess != nil {
		msg, _ := renderTemplate(r.OnSuccess.Message, data)
		result.Message = msg
	}

	return result, nil
}

// Plan previews all steps without executing them. Returns the rendered
// commands for each step after template expansion.
func (r *Recipe) Plan(params map[string]string) ([]StepPlan, error) {
	resolved, err := r.ResolveParams(params)
	if err != nil {
		return nil, err
	}

	data := make(map[string]string)
	for k, v := range resolved {
		data[k] = v
	}

	var plans []StepPlan
	for _, step := range r.Steps {
		rendered, err := renderTemplate(step.Command, data)
		if err != nil {
			return nil, fmt.Errorf("step %q template error: %w", step.Name, err)
		}

		sp := StepPlan{
			Name:    step.Name,
			Command: rendered,
		}

		if step.When != "" {
			sp.When = step.When
			shouldRun, _ := evaluateWhen(step.When, data)
			sp.WouldRun = shouldRun
		} else {
			sp.WouldRun = true
		}

		plans = append(plans, sp)
	}
	return plans, nil
}

// StepPlan describes a single step's plan preview.
type StepPlan struct {
	Name     string `json:"name"`
	Command  string `json:"command"`
	When     string `json:"when,omitempty"`
	WouldRun bool   `json:"would_run"`
}

// RenderPlanJSON writes the plan as structured JSON.
func RenderPlanJSON(w io.Writer, plans []StepPlan) error {
	out, err := json.MarshalIndent(plans, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}

// RenderPlanHuman writes a human-readable plan preview.
func RenderPlanHuman(w io.Writer, recipeName string, plans []StepPlan) {
	fmt.Fprintf(w, "Recipe: %s\n", recipeName)
	fmt.Fprintf(w, "Steps: %d\n\n", len(plans))
	for i, p := range plans {
		status := "will run"
		if !p.WouldRun {
			status = "will skip"
		}
		fmt.Fprintf(w, "  [%d] %s (%s)\n", i+1, p.Name, status)
		fmt.Fprintf(w, "      jc %s\n", p.Command)
		if p.When != "" {
			fmt.Fprintf(w, "      when: %s\n", p.When)
		}
	}
	fmt.Fprintln(w, "\nNo changes made (plan mode).")
}

// RecipesDir returns the directory where user-defined recipes are stored.
// It is a variable to allow test overrides.
var RecipesDir = func() string {
	return filepath.Join(config.ConfigDir(), "recipes")
}

// LoadFromDir loads all recipe files (*.yaml, *.yml) from a directory.
func LoadFromDir(dir string) ([]*Recipe, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read recipes directory %s: %w", dir, err)
	}

	var recipes []*Recipe
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		r, err := ParseFile(filepath.Join(dir, name))
		if err != nil {
			// Log warning but continue loading other recipes.
			fmt.Fprintf(os.Stderr, "Warning: skipping invalid recipe %s: %v\n", name, err)
			continue
		}
		recipes = append(recipes, r)
	}
	return recipes, nil
}

// FindByName searches for a recipe by name in the given list.
func FindByName(recipes []*Recipe, name string) *Recipe {
	for _, r := range recipes {
		if r.Name == name {
			return r
		}
	}
	return nil
}

// CobraCommand is the interface for a Cobra-like command used by the dispatcher.
type CobraCommand interface {
	SetArgs(a []string)
	SetOut(w io.Writer)
	SetErr(w io.Writer)
	Execute() error
}

// NewDispatcher creates a CommandDispatcher that dispatches commands to the
// CLI command tree. The newRootCmd function creates a fresh root command
// for each dispatch call, ensuring isolated flag state per step.
func NewDispatcher(newRootCmd func() CobraCommand) CommandDispatcher {
	return func(args []string) (string, error) {
		cmd := newRootCmd()
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs(args)

		err := cmd.Execute()
		output := stdout.String()
		if err != nil {
			// Include stderr in error message for context.
			errMsg := err.Error()
			if s := strings.TrimSpace(stderr.String()); s != "" {
				errMsg = s
			}
			return output, errors.New(errMsg)
		}
		return output, nil
	}
}
