package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/klaassen-consulting/jc/internal/accessreq"
)

const (
	arUserID   = "aabbccddee112233aabb1001"
	arDeviceID = "aabbccddee112233aabb2001"
)

// startAccessRequestFindServer behaves like the live API: the lookup accepts
// anything and answers {"results": [...]}; users and devices exist by id.
func startAccessRequestFindServer(t *testing.T, lookups *atomic.Int32) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/systemusers":
			json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"_id": arUserID, "username": "alice"}}, "totalCount": 1})
		case r.URL.Path == "/api/systems":
			json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"_id": arDeviceID, "hostname": "JDOE-MBP"}}, "totalCount": 1})
		case r.URL.Path == "/api/systemusers/"+arUserID, r.URL.Path == "/api/systems/"+arDeviceID:
			w.Write([]byte(`{"_id":"x"}`))
		case strings.HasPrefix(r.URL.Path, "/api/systemusers/"), strings.HasPrefix(r.URL.Path, "/api/systems/"):
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
		case r.URL.Path == "/api/v2/accessrequests":
			lookups.Add(1)
			q := r.URL.Query()
			if q.Get("requesterId") == arUserID && q.Get("applicationId") == arDeviceID && q.Get("state") == "granted" {
				w.Write([]byte(`{"results":[{"accessId":"a1","accessState":"granted"}]}`))
				return
			}
			w.Write([]byte(`{"results":null}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestMCP_AccessRequestsFind(t *testing.T) {
	setupToolTest(t)
	var lookups atomic.Int32
	ts := startAccessRequestFindServer(t, &lookups)
	overrideV1ClientForTest(t, ts.URL)
	overrideV2ClientForTest(t, ts.URL)
	cs := connectToolTestServer(t, Options{})

	decode := func(text string) map[string]any {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal([]byte(text), &m); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, text)
		}
		return m
	}

	t.Run("a match returns one row and no note", func(t *testing.T) {
		res := callTool(t, cs, "access_requests_find", map[string]any{"user": "alice", "device": "JDOE-MBP", "state": "granted"})
		if res.IsError {
			t.Fatalf("errored: %s", getResultText(t, res))
		}
		m := decode(getResultText(t, res))
		data, _ := m["data"].([]any)
		if len(data) != 1 || m["returned"] != float64(1) || m["note"] != nil {
			t.Errorf("want one row and no note, got %v", m)
		}
	})

	t.Run("an empty result is an array and carries the note", func(t *testing.T) {
		res := callTool(t, cs, "access_requests_find", map[string]any{"user": "alice", "device": "JDOE-MBP", "state": "nonsense"})
		if res.IsError {
			t.Fatalf("errored: %s", getResultText(t, res))
		}
		m := decode(getResultText(t, res))
		data, ok := m["data"].([]any)
		if !ok || len(data) != 0 || m["note"] != accessreq.Unverified {
			t.Errorf("want data [] and the note, got %v", m)
		}
	})

	t.Run("a swapped id errors without a lookup", func(t *testing.T) {
		before := lookups.Load()
		res := callTool(t, cs, "access_requests_find", map[string]any{"user": arDeviceID, "device": arUserID, "state": "granted"})
		if !res.IsError || !strings.Contains(getResultText(t, res), "swapped") {
			t.Errorf("want a swapped-id error, got %s", getResultText(t, res))
		}
		if lookups.Load() != before {
			t.Error("the lookup ran despite an unknown user id")
		}
	})

	t.Run("an unresolvable user name errors without a lookup", func(t *testing.T) {
		before := lookups.Load()
		res := callTool(t, cs, "access_requests_find", map[string]any{"user": "nobody", "device": "JDOE-MBP", "state": "granted"})
		if !res.IsError {
			t.Errorf("want an error, got %s", getResultText(t, res))
		}
		if lookups.Load() != before {
			t.Error("the lookup ran despite an unresolvable user")
		}
	})
}

// The state input's description is a struct tag and cannot be computed, so
// check it names every candidate.
func TestMCP_AccessRequestFindStateTagNamesEveryCandidate(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "access_requests_find" {
			continue
		}
		raw, _ := json.Marshal(tool.InputSchema)
		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		desc := schema.Properties["state"].Description
		for _, st := range accessreq.CandidateStates {
			if !strings.Contains(desc, st) {
				t.Errorf("the state description %q does not name %q", desc, st)
			}
		}
		return
	}
	t.Fatal("access_requests_find is not registered")
}
