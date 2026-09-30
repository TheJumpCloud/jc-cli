package usergroups

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/api"
)

// serve returns a V2 client pointed at a server that answers each path from
// the supplied map. A path with no entry returns 500.
func serve(t *testing.T, bodies map[string]string) *api.V2Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for path, body := range bodies {
			if strings.HasPrefix(r.URL.Path, path) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
				return
			}
		}
		// 400, not 500: the client retries 5xx with backoff, and a fake
		// catalog returning 500 made this file take 18 seconds. A
		// non-retryable status exercises the same degrade-to-ids path.
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	c := api.NewV2ClientWithKey("test-key")
	c.BaseURL = srv.URL
	return c
}

const twoGroups = `[{"id":"g1","type":"user_group"},{"id":"g2","type":"user_group"}]`

func TestResolve_JoinsNamesFromTheCatalog(t *testing.T) {
	v2 := serve(t, map[string]string{
		"/users/u1/memberof": twoGroups,
		"/usergroups":        `[{"id":"g1","name":"Engineering"},{"id":"g2","name":"All Users"}]`,
		"/systemgroups":      `[]`,
	})
	refs, err := Resolve(context.Background(), v2, UserMemberOfPath("u1"), func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2", len(refs))
	}
	// Sorted by name, so "All Users" precedes "Engineering".
	if refs[0].Name != "All Users" || refs[1].Name != "Engineering" {
		t.Errorf("not sorted by name: %+v", refs)
	}
}

// The asymmetry this package is built around: a missing NAME is cosmetic, a
// missing GROUP is not. A catalog that cannot be read must still report the
// memberships, by id, with a warning.
func TestResolve_CatalogFailureDegradesToIDsRatherThanDroppingGroups(t *testing.T) {
	v2 := serve(t, map[string]string{"/users/u1/memberof": twoGroups}) // catalogs 500
	var warnings []string
	refs, err := Resolve(context.Background(), v2, UserMemberOfPath("u1"),
		func(m string) { warnings = append(warnings, m) })
	if err != nil {
		t.Fatalf("a catalog failure must not fail the lookup: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2 — knowing someone is in 2 groups matters "+
			"more than knowing their names", len(refs))
	}
	for _, r := range refs {
		if r.ID == "" {
			t.Error("a degraded ref must still carry its id")
		}
	}
	if len(warnings) == 0 {
		t.Error("degrading silently is the whole problem; the caller must be warned")
	}
}

// An association jc cannot READ is a group the user might be in. Dropping it
// understates access, which is the wrong direction for a number used in
// offboarding and access reviews.
func TestResolve_UndecodableAssociationIsReported(t *testing.T) {
	v2 := serve(t, map[string]string{
		"/users/u1/memberof": `[{"id":"g1"},"a bare string"]`,
		"/usergroups":        `[]`,
		"/systemgroups":      `[]`,
	})
	_, err := Resolve(context.Background(), v2, UserMemberOfPath("u1"), func(string) {})
	if err == nil {
		t.Fatal("an unreadable association was skipped; the user would be reported " +
			"in fewer groups than they belong to")
	}
	if !strings.Contains(err.Error(), "did not decode") {
		t.Errorf("error should say what happened, got: %v", err)
	}
}

// A record with no id is NOT the same as one that could not be read. The old
// code tested `err != nil || g.ID == ""` and conflated them.
func TestResolve_AssociationWithoutAnIDIsALegitimateSkip(t *testing.T) {
	v2 := serve(t, map[string]string{
		"/users/u1/memberof": `[{"id":"g1","type":"user_group"},{"type":"something_else"}]`,
		"/usergroups":        `[{"id":"g1","name":"Engineering"}]`,
		"/systemgroups":      `[]`,
	})
	refs, err := Resolve(context.Background(), v2, UserMemberOfPath("u1"), func(string) {})
	if err != nil {
		t.Fatalf("an association with no id is not drift: %v", err)
	}
	if len(refs) != 1 || refs[0].Name != "Engineering" {
		t.Errorf("got %+v, want just Engineering", refs)
	}
}

// Unnamed groups sort last so a partial resolution still surfaces what it found.
func TestResolve_UnnamedGroupsSortLast(t *testing.T) {
	v2 := serve(t, map[string]string{
		"/users/u1/memberof": `[{"id":"unknown"},{"id":"g1"}]`,
		"/usergroups":        `[{"id":"g1","name":"Engineering"}]`,
		"/systemgroups":      `[]`,
	})
	refs, err := Resolve(context.Background(), v2, UserMemberOfPath("u1"), func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[0].Name != "Engineering" || refs[1].Name != "" {
		t.Errorf("got %+v, want the named group first", refs)
	}
}

func TestResolve_NoMembershipsReturnsEmptyNotNil(t *testing.T) {
	v2 := serve(t, map[string]string{"/users/u1/memberof": `[]`})
	refs, err := Resolve(context.Background(), v2, UserMemberOfPath("u1"), func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if refs == nil || len(refs) != 0 {
		t.Errorf("got %v, want an empty slice", refs)
	}
	if b, _ := json.Marshal(refs); string(b) != "[]" {
		t.Errorf("empty memberships should marshal as [], got %s", b)
	}
}

func TestMemberOfPaths(t *testing.T) {
	if got := UserMemberOfPath("u1"); got != "/users/u1/memberof" {
		t.Errorf("got %q", got)
	}
	if got := DeviceMemberOfPath("d1"); got != "/systems/d1/memberof" {
		t.Errorf("got %q — devices live under /systems", got)
	}
}
