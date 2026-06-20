package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/mcp"
	"github.com/klaassen-consulting/jc/internal/recipe"
	"github.com/spf13/cobra"
)

func newMcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP (Model Context Protocol) server for AI integration",
		Long: `Start an MCP server that exposes JumpCloud operations as tools and resources
for AI assistants like Claude Desktop and Claude Code.

205 tools cover the full JumpCloud surface plus a dedicated Apple MDM
payloads catalog (apple_mdm_payloads_search / _show / _template /
_create_policy). The catalog lets an agent map a natural-language MDM
intent — "disable AirDrop on iPads", "enforce FileVault on Macs" — to
one of Apple's vendored schemas (com.apple.applicationaccess,
com.apple.security.firewall, …) and create a JumpCloud Custom MDM
Configuration Profile from it in one tool call. The create_policy tool
routes through the step-up auth gate (Touch ID / TTY / webhook) before
any POST to JumpCloud.

Configure in Claude Desktop:
  {
    "mcpServers": {
      "jc": {
        "command": "jc",
        "args": ["mcp", "serve"]
      }
    }
  }`,
	}

	cmd.AddCommand(newMcpServeCmd())
	cmd.AddCommand(newMcpToolsCmd())
	return cmd
}

func newMcpServeCmd() *cobra.Command {
	var (
		rateLimit          int
		readOnly           bool
		transport          string
		addr               string
		port               int
		corsOrigin         string
		requireAuth        bool
		requireStepUp      bool
		stepUpAuth         string
		signDestructiveOps bool
		tlsCert            string
		tlsKey             string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start MCP server",
		Long: `Start an MCP server using the specified transport.

Transports:
  stdio (default)  Communicates over stdin/stdout using JSON-RPC 2.0.
                   Used by Claude Desktop and Claude Code.

  sse              Starts an HTTP server with Server-Sent Events transport.
                   Accessible by remote MCP clients over the network.
                   Authentication required via x-api-key or Bearer token header.

The server reuses the CLI's authentication, API clients, caching, and
resolution engine. All tool calls are rate-limited and logged to
~/.config/jc/mcp-audit.log.

Configuration can be set in config.yaml under the 'mcp' section:
  mcp:
    rate_limit: 60
    read_only: false
    audit_log: true
    plan_first: true
    sse_port: 8080
    allowed_tools: []
    blocked_tools: []

Tool Allow/Block Lists:
  Use allowed_tools and blocked_tools to control which tools are available.
  Patterns use glob-style matching (e.g., "users_*", "devices_erase").
  Block list takes precedence over allow list.
  Use 'jc mcp tools' to see which tools are available after filtering.

SSE Examples:
  jc mcp serve --transport sse
  jc mcp serve --transport sse --port 9090
  jc mcp serve --transport sse --addr 0.0.0.0:8080 --tls-cert cert.pem --tls-key key.pem
  jc mcp serve --transport sse --cors-origin "https://app.example.com"

Streamable HTTP Examples (for Claude Desktop custom connectors and MCP Apps):
  jc mcp serve --transport http
  jc mcp serve --transport http --port 8090
  jc mcp serve --transport http --require-auth      (for tunnels)

Security: the http transport is stateless and permissive by default (wide-open
CORS, cross-origin checks disabled) so browser-based MCP clients like basic-host
and MCP Apps UIs can connect without friction. When exposing the server via a
tunnel (cloudflared, ngrok, etc.), add --require-auth to enforce that every
request carries the configured JumpCloud API key via x-api-key or
Authorization: Bearer. --require-auth reads the key from 'jc auth login',
JC_API_KEY, or the --api-key global flag, and refuses to start without one.

Use JC_PROFILE environment variable to select which JumpCloud org to use.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Use config values as defaults; CLI flags override.
			if !cmd.Flags().Changed("rate-limit") {
				rateLimit = config.MCPRateLimit()
			}
			if !cmd.Flags().Changed("read-only") {
				readOnly = config.MCPReadOnly()
			}
			if !cmd.Flags().Changed("port") {
				port = config.MCPSSEPort()
			}
			if !cmd.Flags().Changed("require-step-up") {
				requireStepUp = config.MCPRequireStepUp()
			}
			if !cmd.Flags().Changed("step-up-authenticator") {
				stepUpAuth = config.MCPStepUpAuthenticator()
			}
			if !cmd.Flags().Changed("sign-destructive") {
				signDestructiveOps = config.MCPSignDestructiveOps()
			}

			// Profile-role enforcement: a profile bound to a read-only OAuth
			// client must not advertise mutation tools. Reject the start
			// rather than silently coercing — operators who passed
			// --read-only=false should see the error, not a phantom override.
			coerced, warning, err := applyProfileRole(
				config.ActiveProfile(),
				config.IsReadOnlyProfile(),
				cmd.Flags().Changed("read-only"),
				readOnly,
			)
			if err != nil {
				return err
			}
			readOnly = coerced
			if warning != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), warning)
			}

			// Stdio transport hands stdin/stdout to the JSON-RPC stream;
			// a TTY-based step-up prompt has nowhere to go. Skip the
			// warning if the resolved authenticator runs out-of-band of
			// the transport — on darwin, Touch ID is an OS modal that
			// reaches the operator regardless of stdin.
			if requireStepUp && transport == "stdio" && !mcp.StepUpReachesOperatorOnStdio(stepUpAuth) {
				fmt.Fprintln(cmd.ErrOrStderr(),
					"jc: --require-step-up is set but transport is stdio; TTY prompts cannot reach the operator. "+
						"Destructive calls will be rejected as 'step-up unavailable'. Use --transport http with a terminal "+
						"session, or set --step-up-authenticator=touchid on macOS (KLA-412), or wait for the out-of-band "+
						"authenticator in KLA-413.")
			}
			return runMcpServe(rateLimit, readOnly, transport, addr, port, corsOrigin, tlsCert, tlsKey, requireAuth, requireStepUp, stepUpAuth, signDestructiveOps)
		},
	}

	cmd.Flags().IntVar(&rateLimit, "rate-limit", 60, "Maximum tool calls per minute")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "Disable all mutation tools")
	cmd.Flags().StringVar(&transport, "transport", "stdio", "Transport type: stdio, sse, or http")
	cmd.Flags().StringVar(&addr, "addr", "", "Listen address for SSE transport (default 127.0.0.1:<port>)")
	cmd.Flags().IntVar(&port, "port", 8080, "Port for SSE transport (default 8080)")
	cmd.Flags().StringVar(&corsOrigin, "cors-origin", "", "Allowed CORS origin for SSE transport")
	cmd.Flags().StringVar(&tlsCert, "tls-cert", "", "TLS certificate file for SSE transport")
	cmd.Flags().StringVar(&tlsKey, "tls-key", "", "TLS private key file for SSE transport")
	cmd.Flags().BoolVar(&requireAuth, "require-auth", false, "Require x-api-key / Authorization Bearer on every request (http transport). Off by default so local browser clients like basic-host can connect. Turn on when exposing via a tunnel.")
	cmd.Flags().BoolVar(&requireStepUp, "require-step-up", false, "Require step-up auth before any destructive tool with execute=true fires. On macOS, the default authenticator is Touch ID (works in stdio transport too); on other platforms it's a TTY API-key prompt that needs a controlling terminal. Defaults to mcp.require_step_up_for_destructive in config.")
	cmd.Flags().StringVar(&stepUpAuth, "step-up-authenticator", "auto", "Which step-up channel to use when --require-step-up is set: 'auto' (Touch ID on macOS, TTY elsewhere), 'tty' (force API-key prompt), 'touchid' (macOS biometric, falls back to TTY if unavailable). Defaults to mcp.step_up_authenticator in config.")
	cmd.Flags().BoolVar(&signDestructiveOps, "sign-destructive", false, "Append a signed Ed25519 manifest to ~/.config/jc/mcp-audit-signed.log for every successful destructive op. Generates a per-profile keypair on first use; private key in keychain, pubkey in config. Verify chain with 'jc audit verify'. Defaults to mcp.sign_destructive_ops in config.")

	return cmd
}

func newMcpToolsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tools",
		Short: "List available MCP tools",
		Long: `List all MCP tools that would be available when starting the server.
Respects the allow/block list configuration in config.yaml:

  mcp:
    allowed_tools: ["users_*", "devices_list"]
    blocked_tools: ["devices_erase"]

Use --read-only to additionally see which tools survive read-only mode.

Tool names use underscore-separated resource_verb format (e.g., users_list,
devices_get, groups_add_member).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			readOnly, _ := cmd.Flags().GetBool("read-only")
			return runMcpTools(cmd, readOnly)
		},
	}
}

