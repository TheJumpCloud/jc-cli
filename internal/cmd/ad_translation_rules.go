package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
	"github.com/klaassen-consulting/jc/internal/transrule"
)

// newADTranslationRulesCmd builds the `jc ad translation-rules` subtree, which
// manages the attribute mappings that translate JumpCloud users to and from an
// Active Directory instance.
func newADTranslationRulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "translation-rules",
		Aliases: []string{"translation-rule", "rules"},
		Short:   "Manage Active Directory attribute translation rules",
		Long: `Manage the attribute translation rules of a JumpCloud Active Directory integration.

A translation rule maps a JumpCloud user field (source) to an Active Directory
attribute (destination). The source is interpreted according to its type:

  path      A JumpCloud user field path (e.g. firstname)
  expr      An expression (e.g. jcUser.username + '@' + domain.domainName)
  constant  A literal value
  template  A Go template

Rules marked editable=false are JumpCloud defaults and cannot be changed.
Destinations that are not native AD attributes are stored under
customAttributes.* by the server.`,
	}

	cmd.AddCommand(newADTranslationRulesListCmd())
	cmd.AddCommand(newADTranslationRulesRecommendationsCmd())
	cmd.AddCommand(newADTranslationRulesCreateCmd())
	cmd.AddCommand(newADTranslationRulesUpdateCmd())
	cmd.AddCommand(newADTranslationRulesDeleteCmd())
	cmd.AddCommand(newADTranslationRulesBulkCmd())
	cmd.AddCommand(newADTranslationRulesPreviewCmd())

	return cmd
}

// listTranslationRules fetches every translation rule of an AD, decoded into
// the typed wire shape.
func listTranslationRules(ctx context.Context, client *api.V2Client, adID string, limit int, sort string, filters []filter.Expression) ([]transrule.Rule, []json.RawMessage, error) {
	result, err := client.ListAll(ctx, transrule.Endpoint(adID), api.V2ListOptions{
		Limit:       limit,
		Sort:        sort,
		Filter:      filter.ToV2Queries(filters),
		ResponseKey: "rules",
	})
	if err != nil {
		return nil, nil, err
	}
	rules := make([]transrule.Rule, 0, len(result.Data))
	for _, raw := range result.Data {
		var r transrule.Rule
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, nil, fmt.Errorf("decoding translation rule: %w", err)
		}
		rules = append(rules, r)
	}
	return rules, result.Data, nil
}

func newADTranslationRulesListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list <ad-domain-or-id>",
		Aliases: []string{"ls"},
		Short:   "List the translation rules of an Active Directory integration",
		Long: `List the attribute translation rules of a JumpCloud Active Directory integration.

Default fields: objectId, source, destination, sourceType, direction, appliedOn, editable.

Filter examples:
  --filter 'direction=EXPORT'
  --filter 'destination=sAMAccountName'`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.ActiveDirectoryConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runADTranslationRulesList(cmd, args[0], limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'direction=EXPORT')")

	return cmd
}

func runADTranslationRulesList(cmd *cobra.Command, identifier string, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	adID, err := resolveAD(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	_, raw, err := listTranslationRules(cmd.Context(), client, adID, limit, sort, exprs)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = transrule.DefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), raw, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(raw))
	}

	return nil
}

func newADTranslationRulesRecommendationsCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "recommendations",
		Aliases: []string{"recommended", "catalog"},
		Short:   "List JumpCloud's recommended translation rules",
		Long: `List the catalog of translation rules JumpCloud recommends for Active Directory.

These are suggestions, not rules of a specific integration: they carry no
objectId and are not attached to any Active Directory until created. Use them
as a starting point for 'jc ad translation-rules create'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runADTranslationRulesRecommendations(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'direction=EXPORT')")

	return cmd
}

func runADTranslationRulesRecommendations(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), transrule.RecommendationEndpoint, api.V2ListOptions{
		Limit:       limit,
		Sort:        sort,
		Filter:      filter.ToV2Queries(exprs),
		ResponseKey: "rules",
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = transrule.DefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newADTranslationRulesCreateCmd() *cobra.Command {
	var (
		source      string
		destination string
		sourceType  string
		direction   string
		appliedOn   []string
	)

	cmd := &cobra.Command{
		Use:   "create <ad-domain-or-id> --source <src> --destination <dst>",
		Short: "Create a translation rule",
		Long: `Create an attribute translation rule on a JumpCloud Active Directory integration.

