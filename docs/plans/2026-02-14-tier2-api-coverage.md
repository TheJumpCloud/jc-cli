# Tier 2 API Coverage — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add CLI commands for Software Management, LDAP Servers, Active Directory, and Organization Settings — 4 new resource command groups extending the JC CLI.

**Architecture:** Each resource gets its own `.go` + `_test.go` file in `internal/cmd/`. All V2 resources follow the `iplists.go` pattern exactly (V2Client, V2Resolver, bare-array responses, Link-header pagination). Organizations follows the V1 read-only pattern. Final task wires everything into root.go, schema.go, cli_error.go, resolve.go, and mcp/tools.go.

**Tech Stack:** Go, Cobra, Viper, httptest, `internal/api` V2Client/V1Client, `internal/resolve`, `internal/output`, `internal/plan`, `internal/filter`

---

### Task 1: Software Management — full V2 CRUD

**Files:**
- Create: `internal/cmd/software.go`
- Create: `internal/cmd/software_test.go`

This is the most complex Tier 2 resource because of the nested `settings` array. Follow `iplists.go` exactly for structure.

**Step 1: Write the test file with mock server and all tests**

Create `internal/cmd/software_test.go`. The mock server handles `/softwareapps` (list, create) and `/softwareapps/{id}` (get, update, delete).

Sample fixture:
```go
func sampleSoftwareApps() []map[string]any {
    return []map[string]any{
        {
            "id":          "aabbccddee112233aabb5001",
            "displayName": "Firefox",
            "settings":    []any{map[string]any{"packageId": "firefox-pkg", "packageManager": "CHOCOLATEY", "desiredState": "INSTALL"}},
            "createdAt":   "2024-01-15T10:00:00Z",
            "updatedAt":   "2024-06-01T12:00:00Z",
        },
        {
            "id":          "aabbccddee112233aabb5002",
            "displayName": "Slack",
            "settings":    []any{map[string]any{"packageId": "slack-pkg", "packageManager": "APPLE_CUSTOM", "desiredState": "INSTALL"}},
            "createdAt":   "2024-02-10T08:00:00Z",
            "updatedAt":   "2024-05-20T15:00:00Z",
        },
    }
}
```

`startSoftwareServer(t, apps)` pattern mirrors `startIPListsServer` exactly:
- `GET /softwareapps` → return bare array
- `POST /softwareapps` → assign ID, return 201
- `GET /softwareapps/{id}` → return matching item
- `PUT /softwareapps/{id}` → merge body, return
- `DELETE /softwareapps/{id}` → return 200

Tests to write (follow iplist test pattern exactly):
```go
func TestSoftwareList_JSON(t *testing.T)         // list returns all apps with default fields
func TestSoftwareList_Limit(t *testing.T)        // --limit 1 returns 1 item
func TestSoftwareList_Filter(t *testing.T)       // --filter 'displayName=Firefox'
func TestSoftwareGet(t *testing.T)               // get by ID → full object
func TestSoftwareGet_ByName(t *testing.T)        // get by displayName → resolves to ID
func TestSoftwareGet_NotFound(t *testing.T)      // bad ID → error
func TestSoftwareCreate(t *testing.T)            // --name "Chrome" --settings '{...}' → created object
func TestSoftwareCreate_Plan(t *testing.T)       // --plan → ExitError{Code:10}
func TestSoftwareCreate_MissingName(t *testing.T) // missing --name → error
func TestSoftwareUpdate(t *testing.T)            // update --name "NewName" → updated object
func TestSoftwareUpdate_Plan(t *testing.T)       // --plan → ExitError{Code:10}
func TestSoftwareUpdate_NoFields(t *testing.T)   // no flags → error
func TestSoftwareDelete(t *testing.T)            // delete --force → success message
func TestSoftwareDelete_Plan(t *testing.T)       // --plan → ExitError{Code:10}
func TestSoftwareDelete_Cancel(t *testing.T)     // answer "n" → cancelled
```

