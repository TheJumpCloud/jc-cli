# Integration Test Script — Design

**Issue**: N/A (operational tooling)
**Date**: 2026-02-21

## Goal

A standalone bash script (`scripts/integration-test.sh`) that exercises the full `jc` CLI surface against a live JumpCloud organization. Creates test resources, runs operations, verifies output formats, and always cleans up.

## Decisions

- **Shell script** — no Go toolchain required on the test host, just `jc` on PATH
- **Full surface** — every resource type exercised (list at minimum), mutable lifecycle for users/groups
- **Trap-based cleanup** — `trap cleanup INT TERM EXIT` with idempotent `--force` deletes
- **All 6 output formats** tested across read-only probes
- **Recipe engine** tested via onboard-user / offboard-user
- **MCP** verified via `jc mcp tools` count only (no server startup)

## Test Flow

### Phase 1 — Preflight

| Step | Command | Assertion |
|------|---------|-----------|
| Version | `jc --version` | Non-empty output |
| Auth | `jc auth status --quiet` | Exit code 0 |
| MCP tools | `jc mcp tools \| wc -l` | = 158 |
| Org | `jc org list` | Exit code 0, non-empty |

### Phase 2 — Mutable Lifecycle

Test user: `jctest-<unix-timestamp>`, email: `jctest-<ts>@test.jumpcloud.invalid`
Test group: `jctest-group-<unix-timestamp>`

| Step | Command | Assertion |
|------|---------|-----------|
| Create user | `jc users create --username ... --email ... --firstname Test --lastname User --ids` | Captures user ID |
| Get user | `jc users get $TEST_USER_ID` | Contains username |
| Search user | `jc users search "jctest-$TS"` | At least 1 result |
| Update user | `jc users update $TEST_USER_ID --department "Integration Test"` | Exit 0 |
| Lock user | `jc users lock $TEST_USER_ID` | Exit 0 |
| Unlock user | `jc users unlock $TEST_USER_ID` | Exit 0 |
| Create group | `jc groups user create --name "jctest-group-$TS" --ids` | Captures group ID |
| Add member | `jc groups add-member $TEST_GROUP_ID --user $TEST_USER_ID` | Exit 0 |
| Graph traverse | `jc graph traverse --from user:$TEST_USER_ID --to user_group` | Contains group ID |
| Remove member | `jc groups remove-member $TEST_GROUP_ID --user $TEST_USER_ID` | Exit 0 |

Cleanup (reverse order): delete group → delete user.

### Phase 3 — Recipe Engine

Second test user: `jctest-recipe-<ts>`, second group: `jctest-recipe-group-<ts>`

| Step | Command | Assertion |
|------|---------|-----------|
| Recipe list | `jc recipe list -t` | Contains "onboard-user" |
| Recipe show | `jc recipe show onboard-user` | Non-empty |
| Create recipe group | `jc groups user create --name "jctest-recipe-group-$TS" --ids` | Captures group ID |
| Onboard (plan) | `jc recipe run onboard-user --param username=... --param email=... --param firstname=Test --param lastname=Recipe --param group="jctest-recipe-group-$TS" --plan` | Exit 10 |
| Onboard (execute) | `jc recipe run onboard-user --param ... --force` | Exit 0, captures user ID |
| Verify user | `jc users get "jctest-recipe-$TS"` | Exit 0 |
| Offboard | `jc recipe run offboard-user --param user="jctest-recipe-$TS" --param delete_user=true --force` | Exit 0 |
| Verify gone | `jc users get "jctest-recipe-$TS"` | Exit non-zero |

Cleanup: delete recipe group (recipe deletes the user).

### Phase 4 — Read-Only Probes

Every resource type with at least a `list` call. Output formats distributed across probes.

