package mcp

import (
	"context"
	"strings"
	"testing"
)

// TestNoSecretReadingTools is the guard on a decision that is easy to erode.
//
// jc's CLI can read a credential's stored secret. The MCP server deliberately
// cannot, and no tool description or confirmation prompt would make it safe
// to add: this server's safety model classifies tools by what they MUTATE, so
// a secret read lands as read-only and passes every gate — the step-up
// prompt, the plan-by-default convention, the destructive classification. All
// of it is about undoing a change, and nothing undoes a value that has
// already been written into a transcript.
//
// So the rule is structural rather than advisory, and this test is the place
// it is enforced. If a future change adds a vault tool whose name or
// description reaches for a secret, this fails and the reasoning is here to
// read before anyone overrides it.
func TestNoSecretReadingTools(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	// Names that would indicate a secret-bearing endpoint was exposed. These
	// mirror the four CLI-only commands and the endpoints behind them.
	bannedName := []string{"secret", "with_secret", "with-secret", "mfa_code", "mfa-code", "totp"}

	for _, tool := range res.Tools {
		lower := strings.ToLower(tool.Name)
		for _, b := range bannedName {
			if strings.Contains(lower, b) {
				t.Errorf("tool %q looks like it exposes a credential secret.\n\n"+
					"Reading a secret mutates nothing, so it classifies as read-only and "+
					"passes every gate this server has — and the damage is done once the "+
					"value is in a transcript. These reads are CLI-only on purpose; see "+
					"internal/cmd/password_vault_secrets.go. If this is a false positive, "+
					"rename the tool rather than relaxing this test.", tool.Name)
			}
		}
	}
}

// TestNoToolDescribesReadingASecret catches the same thing arriving under an
// innocuous name — a tool called password_vault_credential_detail whose
// description offers to return the password.
func TestNoToolDescribesReadingASecret(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	// Phrases that promise a value rather than merely mentioning secrets.
	// The existing vault tools DO talk about secrets — they say they do not
	// return them — so matching the word alone would fail on correct code.
	promises := []string{
		"returns the password", "return the password",
		"returns the secret", "return the secret",
		"including its secret", "including the secret",
		"with its secret value", "reveals the",
	}
	for _, tool := range res.Tools {
		lower := strings.ToLower(tool.Description)
		for _, p := range promises {
			if strings.Contains(lower, p) {
				t.Errorf("tool %q describes returning a secret value: %q found in its "+
					"description. Secret reads are CLI-only; see "+
					"internal/cmd/password_vault_secrets.go", tool.Name, p)
			}
		}
	}
}
