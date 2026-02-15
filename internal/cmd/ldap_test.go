package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sampleSambaDomains returns test samba domain records.
func sampleSambaDomains() []map[string]any {
	return []map[string]any{
		{"id": "ddd444ddd444ddd444ddd444", "name": "WORKGROUP", "sid": "S-1-5-21-1234567890"},
		{"id": "eee555eee555eee555eee555", "name": "CORP", "sid": "S-1-5-21-9876543210"},
	}
}

// startLDAPServer creates a mock JumpCloud V2 server that handles /ldapservers endpoints
// including samba domain sub-resources.
func startLDAPServer(t *testing.T, servers []map[string]any) *httptest.Server {
	t.Helper()
	sambaDomains := sampleSambaDomains()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /ldapservers — list endpoint.
		if r.URL.Path == "/ldapservers" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(servers)
			return
		}

		// POST /ldapservers — create endpoint.
		if r.URL.Path == "/ldapservers" && r.Method == http.MethodPost {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			input["id"] = "new123new123new123new123"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(input)
			return
		}

		// Routes under /ldapservers/{id}.
		if strings.HasPrefix(r.URL.Path, "/ldapservers/") {
			rest := strings.TrimPrefix(r.URL.Path, "/ldapservers/")
			parts := strings.SplitN(rest, "/", 2)
			id := parts[0]

			// Samba domain sub-resource routes.
			if len(parts) == 2 && strings.HasPrefix(parts[1], "sambadomains") {
				// Verify parent LDAP server exists.
				var parentFound bool
				for _, s := range servers {
					if s["id"] == id {
						parentFound = true
						break
					}
				}
				if !parentFound {
					w.WriteHeader(http.StatusNotFound)
					w.Write([]byte(`{"message":"LDAP server not found"}`))
					return
				}

				sambaPath := parts[1]
				// GET /ldapservers/{id}/sambadomains — list samba domains.
				if sambaPath == "sambadomains" && r.Method == http.MethodGet {
					json.NewEncoder(w).Encode(sambaDomains)
					return
				}
				// POST /ldapservers/{id}/sambadomains — create samba domain.
				if sambaPath == "sambadomains" && r.Method == http.MethodPost {
					var input map[string]any
					json.NewDecoder(r.Body).Decode(&input)
					input["id"] = "fff666fff666fff666fff666"
					w.WriteHeader(http.StatusCreated)
					json.NewEncoder(w).Encode(input)
					return
				}
				// Routes under /ldapservers/{id}/sambadomains/{domainId}.
				if strings.HasPrefix(sambaPath, "sambadomains/") {
					domainID := strings.TrimPrefix(sambaPath, "sambadomains/")
					var domainFound map[string]any
					for _, d := range sambaDomains {
						if d["id"] == domainID {
							domainFound = d
							break
						}
					}
					if domainFound == nil {
						w.WriteHeader(http.StatusNotFound)
						w.Write([]byte(`{"message":"Samba domain not found"}`))
						return
					}
					switch r.Method {
					case http.MethodGet:
						json.NewEncoder(w).Encode(domainFound)
						return
					case http.MethodPut:
						var input map[string]any
						json.NewDecoder(r.Body).Decode(&input)
						for k, v := range input {
							domainFound[k] = v
						}
						json.NewEncoder(w).Encode(domainFound)
						return
					case http.MethodDelete:
						w.WriteHeader(http.StatusOK)
						json.NewEncoder(w).Encode(domainFound)
						return
					}
				}
			}

			// Standard LDAP server routes.
			var found map[string]any
			for _, s := range servers {
				if s["id"] == id {
					found = s
					break
				}
			}

			if found == nil {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}

			switch r.Method {
			case http.MethodGet:
				json.NewEncoder(w).Encode(found)
				return
			case http.MethodPut:
				var input map[string]any
				json.NewDecoder(r.Body).Decode(&input)
				for k, v := range input {
					found[k] = v
				}
				json.NewEncoder(w).Encode(found)
				return
			case http.MethodDelete:
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(found)
				return
			}
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func sampleLDAPServers() []map[string]any {
	return []map[string]any{
		{
			"id":                           "aabbccddee112233aabb6001",
			"name":                         "jumpcloud",
			"userLockoutAction":            "maintain",
			"userPasswordExpirationAction": "maintain",
		},
		{
			"id":                           "aabbccddee112233aabb6002",
			"name":                         "corp-ldap",
			"userLockoutAction":            "disable",
			"userPasswordExpirationAction": "disable",
		},
	}
}

// overrideLDAPConfirmReader injects a bufio.Reader for LDAP confirmation prompts.
func overrideLDAPConfirmReader(t *testing.T, input string) {
	t.Helper()
	orig := confirmReader
	confirmReader = bufio.NewReader(strings.NewReader(input))
	t.Cleanup(func() { confirmReader = orig })
}

// --- List Tests ---

func TestLDAPList_JSON(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d LDAP servers, want 2", len(result))
	}
}

func TestLDAPList_Limit(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "list", "--limit", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	// The mock server returns all items; limit is passed as query param.
	// We just verify the command runs without error.
	if len(result) == 0 {
		t.Error("expected at least 1 result")
	}
}

