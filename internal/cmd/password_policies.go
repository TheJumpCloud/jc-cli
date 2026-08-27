package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/pwpolicy"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

func newPasswordPoliciesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "password-policies",
		Aliases: []string{"password-policy", "pwpolicies"},
		Short:   "Manage JumpCloud password policies",
		Long: `List, inspect, create, update, reorder, and delete JumpCloud password policies.

A password policy is bound to user groups. When a user falls under more than
one policy, the policy with the LOWEST precedence number wins; use
` + "`set-precedence`" + ` to change that order. Exactly one policy is the
org-wide default, which governs every user no group-bound policy covers.

Each requirement has a value and a separate switch (e.g. minLength and
enableMinLength). Setting a value through this command switches its
requirement on automatically, so a value can never be left silently inert.`,
	}

	cmd.AddCommand(newPasswordPoliciesListCmd())
	cmd.AddCommand(newPasswordPoliciesGetCmd())
	cmd.AddCommand(newPasswordPoliciesForUserCmd())
	cmd.AddCommand(newPasswordPoliciesForGroupCmd())
	cmd.AddCommand(newPasswordPoliciesCreateCmd())
	cmd.AddCommand(newPasswordPoliciesUpdateCmd())
	cmd.AddCommand(newPasswordPoliciesDeleteCmd())
	cmd.AddCommand(newPasswordPoliciesSetPrecedenceCmd())

	return cmd
}

// resolvePasswordPolicy maps a policy name or object ID to an object ID.
//
// The generic V2 resolver cannot be reused here: the list endpoint answers a
// {results:[…]} envelope keyed on objectId rather than id, and policy names
// are neither unique nor required (the org default ships with an empty name).
func resolvePasswordPolicy(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	if resolve.IsID(identifier) {
		return identifier, nil
	}

	items, err := listPasswordPolicies(ctx, client)
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

func listPasswordPolicies(ctx context.Context, client *api.V2Client) ([]pwpolicy.ListItem, error) {
	raw, err := client.Get(ctx, pwpolicy.Endpoint)
	if err != nil {
		return nil, err
	}
	return pwpolicy.ParseListItems(raw)
}

// ---------- list ----------

func newPasswordPoliciesListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all password policies",
		Long: `List every password policy in the organization, in precedence order.

Default fields: objectId, name, precedence, default, groupCount, minLength.

The list projection is sparse: it reports only the settings a policy actually
enables, so a requirement missing from a row is off, not zero. Use ` + "`get`" + `
for the complete policy.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPasswordPoliciesList(cmd)
		},
	}
	return cmd
}

func runPasswordPoliciesList(cmd *cobra.Command) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	raw, err := client.Get(cmd.Context(), pwpolicy.Endpoint)
	if err != nil {
		return err
	}

	rows, err := pwpolicy.ParseList(raw)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = pwpolicy.ListDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(rows))
	}
	return nil
}

// ---------- get / for-user / for-group ----------

func newPasswordPoliciesGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get a password policy by name or ID",
		Long: `Get one password policy in full, including every requirement and the user
groups bound to it.

Accepts an object ID or a policy name. Names are not unique in JumpCloud and
the org default has none, so an ambiguous name is reported rather than guessed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPasswordPoliciesGet(cmd, args[0])
		},
	}
	return cmd
}

func runPasswordPoliciesGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolvePasswordPolicy(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	raw, err := client.Get(cmd.Context(), pwpolicy.PolicyEndpoint(id))
	if err != nil {
		return err
	}

	return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
}

func newPasswordPoliciesForUserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "for-user <username-or-id>",
		Aliases: []string{"user"},
		Short:   "Show the password policy currently governing a user",
		Long: `Show which password policy actually applies to a user right now.

This is the effective policy after group bindings and precedence are resolved
server-side — not merely the policies the user's groups are bound to.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPasswordPoliciesForUser(cmd, args[0])
		},
	}
	return cmd
}

func runPasswordPoliciesForUser(cmd *cobra.Command, identifier string) error {
	v1, err := newV1Client()
	if err != nil {
		return err
	}
	userID, err := resolveUser(cmd.Context(), v1, identifier)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}
	raw, err := client.Get(cmd.Context(), pwpolicy.UserEndpoint(userID))
	if err != nil {
		return err
	}

	return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
}

func newPasswordPoliciesForGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "for-group <group-name-or-id>",
		Aliases:           []string{"usergroup"},
		Short:             "Show the password policy currently governing a user group",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserGroupConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPasswordPoliciesForGroup(cmd, args[0])
		},
	}
	return cmd
}

func runPasswordPoliciesForGroup(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	groupID, err := resolveUserGroup(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	raw, err := client.Get(cmd.Context(), pwpolicy.UserGroupEndpoint(groupID))
	if err != nil {
		return err
	}

	return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
}

// ---------- shared requirement flags ----------

// pwPolicyFlags holds every requirement a caller may set on create or update.
// Both commands bind the same struct so the two surfaces cannot drift on flag
// names, and changedPolicyFields() turns whatever the user actually typed into
// the JSON-keyed change set that pwpolicy.ApplyChanges consumes.
type pwPolicyFlags struct {
	name        string
	description string

	minLength                           int
	needsLowercase                      bool
	needsUppercase                      bool
	needsNumeric                        bool
	needsSymbolic                       bool
	allowUsernameSubstring              bool
	disallowCommonlyUsedPasswords       bool
	disallowSequentialOrRepetitiveChars bool

	passwordExpirationInDays int
	maxHistory               int
	minChangePeriodInDays    int

	maxLoginAttempts           int
	lockoutTimeInSeconds       int
	resetLockoutCounterMinutes int

	daysBeforeExpirationToForceReset int
	daysAfterExpirationToSelfRecover int
	enableRecoveryEmail              bool
	allowUnenrolledMFAPasswordReset  bool
	displayComplexityOnResetScreen   bool

	// Explicit off-switches. Setting a value turns its requirement on; these
	// are how a caller turns one off without also having to restate its value.
	disable []string

	groups []string
}

// pwPolicyFlagFields maps each CLI flag to the JSON field it writes.
var pwPolicyFlagFields = []struct {
	flag string
	json string
}{
	{"name", "name"},
	{"description", "description"},
	{"min-length", "minLength"},
	{"needs-lowercase", "needsLowercase"},
	{"needs-uppercase", "needsUppercase"},
	{"needs-numeric", "needsNumeric"},
	{"needs-symbolic", "needsSymbolic"},
	{"allow-username-substring", "allowUsernameSubstring"},
	{"disallow-common-passwords", "disallowCommonlyUsedPasswords"},
	{"disallow-sequential-chars", "disallowSequentialOrRepetitiveChars"},
	{"expiration-days", "passwordExpirationInDays"},
	{"max-history", "maxHistory"},
	{"min-change-period-days", "minChangePeriodInDays"},
	{"max-login-attempts", "maxLoginAttempts"},
	{"lockout-seconds", "lockoutTimeInSeconds"},
	{"reset-lockout-counter-minutes", "resetLockoutCounterMinutes"},
	{"days-before-expiration-force-reset", "daysBeforeExpirationToForceReset"},
	{"days-after-expiration-self-recover", "daysAfterExpirationToSelfRecover"},
	{"enable-recovery-email", "enableRecoveryEmail"},
	{"allow-unenrolled-mfa-reset", "allowUnenrolledMFAPasswordReset"},
	{"display-complexity-on-reset", "displayComplexityOnResetScreen"},
}

// disableTargets maps the friendly names accepted by --disable to the enable*
// field each one switches off.
var disableTargets = map[string]string{
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

func addPasswordPolicyFlags(cmd *cobra.Command, f *pwPolicyFlags) {
	fs := cmd.Flags()
	fs.StringVar(&f.name, "name", "", "Policy name")
	fs.StringVar(&f.description, "description", "", "Policy description")

	fs.IntVar(&f.minLength, "min-length", 0, "Minimum password length")
	fs.BoolVar(&f.needsLowercase, "needs-lowercase", false, "Require a lowercase character")
	fs.BoolVar(&f.needsUppercase, "needs-uppercase", false, "Require an uppercase character")
	fs.BoolVar(&f.needsNumeric, "needs-numeric", false, "Require a digit")
	fs.BoolVar(&f.needsSymbolic, "needs-symbolic", false, "Require a symbol")
	fs.BoolVar(&f.allowUsernameSubstring, "allow-username-substring", false, "Allow the username to appear inside the password")
	fs.BoolVar(&f.disallowCommonlyUsedPasswords, "disallow-common-passwords", false, "Reject commonly used passwords")
	fs.BoolVar(&f.disallowSequentialOrRepetitiveChars, "disallow-sequential-chars", false, "Reject sequential or repeated characters")

	fs.IntVar(&f.passwordExpirationInDays, "expiration-days", 0, "Days until a password expires")
	fs.IntVar(&f.maxHistory, "max-history", 0, "Number of previous passwords that may not be reused")
	fs.IntVar(&f.minChangePeriodInDays, "min-change-period-days", 0, "Minimum days between password changes")

	fs.IntVar(&f.maxLoginAttempts, "max-login-attempts", 0, "Failed attempts before lockout")
	fs.IntVar(&f.lockoutTimeInSeconds, "lockout-seconds", 0, "Lockout duration in seconds")
	fs.IntVar(&f.resetLockoutCounterMinutes, "reset-lockout-counter-minutes", 0, "Minutes before the failed-attempt counter resets")

	fs.IntVar(&f.daysBeforeExpirationToForceReset, "days-before-expiration-force-reset", 0, "Days before expiry to force a reset")
	fs.IntVar(&f.daysAfterExpirationToSelfRecover, "days-after-expiration-self-recover", 0, "Days after expiry a user may still self-recover (-1 for no limit)")
	fs.BoolVar(&f.enableRecoveryEmail, "enable-recovery-email", false, "Allow password recovery by email")
	fs.BoolVar(&f.allowUnenrolledMFAPasswordReset, "allow-unenrolled-mfa-reset", false, "Allow password reset for users not enrolled in MFA")
	fs.BoolVar(&f.displayComplexityOnResetScreen, "display-complexity-on-reset", false, "Show the complexity requirements on the reset screen")

	fs.StringSliceVar(&f.disable, "disable", nil,
		"Turn a requirement off without restating its value (repeatable): "+disableTargetList())
	fs.StringSliceVar(&f.groups, "group", nil, "User group to bind the policy to (repeatable). Use --group '' to clear all bindings")
}

func disableTargetList() string {
	names := make([]string, 0, len(disableTargets))
	for k := range disableTargets {
		names = append(names, k)
	}
	sortStrings(names)
	return strings.Join(names, ", ")
}

// changedPolicyFields collects only the flags the user actually typed, keyed
// by JSON field name. Cobra's Changed() is what makes an unset int flag mean
// "leave alone" rather than "set to 0" — which matters here, because 0 is a
// legal value for several of these settings.
func changedPolicyFields(cmd *cobra.Command, f *pwPolicyFlags) (map[string]any, error) {
	changes := map[string]any{}

	values := map[string]any{
		"name": f.name, "description": f.description,
		"minLength": f.minLength, "needsLowercase": f.needsLowercase,
		"needsUppercase": f.needsUppercase, "needsNumeric": f.needsNumeric,
		"needsSymbolic": f.needsSymbolic, "allowUsernameSubstring": f.allowUsernameSubstring,
		"disallowCommonlyUsedPasswords":       f.disallowCommonlyUsedPasswords,
		"disallowSequentialOrRepetitiveChars": f.disallowSequentialOrRepetitiveChars,
		"passwordExpirationInDays":            f.passwordExpirationInDays,
		"maxHistory":                          f.maxHistory,
		"minChangePeriodInDays":               f.minChangePeriodInDays,
		"maxLoginAttempts":                    f.maxLoginAttempts,
		"lockoutTimeInSeconds":                f.lockoutTimeInSeconds,
		"resetLockoutCounterMinutes":          f.resetLockoutCounterMinutes,
		"daysBeforeExpirationToForceReset":    f.daysBeforeExpirationToForceReset,
		"daysAfterExpirationToSelfRecover":    f.daysAfterExpirationToSelfRecover,
		"enableRecoveryEmail":                 f.enableRecoveryEmail,
		"allowUnenrolledMFAPasswordReset":     f.allowUnenrolledMFAPasswordReset,
		"displayComplexityOnResetScreen":      f.displayComplexityOnResetScreen,
	}

	for _, ff := range pwPolicyFlagFields {
		if cmd.Flags().Changed(ff.flag) {
			changes[ff.json] = values[ff.json]
		}
	}

	for _, d := range f.disable {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		enable, ok := disableTargets[d]
		if !ok {
			return nil, fmt.Errorf("unknown --disable target %q (valid: %s)", d, disableTargetList())
		}
		changes[enable] = false
	}

	return changes, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// resolveGroupBindings maps --group values to user group IDs. A single empty
// value is the documented way to clear every binding, and returns a non-nil
// empty slice so the body carries an explicit empty array.
func resolveGroupBindings(ctx context.Context, client *api.V2Client, groups []string) ([]string, error) {
	if len(groups) == 1 && strings.TrimSpace(groups[0]) == "" {
		return []string{}, nil
	}
	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		id, err := resolveUserGroup(ctx, client, g)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ---------- create ----------

func newPasswordPoliciesCreateCmd() *cobra.Command {
	var f pwPolicyFlags

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a password policy",
		Long: `Create a password policy.

