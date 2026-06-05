package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/keychain"
	"github.com/klaassen-consulting/jc/internal/version"
)

// doctorReport is the structured shape of `jc doctor`'s output. JSON
// mode marshals this directly; text mode renders it through human
// formatters below. Every secret-bearing field carries a fingerprint
// (last 4 chars masked with ****) and never the raw value.
//
// Borrowed shape and the no-auth-required principle from jamf-cli
// (`internal/commands/doctor.go`): the command must work when the
// operator's setup is broken — that's the whole point.
type doctorReport struct {
	Build   buildSection   `json:"build"`
	Profile profileSection `json:"profile"`
	Config  configSection  `json:"config"`
	Auth    authSection    `json:"auth"`
	API     apiSection     `json:"api"`
	LLM     llmSection     `json:"llm"`
	MCP     mcpSection     `json:"mcp"`
}

type buildSection struct {
	Version   string `json:"version"`
	GoVersion string `json:"go_version"`
	OSArch    string `json:"os_arch"`
}

type profileSection struct {
	Active    string   `json:"active"`
	Source    string   `json:"source"`
	Available []string `json:"available"`
}

type configSection struct {
	Path    string `json:"path"`
	Dir     string `json:"dir"`
	Exists  bool   `json:"exists"`
	FileMode string `json:"file_mode,omitempty"`
	DirMode  string `json:"dir_mode,omitempty"`
}

type authSection struct {
	Method      string `json:"method"`
	Source      string `json:"source"`
	Fingerprint string `json:"fingerprint,omitempty"`
	OrgID       string `json:"org_id,omitempty"`
	OrgIDSource string `json:"org_id_source,omitempty"`
}

type apiSection struct {
	V1BaseURL   string    `json:"v1_base_url"`
	V2BaseURL   string    `json:"v2_base_url"`
	Probe       *apiProbe `json:"probe,omitempty"`
}

type apiProbe struct {
	Status     string `json:"status"`
	StatusCode int    `json:"status_code,omitempty"`
	LatencyMS  int64  `json:"latency_ms,omitempty"`
	Error      string `json:"error,omitempty"`
}

type llmSection struct {
	Provider     string `json:"provider"`
	Model        string `json:"model,omitempty"`
	URL          string `json:"url,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
	APIKeySource string `json:"api_key_source,omitempty"`
}

type mcpSection struct {
	RequireStepUp  bool   `json:"require_step_up"`
	StepUpAuth     string `json:"step_up_authenticator,omitempty"`
	SigningEnabled bool   `json:"signing_enabled"`
	SigningPubkey  string `json:"signing_pubkey,omitempty"`
	WebhookURL     string `json:"webhook_url,omitempty"`
}

func newDoctorCmd() *cobra.Command {
	var noProbe bool
	var probeTimeout time.Duration
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose jc configuration: profile, auth source, API reachability, LLM, MCP",
		Long: `Run a no-auth-required diagnostic that prints what jc actually resolved at
runtime: active profile, config file path, API key source (flag / env / profile
/ keychain), JumpCloud API reachability, and the LLM + MCP server settings.

Designed to work even when authentication is misconfigured — the connectivity
probe distinguishes "DNS/TLS works but the key is wrong" from "the host is
unreachable". Use this first when triaging an auth or connectivity issue;
secrets are fingerprinted (last 4 chars) and never printed in full.

Examples:
  jc doctor                       # human-readable text output
  jc doctor --output json         # machine-readable for scripts / runbooks
  jc doctor --no-probe            # skip the HEAD request (offline triage)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Did the operator pass --api-key explicitly? The flag is
			// defined on the root command (PersistentFlag). cobra walks
			// up the tree to find it, so this works from a subcommand.
			flagAPIKeySet := false
			if root := cmd.Root(); root != nil {
				if f := root.PersistentFlags().Lookup("api-key"); f != nil {
					flagAPIKeySet = f.Changed
				}
			}
			rep := collectDoctorReport(cmd.Context(), !noProbe, probeTimeout, flagAPIKeySet)

			// Respect --output json explicitly OR the global --output flag /
			// JC_OUTPUT env / config default. JSON is the script-friendly
			// path; "text" is the human default.
			output := config.Output()
			if jsonOut || output == "json" {
				return printDoctorJSON(cmd.OutOrStdout(), rep)
			}
			return printDoctorText(cmd.OutOrStdout(), rep)
		},
	}

	cmd.Flags().BoolVar(&noProbe, "no-probe", false, "Skip the HEAD connectivity probe (offline triage)")
	cmd.Flags().DurationVar(&probeTimeout, "probe-timeout", 5*time.Second, "Timeout for the connectivity probe")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Shorthand for --output json")

	return cmd
}

