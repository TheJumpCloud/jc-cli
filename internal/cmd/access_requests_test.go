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
//   - GET /accessrequests — lookup by requesterId, applicationId and state
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

		// GET /accessrequests — as live: a lookup, 400 unless requesterId,
		// applicationId and state are all present, then {"results": [...]}.
		if r.URL.Path == "/accessrequests" && r.Method == http.MethodGet {
			q := r.URL.Query()
			if q.Get("requesterId") == "" || q.Get("applicationId") == "" || q.Get("state") == "" {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"message":"error validating request: error validating GetAccessRequestByRequesterIdApplicationIdState"}`))
				return
			}
			var out []map[string]any
			for _, rq := range reqs {
				if rq["requestorId"] == q.Get("requesterId") && rq["resourceId"] == q.Get("applicationId") &&
					rq["accessState"] == q.Get("state") {
					out = append(out, rq)
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"results": out})
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

// --- List and find tests ---

// The old list never worked: the API has no list. It now explains that and
// names find, rather than surfacing the server's 400.
func TestAccessRequestsList_ExplainsThereIsNoList(t *testing.T) {
	setupUsersTest(t)
	ts := startAccessRequestsServer(t, sampleAccessRequests(), nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	for _, args := range [][]string{{"access-requests", "list"}, {"ar", "ls"}} {
		cmd := NewRootCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "access-requests find") {
			t.Errorf("%v: want the no-list explanation, got %v", args, err)
		}
	}
}

func TestAccessRequestsFind_ResolvesAndFilters(t *testing.T) {
	setupUsersTest(t)
	users := []map[string]any{{"_id": "aabbccddee112233aabb1001", "username": "alice"}}
	devices := []map[string]any{{"_id": "aabbccddee112233aabb2001", "hostname": "JDOE-MBP"}}
	reqs := []map[string]any{
		{"accessId": "a1", "requestorId": "aabbccddee112233aabb1001", "resourceId": "aabbccddee112233aabb2001", "accessState": "granted"},
		{"accessId": "a2", "requestorId": "aabbccddee112233aabb1001", "resourceId": "aabbccddee112233aabb2001", "accessState": "revoked"},
	}
	ts := startAccessRequestsServer(t, reqs, users, devices)
	defer ts.Close()
	overrideV2Client(t, ts.URL)
	overrideV1Client(t, ts.URL)

	cmd := NewRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"access-requests", "find", "--user", "alice", "--device", "JDOE-MBP", "--state", "granted"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("find: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out.String())
	}
	if len(got) != 1 || got[0]["accessId"] != "a1" {
		t.Errorf("want only a1, got %v", got)
	}

	// An empty result carries the unverified-state caveat.
	cmd = NewRootCmd()
	out.Reset()
	errOut.Reset()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"access-requests", "find", "--user", "alice", "--device", "JDOE-MBP", "--state", "nonsense"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("find: %v", err)
	}
	if !strings.Contains(errOut.String(), "have not been verified") {
		t.Errorf("an empty result must carry the caveat, got %q", errOut.String())
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

// --- Update Tests ---

func TestAccessRequestsUpdate(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"access-requests", "update", "aabbccddee112233aabb0001",
		"--expiry", "2026-05-01T00:00:00Z",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["expiry"] != "2026-05-01T00:00:00Z" {
		t.Errorf("expiry = %v, want '2026-05-01T00:00:00Z'", result["expiry"])
	}
}

func TestAccessRequestsUpdate_NoFields(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"access-requests", "update", "aabbccddee112233aabb0001",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no fields specified, got nil")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error = %q, want to contain 'no fields to update'", err.Error())
	}
}

func TestAccessRequestsUpdate_Plan(t *testing.T) {
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
		"access-requests", "update", "aabbccddee112233aabb0001",
		"--expiry", "2026-05-01T00:00:00Z",
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
	if !strings.Contains(stderr, "update") {
		t.Errorf("plan should mention 'update', got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "access request") {
		t.Errorf("plan should mention 'access request', got:\n%s", stderr)
	}
}

// --- Revoke Tests ---

func TestAccessRequestsRevoke(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"access-requests", "revoke", "aabbccddee112233aabb0001",
		"--force",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "revoked") {
		t.Errorf("output should contain 'revoked', got: %s", out)
	}
}

func TestAccessRequestsRevoke_Cancelled(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)
	overrideConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	var buf bytes.Buffer
	errBuf := &bytes.Buffer{}
	cmd.SetOut(&buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"access-requests", "revoke", "aabbccddee112233aabb0001",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "Cancelled") {
		t.Errorf("stderr should contain 'Cancelled', got: %s", stderr)
	}

	// stdout should be empty (no revocation happened).
	if buf.Len() != 0 {
		t.Errorf("stdout should be empty after cancel, got: %s", buf.String())
	}
}

func TestAccessRequestsRevoke_Plan(t *testing.T) {
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
		"access-requests", "revoke", "aabbccddee112233aabb0001",
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
	if !strings.Contains(stderr, "revoke") {
		t.Errorf("plan should mention 'revoke', got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "access request") {
		t.Errorf("plan should mention 'access request', got:\n%s", stderr)
	}
}