Each test follows this setup:
```go
setupUsersTest(t)
apps := sampleSoftwareApps()
ts := startSoftwareServer(t, apps)
defer ts.Close()
overrideV2Client(t, ts.URL)
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cmd/ -run TestSoftware -count=1 -v`
Expected: FAIL (functions not defined)

**Step 3: Write the implementation**

Create `internal/cmd/software.go` following `iplists.go` exactly:

```go
package cmd

// softwareDefaultFields: id, displayName, createdAt, updatedAt
var softwareDefaultFields = []string{"id", "displayName", "createdAt", "updatedAt"}

// resolveSoftwareApp uses V2Resolver with SoftwareAppConfig
func resolveSoftwareApp(ctx, client, identifier) (string, error)

func newSoftwareCmd() *cobra.Command       // parent: Use "software", Short "Manage JumpCloud software apps"
func newSoftwareListCmd() *cobra.Command    // list with --limit, --sort, --filter
func runSoftwareList(cmd, limit, sort, filters) error  // V2 ListAll → WriteList → footer

func newSoftwareGetCmd() *cobra.Command     // get <name-or-id>
func runSoftwareGet(cmd, identifier) error  // resolve → Get → WriteSingle

func newSoftwareCreateCmd() *cobra.Command  // --name (required), --settings (optional JSON)
func runSoftwareCreate(cmd, name, settings) error  // plan check → Create → WriteSingle

func newSoftwareUpdateCmd() *cobra.Command  // <name-or-id> --name, --settings
func runSoftwareUpdate(cmd, identifier, name, settings) error  // Changed check → plan → resolve → Update → WriteSingle

func newSoftwareDeleteCmd() *cobra.Command  // <name-or-id>, alias "rm"
func runSoftwareDelete(cmd, identifier) error  // resolve → Get(name) → plan/confirm → Delete
```

Key details:
- `--settings` flag accepts raw JSON string (same pattern as `--config` on apps.go)
- Create body: `{"displayName": name, "settings": parsedSettings}` (note: API uses `displayName`, not `name`)
- Update uses `cmd.Flags().Changed()` for each field
- Delete fetches first for name display in confirmation prompt
- V2 endpoint: `/softwareapps`
- Resolver uses `displayName` as NameField

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/cmd/ -run TestSoftware -count=1 -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/cmd/software.go internal/cmd/software_test.go
git commit -m "feat(software): add software management commands (list, get, create, update, delete)"
```

---

### Task 2: LDAP Servers — full V2 CRUD

**Files:**
- Create: `internal/cmd/ldap.go`
- Create: `internal/cmd/ldap_test.go`

Simplest V2 resource — only 4 fields. Exact same pattern as iplists.go.

**Step 1: Write the test file with mock server and all tests**

Create `internal/cmd/ldap_test.go`.

Sample fixture:
```go
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
```

`startLDAPServer(t, servers)` — same pattern as `startIPListsServer`:
- `GET /ldapservers` → bare array
- `POST /ldapservers` → assign ID, return 201
- `GET /ldapservers/{id}` → return matching
- `PUT /ldapservers/{id}` → merge, return
- `DELETE /ldapservers/{id}` → return 200

Tests:
```go
func TestLDAPList_JSON(t *testing.T)
func TestLDAPList_Limit(t *testing.T)
func TestLDAPGet(t *testing.T)
func TestLDAPGet_ByName(t *testing.T)
func TestLDAPGet_NotFound(t *testing.T)
func TestLDAPCreate(t *testing.T)            // --name "new-ldap"
func TestLDAPCreate_Plan(t *testing.T)
func TestLDAPCreate_MissingName(t *testing.T)
func TestLDAPUpdate(t *testing.T)            // --user-lockout-action disable
func TestLDAPUpdate_Plan(t *testing.T)
func TestLDAPUpdate_NoFields(t *testing.T)
func TestLDAPDelete(t *testing.T)
func TestLDAPDelete_Plan(t *testing.T)
func TestLDAPDelete_Cancel(t *testing.T)
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cmd/ -run TestLDAP -count=1 -v`
Expected: FAIL

**Step 3: Write the implementation**

Create `internal/cmd/ldap.go`:

```go
// ldapDefaultFields: id, name, userLockoutAction, userPasswordExpirationAction
var ldapDefaultFields = []string{"id", "name", "userLockoutAction", "userPasswordExpirationAction"}

