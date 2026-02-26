# Access Requests Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `jc access-requests` as a V2 CLI resource for managing temporary elevated device privileges.

**Architecture:** Standard V2 resource pattern (5 subcommands, no resolver needed since access requests use UUID identifiers). The revoke action uses `V2Client.Create()` since it's a `POST /accessrequests/{id}/revoke`. User and device flags on create resolve names via existing `UserConfig` and `DeviceConfig` resolvers.

**Tech Stack:** Go, Cobra, Viper, httptest

---

### Task 1: Create CLI command file with mock server and list test

**Files:**
- Create: `internal/cmd/access_requests.go`
- Create: `internal/cmd/access_requests_test.go`

**Step 1: Write the test file skeleton with mock server and list test**

Create `internal/cmd/access_requests_test.go`:

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

func startAccessRequestsServer(t *testing.T, requests []map[string]any, users []map[string]any, devices []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// V1 /systemusers — for user name resolution during create.
		if r.URL.Path == "/systemusers" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{
				"results":    users,
				"totalCount": len(users),
			})
			return
		}

		// V1 /systems — for device name resolution during create.
		if r.URL.Path == "/systems" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{
				"results":    devices,
				"totalCount": len(devices),
			})
			return
		}

		// GET /accessrequests — list.
		if r.URL.Path == "/accessrequests" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(requests)
			return
		}

		// POST /accessrequests — create.
		if r.URL.Path == "/accessrequests" && r.Method == http.MethodPost {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			input["accessId"] = "acc123acc123acc123acc123"
			input["accessState"] = "granted"
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(input)
			return
		}

		// Routes under /accessrequests/{id}...
		if strings.HasPrefix(r.URL.Path, "/accessrequests/") {
			rest := strings.TrimPrefix(r.URL.Path, "/accessrequests/")
			parts := strings.SplitN(rest, "/", 2)
			reqID := parts[0]

			// Find the access request.
			var found map[string]any
			for _, ar := range requests {
				if ar["accessId"] == reqID {
					found = ar
					break
				}
			}

			// POST /accessrequests/{id}/revoke
			if len(parts) == 2 && parts[1] == "revoke" && r.Method == http.MethodPost {
				if found == nil {
					w.WriteHeader(http.StatusNotFound)
					w.Write([]byte(`{"message":"Not Found"}`))
					return
				}
				resp := make(map[string]any)
				for k, v := range found {
					resp[k] = v
				}
				resp["accessState"] = "revoked"
				json.NewEncoder(w).Encode(resp)
				return
			}

			// No sub-path: /accessrequests/{id}
			if len(parts) == 1 {
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
				}
			}
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
}

func sampleAccessRequests() []map[string]any {
	return []map[string]any{
		{
			"accessId":    "aabbccddee112233aabb0001",
			"requestorId": "aabbccddee112233aabb1001",
			"resourceId":  "aabbccddee112233aabb2001",
			"accessState": "granted",
			"expiry":      "2026-03-01T00:00:00Z",
		},
		{
			"accessId":    "aabbccddee112233aabb0002",
			"requestorId": "aabbccddee112233aabb1002",
			"resourceId":  "aabbccddee112233aabb2002",
			"accessState": "revoked",
			"expiry":      "2026-02-15T00:00:00Z",
		},
	}
}

func sampleUsersForAR() []map[string]any {
	return []map[string]any{
		{"_id": "aabbccddee112233aabb1001", "username": "alice", "email": "alice@example.com"},
	}
}

func sampleDevicesForAR() []map[string]any {
	return []map[string]any{
		{"_id": "aabbccddee112233aabb2001", "displayName": "Alices-MacBook", "hostname": "alices-macbook"},
	}
}

func TestAccessRequestsList_JSON(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"access-requests", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d access requests, want 2", len(result))
	}
}

func TestAccessRequestsList_Alias(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ar", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if len(result) != 2 {
		t.Errorf("got %d access requests, want 2", len(result))
	}
}
```

**Step 2: Write the command file with parent + list subcommand**

Create `internal/cmd/access_requests.go`:

```go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
)