// collectDoctorReport assembles the full report. Pure data collection —
// no I/O beyond the optional probe — so it's safe to call from tests
// with probe disabled.
//
// flagAPIKeySet must come from the caller (it's the root command's
// --api-key Changed bit) so collectAuth can correctly attribute the
// resolved value to the flag vs JC_API_KEY env when both are set.
func collectDoctorReport(ctx context.Context, probe bool, timeout time.Duration, flagAPIKeySet bool) doctorReport {
	rep := doctorReport{
		Build:   collectBuild(),
		Profile: collectProfile(),
		Config:  collectConfig(),
		Auth:    collectAuth(flagAPIKeySet),
		API:     collectAPI(),
		LLM:     collectLLM(),
		MCP:     collectMCP(),
	}
	if probe {
		rep.API.Probe = runAPIProbe(ctx, timeout)
	}
	return rep
}

func collectBuild() buildSection {
	return buildSection{
		Version:   strings.TrimPrefix(version.Number, "v"),
		GoVersion: runtime.Version(),
		OSArch:    runtime.GOOS + "/" + runtime.GOARCH,
	}
}

func collectProfile() profileSection {
	active := config.ActiveProfile()
	source := "config"
	switch {
	case os.Getenv("JC_PROFILE") != "":
		source = "JC_PROFILE env"
	case active == "default":
		// Could be explicit or fallback; we can't distinguish without
		// reading the raw YAML. Be honest about the ambiguity.
		source = "config (or default)"
	}

	// List available profiles from the YAML 'profiles' map.
	var available []string
	if profiles, ok := viper.AllSettings()["profiles"].(map[string]any); ok {
		for name := range profiles {
			available = append(available, name)
		}
		sort.Strings(available)
	}

	return profileSection{
		Active:    active,
		Source:    source,
		Available: available,
	}
}

func collectConfig() configSection {
	path := config.ConfigPath()
	dir := config.ConfigDir()

	cs := configSection{Path: path, Dir: dir}
	if info, err := os.Stat(path); err == nil {
		cs.Exists = true
		cs.FileMode = fmt.Sprintf("%#o", info.Mode().Perm())
	}
	if info, err := os.Stat(dir); err == nil {
		cs.DirMode = fmt.Sprintf("%#o", info.Mode().Perm())
	}
	return cs
}

