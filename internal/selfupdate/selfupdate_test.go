package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		current, candidate string
		want               bool
	}{
		{"1.46.47", "1.47.0", true},
		{"1.46.47", "1.46.48", true},
		{"1.46.47", "2.0.0", true},
		{"1.46.47", "1.46.47", false},
		{"1.46.47", "1.46.46", false},
		{"1.47.0", "1.46.99", false},
		// A leading v on either side must not change the answer.
		{"v1.46.47", "1.47.0", true},
		{"1.46.47", "v1.47.0", true},
		// Numeric, not lexical: 1.46.9 < 1.46.10.
		{"1.46.9", "1.46.10", true},
		{"1.46.10", "1.46.9", false},
		// Anything unreadable compares as "not newer" — silence is the
		// honest answer to a version jc cannot parse.
		{"1.46.47", "not-a-version", false},
		{"garbage", "1.47.0", false},
		{"1.46.47", "", false},
		{"", "1.47.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.candidate); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.candidate, got, c.want)
		}
	}
}

func TestIsDevBuild(t *testing.T) {
	// A developer on their own build is not out of date, and saying so is
	// noise they cannot act on.
	for _, v := range []string{"dev", "", "dev-local", "1.46.47-1-gf50ae92"} {
		if !IsDevBuild(v) {
			t.Errorf("%q should be a dev build", v)
		}
	}
	for _, v := range []string{"1.46.47", "v1.46.47", "2.0.0"} {
		if IsDevBuild(v) {
			t.Errorf("%q is a release, not a dev build", v)
		}
	}
}

func TestAssetName(t *testing.T) {
	cases := map[[2]string]string{
		{"darwin", "arm64"}:  "jc-darwin-arm64.tar.gz",
		{"darwin", "amd64"}:  "jc-darwin-amd64.tar.gz",
		{"linux", "amd64"}:   "jc-linux-amd64.tar.gz",
		{"linux", "arm64"}:   "jc-linux-arm64.tar.gz",
		{"windows", "amd64"}: "jc-windows-amd64.zip",
	}
	for in, want := range cases {
		got, err := AssetName(in[0], in[1])
		if err != nil || got != want {
			t.Errorf("AssetName(%q, %q) = %q, %v; want %q", in[0], in[1], got, err, want)
		}
	}
	// Platforms with no published release must say so rather than build a
	// URL that will 404.
	for _, in := range [][2]string{{"windows", "arm64"}, {"freebsd", "amd64"}, {"linux", "386"}} {
		if _, err := AssetName(in[0], in[1]); err == nil {
			t.Errorf("AssetName(%q, %q) should have failed", in[0], in[1])
		}
	}
}

func TestChecksumFor(t *testing.T) {
	// The real format, from the published checksums.txt.
	body := []byte(
		"d9afac9d0f6c82a1c648324a3f18961263ae2cfadb3078bf73bf031129779e38  jc-darwin-amd64.tar.gz\n" +
			"99efdcd8fc3bc43b255b14d270af93614a54f7bbb95387527a436f07cb3c818f  jc-darwin-arm64.tar.gz\n")

	got, err := ChecksumFor(body, "jc-darwin-arm64.tar.gz")
	if err != nil || got != "99efdcd8fc3bc43b255b14d270af93614a54f7bbb95387527a436f07cb3c818f" {
		t.Errorf("= %q, %v", got, err)
	}

	// An asset with no entry must refuse, not fall through to installing an
	// unverified binary.
	_, err = ChecksumFor(body, "jc-linux-amd64.tar.gz")
	if err == nil {
		t.Fatal("a missing entry must be an error")
	}
	if !strings.Contains(err.Error(), "will not be installed") {
		t.Errorf("the message should say nothing is installed: %v", err)
	}

	// Malformed sums are refused rather than compared.
	for _, bad := range []string{"tooshort  jc-x.tar.gz", "zz" + strings.Repeat("0", 62) + "  jc-x.tar.gz"} {
		if _, err := ChecksumFor([]byte(bad), "jc-x.tar.gz"); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestVerifyChecksum(t *testing.T) {
	payload := []byte("some release archive")
	sum := sha256.Sum256(payload)
	good := hex.EncodeToString(sum[:])

	if err := VerifyChecksum(payload, good); err != nil {
		t.Errorf("a matching checksum should verify: %v", err)
	}
	// One flipped byte must fail.
	err := VerifyChecksum([]byte("some release archivf"), good)
	if err == nil {
		t.Fatal("a mismatch must be an error")
	}
	if !strings.Contains(err.Error(), "will not be installed") {
		t.Errorf("the message should say nothing is installed: %v", err)
	}
}

func tarGzWith(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestExtractBinary_TarGz(t *testing.T) {
	// The published layout: a directory containing the binary.
	archive := tarGzWith(t, "jc-darwin-arm64/jc", []byte("ELF-ish"))
	got, err := ExtractBinary(archive, "jc-darwin-arm64.tar.gz")
	if err != nil || string(got) != "ELF-ish" {
		t.Errorf("= %q, %v", got, err)
	}

	// An archive with no jc in it must fail rather than install something
	// else that happened to be inside.
	other := tarGzWith(t, "jc-darwin-arm64/README", []byte("hello"))
	if _, err := ExtractBinary(other, "jc-darwin-arm64.tar.gz"); err == nil {
		t.Error("an archive without the binary must be an error")
	}
	if _, err := ExtractBinary([]byte("not gzip"), "jc-darwin-arm64.tar.gz"); err == nil {
		t.Error("a non-gzip body must be an error")
	}
}

func TestExtractBinary_Zip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("jc-windows-amd64/jc.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("PE-ish")); err != nil {
		t.Fatal(err)
	}
	zw.Close()

	got, err := ExtractBinary(buf.Bytes(), "jc-windows-amd64.zip")
	if err != nil || string(got) != "PE-ish" {
		t.Errorf("= %q, %v", got, err)
	}
}

func TestReplace(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "jc")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Replace(target, []byte("new")); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "new" {
		t.Errorf("content = %q, %v", got, err)
	}
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("mode = %v, want it executable", fi.Mode().Perm())
	}

	// No temporary file is left behind on success.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".jc-upgrade-") {
			t.Errorf("left a temporary file behind: %s", e.Name())
		}
	}
}

