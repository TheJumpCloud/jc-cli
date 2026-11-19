package accessreq

import (
	"strings"
	"testing"
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
	if rows, err := ParseFind([]byte(`{"results":null}`)); err != nil || len(rows) != 0 {
		t.Errorf("null results: %v %v", rows, err)
	}
	for _, bad := range []string{`[]`, `{}`, `{"results":{}}`} {
		if _, err := ParseFind([]byte(bad)); err == nil {
			t.Errorf("%s must be an error", bad)
		}
	}
}
