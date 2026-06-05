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
	"go.yaml.in/yaml/v3"

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
			// JC_OUTPUT env / config default. Hierarchical (json, yaml) and
			// human (text) formats are honored; tabular formats (table,
			// csv, ndjson) don't fit a nested doctor report — Bugbot flagged
			// silently downgrading those to text as a surprise, so we
			// surface a stderr note when we do.
			output := strings.ToLower(config.Output())
			if jsonOut {
				output = "json"
			}
			switch output {
			case "json":
				return printDoctorJSON(cmd.OutOrStdout(), rep)
			case "yaml":
				return printDoctorYAML(cmd.OutOrStdout(), rep)
			case "human", "text", "":
				return printDoctorText(cmd.OutOrStdout(), rep)
			default:
				fmt.Fprintf(cmd.ErrOrStderr(),
					"jc doctor: %q output not supported for a hierarchical report; rendering as human text\n",
					output)
				return printDoctorText(cmd.OutOrStdout(), rep)
			}
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

	envKey := os.Getenv("JC_API_KEY")
	flagOrEnv := viper.GetString("api_key")
	profileRaw := viper.GetString("profiles." + profile + ".api_key")
	hasAPIKey := flagOrEnv != "" || profileRaw != ""

	// api.NewClient() resolution precedence (mirror it exactly so the
	// doctor report matches what jc actually does):
	//   1. AuthMethod == "service_account" AND client_id + client_secret
	//      → OAuth Bearer.
	//   2. AuthMethod == "service_account" AND creds missing AND an
	//      api_key resolves → SILENT FALLBACK to api_key (Bugbot caught
	//      this third-order case where reporting `method: service_account`
	//      with an x-api-key source was internally inconsistent).
	//   3. AuthMethod == "api_key" → resolve key from flag/env/keychain/
	//      profile config.
	//   4. Nothing configured → no auth.
	if as.Method == "service_account" {
		// Peek at the raw client_secret to distinguish "not configured"
		// from "keychain reference exists but resolution failed" —
		// config.ClientSecret() resolves keychain refs transparently
		// and returns "" on failure, losing that distinction.
		clientID := config.ClientID()
		clientSecretRaw := viper.GetString("profiles." + profile + ".client_secret")
		clientSecret := config.ClientSecret()

		oauthAvailable := clientID != "" && clientSecret != ""
		keychainFailed := clientID != "" &&
			strings.HasPrefix(clientSecretRaw, "keychain://") &&
			clientSecret == ""

		switch {
		case oauthAvailable:
			// (1) Happy OAuth path.
			as.Source = "service_account (OAuth)"
			as.Fingerprint = fingerprint(clientID)
			as.OrgID, as.OrgIDSource = collectOrgID(profile)
			return as
		case !hasAPIKey && keychainFailed:
			// (4a) OAuth would be configured but the client_secret
			// keychain is unreadable AND there's no api_key fallback.
			// Report the actual cause so the operator fixes the
			// keychain rather than re-entering their secret.
			ref := strings.TrimPrefix(clientSecretRaw, "keychain://")
			as.Source = fmt.Sprintf("service_account (client_secret keychain unavailable: %s)", ref)
			as.Fingerprint = fingerprint(clientID)
			as.OrgID, as.OrgIDSource = collectOrgID(profile)
			return as
		case !hasAPIKey:
			// (4b) Neither auth path will work and no special cause to
			// surface.
			as.Source = "service_account (no client credentials)"
			as.OrgID, as.OrgIDSource = collectOrgID(profile)
			return as
		default:
			// (2) Silent fallback to api_key. The earlier Bugbot
			// finding flagged that reporting `method: service_account`
			// with an x-api-key source was inconsistent; this branch
			// re-labels method honestly. When the keychain miss caused
			// the fallback, surface that in the method label too so
			// the operator sees both that their service_account is
			// broken AND why.
			if keychainFailed {
				as.Method = "api_key (service_account fallback, client_secret keychain unavailable)"
			} else {
				as.Method = "api_key (service_account fallback)"
			}
		}
	}

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

