package tui

import "testing"

// TestBuildRegistry_IncludesRecipesEntry ensures the Recipes virtual entry is
// registered on the home grid and lives in the Workflows category.
func TestBuildRegistry_IncludesRecipesEntry(t *testing.T) {
	entries := BuildRegistry()
	var found *ResourceEntry
	for i := range entries {
		if entries[i].Key == "recipes" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected 'recipes' entry in registry, not found")
	}
	if found.DisplayName != "Recipes" {
		t.Errorf("DisplayName = %q, want 'Recipes'", found.DisplayName)
	}
	if found.Category != CategoryWorkflows {
		t.Errorf("Category = %q, want %q", found.Category, CategoryWorkflows)
	}
	// Virtual entries have no API endpoint or schema.
	if found.ListEndpoint != "" {
		t.Errorf("ListEndpoint should be empty for virtual entry, got %q", found.ListEndpoint)
	}
}

// TestCategoryOrder_IncludesWorkflows guards the display-order addition.
func TestCategoryOrder_IncludesWorkflows(t *testing.T) {
	var found bool
	for _, c := range CategoryOrder {
		if c == CategoryWorkflows {
			found = true
			break
		}
	}
	if !found {
		t.Error("CategoryWorkflows missing from CategoryOrder")
	}
}
