# Tier 1 API Coverage Gaps — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Close the 5 highest-value API coverage gaps by extending existing resource commands with missing CRUD operations.

**Architecture:** Each gap is a self-contained extension of an existing `.go` file. All follow established patterns (iplists.go for V2 CRUD, users.go for V1 CRUD+search, graph.go for graph operations). No new files — only modifications.

**Tech Stack:** Go, Cobra, Viper, httptest, `internal/api` V1/V2 clients, `internal/resolve`, `internal/output`, `internal/plan`

---

### Task 1: Policies — create command

**Files:**
- Modify: `internal/cmd/policies.go` — add `newPoliciesCreateCmd()` + `runPoliciesCreate()`
- Modify: `internal/cmd/policies_test.go` — add test server create handler + tests

**Step 1: Extend test server and write failing tests**

In `policies_test.go`, update the test server to handle POST (create) and PUT/DELETE for `/policies/{id}`, then add create tests. The test server pattern follows `startIPListsServer` exactly.

Create or extend `startPoliciesServer` to handle:
- `GET /policies` — return policies array (V2 bare array)
- `POST /policies` — read body, assign ID, return 201
- `GET /policies/{id}` — return matching policy
- `PUT /policies/{id}` — merge body into existing, return
- `DELETE /policies/{id}` — return 200

Sample policies fixture:
```go
func samplePolicies() []map[string]any {
	return []map[string]any{
		{
			"id":       "aabbccddee112233aabb3001",
			"name":     "Disk Encryption",
			"template": map[string]any{"id": "tmpl001", "name": "disk_encryption"},
			"os":       "darwin",
		},
		{
			"id":       "aabbccddee112233aabb3002",
			"name":     "Screen Lock",
			"template": map[string]any{"id": "tmpl002", "name": "screen_lock"},
			"os":       "windows",
		},
	}
}
```

Tests to add:
```go
func TestPoliciesCreate(t *testing.T)            // --name "Test Policy" --template-id tmpl001 → returns created object
func TestPoliciesCreate_Plan(t *testing.T)       // --plan → ExitError{Code:10}
func TestPoliciesCreate_MissingName(t *testing.T) // missing --name → error
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cmd/ -run TestPoliciesCreate -count=1 -v`
Expected: FAIL (functions not defined)

**Step 3: Implement create command**

In `policies.go`, add the parent command registration and implementation:

```go
// In newPoliciesCmd(), add:
cmd.AddCommand(newPoliciesCreateCmd())

func newPoliciesCreateCmd() *cobra.Command {
	var (
		name       string
		templateID string
		values     string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new policy",
		Long: `Create a new JumpCloud policy.

Required fields: --name, --template-id.
Use --values to pass template-specific configuration as a JSON string.
The newly created policy object is returned.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPoliciesCreate(cmd, name, templateID, values)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Policy name (required)")
	cmd.Flags().StringVar(&templateID, "template-id", "", "Policy template ID (required)")
	cmd.Flags().StringVar(&values, "values", "", "Template-specific config as JSON")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("template-id")
	return cmd
}

