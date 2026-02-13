## Codebase Patterns
- Entry point: `cmd/jc/main.go` calls `cmd.Execute()` in `internal/cmd/`
- Cobra commands defined in `internal/cmd/` — each command is a function returning `*cobra.Command`
- Viper config initialized in `internal/config/config.go` before root command runs
- Version set via `-ldflags -X` at build time; defaults to `"dev"` in source
- Makefile targets: `build`, `test`, `lint`, `install`, `clean`
- Use `rootCmd.SetVersionTemplate` to customize `--version` output format
- Tests use `NewRootCmd()` + `SetOut(buf)` + `SetArgs()` pattern for command testing
- Git identity: `juergen@klaassen-consulting.com` / `Juergen Klaassen`
- Config path resolution: `JC_CONFIG` env → `XDG_CONFIG_HOME/jc/` → `~/.config/jc/`
- Config tests use `t.Setenv()` + `t.TempDir()` + `viper.Reset()` for isolation
- `config.Init()` returns `error` — caller (`Execute()`) must handle it
- Default config written as `const DefaultConfig` string in `config.go`
- Resource commands follow pattern: `newXxxCmd()` parent + `newXxxListCmd()`/`newXxxGetCmd()` subcommands
- `newV1Client` var is shared across resource commands (users, devices) — single test override point
- `writeListFooter()` is a shared utility for "── N of TOTAL items ──" footers
- Test servers: `startXxxServer(t, data)` returns `*httptest.Server` matching V1 API response shape
- `setupUsersTest(t)` reusable across resource test files (keyring, viper, config init)
- V2 API: `V2Client` embeds `*Client` (same as V1), base URL `https://console.jumpcloud.com/api/v2`
- V2 pagination: Link headers with `rel="next"` (RFC 5988), no totalCount in response body
- V2 responses: bare JSON arrays `[{...}]` (not wrapped like V1 `{"results":..., "totalCount":...}`)
- `parseLinkNext()` extracts next URL from Link header — handles multiple relations, whitespace
- `V2ListResult` has no `TotalCount` field (unlike V1's `ListResult`) — V2 API doesn't expose it
- `NewV2ClientWithKey(key)` / `NewV2Client()` constructors mirror V1 pattern
- V2 client includes `Patch()` method (V2 APIs often use PATCH for partial updates)
- `newV2Client` var in `groups.go` for V2 test injection (separate from `newV1Client`)
- `overrideV2Client(t, serverURL)` pattern for V2 resource command tests
- `V2Resolver` in resolve package mirrors `Resolver` but uses `V2Client`; V2 uses `id` field (not `_id`)
- Groups command hierarchy: `jc groups user list/get/create/update/delete`
- V2 list footer shows `── N items ──` (no totalCount available unlike V1)
- `ToV2Query()` / `ToV2Queries()` in filter package: `field:op:value` (no `$` prefix)
- User group default fields: id, name, description, type
- `UserGroupConfig` / `DeviceGroupConfig` in resolve package for V2 name-to-ID resolution

---

## 2026-02-13 - US-001
- Implemented Go module setup with Cobra root command and Viper config
- Files created:
  - `cmd/jc/main.go` — binary entry point
  - `internal/cmd/root.go` — root command, version, completion, global flags
  - `internal/cmd/root_test.go` — tests for version, help, flags, completion
  - `internal/config/config.go` — Viper initialization
  - `Makefile` — build/test/lint/install/clean targets
  - `.gitignore` — standard Go ignores
  - `go.mod` / `go.sum` — dependencies (cobra v1.10.2, viper v1.21.0)
- **Learnings for future iterations:**
  - Cobra's `Version` field + `SetVersionTemplate` controls `--version` flag output
  - `go build ./...` is the acceptance criteria check — must succeed with zero errors
  - `SilenceUsage: true` and `SilenceErrors: true` prevent Cobra from printing usage on errors
  - Go 1.25.5 is the runtime version on this machine
  - Persistent flags on root command are inherited by all subcommands automatically
---

## 2026-02-13 - US-002
- Implemented configuration file system with XDG Base Directory support
- Files changed:
  - `internal/config/config.go` — rewrote: auto-creation, XDG paths, JC_CONFIG override, Viper defaults, error handling
  - `internal/config/config_test.go` — new: 14 tests covering path resolution, permissions, YAML parsing, profiles
  - `internal/cmd/root.go` — updated `Execute()` to handle `Init()` error return
  - `.chief/prds/main/prd.json` — marked US-002 as complete
- **Learnings for future iterations:**
  - Viper is global state — tests MUST call `viper.Reset()` between tests to avoid contamination
  - `t.Setenv()` (Go 1.17+) auto-restores env vars after test — cleaner than manual save/restore
  - `viper.SetConfigFile(path)` is more precise than `SetConfigName` + `AddConfigPath` when you know the exact path
  - `os.MkdirAll` with 0700 is idempotent — safe to call even if directory exists
  - Config file format: YAML with `active_profile`, `defaults`, `cache`, `profiles` top-level sections
  - Invalid YAML errors from Viper include parse details — wrap with file path for user-friendly messages
---

## 2026-02-13 - US-015
- Implemented devices list and get commands (V1 Systems API)
- Files changed:
  - `internal/cmd/devices.go` — new: `newDevicesCmd()` parent + `newDevicesListCmd()` + `newDevicesGetCmd()`
  - `internal/cmd/devices_test.go` — new: 25 tests covering JSON, table, CSV, --ids, --quiet, --limit, --sort, pagination, get, not-found, help
  - `internal/cmd/root.go` — added `newDevicesCmd()` registration
  - `.chief/prds/main/prd.json` — marked US-015 as complete
- **Learnings for future iterations:**
  - V1 Systems API endpoint is `/systems` (list) and `/systems/{id}` (get) — analogous to `/systemusers`
  - Device default fields: displayName, hostname, os, osVersion, lastContact, agentVersion
  - `setupUsersTest(t)` is reusable across all resource test files — no need for per-resource setup
  - `overrideV1Client(t, serverURL)` works for all resource commands since they share `newV1Client`
  - Adding a new resource command is straightforward: new file, register in root, write test server + tests
- Filter parser in `internal/filter/` — `Parse()`, `ParseAll()`, `ToV1Queries()` for translating user syntax to V1 API format
- Use `StringArrayVar` (not `StringSliceVar`) for `--filter` flag — preserves values with commas/spaces
- V1 API `filter` query param format: `field:$op:value` where `$op` is `$eq`, `$ne`, `$gt`, `$gte`, `$lt`, `$lte`
- `buildListURL()` in v1.go accepts full `ListOptions` struct — extensible for future query params
---

## 2026-02-13 - US-016
- Implemented devices delete command with confirmation prompt
- Files changed:
  - `internal/cmd/devices.go` — added `newDevicesDeleteCmd()` + `runDevicesDelete()`, updated parent cmd to register delete, added imports for encoding/json, fmt, strings, viper
  - `internal/cmd/devices_test.go` — added 8 delete tests (force, confirm yes/no, empty input, not found, missing arg, API endpoint, help includes delete), updated `startDevicesServer` to handle DELETE method
  - `.chief/prds/main/prd.json` — marked US-016 as complete
- **Learnings for future iterations:**
  - Device delete follows same pattern as users delete: GET first to show details, prompt, then DELETE
  - Confirmation prompt shows hostname, OS, and last contact date (truncated at 'T' for readability)
  - `getConfirmReader()` and `confirmReader` var are shared across users.go and devices.go — no duplication needed
  - `startDevicesServer` needed to be updated to switch on HTTP method (GET/DELETE) for `/systems/{id}` routes
  - `overrideDevicesConfirmReader` is a thin wrapper around the shared `confirmReader` — same pattern as `overrideConfirmReader` in users_test.go
---

## 2026-02-13 - US-018
- Implemented name-to-ID resolution with file-based caching
- Files created:
  - `internal/resolve/resolve.go` — `Resolver` struct with `Resolve()`, `IsID()` (24-char hex detection), `ResourceConfig` for users/devices, JSON file cache with configurable TTL, `--no-cache` bypass, ambiguous match error handling, cache dir created with 0700 permissions
  - `internal/resolve/resolve_test.go` — 14 tests covering IsID valid/invalid, ID passthrough, user by username, device by hostname, case-insensitive, not found, ambiguous, cache hit, cache expired, cache stored, --no-cache flag, cache disabled, cache dir creation
- Files changed:
  - `internal/cmd/users.go` — added `resolveUser()` helper, integrated resolver into get/update/delete/lock/unlock/reset-mfa/reset-password commands, updated Use/Long descriptions to show `<username-or-id>`
  - `internal/cmd/devices.go` — added `resolveDevice()` helper, integrated resolver into get/delete/MDM commands, updated Use/Long descriptions to show `<hostname-or-id>`
  - `internal/cmd/users_test.go` — migrated all IDs to 24-char hex strings (required by IsID), updated "not found" error assertions from HTTP 404 to resolver error format
  - `internal/cmd/devices_test.go` — same ID migration and error assertion updates as users_test.go
  - `.chief/prds/main/prd.json` — marked US-018 as complete
- **Learnings for future iterations:**
  - `IsID()` uses `regexp.MustCompile("^[0-9a-fA-F]{24}$")` — all test IDs must be exactly 24 hex chars or the resolver tries API lookup
  - Resolver uses `ListAll()` to fetch all resources, then filters client-side by name field — maximally compatible with V1 API
  - `ResourceConfig` pattern makes resolver resource-agnostic: just specify CacheKey, ListEndpoint, NameField, IDField
  - File-based JSON cache: one file per resource type (users.json, systems.json) under `~/.cache/jc/`
  - Cache uses lowercase name keys for case-insensitive matching
  - Thin `resolveUser()`/`resolveDevice()` helpers in command files keep resolver integration minimal
  - When updating test IDs, must also update: mock server path handlers, inline JSON strings, expected API path assertions, and ID comparison values
---

## 2026-02-13 - US-019
- Implemented shell completions (Bash, Zsh, Fish) with output format flag completions
- Files changed:
  - `internal/cmd/root.go` — added `RegisterFlagCompletionFunc` for `--output` flag with all valid output formats and `ShellCompDirectiveNoFileComp`
  - `internal/cmd/root_test.go` — added 12 new completion tests: shell-specific script validation (bash functions, zsh compdef, fish complete), invalid shell error, missing arg error, subcommand inclusion, global flag inclusion, output format completion via `__complete`, help installation instructions, ValidArgs verification, output flag completion function registration
  - `.chief/prds/main/prd.json` — marked US-019 as complete
- **Learnings for future iterations:**
  - Cobra's bash/zsh/fish completions use the `__complete` binary mechanism at runtime — flag values are NOT inlined in the completion script
  - `RegisterFlagCompletionFunc` on a persistent flag must be called on the command where the flag is defined (root), not on subcommands
  - `cobra.ShellCompDirectiveNoFileComp` prevents file path suggestions when completing flag values — important for enum-style flags
  - Test flag completions via `__complete` command: `rootCmd.SetArgs([]string{"__complete", "--flag", ""})` triggers Cobra's internal completion
  - `ValidArgs` on a command automatically provides shell completion for positional arguments — no extra registration needed
  - The existing `newCompletionCmd()` already had good structure (ValidArgs, Long help with install instructions) — just needed flag completion enhancement
---

## 2026-02-13 - US-020
- Implemented filtering engine with shared filter parser and --filter/--search flags
- Files created:
  - `internal/filter/filter.go` — `Parse()`, `ParseAll()`, `ToV1Queries()`, `Expression` struct, operator mapping (=, !=, >=, <=, >, <) to V1 API format ($eq, $ne, $gte, $lte, $gt, $lt)
  - `internal/filter/filter_test.go` — 15 tests covering all operators, whitespace trimming, empty values, invalid syntax, multiple filters, V1 query conversion
- Files changed:
  - `internal/api/v1.go` — extended `ListOptions` with `Filter []string` and `Search string` fields; updated `buildListURL()` to accept full `ListOptions` and append `filter` and `search` query params
  - `internal/cmd/users.go` — added `--filter` (StringArrayVar) and `--search` flags to `newUsersListCmd()` and `--filter` to `newUsersSearchCmd()`; filter parsing/validation in `runUsersList()` and `runUsersSearch()`
  - `internal/cmd/devices.go` — added `--filter` (StringArrayVar) and `--search` flags to `newDevicesListCmd()`; filter parsing/validation in `runDevicesList()`
  - `internal/cmd/users_test.go` — 8 new tests: filter API query param, multiple filters, comparison operators, invalid syntax, search param, filter+sort combo, search with filter, help includes filter/search
  - `internal/cmd/devices_test.go` — 6 new tests: filter API query param, multiple filters, invalid syntax, search param, filter+sort+limit combo, help includes filter/search
  - `.chief/prds/main/prd.json` — marked US-020 as complete
- **Learnings for future iterations:**
  - `StringArrayVar` is critical for `--filter` — `StringSliceVar` would split on commas inside values like `os=Mac OS X`
  - V1 API accepts multiple `filter` query params: `?filter=field:$eq:value&filter=field2:$ne:value2` — these combine with AND logic
  - `buildListURL()` was refactored to accept the full `ListOptions` struct instead of individual params — cleaner and extensible
  - Filter parsing happens before API client creation in the command layer — fast validation before network calls
  - Operator ordering in the parser matters: `>=` must be matched before `>` to avoid false matches
---

## 2026-02-13 - US-023
- Implemented V2 API client layer with Link header pagination
- Files created:
  - `internal/api/v2.go` — `V2Client` struct with `ListAll()` (Link header pagination), `Get()`, `Create()`, `Update()`, `Delete()`, `Patch()` methods; `V2ListOptions`, `V2ListResult` types; `parseLinkNext()` for RFC 5988 Link header parsing; `buildV2ListURL()` with filter/sort/search/limit query params
  - `internal/api/v2_test.go` — 27 tests covering: single/multi-page pagination, limit capping, limit < page size, empty results, context cancellation, API errors, sort/filter/search params, Link header following, Get/Create/Update/Delete/Patch success and error cases, constructors, transport sharing with V1, x-api-key auth, parseLinkNext edge cases, multi-relation Link headers
- Files changed:
  - `.chief/prds/main/prd.json` — marked US-023 as complete
  - `.chief/prds/main/progress.md` — added codebase patterns for V2 client
- **Learnings for future iterations:**
  - V2 pagination via Link headers means the client follows opaque URLs — first request uses `buildV2ListURL()`, subsequent requests use the `nextURL` from the Link header directly
  - V2 responses are bare JSON arrays — no wrapper object, no totalCount — so `V2ListResult` intentionally omits `TotalCount` (unlike V1's `ListResult`)
  - `parseLinkNext()` must handle comma-separated multiple Link relations (e.g., `<prev>; rel="prev", <next>; rel="next"`)
  - V2 client adds `Patch()` method since V2 APIs commonly use PATCH for partial updates (V1 uses PUT exclusively)
  - V2 filter format is different from V1: V2 uses `name:eq:value` (no `$` prefix on operators)
  - `V2Client` embeds `*Client` with overridden `BaseURL` — shares the entire transport chain (auth, logging, retry) with V1
---

## 2026-02-13 - US-024
- Implemented User Groups CRUD commands using V2 API
- Files created:
  - `internal/cmd/groups.go` — `newGroupsCmd()` parent + `newGroupsUserCmd()` + list/get/create/update/delete subcommands; `newV2Client` var for test injection; `resolveUserGroup()` using V2Resolver; V2 filter support
  - `internal/cmd/groups_test.go` — 30 tests covering: list (JSON, table, CSV, IDs, quiet, footer, empty, filter, sort, limit, invalid filter), get (by ID, by name, not found, missing arg), create (full, name-only, missing name, API endpoint), update (by ID, by name, no fields, API endpoint), delete (force, force by name, confirm yes/no, empty input, not found, missing arg, prompt shows name), help structure
- Files changed:
  - `internal/cmd/root.go` — registered `newGroupsCmd()` in root command
  - `internal/resolve/resolve.go` — added `V2Resolver` struct with Resolve/cache methods; added `UserGroupConfig` and `DeviceGroupConfig` resource configs (V2 uses `id` not `_id`)
  - `internal/filter/filter.go` — added `ToV2Query()` and `ToV2Queries()` for V2 filter format (`field:op:value`, no `$` prefix)
  - `internal/filter/filter_test.go` — added `TestToV2Query` and `TestToV2Queries` tests
  - `.chief/prds/main/prd.json` — marked US-024 as complete
- **Learnings for future iterations:**
  - V2 resources use `id` (not `_id`) as the ID field — requires separate resolver config
  - V2 resolver (`V2Resolver`) mirrors V1 resolver pattern but takes `*api.V2Client` — shares cache infrastructure
  - `newV2Client` var is separate from `newV1Client` since group commands use V2 API while users/devices use V1
  - V2 test server returns bare JSON arrays — `json.NewEncoder(w).Encode(groups)` directly, no wrapper
  - V2 list footer omits totalCount — just shows `── N items ──` since V2 API doesn't expose total
  - Group commands nest under `jc groups user` (not `jc usergroups`) — prepares for `jc groups device` in US-025
  - `startUserGroupsServer` handles GET list, POST create, GET/PUT/DELETE by ID — similar to V1 test servers but simpler (no pagination needed for basic tests)
  - Confirmation prompts reuse shared `getConfirmReader()` and `overrideConfirmReader(t, input)` from users.go/users_test.go
---
