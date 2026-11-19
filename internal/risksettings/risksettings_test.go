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
		{"lists", "a, b", true}, {"lists", `["a"]`, true}, {"sev", "high", true}} {
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
