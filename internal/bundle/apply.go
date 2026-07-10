package bundle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/windows_mdm"
)

// Apply orchestration lives in this package (not internal/cmd) so the
// MCP bundle_apply tool reuses BuildApplyPlan/Execute instead of
// re-implementing the N-step sequence (KLA-470/472).
//
// The split is deliberate: BuildApplyPlan does ALL fallible read-only
// work — deep validation, in-memory mobileconfig emission, template
// resolution by name, name-conflict pre-flight — so Execute is a
// straight run of POSTs with no surprises left to discover mid-way.

// ApplyOptions configures one apply run.
type ApplyOptions struct {
	// PolicyGroupName overrides the default "<bundle> (v<version>)".
	PolicyGroupName string
	// DeviceGroupID, when set, binds the policy group to this device
	// group as the final step. Resolution from a name happens in the
	// caller (cmd has the resolver + cache plumbing).
	DeviceGroupID string
	// DeviceGroupName is display-only (step detail lines).
	DeviceGroupName string
}

// ApplyStep is one unit of work in the plan, in execution order —
// it powers both --plan preview and post-failure reporting.
type ApplyStep struct {
	// Kind is policy | policy_group | member | binding.
	Kind string `json:"kind"`
	// Name is the tenant-side display name of what this step touches.
	Name string `json:"name"`
	// Detail is one human line (template, unit counts, target).
	Detail string `json:"detail"`
}

// plannedPolicy is one ready-to-POST policy body.
type plannedPolicy struct {
	name string
	body map[string]any
}

// ApplyPlan is the fully-resolved, conflict-checked execution plan.
type ApplyPlan struct {
	Bundle          *Bundle
	PolicyGroupName string
	DeviceGroupID   string
	DeviceGroupName string
	Steps           []ApplyStep

	policies []plannedPolicy
}

// CreatedResource records one tenant object Execute created.
type CreatedResource struct {
	Kind string `json:"kind"` // policy | policy_group
	Name string `json:"name"`
	ID   string `json:"id"`
}

// ApplyResult is what Execute produced — on failure it still lists
// everything already created, so the caller can print exact cleanup
// commands instead of leaving the operator to hunt down strays.
type ApplyResult struct {
	Created       []CreatedResource `json:"created"`
	PolicyGroupID string            `json:"policy_group_id,omitempty"`
	Bound         bool              `json:"bound"`
}

// PolicyName composes the tenant policy name for a unit.
func PolicyName(b *Bundle, unitName string) string {
	return b.Name + "/" + unitName
}

// DefaultPolicyGroupName is the group Execute creates when the caller
// didn't override it. The version is part of the name on purpose:
// v1 apply is create-only, so applying a new bundle version lands
// beside the old group rather than colliding with it.
func DefaultPolicyGroupName(b *Bundle) string {
	return fmt.Sprintf("%s (v%s)", b.Name, b.Version)
}

// ProvenanceMarker is written into the policy group description so
// status (KLA-471) can match tenant state back to a bundle+version
// even if the operator renames things.
func ProvenanceMarker(b *Bundle) string {
	return fmt.Sprintf("bundle:%s@%s", b.Name, b.Version)
}

