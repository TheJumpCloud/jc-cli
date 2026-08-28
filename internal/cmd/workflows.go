package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
	"github.com/klaassen-consulting/jc/internal/tui/style"
	"github.com/klaassen-consulting/jc/internal/workflow"
)

func newWorkflowsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workflows",
		Aliases: []string{"workflow", "wf"},
		Short:   "Manage JumpCloud Workflows (server-side event automation)",
		Long: `List, inspect, author, validate, create, and run JumpCloud Workflows.

A workflow is a DSL document that JumpCloud runs server-side when a Directory
Insights event fires, on a schedule, or when triggered through the API. This is
distinct from ` + "`jc recipe`" + `, which runs multi-step jc commands locally.

The OpenAPI spec describes the DSL only as "object", so authoring by hand is
guesswork and mistakes surface at run time. The intended loop is:

  jc workflows templates list                  # 12 templates, served live
  jc workflows templates init <id> > wf.json   # fillable copy
  # edit wf.json
  jc workflows validate wf.json                # locally, before anything ships
  jc workflows explain wf.json                 # what it will actually do
  jc workflows create --file wf.json --role "<role>" --plan

Workflows run as a role you choose, and can send email and call external
connectors. Both are surfaced rather than assumed — see ` + "`validate`" + ` and
` + "`explain`" + `.`,
	}

	cmd.AddCommand(newWorkflowsListCmd())
	cmd.AddCommand(newWorkflowsGetCmd())
	cmd.AddCommand(newWorkflowsCreateCmd())
	cmd.AddCommand(newWorkflowsUpdateCmd())
	cmd.AddCommand(newWorkflowsDeleteCmd())
	cmd.AddCommand(newWorkflowsTriggerCmd())
	cmd.AddCommand(newWorkflowsRunsCmd())
	cmd.AddCommand(newWorkflowsTemplatesCmd())
	cmd.AddCommand(newWorkflowsEventTypesCmd())
	cmd.AddCommand(newWorkflowsSimulateCmd())
	cmd.AddCommand(newWorkflowsHealthCmd())
	cmd.AddCommand(newWorkflowsLintCmd())
	cmd.AddCommand(newWorkflowsValidateCmd())
	cmd.AddCommand(newWorkflowsExplainCmd())

	return cmd
}

// resolveWorkflow maps a workflow name or ID to an ID. Workflow IDs are opaque
// strings rather than 24-hex, so the generic resolver's ID heuristic does not
// apply; an exact ID match against the list is what disambiguates.
func resolveWorkflow(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	raw, err := client.Get(ctx, workflow.Endpoint)
	if err != nil {
		return "", err
	}
	rows, err := workflow.ParseList(raw)
	if err != nil {
		return "", err
	}

	var byName []workflow.Workflow
	for _, r := range rows {
		w, err := workflow.ParseWorkflow(r)
		if err != nil {
			continue
		}
		if w.ID == identifier {
			return w.ID, nil
		}
		if strings.EqualFold(w.Name, identifier) {
			byName = append(byName, w)
		}
	}

	switch len(byName) {
	case 0:
		return "", fmt.Errorf("workflow %q not found", identifier)
	case 1:
		return byName[0].ID, nil
	default:
		ids := make([]string, 0, len(byName))
		for _, w := range byName {
			ids = append(ids, w.ID)
		}
		return "", fmt.Errorf("workflow name %q is ambiguous (%d share it: %s); use an ID",
			identifier, len(byName), strings.Join(ids, ", "))
	}
}

// ---------- list / get ----------

func newWorkflowsListCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List workflows",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			endpoint := workflow.Endpoint
			if limit > 0 {
				endpoint = fmt.Sprintf("%s?limit=%d", endpoint, limit)
			}
			raw, err := client.Get(cmd.Context(), endpoint)
			if err != nil {
				return err
			}
			rows, err := workflow.ParseList(raw)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = workflow.ListDefaultFields
			if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(rows))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of results to return (0 = all)")
	return cmd
}

func newWorkflowsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get a workflow by name or ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			id, err := resolveWorkflow(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), workflow.WorkflowEndpoint(id))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
}

// ---------- runs ----------

func newWorkflowsRunsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "Inspect workflow runs",
		Long: `List and inspect workflow runs.

Runs outlive the workflow they came from: a run whose workflow was deleted
still lists, carrying workflowDeletedAt. Runs are the audit trail, so this is
not scoped under a workflow.`,
	}

	var workflowFilter string
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List workflow runs",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			endpoint := workflow.RunsEndpoint
			if workflowFilter != "" {
				id, err := resolveWorkflow(cmd.Context(), client, workflowFilter)
				if err != nil {
					// The workflow may have been deleted while its runs
					// remain, so fall back to the raw value.
					id = workflowFilter
				}
				endpoint = fmt.Sprintf("%s?workflow_id=%s", endpoint, id)
			}
			raw, err := client.Get(cmd.Context(), endpoint)
			if err != nil {
				return err
			}
			rows, err := workflow.ParseList(raw)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = workflow.RunDefaultFields
			if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(rows))
			}
			return nil
		},
	}
	list.Flags().StringVar(&workflowFilter, "workflow", "", "Only runs of this workflow (name or ID)")

	var trace bool
	get := &cobra.Command{
		Use:   "get <run-id>",
		Short: "Get a workflow run by ID",
		Long: `Get one workflow run.

A completed run carries a full per-step execution trace: the method, URL and
status each step called, AND the complete response body. Intermediate state is
therefore directly inspectable — it does not have to be exfiltrated through an
email step to be seen.

A failing step halts the run: everything after it is reported as not executed.
A switch node also records which cases were evaluated and which branch was
chosen. Nodes carry an is_output_truncated flag; the size at which the ENGINE
truncates a step's body is undocumented and is a different ceiling from the MCP
server's own result limit, so a very large step response may not be faithful
even when the trace itself returns fine.

Use --trace for a readable summary instead of the full document.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), workflow.RunEndpoint(args[0]))
			if err != nil {
				return err
			}
			if !trace {
				return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
			}
			run, err := workflow.ParseRun(raw)
			if err != nil {
				return err
			}
			return writeRunTrace(cmd, run)
		},
	}
	get.Flags().BoolVar(&trace, "trace", false, "Show the per-step execution trace instead of the full run")

	cmd.AddCommand(list, get)
	return cmd
}

// ---------- templates ----------

func newWorkflowsTemplatesCmd() *cobra.Command {
	var correctedOnly, withCorrected bool

	cmd := &cobra.Command{
		Use:     "templates",
		Aliases: []string{"template"},
		Short:   "Browse the workflow template catalog",
		Long: `Browse the workflow templates JumpCloud serves.

The catalog is the practical starting point for authoring: the DSL has no
published schema, so a working template is the most reliable specification
available. Templates ship with REPLACE_WITH_* markers for the values only you
can supply.`,
	}

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List workflow templates",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Re-encode without the DSL: a full catalog is ~40KB of nested
			// documents, which is not a list view.
			rows := make([]json.RawMessage, 0, 16)

			if correctedOnly || withCorrected {
				for _, ct := range workflow.CorrectedTemplates() {
					b, err := json.Marshal(map[string]any{
						"id": ct.ID, "name": ct.Name, "category": ct.Category,
						"description": ct.Description, "source": "jc",
						"corrects": ct.Corrects, "changes": ct.Changes,
					})
					if err != nil {
						return err
					}
					rows = append(rows, b)
				}
			}

			if !correctedOnly {
				client, err := newV2Client()
				if err != nil {
					return err
				}
				raw, err := client.Get(cmd.Context(), workflow.TemplatesEndpoint)
				if err != nil {
					return err
				}
				templates, err := workflow.ParseTemplates(raw)
				if err != nil {
					return err
				}
				for _, t := range templates {
					row := map[string]any{
						"id": t.ID, "name": t.Name, "category": t.Category,
						"description": t.Description, "source": "jumpcloud",
					}
					// Say so on the original, not only on the replacement:
					// this list is where someone picks what to copy.
					if ct, ok := workflow.CorrectionFor(t.Name); ok {
						row["corrected_by"] = ct.ID
					}
					b, err := json.Marshal(row)
					if err != nil {
						return err
					}
					rows = append(rows, b)
				}
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = workflow.TemplateDefaultFields
			if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(rows))
			}
			return nil
		},
	}

	show := &cobra.Command{
		Use:   "show <template-id-or-name>",
		Short: "Show one workflow template in full, including its DSL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			t, err := findTemplate(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			raw, err := json.Marshal(t)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}

	var initName, initDescription string
	var initSet []string
	initCmd := &cobra.Command{
		Use:   "init <template-id-or-name>",
		Short: "Emit a fillable workflow file from a template",
		Long: `Emit a template as a workflow file ready to create.

Templates ship with REPLACE_WITH_* markers for the values only you can supply.
Fill them with --set, by name rather than by ID:

  jc workflows templates init <id> \
      --set COMMAND_ID="Restart Printer Spooler" \
      --set DEVICE_GROUP_ID="Staging Devices" > wf.json

Names resolve the same way --role does; a 24-character ID passes through
untouched. With no --set the markers are left in place and listed on stderr
with what each one expects, so stdout stays pipeable either way.

` + "`jc workflows validate`" + ` refuses to create while any marker remains.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			t, err := findTemplate(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}

			d, err := workflow.ParseDSL(t.DSL)
			if err != nil {
				return err
			}

			dsl := t.DSL
			if len(initSet) > 0 {
				values, err := parseSetFlags(initSet)
				if err != nil {
					return err
				}
				// Resolve before filling, so a bad name fails with the marker
				// named rather than producing a workflow with a name where an
				// ID belongs.
				for marker, raw := range values {
					resolved, err := resolvePlaceholderValue(cmd.Context(), client, marker, raw)
					if err != nil {
						return err
					}
					values[marker] = resolved
				}
				dsl, err = d.Fill(values)
				if err != nil {
					return err
				}
			}

			name := initName
			if name == "" {
				name = t.Name
			}
			description := initDescription
			if description == "" {
				description = t.Description
			}
			doc := map[string]any{
				"name":        name,
				"description": description,
				"status":      workflow.StatusInactive,
				"dsl":         dsl,
			}
			out, err := json.MarshalIndent(doc, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(out))

			// Report what still needs filling to stderr, so stdout stays
			// pipeable. Naming each marker's kind is what makes this usable
			// without going and reading the template.
			filled := mustParseDSL(dsl)
			if markers := filled.PlaceholderMarkers(); len(markers) > 0 {
				kinds := filled.PlaceholderKinds()
				fmt.Fprintf(cmd.ErrOrStderr(), "\n%d placeholder(s) still to fill:\n", len(markers))
				for _, m := range markers {
					k := kinds[m]
					hint := ""
					if k.Resolvable {
						hint = "  (--set accepts a name)"
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "  %-44s %s%s\n",
						strings.TrimPrefix(m, "REPLACE_WITH_"), k.Describe, hint)
				}
			}

			// Validate what was just emitted. A template is the only worked
			// example most operators will see, so handing one over without
			// saying it has problems teaches the problems. Findings go to
			// stderr and do not fail the command: the file is still the right
			// starting point, and the markers left in it produce findings of
			// their own that would otherwise drown the real ones.
			reportTemplateFindings(cmd, t.Name, workflow.Validate(filled))
			return nil
		},
	}
	initCmd.Flags().StringVar(&initName, "name", "", "Override the workflow name (defaults to the template's)")
	initCmd.Flags().StringVar(&initDescription, "description", "", "Override the description")
	initCmd.Flags().StringArrayVar(&initSet, "set", nil,
		"Fill a placeholder: MARKER=VALUE, repeatable. Resolvable markers accept a name")

	list.Flags().BoolVar(&correctedOnly, "corrected", false,
		"List only jc's corrected copies of the templates that ship with a defect")
	list.Flags().BoolVar(&withCorrected, "with-corrected", false,
		"List the corrected copies alongside JumpCloud's catalog")

	cmd.AddCommand(list, show, initCmd)
	return cmd
}

// reportTemplateFindings prints what is wrong with a template that was just
// emitted, ignoring the findings that only exist because markers are still in
// place. Those are expected at this point and would bury the ones that are
// not — a template's own defects, which the operator is about to inherit.
func reportTemplateFindings(cmd *cobra.Command, name string, res workflow.Result) {
	real := workflow.WithoutPlaceholderFindings(res).Findings
	if len(real) == 0 {
		return
	}

	w := cmd.ErrOrStderr()
	fmt.Fprintf(w, "\n%d finding(s) in the template itself, which you are about to inherit:\n", len(real))
	for _, f := range real {
		fmt.Fprintf(w, "  %s\n", f.String())
	}
	if ct, ok := workflow.CorrectionFor(name); ok {
		fmt.Fprintf(w, "\nA corrected copy of this template is available:\n  jc workflows templates init %s\n  (%s)\n",
			ct.ID, ct.Changes)
		return
	}
	fmt.Fprintf(w, "\nRun: jc workflows validate --template %q\n", name)
}

// findTemplate resolves a template by ID or name against the served catalog.
func findTemplate(ctx context.Context, client *api.V2Client, identifier string) (workflow.Template, error) {
	// jc's corrected copies resolve first, and only by their "jc:" ID. A
	// corrected template shares its NAME with the JumpCloud original, so
	// matching on name here would silently swap one for the other — the
	// operator must ask for the correction explicitly.
	if workflow.IsCorrectedID(identifier) {
		ct, ok := workflow.FindCorrected(identifier)
		if !ok {
			return workflow.Template{}, fmt.Errorf("no corrected template %q (try: jc workflows templates list --corrected)", identifier)
		}
		return workflow.Template{ID: ct.ID, Name: ct.Name, Description: ct.Description,
			Category: ct.Category, DSL: ct.DSL}, nil
	}

	raw, err := client.Get(ctx, workflow.TemplatesEndpoint)
	if err != nil {
		return workflow.Template{}, err
	}
	templates, err := workflow.ParseTemplates(raw)
	if err != nil {
		return workflow.Template{}, err
	}
	for _, t := range templates {
		if t.ID == identifier {
			return t, nil
		}
	}
	for _, t := range templates {
		if strings.EqualFold(t.Name, identifier) {
			return t, nil
		}
	}
	return workflow.Template{}, fmt.Errorf("workflow template %q not found (try: jc workflows templates list)", identifier)
}

