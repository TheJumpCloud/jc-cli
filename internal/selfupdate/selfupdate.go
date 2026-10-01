// Package selfupdate checks whether a newer jc release exists and, when
// asked, installs it.
//
// It exists because the two install paths both leave operators stranded on
// old builds: a downloaded binary has no update path at all, and building
// from source needs a Go toolchain most admins running this CLI do not have
// (issue #48). Homebrew would solve it, but only for people on Homebrew and
// only once a tap exists.
//
// Two rules shape everything here.
//
// THE CHECK MUST NEVER COST ANYTHING. It is cached, it is skipped whenever
// output is going anywhere but a terminal, and every failure is silent. A
// version check that slows down `jc users list`, or breaks a pipeline because
// GitHub had a bad minute, is worse than no version check.
//
// THE UPGRADE IS NEVER AUTOMATIC. jc administers an identity platform; a tool
// that silently rewrites its own code is a supply-chain surface and it breaks
// reproducibility for anyone pinning a version in CI or an MDM fleet. The
// check tells you; you decide.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ReleasesAPI is the unauthenticated endpoint the check uses. The repository
// is public, so no credential is involved and none is sent.
const ReleasesAPI = "https://api.github.com/repos/TheJumpCloud/jc-cli/releases/latest"

// DownloadBase is where release assets live.
const DownloadBase = "https://github.com/TheJumpCloud/jc-cli/releases/download"

// CheckTimeout bounds the check. It is short on purpose: the answer is a
// convenience, and nothing may wait on it.
const CheckTimeout = 2 * time.Second

// DownloadTimeout bounds an upgrade, which the operator asked for and is
// watching, so it can afford to be patient.
const DownloadTimeout = 2 * time.Minute

// CheckInterval is how long a check result is reused.
const CheckInterval = 24 * time.Hour

// Release is the part of a GitHub release jc needs.
type Release struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

// LatestRelease fetches the newest published release.
func LatestRelease(ctx context.Context, client *http.Client, timeout time.Duration) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ReleasesAPI, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	return decodeRelease(resp)
}

// decodeRelease reads a release from a response. It is separate so the
// decoding and its failure modes can be tested against a real HTTP response
// without swapping the production endpoint for a variable.
func decodeRelease(resp *http.Response) (Release, error) {
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("release check returned HTTP %d", resp.StatusCode)
	}
	var rel Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return Release{}, fmt.Errorf("decoding the release: %w", err)
	}
	if rel.TagName == "" {
		return Release{}, fmt.Errorf("the release carries no tag")
	}
	return rel, nil
}

// --- Version comparison ---------------------------------------------------

// IsDevBuild reports whether a version string is a local build rather than a
// release. A developer running their own build is not out of date, and
// telling them so is noise.
func IsDevBuild(v string) bool {
	v = strings.TrimSpace(v)
	return v == "" || v == "dev" || strings.HasPrefix(v, "dev") ||
		// `git describe` output on a commit past the tag: 1.46.47-1-gf50ae92.
		strings.Count(v, "-") >= 2
}

