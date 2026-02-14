// Package simulator evaluates JumpCloud authentication policies against a
// simulated login context. It performs client-side policy evaluation since
// JumpCloud has no server-side simulation API.
package simulator

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// SimulationContext provides the facts about a simulated login attempt.
type SimulationContext struct {
	UserID          string   // resolved user ID
	UserGroups      []string // group IDs the user belongs to
	IP              string   // source IP address
	DeviceID        string   // optional device ID
	DeviceManaged   *bool    // nil = unknown
	DeviceEncrypted *bool    // nil = unknown
	Location        string   // country code (e.g., "US")
	MFAConfigured   bool
}

// Policy is the subset of auth policy fields needed for evaluation.
type Policy struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Disabled   bool            `json:"disabled"`
	Type       string          `json:"type"`
	Conditions json.RawMessage `json:"conditions"`
	Targets    PolicyTargets   `json:"targets"`
	Effect     string          `json:"effect"` // "allow", "deny", "allow_with_mfa"
}

// PolicyTargets defines which users/groups/apps a policy applies to.
type PolicyTargets struct {
	UserGroups   []string `json:"userGroups"`
	Applications []string `json:"applications"`
	AllUsers     bool     `json:"allUsers"`
}

// PolicyResult is the evaluation result for a single policy.
type PolicyResult struct {
	PolicyID   string `json:"policy_id"`
	PolicyName string `json:"policy_name"`
	Match      string `json:"match"`  // "applies", "does_not_apply", "unknown"
	Effect     string `json:"effect"` // "allow", "deny", "allow_with_mfa", "n/a"
	Reason     string `json:"reason"`
}

// SimulationReport is the top-level simulation result.
type SimulationReport struct {
	Outcome  string         `json:"outcome"` // "allowed", "denied", "mfa_required", "unknown"
	Policies []PolicyResult `json:"policies"`
	Summary  string         `json:"summary"`
}

// TriState represents a three-valued logic result.
type TriState int

const (
	TriTrue    TriState = 1
	TriFalse   TriState = -1
	TriUnknown TriState = 0
)

// IPListResolver provides IP entries for a given IP list ID.
// This allows the simulator to remain independent of API calls.
type IPListResolver func(listID string) ([]string, error)

// EvaluatePolicy evaluates a single policy against the given context.
func EvaluatePolicy(policy Policy, ctx SimulationContext, ipResolver IPListResolver) PolicyResult {
	result := PolicyResult{
		PolicyID:   policy.ID,
		PolicyName: policy.Name,
	}

	if policy.Disabled {
		result.Match = "does_not_apply"
		result.Effect = "n/a"
		result.Reason = "Policy is disabled"
		return result
	}

	// Check if the policy targets this user.
	if !policyTargetsUser(policy.Targets, ctx) {
		result.Match = "does_not_apply"
		result.Effect = "n/a"
		result.Reason = "User is not in policy target groups"
		return result
	}

	// Evaluate conditions.
	condMatch := evaluateConditions(policy.Conditions, ctx, ipResolver)

	switch condMatch {
	case TriTrue:
		result.Match = "applies"
		result.Effect = policy.Effect
		result.Reason = fmt.Sprintf("Conditions matched — effect: %s", policy.Effect)
	case TriFalse:
		result.Match = "does_not_apply"
		result.Effect = "n/a"
		result.Reason = "Conditions did not match"
	case TriUnknown:
		result.Match = "unknown"
		result.Effect = policy.Effect
		result.Reason = "Insufficient context to fully evaluate conditions"
	}

	return result
}