// resolvePlaceholderValue turns what the operator typed into the ID the DSL
// needs. A 24-hex ID passes straight through, a name is looked up, and free
// text is taken literally — the same contract --role already has.
func resolvePlaceholderValue(ctx context.Context, client *api.V2Client, marker, value string) (string, error) {
	kind := workflow.ClassifyPlaceholder(marker)
	if !kind.Resolvable || resolve.IsID(value) {
		return value, nil
	}

	// Workflows are not resolvable through the generic resolver: their IDs are
	// not 24-hex and the list is keyed differently.
	if kind.Kind == workflow.KindWorkflow {
		return resolveWorkflow(ctx, client, value)
	}

	pr, ok := resolve.WorkflowPlaceholderConfigs[kind.Kind]
	if !ok {
		return value, nil
	}

	var (
		id  string
		err error
	)
	if pr.V1 {
		v1, cerr := newV1Client()
		if cerr != nil {
			return "", cerr
		}
		id, err = resolve.NewResolver(v1).Resolve(ctx, value, pr.Config)
	} else {
		id, err = resolve.NewV2Resolver(client).Resolve(ctx, value, pr.Config)
	}
	if err != nil {
		// Deliberately %v rather than %w: the CLI error renderer unwraps to
		// the typed resolve.ResolveError and shows only its message, which
		// would drop the marker — the one detail that says WHICH --set is
		// wrong when several are in play.
		return "", fmt.Errorf("--set %s: could not resolve %s %q (%v)",
			strings.TrimPrefix(marker, "REPLACE_WITH_"), kind.Describe, value, err)
	}
	return id, nil
}

// parseSetFlags turns repeated --set MARKER=VALUE into a map, accepting either
// the bare marker name or its full REPLACE_WITH_ form.
func parseSetFlags(pairs []string) (map[string]string, error) {
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		name, value, ok := strings.Cut(p, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --set %q: expected MARKER=VALUE", p)
		}
		marker := workflow.NormalizeMarker(name)
		if marker == "" {
			return nil, fmt.Errorf("invalid --set %q: empty marker", p)
		}
		out[marker] = value
	}
	return out, nil
}

func mustParseDSL(raw json.RawMessage) workflow.DSL {
	d, _ := workflow.ParseDSL(raw)
	return d
}

// ---------- validate / explain ----------

// workflowDoc is the file format create, update, validate, and explain share:
// the workflow's metadata plus its DSL, which is what `templates init` emits.
type workflowDoc struct {
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Status      string          `json:"status,omitempty"`
	DSL         json.RawMessage `json:"dsl,omitempty"`
}

// loadWorkflowDoc reads a workflow file, accepting either the full document
// {name, dsl, …} or a bare DSL, so a hand-written DSL fragment still works.
func loadWorkflowDoc(cmd *cobra.Command, path string) (workflowDoc, error) {
	raw, err := readJSONFileArg(cmd, path, "--file")
	if err != nil {
		return workflowDoc{}, err
	}
	var doc workflowDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return workflowDoc{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if len(doc.DSL) == 0 {
		// No "dsl" key: treat the whole file as the DSL itself.
		if _, err := workflow.ParseDSL(raw); err != nil {
			return workflowDoc{}, fmt.Errorf("%s has no \"dsl\" key and is not a DSL document: %w", path, err)
		}
		doc.DSL = raw
	}
	return doc, nil
}

func newWorkflowsValidateCmd() *cobra.Command {
	var roleFlag, templateFlag string

	cmd := &cobra.Command{
		Use:   "validate [file]",
		Short: "Validate a workflow DSL file, a template, or a saved workflow",
		Long: `Check a workflow file before it reaches the API.

JumpCloud accepts a malformed DSL and fails only when the workflow runs, so
these checks are the difference between an error now and a workflow that
silently never works. Validation covers the trigger, task structure, control
flow, pagination, every Expr expression (compiled with the same engine), and
every operationId (against JumpCloud's own OpenAPI operations).

With --role, each step is also checked against that role's API scopes. The
execution role is the only thing between an unattended workflow and the API —
without this, validation confirms an operation EXISTS, not that the workflow
may call it, so a destructive operationId passes silently.

Scope findings are warnings, not errors: the spec's scope list is a lower
bound, and the API has been observed to accept a scope the spec omits.

Use "-" to read from stdin. With --template, a template from the served
catalog is validated instead of a file — worth doing before copying one,
since the templates are the only worked examples of a DSL that has no
published schema.

Exits non-zero when the workflow is invalid. To check everything on the
tenant at once, use "jc workflows lint".`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := ""
			var doc workflowDoc
			var err error

			switch {
			case templateFlag != "" && len(args) > 0:
				return fmt.Errorf("give either a file or --template, not both")
			case templateFlag != "":
				client, cerr := newV2Client()
				if cerr != nil {
					return cerr
				}
				t, terr := findTemplate(cmd.Context(), client, templateFlag)
				if terr != nil {
					return terr
				}
				source = "template " + t.Name
				doc = workflowDoc{Name: t.Name, DSL: t.DSL}
			case len(args) == 0:
				return fmt.Errorf("give a workflow file, or --template <name-or-id>")
			default:
				source = args[0]
				doc, err = loadWorkflowDoc(cmd, args[0])
			}
			if err != nil {
				return err
			}
			d, err := workflow.ParseDSL(doc.DSL)
			if err != nil {
				return err
			}

			res := workflow.Validate(d)
			if roleFlag != "" {
				name, scopes, err := lookupRoleScopes(cmd.Context(), roleFlag)
				if err != nil {
					return err
				}
				res = workflow.ValidateWithRole(d, name, scopes)
			}

			if templateFlag != "" {
				// A template is SUPPOSED to have placeholders, so counting
				// them as errors would call every template invalid — and
				// would contradict "jc workflows lint --templates", which
				// does not. Report them as work to do, not as defects.
				markers := d.PlaceholderMarkers()
				res = workflow.WithoutPlaceholderFindings(res)
				writeValidationReport(cmd, res)
				if len(markers) > 0 && output.CurrentOptions().Format != "json" {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"\n%d placeholder(s) to fill before this can be created; "+
							"\"jc workflows templates init\" lists what each expects.\n", len(markers))
				}
				if !res.OK() {
					return fmt.Errorf("template %s has validation errors", doc.Name)
				}
				return nil
			}

			writeValidationReport(cmd, res)
			if !res.OK() {
				return fmt.Errorf("%s is not a valid workflow", source)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&roleFlag, "role", "",
		"Also check each step against this role's API scopes (name or ID)")
	cmd.Flags().StringVar(&templateFlag, "template", "",
		"Validate a template from the served catalog instead of a file (name or ID)")

	return cmd
}

// lookupRoleScopes resolves a role and returns its name and scope list.
func lookupRoleScopes(ctx context.Context, identifier string) (string, []string, error) {
	client, err := newV2Client()
	if err != nil {
		return "", nil, err
	}
	id, err := resolve.NewV2Resolver(client).Resolve(ctx, identifier, resolve.RoleConfig)
	if err != nil {
		return "", nil, err
	}
	raw, err := client.Get(ctx, "/roles/"+id)
	if err != nil {
		return "", nil, fmt.Errorf("reading role %s: %w", identifier, err)
	}
	var role struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &role); err != nil {
		return "", nil, fmt.Errorf("decoding role: %w", err)
	}
	if role.Name == "" {
		role.Name = identifier
	}
	return role.Name, role.Scopes, nil
}

