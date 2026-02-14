package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// sampleCommands returns a set of test command records.
func sampleCommands() []map[string]any {
	return []map[string]any{
		{
			"_id":                "aaa111aaa111aaa111aaa111",
			"name":              "Update Agents",
			"command":           "apt update && apt upgrade -y",
			"commandType":       "linux",
			"schedule":          "daily",
			"scheduleRepeatType": "day",
		},
		{
			"_id":                "bbb222bbb222bbb222bbb222",
			"name":              "Restart Service",
			"command":           "systemctl restart nginx",
			"commandType":       "linux",
			"schedule":          "",
			"scheduleRepeatType": "",
		},
		{
			"_id":                "ccc333ccc333ccc333ccc333",
			"name":              "Collect Logs",
			"command":           "tar czf /tmp/logs.tar.gz /var/log",
			"commandType":       "mac",
			"schedule":          "weekly",
			"scheduleRepeatType": "week",
		},
	}
}

// startCommandsServer creates a mock JumpCloud server that handles /commands endpoints.
func startCommandsServer(t *testing.T, commands []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /commands — list endpoint.
		if r.URL.Path == "/commands" && r.Method == http.MethodGet {
			skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if limit == 0 {
				limit = 100
			}

			end := skip + limit
			if end > len(commands) {
				end = len(commands)
			}
			var page []map[string]any
			if skip < len(commands) {
				page = commands[skip:end]
			}
			if page == nil {
				page = []map[string]any{}
			}

			resp := map[string]any{
				"results":    page,
				"totalCount": len(commands),
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// POST /commands — create endpoint.
		if r.URL.Path == "/commands" && r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			var input map[string]any
			json.Unmarshal(body, &input)

			input["_id"] = "new123new123new123new123"
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(input)
			return
		}

		// Routes under /commands/{id}.
		if strings.HasPrefix(r.URL.Path, "/commands/") {
			id := strings.TrimPrefix(r.URL.Path, "/commands/")

			switch r.Method {
			case http.MethodGet:
				for _, c := range commands {
					if c["_id"] == id {
						json.NewEncoder(w).Encode(c)
						return
					}
				}
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return

			case http.MethodPut:
				for _, c := range commands {
					if c["_id"] == id {
						body, _ := io.ReadAll(r.Body)
						var updates map[string]any
						json.Unmarshal(body, &updates)
						merged := make(map[string]any)
						for k, v := range c {
							merged[k] = v
						}
						for k, v := range updates {
							merged[k] = v
						}
						json.NewEncoder(w).Encode(merged)
						return
					}
				}
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return

			case http.MethodDelete:
				for _, c := range commands {
					if c["_id"] == id {
						json.NewEncoder(w).Encode(c)
						return
					}
				}
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
		}

		w.WriteHeader(http.StatusNotFound)
	}))
}

// --- List Tests ---

func TestCommandsListJSON(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\nOutput: %s", err, buf.String())
	}
	if len(result) != 3 {
		t.Errorf("expected 3 commands, got %d", len(result))
	}
}

func TestCommandsListTable(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "list", "--output", "table"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Update Agents") {
		t.Errorf("table output should contain 'Update Agents', got: %s", out)
	}
	if !strings.Contains(out, "linux") {
		t.Errorf("table output should contain 'linux', got: %s", out)
	}
}

func TestCommandsListCSV(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "list", "--output", "csv"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected CSV header + data rows, got %d lines", len(lines))
	}
	// Header row should contain default fields.
	if !strings.Contains(lines[0], "name") {
		t.Errorf("CSV header should contain 'name', got: %s", lines[0])
	}
}

func TestCommandsListIDs(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "list", "--ids"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 IDs, got %d lines: %v", len(lines), lines)
	}
	if lines[0] != "aaa111aaa111aaa111aaa111" {
		t.Errorf("expected first ID 'aaa111aaa111aaa111aaa111', got %q", lines[0])
	}
}

func TestCommandsListQuiet(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "list", "--quiet"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("expected empty output with --quiet, got: %s", buf.String())
	}
}