func runPoliciesCreate(cmd *cobra.Command, name, templateID, values string) error {
	if viper.GetBool("plan") {
		effects := []string{"name: " + name, "template: " + templateID}
		if values != "" {
			effects = append(effects, "values: (custom JSON)")
		}
		p := &plan.Plan{
			Action:   "create", Resource: "policy", Target: name,
			Effects: effects, Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":     name,
		"template": map[string]any{"id": templateID},
	}
	if values != "" {
		var v map[string]any
		if err := json.Unmarshal([]byte(values), &v); err != nil {
			return fmt.Errorf("invalid --values JSON: %w", err)
		}
		body["values"] = v
	}

	result, err := client.Create(cmd.Context(), "/policies", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/cmd/ -run TestPoliciesCreate -count=1 -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cmd/policies.go internal/cmd/policies_test.go
git commit -m "feat(policies): add create command"
```

---

### Task 2: Policies — update command

**Files:**
- Modify: `internal/cmd/policies.go` — add `newPoliciesUpdateCmd()` + `runPoliciesUpdate()`
- Modify: `internal/cmd/policies_test.go` — add update tests

**Step 1: Write failing tests**

```go
func TestPoliciesUpdate(t *testing.T)         // update by ID with --name → returns updated
func TestPoliciesUpdate_NoFields(t *testing.T) // no flags → "no fields to update" error
func TestPoliciesUpdate_Plan(t *testing.T)     // --plan → ExitError{Code:10}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cmd/ -run TestPoliciesUpdate -count=1 -v`

**Step 3: Implement update command**

```go
// In newPoliciesCmd(), add:
cmd.AddCommand(newPoliciesUpdateCmd())

func newPoliciesUpdateCmd() *cobra.Command {
	var (
		name   string
		values string
	)
	cmd := &cobra.Command{
		Use:   "update <policy-name-or-id>",
		Short: "Update a policy",
		Long: `Update an existing JumpCloud policy.

Accepts a policy name or 24-character hex ID.
Specify only the fields you want to change. The updated policy object is returned.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPoliciesUpdate(cmd, args[0], name, values)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Policy name")
	cmd.Flags().StringVar(&values, "values", "", "Template-specific config as JSON")
	return cmd
}

func runPoliciesUpdate(cmd *cobra.Command, identifier, name, values string) error {
	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		body["name"] = name
	}
	if cmd.Flags().Changed("values") {
		var v map[string]any
		if err := json.Unmarshal([]byte(values), &v); err != nil {
			return fmt.Errorf("invalid --values JSON: %w", err)
		}
		body["values"] = v
	}
	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --name, --values)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action: "update", Resource: "policy", Target: identifier,
			Effects: effects, Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolvePolicy(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	result, err := client.Update(cmd.Context(), "/policies/"+id, body)
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}
```

**Step 4: Run tests, verify pass**

**Step 5: Commit**

```bash
git add internal/cmd/policies.go internal/cmd/policies_test.go
git commit -m "feat(policies): add update command"
```

---

### Task 3: Policies — delete command

**Files:**
- Modify: `internal/cmd/policies.go` — add `newPoliciesDeleteCmd()` + `runPoliciesDelete()`
- Modify: `internal/cmd/policies_test.go` — add delete tests
- Modify: `internal/schema/schema.go` — update policies verbs

**Step 1: Write failing tests**

```go
func TestPoliciesDelete_Force(t *testing.T)    // --force → "deleted successfully"
func TestPoliciesDelete_Plan(t *testing.T)     // --plan → ExitError{Code:10}
func TestPoliciesDelete_NotFound(t *testing.T) // unknown → error
func TestPoliciesDelete_MissingArg(t *testing.T) // no arg → error
```

**Step 2: Run tests to verify they fail**

**Step 3: Implement delete command**

Follow the exact pattern from `runIPListsDelete` — fetch first to show name, plan mode, confirmation prompt, then delete.

```go
// In newPoliciesCmd(), add:
cmd.AddCommand(newPoliciesDeleteCmd())
```

The delete implementation follows `iplists.go` delete pattern exactly: resolve → get → plan check → confirm → delete → success message.

**Step 4: Update schema verbs**

In `internal/schema/schema.go`, update the policies entry:
```go
Verbs: []string{"list", "get", "create", "update", "delete", "results"},
```

**Step 5: Run full policies tests + schema tests**

Run: `go test ./internal/cmd/ -run TestPolicies -count=1 -v`
Run: `go test ./internal/schema/ -count=1 -v`

**Step 6: Commit**

```bash
git add internal/cmd/policies.go internal/cmd/policies_test.go internal/schema/schema.go
git commit -m "feat(policies): add delete command, update schema verbs"
```

---

### Task 4: Graph — bind command

**Files:**
- Modify: `internal/cmd/graph.go` — add `newGraphBindCmd()` + `runGraphBind()`
- Modify: `internal/cmd/graph_test.go` — extend test server + add bind tests

**Step 1: Extend test server and write failing tests**

Extend `startGraphServer` to also handle POST to `/{resource}/{id}/associations`. The POST body format is `{"op":"add","type":"<type>","id":"<id>"}`.

The `--to` flag for bind/unbind needs to be `type:name-or-id` (not just a type like traverse), since we need to resolve both sides.

Tests:
```go
func TestGraphBind_UserToApplication(t *testing.T) // --from user:jdoe --to application:Slack → success
func TestGraphBind_Plan(t *testing.T)               // --plan → ExitError{Code:10}
func TestGraphBind_InvalidFrom(t *testing.T)        // bad format → error
func TestGraphBind_InvalidTarget(t *testing.T)      // invalid combo → error
```

**Step 2: Run tests to verify they fail**

**Step 3: Implement bind command**

Key design: `--to` takes `type:name-or-id` format (same as `--from`). We parse both, resolve both IDs, validate the source→target type combo, then POST to the source's associations endpoint.

```go
func newGraphBindCmd() *cobra.Command {
	var fromFlag, toFlag string
	cmd := &cobra.Command{
		Use:   "bind --from <type>:<name-or-id> --to <type>:<name-or-id>",
		Short: "Create an association between resources",
		Long: `Create a graph association between two JumpCloud resources.

Both --from and --to use the format type:name-or-id.
The same source/target validation as 'traverse' applies.

Examples:
  jc graph bind --from user_group:Engineering --to application:Slack
  jc graph bind --from device_group:Servers --to policy:DiskEncryption`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGraphBind(cmd, fromFlag, toFlag)
		},
	}
	cmd.Flags().StringVar(&fromFlag, "from", "", "Source resource as type:name-or-id")
	cmd.Flags().StringVar(&toFlag, "to", "", "Target resource as type:name-or-id")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}