// writeValidationReport prints findings and the side-effect summary. Machine
// output gets the whole Result; humans get a readable report.
func writeValidationReport(cmd *cobra.Command, res workflow.Result) {
	opts := output.CurrentOptions()
	if opts.Format == "json" {
		raw, err := json.MarshalIndent(res, "", "  ")
		if err == nil {
			fmt.Fprintln(cmd.OutOrStdout(), string(raw))
			return
		}
	}

	w := cmd.ErrOrStderr()
	for _, f := range res.Findings {
		fmt.Fprintf(w, "%s\n", f.String())
	}
	if len(res.SideEffects) > 0 {
		fmt.Fprintf(w, "\nSide effects (%d) — these reach outside JumpCloud:\n", len(res.SideEffects))
		for _, se := range res.SideEffects {
			fmt.Fprintf(w, "  %s: %s\n", se.Task, se.What)
			for _, tg := range se.Targets {
				fmt.Fprintf(w, "      → %s\n", tg)
			}
		}
	}
	if res.OK() {
		msg := "Workflow is valid"
		if res.TriggerType != "" {
			msg += fmt.Sprintf(" (trigger: %s)", res.TriggerType)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s.\n", msg)
	}
}

func newWorkflowsExplainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain <file-or-workflow>",
		Short: "Explain in plain English what a workflow does",
		Long: `Render a workflow as prose: when it fires, what each step calls, and
which steps reach outside JumpCloud.

Each jc_operation step is resolved through JumpCloud's own OpenAPI operations,
so a reader sees "POST /api/runCommand — Run a command" rather than an opaque
operationId. Accepts a local file, or the name or ID of an existing workflow.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dsl, title, err := loadDSLFromFileOrAPI(cmd, args[0])
			if err != nil {
				return err
			}
			return explainWorkflow(cmd, title, dsl)
		},
	}
	return cmd
}

// loadDSLFromFileOrAPI accepts a path or an existing workflow's name/ID, so
// `explain` works both while authoring and when reviewing what is deployed.
func loadDSLFromFileOrAPI(cmd *cobra.Command, arg string) (json.RawMessage, string, error) {
	if doc, err := loadWorkflowDoc(cmd, arg); err == nil {
		title := doc.Name
		if title == "" {
			title = arg
		}
		return doc.DSL, title, nil
	}

	client, err := newV2Client()
	if err != nil {
		return nil, "", err
	}
	id, err := resolveWorkflow(cmd.Context(), client, arg)
	if err != nil {
		return nil, "", fmt.Errorf("%q is neither a readable file nor a known workflow: %w", arg, err)
	}
	raw, err := client.Get(cmd.Context(), workflow.WorkflowEndpoint(id))
	if err != nil {
		return nil, "", err
	}
	w, err := workflow.ParseWorkflow(raw)
	if err != nil {
		return nil, "", err
	}
	title := w.Name
	if title == "" {
		title = w.ID
	}
	return w.DSL, title, nil
}

func explainWorkflow(cmd *cobra.Command, title string, raw json.RawMessage) error {
	d, err := workflow.ParseDSL(raw)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s\n\n", title)

	trigger, err := d.Trigger()
	if err != nil {
		fmt.Fprintf(out, "Trigger: (invalid) %v\n", err)
	} else {
		fmt.Fprintf(out, "Trigger: %s\n", describeTrigger(trigger))
		if trigger.Condition != "" {
			fmt.Fprintf(out, "  only when: %s\n", trigger.Condition)
		}
	}

	fmt.Fprintf(out, "\nSteps:\n")
	for _, t := range d.Tasks() {
		indent := "  "
		if t.Depth > 0 {
			indent = "      "
		}
		fmt.Fprintf(out, "%s%s\n", indent, t.Describe())
		if cond, ok := t.Body["if"].(string); ok {
			fmt.Fprintf(out, "%s    if: %s\n", indent, cond)
		}
	}

	res := workflow.Validate(d)
	if len(res.SideEffects) > 0 {
		fmt.Fprintf(out, "\nReaches outside JumpCloud:\n")
		for _, se := range res.SideEffects {
			fmt.Fprintf(out, "  %s — %s\n", se.Task, se.What)
			for _, tg := range se.Targets {
				fmt.Fprintf(out, "      → %s\n", tg)
			}
		}
	}
	if !res.OK() {
		fmt.Fprintf(cmd.ErrOrStderr(), "\n%d validation problem(s); run `jc workflows validate` for detail.\n",
			len(res.Errors()))
	}
	return nil
}

func describeTrigger(t workflow.TriggerStyle) string {
	switch {
	case t.Frequency != "":
		return fmt.Sprintf("runs %s on a schedule", t.Frequency)
	case t.Source == workflow.TriggerEvents:
		return fmt.Sprintf("on the Directory Insights event %q", t.EventType)
	case t.Source == workflow.TriggerExternal:
		return "manual — started through the API (jc workflows trigger)"
	}
	return "(none)"
}

// ---------- create / update / delete / trigger ----------

// adminRoleHint marks roles that grant broad privilege. A workflow runs
// unattended with its role's permissions, so binding one of these is a
// standing grant worth naming out loud rather than burying in a field.
func adminRoleHint(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "administrator") || strings.Contains(n, "super admin")
}

// resolveExecutionRole maps a role name or ID to an ID, and reports the name
// back so the plan and the confirmation can say which role was chosen.
func resolveExecutionRole(ctx context.Context, client *api.V2Client, identifier string) (id, name string, err error) {
	r := resolve.NewV2Resolver(client)
	id, err = r.Resolve(ctx, identifier, resolve.RoleConfig)
	if err != nil {
		return "", "", err
	}
	name = identifier
	if raw, gerr := client.Get(ctx, "/roles/"+id); gerr == nil {
		var role struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &role) == nil && role.Name != "" {
			name = role.Name
		}
	}
	return id, name, nil
}

// checkSideEffects blocks a create or update whose DSL reaches outside
// JumpCloud unless the caller opted in explicitly.
func checkSideEffects(cmd *cobra.Command, res workflow.Result, allow bool) error {
	if len(res.SideEffects) == 0 || allow {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "this workflow reaches outside JumpCloud (%d step%s):", len(res.SideEffects), plural(len(res.SideEffects)))
	for _, se := range res.SideEffects {
		fmt.Fprintf(&b, "\n  %s — %s", se.Task, se.What)
		for _, tg := range se.Targets {
			fmt.Fprintf(&b, "\n      → %s", tg)
		}
	}
	b.WriteString("\n\nRe-run with --allow-side-effects to create it anyway.")
	return fmt.Errorf("%s", b.String())
}

func newWorkflowsCreateCmd() *cobra.Command {
	var (
		file, role, name, description, status string
		allowSideEffects                      bool
	)

	cmd := &cobra.Command{
		Use:   "create --file <file> --role <name-or-id>",
		Short: "Create a workflow from a file",
		Long: `Create a workflow from a file produced by ` + "`jc workflows templates init`" + `
or written by hand.

The DSL is validated locally first, so a malformed workflow fails here with a
JSON path rather than at run time with an opaque message.

--role sets the execution role, which decides what JumpCloud API operations the
workflow may call. Choose the least privilege the workflow needs: it runs
unattended.

The API enforces two things at create time, both worth knowing before you pick:

  * the role must cover every operation the DSL calls — a workflow that runs a
    command needs a role with command scopes, and JumpCloud names the missing
    scopes in the error;
  * your own API key must be permitted to ASSIGN that role, which is a separate
    check and can fail even when the role itself would be correct.

New workflows are created inactive unless --status active is given.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := loadWorkflowDoc(cmd, file)
			if err != nil {
				return err
			}
			if name != "" {
				doc.Name = name
			}
			if description != "" {
				doc.Description = description
			}
			if doc.Name == "" {
				return fmt.Errorf("workflow needs a name: set it in the file or pass --name")
			}

			// The flag overrides the file; the file is used when the flag is
			// absent. Assigning unconditionally silently discarded a status
			// set in the document, which then failed later as "workflow is
			// not active" with nothing pointing back at the cause.
			if status != "" {
				doc.Status = status
			}
			if doc.Status == "" {
				doc.Status = workflow.StatusInactive
			}
			if doc.Status != workflow.StatusActive && doc.Status != workflow.StatusInactive {
				return fmt.Errorf("--status must be active or inactive, got %q", doc.Status)
			}

			res := workflow.ValidateRaw(doc.DSL)
			if err := res.Err(); err != nil {
				return err
			}
			if err := checkSideEffects(cmd, res, allowSideEffects); err != nil {
				return err
			}

			client, err := newV2Client()
			if err != nil {
				return err
			}
			roleID, roleName, err := resolveExecutionRole(cmd.Context(), client, role)
			if err != nil {
				return err
			}

			effects := []string{
				fmt.Sprintf("trigger: %s", res.TriggerType),
				fmt.Sprintf("status: %s", doc.Status),
				fmt.Sprintf("runs as role: %s", roleName),
			}
			if adminRoleHint(roleName) {
				effects = append(effects, "WARNING: an administrator role is a broad standing grant")
			}
			for _, se := range res.SideEffects {
				effects = append(effects, fmt.Sprintf("%s: %s", se.Task, se.What))
			}

			if viper.GetBool("plan") {
				return renderPlan(cmd, &plan.Plan{
					Action: "create", Resource: "workflow", Target: doc.Name,
					Effects: effects, Reversible: true,
				})
			}

			raw, err := client.Create(cmd.Context(), workflow.Endpoint, workflow.CreateBody(workflow.Workflow{
				Name: doc.Name, Description: doc.Description, DSL: doc.DSL,
				Status: doc.Status, ExecutionRoleID: roleID,
			}))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "Workflow file (use - for stdin)")
	cmd.Flags().StringVar(&role, "role", "", "Execution role name or ID")
	cmd.Flags().StringVar(&name, "name", "", "Override the workflow name")
	cmd.Flags().StringVar(&description, "description", "", "Override the description")
	cmd.Flags().StringVar(&status, "status", "", "active or inactive (default inactive)")
	cmd.Flags().BoolVar(&allowSideEffects, "allow-side-effects", false,
		"Permit a DSL that sends email or calls external connectors")
	_ = cmd.MarkFlagRequired("file")
	_ = cmd.MarkFlagRequired("role")

	return cmd
}

