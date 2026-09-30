package pwm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func fetcherFor(t *testing.T, body string, err error) Fetcher {
	t.Helper()
	return func(_ context.Context, endpoint string) (json.RawMessage, error) {
		if endpoint != VaultStatusEndpoint {
			t.Errorf("probe hit %q, want %q", endpoint, VaultStatusEndpoint)
		}
		if err != nil {
			return nil, err
		}
		return json.RawMessage(body), nil
	}
}

func TestVaultActive(t *testing.T) {
	cases := []struct {
		name string
		body string
		err  error
		want bool
	}{
		// The live shape, from an org that has migrated.
		{"active", `{"isActive":true,"isPam":false,"jumpcloudSsoApplicationId":"6ab","vaultoneConsoleUrl":"https://x"}`, nil, true},
		{"inactive", `{"isActive":false,"isPam":false}`, nil, false},

		// Every uncertain answer must mean "do not claim a migration".
		// Claiming one wrongly replaces a true error with a misleading one.
		{"probe errored", "", errors.New("network down"), false},
		{"undecodable", `not json`, nil, false},
		{"flag absent", `{"isPam":false}`, nil, false},
		{"empty object", `{}`, nil, false},
		{"null", `null`, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := VaultActive(context.Background(), fetcherFor(t, c.body, c.err))
			if got != c.want {
				t.Errorf("VaultActive() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestMigrationHint(t *testing.T) {
	err := MigrationHint("/passwordmanager/overview")
	msg := err.Error()
	for _, want := range []string{
		"not active on this organization",
		"/passwordmanager/overview",
		"Password Vault",
		"have not migrated",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("hint missing %q:\n%s", want, msg)
		}
	}
}

// TestMigrationHint_NamesVaultCommandOnceItExists is a tripwire, in the same
// spirit as TestEveryLeafIsClassified: it fails the moment a `jc
// password-vault` command is registered, so the hint stops pointing people at
// the console and names the command instead.
//
// It cannot import internal/cmd (that would be an import cycle), so it keys
// off the hint text itself. When the command lands, update MigrationHint to
// name it and update this test to assert that name.
func TestMigrationHint_NamesVaultCommandOnceItExists(t *testing.T) {
	msg := MigrationHint("/passwordmanager/users").Error()
	mentionsCommand := strings.Contains(msg, "jc password-vault")
	pointsAtConsole := strings.Contains(msg, "JumpCloud console")

	if mentionsCommand && pointsAtConsole {
		t.Error("hint names `jc password-vault` AND still points at the console; " +
			"drop the console fallback now that the command exists")
	}
	if !mentionsCommand && !pointsAtConsole {
		t.Error("hint neither names `jc password-vault` nor points at the console; " +
			"it must tell the operator where to go")
	}
}
