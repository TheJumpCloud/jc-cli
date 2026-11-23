package accessreq

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"testing"

	"github.com/TheJumpCloud/jc-cli/internal/api"
)

func TestFindPath(t *testing.T) {
	p, err := FindPath("u1", "d1", " pending ")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"requesterId=u1", "applicationId=d1", "state=pending"} {
		if !strings.Contains(p, want) {
			t.Errorf("%s lacks %s", p, want)
		}
	}
	for _, c := range [][3]string{{"", "d", "s"}, {"u", "", "s"}, {"u", "d", " "}} {
		if _, err := FindPath(c[0], c[1], c[2]); err == nil {
			t.Errorf("%v must be refused", c)
		}
	}
}

func TestParseFind(t *testing.T) {
	rows, err := ParseFind([]byte(`{"results":[{"accessId":"a"}]}`))
	if err != nil || len(rows) != 1 {
		t.Fatalf("got %v %v", rows, err)
	}
	if rows, err := ParseFind([]byte(`{"results":null}`)); err != nil || rows == nil || len(rows) != 0 {
		t.Errorf("null results must be an empty, non-nil slice: %#v %v", rows, err)
	}
	for _, bad := range []string{`[]`, `{}`, `{"results":{}}`} {
		if _, err := ParseFind([]byte(bad)); err == nil {
			t.Errorf("%s must be an error", bad)
		}
	}
}

type fakeGetter map[string]error

func (f fakeGetter) Get(_ context.Context, endpoint string) (json.RawMessage, error) {
	if err, ok := f[endpoint]; ok {
		return nil, err
	}
	return json.RawMessage(`{}`), nil
}

func TestConfirm(t *testing.T) {
	ctx := context.Background()
	if err := Confirm(ctx, fakeGetter{}, "u1", "d1"); err != nil {
		t.Fatalf("both exist: %v", err)
	}
	notFound := &api.APIError{StatusCode: http.StatusNotFound}
	err := Confirm(ctx, fakeGetter{"/systemusers/d1": notFound}, "d1", "u1")
	if err == nil || !strings.Contains(err.Error(), "no user has the id d1") || !strings.Contains(err.Error(), "swapped") {
		t.Errorf("a missing user must say so: %v", err)
	}
	err = Confirm(ctx, fakeGetter{"/systems/x": notFound}, "u1", "x")
	if err == nil || !strings.Contains(err.Error(), "no device has the id x") {
		t.Errorf("a missing device must say so: %v", err)
	}
	err = Confirm(ctx, fakeGetter{"/systems/x": &api.APIError{StatusCode: 500}}, "u1", "x")
	if err == nil || !strings.Contains(err.Error(), "checking the device") {
		t.Errorf("other errors pass through: %v", err)
	}
}

func TestUnverifiedNamesEveryCandidate(t *testing.T) {
	for _, st := range CandidateStates {
		if !strings.Contains(Unverified, st) {
			t.Errorf("Unverified does not name %q", st)
		}
	}
}
