package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startTransRuleV2Server mocks the AD translation-rules family. The PUT is
// faithful to the live API (full replace: it rejects a body missing any of the
// replace fields), so a partial update would fail loudly.
func startTransRuleV2Server(t *testing.T) *httptest.Server {
	t.Helper()
	rules := []map[string]any{
		{
			"objectId": "aaaa1111aaaa1111aaaa1111", "source": "firstname", "destination": "givenName",
			"sourceType": "PATH", "direction": "EXPORT", "appliedOn": []string{"CREATE", "UPDATE"}, "editable": true,
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := strings.TrimPrefix(r.URL.Path, "/api/v2")
		const prefix = "/activedirectories/aabbccddee112233aabb7001/translation-rules"

		switch {
		case p == "/activedirectories" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "aabbccddee112233aabb7001", "domain": "corp.example.com"},
			})

		case p == "/activedirectories/activedirectory/translation-rules/recommendation" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"totalCount": 1, "rules": []map[string]any{
				{"objectId": "", "source": "lastname", "destination": "sn", "sourceType": "PATH"},
			}})

		case p == "/activedirectories/translation-rules/preview" && r.Method == http.MethodPost:
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			if in["userObjectId"] == nil || in["translationRules"] == nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"message":"missing required property"}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"sourceUser": `{"username":"alice"}`, "destinationUser": `{"sAMAccountName":"alice"}`,
			})

		case p == prefix && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"totalCount": len(rules), "rules": rules})

		case p == prefix && r.Method == http.MethodPost:
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			in["objectId"] = "cccc3333cccc3333cccc3333"
			rules = append(rules, in)
			w.Write([]byte(`{}`)) // live API returns an empty body

		case p == prefix+"/bulk" && r.Method == http.MethodPost:
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			json.NewEncoder(w).Encode(map[string]any{
				"insertedTranslationRules":       in["insertTranslationRules"],
				"deleteTranslationRuleObjectIds": in["deleteTranslationRuleObjectIds"],
			})

		case strings.HasPrefix(p, prefix+"/") && r.Method == http.MethodPut:
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			for _, key := range []string{"source", "destination", "sourceType", "appliedOn"} {
				if _, ok := in[key]; !ok {
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(`{"message":"missing required property: ` + key + `"}`))
					return
				}
			}
			id := strings.TrimPrefix(p, prefix+"/")
			for _, rule := range rules {
				if rule["objectId"] == id {
					for k, v := range in {
						rule[k] = v
					}
					json.NewEncoder(w).Encode(rule)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)

		case strings.HasPrefix(p, prefix+"/") && r.Method == http.MethodDelete:
			w.Write([]byte(``))

		default:
			t.Errorf("unexpected: %s %s", r.Method, p)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMCPTransRules_List(t *testing.T) {
	overrideV2ClientForTest(t, startTransRuleV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "ad_translation_rules_list", map[string]any{
		"identifier": "corp.example.com",
	}))
	if !strings.Contains(out, "givenName") {
		t.Errorf("unexpected list output: %s", out)
	}
}

func TestMCPTransRules_Recommendations(t *testing.T) {
	overrideV2ClientForTest(t, startTransRuleV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "ad_translation_rules_recommendations", map[string]any{}))
	if !strings.Contains(out, `"sn"`) {
		t.Errorf("unexpected recommendations output: %s", out)
	}
}

// Without execute=true a mutation returns a plan and touches nothing.
func TestMCPTransRules_CreatePlan(t *testing.T) {
	overrideV2ClientForTest(t, startTransRuleV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "ad_translation_rules_create", map[string]any{
		"identifier": "corp.example.com", "source": "department", "destination": "department",
	}))
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if m["plan"] != true {
		t.Errorf("expected a plan, got %s", out)
	}
	effects, _ := m["effects"].(map[string]any)
	if effects["sourceType"] != "PATH" || effects["direction"] != "EXPORT" {
		t.Errorf("plan should show normalized enum defaults: %s", out)
	}
}

// The create endpoint returns {}, so the tool re-reads the list to echo the
// created rule.
func TestMCPTransRules_CreateExecuteEchoesRule(t *testing.T) {
	overrideV2ClientForTest(t, startTransRuleV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "ad_translation_rules_create", map[string]any{
		"identifier": "corp.example.com", "source": "department", "destination": "department",
		"source_type": "path", "execute": true,
	}))
	if !strings.Contains(out, "cccc3333cccc3333cccc3333") {
		t.Errorf("created rule not echoed from re-read: %s", out)
	}
}

