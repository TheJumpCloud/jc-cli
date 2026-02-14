package recipe

import (
	"embed"
	"fmt"
	"os"
	"sort"
	"strings"
)

//go:embed builtin/*.yaml
var builtinFS embed.FS

// LoadBuiltIn loads all built-in recipe files embedded in the binary.
func LoadBuiltIn() ([]*Recipe, error) {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		return nil, fmt.Errorf("cannot read built-in recipes: %w", err)
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
		data, err := builtinFS.ReadFile("builtin/" + name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: cannot read built-in recipe %s: %v\n", name, err)
			continue
		}
		r, err := Parse(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: invalid built-in recipe %s: %v\n", name, err)
			continue
		}
		recipes = append(recipes, r)
	}

	sort.Slice(recipes, func(i, j int) bool {
		return recipes[i].Name < recipes[j].Name
	})
	return recipes, nil
}

// LoadAll loads both built-in and user-defined recipes. User-defined recipes
// with the same name as a built-in recipe take precedence.
func LoadAll() ([]*Recipe, error) {
	builtin, err := LoadBuiltIn()
	if err != nil {
		return nil, err
	}

	user, err := LoadFromDir(RecipesDir())
	if err != nil {
		return nil, err
	}

	// Index user recipes by name for override lookup.
	userByName := make(map[string]*Recipe, len(user))
	for _, r := range user {
		userByName[r.Name] = r
	}

	// Start with built-in, overriding with user-defined when names match.
	var all []*Recipe
	seen := make(map[string]bool)
	for _, r := range builtin {
		if override, ok := userByName[r.Name]; ok {
			all = append(all, override)
		} else {
			all = append(all, r)
		}
		seen[r.Name] = true
	}

	// Add user-defined recipes that don't override built-in ones.
	for _, r := range user {
		if !seen[r.Name] {
			all = append(all, r)
		}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Name < all[j].Name
	})
	return all, nil
}