// Simulate evaluates all policies against the given context and returns
// a comprehensive report. Policies are evaluated deny-first.
func Simulate(policies []Policy, ctx SimulationContext, ipResolver IPListResolver) SimulationReport {
	report := SimulationReport{
		Policies: make([]PolicyResult, 0, len(policies)),
	}

	// Sort policies: deny-first, then allow_with_mfa, then allow.
	sorted := make([]Policy, len(policies))
	copy(sorted, policies)
	sort.Slice(sorted, func(i, j int) bool {
		return effectPriority(sorted[i].Effect) < effectPriority(sorted[j].Effect)
	})

	var hasUnknown bool
	var hasApply bool

	for _, p := range sorted {
		result := EvaluatePolicy(p, ctx, ipResolver)
		report.Policies = append(report.Policies, result)

		if result.Match == "unknown" {
			hasUnknown = true
		}

		if result.Match == "applies" {
			hasApply = true
			// First matching deny wins immediately.
			if result.Effect == "deny" {
				report.Outcome = "denied"
				report.Summary = fmt.Sprintf("Denied by policy %q", p.Name)
				return report
			}
		}
	}

	if hasUnknown && !hasApply {
		report.Outcome = "unknown"
		report.Summary = "Insufficient context to determine outcome — provide more simulation flags"
		return report
	}

	// Check for MFA requirement among matching policies.
	for _, r := range report.Policies {
		if r.Match == "applies" && r.Effect == "allow_with_mfa" {
			report.Outcome = "mfa_required"
			report.Summary = fmt.Sprintf("MFA required by policy %q", r.PolicyName)
			return report
		}
	}

	// If any policy explicitly allows.
	for _, r := range report.Policies {
		if r.Match == "applies" && r.Effect == "allow" {
			report.Outcome = "allowed"
			report.Summary = fmt.Sprintf("Allowed by policy %q", r.PolicyName)
			return report
		}
	}

	if hasUnknown {
		report.Outcome = "unknown"
		report.Summary = "Insufficient context to determine outcome — provide more simulation flags"
		return report
	}

	report.Outcome = "allowed"
	report.Summary = "No matching policies deny or restrict access"
	return report
}

// effectPriority returns a sort priority for policy effects (lower = evaluated first).
func effectPriority(effect string) int {
	switch effect {
	case "deny":
		return 0
	case "allow_with_mfa":
		return 1
	case "allow":
		return 2
	default:
		return 3
	}
}

// policyTargetsUser returns true if the policy applies to the user.
func policyTargetsUser(targets PolicyTargets, ctx SimulationContext) bool {
	if targets.AllUsers {
		return true
	}

	for _, gid := range targets.UserGroups {
		for _, ug := range ctx.UserGroups {
			if gid == ug {
				return true
			}
		}
	}

	return false
}

// evaluateConditions evaluates the conditions JSON tree against the context.
func evaluateConditions(raw json.RawMessage, ctx SimulationContext, ipResolver IPListResolver) TriState {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" {
		// No conditions = always matches.
		return TriTrue
	}

	var cond map[string]json.RawMessage
	if err := json.Unmarshal(raw, &cond); err != nil {
		return TriUnknown
	}

	if len(cond) == 0 {
		return TriTrue
	}

	// Process logical operators.
	if allRaw, ok := cond["all"]; ok {
		return evalAll(allRaw, ctx, ipResolver)
	}
	if anyRaw, ok := cond["any"]; ok {
		return evalAny(anyRaw, ctx, ipResolver)
	}
	if notRaw, ok := cond["not"]; ok {
		inner := evaluateConditions(notRaw, ctx, ipResolver)
		return triNot(inner)
	}

	// Process leaf predicates.
	return evaluateLeafPredicates(cond, ctx, ipResolver)
}

func evalAll(raw json.RawMessage, ctx SimulationContext, ipResolver IPListResolver) TriState {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return TriUnknown
	}

	hasUnknown := false
	for _, item := range items {
		result := evaluateConditions(item, ctx, ipResolver)
		if result == TriFalse {
			return TriFalse
		}
		if result == TriUnknown {
			hasUnknown = true
		}
	}

	if hasUnknown {
		return TriUnknown
	}
	return TriTrue
}

