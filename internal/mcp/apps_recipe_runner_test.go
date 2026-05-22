package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/recipe"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fetchRecipeListData reads the embedded built-in recipes (no user
// recipes in the test process since RecipesDir lives under the user's
// XDG config). The built-in set ships with the repo, so the call is
// deterministic and doesn't need network or filesystem fixtures.
func TestFetchRecipeListData_ReturnsBuiltinCatalog(t *testing.T) {
	data, err := fetchRecipeListData()
	if err != nil {
		t.Fatalf("fetchRecipeListData: %v", err)
	}
	if len(data.Recipes) == 0 {
		t.Fatal("expected at least one built-in recipe, got 0")
	}

	// All built-ins must have a non-empty name + step count > 0 and be
	// labeled "builtin" since the test process has no user recipes
	// installed under JC_CONFIG (the tools test harness points at a
	// tmpdir).
	for _, r := range data.Recipes {
		if r.Name == "" {
			t.Errorf("recipe with empty name: %+v", r)
		}
		if r.StepCount == 0 {
			t.Errorf("recipe %q has 0 steps; LoadAll should reject empty recipes", r.Name)
		}
		if r.StepCount != len(r.StepNames) {
			t.Errorf("recipe %q step_count=%d but %d step_names", r.Name, r.StepCount, len(r.StepNames))
		}
		if r.Source != "builtin" {
			t.Errorf("recipe %q source=%q, want builtin (no user recipes in test env)", r.Name, r.Source)
		}
	}
}

func TestRecipeList_HasNoUIMetadata(t *testing.T) {
	// recipe_list is a plain tool, not an MCP App, so it shouldn't carry
	// a _meta.ui.resourceUri — that's reserved for tools that render an
	// iframe. Confirms the registration uses nil meta (the recipe_runner_view
	// MCP App is the one that gets the ui metadata).
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	result, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var found *mcp.Tool
	for _, tool := range result.Tools {
		if tool.Name == "recipe_list" {
			found = tool
			break
		}
	}
	if found == nil {
		t.Fatal("recipe_list tool missing")
	}
	if found.Meta != nil {
		if ui, ok := map[string]any(found.Meta)["ui"]; ok {
			t.Errorf("recipe_list should not carry _meta.ui (got %v) — that's the recipe_runner_view app's role", ui)
		}
	}
}

func TestRecipeList_ReturnsCatalog(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	result := callTool(t, cs, "recipe_list", map[string]any{})
	if result.IsError {
		t.Fatalf("recipe_list errored: %s", getResultText(t, result))
	}
	text := getResultText(t, result)
	var payload recipeListData
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal recipe_list result: %v\n%s", err, text)
	}
	if len(payload.Recipes) == 0 {
		t.Errorf("recipe_list returned no recipes")
	}
}

func TestRecipeRunnerView_HasUIMetadata(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	result, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var found *mcp.Tool
	for _, tool := range result.Tools {
		if tool.Name == "recipe_runner_view" {
			found = tool
			break
		}
	}
	if found == nil {
		t.Fatal("recipe_runner_view tool missing")
	}
	meta := map[string]any(found.Meta)
	ui, ok := meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("expected _meta.ui to be a map, got %T", meta["ui"])
	}
	if uri, _ := ui["resourceUri"].(string); uri != "ui://jc/recipe-runner" {
		t.Errorf("resourceUri = %q, want ui://jc/recipe-runner", uri)
	}
}

func TestRecipeRunnerResource_ServesHTMLWithInjection(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	result, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "ui://jc/recipe-runner"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(result.Contents) == 0 {
		t.Fatal("empty resource contents")
	}
	c := result.Contents[0]
	if c.MIMEType != mcpAppMIMEType {
		t.Errorf("MIME = %q, want %q", c.MIMEType, mcpAppMIMEType)
	}
	if !strings.Contains(c.Text, "window.jcApp") {
		t.Error("served HTML missing common.js injection")
	}
	if strings.Contains(c.Text, appCommonMarker) {
		t.Error("served HTML still contains injection marker")
	}
	if !strings.Contains(c.Text, "JumpCloud Recipe Runner") {
		t.Error("served HTML missing page title")
	}
}

// recordingDispatcher captures every dispatched arg slice and returns
// the configured per-recipe outputs. Used to verify recipe_run execute
// path wires through RecipeDispatcher correctly and that the chokepoint
// gates Execute: true behind step-up auth.
type recordingDispatcher struct {
	calls   [][]string
	outputs map[string]string // first-arg-as-key → stdout
	errs    map[string]error  // first-arg-as-key → err
}

func (r *recordingDispatcher) dispatch(args []string) (string, error) {
	r.calls = append(r.calls, args)
	key := ""
	if len(args) > 0 {
		key = args[0]
	}
	if err, ok := r.errs[key]; ok {
		return r.outputs[key], err
	}
	return r.outputs[key], nil
}

