package mcp

import "github.com/klaassen-consulting/jc/internal/recipe"

// RecipeDispatcher is the command dispatcher used to execute recipe steps
// from the MCP `recipe_run` tool (when called with Execute: true) and from
// the recipe_runner_view MCP App. Mirrors the
// internal/tui/screen.RecipeDispatcher hook: the cmd package wires it at
// startup with recipe.NewDispatcher(newRootCmdForRecipeStep), keeping the
// dependency one-way (cmd → mcp) and avoiding an import cycle.
//
// When nil (e.g. MCP server constructed in tests without the cmd wiring),
// recipe_run with Execute: true fails closed with a clear error rather
// than panicking. Plan-mode (Execute: false) doesn't need the dispatcher
// and works regardless.
var RecipeDispatcher recipe.CommandDispatcher