var accessRequestDefaultFields = []string{"accessId", "requestorId", "resourceId", "accessState", "expiry"}

func newAccessRequestsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "access-requests",
		Aliases: []string{"ar"},
		Short:   "Manage JumpCloud access requests",
		Long:    "List, get, create, update, and revoke JumpCloud temporary elevated device privilege requests.",
	}

	cmd.AddCommand(newAccessRequestsListCmd())

	return cmd
}

func newAccessRequestsListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all access requests",
		Long: `List all JumpCloud access requests for temporary elevated device privileges.

Default fields: accessId, requestorId, resourceId, accessState, expiry.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'accessState:eq:granted'     Active grants only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAccessRequestsList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'accessState:eq:granted')")

	return cmd
}

func runAccessRequestsList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/accessrequests", api.V2ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = accessRequestDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}
```

**Step 3: Register command in root.go**

In `internal/cmd/root.go`:
- Add `rootCmd.AddCommand(newAccessRequestsCmd())` after line 126 (after `newSaaSManagementCmd()`)
- Add `"access-requests": true,` and `"ar": true,` to `builtinCommands` map

**Step 4: Run the list test**

Run: `go test ./internal/cmd/ -run TestAccessRequestsList -count=1 -v`
Expected: PASS (both `_JSON` and `_Alias`)

**Step 5: Commit**

```bash
git add internal/cmd/access_requests.go internal/cmd/access_requests_test.go internal/cmd/root.go
git commit -m "feat(access-requests): add list subcommand with mock server and tests"
```

---

### Task 2: Add get subcommand

**Files:**
- Modify: `internal/cmd/access_requests.go`
- Modify: `internal/cmd/access_requests_test.go`

**Step 1: Write the get tests**

Add to `access_requests_test.go`:

```go
func TestAccessRequestsGet_ByID(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"access-requests", "get", "aabbccddee112233aabb0001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["accessState"] != "granted" {
		t.Errorf("accessState = %q, want 'granted'", result["accessState"])
	}
}

func TestAccessRequestsGet_NotFound(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"access-requests", "get", "aabbccddee112233aabb9999"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for not-found access request, got nil")
	}
}
```

**Step 2: Implement the get subcommand**

Add to `access_requests.go`:

```go
func newAccessRequestsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <access-id>",
		Short: "Get an access request by ID",
		Long: `Get a single JumpCloud access request by its access ID.

Accepts the 24-character hex access ID returned when creating a request.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAccessRequestsGet(cmd, args[0])
		},
	}

	return cmd
}

func runAccessRequestsGet(cmd *cobra.Command, id string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/accessrequests/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}
```

Wire it: add `cmd.AddCommand(newAccessRequestsGetCmd())` in `newAccessRequestsCmd()`.

**Step 3: Run tests**

Run: `go test ./internal/cmd/ -run TestAccessRequestsGet -count=1 -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/cmd/access_requests.go internal/cmd/access_requests_test.go
git commit -m "feat(access-requests): add get subcommand"
```

---

### Task 3: Add create subcommand with user/device resolution

**Files:**
- Modify: `internal/cmd/access_requests.go`
- Modify: `internal/cmd/access_requests_test.go`

**Step 1: Write the create tests**

Add to `access_requests_test.go`:

```go
func TestAccessRequestsCreate(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	users := sampleUsersForAR()
	devices := sampleDevicesForAR()
	ts := startAccessRequestsServer(t, reqs, users, devices)
	defer ts.Close()
	overrideV2Client(t, ts.URL)
	overrideV1Client(t, ts.URL)

	dir := t.TempDir()
	t.Setenv("JC_CONFIG", dir)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"access-requests", "create",
		"--user", "aabbccddee112233aabb1001",
		"--device", "aabbccddee112233aabb2001",
		"--expiry", "2026-03-01T00:00:00Z",
		"--sudo",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["accessId"] != "acc123acc123acc123acc123" {
		t.Errorf("accessId = %q, want 'acc123acc123acc123acc123'", result["accessId"])
	}
	if result["requestorId"] != "aabbccddee112233aabb1001" {
		t.Errorf("requestorId = %q, want user ID", result["requestorId"])
	}
}

