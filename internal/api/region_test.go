package api

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// withEnv sets environment variables for one test and restores them after.
func withEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

// resetRegionState clears the viper key the region resolves through, so one
// test cannot leak a region into the next.
func resetRegionState(t *testing.T) {
	t.Helper()
	prev := viper.GetString("region")
	viper.Set("region", "")
	t.Cleanup(func() { viper.Set("region", prev) })
}

func TestLookupRegion(t *testing.T) {
	for _, code := range []string{"us", "eu", "in", "US", " eu "} {
		if _, err := LookupRegion(code); err != nil {
			t.Errorf("LookupRegion(%q) failed: %v", code, err)
		}
	}
	// Empty means the default rather than an error, because constructors
	// call this on a path where there is nothing useful to report.
	r, err := LookupRegion("")
	if err != nil || r.Code != DefaultRegion {
		t.Errorf("empty code = %q, %v; want the default", r.Code, err)
	}

	_, err = LookupRegion("apac")
	if err == nil {
		t.Fatal("an unknown region must be an error")
	}
	// The message must say this is not a preference — somebody who picks the
	// wrong one gets "not found" for records that exist.
	for _, want := range []string{"apac", "eu", "in", "us", "has to"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message should contain %q: %v", want, err)
		}
	}
}

// TestEveryRegionIsComplete guards the thing that would be worst to get
// wrong: a region that carries three hosts and silently leaves the fourth
// pointing at the US.
func TestEveryRegionIsComplete(t *testing.T) {
	for _, r := range regions {
		if r.Code == "" || r.Name == "" {
			t.Errorf("region %+v is missing its identity", r)
		}
		hosts := map[string]string{
			"V1BaseURL": r.V1BaseURL, "V2BaseURL": r.V2BaseURL,
			"InsightsBaseURL": r.InsightsBaseURL, "OAuthTokenURL": r.OAuthTokenURL,
		}
		for name, url := range hosts {
			if !strings.HasPrefix(url, "https://") {
				t.Errorf("%s.%s = %q, want an https URL", r.Code, name, url)
			}
			// A non-US region whose host has no region label is one somebody
			// copied from the US row and forgot to edit.
			if r.Code != DefaultRegion && !strings.Contains(url, "."+r.Code+".jumpcloud.com") {
				t.Errorf("%s.%s = %q carries no %q region label — copied from US?",
					r.Code, name, url, r.Code)
			}
			if r.Code == DefaultRegion && (strings.Contains(url, ".eu.") || strings.Contains(url, ".in.")) {
				t.Errorf("us.%s = %q carries another region's label", name, url)
			}
		}
	}
}

// TestUSRegionMatchesTheConstants keeps the default byte-identical to what jc
// used before regions existed. If these drift, an existing installation
// silently changes where it sends requests.
func TestUSRegionMatchesTheConstants(t *testing.T) {
	us, err := LookupRegion("us")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][2]string{
		"v1":       {us.V1BaseURL, BaseURL},
		"v2":       {us.V2BaseURL, V2BaseURL},
		"insights": {us.InsightsBaseURL, InsightsBaseURL},
		"oauth":    {us.OAuthTokenURL, OAuthTokenURL},
	}
	for name, pair := range cases {
		if pair[0] != pair[1] {
			t.Errorf("%s: region has %q, constant has %q — the default has moved", name, pair[0], pair[1])
		}
	}
}

func TestResolveDefaultsToUS(t *testing.T) {
	resetRegionState(t)
	withEnv(t, map[string]string{"JC_REGION": "", EnvV1BaseURL: "", EnvV2BaseURL: "",
		EnvInsightsBaseURL: "", EnvOAuthTokenURL: ""})

	if got := ResolveV1BaseURL(); got != BaseURL {
		t.Errorf("v1 = %q, want the unchanged default %q", got, BaseURL)
	}
	if got := ResolveV2BaseURL(); got != V2BaseURL {
		t.Errorf("v2 = %q, want %q", got, V2BaseURL)
	}
	if got := ResolveInsightsBaseURL(); got != InsightsBaseURL {
		t.Errorf("insights = %q, want %q", got, InsightsBaseURL)
	}
	if got := ResolveOAuthTokenURL(); got != OAuthTokenURL {
		t.Errorf("oauth = %q, want %q", got, OAuthTokenURL)
	}
}

