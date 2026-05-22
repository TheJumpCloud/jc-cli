package mcp

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// webhookDefaultTimeout is the default deadline for receiving a verdict
// from the operator-configured approval receiver. Per KLA-413, timeout =
// deny (fail closed) so an unattended request never silently slips
// through.
const webhookDefaultTimeout = 5 * time.Minute

// webhookCallbackPath is the path prefix on the loopback listener that
// receivers POST verdicts to. The token follows the prefix, e.g.
// /approval/<base64-token>.
const webhookCallbackPath = "/approval/"

// approvalEnvelope is the JSON object POSTed to the operator-configured
// webhook. It carries enough context for a human approver to make a
// decision plus the correlation token the receiver echoes back.
//
// ArgsRedacted shares the same redaction pass used by the Ed25519 signer,
// so sensitive params (passwords, tokens, shared_secrets, ...) never
// leave the host even when an operator points the webhook at an
// external bot.
type approvalEnvelope struct {
	Tool         string          `json:"tool"`
	Target       string          `json:"target,omitempty"`
	ArgsRedacted json.RawMessage `json:"args_redacted"`
	Timestamp    string          `json:"timestamp"`
	Nonce        string          `json:"nonce"`
	Token        string          `json:"token"`
	CallbackURL  string          `json:"callback_url"`
	Profile      string          `json:"profile,omitempty"`
}

