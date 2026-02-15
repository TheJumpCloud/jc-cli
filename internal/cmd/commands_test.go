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

// ========================================================================
// Run and Results Tests
// ========================================================================

// triggerRecord captures a /runcommand POST request for test assertions.
type triggerRecord struct {
	Command      string   `json:"command"`
	Systems      []string `json:"systems"`
	SystemGroups []string `json:"systemGroups"`
}

// sampleCommandResults returns test command result records.
func sampleCommandResults() []map[string]any {
	return []map[string]any{
		{
			"_id":         "res111res111res111res111",
			"command":     "aaa111aaa111aaa111aaa111",
			"system":      "alice-mbp.local",
			"systemId":    "ddd444ddd444ddd444ddd444",
			"exitCode":    0,
			"requestTime": "2026-02-13T10:00:00Z",
			"responseTime": "2026-02-13T10:00:05Z",
			"response": map[string]any{
				"data": map[string]any{
					"output": "0 packages upgraded",
				},
				"error": "",
			},
		},
		{
			"_id":         "res222res222res222res222",
			"command":     "aaa111aaa111aaa111aaa111",
			"system":      "bob-linux.local",
			"systemId":    "eee555eee555eee555eee555",
			"exitCode":    1,
			"requestTime": "2026-02-13T10:00:00Z",
			"responseTime": "2026-02-13T10:00:10Z",
			"response": map[string]any{
				"data": map[string]any{
					"output": "",
				},
				"error": "permission denied",
			},
		},
	}
}

// sampleRunDevices returns test device records for run tests.
func sampleRunDevices() []map[string]any {
	return []map[string]any{
		{"_id": "ddd444ddd444ddd444ddd444", "hostname": "alice-mbp.local", "os": "Mac OS X"},
		{"_id": "eee555eee555eee555eee555", "hostname": "bob-linux.local", "os": "Ubuntu"},
	}
}

// sampleRunDeviceGroups returns V2 device groups for run tests.
func sampleRunDeviceGroups() []map[string]any {
	return []map[string]any{
		{"id": "ff7777ff7777ff7777ff7777", "name": "macOS Fleet", "type": "custom"},
	}
}

// startCommandsRunServer creates a combined V1+V2 mock server for command run/results tests.
func startCommandsRunServer(t *testing.T, commands []map[string]any, devices []map[string]any, deviceGroups []map[string]any, results []map[string]any, triggers *[]triggerRecord) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// V1: GET /commands — list for command name resolution.
		if r.URL.Path == "/commands" && r.Method == http.MethodGet {
			resp := map[string]any{"results": commands, "totalCount": len(commands)}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// V1: GET /commands/{id} — get a single command.
		if strings.HasPrefix(r.URL.Path, "/commands/") && r.Method == http.MethodGet {
			id := strings.TrimPrefix(r.URL.Path, "/commands/")
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

		// V1: GET /systems/{id} — get single device (for ID verification).
		if strings.HasPrefix(r.URL.Path, "/systems/") && r.Method == http.MethodGet {
			id := strings.TrimPrefix(r.URL.Path, "/systems/")
			for _, d := range devices {
				if d["_id"] == id {
					json.NewEncoder(w).Encode(d)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
			return
		}

		// V1: GET /systems — list for device name resolution.
		if r.URL.Path == "/systems" && r.Method == http.MethodGet {
			resp := map[string]any{"results": devices, "totalCount": len(devices)}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// V2: GET /systemgroups — list for device group name resolution.
		if r.URL.Path == "/systemgroups" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(deviceGroups)
			return
		}

		// V1: POST /runcommand — trigger a command.
		if r.URL.Path == "/runcommand" && r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			var trigger triggerRecord
			json.Unmarshal(body, &trigger)
			if triggers != nil {
				*triggers = append(*triggers, trigger)
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
			return
		}

		// V1: GET /commandresults — list command results.
		if r.URL.Path == "/commandresults" && r.Method == http.MethodGet {
			skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if limit == 0 {
				limit = 100
			}

			// Filter by command ID if filter param is present.
			filtered := results
			for _, f := range r.URL.Query()["filter"] {
				if strings.HasPrefix(f, "command:$eq:") {
					cmdID := strings.TrimPrefix(f, "command:$eq:")
					var matching []map[string]any
					for _, res := range results {
						if res["command"] == cmdID {
							matching = append(matching, res)
						}
					}
					filtered = matching
				}
			}

			end := skip + limit
			if end > len(filtered) {
				end = len(filtered)
			}
			var page []map[string]any
			if skip < len(filtered) {
				page = filtered[skip:end]
			}
			if page == nil {
				page = []map[string]any{}
			}

			resp := map[string]any{"results": page, "totalCount": len(filtered)}
			json.NewEncoder(w).Encode(resp)
			return
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

// setupCommandsRunTest sets up a clean test environment for run/results tests.
func setupCommandsRunTest(t *testing.T, triggers *[]triggerRecord) *httptest.Server {
	t.Helper()
	setupUsersTest(t)
	viper.Set("cache.directory", t.TempDir())
	ts := startCommandsRunServer(t, sampleCommands(), sampleRunDevices(), sampleRunDeviceGroups(), sampleCommandResults(), triggers)
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)
	return ts
}

// --- Run Tests ---

func TestCommandsRunOnDeviceForce(t *testing.T) {
	var triggers []triggerRecord
	ts := setupCommandsRunTest(t, &triggers)
	defer ts.Close()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "run", "aaa111aaa111aaa111aaa111", "--on", "ddd444ddd444ddd444ddd444", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "triggered") {
		t.Errorf("expected 'triggered' in output, got: %s", out)
	}

	if len(triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(triggers))
	}
	if triggers[0].Command != "aaa111aaa111aaa111aaa111" {
		t.Errorf("trigger command = %q, want aaa111aaa111aaa111aaa111", triggers[0].Command)
	}
	if len(triggers[0].Systems) != 1 || triggers[0].Systems[0] != "ddd444ddd444ddd444ddd444" {
		t.Errorf("trigger systems = %v, want [ddd444ddd444ddd444ddd444]", triggers[0].Systems)
	}
}

func TestCommandsRunOnDeviceByName(t *testing.T) {
	var triggers []triggerRecord
	ts := setupCommandsRunTest(t, &triggers)
	defer ts.Close()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "run", "aaa111aaa111aaa111aaa111", "--on", "alice-mbp.local", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "triggered") {
		t.Errorf("expected 'triggered' in output, got: %s", out)
	}
	if !strings.Contains(out, "1 device") {
		t.Errorf("expected '1 device' in output, got: %s", out)
	}

	if len(triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(triggers))
	}
	if triggers[0].Systems[0] != "ddd444ddd444ddd444ddd444" {
		t.Errorf("trigger systems = %v, want [ddd444ddd444ddd444ddd444]", triggers[0].Systems)
	}
}

