package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/config"
)

// The suppression rules are the security-relevant part of the update check.
// A version notice that reaches piped output corrupts it; one that reaches an
// MCP session corrupts a protocol stream. Each rule gets a test.
func TestUpdateCheckDisabled(t *testing.T) {
	restore := config.SetTerminalFuncForTest(func(int) bool { return true })
	defer restore()

	base := func(t *testing.T) *cobra.Command {
		t.Helper()
		viper.Reset()
		viper.Set("update.check", true)
		t.Setenv("JC_NO_UPDATE_CHECK", "")
		t.Setenv("CI", "")
		return &cobra.Command{Use: "list"}
	}

	t.Run("interactive default is enabled", func(t *testing.T) {
		if updateCheckDisabled(base(t)) {
			t.Error("an interactive run should check")
		}
	})

	t.Run("quiet", func(t *testing.T) {
		cmd := base(t)
		viper.Set("quiet", true)
		if !updateCheckDisabled(cmd) {
			t.Error("--quiet must suppress the notice")
		}
	})

	t.Run("ids only", func(t *testing.T) {
		cmd := base(t)
		viper.Set("ids", true)
		// --ids output is piped into xargs; a notice would be read as an id.
		if !updateCheckDisabled(cmd) {
			t.Error("--ids must suppress the notice")
		}
	})

	t.Run("config off", func(t *testing.T) {
		cmd := base(t)
		viper.Set("update.check", false)
		if !updateCheckDisabled(cmd) {
			t.Error("update.check=false must suppress the notice")
		}
	})

	t.Run("env off", func(t *testing.T) {
		cmd := base(t)
		t.Setenv("JC_NO_UPDATE_CHECK", "1")
		if !updateCheckDisabled(cmd) {
			t.Error("JC_NO_UPDATE_CHECK must suppress the notice")
		}
	})

	t.Run("CI", func(t *testing.T) {
		cmd := base(t)
		t.Setenv("CI", "true")
		if !updateCheckDisabled(cmd) {
			t.Error("CI must suppress the notice — nobody is there to act on it")
		}
	})

	t.Run("stderr not a terminal", func(t *testing.T) {
		cmd := base(t)
		undo := config.SetTerminalFuncForTest(func(int) bool { return false })
		defer undo()
		if !updateCheckDisabled(cmd) {
			t.Error("a redirected stderr must suppress the notice")
		}
	})

	t.Run("mcp serve", func(t *testing.T) {
		viper.Reset()
		viper.Set("update.check", true)
		t.Setenv("JC_NO_UPDATE_CHECK", "")
		t.Setenv("CI", "")
		// `mcp serve` speaks a protocol on stdio; anything else written to
		// the stream corrupts the session.
		mcp := &cobra.Command{Use: "mcp"}
		serve := &cobra.Command{Use: "serve"}
		mcp.AddCommand(serve)
		if !updateCheckDisabled(serve) {
			t.Error("mcp serve must suppress the notice")
		}
	})

	t.Run("upgrade itself", func(t *testing.T) {
		viper.Reset()
		viper.Set("update.check", true)
		t.Setenv("JC_NO_UPDATE_CHECK", "")
		t.Setenv("CI", "")
		up := &cobra.Command{Use: "upgrade"}
		if !updateCheckDisabled(up) {
			t.Error("jc upgrade says this itself; the notice would be duplicate")
		}
	})
}
