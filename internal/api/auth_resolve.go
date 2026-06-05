package api

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/keychain"
)

// ResolvedAuth is the single source of truth for "what auth will jc
// actually use, and where did it come from?" — used by both NewClient
// (which builds the HTTP client from the resolved values) and by
// diagnostic surfaces like `jc doctor` (which render the attribution
// for the operator).
//
// Background: PR #42 (KLA-439, jc doctor) duplicated NewClient's
// precedence logic in cmd/doctor.go's collectAuth. Across the next
// 12 commits Bugbot found 13 distinct edges where the two
// implementations drifted (auth method labels, OAuth fallback to
// api_key, keychain-ref resolution failures, --api-key vs JC_API_KEY
// precedence, ...). Pulling resolution into the api package
// eliminates the entire class by construction: one decision tree, two
// consumers, structurally impossible to disagree.
type ResolvedAuth struct {
	// Method is the auth scheme the resolved client will actually use.
	// "api_key" | "service_account" | "api_key (service_account fallback)"
	// | "api_key (service_account fallback, client_secret keychain unavailable)"
	// — when service_account is configured but creds fall back to
	// api_key, the method label surfaces both facts so the operator
	// sees what happened without having to triage.
	Method string

	// Source is the operator-facing description of *where* the active
	// credential came from. Examples:
	//   "--api-key flag"
	//   "JC_API_KEY env"
	//   "keychain (jc/default)"
	//   "profile config (plaintext)"
	//   "service_account (OAuth)"
	//   "service_account (client_secret keychain unavailable: jc/default)"
	//   "service_account (no client credentials)"
	//   "(unset)"
	Source string

	// Fingerprint is "****abcd" — last 4 chars of the active credential
	// (api_key for api_key auth, client_id for OAuth). Empty when no
	// credential resolved.
	Fingerprint string

	// OrgID is the JumpCloud organization ID, resolved via the same
	// precedence as config.OrgID(). Empty when not configured.
	OrgID string

	// OrgIDSource describes where OrgID came from:
	//   "JC_ORG_ID env" | "top-level config" | "profile config"
	OrgIDSource string

	// Internal fields used by NewClient to build the live transport.
	// Unexported so external callers (doctor, audit verifiers) can't
	// accidentally leak the raw key.

	// apiKey is the plaintext credential when Method resolves to an
	// api_key path. Empty for OAuth and unresolved cases.
	apiKey string

	// tokenCache is non-nil when Method is "service_account" (OAuth
	// happy path). The cache encapsulates client_id + client_secret
	// for lazy token fetching.
	tokenCache *TokenCache
}

// Hint carries disambiguating signals only a caller with cobra/pflag
// state can supply. Fields are all optional — the zero value works
// for callers (like NewClient) that don't have the information.
//
// Why this lives in the api package: ResolveActiveAuth is the single
// source of truth, and the api_key flag-vs-env disambiguation is one
// of the precedence questions it must answer. But pflag itself is a
// cmd-package concern, so the caller plumbs the .Changed bit through
// a typed hint rather than the api package importing pflag.
type Hint struct {
	// APIKeyFlagChanged is true when the operator passed --api-key on
	// the command line (cobra pflag .Changed bit). When set AND the
	// resolved api_key value is non-empty, ResolveActiveAuth attributes
	// the source to "--api-key flag" rather than "JC_API_KEY env"
	// — matching cobra/viper precedence (flag overrides env, even when
	// values happen to match).
	APIKeyFlagChanged bool
}