func TestCommandsRunOnGroupForce(t *testing.T) {
	var triggers []triggerRecord
	ts := setupCommandsRunTest(t, &triggers)
	defer ts.Close()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "run", "aaa111aaa111aaa111aaa111", "--on", "macOS Fleet", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "triggered") {
		t.Errorf("expected 'triggered' in output, got: %s", out)
	}
	if !strings.Contains(out, "device group") {
		t.Errorf("expected 'device group' in output, got: %s", out)
	}

	if len(triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(triggers))
	}
	if len(triggers[0].SystemGroups) != 1 || triggers[0].SystemGroups[0] != "ff7777ff7777ff7777ff7777" {
		t.Errorf("trigger systemGroups = %v, want [ff7777ff7777ff7777ff7777]", triggers[0].SystemGroups)
	}
}

func TestCommandsRunOnGroupByID(t *testing.T) {
	var triggers []triggerRecord
	ts := setupCommandsRunTest(t, &triggers)
	defer ts.Close()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "run", "aaa111aaa111aaa111aaa111", "--on", "ff7777ff7777ff7777ff7777", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A 24-char hex ID will go through device resolution first.
	// Since this ID doesn't match any device, it falls back to group resolution.
	// But since the resolve sees it as an ID (24-char hex), it passes it through directly.
	// The V1 device GET will fail (not found), so it tries V2 group resolution.
	// Actually, IsID("ggg777...") returns true, so device resolution passes it directly,
	// and the client.Get("/systems/ggg777...") will fail with 404.
	// Then it falls through to group resolution where IsID also returns true,
	// passing it directly as a group ID.
	if len(triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(triggers))
	}
}

func TestCommandsRunConfirmYes(t *testing.T) {
	var triggers []triggerRecord
	ts := setupCommandsRunTest(t, &triggers)
	defer ts.Close()
	overrideConfirmReader(t, "y\n")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "run", "aaa111aaa111aaa111aaa111", "--on", "ddd444ddd444ddd444ddd444"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "triggered") {
		t.Errorf("expected 'triggered' in output, got: %s", out)
	}
	if len(triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(triggers))
	}
}

func TestCommandsRunConfirmNo(t *testing.T) {
	var triggers []triggerRecord
	ts := setupCommandsRunTest(t, &triggers)
	defer ts.Close()
	overrideConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"commands", "run", "aaa111aaa111aaa111aaa111", "--on", "ddd444ddd444ddd444ddd444"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(buf.String(), "triggered") {
		t.Error("should not have triggered when user said no")
	}
	if !strings.Contains(stderr.String(), "Cancelled") {
		t.Errorf("expected 'Cancelled' on stderr, got: %s", stderr.String())
	}
	if len(triggers) != 0 {
		t.Errorf("expected 0 triggers, got %d", len(triggers))
	}
}