// Newer reports whether candidate is a later release than current.
//
// Versions here are plain dotted numbers — 1.46.47 — so this compares
// numerically field by field rather than pulling in a semver dependency for
// a format that has never had a pre-release suffix. Anything unparseable
// compares as "not newer", because the honest answer to a version jc cannot
// read is silence.
func Newer(current, candidate string) bool {
	c, ok1 := parseVersion(current)
	n, ok2 := parseVersion(candidate)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < 3; i++ {
		if n[i] != c[i] {
			return n[i] > c[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return out, false
	}
	parts := strings.SplitN(v, ".", 4)
	if len(parts) < 2 {
		return out, false
	}
	for i := 0; i < 3 && i < len(parts); i++ {
		// Tolerate a trailing suffix on the last field so a describe-style
		// version does not fail the whole parse.
		field := parts[i]
		if cut := strings.IndexAny(field, "-+"); cut >= 0 {
			field = field[:cut]
		}
		n, err := strconv.Atoi(field)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// --- The cached check -----------------------------------------------------

// State is what jc remembers between checks, so the network is touched at
// most once a day rather than once a command.
type State struct {
	LastCheck     time.Time `json:"last_check"`
	LatestVersion string    `json:"latest_version"`
	// LastNotified stops the notice repeating within one interval even when
	// the cached answer says an upgrade exists.
	LastNotified time.Time `json:"last_notified"`
}

// StatePath is where the check result is cached.
func StatePath(configDir string) string {
	return filepath.Join(configDir, "update-check.json")
}

// LoadState reads the cache. A missing or unreadable file is an empty state,
// never an error: a corrupt cache should cost a network call, not a command.
func LoadState(configDir string) State {
	b, err := os.ReadFile(StatePath(configDir))
	if err != nil {
		return State{}
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}
	}
	return s
}

// SaveState writes the cache, best effort.
func SaveState(configDir string, s State) {
	b, err := json.Marshal(s)
	if err != nil {
		return
	}
	_ = os.MkdirAll(configDir, 0o755)
	_ = os.WriteFile(StatePath(configDir), b, 0o600)
}

// Due reports whether the cached result has expired.
func (s State) Due(now time.Time) bool {
	return now.Sub(s.LastCheck) >= CheckInterval
}

// ShouldNotify reports whether to print a notice for a cached newer version.
func (s State) ShouldNotify(current string, now time.Time) bool {
	if s.LatestVersion == "" || !Newer(current, s.LatestVersion) {
		return false
	}
	return now.Sub(s.LastNotified) >= CheckInterval
}

// --- Assets ---------------------------------------------------------------

// AssetName is the release archive for a platform. The published names are
// jc-<os>-<arch>.tar.gz, and .zip on Windows.
func AssetName(goos, goarch string) (string, error) {
	switch goos {
	case "darwin", "linux":
		if goarch != "amd64" && goarch != "arm64" {
			return "", unsupported(goos, goarch)
		}
		return fmt.Sprintf("jc-%s-%s.tar.gz", goos, goarch), nil
	case "windows":
		if goarch != "amd64" {
			return "", unsupported(goos, goarch)
		}
		return "jc-windows-amd64.zip", nil
	}
	return "", unsupported(goos, goarch)
}

func unsupported(goos, goarch string) error {
	return fmt.Errorf("no published jc release for %s/%s — releases cover darwin and linux "+
		"on amd64 and arm64, and windows on amd64", goos, goarch)
}

// AssetURL is where an asset for a version lives.
func AssetURL(version, asset string) string {
	return DownloadBase + "/" + version + "/" + asset
}

// --- Integrity ------------------------------------------------------------

// ChecksumFor finds an asset's expected SHA-256 in a checksums.txt body,
// which is the standard `<hex>  <filename>` format.
func ChecksumFor(checksums []byte, asset string) (string, error) {
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		if fields[1] == asset {
			sum := strings.ToLower(fields[0])
			if len(sum) != 64 {
				return "", fmt.Errorf("the checksum for %s is not a SHA-256", asset)
			}
			if _, err := hex.DecodeString(sum); err != nil {
				return "", fmt.Errorf("the checksum for %s is not hexadecimal", asset)
			}
			return sum, nil
		}
	}
	return "", fmt.Errorf("checksums.txt lists no entry for %s, so the download cannot be "+
		"verified and will not be installed", asset)
}

// VerifyChecksum compares a downloaded archive against its published sum.
//
// WHAT THIS DOES AND DOES NOT PROVE. checksums.txt is fetched from the same
// release as the archive, so this establishes that the bytes arrived intact —
// not that the release is authentic. Anyone able to publish a release can
// publish a matching checksum.
//
// Releases carry a Sigstore signature over checksums.txt, which does close
// that gap, and this function does not check it. That is a decision rather
// than an omission: sigstore-go pulls in roughly seventy modules against a
// go.sum of a hundred and forty-six lines, and doubling the dependency tree
// to verify the dependency tree is not obviously a gain — the verifier is
// itself code that has to be trusted. Signature checking lives in
// scripts/verify-release.sh, for the cases where it matters.
//
// So: this is an integrity check. Calling it more would be dishonest.
func VerifyChecksum(archive []byte, want string) error {
	sum := sha256.Sum256(archive)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("the download does not match its published checksum "+
			"(expected %s, got %s) — it will not be installed", want, got)
	}
	return nil
}

// --- Extraction -----------------------------------------------------------

// maxBinarySize bounds what will be extracted, so a hostile or corrupt
// archive cannot exhaust memory.
const maxBinarySize = 200 << 20 // 200 MB

// ExtractBinary pulls the jc executable out of a release archive. The
// published layout is a single directory containing the binary —
// jc-darwin-arm64/jc — so the name is matched rather than the path.
func ExtractBinary(archive []byte, asset string) ([]byte, error) {
	if strings.HasSuffix(asset, ".zip") {
		return extractZip(archive)
	}
	return extractTarGz(archive)
}

func wantedName(name string) bool {
	base := path.Base(filepath.ToSlash(name))
	return base == "jc" || base == "jc.exe"
}

func extractTarGz(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("the download is not a gzip archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading the archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || !wantedName(hdr.Name) {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(tr, maxBinarySize))
		if err != nil {
			return nil, fmt.Errorf("extracting the binary: %w", err)
		}
		return b, nil
	}
	return nil, fmt.Errorf("the archive contains no jc binary")
}

func extractZip(archive []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("the download is not a zip archive: %w", err)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !wantedName(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("opening the binary in the archive: %w", err)
		}
		defer rc.Close()
		b, err := io.ReadAll(io.LimitReader(rc, maxBinarySize))
		if err != nil {
			return nil, fmt.Errorf("extracting the binary: %w", err)
		}
		return b, nil
	}
	return nil, fmt.Errorf("the archive contains no jc binary")
}

