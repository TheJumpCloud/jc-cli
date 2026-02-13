package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// startDevicesServer creates a mock JumpCloud server that handles /systems endpoints.
func startDevicesServer(t *testing.T, devices []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /systems — list endpoint.
		if r.URL.Path == "/systems" && r.Method == http.MethodGet {
			skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if limit == 0 {
				limit = 100
			}

			end := skip + limit
			if end > len(devices) {
				end = len(devices)
			}
			var page []map[string]any
			if skip < len(devices) {
				page = devices[skip:end]
			}
			if page == nil {
				page = []map[string]any{}
			}

			resp := map[string]any{
				"results":    page,
				"totalCount": len(devices),
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// GET /systems/{id} — get endpoint.
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

		w.WriteHeader(http.StatusNotFound)
	}))
}

// sampleDevices returns a set of test device records.
func sampleDevices() []map[string]any {
	return []map[string]any{
		{
			"_id":          "dev-aaa111",
			"displayName":  "ALICE-MBP",
			"hostname":     "alice-mbp.local",
			"os":           "Mac OS X",
			"osVersion":    "15.3",
			"lastContact":  "2026-02-13T10:00:00Z",
			"agentVersion": "3.1.0",
		},
		{
			"_id":          "dev-bbb222",
			"displayName":  "BOB-LINUX",
			"hostname":     "bob-linux.local",
			"os":           "Ubuntu",
			"osVersion":    "24.04",
			"lastContact":  "2026-02-12T08:30:00Z",
			"agentVersion": "3.0.5",
		},
		{
			"_id":          "dev-ccc333",
			"displayName":  "CHARLIE-WIN",
			"hostname":     "charlie-win.local",
			"os":           "Windows",
			"osVersion":    "11",
			"lastContact":  "2026-02-10T15:45:00Z",
			"agentVersion": "2.9.8",
		},
	}
}

// --- Devices List Tests ---

func TestDevicesList_JSON(t *testing.T) {
	setupUsersTest(t) // reuses shared setup (keyring, viper, config)
	devices := sampleDevices()
	ts := startDevicesServer(t, devices)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, out.String())
	}
	if len(result) != 3 {
		t.Errorf("got %d devices, want 3", len(result))
	}
}

func TestDevicesList_Table(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "list", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "DISPLAYNAME") {
		t.Errorf("table output missing DISPLAYNAME header:\n%s", output)
	}
	if !strings.Contains(output, "HOSTNAME") {
		t.Errorf("table output missing HOSTNAME header:\n%s", output)
	}
	if !strings.Contains(output, "ALICE-MBP") {
		t.Errorf("table output missing device 'ALICE-MBP':\n%s", output)
	}
}

func TestDevicesList_TableShorthand(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "list", "-t"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(out.String(), "DISPLAYNAME") {
		t.Errorf("-t flag did not produce table output:\n%s", out.String())
	}
}

func TestDevicesList_Limit(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "list", "--limit", "2"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("got %d devices, want 2 (limit)", len(result))
	}
}

func TestDevicesList_Sort(t *testing.T) {
	setupUsersTest(t)

	var capturedSort string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSort = r.URL.Query().Get("sort")
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"results":    []map[string]any{},
			"totalCount": 0,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "list", "--sort", "hostname"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedSort != "hostname" {
		t.Errorf("sort param = %q, want %q", capturedSort, "hostname")
	}
}

func TestDevicesList_SortDescending(t *testing.T) {
	setupUsersTest(t)

	var capturedSort string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSort = r.URL.Query().Get("sort")
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"results":    []map[string]any{},
			"totalCount": 0,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "list", "--sort", "-lastContact"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedSort != "-lastContact" {
		t.Errorf("sort param = %q, want %q", capturedSort, "-lastContact")
	}
}

func TestDevicesList_EmptyResult(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, []map[string]any{})
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if strings.TrimSpace(out.String()) != "[]" {
		t.Errorf("expected empty JSON array, got: %q", out.String())
	}
}

func TestDevicesList_IDs(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "list", "--ids"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Errorf("got %d IDs, want 3: %v", len(lines), lines)
	}
	if lines[0] != "dev-aaa111" {
		t.Errorf("first ID = %q, want %q", lines[0], "dev-aaa111")
	}
}

func TestDevicesList_Quiet(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "list", "--quiet"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if out.String() != "" {
		t.Errorf("expected empty output with --quiet, got: %q", out.String())
	}
}

func TestDevicesList_CSV(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "list", "--output", "csv"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 { // header + 3 rows
		t.Errorf("got %d CSV lines, want 4 (header + 3 rows)", len(lines))
	}
	if !strings.Contains(lines[0], "displayName") {
		t.Errorf("CSV header missing 'displayName': %s", lines[0])
	}
}

func TestDevicesList_Footer(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"devices", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	footer := errOut.String()
	if !strings.Contains(footer, "3 items") {
		t.Errorf("footer missing count info: %q", footer)
	}
}

func TestDevicesList_FooterWithLimit(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"devices", "list", "--limit", "2"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	footer := errOut.String()
	if !strings.Contains(footer, "2 of 3 items") {
		t.Errorf("footer should show '2 of 3 items', got: %q", footer)
	}
}

