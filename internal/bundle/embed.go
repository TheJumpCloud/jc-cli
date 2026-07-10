package bundle

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

// Builtin bundles ship in the binary. Licensing gate (KLA-468): only
// content we may redistribute goes here — original jc-cli-authored
// examples, NIST mSCP derivations (CC BY 4.0, attribution required),
// DISA STIG derivations (public domain). Never CIS Benchmark content
// or branding.
//
//go:embed builtin/*.yaml
var builtinFS embed.FS

// LoadBuiltIn loads the bundles embedded in the binary. Unlike user
// files, a broken builtin is a packaging bug, so it fails loudly
// instead of warn-and-skip (the embed test catches it before release).
func LoadBuiltIn() ([]*Bundle, error) {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		return nil, fmt.Errorf("cannot read built-in bundles: %w", err)
	}

	var bundles []*Bundle
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		data, err := builtinFS.ReadFile("builtin/" + name)
		if err != nil {
			return nil, fmt.Errorf("reading built-in bundle %s: %w", name, err)
		}
		b, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("built-in bundle %s: %w", name, err)
		}
		stampOrigin(b, OriginBuiltin)
		bundles = append(bundles, b)
	}

	sort.Slice(bundles, func(i, j int) bool { return bundles[i].Name < bundles[j].Name })
	return bundles, nil
}
