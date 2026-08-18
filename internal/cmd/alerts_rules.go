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
	"github.com/klaassen-consulting/jc/internal/healthrule"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

func resolveHealthRule(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.HealthRuleConfig)
}

// newAlertsRulesCmd builds the `jc alerts rules …` subtree: the
// health-monitoring rules that raise alerts (Monitoring & Alerting).
func newAlertsRulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "rules",
		Aliases: []string{"rule"},
		Short:   "Manage health-monitoring rules (the definitions that raise alerts)",
		Long: `List, inspect, toggle, and manage JumpCloud health-monitoring rules.

Health-monitoring rules are the definitions that raise the alerts handled by
'jc alerts'. Each rule has a category, severity, and status (enabled/disabled),
and either derives from a rule template or is event-filter based.

Rules are addressed by objectId (from 'jc alerts rules list --ids'); a unique
rule name also resolves. The rule body is a large nested object, so 'create'
and 'update' take the raw JSON via --rule-file (a body from
'jc alerts rules get <id>' round-trips).`,
	}
	cmd.AddCommand(newAlertsRulesListCmd())
	cmd.AddCommand(newAlertsRulesGetCmd())
	cmd.AddCommand(newAlertsRulesStatsCmd())
	cmd.AddCommand(newAlertsRulesTemplatesCmd())
	cmd.AddCommand(newAlertsRulesStatusCmd())
	cmd.AddCommand(newAlertsRulesCreateCmd())
	cmd.AddCommand(newAlertsRulesUpdateCmd())
	cmd.AddCommand(newAlertsRulesDeleteCmd())
	return cmd
}

func newAlertsRulesListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List health-monitoring rules",
		Long: `List JumpCloud health-monitoring rules.

Default fields: objectId, name, category, severity, status, ruleType.
Filter e.g. --filter 'status=RULE_STATUS_ENABLED'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHealthRulesList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'status=RULE_STATUS_ENABLED')")
	return cmd
}

func runHealthRulesList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	result, err := client.ListAll(cmd.Context(), healthrule.RulesEndpoint, api.V2ListOptions{
		Limit:       limit,
		Sort:        sort,
		Filter:      filter.ToV2Queries(exprs),
		ResponseKey: "rules",
	})
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	opts.DefaultFields = healthrule.DefaultFields
	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}
	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}
	return nil
}

func newAlertsRulesGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "get <id-or-name>",
		Short:             "Get a health-monitoring rule by ID or name",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.HealthRuleConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			id, err := resolveHealthRule(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			result, err := client.Get(cmd.Context(), healthrule.RulesEndpoint+"/"+id)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), healthrule.Unwrap(result, "rule"), output.CurrentOptions())
		},
	}
	return cmd
}

func newAlertsRulesStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show health-monitoring rule statistics",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			result, err := client.Get(cmd.Context(), healthrule.StatsEndpoint)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), result, output.CurrentOptions())
		},
	}
}

func newAlertsRulesTemplatesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "templates",
		Aliases: []string{"template", "tmpl"},
		Short:   "Browse read-only rule templates",
		Long:    "List and inspect JumpCloud health-monitoring rule templates. Templates are read-only definitions that template-based rules derive from.",
	}
	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List rule templates",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			result, err := client.ListAll(cmd.Context(), healthrule.TemplatesEndpoint, api.V2ListOptions{
				ResponseKey: "templates",
			})
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = healthrule.TemplateDefaultFields
			if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:               "get <id-or-name>",
		Short:             "Get a rule template by ID or name",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.HealthRuleTemplateConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			r := resolve.NewV2Resolver(client)
			id, err := r.Resolve(cmd.Context(), args[0], resolve.HealthRuleTemplateConfig)
			if err != nil {
				return err
			}
			result, err := client.Get(cmd.Context(), healthrule.TemplatesEndpoint+"/"+id)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), healthrule.Unwrap(result, "template"), output.CurrentOptions())
		},
	})
	return cmd
}

func newAlertsRulesStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "status <id-or-name> <enabled|disabled>",
		Short:             "Enable or disable a health-monitoring rule",
		Long:              "Enable or disable a health-monitoring rule. A disabled rule stops raising new alerts.",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeResourceNames(resolve.HealthRuleConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHealthRulesStatus(cmd, args[0], args[1])
		},
	}
	return cmd
}

func runHealthRulesStatus(cmd *cobra.Command, identifier, status string) error {
	apiStatus, err := healthrule.NormalizeStatus(status)
	if err != nil {
		return err
	}
	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:     "set status",
			Resource:   "health-monitoring rule",
			Target:     identifier,
			Effects:    []string{"status → " + strings.ToLower(status)},
			Reversible: true,
		})
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveHealthRule(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	result, err := client.Patch(cmd.Context(), healthrule.RulesEndpoint+"/"+id+"/status", healthrule.StatusBody(apiStatus))
	if err != nil {
		return err
	}
	return output.WriteSingle(cmd.OutOrStdout(), healthrule.Unwrap(result, "rule"), output.CurrentOptions())
}

func newAlertsRulesCreateCmd() *cobra.Command {
	var ruleFile string
	cmd := &cobra.Command{
		Use:   "create --rule-file <file>",
		Short: "Create a health-monitoring rule from a JSON file",
		Long: `Create a JumpCloud health-monitoring rule.

