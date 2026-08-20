package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startSambaV2Server serves the /ldapservers/{id}/sambadomains family, with a
// faithful full-replace PUT that rejects a body missing name or sid.
func startSambaV2Server(t *testing.T) *httptest.Server {
	t.Helper()
	domain := map[string]any{"id": "ddd444ddd444ddd444ddd444", "name": "WORKGROUP", "sid": "S-1-2-21-999"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := strings.TrimPrefix(r.URL.Path, "/api/v2")
		switch {
		case p == "/ldapservers" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode([]map[string]any{{"id": "aabbccddee112233aabb6001", "name": "jumpcloud"}})
		case strings.HasSuffix(p, "/sambadomains/ddd444ddd444ddd444ddd444") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(domain)
		case strings.HasSuffix(p, "/sambadomains/ddd444ddd444ddd444ddd444") && r.Method == http.MethodPut:
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			if _, ok := in["name"]; !ok {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"message":"missing required property: name"}`))
				return
			}
			if _, ok := in["sid"]; !ok {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"message":"missing required property: sid"}`))
				return
			}
			for k, v := range in {
				domain[k] = v
			}
			json.NewEncoder(w).Encode(domain)
		default:
			t.Errorf("unexpected: %s %s", r.Method, p)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A --name-only update must fetch-merge so the untouched sid survives the
// full-replace PUT.
func TestMCPSambaDomainUpdate_FetchMerge(t *testing.T) {
	overrideV2ClientForTest(t, startSambaV2Server(t).URL)
	cs := connectToolTestServer(t, Options{})
	out := getResultText(t, callTool(t, cs, "ldap_samba_domain_update", map[string]any{
		"ldap_server": "jumpcloud", "domain_id": "ddd444ddd444ddd444ddd444", "name": "UPDATED", "execute": true,
	}))
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if m["name"] != "UPDATED" {
		t.Errorf("name = %v", m["name"])
	}
	if m["sid"] == nil || m["sid"] == "" {
		t.Errorf("partial update must preserve sid via fetch-merge, got %v", m["sid"])
	}
}
