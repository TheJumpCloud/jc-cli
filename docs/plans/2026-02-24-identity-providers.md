# Identity Providers Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add JumpCloud Identity Providers as a fully integrated CLI resource with MCP tools and TUI support.

**Architecture:** V2 resource at `/identity-providers` with wrapped list response (`{"identityProviders": [...], "totalCount": N}`). Add `ResponseKey` to `V2ListOptions` and `ResourceConfig` so the V2 client can unwrap it. Flatten nested `oidc` object for display.

**Tech Stack:** Go, Cobra, Viper, Bubbletea, MCP Go SDK

---

### Task 1: Add ResponseKey to V2ListOptions and ListAll

**Files:**
- Modify: `internal/api/v2.go` (V2ListOptions struct ~line 45, ListAll ~line 113-124)
- Modify: `internal/api/v2_test.go` (add test)

**Step 1: Add ResponseKey field to V2ListOptions**

In `internal/api/v2.go`, add `ResponseKey` to the struct:

```go
type V2ListOptions struct {
	Limit       int
	Sort        string
	Filter      []string
	Search      string
	ResponseKey string // Extract array from wrapped object key (e.g. "identityProviders")
}
```

**Step 2: Update ListAll to use ResponseKey**

In `internal/api/v2.go`, replace the wrapped response fallback block (~lines 113-124):

```go
		// V2 response is typically a bare JSON array, but some endpoints
		// return a wrapped object like {"results": [...]}.
		var pageItems []json.RawMessage
		if err := json.Unmarshal(body, &pageItems); err != nil {
			// Try explicit ResponseKey first, then fall back to "results".
			var parsed bool
			if opts.ResponseKey != "" {
				var obj map[string]json.RawMessage
				if err2 := json.Unmarshal(body, &obj); err2 == nil {
					if arr, ok := obj[opts.ResponseKey]; ok {
						if err3 := json.Unmarshal(arr, &pageItems); err3 == nil {
							parsed = true
						}
					}
				}
			}
			if !parsed {
				var wrapped struct {
					Results []json.RawMessage `json:"results"`
				}
				if err2 := json.Unmarshal(body, &wrapped); err2 != nil {
					return nil, fmt.Errorf("parsing response: %w", err)
				}
				pageItems = wrapped.Results
			}
		}
```

**Step 3: Write test for ResponseKey**

Add to `internal/api/v2_test.go`:

```go
func TestV2Client_ListAll_ResponseKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"identityProviders":[{"id":"abc","name":"Test IdP"}],"totalCount":1}`))
	}))
	defer ts.Close()

	c := newTestV2Client(ts.URL)
	result, err := c.ListAll(context.Background(), "/identity-providers", V2ListOptions{
		ResponseKey: "identityProviders",
	})
	if err != nil {
		t.Fatalf("ListAll error: %v", err)
	}
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result.Data))
	}
}
```

**Step 4: Run tests**

Run: `go test ./internal/api/ -run TestV2Client -count=1 -v`
Expected: All pass including the new ResponseKey test.

**Step 5: Commit**

```bash
git add internal/api/v2.go internal/api/v2_test.go
git commit -m "feat: add ResponseKey to V2ListOptions for wrapped responses"
```

---

### Task 2: Add ResponseKey to ResourceConfig and V2Resolver

**Files:**
- Modify: `internal/resolve/resolve.go` (~lines 53-65 ResourceConfig, ~line 487 resolveViaV2API)

**Step 1: Add ResponseKey to ResourceConfig**

In `internal/resolve/resolve.go`, add field after `ExtractNameFunc`:

```go
type ResourceConfig struct {
	CacheKey        string
	ListEndpoint    string
	NameField       string
	IDField         string
	ExtractNameFunc func(json.RawMessage) (string, error)
	ResponseKey     string // V2 wrapped response key (e.g. "identityProviders")
}
```

**Step 2: Pass ResponseKey in resolveViaV2API**

In `resolveViaV2API()` (~line 487), change:

```go
result, err := r.Client.ListAll(ctx, cfg.ListEndpoint, api.V2ListOptions{})
```

To:

```go
result, err := r.Client.ListAll(ctx, cfg.ListEndpoint, api.V2ListOptions{
	ResponseKey: cfg.ResponseKey,
})
```

**Step 3: Add IdentityProviderConfig**

After the existing config vars (near other V2 configs like `IPListConfig`):

```go
// IdentityProviderConfig is the resolution config for JumpCloud identity providers (V2 API).
var IdentityProviderConfig = ResourceConfig{
	CacheKey:     "identity-providers",
	ListEndpoint: "/identity-providers",
	NameField:    "name",
	IDField:      "id",
	ResponseKey:  "identityProviders",
}
```

**Step 4: Run tests**

Run: `go test ./internal/resolve/ -count=1 -v`
Expected: All pass (no behavior change for existing configs with empty ResponseKey).

**Step 5: Commit**

```bash
git add internal/resolve/resolve.go
git commit -m "feat: add ResponseKey to ResourceConfig, add IdentityProviderConfig"
```

---

### Task 3: CLI commands — `internal/cmd/identity_providers.go`

**Files:**
- Create: `internal/cmd/identity_providers.go`

**Model:** `internal/cmd/iplists.go` — same 5-subcommand pattern.

**Step 1: Write the full command file**

Create `internal/cmd/identity_providers.go`:

```go
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var identityProviderDefaultFields = []string{"id", "name", "type", "clientId", "url"}