func TestRecipeRun_ExecuteTrueRunsDispatcher(t *testing.T) {
	setupToolTest(t)

	// Wire a recording dispatcher; restore the (nil) prior value after.
	rd := &recordingDispatcher{
		outputs: map[string]string{"ping": "pong"},
	}
	origDispatcher := RecipeDispatcher
	RecipeDispatcher = recipe.CommandDispatcher(rd.dispatch)
	t.Cleanup(func() { RecipeDispatcher = origDispatcher })

	cs := connectToolTestServer(t, Options{})

	// Use the built-in recipe whose steps are simple enough to drive
	// with the recording dispatcher. The jc-ping recipe (if present) or
	// any other zero-param recipe works; fall back to introspecting the
	// catalog if our chosen name has drifted.
	listRes := callTool(t, cs, "recipe_list", map[string]any{})
	var catalog recipeListData
	if err := json.Unmarshal([]byte(getResultText(t, listRes)), &catalog); err != nil {
		t.Fatalf("unmarshal catalog: %v", err)
	}

	// Find a recipe with no required parameters and at least one step.
	var pick *recipeListEntry
	for i := range catalog.Recipes {
		r := &catalog.Recipes[i]
		anyRequired := false
		for _, p := range r.Parameters {
			if p.Required {
				anyRequired = true
				break
			}
		}
		if !anyRequired && r.StepCount > 0 {
			pick = r
			break
		}
	}
	if pick == nil {
		t.Skip("no zero-required-param built-in recipe available; skipping execute round-trip")
	}

	result := callTool(t, cs, "recipe_run", map[string]any{
		"name":    pick.Name,
		"execute": true,
	})
	if result.IsError {
		// The recipe may have wanted real JumpCloud API responses;
		// what matters here is that the dispatcher was called, not
		// that the recipe succeeded against our mock.
		t.Logf("recipe_run errored as expected (mock dispatcher): %s", getResultText(t, result))
	}
	if len(rd.calls) == 0 {
		t.Errorf("Execute: true should have invoked the dispatcher; calls=%v", rd.calls)
	}
}

func TestRecipeRun_ExecuteFalsePreservesPlanBehavior(t *testing.T) {
	setupToolTest(t)

	// Plan-only path must NOT need the dispatcher. Setting it to nil
	// confirms the pre-KLA-406 contract (omit execute → safe preview)
	// still holds without execution wiring.
	origDispatcher := RecipeDispatcher
	RecipeDispatcher = nil
	t.Cleanup(func() { RecipeDispatcher = origDispatcher })

	cs := connectToolTestServer(t, Options{})

	listRes := callTool(t, cs, "recipe_list", map[string]any{})
	var catalog recipeListData
	if err := json.Unmarshal([]byte(getResultText(t, listRes)), &catalog); err != nil {
		t.Fatalf("unmarshal catalog: %v", err)
	}
	if len(catalog.Recipes) == 0 {
		t.Skip("no built-in recipes in test env")
	}
	pick := catalog.Recipes[0]

	// Build params with default values so required-param recipes still
	// plan cleanly.
	params := map[string]string{}
	for _, p := range pick.Parameters {
		if p.Default != "" {
			params[p.Name] = p.Default
		} else if p.Required {
			params[p.Name] = "test-value"
		}
	}

	result := callTool(t, cs, "recipe_run", map[string]any{
		"name":    pick.Name,
		"params":  params,
		"execute": false,
	})
	if result.IsError {
		t.Fatalf("plan-only path errored: %s", getResultText(t, result))
	}
	// The pre-KLA-406 shape returns a []StepPlan, so the result text
	// should parse as a JSON array (object would mean execution path
	// accidentally fired).
	text := getResultText(t, result)
	if !strings.HasPrefix(strings.TrimSpace(text), "[") {
		t.Errorf("plan-only result should be a JSON array of plans, got: %s", text)
	}
}

func TestRecipeRun_ExecuteFailsWhenDispatcherUnwired(t *testing.T) {
	setupToolTest(t)

	origDispatcher := RecipeDispatcher
	RecipeDispatcher = nil
	t.Cleanup(func() { RecipeDispatcher = origDispatcher })

	cs := connectToolTestServer(t, Options{})

	listRes := callTool(t, cs, "recipe_list", map[string]any{})
	var catalog recipeListData
	if err := json.Unmarshal([]byte(getResultText(t, listRes)), &catalog); err != nil {
		t.Fatalf("unmarshal catalog: %v", err)
	}
	if len(catalog.Recipes) == 0 {
		t.Skip("no built-in recipes")
	}

	// Pick any recipe; the dispatcher-nil check fires before param resolution.
	result := callTool(t, cs, "recipe_run", map[string]any{
		"name":    catalog.Recipes[0].Name,
		"execute": true,
	})
	if !result.IsError {
		t.Fatal("expected execute=true with nil dispatcher to error, got success")
	}
	text := getResultText(t, result)
	if !strings.Contains(text, "RecipeDispatcher") || !strings.Contains(text, "server-config issue") {
		t.Errorf("error message should clearly attribute to server config; got: %s", text)
	}
}
