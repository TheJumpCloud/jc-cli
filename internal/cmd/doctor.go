package cmd

import (
	"context"
	"encoding/json"
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
			rep := collectDoctorReport(cmd.Context(), !noProbe, probeTimeout)

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
// no I/O beyond the optional HEAD probe — so it's safe to call from
// tests with probe disabled.
func collectDoctorReport(ctx context.Context, probe bool, timeout time.Duration) doctorReport {
	rep := doctorReport{
		Build:   collectBuild(),
		Profile: collectProfile(),
		Config:  collectConfig(),
		Auth:    collectAuth(),
		API:     collectAPI(),
		LLM:     collectLLM(),
		MCP:     collectMCP(),
	}
	if probe {
		rep.API.Probe = runAPIProbe(ctx, rep.API.V2BaseURL, timeout)
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
// the binding. We disambiguate by peeking at the raw env. The flag
// always wins over env, so if the env is set AND the active value
// matches it, we attribute to env; otherwise to flag.
func collectAuth() authSection {
	as := authSection{Method: config.AuthMethod()}

	envKey := os.Getenv("JC_API_KEY")
	flagOrEnv := viper.GetString("api_key")
	profile := config.ActiveProfile()
	profileRaw := viper.GetString("profiles." + profile + ".api_key")

	switch {
	case flagOrEnv != "" && envKey != "" && flagOrEnv == envKey:
		as.Source = "JC_API_KEY env"
		as.Fingerprint = fingerprint(envKey)
	case flagOrEnv != "":
		as.Source = "--api-key flag"
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
	default:
		as.Source = "(unset)"
	}

	// Org ID source: env vs profile.
	if envOrg := os.Getenv("JC_ORG_ID"); envOrg != "" {
		as.OrgID = envOrg
		as.OrgIDSource = "JC_ORG_ID env"
	} else if profileOrg := viper.GetString("profiles." + profile + ".org_id"); profileOrg != "" {
		as.OrgID = profileOrg
		as.OrgIDSource = "profile config"
	}

	return as
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

// runAPIProbe hits a known JumpCloud V2 endpoint with the resolved API
// key and reports outcome. We use `GET /v2/usergroups?limit=1` — the
// cheapest real V2 read JumpCloud offers; returns 200 when auth works,
// 401/403 when the key is wrong, and a network error if the host is
// unreachable. The status field distinguishes those three so the
// operator can tell "DNS/TLS works but the key is wrong" from "the
// host is unreachable" — that's the whole reason this command exists.
//
// We don't HEAD the API root because the JumpCloud edge returns 404
// for HEAD on `/api/v2`; that would be indistinguishable from a real
// outage. A 1-row GET is a few KB and works against a real handler.
//
// Never returns a non-nil error to the caller; failures are encoded
// in the report so JSON consumers always get a parseable response.
func runAPIProbe(ctx context.Context, baseURL string, timeout time.Duration) *apiProbe {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	probeURL := strings.TrimRight(baseURL, "/") + "/usergroups?limit=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return &apiProbe{Status: "unreachable", Error: err.Error()}
	}
	if key := config.APIKey(); key != "" {
		req.Header.Set("x-api-key", key)
	}
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start)

	probe := &apiProbe{LatencyMS: latency.Milliseconds()}
	if err != nil {
		probe.Status = "unreachable"
		probe.Error = err.Error()
		return probe
	}
	defer resp.Body.Close()

	probe.StatusCode = resp.StatusCode
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		probe.Status = "ok"
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		probe.Status = "auth_failed"
	default:
		probe.Status = fmt.Sprintf("http_%d", resp.StatusCode)
	}
	return probe
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
