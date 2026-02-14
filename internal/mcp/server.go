// Package mcp provides the MCP (Model Context Protocol) server for jc.
// It wraps the CLI infrastructure to expose JumpCloud operations as MCP tools
// and resources over stdio transport.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server wraps the MCP SDK server with rate limiting and audit logging.
type Server struct {
	mcpServer *mcp.Server
	limiter   *rateLimiter
	auditLog  *auditLogger
	readOnly  bool
}

// Options configures the MCP server.
type Options struct {
	// RateLimit is the maximum tool calls per minute. 0 means default (60).
	RateLimit int
	// ReadOnly disables all mutation tools.
	ReadOnly bool
	// AuditLogPath overrides the default audit log path.
	AuditLogPath string
}

// nowFunc is overridable for tests.
var nowFunc = time.Now

// NewServer creates a new MCP server with the given options.
func NewServer(opts Options) *Server {
	if opts.RateLimit <= 0 {
		opts.RateLimit = 60
	}

	auditPath := opts.AuditLogPath
	if auditPath == "" {
		auditPath = filepath.Join(config.ConfigDir(), "mcp-audit.log")
	}

	// Create slog logger that writes to stderr (not stdout, which is the JSON-RPC stream).
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	mcpServer := mcp.NewServer(
		&mcp.Implementation{
			Name:    "jc",
			Version: version.Number,
		},
		&mcp.ServerOptions{
			Instructions: "JumpCloud CLI MCP server. Manage users, devices, groups, policies, commands, and more.",
			Logger:       logger,
		},
	)

	al := newAuditLogger(auditPath)

	s := &Server{
		mcpServer: mcpServer,
		limiter:   newRateLimiter(opts.RateLimit),
		auditLog:  al,
		readOnly:  opts.ReadOnly,
	}

	s.registerTools()
	s.registerResources()
	s.registerPrompts()

	return s
}

// Run starts the MCP server on stdio transport. It blocks until the client
// disconnects or the context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	defer s.auditLog.close()
	return s.mcpServer.Run(ctx, &mcp.StdioTransport{})
}

// RunWithTransport starts the MCP server with a custom transport (for testing).
func (s *Server) RunWithTransport(ctx context.Context, t mcp.Transport) error {
	defer s.auditLog.close()
	return s.mcpServer.Run(ctx, t)
}

// MCPServer returns the underlying MCP server (for testing).
func (s *Server) MCPServer() *mcp.Server {
	return s.mcpServer
}

// rateLimiter implements a token-bucket rate limiter for tool calls.
type rateLimiter struct {
	mu         sync.Mutex
	maxPerMin  int
	timestamps []time.Time
}

func newRateLimiter(maxPerMin int) *rateLimiter {
	return &rateLimiter{
		maxPerMin:  maxPerMin,
		timestamps: make([]time.Time, 0, maxPerMin),
	}
}

// allow checks if a tool call is allowed under the rate limit.
// Returns an error if the rate limit is exceeded.
func (rl *rateLimiter) allow() error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := nowFunc()
	cutoff := now.Add(-time.Minute)

	// Prune old timestamps.
	pruned := rl.timestamps[:0]
	for _, ts := range rl.timestamps {
		if ts.After(cutoff) {
			pruned = append(pruned, ts)
		}
	}
	rl.timestamps = pruned

	if len(rl.timestamps) >= rl.maxPerMin {
		return fmt.Errorf("rate limit exceeded: %d calls/minute", rl.maxPerMin)
	}

	rl.timestamps = append(rl.timestamps, now)
	return nil
}

// auditLogger writes tool call records to a log file.
type auditLogger struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
}

// auditEntry represents a single tool call in the audit log.
type auditEntry struct {
	Timestamp  string          `json:"timestamp"`
	Tool       string          `json:"tool"`
	Parameters json.RawMessage `json:"parameters"`
	Success    bool            `json:"success"`
	Error      string          `json:"error,omitempty"`
}

func newAuditLogger(path string) *auditLogger {
	al := &auditLogger{}
	// Ensure directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create audit log directory: %v\n", err)
		return al
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not open audit log: %v\n", err)
		return al
	}
	al.file = f
	al.enc = json.NewEncoder(f)
	return al
}

func (al *auditLogger) log(tool string, params json.RawMessage, success bool, errMsg string) {
	al.mu.Lock()
	defer al.mu.Unlock()
	if al.enc == nil {
		return
	}
	_ = al.enc.Encode(auditEntry{
		Timestamp:  nowFunc().UTC().Format(time.RFC3339),
		Tool:       tool,
		Parameters: params,
		Success:    success,
		Error:      errMsg,
	})
}

func (al *auditLogger) close() {
	al.mu.Lock()
	defer al.mu.Unlock()
	if al.file != nil {
		al.file.Close()
		al.file = nil
		al.enc = nil
	}
}