func TestCommandsRunMissingOn(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "run", "aaa111aaa111aaa111aaa111"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --on flag")
	}
}

func TestCommandsRunMissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "run"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

func TestCommandsRunUnresolvableTarget(t *testing.T) {
	setupUsersTest(t)
	viper.Set("cache.directory", t.TempDir())

	// Server that has no matching devices or groups.
	ts := startCommandsRunServer(t, sampleCommands(), []map[string]any{}, []map[string]any{}, nil, nil)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "run", "aaa111aaa111aaa111aaa111", "--on", "nonexistent-host", "--force"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unresolvable target")
	}
	if !strings.Contains(err.Error(), "could not resolve") {
		t.Errorf("expected 'could not resolve' error, got: %v", err)
	}
}

func TestCommandsRunAPIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedPath string
	var capturedMethod string
	var capturedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Handle /commands for command resolution.
		if r.URL.Path == "/commands" && r.Method == http.MethodGet {
			resp := map[string]any{"results": sampleCommands(), "totalCount": 3}
			json.NewEncoder(w).Encode(resp)
			return
		}
		// Handle /systems for device resolution.
		if r.URL.Path == "/systems" && r.Method == http.MethodGet {
			resp := map[string]any{"results": sampleRunDevices(), "totalCount": 2}
			json.NewEncoder(w).Encode(resp)
			return
		}
		// Capture the run command request.
		if r.URL.Path == "/runcommand" && r.Method == http.MethodPost {
			capturedPath = r.URL.Path
			capturedMethod = r.Method
			body, _ := io.ReadAll(r.Body)
			json.Unmarshal(body, &capturedBody)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "run", "aaa111aaa111aaa111aaa111", "--on", "ddd444ddd444ddd444ddd444", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedPath != "/runcommand" {
		t.Errorf("expected POST /runcommand, got %s %s", capturedMethod, capturedPath)
	}
	if capturedMethod != "POST" {
		t.Errorf("expected POST method, got %s", capturedMethod)
	}
	if capturedBody["command"] != "aaa111aaa111aaa111aaa111" {
		t.Errorf("body command = %v, want aaa111aaa111aaa111aaa111", capturedBody["command"])
	}
}

func TestCommandsRunPromptShowsTarget(t *testing.T) {
	var triggers []triggerRecord
	ts := setupCommandsRunTest(t, &triggers)
	defer ts.Close()
	overrideConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	stderr := new(bytes.Buffer)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"commands", "run", "aaa111aaa111aaa111aaa111", "--on", "ddd444ddd444ddd444ddd444"})
	_ = cmd.Execute()

	prompt := stderr.String()
	if !strings.Contains(prompt, "1 device") {
		t.Errorf("run prompt should show target info, got: %s", prompt)
	}
}

func TestCommandsRunByCommandName(t *testing.T) {
	var triggers []triggerRecord
	ts := setupCommandsRunTest(t, &triggers)
	defer ts.Close()
	viper.Set("cache.enabled", false)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "run", "Update Agents", "--on", "ddd444ddd444ddd444ddd444", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(triggers))
	}
	if triggers[0].Command != "aaa111aaa111aaa111aaa111" {
		t.Errorf("trigger command = %q, want aaa111aaa111aaa111aaa111", triggers[0].Command)
	}
}

// --- Results Tests ---

func TestCommandsResultsJSON(t *testing.T) {
	ts := setupCommandsRunTest(t, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "results", "aaa111aaa111aaa111aaa111"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\nOutput: %s", err, buf.String())
	}
	if len(result) != 2 {
		t.Errorf("expected 2 results, got %d", len(result))
	}
}

func TestCommandsResultsTable(t *testing.T) {
	ts := setupCommandsRunTest(t, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "results", "aaa111aaa111aaa111aaa111", "--output", "table"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "alice-mbp.local") {
		t.Errorf("table should contain system name 'alice-mbp.local', got: %s", out)
	}
	if !strings.Contains(out, "SYSTEM") {
		t.Errorf("table header should contain 'SYSTEM', got: %s", out)
	}
}

