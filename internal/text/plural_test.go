package text

import "testing"

func TestPlural(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "s"},
		{1, ""},
		{2, "s"},
		{-1, "s"},
	}
	for _, c := range cases {
		if got := Plural(c.n); got != c.want {
			t.Errorf("Plural(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestCount(t *testing.T) {
	cases := []struct {
		n        int
		singular string
		want     string
	}{
		// The case the list footer got wrong for every single-row list.
		{1, "item", "1 item"},
		{0, "item", "0 items"},
		{2, "item", "2 items"},
		{1, "row", "1 row"},
		{3, "check", "3 checks"},
	}
	for _, c := range cases {
		if got := Count(c.n, c.singular); got != c.want {
			t.Errorf("Count(%d, %q) = %q, want %q", c.n, c.singular, got, c.want)
		}
	}
}

func TestCountOf(t *testing.T) {
	cases := []struct {
		n                int
		singular, plural string
		want             string
	}{
		{1, "entry", "entries", "1 entry"},
		{2, "entry", "entries", "2 entries"},
		{1, "match", "matches", "1 match"},
		{0, "match", "matches", "0 matches"},
	}
	for _, c := range cases {
		if got := CountOf(c.n, c.singular, c.plural); got != c.want {
			t.Errorf("CountOf(%d, %q, %q) = %q, want %q", c.n, c.singular, c.plural, got, c.want)
		}
	}
}