Required: --source and --destination.

  jc ad translation-rules create corp.example.com \
    --source department --destination department

  jc ad translation-rules create corp.example.com \
    --source-type expr --source "jcUser.firstname + ' ' + jcUser.lastname" \
    --destination cn

The API returns an empty body on create; the rule list is re-read so the
created rule is printed.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.ActiveDirectoryConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runADTranslationRulesCreate(cmd, args[0], source, destination, sourceType, direction, appliedOn)
		},
	}

	cmd.Flags().StringVar(&source, "source", "", "Source field path or expression (required)")
	cmd.Flags().StringVar(&destination, "destination", "", "Destination attribute path (required)")
	cmd.Flags().StringVar(&sourceType, "source-type", "path", "Source type: path, expr, constant, or template")
	cmd.Flags().StringVar(&direction, "direction", "export", "Direction: export or import")
	cmd.Flags().StringSliceVar(&appliedOn, "applied-on", []string{"create", "update"}, "Operations the rule applies on: create, update")
	_ = cmd.MarkFlagRequired("source")
	_ = cmd.MarkFlagRequired("destination")

	return cmd
}

func runADTranslationRulesCreate(cmd *cobra.Command, identifier, source, destination, sourceType, direction string, appliedOn []string) error {
	st, err := transrule.NormalizeSourceType(sourceType)
	if err != nil {
		return err
	}
	dir, err := transrule.NormalizeDirection(direction)
	if err != nil {
		return err
	}
	ops, err := transrule.NormalizeAppliedOn(appliedOn)
	if err != nil {
		return err
	}

	body := map[string]any{
		"source":      source,
		"destination": destination,
		"sourceType":  st,
		"direction":   dir,
		"appliedOn":   ops,
	}

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:   "create",
			Resource: "AD translation rule",
			Target:   identifier,
			Effects: []string{
				fmt.Sprintf("%s (%s) → %s", source, st, destination),
				"direction: " + dir,
				"appliedOn: " + strings.Join(ops, ", "),
			},
			Reversible: true,
		})
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	adID, err := resolveAD(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	if _, err := client.Create(cmd.Context(), transrule.Endpoint(adID), body); err != nil {
		return err
	}

	// The create endpoint returns an empty body (verified live), so re-read the
	// list and print the rule that now matches the requested mapping.
	rules, _, err := listTranslationRules(cmd.Context(), client, adID, 0, "", nil)
	if err != nil {
		return err
	}
	for _, r := range rules {
		if r.Source == source && r.Destination == destination {
			return output.WriteSingle(cmd.OutOrStdout(), mustMarshal(r), output.CurrentOptions())
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Translation rule %s → %s created.\n", source, destination)
	return nil
}

func newADTranslationRulesUpdateCmd() *cobra.Command {
	var (
		source      string
		destination string
		sourceType  string
		appliedOn   []string
	)

	cmd := &cobra.Command{
		Use:   "update <ad-domain-or-id> <rule-id>",
		Short: "Update a translation rule",
		Long: `Update an attribute translation rule of a JumpCloud Active Directory integration.

Specify only the fields to change; the current rule is fetched and unchanged
fields are preserved (the API's PUT is a full replace). A rule's direction
cannot be changed after creation, and rules with editable=false are JumpCloud
defaults that the API rejects.`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.ActiveDirectoryConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runADTranslationRulesUpdate(cmd, args[0], args[1], source, destination, sourceType, appliedOn)
		},
	}

	cmd.Flags().StringVar(&source, "source", "", "New source field path or expression")
	cmd.Flags().StringVar(&destination, "destination", "", "New destination attribute path")
	cmd.Flags().StringVar(&sourceType, "source-type", "", "New source type: path, expr, constant, or template")
	cmd.Flags().StringSliceVar(&appliedOn, "applied-on", nil, "New operations the rule applies on: create, update")

	return cmd
}

func runADTranslationRulesUpdate(cmd *cobra.Command, identifier, ruleID, source, destination, sourceType string, appliedOn []string) error {
	var (
		srcPtr  *string
		dstPtr  *string
		typePtr *string
		ops     []string
	)

	if cmd.Flags().Changed("source") {
		srcPtr = &source
	}
	if cmd.Flags().Changed("destination") {
		dstPtr = &destination
	}
	if cmd.Flags().Changed("source-type") {
		st, err := transrule.NormalizeSourceType(sourceType)
		if err != nil {
			return err
		}
		typePtr = &st
	}
	if cmd.Flags().Changed("applied-on") {
		normalized, err := transrule.NormalizeAppliedOn(appliedOn)
		if err != nil {
			return err
		}
		ops = normalized
	}

	if srcPtr == nil && dstPtr == nil && typePtr == nil && ops == nil {
		return fmt.Errorf("no fields to update. Specify at least one of --source, --destination, --source-type, --applied-on")
	}

	if viper.GetBool("plan") {
		var effects []string
		if srcPtr != nil {
			effects = append(effects, "source: "+*srcPtr)
		}
		if dstPtr != nil {
			effects = append(effects, "destination: "+*dstPtr)
		}
		if typePtr != nil {
			effects = append(effects, "sourceType: "+*typePtr)
		}
		if ops != nil {
			effects = append(effects, "appliedOn: "+strings.Join(ops, ", "))
		}
		return renderPlan(cmd, &plan.Plan{
			Action:     "update",
			Resource:   "AD translation rule",
			Target:     fmt.Sprintf("%s (%s)", ruleID, identifier),
			Effects:    effects,
			Reversible: true,
		})
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	adID, err := resolveAD(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// PUT is a full replace, so merge the changed fields onto the current rule.
	rules, _, err := listTranslationRules(cmd.Context(), client, adID, 0, "", nil)
	if err != nil {
		return err
	}
	cur, ok := transrule.FindRule(rules, ruleID)
	if !ok {
		return fmt.Errorf("translation rule %q not found on Active Directory %q", ruleID, identifier)
	}

	result, err := client.Update(cmd.Context(), transrule.RuleEndpoint(adID, ruleID),
		transrule.UpdateBody(cur, srcPtr, dstPtr, typePtr, ops))
	if err != nil {
		return err
	}

	return output.WriteSingle(cmd.OutOrStdout(), result, output.CurrentOptions())
}

func newADTranslationRulesDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <ad-domain-or-id> <rule-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a translation rule",
		Long: `Delete an attribute translation rule from a JumpCloud Active Directory integration.

Shows the rule's mapping before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.ActiveDirectoryConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runADTranslationRulesDelete(cmd, args[0], args[1])
		},
	}

	return cmd
}

func runADTranslationRulesDelete(cmd *cobra.Command, identifier, ruleID string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	adID, err := resolveAD(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// There is no single-rule GET, so read the list to describe the target.
	rules, _, err := listTranslationRules(cmd.Context(), client, adID, 0, "", nil)
	if err != nil {
		return err
	}
	cur, ok := transrule.FindRule(rules, ruleID)
	if !ok {
		return fmt.Errorf("translation rule %q not found on Active Directory %q", ruleID, identifier)
	}
	mapping := fmt.Sprintf("%s → %s", cur.Source, cur.Destination)

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:   "delete",
			Resource: "AD translation rule",
			Target:   fmt.Sprintf("%s (%s)", ruleID, mapping),
			Effects:  []string{"Remove translation rule " + mapping},
		})
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete translation rule %q? [y/N] ", mapping)
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

	if _, err := client.Delete(cmd.Context(), transrule.RuleEndpoint(adID, ruleID)); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Translation rule %q deleted successfully.\n", mapping)
	return nil
}

func newADTranslationRulesBulkCmd() *cobra.Command {
	var bulkFile string

	cmd := &cobra.Command{
		Use:   "bulk <ad-domain-or-id> --file <file>",
		Short: "Apply insert/update/delete translation rules in one request",
		Long: `Apply a batch of translation-rule operations to an Active Directory in one request.

The body is raw JSON with at least one of:

  {
    "insertTranslationRules": [ {"source": "…", "destination": "…", "sourceType": "PATH",
                                 "direction": "EXPORT", "appliedOn": ["CREATE"]} ],
    "updateTranslationRules": [ {"objectId": "…", "source": "…", "destination": "…"} ],
    "deleteTranslationRuleObjectIds": [ "…" ]
  }

Use - to read the body from stdin. Unknown top-level keys are rejected.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.ActiveDirectoryConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runADTranslationRulesBulk(cmd, args[0], bulkFile)
		},
	}

	cmd.Flags().StringVar(&bulkFile, "file", "", "Path to a JSON file with the bulk operations (or - for stdin)")
	_ = cmd.MarkFlagRequired("file")

	return cmd
}

