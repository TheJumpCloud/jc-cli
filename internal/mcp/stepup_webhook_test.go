package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockReceiver runs an httptest server that records the inbound
// approval envelope and (optionally) POSTs a verdict back to the
// callback URL after a configurable delay. Drop-in stand-in for the
// real receiver (Slack bot, internal approver app) in tests.
type mockReceiver struct {
	server          *httptest.Server
	verdict         approvalVerdict
	postVerdict     bool
	verdictDelay    time.Duration
	verdictMutator  func(envelope approvalEnvelope, body []byte) ([]byte, string) // override body + target URL
	respondStatus   int
	respondDelay    time.Duration
	mu              sync.Mutex
	envelopes       []approvalEnvelope
	postErr         error
}

func newMockReceiver() *mockReceiver {
	m := &mockReceiver{
		verdict:       approvalVerdict{Decision: "approve", Approver: "alice@example.com"},
		postVerdict:   true,
		respondStatus: http.StatusAccepted,
	}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

func (m *mockReceiver) URL() string { return m.server.URL }

func (m *mockReceiver) Close() { m.server.Close() }

// Envelopes returns a snapshot of what the webhook posted.
func (m *mockReceiver) Envelopes() []approvalEnvelope {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]approvalEnvelope, len(m.envelopes))
	copy(out, m.envelopes)
	return out
}

