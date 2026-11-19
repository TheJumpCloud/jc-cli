package siem

import (
	"strings"
	"testing"
	"time"
)

func TestQueryPath(t *testing.T) {
	s := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	p, err := Query{Since: s, Until: s.Add(time.Hour)}.Path()
	if err != nil || !strings.Contains(p, "event_types=all") || !strings.Contains(p, "since=2026-11-19T10%3A00%3A00Z") {
		t.Fatalf("got %s %v", p, err)
	}
	for name, q := range map[string]Query{
		"too long":  {Since: s, Until: s.Add(61 * time.Minute)},
		"backwards": {Since: s, Until: s.Add(-time.Minute)},
		"all+other": {Since: s, Until: s.Add(time.Minute), Types: []string{"all", "x"}},
		"limit":     {Since: s, Until: s.Add(time.Minute), Limit: 1001},
		"no window": {},
	} {
		if _, err := q.Path(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestParsePage(t *testing.T) {
	p, err := ParsePage([]byte(`{"events":[{"id":"a"}],"next_cursor":"c"}`))
	if err != nil || len(p.Events) != 1 || p.NextCursor != "c" {
		t.Fatalf("got %+v %v", p, err)
	}
	if p, err := ParsePage([]byte(`{"events":[]}`)); err != nil || p.NextCursor != "" {
		t.Errorf("last page: %+v %v", p, err)
	}
	if _, err := ParsePage([]byte(`{}`)); err == nil {
		t.Error("a response with no events field must be an error")
	}
}

func TestNewQuery(t *testing.T) {
	before := time.Now().UTC()
	q, err := NewQuery("", "", 3*time.Hour, "all", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if q.Until.Sub(q.Since) != time.Hour || q.Until.Second() != 0 {
		t.Errorf("default window: %s → %s", q.Since, q.Until)
	}
	if d := before.Add(-3 * time.Hour).Sub(q.Until); d < 0 || d > 2*time.Minute {
		t.Errorf("default window should end about 3h ago, ends %s", q.Until)
	}
	q, err = NewQuery("2026-11-19T10:00:00Z", "", 0, "a,b", 5, "c")
	if err != nil || !q.Until.Equal(q.Since.Add(time.Hour)) || len(q.Types) != 2 || q.Cursor != "c" {
		t.Errorf("since without until: %+v %v", q, err)
	}
	for name, in := range map[string][2]string{
		"until without since": {"", "2026-11-19T11:00:00Z"},
		"since not RFC 3339":  {"2026-11-19 10:00", ""},
		"until not RFC 3339":  {"2026-11-19T10:00:00Z", "11:00"},
	} {
		if _, err := NewQuery(in[0], in[1], 0, "all", 0, ""); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestQueryPathLimits(t *testing.T) {
	s := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	if _, err := (Query{Since: s, Until: s.Add(time.Minute), Limit: -1}).Path(); err == nil {
		t.Error("a negative limit must be refused")
	}
	types := make([]string, 26)
	for i := range types {
		types[i] = "t" + string(rune('a'+i))
	}
	if _, err := (Query{Since: s, Until: s.Add(time.Minute), Types: types}).Path(); err == nil {
		t.Error("26 event types must be refused")
	}
	if p, err := (Query{Since: s, Until: s.Add(time.Minute), Types: types[:25], Limit: 10, Cursor: "c"}).Path(); err != nil ||
		!strings.Contains(p, "limit=10") || !strings.Contains(p, "next_cursor=c") {
		t.Errorf("25 types, limit and cursor: %s %v", p, err)
	}
}