// ResolveActiveAuth returns the active auth resolution with provenance.
// Decision tree mirrors api.NewClient() exactly (because NewClient
// now calls it). The cmd/doctor.go collectAuth function consumes this
// and copies fields into its display struct — no precedence walking
// outside the api package.
//
// Decision order:
//  1. AuthMethod == "service_account" with valid client_id + client_secret
//     → OAuth Bearer.
//  2. AuthMethod == "service_account" without an api_key fallback AND
//     client_secret is a failed keychain reference → report the keychain
//     failure (operator's actionable cause).
//  3. AuthMethod == "service_account" without an api_key fallback AND
//     no special cause → report "no client credentials" (misconfig).
//  4. AuthMethod == "service_account" with creds missing AND an
//     api_key resolves → silently fall back to api_key, re-label method
//     so the report doesn't claim OAuth.
//  5. AuthMethod == "api_key" (or fallback from 4) → resolve key from
//     flag / env / keychain / profile config.
//  6. Nothing configured → Source = "(unset)".
func ResolveActiveAuth(hint Hint) ResolvedAuth {
	r := ResolvedAuth{Method: config.AuthMethod()}
	profile := config.ActiveProfile()

	r.OrgID, r.OrgIDSource = resolveOrgID(profile)

	// Determine whether an api_key WOULD resolve to a real value.
	// This is the structurally-correct check (the one cmd/doctor's
	// hasAPIKey got wrong in finding #13): a keychain:// ref that
	// fails to resolve is NOT a fallback — it produces no usable key.
	envKey := os.Getenv("JC_API_KEY")
	flagOrEnv := viper.GetString("api_key")
	profileRaw := viper.GetString("profiles." + profile + ".api_key")
	resolvedAPIKey, resolvedAPIKeySource := resolveAPIKeyChain(profile, hint, flagOrEnv, envKey, profileRaw)
	hasAPIKey := resolvedAPIKey != ""

	// --- Service-account branch ---------------------------------------------
	if r.Method == "service_account" {
		clientID := config.ClientID()
		clientSecretRaw := viper.GetString("profiles." + profile + ".client_secret")
		clientSecret := config.ClientSecret()

		oauthAvailable := clientID != "" && clientSecret != ""
		keychainFailed := clientID != "" &&
			strings.HasPrefix(clientSecretRaw, "keychain://") &&
			clientSecret == ""

		switch {
		case oauthAvailable:
			r.Source = "service_account (OAuth)"
			r.Fingerprint = Fingerprint(clientID)
			r.tokenCache = NewTokenCache(clientID, clientSecret)
			return r
		case !hasAPIKey && keychainFailed:
			ref := strings.TrimPrefix(clientSecretRaw, "keychain://")
			r.Source = fmt.Sprintf("service_account (client_secret keychain unavailable: %s)", ref)
			r.Fingerprint = Fingerprint(clientID)
			return r
		case !hasAPIKey:
			r.Source = "service_account (no client credentials)"
			return r
		default:
			// Silent fallback to api_key. Re-label method so the report
			// surfaces both that service_account is the configured intent
			// AND that jc is actually using x-api-key.
			if keychainFailed {
				r.Method = "api_key (service_account fallback, client_secret keychain unavailable)"
			} else {
				r.Method = "api_key (service_account fallback)"
			}
			// Fall through to api_key attribution below.
		}
	}

	// --- API-key branch -----------------------------------------------------
	if resolvedAPIKey != "" {
		r.apiKey = resolvedAPIKey
		r.Source = resolvedAPIKeySource
		r.Fingerprint = Fingerprint(resolvedAPIKey)
		return r
	}

	// resolvedAPIKey is empty but resolveAPIKeyChain may have populated
	// a source label anyway (the keychain-miss case: the operator
	// configured a keychain:// ref that couldn't be resolved). Surface
	// that as the source + "(keychain unavailable)" fingerprint so the
	// report tells the operator their intent AND its failure — distinct
	// from "(unset)" which would suggest nothing was configured.
	if resolvedAPIKeySource != "" {
		r.Source = resolvedAPIKeySource
		r.Fingerprint = "(keychain unavailable)"
		return r
	}

	// Nothing resolved.
	r.Source = "(unset)"
	return r
}

// resolveAPIKeyChain returns the resolved api_key (post-keychain) and
// a source label. Returns ("", "") when no key resolves.
//
// Precedence (mirrors config.APIKey() + the flag-vs-env disambiguation
// doctor needs):
//  1. --api-key flag (when hint.APIKeyFlagChanged AND viper value non-empty)
//  2. JC_API_KEY env (when raw env matches the resolved viper value)
//  3. profile config: keychain:// ref → resolve via keychain package
//  4. profile config: plaintext
func resolveAPIKeyChain(profile string, hint Hint, flagOrEnv, envKey, profileRaw string) (string, string) {
	switch {
	case hint.APIKeyFlagChanged && flagOrEnv != "":
		return flagOrEnv, "--api-key flag"
	case flagOrEnv != "" && envKey != "" && flagOrEnv == envKey:
		return envKey, "JC_API_KEY env"
	case flagOrEnv != "":
		// Resolved via viper but neither flag nor a matching env — the
		// value came from a non-flag, non-direct-env source (e.g. a
		// config-level binding). Attribute conservatively.
		return flagOrEnv, "viper-resolved (config or env)"
	case strings.HasPrefix(profileRaw, "keychain://"):
		resolved, err := keychain.Resolve(profileRaw)
		ref := strings.TrimPrefix(profileRaw, "keychain://")
		if err != nil || resolved == "" {
			// Keychain miss for api_key — distinct from "never
			// configured." Returning empty resolved value AND a source
			// label so the doctor can report the failure cause; the
			// caller (ResolveActiveAuth) treats this as no-api-key.
			return "", fmt.Sprintf("keychain (%s) — unavailable", ref)
		}
		return resolved, fmt.Sprintf("keychain (%s)", ref)
	case profileRaw != "":
		return profileRaw, "profile config (plaintext)"
	default:
		return "", ""
	}
}

// resolveOrgID mirrors config.OrgID()'s precedence and adds source
// attribution.
func resolveOrgID(profile string) (string, string) {
	if topLevel := viper.GetString("org_id"); topLevel != "" {
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

// Fingerprint returns "****" + the last 4 chars of s. Used everywhere
// jc displays a credential identifier (audit log signing pubkey, TTY
// step-up prompts, doctor report). Returns "(unset)" for empty input
// so consumers don't have to special-case absent values.
func Fingerprint(s string) string {
	if s == "" {
		return "(unset)"
	}
	if len(s) <= 4 {
		return "****"
	}
	return "****" + s[len(s)-4:]
}

// APIKey returns the resolved api_key for use by NewClient. Empty
// when the active auth method is OAuth or unresolved.
func (r ResolvedAuth) APIKey() string { return r.apiKey }

// TokenCache returns the resolved OAuth token cache for use by
// NewClient. Nil when the active auth method is api_key.
func (r ResolvedAuth) TokenCache() *TokenCache { return r.tokenCache }
