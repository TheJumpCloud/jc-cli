package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestExplain_UsersDelete(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "users", "delete", "jdoe"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "delete") {
		t.Errorf("expected 'delete' in output, got: %s", output)
	}
	if !strings.Contains(output, "users") {
		t.Errorf("expected 'users' in output, got: %s", output)
	}
	if !strings.Contains(output, "DESTRUCTIVE") {
		t.Errorf("expected destructive warning in output, got: %s", output)
	}
	if !strings.Contains(output, "irreversible") {
		t.Errorf("expected 'irreversible' in output, got: %s", output)
	}
	if !strings.Contains(output, "No action taken") {
		t.Errorf("expected 'No action taken' in output, got: %s", output)
	}
}

func TestExplain_UsersDelete_JSON(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "users", "delete", "jdoe", "--output", "json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var explanation Explanation
	if err := json.Unmarshal(out.Bytes(), &explanation); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, out.String())
	}

	if explanation.Action != "delete" {
		t.Errorf("expected action 'delete', got %q", explanation.Action)
	}
	if explanation.Resource != "users" {
		t.Errorf("expected resource 'users', got %q", explanation.Resource)
	}
	if explanation.Reversible {
		t.Error("expected reversible=false for delete")
	}
	if !explanation.Destructive {
		t.Error("expected destructive=true for delete")
	}
	if !explanation.RequiresAuth {
		t.Error("expected requires_auth=true")
	}
	if len(explanation.SideEffects) == 0 {
		t.Error("expected side effects for delete")
	}
	if len(explanation.Warnings) == 0 {
		t.Error("expected warnings for delete")
	}
	if explanation.Command != "users delete jdoe" {
		t.Errorf("expected command 'users delete jdoe', got %q", explanation.Command)
	}
}

func TestExplain_UsersList(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "users", "list"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "List all JumpCloud system users") {
		t.Errorf("expected users list description, got: %s", output)
	}
	if strings.Contains(output, "DESTRUCTIVE") {
		t.Errorf("list should not be destructive, got: %s", output)
	}
}

func TestExplain_UsersList_JSON(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "users", "list", "--output", "json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var explanation Explanation
	if err := json.Unmarshal(out.Bytes(), &explanation); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if explanation.Action != "list" {
		t.Errorf("expected action 'list', got %q", explanation.Action)
	}
	if !explanation.Reversible {
		t.Error("list should be reversible")
	}
	if explanation.Destructive {
		t.Error("list should not be destructive")
	}
}

func TestExplain_DevicesErase(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "devices", "erase", "MY-LAPTOP"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "DESTRUCTIVE") {
		t.Errorf("expected destructive warning for erase, got: %s", output)
	}
	if !strings.Contains(output, "EXTREMELY DESTRUCTIVE") {
		t.Errorf("expected 'EXTREMELY DESTRUCTIVE' warning, got: %s", output)
	}
	if !strings.Contains(output, "confirm-erase") {
		t.Errorf("expected mention of --confirm-erase flag, got: %s", output)
	}
}

func TestExplain_DevicesErase_JSON(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "devices", "erase", "--output", "json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var explanation Explanation
	if err := json.Unmarshal(out.Bytes(), &explanation); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if explanation.Action != "erase" {
		t.Errorf("expected action 'erase', got %q", explanation.Action)
	}
	if !explanation.Destructive {
		t.Error("erase should be destructive")
	}
	if explanation.Reversible {
		t.Error("erase should not be reversible")
	}
}

func TestExplain_GroupsAddMember(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	// Flags like --user are part of the explained command, pass as a single quoted string.
	cmd.SetArgs([]string{"explain", "groups", "add-member"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Add a user or device to a group") {
		t.Errorf("expected add-member description, got: %s", output)
	}
}

func TestExplain_GroupsUserList(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "groups", "user", "list"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "List all user groups") {
		t.Errorf("expected groups user list description, got: %s", output)
	}
}

func TestExplain_GroupsUserDelete(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "groups", "user", "delete", "Engineering", "--output", "json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var explanation Explanation
	if err := json.Unmarshal(out.Bytes(), &explanation); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if explanation.Action != "delete" {
		t.Errorf("expected action 'delete', got %q", explanation.Action)
	}
	if explanation.Resource != "groups user" {
		t.Errorf("expected resource 'groups user', got %q", explanation.Resource)
	}
	if !explanation.Destructive {
		t.Error("group delete should be destructive")
	}
	if explanation.Reversible {
		t.Error("group delete should not be reversible")
	}
}

func TestExplain_GroupsDeviceOnly(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "groups", "device"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Manage JumpCloud device groups") {
		t.Errorf("expected device groups description, got: %s", output)
	}
}

func TestExplain_ResourceOnly(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "users"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Manage JumpCloud system users") {
		t.Errorf("expected resource description, got: %s", output)
	}
}

func TestExplain_UnknownCommand(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "foobar"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Unknown command") {
		t.Errorf("expected 'Unknown command' message, got: %s", output)
	}
	if !strings.Contains(output, "jc --help") {
		t.Errorf("expected suggestion to run --help, got: %s", output)
	}
}

func TestExplain_UnknownVerb(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "users", "foobar"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Unknown subcommand") {
		t.Errorf("expected 'Unknown subcommand' message, got: %s", output)
	}
}

