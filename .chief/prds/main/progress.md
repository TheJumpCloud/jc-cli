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