```

The `runGraphBind` implementation:
1. Parse `--from` with `parseFromFlag()`
2. Parse `--to` with `parseFromFlag()` (reuse same parser — both are `type:identifier`)
3. Validate source→target type combo with `isValidTargetType()`
4. Resolve both identifiers to IDs
5. Map target type alias (device→system, device_group→system_group)
6. Plan mode check
7. POST to `/{source_endpoint}/{source_id}/associations` with body `{"op":"add","type":"<api_target>","id":"<target_id>"}`
8. Print success message

**Step 4: Run tests, verify pass**

**Step 5: Commit**

```bash
git add internal/cmd/graph.go internal/cmd/graph_test.go
git commit -m "feat(graph): add bind command for creating associations"
```

---

### Task 5: Graph — unbind command

**Files:**
- Modify: `internal/cmd/graph.go` — add `newGraphUnbindCmd()` (reuses `runGraphManage` with `op:"remove"`)
- Modify: `internal/cmd/graph_test.go` — add unbind tests

**Step 1: Write failing tests**

```go
func TestGraphUnbind_UserFromApplication(t *testing.T) // → success
func TestGraphUnbind_Plan(t *testing.T)                 // → ExitError{Code:10}
```

**Step 2: Run tests to verify they fail**

**Step 3: Implement unbind command**

Refactor: extract shared logic from `runGraphBind` into `runGraphManage(cmd, from, to, op string)` where `op` is `"add"` or `"remove"`. Both `bind` and `unbind` call this with different ops.

`unbind` additionally shows a confirmation prompt (unless `--force`) before removing.

```go
func newGraphUnbindCmd() *cobra.Command {
	var fromFlag, toFlag string
	cmd := &cobra.Command{
		Use:   "unbind --from <type>:<name-or-id> --to <type>:<name-or-id>",
		Short: "Remove an association between resources",
		Long: `Remove a graph association between two JumpCloud resources.

Both --from and --to use the format type:name-or-id.
Shows a confirmation prompt before removing. Use --force to skip.

Examples:
  jc graph unbind --from user_group:Engineering --to application:Slack`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGraphManage(cmd, fromFlag, toFlag, "remove")
		},
	}
	cmd.Flags().StringVar(&fromFlag, "from", "", "Source resource as type:name-or-id")
	cmd.Flags().StringVar(&toFlag, "to", "", "Target resource as type:name-or-id")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}
