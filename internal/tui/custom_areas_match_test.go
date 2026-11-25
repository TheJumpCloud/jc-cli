package tui

import (
	"encoding/json"
	"testing"
)

func TestMatchRows_KeepsUnreadableRow(t *testing.T) {
	rows := []json.RawMessage{json.RawMessage(`"oops"`), json.RawMessage(`{"name":"alice"}`), json.RawMessage(`{"name":"bob"}`)}
	if got := MatchRows(rows, "alice"); len(got) != 2 {
		t.Errorf("want unreadable row kept plus the match, got %d", len(got))
	}
}
