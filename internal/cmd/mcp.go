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
	"github.com/spf13/cobra"
)

func newMcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP (Model Context Protocol) server for AI integration",
		Long: `Start an MCP server that exposes JumpCloud operations as tools and resources
for AI assistants like Claude Desktop and Claude Code.

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
		rateLimit  int
		readOnly   bool
		transport  string
		addr       string
		port       int
		corsOrigin string
		tlsCert    string
		tlsKey     string
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

Security: the http transport is stateless and permissive by default (wide-open
CORS, cross-origin checks disabled) so browser-based MCP clients like basic-host
and MCP Apps UIs can connect. When exposing the server via a tunnel (cloudflared,
ngrok, etc.), configure an API key — via 'jc auth login', the JC_API_KEY env
var, or the --api-key global flag — so the auth middleware rejects
unauthenticated tool calls from anyone who discovers the URL.

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
			return runMcpServe(rateLimit, readOnly, transport, addr, port, corsOrigin, tlsCert, tlsKey)
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
	server := mcp.NewServer(mcp.Options{
		RateLimit:    60,
		ReadOnly:     readOnly,
		AllowedTools: config.MCPAllowedTools(),
		BlockedTools: config.MCPBlockedTools(),
	})

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

func runMcpServe(rateLimit int, readOnly bool, transport, addr string, port int, corsOrigin, tlsCert, tlsKey string) error {
	server := mcp.NewServer(mcp.Options{
		RateLimit:    rateLimit,
		ReadOnly:     readOnly,
		AuditEnabled: config.MCPAuditLog(),
		AllowedTools: config.MCPAllowedTools(),
		BlockedTools: config.MCPBlockedTools(),
	})

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
		// Streamable HTTP transport for Claude Desktop custom connectors and MCP Apps.
		// Auth is optional here (unlike sse) so browser-based MCP clients like
		// basic-host can connect during local development. When the operator has
		// configured an API key (via `jc auth login`, JC_API_KEY, or --api-key),
		// we pass it through so the server's auth middleware rejects unauth'd
		// calls — critical when exposing via a cloudflared tunnel.
		listenAddr := resolveSSEAddr(addr, port)
		apiKey := config.APIKey()

		scheme := "http"
		fmt.Fprintf(os.Stderr, "jc: starting MCP server on Streamable HTTP transport at %s://%s/mcp\n", scheme, listenAddr)
		if apiKey == "" {
			// Warn if binding beyond loopback without auth — that's the
			// combination a tunnel creates, too.
			if host, _, err := net.SplitHostPort(listenAddr); err == nil {
				if host != "127.0.0.1" && host != "::1" && host != "localhost" {
					fmt.Fprintln(os.Stderr, "jc: WARNING: HTTP transport running without an API key. Anyone who reaches the server can call all tools.")
				}
			}
		} else {
			fmt.Fprintln(os.Stderr, "jc: HTTP transport requires x-api-key or Authorization: Bearer header for all requests.")
		}

		return server.RunStreamableHTTP(ctx, mcp.SSEConfig{
			Addr:       listenAddr,
			CORSOrigin: "*",
			APIKey:     apiKey,
		})

	default:
		return fmt.Errorf("unknown transport %q: must be 'stdio', 'sse', or 'http'", transport)
	}
}