func newWorkflowsUpdateCmd() *cobra.Command {
	var (
		file, role, name, description, status string
		allowSideEffects                      bool
	)

	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Update a workflow",
		Long: `Update a workflow.

The current workflow is read first and sent back complete with the changes
applied, because this API's PUT is full-replace. A new DSL is validated locally
before it is sent.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" && name == "" && description == "" && status == "" && role == "" {
				return fmt.Errorf("no changes requested: pass --file, --name, --description, --status, or --role")
			}

			client, err := newV2Client()
			if err != nil {
				return err
			}
			id, err := resolveWorkflow(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			curRaw, err := client.Get(cmd.Context(), workflow.WorkflowEndpoint(id))
			if err != nil {
				return err
			}
			cur, err := workflow.ParseWorkflow(curRaw)
			if err != nil {
				return err
			}

			next := cur
			var effects []string

			if file != "" {
				doc, err := loadWorkflowDoc(cmd, file)
				if err != nil {
					return err
				}
				next.DSL = doc.DSL
				effects = append(effects, "replace the DSL from "+file)
			}
			if name != "" {
				next.Name = name
				effects = append(effects, "name: "+cur.Name+" -> "+name)
			}
			if description != "" {
				next.Description = description
				effects = append(effects, "description updated")
			}
			if status != "" {
				if status != workflow.StatusActive && status != workflow.StatusInactive {
					return fmt.Errorf("--status must be active or inactive, got %q", status)
				}
				next.Status = status
				effects = append(effects, "status: "+cur.Status+" -> "+status)
			}
			if role != "" {
				roleID, roleName, err := resolveExecutionRole(cmd.Context(), client, role)
				if err != nil {
					return err
				}
				next.ExecutionRoleID = roleID
				effects = append(effects, "execution role: "+roleName)
				if adminRoleHint(roleName) {
					effects = append(effects, "WARNING: an administrator role is a broad standing grant")
				}
			}

			res := workflow.ValidateRaw(next.DSL)
			if err := res.Err(); err != nil {
				return err
			}
			if file != "" {
				if err := checkSideEffects(cmd, res, allowSideEffects); err != nil {
					return err
				}
			}
			for _, se := range res.SideEffects {
				effects = append(effects, fmt.Sprintf("%s: %s", se.Task, se.What))
			}

			target := cur.Name
			if target == "" {
				target = id
			}
			if viper.GetBool("plan") {
				return renderPlan(cmd, &plan.Plan{
					Action: "update", Resource: "workflow", Target: target,
					Effects: effects, Reversible: true,
				})
			}

			raw, err := client.Update(cmd.Context(), workflow.WorkflowEndpoint(id), workflow.UpdateBody(next))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "Replace the DSL from this file (use - for stdin)")
	cmd.Flags().StringVar(&role, "role", "", "Change the execution role")
	cmd.Flags().StringVar(&name, "name", "", "Rename the workflow")
	cmd.Flags().StringVar(&description, "description", "", "Change the description")
	cmd.Flags().StringVar(&status, "status", "", "active or inactive")
	cmd.Flags().BoolVar(&allowSideEffects, "allow-side-effects", false,
		"Permit a DSL that sends email or calls external connectors")

	return cmd
}

func newWorkflowsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <name-or-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a workflow",
		Long: `Delete a workflow.

Its runs are not deleted: they remain listed with workflowDeletedAt set, so the
audit trail survives.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			id, err := resolveWorkflow(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), workflow.WorkflowEndpoint(id))
			if err != nil {
				return err
			}
			w, err := workflow.ParseWorkflow(raw)
			if err != nil {
				return err
			}

			label := w.Name
			if label == "" {
				label = id
			}
			effects := []string{
				fmt.Sprintf("status was %s, trigger %s", w.Status, w.TriggerType),
				"past runs are kept and stay listed",
			}

			if viper.GetBool("plan") {
				return renderPlan(cmd, &plan.Plan{
					Action: "delete", Resource: "workflow", Target: label, Effects: effects,
				})
			}
			if mustAbortWithoutTTY() {
				fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
				return nil
			}
			if shouldConfirm() {
				ok, err := askYesNo(cmd, fmt.Sprintf("Delete workflow %q?", label))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
					return nil
				}
			}

			if _, err := client.Delete(cmd.Context(), workflow.WorkflowEndpoint(id)); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Workflow %q deleted.\n", label)
			return nil
		},
	}
}