A new policy starts with every requirement off; each flag you pass switches its
requirement on. Bind the policy to user groups with --group, which may be
repeated. A policy bound to no groups governs nobody until you bind one.

Examples:
  jc password-policies create --name "Engineering" --min-length 16 \
      --needs-uppercase --needs-numeric --group Engineering

  jc password-policies create --name "Contractors" --min-length 12 \
      --expiration-days 30 --max-login-attempts 5 --plan`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPasswordPoliciesCreate(cmd, &f)
		},
	}

	addPasswordPolicyFlags(cmd, &f)
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func runPasswordPoliciesCreate(cmd *cobra.Command, f *pwPolicyFlags) error {
	changes, err := changedPolicyFields(cmd, f)
	if err != nil {
		return err
	}

	// A create starts from the zero policy rather than a fetched one, so
	// ApplyChanges still owns the value/enable pairing.
	policy, err := pwpolicy.ApplyChanges(pwpolicy.Policy{}, changes)
	if err != nil {
		return err
	}

	if viper.GetBool("plan") {
		effects := pwpolicy.Diff(pwpolicy.Policy{}, policy)
		if len(f.groups) > 0 {
			effects = append(effects, "bind to groups: "+strings.Join(f.groups, ", "))
		} else {
			effects = append(effects, "bound to no groups (governs nobody until bound)")
		}
		return renderPlan(cmd, &plan.Plan{
			Action:     "create",
			Resource:   "password policy",
			Target:     f.name,
			Effects:    effects,
			Reversible: true,
		})
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	var groupIDs []string
	if len(f.groups) > 0 {
		groupIDs, err = resolveGroupBindings(cmd.Context(), client, f.groups)
		if err != nil {
			return err
		}
	}

	raw, err := client.Create(cmd.Context(), pwpolicy.Endpoint, pwpolicy.Body(policy, groupIDs))
	if err != nil {
		return err
	}

	return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
}

// ---------- update ----------

func newPasswordPoliciesUpdateCmd() *cobra.Command {
	var f pwPolicyFlags

	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Update a password policy",
		Long: `Update a password policy, changing only the settings you pass.

