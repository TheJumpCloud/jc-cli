## Codebase Patterns
- Viper env prefix is `JC_` — all env vars start with `JC_`
- Use `viper.BindEnv("config.key", "JC_ENV_VAR")` for explicit env-to-key mappings (especially for nested keys where AutomaticEnv would require JC_DEFAULTS_OUTPUT instead of JC_OUTPUT)
- Config file path: `~/.config/jc/config.yaml` (XDG-compliant)
- Cobra root command defined in `internal/cmd/root.go`, config in `internal/config/config.go`
- Tests use `t.Setenv` + `t.TempDir()` for isolation; always call `viper.Reset()` in config tests
- Flag binding: `--output` flag binds to Viper key `defaults.output` (not `output`) to match config nesting
- `--api-key` flag binds to Viper key `api_key` (underscore) to share priority with `JC_API_KEY`
- Build version injected via `-ldflags` at build time
- Makefile targets: build, test, lint (vet), install, clean
---

## 2026-02-13 - US-001
- Go module scaffolded with Cobra root command and Viper
- Files: cmd/jc/main.go, internal/cmd/root.go, internal/cmd/root_test.go, Makefile, go.mod
---

## 2026-02-13 - US-002
- Configuration file system with auto-creation, XDG support, JC_CONFIG override
- Files: internal/config/config.go, internal/config/config_test.go
---

## 2026-02-13 - US-003
- What was implemented:
  - Explicit env var bindings: JC_API_KEY, JC_ORG_ID, JC_PROFILE, JC_OUTPUT, JC_NO_COLOR
  - Helper functions: ActiveProfile(), APIKey(), OrgID(), Output(), NoColor(), NoColorFromEnv()
  - Full flag-to-Viper bindings for all persistent flags (previously only 4 were bound)
  - Fixed --output flag binding from "output" to "defaults.output" for correct config file interop
  - NO_COLOR standard env var support (https://no-color.org/)
  - Priority chain verified: flags > env vars > config file > built-in defaults
- Files changed:
  - internal/config/config.go — added bindEnvVars(), helper functions
  - internal/config/config_test.go — added 11 new tests for env var support
  - internal/cmd/root.go — bound all flags to Viper, fixed output key mapping
  - internal/cmd/root_test.go — added 3 priority chain integration tests
- **Learnings for future iterations:**
  - Viper's `AutomaticEnv()` maps `JC_DEFAULTS_OUTPUT` to `defaults.output`, but users expect `JC_OUTPUT`. Use explicit `BindEnv("defaults.output", "JC_OUTPUT")` for user-friendly names.
  - When binding `--api-key` flag (hyphenated) to Viper, use `api_key` (underscore) as the Viper key so it shares priority with `JC_API_KEY` env var.
  - APIKey() must check top-level `api_key` first (from env/flag), then fall back to `profiles.<active>.api_key` from config.
  - The `--output` flag default is "json" in Cobra, but must bind to `defaults.output` in Viper — not just `output` — otherwise config file value `defaults.output: table` won't be picked up.
---