func newWorkflowsTriggerCmd() *cobra.Command {
	var dataFlag string

	cmd := &cobra.Command{
		Use:   "trigger <name-or-id>",
		Short: "Start a manual run of an external-trigger workflow",
		Long: `Start a workflow run.

This only does anything for workflows whose trigger source is "external"; the
command refuses others rather than posting a call that silently does nothing.

The workflow then executes for real, with whatever privileges its execution
role grants — it may run commands on devices, change users, send email, or call
external systems. The resolved steps are shown before confirmation.

--data supplies the run input, which the workflow reads as ${ input.<field> }.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data := map[string]any{}
			if dataFlag != "" {
				if err := json.Unmarshal([]byte(dataFlag), &data); err != nil {
					return fmt.Errorf("invalid --data JSON: %w", err)
				}
			}

			client, err := newV2Client()
			if err != nil {
				return err
			}
			id, err := resolveWorkflow(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), workflow.WorkflowEndpoint(id))
			if err != nil {
				return err
			}
			w, err := workflow.ParseWorkflow(raw)
			if err != nil {
				return err
			}

			if w.TriggerType != "" && w.TriggerType != workflow.TriggerExternal {
				return fmt.Errorf(
					"workflow %q has a %s trigger, so a manual run does nothing; only external-trigger workflows can be started this way",
					args[0], w.TriggerType)
			}

			label := w.Name
			if label == "" {
				label = id
			}

			d, derr := workflow.ParseDSL(w.DSL)
			var effects []string
			if derr == nil {
				for _, t := range d.Tasks() {
					effects = append(effects, t.Describe())
				}
				for _, se := range workflow.Validate(d).SideEffects {
					effects = append(effects, fmt.Sprintf("REACHES OUTSIDE JUMPCLOUD — %s: %s", se.Task, se.What))
				}
			}
			if w.Status != workflow.StatusActive {
				effects = append(effects, fmt.Sprintf("NOTE: workflow status is %q", w.Status))
			}

			if viper.GetBool("plan") {
				return renderPlan(cmd, &plan.Plan{
					Action: "trigger", Resource: "workflow", Target: label, Effects: effects,
				})
			}
			if mustAbortWithoutTTY() {
				fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
				return nil
			}
			if shouldConfirm() {
				fmt.Fprintf(cmd.ErrOrStderr(), "Running %q will execute:\n", label)
				for _, e := range effects {
					fmt.Fprintf(cmd.ErrOrStderr(), "  %s\n", e)
				}
				ok, err := askYesNo(cmd, "Start this workflow run?")
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
					return nil
				}
			}

			out, err := client.Create(cmd.Context(), workflow.TriggerEndpoint(id), workflow.TriggerBody(data))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), out, output.CurrentOptions())
		},
	}

	cmd.Flags().StringVar(&dataFlag, "data", "", "Run input as a JSON object, readable in the DSL as ${ input.<field> }")
	return cmd
}

// writeRunTrace renders a run's per-step execution trace, which is where a
// failed run's cause actually shows up.
func writeRunTrace(cmd *cobra.Command, run workflow.Run) error {
	out := cmd.OutOrStdout()
	name := run.Name
	if name == "" {
		name = run.WorkflowID
	}
	fmt.Fprintf(out, "%s — %s\n", name, run.Status)
	fmt.Fprintf(out, "  run %s  started %s", run.ID, run.StartedAt)
	if run.CompletedAt != "" {
		fmt.Fprintf(out, "  completed %s", run.CompletedAt)
	}
	fmt.Fprintln(out)
	if run.WorkflowDeletedAt != "" {
		fmt.Fprintf(out, "  (the workflow was deleted %s; this run is retained)\n", run.WorkflowDeletedAt)
	}
	if run.Error != "" {
		fmt.Fprintf(out, "  error: %s\n", run.Error)
	}

	nodes := run.ExecutionDetails.Nodes
	if len(nodes) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "\nNo execution trace on this run.")
		return nil
	}
	fmt.Fprintln(out, "\nSteps:")
	for _, n := range nodes {
		fmt.Fprintf(out, "  %s\n", n.Describe())
	}
	if failed, ok := run.FailedNode(); ok {
		fmt.Fprintf(out, "\nFirst failure: %s\n", failed.Name)
	}
	return nil
}

func newWorkflowsEventTypesCmd() *cobra.Command {
	var service, search string

	cmd := &cobra.Command{
		Use:     "event-types",
		Aliases: []string{"events"},
		Short:   "List the Directory Insights event types a jc_events trigger can use",
		Long: `List the event types a jc_events workflow trigger can listen for.

This is the vocabulary for the trigger's "type" field, and nothing in the
workflows API validates it: a mistyped type saves, activates, and then silently
never fires — indistinguishable from an event that has not happened yet, with
no run to inspect because no run ever starts.

The catalog is a lower bound. A live tenant emitted 30 types this
documentation does not list, so a type missing here is not proof it is
invalid — which is why validate warns rather than rejects.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			matches := workflow.EventTypes(service, search)
			names := make([]string, 0, len(matches))
			for n := range matches {
				names = append(names, n)
			}
			sort.Strings(names)

			rows := make([]json.RawMessage, 0, len(names))
			for _, n := range names {
				e := matches[n]
				b, err := json.Marshal(map[string]any{
					"event_type": n, "service": e.Service, "describes": e.Describe,
					// What a condition on this event may reference.
					"payload_fields": workflow.EventFields(n),
				})
				if err != nil {
					return err
				}
				rows = append(rows, b)
			}

			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"event_type", "service", "describes"}
			if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				fmt.Fprintf(cmd.ErrOrStderr(), "── %d of %d event types ──\n",
					len(rows), workflow.EventTypeCount())
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&service, "service", "", "Only this Directory Insights service")
	cmd.Flags().StringVar(&search, "search", "", "Substring matched against the name and description")

	return cmd
}