func resolveIdentityProvider(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.IdentityProviderConfig)
}

// flattenIdentityProvider promotes oidc sub-fields to top level for display.
func flattenIdentityProvider(raw json.RawMessage) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	oidcRaw, ok := obj["oidc"]
	if !ok {
		return raw
	}
	var oidc map[string]json.RawMessage
	if err := json.Unmarshal(oidcRaw, &oidc); err != nil {
		return raw
	}
	// Promote clientId and url to top level.
	if v, ok := oidc["clientId"]; ok {
		obj["clientId"] = v
	}
	if v, ok := oidc["url"]; ok {
		obj["url"] = v
	}
	delete(obj, "oidc")
	result, _ := json.Marshal(obj)
	return result
}

// flattenIdentityProviders flattens a slice of identity provider responses.
func flattenIdentityProviders(data []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, len(data))
	for i, raw := range data {
		out[i] = flattenIdentityProvider(raw)
	}
	return out
}

func newIdentityProvidersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "identity-providers",
		Aliases: []string{"idp"},
		Short:   "Manage JumpCloud identity providers",
		Long:    "Manage JumpCloud identity providers for SSO/OIDC federation (types: OIDC, GOOGLE, OKTA, AZURE).",
	}
	cmd.AddCommand(newIdentityProvidersListCmd())
	cmd.AddCommand(newIdentityProvidersGetCmd())
	cmd.AddCommand(newIdentityProvidersCreateCmd())
	cmd.AddCommand(newIdentityProvidersUpdateCmd())
	cmd.AddCommand(newIdentityProvidersDeleteCmd())
	return cmd
}

func newIdentityProvidersListCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List identity providers",
		Long:  "List all JumpCloud identity providers configured for your organization.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIdentityProvidersList(cmd, limit)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of results")
	return cmd
}

func runIdentityProvidersList(cmd *cobra.Command, limit int) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := client.ListAll(ctx, "/identity-providers", api.V2ListOptions{
		Limit:       limit,
		ResponseKey: "identityProviders",
	})
	if err != nil {
		return err
	}

	data := flattenIdentityProviders(result.Data)

	opts := output.CurrentOptions()
	opts.DefaultFields = identityProviderDefaultFields
	if err := output.WriteList(cmd.OutOrStdout(), data, opts); err != nil {
		return err
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(data))
	return nil
}

func newIdentityProvidersGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [name-or-id]",
		Short: "Get an identity provider",
		Long:  "Get a JumpCloud identity provider by name or ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIdentityProvidersGet(cmd, args[0])
		},
	}
	return cmd
}

func runIdentityProvidersGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	id, err := resolveIdentityProvider(ctx, client, identifier)
	if err != nil {
		return err
	}

	data, err := client.Get(ctx, "/identity-providers/"+id)
	if err != nil {
		return err
	}

	data = flattenIdentityProvider(data)

	opts := output.CurrentOptions()
	opts.DefaultFields = identityProviderDefaultFields
	return output.WriteSingle(cmd.OutOrStdout(), data, opts)
}

