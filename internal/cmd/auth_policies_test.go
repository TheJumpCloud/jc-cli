package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startAuthPoliciesServer creates a mock JumpCloud V2 server that handles /authn/policies endpoints.
func startAuthPoliciesServer(t *testing.T, policies []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /authn/policies — list endpoint.
		if r.URL.Path == "/authn/policies" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(policies)
			return
		}

		// POST /authn/policies — create endpoint.
		if r.URL.Path == "/authn/policies" && r.Method == http.MethodPost {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			input["id"] = "new123new123new123new123"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(input)
			return
		}

		// Routes under /authn/policies/{id}.
		if strings.HasPrefix(r.URL.Path, "/authn/policies/") {
			id := strings.TrimPrefix(r.URL.Path, "/authn/policies/")

			var found map[string]any
			for _, p := range policies {
				if p["id"] == id {
					found = p
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

func sampleAuthPolicies() []map[string]any {
	return []map[string]any{
		{
			"id":       "aabbccddee112233aabb3001",
			"name":     "MFA Required",
			"disabled": false,
			"type":     "user_portal",
			"conditions": map[string]any{
				"ipAddressIn": "aabbccddee112233aabb2001",
			},
			"effect": "allow_with_mfa",
		},
		{
			"id":       "aabbccddee112233aabb3002",
			"name":     "Block External",
			"disabled": false,
			"type":     "user_portal",
			"conditions": map[string]any{
				"ipAddressNotIn": "aabbccddee112233aabb2001",
			},
			"effect": "deny",
		},
		{
			"id":       "aabbccddee112233aabb3003",
			"name":     "Disabled Policy",
			"disabled": true,
			"type":     "admin",
			"conditions": map[string]any{},
			"effect":     "allow",
		},
	}
}

// --- List Tests ---

func TestAuthPoliciesList_JSON(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 3 {
		t.Errorf("got %d policies, want 3", len(result))
	}
}

func TestAuthPoliciesList_Table(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "list", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "MFA Required") {
		t.Errorf("table output should contain 'MFA Required', got:\n%s", out)
	}
}

func TestAuthPoliciesList_Footer(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf, errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"auth-policies", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	footer := errBuf.String()
	if !strings.Contains(footer, "3 items") {
		t.Errorf("footer should contain '3 items', got: %q", footer)
	}
}

func TestAuthPoliciesList_Empty(t *testing.T) {
	setupUsersTest(t)
	ts := startAuthPoliciesServer(t, []map[string]any{})
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 0 {
		t.Errorf("got %d policies, want 0", len(result))
	}
}

func TestAuthPoliciesList_IDs(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "list", "--ids"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Errorf("got %d ID lines, want 3", len(lines))
	}
}

// --- Get Tests ---

func TestAuthPoliciesGet_ByID(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "get", "aabbccddee112233aabb3001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "MFA Required" {
		t.Errorf("name = %q, want 'MFA Required'", result["name"])
	}
}

func TestAuthPoliciesGet_ByName(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "get", "MFA Required"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["id"] != "aabbccddee112233aabb3001" {
		t.Errorf("id = %q, want 'aabbccddee112233aabb3001'", result["id"])
	}
}

func TestAuthPoliciesGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "get", "NonExistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found policy, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestAuthPoliciesGet_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "get"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing arg, got nil")
	}
}

// --- Create Tests ---

func TestAuthPoliciesCreate(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "create", "--name", "New Policy", "--type", "user_portal"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "New Policy" {
		t.Errorf("name = %q, want 'New Policy'", result["name"])
	}
	if result["id"] != "new123new123new123new123" {
		t.Errorf("id = %q, want 'new123new123new123new123'", result["id"])
	}
}

func TestAuthPoliciesCreate_WithConditions(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	condJSON := `{"ipAddressIn":"aabbccddee112233aabb2001"}`

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "create", "--name", "Cond Policy", "--conditions", condJSON})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["conditions"] == nil {
		t.Error("conditions should be present in result")
	}
}

func TestAuthPoliciesCreate_InvalidConditions(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "create", "--name", "Bad", "--conditions", "{invalid"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "invalid --conditions JSON") {
		t.Errorf("error should mention invalid JSON, got: %v", err)
	}
}

func TestAuthPoliciesCreate_Plan(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "create", "--name", "Test", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestAuthPoliciesCreate_MissingRequired(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "create"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --name, got nil")
	}
}

// --- Update Tests ---