func resolveLDAP(ctx, client, identifier) // V2Resolver + LDAPServerConfig

func newLDAPCmd() *cobra.Command          // Use "ldap", Short "Manage JumpCloud LDAP servers"
func newLDAPListCmd()                     // --limit, --sort, --filter
func runLDAPList(cmd, limit, sort, filters)

func newLDAPGetCmd()                      // get <name-or-id>
func runLDAPGet(cmd, identifier)

func newLDAPCreateCmd()                   // --name (required), --user-lockout-action, --user-password-expiration-action
func runLDAPCreate(cmd, name, lockout, expiration)

func newLDAPUpdateCmd()                   // <name-or-id> --name, --user-lockout-action, --user-password-expiration-action
func runLDAPUpdate(cmd, identifier, name, lockout, expiration)

func newLDAPDeleteCmd()                   // <name-or-id>, alias "rm"
func runLDAPDelete(cmd, identifier)
```

Key details:
- Create body: `{"name": name}` plus optional lockout/expiration fields
- Flag names use kebab-case: `--user-lockout-action`, `--user-password-expiration-action`
- Body keys use camelCase: `userLockoutAction`, `userPasswordExpirationAction`
- V2 endpoint: `/ldapservers`

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/cmd/ -run TestLDAP -count=1 -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/cmd/ldap.go internal/cmd/ldap_test.go
git commit -m "feat(ldap): add LDAP server management commands (list, get, create, update, delete)"
```

---

### Task 3: Active Directory — full V2 CRUD

**Files:**
- Create: `internal/cmd/ad.go`
- Create: `internal/cmd/ad_test.go`

Uses `domain` as the human-readable identifier instead of `name`.

**Step 1: Write the test file with mock server and all tests**

Create `internal/cmd/ad_test.go`.

Sample fixture:
```go
func sampleADs() []map[string]any {
    return []map[string]any{
        {
            "id":              "aabbccddee112233aabb7001",
            "domain":          "corp.example.com",
            "useCase":         "ADASAUTHORITY",
            "groupsEnabled":   true,
            "delegationState": "ENABLED",
        },
        {
            "id":              "aabbccddee112233aabb7002",
            "domain":          "dev.example.com",
            "useCase":         "ADASAUTHORITY",
            "groupsEnabled":   false,
            "delegationState": "DISABLED",
        },
    }
}
```

`startADServer(t, ads)` — same V2 pattern but using `/activedirectories`.

Tests:
```go
func TestADList_JSON(t *testing.T)
func TestADList_Limit(t *testing.T)
func TestADGet(t *testing.T)
func TestADGet_ByDomain(t *testing.T)        // resolve by domain name
func TestADGet_NotFound(t *testing.T)
func TestADCreate(t *testing.T)              // --domain "new.example.com"
func TestADCreate_Plan(t *testing.T)
func TestADCreate_MissingDomain(t *testing.T)
func TestADUpdate(t *testing.T)              // --use-case "ADASAUTHORITY"
func TestADUpdate_Plan(t *testing.T)
func TestADUpdate_NoFields(t *testing.T)
func TestADDelete(t *testing.T)
func TestADDelete_Plan(t *testing.T)
func TestADDelete_Cancel(t *testing.T)
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cmd/ -run TestAD -count=1 -v`
Expected: FAIL

**Step 3: Write the implementation**

Create `internal/cmd/ad.go`:

