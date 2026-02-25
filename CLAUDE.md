# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

```bash
make build          # Build binary → ./jc (injects version via ldflags)
make test           # Run all tests (go test ./... -count=1)
make lint           # Run go vet
make install        # Install to $GOPATH/bin with version ldflags
go test ./internal/cmd/ -run TestUsersGet -count=1   # Run a single test
```

## Architecture

Go CLI for JumpCloud (`github.com/klaassen-consulting/jc`) built with Cobra + Viper.

**Entry point**: `cmd/jc/main.go` → `internal/cmd.Execute()`

### Package Layout

- `internal/cmd/` — All CLI commands (root + resource subcommands). This is the largest package (~48K lines).
- `internal/api/` — HTTP clients: `Client` (base), `V1Client`, `V2Client`, `InsightsClient`. Transport chain: auth → logging → retry → base.
- `internal/output/` — Format-agnostic output engine. Accepts `[]json.RawMessage`. Formats: json, table, csv, yaml, ndjson, human.
- `internal/config/` — Viper-based config with env var bindings, profile management, keychain resolution.
- `internal/resolve/` — Name-to-ID resolution with file-based caching. `Resolver` (V1) and `V2Resolver`.
- `internal/filter/` — Filter expression parser: `field:op:value` → V1 (`$op`) or V2 (bare `op`) query params.
- `internal/recipe/` — YAML recipe engine with template variables, conditional steps, embedded built-ins.
- `internal/mcp/` — MCP server using official Go SDK. Tool registration, filtering, plan-first safety.
- `internal/ask/` — LLM integration (Anthropic, OpenAI, Ollama) for conversational CLI translation.
- `internal/version/` — Shared leaf package for version constant (avoids circular imports).
- `internal/schema/` — Machine-readable CLI schema (`ResourceSchema`, `CommandManifest`). Drives TUI, MCP tools, and `jc ask`.
- `internal/tui/` — Interactive terminal UI (Bubbletea). Sub-packages: `style/`, `component/`, `screen/`, `fetch/`.
- `internal/plan/` — Plan mode rendering. `Plan` struct, `ExitCodePlan = 10`.
- `internal/keychain/` — OS keychain wrapper (`zalando/go-keyring`, NOT `99designs/keyring`).
- `internal/simulator/` — Auth policy simulator. Three-valued logic, IP matching, blast-radius analysis.

### Resource Command Pattern

Every resource (users, devices, groups, commands, policies, apps) follows the same structure:

1. **Package-level default fields**: `var userDefaultFields = []string{...}`
2. **Overridable client factory**: `var newV1Client = func() (*api.V1Client, error) { ... }` (for test injection)
3. **Parent command** with subcommands: `list`, `get`, `create`, `update`, `delete`, plus resource-specific actions
4. **List**: parse filters → `client.ListAll()` → `output.WriteList()` → footer to stderr
5. **Get**: resolve name/ID → `client.Get()` → `output.WriteSingle()`
6. **Mutations**: check `viper.GetBool("plan")` for dry-run, respect `--force`, support stdin batch mode

### Key Design Decisions

- **Resource-agnostic output**: All data flows as `[]json.RawMessage` — no resource-specific structs in the output pipeline.
- **Data to stdout, metadata to stderr**: List footers, progress, confirmations go to stderr. Keeps piping clean.
- **Var-based test injection**: Client factories (`newV1Client`, `newV2Client`, etc.) are package-level `var` functions replaced in tests.
- **Plan mode**: Mutations check `viper.GetBool("plan")` and return `&ExitError{Code: plan.ExitCodePlan}` with action details instead of executing.
- **PersistentPreRunE on root only**: Cobra does NOT auto-chain PersistentPreRunE for subcommands. All global validation lives in root's PersistentPreRunE.
- **Resolver caching**: 24-char hex IDs pass through without API calls. Names trigger case-insensitive lookup with file-based cache (TTL from config).

### TUI Architecture