func TestAccessRequestsCreate_Plan(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"access-requests", "create",
		"--user", "aabbccddee112233aabb1001",
		"--device", "aabbccddee112233aabb2001",
		"--expiry", "2026-03-01T00:00:00Z",
		"--plan",
	})

	err := cmd.Execute()
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
```

**Step 2: Implement the create subcommand**

Add imports to `access_requests.go`:

```go
import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
)
```

Add the resolver helpers and create command:

```go
func resolveUser(ctx context.Context, identifier string) (string, error) {
	client, err := newV1Client()
	if err != nil {
		return "", err
	}
	r := resolve.NewResolver(client)
	return r.Resolve(ctx, identifier, resolve.UserConfig)
}

func resolveDevice(ctx context.Context, identifier string) (string, error) {
	client, err := newV1Client()
	if err != nil {
		return "", err
	}
	r := resolve.NewResolver(client)
	return r.Resolve(ctx, identifier, resolve.DeviceConfig)
}

func newAccessRequestsCreateCmd() *cobra.Command {
	var (
		userFlag        string
		deviceFlag      string
		expiryFlag      string
		sudoFlag        bool
		sudoNoPasswd    bool
		remarksFlag     string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new access request",
		Long: `Create a temporary elevated device privilege request.

Grants the specified user temporary admin/sudo access on the target device.
The access will be automatically revoked at the expiry time.

Required flags: --user, --device, --expiry.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAccessRequestsCreate(cmd, userFlag, deviceFlag, expiryFlag, sudoFlag, sudoNoPasswd, remarksFlag)
		},
	}

	cmd.Flags().StringVar(&userFlag, "user", "", "User name or ID (required)")
	cmd.Flags().StringVar(&deviceFlag, "device", "", "Device name or ID (required)")
	cmd.Flags().StringVar(&expiryFlag, "expiry", "", "Expiry time in RFC 3339 format (required)")
	cmd.Flags().BoolVar(&sudoFlag, "sudo", false, "Enable sudo access")
	cmd.Flags().BoolVar(&sudoNoPasswd, "sudo-nopasswd", false, "Enable passwordless sudo")
	cmd.Flags().StringVar(&remarksFlag, "remarks", "", "Optional remarks")
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("device")
	_ = cmd.MarkFlagRequired("expiry")

	return cmd
}

func runAccessRequestsCreate(cmd *cobra.Command, user, device, expiry string, sudo, sudoNoPasswd bool, remarks string) error {
	if viper.GetBool("plan") {
		effects := []string{
			"user: " + user,
			"device: " + device,
			"expiry: " + expiry,
		}
		if sudo {
			effects = append(effects, "sudo: enabled")
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "access request",
			Target:     user + " → " + device,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	userID, err := resolveUser(cmd.Context(), user)
	if err != nil {
		return fmt.Errorf("resolving user: %w", err)
	}

	deviceID, err := resolveDevice(cmd.Context(), device)
	if err != nil {
		return fmt.Errorf("resolving device: %w", err)
	}

	body := map[string]any{
		"requestorId":  userID,
		"resourceId":   deviceID,
		"resourceType": "device",
		"expiry":       expiry,
	}
	if remarks != "" {
		body["remarks"] = remarks
	}
	if sudo || sudoNoPasswd {
		body["additionalAttributes"] = map[string]any{
			"sudo": map[string]any{
				"enabled":         sudo || sudoNoPasswd,
				"withoutPassword": sudoNoPasswd,
			},
		}
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.Create(cmd.Context(), "/accessrequests", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}
```

Wire it: add `cmd.AddCommand(newAccessRequestsCreateCmd())` in `newAccessRequestsCmd()`.

**Step 3: Run tests**

Run: `go test ./internal/cmd/ -run TestAccessRequestsCreate -count=1 -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/cmd/access_requests.go internal/cmd/access_requests_test.go
git commit -m "feat(access-requests): add create subcommand with user/device resolution"
```

---

### Task 4: Add update subcommand

**Files:**
- Modify: `internal/cmd/access_requests.go`
- Modify: `internal/cmd/access_requests_test.go`

**Step 1: Write the update tests**

Add to `access_requests_test.go`:

```go
func TestAccessRequestsUpdate(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"access-requests", "update", "aabbccddee112233aabb0001", "--expiry", "2026-04-01T00:00:00Z"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v\nOutput: %s", err, buf.String())
	}

	if result["expiry"] != "2026-04-01T00:00:00Z" {
		t.Errorf("expiry = %q, want '2026-04-01T00:00:00Z'", result["expiry"])
	}
}

func TestAccessRequestsUpdate_NoFields(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"access-requests", "update", "aabbccddee112233aabb0001"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for no fields, got nil")
	}
}

func TestAccessRequestsUpdate_Plan(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"access-requests", "update", "aabbccddee112233aabb0001", "--expiry", "2026-04-01T00:00:00Z", "--plan"})

	err := cmd.Execute()
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
```

**Step 2: Implement the update subcommand**

Add to `access_requests.go`:

```go
func newAccessRequestsUpdateCmd() *cobra.Command {
	var (
		expiryFlag  string
		remarksFlag string
	)

	cmd := &cobra.Command{
		Use:   "update <access-id>",
		Short: "Update an access request",
		Long: `Update an existing JumpCloud access request.

Accepts the access ID. Specify only the fields you want to change.
Common use case: extending the expiry time.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAccessRequestsUpdate(cmd, args[0], expiryFlag, remarksFlag)
		},
	}

	cmd.Flags().StringVar(&expiryFlag, "expiry", "", "New expiry time in RFC 3339 format")
	cmd.Flags().StringVar(&remarksFlag, "remarks", "", "New remarks")

	return cmd
}

