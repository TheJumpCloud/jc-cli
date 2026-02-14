package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
	"github.com/klaassen-consulting/jc/internal/simulator"
)

// authPolicyDefaultFields is the default field subset shown for auth policy output.
var authPolicyDefaultFields = []string{"id", "name", "disabled", "type", "conditions"}

// resolveAuthPolicy resolves an auth policy name or ID to a JumpCloud auth policy ID.
func resolveAuthPolicy(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.AuthPolicyConfig)
}

func newAuthPoliciesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth-policies",
		Short: "Manage JumpCloud authentication policies",
		Long:  "List, get, create, update, delete, enable/disable, simulate, and analyze JumpCloud authentication policies for conditional access.",
	}

	cmd.AddCommand(newAuthPoliciesListCmd())
	cmd.AddCommand(newAuthPoliciesGetCmd())
	cmd.AddCommand(newAuthPoliciesCreateCmd())
	cmd.AddCommand(newAuthPoliciesUpdateCmd())
	cmd.AddCommand(newAuthPoliciesDeleteCmd())
	cmd.AddCommand(newAuthPoliciesEnableCmd())
	cmd.AddCommand(newAuthPoliciesDisableCmd())
	cmd.AddCommand(newAuthPoliciesSimulateCmd())
	cmd.AddCommand(newAuthPoliciesBlastRadiusCmd())

	return cmd
}

func newAuthPoliciesListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all authentication policies",
		Long: `List all JumpCloud authentication policies.

Default fields: id, name, disabled, type, conditions.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'name=MFA Required'     Exact match
  --filter 'disabled=false'        Active policies only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthPoliciesList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -name)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'name=MFA Required')")

	return cmd
}

func runAuthPoliciesList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/authn/policies", api.V2ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = authPolicyDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newAuthPoliciesGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get an authentication policy by name or ID",
		Long: `Get a single JumpCloud authentication policy by name or ID.

Accepts a policy name (e.g., "MFA Required") or a 24-character hex ID.
Names are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthPoliciesGet(cmd, args[0])
		},
	}

	return cmd
}

func runAuthPoliciesGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveAuthPolicy(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/authn/policies/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAuthPoliciesCreateCmd() *cobra.Command {
	var (
		name               string
		policyType         string
		disabled           bool
		conditions         string
		mfa                bool
		allowMFAEnrollment bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new authentication policy",
		Long: `Create a new JumpCloud authentication policy.

Required field: --name.
Use --conditions to supply the conditions tree as a JSON string.
Use --mfa to require MFA, or --allow-mfa-enrollment to allow self-enrollment.

The newly created policy object is returned.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthPoliciesCreate(cmd, name, policyType, disabled, conditions, mfa, allowMFAEnrollment)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Policy name (required)")
	cmd.Flags().StringVar(&policyType, "type", "", "Policy type (e.g., user_portal, admin)")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "Create the policy in disabled state")
	cmd.Flags().StringVar(&conditions, "conditions", "", "Conditions tree as raw JSON string")
	cmd.Flags().BoolVar(&mfa, "mfa", false, "Require MFA for this policy")
	cmd.Flags().BoolVar(&allowMFAEnrollment, "allow-mfa-enrollment", false, "Allow MFA self-enrollment")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func runAuthPoliciesCreate(cmd *cobra.Command, name, policyType string, disabled bool, conditions string, mfa, allowMFAEnrollment bool) error {
	if viper.GetBool("plan") {
		effects := []string{"name: " + name}
		if policyType != "" {
			effects = append(effects, "type: "+policyType)
		}
		if disabled {
			effects = append(effects, "disabled: true")
		}
		if conditions != "" {
			effects = append(effects, "conditions: <json>")
		}
		if mfa {
			effects = append(effects, "mfa: required")
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "authentication policy",
			Target:     name,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":     name,
		"disabled": disabled,
	}
	if policyType != "" {
		body["type"] = policyType
	}
	if conditions != "" {
		var cond any
		if err := json.Unmarshal([]byte(conditions), &cond); err != nil {
			return fmt.Errorf("invalid --conditions JSON: %w", err)
		}
		body["conditions"] = cond
	}
	if mfa {
		body["mfa"] = map[string]any{"required": true}
	}
	if allowMFAEnrollment {
		if mfaMap, ok := body["mfa"].(map[string]any); ok {
			mfaMap["allowEnrollment"] = true
		} else {
			body["mfa"] = map[string]any{"allowEnrollment": true}
		}
	}

	result, err := client.Create(cmd.Context(), "/authn/policies", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAuthPoliciesUpdateCmd() *cobra.Command {
	var (
		name       string
		conditions string
		disabled   bool
		enabled    bool
	)

	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Update an authentication policy",
		Long: `Update an existing JumpCloud authentication policy.

Accepts a policy name or 24-character hex ID.
Specify only the fields you want to change. The updated policy object is returned.

Use --disabled or --enabled to toggle the policy state.
Use --conditions to replace the conditions tree (raw JSON).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthPoliciesUpdate(cmd, args[0], name, conditions, disabled, enabled)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Policy name")
	cmd.Flags().StringVar(&conditions, "conditions", "", "Conditions tree as raw JSON string")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "Disable the policy")
	cmd.Flags().BoolVar(&enabled, "enabled", false, "Enable the policy")

	return cmd
}

func runAuthPoliciesUpdate(cmd *cobra.Command, identifier, name, conditions string, disabled, enabled bool) error {
	if cmd.Flags().Changed("disabled") && cmd.Flags().Changed("enabled") {
		return fmt.Errorf("--disabled and --enabled are mutually exclusive")
	}

	body := map[string]any{}

	if cmd.Flags().Changed("name") {
		body["name"] = name
	}
	if cmd.Flags().Changed("conditions") {
		var cond any
		if err := json.Unmarshal([]byte(conditions), &cond); err != nil {
			return fmt.Errorf("invalid --conditions JSON: %w", err)
		}
		body["conditions"] = cond
	}
	if cmd.Flags().Changed("disabled") {
		body["disabled"] = disabled
	}
	if cmd.Flags().Changed("enabled") {
		body["disabled"] = !enabled
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --name, --conditions, --disabled, --enabled)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "authentication policy",
			Target:     identifier,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveAuthPolicy(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/authn/policies/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAuthPoliciesDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <name-or-id>",
		Aliases: []string{"rm"},
		Short:   "Delete an authentication policy",
		Long: `Delete a JumpCloud authentication policy.

Accepts a policy name or 24-character hex ID.
Shows the policy name before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthPoliciesDelete(cmd, args[0])
		},
	}

	return cmd
}

func runAuthPoliciesDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveAuthPolicy(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	policyData, err := client.Get(cmd.Context(), "/authn/policies/"+id)
	if err != nil {
		return err
	}

	var policy struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(policyData, &policy); err != nil {
		return fmt.Errorf("parsing policy data: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "authentication policy",
			Target:   fmt.Sprintf("%s (%s)", policy.Name, id),
			Effects:  []string{"Remove authentication policy and all target bindings"},
		}
		return renderPlan(cmd, p)
	}

	if !viper.GetBool("force") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete authentication policy %q? [y/N] ", policy.Name)
		reader := getConfirmReader()
		answer, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading confirmation: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
			return nil
		}
	}

	_, err = client.Delete(cmd.Context(), "/authn/policies/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Authentication policy %q deleted successfully.\n", policy.Name)
	return nil
}

func newAuthPoliciesEnableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable <name-or-id>",
		Short: "Enable an authentication policy",
		Long: `Enable a JumpCloud authentication policy (sets disabled: false).

Convenience command equivalent to: jc auth-policies update <id> --enabled`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthPoliciesToggle(cmd, args[0], false)
		},
	}

	return cmd
}

func newAuthPoliciesDisableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable <name-or-id>",
		Short: "Disable an authentication policy",
		Long: `Disable a JumpCloud authentication policy (sets disabled: true).

Convenience command equivalent to: jc auth-policies update <id> --disabled`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthPoliciesToggle(cmd, args[0], true)
		},
	}

	return cmd
}

func runAuthPoliciesToggle(cmd *cobra.Command, identifier string, disabled bool) error {
	action := "enable"
	if disabled {
		action = "disable"
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:     action,
			Resource:   "authentication policy",
			Target:     identifier,
			Effects:    []string{fmt.Sprintf("Set disabled: %v", disabled)},
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolveAuthPolicy(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	body := map[string]any{"disabled": disabled}
	result, err := client.Update(cmd.Context(), "/authn/policies/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

// --- Simulate Command ---

func newAuthPoliciesSimulateCmd() *cobra.Command {
	var (
		userFlag     string
		ipFlag       string
		deviceFlag   string
		locationFlag string
	)

	cmd := &cobra.Command{
		Use:   "simulate <policy-name-or-id>",
		Short: "Simulate policy evaluation for a user",
		Long: `Simulate whether a user would be allowed or denied by an authentication policy.

Resolves the policy and user, fetches the user's group memberships, and evaluates
the policy conditions locally. Missing context (no --ip, --device, --location)
results in "unknown" for conditions that depend on that data.

Examples:
  jc auth-policies simulate "MFA Required" --user jdoe
  jc auth-policies simulate "MFA Required" --user jdoe --ip 10.0.0.1
  jc auth-policies simulate "Block External" --user admin@corp.com --ip 203.0.113.5 --location US`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthPoliciesSimulate(cmd, args[0], userFlag, ipFlag, deviceFlag, locationFlag)
		},
	}

	cmd.Flags().StringVar(&userFlag, "user", "", "User name or ID to simulate (required)")
	cmd.Flags().StringVar(&ipFlag, "ip", "", "Source IP address for the simulation")
	cmd.Flags().StringVar(&deviceFlag, "device", "", "Device name or ID (fetches managed/encrypted status)")
	cmd.Flags().StringVar(&locationFlag, "location", "", "Country code (e.g., US, DE)")
	_ = cmd.MarkFlagRequired("user")

	return cmd
}

func runAuthPoliciesSimulate(cmd *cobra.Command, policyIdentifier, userIdentifier, ip, deviceIdentifier, location string) error {
	ctx := cmd.Context()

	v1Client, err := newV1Client()
	if err != nil {
		return err
	}

	v2Client, err := newV2Client()
	if err != nil {
		return err
	}

	// Resolve policy.
	policyID, err := resolveAuthPolicy(ctx, v2Client, policyIdentifier)
	if err != nil {
		return err
	}

	policyRaw, err := v2Client.Get(ctx, "/authn/policies/"+policyID)
	if err != nil {
		return err
	}

	var policy simulator.Policy
	if err := json.Unmarshal(policyRaw, &policy); err != nil {
		return fmt.Errorf("parsing policy: %w", err)
	}

	// Resolve user.
	userID, err := resolveUser(ctx, v1Client, userIdentifier)
	if err != nil {
		return err
	}

	// Fetch user's group memberships via V2 graph.
	userGroups, err := fetchUserGroupIDs(ctx, v2Client, userID)
	if err != nil {
		return err
	}

	simCtx := simulator.SimulationContext{
		UserID:     userID,
		UserGroups: userGroups,
		IP:         ip,
		Location:   location,
	}

	// Optionally resolve device status.
	if deviceIdentifier != "" {
		deviceID, err := resolveDevice(ctx, v1Client, deviceIdentifier)
		if err != nil {
			return err
		}
		simCtx.DeviceID = deviceID

		deviceRaw, err := v1Client.Get(ctx, "/systems/"+deviceID)
		if err == nil {
			managed, encrypted := parseDeviceStatus(deviceRaw)
			simCtx.DeviceManaged = managed
			simCtx.DeviceEncrypted = encrypted
		}
	}

	// Build IP resolver using V2 IP lists.
	ipResolver := func(listID string) ([]string, error) {
		return fetchIPListEntries(ctx, v2Client, listID)
	}

	result := simulator.EvaluatePolicy(policy, simCtx, ipResolver)

	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshaling result: %w", err)
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), json.RawMessage(data), opts)
}

// fetchUserGroupIDs returns the IDs of user groups the user belongs to
// by traversing the V2 graph associations.
func fetchUserGroupIDs(ctx context.Context, v2Client *api.V2Client, userID string) ([]string, error) {
	endpoint := "/users/" + userID + "/memberof"
	result, err := v2Client.ListAll(ctx, endpoint, api.V2ListOptions{})
	if err != nil {
		// Non-fatal: return empty groups rather than failing.
		return nil, nil
	}

	var groups []string
	for _, raw := range result.Data {
		var assoc struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &assoc); err == nil && assoc.Type == "user_group" {
			groups = append(groups, assoc.ID)
		}
	}
	return groups, nil
}

// fetchIPListEntries fetches the IP entries from an IP list by ID.
func fetchIPListEntries(ctx context.Context, v2Client *api.V2Client, listID string) ([]string, error) {
	raw, err := v2Client.Get(ctx, "/iplists/"+listID)
	if err != nil {
		return nil, err
	}

	var ipList struct {
		IPs []string `json:"ips"`
	}
	if err := json.Unmarshal(raw, &ipList); err != nil {
		return nil, fmt.Errorf("parsing IP list: %w", err)
	}
	return ipList.IPs, nil
}

// parseDeviceStatus extracts managed and encrypted status from a device JSON object.
func parseDeviceStatus(raw json.RawMessage) (managed *bool, encrypted *bool) {
	var device map[string]any
	if err := json.Unmarshal(raw, &device); err != nil {
		return nil, nil
	}

	if v, ok := device["agentVersion"].(string); ok && v != "" {
		t := true
		managed = &t
	}

	if fde, ok := device["fde"].(map[string]any); ok {
		if active, ok := fde["active"].(bool); ok {
			encrypted = &active
		}
	}

	return managed, encrypted
}

// --- Blast Radius Command ---

func newAuthPoliciesBlastRadiusCmd() *cobra.Command {
	var limitFlag int

	cmd := &cobra.Command{
		Use:   "blast-radius <policy-name-or-id>",
		Short: "Show users and groups affected by a policy",
		Long: `Analyze the blast radius of an authentication policy.

Fetches the policy's target user groups, resolves group members, and reports
the deduplicated list of affected users. Useful for understanding impact
before enabling or modifying a policy.

Examples:
  jc auth-policies blast-radius "MFA Required"
  jc auth-policies blast-radius "Block External" --limit 50`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthPoliciesBlastRadius(cmd, args[0], limitFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 100, "Maximum number of affected users to return")

	return cmd
}

func runAuthPoliciesBlastRadius(cmd *cobra.Command, identifier string, limit int) error {
	ctx := cmd.Context()

	v2Client, err := newV2Client()
	if err != nil {
		return err
	}

	// Resolve policy.
	policyID, err := resolveAuthPolicy(ctx, v2Client, identifier)
	if err != nil {
		return err
	}

	policyRaw, err := v2Client.Get(ctx, "/authn/policies/"+policyID)
	if err != nil {
		return err
	}

	var policy struct {
		Name    string `json:"name"`
		Targets struct {
			UserGroups []string `json:"userGroups"`
			AllUsers   bool     `json:"allUsers"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(policyRaw, &policy); err != nil {
		return fmt.Errorf("parsing policy: %w", err)
	}

	if policy.Targets.AllUsers {
		fmt.Fprintf(cmd.ErrOrStderr(), "Policy %q targets ALL users.\n", policy.Name)
		return listAllUsersForBlastRadius(cmd, limit)
	}

	if len(policy.Targets.UserGroups) == 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Policy %q has no target user groups.\n", policy.Name)
		return nil
	}

	// Collect members from all target user groups.
	seen := make(map[string]bool)
	var members []json.RawMessage

	for _, groupID := range policy.Targets.UserGroups {
		if limit > 0 && len(members) >= limit {
			break
		}

		result, err := v2Client.ListAll(ctx, "/usergroups/"+groupID+"/members", api.V2ListOptions{})
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not fetch members for group %s: %v\n", groupID, err)
			continue
		}

		for _, raw := range result.Data {
			var member struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &member); err == nil && !seen[member.ID] {
				seen[member.ID] = true
				members = append(members, raw)
				if limit > 0 && len(members) >= limit {
					break
				}
			}
		}
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = []string{"id", "type"}

	if err := output.WriteList(cmd.OutOrStdout(), members, opts); err != nil {
		return err
	}

	summary := fmt.Sprintf("── %d affected users across %d groups ──", len(members), len(policy.Targets.UserGroups))
	if limit > 0 && len(members) >= limit {
		summary = fmt.Sprintf("── %d affected users (limited, may be more) across %d groups ──", len(members), len(policy.Targets.UserGroups))
	}
	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintln(cmd.ErrOrStderr(), summary)
	}

	return nil
}

// listAllUsersForBlastRadius lists users from V1 API when the policy targets all users.
func listAllUsersForBlastRadius(cmd *cobra.Command, limit int) error {
	v1Client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := v1Client.ListAll(cmd.Context(), "/systemusers", api.ListOptions{
		Limit: limit,
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = []string{"_id", "username", "email"}

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	count := fmt.Sprintf("── %d users (all users targeted) ──", len(result.Data))
	if limit > 0 && len(result.Data) >= limit {
		count = fmt.Sprintf("── %d users shown (limited, all users targeted) ──", len(result.Data))
	}
	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintln(cmd.ErrOrStderr(), count)
	}

	return nil
}