func newWorkflowsSimulateCmd() *cobra.Command {
	var dataFlag, compareRun string

	cmd := &cobra.Command{
		Use:   "simulate <file>",
		Short: "Plan what a workflow would call, without running it",
		Long: `Work out which objects a workflow would touch, and with what parameters,
without creating it or running it.

Conditions are evaluated with the same Expr engine the DSL uses and ${ }
references are resolved against --data, so each step is reported with its real
parameters. Reads are reported as would-call; writes, emails and connector
calls are reported as stubbed and are never performed. Nothing is sent.

This needs no created workflow, no active status and no write-capable role, so
it is usable with read-only access.

It is a plan, NOT a prediction of engine behaviour. Branch selection,
halt-on-error and expression semantics here are this tool's reading of the DSL,
not observations of JumpCloud's runtime.

--compare-run <run-id> is how you check that reading against reality. It holds
the plan next to a real run's trace and reports, per task, where the two agree
and where they do not. The direction worth acting on is "ran-but-planned-skip":
the workflow touched something the plan said it would not.

Divergence is not automatically a bug here. A guard that reads a prior step's
response body cannot be evaluated without one, and the plan reports that as
unresolved rather than guessing — those are excluded from the divergence count.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := loadWorkflowDoc(cmd, args[0])
			if err != nil {
				return err
			}
			input := map[string]any{}
			if dataFlag != "" {
				if err := json.Unmarshal([]byte(dataFlag), &input); err != nil {
					return fmt.Errorf("invalid --data JSON: %w", err)
				}
			}

			res, err := workflow.SimulateRaw(doc.DSL, input)
			if err != nil {
				return err
			}

			if compareRun != "" {
				return compareSimToRun(cmd, res, compareRun)
			}

			opts := output.CurrentOptions()
			if opts.Format == "json" {
				raw, err := json.MarshalIndent(res, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(raw))
				return nil
			}

			out := cmd.OutOrStdout()
			for _, s := range res.Steps {
				line := fmt.Sprintf("  [%-11s] %-24s", s.Status, s.Task)
				if s.Operation != "" {
					line += " " + s.Operation
				} else if s.Call != "" {
					line += " " + s.Call
				}
				fmt.Fprintln(out, line)
				if s.Why != "" {
					fmt.Fprintf(out, "      %s\n", style.Subtitle.Render(s.Why))
				}
				for _, block := range []string{"pathParams", "queryParams", "bodyParams", "recipients"} {
					if v, ok := s.Params[block]; ok {
						b, _ := json.Marshal(v)
						fmt.Fprintf(out, "      %s %s\n", block+":", string(b))
					}
				}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "\n%s\n", res.Caveat)
			return nil
		},
	}

	cmd.Flags().StringVar(&compareRun, "compare-run", "",
		"Compare this plan against a real run's trace, by run ID")
	cmd.Flags().StringVar(&dataFlag, "data", "",
		"Input to resolve ${ input.<field> } against, as a JSON object")
	return cmd
}

func newWorkflowsHealthCmd() *cobra.Command {
	var days int

	cmd := &cobra.Command{
		Use:   "health",
		Short: "Find event-triggered workflows that should have fired and did not",
		Long: `Cross-reference each active jc_events workflow against the event stream it
listens to.

A workflow with a mistyped or never-emitted event type saves, activates, and
silently never fires. Nothing surfaces that: a workflow that can never match
and one whose event has not happened yet look identical — there is no match
counter and no last-evaluated timestamp anywhere in the product.

This can tell them apart, because it holds both halves. Directory Insights
knows how often the event actually occurred; the runs list knows how often the
workflow ran. Three outcomes:

  never-fired    the event occurred and the workflow did not run — the
                 failure with no other signal
  firing         the event occurred and the workflow ran
  unverifiable   the event did not occur, so nothing can be concluded

Fewer runs than events is NOT reported as a fault: a trigger condition
legitimately filters, and saying otherwise would make this untrustworthy.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if days <= 0 {
				return fmt.Errorf("--days must be positive")
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}

			raw, err := client.Get(cmd.Context(), workflow.Endpoint)
			if err != nil {
				return err
			}
			rows, err := workflow.ParseList(raw)
			if err != nil {
				return err
			}

			rawRuns, err := client.Get(cmd.Context(), workflow.RunsEndpoint)
			if err != nil {
				return err
			}
			runs, err := workflow.ParseRuns(rawRuns)
			if err != nil {
				return err
			}

			now := time.Now().UTC()
			since := now.AddDate(0, 0, -days)

			// Counts are cached per (event type, window start) rather than per
			// event type alone: workflows younger than the window are judged
			// over a shorter one, so they need their own count. Workflows
			// older than the window — the common case — all share a key.
			counts := map[string]int{}
			reports := make([]workflow.HealthReport, 0, len(rows))

			for _, row := range rows {
				w, err := workflow.ParseWorkflow(row)
				if err != nil {
					continue
				}

				eventType := ""
				if d, derr := workflow.ParseDSL(w.DSL); derr == nil {
					if t, terr := d.Trigger(); terr == nil {
						eventType = t.EventType
					}
				}

				start := workflow.EffectiveSince(w, since)

				events := 0
				if w.Status == workflow.StatusActive && w.TriggerType == workflow.TriggerEvents && eventType != "" {
					key := eventType + "@" + start.Format(time.RFC3339)
					n, ok := counts[key]
					if !ok {
						n, err = countEvents(cmd.Context(), eventType, start, now)
						if err != nil {
							return fmt.Errorf("counting %s events: %w", eventType, err)
						}
						counts[key] = n
					}
					events = n
				}

				reports = append(reports, workflow.AssessHealth(w, events,
					workflow.RunsWithin(runs, w.ID, start), start))
			}

			workflow.SortHealth(reports)
			return writeHealthReport(cmd, reports, days)
		},
	}

	cmd.Flags().IntVar(&days, "days", 7, "How many days of event history to compare against")
	return cmd
}

// countEvents asks Directory Insights how often one event type occurred.
func countEvents(ctx context.Context, eventType string, start, end time.Time) (int, error) {
	client, err := newInsightsClient()
	if err != nil {
		return 0, err
	}
	return client.CountEvents(ctx, api.InsightsQuery{
		Service:          "all",
		StartTime:        start.UTC().Format(time.RFC3339),
		EndTime:          end.UTC().Format(time.RFC3339),
		SearchTermFilter: map[string]any{"event_type": eventType},
	})
}

func writeHealthReport(cmd *cobra.Command, reports []workflow.HealthReport, days int) error {
	opts := output.CurrentOptions()
	if opts.Format == "json" {
		raw, err := json.MarshalIndent(reports, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(raw))
		return nil
	}

	out := cmd.OutOrStdout()
	if len(reports) == 0 {
		fmt.Fprintln(out, "No workflows.")
		return nil
	}

	broken := 0
	for _, r := range reports {
		if r.Verdict == workflow.HealthNeverFired {
			broken++
		}
		label := string(r.Verdict)
		name := r.Name
		if name == "" {
			name = r.WorkflowID
		}
		line := fmt.Sprintf("  [%-12s] %-34s", label, name)
		if r.EventType != "" {
			line += fmt.Sprintf(" %-34s events=%-5d runs=%d", r.EventType, r.Events, r.Runs)
		}
		if r.Verdict == workflow.HealthNeverFired {
			fmt.Fprintln(out, style.Error.Render(line))
		} else {
			fmt.Fprintln(out, line)
		}
		fmt.Fprintf(out, "      %s\n", style.Subtitle.Render(r.Detail))
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "\n── %d workflows over %d days; %d never fired despite their event occurring ──\n",
		len(reports), days, broken)
	return nil
}

