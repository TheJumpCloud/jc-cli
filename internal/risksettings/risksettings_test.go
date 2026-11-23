package risksettings

import "testing"

func TestParseAndCoerce(t *testing.T) {
	s, err := Parse([]byte(`{"features":[{"name":"identity_risk_detection","settings":[{"name":"enabled","value":true},{"name":"days","value":60},{"name":"lists","value":[]},{"name":"sev","value":"off"}]}]}`))
	if err != nil || len(s) != 4 {
		t.Fatalf("got %v %v", s, err)
	}
	for _, c := range []struct {
		name, in string
		ok       bool
	}{{"enabled", "false", true}, {"enabled", "maybe", false}, {"days", "61", true}, {"days", "6x", false},
		{"lists", "a, b", true}, {"lists", `["a"]`, true}, {"lists", "13335,15169", false}, {"lists", "[13335]", true},
		{"sev", "high", true}} {
		cur, _ := Find(s, c.name)
		if _, err := Coerce(cur.Value, c.in); (err == nil) != c.ok {
			t.Errorf("%s=%q: %v", c.name, c.in, err)
		}
	}
	if v, _ := Coerce(float64(60), "61"); !Equal(v, float64(61)) {
		t.Error("a whole number must compare equal to its read-back float")
	}
	if _, err := Parse([]byte(`{}`)); err == nil {
		t.Error("no features must be an error")
	}
}

func TestCoerceTypedLists(t *testing.T) {
	for _, c := range []struct {
		name    string
		current any
		in      string
		want    string // JSON of the result; "" means an error
	}{
		{"numeric list, comma-separated", []any{float64(13335)}, "13335, 15169", "[13335,15169]"},
		{"numeric list, JSON", []any{float64(1)}, "[2,3]", "[2,3]"},
		{"numeric list, a word", []any{float64(1)}, "2,x", ""},
		{"text list", []any{"10.0.0.0/8"}, "1.2.3.4/32, 5.6.7.8/32", `["1.2.3.4/32","5.6.7.8/32"]`},
		{"text list keeps numeric-looking text", []any{"a"}, "123", `["123"]`},
		{"bool list", []any{true}, "false,true", "[false,true]"},
		{"empty list, text", []any{}, "a,b", `["a","b"]`},
		{"null takes a JSON number", nil, "90", "90"},
		{"null takes a JSON bool", nil, "false", "false"},
		{"null takes a JSON list", nil, `["a"]`, `["a"]`},
		{"null refuses bare text", nil, "value", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := Coerce(c.current, c.in)
			if c.want == "" {
				if err == nil {
					t.Errorf("want an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !Equal(got, mustJSON(t, c.want)) {
				t.Errorf("got %v, want %s", got, c.want)
			}
		})
	}
}

func mustJSON(t *testing.T, s string) any {
	t.Helper()
	v, err := Coerce(nil, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
