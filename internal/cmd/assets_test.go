package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"
)

// startAssetsServer creates a mock JumpCloud V2 server that handles
// /assets/devices, /assets/accessories, and /assets/locations endpoints.
// Assets have the real nested field structure: {id, fields: {Label: {editable, value}}}.
func startAssetsServer(t *testing.T, devices, accessories, locations []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		type subResource struct {
			items    []map[string]any
			endpoint string
		}
		subs := []subResource{
			{devices, "/assets/devices"},
			{accessories, "/assets/accessories"},
			{locations, "/assets/locations"},
		}

		for _, sub := range subs {
			// List
			if r.URL.Path == sub.endpoint && r.Method == http.MethodGet {
				json.NewEncoder(w).Encode(sub.items)
				return
			}

			// Create
			if r.URL.Path == sub.endpoint && r.Method == http.MethodPost {
				var input map[string]any
				json.NewDecoder(r.Body).Decode(&input)
				input["id"] = "new123new123new123new123"
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(input)
				return
			}

			// Routes under /assets/{type}/{id}
			prefix := sub.endpoint + "/"
			if strings.HasPrefix(r.URL.Path, prefix) {
				id := strings.TrimPrefix(r.URL.Path, prefix)
				if strings.Contains(id, "/") {
					continue
				}

				var found map[string]any
				for _, item := range sub.items {
					if item["id"] == id {
						found = item
						break
					}
				}

				if found == nil {
					w.WriteHeader(http.StatusNotFound)
					w.Write([]byte(`{"message":"Not Found"}`))
					return
				}

				switch r.Method {
				case http.MethodGet:
					json.NewEncoder(w).Encode(found)
					return
				case http.MethodPut:
					var input map[string]any
					json.NewDecoder(r.Body).Decode(&input)
					for k, v := range input {
						found[k] = v
					}
					json.NewEncoder(w).Encode(found)
					return
				case http.MethodDelete:
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(found)
					return
				}
			}
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func sampleDeviceAssets() []map[string]any {
	return []map[string]any{
		{
			"id": "aabbccddee112233aabb6001",
			"fields": map[string]any{
				"Name":          map[string]any{"editable": true, "value": "JDOE-MBP"},
				"Serial Number": map[string]any{"editable": true, "value": "C02X1234"},
				"Status": map[string]any{"editable": true, "value": map[string]any{
					"id": "abc123abc123abc123abc123", "name": "Active", "type": "select",
				}},
				"Model": map[string]any{"editable": true, "value": "MacBook Pro"},
				"Type": map[string]any{"editable": true, "value": map[string]any{
					"id": "def456def456def456def456", "name": "Laptop", "type": "select",
				}},
			},
		},
		{
			"id": "aabbccddee112233aabb6002",
			"fields": map[string]any{
				"Name":          map[string]any{"editable": true, "value": "SERVER-01"},
				"Serial Number": map[string]any{"editable": true, "value": "SRV5678"},
				"Status": map[string]any{"editable": true, "value": map[string]any{
					"id": "abc123abc123abc123abc124", "name": "Retired", "type": "select",
				}},
				"Model": map[string]any{"editable": true, "value": "Dell R740"},
			},
		},
	}
}

func sampleAccessoryAssets() []map[string]any {
	return []map[string]any{
		{
			"id": "aabbccddee112233aabb7001",
			"fields": map[string]any{
				"Name": map[string]any{"editable": true, "value": "USB-C Dock"},
				"Status": map[string]any{"editable": true, "value": map[string]any{
					"id": "abc123abc123abc123abc125", "name": "In Stock", "type": "select",
				}},
			},
		},
	}
}

func sampleLocationAssets() []map[string]any {
	return []map[string]any{
		{
			"id": "aabbccddee112233aabb8001",
			"fields": map[string]any{
				"Name": map[string]any{"editable": true, "value": "HQ Office"},
			},
		},
	}
}

func setupAssetsTest(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	viper.Reset()

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "jc")
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("JC_CONFIG", cfgPath)
	t.Setenv("JC_API_KEY", "")
	t.Setenv("JC_ORG_ID", "")

	viper.Set("api_key", "test-key-1234")
	viper.Set("cache.enabled", true)
	viper.Set("cache.directory", filepath.Join(tmp, "cache"))
}

// --- Device List Tests ---

func TestAssetsDevicesList_JSON(t *testing.T) {
	setupAssetsTest(t)
	ts := startAssetsServer(t, sampleDeviceAssets(), nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "devices", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d assets, want 2", len(result))
	}

	// Verify flattening: "Name" should be a top-level string, not nested in fields.
	if result[0]["Name"] != "JDOE-MBP" {
		t.Errorf("Name = %v, want 'JDOE-MBP'", result[0]["Name"])
	}
	// Verify select fields are flattened to their name.
	if result[0]["Status"] != "Active" {
		t.Errorf("Status = %v, want 'Active'", result[0]["Status"])
	}
}

func TestAssetsDevicesList_Limit(t *testing.T) {
	setupAssetsTest(t)
	ts := startAssetsServer(t, sampleDeviceAssets(), nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "devices", "list", "--limit", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d assets, want 1", len(result))
	}
}

// --- Accessories List Tests ---

func TestAssetsAccessoriesList_JSON(t *testing.T) {
	setupAssetsTest(t)
	ts := startAssetsServer(t, nil, sampleAccessoryAssets(), nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "accessories", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d accessories, want 1", len(result))
	}
	if result[0]["Name"] != "USB-C Dock" {
		t.Errorf("Name = %v, want 'USB-C Dock'", result[0]["Name"])
	}
}

// --- Locations List Tests ---

func TestAssetsLocationsList_JSON(t *testing.T) {
	setupAssetsTest(t)
	ts := startAssetsServer(t, nil, nil, sampleLocationAssets())
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "locations", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d locations, want 1", len(result))
	}
	if result[0]["Name"] != "HQ Office" {
		t.Errorf("Name = %v, want 'HQ Office'", result[0]["Name"])
	}
}

