package version

import (
	"runtime/debug"
	"strings"
	"sync"
)

// Number is set at build time via -ldflags. It stays "dev" for a plain
// `go build`, which is how most local builds are made.
var Number = "dev"

// Describe returns the fullest identification of this binary available.
//
// A verification pass could not tell which build it was testing: jc_ping
// reported a bare "vdev", because -ldflags is only applied by `make build` and
// a plain `go build` leaves Number alone. That ambiguity already cost one full
// round of testing against a stale binary, with the findings written up before
// anyone noticed.
//
// So the commit is read from the build info Go embeds automatically, which
// needs no flags and no discipline: any build from a git checkout carries
// vcs.revision and vcs.modified. A version injected by ldflags still wins when
// present — it names a release — but it is no longer the only source.
func Describe() string {
	parts := []string{Number}
	if rev, dirty, ok := vcs(); ok {
		short := rev
		if len(short) > 12 {
			short = short[:12]
		}
		if dirty {
			short += "-dirty"
		}
		// Do not repeat the commit when ldflags already embedded it.
		if !strings.Contains(Number, short[:min(7, len(short))]) {
			parts = append(parts, short)
		}
	}
	return strings.Join(parts, " ")
}

var (
	vcsOnce sync.Once
	vcsRev  string
	vcsMod  bool
	vcsOK   bool
)

// vcs reads the revision Go records at build time. Absent for a binary built
// outside a repository, or with -buildvcs=false.
func vcs() (rev string, dirty, ok bool) {
	vcsOnce.Do(func() {
		info, available := debug.ReadBuildInfo()
		if !available {
			return
		}
		vcsRev, vcsMod, vcsOK = parseVCS(info.Settings)
	})
	return vcsRev, vcsMod, vcsOK
}

// parseVCS extracts the revision and modified flag from build settings.
func parseVCS(settings []debug.BuildSetting) (rev string, dirty, ok bool) {
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			rev, ok = s.Value, true
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	return rev, dirty, ok
}

// Commit returns the full VCS revision this binary was built from and whether
// the tree was dirty. ok is false when the build carries no VCS information.
func Commit() (rev string, dirty, ok bool) {
	return vcs()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