// collectOrgID returns the resolved org ID + source. Mirrors
// config.OrgID()'s precedence exactly:
//
//  1. viper "org_id" — picks up JC_ORG_ID env (via BindEnv), a
//     top-level `org_id:` in config.yaml, or a flag bound to "org_id".
//  2. profile config (profiles.<name>.org_id).
//
// Bugbot caught the original implementation reading os.Getenv("JC_ORG_ID")
// directly, which missed the top-level config-file case — the doctor
// would have reported no org ID while config.OrgID() (and every other
// jc command) still resolved one.
func collectOrgID(profile string) (string, string) {
	if topLevel := viper.GetString("org_id"); topLevel != "" {
		// Attribute to env when the env var matches the resolved value
		// (most common case); fall back to a top-level-config attribution
		// otherwise. Cannot distinguish flag vs config without a
		// pflag.Changed peek, and there's no --org-id flag today.
		if os.Getenv("JC_ORG_ID") == topLevel {
			return topLevel, "JC_ORG_ID env"
		}
		return topLevel, "top-level config"
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

	// Run ListAll in a goroutine so the probe itself respects
	// --probe-timeout even when the underlying call doesn't. Bugbot
	// caught that for service_account profiles, the OAuth token-fetch
	// hop uses TokenCache.fetchToken() which has its own 30s http.Client
	// and doesn't honor the probe context — so a hung OAuth endpoint
	// could make `jc doctor --probe-timeout 100ms` run 30+ seconds.
	//
	// The leaked goroutine dies with the process (jc doctor exits as
	// soon as we print); we don't try to cancel the underlying HTTP call,
	// just return early so the operator sees the timeout they asked for.
	type result struct {
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		_, listErr := client.ListAll(ctx, "/usergroups", api.V2ListOptions{Limit: 1})
		resultCh <- result{err: listErr}
	}()

	select {
	case r := <-resultCh:
		latency := time.Since(start)
		probe := classifyProbeError(r.err)
		probe.LatencyMS = latency.Milliseconds()
		return probe
	case <-ctx.Done():
		latency := time.Since(start)
		return &apiProbe{
			Status:    "timeout",
			LatencyMS: latency.Milliseconds(),
			Error:     fmt.Sprintf("probe deadline (%s) exceeded; OAuth token-fetch or upstream HTTP may not honor the context — increase --probe-timeout or use --no-probe", timeout),
		}
	}
}

// classifyProbeError turns a (client.ListAll) result into the
// status/status_code/error triple the report uses. Split from
// runAPIProbe so tests can validate the error classification without
// needing to mock the entire HTTP client.
//
// Three shapes of error matter:
//
//  1. *api.APIError — request reached JumpCloud and got a non-2xx
//     response. Status code tells us auth_failed vs other.
//  2. OAuth token-exchange failure — the bearer transport wraps an
//     error containing one of the distinct phrases from
//     internal/api/oauth.go (e.g. "invalid client credentials").
//     Bugbot caught the original code mis-bucketing these as
//     "unreachable" — operators with bad service-account creds saw a
//     network-trouble suggestion instead of an auth one.
//  3. Anything else (DNS failure, connection refused, timeout) —
//     genuinely unreachable.
func classifyProbeError(err error) *apiProbe {
	if err == nil {
		return &apiProbe{Status: "ok", StatusCode: http.StatusOK}
	}
	// 0. Context errors fast-path. When --probe-timeout fires AND the
	//    upstream HTTP client honors the context, ListAll returns a
	//    context-deadline error BEFORE runAPIProbe's select reaches
	//    ctx.Done(). Classifying via APIError/OAuth markers would land
	//    on "unreachable" — misleading triage away from "increase
	//    --probe-timeout." Same idea catches context.Canceled (Ctrl-C,
	//    parent cancellation) so the report says timeout instead of
	//    pretending the host is down. Bugbot caught the inverse:
	//    well-behaved upstream → my code mis-classified more often.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &apiProbe{Status: "timeout", Error: err.Error()}
	}
	// 1. APIError carries the HTTP status code from a real response.
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
	// 2. OAuth token-exchange failures. The exact phrases come from
	//    internal/api/oauth.go's fetchToken() error returns.
	//    String-matching is brittle but the alternative (typed errors
	//    in the api package) is out of scope for the doctor command.
	//    If oauth.go ever refactors these wraps, tests fail loudly so
	//    we catch the drift.
	msg := err.Error()
	for _, marker := range oauthAuthFailureMarkers {
		if strings.Contains(msg, marker) {
			return &apiProbe{Status: "auth_failed", Error: msg}
		}
	}
	// 3. Anything else is a real transport problem.
	return &apiProbe{Status: "unreachable", Error: msg}
}

// oauthAuthFailureMarkers are the substring signals that an error from
// the bearer-token transport is an auth failure rather than a network
// failure. Pulled from internal/api/oauth.go's fetchToken() error
// returns. Keep this list in sync if those wrap strings change.
var oauthAuthFailureMarkers = []string{
	"invalid client credentials",         // 401 from token endpoint
	"client credentials lack permission", // 403 from token endpoint
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

// printDoctorYAML round-trips through JSON so the YAML output uses
// the same snake_case keys the JSON output does (otherwise yaml.v3
// would lowercase the Go field names, giving us `goversion` instead
// of `go_version`). Pure CPU; the data is small.
func printDoctorYAML(w io.Writer, rep doctorReport) error {
	jb, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	var generic any
	if err := json.Unmarshal(jb, &generic); err != nil {
		return err
	}
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	defer enc.Close()
	return enc.Encode(generic)
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
