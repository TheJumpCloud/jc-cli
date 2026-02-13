## Codebase Patterns
- Entry point: `cmd/jc/main.go` calls `cmd.Execute()` in `internal/cmd/`
- Cobra commands defined in `internal/cmd/` — each command is a function returning `*cobra.Command`
- Viper config initialized in `internal/config/config.go` before root command runs
- Version set via `-ldflags -X` at build time; defaults to `"dev"` in source
- Makefile targets: `build`, `test`, `lint`, `install`, `clean`
- Use `rootCmd.SetVersionTemplate` to customize `--version` output format
- Tests use `NewRootCmd()` + `SetOut(buf)` + `SetArgs()` pattern for command testing
- Git identity: `juergen@klaassen-consulting.com` / `Juergen Klaassen`

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