// collectAuth honestly reports where the API key came from, BEFORE
// resolving keychain references. Order matches config.APIKey():
//   1. --api-key flag (viper's "api_key" key, set via PersistentFlag)
//   2. JC_API_KEY env var (also bound to viper's "api_key" key)
//   3. profile config (plaintext or keychain:// reference)
//
// (1) and (2) are indistinguishable at the viper level since they share
// the binding. We disambiguate by checking whether the binding was set
// via flag — viper's IsSet on a binding name reports whether ANY non-
// default source provided the value, so we additionally compare against
// the raw env value: when the resolved value matches the env value
// (and the env IS set), it's safe to call it env; otherwise it's the
// flag, which has higher precedence in cobra/viper.
//
// Bugbot caught the original ordering, which misattributed a flag
// override to env when the operator happened to set both to the same
// string. The fix checks the flag-set bit first via cobra-provided
// state plumbed in by the caller (we read the persistent flag
// definition's Changed bit).
func collectAuth(flagAPIKeySet bool) authSection {
	as := authSection{Method: config.AuthMethod()}
	profile := config.ActiveProfile()

	// Service-account profiles short-circuit: api.NewClient() honors
	// AuthMethod() == "service_account" with valid client_id +
	// client_secret BEFORE it ever consults the API key. Reporting
	// "JC_API_KEY env" when the probe (and every other jc command)
	// actually uses OAuth Bearer would be a lie — Bugbot caught this
	// when JC_API_KEY happened to be set in the environment for a
	// different profile or session.
	if as.Method == "service_account" && config.ClientID() != "" && config.ClientSecret() != "" {
		as.Source = "service_account (OAuth)"
		as.Fingerprint = fingerprint(config.ClientID())
		as.OrgID, as.OrgIDSource = collectOrgID(profile)
		return as
	}

	envKey := os.Getenv("JC_API_KEY")
	flagOrEnv := viper.GetString("api_key")
	profileRaw := viper.GetString("profiles." + profile + ".api_key")

	switch {
	case flagAPIKeySet && flagOrEnv != "":
		// Flag overrides env in cobra precedence. Reported as flag
		// even when env happens to have the same value.
		as.Source = "--api-key flag"
		as.Fingerprint = fingerprint(flagOrEnv)
	case flagOrEnv != "" && envKey != "" && flagOrEnv == envKey:
		as.Source = "JC_API_KEY env"
		as.Fingerprint = fingerprint(envKey)
	case flagOrEnv != "":
		// Active value differs from env (or env is unset) and the flag
		// wasn't set — only path that fits is the profile config that
		// viper auto-merged into the key. Fall through to profile branch
		// below; we'd never get here in practice because the env path
		// above would have matched. Belt-and-suspenders.
		as.Source = "viper-resolved (config or env)"
		as.Fingerprint = fingerprint(flagOrEnv)
	case strings.HasPrefix(profileRaw, "keychain://"):
		as.Source = fmt.Sprintf("keychain (%s)", strings.TrimPrefix(profileRaw, "keychain://"))
		if resolved, err := keychain.Resolve(profileRaw); err == nil {
			as.Fingerprint = fingerprint(resolved)
		} else {
			as.Fingerprint = "(keychain unavailable)"
		}
	case profileRaw != "":
		as.Source = "profile config (plaintext)"
		as.Fingerprint = fingerprint(profileRaw)
	case as.Method == "service_account":
		// service_account profile without valid client creds — neither
		// auth path will work. Surface it honestly so the operator
		// knows to fix the client_id/client_secret.
		as.Source = "service_account (no client credentials)"
	default:
		as.Source = "(unset)"
	}

	as.OrgID, as.OrgIDSource = collectOrgID(profile)
	return as
}

// collectOrgID returns the resolved org ID + source (env vs profile).
// Pulled out of collectAuth so the service-account short-circuit at
// the top of that function still reports org ID consistently.
func collectOrgID(profile string) (string, string) {
	if envOrg := os.Getenv("JC_ORG_ID"); envOrg != "" {
		return envOrg, "JC_ORG_ID env"
	}
	if profileOrg := viper.GetString("profiles." + profile + ".org_id"); profileOrg != "" {
		return profileOrg, "profile config"
	}
	return "", ""
}

func collectAPI() apiSection {
	return apiSection{
		V1BaseURL: api.BaseURL,
		V2BaseURL: api.V2BaseURL,
	}
}

func collectLLM() llmSection {
	ls := llmSection{
		Provider: viper.GetString("ask.provider"),
		Model:    viper.GetString("ask.model"),
		URL:      viper.GetString("ask.url"),
	}
	envKey := os.Getenv("JC_ASK_API_KEY")
	profileKey := viper.GetString("ask.api_key")
	switch {
	case envKey != "":
		ls.APIKey = fingerprint(envKey)
		ls.APIKeySource = "JC_ASK_API_KEY env"
	case profileKey != "":
		ls.APIKey = fingerprint(profileKey)
		ls.APIKeySource = "ask.api_key config"
	default:
		ls.APIKey = "(unset)"
	}
	return ls
}

