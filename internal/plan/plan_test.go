package plan

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPlan_RenderHuman_Delete(t *testing.T) {
	p := &Plan{
		Action:     "delete",
		Resource:   "user",
		Target:     "jdoe (5f...1234)",
		Effects:    []string{"Remove user from JumpCloud", "User will lose access to all resources"},
		Reversible: false,
	}

	var buf bytes.Buffer
	if err := p.RenderHuman(&buf); err != nil {
		t.Fatalf("RenderHuman: %v", err)
	}

	out := buf.String()

	// Should contain key plan elements.
	if !strings.Contains(out, "delete") {
		t.Errorf("expected action 'delete' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "user") {
		t.Errorf("expected resource 'user' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "jdoe") {
		t.Errorf("expected target 'jdoe' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Remove user from JumpCloud") {
		t.Errorf("expected effect in output, got:\n%s", out)
	}
	if !strings.Contains(out, "irreversible") || !strings.Contains(strings.ToLower(out), "no") {
		// Should indicate not reversible.
	}
	if !strings.Contains(out, "No changes made (plan mode)") {
		t.Errorf("expected plan footer in output, got:\n%s", out)
	}
}

func TestPlan_RenderHuman_Create(t *testing.T) {
	p := &Plan{
		Action:     "create",
		Resource:   "user",
		Target:     "jdoe",
		Effects:    []string{"username: jdoe", "email: jdoe@acme.com"},
		Reversible: true,
	}

	var buf bytes.Buffer
	if err := p.RenderHuman(&buf); err != nil {
		t.Fatalf("RenderHuman: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "create") {
		t.Errorf("expected action 'create' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "username: jdoe") {
		t.Errorf("expected field in output, got:\n%s", out)
	}
}

func TestPlan_RenderHuman_Update(t *testing.T) {
	p := &Plan{
		Action:     "update",
		Resource:   "user",
		Target:     "jdoe (5f...1234)",
		Effects:    []string{"department: Sales → Engineering"},
		Reversible: true,
	}

	var buf bytes.Buffer
	if err := p.RenderHuman(&buf); err != nil {
		t.Fatalf("RenderHuman: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "update") {
		t.Errorf("expected action 'update' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "department: Sales → Engineering") {
		t.Errorf("expected field change in output, got:\n%s", out)
	}
}

func TestPlan_RenderJSON(t *testing.T) {
	p := &Plan{
		Action:     "delete",
		Resource:   "user",
		Target:     "jdoe (5f...1234)",
		Effects:    []string{"Remove user from JumpCloud"},
		Reversible: false,
	}

	var buf bytes.Buffer
	if err := p.RenderJSON(&buf); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}

	// Parse the JSON output.
	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, buf.String())
	}

	if result["action"] != "delete" {
		t.Errorf("action = %v, want delete", result["action"])
	}
	if result["resource"] != "user" {
		t.Errorf("resource = %v, want user", result["resource"])
	}
	if result["target"] != "jdoe (5f...1234)" {
		t.Errorf("target = %v, want jdoe (5f...1234)", result["target"])
	}
	if result["reversible"] != false {
		t.Errorf("reversible = %v, want false", result["reversible"])
	}

	effects, ok := result["effects"].([]any)
	if !ok || len(effects) != 1 {
		t.Fatalf("effects = %v, want 1-element array", result["effects"])
	}
	if effects[0] != "Remove user from JumpCloud" {
		t.Errorf("effects[0] = %v", effects[0])
	}
}

func TestPlan_RenderJSON_Deterministic(t *testing.T) {
	p := &Plan{
		Action:     "create",
		Resource:   "user",
		Target:     "newuser",
		Effects:    []string{"username: newuser", "email: new@acme.com"},
		Reversible: true,
	}

	var buf1, buf2 bytes.Buffer
	p.RenderJSON(&buf1)
	p.RenderJSON(&buf2)

	if buf1.String() != buf2.String() {
		t.Errorf("RenderJSON is not deterministic:\n%s\nvs\n%s", buf1.String(), buf2.String())
	}
}

func TestPlan_RenderHuman_NoEffects(t *testing.T) {
	p := &Plan{
		Action:   "lock",
		Resource: "user",
		Target:   "jdoe",
	}

	var buf bytes.Buffer
	if err := p.RenderHuman(&buf); err != nil {
		t.Fatalf("RenderHuman: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "lock") {
		t.Errorf("expected action 'lock' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "No changes made (plan mode)") {
		t.Errorf("expected plan footer in output, got:\n%s", out)
	}
}

func TestPlan_Render_UsesFormat(t *testing.T) {
	p := &Plan{
		Action:     "delete",
		Resource:   "device",
		Target:     "MACBOOK-01",
		Effects:    []string{"Remove device record"},
		Reversible: true,
	}

	// JSON format.
	var jsonBuf bytes.Buffer
	if err := p.Render(&jsonBuf, "json"); err != nil {
		t.Fatalf("Render json: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(jsonBuf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Human format (default).
	var humanBuf bytes.Buffer
	if err := p.Render(&humanBuf, "table"); err != nil {
		t.Fatalf("Render table: %v", err)
	}
	if !strings.Contains(humanBuf.String(), "No changes made (plan mode)") {
		t.Errorf("expected human output for non-json format")
	}
}

func TestExitCodePlan(t *testing.T) {
	if ExitCodePlan != 10 {
		t.Errorf("ExitCodePlan = %d, want 10", ExitCodePlan)
	}
}

func TestPlan_RenderHuman_Wrap(t *testing.T) {
	words := strings.TrimSpace(strings.Repeat("alpha beta gamma delta ", 6)) // 143 runes
	words = words[:120]
	words = strings.TrimSpace(words)
	token := strings.Repeat("x", 80)
	tests := []struct {
		name     string
		plan     Plan
		wantRows int // minimum number of box rows; 0 = don't check
		wrapped  bool
		joined   string // expected text rejoined from the box, "" = skip
	}{
		{"fits", Plan{Action: "update", Resource: "user", Target: "jdoe", Effects: []string{"a: b"}}, 0, false, ""},
		{"worded effect", Plan{Action: "update", Resource: "user", Target: "jdoe", Effects: []string{"note: " + words}}, 0, true, "note: " + words},
		{"long token target", Plan{Action: "update", Resource: "user", Target: token}, 0, true, ""},
		{"arrow effect", Plan{Action: "update", Resource: "ade", Target: "t", Effects: []string{"zero-touch: false → true"}}, 0, false, ""},
		{"46 rune arrow effect", Plan{Action: "update", Resource: "ade", Target: "t", Effects: []string{"k: " + strings.Repeat("→", 43)}}, 0, false, ""},
		{"long title", Plan{Action: strings.Repeat("act ", 20), Resource: "r", Target: "t"}, 0, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := tc.plan.RenderHuman(&buf); err != nil {
				t.Fatal(err)
			}
			rows := 0
			var effect []string
			for _, l := range strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n") {
				if strings.HasPrefix(l, "No changes made") {
					continue
				}
				rows++
				if n := utf8.RuneCountInString(l); n != 52 {
					t.Errorf("line is %d runes, want 52: %q", n, l)
				}
				if strings.HasPrefix(l, "│   ") {
					effect = append(effect, strings.TrimSpace(strings.Trim(l, "│ ")))
				}
			}
			out := buf.String()
			if tc.joined != "" {
				if got := strings.Join(effect, " "); got != tc.joined {
					t.Errorf("rejoined effect = %q, want %q", got, tc.joined)
				}
			}
			for _, e := range tc.plan.Effects {
				if !tc.wrapped && !strings.Contains(out, e) {
					t.Errorf("effect %q was wrapped", e)
				}
			}
			if tc.name == "long token target" {
				got := strings.Join(strings.FieldsFunc(out, func(r rune) bool { return r != 'x' }), "")
				if got != token {
					t.Errorf("token chars = %q, want %q", got, token)
				}
			}
		})
	}
}
