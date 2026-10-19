package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/klaassen-consulting/jc/internal/api"
)

// pagedAgents serves /agents as a cursor-paged endpoint, recording the query
// of every request so a test can assert what was and was not sent.
//
// It emits NO Link header, like the real one. That is the whole reason this
// package pages for itself: api.V2Client.ListAll falls back to incrementing
// skip when there is no Link header, and the real /agents ignores skip — so
// the fallback would re-request page one forever, appending duplicates.
func pagedAgents(t *testing.T, names []string, pageSize int) (*api.V2Client, func() []string) {
	t.Helper()

	var mu sync.Mutex
	var queries []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.RawQuery)
		mu.Unlock()

		start := 0
		if tok := r.URL.Query().Get("pageToken"); tok != "" {
			if _, err := fmt.Sscanf(tok, "cursor-%d", &start); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		end := start + pageSize
		if end > len(names) {
			end = len(names)
		}

		rows := make([]string, 0, end-start)
		for _, n := range names[start:end] {
			rows = append(rows, fmt.Sprintf(`{"id":"id-%s","name":%q}`, n, n))
		}
		body := fmt.Sprintf(`{"agents":[%s],"totalSize":%d`, strings.Join(rows, ","), len(names))
		if end < len(names) {
			body += fmt.Sprintf(`,"nextPageToken":"cursor-%d"`, end)
		}
		body += "}"

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c := api.NewV2ClientWithKey("test-key")
	c.BaseURL = srv.URL
	return c, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), queries...)
	}
}

func TestListAgents_FollowsEveryPageExactlyOnce(t *testing.T) {
	want := []string{"a", "b", "c", "d", "e", "f", "g"}
	v2, queries := pagedAgents(t, want, 3)

	rows, total, err := ListAgents(context.Background(), v2, nil)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if total != len(want) {
		t.Errorf("totalSize = %d, want %d", total, len(want))
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d — a short read loses agents and a long one repeats them",
			len(rows), len(want))
	}

	got := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, raw := range rows {
		var a struct{ Name string }
		if err := json.Unmarshal(raw, &a); err != nil {
			t.Fatal(err)
		}
		if seen[a.Name] {
			t.Errorf("agent %q appeared twice", a.Name)
		}
		seen[a.Name] = true
		got = append(got, a.Name)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("rows = %v, want %v", got, want)
	}
	// 7 records at 3 a page: three pages, and the third has no cursor.
	if n := len(queries()); n != 3 {
		t.Errorf("made %d requests, want 3", n)
	}
}

func TestListAgents_NeverSendsOffsetOrSortParams(t *testing.T) {
	// /agents ignores limit, skip and sort in silence. Sending one would
	// look like it worked.
	v2, queries := pagedAgents(t, []string{"a", "b", "c", "d"}, 2)
	if _, _, err := ListAgents(context.Background(), v2, nil); err != nil {
		t.Fatal(err)
	}
	for _, q := range queries() {
		for _, banned := range []string{"limit=", "skip=", "sort="} {
			if strings.Contains(q, banned) {
				t.Errorf("request query %q contains %q, which the agents listing silently ignores", q, banned)
			}
		}
	}
}

func TestListAgents_SinglePageMakesOneRequest(t *testing.T) {
	v2, queries := pagedAgents(t, []string{"a", "b"}, 100)
	rows, _, err := ListAgents(context.Background(), v2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Errorf("rows = %d, want 2", len(rows))
	}
	if n := len(queries()); n != 1 {
		t.Errorf("made %d requests for a single page, want 1", n)
	}
}

func TestListAgents_FiltersAreForwarded(t *testing.T) {
	v2, queries := pagedAgents(t, []string{"a"}, 100)
	if _, _, err := ListAgents(context.Background(), v2, []string{"state:eq:active"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(queries()[0], "filter=state%3Aeq%3Aactive") {
		t.Errorf("the filter was not sent: %q", queries()[0])
	}
}

// A server that hands back the same cursor forever. Without the guard this is
// an infinite loop that appends two rows per turn until memory runs out —
// which is exactly what routing /agents through the generic skip-based
// pagination would produce against the real endpoint.
func TestListAgents_RepeatingCursorIsReportedNotLooped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"agents":[{"id":"a"},{"id":"b"}],"totalSize":99,"nextPageToken":"stuck"}`))
	}))
	defer srv.Close()

	v2 := api.NewV2ClientWithKey("test-key")
	v2.BaseURL = srv.URL

	_, _, err := ListAgents(context.Background(), v2, nil)
	if err == nil {
		t.Fatal("a repeating page cursor was followed instead of reported")
	}
	if !strings.Contains(err.Error(), "repeating page cursor") {
		t.Errorf("error does not explain what happened: %v", err)
	}
	if !strings.Contains(err.Error(), "duplicates") {
		t.Errorf("error does not say why jc stopped: %v", err)
	}
}

// A server that keeps issuing fresh cursors. The repeating-cursor guard
// cannot catch this one, so the page bound has to.
func TestListAgents_UnboundedPagingIsStopped(t *testing.T) {
	var n int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		token := n
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(
			`{"agents":[{"id":"a"}],"totalSize":9999999,"nextPageToken":"fresh-%d"}`, token)))
	}))
	defer srv.Close()

	v2 := api.NewV2ClientWithKey("test-key")
	v2.BaseURL = srv.URL

	_, _, err := ListAgents(context.Background(), v2, nil)
	if err == nil {
		t.Fatal("paging never stopped")
	}
	if !strings.Contains(err.Error(), "did not stop paging") {
		t.Errorf("error does not explain what happened: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if n != maxAgentPages {
		t.Errorf("made %d requests, want the bound of %d", n, maxAgentPages)
	}
}

func TestListAgents_ContextCancellationStops(t *testing.T) {
	v2, _ := pagedAgents(t, []string{"a", "b", "c", "d"}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := ListAgents(ctx, v2, nil); err == nil {
		t.Error("a cancelled context did not stop the read")
	}
}

func TestGetAgentsPage_ReportsTheCursorForManualPaging(t *testing.T) {
	v2, _ := pagedAgents(t, []string{"a", "b", "c"}, 2)
	p, err := GetAgentsPage(context.Background(), v2, nil, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rows) != 2 || p.TotalSize != 3 || p.NextPageToken == "" {
		t.Errorf("page = %+v, want 2 rows of 3 and a cursor", p)
	}

	next, err := GetAgentsPage(context.Background(), v2, nil, 2, p.NextPageToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Rows) != 1 || next.NextPageToken != "" {
		t.Errorf("last page = %+v, want 1 row and no cursor", next)
	}
}

func TestListGroups_UsesTheGenericOffsetHelper(t *testing.T) {
	// /agentgroups returns a bare array and honours limit/skip and sort,
	// which is what the generic v2 list helper already does.
	v2, _ := serve(t, map[string]string{
		"/agentgroups": `[{"id":"g1","name":"AWS JKC Agent"},{"id":"g2","name":"SF Agent"}]`,
	})
	rows, err := ListGroups(context.Background(), v2, nil, 0, "name")
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("rows = %d, want 2", len(rows))
	}
}
