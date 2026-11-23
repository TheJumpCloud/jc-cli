package emailsubs

import (
	"encoding/json"
	"testing"
)

func TestParse(t *testing.T) {
	rows, subs, err := Parse([]byte(`{"subscriptions":[{"emailKey":"a","title":"A","subscribed":true},{"emailKey":"b","subscribed":false}]}`))
	if err != nil || len(rows) != 2 || len(subs) != 2 || !subs[0].Subscribed || subs[1].EmailKey != "b" {
		t.Fatalf("got %v %v %v", rows, subs, err)
	}
	if on, err := State(subs, "a"); err != nil || !on {
		t.Errorf("State(a) = %v %v", on, err)
	}
	if _, err := State(subs, "zz"); err == nil {
		t.Error("an unknown key must be an error")
	}
	for _, bad := range []string{`{}`, `{"subscriptions":null}`, `[]`, `{"subscriptions":[{"subscribed":"yes"}]}`} {
		if _, _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%s must be an error", bad)
		}
	}
}

func TestBody(t *testing.T) {
	b, _ := json.Marshal(Body("lockout-notice-admin", false))
	if string(b) != `{"subscriptions":[{"emailKey":"lockout-notice-admin","subscribed":false}]}` {
		t.Errorf("body: %s", b)
	}
}
