package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/selfupdate"
	"github.com/klaassen-consulting/jc/internal/version"
)

// The update check and the upgrade command.
//
// Installing jc from a release archive left no way to find out a newer one
// exists, and building from source needs a Go toolchain most admins running
// this CLI do not have (issue #48). Homebrew would help the people on
// Homebrew; this helps everybody, and does not need a tap to exist first.
//
// The check is bounded, cached, silent on failure and suppressed whenever
// output is going anywhere but a terminal. The upgrade is never automatic.

// updateCheckDisabled reports whether the launch check should be skipped.
//
// The list matters more than the mechanism. A version notice that appears in
// piped JSON, in a CI log, or in an MCP session is a bug, not a feature.
func updateCheckDisabled(cmd *cobra.Command) bool {
	switch {
	case viper.GetBool("quiet"), viper.GetBool("ids"):
		return true
	case os.Getenv("JC_NO_UPDATE_CHECK") != "":
		return true
	case viper.IsSet("update.check") && !viper.GetBool("update.check"):
		return true
	// Anything non-interactive: a person has to be there to act on it.
	case os.Getenv("CI") != "":
		return true
	// stderr is where the notice goes, so that is the stream that decides.
	// A command whose stdout is piped into jq may still have someone watching.
	case !config.IsStderrTerminal():
		return true
	}
	// `mcp serve` speaks a protocol on stdio; nothing else may be written.
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "serve" && c.Parent() != nil && c.Parent().Name() == "mcp" {
			return true
		}
		if c.Name() == "upgrade" {
			return true // it is about to say this itself
		}
	}
	return false
}

// maybeNotifyUpdate prints a one-line notice when a newer release exists.
//
// It runs AFTER the command, so the check can never delay output, and it
// writes to stderr so it can never corrupt a piped result. Every failure is
// silent: a version check is a convenience and must not turn a working
// command into a broken one.
func maybeNotifyUpdate(cmd *cobra.Command) {
	if updateCheckDisabled(cmd) || selfupdate.IsDevBuild(version.Number) {
		return
	}
	dir := config.ConfigDir()
	state := selfupdate.LoadState(dir)
	now := time.Now()

	if state.Due(now) {
		rel, err := selfupdate.LatestRelease(cmd.Context(), http.DefaultClient, selfupdate.CheckTimeout)
		state.LastCheck = now
		if err == nil {
			state.LatestVersion = rel.TagName
		}
		selfupdate.SaveState(dir, state)
	}

	if !state.ShouldNotify(version.Number, now) {
		return
	}
	state.LastNotified = now
	selfupdate.SaveState(dir, state)

	fmt.Fprintf(cmd.ErrOrStderr(),
		"\njc %s → %s available.  Run `jc upgrade` to install it.\n"+
			"  Silence this: jc config set update.check false\n",
		version.Number, state.LatestVersion)
}

func newUpgradeCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Install the newest jc release",
		Long: `Download the newest published jc release and replace this binary with it.

Nothing happens without you asking: jc never updates itself in the background.
A tool that administers an identity platform silently rewriting its own code
would be a supply-chain surface, and it would break anyone pinning a version
in CI or an MDM fleet.

WHAT IS VERIFIED HERE. The download is checked against the SHA-256 published
in the release's checksums.txt. That establishes the bytes arrived intact. It
does NOT by itself establish the release is authentic, because the checksums
come from the same release as the archive.

Releases ARE signed — checksums.txt carries a Sigstore signature from the
release workflow's own identity — but jc does not check it, deliberately. A
Go verifier would add about seventy modules to a CLI that ships with a
hundred and forty-six lines of go.sum, and doubling the dependency tree in
order to verify the dependency tree is a poor trade. Verify by hand when it
matters:

  scripts/verify-release.sh <version> <asset>

or directly:

  cosign verify-blob --bundle checksums.txt.sigstore.json \
    --certificate-identity \
      https://github.com/TheJumpCloud/jc-cli/.github/workflows/release.yml@refs/heads/main \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    checksums.txt

If the binary is somewhere you cannot write — /usr/local/bin on most systems —
jc says so and prints the command to run rather than asking for your password.`,
		Example: `  jc upgrade
  jc upgrade --plan     # show what would happen, download nothing
  jc upgrade --force    # skip the confirmation

  # Turn the launch notice off entirely:
  jc config set update.check false`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpgrade(cmd, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Replace without confirming")
	return cmd
}

