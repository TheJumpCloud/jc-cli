package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"

	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/recipe"
)

// overrideStdinSource injects a reader for stdin-based tests.
func overrideStdinSource(t *testing.T, input string) {
	t.Helper()
	orig := stdinSource
	stdinSource = strings.NewReader(input)
	t.Cleanup(func() { stdinSource = orig })
}

// overrideStdinParamsReader injects a reader for --params-stdin tests.
func overrideStdinParamsReader(t *testing.T, input string) {
	t.Helper()
	orig := stdinParamsReader
	stdinParamsReader = strings.NewReader(input)
	t.Cleanup(func() { stdinParamsReader = orig })
}

// --- readLinesFromStdin unit tests ---

func TestReadLinesFromStdin_MultipleLines(t *testing.T) {
	overrideStdinSource(t, "alice\nbob\ncharlie\n")

	lines, err := readLinesFromStdin()
	if err != nil {
		t.Fatalf("readLinesFromStdin error: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "alice" || lines[1] != "bob" || lines[2] != "charlie" {
		t.Errorf("unexpected lines: %v", lines)
	}
}

func TestReadLinesFromStdin_EmptyInput(t *testing.T) {
	overrideStdinSource(t, "")

	lines, err := readLinesFromStdin()
	if err != nil {
		t.Fatalf("readLinesFromStdin error: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(lines))
	}
}

func TestReadLinesFromStdin_SkipsBlankLines(t *testing.T) {
	overrideStdinSource(t, "alice\n\n  \nbob\n\n")

	lines, err := readLinesFromStdin()
	if err != nil {
		t.Fatalf("readLinesFromStdin error: %v", err)
	}
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %v", len(lines), lines)
	}
}

func TestReadLinesFromStdin_TrimWhitespace(t *testing.T) {
	overrideStdinSource(t, "  alice  \n\tbob\t\n")

	lines, err := readLinesFromStdin()
	if err != nil {
		t.Fatalf("readLinesFromStdin error: %v", err)
	}
	if lines[0] != "alice" || lines[1] != "bob" {
		t.Errorf("expected trimmed values, got: %v", lines)
	}
}

// --- runStdinBatch unit tests ---

func TestRunStdinBatch_AllSucceed(t *testing.T) {
	progressBuf := &bytes.Buffer{}
	result := runStdinBatch([]string{"a", "b", "c"}, "item", "Processing", progressBuf, func(id string) error {
		return nil
	})

	if result.Succeeded != 3 {
		t.Errorf("expected 3 succeeded, got %d", result.Succeeded)
	}
	if result.Failed != 0 {
		t.Errorf("expected 0 failed, got %d", result.Failed)
	}
	if !strings.Contains(progressBuf.String(), "Processing 1 of 3") {
		t.Errorf("progress should contain item numbers: %q", progressBuf.String())
	}
	if !strings.Contains(progressBuf.String(), "3 succeeded, 0 failed") {
		t.Errorf("summary should show 3 succeeded: %q", progressBuf.String())
	}
}

func TestRunStdinBatch_SomeFail(t *testing.T) {
	progressBuf := &bytes.Buffer{}
	result := runStdinBatch([]string{"a", "fail", "c"}, "item", "Processing", progressBuf, func(id string) error {
		if id == "fail" {
			return io.ErrUnexpectedEOF
		}
		return nil
	})

	if result.Succeeded != 2 {
		t.Errorf("expected 2 succeeded, got %d", result.Succeeded)
	}
	if result.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", result.Failed)
	}
	if !strings.Contains(progressBuf.String(), "FAILED") {
		t.Errorf("progress should contain FAILED: %q", progressBuf.String())
	}
}

// --- Users delete --stdin integration tests ---

func TestUsersDeleteStdin_DeleteMultiple(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	// Provide two user IDs via stdin.
	overrideStdinSource(t, "aaa111aaa111aaa111aaa111\nbbb222bbb222bbb222bbb222\n")

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"users", "delete", "--stdin", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	progress := errBuf.String()
	if !strings.Contains(progress, "delete 1 of 2") {
		t.Errorf("progress should show 1 of 2: %q", progress)
	}
	if !strings.Contains(progress, "delete 2 of 2") {
		t.Errorf("progress should show 2 of 2: %q", progress)
	}
	if !strings.Contains(progress, "2 succeeded, 0 failed") {
		t.Errorf("summary should show 2 succeeded: %q", progress)
	}
}

func TestUsersDeleteStdin_EmptyStdinNoError(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	overrideStdinSource(t, "")

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "delete", "--stdin"})

	// KLA-446 semantics change: an empty batch source is an ERROR, not
	// a silent no-op — an upstream filter matching nothing should be
	// visible, not vanish.
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no identifiers") {
		t.Fatalf("expected empty-stdin error, got: %v", err)
	}
}

func TestUsersDeleteStdin_InvalidUser(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	// Mix valid and invalid IDs.
	overrideStdinSource(t, "aaa111aaa111aaa111aaa111\nfff666fff666fff666fff666\n")

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"users", "delete", "--stdin", "--force"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for partially failed batch")
	}
	if !strings.Contains(err.Error(), "1 of 2 delete operations failed") {
		t.Errorf("error should report failure count: %q", err.Error())
	}

	progress := errBuf.String()
	if !strings.Contains(progress, "1 succeeded, 1 failed") {
		t.Errorf("summary should show 1 succeeded, 1 failed: %q", progress)
	}
}

func TestUsersDelete_NoArgsNoStdinError(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"users", "delete"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no args and no stdin")
	}
	if !strings.Contains(err.Error(), "requires an identifier argument") {
		t.Errorf("error should mention required argument: %q", err.Error())
	}
}

