package cmd

import (
	"testing"

	"github.com/spf13/viper"
)

// overrideIsStdinPiped overrides isStdinPiped for the test and restores on cleanup.
func overrideIsStdinPiped(t *testing.T, piped bool) {
	t.Helper()
	orig := isStdinPiped
	t.Cleanup(func() { isStdinPiped = orig })
	isStdinPiped = func() bool { return piped }
}

func TestShouldConfirm_ForceSkips(t *testing.T) {
	viper.Reset()
	viper.SetDefault("defaults.confirm_destructive", true)
	viper.Set("force", true)
	defer viper.Set("force", false)
	overrideIsStdinPiped(t, false)

	if shouldConfirm() {
		t.Error("shouldConfirm should return false when --force is set")
	}
	if mustAbortWithoutTTY() {
		t.Error("mustAbortWithoutTTY should return false when --force is set")
	}
}

func TestShouldConfirm_NonInteractiveSkips(t *testing.T) {
	viper.Reset()
	viper.SetDefault("defaults.confirm_destructive", true)
	viper.Set("non-interactive", true)
	defer viper.Set("non-interactive", false)
	overrideIsStdinPiped(t, false)

	if shouldConfirm() {
		t.Error("shouldConfirm should return false when --non-interactive is set")
	}
	if mustAbortWithoutTTY() {
		t.Error("mustAbortWithoutTTY should return false when --non-interactive is set")
	}
}

func TestShouldConfirm_ConfirmDestructiveFalseSkips(t *testing.T) {
	viper.Reset()
	viper.Set("defaults.confirm_destructive", false)
	overrideIsStdinPiped(t, false)

	if shouldConfirm() {
		t.Error("shouldConfirm should return false when defaults.confirm_destructive is false")
	}
	if mustAbortWithoutTTY() {
		t.Error("mustAbortWithoutTTY should return false when confirm_destructive is false")
	}
}

func TestShouldConfirm_PipedStdinSkips(t *testing.T) {
	viper.Reset()
	viper.SetDefault("defaults.confirm_destructive", true)
	overrideIsStdinPiped(t, true) // piped stdin, no --force

	if shouldConfirm() {
		t.Error("shouldConfirm should return false when stdin is piped")
	}
	if !mustAbortWithoutTTY() {
		t.Error("mustAbortWithoutTTY should return true when stdin is piped and no skip flags set")
	}
}

func TestShouldConfirm_PipedStdinWithForce(t *testing.T) {
	viper.Reset()
	viper.SetDefault("defaults.confirm_destructive", true)
	viper.Set("force", true)
	defer viper.Set("force", false)
	overrideIsStdinPiped(t, true) // piped stdin, but --force set

	if shouldConfirm() {
		t.Error("shouldConfirm should return false")
	}
	if mustAbortWithoutTTY() {
		t.Error("mustAbortWithoutTTY should return false when --force is set, even with piped stdin")
	}
}

func TestShouldConfirm_TTYStdinConfirms(t *testing.T) {
	viper.Reset()
	viper.SetDefault("defaults.confirm_destructive", true)
	overrideIsStdinPiped(t, false) // TTY

	if !shouldConfirm() {
		t.Error("shouldConfirm should return true when stdin is a TTY and no skip flags set")
	}
	if mustAbortWithoutTTY() {
		t.Error("mustAbortWithoutTTY should return false when stdin is a TTY")
	}
}
