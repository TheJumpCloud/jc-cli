package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/plan"
)

// --- Users Plan Mode Tests ---

func TestUsersCreate_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"users", "create", "--username", "newuser", "--email", "new@acme.com", "--plan"})

	err := cmd.Execute()

	// Plan mode should return ExitError with code 10.
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exitErr.Code != plan.ExitCodePlan {
		t.Errorf("exit code = %d, want %d", exitErr.Code, plan.ExitCodePlan)
	}

	// stderr should contain plan output.
	stderr := errBuf.String()
	if !strings.Contains(stderr, "create") {
		t.Errorf("plan should mention 'create', got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "user") {
		t.Errorf("plan should mention 'user', got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "newuser") {
		t.Errorf("plan should mention target 'newuser', got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "No changes made") {
		t.Errorf("plan should contain 'No changes made', got:\n%s", stderr)
	}

	// stdout should be empty (no data written).
	if out.Len() != 0 {
		t.Errorf("stdout should be empty in plan mode, got: %s", out.String())
	}
}

func TestUsersCreate_PlanJSON(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"users", "create", "--username", "newuser", "--email", "new@acme.com", "--plan", "--output", "json"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}

	// JSON plan should be on stdout.
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, out.String())
	}
	if result["action"] != "create" {
		t.Errorf("action = %v, want create", result["action"])
	}
	if result["resource"] != "user" {
		t.Errorf("resource = %v, want user", result["resource"])
	}
}

func TestUsersUpdate_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"users", "update", "aaa111aaa111aaa111aaa111", "--department", "Engineering", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exitErr.Code != plan.ExitCodePlan {
		t.Errorf("exit code = %d, want %d", exitErr.Code, plan.ExitCodePlan)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "update") {
		t.Errorf("plan should mention 'update', got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "department") {
		t.Errorf("plan should mention 'department', got:\n%s", stderr)
	}
}

func TestUsersUpdate_PlanJSON(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"users", "update", "aaa111aaa111aaa111aaa111", "--department", "Engineering", "--plan", "--output", "json"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, out.String())
	}
	if result["action"] != "update" {
		t.Errorf("action = %v, want update", result["action"])
	}
	effects, ok := result["effects"].([]any)
	if !ok || len(effects) == 0 {
		t.Errorf("expected effects array, got %v", result["effects"])
	}
}

func TestUsersDelete_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"users", "delete", "aaa111aaa111aaa111aaa111", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exitErr.Code != plan.ExitCodePlan {
		t.Errorf("exit code = %d, want %d", exitErr.Code, plan.ExitCodePlan)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "delete") {
		t.Errorf("plan should mention 'delete', got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "alice") {
		t.Errorf("plan should mention target user 'alice', got:\n%s", stderr)
	}
	// stdout should be empty.
	if out.Len() != 0 {
		t.Errorf("stdout should be empty in plan mode, got: %s", out.String())
	}
}

func TestUsersLock_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"users", "lock", "aaa111aaa111aaa111aaa111", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exitErr.Code != plan.ExitCodePlan {
		t.Errorf("exit code = %d, want %d", exitErr.Code, plan.ExitCodePlan)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "lock") {
		t.Errorf("plan should mention 'lock', got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "alice") {
		t.Errorf("plan should mention target user, got:\n%s", stderr)
	}
}

func TestUsersUnlock_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startUsersServer(t, sampleUsers())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"users", "unlock", "aaa111aaa111aaa111aaa111", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "unlock") {
		t.Errorf("plan should mention 'unlock', got:\n%s", stderr)
	}
}

// --- Devices Plan Mode Tests ---

func TestDevicesDelete_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"devices", "delete", "aaa111aaa111aaa111aaa111", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exitErr.Code != plan.ExitCodePlan {
		t.Errorf("exit code = %d, want %d", exitErr.Code, plan.ExitCodePlan)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "delete") {
		t.Errorf("plan should mention 'delete', got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "device") {
		t.Errorf("plan should mention 'device', got:\n%s", stderr)
	}
}

func TestDevicesLock_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"devices", "lock", "aaa111aaa111aaa111aaa111", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "lock") {
		t.Errorf("plan should mention 'lock', got:\n%s", stderr)
	}
}

func TestDevicesErase_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"devices", "erase", "aaa111aaa111aaa111aaa111", "--confirm-erase", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "erase") {
		t.Errorf("plan should mention 'erase', got:\n%s", stderr)
	}
}

// --- Groups Plan Mode Tests ---

func TestGroupsUserCreate_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startUserGroupsServer(t, sampleGroups())
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"groups", "user", "create", "--name", "NewGroup", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exitErr.Code != plan.ExitCodePlan {
		t.Errorf("exit code = %d, want %d", exitErr.Code, plan.ExitCodePlan)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "create") {
		t.Errorf("plan should mention 'create', got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "NewGroup") {
		t.Errorf("plan should mention group name, got:\n%s", stderr)
	}
}

func TestGroupsUserDelete_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startUserGroupsServer(t, sampleGroups())
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"groups", "user", "delete", "aabbccddee112233aabb0001", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "delete") {
		t.Errorf("plan should mention 'delete', got:\n%s", stderr)
	}
}

func TestGroupsUserUpdate_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startUserGroupsServer(t, sampleGroups())
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"groups", "user", "update", "aabbccddee112233aabb0001", "--name", "Renamed", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "update") {
		t.Errorf("plan should mention 'update', got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "name") {
		t.Errorf("plan should mention the field being updated, got:\n%s", stderr)
	}
}

func TestGroupsDeviceCreate_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startDeviceGroupsServer(t, sampleDeviceGroups())
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"groups", "device", "create", "--name", "NewDeviceGroup", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "create") {
		t.Errorf("plan should mention 'create', got:\n%s", stderr)
	}
}

func TestGroupsDeviceDelete_Plan(t *testing.T) {
	setupUsersTest(t)
	ts := startDeviceGroupsServer(t, sampleDeviceGroups())
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"groups", "device", "delete", "dd11ee22ff33dd11ee220001", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "delete") {
		t.Errorf("plan should mention 'delete', got:\n%s", stderr)
	}
}

func TestGroupsAddMember_Plan(t *testing.T) {
	var records []membershipRecord
	ts := setupMembershipTest(t, &records)
	defer ts.Close()

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"groups", "add-member", "aabbccddee112233aabb0001", "--user", "aa11bb22cc33dd44ee550001", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "add") {
		t.Errorf("plan should mention 'add', got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "member") {
		t.Errorf("plan should mention 'member', got:\n%s", stderr)
	}

	// No membership records should have been created.
	if len(records) != 0 {
		t.Errorf("plan mode should not create membership records, got %d", len(records))
	}
}

func TestGroupsRemoveMember_Plan(t *testing.T) {
	var records []membershipRecord
	ts := setupMembershipTest(t, &records)
	defer ts.Close()

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"groups", "remove-member", "aabbccddee112233aabb0001", "--user", "aa11bb22cc33dd44ee550001", "--plan"})

	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "remove") {
		t.Errorf("plan should mention 'remove', got:\n%s", stderr)
	}

	// No membership records should have been created.
	if len(records) != 0 {
		t.Errorf("plan mode should not create membership records, got %d", len(records))
	}
}