| Resource | Command | Format |
|----------|---------|--------|
| Users | `jc users list --limit 3` | json (default) |
| Devices | `jc devices list --limit 3 -t` | table |
| User Groups | `jc groups user list --limit 3 --output csv` | csv |
| Device Groups | `jc groups device list --limit 3 --output yaml` | yaml |
| Commands | `jc commands list --limit 3` | json |
| Policies | `jc policies list --limit 3 -t` | table |
| Policy Groups | `jc policy-groups list --limit 3 --output ndjson` | ndjson |
| Policy Templates | `jc policy-templates list --limit 3 --output human` | human |
| Apps | `jc apps list --limit 3 -t` | table |
| App Templates | `jc app-templates list --limit 3` | json |
| Admins | `jc admins list --limit 3 --output csv` | csv |
| Auth Policies | `jc auth-policies list --limit 3 --output yaml` | yaml |
| IP Lists | `jc iplists list --limit 3 --output ndjson` | ndjson |
| Insights | `jc insights query --service all --last 1h --limit 5` | json |
| Insights count | `jc insights count --service all --last 1h` | json |
| Org | `jc org list --output yaml` | yaml |
| Software | `jc software list --limit 3 --output ndjson` | ndjson |
| LDAP | `jc ldap list --output human` | human |
| AD | `jc ad list --limit 3` | json |
| RADIUS | `jc radius list --output yaml` | yaml |
| Apple MDM | `jc apple-mdm list --limit 3 -t` | table |
| G Suite | `jc gsuite list --limit 3` | json |
| Office 365 | `jc office365 list --limit 3` | json |
| Duo | `jc duo list --limit 3 --output csv` | csv |
| Custom Emails | `jc custom-emails templates -t` | table |
| System Insights | `jc system-insights tables` | json |
| System Insights query | `jc system-insights os_version --limit 3 -t` | table |
| User States | `jc user-states list --limit 3` | json |

Also test flag combinations:
- `jc users list --limit 2 --fields username,email -t` (field selection)
- `jc users list --limit 2 --exclude password -t` (field exclusion)
- `jc users list --limit 2 --all -t` (all fields)
- `jc users list --limit 2 --ids` (IDs mode)
- `jc devices list --limit 2 --query "[].hostname"` (JMESPath)

### Phase 5 — Utilities

| Step | Command | Assertion |
|------|---------|-----------|
| Explain | `jc explain users delete testuser` | Exit 0, contains "DELETE" |
| Config | `jc config view` | Exit 0, non-empty |
| Schema resources | `jc schema resources` | Exit 0, contains "users" |
| Schema commands | `jc schema commands` | Exit 0, non-empty |
| Completion | `jc completion bash > /dev/null` | Exit 0 |

## Output Format

```
jc integration test — v1.4.1
═══════════════════════════════

Phase 1: Preflight
  [PASS] version check
  [PASS] auth status
  [PASS] mcp tools count (158)
  [PASS] org list

Phase 2: Mutable Lifecycle
  [PASS] users create (jctest-1740100000)
  [PASS] users get
  ...

Phase 3: Recipe Engine
  [PASS] recipe list
  [PASS] onboard-user (plan)
  [PASS] onboard-user (execute)
  ...

Phase 4: Read-Only Probes
  [PASS] users list (json)
  [PASS] devices list (table)
  [FAIL] duo list (csv) — exit code 3 (auth)
  ...

Phase 5: Utilities
  [PASS] explain
  [PASS] config view
  ...

═══════════════════════════════
Results: 42/44 passed, 2 failed
Cleanup: OK (2 resources deleted)
```

Exit 0 if all pass, 1 if any fail.

## Cleanup Strategy

- `trap cleanup INT TERM EXIT` registered at script start
- Resource IDs tracked in global variables
- Cleanup runs in reverse creation order: remove memberships → delete groups → delete users
- All deletes use `--force` and suppress stderr
- Idempotent: deleting a non-existent resource is not an error

## Not in Scope

- TUI (interactive terminal, manual testing only)
- MCP server JSON-RPC (unit tests cover this)
- `jc ask` (requires LLM provider)
- Device erase/lock (destructive to real devices)
- Bulk CSV (temp file management adds complexity for little coverage gain)

## Files

- `scripts/integration-test.sh` — The test script (single file, ~300 lines)