// BuildApplyPlan validates the bundle, builds every policy body
// in-memory (emitting mobileconfigs, resolving the JumpCloud templates
// actually needed by name), and pre-flights name conflicts. It
// performs only read-only API calls.
func BuildApplyPlan(ctx context.Context, client *api.V2Client, b *Bundle, cat *apple_mdm.Catalog, opts ApplyOptions) (*ApplyPlan, error) {
	if err := Validate(b, cat); err != nil {
		return nil, err
	}

	p := &ApplyPlan{
		Bundle:          b,
		PolicyGroupName: opts.PolicyGroupName,
		DeviceGroupID:   opts.DeviceGroupID,
		DeviceGroupName: opts.DeviceGroupName,
	}
	if p.PolicyGroupName == "" {
		p.PolicyGroupName = DefaultPolicyGroupName(b)
	}

	// Resolve each JumpCloud template at most once, and only the ones
	// this bundle actually uses.
	var (
		appleTmpls   = map[string]apple_mdm.CustomMDMTemplate{}
		omaURITmpl   *windows_mdm.CustomTemplate
		registryTmpl *windows_mdm.CustomTemplate
	)

	for i := range b.Policies {
		u := &b.Policies[i]
		policyName := PolicyName(b, u.Name)

		switch u.Type {
		case UnitAppleProfile:
			instances, env, err := u.Profile.BuildPayloadInstances(cat)
			if err != nil {
				// Validate already passed, so this is unreachable in
				// practice; kept for defense.
				return nil, err
			}
			if unsupported := apple_mdm.UnsupportedPayloadTypes(instances, u.OS); len(unsupported) > 0 {
				return nil, fmt.Errorf(
					"policies[%d] (%s): payload(s) do not declare support for %s: %s",
					i, u.Name, u.OS, strings.Join(unsupported, ", "))
			}
			family := apple_mdm.OSFamilyDarwin
			if u.OS == "iOS" {
				family = apple_mdm.OSFamilyIOS
			}
			tmpl, ok := appleTmpls[family]
			if !ok {
				tmpl, err = apple_mdm.ResolveCustomMDMTemplate(ctx, client, family)
				if err != nil {
					return nil, fmt.Errorf("resolving Custom MDM template (%s): %w", family, err)
				}
				appleTmpls[family] = tmpl
			}
			var plistBuf bytes.Buffer
			if err := apple_mdm.EmitMobileconfig(&plistBuf, env, instances); err != nil {
				return nil, fmt.Errorf("policies[%d] (%s): emitting mobileconfig: %w", i, u.Name, err)
			}
			redispatch := u.Redispatch == nil || *u.Redispatch
			p.policies = append(p.policies, plannedPolicy{
				name: policyName,
				body: apple_mdm.BuildCustomMDMPolicyBody(policyName, tmpl, plistBuf.Bytes(), redispatch),
			})
			p.Steps = append(p.Steps, ApplyStep{
				Kind: "policy", Name: policyName,
				Detail: fmt.Sprintf("%s profile, %d payload(s), template %s", u.OS, len(instances), tmpl.Name),
			})

		case UnitWindowsOMAURI:
			settings, err := windows_mdm.NormalizeAndValidateSettings(u.WindowsSettings())
			if err != nil {
				return nil, err // unreachable after Validate; defense
			}
			if omaURITmpl == nil {
				tmpl, err := windows_mdm.ResolveOMAURITemplate(ctx, client)
				if err != nil {
					return nil, fmt.Errorf("resolving Custom MDM (OMA-URI) template: %w", err)
				}
				omaURITmpl = &tmpl
			}
			p.policies = append(p.policies, plannedPolicy{
				name: policyName,
				body: windows_mdm.BuildOMAURIPolicyBody(policyName, *omaURITmpl, settings),
			})
			p.Steps = append(p.Steps, ApplyStep{
				Kind: "policy", Name: policyName,
				Detail: fmt.Sprintf("%d OMA-URI setting(s), template %s", len(settings), omaURITmpl.Name),
			})

		case UnitWindowsRegistry:
			keys, err := windows_mdm.NormalizeAndValidateKeys(u.WindowsKeys())
			if err != nil {
				return nil, err // unreachable after Validate; defense
			}
			if registryTmpl == nil {
				tmpl, err := windows_mdm.ResolveRegistryTemplate(ctx, client)
				if err != nil {
					return nil, fmt.Errorf("resolving Custom Registry Keys template: %w", err)
				}
				registryTmpl = &tmpl
			}
			p.policies = append(p.policies, plannedPolicy{
				name: policyName,
				body: windows_mdm.BuildRegistryPolicyBody(policyName, *registryTmpl, keys),
			})
			p.Steps = append(p.Steps, ApplyStep{
				Kind: "policy", Name: policyName,
				Detail: fmt.Sprintf("%d registry key(s), template %s", len(keys), registryTmpl.Name),
			})
		}
	}

	p.Steps = append(p.Steps, ApplyStep{
		Kind: "policy_group", Name: p.PolicyGroupName,
		Detail: fmt.Sprintf("policy group holding all %d policies (description: %s)", len(p.policies), ProvenanceMarker(b)),
	})
	for _, pol := range p.policies {
		p.Steps = append(p.Steps, ApplyStep{
			Kind: "member", Name: pol.name,
			Detail: "add to policy group " + p.PolicyGroupName,
		})
	}
	if p.DeviceGroupID != "" {
		target := p.DeviceGroupName
		if target == "" {
			target = p.DeviceGroupID
		}
		p.Steps = append(p.Steps, ApplyStep{
			Kind: "binding", Name: target,
			Detail: "bind device group → policy group " + p.PolicyGroupName,
		})
	}

	if err := p.preflightNameConflicts(ctx, client); err != nil {
		return nil, err
	}
	return p, nil
}