```

Register both in `newGraphCmd()`:
```go
cmd.AddCommand(newGraphBindCmd())
cmd.AddCommand(newGraphUnbindCmd())
```

**Step 4: Run tests, verify pass**

**Step 5: Commit**

```bash
git add internal/cmd/graph.go internal/cmd/graph_test.go
git commit -m "feat(graph): add unbind command, refactor shared manage logic"
```

---

### Task 6: Admins — get/create/update/delete

**Files:**
- Modify: `internal/cmd/admins.go` — add 4 new commands
- Modify: `internal/cmd/admins_test.go` — extend server + add tests
- Modify: `internal/resolve/resolve.go` — add `AdminConfig`
- Modify: `internal/cmd/cli_error.go` — add `ErrCodeAdminNotFound`

**Step 1: Add AdminConfig to resolve**

In `internal/resolve/resolve.go`:
```go
// AdminConfig is the resolution config for JumpCloud administrators (V1 API).
var AdminConfig = ResourceConfig{
	CacheKey:     "admins",
	ListEndpoint: "/users",
	NameField:    "email",
	IDField:      "_id",
}
```

**Step 2: Add ErrCodeAdminNotFound**

In `internal/cmd/cli_error.go`:
```go
ErrCodeAdminNotFound = "ADMIN_NOT_FOUND"
```

**Step 3: Extend test server**

In `admins_test.go`, extend `startAdminsServer` to handle:
- `GET /users/{id}` — return matching admin
- `POST /users` — create admin (assign ID, return 201)
- `PUT /users/{id}` — merge body, return updated
- `DELETE /users/{id}` — return 200

**Step 4: Write failing tests**

```go
// Get tests
func TestAdminsGet_ByID(t *testing.T)        // by ID → returns admin
func TestAdminsGet_ByEmail(t *testing.T)      // by email → resolves and returns
func TestAdminsGet_NotFound(t *testing.T)     // unknown → error

// Create tests
func TestAdminsCreate(t *testing.T)           // --email admin@test.com → returns created
func TestAdminsCreate_Plan(t *testing.T)      // --plan → ExitError
func TestAdminsCreate_MissingEmail(t *testing.T)

// Update tests
func TestAdminsUpdate(t *testing.T)           // --enable-mfa → returns updated
func TestAdminsUpdate_NoFields(t *testing.T)  // no flags → error
func TestAdminsUpdate_Plan(t *testing.T)