```go
// adDefaultFields: id, domain, useCase, groupsEnabled, delegationState
var adDefaultFields = []string{"id", "domain", "useCase", "groupsEnabled", "delegationState"}

func resolveAD(ctx, client, identifier) // V2Resolver + ActiveDirectoryConfig

func newADCmd() *cobra.Command           // Use "ad", Short "Manage JumpCloud Active Directory integrations"
func newADListCmd()                      // --limit, --sort, --filter
func runADList(cmd, limit, sort, filters)

func newADGetCmd()                       // get <domain-or-id>
func runADGet(cmd, identifier)

func newADCreateCmd()                    // --domain (required), --use-case
func runADCreate(cmd, domain, useCase)

func newADUpdateCmd()                    // <domain-or-id> --use-case, --groups-enabled (bool flag)
func runADUpdate(cmd, identifier, useCase string, groupsEnabled bool)

func newADDeleteCmd()                    // <domain-or-id>, alias "rm"
func runADDelete(cmd, identifier)
```

Key details:
- Resolver uses `domain` as NameField (not `name`)
- Create body: `{"domain": domain}` plus optional useCase
- `--groups-enabled` is a bool flag mapped to `groupsEnabled` in body
- Update uses `cmd.Flags().Changed()` for each field
- Delete confirmation shows domain (not name): `Delete AD "corp.example.com"? [y/N]`
- V2 endpoint: `/activedirectories`

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/cmd/ -run TestAD -count=1 -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/cmd/ad.go internal/cmd/ad_test.go
git commit -m "feat(ad): add Active Directory management commands (list, get, create, update, delete)"
```

---

### Task 4: Organizations — V1 read-only

**Files:**
- Create: `internal/cmd/org.go`
- Create: `internal/cmd/org_test.go`

Simplest resource — read-only, V1 API, no resolver needed.

**Step 1: Write the test file with mock server and all tests**

Create `internal/cmd/org_test.go`.

Sample fixture:
```go
func sampleOrgs() []map[string]any {
    return []map[string]any{
        {
            "_id":         "aabbccddee112233aabb8001",
            "id":          "aabbccddee112233aabb8001",
            "displayName": "Klaassen Consulting",
            "created":     "2023-01-01T00:00:00Z",
            "logoUrl":     "",
        },
    }
}
```

`startOrgServer(t, orgs)` — V1 pattern:
- `GET /organizations` → `{"results": orgs, "totalCount": len(orgs)}`
- `GET /organizations/{id}` → return matching org

Tests:
```go
func TestOrgList_JSON(t *testing.T)
func TestOrgGet(t *testing.T)
func TestOrgGet_NotFound(t *testing.T)
```

Note: Uses `overrideV1Client(t, ts.URL)` since Organizations is a V1 resource.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cmd/ -run TestOrg -count=1 -v`
Expected: FAIL

**Step 3: Write the implementation**

Create `internal/cmd/org.go`:

```go
// orgDefaultFields: _id, displayName, created
var orgDefaultFields = []string{"_id", "displayName", "created"}

func newOrgCmd() *cobra.Command       // Use "org", Short "View JumpCloud organization info"
func newOrgListCmd()                  // list (no flags needed — orgs are few)
func runOrgList(cmd)                  // V1 ListAll → WriteList → footer

func newOrgGetCmd()                   // get <org-id>
func runOrgGet(cmd, id)              // V1 Get → WriteSingle
```

Key details:
- Read-only — no create/update/delete subcommands
- No resolver — `get` takes a raw ID argument
- Uses `newV1Client()` (not V2)
- V1 endpoint: `/organizations`
- List returns V1 format: `{"results": [...], "totalCount": N}`
- Footer uses `writeListFooter(cmd, len, total)` (V1 pattern with totalCount)
- `get` is `cobra.ExactArgs(1)` — takes the org ID directly

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/cmd/ -run TestOrg -count=1 -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/cmd/org.go internal/cmd/org_test.go
git commit -m "feat(org): add organization commands (list, get)"
```

---

### Task 5: Wiring — root, resolver, schema, error codes, MCP

**Files:**
- Modify: `internal/cmd/root.go` — add 4 `AddCommand` calls + 4 builtinCommands entries
- Modify: `internal/resolve/resolve.go` — add 3 resolver configs (SoftwareAppConfig, LDAPServerConfig, ActiveDirectoryConfig)
- Modify: `internal/cmd/cli_error.go` — add 3 error codes
- Modify: `internal/schema/schema.go` — add 4 resource entries + 4 command entries
- Modify: `internal/mcp/tools.go` — register MCP tools for all 4 resources

**Step 1: Add resolver configs**

In `internal/resolve/resolve.go`, add after `AdminConfig`:

```go
var SoftwareAppConfig = ResourceConfig{
    CacheKey:     "softwareapps",
    ListEndpoint: "/softwareapps",
    NameField:    "displayName",
    IDField:      "id",
}

