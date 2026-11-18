package featuresettings

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestPath(t *testing.T) {
	if got := Path("", ""); got != Endpoint {
		t.Errorf("no filters: %q", got)
	}
	u, err := url.Parse(Path("mcp", "ai"))
	if err != nil {
		t.Fatal(err)
	}
	f := u.Query()["filter"]
	if len(f) != 2 || f[0] != "name:eq:mcp" || f[1] != "category:eq:ai" {
		t.Errorf("filters = %v, want name:eq:mcp and category:eq:ai (eq only)", f)
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"mcp", "user_mcp", "identity_risk_detection", "ai", "login_ui"} {
		if !ValidName(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "MCP", "mcp&filter=x", "a:b", "user-mcp", "mcp "} {
		if ValidName(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	if !strings.Contains(ErrInvalidName("name", "X").Error(), "lower-case") {
		t.Error("message should name the expected format")
	}
}

const fixture = `{"features":[{"name":"mcp","category":"ai","settings":[{"name":"enabled","value":true},{"name":"session_auto_revoke_days","value":30}]},{"name":"helpdesk_contact_info","category":"login_ui","settings":[{"name":"contact_info","value":{}}]}]}`

func TestParseAndFlatten(t *testing.T) {
	raw, features, err := Parse(json.RawMessage(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 || features[0].Name != "mcp" || len(features[0].Settings) != 2 {
		t.Fatalf("parsed %d features: %+v", len(raw), features)
	}
	rows, err := Flatten(features)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (one per setting)", len(rows))
	}
	var r map[string]any
	_ = json.Unmarshal(rows[1], &r)
	if r["feature"] != "mcp" || r["category"] != "ai" || r["setting"] != "session_auto_revoke_days" || r["value"] != float64(30) {
		t.Errorf("row 1 = %v", r)
	}
}

func TestParse_RejectsWrongShape(t *testing.T) {
	for _, bad := range []string{`[]`, `{}`, `{"features":{}}`} {
		if _, _, err := Parse(json.RawMessage(bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if _, _, err := Parse(json.RawMessage(`{"features":[]}`)); err != nil {
		t.Errorf("empty list rejected: %v", err)
	}
}
