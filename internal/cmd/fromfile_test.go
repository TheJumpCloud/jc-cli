package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadBatchIdentifiers_CommentsBlanksLineNumbers(t *testing.T) {
	input := `# offboarding batch 2026-07
alice

bob
# charlie is on hold
dave
`
	ids, err := readBatchIdentifiers(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	want := []batchIdentifier{{"alice", 2}, {"bob", 4}, {"dave", 6}}
	if len(ids) != len(want) {
		t.Fatalf("got %d ids: %+v", len(ids), ids)
	}
	for i, w := range want {
		if ids[i] != w {
			t.Errorf("ids[%d] = %+v, want %+v", i, ids[i], w)
		}
	}
}

// batchWiredCommands is the coverage contract: every command listed
// here must carry --from-file and --stdin. Add new single-identifier
// mutations to this list when wiring them.
var batchWiredCommands = [][]string{
	{"users", "delete"}, {"users", "lock"}, {"users", "unlock"},
	{"users", "reset-mfa"}, {"users", "reset-password"},
	{"devices", "delete"}, {"devices", "lock"}, {"devices", "restart"}, {"devices", "erase"},
	{"groups", "user", "delete"}, {"groups", "device", "delete"},
	{"access-requests", "revoke"},
	{"ad", "delete"}, {"admins", "delete"}, {"apple-mdm", "delete"},
	{"apps", "delete"}, {"auth-policies", "delete"}, {"commands", "delete"},
	{"custom-emails", "delete"}, {"duo", "delete"},
	{"identity-providers", "delete"}, {"iplists", "delete"}, {"ldap", "delete"},
	{"policies", "delete"}, {"policy-groups", "delete"}, {"radius", "delete"},
	{"saas-management", "delete"}, {"software", "delete"}, {"user-states", "delete"},
}

func TestBatchSourceFlags_WiredEverywhere(t *testing.T) {
	root := NewRootCmd()
	for _, path := range batchWiredCommands {
		cmd, _, err := root.Find(path)
		if err != nil {
			t.Errorf("%v: not found: %v", path, err)
			continue
		}
		if cmd.Flags().Lookup("from-file") == nil {
			t.Errorf("%v: missing --from-file", path)
		}
		if cmd.Flags().Lookup("stdin") == nil {
			t.Errorf("%v: missing --stdin", path)
		}
	}
}

func TestCollectBatchIdentifiers_MutualExclusion(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "ids.txt")
	if err := os.WriteFile(file, []byte("alice\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	build := func(args ...string) error {
		root := NewRootCmd()
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(append([]string{"admins", "delete"}, args...))
		return root.Execute()
	}

	// arg + --from-file → refused.
	err := build("someone", "--from-file", file)
	if err == nil || !strings.Contains(err.Error(), "one identifier source") {
		t.Errorf("arg+file should be refused: %v", err)
	}
	// --from-file + --stdin → refused.
	err = build("--from-file", file, "--stdin")
	if err == nil || !strings.Contains(err.Error(), "one identifier source") {
		t.Errorf("file+stdin should be refused: %v", err)
	}
	// missing file → actionable error.
	err = build("--from-file", filepath.Join(dir, "nope.txt"))
	if err == nil || !strings.Contains(err.Error(), "opening --from-file") {
		t.Errorf("missing file error wrong: %v", err)
	}
	// comment-only file → "no identifiers".
	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, []byte("# nothing here\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = build("--from-file", empty)
	if err == nil || !strings.Contains(err.Error(), "no identifiers") {
		t.Errorf("empty file error wrong: %v", err)
	}
}

func TestBatchFromFile_PlanAndForceGate(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	dir := t.TempDir()
	file := filepath.Join(dir, "batch.txt")
	if err := os.WriteFile(file, []byte("# batch\naaa111aaa111aaa111aaa111\nbbb222bbb222bbb222bbb222\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// --plan renders ONE aggregated plan (exit code 10 via plan error)
	// and executes nothing — net-new for batch paths (the old stdin
	// path bypassed plan mode entirely).
	root := NewRootCmd()
	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(errBuf)
	root.SetArgs([]string{"users", "delete", "--from-file", file, "--plan"})
	planErr := root.Execute()
	rendered := out.String() + errBuf.String()
	if !strings.Contains(rendered, "batch of 2") || !strings.Contains(rendered, "line 2:") {
		t.Errorf("aggregated plan should list rows with line numbers:\n%s\nerr=%v", rendered, planErr)
	}

	// Without --force (and not --plan): refused before any row runs.
	root = NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"users", "delete", "--from-file", file})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("batch without --force should be refused: %v", err)
	}

	// With --force: both rows execute; summary on stderr.
	root = NewRootCmd()
	errBuf = &bytes.Buffer{}
	root.SetOut(&bytes.Buffer{})
	root.SetErr(errBuf)
	root.SetArgs([]string{"users", "delete", "--from-file", file, "--force"})
	if err := root.Execute(); err != nil {
		t.Fatalf("forced batch failed: %v", err)
	}
	if !strings.Contains(errBuf.String(), "2 succeeded, 0 failed") {
		t.Errorf("summary missing: %q", errBuf.String())
	}
}

func TestBatchFromFile_FailuresReportLineNumbers(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	dir := t.TempDir()
	file := filepath.Join(dir, "batch.txt")
	// Line 1 valid, line 2 comment, line 3 bogus.
	if err := os.WriteFile(file, []byte("aaa111aaa111aaa111aaa111\n# hold\nfff666fff666fff666fff666\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := NewRootCmd()
	errBuf := &bytes.Buffer{}
	root.SetOut(&bytes.Buffer{})
	root.SetErr(errBuf)
	root.SetArgs([]string{"users", "delete", "--from-file", file, "--force"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "1 of 2 delete operations failed") {
		t.Fatalf("expected partial-failure error, got %v", err)
	}
	// The failure detail points at the ORIGINAL file line (3, not row 2).
	if !strings.Contains(errBuf.String(), "line 3 (fff666fff666fff666fff666)") {
		t.Errorf("failure should carry original line number:\n%s", errBuf.String())
	}
}