// approvalVerdict is the JSON body the receiver POSTs back to the
// loopback callback. Decision is "approve" (proceed) or "deny" (block);
// anything else is treated as deny. Approver / Reason are advisory and
// recorded but do not change the gate.
type approvalVerdict struct {
	Decision string `json:"decision"`
	Approver string `json:"approver,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// webhookStepUp authorizes destructive MCP operations via an out-of-band
// approval flow. The chokepoint hands authorize() the tool name + target;
// this implementation POSTs the redacted envelope to the configured
// webhook URL and blocks on a verdict callback. Designed for compliance
// frameworks that require dual control on destructive admin ops, or for
// stdio MCP transports where neither a TTY prompt nor Touch ID can reach
// the operator.
//
// Threat model:
//   - The webhook URL is operator-configured and assumed trustworthy enough
//     to present the envelope to a designated approver. Sensitive params
//     are redacted before they leave the process.
//   - The callback listener binds to loopback by default. The HMAC-bound
//     single-use token rejects verdicts that don't match an in-flight
//     request, so a stolen token can't approve a different envelope and
//     a replayed token can't fire twice.
//   - Failure modes (no webhook configured, POST fails, receiver returns
//     non-2xx, timeout, ctx cancelled, malformed verdict) all return
//     errStepUpUnavailable or errStepUpDenied — never nil.
type webhookStepUp struct {
	webhookURL  string
	httpClient  *http.Client
	timeout     time.Duration
	hmacKey     []byte // per-process random; binds token → envelope
	profile     string

	mu       sync.Mutex
	pending  map[string]*pendingApproval
	listener net.Listener
	server   *http.Server
}

// pendingApproval holds the response channel and the canonical envelope
// bytes the token was derived from. Storing the envelope (not just the
// channel) lets the callback handler re-verify the HMAC binding before
// honoring a verdict — a verdict POSTed with a forged or stale token
// won't match a live envelope and is rejected.
type pendingApproval struct {
	ch       chan approvalVerdict
	envelope []byte
}

// newWebhookStepUp constructs a webhook-driven authenticator. webhookURL
// is required (the destination for outbound approval requests).
// callbackAddr is the loopback listen address for inbound verdicts;
// empty defaults to "127.0.0.1:0" so the kernel picks an ephemeral port.
// timeout is the per-request approval deadline; zero defaults to
// webhookDefaultTimeout.
//
// The returned struct owns a live HTTP listener and must be Close()'d
// when the MCP server exits. Server.Run / RunSSE / RunStreamableHTTP
// take care of that via a deferred Close call against the
// authenticatorCloser type assertion.
func newWebhookStepUp(webhookURL, callbackAddr string, timeout time.Duration, profile string) (*webhookStepUp, error) {
	if webhookURL == "" {
		return nil, errors.New("approval_webhook_url is empty")
	}
	if timeout <= 0 {
		timeout = webhookDefaultTimeout
	}
	if callbackAddr == "" {
		callbackAddr = "127.0.0.1:0"
	}

	hmacKey := make([]byte, 32)
	if _, err := rand.Read(hmacKey); err != nil {
		return nil, fmt.Errorf("generating webhook HMAC key: %w", err)
	}

	w := &webhookStepUp{
		webhookURL: webhookURL,
		// Outbound POSTs are short request/response — a 30s upper bound
		// protects against a hung receiver eating our worker. The verdict
		// itself flows back via the loopback listener, not this HTTP call.
		httpClient: &http.Client{Timeout: 30 * time.Second},
		timeout:    timeout,
		hmacKey:    hmacKey,
		profile:    profile,
		pending:    make(map[string]*pendingApproval),
	}

	ln, err := net.Listen("tcp", callbackAddr)
	if err != nil {
		return nil, fmt.Errorf("binding webhook callback listener on %s: %w", callbackAddr, err)
	}
	w.listener = ln

	mux := http.NewServeMux()
	mux.HandleFunc(webhookCallbackPath, w.handleVerdict)
	w.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		MaxHeaderBytes:    1 << 16, // 64 KB
	}
	go func() {
		// Serve returns http.ErrServerClosed on normal shutdown; ignore.
		_ = w.server.Serve(ln)
	}()

	return w, nil
}

// CallbackAddr returns the loopback address the listener is bound to.
// Useful for logging at startup and for tests that need to know the
// ephemeral port the kernel picked.
func (w *webhookStepUp) CallbackAddr() string {
	if w.listener == nil {
		return ""
	}
	return w.listener.Addr().String()
}

// Close shuts down the callback listener. Safe to call from any
// goroutine; idempotent. Server lifecycle defers this.
func (w *webhookStepUp) Close() error {
	if w.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return w.server.Shutdown(ctx)
}

// authorize implements the stepUpAuthenticator contract. Computes the
// envelope, POSTs it to the configured webhook, then blocks on the
// callback or the timeout. Fail closed: every error path returns
// errStepUpUnavailable (infrastructure) or errStepUpDenied (operator
// decision / timeout / cancellation).
func (w *webhookStepUp) authorize(ctx context.Context, toolName, target string) error {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return errStepUpUnavailable
	}

	envelope := approvalEnvelope{
		Tool:      toolName,
		Target:    target,
		Timestamp: nowFunc().UTC().Format(time.RFC3339),
		Nonce:     base64.StdEncoding.EncodeToString(nonce),
		Profile:   w.profile,
	}
	// Note: ArgsRedacted is intentionally left empty at this stage. The
	// authorize() interface predates KLA-413 and does not carry the raw
	// args. The token binds the verdict to tool+target+timestamp+nonce,
	// which is enough to prevent cross-request approval forging. If a
	// future ticket extends the interface with args, plumb them through
	// here so the human approver sees what they're approving.

	canonical, err := canonicalEnvelopeForToken(envelope)
	if err != nil {
		return errStepUpUnavailable
	}
	token := w.tokenFor(canonical)
	envelope.Token = token
	envelope.CallbackURL = w.callbackURLFor(token)

	ch := make(chan approvalVerdict, 1)
	w.mu.Lock()
	w.pending[token] = &pendingApproval{ch: ch, envelope: canonical}
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		delete(w.pending, token)
		w.mu.Unlock()
	}()

	body, err := json.Marshal(envelope)
	if err != nil {
		return errStepUpUnavailable
	}

	postCtx, postCancel := context.WithTimeout(ctx, 30*time.Second)
	defer postCancel()
	req, err := http.NewRequestWithContext(postCtx, http.MethodPost, w.webhookURL, bytes.NewReader(body))
	if err != nil {
		return errStepUpUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "jc-mcp-stepup-webhook/1")
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return errStepUpUnavailable
	}
	// Drain + close so the keep-alive pool can reuse the connection.
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errStepUpUnavailable
	}

	timer := time.NewTimer(w.timeout)
	defer timer.Stop()
	select {
	case v := <-ch:
		if strings.EqualFold(v.Decision, "approve") {
			return nil
		}
		return errStepUpDenied
	case <-timer.C:
		// Timeout = deny per KLA-413 spec. A silent timeout that
		// allowed the call would be the worst-of-both: the operator
		// thinks they have dual control while the gate flaps open.
		return errStepUpDenied
	case <-ctx.Done():
		return errStepUpDenied
	}
}

// handleVerdict accepts a POST at /approval/<token>. The token must
// match a live pending request, and the HMAC re-derivation must match
// the stored envelope. Either mismatch → 404 (don't leak which token
// is or isn't valid).
func (w *webhookStepUp) handleVerdict(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(r.URL.Path, webhookCallbackPath)
	if token == "" || strings.Contains(token, "/") {
		http.NotFound(rw, r)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		http.Error(rw, "read body", http.StatusBadRequest)
		return
	}
	var verdict approvalVerdict
	if err := json.Unmarshal(body, &verdict); err != nil {
		http.Error(rw, "invalid json", http.StatusBadRequest)
		return
	}

	w.mu.Lock()
	pa, ok := w.pending[token]
	if ok {
		// Re-derive the expected token from the stored canonical envelope
		// and compare in constant time. An attacker who guessed or
		// observed a token can't approve a different envelope this way,
		// and a token replayed after delivery is dropped by the
		// single-shot channel below.
		expected := w.tokenFor(pa.envelope)
		if subtle.ConstantTimeCompare([]byte(expected), []byte(token)) != 1 {
			ok = false
		}
	}
	if !ok {
		w.mu.Unlock()
		// 404 (not 401) so a probe can't enumerate live tokens by
		// comparing status codes against fabricated ones.
		http.NotFound(rw, r)
		return
	}
	// Consume immediately so a replay POST against the same token (after
	// authorize() returned) finds no pending entry.
	delete(w.pending, token)
	w.mu.Unlock()

	// Non-blocking send: the receiver of pa.ch is select'd; the channel
	// has capacity 1, so this never blocks for a valid live request.
	select {
	case pa.ch <- verdict:
	default:
	}

	rw.WriteHeader(http.StatusAccepted)
	_, _ = rw.Write([]byte(`{"status":"recorded"}`))
}

// tokenFor returns the base64url-encoded HMAC-SHA256 over the canonical
// envelope. Callers pass in the canonical bytes; the function never
// re-computes them from a struct field. Using base64url (no padding,
// no slashes) keeps the token URL-safe for the callback path.
func (w *webhookStepUp) tokenFor(canonical []byte) string {
	mac := hmac.New(sha256.New, w.hmacKey)
	mac.Write(canonical)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// callbackURLFor returns the absolute http://loopback:port/approval/<token>
// URL the receiver should POST verdicts to. Scheme is fixed to http;
// the listener is loopback, so TLS would add no security.
func (w *webhookStepUp) callbackURLFor(token string) string {
	return fmt.Sprintf("http://%s%s%s", w.CallbackAddr(), webhookCallbackPath, token)
}

// canonicalEnvelopeForToken returns the byte sequence the HMAC binds
// over. The signature copy clears Token + CallbackURL (both derived
// from the canonical bytes, so they must not feed back into them) and
// relies on encoding/json's deterministic alphabetical key order.
//
// Shared with handleVerdict via the stored envelope bytes so the
// verifier and the producer compute the exact same string. If the
// envelope grows a new field, callers downstream that recompute the
// token will need to recompute against the new canonical form.
func canonicalEnvelopeForToken(e approvalEnvelope) ([]byte, error) {
	cp := e
	cp.Token = ""
	cp.CallbackURL = ""
	return json.Marshal(cp)
}
