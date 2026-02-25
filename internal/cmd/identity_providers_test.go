package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func startIdentityProvidersServer(t *testing.T, idps []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/identity-providers" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{
				"identityProviders": idps,
				"totalCount":        len(idps),
			})
			return
		}

		if r.URL.Path == "/identity-providers" && r.Method == http.MethodPost {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			input["id"] = "aabbccddee112233aabb9001"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(input)
			return
		}

		if strings.HasPrefix(r.URL.Path, "/identity-providers/") {
			id := strings.TrimPrefix(r.URL.Path, "/identity-providers/")

			var found map[string]any
			for _, idp := range idps {
				if idp["id"] == id {
					found = idp
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

func sampleIdentityProviders() []map[string]any {
	return []map[string]any{
		{
			"id":   "aabbccddee112233aabb9001",
			"name": "Corporate OIDC",
			"type": "OIDC",
			"oidc": map[string]any{
				"clientId":     "corp-client-123",
				"clientSecret": "",
				"url":          "https://accounts.google.com",
			},
		},
	}
}

func TestIdentityProvidersList_JSON(t *testing.T) {
	setupUsersTest(t)
	idps := sampleIdentityProviders()
	ts := startIdentityProvidersServer(t, idps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"identity-providers", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 1 {
		t.Errorf("got %d items, want 1", len(result))
	}
	if result[0]["clientId"] != "corp-client-123" {
		t.Errorf("expected clientId=corp-client-123, got %v", result[0]["clientId"])
	}
	if result[0]["url"] != "https://accounts.google.com" {
		t.Errorf("expected url=https://accounts.google.com, got %v", result[0]["url"])
	}
	if result[0]["oidc"] != nil {
		t.Errorf("expected oidc to be removed after flattening, got %v", result[0]["oidc"])
	}
}

func TestIdentityProvidersList_Table(t *testing.T) {
	setupUsersTest(t)
	idps := sampleIdentityProviders()
	ts := startIdentityProvidersServer(t, idps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"identity-providers", "list", "--output", "table"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Corporate OIDC") {
		t.Errorf("table should contain 'Corporate OIDC', got:\n%s", out)
	}
}

func TestIdentityProvidersGet_ByID(t *testing.T) {
	setupUsersTest(t)
	idps := sampleIdentityProviders()
	ts := startIdentityProvidersServer(t, idps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"identity-providers", "get", "aabbccddee112233aabb9001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["name"] != "Corporate OIDC" {
		t.Errorf("expected name=Corporate OIDC, got %v", result["name"])
	}
	if result["clientId"] != "corp-client-123" {
		t.Errorf("expected clientId=corp-client-123, got %v", result["clientId"])
	}
}

func TestIdentityProvidersGet_ByName(t *testing.T) {
	setupUsersTest(t)
	idps := sampleIdentityProviders()
	ts := startIdentityProvidersServer(t, idps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"identity-providers", "get", "Corporate OIDC"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if result["name"] != "Corporate OIDC" {
		t.Errorf("expected name=Corporate OIDC, got %v", result["name"])
	}
}

func TestIdentityProvidersCreate(t *testing.T) {
	setupUsersTest(t)
	idps := sampleIdentityProviders()
	ts := startIdentityProvidersServer(t, idps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"identity-providers", "create",
		"--name", "New IdP",
		"--type", "GOOGLE",
		"--client-id", "google-123",
		"--client-secret", "secret-456",
		"--url", "https://accounts.google.com",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}
	if result["name"] != "New IdP" {
		t.Errorf("expected name=New IdP, got %v", result["name"])
	}
}

func TestIdentityProvidersCreate_PlanMode(t *testing.T) {
	setupUsersTest(t)
	ts := startIdentityProvidersServer(t, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"identity-providers", "create",
		"--name", "Plan IdP",
		"--type", "OIDC",
		"--client-id", "plan-client",
		"--client-secret", "plan-secret",
		"--url", "https://example.com",
		"--plan",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected plan mode exit error")
	}
	if exitErr, ok := err.(*ExitError); !ok || exitErr.Code != 10 {
		t.Errorf("expected ExitError with code 10, got %v", err)
	}
}

func TestIdentityProvidersDelete_Force(t *testing.T) {
	setupUsersTest(t)
	idps := sampleIdentityProviders()
	ts := startIdentityProvidersServer(t, idps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"identity-providers", "delete", "aabbccddee112233aabb9001", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Deleted") {
		t.Errorf("expected 'Deleted' in output, got: %s", out)
	}
}

func TestIdentityProviders_Help(t *testing.T) {
	setupUsersTest(t)
	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"identity-providers", "--help"})
	cmd.Execute()

	out := buf.String()
	if !strings.Contains(out, "identity providers") {
		t.Errorf("help should mention identity providers, got:\n%s", out)
	}
}

func TestIdentityProviders_Alias(t *testing.T) {
	setupUsersTest(t)
	idps := sampleIdentityProviders()
	ts := startIdentityProvidersServer(t, idps)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"idp", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}