func collectMCP() mcpSection {
	ms := mcpSection{
		RequireStepUp:  config.MCPRequireStepUp(),
		StepUpAuth:     viper.GetString("mcp.step_up_authenticator"),
		SigningEnabled: config.MCPSignDestructiveOps(),
		WebhookURL:     config.MCPApprovalWebhookURL(),
	}
	if ms.StepUpAuth == "" && ms.RequireStepUp {
		ms.StepUpAuth = "auto"
	}
	// SigningPubkey fingerprint when present in the active profile's
	// config — the operator may have generated one even when signing
	// is currently disabled.
	if pub := viper.GetString("profiles." + config.ActiveProfile() + ".signing_pubkey"); pub != "" {
		ms.SigningPubkey = fingerprint(pub)
	}
	return ms
}

// runAPIProbe hits a known JumpCloud V2 endpoint via the same client
// jc uses for every other API call — so it exercises whichever auth
// method the active profile is configured for (api_key OR
// service_account OAuth). The status field distinguishes "ok" from
// "auth_failed" from "unreachable" so the operator can tell "DNS/TLS
// works but the key is wrong" from "the host is unreachable" — that's
// the whole reason this command exists.
//
// We don't HEAD the API root: JumpCloud's edge returns 404 there,
// indistinguishable from a real outage. `GET /v2/usergroups?limit=1`
// is a few KB and hits a real handler.
//
// Bugbot on PR #42 caught the original hand-rolled HTTP probe which
// only sent `x-api-key` — a valid service_account profile would
// report `auth_failed` even though every other jc command worked.
// Routing through api.NewV2Client fixes that without leaking the
// client-construction error into our return shape.
//
// Never returns a non-nil error to the caller; failures are encoded
// in the report so JSON consumers always get a parseable response.
func runAPIProbe(ctx context.Context, timeout time.Duration) *apiProbe {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	client, err := api.NewV2Client()
	if err != nil {
		// No credentials configured. Distinct from "host is unreachable"
		// — we know nothing about the host yet because we never asked.
		return &apiProbe{
			Status: "no_credentials",
			Error:  err.Error(),
		}
	}

	_, err = client.ListAll(ctx, "/usergroups", api.V2ListOptions{Limit: 1})
	latency := time.Since(start)
	probe := classifyProbeError(err)
	probe.LatencyMS = latency.Milliseconds()
	return probe
}

// classifyProbeError turns a (client.ListAll) result into the
// status/status_code/error triple the report uses. Split from
// runAPIProbe so tests can validate the error classification without
// needing to mock the entire HTTP client.
func classifyProbeError(err error) *apiProbe {
	if err == nil {
		return &apiProbe{Status: "ok", StatusCode: http.StatusOK}
	}
	// Unwrap to find an APIError if present. APIError carries the HTTP
	// status code from the real response; transport errors (DNS,
	// connection refused, timeout) won't.
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		probe := &apiProbe{StatusCode: apiErr.StatusCode}
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized, apiErr.StatusCode == http.StatusForbidden:
			probe.Status = "auth_failed"
		default:
			probe.Status = fmt.Sprintf("http_%d", apiErr.StatusCode)
		}
		return probe
	}
	return &apiProbe{Status: "unreachable", Error: err.Error()}
}

// fingerprint masks all but the last 4 characters. Matches the
// convention used by the TTY step-up prompt and the audit log's
// signing-pubkey display. Returns "(unset)" when empty so consumers
// don't have to special-case absent fields.
func fingerprint(s string) string {
	if s == "" {
		return "(unset)"
	}
	if len(s) <= 4 {
		return "****"
	}
	return "****" + s[len(s)-4:]
}

