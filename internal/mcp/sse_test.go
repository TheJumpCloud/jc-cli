package mcp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// startSSEServer starts an SSE MCP server on a random port and returns the
// server and base URL. The server is stopped when the test finishes.
func startSSEServer(t *testing.T, cfg SSEConfig) (*Server, string) {
	t.Helper()
	setupTest(t)

	if cfg.Addr == "" {
		cfg.Addr = ":0" // random port
	}

	opts := Options{
		RateLimit:    60,
		AuditLogPath: filepath.Join(t.TempDir(), "audit.log"),
	}
	server := MustNewServer(opts)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.RunSSE(ctx, cfg)
	}()

	// Wait for server to start listening.
	var addr net.Addr
	for i := 0; i < 50; i++ {
		addr = server.Listener()
		if addr != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if addr == nil {
		t.Fatal("SSE server did not start listening")
	}

	baseURL := "http://" + addr.String()
	return server, baseURL
}

func TestSSE_ServerStartsAndAcceptsConnections(t *testing.T) {
	server, baseURL := startSSEServer(t, SSEConfig{
		APIKey: "test-key",
	})
	_ = server

	// Connect via SSE client transport.
	transport := &mcp.SSEClientTransport{
		Endpoint: baseURL,
		HTTPClient: &http.Client{
			Transport: &apiKeyTransport{key: "test-key"},
		},
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-sse-client",
		Version: "1.0",
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	defer cs.Close()

	// Verify we can call tools.
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "jc_ping"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatal("expected jc_ping to succeed")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if !strings.Contains(tc.Text, "running") {
		t.Errorf("expected 'running' in ping response, got %q", tc.Text)
	}
}

func TestSSE_ListToolsWorks(t *testing.T) {
	_, baseURL := startSSEServer(t, SSEConfig{
		APIKey: "test-key",
	})

	transport := &mcp.SSEClientTransport{
		Endpoint: baseURL,
		HTTPClient: &http.Client{
			Transport: &apiKeyTransport{key: "test-key"},
		},
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-sse-client",
		Version: "1.0",
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	defer cs.Close()

	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(result.Tools) == 0 {
		t.Fatal("expected at least one tool")
	}

	var foundPing bool
	for _, tool := range result.Tools {
		if tool.Name == "jc_ping" {
			foundPing = true
		}
	}
	if !foundPing {
		t.Error("expected jc_ping tool")
	}
}

func TestSSE_ListResourcesWorks(t *testing.T) {
	_, baseURL := startSSEServer(t, SSEConfig{
		APIKey: "test-key",
	})

	transport := &mcp.SSEClientTransport{
		Endpoint: baseURL,
		HTTPClient: &http.Client{
			Transport: &apiKeyTransport{key: "test-key"},
		},
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-sse-client",
		Version: "1.0",
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	defer cs.Close()

	result, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(result.Resources) < 20 {
		t.Fatalf("expected at least 20 resources, got %d", len(result.Resources))
	}
}

func TestSSE_AuthRejectsUnauthenticated(t *testing.T) {
	_, baseURL := startSSEServer(t, SSEConfig{
		APIKey: "test-key",
	})

	// Try connecting without API key.
	transport := &mcp.SSEClientTransport{
		Endpoint:   baseURL,
		HTTPClient: http.DefaultClient,
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0",
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Connect(ctx, transport, nil)
	if err == nil {
		t.Fatal("expected connection to fail without API key")
	}
	if !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("expected Unauthorized error, got: %v", err)
	}
}

func TestSSE_AuthRejectsWrongKey(t *testing.T) {
	_, baseURL := startSSEServer(t, SSEConfig{
		APIKey: "correct-key",
	})

	transport := &mcp.SSEClientTransport{
		Endpoint: baseURL,
		HTTPClient: &http.Client{
			Transport: &apiKeyTransport{key: "wrong-key"},
		},
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0",
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Connect(ctx, transport, nil)
	if err == nil {
		t.Fatal("expected connection to fail with wrong API key")
	}
	if !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("expected Unauthorized error, got: %v", err)
	}
}

func TestSSE_AuthAcceptsBearerToken(t *testing.T) {
	_, baseURL := startSSEServer(t, SSEConfig{
		APIKey: "test-key",
	})

	transport := &mcp.SSEClientTransport{
		Endpoint: baseURL,
		HTTPClient: &http.Client{
			Transport: &bearerTransport{token: "test-key"},
		},
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0",
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("SSE connect with Bearer: %v", err)
	}
	defer cs.Close()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "jc_ping"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatal("expected ping to succeed with Bearer auth")
	}
}

func TestSSE_NoAuthRequired(t *testing.T) {
	// When APIKey is empty, no auth is required.
	_, baseURL := startSSEServer(t, SSEConfig{
		APIKey: "",
	})

	transport := &mcp.SSEClientTransport{
		Endpoint:   baseURL,
		HTTPClient: http.DefaultClient,
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0",
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("SSE connect without auth: %v", err)
	}
	defer cs.Close()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "jc_ping"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatal("expected ping to succeed without auth")
	}
}

func TestSSE_CORSHeaders(t *testing.T) {
	_, baseURL := startSSEServer(t, SSEConfig{
		APIKey:     "test-key",
		CORSOrigin: "https://app.example.com",
	})

	// Make a direct HTTP OPTIONS request to check CORS headers.
	req, _ := http.NewRequest(http.MethodOptions, baseURL, nil)
	req.Header.Set("x-api-key", "test-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204 for OPTIONS, got %d", resp.StatusCode)
	}
	origin := resp.Header.Get("Access-Control-Allow-Origin")
	if origin != "https://app.example.com" {
		t.Errorf("expected CORS origin 'https://app.example.com', got %q", origin)
	}
	methods := resp.Header.Get("Access-Control-Allow-Methods")
	if !strings.Contains(methods, "POST") || !strings.Contains(methods, "GET") {
		t.Errorf("expected CORS methods to include GET and POST, got %q", methods)
	}
	headers := resp.Header.Get("Access-Control-Allow-Headers")
	// Server sends "*" so all custom MCP headers (Mcp-Session-Id,
	// Mcp-Protocol-Version, Last-Event-ID, x-api-key, etc.) are allowed
	// without having to enumerate them and chase SDK additions.
	if !strings.Contains(headers, "*") && !strings.Contains(headers, "x-api-key") {
		t.Errorf("expected CORS headers to be wildcard or include x-api-key, got %q", headers)
	}
	if !strings.Contains(headers, "*") && !strings.Contains(headers, "Mcp-Session-Id") {
		t.Errorf("expected CORS headers to be wildcard or include Mcp-Session-Id, got %q", headers)
	}
	exposed := resp.Header.Get("Access-Control-Expose-Headers")
	if !strings.Contains(exposed, "Mcp-Session-Id") {
		t.Errorf("expected Mcp-Session-Id in Access-Control-Expose-Headers so clients can read it from the initialize response, got %q", exposed)
	}
}

