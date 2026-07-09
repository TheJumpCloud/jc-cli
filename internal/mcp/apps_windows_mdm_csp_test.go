package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// installFixtureSnapshotMCP points the catalog cache at a temp dir
// holding the hand-written DDF fixture (no Microsoft content), so the
// csp tools run offline. windows_mdm.DefaultCatalog memoizes per
// process; every csp test in this binary shares the fixture catalog.
func installFixtureSnapshotMCP(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	dir := filepath.Join(root, "jc", "windows-mdm-ddf", windows_mdm.SnapshotName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Sample_AreaDDF.xml", "SampleCSP.xml"} {
		src, err := os.ReadFile(filepath.Join("..", "windows_mdm", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".complete.v2"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsMDMCSPSearch_FiltersAndTruncation(t *testing.T) {
	installFixtureSnapshotMCP(t)
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	// Unfiltered — everything in the fixture, no truncation at the
	// default limit.
	res := callTool(t, cs, "windows_mdm_csp_search", map[string]any{})
	if res.IsError {
		t.Fatalf("search errored: %s", getResultText(t, res))
	}
	var all windowsMDMCSPSearchResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &all); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// 5 policy-area + 3 standalone-CSP fixture settings (KLA-467).
	if all.Total != 8 || all.Matched != 8 || all.Truncated {
		t.Errorf("unfiltered result wrong: %+v", all)
	}

	// Search over description text.
	res = callTool(t, cs, "windows_mdm_csp_search", map[string]any{"search": "times out"})
	var hits windowsMDMCSPSearchResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &hits); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if hits.Matched != 1 || hits.Settings[0].Setting != "Sample/WidgetTimeout" {
		t.Errorf("search filter wrong: %+v", hits)
	}

	// exclude_admx drops the flagged entry; scope filters user rows.
	res = callTool(t, cs, "windows_mdm_csp_search", map[string]any{
		"scope": "device", "exclude_admx": true,
	})
	var filtered windowsMDMCSPSearchResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &filtered); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, s := range filtered.Settings {
		if s.ADMXBacked || s.Scope != "device" {
			t.Errorf("filter leak: %+v", s)
		}
	}

	// Truncation is explicit, never silent.
	res = callTool(t, cs, "windows_mdm_csp_search", map[string]any{"limit": 2})
	var capped windowsMDMCSPSearchResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &capped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if capped.Returned != 2 || !capped.Truncated || capped.Matched != 8 {
		t.Errorf("truncation flags wrong: %+v", capped)
	}

	// Bad scope errors cleanly.
	res = callTool(t, cs, "windows_mdm_csp_search", map[string]any{"scope": "machine"})
	if !res.IsError {
		t.Error("expected error for bad scope")
	}
}

func TestWindowsMDMCSPShow_FullMetadata(t *testing.T) {
	installFixtureSnapshotMCP(t)
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "windows_mdm_csp_show", map[string]any{"setting": "sample/allowwidget"})
	if res.IsError {
		t.Fatalf("show errored: %s", getResultText(t, res))
	}
	var out windowsMDMCSPShowResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	s := out.Setting
	if s.URI != "./Device/Vendor/MSFT/Policy/Config/Sample/AllowWidget" ||
		s.Format != "int" || s.AllowedValues == nil || len(s.AllowedValues.Enum) != 2 {
		t.Errorf("show metadata wrong: %+v", s)
	}

	// Miss — error steers the agent to search.
	res = callTool(t, cs, "windows_mdm_csp_show", map[string]any{"setting": "Sample/Nope"})
	if !res.IsError || !strings.Contains(getResultText(t, res), "windows_mdm_csp_search") {
		t.Errorf("miss should point at the search tool: %s", getResultText(t, res))
	}
}

func TestWindowsMDMCSPTemplate_FeedsCreateTool(t *testing.T) {
	installFixtureSnapshotMCP(t)
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})

	res := callTool(t, cs, "windows_mdm_csp_template", map[string]any{
		"settings": []string{"Sample/AllowWidget", "Sample/LegacyAdmxThing"},
	})
	if res.IsError {
		t.Fatalf("template errored: %s", getResultText(t, res))
	}
	var out windowsMDMCSPTemplateResult
	if err := json.Unmarshal([]byte(getResultText(t, res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Settings) != 2 || out.Settings[0].Value != "1" || out.Settings[0].Format != "int" {
		t.Errorf("template settings wrong: %+v", out.Settings)
	}
	// ADMX warning present and explicit.
	if len(out.Warnings) != 1 || !strings.Contains(out.Warnings[0], "ADMX-backed") {
		t.Errorf("expected 1 ADMX warning: %v", out.Warnings)
	}
	// The contract: the emitted triples validate on the create path.
	converted := make([]windows_mdm.OMAURISetting, len(out.Settings))
	copy(converted, out.Settings)
	if _, err := windows_mdm.NormalizeAndValidateSettings(converted[:1]); err != nil {
		t.Errorf("template output should pass create-path validation: %v", err)
	}

	// Empty settings errors.
	res = callTool(t, cs, "windows_mdm_csp_template", map[string]any{"settings": []string{}})
	if !res.IsError {
		t.Error("expected error for empty settings list")
	}
}