func TestDevicesList_DefaultFields(t *testing.T) {
	setupUsersTest(t)

	devices := []map[string]any{
		{
			"_id":            "dev-aaa111",
			"displayName":    "ALICE-MBP",
			"hostname":       "alice-mbp.local",
			"os":             "Mac OS X",
			"osVersion":      "15.3",
			"lastContact":    "2026-02-13T10:00:00Z",
			"agentVersion":   "3.1.0",
			"serialNumber":   "C02XG2JFH7JY",
			"systemTimezone": "America/New_York",
		},
	}
	ts := startDevicesServer(t, devices)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "list", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	tableOut := out.String()
	if !strings.Contains(tableOut, "DISPLAYNAME") {
		t.Errorf("table missing DISPLAYNAME column")
	}
	// serialNumber and systemTimezone are not in default fields.
	if strings.Contains(tableOut, "SERIALNUMBER") {
		t.Errorf("table should not show SERIALNUMBER in default fields")
	}
	if strings.Contains(tableOut, "SYSTEMTIMEZONE") {
		t.Errorf("table should not show SYSTEMTIMEZONE in default fields")
	}
}

func TestDevicesList_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"results":    []map[string]any{},
			"totalCount": 0,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedPath != "/systems" {
		t.Errorf("API path = %q, want %q", capturedPath, "/systems")
	}
}

func TestDevicesList_AuthError(t *testing.T) {
	setupUsersTest(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "list"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}
}

func TestDevicesList_Pagination(t *testing.T) {
	setupUsersTest(t)

	devices := make([]map[string]any, 15)
	for i := range devices {
		devices[i] = map[string]any{
			"_id":          fmt.Sprintf("dev-%02d", i),
			"displayName":  fmt.Sprintf("DEVICE-%02d", i),
			"hostname":     fmt.Sprintf("device-%02d.local", i),
			"os":           "Mac OS X",
			"osVersion":    "15.3",
			"lastContact":  "2026-02-13T10:00:00Z",
			"agentVersion": "3.1.0",
		}
	}

	ts := startDevicesServer(t, devices)
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if len(result) != 15 {
		t.Errorf("got %d devices, want 15", len(result))
	}
}

// --- Devices Get Tests ---

func TestDevicesGet_Success(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "get", "dev-aaa111"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var device map[string]any
	if err := json.Unmarshal(out.Bytes(), &device); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, out.String())
	}
	if device["hostname"] != "alice-mbp.local" {
		t.Errorf("hostname = %v, want alice-mbp.local", device["hostname"])
	}
	if device["displayName"] != "ALICE-MBP" {
		t.Errorf("displayName = %v, want ALICE-MBP", device["displayName"])
	}
}

func TestDevicesGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "get", "nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent device, got nil")
	}

	if !strings.Contains(err.Error(), "404") && !strings.Contains(err.Error(), "Not Found") {
		t.Errorf("error should mention 404 or Not Found, got: %v", err)
	}
}

func TestDevicesGet_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "get"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing argument, got nil")
	}
}

func TestDevicesGet_TableOutput(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "get", "dev-bbb222", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(out.String(), "bob-linux.local") {
		t.Errorf("table output should contain 'bob-linux.local':\n%s", out.String())
	}
}

func TestDevicesGet_HumanOutput(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "get", "dev-aaa111", "--output", "human"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(out.String(), "hostname:") {
		t.Errorf("human output missing 'hostname:' label:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "alice-mbp.local") {
		t.Errorf("human output missing value 'alice-mbp.local':\n%s", out.String())
	}
}

func TestDevicesGet_IDs(t *testing.T) {
	setupUsersTest(t)
	ts := startDevicesServer(t, sampleDevices())
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "get", "dev-aaa111", "--ids"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if strings.TrimSpace(out.String()) != "dev-aaa111" {
		t.Errorf("--ids output = %q, want %q", strings.TrimSpace(out.String()), "dev-aaa111")
	}
}

func TestDevicesGet_APIEndpoint(t *testing.T) {
	setupUsersTest(t)

	var capturedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"_id":"dev-abc123","hostname":"test.local","displayName":"TEST"}`))
	}))
	defer ts.Close()
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "get", "dev-abc123"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if capturedPath != "/systems/dev-abc123" {
		t.Errorf("API path = %q, want %q", capturedPath, "/systems/dev-abc123")
	}
}

// --- Help Output Tests ---

func TestDevicesCmd_Help(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	help := out.String()
	if !strings.Contains(help, "list") {
		t.Errorf("devices help should mention 'list' subcommand:\n%s", help)
	}
	if !strings.Contains(help, "get") {
		t.Errorf("devices help should mention 'get' subcommand:\n%s", help)
	}
}

func TestDevicesListCmd_Help(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"devices", "list", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	help := out.String()
	if !strings.Contains(help, "--limit") {
		t.Errorf("list help should mention --limit flag:\n%s", help)
	}
	if !strings.Contains(help, "--sort") {
		t.Errorf("list help should mention --sort flag:\n%s", help)
	}
}

func TestRootHelp_IncludesDevices(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(out.String(), "devices") {
		t.Errorf("root help should list 'devices' command:\n%s", out.String())
	}
}