The current policy is read first and sent back complete with your changes
applied, so untouched requirements are preserved. Setting a value switches its
requirement on; use --disable to switch one off without restating its value.

Group bindings are left alone unless --group is given, which replaces them
outright. --group '' clears every binding.

Examples:
  jc password-policies update Engineering --min-length 16
  jc password-policies update Engineering --disable expiration
  jc password-policies update 67dc22433f87810001e2bf1c --group Eng --group Ops`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPasswordPoliciesUpdate(cmd, args[0], &f)
		},
	}

	addPasswordPolicyFlags(cmd, &f)

	return cmd
}

func runPasswordPoliciesUpdate(cmd *cobra.Command, identifier string, f *pwPolicyFlags) error {
	changes, err := changedPolicyFields(cmd, f)
	if err != nil {
		return err
	}
	if len(changes) == 0 && !cmd.Flags().Changed("group") {
		return fmt.Errorf("no fields to update. Pass at least one requirement flag, --disable, or --group")
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	id, err := resolvePasswordPolicy(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	curRaw, err := client.Get(cmd.Context(), pwpolicy.PolicyEndpoint(id))
	if err != nil {
		return err
	}
	cur, err := pwpolicy.ParseDetail(curRaw)
	if err != nil {
		return err
	}

	next := cur.Policy
	if len(changes) > 0 {
		next, err = pwpolicy.ApplyChanges(cur.Policy, changes)
		if err != nil {
			return err
		}
	}

	// Bindings are preserved unless --group was given: reads report them under
	// groups, writes take them under groupIds.
	groupIDs := cur.GroupIDs()
	groupLabel := ""
	if cmd.Flags().Changed("group") {
		groupIDs, err = resolveGroupBindings(cmd.Context(), client, f.groups)
		if err != nil {
			return err
		}
		if len(groupIDs) == 0 {
			groupLabel = "group bindings: cleared"
		} else {
			groupLabel = "group bindings: " + strings.Join(f.groups, ", ")
		}
	}

	effects := pwpolicy.Diff(cur.Policy, next)
	if groupLabel != "" {
		effects = append(effects, groupLabel)
	}
	if len(effects) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "No changes: the policy already has these values.")
		return nil
	}

	// The plan box is a fixed width, so the target stays short and the
	// org-default warning goes in the effects where it can wrap.
	target := id
	if cur.Policy.Name != "" {
		target = cur.Policy.Name
	}
	if cur.Policy.Default {
		effects = append(effects, "this is the org default policy")
	}

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:     "update",
			Resource:   "password policy",
			Target:     target,
			Effects:    effects,
			Reversible: true,
		})
	}

	// Changing the default policy re-governs every user not covered by a
	// group-bound policy, so it is worth a second look before it lands.
	if cur.Policy.Default {
		fmt.Fprintf(cmd.ErrOrStderr(), "This is the organization default password policy — it governs every user no other policy covers.\n")
		for _, e := range effects {
			fmt.Fprintf(cmd.ErrOrStderr(), "  %s\n", e)
		}
		if mustAbortWithoutTTY() {
			fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
			return nil
		}
		if shouldConfirm() {
			ok, err := askYesNo(cmd, "Update the organization default password policy?")
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
				return nil
			}
		}
	}

	raw, err := client.Update(cmd.Context(), pwpolicy.PolicyEndpoint(id), pwpolicy.Body(next, groupIDs))
	if err != nil {
		return err
	}

	return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
}

// ---------- delete ----------

func newPasswordPoliciesDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <name-or-id> [name-or-id...]",
		Aliases: []string{"rm"},
		Short:   "Delete one or more password policies",
		Long: `Delete password policies.