// preflightNameConflicts rejects the apply when any tenant policy or
// policy group already carries a name this plan would create. v1 apply
// is create-only by design — no silent adopt/update — so the operator
// gets every conflict plus remediation in one message.
func (p *ApplyPlan) preflightNameConflicts(ctx context.Context, client *api.V2Client) error {
	existing, err := listNames(ctx, client, "/policies")
	if err != nil {
		return fmt.Errorf("pre-flight: listing policies: %w", err)
	}
	var conflicts []string
	for _, pol := range p.policies {
		if existing[pol.name] {
			conflicts = append(conflicts, fmt.Sprintf("policy %q already exists", pol.name))
		}
	}

	groups, err := listNames(ctx, client, "/policygroups")
	if err != nil {
		return fmt.Errorf("pre-flight: listing policy groups: %w", err)
	}
	if groups[p.PolicyGroupName] {
		conflicts = append(conflicts, fmt.Sprintf("policy group %q already exists", p.PolicyGroupName))
	}

	if len(conflicts) > 0 {
		return fmt.Errorf(
			"apply would collide with existing tenant objects:\n  - %s\n"+
				"bundle apply is create-only: delete the previous apply (policy group + member policies), "+
				"bump the bundle version, or pass --policy-group-name",
			strings.Join(conflicts, "\n  - "))
	}
	return nil
}

// listNames fetches every object name on a V2 list endpoint.
func listNames(ctx context.Context, client *api.V2Client, endpoint string) (map[string]bool, error) {
	result, err := client.ListAll(ctx, endpoint, api.V2ListOptions{})
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(result.Data))
	for _, raw := range result.Data {
		var obj struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &obj); err == nil && obj.Name != "" {
			names[obj.Name] = true
		}
	}
	return names, nil
}

// Execute runs the plan: N policy creates, the policy group create,
// member adds, and the optional device-group binding — stopping at the
// first failure WITHOUT rolling back (auto-delete is destructive-class
// and a half-failed rollback is worse than a precise report). The
// returned ApplyResult always lists what was created, success or not.
func (p *ApplyPlan) Execute(ctx context.Context, client *api.V2Client) (*ApplyResult, error) {
	res := &ApplyResult{}

	fail := func(step string, err error) (*ApplyResult, error) {
		var cleanup []string
		for _, c := range res.Created {
			switch c.Kind {
			case "policy":
				cleanup = append(cleanup, fmt.Sprintf("  jc policies delete %s        # %s", c.ID, c.Name))
			case "policy_group":
				cleanup = append(cleanup, fmt.Sprintf("  jc policy-groups delete %s   # %s", c.ID, c.Name))
			}
		}
		msg := fmt.Sprintf("apply failed at %s: %v", step, err)
		if len(cleanup) > 0 {
			msg += fmt.Sprintf("\n%d object(s) were already created and were NOT rolled back:\n%s",
				len(res.Created), strings.Join(cleanup, "\n"))
		}
		return res, fmt.Errorf("%s", msg)
	}

	for _, pol := range p.policies {
		raw, err := client.Create(ctx, "/policies", pol.body)
		if err != nil {
			return fail(fmt.Sprintf("creating policy %q", pol.name), err)
		}
		id, err := idFrom(raw)
		if err != nil {
			return fail(fmt.Sprintf("creating policy %q", pol.name), err)
		}
		res.Created = append(res.Created, CreatedResource{Kind: "policy", Name: pol.name, ID: id})
	}

	raw, err := client.Create(ctx, "/policygroups", map[string]any{
		"name":        p.PolicyGroupName,
		"description": ProvenanceMarker(p.Bundle),
	})
	if err != nil {
		return fail(fmt.Sprintf("creating policy group %q", p.PolicyGroupName), err)
	}
	gid, err := idFrom(raw)
	if err != nil {
		return fail(fmt.Sprintf("creating policy group %q", p.PolicyGroupName), err)
	}
	res.PolicyGroupID = gid
	res.Created = append(res.Created, CreatedResource{Kind: "policy_group", Name: p.PolicyGroupName, ID: gid})

	for _, c := range res.Created {
		if c.Kind != "policy" {
			continue
		}
		_, err := client.Create(ctx, "/policygroups/"+gid+"/members", map[string]any{
			"op": "add", "type": "policy", "id": c.ID,
		})
		if err != nil {
			return fail(fmt.Sprintf("adding policy %q to policy group", c.Name), err)
		}
	}

	if p.DeviceGroupID != "" {
		_, err := client.Create(ctx, "/systemgroups/"+p.DeviceGroupID+"/associations", map[string]any{
			"op": "add", "type": "policy_group", "id": gid,
		})
		if err != nil {
			return fail("binding device group to policy group", err)
		}
		res.Bound = true
	}

	return res, nil
}

// idFrom extracts the id from a V2 create response.
func idFrom(raw json.RawMessage) (string, error) {
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", fmt.Errorf("decoding create response: %w", err)
	}
	if obj.ID == "" {
		return "", fmt.Errorf("create response carried no id: %s", string(raw))
	}
	return obj.ID, nil
}
