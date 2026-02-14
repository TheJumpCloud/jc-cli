package recipe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBuiltIn_ReturnsAllRecipes(t *testing.T) {
	recipes, err := LoadBuiltIn()
	if err != nil {
		t.Fatalf("LoadBuiltIn failed: %v", err)
	}
	if len(recipes) < 10 {
		t.Errorf("Expected at least 10 built-in recipes, got %d", len(recipes))
	}
}

func TestLoadBuiltIn_RecipeNames(t *testing.T) {
	recipes, err := LoadBuiltIn()
	if err != nil {
		t.Fatalf("LoadBuiltIn failed: %v", err)
	}

	expected := []string{
		"audit-inactive-users",
		"audit-unmanaged-devices",
		"bulk-create-users",
		"compliance-report",
		"group-sync",
		"mfa-enforcement-check",
		"offboard-user",
		"onboard-user",
		"password-expiry-report",
		"security-audit",
		"stale-device-cleanup",
	}

	names := make(map[string]bool)
	for _, r := range recipes {
		names[r.Name] = true
	}

	for _, name := range expected {
		if !names[name] {
			t.Errorf("Missing built-in recipe: %s", name)
		}
	}
}

func TestLoadBuiltIn_SortedByName(t *testing.T) {
	recipes, err := LoadBuiltIn()
	if err != nil {
		t.Fatalf("LoadBuiltIn failed: %v", err)
	}
	for i := 1; i < len(recipes); i++ {
		if recipes[i].Name < recipes[i-1].Name {
			t.Errorf("Recipes not sorted: %q comes after %q", recipes[i].Name, recipes[i-1].Name)
		}
	}
}

func TestLoadBuiltIn_AllRecipesValid(t *testing.T) {
	recipes, err := LoadBuiltIn()
	if err != nil {
		t.Fatalf("LoadBuiltIn failed: %v", err)
	}
	for _, r := range recipes {
		if r.Name == "" {
			t.Error("Found recipe with empty name")
		}
		if len(r.Steps) == 0 {
			t.Errorf("Recipe %q has no steps", r.Name)
		}
		if r.Description == "" {
			t.Errorf("Recipe %q has no description", r.Name)
		}
		if r.Author == "" {
			t.Errorf("Recipe %q has no author", r.Name)
		}
		if r.Version == "" {
			t.Errorf("Recipe %q has no version", r.Name)
		}
		if len(r.Tags) == 0 {
			t.Errorf("Recipe %q has no tags", r.Name)
		}
		// Validate succeeds (already called by Parse, but be explicit).
		if err := r.Validate(); err != nil {
			t.Errorf("Recipe %q validation failed: %v", r.Name, err)
		}
	}
}

func TestLoadBuiltIn_OnboardUser(t *testing.T) {
	recipes, err := LoadBuiltIn()
	if err != nil {
		t.Fatalf("LoadBuiltIn failed: %v", err)
	}
	r := FindByName(recipes, "onboard-user")
	if r == nil {
		t.Fatal("onboard-user recipe not found")
	}

	if r.Description == "" {
		t.Error("onboard-user has no description")
	}

	// Check required parameters.
	requiredParams := map[string]bool{"username": false, "email": false, "firstname": false, "lastname": false}
	for _, p := range r.Parameters {
		if _, ok := requiredParams[p.Name]; ok {
			if !p.Required {
				t.Errorf("Parameter %q should be required", p.Name)
			}
			requiredParams[p.Name] = true
		}
	}
	for name, found := range requiredParams {
		if !found {
			t.Errorf("Missing required parameter: %s", name)
		}
	}

	// Should have create, optionally add-to-group, and verify steps.
	if len(r.Steps) < 2 {
		t.Errorf("Expected at least 2 steps, got %d", len(r.Steps))
	}

	if r.OnSuccess == nil {
		t.Error("onboard-user has no on_success hook")
	}
	if r.OnFailure == nil {
		t.Error("onboard-user has no on_failure hook")
	}
}

func TestLoadBuiltIn_OffboardUser(t *testing.T) {
	recipes, err := LoadBuiltIn()
	if err != nil {
		t.Fatalf("LoadBuiltIn failed: %v", err)
	}
	r := FindByName(recipes, "offboard-user")
	if r == nil {
		t.Fatal("offboard-user recipe not found")
	}

	// User parameter is required.
	var hasUserParam bool
	for _, p := range r.Parameters {
		if p.Name == "user" && p.Required {
			hasUserParam = true
		}
	}
	if !hasUserParam {
		t.Error("offboard-user missing required 'user' parameter")
	}

	// Should have lock, remove-groups, reset-mfa steps.
	if len(r.Steps) < 3 {
		t.Errorf("Expected at least 3 steps, got %d", len(r.Steps))
	}
}

func TestLoadBuiltIn_AllStepsHaveCommands(t *testing.T) {
	recipes, err := LoadBuiltIn()
	if err != nil {
		t.Fatalf("LoadBuiltIn failed: %v", err)
	}
	for _, r := range recipes {
		for i, step := range r.Steps {
			if step.Name == "" {
				t.Errorf("Recipe %q step %d has empty name", r.Name, i+1)
			}
			if step.Command == "" {
				t.Errorf("Recipe %q step %q has empty command", r.Name, step.Name)
			}
		}
	}
}