func runADTranslationRulesBulk(cmd *cobra.Command, identifier, bulkFile string) error {
	raw, err := readJSONFileArg(cmd, bulkFile, "--file")
	if err != nil {
		return err
	}

	body, ops, err := transrule.ParseBulkFile(raw)
	if err != nil {
		return err
	}

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:   "bulk",
			Resource: "AD translation rules",
			Target:   identifier,
			Effects:  []string{ops.Summary(), "source: " + jsonSourceLabel(bulkFile)},
		})
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	adID, err := resolveAD(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Create(cmd.Context(), transrule.BulkEndpoint(adID), body)
	if err != nil {
		return err
	}

	return output.WriteSingle(cmd.OutOrStdout(), result, output.CurrentOptions())
}

func newADTranslationRulesPreviewCmd() *cobra.Command {
	var (
		rulesFile string
		userFlag  string
		adFlag    string
	)

	cmd := &cobra.Command{
		Use:   "preview --rules-file <file> --user <user>",
		Short: "Preview the result of applying translation rules to a user",
		Long: `Preview what a set of translation rules produces for a real JumpCloud user.

Nothing is written: the API returns the user before translation (sourceUser)
and the translated result (destinationUser). The rules body is a JSON array of
rule objects, or a list body wrapped in {"translationRules": […]} / {"rules": […]}
so output from 'jc ad translation-rules list' round-trips. Use - for stdin.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runADTranslationRulesPreview(cmd, rulesFile, userFlag, adFlag)
		},
	}

	cmd.Flags().StringVar(&rulesFile, "rules-file", "", "Path to a JSON file with the rules to preview (or - for stdin)")
	cmd.Flags().StringVar(&userFlag, "user", "", "Username, email, or ID of the user to preview against (required)")
	cmd.Flags().StringVar(&adFlag, "ad", "", "Active Directory domain or ID to preview against")
	_ = cmd.MarkFlagRequired("rules-file")
	_ = cmd.MarkFlagRequired("user")

	return cmd
}

func runADTranslationRulesPreview(cmd *cobra.Command, rulesFile, userIdentifier, adIdentifier string) error {
	raw, err := readJSONFileArg(cmd, rulesFile, "--rules-file")
	if err != nil {
		return err
	}

	rules, err := transrule.ParsePreviewRules(raw)
	if err != nil {
		return err
	}

	v1, err := newV1Client()
	if err != nil {
		return err
	}

	userID, err := resolveUser(cmd.Context(), v1, userIdentifier)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	var adID string
	if adIdentifier != "" {
		adID, err = resolveAD(cmd.Context(), client, adIdentifier)
		if err != nil {
			return err
		}
	}

	result, err := client.Create(cmd.Context(), transrule.PreviewEndpoint,
		transrule.PreviewBody(rules, userID, adID))
	if err != nil {
		return err
	}

	return output.WriteSingle(cmd.OutOrStdout(), result, output.CurrentOptions())
}

// readJSONFileArg reads a JSON file flag value, accepting "-" for stdin.
func readJSONFileArg(cmd *cobra.Command, path, flagName string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("%s is required", flagName)
	}
	if path == "-" {
		raw, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", flagName, err)
		}
		return raw, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", flagName, err)
	}
	return raw, nil
}

// jsonSourceLabel renders a file flag value for plan output.
func jsonSourceLabel(path string) string {
	if path == "-" {
		return "stdin"
	}
	return path
}

// mustMarshal renders a value as raw JSON for output helpers; it cannot fail
// for the plain structs it is used with.
func mustMarshal(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return raw
}