func evalAny(raw json.RawMessage, ctx SimulationContext, ipResolver IPListResolver) TriState {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return TriUnknown
	}

	hasUnknown := false
	for _, item := range items {
		result := evaluateConditions(item, ctx, ipResolver)
		if result == TriTrue {
			return TriTrue
		}
		if result == TriUnknown {
			hasUnknown = true
		}
	}

	if hasUnknown {
		return TriUnknown
	}
	return TriFalse
}

func triNot(t TriState) TriState {
	switch t {
	case TriTrue:
		return TriFalse
	case TriFalse:
		return TriTrue
	default:
		return TriUnknown
	}
}

// evaluateLeafPredicates evaluates individual condition predicates.
func evaluateLeafPredicates(cond map[string]json.RawMessage, ctx SimulationContext, ipResolver IPListResolver) TriState {
	result := TriTrue

	for key, val := range cond {
		var leafResult TriState

		switch key {
		case "ipAddressIn":
			leafResult = evalIPAddressIn(val, ctx, ipResolver)
		case "ipAddressNotIn":
			leafResult = triNot(evalIPAddressIn(val, ctx, ipResolver))
		case "deviceManaged":
			leafResult = evalBoolPredicate(val, ctx.DeviceManaged)
		case "deviceEncrypted":
			leafResult = evalBoolPredicate(val, ctx.DeviceEncrypted)
		case "locationIn":
			leafResult = evalLocationIn(val, ctx)
		case "locationNotIn":
			leafResult = triNot(evalLocationIn(val, ctx))
		case "mfaConfigured":
			leafResult = evalMFAConfigured(val, ctx)
		default:
			// Unknown predicate — can't evaluate.
			leafResult = TriUnknown
		}

		// All predicates at this level are implicitly AND-ed.
		if leafResult == TriFalse {
			return TriFalse
		}
		if leafResult == TriUnknown {
			result = TriUnknown
		}
	}

	return result
}

func evalIPAddressIn(val json.RawMessage, ctx SimulationContext, ipResolver IPListResolver) TriState {
	if ctx.IP == "" {
		return TriUnknown
	}

	// Value can be a string (IP list ID) or array of strings.
	var listID string
	if err := json.Unmarshal(val, &listID); err == nil {
		if ipResolver == nil {
			return TriUnknown
		}
		entries, err := ipResolver(listID)
		if err != nil {
			return TriUnknown
		}
		if MatchIP(ctx.IP, entries) {
			return TriTrue
		}
		return TriFalse
	}

	// Try as direct IP list.
	var ips []string
	if err := json.Unmarshal(val, &ips); err == nil {
		if MatchIP(ctx.IP, ips) {
			return TriTrue
		}
		return TriFalse
	}

	return TriUnknown
}

func evalBoolPredicate(val json.RawMessage, actual *bool) TriState {
	if actual == nil {
		return TriUnknown
	}

	var expected bool
	if err := json.Unmarshal(val, &expected); err != nil {
		return TriUnknown
	}

	if *actual == expected {
		return TriTrue
	}
	return TriFalse
}

func evalLocationIn(val json.RawMessage, ctx SimulationContext) TriState {
	if ctx.Location == "" {
		return TriUnknown
	}

	// Value can be a single string or array of strings.
	var single string
	if err := json.Unmarshal(val, &single); err == nil {
		if strings.EqualFold(ctx.Location, single) {
			return TriTrue
		}
		return TriFalse
	}

	var locations []string
	if err := json.Unmarshal(val, &locations); err == nil {
		for _, loc := range locations {
			if strings.EqualFold(ctx.Location, loc) {
				return TriTrue
			}
		}
		return TriFalse
	}

	return TriUnknown
}

func evalMFAConfigured(val json.RawMessage, ctx SimulationContext) TriState {
	var expected bool
	if err := json.Unmarshal(val, &expected); err != nil {
		return TriUnknown
	}

	if ctx.MFAConfigured == expected {
		return TriTrue
	}
	return TriFalse
}