func TestCommandsListFooter(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	stderr := new(bytes.Buffer)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"commands", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	footer := stderr.String()
	if !strings.Contains(footer, "3 items") {
		t.Errorf("footer should contain '3 items', got: %s", footer)
	}
}

func TestCommandsListEmpty(t *testing.T) {
	setupUsersTest(t)
	server := startCommandsServer(t, []map[string]any{})
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result []any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty array, got %d items", len(result))
	}
}

func TestCommandsListLimit(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "list", "--limit", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result []any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 command with --limit 1, got %d", len(result))
	}
}

func TestCommandsListSort(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "list", "--sort", "name"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Just verify the sort parameter is passed (we can't verify sort order from mock).
	if buf.Len() == 0 {
		t.Error("expected output, got empty")
	}
}

func TestCommandsListFilter(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "list", "--filter", "commandType=linux"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the command succeeds — the actual filtering happens server-side.
	var result []any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

func TestCommandsListInvalidFilter(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "list", "--filter", "invalid"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid filter")
	}
	if !strings.Contains(err.Error(), "invalid filter") {
		t.Errorf("expected 'invalid filter' error, got: %v", err)
	}
}

func TestCommandsListSearch(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "list", "--search", "apt"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the command succeeds.
	var result []any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

// --- Get Tests ---

func TestCommandsGetByID(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "get", "aaa111aaa111aaa111aaa111"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["name"] != "Update Agents" {
		t.Errorf("expected name 'Update Agents', got %v", result["name"])
	}
}

func TestCommandsGetByName(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	// Disable cache so resolution goes through API.
	viper.Set("cache.enabled", false)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "get", "Update Agents"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["name"] != "Update Agents" {
		t.Errorf("expected name 'Update Agents', got %v", result["name"])
	}
}

func TestCommandsGetNotFound(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "get", "fff999fff999fff999fff999"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found command")
	}
}

func TestCommandsGetMissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "get"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

// --- Create Tests ---

func TestCommandsCreateFull(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "create", "--name", "Test Command", "--command", "echo hello", "--type", "linux"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["name"] != "Test Command" {
		t.Errorf("expected name 'Test Command', got %v", result["name"])
	}
	if result["command"] != "echo hello" {
		t.Errorf("expected command 'echo hello', got %v", result["command"])
	}
	if result["commandType"] != "linux" {
		t.Errorf("expected commandType 'linux', got %v", result["commandType"])
	}
}

func TestCommandsCreateMissingName(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "create", "--command", "echo hello", "--type", "linux"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --name")
	}
}

func TestCommandsCreateMissingCommand(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "create", "--name", "Test", "--type", "linux"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --command")
	}
}

func TestCommandsCreateMissingType(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "create", "--name", "Test", "--command", "echo hello"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --type")
	}
}

func TestCommandsCreateAPIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var receivedPath string
	var receivedMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"_id":"new123new123new123new123","name":"Test","command":"echo","commandType":"linux"}`))
	}))
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "create", "--name", "Test", "--command", "echo", "--type", "linux"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedPath != "/commands" {
		t.Errorf("expected POST /commands, got %s %s", receivedMethod, receivedPath)
	}
	if receivedMethod != "POST" {
		t.Errorf("expected POST method, got %s", receivedMethod)
	}
}

// --- Update Tests ---

func TestCommandsUpdateByID(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "update", "aaa111aaa111aaa111aaa111", "--command", "apt upgrade -y"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["command"] != "apt upgrade -y" {
		t.Errorf("expected command 'apt upgrade -y', got %v", result["command"])
	}
	// Existing fields should be preserved.
	if result["name"] != "Update Agents" {
		t.Errorf("expected name preserved as 'Update Agents', got %v", result["name"])
	}
}

func TestCommandsUpdateByName(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	viper.Set("cache.enabled", false)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "update", "Update Agents", "--command", "apt upgrade -y"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["command"] != "apt upgrade -y" {
		t.Errorf("expected command 'apt upgrade -y', got %v", result["command"])
	}
}

func TestCommandsUpdateNoFields(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "update", "aaa111aaa111aaa111aaa111"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no fields specified")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("expected 'no fields to update' error, got: %v", err)
	}
}

func TestCommandsUpdateAPIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var receivedPath string
	var receivedMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"_id":"aaa111aaa111aaa111aaa111","name":"Updated","command":"echo","commandType":"linux"}`))
	}))
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "update", "aaa111aaa111aaa111aaa111", "--name", "Updated"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedPath != "/commands/aaa111aaa111aaa111aaa111" {
		t.Errorf("expected PUT /commands/aaa111aaa111aaa111aaa111, got %s %s", receivedMethod, receivedPath)
	}
	if receivedMethod != "PUT" {
		t.Errorf("expected PUT method, got %s", receivedMethod)
	}
}