// --- Get Tests ---

func TestLDAPGet(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "get", "aabbccddee112233aabb6001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "jumpcloud" {
		t.Errorf("name = %q, want 'jumpcloud'", result["name"])
	}
}

func TestLDAPGet_ByName(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "get", "jumpcloud"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["id"] != "aabbccddee112233aabb6001" {
		t.Errorf("id = %q, want 'aabbccddee112233aabb6001'", result["id"])
	}
}

func TestLDAPGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "get", "aabbccddee112233aabb9999"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found LDAP server, got nil")
	}
}

// --- Create Tests ---

func TestLDAPCreate(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "create", "--name", "new-ldap"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "new-ldap" {
		t.Errorf("name = %q, want 'new-ldap'", result["name"])
	}
	if result["id"] != "new123new123new123new123" {
		t.Errorf("id = %q, want 'new123new123new123new123'", result["id"])
	}
}

func TestLDAPCreate_Plan(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "create", "--name", "new-ldap", "--plan"})

	err := cmd.Execute()
	// Plan mode returns ExitError with plan exit code.
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
	if exitErr.Code != 10 {
		t.Errorf("exit code = %d, want 10", exitErr.Code)
	}
}

func TestLDAPCreate_MissingName(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "create"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing --name, got nil")
	}
}

// --- Update Tests ---

func TestLDAPUpdate(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "update", "aabbccddee112233aabb6001", "--user-lockout-action", "disable"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["userLockoutAction"] != "disable" {
		t.Errorf("userLockoutAction = %q, want 'disable'", result["userLockoutAction"])
	}
}

func TestLDAPUpdate_Plan(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "update", "aabbccddee112233aabb6001", "--name", "renamed", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestLDAPUpdate_NoFields(t *testing.T) {
	setupUsersTest(t)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "update", "aabbccddee112233aabb6001"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no fields, got nil")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("error should mention 'no fields to update', got: %v", err)
	}
}

// --- Delete Tests ---

func TestLDAPDelete(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "delete", "aabbccddee112233aabb6001", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("output should confirm deletion, got: %s", out)
	}
}

func TestLDAPDelete_Plan(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "delete", "aabbccddee112233aabb6001", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestLDAPDelete_Cancel(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)
	overrideLDAPConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"ldap", "delete", "aabbccddee112233aabb6001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Should be cancelled, no deletion message.
	if strings.Contains(out.String(), "deleted") {
		t.Errorf("should not have deleted, got: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "Cancelled") {
		t.Errorf("stderr should show 'Cancelled', got: %q", errOut.String())
	}
}

// ========================================================================
// Samba Domain Tests
// ========================================================================

func TestLDAPSambaDomains_List(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "samba-domains", "aabbccddee112233aabb6001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d samba domains, want 2", len(result))
	}
}

func TestLDAPSambaDomain_Get(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "samba-domain-get", "aabbccddee112233aabb6001", "--domain-id", "ddd444ddd444ddd444ddd444"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "WORKGROUP" {
		t.Errorf("name = %q, want 'WORKGROUP'", result["name"])
	}
}

func TestLDAPSambaDomain_Create(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "samba-domain-create", "aabbccddee112233aabb6001", "--name", "NEWDOMAIN", "--sid", "S-1-5-21-111"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "NEWDOMAIN" {
		t.Errorf("name = %q, want 'NEWDOMAIN'", result["name"])
	}
	if result["sid"] != "S-1-5-21-111" {
		t.Errorf("sid = %q, want 'S-1-5-21-111'", result["sid"])
	}
}

func TestLDAPSambaDomain_CreatePlan(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "samba-domain-create", "aabbccddee112233aabb6001", "--name", "X", "--sid", "S-1", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestLDAPSambaDomain_Update(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "samba-domain-update", "aabbccddee112233aabb6001", "--domain-id", "ddd444ddd444ddd444ddd444", "--name", "UPDATED"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["name"] != "UPDATED" {
		t.Errorf("name = %q, want 'UPDATED'", result["name"])
	}
}

func TestLDAPSambaDomain_DeleteForce(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "samba-domain-delete", "aabbccddee112233aabb6001", "--domain-id", "ddd444ddd444ddd444ddd444", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "deleted successfully") {
		t.Errorf("output should confirm deletion, got: %s", out)
	}
}

func TestLDAPSambaDomain_DeletePlan(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "samba-domain-delete", "aabbccddee112233aabb6001", "--domain-id", "ddd444ddd444ddd444ddd444", "--plan"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected ExitError for plan mode, got nil")
	}
	var exitErr *ExitError
	if !errorAs(err, &exitErr) {
		t.Fatalf("expected ExitError, got: %T: %v", err, err)
	}
}

func TestLDAPSambaDomain_NotFound(t *testing.T) {
	setupUsersTest(t)
	servers := sampleLDAPServers()
	ts := startLDAPServer(t, servers)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ldap", "samba-domain-get", "aabbccddee112233aabb6001", "--domain-id", "fff999fff999fff999fff999"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found samba domain")
	}
}
