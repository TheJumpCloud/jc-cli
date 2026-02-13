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
- API client in `internal/api/` — `authTransport` (custom RoundTripper) injects x-api-key, Content-Type, Accept, User-Agent on every request
- `api.NewClient()` reads key from `config.APIKey()`, `api.NewClientWithKey(key)` for explicit key
- `api.RedactKey(key)` shows only last 4 chars — use this everywhere API keys are logged
- `api.ValidateAPIKey()` calls `GET /api/organizations` to verify credentials
- JumpCloud organizations endpoint returns `{"results": [...]}` wrapper; fallback to direct object parse
- Keychain package in `internal/keychain/` wraps `zalando/go-keyring` with service name "jc"
- `keychain.Resolve(value)` transparently resolves `keychain://jc/<profile>` URIs or returns plaintext as-is
- `config.APIKey()` resolves keychain refs — all consumers (api.Client, auth commands) get keychain support automatically
- Use `keyring.MockInit()` in tests to avoid real keychain access; `keyring.MockInitWithError(err)` to simulate failures
- Config stores `keychain://jc/<profile>` as the `api_key` value to indicate the real key lives in the OS keychain
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

## 2026-02-13 - US-004
- What was implemented:
  - HTTP client with `authTransport` (custom RoundTripper) that injects x-api-key, Content-Type, Accept, User-Agent headers
  - `NewClient()` creates client from configured API key; `NewClientWithKey(key)` for explicit key
  - `ValidateAPIKey()` validates credentials via GET /api/organizations
  - `RedactKey(key)` shows "****" + last 4 chars for safe logging
  - `ErrNoAPIKey` with actionable error message: "Run jc auth login or set JC_API_KEY"
  - Clear error messages for HTTP 401 (invalid key), 403 (insufficient permissions), and server errors
- Files changed:
  - internal/api/client.go — Client struct, NewClient(), NewClientWithKey(), authTransport, RedactKey()
  - internal/api/auth.go — ValidateAPIKey(), Organization struct, truncateBody()
  - internal/api/client_test.go — 10 tests for client creation, header injection, redaction, config integration
  - internal/api/auth_test.go — 8 tests for validation success, 401, 403, 500, empty results, connection errors
- **Learnings for future iterations:**
  - `json.Unmarshal` into a struct with a `Results` field succeeds even when `results` doesn't exist in JSON — it just leaves `Results` as nil/empty. Always check `len(wrapper.Results) > 0` instead of relying on unmarshal error.
  - Use `req.Clone(req.Context())` in custom RoundTrippers to avoid mutating the caller's request.
  - `httptest.NewServer` is the standard Go pattern for testing HTTP clients — no mocking libraries needed.
  - The `--api-key` flag was already wired from US-003; US-004 only needed to build the consumer (api.Client).
---

## 2026-02-13 - US-005
- What was implemented:
  - New `internal/keychain/` package wrapping `zalando/go-keyring` for cross-platform keychain access
  - `keychain.Set(profile, key)` — stores API key in OS keychain (macOS Keychain / Linux secret-tool)
  - `keychain.Get(profile)` — retrieves API key from keychain
  - `keychain.Delete(profile)` — removes API key from keychain
  - `keychain.IsAvailable()` — checks if OS keychain is usable
  - `keychain.URI(profile)` — generates `keychain://jc/<profile>` reference URI
  - `keychain.IsKeychainRef(value)` / `keychain.ProfileFromURI(value)` — URI parsing utilities
  - `keychain.Resolve(value)` — transparently resolves keychain refs or returns plaintext
  - Updated `config.APIKey()` to resolve `keychain://jc/<profile>` references from config
  - Graceful fallback: if keychain is unavailable, warns on stderr and returns empty string
  - JC_API_KEY env var always takes priority over keychain refs (env is never a keychain ref)
- Files changed:
  - internal/keychain/keychain.go — new keychain wrapper package
  - internal/keychain/keychain_test.go — 17 tests covering all keychain operations
  - internal/config/config.go — updated APIKey() to resolve keychain refs, added keychain import
  - internal/config/config_test.go — added 5 keychain integration tests
  - go.mod, go.sum — added `github.com/zalando/go-keyring v0.2.6`
- **Learnings for future iterations:**
  - `go-keyring` provides `MockInit()` and `MockInitWithError(err)` for testing — no real keychain access needed in tests.
  - The `keychain` package intentionally has NO import of `config` to avoid circular dependencies. Config depends on keychain, not vice versa.
  - Keychain service name is "jc" and account name is the profile name — this matches the acceptance criteria.
  - `jc auth login` (US-006) will use `keychain.Set()` + write `keychain://jc/<profile>` to config. `jc auth logout` will use `keychain.Delete()`.
---