func TestMCPTransRules_CreateRejectsBadEnum(t *testing.T) {
	overrideV2ClientForTest(t, startTransRuleV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "ad_translation_rules_create", map[string]any{
		"identifier": "corp.example.com", "source": "a", "destination": "b",
		"source_type": "bogus", "execute": true,
	}))
	if !strings.Contains(out, "invalid source type") {
		t.Errorf("expected enum validation error, got %s", out)
	}
}

// A partial update must fetch-merge so the full-replace PUT keeps the fields
// the caller did not name.
func TestMCPTransRules_UpdateFetchMerge(t *testing.T) {
	overrideV2ClientForTest(t, startTransRuleV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "ad_translation_rules_update", map[string]any{
		"identifier": "corp.example.com", "rule_id": "aaaa1111aaaa1111aaaa1111",
		"source": "preferredName", "execute": true,
	}))
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if m["source"] != "preferredName" {
		t.Errorf("source = %v", m["source"])
	}
	if m["destination"] != "givenName" {
		t.Errorf("partial update must preserve destination, got %v", m["destination"])
	}
	if m["sourceType"] != "PATH" {
		t.Errorf("partial update must preserve sourceType, got %v", m["sourceType"])
	}
}

func TestMCPTransRules_UpdateRequiresAField(t *testing.T) {
	overrideV2ClientForTest(t, startTransRuleV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "ad_translation_rules_update", map[string]any{
		"identifier": "corp.example.com", "rule_id": "aaaa1111aaaa1111aaaa1111", "execute": true,
	}))
	if !strings.Contains(out, "no fields to update") {
		t.Errorf("expected no-fields error, got %s", out)
	}
}

func TestMCPTransRules_DeletePlanNamesMapping(t *testing.T) {
	overrideV2ClientForTest(t, startTransRuleV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "ad_translation_rules_delete", map[string]any{
		"identifier": "corp.example.com", "rule_id": "aaaa1111aaaa1111aaaa1111",
	}))
	if !strings.Contains(out, "firstname") || !strings.Contains(out, "givenName") {
		t.Errorf("delete plan should name the mapping: %s", out)
	}
}

func TestMCPTransRules_Bulk(t *testing.T) {
	overrideV2ClientForTest(t, startTransRuleV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "ad_translation_rules_bulk", map[string]any{
		"identifier": "corp.example.com",
		"bulk_json":  `{"deleteTranslationRuleObjectIds":["aaaa1111aaaa1111aaaa1111"]}`,
		"execute":    true,
	}))
	if !strings.Contains(out, "aaaa1111aaaa1111aaaa1111") {
		t.Errorf("unexpected bulk output: %s", out)
	}
}

func TestMCPTransRules_BulkRejectsUnknownKey(t *testing.T) {
	overrideV2ClientForTest(t, startTransRuleV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})

	out := getResultText(t, callTool(t, cs, "ad_translation_rules_bulk", map[string]any{
		"identifier": "corp.example.com", "bulk_json": `{"insertTranslationRule":[]}`, "execute": true,
	}))
	if !strings.Contains(out, "unknown bulk key") {
		t.Errorf("expected unknown-key error, got %s", out)
	}
}

func TestMCPTransRules_ReadOnlyBlocksMutations(t *testing.T) {
	overrideV2ClientForTest(t, startTransRuleV2Server(t).URL)
	cs := connectToolTestServer(t, Options{ReadOnly: true})

	cases := []struct {
		tool string
		args map[string]any
	}{
		{"ad_translation_rules_create", map[string]any{
			"identifier": "corp.example.com", "source": "a", "destination": "b", "execute": true,
		}},
		{"ad_translation_rules_update", map[string]any{
			"identifier": "corp.example.com", "rule_id": "aaaa1111aaaa1111aaaa1111",
			"source": "a", "execute": true,
		}},
		{"ad_translation_rules_delete", map[string]any{
			"identifier": "corp.example.com", "rule_id": "aaaa1111aaaa1111aaaa1111", "execute": true,
		}},
		{"ad_translation_rules_bulk", map[string]any{
			"identifier": "corp.example.com",
			"bulk_json":  `{"deleteTranslationRuleObjectIds":["x"]}`, "execute": true,
		}},
	}
	for _, c := range cases {
		out := getResultText(t, callTool(t, cs, c.tool, c.args))
		if !strings.Contains(out, "read-only") {
			t.Errorf("%s should be blocked in read-only mode, got %s", c.tool, out)
		}
	}
}
