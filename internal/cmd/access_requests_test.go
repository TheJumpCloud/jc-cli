package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/plan"
)

// startAccessRequestsServer creates a mock JumpCloud V2 server that handles access request endpoints:
//   - GET /accessrequests — list (bare JSON array)
//   - POST /accessrequests — create
//   - GET /accessrequests/{id} — get single
//   - PUT /accessrequests/{id} — update
//   - POST /accessrequests/{id}/revoke — revoke
//   - GET /systemusers — V1 user resolution
//   - GET /systems — V1 device resolution
func startAccessRequestsServer(t *testing.T, reqs []map[string]any, users []map[string]any, devices []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /systemusers — V1 user resolution (wrapped response).
		if r.URL.Path == "/systemusers" && r.Method == http.MethodGet {
			if users == nil {
				users = []map[string]any{}
			}
			json.NewEncoder(w).Encode(map[string]any{
				"results":    users,
				"totalCount": len(users),
			})
			return
		}

		// GET /systems — V1 device resolution (wrapped response).
		if r.URL.Path == "/systems" && r.Method == http.MethodGet {
			if devices == nil {
				devices = []map[string]any{}
			}
			json.NewEncoder(w).Encode(map[string]any{
				"results":    devices,
				"totalCount": len(devices),
			})
			return
		}

		// GET /accessrequests — list (bare JSON array).
		if r.URL.Path == "/accessrequests" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(reqs)
			return
		}

		// POST /accessrequests — create.
		if r.URL.Path == "/accessrequests" && r.Method == http.MethodPost {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			input["accessId"] = "aabbccddee112233aabb0099"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(input)
			return
		}

		// Routes under /accessrequests/{id}...
		if strings.HasPrefix(r.URL.Path, "/accessrequests/") {
			rest := strings.TrimPrefix(r.URL.Path, "/accessrequests/")
			parts := strings.SplitN(rest, "/", 2)
			reqID := parts[0]

			var found map[string]any
			for _, req := range reqs {
				if req["accessId"] == reqID {
					found = req
					break
				}
			}

			// Sub-path: /accessrequests/{id}/revoke
			if len(parts) == 2 && parts[1] == "revoke" && r.Method == http.MethodPost {
				if found == nil {
					w.WriteHeader(http.StatusNotFound)
					w.Write([]byte(`{"message":"Not Found"}`))
					return
				}
				resp := make(map[string]any)
				for k, v := range found {
					resp[k] = v
				}
				resp["accessState"] = "revoked"
				json.NewEncoder(w).Encode(resp)
				return
			}

			// No sub-path: /accessrequests/{id}
			if len(parts) == 1 {
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
				}
			}
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func sampleAccessRequests() []map[string]any {
	return []map[string]any{
		{
			"accessId":    "aabbccddee112233aabb0001",
			"requestorId": "aabbccddee112233aabb1001",
			"resourceId":  "aabbccddee112233aabb2001",
			"accessState": "granted",
			"expiry":      "2026-03-01T00:00:00Z",
		},
		{
			"accessId":    "aabbccddee112233aabb0002",
			"requestorId": "aabbccddee112233aabb1002",
			"resourceId":  "aabbccddee112233aabb2002",
			"accessState": "revoked",
			"expiry":      "2026-02-15T00:00:00Z",
		},
	}
}

// --- List Tests ---

func TestAccessRequestsList_JSON(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"access-requests", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d access requests, want 2", len(result))
	}
}

func TestAccessRequestsList_Alias(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ar", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d access requests, want 2", len(result))
	}
}

// --- Get Tests ---

func TestAccessRequestsGet_ByID(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"access-requests", "get", "aabbccddee112233aabb0001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["accessState"] != "granted" {
		t.Errorf("accessState = %v, want 'granted'", result["accessState"])
	}
	if result["accessId"] != "aabbccddee112233aabb0001" {
		t.Errorf("accessId = %v, want 'aabbccddee112233aabb0001'", result["accessId"])
	}
}

func TestAccessRequestsGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"access-requests", "get", "000000000000000000000000"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent ID, got nil")
	}
}

// --- Create Tests ---

func TestAccessRequestsCreate(t *testing.T) {
	setupUsersTest(t)
	users := []map[string]any{
		{"_id": "aabbccddee112233aabb1001", "username": "alice"},
	}
	devices := []map[string]any{
		{"_id": "aabbccddee112233aabb2001", "hostname": "JDOE-MBP"},
	}
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, users, devices)
	defer ts.Close()
	overrideV2Client(t, ts.URL)
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"access-requests", "create",
		"--user", "aabbccddee112233aabb1001",
		"--device", "aabbccddee112233aabb2001",
		"--expiry", "2026-04-01T00:00:00Z",
		"--sudo",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["accessId"] == nil || result["accessId"] == "" {
		t.Error("expected accessId in response")
	}
	if result["requestorId"] != "aabbccddee112233aabb1001" {
		t.Errorf("requestorId = %v, want aabbccddee112233aabb1001", result["requestorId"])
	}
	if result["resourceId"] != "aabbccddee112233aabb2001" {
		t.Errorf("resourceId = %v, want aabbccddee112233aabb2001", result["resourceId"])
	}
}

func TestAccessRequestsCreate_Plan(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"access-requests", "create",
		"--user", "aabbccddee112233aabb1001",
		"--device", "aabbccddee112233aabb2001",
		"--expiry", "2026-04-01T00:00:00Z",
		"--plan",
	})

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
	if !strings.Contains(stderr, "access request") {
		t.Errorf("plan should mention 'access request', got:\n%s", stderr)
	}
}
