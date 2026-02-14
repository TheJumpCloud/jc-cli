package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
	return cmd
}

func newMcpServeCmd() *cobra.Command {
	var (
		rateLimit int
		readOnly  bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start MCP server on stdio transport",
		Long: `Start an MCP server that communicates over stdin/stdout using JSON-RPC 2.0.

The server reuses the CLI's authentication, API clients, caching, and
resolution engine. All tool calls are rate-limited and logged to
~/.config/jc/mcp-audit.log.

Use JC_PROFILE environment variable to select which JumpCloud org to use.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMcpServe(rateLimit, readOnly)
		},
	}

	cmd.Flags().IntVar(&rateLimit, "rate-limit", 60, "Maximum tool calls per minute")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "Disable all mutation tools")

	return cmd
}

func runMcpServe(rateLimit int, readOnly bool) error {
	// Log to stderr so we don't corrupt the JSON-RPC stream on stdout.
	fmt.Fprintln(os.Stderr, "jc: starting MCP server on stdio transport")

	server := mcp.NewServer(mcp.Options{
		RateLimit: rateLimit,
		ReadOnly:  readOnly,
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

	return server.Run(ctx)
}
