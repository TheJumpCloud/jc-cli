package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/pwpolicy"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

type pwPolicyGetInput struct {
	Identifier string `json:"identifier" jsonschema:"Password policy object ID, or its name. Names are not unique and the org default has none, so prefer an object ID."`
}

type pwPolicyForUserInput struct {
	User string `json:"user" jsonschema:"Username, email, or user ID"`
}

type pwPolicyForGroupInput struct {
	Group string `json:"group" jsonschema:"User group name or ID"`
}

// pwPolicySettings carries the requirement fields shared by create and update.
// Every value is a pointer so that "not supplied" stays distinguishable from
// zero — 0 is a legal value for several of these settings.
type pwPolicySettings struct {
	Name        *string `json:"name,omitempty" jsonschema:"Policy name"`
	Description *string `json:"description,omitempty" jsonschema:"Policy description"`

	MinLength                           *int  `json:"min_length,omitempty" jsonschema:"Minimum password length"`
	NeedsLowercase                      *bool `json:"needs_lowercase,omitempty" jsonschema:"Require a lowercase character"`
	NeedsUppercase                      *bool `json:"needs_uppercase,omitempty" jsonschema:"Require an uppercase character"`
	NeedsNumeric                        *bool `json:"needs_numeric,omitempty" jsonschema:"Require a digit"`
	NeedsSymbolic                       *bool `json:"needs_symbolic,omitempty" jsonschema:"Require a symbol"`
	AllowUsernameSubstring              *bool `json:"allow_username_substring,omitempty" jsonschema:"Allow the username to appear inside the password"`
	DisallowCommonlyUsedPasswords       *bool `json:"disallow_common_passwords,omitempty" jsonschema:"Reject commonly used passwords"`
	DisallowSequentialOrRepetitiveChars *bool `json:"disallow_sequential_chars,omitempty" jsonschema:"Reject sequential or repeated characters"`

	PasswordExpirationInDays *int `json:"expiration_days,omitempty" jsonschema:"Days until a password expires"`
	MaxHistory               *int `json:"max_history,omitempty" jsonschema:"Number of previous passwords that may not be reused"`
	MinChangePeriodInDays    *int `json:"min_change_period_days,omitempty" jsonschema:"Minimum days between password changes"`

	MaxLoginAttempts           *int `json:"max_login_attempts,omitempty" jsonschema:"Failed attempts before lockout"`
	LockoutTimeInSeconds       *int `json:"lockout_seconds,omitempty" jsonschema:"Lockout duration in seconds"`
	ResetLockoutCounterMinutes *int `json:"reset_lockout_counter_minutes,omitempty" jsonschema:"Minutes before the failed-attempt counter resets"`

	DaysBeforeExpirationToForceReset *int  `json:"days_before_expiration_force_reset,omitempty" jsonschema:"Days before expiry to force a reset"`
	DaysAfterExpirationToSelfRecover *int  `json:"days_after_expiration_self_recover,omitempty" jsonschema:"Days after expiry a user may still self-recover (-1 for no limit)"`
	EnableRecoveryEmail              *bool `json:"enable_recovery_email,omitempty" jsonschema:"Allow password recovery by email"`
	AllowUnenrolledMFAPasswordReset  *bool `json:"allow_unenrolled_mfa_reset,omitempty" jsonschema:"Allow password reset for users not enrolled in MFA"`
	DisplayComplexityOnResetScreen   *bool `json:"display_complexity_on_reset,omitempty" jsonschema:"Show the complexity requirements on the reset screen"`

	Disable []string `json:"disable,omitempty" jsonschema:"Requirements to switch off without restating their value: min-length, expiration, max-history, min-change-period, max-login-attempts, lockout, reset-lockout-counter, days-before-expiration-force-reset, days-after-expiration-self-recover"`
}

