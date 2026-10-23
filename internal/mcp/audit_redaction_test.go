package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The audit log and the signed manifests of destructive operations record the
// arguments a tool was called with. Both pass them through redactParams,
// whose key list is an INCLUSION list — so anything nobody thought to list is
// written verbatim to a file on disk.
//
// This file exists because that failed open exactly once in a way worth
// keeping a test for: apple_mdm_device_erase and apple_mdm_device_lock were
// the first tools to take a credential as an argument, and the six-digit PIN
// they carry — the only way past a locked Mac's firmware lock — landed in the
// audit log in clear text. A background security review caught it; the log
// line was then read off disk to confirm it before the fix.

// TestAuditLogDoesNotRecordADeviceLockPIN is the regression test for that
// leak, written against the file rather than against the redaction map, so it
// still holds if the redaction path changes shape.
func TestAuditLogDoesNotRecordADeviceLockPIN(t *testing.T) {
	for _, tool := range []string{"apple_mdm_device_erase", "apple_mdm_device_lock"} {
		t.Run(tool, func(t *testing.T) {
			setupToolTest(t)
			srv, _ := startActionRecorder(t, true)
			overrideV2ClientForTest(t, srv.URL)
			logPath := filepath.Join(t.TempDir(), "audit.log")
			cs := connectToolTestServer(t, Options{AuditLogPath: logPath})

			const pin = "483927"
			res := callTool(t, cs, tool, actionArgs(map[string]any{"pin": pin, "execute": true}))
			if res.IsError {
				t.Fatalf("%s errored: %s", tool, getResultText(t, res))
			}

			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("reading the audit log: %v", err)
			}
			if !strings.Contains(string(data), tool) {
				t.Fatalf("the audit log does not mention %s, so this test proved nothing:\n%s",
					tool, data)
			}
			if strings.Contains(string(data), pin) {
				t.Errorf("the device lock PIN is in the audit log in clear text:\n%s\n\n"+
					"Add the parameter name to sensitiveParamKeys in server.go.", data)
			}
			if !strings.Contains(string(data), "REDACTED") {
				t.Errorf("the audit log records no redaction, so the pin argument was not "+
					"recognized as sensitive:\n%s", data)
			}
		})
	}
}

// secretish matches argument names that look like they carry a credential.
// Deliberately broad: the cost of a false positive is one line in
// notASecret below, and the cost of a false negative is a credential on disk.
var secretish = regexp.MustCompile(`(?i)(^|_)(pin|passcode|password|passphrase|secret|token|key|credential)s?($|_)`)

// notASecret names every argument that matches secretish and is nonetheless
// safe to record, with the reason. This is the half that makes the guard fail
// closed: a new argument matching the pattern must appear either in
// sensitiveParamKeys or here, and until it does this test fails.
var notASecret = map[string]string{
	"token_id":          "an ADE token's object id, not the token value",
	"payloadtype_or_id": "an Apple payload type such as com.apple.wifi.managed",
	"key":               "an Edge Config / key-value item name, not a credential",
	"keys":              "registry rows for a Windows MDM policy",
	"product_key":       "an Apple software update identifier, not a credential",
	"public_key":        "a public key — already redacted, listed here for completeness",
	"sort_key":          "a sort field name",
	"partition_key":     "a partition field name",
	// Matched by the pattern, genuinely not credentials. Each one was read
	// before being listed here.
	"user_password_expiration_action": "an LDAP policy action name, not a password",
	"disallow_common_passwords":       "a boolean password-policy setting",
	"key_id":                          "an SSH key's object id, not the key material",
}

// TestEverySecretishToolArgumentIsRedacted walks every registered tool's
// input schema and checks the credential-looking arguments against the
// redaction list. It is the structural half of the fix: the PIN leak happened
// because nothing connected "a tool gained a credential argument" to "the
// redaction list needs a new entry".
func TestEverySecretishToolArgumentIsRedacted(t *testing.T) {
	setupToolTest(t)
	cs := connectToolTestServer(t, Options{})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	var checked int
	for _, tool := range res.Tools {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshalling the schema for %s: %v", tool.Name, err)
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("decoding the schema for %s: %v", tool.Name, err)
		}
		for name := range schema.Properties {
			if !secretish.MatchString(name) {
				continue
			}
			checked++
			if sensitiveParamKeys[name] {
				continue
			}
			if _, exempt := notASecret[name]; exempt {
				continue
			}
			t.Errorf("%s takes %q, which looks like a credential, but it is neither in "+
				"sensitiveParamKeys nor declared in notASecret.\n\n"+
				"Tool arguments are written to the audit log and to the signed manifest of "+
				"every destructive operation. Add it to sensitiveParamKeys if it carries a "+
				"value, or to notASecret with the reason if it does not.", tool.Name, name)
		}
	}

	// If the schema walk ever stops finding anything, the guard has gone
	// blind rather than clean.
	if checked == 0 {
		t.Fatal("no credential-looking arguments were examined at all — the schema walk is broken")
	}
}

// And the redaction itself, at the unit level: a parameter named pin is
// replaced rather than merely absent from the output.
func TestRedactParamsReplacesAPIN(t *testing.T) {
	out := string(redactParams(json.RawMessage(`{"device_id":"6adb8d357ec0ff0001d101ea","pin":"483927"}`)))
	if strings.Contains(out, "483927") {
		t.Errorf("redactParams left the PIN in place: %s", out)
	}
	if !strings.Contains(out, "6adb8d357ec0ff0001d101ea") {
		t.Errorf("redactParams dropped the device id, which the audit log needs: %s", out)
	}
}