Import hierarchy (strict — NO cycles allowed): `style` (leaf) ← `component` ← `screen` ← `tui` ← `cmd/tui.go` (assembly).

- `Screen` interface (`tea.Model` + `Title()`) with `NavStack` push/pop navigation and breadcrumbs.
- `ResourceEntry` registry maps resource names to schema-driven `ListScreen`/`DetailScreen` — all 28 resources use the same generic views.
- `fetch/` subpackage: async data loading with generation-based cache staleness (each navigation bumps generation; stale results are discarded).

## Testing Patterns

Tests use pure `testing.T` (no external frameworks). Standard setup:

```go
keyring.MockInit()                           // Disable real keychain
t.Setenv("JC_CONFIG", dir)                   // Isolate config
viper.Reset()                                // Clear global state
overrideV1Client(t, server.URL)              // Redirect to httptest server
```

- `startUsersServer(t, users)` / similar — mock API servers with CRUD + search
- `overrideAPIClient`, `overrideV1Client`, `overrideV2Client`, `overrideInsightsClient`, `overrideAskClient` — per-client injection
- `writeTempCSV(t, content)` — create temp CSV fixtures
- Test IDs must be valid 24-char hex (`[0-9a-fA-F]{24}`) — letters g+ break `IsID()` pass-through
- Membership/graph tests need `cache.directory` set to temp dir (stale real cache breaks tests)

## Adding a New Resource

This is the most common and error-prone workflow. Every new resource requires changes in 6+ files:

1. `internal/cmd/<resource>.go` — Command + subcommands (list/get/create/update/delete)
2. `internal/cmd/<resource>_test.go` — Tests with mock server
3. `internal/schema/schema.go` — Add to `Resources` map + `BuildCommandManifest()`
4. `internal/mcp/tools.go` — Register MCP tools
5. `internal/cmd/root.go` — `AddCommand()` + add to `builtinCommands` map
6. **Update hardcoded counts in 3 test files**: `internal/schema/schema_test.go`, `internal/cmd/schema_test.go`, `internal/mcp/tools_test.go`

Forgetting step 6 causes silent test failures. Always grep for the current count before adding.

## Important Conventions

- **Version injection**: `internal/version/version.go` — set via ldflags: `-X 'module/internal/version.Number=...'`
- **Exit codes**: 0=success, 1=general, 2=usage, 3=auth, 4=permission, 5=rate_limit, 10=plan, 130=interrupted
- **Structured errors**: `CLIError{Code,Message,Suggestion}` in `cli_error.go`; `ToCLIError()` converts at Execute() boundary
- **Filter flags**: Use `StringArrayVar` (not `StringSliceVar`) for `--filter` — preserves values with commas
- **Config writes**: `viper.WriteConfigAs(tmp.yaml)` + `os.Rename()` for atomicity. Extension must be `.yaml`.
- **V1 vs V2 APIs**: V1 uses `_id`, `{"results":[...], "totalCount":N}`, filter `$op`. V2 uses `id`, bare arrays, Link header pagination, filter without `$`.
- **MCP tool names**: Only `[a-zA-Z0-9_.-]` allowed (SDK validates). Use underscores for namespacing.
- **jsonschema struct tags**: Description-only (no `key=value` format, no `,required`). Required controlled by `json:"...,omitempty"`.
- **`drainAndClose(resp)`**: Always drain response body before retry to enable HTTP connection reuse.
- **`--fields`/`--exclude` mutual exclusivity**: Validated in root `PersistentPreRunE`.
- **Go version**: `go 1.25.5` (from go.mod).
- **Keyring import**: Always `zalando/go-keyring`, NOT `99designs/keyring` — LLM agents commonly confuse these.
- **Alias system**: `expandAliases()` in root.go runs before Cobra parse; short forms `u`, `d`, `g`, `i` in `builtinCommands` map.

## PRD & Progress

- PRD: `.chief/prds/main/prd.json`
- Progress tracker: `progress.md`