func runAccessRequestsUpdate(cmd *cobra.Command, id, expiry, remarks string) error {
	body := map[string]any{}

	if cmd.Flags().Changed("expiry") {
		body["expiry"] = expiry
	}
	if cmd.Flags().Changed("remarks") {
		body["remarks"] = remarks
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --expiry, --remarks)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "access request",
			Target:     id,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/accessrequests/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}
```

Wire it: add `cmd.AddCommand(newAccessRequestsUpdateCmd())` in `newAccessRequestsCmd()`.

**Step 3: Run tests**

Run: `go test ./internal/cmd/ -run TestAccessRequestsUpdate -count=1 -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/cmd/access_requests.go internal/cmd/access_requests_test.go
git commit -m "feat(access-requests): add update subcommand"
```

---

### Task 5: Add revoke subcommand

**Files:**
- Modify: `internal/cmd/access_requests.go`
- Modify: `internal/cmd/access_requests_test.go`

**Step 1: Write the revoke tests**

Add to `access_requests_test.go`:

```go
func overrideARConfirmReader(t *testing.T, input string) {
	t.Helper()
	orig := confirmReader
	confirmReader = bufio.NewReader(strings.NewReader(input))
	t.Cleanup(func() { confirmReader = orig })
}

func TestAccessRequestsRevoke(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"access-requests", "revoke", "aabbccddee112233aabb0001", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "revoked") {
		t.Errorf("output does not contain 'revoked': %s", out)
	}
}

func TestAccessRequestsRevoke_Cancelled(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)
	overrideARConfirmReader(t, "n\n")

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"access-requests", "revoke", "aabbccddee112233aabb0001"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}

