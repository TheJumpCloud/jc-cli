package cmd

import (
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/pwm"
)

// TestMigrationHintNamesTheVaultCommand keeps the Password Manager migration
// hint and the Password Vault command tree in step.
//
// The hint lives in internal/pwm, which cannot import internal/cmd, so the
// check has to live here — this is the only package that can see both. An
// earlier version of this test lived in internal/pwm and could only check the
// message against itself, which is weaker than it sounded: it could not tell
// whether the command it names exists.
func TestMigrationHintNamesTheVaultCommand(t *testing.T) {
	var exists bool
	for _, c := range NewRootCmd().Commands() {
		if c.Name() == "password-vault" {
			exists = true
			break
		}
	}

	msg := pwm.MigrationHint("/passwordmanager/overview").Error()
	names := strings.Contains(msg, "jc password-vault")

	switch {
	case exists && !names:
		t.Error("`jc password-vault` exists but the Password Manager migration hint " +
			"still sends people to the console. Update pwm.MigrationHint to name it.")
	case !exists && names:
		t.Error("the Password Manager migration hint names `jc password-vault`, which " +
			"is not registered. Pointing people at a command that does not exist is " +
			"the mistake the empirical-gate convention made for three months.")
	}
}