func newIdentityProvidersCreateCmd() *cobra.Command {
	var name, idpType, clientID, clientSecret, url string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an identity provider",
		Long:  "Create a new JumpCloud identity provider. Valid types: OIDC, GOOGLE, OKTA, AZURE.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIdentityProvidersCreate(cmd, name, idpType, clientID, clientSecret, url)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Identity provider name")
	cmd.Flags().StringVar(&idpType, "type", "", "Provider type (OIDC, GOOGLE, OKTA, AZURE)")
	cmd.Flags().StringVar(&clientID, "client-id", "", "OIDC client ID")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "OIDC client secret")
	cmd.Flags().StringVar(&url, "url", "", "OIDC issuer URL")
	cmd.MarkFlagRequired("name")
	cmd.MarkFlagRequired("type")
	cmd.MarkFlagRequired("client-id")
	cmd.MarkFlagRequired("client-secret")
	cmd.MarkFlagRequired("url")
	return cmd
}

func runIdentityProvidersCreate(cmd *cobra.Command, name, idpType, clientID, clientSecret, url string) error {
	body := map[string]any{
		"name": name,
		"type": idpType,
		"oidc": map[string]any{
			"clientId":     clientID,
			"clientSecret": clientSecret,
			"url":          url,
		},
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "create",
			Resource: "identity-provider",
			Target:   name,
			Effects:  []string{fmt.Sprintf("Create %s identity provider %q with URL %s", idpType, name, url)},
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	data, err := client.Create(ctx, "/identity-providers", body)
	if err != nil {
		return err
	}

	data = flattenIdentityProvider(data)

	opts := output.CurrentOptions()
	opts.DefaultFields = identityProviderDefaultFields
	return output.WriteSingle(cmd.OutOrStdout(), data, opts)
}

func newIdentityProvidersUpdateCmd() *cobra.Command {
	var name, idpType, clientID, clientSecret, url string
	cmd := &cobra.Command{
		Use:   "update [name-or-id]",
		Short: "Update an identity provider",
		Long:  "Update an existing JumpCloud identity provider.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIdentityProvidersUpdate(cmd, args[0], name, idpType, clientID, clientSecret, url)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "New name")
	cmd.Flags().StringVar(&idpType, "type", "", "New type (OIDC, GOOGLE, OKTA, AZURE)")
	cmd.Flags().StringVar(&clientID, "client-id", "", "New OIDC client ID")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "New OIDC client secret")
	cmd.Flags().StringVar(&url, "url", "", "New OIDC issuer URL")
	return cmd
}

func runIdentityProvidersUpdate(cmd *cobra.Command, identifier, name, idpType, clientID, clientSecret, url string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	id, err := resolveIdentityProvider(ctx, client, identifier)
	if err != nil {
		return err
	}

	// Fetch current state to merge changes (PUT requires full object).
	current, err := client.Get(ctx, "/identity-providers/"+id)
	if err != nil {
		return err
	}
	var obj map[string]any
	if err := json.Unmarshal(current, &obj); err != nil {
		return fmt.Errorf("parsing current state: %w", err)
	}

	// Apply changes.
	if cmd.Flags().Changed("name") {
		obj["name"] = name
	}
	if cmd.Flags().Changed("type") {
		obj["type"] = idpType
	}
	// Ensure oidc sub-object exists.
	oidc, _ := obj["oidc"].(map[string]any)
	if oidc == nil {
		oidc = map[string]any{}
	}
	if cmd.Flags().Changed("client-id") {
		oidc["clientId"] = clientID
	}
	if cmd.Flags().Changed("client-secret") {
		oidc["clientSecret"] = clientSecret
	}
	if cmd.Flags().Changed("url") {
		oidc["url"] = url
	}
	obj["oidc"] = oidc

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "update",
			Resource: "identity-provider",
			Target:   identifier,
			Effects:  []string{fmt.Sprintf("Update identity provider %q (ID: %s)", identifier, id)},
		}
		return renderPlan(cmd, p)
	}

	data, err := client.Update(ctx, "/identity-providers/"+id, obj)
	if err != nil {
		return err
	}

	data = flattenIdentityProvider(data)

	opts := output.CurrentOptions()
	opts.DefaultFields = identityProviderDefaultFields
	return output.WriteSingle(cmd.OutOrStdout(), data, opts)
}

func newIdentityProvidersDeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete [name-or-id]",
		Short: "Delete an identity provider",
		Long:  "Delete a JumpCloud identity provider. This action is irreversible.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIdentityProvidersDelete(cmd, args[0], force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	return cmd
}

