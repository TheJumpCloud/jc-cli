package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleTranslationRules() []map[string]any {
	return []map[string]any{
		{
			"objectId":    "aaaa1111aaaa1111aaaa1111",
			"source":      "firstname",
			"destination": "givenName",
			"sourceType":  "PATH",
			"direction":   "EXPORT",
			"appliedOn":   []string{"CREATE", "UPDATE"},
			"editable":    true,
		},
		{
			"objectId":    "bbbb2222bbbb2222bbbb2222",
			"source":      "username",
			"destination": "sAMAccountName",
			"sourceType":  "PATH",
			"direction":   "EXPORT",
			"appliedOn":   []string{"CREATE"},
			"editable":    false,
		},
	}
}

// startTranslationRulesServer mocks the AD + translation-rules V2 endpoints.
// The PUT handler is faithful to the live API: it is a full replace and the
// response echoes the merged rule, so a partial body would visibly drop fields.
func startTranslationRulesServer(t *testing.T, rules []map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path

		switch {
		case p == "/activedirectories" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(sampleADs())
			return

		case p == "/activedirectories/activedirectory/translation-rules/recommendation" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{
				"totalCount": 1,
				"rules": []map[string]any{{
					"objectId": "", "source": "lastname", "destination": "sn",
					"sourceType": "PATH", "direction": "EXPORT", "appliedOn": []string{"CREATE"},
				}},
			})
			return

		case p == "/activedirectories/translation-rules/preview" && r.Method == http.MethodPost:
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			if in["userObjectId"] == nil || in["translationRules"] == nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"message":"missing required property"}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"sourceUser":      `{"username":"alice"}`,
				"destinationUser": `{"sAMAccountName":"alice"}`,
			})
			return
		}

		const prefix = "/activedirectories/aabbccddee112233aabb7001/translation-rules"
		switch {
		case p == prefix && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"totalCount": len(rules), "rules": rules})

		case p == prefix && r.Method == http.MethodPost:
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			in["objectId"] = "cccc3333cccc3333cccc3333"
			in["editable"] = true
			rules = append(rules, in)
			// The live API returns an empty body on create.
			w.Write([]byte(`{}`))

		case p == prefix+"/bulk" && r.Method == http.MethodPost:
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			json.NewEncoder(w).Encode(map[string]any{
				"insertedTranslationRules":       in["insertTranslationRules"],
				"updatedTranslationRules":        []any{},
				"deleteTranslationRuleObjectIds": in["deleteTranslationRuleObjectIds"],
			})

		case strings.HasPrefix(p, prefix+"/") && r.Method == http.MethodPut:
			id := strings.TrimPrefix(p, prefix+"/")
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			for _, key := range []string{"source", "destination", "sourceType", "appliedOn"} {
				if _, ok := in[key]; !ok {
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(`{"message":"missing required property: ` + key + `"}`))
					return
				}
			}
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
			t.Errorf("unexpected request: %s %s", r.Method, p)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runTransRuleCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestADTranslationRules_List(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startTranslationRulesServer(t, sampleTranslationRules()).URL)

	out, err := runTransRuleCmd(t, "ad", "translation-rules", "list", "corp.example.com")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("JSON parse error: %v\n%s", err, out)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rules, want 2", len(got))
	}
	if got[0]["destination"] != "givenName" {
		t.Errorf("destination = %v", got[0]["destination"])
	}
}

func TestADTranslationRules_Recommendations(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startTranslationRulesServer(t, sampleTranslationRules()).URL)

	out, err := runTransRuleCmd(t, "ad", "translation-rules", "recommendations")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("JSON parse error: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0]["destination"] != "sn" {
		t.Errorf("unexpected recommendations: %s", out)
	}
}

// Create normalizes the friendly enum values and, since the API returns an
// empty body, prints the created rule by re-reading the list.
func TestADTranslationRules_Create(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startTranslationRulesServer(t, sampleTranslationRules()).URL)

	out, err := runTransRuleCmd(t, "ad", "translation-rules", "create", "corp.example.com",
		"--source", "department", "--destination", "department", "--source-type", "path")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("JSON parse error: %v\n%s", err, out)
	}
	if got["objectId"] != "cccc3333cccc3333cccc3333" {
		t.Errorf("created rule not echoed from re-read: %s", out)
	}
}