func runMcpTools(cmd *cobra.Command, readOnly bool) error {
	server, err := mcp.NewServer(mcp.Options{
		RateLimit:    60,
		ReadOnly:     readOnly,
		AllowedTools: config.MCPAllowedTools(),
		BlockedTools: config.MCPBlockedTools(),
	})
	if err != nil {
		return fmt.Errorf("creating MCP server: %w", err)
	}

	tools := server.ListToolNames()
	for _, name := range tools {
		fmt.Fprintln(cmd.OutOrStdout(), name)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "── %d tools ──\n", len(tools))
	return nil
}

// resolveSSEAddr returns the listen address for the SSE server.
// If addr is empty, it defaults to loopback (127.0.0.1:<port>).
// If addr is explicitly set, it is returned as-is.
func resolveSSEAddr(addr string, port int) string {
	if addr == "" {
		return fmt.Sprintf("127.0.0.1:%d", port)
	}
	return addr
}

// applyProfileRole reconciles the --read-only flag with the active
// profile's role. Returns the effective read-only value, an optional
// warning message to surface to the operator, and an error if the flag
// and profile role conflict.
//
// Rules:
//   - Profile is not read-only → pass through unchanged.
//   - Profile is read-only and operator passed --read-only=false → error.
//   - Profile is read-only and read-only is already true → no warning,
//     no change (operator and profile agree).
//   - Profile is read-only and read-only is false but flag wasn't
//     explicitly set → coerce to true and emit a warning.
func applyProfileRole(activeProfile string, profileReadOnly, flagChanged, readOnly bool) (bool, string, error) {
	if !profileReadOnly {
		return readOnly, "", nil
	}
	if flagChanged && !readOnly {
		return false, "", fmt.Errorf("active profile %q is read-only; --read-only=false is incompatible", activeProfile)
	}
	if readOnly {
		return true, "", nil
	}
	return true, fmt.Sprintf("Profile %q is read-only — forcing --read-only and rejecting destructive tools.", activeProfile), nil
}