// --- Devices delete --stdin integration tests ---

func TestDevicesDeleteStdin_DeleteMultiple(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	overrideStdinSource(t, "aaa111aaa111aaa111aaa111\nbbb222bbb222bbb222bbb222\n")

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"devices", "delete", "--stdin", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	progress := errBuf.String()
	if !strings.Contains(progress, "2 succeeded, 0 failed") {
		t.Errorf("summary should show 2 succeeded: %q", progress)
	}
}

func TestDevicesDeleteStdin_EmptyStdinNoError(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	overrideStdinSource(t, "")

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "delete", "--stdin"})

	// Same KLA-446 semantics change as the users test above.
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no identifiers") {
		t.Fatalf("expected empty-stdin error, got: %v", err)
	}
}

func TestDevicesDelete_NoArgsNoStdinError(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "delete"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no args and no stdin")
	}
	if !strings.Contains(err.Error(), "requires an identifier argument") {
		t.Errorf("error should mention required argument: %q", err.Error())
	}
}

// --- Groups delete --stdin integration tests ---

func TestGroupsUserDeleteStdin_DeleteMultiple(t *testing.T) {
	setupStdinGroupsTest(t)
	groups := sampleGroups()
	ts := startUserGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	// Use IDs from sampleGroups().
	overrideStdinSource(t, "aabbccddee112233aabb0001\naabbccddee112233aabb0002\n")

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"groups", "user", "delete", "--stdin", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	progress := errBuf.String()
	if !strings.Contains(progress, "2 succeeded, 0 failed") {
		t.Errorf("summary should show 2 succeeded: %q", progress)
	}
}

func TestGroupsDeviceDeleteStdin_DeleteMultiple(t *testing.T) {
	setupStdinGroupsTest(t)
	groups := sampleDeviceGroups()
	ts := startDeviceGroupsServer(t, groups)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	// Use IDs from sampleDeviceGroups().
	overrideStdinSource(t, "dd11ee22ff33dd11ee220001\ndd11ee22ff33dd11ee220002\n")

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"groups", "device", "delete", "--stdin", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	progress := errBuf.String()
	if !strings.Contains(progress, "2 succeeded, 0 failed") {
		t.Errorf("summary should show 2 succeeded: %q", progress)
	}
}

// --- Recipe --params-stdin tests ---

func TestParseParamsFromStdin_ValidJSON(t *testing.T) {
	overrideStdinParamsReader(t, `{"username":"jdoe","email":"jdoe@acme.com"}`)

	params, err := parseParamsFromStdin()
	if err != nil {
		t.Fatalf("parseParamsFromStdin error: %v", err)
	}
	if params["username"] != "jdoe" {
		t.Errorf("expected username=jdoe, got %q", params["username"])
	}
	if params["email"] != "jdoe@acme.com" {
		t.Errorf("expected email=jdoe@acme.com, got %q", params["email"])
	}
}

func TestParseParamsFromStdin_EmptyInput(t *testing.T) {
	overrideStdinParamsReader(t, "")

	params, err := parseParamsFromStdin()
	if err != nil {
		t.Fatalf("parseParamsFromStdin error: %v", err)
	}
	if len(params) != 0 {
		t.Errorf("expected empty params, got %v", params)
	}
}

func TestParseParamsFromStdin_InvalidJSON(t *testing.T) {
	overrideStdinParamsReader(t, "not json")

	_, err := parseParamsFromStdin()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid JSON on stdin") {
		t.Errorf("error should mention invalid JSON: %q", err.Error())
	}
}

func TestParseParamsFromStdin_MixedTypes(t *testing.T) {
	overrideStdinParamsReader(t, `{"name":"test","count":42,"active":true}`)

	params, err := parseParamsFromStdin()
	if err != nil {
		t.Fatalf("parseParamsFromStdin error: %v", err)
	}
	if params["name"] != "test" {
		t.Errorf("expected name=test, got %q", params["name"])
	}
	if params["count"] != "42" {
		t.Errorf("expected count=42, got %q", params["count"])
	}
	if params["active"] != "true" {
		t.Errorf("expected active=true, got %q", params["active"])
	}
}

func TestRecipeRunParamsStdin_Integration(t *testing.T) {
	setupUsersTest(t)

	// Create a test recipe that uses a parameter.
	dir := t.TempDir()
	origRecipesDir := recipe.RecipesDir
	recipe.RecipesDir = func() string { return dir }
	t.Cleanup(func() { recipe.RecipesDir = origRecipesDir })

	recipeYAML := `name: test-stdin
description: Test params-stdin
parameters:
  - name: greeting
    required: true
steps:
  - name: echo
    command: version
`
	if err := os.WriteFile(filepath.Join(dir, "test-stdin.yaml"), []byte(recipeYAML), 0600); err != nil {
		t.Fatalf("writing test recipe: %v", err)
	}

	overrideStdinParamsReader(t, `{"greeting":"hello"}`)

	// Override recipe root command factory.
	origRootCmdForRecipe := newRootCmdForRecipe
	newRootCmdForRecipe = func() recipe.CobraCommand { return NewRootCmd() }
	t.Cleanup(func() { newRootCmdForRecipe = origRootCmdForRecipe })

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"recipe", "run", "test-stdin", "--params-stdin"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}

// --- Test helpers ---

func setupStdinGroupsTest(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	viper.Reset()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_ORG_ID", "")
	t.Setenv("JC_PROFILE", "")
	t.Setenv("JC_OUTPUT", "")

	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(cfgPath, []byte("active_profile: default\nprofiles:\n  default:\n    api_key: test-key-1234\ncache:\n  directory: "+tmp+"/cache\n"), 0600)

	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}
}
