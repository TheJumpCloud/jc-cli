package version

import (
	"runtime/debug"
	"strings"
	"testing"
)

// Describe must say something more identifying than "dev".
//
// A verification pass could not tell which build it had tested, because
// jc_ping reported a bare "vdev" — -ldflags is applied only by `make build`,
// and a plain `go build` leaves Number alone. That ambiguity had already cost
// a full round of testing against a stale binary, with findings written up
// before anyone noticed the binary was old.
//
// The commit therefore comes from the build info Go embeds automatically,
// which needs no flags and no discipline.
func TestDescribe_IdentifiesTheBuild(t *testing.T) {
	got := Describe()
	if got == "" {
		t.Fatal("Describe returned nothing")
	}
	if !strings.HasPrefix(got, Number) {
		t.Errorf("Describe = %q, should start with the version %q", got, Number)
	}

	// Under `go test` the binary is built from the module, so vcs info is
	// normally present. When it is, Describe must be more than the version
	// alone — that is the entire point.
	if _, _, ok := vcs(); ok {
		if got == Number {
			t.Errorf("Describe = %q, identical to Number — the commit was available and not used", got)
		}
	} else {
		t.Log("no vcs info in this build; Describe correctly falls back to the version alone")
	}
}

// A dirty tree must be visible. A binary built from uncommitted changes is not
// the commit it names, and reporting the bare hash would assert otherwise.
func TestDescribe_DirtyIsNotHidden(t *testing.T) {
	rev, dirty, ok := vcs()
	if !ok {
		t.Skip("no vcs info in this build")
	}
	got := Describe()
	if dirty && !strings.Contains(got, "dirty") {
		t.Errorf("built from a modified tree at %s but Describe = %q says nothing about it", rev, got)
	}
	if !dirty && strings.Contains(got, "dirty") {
		t.Errorf("tree is clean but Describe = %q claims otherwise", got)
	}
}

// Describe is called on every ping; it must not re-read build info each time.
func TestDescribe_IsStable(t *testing.T) {
	if a, b := Describe(), Describe(); a != b {
		t.Errorf("Describe is not stable: %q then %q", a, b)
	}
}

func TestParseVCS(t *testing.T) {
	rev := strings.Repeat("b", 40)
	got, dirty, ok := parseVCS([]debug.BuildSetting{
		{Key: "vcs.revision", Value: rev},
		{Key: "vcs.modified", Value: "true"},
	})
	if got != rev || !dirty || !ok {
		t.Errorf("parseVCS = %q,%v,%v", got, dirty, ok)
	}
	got, dirty, ok = parseVCS([]debug.BuildSetting{{Key: "vcs.revision", Value: rev}, {Key: "vcs.modified", Value: "false"}})
	if got != rev || dirty || !ok {
		t.Errorf("clean parseVCS = %q,%v,%v", got, dirty, ok)
	}
	if got, dirty, ok := parseVCS(nil); got != "" || dirty || ok {
		t.Errorf("absent parseVCS = %q,%v,%v", got, dirty, ok)
	}
}

func TestCommitMatchesVCS(t *testing.T) {
	r1, d1, ok1 := Commit()
	r2, d2, ok2 := vcs()
	if r1 != r2 || d1 != d2 || ok1 != ok2 {
		t.Error("Commit() should mirror vcs()")
	}
}