func (m *mockReceiver) handle(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, _ := io.ReadAll(r.Body)
	var env approvalEnvelope
	_ = json.Unmarshal(body, &env)

	m.mu.Lock()
	m.envelopes = append(m.envelopes, env)
	m.mu.Unlock()

	if m.respondDelay > 0 {
		time.Sleep(m.respondDelay)
	}
	if m.respondStatus != http.StatusAccepted && m.respondStatus != http.StatusOK && m.respondStatus != 0 {
		w.WriteHeader(m.respondStatus)
		return
	}
	w.WriteHeader(http.StatusOK)

	if !m.postVerdict {
		return
	}
	go func() {
		if m.verdictDelay > 0 {
			time.Sleep(m.verdictDelay)
		}
		body, _ := json.Marshal(m.verdict)
		target := env.CallbackURL
		if m.verdictMutator != nil {
			body, target = m.verdictMutator(env, body)
		}
		resp, err := http.Post(target, "application/json", bytes.NewReader(body))
		if err != nil {
			m.mu.Lock()
			m.postErr = err
			m.mu.Unlock()
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
}

func TestWebhookStepUp_RemediationIsChannelAware(t *testing.T) {
	// Pin the webhook-flavored remediation text — what makes Bugbot's
	// learned rule satisfied is that webhook errors don't suggest TTY
	// or Touch ID. Drift in the message wording (e.g. dropping the
	// config-key reference) would defeat the remediation hint.
	w := &webhookStepUp{}

	denyText := w.remediation(errStepUpDenied)
	if !strings.Contains(denyText, "approver") {
		t.Errorf("deny remediation should reference the approver: %q", denyText)
	}
	if !strings.Contains(denyText, "mcp.approval_timeout") {
		t.Errorf("deny remediation should name mcp.approval_timeout: %q", denyText)
	}
	if strings.Contains(denyText, "Touch ID") || strings.Contains(denyText, "TTY") {
		t.Errorf("deny remediation should not mention TTY/Touch ID: %q", denyText)
	}

	unavailText := w.remediation(errStepUpUnavailable)
	if !strings.Contains(unavailText, "mcp.approval_webhook_url") {
		t.Errorf("unavailable remediation should name mcp.approval_webhook_url: %q", unavailText)
	}
	if strings.Contains(unavailText, "Touch-ID-capable Mac") {
		t.Errorf("unavailable remediation should not suggest a Touch-ID-capable Mac: %q", unavailText)
	}
}

// KLA-420: when an operator enables --require-step-up
// --step-up-authenticator=webhook without also enabling signing
// (mcp.sign_destructive_ops), the webhook envelope's profile field
// must still carry the active profile name. Pre-fix, server.go wired
// Profile = opts.SigningProfile directly with no fallback, so
// webhook-only setups emitted `"profile": ""` to receivers.
func TestNewServer_WebhookProfileFallsBackToActiveProfile(t *testing.T) {
	setupTest(t)

	s := MustNewServer(Options{
		RequireStepUp:        true,
		StepUpAuthenticator:  "webhook",
		ApprovalWebhookURL:   "http://127.0.0.1:1/notused",
		ApprovalCallbackAddr: "127.0.0.1:0",
		ApprovalTimeout:      1 * time.Second,
		// SigningProfile intentionally left empty — that's the bug:
		// pre-fix this would propagate `profile: ""` into the webhook.
	})
	t.Cleanup(s.shutdownStepUp)

	w, ok := s.stepUp.(*webhookStepUp)
	if !ok {
		t.Fatalf("expected *webhookStepUp, got %T", s.stepUp)
	}
	// setupTest configures active_profile=default; resolution must pick
	// that up rather than leaving the field empty.
	if w.profile != "default" {
		t.Errorf("webhookStepUp.profile = %q, want %q (active profile fallback)", w.profile, "default")
	}
}

// Companion: an explicit SigningProfile must still flow through
// unchanged. The fallback only kicks in when the explicit value is
// empty.
func TestNewServer_WebhookProfileHonorsExplicitSigningProfile(t *testing.T) {
	setupTest(t)

	s := MustNewServer(Options{
		RequireStepUp:        true,
		StepUpAuthenticator:  "webhook",
		ApprovalWebhookURL:   "http://127.0.0.1:1/notused",
		ApprovalCallbackAddr: "127.0.0.1:0",
		ApprovalTimeout:      1 * time.Second,
		SigningProfile:       "staging",
	})
	t.Cleanup(s.shutdownStepUp)

	w, ok := s.stepUp.(*webhookStepUp)
	if !ok {
		t.Fatalf("expected *webhookStepUp, got %T", s.stepUp)
	}
	if w.profile != "staging" {
		t.Errorf("webhookStepUp.profile = %q, want %q (explicit SigningProfile)", w.profile, "staging")
	}
}

func TestNewWebhookStepUp_RejectsEmptyURL(t *testing.T) {
	_, err := newWebhookStepUp("", "", 0, "default")
	if err == nil {
		t.Fatal("expected error when webhook URL is empty, got nil")
	}
}

func TestWebhookStepUp_RoundTripApprove(t *testing.T) {
	rcv := newMockReceiver()
	defer rcv.Close()

	w, err := newWebhookStepUp(rcv.URL(), "", 5*time.Second, "default")
	if err != nil {
		t.Fatalf("newWebhookStepUp: %v", err)
	}
	defer w.Close()

	if err := w.authorize(context.Background(), "users_delete", "alice"); err != nil {
		t.Errorf("authorize() = %v, want nil", err)
	}

	envs := rcv.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("receiver got %d envelopes, want 1", len(envs))
	}
	got := envs[0]
	if got.Tool != "users_delete" {
		t.Errorf("envelope.tool = %q, want users_delete", got.Tool)
	}
	if got.Target != "alice" {
		t.Errorf("envelope.target = %q, want alice", got.Target)
	}
	if got.Token == "" {
		t.Error("envelope.token is empty")
	}
	if got.Nonce == "" {
		t.Error("envelope.nonce is empty")
	}
	if got.Timestamp == "" {
		t.Error("envelope.timestamp is empty")
	}
	if !strings.HasPrefix(got.CallbackURL, "http://127.0.0.1:") {
		t.Errorf("envelope.callback_url = %q, want loopback http URL", got.CallbackURL)
	}
	if got.Profile != "default" {
		t.Errorf("envelope.profile = %q, want default", got.Profile)
	}
}

func TestWebhookStepUp_DenyBlocks(t *testing.T) {
	rcv := newMockReceiver()
	rcv.verdict = approvalVerdict{Decision: "deny", Approver: "alice@example.com", Reason: "test"}
	defer rcv.Close()

	w, err := newWebhookStepUp(rcv.URL(), "", 5*time.Second, "default")
	if err != nil {
		t.Fatalf("newWebhookStepUp: %v", err)
	}
	defer w.Close()

	err = w.authorize(context.Background(), "users_delete", "alice")
	if !errors.Is(err, errStepUpDenied) {
		t.Errorf("authorize() = %v, want errStepUpDenied", err)
	}
}

func TestWebhookStepUp_UnknownDecisionBlocks(t *testing.T) {
	// Anything that isn't a case-insensitive "approve" must be treated
	// as deny — fail closed against a misconfigured receiver that
	// invents its own verdict strings.
	rcv := newMockReceiver()
	rcv.verdict = approvalVerdict{Decision: "maybe"}
	defer rcv.Close()

	w, err := newWebhookStepUp(rcv.URL(), "", 5*time.Second, "default")
	if err != nil {
		t.Fatalf("newWebhookStepUp: %v", err)
	}
	defer w.Close()

	err = w.authorize(context.Background(), "users_delete", "alice")
	if !errors.Is(err, errStepUpDenied) {
		t.Errorf("authorize() = %v, want errStepUpDenied", err)
	}
}

func TestWebhookStepUp_TimeoutIsDeny(t *testing.T) {
	// Receiver acknowledges the POST but never sends a verdict.
	// authorize() must fail closed (errStepUpDenied) after the timeout.
	rcv := newMockReceiver()
	rcv.postVerdict = false
	defer rcv.Close()

	w, err := newWebhookStepUp(rcv.URL(), "", 100*time.Millisecond, "default")
	if err != nil {
		t.Fatalf("newWebhookStepUp: %v", err)
	}
	defer w.Close()

	start := time.Now()
	err = w.authorize(context.Background(), "users_delete", "alice")
	elapsed := time.Since(start)
	if !errors.Is(err, errStepUpDenied) {
		t.Errorf("authorize() = %v, want errStepUpDenied (timeout = deny)", err)
	}
	if elapsed < 100*time.Millisecond {
		t.Errorf("authorize returned in %v, want at least timeout duration (100ms)", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("authorize blocked %v, much longer than timeout", elapsed)
	}
}

func TestWebhookStepUp_ContextCancelIsDeny(t *testing.T) {
	rcv := newMockReceiver()
	rcv.postVerdict = false
	defer rcv.Close()

	w, err := newWebhookStepUp(rcv.URL(), "", 5*time.Second, "default")
	if err != nil {
		t.Fatalf("newWebhookStepUp: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	err = w.authorize(ctx, "users_delete", "alice")
	if !errors.Is(err, errStepUpDenied) {
		t.Errorf("authorize() = %v, want errStepUpDenied (ctx cancel = deny)", err)
	}
}

func TestWebhookStepUp_Receiver5xxIsUnavailable(t *testing.T) {
	rcv := newMockReceiver()
	rcv.respondStatus = http.StatusInternalServerError
	rcv.postVerdict = false
	defer rcv.Close()

	w, err := newWebhookStepUp(rcv.URL(), "", 5*time.Second, "default")
	if err != nil {
		t.Fatalf("newWebhookStepUp: %v", err)
	}
	defer w.Close()

	err = w.authorize(context.Background(), "users_delete", "alice")
	if !errors.Is(err, errStepUpUnavailable) {
		t.Errorf("authorize() = %v, want errStepUpUnavailable (receiver 500)", err)
	}
}

func TestWebhookStepUp_ReceiverUnreachableIsUnavailable(t *testing.T) {
	// Point at a port that's almost certainly nothing — 127.0.0.1:1
	// (port 1 is privileged and unlikely to be listening).
	w, err := newWebhookStepUp("http://127.0.0.1:1/approve", "", 5*time.Second, "default")
	if err != nil {
		t.Fatalf("newWebhookStepUp: %v", err)
	}
	defer w.Close()

	err = w.authorize(context.Background(), "users_delete", "alice")
	if !errors.Is(err, errStepUpUnavailable) {
		t.Errorf("authorize() = %v, want errStepUpUnavailable (receiver unreachable)", err)
	}
}

func TestWebhookStepUp_ForgedTokenIs404(t *testing.T) {
	// A receiver that posts a verdict to a fabricated callback URL with
	// a different token must not be able to approve a real request.
	rcv := newMockReceiver()
	rcv.verdictMutator = func(env approvalEnvelope, body []byte) ([]byte, string) {
		// Rewrite the path to use a fake token — same listener, wrong
		// correlation. The handler must return 404 and the original
		// authorize() call must block until timeout.
		idx := strings.LastIndex(env.CallbackURL, "/")
		forged := env.CallbackURL[:idx+1] + "FAKE-NOT-A-REAL-TOKEN"
		return body, forged
	}
	defer rcv.Close()

	w, err := newWebhookStepUp(rcv.URL(), "", 300*time.Millisecond, "default")
	if err != nil {
		t.Fatalf("newWebhookStepUp: %v", err)
	}
	defer w.Close()

	err = w.authorize(context.Background(), "users_delete", "alice")
	if !errors.Is(err, errStepUpDenied) {
		t.Errorf("authorize() = %v, want errStepUpDenied (forged token must not approve)", err)
	}
}

func TestWebhookStepUp_ReplayedVerdictIsIgnored(t *testing.T) {
	// First verdict approves; the second POST against the same token
	// must hit a now-empty pending map and return 404. Otherwise a
	// receiver that double-fires could keep an authorize() flow alive
	// across requests or, worse, the second call could land on a *new*
	// authorize() call with the same token (extremely unlikely with
	// HMAC over random nonce, but the explicit replay reject is the
	// belt-and-suspenders we want).
	approvedOnce := make(chan struct{}, 1)
	var replayStatus atomic.Int32

	rcv := newMockReceiver()
	rcv.verdictMutator = func(env approvalEnvelope, body []byte) ([]byte, string) {
		// First-call body is the default approve verdict; replay
		// uses the same token URL.
		go func() {
			// Send a SECOND POST after a small delay to simulate a
			// double-fire.
			time.Sleep(50 * time.Millisecond)
			resp, err := http.Post(env.CallbackURL, "application/json", bytes.NewReader(body))
			if err == nil {
				replayStatus.Store(int32(resp.StatusCode))
				resp.Body.Close()
			}
			approvedOnce <- struct{}{}
		}()
		return body, env.CallbackURL
	}
	defer rcv.Close()

	w, err := newWebhookStepUp(rcv.URL(), "", 5*time.Second, "default")
	if err != nil {
		t.Fatalf("newWebhookStepUp: %v", err)
	}
	defer w.Close()

	if err := w.authorize(context.Background(), "users_delete", "alice"); err != nil {
		t.Errorf("authorize() = %v, want nil", err)
	}
	// Wait for the replay POST to finish so we can read its status.
	select {
	case <-approvedOnce:
	case <-time.After(2 * time.Second):
		t.Fatal("replay POST never completed")
	}
	if got := replayStatus.Load(); got != http.StatusNotFound {
		t.Errorf("replay POST status = %d, want 404", got)
	}
}

func TestWebhookStepUp_MalformedVerdictRejected(t *testing.T) {
	rcv := newMockReceiver()
	rcv.verdictMutator = func(env approvalEnvelope, _ []byte) ([]byte, string) {
		return []byte("not-json"), env.CallbackURL
	}
	rcv.verdictDelay = 0
	defer rcv.Close()

	w, err := newWebhookStepUp(rcv.URL(), "", 300*time.Millisecond, "default")
	if err != nil {
		t.Fatalf("newWebhookStepUp: %v", err)
	}
	defer w.Close()

	// Malformed body → handler returns 400 → no verdict consumed →
	// authorize() blocks until timeout = deny.
	err = w.authorize(context.Background(), "users_delete", "alice")
	if !errors.Is(err, errStepUpDenied) {
		t.Errorf("authorize() = %v, want errStepUpDenied (malformed verdict drops to timeout)", err)
	}
}

func TestWebhookStepUp_HandlerRejectsWrongMethod(t *testing.T) {
	// Belt-and-suspenders: GETs / OPTIONs against /approval/<token>
	// must not consume a pending entry. Tested at the HTTP layer
	// directly so we can isolate the method check.
	rcv := newMockReceiver()
	rcv.postVerdict = false
	defer rcv.Close()

	w, err := newWebhookStepUp(rcv.URL(), "", 100*time.Millisecond, "default")
	if err != nil {
		t.Fatalf("newWebhookStepUp: %v", err)
	}
	defer w.Close()

	url := "http://" + w.CallbackAddr() + webhookCallbackPath + "any-token"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET callback URL: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", resp.StatusCode)
	}
}

func TestWebhookStepUp_CloseShutsDownListener(t *testing.T) {
	rcv := newMockReceiver()
	rcv.postVerdict = false
	defer rcv.Close()

	w, err := newWebhookStepUp(rcv.URL(), "", 1*time.Second, "default")
	if err != nil {
		t.Fatalf("newWebhookStepUp: %v", err)
	}
	addr := w.CallbackAddr()
	if addr == "" {
		t.Fatal("CallbackAddr is empty after construction")
	}

	if err := w.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}

	// After shutdown, the listener should reject new connections.
	resp, err := http.Get("http://" + addr + webhookCallbackPath + "anything")
	if err == nil {
		resp.Body.Close()
		t.Errorf("GET after Close succeeded with status %d, want connection error", resp.StatusCode)
	}
}

func TestWebhookStepUp_TokenBindsToCanonicalEnvelope(t *testing.T) {
	// Two requests with different tool names must produce different
	// tokens — that's what makes the token genuinely correlation-only.
	// A token leaked from request A must not match request B.
	rcv := newMockReceiver()
	rcv.postVerdict = false
	defer rcv.Close()

	w, err := newWebhookStepUp(rcv.URL(), "", 50*time.Millisecond, "default")
	if err != nil {
		t.Fatalf("newWebhookStepUp: %v", err)
	}
	defer w.Close()

	// Two concurrent authorize() calls (both will time out, that's
	// fine — we just want the envelopes the receiver collected).
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = w.authorize(context.Background(), "users_delete", "alice") }()
	go func() { defer wg.Done(); _ = w.authorize(context.Background(), "devices_delete", "host-001") }()
	wg.Wait()

	envs := rcv.Envelopes()
	if len(envs) != 2 {
		t.Fatalf("got %d envelopes, want 2", len(envs))
	}
	if envs[0].Token == "" || envs[1].Token == "" {
		t.Fatal("token missing on one of the envelopes")
	}
	if envs[0].Token == envs[1].Token {
		t.Errorf("two different envelopes produced the same token %q — token is not envelope-bound", envs[0].Token)
	}
}
