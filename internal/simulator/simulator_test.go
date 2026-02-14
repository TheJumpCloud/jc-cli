package simulator

import (
	"encoding/json"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func makeIPResolver(lists map[string][]string) IPListResolver {
	return func(listID string) ([]string, error) {
		if ips, ok := lists[listID]; ok {
			return ips, nil
		}
		return nil, nil
	}
}

func makePolicy(id, name, effect string, disabled bool, targets PolicyTargets, conditions any) Policy {
	condJSON, _ := json.Marshal(conditions)
	return Policy{
		ID:         id,
		Name:       name,
		Disabled:   disabled,
		Type:       "user_portal",
		Conditions: condJSON,
		Targets:    targets,
		Effect:     effect,
	}
}

func allUsersTarget() PolicyTargets {
	return PolicyTargets{AllUsers: true}
}

func groupTarget(groups ...string) PolicyTargets {
	return PolicyTargets{UserGroups: groups}
}

// --- EvaluatePolicy Tests ---

func TestEvaluatePolicy_DisabledPolicy(t *testing.T) {
	p := makePolicy("p1", "Disabled", "deny", true, allUsersTarget(), nil)
	ctx := SimulationContext{UserID: "u1"}

	result := EvaluatePolicy(p, ctx, nil)

	if result.Match != "does_not_apply" {
		t.Errorf("match = %q, want does_not_apply", result.Match)
	}
	if result.Effect != "n/a" {
		t.Errorf("effect = %q, want n/a", result.Effect)
	}
}

func TestEvaluatePolicy_UserNotInTarget(t *testing.T) {
	p := makePolicy("p1", "Group Only", "deny", false, groupTarget("group1"), nil)
	ctx := SimulationContext{UserID: "u1", UserGroups: []string{"group2"}}

	result := EvaluatePolicy(p, ctx, nil)

	if result.Match != "does_not_apply" {
		t.Errorf("match = %q, want does_not_apply", result.Match)
	}
}

func TestEvaluatePolicy_AllUsersTarget(t *testing.T) {
	p := makePolicy("p1", "Allow All", "allow", false, allUsersTarget(), nil)
	ctx := SimulationContext{UserID: "u1"}

	result := EvaluatePolicy(p, ctx, nil)

	if result.Match != "applies" {
		t.Errorf("match = %q, want applies", result.Match)
	}
	if result.Effect != "allow" {
		t.Errorf("effect = %q, want allow", result.Effect)
	}
}

func TestEvaluatePolicy_GroupTarget_Match(t *testing.T) {
	p := makePolicy("p1", "Group Policy", "deny", false, groupTarget("g1", "g2"), nil)
	ctx := SimulationContext{UserID: "u1", UserGroups: []string{"g2", "g3"}}

	result := EvaluatePolicy(p, ctx, nil)

	if result.Match != "applies" {
		t.Errorf("match = %q, want applies", result.Match)
	}
	if result.Effect != "deny" {
		t.Errorf("effect = %q, want deny", result.Effect)
	}
}

func TestEvaluatePolicy_IPCondition_Match(t *testing.T) {
	conditions := map[string]any{
		"ipAddressIn": []string{"10.0.0.0/24"},
	}
	p := makePolicy("p1", "IP Policy", "allow", false, allUsersTarget(), conditions)
	ctx := SimulationContext{UserID: "u1", IP: "10.0.0.5"}

	result := EvaluatePolicy(p, ctx, nil)

	if result.Match != "applies" {
		t.Errorf("match = %q, want applies", result.Match)
	}
}

func TestEvaluatePolicy_IPCondition_NoMatch(t *testing.T) {
	conditions := map[string]any{
		"ipAddressIn": []string{"10.0.0.0/24"},
	}
	p := makePolicy("p1", "IP Policy", "allow", false, allUsersTarget(), conditions)
	ctx := SimulationContext{UserID: "u1", IP: "192.168.1.1"}

	result := EvaluatePolicy(p, ctx, nil)

	if result.Match != "does_not_apply" {
		t.Errorf("match = %q, want does_not_apply", result.Match)
	}
}

func TestEvaluatePolicy_IPCondition_Unknown(t *testing.T) {
	conditions := map[string]any{
		"ipAddressIn": []string{"10.0.0.0/24"},
	}
	p := makePolicy("p1", "IP Policy", "allow", false, allUsersTarget(), conditions)
	ctx := SimulationContext{UserID: "u1"} // No IP provided.

	result := EvaluatePolicy(p, ctx, nil)

	if result.Match != "unknown" {
		t.Errorf("match = %q, want unknown", result.Match)
	}
}

func TestEvaluatePolicy_IPListResolver(t *testing.T) {
	conditions := map[string]any{
		"ipAddressIn": "list001",
	}
	resolver := makeIPResolver(map[string][]string{
		"list001": {"10.0.0.0/24", "192.168.1.0/24"},
	})
	p := makePolicy("p1", "IP List Policy", "allow", false, allUsersTarget(), conditions)
	ctx := SimulationContext{UserID: "u1", IP: "192.168.1.50"}

	result := EvaluatePolicy(p, ctx, resolver)

	if result.Match != "applies" {
		t.Errorf("match = %q, want applies", result.Match)
	}
}

func TestEvaluatePolicy_DeviceManaged(t *testing.T) {
	conditions := map[string]any{
		"deviceManaged": true,
	}
	p := makePolicy("p1", "Managed Only", "allow", false, allUsersTarget(), conditions)

	// Device is managed.
	ctx := SimulationContext{UserID: "u1", DeviceManaged: boolPtr(true)}
	result := EvaluatePolicy(p, ctx, nil)
	if result.Match != "applies" {
		t.Errorf("managed device: match = %q, want applies", result.Match)
	}

	// Device is not managed.
	ctx.DeviceManaged = boolPtr(false)
	result = EvaluatePolicy(p, ctx, nil)
	if result.Match != "does_not_apply" {
		t.Errorf("unmanaged device: match = %q, want does_not_apply", result.Match)
	}

	// Device status unknown.
	ctx.DeviceManaged = nil
	result = EvaluatePolicy(p, ctx, nil)
	if result.Match != "unknown" {
		t.Errorf("unknown device: match = %q, want unknown", result.Match)
	}
}

func TestEvaluatePolicy_LocationIn(t *testing.T) {
	conditions := map[string]any{
		"locationIn": []string{"US", "CA"},
	}
	p := makePolicy("p1", "Location Policy", "allow", false, allUsersTarget(), conditions)

	// User in US.
	ctx := SimulationContext{UserID: "u1", Location: "US"}
	result := EvaluatePolicy(p, ctx, nil)
	if result.Match != "applies" {
		t.Errorf("US location: match = %q, want applies", result.Match)
	}

	// User in DE.
	ctx.Location = "DE"
	result = EvaluatePolicy(p, ctx, nil)
	if result.Match != "does_not_apply" {
		t.Errorf("DE location: match = %q, want does_not_apply", result.Match)
	}

	// Location unknown.
	ctx.Location = ""
	result = EvaluatePolicy(p, ctx, nil)
	if result.Match != "unknown" {
		t.Errorf("unknown location: match = %q, want unknown", result.Match)
	}
}

func TestEvaluatePolicy_IPAddressNotIn(t *testing.T) {
	conditions := map[string]any{
		"ipAddressNotIn": []string{"10.0.0.0/24"},
	}
	p := makePolicy("p1", "Block Internal", "deny", false, allUsersTarget(), conditions)

	// IP outside the blocklist.
	ctx := SimulationContext{UserID: "u1", IP: "192.168.1.1"}
	result := EvaluatePolicy(p, ctx, nil)
	if result.Match != "applies" {
		t.Errorf("external IP: match = %q, want applies", result.Match)
	}

	// IP inside the blocklist.
	ctx.IP = "10.0.0.5"
	result = EvaluatePolicy(p, ctx, nil)
	if result.Match != "does_not_apply" {
		t.Errorf("internal IP: match = %q, want does_not_apply", result.Match)
	}
}

func TestEvaluatePolicy_MFAConfigured(t *testing.T) {
	conditions := map[string]any{
		"mfaConfigured": true,
	}
	p := makePolicy("p1", "MFA Check", "allow", false, allUsersTarget(), conditions)

	ctx := SimulationContext{UserID: "u1", MFAConfigured: true}
	result := EvaluatePolicy(p, ctx, nil)
	if result.Match != "applies" {
		t.Errorf("MFA configured: match = %q, want applies", result.Match)
	}

	ctx.MFAConfigured = false
	result = EvaluatePolicy(p, ctx, nil)
	if result.Match != "does_not_apply" {
		t.Errorf("MFA not configured: match = %q, want does_not_apply", result.Match)
	}
}

// --- Nested Conditions ---

func TestEvaluatePolicy_AllConditions(t *testing.T) {
	conditions := map[string]any{
		"all": []map[string]any{
			{"ipAddressIn": []string{"10.0.0.0/24"}},
			{"deviceManaged": true},
		},
	}
	p := makePolicy("p1", "All Cond", "allow", false, allUsersTarget(), conditions)

	// Both conditions true.
	ctx := SimulationContext{UserID: "u1", IP: "10.0.0.5", DeviceManaged: boolPtr(true)}
	result := EvaluatePolicy(p, ctx, nil)
	if result.Match != "applies" {
		t.Errorf("both true: match = %q, want applies", result.Match)
	}

	// One false.
	ctx.IP = "192.168.1.1"
	result = EvaluatePolicy(p, ctx, nil)
	if result.Match != "does_not_apply" {
		t.Errorf("one false: match = %q, want does_not_apply", result.Match)
	}

	// One unknown.
	ctx.IP = "10.0.0.5"
	ctx.DeviceManaged = nil
	result = EvaluatePolicy(p, ctx, nil)
	if result.Match != "unknown" {
		t.Errorf("one unknown: match = %q, want unknown", result.Match)
	}
}

func TestEvaluatePolicy_AnyConditions(t *testing.T) {
	conditions := map[string]any{
		"any": []map[string]any{
			{"ipAddressIn": []string{"10.0.0.0/24"}},
			{"locationIn": "US"},
		},
	}
	p := makePolicy("p1", "Any Cond", "allow", false, allUsersTarget(), conditions)

	// First true.
	ctx := SimulationContext{UserID: "u1", IP: "10.0.0.5", Location: "DE"}
	result := EvaluatePolicy(p, ctx, nil)
	if result.Match != "applies" {
		t.Errorf("first true: match = %q, want applies", result.Match)
	}

	// Second true.
	ctx.IP = "192.168.1.1"
	ctx.Location = "US"
	result = EvaluatePolicy(p, ctx, nil)
	if result.Match != "applies" {
		t.Errorf("second true: match = %q, want applies", result.Match)
	}

	// Both false.
	ctx.IP = "192.168.1.1"
	ctx.Location = "DE"
	result = EvaluatePolicy(p, ctx, nil)
	if result.Match != "does_not_apply" {
		t.Errorf("both false: match = %q, want does_not_apply", result.Match)
	}
}

func TestEvaluatePolicy_NotCondition(t *testing.T) {
	conditions := map[string]any{
		"not": map[string]any{
			"locationIn": "CN",
		},
	}
	p := makePolicy("p1", "Not CN", "allow", false, allUsersTarget(), conditions)

	// Not in CN.
	ctx := SimulationContext{UserID: "u1", Location: "US"}
	result := EvaluatePolicy(p, ctx, nil)
	if result.Match != "applies" {
		t.Errorf("not CN: match = %q, want applies", result.Match)
	}

	// In CN.
	ctx.Location = "CN"
	result = EvaluatePolicy(p, ctx, nil)
	if result.Match != "does_not_apply" {
		t.Errorf("in CN: match = %q, want does_not_apply", result.Match)
	}
}

// --- Simulate (multi-policy) Tests ---

func TestSimulate_SingleAllow(t *testing.T) {
	policies := []Policy{
		makePolicy("p1", "Allow All", "allow", false, allUsersTarget(), nil),
	}
	ctx := SimulationContext{UserID: "u1"}

	report := Simulate(policies, ctx, nil)

	if report.Outcome != "allowed" {
		t.Errorf("outcome = %q, want allowed", report.Outcome)
	}
	if len(report.Policies) != 1 {
		t.Fatalf("got %d policy results, want 1", len(report.Policies))
	}
}

func TestSimulate_SingleDeny(t *testing.T) {
	policies := []Policy{
		makePolicy("p1", "Deny All", "deny", false, allUsersTarget(), nil),
	}
	ctx := SimulationContext{UserID: "u1"}

	report := Simulate(policies, ctx, nil)

	if report.Outcome != "denied" {
		t.Errorf("outcome = %q, want denied", report.Outcome)
	}
}

func TestSimulate_DenyOverridesAllow(t *testing.T) {
	policies := []Policy{
		makePolicy("p1", "Allow", "allow", false, allUsersTarget(), nil),
		makePolicy("p2", "Deny", "deny", false, allUsersTarget(), nil),
	}
	ctx := SimulationContext{UserID: "u1"}

	report := Simulate(policies, ctx, nil)

	if report.Outcome != "denied" {
		t.Errorf("outcome = %q, want denied (deny overrides allow)", report.Outcome)
	}
}

func TestSimulate_MFARequired(t *testing.T) {
	policies := []Policy{
		makePolicy("p1", "MFA Policy", "allow_with_mfa", false, allUsersTarget(), nil),
	}
	ctx := SimulationContext{UserID: "u1"}

	report := Simulate(policies, ctx, nil)

	if report.Outcome != "mfa_required" {
		t.Errorf("outcome = %q, want mfa_required", report.Outcome)
	}
}

func TestSimulate_UnknownContext(t *testing.T) {
	conditions := map[string]any{
		"deviceManaged": true,
	}
	policies := []Policy{
		makePolicy("p1", "Device Policy", "deny", false, allUsersTarget(), conditions),
	}
	ctx := SimulationContext{UserID: "u1"} // No device info.

	report := Simulate(policies, ctx, nil)

	if report.Outcome != "unknown" {
		t.Errorf("outcome = %q, want unknown", report.Outcome)
	}
}

func TestSimulate_DisabledPolicySkipped(t *testing.T) {
	policies := []Policy{
		makePolicy("p1", "Disabled Deny", "deny", true, allUsersTarget(), nil),
		makePolicy("p2", "Allow", "allow", false, allUsersTarget(), nil),
	}
	ctx := SimulationContext{UserID: "u1"}

	report := Simulate(policies, ctx, nil)

	if report.Outcome != "allowed" {
		t.Errorf("outcome = %q, want allowed (disabled deny should be skipped)", report.Outcome)
	}

	// The disabled policy should show does_not_apply.
	for _, pr := range report.Policies {
		if pr.PolicyName == "Disabled Deny" && pr.Match != "does_not_apply" {
			t.Errorf("disabled policy match = %q, want does_not_apply", pr.Match)
		}
	}
}

func TestSimulate_NoPoliciesApply(t *testing.T) {
	policies := []Policy{
		makePolicy("p1", "Group Policy", "deny", false, groupTarget("other-group"), nil),
	}
	ctx := SimulationContext{UserID: "u1", UserGroups: []string{"my-group"}}

	report := Simulate(policies, ctx, nil)

	if report.Outcome != "allowed" {
		t.Errorf("outcome = %q, want allowed (no policies apply)", report.Outcome)
	}
}

func TestSimulate_IPBasedDeny(t *testing.T) {
	conditions := map[string]any{
		"ipAddressNotIn": []string{"10.0.0.0/8"},
	}
	policies := []Policy{
		makePolicy("p1", "Block External", "deny", false, allUsersTarget(), conditions),
		makePolicy("p2", "Allow Internal", "allow", false, allUsersTarget(), nil),
	}

	// External IP — should be denied.
	ctx := SimulationContext{UserID: "u1", IP: "203.0.113.1"}
	report := Simulate(policies, ctx, nil)
	if report.Outcome != "denied" {
		t.Errorf("external IP: outcome = %q, want denied", report.Outcome)
	}

	// Internal IP — deny doesn't apply, allow does.
	ctx.IP = "10.0.0.5"
	report = Simulate(policies, ctx, nil)
	if report.Outcome != "allowed" {
		t.Errorf("internal IP: outcome = %q, want allowed", report.Outcome)
	}
}

func TestSimulate_EmptyPolicies(t *testing.T) {
	report := Simulate(nil, SimulationContext{UserID: "u1"}, nil)

	if report.Outcome != "allowed" {
		t.Errorf("outcome = %q, want allowed (no policies)", report.Outcome)
	}
}

func TestSimulate_ComplexConditions(t *testing.T) {
	// Policy: deny if NOT in US AND device is NOT managed.
	conditions := map[string]any{
		"all": []map[string]any{
			{"not": map[string]any{"locationIn": "US"}},
			{"not": map[string]any{"deviceManaged": true}},
		},
	}
	policies := []Policy{
		makePolicy("p1", "Geo+Device", "deny", false, allUsersTarget(), conditions),
	}

	// Outside US, unmanaged device — denied.
	ctx := SimulationContext{UserID: "u1", Location: "DE", DeviceManaged: boolPtr(false)}
	report := Simulate(policies, ctx, nil)
	if report.Outcome != "denied" {
		t.Errorf("DE+unmanaged: outcome = %q, want denied", report.Outcome)
	}

	// In US — deny doesn't apply.
	ctx.Location = "US"
	report = Simulate(policies, ctx, nil)
	if report.Outcome != "allowed" {
		t.Errorf("US: outcome = %q, want allowed", report.Outcome)
	}

	// Outside US, managed device — deny doesn't apply.
	ctx.Location = "DE"
	ctx.DeviceManaged = boolPtr(true)
	report = Simulate(policies, ctx, nil)
	if report.Outcome != "allowed" {
		t.Errorf("DE+managed: outcome = %q, want allowed", report.Outcome)
	}
}
