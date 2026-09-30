package api

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/viper"
)

// Regional hosts.
//
// JumpCloud serves three regions and jc reached only one of them: every base
// URL was a constant pointing at the US console, so an EU- or India-hosted
// org had no way onto the CLI but to patch and rebuild (issue #127).
//
// Four host families needed moving, not two. The console ones are the obvious
// pair, but Directory Insights lives on a different domain entirely and the
// OAuth token endpoint on a third. Leaving either behind would have been
// worse than the original problem: an operator would authenticate against
// their own region and then silently query the US audit log.
//
// The regional rule is uniform once all four are laid out — the region label
// is inserted immediately before "jumpcloud.com":
//
//	console.jumpcloud.com         -> console.eu.jumpcloud.com
//	api.jumpcloud.com             -> api.eu.jumpcloud.com
//	admin-oauth.id.jumpcloud.com  -> admin-oauth.id.eu.jumpcloud.com
//
// The hosts are written out rather than derived, because a rule that holds
// for three cases is not yet a rule, and a wrong host here sends credentials
// somewhere unintended.
//
// Sources. The console hosts are the `servers` block of the Console API spec
// and the Insights hosts the same block of the Directory Insights spec, both
// carrying matching x-jc-region markers. The OAuth endpoints were read from
// each host's own OpenID discovery document on 2026-09-30 — all three exist,
// all three advertise the client_credentials grant a service account uses.

// Region is a JumpCloud service region.
type Region struct {
	// Code is the identifier used by --region, JC_REGION and the config key.
	// It matches the x-jc-region values in JumpCloud's own API specs.
	Code string
	// Name is the human-readable region, as the specs describe it.
	Name string

	V1BaseURL       string
	V2BaseURL       string
	InsightsBaseURL string
	OAuthTokenURL   string
}

// regions is the registry. US first because it is the default.
var regions = []Region{
	{
		Code: "us", Name: "United States",
		V1BaseURL:       "https://console.jumpcloud.com/api",
		V2BaseURL:       "https://console.jumpcloud.com/api/v2",
		InsightsBaseURL: "https://api.jumpcloud.com/insights/directory/v1",
		OAuthTokenURL:   "https://admin-oauth.id.jumpcloud.com/oauth2/token",
	},
	{
		Code: "eu", Name: "European Union",
		V1BaseURL:       "https://console.eu.jumpcloud.com/api",
		V2BaseURL:       "https://console.eu.jumpcloud.com/api/v2",
		InsightsBaseURL: "https://api.eu.jumpcloud.com/insights/directory/v1",
		OAuthTokenURL:   "https://admin-oauth.id.eu.jumpcloud.com/oauth2/token",
	},
	{
		Code: "in", Name: "India",
		V1BaseURL:       "https://console.in.jumpcloud.com/api",
		V2BaseURL:       "https://console.in.jumpcloud.com/api/v2",
		InsightsBaseURL: "https://api.in.jumpcloud.com/insights/directory/v1",
		OAuthTokenURL:   "https://admin-oauth.id.in.jumpcloud.com/oauth2/token",
	},
}

// DefaultRegion is what jc uses when nothing selects one. It is the region
// jc has always used, so an existing installation sees no change.
const DefaultRegion = "us"

// RegionCodes lists the known region codes, for help text and validation.
func RegionCodes() []string {
	out := make([]string, 0, len(regions))
	for _, r := range regions {
		out = append(out, r.Code)
	}
	sort.Strings(out)
	return out
}

// LookupRegion returns the region for a code.
func LookupRegion(code string) (Region, error) {
	c := strings.ToLower(strings.TrimSpace(code))
	if c == "" {
		c = DefaultRegion
	}
	for _, r := range regions {
		if r.Code == c {
			return r, nil
		}
	}
	return Region{}, fmt.Errorf("unknown region %q: jc knows %s. An org is served from one "+
		"region and cannot be reached from another, so this is not a preference — it has to "+
		"match where the organization is hosted",
		code, strings.Join(RegionCodes(), ", "))
}