func TestLoadBuiltIn_ParameterTypesValid(t *testing.T) {
	recipes, err := LoadBuiltIn()
	if err != nil {
		t.Fatalf("LoadBuiltIn failed: %v", err)
	}
	validTypes := map[string]bool{"": true, "string": true, "bool": true, "int": true}
	for _, r := range recipes {
		for _, p := range r.Parameters {
			if !validTypes[p.Type] {
				t.Errorf("Recipe %q parameter %q has invalid type: %q", r.Name, p.Name, p.Type)
			}
		}
	}
}

func TestLoadAll_BuiltInOnly(t *testing.T) {
	// Point RecipesDir to an empty temp dir so only built-in recipes load.
	dir := t.TempDir()
	origDir := RecipesDir
	RecipesDir = func() string { return dir }
	t.Cleanup(func() { RecipesDir = origDir })

	recipes, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(recipes) < 10 {
		t.Errorf("Expected at least 10 recipes, got %d", len(recipes))
	}
}

func TestLoadAll_UserOverridesBuiltIn(t *testing.T) {
	dir := t.TempDir()
	origDir := RecipesDir
	RecipesDir = func() string { return dir }
	t.Cleanup(func() { RecipesDir = origDir })

	// Write a user recipe that overrides onboard-user.
	userRecipe := `
name: onboard-user
description: Custom onboard recipe
steps:
  - name: custom-step
    command: users list
`
	if err := os.WriteFile(filepath.Join(dir, "onboard-user.yaml"), []byte(userRecipe), 0600); err != nil {
		t.Fatal(err)
	}

	recipes, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	r := FindByName(recipes, "onboard-user")
	if r == nil {
		t.Fatal("onboard-user not found")
	}
	if r.Description != "Custom onboard recipe" {
		t.Errorf("Expected user override, got description: %q", r.Description)
	}
	if len(r.Steps) != 1 || r.Steps[0].Name != "custom-step" {
		t.Error("Expected custom step from user override")
	}
}

func TestLoadAll_UserAddsNewRecipe(t *testing.T) {
	dir := t.TempDir()
	origDir := RecipesDir
	RecipesDir = func() string { return dir }
	t.Cleanup(func() { RecipesDir = origDir })

	// Write a user recipe with a new name.
	userRecipe := `
name: my-custom-recipe
description: A custom recipe
steps:
  - name: step-one
    command: users list
`
	if err := os.WriteFile(filepath.Join(dir, "custom.yaml"), []byte(userRecipe), 0600); err != nil {
		t.Fatal(err)
	}

	recipes, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	r := FindByName(recipes, "my-custom-recipe")
	if r == nil {
		t.Fatal("my-custom-recipe not found")
	}
	if r.Description != "A custom recipe" {
		t.Errorf("Description = %q, want %q", r.Description, "A custom recipe")
	}
}

func TestLoadAll_SortedByName(t *testing.T) {
	dir := t.TempDir()
	origDir := RecipesDir
	RecipesDir = func() string { return dir }
	t.Cleanup(func() { RecipesDir = origDir })

	recipes, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	for i := 1; i < len(recipes); i++ {
		if recipes[i].Name < recipes[i-1].Name {
			t.Errorf("Recipes not sorted: %q comes after %q", recipes[i].Name, recipes[i-1].Name)
		}
	}
}

func TestLoadAll_EmptyUserDir(t *testing.T) {
	dir := t.TempDir()
	origDir := RecipesDir
	RecipesDir = func() string { return dir }
	t.Cleanup(func() { RecipesDir = origDir })

	recipes, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	// Should still have all built-in recipes.
	builtIn, _ := LoadBuiltIn()
	if len(recipes) != len(builtIn) {
		t.Errorf("Expected %d recipes (built-in only), got %d", len(builtIn), len(recipes))
	}
}

func TestLoadAll_NonExistentUserDir(t *testing.T) {
	origDir := RecipesDir
	RecipesDir = func() string { return "/nonexistent/recipes/dir" }
	t.Cleanup(func() { RecipesDir = origDir })

	recipes, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	// Should still have all built-in recipes.
	builtIn, _ := LoadBuiltIn()
	if len(recipes) != len(builtIn) {
		t.Errorf("Expected %d recipes (built-in only), got %d", len(builtIn), len(recipes))
	}
}

func TestLoadBuiltIn_RecipeTemplatesRenderable(t *testing.T) {
	recipes, err := LoadBuiltIn()
	if err != nil {
		t.Fatalf("LoadBuiltIn failed: %v", err)
	}

	for _, r := range recipes {
		// Build a dummy parameter map with all parameters set.
		params := make(map[string]string)
		for _, p := range r.Parameters {
			if p.Default != "" {
				params[p.Name] = p.Default
			} else {
				params[p.Name] = "test-value"
			}
		}

		// Plan should work (renders templates without executing).
		plans, err := r.Plan(params)
		if err != nil {
			t.Errorf("Recipe %q plan failed: %v", r.Name, err)
			continue
		}
		if len(plans) != len(r.Steps) {
			t.Errorf("Recipe %q: plan returned %d steps, expected %d", r.Name, len(plans), len(r.Steps))
		}
	}
}