func TestSSE_NoCORSHeadersWhenNotConfigured(t *testing.T) {
	_, baseURL := startSSEServer(t, SSEConfig{
		APIKey: "test-key",
	})

	req, _ := http.NewRequest(http.MethodGet, baseURL, nil)
	req.Header.Set("x-api-key", "test-key")
	req.Header.Set("Accept", "text/event-stream")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Context timeout is expected since SSE is a long-lived connection.
		return
	}
	defer resp.Body.Close()

	origin := resp.Header.Get("Access-Control-Allow-Origin")
	if origin != "" {
		t.Errorf("expected no CORS origin header, got %q", origin)
	}
}

func TestSSE_TLSSupport(t *testing.T) {
	// Generate self-signed certificate.
	certFile, keyFile := generateTestCert(t)

	_, baseURL := startSSEServer(t, SSEConfig{
		APIKey:  "test-key",
		TLSCert: certFile,
		TLSKey:  keyFile,
	})
	// Replace http:// with https:// since TLS is enabled.
	baseURL = strings.Replace(baseURL, "http://", "https://", 1)

	// Create TLS client that trusts our self-signed cert.
	transport := &mcp.SSEClientTransport{
		Endpoint: baseURL,
		HTTPClient: &http.Client{
			Transport: &apiKeyTLSTransport{
				key: "test-key",
			},
		},
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-tls-client",
		Version: "1.0",
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("SSE TLS connect: %v", err)
	}
	defer cs.Close()

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "jc_ping"})
	if err != nil {
		t.Fatalf("CallTool over TLS: %v", err)
	}
	if result.IsError {
		t.Fatal("expected ping to succeed over TLS")
	}
}

func TestSSE_GracefulShutdown(t *testing.T) {
	setupTest(t)

	cfg := SSEConfig{
		Addr:   ":0",
		APIKey: "test-key",
	}
	opts := Options{
		RateLimit:    60,
		AuditLogPath: filepath.Join(t.TempDir(), "audit.log"),
	}
	server := MustNewServer(opts)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.RunSSE(ctx, cfg)
	}()

	// Wait for server to start.
	for i := 0; i < 50; i++ {
		if server.Listener() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if server.Listener() == nil {
		t.Fatal("server did not start")
	}

	// Cancel context to trigger shutdown.
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected nil error on graceful shutdown, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within 5 seconds")
	}
}

