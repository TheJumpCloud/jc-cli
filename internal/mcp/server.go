// Package mcp provides the MCP (Model Context Protocol) server for jc.
// It wraps the CLI infrastructure to expose JumpCloud operations as MCP tools
// and resources over stdio and SSE transports.
package mcp

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
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

	mu       sync.Mutex
	listener net.Listener // SSE listener, set during RunSSE
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

// SSEConfig configures the SSE HTTP server.
type SSEConfig struct {
	// Addr is the listen address (e.g., ":8080").
	Addr string
	// CORSOrigin is the allowed CORS origin. Empty means no CORS headers.
	CORSOrigin string
	// TLSCert is the path to the TLS certificate file.
	TLSCert string
	// TLSKey is the path to the TLS private key file.
	TLSKey string
	// APIKey is the required API key for authentication. Empty means no auth.
	APIKey string
}

// RunSSE starts the MCP server as an HTTP+SSE server. It blocks until the
// context is cancelled or the server encounters a fatal error.
func (s *Server) RunSSE(ctx context.Context, cfg SSEConfig) error {
	defer s.auditLog.close()

	handler := mcp.NewSSEHandler(func(r *http.Request) *mcp.Server {
		return s.mcpServer
	}, nil)

	// Wrap with auth middleware if an API key is configured.
	var h http.Handler = handler
	if cfg.APIKey != "" {
		h = s.authMiddleware(cfg.APIKey, h)
	}

	// Wrap with CORS middleware if configured.
	if cfg.CORSOrigin != "" {
		h = corsMiddleware(cfg.CORSOrigin, h)
	}

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: h,
	}

	// Configure TLS if cert and key are provided.
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return fmt.Errorf("loading TLS certificate: %w", err)
		}
		srv.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
	}

	// Listen on the configured address.
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}

	// Store the listener for test access.
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	// Start graceful shutdown goroutine.
	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	if srv.TLSConfig != nil {
		tlsLn := tls.NewListener(ln, srv.TLSConfig)
		err = srv.Serve(tlsLn)
	} else {
		err = srv.Serve(ln)
	}

	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Listener returns the active SSE listener address, or nil if not running.
func (s *Server) Listener() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr()
	}
	return nil
}

// authMiddleware returns an HTTP handler that validates the API key from
// x-api-key header or Authorization: Bearer header.
func (s *Server) authMiddleware(apiKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("x-api-key")
		if key == "" {
			auth := r.Header.Get("Authorization")
			if len(auth) > 7 && auth[:7] == "Bearer " {
				key = auth[7:]
			}
		}
		if key != apiKey {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware adds CORS headers to HTTP responses.
func corsMiddleware(origin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, x-api-key, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
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