type pwPolicyCreateInput struct {
	pwPolicySettings
	Groups  []string `json:"groups,omitempty" jsonschema:"User group names or IDs to bind the policy to. A policy bound to no groups governs nobody."`
	Execute bool     `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type pwPolicyUpdateInput struct {
	Identifier string `json:"identifier" jsonschema:"Password policy object ID or name"`
	pwPolicySettings
	Groups  []string `json:"groups,omitempty" jsonschema:"Replace the policy's group bindings with these group names or IDs. Omit to leave bindings alone; pass an empty array to clear them."`
	Execute bool     `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type pwPolicyDeleteInput struct {
	Identifiers []string `json:"identifiers" jsonschema:"Password policy object IDs or names to delete. Two or more are removed in one batch call."`
	Execute     bool     `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type pwPolicyPrecedenceEntryInput struct {
	Identifier string `json:"identifier" jsonschema:"Password policy object ID or name"`
	Precedence int    `json:"precedence" jsonschema:"Evaluation order; the LOWEST number wins when several policies cover a user"`
}

type pwPolicySetPrecedenceInput struct {
	Entries []pwPolicyPrecedenceEntryInput `json:"entries" jsonschema:"The policies to reorder, each with its new precedence"`
	Execute bool                           `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

// changeSet turns the supplied settings into the JSON-keyed change map that
// pwpolicy.ApplyChanges consumes. Only non-nil pointers become changes, which
// is what keeps an omitted field from being written as zero.
func (s pwPolicySettings) changeSet() (map[string]any, error) {
	changes := map[string]any{}

	add := func(key string, v any) {
		switch p := v.(type) {
		case *string:
			if p != nil {
				changes[key] = *p
			}
		case *int:
			if p != nil {
				changes[key] = *p
			}
		case *bool:
			if p != nil {
				changes[key] = *p
			}
		}
	}

	add("name", s.Name)
	add("description", s.Description)
	add("minLength", s.MinLength)
	add("needsLowercase", s.NeedsLowercase)
	add("needsUppercase", s.NeedsUppercase)
	add("needsNumeric", s.NeedsNumeric)
	add("needsSymbolic", s.NeedsSymbolic)
	add("allowUsernameSubstring", s.AllowUsernameSubstring)
	add("disallowCommonlyUsedPasswords", s.DisallowCommonlyUsedPasswords)
	add("disallowSequentialOrRepetitiveChars", s.DisallowSequentialOrRepetitiveChars)
	add("passwordExpirationInDays", s.PasswordExpirationInDays)
	add("maxHistory", s.MaxHistory)
	add("minChangePeriodInDays", s.MinChangePeriodInDays)
	add("maxLoginAttempts", s.MaxLoginAttempts)
	add("lockoutTimeInSeconds", s.LockoutTimeInSeconds)
	add("resetLockoutCounterMinutes", s.ResetLockoutCounterMinutes)
	add("daysBeforeExpirationToForceReset", s.DaysBeforeExpirationToForceReset)
	add("daysAfterExpirationToSelfRecover", s.DaysAfterExpirationToSelfRecover)
	add("enableRecoveryEmail", s.EnableRecoveryEmail)
	add("allowUnenrolledMFAPasswordReset", s.AllowUnenrolledMFAPasswordReset)
	add("displayComplexityOnResetScreen", s.DisplayComplexityOnResetScreen)

	for _, d := range s.Disable {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		enable, ok := pwPolicyDisableTargets[d]
		if !ok {
			return nil, fmt.Errorf("unknown disable target %q", d)
		}
		changes[enable] = false
	}

	return changes, nil
}

// pwPolicyDisableTargets mirrors the CLI's --disable vocabulary so both
// surfaces accept the same words for the same switches.
var pwPolicyDisableTargets = map[string]string{
	"min-length":                         "enableMinLength",
	"expiration":                         "enablePasswordExpirationInDays",
	"max-history":                        "enableMaxHistory",
	"min-change-period":                  "enableMinChangePeriodInDays",
	"max-login-attempts":                 "enableMaxLoginAttempts",
	"lockout":                            "enableLockoutTimeInSeconds",
	"reset-lockout-counter":              "enableResetLockoutCounter",
	"days-before-expiration-force-reset": "enableDaysBeforeExpirationToForceReset",
	"days-after-expiration-self-recover": "enableDaysAfterExpirationToSelfRecover",
}

// resolvePasswordPolicyID maps a name or object ID to an object ID. The
// generic V2 resolver cannot be used: the list endpoint answers a
// {results:[…]} envelope keyed on objectId, and names are neither unique nor
// required.
func resolvePasswordPolicyID(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	if resolve.IsID(identifier) {
		return identifier, nil
	}
	raw, err := client.Get(ctx, pwpolicy.Endpoint)
	if err != nil {
		return "", err
	}
	items, err := pwpolicy.ParseListItems(raw)
	if err != nil {
		return "", err
	}
	var matches []pwpolicy.ListItem
	for _, it := range items {
		if strings.EqualFold(it.Name, identifier) {
			matches = append(matches, it)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("password policy %q not found", identifier)
	case 1:
		return matches[0].ObjectID, nil
	default:
		ids := make([]string, 0, len(matches))
		for _, m := range matches {
			ids = append(ids, m.ObjectID)
		}
		return "", fmt.Errorf("password policy name %q is ambiguous (%d policies share it: %s); use an object ID",
			identifier, len(matches), strings.Join(ids, ", "))
	}
}

func resolvePasswordPolicyGroups(ctx context.Context, client *api.V2Client, groups []string) ([]string, error) {
	ids := make([]string, 0, len(groups))
	r := resolve.NewV2Resolver(client)
	for _, g := range groups {
		id, err := r.Resolve(ctx, g, resolve.UserGroupConfig)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *Server) registerPasswordPolicyTools() {
	addTypedTool(s, "password_policies_list", "List every JumpCloud password policy in the organization. Returns a flat, SPARSE projection: each row carries objectId, name, precedence, default, groupCount, and only the requirements that policy actually enables — a requirement missing from a row is switched off, not zero. Use password_policies_get for a policy's complete settings.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Get(ctx, pwpolicy.Endpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("listing password policies: %v", err)), nil, nil
			}
			rows, err := pwpolicy.ParseList(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			res, err := jsonResult(rows)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "password_policies_get", "Get one JumpCloud password policy in full by object ID or name, including every requirement and the user groups bound to it.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwPolicyGetInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolvePasswordPolicyID(ctx, client, args.Identifier)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := client.Get(ctx, pwpolicy.PolicyEndpoint(id))
			if err != nil {
				return errorResult(fmt.Sprintf("getting password policy: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "password_policies_for_user", "Show which password policy actually governs a JumpCloud user right now. This is the effective policy after group bindings and precedence are resolved server-side, not merely the policies the user's groups are bound to.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwPolicyForUserInput) (*mcp.CallToolResult, any, error) {
			v1, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			userID, err := resolveV1(ctx, v1, args.User, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Get(ctx, pwpolicy.UserEndpoint(userID))
			if err != nil {
				return errorResult(fmt.Sprintf("getting password policy for user: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "password_policies_for_group", "Show which password policy governs a JumpCloud user group.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwPolicyForGroupInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			groupID, err := r.Resolve(ctx, args.Group, resolve.UserGroupConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := client.Get(ctx, pwpolicy.UserGroupEndpoint(groupID))
			if err != nil {
				return errorResult(fmt.Sprintf("getting password policy for group: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "password_policies_create", "Create a JumpCloud password policy. A new policy starts with every requirement off; each field you supply switches its requirement on. Bind the policy to user groups with `groups` — a policy bound to no groups governs nobody. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwPolicyCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			changes, err := args.changeSet()
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			policy, err := pwpolicy.ApplyChanges(pwpolicy.Policy{}, changes)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}

			if !args.Execute {
				effects := map[string]any{"settings": pwpolicy.Diff(pwpolicy.Policy{}, policy)}
				if len(args.Groups) > 0 {
					effects["groups"] = args.Groups
				} else {
					effects["groups"] = "none — the policy will govern nobody until bound"
				}
				return planResult("create", "password policy", policy.Name, "", effects)
			}

			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			var groupIDs []string
			if len(args.Groups) > 0 {
				groupIDs, err = resolvePasswordPolicyGroups(ctx, client, args.Groups)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
			}
			raw, err := client.Create(ctx, pwpolicy.Endpoint, pwpolicy.Body(policy, groupIDs))
			if err != nil {
				return errorResult(fmt.Sprintf("creating password policy: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "password_policies_update", "Update a JumpCloud password policy, changing only the fields supplied. The current policy is read first and sent back complete with the changes applied, so untouched requirements are preserved. Supplying a value switches its requirement on; use `disable` to switch one off without restating its value. Group bindings are left alone unless `groups` is supplied, which replaces them. Set execute=true to apply; otherwise returns a plan showing the before/after.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwPolicyUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			changes, err := args.changeSet()
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if len(changes) == 0 && args.Groups == nil {
				return errorResult("no changes requested: supply at least one setting, a disable target, or groups"), nil, nil
			}

			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolvePasswordPolicyID(ctx, client, args.Identifier)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			curRaw, err := client.Get(ctx, pwpolicy.PolicyEndpoint(id))
			if err != nil {
				return errorResult(fmt.Sprintf("getting password policy: %v", err)), nil, nil
			}
			cur, err := pwpolicy.ParseDetail(curRaw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}

			next := cur.Policy
			if len(changes) > 0 {
				next, err = pwpolicy.ApplyChanges(cur.Policy, changes)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
			}

			groupIDs := cur.GroupIDs()
			if args.Groups != nil {
				groupIDs, err = resolvePasswordPolicyGroups(ctx, client, args.Groups)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
			}

			if !args.Execute {
				effects := map[string]any{"changes": pwpolicy.Diff(cur.Policy, next)}
				if args.Groups != nil {
					effects["groups"] = args.Groups
				}
				if cur.Policy.Default {
					effects["warning"] = "this is the organization default policy — it governs every user no other policy covers"
				}
				return planResult("update", "password policy", args.Identifier, id, effects)
			}

			raw, err := client.Update(ctx, pwpolicy.PolicyEndpoint(id), pwpolicy.Body(next, groupIDs))
			if err != nil {
				return errorResult(fmt.Sprintf("updating password policy: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "password_policies_delete", "Delete one or more JumpCloud password policies. Two or more are removed in a single batch call. Users governed by a deleted policy fall back to whichever policy covers them next, or to the organization default. The organization default policy cannot be deleted. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwPolicyDeleteInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			if len(args.Identifiers) == 0 {
				return errorResult("no policies given"), nil, nil
			}

			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}

			ids := make([]string, 0, len(args.Identifiers))
			labels := make([]string, 0, len(args.Identifiers))
			for _, identifier := range args.Identifiers {
				id, err := resolvePasswordPolicyID(ctx, client, identifier)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				raw, err := client.Get(ctx, pwpolicy.PolicyEndpoint(id))
				if err != nil {
					return errorResult(fmt.Sprintf("getting password policy: %v", err)), nil, nil
				}
				d, err := pwpolicy.ParseDetail(raw)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				if d.Policy.Default {
					return errorResult(fmt.Sprintf("password policy %s is the organization default and cannot be deleted", id)), nil, nil
				}
				ids = append(ids, id)
				label := d.Policy.Name
				if label == "" {
					label = id
				}
				labels = append(labels, fmt.Sprintf("%s (%d groups bound)", label, len(d.Groups)))
			}

			if !args.Execute {
				return planResult("delete", "password policy", strings.Join(args.Identifiers, ", "), strings.Join(ids, ","), map[string]any{
					"policies": labels,
					"impact":   "affected users fall back to the next policy that covers them, or the org default",
				})
			}

			if len(ids) == 1 {
				if _, err := client.Delete(ctx, pwpolicy.PolicyEndpoint(ids[0])); err != nil {
					return errorResult(fmt.Sprintf("deleting password policy: %v", err)), nil, nil
				}
			} else {
				if _, err := client.DeleteWithBody(ctx, pwpolicy.Endpoint, pwpolicy.BatchDeleteBody(ids)); err != nil {
					return errorResult(fmt.Sprintf("deleting password policies: %v", err)), nil, nil
				}
			}

			res, err := jsonResult(map[string]any{"deleted": ids})
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "password_policies_set_precedence", "Set the evaluation order of JumpCloud password policies. When a user falls under several policies the LOWEST precedence number wins. Precedence is set here rather than through password_policies_update, because the API owns the ordering as a whole. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args pwPolicySetPrecedenceInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			if len(args.Entries) == 0 {
				return errorResult("no entries given"), nil, nil
			}

			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}

			entries := make([]pwpolicy.PrecedenceEntry, 0, len(args.Entries))
			effects := make([]string, 0, len(args.Entries))
			for _, e := range args.Entries {
				id, err := resolvePasswordPolicyID(ctx, client, e.Identifier)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				entries = append(entries, pwpolicy.PrecedenceEntry{ObjectID: id, Precedence: e.Precedence})
				effects = append(effects, fmt.Sprintf("%s -> precedence %d", e.Identifier, e.Precedence))
			}

			if !args.Execute {
				return planResult("set-precedence", "password policy", fmt.Sprintf("%d policies", len(entries)), "", effects)
			}

			if _, err := client.Update(ctx, pwpolicy.PrecedenceEndpoint, entries); err != nil {
				return errorResult(fmt.Sprintf("setting password policy precedence: %v", err)), nil, nil
			}

			res, err := jsonResult(map[string]any{"updated": effects})
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)
}
