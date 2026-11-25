package bundle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/TheJumpCloud/jc-cli/internal/api"
)

// TestListPolicyGroupMembers_Paginates guards the fix that switched
// listPolicyGroupMembers from a bare Get to ListAll: the /members
// association endpoint paginates via limit/skip, so a group with more
// members than one page must still return every member id. Before the
// fix the tail was silently dropped and those units looked "missing".
func TestListPolicyGroupMembers_Paginates(t *testing.T) {
	const total = 250 // > DefaultV2PageSize (100): forces 3 pages
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/policygroups/pg-1/members" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		skip, _ := strconv.Atoi(q.Get("skip"))
		// The real endpoint caps a page server-side regardless of what
		// the client asks: an unpaginated bare Get gets only the first
		// page, not everything. Model that so this test fails if the fix
		// regresses back to Get.
		if limit == 0 || limit > 100 {
			limit = 100
		}
		out := []map[string]any{}
		for i := skip; i < skip+limit && i < total; i++ {
			out = append(out, map[string]any{
				"to": map[string]string{"id": fmt.Sprintf("pol-%d", i), "type": "policy"},
			})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)

	client := api.NewV2ClientWithKey("test")
	client.BaseURL = srv.URL

	ids, err := listPolicyGroupMembers(context.Background(), client, "pg-1")
	if err != nil {
		t.Fatalf("listPolicyGroupMembers: %v", err)
	}
	if len(ids) != total {
		t.Fatalf("got %d member ids, want %d — pagination tail dropped", len(ids), total)
	}
	// Spot-check the first and last, which land on different pages.
	if ids[0] != "pol-0" || ids[total-1] != fmt.Sprintf("pol-%d", total-1) {
		t.Errorf("boundary ids wrong: first=%s last=%s", ids[0], ids[total-1])
	}
}

// A policy group record that does not decode must not turn a lookup into
// "was the bundle applied?": the unreadable record may be the group.
func TestFindAppliedPolicyGroup_UnreadableRecord(t *testing.T) {
	b := &Bundle{Name: "demo", Version: "1"}
	serve := func(body string) *api.V2Client {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		c := api.NewV2ClientWithKey("test")
		c.BaseURL = srv.URL
		return c
	}
	bad := `{"id":123,"name":"x"}`
	_, _, _, err := findAppliedPolicyGroup(context.Background(), serve(`[`+bad+`]`), b)
	if err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("want unreadable error, got %v", err)
	}
	good := fmt.Sprintf(`{"id":"pg1","name":%q,"description":%q}`, DefaultPolicyGroupName(b), "")
	id, _, _, err := findAppliedPolicyGroup(context.Background(), serve(`[`+bad+`,`+good+`]`), b)
	if err != nil || id != "pg1" {
		t.Fatalf("readable match must still win: %q %v", id, err)
	}
	_, _, _, err = findAppliedPolicyGroup(context.Background(), serve(`[]`), b)
	if err == nil || strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("plain not-found expected, got %v", err)
	}
}