The rule body is a large nested object (conditions, filters, notification
channels, …), so it is supplied as raw JSON via --rule-file rather than flags.
The file may be a bare rule object or one wrapped in {"rule": …}, so a body
captured from 'jc alerts rules get <id>' round-trips. Use - to read stdin.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHealthRulesCreate(cmd, ruleFile)
		},
	}
	cmd.Flags().StringVar(&ruleFile, "rule-file", "", `Path to a JSON file with the rule object (or - for stdin)`)
	_ = cmd.MarkFlagRequired("rule-file")
	return cmd
}

func runHealthRulesCreate(cmd *cobra.Command, ruleFile string) error {
	rule, err := readRuleFile(cmd, ruleFile)
	if err != nil {
		return err
	}
	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:   "create",
			Resource: "health-monitoring rule",
			Target:   ruleDisplayName(rule),
			Effects:  []string{"Creates a new health-monitoring rule from " + sourceLabel(ruleFile)},
		})
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	result, err := client.Create(cmd.Context(), healthrule.RulesEndpoint, healthrule.RuleBody(rule))
	if err != nil {
		return err
	}
	return output.WriteSingle(cmd.OutOrStdout(), healthrule.Unwrap(result, "rule"), output.CurrentOptions())
}

func newAlertsRulesUpdateCmd() *cobra.Command {
	var ruleFile string
	cmd := &cobra.Command{
		Use:               "update <id-or-name> --rule-file <file>",
		Short:             "Update a health-monitoring rule from a JSON file",
		Long:              "Update a JumpCloud health-monitoring rule. The full rule object is supplied as raw JSON via --rule-file; a body from 'jc alerts rules get <id>' round-trips. Use - for stdin.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.HealthRuleConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHealthRulesUpdate(cmd, args[0], ruleFile)
		},
	}
	cmd.Flags().StringVar(&ruleFile, "rule-file", "", `Path to a JSON file with the rule object (or - for stdin)`)
	_ = cmd.MarkFlagRequired("rule-file")
	return cmd
}

func runHealthRulesUpdate(cmd *cobra.Command, identifier, ruleFile string) error {
	rule, err := readRuleFile(cmd, ruleFile)
	if err != nil {
		return err
	}
	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:     "update",
			Resource:   "health-monitoring rule",
			Target:     identifier,
			Effects:    []string{"Replaces the rule definition from " + sourceLabel(ruleFile)},
			Reversible: true,
		})
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveHealthRule(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	result, err := client.Patch(cmd.Context(), healthrule.RulesEndpoint+"/"+id, healthrule.RuleBody(rule))
	if err != nil {
		return err
	}
	return output.WriteSingle(cmd.OutOrStdout(), healthrule.Unwrap(result, "rule"), output.CurrentOptions())
}

// readRuleFile reads a --rule-file path (or "-" for stdin) and returns the
// rule object to wrap in the {rule} envelope.
func readRuleFile(cmd *cobra.Command, ruleFile string) (json.RawMessage, error) {
	if ruleFile == "" {
		return nil, fmt.Errorf("--rule-file is required")
	}
	var raw []byte
	var err error
	if ruleFile == "-" {
		raw, err = io.ReadAll(cmd.InOrStdin())
	} else {
		raw, err = os.ReadFile(ruleFile)
	}
	if err != nil {
		return nil, fmt.Errorf("reading --rule-file: %w", err)
	}
	return healthrule.ParseRuleFile(raw)
}

func sourceLabel(ruleFile string) string {
	if ruleFile == "-" {
		return "stdin"
	}
	return ruleFile
}

// ruleDisplayName extracts a rule's name for plan output, falling back to a
// generic label.
func ruleDisplayName(rule json.RawMessage) string {
	var r struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(rule, &r) == nil && r.Name != "" {
		return r.Name
	}
	return "(unnamed rule)"
}

func newAlertsRulesDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "delete <id-or-name>",
		Aliases:           []string{"rm"},
		Short:             "Delete a health-monitoring rule",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.HealthRuleConfig),
		RunE:              batchRunE("health-monitoring rule", "delete", runHealthRulesDelete),
	}
	addBatchSourceFlags(cmd)
	return cmd
}

func runHealthRulesDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveHealthRule(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	// Read the name back for the confirmation/success message.
	name := identifier
	if raw, err := client.Get(cmd.Context(), healthrule.RulesEndpoint+"/"+id); err == nil {
		var r struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(healthrule.Unwrap(raw, "rule"), &r) == nil && r.Name != "" {
			name = r.Name
		}
	}

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:   "delete",
			Resource: "health-monitoring rule",
			Target:   fmt.Sprintf("%s (%s)", name, id),
			Effects:  []string{"Removes the rule; existing alerts it raised are unaffected"},
		})
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete health-monitoring rule %q? [y/N] ", name)
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
	if _, err := client.Delete(cmd.Context(), healthrule.RulesEndpoint+"/"+id); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Health-monitoring rule %q deleted successfully.\n", name)
	return nil
}
