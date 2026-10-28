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

// TestNoCredentialIssuingTools guards the same decision for AI agent
// credentials, where the shape of the risk is inverted.
//
// A Password Vault secret leaks on a READ. An agent credential leaks on a
// WRITE: POST /agents/{id}/inbound-credentials returns a clientSecret exactly
// once, and no read returns it afterwards. So the dangerous call here is a
// creation — which classifies as mutating, gets a plan and a confirmation,
// and still puts a live secret into the transcript when it succeeds. The
// gates are about undoing a change; nothing undoes a disclosed secret.
//
// Listing credentials is fine and is exposed: that endpoint returns a client
// id, type, status and timestamps and no secret, verified against every agent
// on a live tenant. So this cannot ban the word "credential" — it bans
// credential tools that ISSUE, which is the subset that returns a value.
//
// Written before any such tool exists, so the three credential writes cannot
// arrive in MCP by accident later.
func TestNoCredentialIssuingTools(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	issuing := []string{"create", "add", "new", "issue", "rotate", "regenerate", "renew", "reset", "replace"}

	for _, tool := range res.Tools {
		lower := strings.ToLower(tool.Name)
		if !strings.Contains(lower, "credential") {
			continue
		}
		for _, verb := range issuing {
			if !strings.Contains(lower, verb) {
				continue
			}
			t.Errorf("tool %q issues a credential.\n\n"+
				"The call that creates an agent credential is the only call that ever "+
				"returns its clientSecret, so a successful tool invocation puts a live "+
				"secret into the transcript — and it classifies as mutating, which means "+
				"the plan and the confirmation both pass it. These writes are CLI-only on "+
				"purpose; see internal/cmd/agents_credentials.go. If this is a false "+
				"positive, rename the tool rather than relaxing this test.", tool.Name)
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

// TestNoToolLocksADeviceAndHandsBackThePIN guards the fourteenth Apple MDM
// device action, which is the only one with no tool.
//
// POST …/devices/{id}/lockwithgeneratedpin locks a Mac AND returns the PIN
// that unlocks it, which puts it in both categories this file exists for: it
// is destructive, so it would get a plan, a step-up prompt and a signed
// manifest -- and all of that is about undoing a change, while the damage
// here is a credential in a transcript that nothing can retract. The lock
// itself is available through apple_mdm_device_lock, where the caller
// supplies the PIN and the API never returns one.
//
// Written when the actions landed, so the tool cannot arrive later by
// analogy with its twelve neighbours.
func TestNoToolLocksADeviceAndHandsBackThePIN(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	// Name fragments that would indicate the generated-PIN route was wired
	// up. "pin" alone is not one of them: apple_mdm_device_lock legitimately
	// talks about the PIN the caller supplies.
	bannedName := []string{"generated_pin", "generatedpin", "lockpin", "lock_pin", "with_generated"}
	// And the same thing arriving under another name, promising the value.
	promises := []string{
		"returns the pin", "return the pin", "returns a pin", "returns the generated pin",
		"generates a pin and returns", "hands back the pin",
	}

	for _, tool := range res.Tools {
		lower := strings.ToLower(tool.Name)
		for _, b := range bannedName {
			if strings.Contains(lower, b) {
				t.Errorf("tool %q looks like the lockwithgeneratedpin route.\n\n"+
					"That call locks a device and returns the PIN that unlocks it. Being "+
					"destructive it would get a plan and a step-up prompt, and neither "+
					"un-writes a credential from a transcript. It is CLI-only; see "+
					"internal/cmd/apple_mdm_device_actions.go. Use apple_mdm_device_lock, "+
					"where the caller supplies the PIN.", tool.Name)
			}
		}
		desc := strings.ToLower(tool.Description)
		for _, p := range promises {
			if strings.Contains(desc, p) {
				t.Errorf("tool %q describes returning a device lock PIN: %q found in its "+
					"description. That read is CLI-only; see "+
					"internal/cmd/apple_mdm_device_actions.go", tool.Name, p)
			}
		}
	}
}