// Delete tests
func TestAdminsDelete_Force(t *testing.T)     // --force → success
func TestAdminsDelete_Plan(t *testing.T)      // --plan → ExitError
func TestAdminsDelete_NotFound(t *testing.T)
```

**Step 5: Implement all 4 commands**

Add `resolveAdmin` function and a helper in admins.go:
```go
func resolveAdmin(ctx context.Context, client *api.V1Client, identifier string) (string, error) {
	r := resolve.NewResolver(client)
	return r.Resolve(ctx, identifier, resolve.AdminConfig)
}
```

Then add `get`, `create`, `update`, `delete` following the users.go patterns:
- `get` — resolve by email/ID, then GET `/users/{id}`
- `create` — `--email` required, optional `--role`, `--enable-mfa`; POST `/users`
- `update` — resolve, check changed flags, PUT `/users/{id}`
- `delete` — resolve, fetch for display, plan mode, confirm, DELETE `/users/{id}`

Register all in `newAdminsCmd()`:
```go
cmd.AddCommand(newAdminsGetCmd())
cmd.AddCommand(newAdminsCreateCmd())
cmd.AddCommand(newAdminsUpdateCmd())
cmd.AddCommand(newAdminsDeleteCmd())
```

**Step 6: Update schema verbs**

In `internal/schema/schema.go`:
```go
Verbs: []string{"list", "get", "create", "update", "delete"},
```

**Step 7: Run all tests**

Run: `go test ./internal/cmd/ -run TestAdmins -count=1 -v`
Run: `go test ./internal/resolve/ -count=1 -v`
Run: `go test ./internal/schema/ -count=1 -v`

**Step 8: Commit**

```bash
git add internal/cmd/admins.go internal/cmd/admins_test.go internal/resolve/resolve.go internal/cmd/cli_error.go internal/schema/schema.go
git commit -m "feat(admins): add get, create, update, delete commands"
```

---

### Task 7: Devices — update command

**Files:**
- Modify: `internal/cmd/devices.go` — add `newDevicesUpdateCmd()` + `runDevicesUpdate()`
- Modify: `internal/cmd/devices_test.go` — extend server + add tests

**Step 1: Extend test server**

In `devices_test.go`, add `PUT /systems/{id}` handler to `startDevicesServer` (merge body into existing device, return updated).

**Step 2: Write failing tests**

```go
func TestDevicesUpdate(t *testing.T)          // --displayName → returns updated
func TestDevicesUpdate_NoFields(t *testing.T) // no flags → error
func TestDevicesUpdate_Plan(t *testing.T)     // --plan → ExitError
func TestDevicesUpdate_NotFound(t *testing.T) // bad ID → error
```

**Step 3: Implement update command**

```go
func newDevicesUpdateCmd() *cobra.Command {
	var (
		displayName                    string
		allowSshPasswordAuthentication string
		allowMultiFactorAuthentication string
		allowPublicKeyAuthentication   string
	)
	cmd := &cobra.Command{
		Use:   "update <hostname-or-id>",
		Short: "Update a device",
		Long: `Update a JumpCloud system (device).

Accepts a hostname or 24-character hex system ID.
Specify only the fields you want to change. The updated device object is returned.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevicesUpdate(cmd, args[0], displayName,
				allowSshPasswordAuthentication, allowMultiFactorAuthentication, allowPublicKeyAuthentication)
		},
	}
	cmd.Flags().StringVar(&displayName, "displayName", "", "Display name")
	cmd.Flags().StringVar(&allowSshPasswordAuthentication, "allowSshPasswordAuthentication", "", "Allow SSH password auth (true/false)")
	cmd.Flags().StringVar(&allowMultiFactorAuthentication, "allowMultiFactorAuthentication", "", "Allow MFA (true/false)")
	cmd.Flags().StringVar(&allowPublicKeyAuthentication, "allowPublicKeyAuthentication", "", "Allow public key auth (true/false)")
	return cmd
}
```

Follow the iplists update pattern: check `cmd.Flags().Changed()` for each flag, build body, plan mode, resolve, PUT, output.

Register in `newDevicesCmd()`:
```go
cmd.AddCommand(newDevicesUpdateCmd())
```

**Step 4: Run tests, verify pass**

**Step 5: Commit**

```bash
git add internal/cmd/devices.go internal/cmd/devices_test.go
git commit -m "feat(devices): add update command"
```

---

### Task 8: Devices — search command

**Files:**
- Modify: `internal/cmd/devices.go` — add `newDevicesSearchCmd()` + `runDevicesSearch()`
- Modify: `internal/cmd/devices_test.go` — extend server + add tests
- Modify: `internal/schema/schema.go` — update devices verbs

**Step 1: Extend test server**

Add `POST /search/systems` handler to `startDevicesServer` following the same pattern as `startUsersServer` search handler (case-insensitive substring matching across hostname, displayName, os fields).

**Step 2: Write failing tests**

```go
func TestDevicesSearch_JSON(t *testing.T)     // search "MBP" → matching devices
func TestDevicesSearch_Empty(t *testing.T)    // search "nonexistent" → empty
func TestDevicesSearch_Footer(t *testing.T)   // footer shows count
```

**Step 3: Implement search command**

Mirror `runUsersSearch` exactly, but with `/search/systems` endpoint and device-appropriate search fields: `hostname`, `displayName`, `os`, `serialNumber`.

```go
func newDevicesSearchCmd() *cobra.Command {
	var (
		limitFlag  int
		filterFlag []string
	)
	cmd := &cobra.Command{
		Use:   "search <term>",
		Short: "Search for devices by keyword",
		Long: `Search for JumpCloud devices by keyword across hostname, displayName, os, and serialNumber fields.

Uses the V1 POST /api/search/systems endpoint for case-insensitive searching.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevicesSearch(cmd, args[0], limitFlag, filterFlag)
		},
	}
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results")
	return cmd
}
```

Register in `newDevicesCmd()`:
```go
cmd.AddCommand(newDevicesSearchCmd())
```

**Step 4: Update schema verbs**

In `internal/schema/schema.go`:
```go
Verbs: []string{"list", "get", "search", "update", "delete", "lock", "restart", "erase"},
```

**Step 5: Run all device tests + schema tests**

Run: `go test ./internal/cmd/ -run TestDevices -count=1 -v`

**Step 6: Commit**

```bash
git add internal/cmd/devices.go internal/cmd/devices_test.go internal/schema/schema.go
git commit -m "feat(devices): add search and update commands"
```

---

### Task 9: Apps — create/update/delete

**Files:**
- Modify: `internal/cmd/apps.go` — add 3 new commands
- Modify: `internal/cmd/apps_test.go` — extend server + add tests
- Modify: `internal/schema/schema.go` — update apps verbs

**Step 1: Create or extend test server**

Create `startAppsServer` in `apps_test.go` handling:
- `GET /applications` — list (V1 wrapped response)
- `POST /applications` — create (assign ID, return 201)
- `GET /applications/{id}` — get single
- `PUT /applications/{id}` — update
- `DELETE /applications/{id}` — delete

Also needs V2 endpoints for the existing `get` enrichment (associations). Use a combined V1+V2 handler.

**Step 2: Write failing tests**

```go
// Create
func TestAppsCreate(t *testing.T)              // --name "Test App" --sso-type saml → created
func TestAppsCreate_Plan(t *testing.T)         // --plan → ExitError
func TestAppsCreate_MissingRequired(t *testing.T)