func TestRegionMovesEveryHost(t *testing.T) {
	resetRegionState(t)
	withEnv(t, map[string]string{"JC_REGION": "eu", EnvV1BaseURL: "", EnvV2BaseURL: "",
		EnvInsightsBaseURL: "", EnvOAuthTokenURL: ""})

	// All four, not just the console pair. Insights lives on a different
	// domain, and leaving it behind would query the wrong audit log while
	// everything else looked right.
	for name, got := range map[string]string{
		"v1": ResolveV1BaseURL(), "v2": ResolveV2BaseURL(),
		"insights": ResolveInsightsBaseURL(), "oauth": ResolveOAuthTokenURL(),
	} {
		if !strings.Contains(got, ".eu.jumpcloud.com") {
			t.Errorf("%s = %q did not move to the EU", name, got)
		}
	}
}

func TestPerHostOverrideBeatsRegion(t *testing.T) {
	resetRegionState(t)
	withEnv(t, map[string]string{
		"JC_REGION": "eu", EnvV1BaseURL: "https://example.test/api",
		EnvV2BaseURL: "", EnvInsightsBaseURL: "", EnvOAuthTokenURL: "",
	})
	// The narrower instruction wins, and only for the host it names.
	if got := ResolveV1BaseURL(); got != "https://example.test/api" {
		t.Errorf("v1 = %q, want the override", got)
	}
	if got := ResolveV2BaseURL(); !strings.Contains(got, ".eu.") {
		t.Errorf("v2 = %q, want the EU region default", got)
	}
	// A trailing slash is trimmed so paths do not double up.
	t.Setenv(EnvV2BaseURL, "https://example.test/api/v2/")
	if got := ResolveV2BaseURL(); got != "https://example.test/api/v2" {
		t.Errorf("v2 = %q, want the trailing slash trimmed", got)
	}
	// Whitespace-only is not an override.
	t.Setenv(EnvInsightsBaseURL, "   ")
	if got := ResolveInsightsBaseURL(); !strings.Contains(got, ".eu.") {
		t.Errorf("insights = %q, want a blank override ignored", got)
	}
}

func TestEnvBeatsConfig(t *testing.T) {
	resetRegionState(t)
	viper.Set("region", "in")
	withEnv(t, map[string]string{"JC_REGION": "eu"})
	if got := CurrentRegion(); got.Code != "eu" {
		t.Errorf("region = %q, want the env to win", got.Code)
	}

	// With the env unset, the config key applies.
	t.Setenv("JC_REGION", "")
	if got := CurrentRegion(); got.Code != "in" {
		t.Errorf("region = %q, want the config value", got.Code)
	}
}

// TestUnknownValueFallsBackRatherThanPanicking covers the constructor path.
// The flag validates loudly at the CLI layer; down here there is nothing to
// report an error to, so the default is the only safe answer.
func TestUnknownValueFallsBackRatherThanPanicking(t *testing.T) {
	resetRegionState(t)
	withEnv(t, map[string]string{"JC_REGION": "atlantis"})
	if got := CurrentRegion(); got.Code != DefaultRegion {
		t.Errorf("region = %q, want the default", got.Code)
	}
}

func TestCurrentHosts(t *testing.T) {
	resetRegionState(t)
	withEnv(t, map[string]string{"JC_REGION": "eu", EnvV1BaseURL: "",
		EnvV2BaseURL: "", EnvInsightsBaseURL: "", EnvOAuthTokenURL: "https://t.test/token"})

	h := CurrentHosts()
	if h.Region != "eu" || h.RegionName == "" {
		t.Errorf("hosts = %+v", h)
	}
	if h.Source != "JC_REGION env" {
		t.Errorf("source = %q, want the env named", h.Source)
	}
	if len(h.Overridden) != 1 || h.Overridden[0] != EnvOAuthTokenURL {
		t.Errorf("overridden = %v, want just the OAuth override", h.Overridden)
	}
	if h.OAuthToken != "https://t.test/token" {
		t.Errorf("oauth = %q, want the override", h.OAuthToken)
	}
}

func TestRegionCodesSorted(t *testing.T) {
	got := RegionCodes()
	want := []string{"eu", "in", "us"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("RegionCodes() = %v, want %v", got, want)
	}
}