func TestSSE_ReadOnlyMode(t *testing.T) {
	setupTest(t)

	cfg := SSEConfig{
		Addr:   ":0",
		APIKey: "test-key",
	}
	opts := Options{
		RateLimit:    60,
		ReadOnly:     true,
		AuditLogPath: filepath.Join(t.TempDir(), "audit.log"),
	}
	server := MustNewServer(opts)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go server.RunSSE(ctx, cfg)

	// Wait for server to start.
	for i := 0; i < 50; i++ {
		if server.Listener() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	baseURL := "http://" + server.Listener().String()

	transport := &mcp.SSEClientTransport{
		Endpoint: baseURL,
		HTTPClient: &http.Client{
			Transport: &apiKeyTransport{key: "test-key"},
		},
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0",
	}, nil)

	connectCtx, connectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer connectCancel()

	cs, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	defer cs.Close()

	// Read-only tools should work.
	result, err := cs.CallTool(connectCtx, &mcp.CallToolParams{Name: "jc_ping"})
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if result.IsError {
		t.Fatal("expected ping to succeed in read-only mode")
	}
}

// --- Test transport helpers ---

// apiKeyTransport injects x-api-key header into all requests.
type apiKeyTransport struct {
	key string
}

func (t *apiKeyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("x-api-key", t.key)
	return http.DefaultTransport.RoundTrip(req)
}

// bearerTransport injects Authorization: Bearer header into all requests.
type bearerTransport struct {
	token string
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}

// apiKeyTLSTransport injects x-api-key and skips TLS verification (for self-signed certs).
type apiKeyTLSTransport struct {
	key string
}

func (t *apiKeyTLSTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	req.Header.Set("x-api-key", t.key)
	return tr.RoundTrip(req)
}

// generateTestCert creates a self-signed TLS certificate for testing.
func generateTestCert(t *testing.T) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"Test"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	tmpDir := t.TempDir()
	certFile = filepath.Join(tmpDir, "cert.pem")
	keyFile = filepath.Join(tmpDir, "key.pem")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	os.WriteFile(certFile, certPEM, 0600)

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	os.WriteFile(keyFile, keyPEM, 0600)

	return certFile, keyFile
}

// startHTTPStreamServer mirrors startSSEServer but for the Streamable HTTP
// transport. Returns the base URL (including the /mcp path).
func startHTTPStreamServer(t *testing.T, cfg SSEConfig) (*Server, string) {
	t.Helper()
	setupTest(t)

	if cfg.Addr == "" {
		cfg.Addr = ":0"
	}

	server := MustNewServer(Options{
		RateLimit:    60,
		AuditLogPath: filepath.Join(t.TempDir(), "audit.log"),
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() { errCh <- server.RunStreamableHTTP(ctx, cfg) }()

	var addr net.Addr
	for i := 0; i < 50; i++ {
		addr = server.Listener()
		if addr != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if addr == nil {
		t.Fatal("HTTP stream server did not start listening")
	}

	return server, "http://" + addr.String() + "/mcp"
}

// TestHTTP_AuthRejectsUnauthenticated is the regression guard for the high-
// severity Bugbot finding: when APIKey is set on the http transport, requests
// without credentials must be rejected.
func TestHTTP_AuthRejectsUnauthenticated(t *testing.T) {
	_, baseURL := startHTTPStreamServer(t, SSEConfig{
		APIKey:     "test-key",
		CORSOrigin: "*",
	})

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	req, _ := http.NewRequest(http.MethodPost, baseURL, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized without x-api-key, got %d", resp.StatusCode)
	}
}

// TestHTTP_AuthAcceptsCorrectKey confirms auth is actually checked, not bypassed.
func TestHTTP_AuthAcceptsCorrectKey(t *testing.T) {
	_, baseURL := startHTTPStreamServer(t, SSEConfig{
		APIKey:     "test-key",
		CORSOrigin: "*",
	})

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	req, _ := http.NewRequest(http.MethodPost, baseURL, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("x-api-key", "test-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with correct x-api-key, got %d", resp.StatusCode)
	}
}

// TestHTTP_NoAuthWhenNoKey asserts the permissive default: no API key → any
// client can connect (needed for basic-host and local MCP Apps dev).
func TestHTTP_NoAuthWhenNoKey(t *testing.T) {
	_, baseURL := startHTTPStreamServer(t, SSEConfig{
		CORSOrigin: "*",
	})

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	req, _ := http.NewRequest(http.MethodPost, baseURL, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 without auth when no API key configured, got %d", resp.StatusCode)
	}
}