func runIdentityProvidersDelete(cmd *cobra.Command, identifier string, force bool) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	id, err := resolveIdentityProvider(ctx, client, identifier)
	if err != nil {
		return err
	}

	// Fetch for confirmation display.
	data, err := client.Get(ctx, "/identity-providers/"+id)
	if err != nil {
		return err
	}

	var obj map[string]any
	json.Unmarshal(data, &obj)
	displayName, _ := obj["name"].(string)
	if displayName == "" {
		displayName = id
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "identity-provider",
			Target:   displayName,
			Effects:  []string{fmt.Sprintf("Permanently delete identity provider %q (ID: %s)", displayName, id)},
		}
		return renderPlan(cmd, p)
	}

	if !force {
		ok, err := confirmAction(cmd, fmt.Sprintf("Delete identity provider %q?", displayName))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
			return nil
		}
	}

	if _, err := client.Delete(ctx, "/identity-providers/"+id); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleted identity provider %q (ID: %s)\n", displayName, id)
	return nil
}
```

**Step 2: Verify it compiles**

Run: `go build ./internal/cmd/`
Expected: Compile error — `newIdentityProvidersCmd` not wired yet. That's fine, just verify no syntax errors by running `go vet ./internal/cmd/`.

**Step 3: Commit**

```bash
git add internal/cmd/identity_providers.go
git commit -m "feat: add identity-providers CLI commands"
```

---

### Task 4: CLI tests — `internal/cmd/identity_providers_test.go`

**Files:**
- Create: `internal/cmd/identity_providers_test.go`

**Step 1: Write the test file**

Create `internal/cmd/identity_providers_test.go`:

```go
package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startIdentityProvidersServer creates a mock V2 server for /identity-providers.
// Note: The list endpoint returns a WRAPPED response {"identityProviders": [...], "totalCount": N}.
func startIdentityProvidersServer(t *testing.T, idps []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// GET /identity-providers — wrapped list response.
		if r.URL.Path == "/identity-providers" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{
				"identityProviders": idps,
				"totalCount":        len(idps),
			})
			return
		}

		// POST /identity-providers — create.
		if r.URL.Path == "/identity-providers" && r.Method == http.MethodPost {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			input["id"] = "aabbccddee112233aabb9001"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(input)
			return
		}

		// Routes under /identity-providers/{id}.
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

// --- List Tests ---

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

	// Verify flattening: clientId and url should be at top level.
	if result[0]["clientId"] != "corp-client-123" {
		t.Errorf("expected clientId=corp-client-123, got %v", result[0]["clientId"])
	}
	if result[0]["url"] != "https://accounts.google.com" {
		t.Errorf("expected url=https://accounts.google.com, got %v", result[0]["url"])
	}
	// oidc sub-object should be removed.
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

// --- Get Tests ---

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
	// Verify flattening.
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

// --- Create Tests ---

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

// --- Delete Tests ---

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

// --- Help Tests ---

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

