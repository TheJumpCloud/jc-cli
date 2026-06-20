package apple_mdm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadComposeConfig_HappyPath(t *testing.T) {
	yaml := `
name: Corp Baseline (macOS)
identifier: com.corp.baseline.macos
organization: ACME Corp
removal_disallowed: true
payloads:
  - type: com.apple.security.firewall
    display_name: Firewall
    values:
      EnableFirewall: true
      EnableStealthMode: true
  - type: com.apple.screensaver
    values:
      idleTime: 600
      askForPassword: true
`
	path := writeTempCompose(t, yaml)
	cfg, err := LoadComposeConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Name != "Corp Baseline (macOS)" {
		t.Errorf("Name = %q", cfg.Name)
	}
	if !cfg.RemovalDisallowed {
		t.Error("RemovalDisallowed should be true")
	}
	if len(cfg.Payloads) != 2 {
		t.Fatalf("Payloads = %d, want 2", len(cfg.Payloads))
	}
	if cfg.Payloads[0].Type != "com.apple.security.firewall" {
		t.Errorf("Payloads[0].Type = %q", cfg.Payloads[0].Type)
	}
	if cfg.Payloads[0].Values["EnableFirewall"] != true {
		t.Errorf("Payloads[0].Values[EnableFirewall] = %v", cfg.Payloads[0].Values["EnableFirewall"])
	}
}

func TestBuildPayloadInstances_AgainstRealCatalog(t *testing.T) {
	// End-to-end against the real embedded catalog. Two simple scalar
	// payloads — same shape an MSP runbook would actually use.
	cfg := &ComposeConfig{
		Name: "Test Baseline",
		Payloads: []ComposePayload{
			{
				Type: "com.apple.security.firewall",
				Values: map[string]any{
					"EnableFirewall":    true,
					"EnableStealthMode": true,
				},
			},
			{
				Type: "com.apple.screensaver",
				Values: map[string]any{
					"askForPassword": true,
					"moduleName":     "Flurry", // required by Apple's screensaver schema
				},
			},
		},
	}
	cat, err := Default()
	if err != nil {
		t.Fatalf("default catalog: %v", err)
	}
	instances, env, err := cfg.BuildPayloadInstances(cat)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if env.DisplayName != "Test Baseline" {
		t.Errorf("env.DisplayName = %q", env.DisplayName)
	}
	if len(instances) != 2 {
		t.Fatalf("len(instances) = %d, want 2", len(instances))
	}
	// Order must match config order — Apple payload order matters
	// for some schemas (SSO before identity, etc.).
	if instances[0].Schema.Type != "com.apple.security.firewall" {
		t.Errorf("instance order broken: [0] = %q", instances[0].Schema.Type)
	}
	if instances[1].Schema.Type != "com.apple.screensaver" {
		t.Errorf("instance order broken: [1] = %q", instances[1].Schema.Type)
	}
}

func TestBuildPayloadInstances_RejectsEmptyName(t *testing.T) {
	cfg := &ComposeConfig{
		Payloads: []ComposePayload{{Type: "com.apple.security.firewall"}},
	}
	cat, _ := Default()
	_, _, err := cfg.BuildPayloadInstances(cat)
	if err == nil || !strings.Contains(err.Error(), "'name' is required") {
		t.Errorf("expected 'name' required error, got %v", err)
	}
}

func TestBuildPayloadInstances_RejectsEmptyPayloads(t *testing.T) {
	cfg := &ComposeConfig{Name: "x"}
	cat, _ := Default()
	_, _, err := cfg.BuildPayloadInstances(cat)
	if err == nil || !strings.Contains(err.Error(), "'payloads' must contain at least one") {
		t.Errorf("expected empty payloads error, got %v", err)
	}
}

func TestBuildPayloadInstances_AggregatesValidationErrors(t *testing.T) {
	// Two broken payloads + one valid one. The aggregated error
	// must mention both broken ones rather than failing fast on the
	// first. Operators iterate on multi-payload configs and want to
	// fix all problems in one editor session.
	cfg := &ComposeConfig{
		Name: "x",
		Payloads: []ComposePayload{
			{Type: "this.payload.does.not.exist"},
			{Type: "com.apple.security.firewall", Values: map[string]any{"NotAKey": "wrong"}},
			{Type: "com.apple.security.firewall", Values: map[string]any{"EnableFirewall": true}}, // valid
		},
	}
	cat, _ := Default()
	_, _, err := cfg.BuildPayloadInstances(cat)
	if err == nil {
		t.Fatal("expected validation error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "this.payload.does.not.exist") {
		t.Errorf("error missing unknown-type mention: %v", err)
	}
	if !strings.Contains(msg, "com.apple.security.firewall") {
		t.Errorf("error missing schema-validation mention: %v", err)
	}
}

func TestBuildPayloadInstances_MCXAmbiguityErrors(t *testing.T) {
	// com.apple.MCX has 6 variants. Without an explicit ID the
	// loader should refuse and list the variants.
	cfg := &ComposeConfig{
		Name:     "x",
		Payloads: []ComposePayload{{Type: "com.apple.MCX"}},
	}
	cat, _ := Default()
	_, _, err := cfg.BuildPayloadInstances(cat)
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error should mention ambiguity: %v", err)
	}
	if !strings.Contains(err.Error(), "set 'id'") {
		t.Errorf("error should suggest setting 'id': %v", err)
	}
}

func TestBuildPayloadInstances_MCXVariantByID(t *testing.T) {
	// Same ambiguous type but disambiguated by explicit ID.
	cfg := &ComposeConfig{
		Name: "x",
		Payloads: []ComposePayload{{
			Type: "com.apple.MCX",
			ID:   "com.apple.MCX(EnergySaver)",
		}},
	}
	cat, _ := Default()
	instances, _, err := cfg.BuildPayloadInstances(cat)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if instances[0].Schema.ID != "com.apple.MCX(EnergySaver)" {
		t.Errorf("Schema.ID = %q, want com.apple.MCX(EnergySaver)", instances[0].Schema.ID)
	}
}

func TestBuildPayloadInstances_MismatchedIDAndType(t *testing.T) {
	// Explicit ID resolves, but its PayloadType disagrees with the
	// operator's declared Type. Almost always a copy-paste error;
	// surface it loudly.
	cfg := &ComposeConfig{
		Name: "x",
		Payloads: []ComposePayload{{
			Type: "com.apple.NOT.MCX",
			ID:   "com.apple.MCX(EnergySaver)",
		}},
	}
	cat, _ := Default()
	_, _, err := cfg.BuildPayloadInstances(cat)
	if err == nil {
		t.Fatal("expected mismatch error")
	}
	if !strings.Contains(err.Error(), "resolves to payloadtype") {
		t.Errorf("error should mention mismatch: %v", err)
	}
}

func writeTempCompose(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