func TestExplain_MissingArgs(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing args")
	}
}

func TestExplain_QuotedCommandString(t *testing.T) {
	// Test that "users delete jdoe" as a single arg works.
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "users delete jdoe"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "delete") {
		t.Errorf("expected 'delete' in output, got: %s", output)
	}
}

func TestExplain_InsightsQuery(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "insights", "query", "--output", "json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var explanation Explanation
	if err := json.Unmarshal(out.Bytes(), &explanation); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if explanation.Action != "query" {
		t.Errorf("expected action 'query', got %q", explanation.Action)
	}
	if explanation.Resource != "insights" {
		t.Errorf("expected resource 'insights', got %q", explanation.Resource)
	}
	if !explanation.RequiresAuth {
		t.Error("insights query should require auth")
	}
}

func TestExplain_CommandsRun(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "commands", "run", "--output", "json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var explanation Explanation
	if err := json.Unmarshal(out.Bytes(), &explanation); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if explanation.Action != "run" {
		t.Errorf("expected action 'run', got %q", explanation.Action)
	}
	if explanation.Reversible {
		t.Error("commands run should not be reversible")
	}
	if len(explanation.Warnings) == 0 {
		t.Error("commands run should have warnings")
	}
}

func TestExplain_BulkUsers(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "bulk", "users", "--output", "json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var explanation Explanation
	if err := json.Unmarshal(out.Bytes(), &explanation); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if explanation.Action != "users" {
		t.Errorf("expected action 'users', got %q", explanation.Action)
	}
	if explanation.Resource != "bulk" {
		t.Errorf("expected resource 'bulk', got %q", explanation.Resource)
	}
	if len(explanation.Warnings) == 0 {
		t.Error("bulk users should have warnings")
	}
}

func TestExplain_RecipeRun(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "recipe", "run", "--output", "json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var explanation Explanation
	if err := json.Unmarshal(out.Bytes(), &explanation); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if explanation.Action != "run" {
		t.Errorf("expected action 'run', got %q", explanation.Action)
	}
	if len(explanation.Warnings) == 0 {
		t.Error("recipe run should have warnings about multiple operations")
	}
}

func TestExplain_AuthLogin(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "auth", "login", "--output", "json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var explanation Explanation
	if err := json.Unmarshal(out.Bytes(), &explanation); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if explanation.RequiresAuth {
		t.Error("auth login should not require auth")
	}
}

func TestExplain_AuthLogout_SideEffects(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "auth", "logout", "--output", "json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var explanation Explanation
	if err := json.Unmarshal(out.Bytes(), &explanation); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if len(explanation.SideEffects) == 0 {
		t.Error("auth logout should have side effects about credential removal")
	}
}

func TestExplain_HelpOutput(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"explain", "--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Explain describes what a jc command would do") {
		t.Errorf("expected explain help text, got: %s", output)
	}
}

func TestExplain_RootHelpIncludesExplain(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "explain") {
		t.Errorf("expected root help to include 'explain', got: %s", output)
	}
}

func TestExplain_AllKnownResources(t *testing.T) {
	resources := []string{"users", "devices", "groups", "insights", "commands", "policies", "apps", "admins", "graph", "bulk", "recipe", "auth", "config", "schema", "mcp"}
	for _, resource := range resources {
		t.Run(resource, func(t *testing.T) {
			cmd := NewRootCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"explain", resource, "--output", "json"})

			err := cmd.Execute()
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", resource, err)
			}

			var explanation Explanation
			if err := json.Unmarshal(out.Bytes(), &explanation); err != nil {
				t.Fatalf("failed to parse JSON for %s: %v\noutput: %s", resource, err, out.String())
			}

			if explanation.Resource != resource {
				t.Errorf("expected resource %q, got %q", resource, explanation.Resource)
			}
			if explanation.Action != "manage" {
				t.Errorf("expected action 'manage' for resource-only, got %q", explanation.Action)
			}
		})
	}
}

func TestBuildExplanation_UsersLock(t *testing.T) {
	e := buildExplanation([]string{"users", "lock", "jdoe"})
	if e.Action != "lock" {
		t.Errorf("expected action 'lock', got %q", e.Action)
	}
	if !e.Reversible {
		t.Error("lock should be reversible (can unlock)")
	}
	if len(e.SideEffects) == 0 {
		t.Error("lock should have side effects")
	}
}

func TestBuildExplanation_UsersResetMFA(t *testing.T) {
	e := buildExplanation([]string{"users", "reset-mfa", "jdoe"})
	if e.Action != "reset-mfa" {
		t.Errorf("expected action 'reset-mfa', got %q", e.Action)
	}
	if e.Reversible {
		t.Error("reset-mfa should not be reversible")
	}
}

func TestWrapText(t *testing.T) {
	tests := []struct {
		text     string
		maxWidth int
		want     int // expected number of lines
	}{
		{"short", 50, 1},
		{"this is a longer text that should wrap", 20, 3},
		{"", 50, 1},
		{"word", 4, 1},
	}
	for _, tt := range tests {
		lines := wrapText(tt.text, tt.maxWidth)
		if len(lines) != tt.want {
			t.Errorf("wrapText(%q, %d) = %d lines, want %d", tt.text, tt.maxWidth, len(lines), tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"this is long", 8, "this ..."},
		{"exact", 5, "exact"},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}