// --- Delete Tests ---

func TestCommandsDeleteForce(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "delete", "aaa111aaa111aaa111aaa111", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("expected 'deleted successfully', got: %s", out)
	}
	if !strings.Contains(out, "Update Agents") {
		t.Errorf("expected command name in output, got: %s", out)
	}
}

func TestCommandsDeleteForceByName(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	viper.Set("cache.enabled", false)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "delete", "Update Agents", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("expected 'deleted successfully', got: %s", out)
	}
}

func TestCommandsDeleteConfirmYes(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)
	overrideConfirmReader(t, "y\n")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "delete", "aaa111aaa111aaa111aaa111"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("expected 'deleted successfully', got: %s", out)
	}
}

func TestCommandsDeleteConfirmNo(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)
	overrideConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"commands", "delete", "aaa111aaa111aaa111aaa111"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(buf.String(), "deleted") {
		t.Error("should not have deleted when user said no")
	}
	if !strings.Contains(stderr.String(), "Cancelled") {
		t.Errorf("expected 'Cancelled' on stderr, got: %s", stderr.String())
	}
}

func TestCommandsDeleteConfirmEmpty(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)
	overrideConfirmReader(t, "\n")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"commands", "delete", "aaa111aaa111aaa111aaa111"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(buf.String(), "deleted") {
		t.Error("empty input should default to no")
	}
}

func TestCommandsDeleteNotFound(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "delete", "fff999fff999fff999fff999", "--force"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found command")
	}
}

func TestCommandsDeleteMissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "delete"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

func TestCommandsDeletePromptShowsName(t *testing.T) {
	setupUsersTest(t)
	commands := sampleCommands()
	server := startCommandsServer(t, commands)
	defer server.Close()
	overrideV1Client(t, server.URL)
	overrideConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	stderr := new(bytes.Buffer)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"commands", "delete", "aaa111aaa111aaa111aaa111"})
	_ = cmd.Execute()

	prompt := stderr.String()
	if !strings.Contains(prompt, "Update Agents") {
		t.Errorf("delete prompt should show command name, got: %s", prompt)
	}
	if !strings.Contains(prompt, "linux") {
		t.Errorf("delete prompt should show command type, got: %s", prompt)
	}
}

// --- Help Tests ---

func TestCommandsHelpIncludesSubcommands(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "--help"})
	_ = cmd.Execute()

	help := buf.String()
	for _, sub := range []string{"list", "get", "create", "update", "delete"} {
		if !strings.Contains(help, sub) {
			t.Errorf("commands help should include %q subcommand, got: %s", sub, help)
		}
	}
}

func TestCommandsListHelpIncludesFlags(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "list", "--help"})
	_ = cmd.Execute()

	help := buf.String()
	for _, flag := range []string{"--limit", "--sort", "--filter", "--search"} {
		if !strings.Contains(help, flag) {
			t.Errorf("commands list help should include %q flag, got: %s", flag, help)
		}
	}
}

func TestRootHelpIncludesCommands(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--help"})
	_ = cmd.Execute()

	help := buf.String()
	if !strings.Contains(help, "commands") {
		t.Errorf("root help should include 'commands', got: %s", help)
	}
}