func printDoctorJSON(w io.Writer, rep doctorReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

// printDoctorText renders the report as grouped human-readable
// sections. Each line is aligned via a small two-column layout. Kept
// terse so the whole report fits in a terminal screen.
func printDoctorText(out io.Writer, rep doctorReport) error {

	section := func(name string, kvs ...[2]string) {
		fmt.Fprintf(out, "▸ %s\n", name)
		for _, kv := range kvs {
			fmt.Fprintf(out, "  %-12s %s\n", kv[0]+":", kv[1])
		}
		fmt.Fprintln(out)
	}

	section("Build",
		[2]string{"Version", rep.Build.Version},
		[2]string{"Platform", rep.Build.OSArch},
		[2]string{"Go", rep.Build.GoVersion},
	)

	profileLine := rep.Profile.Active + "  (" + rep.Profile.Source + ")"
	if len(rep.Profile.Available) > 0 {
		section("Profile",
			[2]string{"Active", profileLine},
			[2]string{"Available", strings.Join(rep.Profile.Available, ", ")},
		)
	} else {
		section("Profile",
			[2]string{"Active", profileLine},
			[2]string{"Available", "(none — no profiles section in config)"},
		)
	}

	cfgKVs := [][2]string{{"Path", rep.Config.Path}}
	if rep.Config.Exists {
		cfgKVs = append(cfgKVs,
			[2]string{"File mode", rep.Config.FileMode},
			[2]string{"Dir mode", rep.Config.DirMode},
		)
	} else {
		cfgKVs = append(cfgKVs, [2]string{"Status", "MISSING — jc setup never ran"})
	}
	section("Config", cfgKVs...)

	authKVs := [][2]string{
		{"Method", rep.Auth.Method},
		{"Source", rep.Auth.Source},
	}
	if rep.Auth.Fingerprint != "" {
		authKVs = append(authKVs, [2]string{"Key", rep.Auth.Fingerprint})
	}
	if rep.Auth.OrgID != "" {
		authKVs = append(authKVs, [2]string{"Org ID", rep.Auth.OrgID + "  (" + rep.Auth.OrgIDSource + ")"})
	}
	section("Auth", authKVs...)

	apiKVs := [][2]string{
		{"V1", rep.API.V1BaseURL},
		{"V2", rep.API.V2BaseURL},
	}
	if rep.API.Probe != nil {
		p := rep.API.Probe
		probeLine := p.Status
		if p.StatusCode != 0 {
			probeLine = fmt.Sprintf("%s  (HTTP %d, %dms)", p.Status, p.StatusCode, p.LatencyMS)
		} else if p.Error != "" {
			probeLine = fmt.Sprintf("%s  (%s)", p.Status, p.Error)
		}
		apiKVs = append(apiKVs, [2]string{"Probe", probeLine})
	} else {
		apiKVs = append(apiKVs, [2]string{"Probe", "(skipped via --no-probe)"})
	}
	section("API", apiKVs...)

	llmKVs := [][2]string{{"Provider", strOrDash(rep.LLM.Provider)}}
	if rep.LLM.Model != "" {
		llmKVs = append(llmKVs, [2]string{"Model", rep.LLM.Model})
	}
	if rep.LLM.URL != "" {
		llmKVs = append(llmKVs, [2]string{"URL", rep.LLM.URL})
	}
	llmKVs = append(llmKVs, [2]string{"API key", rep.LLM.APIKey})
	if rep.LLM.APIKeySource != "" {
		llmKVs = append(llmKVs, [2]string{"Source", rep.LLM.APIKeySource})
	}
	section("LLM (jc ask)", llmKVs...)

	mcpKVs := [][2]string{
		{"Step-up", boolStr(rep.MCP.RequireStepUp) + "  (" + strOrDash(rep.MCP.StepUpAuth) + ")"},
		{"Signing", boolStr(rep.MCP.SigningEnabled)},
	}
	if rep.MCP.SigningPubkey != "" {
		mcpKVs = append(mcpKVs, [2]string{"Pubkey", rep.MCP.SigningPubkey})
	}
	if rep.MCP.WebhookURL != "" {
		mcpKVs = append(mcpKVs, [2]string{"Webhook", rep.MCP.WebhookURL})
	}
	section("MCP", mcpKVs...)

	return nil
}

func strOrDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func boolStr(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}
