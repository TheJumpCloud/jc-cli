package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTempCSV writes CSV content to a temporary file and returns its path.
func writeTempCSV(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "users.csv")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writing temp CSV: %v", err)
	}
	return path
}

func TestBulkUsersCreate_Success(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	csvPath := writeTempCSV(t, `operation,username,email,firstname,lastname
create,newuser1,newuser1@acme.com,New,User1
create,newuser2,newuser2@acme.com,Another,User2
`)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", csvPath, "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errOut.String()
	if !strings.Contains(stderr, "Processing 1 of 2") {
		t.Errorf("should show progress, got stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "Processing 2 of 2") {
		t.Errorf("should show progress for second row, got stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "2 succeeded, 0 failed") {
		t.Errorf("summary should show 2 succeeded, got stderr: %q", stderr)
	}

	// Verify JSON output contains result rows.
	var results []map[string]any
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatalf("parsing output: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0]["status"] != "succeeded" {
		t.Errorf("first result status = %q, want succeeded", results[0]["status"])
	}
	if results[0]["operation"] != "create" {
		t.Errorf("first result operation = %q, want create", results[0]["operation"])
	}
}

func TestBulkUsersUpdate_Success(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	csvPath := writeTempCSV(t, `operation,_id,email,department
update,aaa111aaa111aaa111aaa111,alice-new@acme.com,Engineering
`)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", csvPath, "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errOut.String()
	if !strings.Contains(stderr, "1 succeeded, 0 failed") {
		t.Errorf("summary should show 1 succeeded, got stderr: %q", stderr)
	}
}

func TestBulkUsersDelete_Success(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	csvPath := writeTempCSV(t, `operation,_id
delete,aaa111aaa111aaa111aaa111
`)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", csvPath, "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errOut.String()
	if !strings.Contains(stderr, "1 succeeded, 0 failed") {
		t.Errorf("summary should show 1 succeeded, got stderr: %q", stderr)
	}
}

func TestBulkUsersMixed_Operations(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	csvPath := writeTempCSV(t, `operation,username,email,firstname,lastname,_id
create,newuser,newuser@acme.com,New,User,
update,,,,,aaa111aaa111aaa111aaa111
delete,,,,,bbb222bbb222bbb222bbb222
`)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", csvPath, "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var results []map[string]any
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatalf("parsing output: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0]["operation"] != "create" {
		t.Errorf("first op = %q, want create", results[0]["operation"])
	}
	if results[1]["operation"] != "update" {
		t.Errorf("second op = %q, want update", results[1]["operation"])
	}
	if results[2]["operation"] != "delete" {
		t.Errorf("third op = %q, want delete", results[2]["operation"])
	}
}

func TestBulkUsersCreate_MissingRequiredField(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	// Missing email for create.
	csvPath := writeTempCSV(t, `operation,username
create,noEmailUser
`)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", csvPath, "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errOut.String()
	if !strings.Contains(stderr, "0 succeeded, 1 failed") {
		t.Errorf("summary should show 1 failed, got stderr: %q", stderr)
	}

	var results []map[string]any
	json.Unmarshal(out.Bytes(), &results)
	if results[0]["status"] != "failed" {
		t.Errorf("status = %q, want failed", results[0]["status"])
	}
	if !strings.Contains(results[0]["error"].(string), "email") {
		t.Errorf("error should mention email, got: %q", results[0]["error"])
	}
}

func TestBulkUsersUpdate_NoIdentifier(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	csvPath := writeTempCSV(t, `operation,email
update,new@acme.com
`)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", csvPath, "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errOut.String()
	if !strings.Contains(stderr, "0 succeeded, 1 failed") {
		t.Errorf("summary should show 1 failed, got stderr: %q", stderr)
	}
}

func TestBulkUsersDelete_NotFound(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	csvPath := writeTempCSV(t, `operation,_id
delete,fff999fff999fff999fff999
`)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", csvPath, "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errOut.String()
	if !strings.Contains(stderr, "0 succeeded, 1 failed") {
		t.Errorf("summary should show 1 failed, got stderr: %q", stderr)
	}
}

func TestBulkUsersPartialFailure(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	// First row succeeds (valid create), second fails (missing email).
	csvPath := writeTempCSV(t, `operation,username,email
create,gooduser,good@acme.com
create,baduser,
`)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", csvPath, "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errOut.String()
	if !strings.Contains(stderr, "1 succeeded, 1 failed") {
		t.Errorf("summary should show 1 succeeded 1 failed, got stderr: %q", stderr)
	}

	var results []map[string]any
	json.Unmarshal(out.Bytes(), &results)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0]["status"] != "succeeded" {
		t.Errorf("first result should be succeeded, got %q", results[0]["status"])
	}
	if results[1]["status"] != "failed" {
		t.Errorf("second result should be failed, got %q", results[1]["status"])
	}
}

func TestBulkUsersPlanMode(t *testing.T) {
	setupUsersTest(t)
	// No mock server needed — plan mode doesn't make API calls.

	csvPath := writeTempCSV(t, `operation,username,email
create,user1,user1@acme.com
delete,user2,
`)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", csvPath, "--plan"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errOut.String()
	if !strings.Contains(stderr, "Plan: 1 creates") {
		t.Errorf("plan should show operation counts, got stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "1 deletes") {
		t.Errorf("plan should show delete count, got stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "No changes made") {
		t.Errorf("plan should say no changes, got stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "[1/2] create user1") {
		t.Errorf("plan should list operations, got stderr: %q", stderr)
	}

	// No JSON output in plan mode.
	if out.Len() > 0 {
		t.Errorf("plan mode should produce no stdout output, got: %q", out.String())
	}
}

func TestBulkUsersConfirmCancel(t *testing.T) {
	setupUsersTest(t)
	overrideConfirmReader(t, "n\n")

	csvPath := writeTempCSV(t, `operation,username,email
create,user1,user1@acme.com
`)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", csvPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errOut.String()
	if !strings.Contains(stderr, "Cancelled") {
		t.Errorf("should show cancelled, got stderr: %q", stderr)
	}
	if out.Len() > 0 {
		t.Errorf("cancelled should produce no stdout, got: %q", out.String())
	}
}

func TestBulkUsersConfirmYes(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideConfirmReader(t, "y\n")

	csvPath := writeTempCSV(t, `operation,username,email
create,user1,user1@acme.com
`)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", csvPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errOut.String()
	if !strings.Contains(stderr, "1 succeeded") {
		t.Errorf("should show 1 succeeded, got stderr: %q", stderr)
	}
}

func TestBulkUsersUnknownOperation(t *testing.T) {
	setupUsersTest(t)

	csvPath := writeTempCSV(t, `operation,username,email
invalid,user1,user1@acme.com
`)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", csvPath, "--force"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown operation")
	}
	if !strings.Contains(err.Error(), "unknown operation") {
		t.Errorf("error should mention unknown operation, got: %q", err.Error())
	}
}

func TestBulkUsersEmptyCSV(t *testing.T) {
	setupUsersTest(t)

	csvPath := writeTempCSV(t, `operation,username,email
`)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", csvPath, "--force"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for empty CSV")
	}
	if !strings.Contains(err.Error(), "at least one data row") {
		t.Errorf("error should mention data row requirement, got: %q", err.Error())
	}
}