A single argument deletes that policy on its own; two or more are removed in
one batch call. Users governed by a deleted policy fall back to whichever
policy covers them next, or to the organization default.

The organization default policy cannot be deleted.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPasswordPoliciesDelete(cmd, args)
		},
	}
	return cmd
}

func runPasswordPoliciesDelete(cmd *cobra.Command, identifiers []string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	type target struct {
		id    string
		label string
	}
	targets := make([]target, 0, len(identifiers))

	for _, identifier := range identifiers {
		id, err := resolvePasswordPolicy(cmd.Context(), client, identifier)
		if err != nil {
			return err
		}

		raw, err := client.Get(cmd.Context(), pwpolicy.PolicyEndpoint(id))
		if err != nil {
			return err
		}
		d, err := pwpolicy.ParseDetail(raw)
		if err != nil {
			return err
		}

		if d.Policy.Default {
			return fmt.Errorf("password policy %s is the organization default and cannot be deleted", id)
		}

		label := d.Policy.Name
		if label == "" {
			label = id
		} else {
			label = fmt.Sprintf("%s (%s)", label, id)
		}
		if n := len(d.Groups); n > 0 {
			label += fmt.Sprintf(", %s bound", fmt.Sprintf("%d group%s", n, plural(n)))
		}
		targets = append(targets, target{id: id, label: label})
	}

	effects := make([]string, 0, len(targets))
	ids := make([]string, 0, len(targets))
	for _, t := range targets {
		effects = append(effects, "delete "+t.label)
		ids = append(ids, t.id)
	}
	effects = append(effects, "affected users fall back to the next policy that covers them, or the org default")

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:   "delete",
			Resource: "password policy",
			Target:   strings.Join(identifiers, ", "),
			Effects:  effects,
		})
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		for _, e := range effects[:len(effects)-1] {
			fmt.Fprintf(cmd.ErrOrStderr(), "  %s\n", e)
		}
		ok, err := askYesNo(cmd, fmt.Sprintf("Delete %s?", pwPolicyCount(len(targets))))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
			return nil
		}
	}

	if len(ids) == 1 {
		if _, err := client.Delete(cmd.Context(), pwpolicy.PolicyEndpoint(ids[0])); err != nil {
			return err
		}
	} else {
		if _, err := client.DeleteWithBody(cmd.Context(), pwpolicy.Endpoint, pwpolicy.BatchDeleteBody(ids)); err != nil {
			return err
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleted %s.\n", pwPolicyCount(len(ids)))
	return nil
}

// ---------- set-precedence ----------

func newPasswordPoliciesSetPrecedenceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "set-precedence <name-or-id>=<precedence> [<name-or-id>=<precedence>...]",
		Aliases: []string{"reorder"},
		Short:   "Set the evaluation order of password policies",
		Long: `Set the precedence of one or more password policies.

When a user falls under several policies, the LOWEST precedence number wins.
Precedence is set here rather than through ` + "`update`" + `, because the API
owns the ordering as a whole.

Example:
  jc password-policies set-precedence Engineering=1 Contractors=2`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPasswordPoliciesSetPrecedence(cmd, args)
		},
	}
	return cmd
}