// Update
func TestAppsUpdate(t *testing.T)              // --name "New Name" → updated
func TestAppsUpdate_NoFields(t *testing.T)     // no flags → error
func TestAppsUpdate_Plan(t *testing.T)

// Delete
func TestAppsDelete_Force(t *testing.T)        // --force → success
func TestAppsDelete_Plan(t *testing.T)         // --plan → ExitError
func TestAppsDelete_NotFound(t *testing.T)
```

**Step 3: Implement all 3 commands**

Create follows the V1 pattern (users.go):
```go
func newAppsCreateCmd() *cobra.Command {
	var (
		name    string
		ssoType string
		config  string
	)
	// --name required, --sso-type required, --config optional JSON
}
```

Update follows the iplists pattern: check `Changed()`, build body, plan, resolve, PUT.

Delete follows the iplists pattern: resolve, fetch, plan, confirm, DELETE.

Register all in `newAppsCmd()`:
```go
cmd.AddCommand(newAppsCreateCmd())
cmd.AddCommand(newAppsUpdateCmd())
cmd.AddCommand(newAppsDeleteCmd())
```

**Step 4: Update schema verbs**

In `internal/schema/schema.go`:
```go
Verbs: []string{"list", "get", "create", "update", "delete"},
```

**Step 5: Run all app tests + schema tests**

Run: `go test ./internal/cmd/ -run TestApps -count=1 -v`
Run: `go test ./internal/schema/ -count=1 -v`

**Step 6: Commit**

```bash
git add internal/cmd/apps.go internal/cmd/apps_test.go internal/schema/schema.go
git commit -m "feat(apps): add create, update, delete commands"
```

---

### Task 10: Final verification

**Step 1: Run full test suite**

Run: `make test`
Expected: ALL PASS

**Step 2: Build binary**

Run: `make build`
Expected: Clean build

**Step 3: Verify help output shows new subcommands**

Run: `./jc policies --help` → should list: list, get, create, update, delete, results
Run: `./jc graph --help` → should list: traverse, bind, unbind
Run: `./jc admins --help` → should list: list, get, create, update, delete
Run: `./jc devices --help` → should list: list, get, search, update, delete, lock, restart, erase
Run: `./jc apps --help` → should list: list, get, create, update, delete

**Step 4: Commit any remaining fixes**

**Step 5: Update progress.md**

Add entry documenting Tier 1 API coverage gaps closed.