// CurrentRegion resolves the active region.
//
// Order: JC_REGION, then the config key (which --region binds to), then the
// default. An unknown value falls back to the default rather than failing
// here, because this is called from constructors that cannot report an error
// usefully; `jc doctor` and the region flag's own validation surface it.
func CurrentRegion() Region {
	for _, code := range []string{os.Getenv("JC_REGION"), viper.GetString("region")} {
		if code == "" {
			continue
		}
		if r, err := LookupRegion(code); err == nil {
			return r
		}
	}
	def, _ := LookupRegion(DefaultRegion)
	return def
}

// Per-host environment overrides.
//
// These exist because they are what the operator who reported #127 was
// already running in production while blocked, and taking them away in the
// same change that fixes the problem would be a poor trade. They also cover
// the case the region registry cannot: a host jc does not know about yet.
//
// A per-host override beats the region, so JC_REGION=eu with
// JC_API_BASE_URL set sends v1 to the override and everything else to the EU.
// That is deliberate — the narrower instruction wins.
const (
	EnvV1BaseURL       = "JC_API_BASE_URL"
	EnvV2BaseURL       = "JC_API_V2_BASE_URL"
	EnvInsightsBaseURL = "JC_INSIGHTS_BASE_URL"
	EnvOAuthTokenURL   = "JC_OAUTH_TOKEN_URL"
)

func override(env, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		return strings.TrimRight(v, "/")
	}
	return fallback
}

// ResolveV1BaseURL and friends give the URL each client should use. Every
// constructor calls these rather than reading a constant, which is what makes
// the region take effect.
func ResolveV1BaseURL() string {
	return override(EnvV1BaseURL, CurrentRegion().V1BaseURL)
}

func ResolveV2BaseURL() string {
	return override(EnvV2BaseURL, CurrentRegion().V2BaseURL)
}

func ResolveInsightsBaseURL() string {
	return override(EnvInsightsBaseURL, CurrentRegion().InsightsBaseURL)
}

func ResolveOAuthTokenURL() string {
	return override(EnvOAuthTokenURL, CurrentRegion().OAuthTokenURL)
}

// ResolvedHosts describes where jc will actually send requests, and why. It
// backs `jc doctor`, which previously printed the constants — so an operator
// on a patched build saw the US endpoints confirmed by the first thing they
// checked, while the CLI used something else.
type ResolvedHosts struct {
	Region     string `json:"region"`
	RegionName string `json:"region_name"`
	Source     string `json:"region_source"`
	V1         string `json:"v1_base_url"`
	V2         string `json:"v2_base_url"`
	Insights   string `json:"insights_base_url"`
	OAuthToken string `json:"oauth_token_url"`
	// Overridden lists the environment variables redirecting a host away
	// from its region default.
	Overridden []string `json:"overridden,omitempty"`
}

// CurrentHosts reports the resolved hosts and where the region came from.
func CurrentHosts() ResolvedHosts {
	r := CurrentRegion()
	source := "default"
	switch {
	case os.Getenv("JC_REGION") != "":
		source = "JC_REGION env"
	case viper.GetString("region") != "":
		source = "region config/flag"
	}

	h := ResolvedHosts{
		Region: r.Code, RegionName: r.Name, Source: source,
		V1:         ResolveV1BaseURL(),
		V2:         ResolveV2BaseURL(),
		Insights:   ResolveInsightsBaseURL(),
		OAuthToken: ResolveOAuthTokenURL(),
	}
	for _, env := range []string{EnvV1BaseURL, EnvV2BaseURL, EnvInsightsBaseURL, EnvOAuthTokenURL} {
		if strings.TrimSpace(os.Getenv(env)) != "" {
			h.Overridden = append(h.Overridden, env)
		}
	}
	return h
}