func runUpgrade(cmd *cobra.Command, force bool) error {
	out := cmd.ErrOrStderr()
	current := version.Number

	rel, err := selfupdate.LatestRelease(cmd.Context(), http.DefaultClient, selfupdate.DownloadTimeout)
	if err != nil {
		return fmt.Errorf("checking for the newest release: %w", err)
	}

	fmt.Fprintf(out, "current  %s\n", current)
	fmt.Fprintf(out, "latest   %-14s published %s\n", rel.TagName, rel.PublishedAt.Format("2006-01-02"))

	if selfupdate.IsDevBuild(current) {
		fmt.Fprintf(out, "\nThis is a local build, not a release, so jc will not replace it.\n"+
			"Install %s over it deliberately if that is what you want.\n", rel.TagName)
		return nil
	}
	if !selfupdate.Newer(current, rel.TagName) {
		fmt.Fprintln(out, "\nAlready up to date.")
		return nil
	}

	asset, err := selfupdate.AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	target, err := selfupdate.TargetPath()
	if err != nil {
		return err
	}
	writable := selfupdate.Writable(target)

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:   "upgrade",
			Resource: "jc",
			Target:   target,
			Effects: []string{
				"from: " + current,
				"to: " + rel.TagName,
				"asset: " + asset,
				"url: " + selfupdate.AssetURL(rel.TagName, asset),
				"verified against: checksums.txt (integrity, not authenticity)",
				fmt.Sprintf("target writable: %v", writable),
			},
			Reversible: false,
		})
	}

	if !writable {
		return fmt.Errorf("%s is not writable by this user, and jc will not ask for "+
			"elevation to overwrite its own binary. Re-run with enough privilege, or "+
			"install to a directory you own:\n"+
			"  sudo jc upgrade\n"+
			"  curl -fsSL %s | tar xz && mv jc-*/jc ~/.local/bin/jc",
			target, selfupdate.AssetURL(rel.TagName, asset))
	}

	fmt.Fprintf(out, "\ndownload %s\n", asset)
	archive, err := fetch(cmd.Context(), selfupdate.AssetURL(rel.TagName, asset))
	if err != nil {
		return fmt.Errorf("downloading %s: %w", asset, err)
	}
	fmt.Fprintf(out, "         %.1f MB\n", float64(len(archive))/(1<<20))

	sums, err := fetch(cmd.Context(), selfupdate.AssetURL(rel.TagName, "checksums.txt"))
	if err != nil {
		return fmt.Errorf("downloading checksums.txt: %w", err)
	}
	want, err := selfupdate.ChecksumFor(sums, asset)
	if err != nil {
		return err
	}
	if err := selfupdate.VerifyChecksum(archive, want); err != nil {
		return err
	}
	fmt.Fprintf(out, "sha256   verified against checksums.txt\n")

	binary, err := selfupdate.ExtractBinary(archive, asset)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "target   %s   (writable)\n\n", target)

	if !force && shouldConfirm() {
		ok, err := askYesNo(cmd, "Replace it?")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(out, "Cancelled.")
			return nil
		}
	} else if !force && mustAbortWithoutTTY() {
		return fmt.Errorf("upgrading requires --force when there is nobody to confirm")
	}

	if err := selfupdate.Replace(target, binary); err != nil {
		return err
	}

	// The cache now describes a version that is installed, so clear it rather
	// than leaving a notice that would fire once more.
	selfupdate.SaveState(config.ConfigDir(), selfupdate.State{
		LastCheck: time.Now(), LatestVersion: rel.TagName, LastNotified: time.Now(),
	})

	fmt.Fprintf(out, "✓ jc %s\n", rel.TagName)
	result, merr := json.Marshal(map[string]string{
		"from": current, "to": rel.TagName, "path": target,
	})
	if merr != nil {
		return merr
	}
	return output.WriteSingle(cmd.OutOrStdout(), result, output.CurrentOptions())
}

// fetch downloads a release asset.
func fetch(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, selfupdate.DownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}
