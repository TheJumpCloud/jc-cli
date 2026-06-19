package apple_mdm

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
)

// SchemaRelease is the Apple tag the vendored schemas correspond to.
// Surfaced via Catalog.Release for diagnostics and the showcase site.
// To bump: see internal/apple_mdm/schemas/<tag>/NOTICE.md for the
// 7-step refresh procedure.
const SchemaRelease = "Release-v26.4"

// vendoredSchemas embeds every Configuration Profile YAML at build
// time. Bundling them in the binary means the catalog browser works
// offline, in a Docker image without network, and at the same speed on
// every host (no first-call parse penalty after init()).
//
//go:embed schemas/Release-v26.4/profiles/*.yaml
var vendoredSchemas embed.FS

// Catalog is the parsed, indexed view of every vendored Apple schema.
// One instance per process — DefaultCatalog is lazily built on first
// access via Default().
type Catalog struct {
	Release  string
	payloads []Payload
	byID     map[string]int // catalog ID (filename) → index
	byType   map[string]int // PayloadType → index of first occurrence
	// Warnings carries per-file parse failures and duplicate-type
	// notices. The catalog deliberately doesn't fail on these —
	// surface them via `jc apple-mdm payloads list --verbose` (TODO)
	// or via the future `payloads update` diff report. Pre-fix the
	// build returned them as a fatal error, which masked the entire
	// catalog if any one file had a quirk (2 Apple files use
	// self-referential YAML anchors that the parser can't handle).
	Warnings []string
}

// defaultOnce + defaultCatalog gate the lazy build. Building takes
// ~few milliseconds for 127 schemas (all YAML, no IO); deferring until
// first use keeps `jc` startup snappy when the user never touches the
// apple-mdm subcommand.
var (
	defaultOnce sync.Once
	defaultCat  *Catalog
	defaultErr  error
)

// Default returns the lazily-built catalog over the embedded schemas.
// Subsequent calls return the same instance.
func Default() (*Catalog, error) {
	defaultOnce.Do(func() {
		defaultCat, defaultErr = build(vendoredSchemas)
	})
	return defaultCat, defaultErr
}

func build(src fs.FS) (*Catalog, error) {
	root := path.Join("schemas", SchemaRelease, "profiles")
	entries, err := fs.ReadDir(src, root)
	if err != nil {
		return nil, fmt.Errorf("apple_mdm: reading %s: %w", root, err)
	}

	c := &Catalog{
		Release:  SchemaRelease,
		payloads: make([]Payload, 0, len(entries)),
		byID:     make(map[string]int, len(entries)),
		byType:   make(map[string]int, len(entries)),
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		p := path.Join(root, e.Name())
		id := strings.TrimSuffix(e.Name(), ".yaml")
		data, err := fs.ReadFile(src, p)
		if err != nil {
			c.Warnings = append(c.Warnings, fmt.Sprintf("%s: read: %v", e.Name(), err))
			continue
		}
		payload, err := ParsePayload(id, p, data)
		if err != nil {
			c.Warnings = append(c.Warnings, err.Error())
			continue
		}
		// IDs are filename-derived and must be globally unique. Two
		// matching IDs would mean two files with the same name — only
		// possible via an embed-directive bug; surface it loudly.
		if _, dup := c.byID[payload.ID]; dup {
			c.Warnings = append(c.Warnings, fmt.Sprintf("duplicate catalog ID %q (file %s)", payload.ID, e.Name()))
			continue
		}
		c.byID[payload.ID] = len(c.payloads)
		// First-wins on Type collisions (e.g. com.apple.MCX has 6
		// variants under different IDs). ByType returns the canonical
		// entry; per-variant access goes through ByID.
		if _, dup := c.byType[payload.Type]; !dup {
			c.byType[payload.Type] = len(c.payloads)
		}
		c.payloads = append(c.payloads, payload)
	}

	if len(c.payloads) == 0 {
		return nil, fmt.Errorf("apple_mdm: catalog is empty — embed directive may have stopped matching (had %d warnings)", len(c.Warnings))
	}
	return c, nil
}

// All returns every payload, ordered by Type for stable list output.
func (c *Catalog) All() []Payload {
	out := make([]Payload, len(c.payloads))
	copy(out, c.payloads)
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// ByType returns the first payload whose canonical PayloadType matches.
// Some payload types (notably com.apple.MCX) ship as multiple files;
// for per-variant access use ByID with the filename-derived ID.
func (c *Catalog) ByType(payloadtype string) (Payload, bool) {
	i, ok := c.byType[payloadtype]
	if !ok {
		return Payload{}, false
	}
	return c.payloads[i], true
}

// ByID returns the payload with the given catalog ID (filename without
// extension), or false if it doesn't exist. Use this for per-variant
// lookup when ByType is ambiguous.
func (c *Catalog) ByID(id string) (Payload, bool) {
	i, ok := c.byID[id]
	if !ok {
		return Payload{}, false
	}
	return c.payloads[i], true
}

// VariantsOf returns every payload sharing the given PayloadType,
// ordered by ID. Returns nil if no variant exists. Useful for
// disambiguating in the CLI when ByType is ambiguous (e.g. asking
// the user to pick a specific MCX variant by ID).
func (c *Catalog) VariantsOf(payloadtype string) []Payload {
	var out []Payload
	for _, p := range c.payloads {
		if p.Type == payloadtype {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// FilterOpts selects payloads for `jc apple-mdm payloads list`. Empty
// fields are no-ops (no filtering). Search matches Type, Title, and
// Description case-insensitively.
type FilterOpts struct {
	OS     string // e.g. "macOS", "iOS"; empty means "any platform"
	Search string // substring matched against Type, Title, Description
}

// Filter applies the options and returns the matching payloads, sorted
// by Type. Returns an empty slice if nothing matches.
func (c *Catalog) Filter(opts FilterOpts) []Payload {
	osNorm := strings.TrimSpace(opts.OS)
	q := strings.ToLower(strings.TrimSpace(opts.Search))

	var out []Payload
	for _, p := range c.payloads {
		if osNorm != "" {
			sup, ok := p.SupportedOS[osNorm]
			if !ok || !sup.Available() {
				continue
			}
		}
		if q != "" && !matchesSearch(p, q) {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// matchesSearch is a case-insensitive substring check over the three
// fields that show up in `payloads list` rows. Deliberately doesn't
// search PayloadKey names — that would mislead users into thinking
// the result is *narrowed* to that key, when in reality the whole
// payload still ships.
func matchesSearch(p Payload, q string) bool {
	return strings.Contains(strings.ToLower(p.Type), q) ||
		strings.Contains(strings.ToLower(p.Title), q) ||
		strings.Contains(strings.ToLower(p.Description), q)
}

// Len returns the total number of payloads in the catalog. Cheap
// accessor for diagnostics output (`jc apple-mdm payloads list` footer).
func (c *Catalog) Len() int { return len(c.payloads) }