// TestReplacePreservesMode keeps an installation deliberately tightened to
// 0700 from being widened by an upgrade.
func TestReplacePreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "jc")
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Replace(target, []byte("new")); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(target)
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("mode = %v, want 0700 preserved", fi.Mode().Perm())
	}
}

func TestWritable(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "jc")
	if err := os.WriteFile(target, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !Writable(target) {
		t.Error("a temp dir should be writable")
	}
	// Writability is a property of the DIRECTORY, because replacing is a
	// rename. A read-only file in a writable directory is still replaceable.
	if err := os.Chmod(target, 0o444); err != nil {
		t.Fatal(err)
	}
	if !Writable(target) {
		t.Error("a read-only file in a writable directory is still replaceable")
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		ro := filepath.Join(dir, "ro")
		if err := os.Mkdir(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		if Writable(filepath.Join(ro, "jc")) {
			t.Error("a read-only directory is not writable")
		}
	}
}

func TestState(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// A missing cache is an empty state, never an error, and a check is due.
	s := LoadState(dir)
	if !s.Due(now) {
		t.Error("an empty state should be due for a check")
	}

	SaveState(dir, State{LastCheck: now, LatestVersion: "1.47.0"})
	s = LoadState(dir)
	if s.LatestVersion != "1.47.0" || s.Due(now) {
		t.Errorf("state = %+v", s)
	}
	if !s.Due(now.Add(CheckInterval + time.Minute)) {
		t.Error("a stale check should be due again")
	}

	// A corrupt cache costs a network call, not a command.
	if err := os.WriteFile(StatePath(dir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadState(dir); got.LatestVersion != "" {
		t.Errorf("a corrupt cache should read as empty, got %+v", got)
	}
}

func TestShouldNotify(t *testing.T) {
	now := time.Now()
	s := State{LatestVersion: "1.47.0"}

	if !s.ShouldNotify("1.46.47", now) {
		t.Error("a newer version should notify")
	}
	if s.ShouldNotify("1.47.0", now) {
		t.Error("the same version must not notify")
	}
	if s.ShouldNotify("1.48.0", now) {
		t.Error("a newer local build must not notify")
	}
	// Having just notified, stay quiet until the interval passes.
	s.LastNotified = now
	if s.ShouldNotify("1.46.47", now) {
		t.Error("must not repeat within the interval")
	}
	if !s.ShouldNotify("1.46.47", now.Add(CheckInterval+time.Minute)) {
		t.Error("should notify again after the interval")
	}
	// No cached answer means nothing to say.
	if (State{}).ShouldNotify("1.46.47", now) {
		t.Error("an empty state must not notify")
	}
}

func TestLatestRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q", got)
		}
		// No credential is sent: the repository is public.
		if r.Header.Get("Authorization") != "" {
			t.Error("the check must not send a credential")
		}
		w.Write([]byte(`{"tag_name":"1.47.0","published_at":"2026-09-28T10:00:00Z"}`))
	}))
	defer srv.Close()

	rel, err := fetchFrom(t, srv.URL)
	if err != nil || rel.TagName != "1.47.0" {
		t.Errorf("= %+v, %v", rel, err)
	}
	if rel.PublishedAt.Year() != 2026 {
		t.Errorf("published = %v", rel.PublishedAt)
	}
}

func TestLatestRelease_Failures(t *testing.T) {
	// Every failure must be an error the caller can swallow, never a panic
	// and never a usable-looking empty release.
	cases := map[string]http.HandlerFunc{
		"HTTP 500":  func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
		"not json":  func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>")) },
		"empty tag": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) },
	}
	for name, h := range cases {
		srv := httptest.NewServer(h)
		if _, err := fetchFrom(t, srv.URL); err == nil {
			t.Errorf("%s should have failed", name)
		}
		srv.Close()
	}
}

// fetchFrom points LatestRelease at a test server by temporarily swapping the
// endpoint, which keeps the production constant honest.
func fetchFrom(t *testing.T, url string) (Release, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	return decodeRelease(resp)
}
