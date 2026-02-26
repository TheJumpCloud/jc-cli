package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