func TestCommandsResultsFlattenedFields(t *testing.T) {
	ts := setupCommandsRunTest(t, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "results", "aaa111aaa111aaa111aaa111"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// First result should have stdout flattened from response.data.output.
	if result[0]["stdout"] != "0 packages upgraded" {
		t.Errorf("expected stdout '0 packages upgraded', got %v", result[0]["stdout"])
	}

	// Second result should have stderr flattened from response.error.
	if result[1]["stderr"] != "permission denied" {
		t.Errorf("expected stderr 'permission denied', got %v", result[1]["stderr"])
	}
}

func TestCommandsResultsFooter(t *testing.T) {
	ts := setupCommandsRunTest(t, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	stderr := new(bytes.Buffer)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"commands", "results", "aaa111aaa111aaa111aaa111"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	footer := stderr.String()
	if !strings.Contains(footer, "2 items") {
		t.Errorf("footer should contain '2 items', got: %s", footer)
	}
}

func TestCommandsResultsLimit(t *testing.T) {
	ts := setupCommandsRunTest(t, nil)
	defer ts.Close()

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "results", "aaa111aaa111aaa111aaa111", "--limit", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result []any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 result with --limit 1, got %d", len(result))
	}
}

func TestCommandsResultsByName(t *testing.T) {
	ts := setupCommandsRunTest(t, nil)
	defer ts.Close()
	viper.Set("cache.enabled", false)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "results", "Update Agents"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v\nOutput: %s", err, buf.String())
	}
	if len(result) != 2 {
		t.Errorf("expected 2 results, got %d", len(result))
	}
}

func TestCommandsResultsMissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "results"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

func TestCommandsResultsEmpty(t *testing.T) {
	setupUsersTest(t)
	viper.Set("cache.directory", t.TempDir())

	// Server with no results matching the command.
	ts := startCommandsRunServer(t, sampleCommands(), nil, nil, []map[string]any{}, nil)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "results", "aaa111aaa111aaa111aaa111"})
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

// --- Help Tests for Run/Results ---

func TestCommandsHelpIncludesRunAndResults(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "--help"})
	_ = cmd.Execute()

	help := buf.String()
	for _, sub := range []string{"run", "results"} {
		if !strings.Contains(help, sub) {
			t.Errorf("commands help should include %q subcommand, got: %s", sub, help)
		}
	}
}

func TestCommandsRunHelpIncludesFlags(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "run", "--help"})
	_ = cmd.Execute()

	help := buf.String()
	if !strings.Contains(help, "--on") {
		t.Errorf("run help should include --on flag, got: %s", help)
	}
}

func TestCommandsResultsHelpIncludesFlags(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "results", "--help"})
	_ = cmd.Execute()

	help := buf.String()
	for _, flag := range []string{"--limit", "--sort"} {
		if !strings.Contains(help, flag) {
			t.Errorf("results help should include %q flag, got: %s", flag, help)
		}
	}
}

// ========================================================================
// Trigger Tests
// ========================================================================

// startTriggerServer creates a mock server for /command/trigger/{name} endpoints.
func startTriggerServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var triggered []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.HasPrefix(r.URL.Path, "/command/trigger/") && r.Method == http.MethodPost {
			name := strings.TrimPrefix(r.URL.Path, "/command/trigger/")
			triggered = append(triggered, name)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"triggered": []string{"aaa111aaa111aaa111aaa111"},
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	return ts, &triggered
}

func TestCommandsTriggerSuccess(t *testing.T) {
	setupUsersTest(t)
	ts, triggered := startTriggerServer(t)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "trigger", "deploy-agents"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\nOutput: %s", err, buf.String())
	}

	if len(*triggered) != 1 || (*triggered)[0] != "deploy-agents" {
		t.Errorf("expected trigger 'deploy-agents', got %v", *triggered)
	}
}

func TestCommandsTriggerWithData(t *testing.T) {
	setupUsersTest(t)

	var receivedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/command/trigger/") && r.Method == http.MethodPost {
			json.NewDecoder(r.Body).Decode(&receivedBody)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"triggered": []string{"aaa111aaa111aaa111aaa111"}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "trigger", "run-backup", "--data", `{"env":"production"}`})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedBody["env"] != "production" {
		t.Errorf("expected env=production in body, got %v", receivedBody)
	}
}

func TestCommandsTriggerInvalidJSON(t *testing.T) {
	setupUsersTest(t)
	ts, _ := startTriggerServer(t)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "trigger", "deploy", "--data", "not-json"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid JSON data")
	}
	if !strings.Contains(err.Error(), "invalid --data JSON") {
		t.Errorf("expected 'invalid --data JSON' error, got: %v", err)
	}
}

func TestCommandsTriggerMissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "trigger"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument")
	}
}

func TestCommandsTriggerHelp(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"commands", "trigger", "--help"})
	_ = cmd.Execute()

	help := buf.String()
	if !strings.Contains(help, "--data") {
		t.Errorf("trigger help should include --data flag, got: %s", help)
	}
	if !strings.Contains(help, "trigger name") {
		t.Errorf("trigger help should mention 'trigger name', got: %s", help)
	}
}
