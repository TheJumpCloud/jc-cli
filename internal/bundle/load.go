package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/klaassen-consulting/jc/internal/config"
)

// Origin values the loader stamps onto Source.Origin when the author
// left it blank.
const (
	OriginBuiltin = "builtin"
	OriginUser    = "user"
)

// BundlesDir returns the directory holding user-defined bundles. A
// variable so tests can override it (same pattern as recipe.RecipesDir).
var BundlesDir = func() string {
	return filepath.Join(config.ConfigDir(), "bundles")
}

// LoadFromDir loads all bundle files (*.yaml, *.yml) from a directory.
// Invalid files are skipped with a stderr warning rather than failing
// the whole load — one broken user file must not take down `jc bundle
// list` (recipe loader precedent).
func LoadFromDir(dir string) ([]*Bundle, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read bundles directory %s: %w", dir, err)
	}

	var bundles []*Bundle
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		b, err := ParseFile(filepath.Join(dir, name))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping invalid bundle %s: %v\n", name, err)
			continue
		}
		stampOrigin(b, OriginUser)
		bundles = append(bundles, b)
	}
	return bundles, nil
}

// LoadAll returns builtin + user bundles; a user bundle with the same
// name overrides the builtin (the escape hatch for tweaking a shipped
// baseline without forking the binary).
func LoadAll() ([]*Bundle, error) {
	builtin, err := LoadBuiltIn()
	if err != nil {
		return nil, err
	}
	user, err := LoadFromDir(BundlesDir())
	if err != nil {
		return nil, err
	}

	userByName := make(map[string]*Bundle, len(user))
	for _, b := range user {
		userByName[b.Name] = b
	}

	var all []*Bundle
	seen := make(map[string]bool)
	for _, b := range builtin {
		if override, ok := userByName[b.Name]; ok {
			all = append(all, override)
		} else {
			all = append(all, b)
		}
		seen[b.Name] = true
	}
	for _, b := range user {
		if !seen[b.Name] {
			all = append(all, b)
		}
	}

	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all, nil
}

// FindByName returns the bundle with the given name, or nil.
func FindByName(bundles []*Bundle, name string) *Bundle {
	for _, b := range bundles {
		if b.Name == name {
			return b
		}
	}
	return nil
}

// stampOrigin fills Source.Origin when the file didn't declare one, so
// list/show always report where a bundle came from. An explicit
// authored origin (e.g. "imported" from a future converter) wins.
func stampOrigin(b *Bundle, origin string) {
	if b.Source == nil {
		b.Source = &Source{}
	}
	if b.Source.Origin == "" {
		b.Source.Origin = origin
	}
}