func runPasswordPoliciesSetPrecedence(cmd *cobra.Command, args []string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	entries := make([]pwpolicy.PrecedenceEntry, 0, len(args))
	effects := make([]string, 0, len(args))

	for _, arg := range args {
		name, value, ok := strings.Cut(arg, "=")
		if !ok {
			return fmt.Errorf("invalid argument %q: expected <name-or-id>=<precedence>", arg)
		}
		precedence, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("invalid precedence in %q: %w", arg, err)
		}

		id, err := resolvePasswordPolicy(cmd.Context(), client, strings.TrimSpace(name))
		if err != nil {
			return err
		}

		entries = append(entries, pwpolicy.PrecedenceEntry{ObjectID: id, Precedence: precedence})
		effects = append(effects, fmt.Sprintf("%s -> precedence %d", strings.TrimSpace(name), precedence))
	}

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:     "set-precedence",
			Resource:   "password policy",
			Target:     pwPolicyPlural(len(entries)),
			Effects:    effects,
			Reversible: true,
		})
	}

	if _, err := client.Update(cmd.Context(), pwpolicy.PrecedenceEndpoint, entries); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Precedence updated for %s.\n", pwPolicyCount(len(entries)))
	return nil
}

// pwPolicyPlural renders a bare policy count, spelling the irregular plural.
func pwPolicyPlural(n int) string {
	if n == 1 {
		return "1 policy"
	}
	return fmt.Sprintf("%d policies", n)
}

// pwPolicyCount renders a policy count with the irregular plural spelled out,
// so confirmation prompts read as English rather than "1 policies".
func pwPolicyCount(n int) string {
	if n == 1 {
		return "1 password policy"
	}
	return fmt.Sprintf("%d password policies", n)
}

// askYesNo prompts on stderr and reports whether the user agreed. Callers must
// have already checked shouldConfirm and mustAbortWithoutTTY.
func askYesNo(cmd *cobra.Command, question string) (bool, error) {
	fmt.Fprintf(cmd.ErrOrStderr(), "%s [y/N] ", question)
	answer, err := getConfirmReader().ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("reading confirmation: %w", err)
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes", nil
}
