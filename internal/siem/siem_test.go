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