// --- Installation ---------------------------------------------------------

// TargetPath is the binary to replace: this process's own executable, with
// symlinks resolved so a link in ~/bin pointing elsewhere is not overwritten
// with a regular file.
func TargetPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locating the running jc binary: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe, nil
	}
	return resolved, nil
}

// Writable reports whether the target can be replaced without elevation,
// which is a property of its DIRECTORY: replacing is a rename, so what
// matters is write permission on the directory rather than on the file.
func Writable(target string) bool {
	dir := filepath.Dir(target)
	probe, err := os.CreateTemp(dir, ".jc-write-probe-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return true
}

// Replace installs newBinary at target atomically.
//
// The temporary file is created in the target's own directory so the rename
// stays within one filesystem — a rename across filesystems is not atomic and
// on many systems is not permitted at all, which would leave no binary where
// one used to be.
func Replace(target string, newBinary []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".jc-upgrade-*")
	if err != nil {
		return fmt.Errorf("creating the replacement next to %s: %w", target, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds

	if _, err := tmp.Write(newBinary); err != nil {
		tmp.Close()
		return fmt.Errorf("writing the replacement: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing the replacement: %w", err)
	}

	mode := os.FileMode(0o755)
	if fi, err := os.Stat(target); err == nil {
		// Keep whatever mode the existing binary had, so an installation
		// deliberately tightened to 0700 is not widened by an upgrade.
		mode = fi.Mode().Perm()
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("setting permissions on the replacement: %w", err)
	}

	// Windows refuses to rename over a running executable, but it does allow
	// the running one to be renamed away first. The leftover is removed on a
	// later run; failing to remove it is not worth failing the upgrade.
	var backup string
	if runtime.GOOS == "windows" {
		backup = target + ".old"
		_ = os.Remove(backup)
		if err := os.Rename(target, backup); err != nil {
			return fmt.Errorf("moving the running binary aside: %w", err)
		}
	}

	if err := os.Rename(tmpName, target); err != nil {
		if backup != "" {
			_ = os.Rename(backup, target) // put it back
		}
		return fmt.Errorf("installing the replacement over %s: %w", target, err)
	}
	if backup != "" {
		_ = os.Remove(backup)
	}
	return nil
}