func TestAccessRequestsRevoke_Plan(t *testing.T) {
	setupUsersTest(t)
	reqs := sampleAccessRequests()
	ts := startAccessRequestsServer(t, reqs, nil, nil)
	defer ts.Close()
	overrideV2Client(t, ts.URL)

	cmd := NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"access-requests", "revoke", "aabbccddee112233aabb0001", "--plan"})

	err := cmd.Execute()
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
```

**Step 2: Implement the revoke subcommand**

Add to `access_requests.go`:

```go
func newAccessRequestsRevokeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <access-id>",
		Short: "Revoke an access request",
		Long: `Revoke a JumpCloud access request, removing temporary elevated privileges.

Accepts the access ID. Prompts for confirmation unless --force is set.
This triggers early revocation — access is normally revoked automatically at expiry.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAccessRequestsRevoke(cmd, args[0])
		},
	}

	return cmd
}

func runAccessRequestsRevoke(cmd *cobra.Command, id string) error {
	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "revoke",
			Resource: "access request",
			Target:   id,
			Effects:  []string{"Remove temporary elevated privileges"},
		}
		return renderPlan(cmd, p)
	}

	if !viper.GetBool("force") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Revoke access request %q? [y/N] ", id)
		reader := getConfirmReader()
		answer, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading confirmation: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
			return nil
		}
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	_, err = client.Create(cmd.Context(), "/accessrequests/"+id+"/revoke", map[string]any{})
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Access request %q revoked successfully.\n", id)
	return nil
}
```

Add `"strings"` to imports. Wire it: add `cmd.AddCommand(newAccessRequestsRevokeCmd())` in `newAccessRequestsCmd()`.

**Step 3: Run tests**

Run: `go test ./internal/cmd/ -run TestAccessRequestsRevoke -count=1 -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/cmd/access_requests.go internal/cmd/access_requests_test.go
git commit -m "feat(access-requests): add revoke subcommand with confirmation"
```

---

### Task 6: Add schema resource entry

**Files:**
- Modify: `internal/schema/schema.go`
- Modify: `internal/schema/schema_test.go`
- Modify: `internal/cmd/schema_test.go`

**Step 1: Add schema entry**

In `internal/schema/schema.go`, add to the `Resources` map (alphabetically — near the top, after "ad" or before "admins"):

```go
"access-requests": {
	Resource:      "access-requests",
	APIVersion:    "v2",
	Verbs:         []string{"list", "get", "create", "update", "revoke"},
	DefaultFields: []string{"accessId", "requestorId", "resourceId", "accessState", "expiry"},
	Fields: []FieldDef{
		{Name: "accessId", Type: "string", Description: "Unique access request identifier", ReadOnly: true},
		{Name: "requestorId", Type: "string", Description: "User ID who requested access"},
		{Name: "resourceId", Type: "string", Description: "Device ID for elevated access"},
		{Name: "resourceType", Type: "string", Description: "Resource type (device)"},
		{Name: "accessState", Type: "string", Description: "Request state (granted, revoked, expired)", ReadOnly: true},
		{Name: "expiry", Type: "datetime", Description: "When the elevated access expires"},
		{Name: "remarks", Type: "string", Description: "Optional remarks"},
		{Name: "additionalAttributes", Type: "object", Description: "Additional attributes (sudo settings)"},
		{Name: "operationId", Type: "string", Description: "Operation identifier", ReadOnly: true},
		{Name: "createdBy", Type: "string", Description: "Creator identifier", ReadOnly: true},
	},
	FilterSupport: true,
	SortSupport:   false,
	IDField:       "accessId",
	NameField:     "",
},
```

Add to `BuildCommandManifest()` (alphabetically):

```go
{
	Path:        "jc access-requests",
	Description: "Manage JumpCloud temporary elevated device privilege requests",
	Subcommands: []string{"list", "get", "create", "update", "revoke"},
	Flags: []FlagEntry{
		{Name: "limit", Type: "int", Description: "Maximum number of results (list)"},
		{Name: "filter", Type: "string[]", Description: "Filter expressions (list)"},
		{Name: "user", Type: "string", Description: "User name or ID (create)"},
		{Name: "device", Type: "string", Description: "Device name or ID (create)"},
		{Name: "expiry", Type: "string", Description: "Expiry time RFC 3339 (create/update)"},
		{Name: "sudo", Type: "bool", Description: "Enable sudo access (create)"},
		{Name: "sudo-nopasswd", Type: "bool", Description: "Enable passwordless sudo (create)"},
		{Name: "remarks", Type: "string", Description: "Optional remarks (create/update)"},
	},
},
```

**Step 2: Update test counts**

