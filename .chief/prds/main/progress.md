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
