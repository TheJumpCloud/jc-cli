package windows_mdm

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Fetch-on-demand snapshot pipeline (KLA-460).
//
// Unlike the Apple catalog (MIT-licensed, vendored, //go:embed),
// Microsoft's DDF drop carries no redistribution license — so the
// binary ships with ZERO Microsoft content. Instead, the pinned
// snapshot zip is downloaded from Microsoft's official URL on first
// use (or via `jc windows-mdm csp update`), SHA-256-verified, and
// cached under the user's cache dir. Everything after that one fetch
// is offline. Air-gapped hosts can pre-place the zip at
// <cachedir>/<SnapshotName>.zip and jc will use it without a network
// call.

// Pinned snapshot. Refreshing to a newer Microsoft drop means
// updating all three values together (and re-verifying the parse
// tests) — same one-commit discipline as the Apple vendor bump.
const (
	// SnapshotName identifies the pinned DDF drop; it's the cache
	// subdirectory name and the manual-placement zip filename.
	SnapshotName = "DDFv2Feb2026"
	// SnapshotURL is Microsoft's official download for the pinned drop
	// (learn.microsoft.com → configuration-service-provider-ddf).
	SnapshotURL = "https://download.microsoft.com/download/015bd9f5-9cca-4821-8a85-a4c5f9a5d0f2/DDFv2Feb2026.zip"
	// SnapshotSHA256 pins the zip bytes. Recorded 2026-07-08 during
	// the KLA-460 empirical gate.
	SnapshotSHA256 = "bf667d895af4a8c8ab5a31065ce0e28ea2f8b649c4dc416f452f62fd1c42ff14"
)

// snapshotMarker is written after a successful extract; its presence
// means the cache dir is complete (a crash mid-extract leaves no
// marker, and the next run re-extracts).
const snapshotMarker = ".complete"

// CacheDir returns the directory the extracted snapshot lives in:
// $XDG_CACHE_HOME/jc/windows-mdm-ddf/<SnapshotName> (or ~/.cache/...),
// matching the resolve package's cache-root convention.
func CacheDir() string {
	root := os.Getenv("XDG_CACHE_HOME")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".cache")
	}
	return filepath.Join(root, "jc", "windows-mdm-ddf", SnapshotName)
}