func TestADTranslationRules_CreateRejectsBadEnum(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startTranslationRulesServer(t, sampleTranslationRules()).URL)

	_, err := runTransRuleCmd(t, "ad", "translation-rules", "create", "corp.example.com",
		"--source", "a", "--destination", "b", "--source-type", "bogus")
	if err == nil {
		t.Fatal("expected error for invalid --source-type")
	}
}

// A partial update must fetch-merge: the mock PUT rejects a body missing any
// of the replace fields, and the echoed rule must keep the untouched ones.
func TestADTranslationRules_UpdateFetchMerge(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startTranslationRulesServer(t, sampleTranslationRules()).URL)

	out, err := runTransRuleCmd(t, "ad", "translation-rules", "update", "corp.example.com",
		"aaaa1111aaaa1111aaaa1111", "--source", "preferredName")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("JSON parse error: %v\n%s", err, out)
	}
	if got["source"] != "preferredName" {
		t.Errorf("source = %v", got["source"])
	}
	if got["destination"] != "givenName" {
		t.Errorf("partial update must preserve destination, got %v", got["destination"])
	}
	if got["sourceType"] != "PATH" {
		t.Errorf("partial update must preserve sourceType, got %v", got["sourceType"])
	}
}

func TestADTranslationRules_UpdateRequiresAField(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startTranslationRulesServer(t, sampleTranslationRules()).URL)

	_, err := runTransRuleCmd(t, "ad", "translation-rules", "update", "corp.example.com", "aaaa1111aaaa1111aaaa1111")
	if err == nil {
		t.Fatal("expected error when no field flags are given")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestADTranslationRules_UpdateUnknownRule(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startTranslationRulesServer(t, sampleTranslationRules()).URL)

	_, err := runTransRuleCmd(t, "ad", "translation-rules", "update", "corp.example.com",
		"ffff9999ffff9999ffff9999", "--source", "x")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestADTranslationRules_Delete(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startTranslationRulesServer(t, sampleTranslationRules()).URL)

	out, err := runTransRuleCmd(t, "ad", "translation-rules", "delete", "corp.example.com",
		"aaaa1111aaaa1111aaaa1111", "--force")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("unexpected output: %s", out)
	}
	if !strings.Contains(out, "firstname → givenName") {
		t.Errorf("delete output should name the mapping: %s", out)
	}
}

func TestADTranslationRules_Bulk(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startTranslationRulesServer(t, sampleTranslationRules()).URL)

	path := filepath.Join(t.TempDir(), "bulk.json")
	body := `{"insertTranslationRules":[{"source":"a","destination":"b","sourceType":"PATH"}],"deleteTranslationRuleObjectIds":["bbbb2222bbbb2222bbbb2222"]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runTransRuleCmd(t, "ad", "translation-rules", "bulk", "corp.example.com", "--file", path)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("JSON parse error: %v\n%s", err, out)
	}
	if got["insertedTranslationRules"] == nil {
		t.Errorf("unexpected bulk output: %s", out)
	}
}

func TestADTranslationRules_BulkRejectsEmptyBody(t *testing.T) {
	setupUsersTest(t)
	overrideV2Client(t, startTranslationRulesServer(t, sampleTranslationRules()).URL)

	path := filepath.Join(t.TempDir(), "bulk.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := runTransRuleCmd(t, "ad", "translation-rules", "bulk", "corp.example.com", "--file", path)
	if err == nil || !strings.Contains(err.Error(), "no operations") {
		t.Fatalf("expected no-operations error, got %v", err)
	}
}

func TestADTranslationRules_Preview(t *testing.T) {
	setupUsersTest(t)
	ts := startTranslationRulesServer(t, sampleTranslationRules())
	overrideV2Client(t, ts.URL)
	overrideV1Client(t, startUsersServer(t, sampleUsers()).URL)

	path := filepath.Join(t.TempDir(), "rules.json")
	body := `{"totalCount":1,"rules":[{"source":"username","destination":"sAMAccountName","sourceType":"PATH"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runTransRuleCmd(t, "ad", "translation-rules", "preview",
		"--rules-file", path, "--user", "alice")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("JSON parse error: %v\n%s", err, out)
	}
	if got["destinationUser"] == nil {
		t.Errorf("unexpected preview output: %s", out)
	}
}