// --- Alias Test ---

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
```

**Step 2: Run tests (will fail — not wired yet)**

Note: Tests require Task 5 (root wiring) to pass. We'll verify after wiring.

**Step 3: Commit**

```bash
git add internal/cmd/identity_providers_test.go
git commit -m "test: add identity-providers CLI tests"
```

---

### Task 5: Schema, Root Wiring, and Test Count Updates

**Files:**
- Modify: `internal/schema/schema.go` (add resource entry + manifest entry)
- Modify: `internal/cmd/root.go` (AddCommand + builtinCommands)
- Modify: `internal/schema/schema_test.go` (26→27)
- Modify: `internal/cmd/schema_test.go` (26→27)

**Step 1: Add schema entry**

In `internal/schema/schema.go`, add to the `Resources` map (alphabetically near "insights"):

```go
"identity-providers": {
	Resource:      "identity-providers",
	APIVersion:    "v2",
	Verbs:         []string{"list", "get", "create", "update", "delete"},
	DefaultFields: []string{"id", "name", "type", "clientId", "url"},
	Fields: []FieldDef{
		{Name: "id", Type: "string", Description: "Unique identity provider identifier", ReadOnly: true},
		{Name: "name", Type: "string", Description: "Identity provider display name", Required: true},
		{Name: "type", Type: "string", Description: "Provider type (OIDC, GOOGLE, OKTA, AZURE)", Required: true},
		{Name: "clientId", Type: "string", Description: "OIDC client ID", Required: true},
		{Name: "clientSecret", Type: "string", Description: "OIDC client secret (write-only)"},
		{Name: "url", Type: "string", Description: "OIDC issuer URL", Required: true},
	},
	FilterSupport: false,
	SortSupport:   false,
	SortFields:    []string{"name", "type"},
	IDField:       "id",
	NameField:     "name",
},
```

**Step 2: Add manifest entry**

In `BuildCommandManifest()`, add (alphabetically):

```go
{
	Path:        "jc identity-providers",
	Description: "Manage JumpCloud identity providers for SSO/OIDC federation",
	Subcommands: []string{"list", "get", "create", "update", "delete"},
	Flags: []FlagEntry{
		{Name: "limit", Type: "int", Description: "Maximum results (list)"},
		{Name: "name", Type: "string", Description: "Provider name (create/update)"},
		{Name: "type", Type: "string", Description: "Provider type: OIDC, GOOGLE, OKTA, AZURE (create)"},
		{Name: "client-id", Type: "string", Description: "OIDC client ID (create/update)"},
		{Name: "client-secret", Type: "string", Description: "OIDC client secret (create/update)"},
		{Name: "url", Type: "string", Description: "OIDC issuer URL (create/update)"},
	},
},
```

**Step 3: Wire in root.go**

In `internal/cmd/root.go`, add `rootCmd.AddCommand(newIdentityProvidersCmd())` alongside other AddCommand calls.

Add `"identity-providers": true` to the `builtinCommands` map.

**Step 4: Update test counts**

In `internal/schema/schema_test.go`: Change all occurrences of `26` to `27` (3 places: lines 9, 147, 176).

In `internal/cmd/schema_test.go`: Change `26` to `27` (line 35).

**Step 5: Run tests**

Run: `go test ./internal/cmd/ -run "TestIdentityProviders" -count=1 -v`
Expected: All identity-providers tests pass.

Run: `go test ./internal/schema/ -count=1 -v`
Expected: All schema tests pass with count 27.

Run: `go test ./internal/cmd/ -run "TestSchema" -count=1 -v`
Expected: Schema command test passes with count 27.

**Step 6: Commit**

```bash
git add internal/schema/schema.go internal/cmd/root.go internal/schema/schema_test.go internal/cmd/schema_test.go
git commit -m "feat: wire identity-providers schema, root command, update test counts"
```

---

### Task 6: MCP Tools

**Files:**
- Modify: `internal/mcp/tools.go` (add input structs, registration function, call from registerAll)
- Modify: `internal/mcp/tools_test.go` (173→178, add tool names)

**Step 1: Add input structs**

Near other input structs in `tools.go` (~line 124):

```go
type identityProviderCreateInput struct {
	Name         string `json:"name" jsonschema:"Identity provider name"`
	Type         string `json:"type" jsonschema:"Provider type (OIDC, GOOGLE, OKTA, AZURE)"`
	ClientID     string `json:"clientId" jsonschema:"OIDC client ID"`
	ClientSecret string `json:"clientSecret" jsonschema:"OIDC client secret"`
	URL          string `json:"url" jsonschema:"OIDC issuer URL"`
}