func TestAuthPoliciesUpdate(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "update", "aabbccddee112233aabb3001", "--name", "Updated Policy"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "Updated Policy" {
		t.Errorf("name = %q, want 'Updated Policy'", result["name"])
	}
}

func TestAuthPoliciesUpdate_DisabledEnabled_MutuallyExclusive(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "update", "aabbccddee112233aabb3001", "--disabled", "--enabled"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for mutually exclusive flags, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should mention 'mutually exclusive', got: %v", err)
	}
}

func TestAuthPoliciesUpdate_NoFields(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "update", "aabbccddee112233aabb3001"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no fields, got nil")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error should mention 'no fields to update', got: %v", err)
	}
}

func TestAuthPoliciesUpdate_Plan(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "update", "aabbccddee112233aabb3001", "--name", "X", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

// --- Delete Tests ---

func TestAuthPoliciesDelete_Force(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "delete", "aabbccddee112233aabb3001", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("output should confirm deletion, got: %s", out)
	}
}

func TestAuthPoliciesDelete_Plan(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "delete", "aabbccddee112233aabb3001", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestAuthPoliciesDelete_NotFound(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "delete", "NonExistent", "--force"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found policy, got nil")
	}
}

// --- Enable/Disable Tests ---

func TestAuthPoliciesEnable(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "enable", "aabbccddee112233aabb3003"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["disabled"] != false {
		t.Errorf("disabled = %v, want false", result["disabled"])
	}
}

func TestAuthPoliciesDisable(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "disable", "aabbccddee112233aabb3001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["disabled"] != true {
		t.Errorf("disabled = %v, want true", result["disabled"])
	}
}