func newWorkflowsLintCmd() *cobra.Command {
	var (
		templatesOnly bool
		includeAll    bool
		checkScopes   bool
	)

	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Validate every workflow on the tenant, and the template catalog",
		Long: `Run the full validator across everything at once.

"jc workflows validate <file>" answers "is this one right?". On a live tenant
the more useful question is "which of the things already running are wrong?",
and nobody asks it, because answering it by hand means exporting every
workflow and validating each in turn.

With --templates, the served template catalog is linted instead. That matters
more than it sounds: the DSL has no published schema, so templates are the
only worked examples, and an idiom that appears in one gets copied into real
workflows. Linting the catalog says which examples are actually safe to copy.

With --scopes, each workflow is additionally checked against ITS OWN execution
role — the role it will really run as, not one named on the command line.

This catches drift, not typos. The API does reject a workflow at CREATE time
if its role lacks a scope ("The Read Only role does not have sufficient
permissions ... requires one of: users, users.delete"), so a newly created
workflow is already consistent. What nothing rechecks is what happens
afterwards: a role edited to drop a scope leaves every workflow running under
it silently broken, and it will fail at run time instead.

Exits non-zero if anything has an error.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}

			var subjects []workflow.LintSubject
			if !templatesOnly || includeAll {
				subs, err := lintWorkflows(cmd.Context(), client, checkScopes)
				if err != nil {
					return err
				}
				subjects = append(subjects, subs...)
			}
			if templatesOnly || includeAll {
				subs, err := lintTemplates(cmd.Context(), client)
				if err != nil {
					return err
				}
				subjects = append(subjects, subs...)
			}

			summary := workflow.Summarize(subjects)
			if err := writeLintReport(cmd, summary); err != nil {
				return err
			}
			if summary.Errors > 0 {
				return fmt.Errorf("%d of %d have validation errors", summary.Errors, summary.Checked)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&templatesOnly, "templates", false,
		"Lint the served template catalog instead of the tenant's workflows")
	cmd.Flags().BoolVar(&includeAll, "all", false, "Lint both the workflows and the templates")
	cmd.Flags().BoolVar(&checkScopes, "scopes", false,
		"Also check each workflow against its own execution role's API scopes")

	return cmd
}

func lintWorkflows(ctx context.Context, client *api.V2Client, checkScopes bool) ([]workflow.LintSubject, error) {
	raw, err := client.Get(ctx, workflow.Endpoint)
	if err != nil {
		return nil, err
	}
	rows, err := workflow.ParseList(raw)
	if err != nil {
		return nil, err
	}

	// Roles are cached across workflows: several commonly share one, and each
	// lookup is a live request.
	type roleInfo struct {
		name   string
		scopes []string
	}
	roles := map[string]roleInfo{}

	subjects := make([]workflow.LintSubject, 0, len(rows))
	for _, row := range rows {
		w, err := workflow.ParseWorkflow(row)
		if err != nil {
			subjects = append(subjects, workflow.LintSubject{
				Kind: "workflow", Skipped: "workflow could not be parsed: " + err.Error()})
			continue
		}

		sub := workflow.LintSubject{Kind: "workflow", ID: w.ID, Name: w.Name, Status: w.Status}
		d, err := workflow.ParseDSL(w.DSL)
		if err != nil {
			sub.Skipped = "dsl could not be parsed: " + err.Error()
			subjects = append(subjects, sub)
			continue
		}

		sub.Result = workflow.Validate(d)
		if checkScopes && w.ExecutionRoleID != "" {
			info, ok := roles[w.ExecutionRoleID]
			if !ok {
				name, scopes, rerr := lookupRoleScopes(ctx, w.ExecutionRoleID)
				if rerr != nil {
					// A role that cannot be read is worth saying so about,
					// but it must not abort the sweep — the other workflows
					// still have findings worth having.
					sub.Result.Findings = append(sub.Result.Findings, workflow.Finding{
						Severity: workflow.Warning,
						Path:     "execution_role_id",
						Message:  "could not read the execution role, so its scopes were not checked",
						Hint:     rerr.Error(),
					})
					subjects = append(subjects, sub)
					continue
				}
				info = roleInfo{name: name, scopes: scopes}
				roles[w.ExecutionRoleID] = info
			}
			sub.Role = info.name
			sub.Result = workflow.ValidateWithRole(d, info.name, info.scopes)
		}
		subjects = append(subjects, sub)
	}
	return subjects, nil
}

func lintTemplates(ctx context.Context, client *api.V2Client) ([]workflow.LintSubject, error) {
	raw, err := client.Get(ctx, workflow.TemplatesEndpoint)
	if err != nil {
		return nil, err
	}
	templates, err := workflow.ParseTemplates(raw)
	if err != nil {
		return nil, err
	}

	subjects := make([]workflow.LintSubject, 0, len(templates))
	for _, t := range templates {
		sub := workflow.LintSubject{Kind: "template", ID: t.ID, Name: t.Name}
		d, err := workflow.ParseDSL(t.DSL)
		if err != nil {
			sub.Skipped = "dsl could not be parsed: " + err.Error()
			subjects = append(subjects, sub)
			continue
		}
		// Placeholders are expected in a template, so they are not counted
		// against it — see WithoutPlaceholderFindings.
		sub.Result = workflow.WithoutPlaceholderFindings(workflow.Validate(d))
		// Naming a defect without naming the fix leaves the operator where
		// they started, since this catalog is what they were going to copy.
		if ct, ok := workflow.CorrectionFor(t.Name); ok && len(sub.Result.Findings) > 0 {
			sub.CorrectedBy = ct.ID
		}
		subjects = append(subjects, sub)
	}
	return subjects, nil
}

func writeLintReport(cmd *cobra.Command, summary workflow.LintSummary) error {
	opts := output.CurrentOptions()
	if opts.Format == "json" {
		raw, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(raw))
		return nil
	}

	out := cmd.OutOrStdout()
	if summary.Checked == 0 {
		fmt.Fprintln(out, "Nothing to lint.")
		return nil
	}

	for _, sub := range summary.Subjects {
		name := sub.Name
		if name == "" {
			name = sub.ID
		}
		head := fmt.Sprintf("%s %s", sub.Kind, name)
		if sub.Status != "" {
			head += " (" + sub.Status + ")"
		}
		if sub.Role != "" {
			head += " as " + sub.Role
		}

		switch {
		case sub.Skipped != "":
			fmt.Fprintf(out, "%s — %s\n", head, style.Subtitle.Render("not checked: "+sub.Skipped))
			continue
		case sub.Errors() > 0:
			fmt.Fprintln(out, style.Error.Render(head))
		case sub.Warnings() > 0:
			fmt.Fprintln(out, head)
		default:
			fmt.Fprintf(out, "%s — %s\n", head, style.Subtitle.Render("clean"))
			continue
		}
		for _, f := range sub.Result.Findings {
			fmt.Fprintf(out, "    %s\n", f.String())
		}
		if sub.CorrectedBy != "" {
			fmt.Fprintf(out, "    %s\n", style.Subtitle.Render(
				"a corrected copy is available: jc workflows templates init "+sub.CorrectedBy))
		}
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "\n── %d checked: %d clean, %d with errors, %d with warnings, %d not checked ──\n",
		summary.Checked, summary.Clean, summary.Errors, summary.Warnings, summary.Skipped)
	return nil
}

// compareSimToRun measures a plan against a real run and reports the diff.
func compareSimToRun(cmd *cobra.Command, sim workflow.SimResult, runID string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	raw, err := client.Get(cmd.Context(), workflow.RunEndpoint(runID))
	if err != nil {
		return fmt.Errorf("reading run %s: %w", runID, err)
	}
	run, err := workflow.ParseRun(raw)
	if err != nil {
		return err
	}
	if len(run.ExecutionDetails.Nodes) == 0 {
		return fmt.Errorf("run %s carries no execution trace, so there is nothing to compare "+
			"(a run still in progress has none yet)", runID)
	}

	cmp := workflow.CompareRun(sim, run)

	opts := output.CurrentOptions()
	if opts.Format == "json" {
		out, err := json.MarshalIndent(cmp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	out := cmd.OutOrStdout()
	for _, tc := range cmp.Tasks {
		line := fmt.Sprintf("  [%-23s] %-24s", tc.Verdict, tc.Task)
		if tc.Status != "" {
			line += " → " + tc.Status
		}
		if tc.Verdict == workflow.VerdictAgree {
			fmt.Fprintln(out, line)
			continue
		}
		fmt.Fprintln(out, style.Error.Render(line))
		if tc.Detail != "" {
			fmt.Fprintf(out, "      %s\n", style.Subtitle.Render(tc.Detail))
		}
	}

	w := cmd.ErrOrStderr()
	fmt.Fprintf(w, "\n── %d agree, %d diverge, %d unresolved in the plan, %d not checkable from the trace ──\n",
		cmp.Agree, cmp.Diverge, cmp.Unresolved, cmp.CannotCompare)
	if cmp.RunHalted {
		fmt.Fprintf(w, "The run halted at %q; tasks after it were never reached.\n", cmp.HaltedAt)
	}
	fmt.Fprintf(w, "%s\n", cmp.Caveat)
	return nil
}