In `internal/schema/schema_test.go`: change `28` → `29` (appears at line 9 and line ~147, two places).

In `internal/cmd/schema_test.go`: change `28` → `29` (line 35).

**Step 3: Run schema tests**

Run: `go test ./internal/schema/ -count=1 -v && go test ./internal/cmd/ -run TestSchema -count=1 -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/schema/schema.go internal/schema/schema_test.go internal/cmd/schema_test.go
git commit -m "feat(access-requests): add schema resource entry (29 resources)"
```

---

### Task 7: Register MCP tools

**Files:**
- Modify: `internal/mcp/tools.go`
- Modify: `internal/mcp/tools_test.go`

**Step 1: Add input types**

Near line ~188 in `tools.go` (after other input type definitions), add:

```go
type accessRequestCreateInput struct {
	User          string `json:"user" jsonschema:"User name or ID"`
	Device        string `json:"device" jsonschema:"Device name or ID"`
	Expiry        string `json:"expiry" jsonschema:"Expiry time in RFC 3339 format"`
	Sudo          bool   `json:"sudo,omitempty" jsonschema:"Enable sudo access"`
	SudoNoPasswd  bool   `json:"sudo_nopasswd,omitempty" jsonschema:"Enable passwordless sudo"`
	Remarks       string `json:"remarks,omitempty" jsonschema:"Optional remarks"`
}

type accessRequestUpdateInput struct {
	Identifier string `json:"identifier" jsonschema:"Access request ID"`
	Expiry     string `json:"expiry,omitempty" jsonschema:"New expiry time in RFC 3339 format"`
	Remarks    string `json:"remarks,omitempty" jsonschema:"New remarks"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}

