# Authentication & Authorization

This document is the single source of truth for how `jc` authenticates to JumpCloud, how it gates operations, and what it can and cannot enforce on its own. It exists so that anyone evaluating jc from a security standpoint can answer five questions from one place:

1. How does jc identify itself to JumpCloud?
2. Where do credentials live, and what protects them?
3. Does jc enforce authorization, or does it inherit it?
4. What stops a destructive operation from firing — especially via the MCP server?
5. What is logged, what is redacted, and what is missing?

Code references throughout point to specific files in this repository so claims are easy to verify.

## TL;DR

- **Auth methods:** API key (default) or OAuth 2.0 client credentials (service account). Both identify a *service account*, not an individual operator.
- **Authorization:** jc has **no internal authorization layer**. Effective permissions are whatever the underlying JumpCloud admin entity can do at the Console level.
- **Destructive ops:** gated by TTY confirmation in the CLI and an `execute: true` flag in the MCP server. The MCP gate is agent-controlled — see [Limitations](#limitations--known-gaps).
- **MCP server:** stdio = parent-process trust; HTTP/SSE = optional API key, loopback-only by default, opt-in CORS/TLS.
- **Audit log:** `~/.config/jc/mcp-audit.log`, JSON per line, sensitive params redacted, **no per-operator identity captured**.

## Authentication

jc supports two authentication methods, exposed through `jc auth login`. Both produce a long-lived bearer credential that authorizes against JumpCloud's admin API.

### API key (default)

The default flow stores a JumpCloud API key in the OS keychain.

```
$ jc auth login
Enter JumpCloud API key: ************************
Validating API key... OK
Logged in to Acme Corp (profile: default)
```

What happens behind the scenes (`internal/cmd/auth.go:143-232`):

1. The key is validated against `GET /api/organizations` (`internal/api/auth.go:18`). A 401 fails the login; the key is never persisted on a failed validation.
2. On success, the org ID is stored in the active profile.
3. The key itself is stored in the OS keychain via `github.com/zalando/go-keyring` under service name `"jc"` and account `<profile>` (`internal/keychain/keychain.go:19`).
4. The config file stores only a reference URI (`keychain://jc/<profile>`) — never the plaintext key.

If the OS keychain is unavailable (CI, headless containers, locked daemons), login fails by default. To explicitly opt in to plaintext storage in the config file:

```
jc auth login --allow-plaintext
```

This emits a warning every time and is intentionally clunky.

### OAuth 2.0 client credentials (service account)

For tooling that needs a refreshable token instead of a long-lived key:

```
$ jc auth login --service-account
Enter client ID: ********
Enter client secret: ************************
```

The grant flow (`internal/api/oauth.go:90-142`):

- `POST https://admin-oauth.id.jumpcloud.com/oauth2/token`
- `grant_type=client_credentials`, `scope=api`
- Authentication: HTTP Basic (`base64(clientID:clientSecret)`)

The response yields an access token (`expires_in` defaults to 3600s if unspecified). Tokens are cached in memory and refreshed on demand with a 30-second pre-expiry buffer (`internal/api/oauth.go:67`). There is no refresh token; the cache fetches a new access token whenever the cached one is within 30 seconds of expiry.

The client secret is stored in the OS keychain under a separate keychain key; the config file references it the same way as API keys.

### Configuration & precedence

A single config file holds non-secret state per profile. Default location is `~/.config/jc/config.yaml` (or `$XDG_CONFIG_HOME/jc/config.yaml`, or `$JC_CONFIG`).

Profile fields (`internal/config/config.go`):

```yaml
active_profile: default

profiles:
  default:
    api_key: keychain://jc/default     # or "" + auth_method: service_account
    org_id: 5f9c1a2b3c4d5e6f7a8b9c0d
    auth_method: ""                     # "" (api_key) or "service_account"
    client_id: ""                       # only for service_account
    client_secret: ""                   # keychain ref, like api_key
```

The directory is created with mode `0700`, the file with mode `0600` (`internal/config/config.go:68-83`).

API-key resolution priority (`internal/config/config.go:173-193`):

1. `JC_API_KEY` environment variable (always plaintext).
2. Active profile's `api_key`, transparently resolving keychain references via `keychain.Resolve`.
3. Empty (no auth).

Profile selection priority:

1. `--org <profile>` flag (per-command override, no persistence).
2. `JC_PROFILE` environment variable.
3. `active_profile` in the config file.
4. `"default"`.

### Identity caveat

Both auth methods identify a **service account**, not a user. The audit log on the JumpCloud side records "the API key did this" or "the OAuth client did this" — not "Jane did this." This matters for accountability in production environments and is the motivation behind the planned step-up auth work; see [What's coming](#whats-coming).

## Authorization

> **jc has no internal authorization layer.** It is a thin client whose effective permissions are exactly what the underlying JumpCloud admin entity can do at the Console level.

There is no role projection, no per-tool RBAC, no scope enforcement inside the binary. If your API key can delete users in the Console, `jc users delete` will work. If it can't, the JumpCloud API will return a 403 and jc will surface the error.

### What does gate operations

Four mechanisms, none of them authorization in the usual sense:

| Mechanism | Layer | What it does |
|---|---|---|
| **TTY confirmation** | CLI | Prompts before destructive ops in interactive sessions; bypassed by `--force` or non-TTY stdin. |
| **`execute: true` flag** | MCP | Destructive MCP tools default to a plan response; only fire the underlying API call when `execute: true` is in the input. |
| **MCP read-only mode** | MCP server | When `--read-only` (or `mcp.read_only: true`) is set, all destructive tool handlers reject invocation with `"server is in read-only mode"`. |
| **MCP tool allow/block lists** | MCP server | Tools matching `mcp.blocked_tools` or not matching `mcp.allowed_tools` are not registered, so they don't appear in `ListTools` and direct calls fail with "tool not found". |

These provide defense-in-depth and operator hygiene. They don't authenticate the operator and they don't replace JumpCloud's RBAC.

## Destructive operations

### CLI confirmation model

`internal/cmd/confirm.go` provides the helpers `shouldConfirm()` and `mustAbortWithoutTTY()` used by every destructive command. The decision tree:

1. If `--force`, `--non-interactive`, or `defaults.confirm_destructive: false` is set → skip the prompt and proceed.
2. Else if stdin is not a TTY → **abort** rather than proceed silently. Stdin batch mode (`jc users delete --stdin`) treats `--force` as implied and confirms once for the batch instead of per-item.
3. Else → prompt; "y"/"yes" continues, anything else cancels.

Notable extra friction:

- `jc devices erase` requires both `--confirm-erase` AND interactive confirmation (`README.md`).
- `--plan` previews any mutation as a diff and never calls the API. Plan mode returns exit code 10 to distinguish from success (0) and error (1).

### MCP destructive ops

The MCP server exposes 30 tools using the `destructiveInput` pattern (`internal/mcp/tools.go:55`):

```go
type destructiveInput struct {
    Identifier string `json:"identifier"`
    Execute    bool   `json:"execute,omitempty"`
}
```

Tools include `users_delete`, `devices_erase`, `policies_delete`, `groups_user_delete`, `auth_policies_disable`, `commands_delete`, `apps_delete`, and similar across all resource types.

Without `execute: true`, the tool returns a structured plan describing what *would* happen and does not call the JumpCloud API. With `execute: true`, the call fires.

### Limitations & known gaps

- **The `execute: true` bit is agent-controlled.** A connected MCP client (Claude Desktop, Claude Code, a custom agent) can flip it itself. There is no human-in-the-loop or cryptographic proof of operator presence.
- **No step-up auth on destructive ops.** Once jc is authenticated, every operation uses the same credential.
- **No per-operator identity in the audit trail.** The audit log records *what tool was called and with what parameters*, but not *who flipped `execute: true`*.

These are tracked in [KLA-408](https://linear.app/klaassenconsulting/issue/KLA-408) (step-up auth gate for MCP-invoked destructive ops). Until that ships, the operational mitigation is to run jc as MCP server in `--read-only` mode (or with a `mcp.blocked_tools` list) by default, and only enable mutations for sessions where you are actively present.

## MCP server trust model

The MCP server is started with `jc mcp serve` (`internal/cmd/mcp.go`). Its security posture differs by transport.

### stdio (default)

```
jc mcp serve              # transport=stdio
```

- Communication is over the parent process's stdin/stdout pipes.
- **No authentication is performed by jc.** The trust boundary is "anyone who can spawn this process has its full privilege."
- This is the standard MCP transport for desktop integrations like Claude Desktop, where the client launches the server as a subprocess.
- Use this when the operator and the agent share the same shell and you trust the agent's prompt-handling chain.

### SSE (`--transport sse`)

```
jc mcp serve --transport sse                           # 127.0.0.1:8080
jc mcp serve --transport sse --addr 0.0.0.0:8080       # all interfaces
jc mcp serve --transport sse --tls-cert c.pem --tls-key k.pem
```

- HTTP server with Server-Sent Events streaming.
- **Default bind is `127.0.0.1:<port>`** — loopback only (`internal/cmd/mcp.go:175`). Binding to a non-loopback address without TLS prints an explicit warning.
- Auth is opt-in via `--require-auth`, which expects `x-api-key: <key>` or `Authorization: Bearer <key>` on every request. The key is read from the configured profile (or `JC_API_KEY`).
- CORS is opt-in via `--cors-origin <origin>` (no wildcards by default).
- TLS is opt-in via `--tls-cert` + `--tls-key`.

### Streamable HTTP (`--transport http`)

```
jc mcp serve --transport http
jc mcp serve --transport http --require-auth     # for tunnels
```

- Required transport for Claude Desktop's custom-connector mode and for browser-based MCP App rendering.
- Mounted at `/mcp` on the configured port. Loopback-only by default.
- Runs in **stateless mode** (no `Mcp-Session-Id` validation) and with **`DisableLocalhostProtection: true`** (DNS-rebinding protection off) so cloudflared tunnels and browser clients (basic-host, MCP Apps UIs) can connect. This is intentional and load-bearing for the MCP Apps feature.
- The `--require-auth` flag enables the same `x-api-key`/`Authorization: Bearer` middleware as SSE. **Always set `--require-auth` when exposing this transport via a tunnel.**
- CORS is permissive by default for the `/mcp` path so cross-origin browser clients work; tighten with `--cors-origin` when exposing publicly.

### Tool allow/block lists

Either via config:

```yaml
mcp:
  allowed_tools: ["users_*", "devices_list", "insights_*"]
  blocked_tools: ["users_delete", "devices_erase", "*_delete"]
```

…or via flags on `jc mcp tools` (preview) and `jc mcp serve`.

Semantics (`internal/mcp/toolfilter.go`):

- Block list takes precedence over allow list.
- Empty allow list = "all tools allowed except blocked".
- Patterns use Go's `filepath.Match` glob syntax (`*_delete`, `users_*`, etc.).
- Filtering happens at `addTypedTool` registration time — disallowed tools never appear in `ListTools` and direct `CallTool` returns "tool not found".

This is the cheapest defense-in-depth measure for MCP deployments. Pair it with `--read-only` for a belt-and-suspenders setup.

### Read-only mode

```
jc mcp serve --read-only
```

When the server is read-only, every destructive tool handler short-circuits with an error response *before* calling the JumpCloud API (`internal/mcp/tools.go`, e.g. `if s.readOnly { return errorResult("server is in read-only mode"), nil, nil }`). The tools remain registered (and visible in `ListTools`) so agents can still discover the surface, but invocations are rejected.

Read-only is the recommended default for any MCP deployment that doesn't explicitly need mutation tools.

### Rate limiting

A token-bucket rate limiter (`internal/mcp/server.go:357-395`) caps tool invocations at `mcp.rate_limit` calls per minute (default 60). Exceeding the cap returns an error immediately and is logged to the audit log as a failed call.

## Audit log

### Location and shape

- Path: `~/.config/jc/mcp-audit.log` (or `$JC_CONFIG`-derived equivalent).
- Format: JSON, one record per line.
- File mode `0600`, directory mode `0700`.
- Enabled by default; toggle with `mcp.audit_log: false` in config.

A record looks like (`internal/mcp/server.go:404-411`):

```json
{
  "timestamp": "2026-04-28T15:02:40Z",
  "tool": "users_update",
  "parameters": {"identifier": "alice", "department": "Eng", "execute": true},
  "success": true,
  "error": ""
}
```

Failed calls (rate-limit exceeded, validation errors, JumpCloud API errors) get `success: false` and a populated `error` field.

### Redaction

Before a record is written, parameters are walked and any of the following keys are replaced with `"****REDACTED****"` (`internal/mcp/server.go:432-458`):

| Key (snake_case) | Key (camelCase) |
|---|---|
| `password` | — |
| `api_key` | `apiKey` |
| `client_secret` | `clientSecret` |
| `shared_secret` | `sharedSecret` |
| `public_key` | `publicKey` |
| `token` | — |

The redaction is shallow (top-level keys only). Tool authors who add new sensitive fields must add them to `sensitiveParamKeys` in `internal/mcp/server.go`.

### Signed audit log (opt-in)

Enable `mcp.sign_destructive_ops: true` (or pass `--sign-destructive` to `jc mcp serve`) to emit a tamper-evident manifest to `~/.config/jc/mcp-audit-signed.log` for every successful destructive op. Each manifest carries an Ed25519 signature over the tool name, redacted args, target, timestamp, and a 32-byte nonce.

Storage:

- A per-profile keypair is generated lazily on the first signed op.
- Private key lives in the OS keychain at `service="jc"`, account `"<profile>:signing_key"`. No plaintext fallback — fail closed if the keychain is unavailable.
- Public key is persisted to config at `profiles.<name>.signing_pubkey` so verifiers can trust the chain on first use without consulting the keychain.

Verification:

```bash
jc audit verify                           # active profile, default log path
jc audit verify --profile production      # named profile
jc audit verify --pubkey <base64>         # override the configured pubkey
jc audit verify --log /path/to/audit.log  # alternate log path
```

A successful run reports the count of verified records and exits 0; any signature mismatch, truncation, or decode error exits non-zero with the offending manifest's nonce + tool so operators can grep the file.

This is detection, not prevention: a successful credential exfiltration still produces forensic record signed with the original keypair, which is hard to forge without also stealing the keychain entry. Pair it with TTY step-up (`mcp.require_step_up_for_destructive`) for in-the-loop pause + signed trail.

### Other audit-log limitations

- **No per-operator identity at the JumpCloud Console level.** The signed manifest binds an op to *the local Ed25519 keypair*, not to a specific person — multiple operators sharing one jc profile are still indistinguishable. Real per-user audit identity needs OAuth Device Flow, tracked in [KLA-414](https://linear.app/klaassenconsulting/issue/KLA-414).
- **Stdio transport carries no session metadata** — there is no client identifier in the record beyond the tool call itself.

## Threat model summary

| Scenario | What protects you today | What doesn't |
|---|---|---|
| **Laptop is online, operator is at the keyboard** | TTY confirmation, `--plan`, `--read-only` MCP mode, keychain storage | — |
| **Laptop is stolen with screen unlocked** | Credentials are in the keychain, not on disk; the OS lock screen is the boundary | jc itself does not require re-auth on resume |
| **API key exfiltrated** | Validated keys hit the org's normal API rate limits and audit log on JumpCloud; `mcp.sign_destructive_ops` produces a tamper-evident local trail | jc has no key-rotation reminder; signed manifests detect rather than prevent |
| **Compromised MCP agent prompt** | Read-only mode, tool allow/block lists, rate limiting, the `execute: true` gate, optional TTY step-up, optional signed-manifest audit trail | The agent controls `execute: true` unless step-up is enabled; signing is post-hoc detection, not prevention |
| **MCP server exposed via tunnel without `--require-auth`** | Loopback default + explicit warning when binding non-loopback without TLS | Once exposed, anyone reaching the URL has full credential privilege |
| **Plaintext config used (`--allow-plaintext`)** | Mode-`0600` config file in a `0700` dir | jc cannot prevent backups, sync clients, or `cat ~/.config/jc/config.yaml` from leaking it |

## What's coming

Active backlog tickets that close gaps called out in this document:

- **[KLA-412](https://linear.app/klaassenconsulting/issue/KLA-412) — Touch ID / WebAuthn step-up authenticator.** macOS Touch ID via `LocalAuthentication.framework` so the prompt works in stdio transport (Claude Desktop). Today's TTY step-up only works in HTTP/SSE transport with a real terminal attached.
- **[KLA-413](https://linear.app/klaassenconsulting/issue/KLA-413) — Out-of-band approval for MCP destructive ops.** Webhook-based dual-control for "delete in production" workflows.
- **[KLA-414](https://linear.app/klaassenconsulting/issue/KLA-414) — OAuth Device Flow.** Per-operator audit identity once JumpCloud exposes `/oauth2/device`. Currently platform-blocked.

## Quick reference

### Files

| Purpose | Path | Mode |
|---|---|---|
| Config | `~/.config/jc/config.yaml` | `0600` |
| Config dir | `~/.config/jc/` | `0700` |
| MCP audit log | `~/.config/jc/mcp-audit.log` | `0600` |
| API key (keychain) | service `"jc"`, account `<profile>` | OS-managed |
| Client secret (keychain) | service `"jc"`, account `<profile>:client_secret` | OS-managed |

### Environment variables

| Variable | Effect |
|---|---|
| `JC_API_KEY` | Override profile `api_key`. Always plaintext, never a keychain ref. |
| `JC_PROFILE` | Override `active_profile`. |
| `JC_ORG_ID` | Override profile `org_id`. |
| `JC_CONFIG` | Override config file path. |
| `JC_NO_COLOR` | Disable color output. |

### Default ports & endpoints

| Endpoint | Default |
|---|---|
| MCP SSE / Streamable HTTP | `127.0.0.1:8080` (loopback) |
| OAuth token endpoint | `https://admin-oauth.id.jumpcloud.com/oauth2/token` |
| API key validation | `GET https://console.jumpcloud.com/api/organizations` |

### Useful commands

```bash
jc auth login                         # API key flow (default)
jc auth login --service-account       # OAuth client credentials flow
jc auth login --allow-plaintext       # opt in to plaintext config storage
jc auth status                        # show active profile, auth method, expiry
jc auth logout                        # remove credential from keychain + config

jc mcp serve --read-only              # safest MCP server posture
jc mcp serve --transport http --require-auth   # exposing via tunnel

jc mcp tools                          # preview the tool set after allow/block
jc mcp tools --read-only              # preview the read-only-mode subset
```

---

If anything here drifts from the code, the code is the source of truth — please file a follow-up that updates both.