func TestBulkUsersNoOperationColumn(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	// No operation column — defaults to create.
	csvPath := writeTempCSV(t, `username,email,firstname
newuser,newuser@acme.com,New
`)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", csvPath, "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errOut.String()
	if !strings.Contains(stderr, "1 succeeded, 0 failed") {
		t.Errorf("should default to create and succeed, got stderr: %q", stderr)
	}
}

func TestBulkUsersMissingFile(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", "/nonexistent/path.csv"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "opening CSV file") {
		t.Errorf("error should mention file opening, got: %q", err.Error())
	}
}

func TestBulkUsersMissingFileFlag(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --file flag")
	}
	if !strings.Contains(err.Error(), "required flag") {
		t.Errorf("error should mention required flag, got: %q", err.Error())
	}
}

func TestBulkUsersTableOutput(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	csvPath := writeTempCSV(t, `operation,username,email
create,user1,user1@acme.com
`)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", csvPath, "--force", "-t"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stdout := out.String()
	if !strings.Contains(stdout, "ROW") {
		t.Errorf("table should have ROW header, got: %q", stdout)
	}
	if !strings.Contains(stdout, "OPERATION") {
		t.Errorf("table should have OPERATION header, got: %q", stdout)
	}
	if !strings.Contains(stdout, "STATUS") {
		t.Errorf("table should have STATUS header, got: %q", stdout)
	}
}

func TestBulkUsersVerboseErrors(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	csvPath := writeTempCSV(t, `operation,username
create,noEmailUser
`)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"bulk", "users", "--file", csvPath, "--force", "--verbose"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errOut.String()
	if !strings.Contains(stderr, "Error:") {
		t.Errorf("verbose should show error details, got stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "email") {
		t.Errorf("verbose error should mention missing email, got stderr: %q", stderr)
	}
}

func TestBulkUsersHelp(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"bulk", "users", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	helpOut := out.String()
	if !strings.Contains(helpOut, "--file") {
		t.Errorf("help should mention --file flag, got: %q", helpOut)
	}
	if !strings.Contains(helpOut, "CSV") {
		t.Errorf("help should mention CSV, got: %q", helpOut)
	}
}

func TestBulkHelpShowsSubcommands(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"bulk", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	helpOut := out.String()
	if !strings.Contains(helpOut, "users") {
		t.Errorf("bulk help should show users subcommand, got: %q", helpOut)
	}
}

func TestRootHelpIncludesBulk(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	helpOut := out.String()
	if !strings.Contains(helpOut, "bulk") {
		t.Errorf("root help should include bulk command, got: %q", helpOut)
	}
}