type accessRequestRevokeInput struct {
	Identifier string `json:"identifier" jsonschema:"Access request ID to revoke"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to execute. Without this the tool returns a plan."`
}
```

**Step 2: Add registration function**

Add after `registerSaaSManagementTools()` (after line ~5417):

```go
func (s *Server) registerAccessRequestsTools() {
	addTypedTool(s, "access_requests_list", "List all JumpCloud access requests for temporary elevated device privileges. Returns objects with accessId, requestorId, resourceId, accessState, expiry.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			exprs, err := filter.ParseAll(args.Filter)
			if err != nil {
				return errorResult(fmt.Sprintf("parsing filters: %v", err)), nil, nil
			}
			result, err := client.ListAll(ctx, "/accessrequests", api.V2ListOptions{
				Limit:  args.Limit,
				Sort:   args.Sort,
				Filter: filter.ToV2Queries(exprs),
			})
			if err != nil {
				return errorResult(fmt.Sprintf("listing access requests: %v", err)), nil, nil
			}
			return rawListResult(result.Data, len(result.Data))
		},
	)

	addTypedTool(s, "access_requests_get", "Get a single JumpCloud access request by its access ID.",
		func(ctx context.Context, req *mcp.CallToolRequest, args getInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Get(ctx, "/accessrequests/"+args.Identifier)
			if err != nil {
				return errorResult(fmt.Sprintf("getting access request: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "access_requests_create", "Create a JumpCloud access request for temporary elevated device privileges.",
		func(ctx context.Context, req *mcp.CallToolRequest, args accessRequestCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			v1client, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating V1 API client: %v", err)), nil, nil
			}
			userR := resolve.NewResolver(v1client)
			userID, err := userR.Resolve(ctx, args.User, resolve.UserConfig)
			if err != nil {
				return errorResult(fmt.Sprintf("resolving user: %v", err)), nil, nil
			}
			deviceR := resolve.NewResolver(v1client)
			deviceID, err := deviceR.Resolve(ctx, args.Device, resolve.DeviceConfig)
			if err != nil {
				return errorResult(fmt.Sprintf("resolving device: %v", err)), nil, nil
			}
			body := map[string]any{
				"requestorId":  userID,
				"resourceId":   deviceID,
				"resourceType": "device",
				"expiry":       args.Expiry,
			}
			if args.Remarks != "" {
				body["remarks"] = args.Remarks
			}
			if args.Sudo || args.SudoNoPasswd {
				body["additionalAttributes"] = map[string]any{
					"sudo": map[string]any{
						"enabled":         args.Sudo || args.SudoNoPasswd,
						"withoutPassword": args.SudoNoPasswd,
					},
				}
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating V2 API client: %v", err)), nil, nil
			}
			data, err := client.Create(ctx, "/accessrequests", body)
			if err != nil {
				return errorResult(fmt.Sprintf("creating access request: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "access_requests_update", "Update a JumpCloud access request (e.g. extend expiry). Set execute=true to apply changes; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args accessRequestUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body := map[string]any{}
			if args.Expiry != "" {
				body["expiry"] = args.Expiry
			}
			if args.Remarks != "" {
				body["remarks"] = args.Remarks
			}
			if len(body) == 0 {
				return errorResult("no fields to update"), nil, nil
			}
			if !args.Execute {
				effects := make([]string, 0, len(body))
				for k, v := range body {
					effects = append(effects, fmt.Sprintf("%s: %v", k, v))
				}
				return planResult("update", "access request", args.Identifier, args.Identifier, effects)
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			data, err := client.Update(ctx, "/accessrequests/"+args.Identifier, body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating access request: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "access_requests_revoke", "Revoke a JumpCloud access request, removing temporary elevated privileges. Set execute=true to revoke; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args accessRequestRevokeInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			if !args.Execute {
				return planResult("revoke", "access request", args.Identifier, args.Identifier, []string{"Remove temporary elevated privileges"})
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			_, err = client.Create(ctx, "/accessrequests/"+args.Identifier+"/revoke", map[string]any{})
			if err != nil {
				return errorResult(fmt.Sprintf("revoking access request: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Access request %q revoked successfully.", args.Identifier)), nil, nil
		},
	)
}
```

**Step 3: Wire registration call**

In `registerTools()`, after `s.registerSaaSManagementTools()` (line 605), add:

```go
// --- Access Requests tools ---
s.registerAccessRequestsTools()
```

**Step 4: Update tools_test.go**

- Update expected tool count: `189` → `194` (line 388)
- Add 5 expected tool names to the `expectedTools` slice: `access_requests_list`, `access_requests_get`, `access_requests_create`, `access_requests_update`, `access_requests_revoke`

**Step 5: Run MCP tests**

Run: `go test ./internal/mcp/ -count=1 -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/tools_test.go
git commit -m "feat(access-requests): register 5 MCP tools (194 total)"
```

---

### Task 8: Promote TUI placeholder to active entry

**Files:**
- Modify: `internal/tui/registry.go`

**Step 1: Remove placeholder and add active entries**

In `internal/tui/registry.go`:

1. Remove from `placeholderEntries`:
   ```go
   {Key: "access-requests", DisplayName: "Access Requests", Category: CategoryAccess, Placeholder: true},
   ```

2. Add to `resourceCategories` map:
   ```go
   "access-requests": CategoryAccess,
   ```

3. Add to `displayNames` map:
   ```go
   "access-requests": "Access Requests",
   ```

4. Add to `listEndpoints` map:
   ```go
   "access-requests": "/accessrequests",
   ```

**Step 2: Run TUI tests**

Run: `go test ./internal/tui/ -count=1 -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/tui/registry.go
git commit -m "feat(access-requests): promote TUI from placeholder to active entry"
```

---

### Task 9: Run full test suite and fix any issues

**Step 1: Run all tests**

Run: `make test`
Expected: PASS (all packages)

**Step 2: Run linter**

Run: `make lint`
Expected: No issues

**Step 3: Build**

Run: `make build`
Expected: `./jc` binary built successfully

**Step 4: Verify CLI**

Run: `./jc access-requests --help`
Expected: Shows list, get, create, update, revoke subcommands

Run: `./jc ar --help`
Expected: Same output (alias works)

**Step 5: Commit any fixes**

If anything needed fixing, commit with:
```bash
git commit -m "fix(access-requests): fix issues found in full test suite"
```