// EnsureSnapshot makes sure the pinned DDF snapshot is extracted into
// dir (CacheDir() unless overridden via the dir argument — tests pass
// a temp dir). Returns the directory holding the *_AreaDDF.xml files.
//
// Resolution order:
//  1. dir already extracted (marker present) → done, no network.
//  2. a manually pre-placed <SnapshotName>.zip next to dir → verify +
//     extract, no network (the air-gapped path).
//  3. download from SnapshotURL → verify SHA-256 → extract.
//
// progress, when non-nil, receives one-line status updates (the CLI
// passes a stderr printer; MCP passes nil).
func EnsureSnapshot(ctx context.Context, dir string, progress func(string)) (string, error) {
	if dir == "" {
		dir = CacheDir()
	}
	if _, err := os.Stat(filepath.Join(dir, snapshotMarker)); err == nil {
		return dir, nil
	}
	note := func(msg string) {
		if progress != nil {
			progress(msg)
		}
	}

	zipPath := dir + ".zip"
	if _, err := os.Stat(zipPath); err != nil {
		note(fmt.Sprintf("Downloading Microsoft Policy CSP DDF snapshot %s (~700KB, one-time)...", SnapshotName))
		if err := downloadSnapshot(ctx, zipPath); err != nil {
			return "", fmt.Errorf(
				"downloading DDF snapshot: %w\n(air-gapped? manually download %s and place it at %s)",
				err, SnapshotURL, zipPath)
		}
	} else {
		note("Using pre-placed snapshot zip at " + zipPath)
	}

	if err := verifySnapshotSHA256(zipPath); err != nil {
		return "", err
	}
	note("Verified SHA-256; extracting...")
	if err := extractAreaDDFs(zipPath, dir); err != nil {
		return "", fmt.Errorf("extracting DDF snapshot: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, snapshotMarker), []byte(SnapshotSHA256+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("finalizing snapshot cache: %w", err)
	}
	return dir, nil
}

func downloadSnapshot(ctx context.Context, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, SnapshotURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", SnapshotURL, resp.StatusCode)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".jc-ddf-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	var ok bool
	defer func() {
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return err
	}
	ok = true
	return nil
}

func verifySnapshotSHA256(zipPath string) error {
	f, err := os.Open(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != SnapshotSHA256 {
		return fmt.Errorf(
			"DDF snapshot %s failed SHA-256 verification:\n  got  %s\n  want %s\nThe file at %s is not the pinned Microsoft drop — delete it and retry, or update the pin if Microsoft re-published the snapshot",
			SnapshotName, got, SnapshotSHA256, zipPath)
	}
	return nil
}

// extractAreaDDFs unpacks only the Policy-area DDF files
// (*_AreaDDF.xml) — the standalone CSP files (BitLocker.xml, VPNv2.xml,
// …) describe full CSPs, not Policy CSP areas, and are out of scope
// for this catalog. Paths are flattened and validated (zip-slip).
func extractAreaDDFs(zipPath, dir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	count := 0
	for _, f := range r.File {
		base := filepath.Base(f.Name)
		if !strings.HasSuffix(base, "_AreaDDF.xml") || f.FileInfo().IsDir() {
			continue
		}
		// filepath.Base above already defeats zip-slip; keep the
		// explicit guard anyway in case the extraction logic changes.
		target := filepath.Join(dir, base)
		if !strings.HasPrefix(target, filepath.Clean(dir)+string(os.PathSeparator)) {
			return fmt.Errorf("zip entry %q escapes extraction dir", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
		count++
	}
	if count == 0 {
		return fmt.Errorf("zip contained no *_AreaDDF.xml files — wrong archive?")
	}
	return nil
}

// ── catalog ────────────────────────────────────────────────────────

// Catalog is the parsed, indexed Policy CSP settings collection.
type Catalog struct {
	// Snapshot names the pinned Microsoft drop the data came from.
	Snapshot string
	settings []Setting
	byRef    map[string]int      // "Area/Name" (lowercased) → index
	byArea   map[string][]int    // lowercased area → indices
	areas    []string            // sorted canonical area names
}

var (
	defaultCatalog     *Catalog
	defaultCatalogErr  error
	defaultCatalogOnce sync.Once
)

// DefaultCatalog loads the catalog from the standard cache location,
// fetching the snapshot first if needed. Cached for the process
// lifetime. progress may be nil.
func DefaultCatalog(ctx context.Context, progress func(string)) (*Catalog, error) {
	defaultCatalogOnce.Do(func() {
		dir, err := EnsureSnapshot(ctx, "", progress)
		if err != nil {
			defaultCatalogErr = err
			return
		}
		defaultCatalog, defaultCatalogErr = LoadCatalog(dir)
	})
	return defaultCatalog, defaultCatalogErr
}

// LoadCatalog parses every *_AreaDDF.xml in dir into an indexed
// catalog.
func LoadCatalog(dir string) (*Catalog, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading DDF snapshot dir: %w", err)
	}
	c := &Catalog{
		Snapshot: SnapshotName,
		byRef:    map[string]int{},
		byArea:   map[string][]int{},
	}
	areaSeen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_AreaDDF.xml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		settings, err := ParseAreaDDF(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		for _, s := range settings {
			idx := len(c.settings)
			c.settings = append(c.settings, s)
			// Device and user variants of the same policy share
			// Area/Name; the device one wins the byRef slot (JC's
			// template is device-scoped) and the user variant stays
			// reachable via Filter.
			key := strings.ToLower(s.Area + "/" + s.Name)
			if prev, dup := c.byRef[key]; !dup || (c.settings[prev].Scope == "user" && s.Scope == "device") {
				c.byRef[key] = idx
			}
			areaKey := strings.ToLower(s.Area)
			c.byArea[areaKey] = append(c.byArea[areaKey], idx)
			if !areaSeen[s.Area] {
				areaSeen[s.Area] = true
				c.areas = append(c.areas, s.Area)
			}
		}
	}
	if len(c.settings) == 0 {
		return nil, fmt.Errorf("no settings parsed from %s — snapshot corrupt?", dir)
	}
	sort.Strings(c.areas)
	return c, nil
}

// Len returns the total number of cataloged settings.
func (c *Catalog) Len() int { return len(c.settings) }

// Areas returns the sorted list of area names.
func (c *Catalog) Areas() []string { return c.areas }

// ByRef looks up a setting by "Area/Name" (case-insensitive). When
// both device and user variants exist, the device one is returned.
func (c *Catalog) ByRef(ref string) (Setting, bool) {
	idx, ok := c.byRef[strings.ToLower(strings.TrimSpace(ref))]
	if !ok {
		return Setting{}, false
	}
	return c.settings[idx], true
}

// FilterOpts narrows Filter results. Zero value = everything.
type FilterOpts struct {
	// Area restricts to one area (case-insensitive exact match).
	Area string
	// Search is a case-insensitive substring match over area, name,
	// URI, and description.
	Search string
	// Scope restricts to "device" or "user".
	Scope string
	// ExcludeADMX drops ADMX-backed settings (their values need
	// ADMX-style XML, not plain scalars).
	ExcludeADMX bool
}

// Filter returns the settings matching every set field of opts.
func (c *Catalog) Filter(opts FilterOpts) []Setting {
	candidates := c.settings
	if opts.Area != "" {
		idxs, ok := c.byArea[strings.ToLower(opts.Area)]
		if !ok {
			return nil
		}
		candidates = make([]Setting, 0, len(idxs))
		for _, i := range idxs {
			candidates = append(candidates, c.settings[i])
		}
	}
	needle := strings.ToLower(opts.Search)
	var out []Setting
	for _, s := range candidates {
		if opts.Scope != "" && s.Scope != opts.Scope {
			continue
		}
		if opts.ExcludeADMX && s.ADMXBacked {
			continue
		}
		if needle != "" {
			hay := strings.ToLower(s.Area + " " + s.Name + " " + s.URI + " " + s.Description)
			if !strings.Contains(hay, needle) {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

// TemplateSetting emits the ready-to-edit {uri, format, value} triple
// for a setting — the exact shape `jc windows-mdm oma-uri
// create-policy --settings-file` (and the MCP create tool) consume.
// The value is seeded from the default, else the first enum value.
func TemplateSetting(s Setting) OMAURISetting {
	value := s.DefaultValue
	if value == "" && s.AllowedValues != nil && len(s.AllowedValues.Enum) > 0 {
		value = s.AllowedValues.Enum[0].Value
	}
	return OMAURISetting{URI: s.URI, Format: s.Format, Value: value}
}
