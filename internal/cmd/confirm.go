package cmd

import "github.com/spf13/viper"

// shouldSkipConfirm returns true if the confirmation prompt should be skipped
// AND the operation should proceed (i.e., the user explicitly opted out of
// confirmation via --force, --non-interactive, or confirm_destructive=false).
func shouldSkipConfirm() bool {
	if viper.GetBool("force") {
		return true
	}
	if viper.GetBool("non-interactive") {
		return true
	}
	// Only skip when confirm_destructive was explicitly configured to false.
	// When unset (e.g., after viper.Reset()), default to confirming.
	if viper.IsSet("defaults.confirm_destructive") && !viper.GetBool("defaults.confirm_destructive") {
		return true
	}
	return false
}

// shouldConfirm returns true if a destructive command should prompt for
// confirmation. Returns false when the prompt should be skipped — either
// because the user explicitly opted out (--force, etc.) or because stdin
// is not a TTY (piped or detached). When false and --force is NOT set,
// callers must cancel the operation rather than proceeding silently.
func shouldConfirm() bool {
	if shouldSkipConfirm() {
		return false
	}
	if isStdinPiped() {
		return false
	}
	return true
}

// mustAbortWithoutTTY returns true when stdin is not interactive and no
// explicit skip flag (--force, --non-interactive, confirm_destructive=false)
// was set. Callers should cancel the operation to avoid executing destructive
// commands without user consent.
func mustAbortWithoutTTY() bool {
	return !shouldSkipConfirm() && isStdinPiped()
}