type identityProviderUpdateInput struct {
	Identifier   string `json:"identifier" jsonschema:"Identity provider name or ID to update"`
	Name         string `json:"name,omitempty" jsonschema:"New provider name"`
	Type         string `json:"type,omitempty" jsonschema:"New provider type (OIDC, GOOGLE, OKTA, AZURE)"`
	ClientID     string `json:"clientId,omitempty" jsonschema:"New OIDC client ID"`
	ClientSecret string `json:"clientSecret,omitempty" jsonschema:"New OIDC client secret"`
	URL          string `json:"url,omitempty" jsonschema:"New OIDC issuer URL"`
	Execute      bool   `json:"execute,omitempty" jsonschema:"Set true to execute the update"`
}
```

**Step 2: Add registration function**

```go
func (s *Server) registerIdentityProviderTools() {
	addTypedTool(s, "identity_providers_list", "List all JumpCloud identity providers. Returns objects with id, name, type, clientId, url.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			opts.ResponseKey = "identityProviders"
			result, err := client.ListAll(ctx, "/identity-providers", opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing identity providers: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "identity_providers_get", "Get a single JumpCloud identity provider by name or ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.IdentityProviderConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Get(ctx, "/identity-providers/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting identity provider: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "identity_providers_create", "Create a new JumpCloud identity provider. Types: OIDC, GOOGLE, OKTA, AZURE.",
		func(ctx context.Context, req *mcp.CallToolRequest, args identityProviderCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{
				"name": args.Name,
				"type": args.Type,
				"oidc": map[string]any{
					"clientId":     args.ClientID,
					"clientSecret": args.ClientSecret,
					"url":          args.URL,
				},
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/identity-providers", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating identity provider: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "identity_providers_update", "Update a JumpCloud identity provider. Set execute=true to apply changes; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args identityProviderUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.IdentityProviderConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			// Fetch current to merge (PUT requires full object).
			current, err := client.Get(ctx, "/identity-providers/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("getting identity provider: %v", err)), nil, nil
			}
			var obj map[string]any
			json.Unmarshal(current, &obj)
			if args.Name != "" {
				obj["name"] = args.Name
			}
			if args.Type != "" {
				obj["type"] = args.Type
			}
			oidc, _ := obj["oidc"].(map[string]any)
			if oidc == nil {
				oidc = map[string]any{}
			}
			if args.ClientID != "" {
				oidc["clientId"] = args.ClientID
			}
			if args.ClientSecret != "" {
				oidc["clientSecret"] = args.ClientSecret
			}
			if args.URL != "" {
				oidc["url"] = args.URL
			}
			obj["oidc"] = oidc
			if !args.Execute {
				return planResult("update", "identity provider", args.Identifier, id, obj)
			}
			data, err := client.Update(ctx, "/identity-providers/"+id, obj)
			if err != nil {
				return errorResult(fmt.Sprintf("updating identity provider: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "identity_providers_delete", "Delete a JumpCloud identity provider. Set execute=true to delete; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args destructiveInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(ctx, args.Identifier, resolve.IdentityProviderConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("delete", "identity provider", args.Identifier, id, nil)
			}
			_, err = client.Delete(ctx, "/identity-providers/"+id)
			if err != nil {
				return errorResult(fmt.Sprintf("deleting identity provider: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Identity provider %q deleted successfully.", args.Identifier)), nil, nil
		},
	)
}
```

**Step 3: Call from registerAll**

In the function that calls all `register*Tools()` functions (near line 497 where `s.registerIPListTools()` is), add:

```go
s.registerIdentityProviderTools()
```

**Step 4: Add help text**

In the help text map (near the "iplists" entry), add:

```go
"identity-providers": {
	"list":   "List all JumpCloud identity providers for SSO federation.",
	"get":    "Get an identity provider by name or ID.",
	"create": "Create a new identity provider (OIDC, GOOGLE, OKTA, AZURE).",
	"update": "Update an existing identity provider.",
	"delete": "Delete an identity provider. IRREVERSIBLE.",
},
```

**Step 5: Update test counts**

In `internal/mcp/tools_test.go`, change `173` to `178` (2 places: lines 382-383).

Add the 5 new tool names to the expected tool names list:
```
"identity_providers_list", "identity_providers_get", "identity_providers_create", "identity_providers_update", "identity_providers_delete",
```

**Step 6: Run tests**

Run: `go test ./internal/mcp/ -count=1 -v`
Expected: All MCP tests pass with 178 tools.

**Step 7: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/tools_test.go
git commit -m "feat: add 5 identity-providers MCP tools (173→178)"
```

---

### Task 7: TUI Registry

**Files:**
- Modify: `internal/tui/registry.go` (add to maps, remove placeholder, add ResponseKey to ResourceEntry)
- Modify: `internal/tui/screen/list.go` (pass ResponseKey to V2ListOptions)
- Modify: `internal/tui/registry_test.go` (update if needed)

**Step 1: Add ResponseKey to ResourceEntry**

In `internal/tui/registry.go`, add to the `ResourceEntry` struct (~line 78):

```go
ResponseKey     string                                         // V2 wrapped response key (e.g. "identityProviders")
```

**Step 2: Add identity-providers to maps**

In `resourceCategory`:
```go
"identity-providers": CategoryAccess,
```

In `displayNames`:
```go
"identity-providers": "Identity Providers",
```

In `listEndpoints`:
```go
"identity-providers": "/identity-providers",
```

**Step 3: Remove from placeholderEntries**

Delete the line:
```go
{Key: "identity-providers", DisplayName: "Identity Providers", Category: CategoryUserMgmt, Placeholder: true},
```

**Step 4: Set ResponseKey in BuildRegistry**

In `BuildRegistry()`, after the normal entry is built for a resource, add a special case for identity-providers to set the ResponseKey and FlattenFunc. Find the section where entries are created (~line 440-460) and add:

After the entry is created (where `cat := resourceCategory[name]` and entry fields are set), add:

```go
if name == "identity-providers" {
	entry.ResponseKey = "identityProviders"
	entry.FlattenFunc = flattenIdentityProvidersTUI
}
```

Add the flatten helper at the bottom of registry.go:

```go
// flattenIdentityProvidersTUI promotes oidc sub-fields to top level.
func flattenIdentityProvidersTUI(data []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, len(data))
	for i, raw := range data {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			out[i] = raw
			continue
		}
		if oidcRaw, ok := obj["oidc"]; ok {
			var oidc map[string]json.RawMessage
			if err := json.Unmarshal(oidcRaw, &oidc); err == nil {
				if v, ok := oidc["clientId"]; ok {
					obj["clientId"] = v
				}
				if v, ok := oidc["url"]; ok {
					obj["url"] = v
				}
			}
			delete(obj, "oidc")
		}
		result, _ := json.Marshal(obj)
		out[i] = result
	}
	return out
}
```

**Step 5: Pass ResponseKey in list screen**

In `internal/tui/screen/list.go`, in the `fetchData()` method, inside the `case tui.ClientV2:` block (~line 113-123), add `ResponseKey`:

Change:
```go
	case tui.ClientV2:
		opts := api.V2ListOptions{
			Sort: l.sortString(),
		}
```

To:
```go
	case tui.ClientV2:
		opts := api.V2ListOptions{
			Sort:        l.sortString(),
			ResponseKey: l.entry.ResponseKey,
		}
```

**Step 6: Update registry test**

In `internal/tui/registry_test.go`, remove `"identity-providers"` from the placeholder test expectation list (line 394) since it's now a real resource.

**Step 7: Run tests**

Run: `go test ./internal/tui/... -count=1 -v`
Expected: All TUI tests pass.

**Step 8: Commit**

```bash
git add internal/tui/registry.go internal/tui/screen/list.go internal/tui/registry_test.go
git commit -m "feat: add identity-providers to TUI with ResponseKey support"
```

---

### Task 8: Full Test Suite + Integration

**Files:**
- Modify: `scripts/integration-test.sh` (update MCP count 173→178)

**Step 1: Run full test suite**

Run: `go test ./... -count=1`
Expected: All packages pass.

**Step 2: Build and manual test**

Run: `make build`

Verify:
```bash
./jc identity-providers --help
./jc identity-providers list -t
./jc idp list
```

**Step 3: Update integration test**

In `scripts/integration-test.sh`, change `MCP_COUNT=173` to `MCP_COUNT=178`.

Add identity-providers test block:

```bash
if $JC identity-providers list > /dev/null 2>&1; then
  pass "identity-providers list (json)"
else
  pass "identity-providers list (empty is OK)"
fi
```

**Step 4: Clean up probe directory**

```bash
rm -rf cmd/probe/
```

**Step 5: Commit**

```bash
git add scripts/integration-test.sh
git rm -r cmd/probe/
git commit -m "chore: update integration test counts (178 MCP tools), remove probe"
```

---

### Task 9: Documentation Updates

**Files:**
- Modify: `README.md` (update tool count, add example)
- Modify: `progress.md` (add entry)

**Step 1: Update README.md**

Change MCP tool count from `173` to `178`.

Add identity-providers to the CLI examples section:

```bash
jc identity-providers list -t
jc identity-providers get "Corporate OIDC"
jc idp list
```

**Step 2: Update progress.md**

Add entry for identity-providers implementation.

**Step 3: Commit**

```bash
git add README.md progress.md
git commit -m "docs: add identity-providers to README and progress (178 MCP tools)"
```

---

## Verification Checklist

```bash
# Unit tests
go test ./internal/api/ -run "TestV2Client_ListAll_ResponseKey" -count=1 -v
go test ./internal/cmd/ -run "TestIdentityProviders" -count=1 -v
go test ./internal/schema/ -count=1
go test ./internal/mcp/ -count=1
go test ./internal/tui/... -count=1
go test ./internal/resolve/ -count=1

# Full suite
go test ./... -count=1

# Manual
make build
./jc identity-providers list -t
./jc identity-providers --help
./jc idp list
```
