// Package mscp converts NIST macOS Security Compliance Project
// baselines (usnistgov/macos_security) into jc security baseline
// bundles (KLA-473).
//
// Licensing (verified in the project's LICENSE.md, 2026-07-10): mSCP
// is CC BY 4.0 with US-government contributions in the public domain —
// redistributable with attribution. One carve-out: "Vendor Description
// content" contributed by Apple and marked as such is NOT licensed, so
// this converter copies only rule IDs, titles, and the machine-readable
// payload facts (mobileconfig_info) — never discussion prose.
//
// The snapshot follows the fetch-on-demand discipline from
// docs/solutions/design-patterns/fetch-on-demand-catalog-licensing:
// pinned tag + SHA-256, cached extract, air-gapped pre-placed zip.
// Note the pin is a GitHub source archive: GitHub has committed to
// checksum stability for tag archives, but if the pin ever mismatches,
// the error says exactly what to do.
package mscp

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
	"strings"
	"time"
)

// Pinned snapshot. Refreshing to a newer mSCP release means updating
// all three values together and re-generating the builtin baselines in
// the same commit (go:generate in internal/bundle keeps them honest).
const (
	// SnapshotTag is the mSCP release tag (macOS 26 "Tahoe", rev 3,
	// released 2026-06-22).
	SnapshotTag = "tahoe_rev3"
	// SnapshotURL is GitHub's source archive for the pinned tag.
	SnapshotURL = "https://github.com/usnistgov/macos_security/archive/refs/tags/" + SnapshotTag + ".zip"
	// SnapshotSHA256 pins the zip bytes. Recorded 2026-07-10 during
	// the KLA-473 empirical gate.
	SnapshotSHA256 = "34f0360931138fbe8f551252db19449c7e74ceba74d8974b95eb4385b0c28a2e"
)

// snapshotMarker is written after a successful extract; a crash
// mid-extract leaves no marker and the next run re-extracts.
const snapshotMarker = ".complete.v1"

// CacheDir returns the extract directory:
// $XDG_CACHE_HOME/jc/mscp/<SnapshotTag> (or ~/.cache/...).
func CacheDir() string {
	root := os.Getenv("XDG_CACHE_HOME")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".cache")
	}
	return filepath.Join(root, "jc", "mscp", SnapshotTag)
}

// EnsureSnapshot makes sure the pinned mSCP release is extracted into
// dir (CacheDir() when dir is empty — tests pass a temp dir). Returns
// the directory holding rules/ and baselines/.
//
// Resolution order matches the windows_mdm catalog: extracted marker →
// pre-placed <dir>.zip (air-gapped) → download from SnapshotURL.
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
		note(fmt.Sprintf("Downloading NIST mSCP release %s (~1MB, one-time)...", SnapshotTag))
		if err := download(ctx, zipPath); err != nil {
			return "", fmt.Errorf(
				"downloading mSCP snapshot: %w\n(air-gapped? manually download %s and place it at %s)",
				err, SnapshotURL, zipPath)
		}
	} else {
		note("Using pre-placed snapshot zip at " + zipPath)
	}

	if err := verifySHA256(zipPath); err != nil {
		return "", err
	}
	note("Verified SHA-256; extracting rules and baselines...")
	if err := extract(zipPath, dir); err != nil {
		return "", fmt.Errorf("extracting mSCP snapshot: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, snapshotMarker), []byte(SnapshotSHA256+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("finalizing snapshot cache: %w", err)
	}
	return dir, nil
}

func download(ctx context.Context, dest string) error {
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

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".jc-mscp-*.tmp")
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

func verifySHA256(zipPath string) error {
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
			"mSCP snapshot %s failed SHA-256 verification:\n  got  %s\n  want %s\nThe file at %s is not the pinned release archive — delete it and retry; if GitHub re-generated the tag archive, update the pin (and regenerate the builtin baselines)",
			SnapshotTag, got, SnapshotSHA256, zipPath)
	}
	return nil
}

// extract unpacks only what the converter reads — rules/**/*.yaml and
// baselines/*.yaml — preserving the tree relative to the archive's
// single top-level directory. Zip-slip guarded.
func extract(zipPath, dir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	extracted := 0
	for _, f := range r.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(f.Name, ".yaml") {
			continue
		}
		// Strip the archive's top-level "macos_security-<tag>/" dir.
		rel := f.Name
		if i := strings.IndexByte(rel, '/'); i >= 0 {
			rel = rel[i+1:]
		}
		if !strings.HasPrefix(rel, "rules/") && !strings.HasPrefix(rel, "baselines/") {
			continue
		}
		dest := filepath.Join(dir, filepath.FromSlash(rel))
		if !strings.HasPrefix(dest, filepath.Clean(dir)+string(os.PathSeparator)) {
			return fmt.Errorf("zip entry %q escapes extract dir", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		src, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(src)
		src.Close()
		if err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return err
		}
		extracted++
	}
	if extracted == 0 {
		return fmt.Errorf("no rules/ or baselines/ YAML found in %s — archive layout changed?", zipPath)
	}
	return nil
}
