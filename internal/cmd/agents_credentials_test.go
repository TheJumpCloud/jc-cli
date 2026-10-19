package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/viper"
)

// The secret this fake API hands out. Every test asserts on where it does
// and does not appear.
const fakeSecret = "NOT-A-REAL-SECRET-abc123xyz"

const fakeClientID = "agent-6aa85171a36f7d16b20d9b60-e242fb7cd915dd8a"

type credServer struct {
	*httptest.Server
	mu sync.Mutex
	// exists drives the one-credential-per-agent behaviour.
	exists     bool
	noSecret   bool
	createFail bool
	// secret lets a test vary the secret's LENGTH, which is what
	// TestCredentialsCreate_OutputDoesNotDependOnTheSecret needs.
	secret   string
	requests []string
}

func (c *credServer) seen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.requests...)
}

func startCredServer(t *testing.T) *credServer {
	t.Helper()
	srv := &credServer{}
	srv.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.mu.Lock()
		srv.requests = append(srv.requests, r.Method+" "+r.URL.Path)
		exists, noSecret, createFail := srv.exists, srv.noSecret, srv.createFail
		secret := srv.secret
		if secret == "" {
			secret = fakeSecret
		}
		srv.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		base := "/agents/" + testAgentID + "/inbound-credentials"

		switch {
		case r.URL.Path == base && r.Method == http.MethodPost:
			if createFail {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"boom"}`))
				return
			}
			if exists {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"message":"credential already exists"}`))
				return
			}
			srv.mu.Lock()
			srv.exists = true
			srv.mu.Unlock()
			if noSecret {
				_, _ = w.Write([]byte(`{"clientId":"` + fakeClientID + `","expiresAt":"2027-01-01T00:00:00Z"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"clientId":"` + fakeClientID + `","clientSecret":"` + secret +
				`","expiresAt":"2026-11-18T14:46:30Z","tokenEndpointAuthMethod":"client_secret_basic",` +
				`"authType":"client_credentials"}`))

		case r.URL.Path == base && r.Method == http.MethodGet:
			if !exists {
				_, _ = w.Write([]byte(`{"credentials":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"credentials":[{"clientId":"` + fakeClientID +
				`","authType":"client_credentials","status":"active",` +
				`"tokenEndpointAuthMethod":"client_secret_basic","expiresAt":"2026-11-18T14:46:30Z"}]}`))

		case strings.HasPrefix(r.URL.Path, base+"/") && r.Method == http.MethodPatch:
			_, _ = w.Write([]byte(`{"clientId":"` + fakeClientID +
				`","status":"active","tokenEndpointAuthMethod":"client_secret_post"}`))

		case strings.HasPrefix(r.URL.Path, base+"/") && r.Method == http.MethodDelete:
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"not found"}`))
				return
			}
			srv.mu.Lock()
			srv.exists = false
			srv.mu.Unlock()
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runCred runs a command with the clipboard stubbed, and returns stdout and
// stderr separately — the distinction is the whole point of --stdout.
func runCred(t *testing.T, srv *credServer, args ...string) (string, string, string, error) {
	t.Helper()
	setupUsersTest(t)
	viper.Set("org_id", "5ec71e8e96bfda0611fc6c5b")
	viper.Set("force", true)
	overrideV2Client(t, srv.URL)

	var clipped string
	orig := clipboardWrite
	clipboardWrite = func(s string) error { clipped = s; return nil }
	t.Cleanup(func() { clipboardWrite = orig })

	cmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errBuf.String(), clipped, err
}

// --- The secret must only go where it was asked to go --------------------

func TestCredentialsCreate_NoDestinationIsRefusedBeforeCalling(t *testing.T) {
	srv := startCredServer(t)
	out, errOut, clip, err := runCred(t, srv, "agents", "credentials", "create", testAgentID)
	if err == nil {
		t.Fatal("a create with no destination was accepted")
	}
	// Critically: nothing was created, so no credential was burned.
	if len(srv.seen()) != 0 {
		t.Errorf("made requests %v for a create that could not deliver its secret", srv.seen())
	}
	assertNoSecret(t, "stdout", out)
	assertNoSecret(t, "stderr", errOut)
	assertNoSecret(t, "clipboard", clip)
	if !strings.Contains(err.Error(), "--out") || !strings.Contains(err.Error(), "--stdout") {
		t.Errorf("the error does not list the destinations: %v", err)
	}
}

func TestCredentialsCreate_DestinationsAreMutuallyExclusive(t *testing.T) {
	srv := startCredServer(t)
	_, _, _, err := runCred(t, srv, "agents", "credentials", "create", testAgentID,
		"--stdout", "--clipboard")
	if err == nil {
		t.Fatal("two destinations were accepted")
	}
	if len(srv.seen()) != 0 {
		t.Errorf("made requests %v before validating the destination", srv.seen())
	}
}

func TestCredentialsCreate_ToFile(t *testing.T) {
	srv := startCredServer(t)
	path := filepath.Join(t.TempDir(), "secret.txt")

	out, errOut, clip, err := runCred(t, srv, "agents", "credentials", "create", testAgentID, "--out", path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The secret is in the file and nowhere else.
	data, rErr := os.ReadFile(path)
	if rErr != nil {
		t.Fatalf("reading the secret file: %v", rErr)
	}
	if strings.TrimSpace(string(data)) != fakeSecret {
		t.Errorf("file holds %q, want the secret", strings.TrimSpace(string(data)))
	}
	assertNoSecret(t, "stdout", out)
	assertNoSecret(t, "stderr", errOut)
	assertNoSecret(t, "clipboard", clip)

	// Mode 0600: a secret file readable by the group or the world is a leak
	// of a different kind.
	info, sErr := os.Stat(path)
	if sErr != nil {
		t.Fatal(sErr)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}

	// And the operator is told what was made, and that it was the only time.
	for _, want := range []string{fakeClientID, path, "only time"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr does not mention %q: %s", want, errOut)
		}
	}
}

func TestCredentialsCreate_RefusesToOverwriteAnExistingFile(t *testing.T) {
	srv := startCredServer(t)
	path := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(path, []byte("something already here"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, _, err := runCred(t, srv, "agents", "credentials", "create", testAgentID, "--out", path)
	if err == nil {
		t.Fatal("an existing file was accepted as a destination")
	}
	// Nothing created: the check happens before the API call.
	if len(srv.seen()) != 0 {
		t.Errorf("made requests %v before checking the destination", srv.seen())
	}
	if !strings.Contains(err.Error(), "No credential was created") {
		t.Errorf("the error does not reassure that nothing was lost: %v", err)
	}
	// The existing contents survive.
	data, _ := os.ReadFile(path)
	if string(data) != "something already here" {
		t.Error("the existing file was modified")
	}
}

func TestCredentialsCreate_ToStdout(t *testing.T) {
	srv := startCredServer(t)
	out, errOut, clip, err := runCred(t, srv, "agents", "credentials", "create", testAgentID, "--stdout")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// stdout carries EXACTLY the secret, so a pipe into a secret manager
	// gets the value and nothing else.
	if strings.TrimSpace(out) != fakeSecret {
		t.Errorf("stdout = %q, want exactly the secret", out)
	}
	assertNoSecret(t, "clipboard", clip)
	// The warning and the report go to stderr, where a pipe does not see
	// them.
	if !strings.Contains(errOut, "once and never again") {
		t.Errorf("stderr does not warn that the secret is one-shot: %q", errOut)
	}
}

func TestCredentialsCreate_ToClipboard(t *testing.T) {
	srv := startCredServer(t)
	out, errOut, clip, err := runCred(t, srv, "agents", "credentials", "create", testAgentID, "--clipboard")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if clip != fakeSecret {
		t.Errorf("clipboard = %q, want the secret", clip)
	}
	assertNoSecret(t, "stdout", out)
	assertNoSecret(t, "stderr", errOut)
	if !strings.Contains(errOut, "clipboard") {
		t.Errorf("stderr does not say where the secret went: %q", errOut)
	}
}

// The length is withheld as deliberately as the value: an exact length
// narrows a brute-force search, which a security review flagged on the
// Password Vault reader for the same reason.
//
// Tested as a PROPERTY rather than by scanning for words like "length".
// The first version of this test did scan, and failed on its own temp
// directory — t.TempDir() embeds the test's name, which contained
// "SecretLength". A substring heuristic over text that includes file paths
// cries wolf, and a test that cries wolf is a test someone deletes.
//
// The property is stronger anyway: if nothing in the report derives from the
// secret's value, then changing the secret cannot change the report.
func TestCredentialsCreate_OutputDoesNotDependOnTheSecret(t *testing.T) {
	render := func(secret string) (string, string) {
		srv := startCredServer(t)
		srv.secret = secret
		dir := t.TempDir()
		path := filepath.Join(dir, "s.txt")

		out, errOut, _, err := runCred(t, srv, "agents", "credentials", "create", testAgentID, "--out", path)
		if err != nil {
			t.Fatalf("Execute with a %d-char secret: %v", len(secret), err)
		}
		// Only the temp path legitimately differs between runs.
		return out, strings.ReplaceAll(errOut, path, "<PATH>")
	}

	shortOut, shortErr := render("a")
	longOut, longErr := render(strings.Repeat("z", 512))

	if shortErr != longErr {
		t.Errorf("the report changed when the secret's length changed, so something in it "+
			"derives from the secret:\n--- 1-char secret ---\n%s\n--- 512-char secret ---\n%s",
			shortErr, longErr)
	}
	if shortOut != longOut {
		t.Errorf("stdout changed with the secret's length:\n%q\n%q", shortOut, longOut)
	}
	// And neither run leaked the value itself.
	for _, text := range []string{shortOut, shortErr, longOut, longErr} {
		if strings.Contains(text, strings.Repeat("z", 32)) {
			t.Errorf("the secret appeared in the output: %q", text)
		}
	}
}

// --- One credential per agent --------------------------------------------

func TestCredentialsCreate_SecondOneExplainsRotation(t *testing.T) {
	srv := startCredServer(t)
	srv.exists = true

	_, _, _, err := runCred(t, srv, "agents", "credentials", "create", testAgentID, "--stdout")
	if err == nil {
		t.Fatal("a second credential was accepted")
	}
	// The error has to carry the whole recovery, because the operator
	// cannot discover the delete-then-create shape from the API's message.
	for _, want := range []string{"only one", "credentials delete", "credentials create", "between"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}

// A reserved file must not be left behind when the create fails.
func TestCredentialsCreate_FailedCreateLeavesNoEmptyFile(t *testing.T) {
	srv := startCredServer(t)
	srv.createFail = true
	path := filepath.Join(t.TempDir(), "secret.txt")

	if _, _, _, err := runCred(t, srv, "agents", "credentials", "create", testAgentID, "--out", path); err == nil {
		t.Fatal("expected the create to fail")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("an empty secret file was left behind after a failed create")
	}
}

// A create whose response carries no secret is a credential the operator
// cannot use, and it has consumed the agent's only slot.
func TestCredentialsCreate_ResponseWithNoSecretIsAnError(t *testing.T) {
	srv := startCredServer(t)
	srv.noSecret = true
	path := filepath.Join(t.TempDir(), "secret.txt")

	_, _, _, err := runCred(t, srv, "agents", "credentials", "create", testAgentID, "--out", path)
	if err == nil {
		t.Fatal("a response with no secret was reported as a success")
	}
	if !strings.Contains(err.Error(), "delete it and create another") {
		t.Errorf("the error does not say how to recover: %v", err)
	}
	if _, sErr := os.Stat(path); !os.IsNotExist(sErr) {
		t.Error("a secret file was left behind for a credential with no secret")
	}
}

// --- update ---------------------------------------------------------------

func TestCredentialsUpdate_OnlyTheAuthMethod(t *testing.T) {
	srv := startCredServer(t)
	srv.exists = true

	_, errOut, _, err := runCred(t, srv, "agents", "credentials", "update", testAgentID, fakeClientID,
		"--auth-method", "client_secret_post")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(errOut, "updated credential") {
		t.Errorf("stderr does not confirm the update: %q", errOut)
	}
}

// The API accepts status and lifetimeDays in an update and discards them,
// returning success. Forwarding them would report a change that did not
// happen, so jc refuses and says why.
func TestCredentialsUpdate_RefusesTheFieldsTheAPIIgnores(t *testing.T) {
	srv := startCredServer(t)
	srv.exists = true

	_, _, _, err := runCred(t, srv, "agents", "credentials", "update", testAgentID, fakeClientID,
		"--auth-method", "client_secret_post", "--status", "inactive")
	if err == nil {
		t.Fatal("--status was accepted, but the API ignores it")
	}
	if !strings.Contains(err.Error(), "ignores it") {
		t.Errorf("the error does not explain the API discards it: %v", err)
	}
	if len(srv.seen()) != 0 {
		t.Errorf("made requests %v for a change that cannot take effect", srv.seen())
	}

	_, _, _, err = runCred(t, srv, "agents", "credentials", "update", testAgentID, fakeClientID,
		"--auth-method", "client_secret_post", "--lifetime", "90")
	if err == nil {
		t.Fatal("--lifetime was accepted on an update, but the API ignores it")
	}
}

func TestCredentialsUpdate_RequiresTheAuthMethod(t *testing.T) {
	srv := startCredServer(t)
	_, _, _, err := runCred(t, srv, "agents", "credentials", "update", testAgentID, fakeClientID)
	if err == nil {
		t.Fatal("an update with no auth method was accepted, but the API requires one")
	}
}

// --- delete ---------------------------------------------------------------

func TestCredentialsDelete_SaysTheAgentCannotAuthenticate(t *testing.T) {
	srv := startCredServer(t)
	srv.exists = true

	_, errOut, _, err := runCred(t, srv, "agents", "credentials", "delete", testAgentID, fakeClientID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// The listing is read back, and the consequence is stated plainly.
	for _, want := range []string{"Deleted credential", "cannot authenticate", "credentials create"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr does not mention %q: %s", want, errOut)
		}
	}
}

func TestCredentialsDelete_NotFoundExplainsItself(t *testing.T) {
	srv := startCredServer(t)
	srv.exists = false

	_, _, _, err := runCred(t, srv, "agents", "credentials", "delete", testAgentID, fakeClientID)
	if err == nil {
		t.Fatal("deleting a credential that is not there succeeded")
	}
	if !strings.Contains(err.Error(), "already be deleted") {
		t.Errorf("the error does not explain the likely cause: %v", err)
	}
}

// --- --plan ---------------------------------------------------------------

func TestCredentialWrites_PlanMakesNoRequestAndNoSecret(t *testing.T) {
	cases := [][]string{
		{"agents", "credentials", "create", testAgentID, "--stdout"},
		{"agents", "credentials", "update", testAgentID, fakeClientID, "--auth-method", "client_secret_post"},
		{"agents", "credentials", "delete", testAgentID, fakeClientID},
	}
	for _, args := range cases {
		srv := startCredServer(t)
		srv.exists = true

		setupUsersTest(t)
		viper.Set("org_id", "5ec71e8e96bfda0611fc6c5b")
		viper.Set("plan", true)
		overrideV2Client(t, srv.URL)

		var clipped string
		orig := clipboardWrite
		clipboardWrite = func(s string) error { clipped = s; return nil }

		cmd := NewRootCmd()
		var out, errBuf bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errBuf)
		cmd.SetArgs(args)
		_ = cmd.Execute()
		clipboardWrite = orig

		for _, r := range srv.seen() {
			if !strings.HasPrefix(r, "GET") {
				t.Errorf("%v with --plan made %s", args, r)
			}
		}
		assertNoSecret(t, "stdout", out.String())
		assertNoSecret(t, "stderr", errBuf.String())
		assertNoSecret(t, "clipboard", clipped)
	}
}

func TestCredentialsCreate_PlanWarnsAboutANonExpiringCredential(t *testing.T) {
	srv := startCredServer(t)
	setupUsersTest(t)
	viper.Set("org_id", "5ec71e8e96bfda0611fc6c5b")
	viper.Set("plan", true)
	overrideV2Client(t, srv.URL)

	cmd := NewRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"agents", "credentials", "create", testAgentID, "--stdout", "--lifetime", "0"})
	_ = cmd.Execute()

	if !strings.Contains(out.String()+errBuf.String(), "NEVER EXPIRES") {
		t.Errorf("a lifetime of 0 was not flagged as non-expiring:\n%s%s", out.String(), errBuf.String())
	}
}

func TestCredentialsCreate_RejectsALifetimeOutsideTheEnum(t *testing.T) {
	srv := startCredServer(t)
	_, _, _, err := runCred(t, srv, "agents", "credentials", "create", testAgentID,
		"--stdout", "--lifetime", "45")
	if err == nil {
		t.Fatal("a lifetime of 45 was accepted, but the API refuses it")
	}
	if len(srv.seen()) != 0 {
		t.Errorf("made requests %v for a lifetime the API refuses", srv.seen())
	}
}

// --- The structural guard ------------------------------------------------

// The raw create response must never reach generic output, because it is the
// one response in this area with a secret in an ordinary JSON field. The
// command prints fields it names individually; this test fails if the whole
// record is ever handed to the output layer.
func TestCredentialsCreate_RawResponseNeverReachesOutput(t *testing.T) {
	srv := startCredServer(t)
	path := filepath.Join(t.TempDir(), "s.txt")

	out, errOut, _, err := runCred(t, srv, "agents", "credentials", "create", testAgentID,
		"--out", path, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	// With -o json, a command that wrote the response through the output
	// layer would emit a JSON object carrying clientSecret.
	for _, stream := range []struct{ name, text string }{{"stdout", out}, {"stderr", errOut}} {
		var rec map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(stream.text)), &rec) == nil {
			if _, ok := rec["clientSecret"]; ok {
				t.Errorf("%s emitted the raw create response, secret included", stream.name)
			}
		}
		assertNoSecret(t, stream.name, stream.text)
	}
}

func assertNoSecret(t *testing.T, where, text string) {
	t.Helper()
	if strings.Contains(text, fakeSecret) {
		t.Errorf("the secret appeared on %s: %q", where, text)
	}
}