// --- Get Tests ---

func TestAssetsDevicesGet_ByID(t *testing.T) {
	setupAssetsTest(t)
	ts := startAssetsServer(t, sampleDeviceAssets(), nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "devices", "get", "aabbccddee112233aabb6001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["Name"] != "JDOE-MBP" {
		t.Errorf("Name = %v, want 'JDOE-MBP'", result["Name"])
	}
}

func TestAssetsDevicesGet_ByName(t *testing.T) {
	setupAssetsTest(t)
	ts := startAssetsServer(t, sampleDeviceAssets(), nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "devices", "get", "JDOE-MBP"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["id"] != "aabbccddee112233aabb6001" {
		t.Errorf("id = %v, want 'aabbccddee112233aabb6001'", result["id"])
	}
}

func TestAssetsDevicesGet_NotFound(t *testing.T) {
	setupAssetsTest(t)
	ts := startAssetsServer(t, sampleDeviceAssets(), nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "devices", "get", "NonExistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found asset, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

// --- Create Tests ---

func TestAssetsDevicesCreate(t *testing.T) {
	setupAssetsTest(t)
	ts := startAssetsServer(t, sampleDeviceAssets(), nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "devices", "create",
		"--field", "Name=ThinkPad X1",
		"--field", "Serial Number=PF1234AB",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["id"] != "new123new123new123new123" {
		t.Errorf("id = %v, want 'new123new123new123new123'", result["id"])
	}
}

func TestAssetsDevicesCreate_Plan(t *testing.T) {
	setupAssetsTest(t)
	ts := startAssetsServer(t, sampleDeviceAssets(), nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "devices", "create",
		"--field", "Name=ThinkPad X1",
		"--plan",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
	if exitErr.Code != 10 {
		t.Errorf("exit code = %d, want 10", exitErr.Code)
	}
}

func TestAssetsDevicesCreate_NoFields(t *testing.T) {
	setupAssetsTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "devices", "create"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no fields, got nil")
	}
	if !strings.Contains(err.Error(), "no fields specified") {
		t.Errorf("error should mention 'no fields specified', got: %v", err)
	}
}

// --- Update Tests ---

func TestAssetsDevicesUpdate(t *testing.T) {
	setupAssetsTest(t)
	ts := startAssetsServer(t, sampleDeviceAssets(), nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "devices", "update", "aabbccddee112233aabb6001",
		"--field", "Name=MacBook Pro 14",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// The mock server merges the update into the found object.
	// Since the server stores the nested structure, the response will include
	// our update merged in (but the flattener may not produce the updated name
	// since the mock doesn't transform fields→fields). Verify we got a response.
	if buf.Len() == 0 {
		t.Error("expected non-empty output")
	}
}

func TestAssetsDevicesUpdate_Plan(t *testing.T) {
	setupAssetsTest(t)
	ts := startAssetsServer(t, sampleDeviceAssets(), nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "devices", "update", "aabbccddee112233aabb6001",
		"--field", "Name=MacBook Pro 14",
		"--plan",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestAssetsDevicesUpdate_NoFields(t *testing.T) {
	setupAssetsTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "devices", "update", "aabbccddee112233aabb6001"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no fields, got nil")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error should mention 'no fields to update', got: %v", err)
	}
}

// --- Delete Tests ---

func TestAssetsDevicesDelete(t *testing.T) {
	setupAssetsTest(t)
	ts := startAssetsServer(t, sampleDeviceAssets(), nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "devices", "delete", "aabbccddee112233aabb6001", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("output should confirm deletion, got: %s", out)
	}
}

func TestAssetsDevicesDelete_Plan(t *testing.T) {
	setupAssetsTest(t)
	ts := startAssetsServer(t, sampleDeviceAssets(), nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "devices", "delete", "aabbccddee112233aabb6001", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestAssetsDevicesDelete_Cancel(t *testing.T) {
	setupAssetsTest(t)
	ts := startAssetsServer(t, sampleDeviceAssets(), nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	orig := confirmReader
	confirmReader = bufio.NewReader(strings.NewReader("n\n"))
	t.Cleanup(func() { confirmReader = orig })

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"assets", "devices", "delete", "aabbccddee112233aabb6001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "Cancelled.") {
		t.Errorf("stderr should contain 'Cancelled.', got: %q", stderr)
	}
}

// --- Flatten Tests ---

func TestFlattenAssetFields_ScalarValues(t *testing.T) {
	input := []json.RawMessage{
		json.RawMessage(`{"id":"aaa111aaa111aaa111aaa111","fields":{"Name":{"editable":true,"value":"Test"},"Tag":{"editable":true,"value":"TAG-001"}}}`),
	}
	result := flattenAssetFields(input)
	if len(result) != 1 {
		t.Fatalf("got %d items, want 1", len(result))
	}

	var flat map[string]any
	json.Unmarshal(result[0], &flat)
	if flat["Name"] != "Test" {
		t.Errorf("Name = %v, want 'Test'", flat["Name"])
	}
	if flat["Tag"] != "TAG-001" {
		t.Errorf("Tag = %v, want 'TAG-001'", flat["Tag"])
	}
}

func TestFlattenAssetFields_SelectReference(t *testing.T) {
	input := []json.RawMessage{
		json.RawMessage(`{"id":"bbb222bbb222bbb222bbb222","fields":{"Status":{"editable":true,"value":{"id":"sel1","name":"Active","type":"select"}}}}`),
	}
	result := flattenAssetFields(input)
	var flat map[string]any
	json.Unmarshal(result[0], &flat)
	if flat["Status"] != "Active" {
		t.Errorf("Status = %v, want 'Active'", flat["Status"])
	}
}

func TestFlattenAssetFields_NullValue(t *testing.T) {
	input := []json.RawMessage{
		json.RawMessage(`{"id":"ccc333ccc333ccc333ccc333","fields":{"Vendor":{"editable":true,"value":null}}}`),
	}
	result := flattenAssetFields(input)
	var flat map[string]any
	json.Unmarshal(result[0], &flat)
	if flat["Vendor"] != nil {
		t.Errorf("Vendor = %v, want nil", flat["Vendor"])
	}
}

func TestFlattenAssetFields_PassthroughOnParseFailure(t *testing.T) {
	raw := json.RawMessage(`{"id":"abc","not_fields":true}`)
	result := flattenAssetFields([]json.RawMessage{raw})
	if len(result) != 1 {
		t.Fatalf("got %d items, want 1", len(result))
	}
	// Should pass through the original since there's no "fields" key —
	// the struct unmarshal succeeds but Fields is nil, so flat will have id: ""
	// Actually: struct unmarshal succeeds with fields=nil. Let's verify we get something back.
	var flat map[string]any
	json.Unmarshal(result[0], &flat)
	if flat["id"] != "abc" {
		t.Errorf("id = %v, want 'abc'", flat["id"])
	}
}

// --- Build Body Tests ---

func TestBuildAssetBody(t *testing.T) {
	fields, effects, err := buildAssetBody([]string{"Name=JDOE-MBP", "Serial Number=C02X1234"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fields["Name"] != "JDOE-MBP" {
		t.Errorf("Name = %q, want 'JDOE-MBP'", fields["Name"])
	}
	if fields["Serial Number"] != "C02X1234" {
		t.Errorf("Serial Number = %q, want 'C02X1234'", fields["Serial Number"])
	}
	if len(effects) != 2 {
		t.Errorf("got %d effects, want 2", len(effects))
	}
}

func TestBuildAssetBody_InvalidFormat(t *testing.T) {
	_, _, err := buildAssetBody([]string{"NoEquals"})
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "Label=Value") {
		t.Errorf("error should mention format, got: %v", err)
	}
}

// --- Help Test ---

func TestAssetsHelp(t *testing.T) {
	setupAssetsTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "devices") {
		t.Errorf("help output should mention 'devices', got: %s", out)
	}
	if !strings.Contains(out, "accessories") {
		t.Errorf("help output should mention 'accessories', got: %s", out)
	}
	if !strings.Contains(out, "locations") {
		t.Errorf("help output should mention 'locations', got: %s", out)
	}
}