func TestAuthPoliciesEnable_Plan(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "enable", "aabbccddee112233aabb3003", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestAuthPoliciesEnable_ByName(t *testing.T) {
	setupUsersTest(t)
	policies := sampleAuthPolicies()
	ts := startAuthPoliciesServer(t, policies)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "enable", "Disabled Policy"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["disabled"] != false {
		t.Errorf("disabled = %v, want false", result["disabled"])
	}
}

// --- Help Tests ---

// --- Simulate Tests ---

// startSimulateServer creates a combined V1+V2 mock server that handles:
//   - V1: /systemusers (user resolution)
//   - V1: /systems/{id} (device status)
//   - V2: /authn/policies, /authn/policies/{id} (auth policies)
//   - V2: /users/{id}/memberof (user group memberships)
//   - V2: /iplists/{id} (IP list entries)
func startSimulateServer(t *testing.T, policies []map[string]any, users []map[string]any, memberships map[string][]map[string]any, ipLists map[string]map[string]any, devices map[string]map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// V1: /systemusers — list for user name resolution.
		if r.URL.Path == "/systemusers" && r.Method == http.MethodGet {
			resp := map[string]any{
				"results":    users,
				"totalCount": len(users),
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// V1: /systems/{id} — get device by ID.
		if strings.HasPrefix(r.URL.Path, "/systems/") && r.Method == http.MethodGet {
			id := strings.TrimPrefix(r.URL.Path, "/systems/")
			if device, ok := devices[id]; ok {
				json.NewEncoder(w).Encode(device)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
			return
		}

		// V2: /authn/policies — list.
		if r.URL.Path == "/authn/policies" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(policies)
			return
		}

		// V2: /authn/policies/{id} — get.
		if strings.HasPrefix(r.URL.Path, "/authn/policies/") && r.Method == http.MethodGet {
			id := strings.TrimPrefix(r.URL.Path, "/authn/policies/")
			for _, p := range policies {
				if p["id"] == id {
					json.NewEncoder(w).Encode(p)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
			return
		}

		// V2: /users/{id}/memberof — user group memberships.
		if strings.HasSuffix(r.URL.Path, "/memberof") && r.Method == http.MethodGet {
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 3 {
				userID := parts[len(parts)-2]
				if groups, ok := memberships[userID]; ok {
					json.NewEncoder(w).Encode(groups)
					return
				}
			}
			json.NewEncoder(w).Encode([]any{})
			return
		}

		// V2: /iplists/{id} — IP list details.
		if strings.HasPrefix(r.URL.Path, "/iplists/") && r.Method == http.MethodGet {
			id := strings.TrimPrefix(r.URL.Path, "/iplists/")
			if ipList, ok := ipLists[id]; ok {
				json.NewEncoder(w).Encode(ipList)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
			return
		}

		// V2: /usergroups/{id}/members — group member listing.
		if strings.Contains(r.URL.Path, "/usergroups/") && strings.HasSuffix(r.URL.Path, "/members") && r.Method == http.MethodGet {
			parts := strings.Split(r.URL.Path, "/")
			// /usergroups/{id}/members → parts: ["", "usergroups", id, "members"]
			if len(parts) >= 4 {
				groupID := parts[2]
				if members, ok := memberships["group:"+groupID]; ok {
					json.NewEncoder(w).Encode(members)
					return
				}
			}
			json.NewEncoder(w).Encode([]any{})
			return
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func simulateTestUsers() []map[string]any {
	return []map[string]any{
		{"_id": "aaa111aaa111aaa111aaa111", "username": "jdoe", "email": "jdoe@example.com"},
		{"_id": "bbb222bbb222bbb222bbb222", "username": "admin", "email": "admin@example.com"},
	}
}

// apiEffect builds the nested effect structure matching the real JumpCloud API format.
func apiEffect(action string, mfaRequired bool) map[string]any {
	return map[string]any{
		"action": action,
		"obligations": map[string]any{
			"mfa": map[string]any{"required": mfaRequired},
		},
	}
}

// apiTargets builds the nested targets structure matching the real JumpCloud API format.
func apiTargets(allUsers bool, groupIDs []string) map[string]any {
	inclusions := []string{}
	if allUsers {
		inclusions = []string{"all"}
	}
	if groupIDs == nil {
		groupIDs = []string{}
	}
	return map[string]any{
		"users":      map[string]any{"inclusions": inclusions},
		"userGroups": map[string]any{"inclusions": groupIDs, "exclusions": []string{}},
		"resources":  []any{},
	}
}

func simulateTestPolicies() []map[string]any {
	return []map[string]any{
		{
			"id":       "aabbccddee112233aabb3001",
			"name":     "MFA Required",
			"disabled": false,
			"type":     "user_portal",
			"conditions": map[string]any{
				"ipAddressIn": "aabbccddee112233aabb2001",
			},
			"effect":  apiEffect("allow", true),
			"targets": apiTargets(true, nil),
		},
		{
			"id":       "aabbccddee112233aabb3002",
			"name":     "Block External",
			"disabled": false,
			"type":     "user_portal",
			"conditions": map[string]any{
				"ipAddressNotIn": "aabbccddee112233aabb2001",
			},
			"effect":  apiEffect("deny", false),
			"targets": apiTargets(false, []string{"ccddee112233aabbccdd0001"}),
		},
		{
			"id":       "aabbccddee112233aabb3003",
			"name":     "Allow All",
			"disabled": false,
			"type":     "user_portal",
			"conditions": map[string]any{},
			"effect":    apiEffect("allow", false),
			"targets":   apiTargets(true, nil),
		},
	}
}

func TestSimulate_UserMatchesIPCondition(t *testing.T) {
	setupUsersTest(t)

	ipLists := map[string]map[string]any{
		"aabbccddee112233aabb2001": {"id": "aabbccddee112233aabb2001", "name": "Office IPs", "ips": []string{"10.0.0.0/24"}},
	}

	ts := startSimulateServer(t, simulateTestPolicies(), simulateTestUsers(),
		map[string][]map[string]any{}, ipLists, nil)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "simulate", "aabbccddee112233aabb3001", "--user", "jdoe", "--ip", "10.0.0.5"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["match"] != "applies" {
		t.Errorf("match = %v, want 'applies'", result["match"])
	}
	if result["effect"] != "allow_with_mfa" {
		t.Errorf("effect = %v, want 'allow_with_mfa'", result["effect"])
	}
}

func TestSimulate_IPNotInRange(t *testing.T) {
	setupUsersTest(t)

	ipLists := map[string]map[string]any{
		"aabbccddee112233aabb2001": {"id": "aabbccddee112233aabb2001", "name": "Office IPs", "ips": []string{"10.0.0.0/24"}},
	}

	ts := startSimulateServer(t, simulateTestPolicies(), simulateTestUsers(),
		map[string][]map[string]any{}, ipLists, nil)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "simulate", "aabbccddee112233aabb3001", "--user", "jdoe", "--ip", "203.0.113.5"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["match"] != "does_not_apply" {
		t.Errorf("match = %v, want 'does_not_apply'", result["match"])
	}
}

func TestSimulate_NoIPProvided(t *testing.T) {
	setupUsersTest(t)

	ts := startSimulateServer(t, simulateTestPolicies(), simulateTestUsers(),
		map[string][]map[string]any{}, nil, nil)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "simulate", "aabbccddee112233aabb3001", "--user", "jdoe"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["match"] != "unknown" {
		t.Errorf("match = %v, want 'unknown' (no IP context)", result["match"])
	}
}

func TestSimulate_GroupTargetedPolicy(t *testing.T) {
	setupUsersTest(t)

	memberships := map[string][]map[string]any{
		"aaa111aaa111aaa111aaa111": {
			{"id": "ccddee112233aabbccdd0001", "type": "user_group"},
		},
	}

	ipLists := map[string]map[string]any{
		"aabbccddee112233aabb2001": {"id": "aabbccddee112233aabb2001", "name": "Office IPs", "ips": []string{"10.0.0.0/24"}},
	}

	ts := startSimulateServer(t, simulateTestPolicies(), simulateTestUsers(),
		memberships, ipLists, nil)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	// Simulate "Block External" which targets user group ccddee112233aabbccdd0001.
	// User jdoe is in that group, IP 203.0.113.5 is NOT in office IPs → deny.
	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "simulate", "Block External", "--user", "jdoe", "--ip", "203.0.113.5"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["match"] != "applies" {
		t.Errorf("match = %v, want 'applies'", result["match"])
	}
	if result["effect"] != "deny" {
		t.Errorf("effect = %v, want 'deny'", result["effect"])
	}
}

func TestSimulate_UserNotInTargetGroup(t *testing.T) {
	setupUsersTest(t)

	// admin is NOT in the target group for "Block External".
	memberships := map[string][]map[string]any{
		"bbb222bbb222bbb222bbb222": {},
	}

	ts := startSimulateServer(t, simulateTestPolicies(), simulateTestUsers(),
		memberships, nil, nil)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "simulate", "Block External", "--user", "admin", "--ip", "203.0.113.5"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["match"] != "does_not_apply" {
		t.Errorf("match = %v, want 'does_not_apply' (user not in target group)", result["match"])
	}
}

func TestSimulate_NoConditionsPolicy(t *testing.T) {
	setupUsersTest(t)

	ts := startSimulateServer(t, simulateTestPolicies(), simulateTestUsers(),
		map[string][]map[string]any{}, nil, nil)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	// "Allow All" has empty conditions and targets all users → always applies.
	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "simulate", "Allow All", "--user", "jdoe"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["match"] != "applies" {
		t.Errorf("match = %v, want 'applies'", result["match"])
	}
	if result["effect"] != "allow" {
		t.Errorf("effect = %v, want 'allow'", result["effect"])
	}
}

func TestSimulate_MissingUserFlag(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "simulate", "some-policy"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --user flag")
	}
}

func TestSimulate_WithDevice(t *testing.T) {
	setupUsersTest(t)

	devices := map[string]map[string]any{
		"ddd111ddd111ddd111ddd111": {
			"_id":          "ddd111ddd111ddd111ddd111",
			"displayName":  "JDOE-MBP",
			"agentVersion": "3.0.0",
			"fde":          map[string]any{"active": true},
		},
	}

	policies := []map[string]any{
		{
			"id":       "aabbccddee112233aabb3004",
			"name":     "Device Check",
			"disabled": false,
			"type":     "user_portal",
			"conditions": map[string]any{
				"deviceManaged": true,
			},
			"effect":  apiEffect("allow", false),
			"targets": apiTargets(true, nil),
		},
	}

	ts := startSimulateServer(t, policies, simulateTestUsers(),
		map[string][]map[string]any{}, nil, devices)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "simulate", "aabbccddee112233aabb3004", "--user", "jdoe", "--device", "ddd111ddd111ddd111ddd111"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["match"] != "applies" {
		t.Errorf("match = %v, want 'applies'", result["match"])
	}
	if result["effect"] != "allow" {
		t.Errorf("effect = %v, want 'allow'", result["effect"])
	}
}

// --- Blast Radius Tests ---

func TestBlastRadius_GroupTargeted(t *testing.T) {
	setupUsersTest(t)

	memberships := map[string][]map[string]any{
		// Members of the target group.
		"group:ccddee112233aabbccdd0001": {
			{"id": "aaa111aaa111aaa111aaa111", "type": "user"},
			{"id": "bbb222bbb222bbb222bbb222", "type": "user"},
		},
	}

	ts := startSimulateServer(t, simulateTestPolicies(), simulateTestUsers(),
		memberships, nil, nil)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	var errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"auth-policies", "blast-radius", "Block External"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d members, want 2", len(result))
	}

	if !strings.Contains(errBuf.String(), "2 affected users") {
		t.Errorf("stderr should mention affected users count, got: %s", errBuf.String())
	}
}

func TestBlastRadius_AllUsersTargeted(t *testing.T) {
	setupUsersTest(t)

	ts := startSimulateServer(t, simulateTestPolicies(), simulateTestUsers(),
		map[string][]map[string]any{}, nil, nil)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	var errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	// "Allow All" targets allUsers: true.
	cmd.SetArgs([]string{"auth-policies", "blast-radius", "Allow All"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(errBuf.String(), "targets ALL users") {
		t.Errorf("stderr should mention all users targeted, got: %s", errBuf.String())
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d users, want 2", len(result))
	}
}

func TestBlastRadius_NoTargetGroups(t *testing.T) {
	setupUsersTest(t)

	// Policy with no target groups and allUsers=false.
	policies := []map[string]any{
		{
			"id":         "aabbccddee112233aabb3005",
			"name":       "No Targets",
			"disabled":   false,
			"type":       "user_portal",
			"conditions": map[string]any{},
			"effect":     apiEffect("allow", false),
			"targets":    apiTargets(false, nil),
		},
	}

	ts := startSimulateServer(t, policies, simulateTestUsers(),
		map[string][]map[string]any{}, nil, nil)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	var errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"auth-policies", "blast-radius", "aabbccddee112233aabb3005"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if !strings.Contains(errBuf.String(), "no target user groups") {
		t.Errorf("stderr should mention no target groups, got: %s", errBuf.String())
	}

	// Stdout should be empty (no data output).
	if buf.Len() > 0 {
		t.Errorf("stdout should be empty, got: %s", buf.String())
	}
}

func TestBlastRadius_WithLimit(t *testing.T) {
	setupUsersTest(t)

	memberships := map[string][]map[string]any{
		"group:ccddee112233aabbccdd0001": {
			{"id": "aaa111aaa111aaa111aaa111", "type": "user"},
			{"id": "bbb222bbb222bbb222bbb222", "type": "user"},
			{"id": "ccc333ccc333ccc333ccc333", "type": "user"},
		},
	}

	ts := startSimulateServer(t, simulateTestPolicies(), simulateTestUsers(),
		memberships, nil, nil)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	var errBuf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"auth-policies", "blast-radius", "Block External", "--limit", "2"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d members, want 2 (limited)", len(result))
	}

	if !strings.Contains(errBuf.String(), "limited") {
		t.Errorf("stderr should mention limit, got: %s", errBuf.String())
	}
}

func TestBlastRadius_DeduplicatesAcrossGroups(t *testing.T) {
	setupUsersTest(t)

	// Policy targeting two groups with overlapping members.
	policies := []map[string]any{
		{
			"id":         "aabbccddee112233aabb3006",
			"name":       "Multi Group",
			"disabled":   false,
			"type":       "user_portal",
			"conditions": map[string]any{},
			"effect":     apiEffect("allow", false),
			"targets":    apiTargets(false, []string{"ccddee112233aabbccdd0001", "ccddee112233aabbccdd0002"}),
		},
	}

	memberships := map[string][]map[string]any{
		"group:ccddee112233aabbccdd0001": {
			{"id": "aaa111aaa111aaa111aaa111", "type": "user"},
			{"id": "bbb222bbb222bbb222bbb222", "type": "user"},
		},
		"group:ccddee112233aabbccdd0002": {
			{"id": "bbb222bbb222bbb222bbb222", "type": "user"}, // duplicate
			{"id": "ccc333ccc333ccc333ccc333", "type": "user"},
		},
	}

	ts := startSimulateServer(t, policies, simulateTestUsers(),
		memberships, nil, nil)
	defer ts.Close()
	overrideV1Client(t, ts.URL)
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "blast-radius", "aabbccddee112233aabb3006"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 3 {
		t.Errorf("got %d members, want 3 (deduplicated)", len(result))
	}
}

func TestBlastRadius_MissingArg(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "blast-radius"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing policy argument")
	}
}

// --- Help Tests ---

func TestAuthPoliciesHelp_Subcommands(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"auth-policies", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	for _, sub := range []string{"list", "get", "create", "update", "delete", "enable", "disable", "simulate", "blast-radius"} {
		if !strings.Contains(out, sub) {
			t.Errorf("help should contain subcommand %q, got:\n%s", sub, out)
		}
	}
}

func TestAuthPoliciesHelp_RootIncludesAuthPolicies(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "auth-policies") {
		t.Errorf("root help should contain 'auth-policies', got:\n%s", out)
	}
}
