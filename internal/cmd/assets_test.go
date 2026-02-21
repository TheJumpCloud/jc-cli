package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startAssetsServer creates a mock JumpCloud V2 server that handles /assets endpoints.
func startAssetsServer(t *testing.T, assets []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /assets — list endpoint.
		if r.URL.Path == "/assets" && r.Method == http.MethodGet {
			// Check for filter query param for simple name filtering.
			filters := r.URL.Query()["filter"]
			if len(filters) > 0 {
				var filtered []map[string]any
				for _, asset := range assets {
					for _, f := range filters {
						// V2 filter format: name:eq:Value
						parts := strings.SplitN(f, ":", 3)
						if len(parts) == 3 && parts[0] == "name" && parts[1] == "eq" {
							if asset["name"] == parts[2] {
								filtered = append(filtered, asset)
							}
						}
					}
				}
				json.NewEncoder(w).Encode(filtered)
				return
			}
			json.NewEncoder(w).Encode(assets)
			return
		}

		// POST /assets — create endpoint.
		if r.URL.Path == "/assets" && r.Method == http.MethodPost {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			input["id"] = "new123new123new123new123"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(input)
			return
		}

		// Routes under /assets/{id}.
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			rest := strings.TrimPrefix(r.URL.Path, "/assets/")
			parts := strings.SplitN(rest, "/", 2)
			id := parts[0]

			// Find the asset by ID for GET/PUT/DELETE.
			var found map[string]any
			for _, asset := range assets {
				if asset["id"] == id {
					found = asset
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

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func sampleAssets() []map[string]any {
	return []map[string]any{
		{
			"id":           "aabbccddee112233aabb6001",
			"name":         "MacBook Pro 16",
			"serialNumber": "C02X1234ABCD",
			"assetTag":     "ASSET-001",
			"status":       "Assigned",
			"type":         "laptop",
			"systemId":     "aabbccddee112233aabb0001",
			"createdAt":    "2024-01-15T10:00:00Z",
			"updatedAt":    "2024-06-01T12:00:00Z",
		},
		{
			"id":           "aabbccddee112233aabb6002",
			"name":         "Dell Monitor 27",
			"serialNumber": "D27X5678EFGH",
			"assetTag":     "ASSET-002",
			"status":       "In Stock",
			"type":         "peripheral",
			"createdAt":    "2024-02-10T08:00:00Z",
			"updatedAt":    "2024-05-20T15:00:00Z",
		},
	}
}

// --- List Tests ---

func TestAssetsList_JSON(t *testing.T) {
	setupUsersTest(t)
	assets := sampleAssets()
	ts := startAssetsServer(t, assets)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "list"})

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
}

func TestAssetsList_Limit(t *testing.T) {
	setupUsersTest(t)
	assets := sampleAssets()
	ts := startAssetsServer(t, assets)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "list", "--limit", "1"})

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

func TestAssetsList_Filter(t *testing.T) {
	setupUsersTest(t)
	assets := sampleAssets()
	ts := startAssetsServer(t, assets)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "list", "--filter", "name=MacBook Pro 16"})

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
	if result[0]["name"] != "MacBook Pro 16" {
		t.Errorf("name = %q, want 'MacBook Pro 16'", result[0]["name"])
	}
}

// --- Get Tests ---

func TestAssetsGet(t *testing.T) {
	setupUsersTest(t)
	assets := sampleAssets()
	ts := startAssetsServer(t, assets)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "get", "aabbccddee112233aabb6001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "MacBook Pro 16" {
		t.Errorf("name = %q, want 'MacBook Pro 16'", result["name"])
	}
}

func TestAssetsGet_ByName(t *testing.T) {
	setupUsersTest(t)
	assets := sampleAssets()
	ts := startAssetsServer(t, assets)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "get", "MacBook Pro 16"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["id"] != "aabbccddee112233aabb6001" {
		t.Errorf("id = %q, want 'aabbccddee112233aabb6001'", result["id"])
	}
}

func TestAssetsGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	assets := sampleAssets()
	ts := startAssetsServer(t, assets)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "get", "NonExistentAsset"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found asset, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

// --- Create Tests ---

func TestAssetsCreate(t *testing.T) {
	setupUsersTest(t)
	assets := sampleAssets()
	ts := startAssetsServer(t, assets)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "create", "--name", "ThinkPad X1", "--serial-number", "PF1234AB", "--status", "In Stock"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "ThinkPad X1" {
		t.Errorf("name = %q, want 'ThinkPad X1'", result["name"])
	}
	if result["id"] != "new123new123new123new123" {
		t.Errorf("id = %q, want 'new123new123new123new123'", result["id"])
	}
}

func TestAssetsCreate_Plan(t *testing.T) {
	setupUsersTest(t)
	assets := sampleAssets()
	ts := startAssetsServer(t, assets)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "create", "--name", "ThinkPad X1", "--plan"})

	err := cmd.Execute()
	// Plan mode returns ExitError with plan exit code.
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

func TestAssetsCreate_MissingName(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "create"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --name, got nil")
	}
}

// --- Update Tests ---

func TestAssetsUpdate(t *testing.T) {
	setupUsersTest(t)
	assets := sampleAssets()
	ts := startAssetsServer(t, assets)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "update", "aabbccddee112233aabb6001", "--name", "MacBook Pro 14"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "MacBook Pro 14" {
		t.Errorf("name = %q, want 'MacBook Pro 14'", result["name"])
	}
}

func TestAssetsUpdate_Plan(t *testing.T) {
	setupUsersTest(t)
	assets := sampleAssets()
	ts := startAssetsServer(t, assets)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "update", "aabbccddee112233aabb6001", "--name", "MacBook Pro 14", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestAssetsUpdate_NoFields(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "update", "aabbccddee112233aabb6001"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no fields, got nil")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error should mention 'no fields to update', got: %v", err)
	}
}

// --- Delete Tests ---

func TestAssetsDelete(t *testing.T) {
	setupUsersTest(t)
	assets := sampleAssets()
	ts := startAssetsServer(t, assets)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "delete", "aabbccddee112233aabb6001", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("output should confirm deletion, got: %s", out)
	}
}

func TestAssetsDelete_Plan(t *testing.T) {
	setupUsersTest(t)
	assets := sampleAssets()
	ts := startAssetsServer(t, assets)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "delete", "aabbccddee112233aabb6001", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestAssetsDelete_Cancel(t *testing.T) {
	setupUsersTest(t)
	assets := sampleAssets()
	ts := startAssetsServer(t, assets)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	// Override confirmReader to simulate "n" answer.
	orig := confirmReader
	confirmReader = bufio.NewReader(strings.NewReader("n\n"))
	t.Cleanup(func() { confirmReader = orig })

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"assets", "delete", "aabbccddee112233aabb6001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "Cancelled.") {
		t.Errorf("stderr should contain 'Cancelled.', got: %q", stderr)
	}
}

// --- Help Test ---

func TestAssetsHelp(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"assets", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "hardware assets") {
		t.Errorf("help output should mention 'hardware assets', got: %s", out)
	}
}