func runMcpServe(rateLimit int, readOnly bool, transport, addr string, port int, corsOrigin, tlsCert, tlsKey string, requireAuth, requireStepUp bool, stepUpAuth string, signDestructiveOps bool) error {
	// Step-up auth's API key dependency is authenticator-specific: only
	// the TTY authenticator (and the auto / touchid paths that fall back
	// to TTY when biometric hardware is missing) actually derives the
	// challenge answer from it. Webhook + real Touch ID never read it.
	// Bugbot caught the unconditional read on PR #34 — the old guard
	// failed webhook operators with a misleading "to derive the
	// challenge answer" error that didn't apply to their channel.
	var stepUpAPIKey string
	if requireStepUp && mcp.StepUpNeedsAPIKey(stepUpAuth) {
		stepUpAPIKey = config.APIKey()
		if stepUpAPIKey == "" {
			return fmt.Errorf("--require-step-up with the TTY step-up channel needs an API key to derive the challenge answer. Run 'jc auth login' or set JC_API_KEY, pick --step-up-authenticator=touchid (macOS Touch ID) or =webhook (out-of-band approval), or drop --require-step-up")
		}
	}

	// Wire the recipe dispatcher so the recipe_run tool can actually
	// execute (Execute: true) and the recipe_runner_view MCP App can
	// drive end-to-end runs. Mirrors the TUI wiring at
	// `screen.RecipeDispatcher = recipe.NewDispatcher(...)`. Plan-mode
	// (Execute: false) doesn't need this, so the assignment is safe
	// even on servers that never run recipes.
	mcp.RecipeDispatcher = recipe.NewDispatcher(newRootCmdForRecipeStep)

	server, err := mcp.NewServer(mcp.Options{
		RateLimit:            rateLimit,
		ReadOnly:             readOnly,
		AuditEnabled:         config.MCPAuditLog(),
		AllowedTools:         config.MCPAllowedTools(),
		BlockedTools:         config.MCPBlockedTools(),
		RequireStepUp:        requireStepUp,
		StepUpAPIKey:         stepUpAPIKey,
		StepUpAuthenticator:  stepUpAuth,
		ApprovalWebhookURL:   config.MCPApprovalWebhookURL(),
		ApprovalCallbackAddr: config.MCPApprovalCallbackAddr(),
		ApprovalTimeout:      config.MCPApprovalTimeout(),
		SignDestructiveOps:   signDestructiveOps,
		SigningProfile:       config.ActiveProfile(),
	})
	if err != nil {
		return fmt.Errorf("starting MCP server: %w", err)
	}

	// Handle graceful shutdown on Ctrl+C / SIGTERM.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "jc: shutting down MCP server")
		cancel()
	}()

	switch transport {
	case "stdio":
		fmt.Fprintln(os.Stderr, "jc: starting MCP server on stdio transport")
		return server.Run(ctx)

	case "sse":
		// SSE transport requires authentication for security.
		apiKey := config.APIKey()
		if apiKey == "" {
			return fmt.Errorf("SSE transport requires authentication. Run 'jc auth login' or set JC_API_KEY")
		}

		listenAddr := resolveSSEAddr(addr, port)

		// Warn if binding to a non-loopback address without TLS.
		if host, _, err := net.SplitHostPort(listenAddr); err == nil {
			if host != "127.0.0.1" && host != "::1" && host != "localhost" && tlsCert == "" {
				fmt.Fprintln(os.Stderr, "jc: WARNING: listening on non-loopback address without TLS. Credentials will be sent in plaintext.")
			}
		}

		scheme := "http"
		if tlsCert != "" {
			scheme = "https"
		}
		fmt.Fprintf(os.Stderr, "jc: starting MCP server on SSE transport at %s://%s\n", scheme, listenAddr)

		return server.RunSSE(ctx, mcp.SSEConfig{
			Addr:       listenAddr,
			CORSOrigin: corsOrigin,
			TLSCert:    tlsCert,
			TLSKey:     tlsKey,
			APIKey:     apiKey,
		})

	case "http":
		// Streamable HTTP transport for Claude Desktop custom connectors and
		// MCP Apps. Auth is off by default so browser-based clients like
		// basic-host can connect for local dev. Enable with --require-auth
		// when exposing via a tunnel; the server then requires x-api-key or
		// Authorization: Bearer matching the configured JumpCloud API key.
		listenAddr := resolveSSEAddr(addr, port)

		var httpAPIKey string
		if requireAuth {
			httpAPIKey = config.APIKey()
			if httpAPIKey == "" {
				return fmt.Errorf("--require-auth needs an API key to enforce. Run 'jc auth login' or set JC_API_KEY, or drop --require-auth for local dev")
			}
		}

		fmt.Fprintf(os.Stderr, "jc: starting MCP server on Streamable HTTP transport at http://%s/mcp\n", listenAddr)
		if httpAPIKey != "" {
			fmt.Fprintln(os.Stderr, "jc: auth REQUIRED (x-api-key or Authorization: Bearer).")
		} else {
			// Warn on non-loopback binds without auth — effectively public.
			if host, _, err := net.SplitHostPort(listenAddr); err == nil {
				if host != "127.0.0.1" && host != "::1" && host != "localhost" {
					fmt.Fprintln(os.Stderr, "jc: WARNING: bound to a non-loopback address without --require-auth. Anyone who reaches the server can call all tools.")
				}
			}
		}

		return server.RunStreamableHTTP(ctx, mcp.SSEConfig{
			Addr:       listenAddr,
			CORSOrigin: "*",
			APIKey:     httpAPIKey,
		})

	default:
		return fmt.Errorf("unknown transport %q: must be 'stdio', 'sse', or 'http'", transport)
	}
}
