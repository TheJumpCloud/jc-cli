package agents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/klaassen-consulting/jc/internal/api"
)

// serve returns a V2 client pointed at a server answering each path from the
// supplied map, and a counter of requests per path.
//
// Unmatched paths answer 400 rather than 500: the client retries 5xx with
// backoff, which turns a test of the degrade path into an 18-second one.
func serve(t *testing.T, bodies map[string]string) (*api.V2Client, func(string) int) {
	t.Helper()

	var mu sync.Mutex
	hits := map[string]int{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()

		if body, ok := bodies[r.URL.Path]; ok {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	c := api.NewV2ClientWithKey("test-key")
	c.BaseURL = srv.URL
	return c, func(path string) int {
		mu.Lock()
		defer mu.Unlock()
		return hits[path]
	}
}

// The three catalogs, each spelling its id and name differently.
var liveCatalogs = map[string]string{
	"/agentgroups":  `[{"id":"6aa85141bda774000189bca6","name":"AWS JKC Agent","type":"agent_group"}]`,
	"/agents":       `{"agents":[{"id":"6aa85171a36f7d16b20d9b60","name":"AWS JKC Agent","state":"active"}],"totalSize":1}`,
	"/applications": `[{"_id":"6331214f6655eeee1d2e2ab5","name":"aws-sso","displayName":"AWS IAM Identity Center","displayLabel":"AWS"}]`,
}

func TestCatalog_NamesEachObjectType(t *testing.T) {
	v2, _ := serve(t, liveCatalogs)
	cat := NewCatalog(v2, nil)
	ctx := context.Background()

	cases := []struct{ objType, id, want string }{
		{"agent_group", "6aa85141bda774000189bca6", "AWS JKC Agent"},
		{"agent", "6aa85171a36f7d16b20d9b60", "AWS JKC Agent"},
		// An application's id is "_id" and its operator-facing name is
		// displayLabel. Reading "name" would give the slug "aws-sso" and
		// "displayName" the connector type.
		{"application", "6331214f6655eeee1d2e2ab5", "AWS"},
	}
	for _, c := range cases {
		got, err := cat.Name(ctx, c.objType, c.id)
		if err != nil {
			t.Fatalf("Name(%s, %s): %v", c.objType, c.id, err)
		}
		if got != c.want {
			t.Errorf("Name(%s, %s) = %q, want %q", c.objType, c.id, got, c.want)
		}
	}
}

func TestCatalog_LoadsEachCatalogAtMostOnce(t *testing.T) {
	v2, hits := serve(t, liveCatalogs)
	cat := NewCatalog(v2, nil)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := cat.Name(ctx, "agent_group", "6aa85141bda774000189bca6"); err != nil {
			t.Fatal(err)
		}
		// An id that is not in the catalog must not trigger a reload.
		if _, err := cat.Name(ctx, "agent_group", "absent"); err != nil {
			t.Fatal(err)
		}
	}
	if n := hits("/agentgroups"); n != 1 {
		t.Errorf("the agent groups catalog was fetched %d times, want 1 — resolving N refs "+
			"must not cost N requests", n)
	}
	if n := hits("/applications"); n != 0 {
		t.Errorf("the applications catalog was fetched %d times for a command that resolved "+
			"no applications, want 0", n)
	}
}

func TestCatalog_UnknownObjectTypeIsNotAnError(t *testing.T) {
	v2, _ := serve(t, liveCatalogs)
	name, err := NewCatalog(v2, nil).Name(context.Background(), "policy", "xxx")
	if err != nil {
		t.Fatalf("an unseen object type produced an error: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
}

func TestCatalog_CatalogFailureDegradesToIDs(t *testing.T) {
	// Nothing is served, so every catalog fetch fails. A missing name is
	// cosmetic; a missing membership is not, so the command must still run.
	v2, _ := serve(t, nil)
	var warnings []string
	cat := NewCatalog(v2, func(s string) { warnings = append(warnings, s) })

	name, err := cat.Name(context.Background(), "agent_group", "6aa85141bda774000189bca6")
	if err != nil {
		t.Fatalf("a failed catalog fetch failed the command: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one — a silent degrade leaves an operator "+
			"wondering why every name is blank", warnings)
	}
	if !strings.Contains(warnings[0], "/agentgroups") {
		t.Errorf("the warning does not name the catalog that failed: %q", warnings[0])
	}
}

func TestCatalog_UndecodableCatalogRecordIsReported(t *testing.T) {
	// Distinct from a failed fetch: a catalog that half-reads produces names
	// that are missing with no indication why.
	v2, _ := serve(t, map[string]string{"/agentgroups": `["not an object"]`})
	_, err := NewCatalog(v2, nil).Name(context.Background(), "agent_group", "g1")
	if err == nil {
		t.Error("a catalog record that would not decode was ignored")
	}
}

func TestResolveGraph_NamesTheTargetAndTheGroupItCameThrough(t *testing.T) {
	v2, _ := serve(t, liveCatalogs)
	cat := NewCatalog(v2, nil)

	refs, err := ParseGraph([]byte(appsViaGroup), "applications")
	if err != nil {
		t.Fatal(err)
	}
	named, err := ResolveGraph(context.Background(), cat, refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(named) != 1 {
		t.Fatalf("named = %d rows, want 1", len(named))
	}
	if named[0].Name != "AWS" {
		t.Errorf("Name = %q, want \"AWS\"", named[0].Name)
	}
	// "AWS, via AWS JKC Agent" answers the question an operator has; a hex
	// id does not.
	if len(named[0].Via) != 1 || named[0].Via[0] != "AWS JKC Agent" {
		t.Errorf("Via = %v, want the group's name", named[0].Via)
	}
}

func TestResolveGraph_UnresolvedViaFallsBackToTheID(t *testing.T) {
	v2, _ := serve(t, map[string]string{"/applications": liveCatalogs["/applications"]})
	cat := NewCatalog(v2, func(string) {})

	refs, err := ParseGraph([]byte(appsViaGroup), "applications")
	if err != nil {
		t.Fatal(err)
	}
	named, err := ResolveGraph(context.Background(), cat, refs)
	if err != nil {
		t.Fatal(err)
	}
	// Dropping the hop would hide that the access is inherited at all.
	if len(named[0].Via) != 1 || named[0].Via[0] != "6aa85141bda774000189bca6" {
		t.Errorf("Via = %v, want the group id when its name cannot be resolved", named[0].Via)
	}
}

func TestResolveMembers_NamesAgents(t *testing.T) {
	v2, _ := serve(t, liveCatalogs)
	cat := NewCatalog(v2, nil)

	refs, err := ParseMembers([]byte(`[{"to":{"id":"6aa85171a36f7d16b20d9b60","type":"agent"}}]`), "members")
	if err != nil {
		t.Fatal(err)
	}
	named, err := ResolveMembers(context.Background(), cat, refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(named) != 1 || named[0].Name != "AWS JKC Agent" {
		t.Errorf("named = %+v, want the agent's name", named)
	}
}

func TestResolveMembers_NamedRowsSortFirst(t *testing.T) {
	v2, _ := serve(t, liveCatalogs)
	cat := NewCatalog(v2, func(string) {})

	refs, err := ParseMembers([]byte(`[
      {"to":{"id":"ffffffffffffffffffffffff","type":"agent"}},
      {"to":{"id":"6aa85171a36f7d16b20d9b60","type":"agent"}}]`), "members")
	if err != nil {
		t.Fatal(err)
	}
	named, err := ResolveMembers(context.Background(), cat, refs)
	if err != nil {
		t.Fatal(err)
	}
	// A partial resolution still surfaces the names it did find.
	if named[0].Name == "" {
		t.Errorf("an unnamed row sorted first: %+v", named)
	}
}