var LDAPServerConfig = ResourceConfig{
    CacheKey:     "ldapservers",
    ListEndpoint: "/ldapservers",
    NameField:    "name",
    IDField:      "id",
}

var ActiveDirectoryConfig = ResourceConfig{
    CacheKey:     "activedirectories",
    ListEndpoint: "/activedirectories",
    NameField:    "domain",
    IDField:      "id",
}
```

**Step 2: Add error codes**

In `internal/cmd/cli_error.go`, add:

```go
ErrCodeSoftwareNotFound = "SOFTWARE_NOT_FOUND"
ErrCodeLDAPNotFound     = "LDAP_NOT_FOUND"
ErrCodeADNotFound       = "AD_NOT_FOUND"
```

**Step 3: Wire commands in root.go**

Add to the `AddCommand` block:
```go
rootCmd.AddCommand(newSoftwareCmd())
rootCmd.AddCommand(newLDAPCmd())
rootCmd.AddCommand(newADCmd())
rootCmd.AddCommand(newOrgCmd())
```

Add to `builtinCommands`:
```go
"software": true, "ldap": true, "ad": true, "org": true,
```

**Step 4: Add schema entries**

In `internal/schema/schema.go`, add resource schemas and command entries for all 4 resources following existing patterns. Each needs:
- `ResourceSchema` with fields, default fields, verbs, API version, filter/sort support
- `CommandEntry` with path, description, subcommands, flags

Software: verbs `[list, get, create, update, delete]`, fields for id/displayName/settings/createdAt/updatedAt, flags for --name/--settings
LDAP: verbs `[list, get, create, update, delete]`, fields for id/name/userLockoutAction/userPasswordExpirationAction
AD: verbs `[list, get, create, update, delete]`, fields for id/domain/useCase/groupsEnabled/delegationState
Org: verbs `[list, get]`, fields for _id/displayName/created/logoUrl

**Step 5: Register MCP tools**

In `internal/mcp/tools.go`, add tool registrations following existing patterns:

For each V2 resource (software, ldap, ad): `_list`, `_get`, `_create`, `_update`, `_delete`
For org: `org_list`, `org_get` only

Destructive operations (create, update, delete) use plan-first safety pattern:
- Check for `execute: true` in input
- Without it, return plan object with action/target/warning/execute_instruction
- With it, execute the operation

**Step 6: Run full test suite**

Run: `make test`
Expected: All packages PASS

**Step 7: Commit**

```bash
git add internal/cmd/root.go internal/resolve/resolve.go internal/cmd/cli_error.go internal/schema/schema.go internal/mcp/tools.go
git commit -m "feat: wire Tier 2 resources into root, schema, resolver, MCP, and error codes"
```

---

### Task 6: Final verification and cleanup

**Step 1: Run full test suite**

Run: `make test`
Expected: All packages PASS, no failures

**Step 2: Build**

Run: `make build`
Expected: Clean build, `./jc` binary produced

**Step 3: Clean up probe tool**

Remove the temporary API probe:
```bash
rm -rf cmd/probe/
```

**Step 4: Smoke test** (if live API available)

```bash
./jc software list --table
./jc ldap list --table
./jc ad list --table
./jc org list --table
```

**Step 5: Update progress.md**

Add Tier 2 entry to `progress.md` with resources added, test counts, and files changed.

**Step 6: Commit and push**

```bash
git add -A
git commit -m "docs: update progress with Tier 2 results"
git push
```
