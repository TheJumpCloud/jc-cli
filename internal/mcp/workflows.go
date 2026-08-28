package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/resolve"
	"github.com/klaassen-consulting/jc/internal/workflow"
)

type wfGetInput struct {
	Identifier string `json:"identifier" jsonschema:"Workflow name or ID"`
}

type wfRunsListInput struct {
	Workflow string `json:"workflow,omitempty" jsonschema:"Only runs of this workflow (name or ID). Runs outlive their workflow, so a deleted workflow's ID still works."`
}

type wfRunGetInput struct {
	RunID string `json:"run_id" jsonschema:"Workflow run ID"`
}

type wfEventTypesInput struct {
	Service string `json:"service,omitempty" jsonschema:"Filter to one Directory Insights service (directory, systems, sso, radius, ldap, mdm, password_manager, software, alert, reports, access_management, asset_management, saas_app_management, notifications, object_storage). Omit or use 'all' for everything."`
	Search  string `json:"search,omitempty" jsonschema:"Substring matched against the event type name and its description"`
}

type wfTemplateInput struct {
	Identifier string `json:"identifier" jsonschema:"Workflow template ID or name"`
}

type wfTemplateInitInput struct {
	Identifier string            `json:"identifier" jsonschema:"Workflow template ID or name"`
	Set        map[string]string `json:"set,omitempty" jsonschema:"Fill placeholders: marker name to value. Resolvable markers accept a name (a command name, a group name) and are looked up; a 24-character ID passes through. Call without this first to see each marker's kind."`
}

type wfSimulateInput struct {
	DSL   map[string]any `json:"dsl" jsonschema:"The workflow DSL document to plan"`
	Input map[string]any `json:"input,omitempty" jsonschema:"Trigger input, referenced in the DSL as ${ input.<field> }"`
}

type wfValidateInput struct {
	DSL  map[string]any `json:"dsl" jsonschema:"The workflow DSL document to validate"`
	Role string         `json:"role,omitempty" jsonschema:"Optionally also check each step against this role's API scopes (name or ID). The execution role is the only thing between an unattended workflow and the API, so without this a destructive operationId validates silently."`
}

type wfExplainInput struct {
	DSL        map[string]any `json:"dsl,omitempty" jsonschema:"A DSL document to explain"`
	Identifier string         `json:"identifier,omitempty" jsonschema:"Or the name or ID of an existing workflow"`
}

type wfCreateInput struct {
	Name             string         `json:"name" jsonschema:"Workflow name"`
	Description      string         `json:"description,omitempty" jsonschema:"Workflow description"`
	DSL              map[string]any `json:"dsl" jsonschema:"The workflow DSL document"`
	Role             string         `json:"role" jsonschema:"Execution role name or ID. This decides which JumpCloud operations the workflow may call — choose least privilege."`
	Status           string         `json:"status,omitempty" jsonschema:"active or inactive (default inactive)"`
	AllowSideEffects bool           `json:"allow_side_effects,omitempty" jsonschema:"Required to create a DSL that sends email or calls external connectors"`
	Execute          bool           `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type wfUpdateInput struct {
	Identifier       string         `json:"identifier" jsonschema:"Workflow name or ID"`
	Name             string         `json:"name,omitempty" jsonschema:"Rename the workflow"`
	Description      string         `json:"description,omitempty" jsonschema:"Change the description"`
	DSL              map[string]any `json:"dsl,omitempty" jsonschema:"Replace the DSL"`
	Role             string         `json:"role,omitempty" jsonschema:"Change the execution role"`
	Status           string         `json:"status,omitempty" jsonschema:"active or inactive"`
	AllowSideEffects bool           `json:"allow_side_effects,omitempty" jsonschema:"Required when the new DSL sends email or calls external connectors"`
	Execute          bool           `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type wfDeleteInput struct {
	Identifier string `json:"identifier" jsonschema:"Workflow name or ID"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type wfTriggerInput struct {
	Identifier string         `json:"identifier" jsonschema:"Workflow name or ID. Only external-trigger workflows can be started this way."`
	Data       map[string]any `json:"data,omitempty" jsonschema:"Run input, readable in the DSL as ${ input.<field> }"`
	Execute    bool           `json:"execute,omitempty" jsonschema:"Set to true to actually run the workflow. Without this the tool returns a plan of the steps it would execute."`
}

// resolveWorkflowID maps a workflow name or ID to an ID. Workflow IDs are
// opaque rather than 24-hex, so the generic resolver's ID heuristic does not
// apply; an exact ID match against the list is what disambiguates.
func resolveWorkflowID(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
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

func findWorkflowTemplate(ctx context.Context, client *api.V2Client, identifier string) (workflow.Template, error) {
	// jc's corrected copies resolve first, and only by their "jc:" ID. A
	// corrected copy shares its NAME with the JumpCloud original, so matching
	// on name would silently swap one for the other.
	if workflow.IsCorrectedID(identifier) {
		ct, ok := workflow.FindCorrected(identifier)
		if !ok {
			return workflow.Template{}, fmt.Errorf("no corrected template %q", identifier)
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
	return workflow.Template{}, fmt.Errorf("workflow template %q not found", identifier)
}

// dslRaw re-encodes a decoded DSL argument. MCP arguments arrive decoded, but
// the contract package works on the raw document so that what is validated is
// byte-for-byte what gets sent.
func dslRaw(m map[string]any) (json.RawMessage, error) {
	if len(m) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encoding dsl: %w", err)
	}
	return raw, nil
}

// roleScopes resolves a role and returns its name and scope list.
func roleScopes(ctx context.Context, client *api.V2Client, identifier string) (string, []string, error) {
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

// resolveWorkflowPlaceholder turns a supplied value into the ID the DSL needs.
// The kind-to-resolver binding is shared with the CLI through
// resolve.WorkflowPlaceholderConfigs so the two surfaces cannot disagree about
// which API a placeholder resolves against.
func resolveWorkflowPlaceholder(ctx context.Context, client *api.V2Client, marker, value string) (string, error) {
	kind := workflow.ClassifyPlaceholder(marker)
	if !kind.Resolvable || resolve.IsID(value) {
		return value, nil
	}
	if kind.Kind == workflow.KindWorkflow {
		return resolveWorkflowID(ctx, client, value)
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
		v1, cerr := newV1ClientFunc()
		if cerr != nil {
			return "", cerr
		}
		id, err = resolve.NewResolver(v1).Resolve(ctx, value, pr.Config)
	} else {
		id, err = resolve.NewV2Resolver(client).Resolve(ctx, value, pr.Config)
	}
	if err != nil {
		return "", fmt.Errorf("%s: resolving %s %q: %w", marker, kind.Describe, value, err)
	}
	return id, nil
}

// sideEffectRefusal builds the message that blocks an ungated create or update.
func sideEffectRefusal(res workflow.Result) string {
	var b strings.Builder
	b.WriteString("this workflow reaches outside JumpCloud and needs allow_side_effects=true:")
	for _, se := range res.SideEffects {
		fmt.Fprintf(&b, "\n  %s — %s", se.Task, se.What)
		for _, tg := range se.Targets {
			fmt.Fprintf(&b, "\n      → %s", tg)
		}
	}
	return b.String()
}

func (s *Server) registerWorkflowTools() {
	addTypedTool(s, "workflows_list", "List JumpCloud Workflows — server-side automations that run on Directory Insights events, on a schedule, or when triggered through the API. Returns id, name, status, trigger_type (jc_events|external|scheduler), and last_ran.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Get(ctx, workflow.Endpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("listing workflows: %v", err)), nil, nil
			}
			rows, err := workflow.ParseList(raw)
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

	addTypedTool(s, "workflows_get", "Get one JumpCloud Workflow in full by name or ID, including its DSL document.",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfGetInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveWorkflowID(ctx, client, args.Identifier)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := client.Get(ctx, workflow.WorkflowEndpoint(id))
			if err != nil {
				return errorResult(fmt.Sprintf("getting workflow: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "workflows_runs_list", "List JumpCloud Workflow runs. Runs OUTLIVE the workflow they came from: a run whose workflow was deleted still lists, carrying workflowDeletedAt. This is the audit trail.",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfRunsListInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			endpoint := workflow.RunsEndpoint
			if args.Workflow != "" {
				id, err := resolveWorkflowID(ctx, client, args.Workflow)
				if err != nil {
					// The workflow may be deleted while its runs remain.
					id = args.Workflow
				}
				endpoint = fmt.Sprintf("%s?workflow_id=%s", endpoint, id)
			}
			raw, err := client.Get(ctx, endpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("listing workflow runs: %v", err)), nil, nil
			}
			rows, err := workflow.ParseList(raw)
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

	addTypedTool(s, "workflows_runs_get", "Get one JumpCloud Workflow run with its full per-step execution trace. Each step records the HTTP method, URL and status it called, AND the complete response body under node_output.body — so intermediate state is directly inspectable and does not have to be exfiltrated through an email step to be observed. Skipped steps are marked, and a failing step halts the run: everything after it reports 'Not executed — workflow failed at a prior task'. A switch node also records case_evaluations (every case, its when expression, and whether it matched) and the branch chosen. Each node carries is_output_truncated; the size at which the ENGINE truncates a step's body is undocumented and is a different ceiling from this server's own result limit, so a very large step response may not be a faithful record even when the trace itself returns fine. This is the only place a failed run's cause is visible. "+workflow.RanPredicateDoc+"",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfRunGetInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Get(ctx, workflow.RunEndpoint(args.RunID))
			if err != nil {
				return errorResult(fmt.Sprintf("getting workflow run: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "workflows_templates_list", "List the JumpCloud Workflow templates. The DSL has no published schema, so these server-served templates are the most reliable specification available — start here when authoring. Returns id, name, category, description, and source, without the DSL bodies. IMPORTANT: four of JumpCloud's templates ship with a defect — they open a task guard with `actions.X.status == 200 &&`, which cannot detect failure (a non-2xx already halted the run) and CAN silently skip the task when the call returns 201 instead of 200. Those carry corrected_by naming jc's repaired copy, whose id starts with \"jc:\"; prefer that copy when authoring, and pass its id to workflows_templates_show or workflows_templates_init exactly as given.",
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Get(ctx, workflow.TemplatesEndpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("listing workflow templates: %v", err)), nil, nil
			}
			templates, err := workflow.ParseTemplates(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			summaries := make([]map[string]any, 0, len(templates)+4)
			for _, ct := range workflow.CorrectedTemplates() {
				summaries = append(summaries, map[string]any{
					"id": ct.ID, "name": ct.Name, "category": ct.Category,
					"description": ct.Description, "source": "jc",
					"corrects": ct.Corrects, "changes": ct.Changes,
				})
			}
			for _, t := range templates {
				row := map[string]any{
					"id": t.ID, "name": t.Name, "category": t.Category,
					"description": t.Description, "source": "jumpcloud",
				}
				if ct, ok := workflow.CorrectionFor(t.Name); ok {
					row["corrected_by"] = ct.ID
				}
				summaries = append(summaries, row)
			}
			res, err := jsonResult(summaries)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "workflows_templates_show", "Show one JumpCloud Workflow template in full, including its DSL.",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfTemplateInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			t, err := findWorkflowTemplate(ctx, client, args.Identifier)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			res, err := jsonResult(t)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "workflows_templates_init", "Turn a JumpCloud Workflow template into a workflow document ready to create. Returns the document plus, for every REPLACE_WITH_* placeholder, what that placeholder expects — a command, a user group, a device group, a policy, or free text. Pass `set` to fill them: resolvable kinds accept a NAME and are looked up, so you do not need to find IDs first. Call once without `set` to see the kinds, then again with them filled.",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfTemplateInitInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			t, err := findWorkflowTemplate(ctx, client, args.Identifier)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			d, err := workflow.ParseDSL(t.DSL)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}

			dsl := t.DSL
			if len(args.Set) > 0 {
				values := make(map[string]string, len(args.Set))
				for marker, raw := range args.Set {
					// Resolve before filling so a bad name fails naming the
					// marker, rather than producing a workflow with a name
					// where an ID belongs.
					resolved, err := resolveWorkflowPlaceholder(ctx, client, workflow.NormalizeMarker(marker), raw)
					if err != nil {
						return errorResult(err.Error()), nil, nil
					}
					values[marker] = resolved
				}
				dsl, err = d.Fill(values)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
			}

			filled := workflow.DSL{}
			if parsed, perr := workflow.ParseDSL(dsl); perr == nil {
				filled = parsed
			}
			kinds := map[string]any{}
			for marker, k := range filled.PlaceholderKinds() {
				kinds[marker] = k
			}

			res, err := jsonResult(map[string]any{
				"name":              t.Name,
				"description":       t.Description,
				"status":            workflow.StatusInactive,
				"dsl":               dsl,
				"placeholders":      filled.PlaceholderMarkers(),
				"placeholder_kinds": kinds,
			})
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "workflows_simulate", "Plan what a JumpCloud workflow WOULD call, without creating or running it. Conditions are evaluated with the same Expr engine the DSL uses and ${ } references are resolved against the supplied input, so each step is reported with its real resolved parameters. Reads are reported as would-call; writes, emails and connector_operation calls are reported as stubbed and are NEVER performed — nothing is sent. Needs no created workflow, no active status and no write-capable role, so it works with read-only access. IMPORTANT: this is a plan, not a prediction of engine behaviour. Branch selection, halt-on-error and expression semantics here are this tool's reading of the DSL, not observations of JumpCloud's runtime; verify behaviour with a real run.",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfSimulateInput) (*mcp.CallToolResult, any, error) {
			raw, err := dslRaw(args.DSL)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if len(raw) == 0 {
				return errorResult("no dsl given"), nil, nil
			}
			sim, err := workflow.SimulateRaw(raw, args.Input)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			res, jerr := jsonResult(sim)
			if jerr != nil {
				return errorResult(jerr.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "workflows_health", "Find event-triggered JumpCloud workflows that SHOULD have fired and did not. Nothing in the product can tell you this: a workflow whose trigger event type is mistyped or never emitted saves, activates, and then silently never runs — and there is no match counter, no last-evaluated timestamp, and no error anywhere to distinguish it from an event that simply has not happened yet. This holds both halves of the comparison, asking Directory Insights how often each trigger's event ACTUALLY occurred and the runs list how often the workflow ran, and reports one of: never-fired (the event occurred and the workflow did not run — the failure with no other signal), firing, or unverifiable (the event did not occur in the window, so nothing can be concluded either way). Fewer runs than events is NOT reported as a fault, because a trigger condition legitimately filters. Read-only: it lists and counts, and creates, activates or runs nothing.",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfHealthInput) (*mcp.CallToolResult, any, error) {
			days := args.Days
			if days == 0 {
				days = 7
			}
			if days < 0 {
				return errorResult("days must be positive"), nil, nil
			}

			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			raw, err := client.Get(ctx, workflow.Endpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("listing workflows: %v", err)), nil, nil
			}
			rows, err := workflow.ParseList(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			rawRuns, err := client.Get(ctx, workflow.RunsEndpoint)
			if err != nil {
				return errorResult(fmt.Sprintf("listing runs: %v", err)), nil, nil
			}
			runs, err := workflow.ParseRuns(rawRuns)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}

			now := nowFunc().UTC()
			since := now.AddDate(0, 0, -days)

			// Counts are cached per (event type, window start) rather than
			// per event type alone: workflows younger than the window are
			// judged over a shorter one, so they need their own count.
			counts := map[string]int{}
			reports := make([]workflow.HealthReport, 0, len(rows))

			for _, row := range rows {
				w, werr := workflow.ParseWorkflow(row)
				if werr != nil {
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
						ic, ierr := newInsightsClientFunc()
						if ierr != nil {
							return errorResult(fmt.Sprintf("creating Insights client: %v", ierr)), nil, nil
						}
						n, ierr = ic.CountEvents(ctx, api.InsightsQuery{
							Service:          "all",
							StartTime:        start.Format(time.RFC3339),
							EndTime:          now.Format(time.RFC3339),
							SearchTermFilter: map[string]any{"event_type": eventType},
						})
						if ierr != nil {
							return errorResult(fmt.Sprintf("counting %s events: %v", eventType, ierr)), nil, nil
						}
						counts[key] = n
					}
					events = n
				}

				reports = append(reports, workflow.AssessHealth(w, events,
					workflow.RunsWithin(runs, w.ID, start), start))
			}

			workflow.SortHealth(reports)

			neverFired := 0
			for _, r := range reports {
				if r.Verdict == workflow.HealthNeverFired {
					neverFired++
				}
			}

			res, jerr := jsonResult(map[string]any{
				"window_days": days,
				"since":       since.Format(time.RFC3339),
				"workflows":   len(reports),
				"never_fired": neverFired,
				"reports":     reports,
			})
			if jerr != nil {
				return errorResult(jerr.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "workflows_lint", "Validate EVERY workflow on the tenant at once, and optionally the served template catalog. workflows_validate answers \"is this one document right?\"; this answers \"which of the things already running are wrong?\" — the question nobody asks, because by hand it means exporting every workflow and checking each in turn. JumpCloud accepts a malformed DSL and fails only at run time, so a broken workflow sits there looking healthy. Set templates to lint the catalog instead: the DSL has no published schema, so templates are the only worked examples, and an idiom that appears in one gets copied into real workflows — linting them says which examples are safe to copy. Set scopes to also check each workflow against ITS OWN execution role, the role it will really run as — this catches DRIFT rather than typos, since the API already rejects a scope-short workflow at create time, but nothing rechecks after a role is edited to drop a scope, which leaves every workflow under it failing at run time. Results are ordered worst-first. A defective template carries corrected_by naming jc's repaired copy, and those corrected copies are linted in the SAME sweep, so the output proves them rather than only reporting the defects. Read-only.",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfLintInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}

			var subjects []workflow.LintSubject

			if !args.Templates || args.All {
				raw, gerr := client.Get(ctx, workflow.Endpoint)
				if gerr != nil {
					return errorResult(fmt.Sprintf("listing workflows: %v", gerr)), nil, nil
				}
				rows, perr := workflow.ParseList(raw)
				if perr != nil {
					return errorResult(perr.Error()), nil, nil
				}

				// Roles are cached: several workflows commonly share one.
				type roleInfo struct {
					name   string
					scopes []string
				}
				roles := map[string]roleInfo{}

				for _, row := range rows {
					w, werr := workflow.ParseWorkflow(row)
					if werr != nil {
						subjects = append(subjects, workflow.LintSubject{
							Kind: "workflow", Skipped: "workflow could not be parsed: " + werr.Error()})
						continue
					}
					sub, d, ok := workflow.LintWorkflow(w)
					if !ok {
						subjects = append(subjects, sub)
						continue
					}

					if args.Scopes && w.ExecutionRoleID != "" {
						info, ok := roles[w.ExecutionRoleID]
						if !ok {
							name, scopes, rerr := roleScopesFunc(ctx, w.ExecutionRoleID)
							if rerr != nil {
								// One unreadable role must not abort the
								// sweep; the other findings still matter.
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
			}

			if args.Templates || args.All {
				raw, gerr := client.Get(ctx, workflow.TemplatesEndpoint)
				if gerr != nil {
					return errorResult(fmt.Sprintf("listing templates: %v", gerr)), nil, nil
				}
				templates, perr := workflow.ParseTemplates(raw)
				if perr != nil {
					return errorResult(perr.Error()), nil, nil
				}
				for _, t := range templates {
					subjects = append(subjects, workflow.LintTemplate(t.ID, t.Name, t.DSL))
				}
				// jc's corrected copies are linted in the same sweep, so the
				// output proves the corrections rather than only reporting
				// the defects they fix.
				subjects = append(subjects, workflow.LintCorrected()...)
			}

			res, jerr := jsonResult(workflow.Summarize(subjects))
			if jerr != nil {
				return errorResult(jerr.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "workflows_compare_run", "Measure a workflow dry run against a REAL run's trace, task by task. workflows_simulate produces a plan and openly states it is this tool's reading of the DSL rather than an observation of JumpCloud's runtime; this is how that reading gets checked. Give the same dsl (and input) you would give workflows_simulate, plus a run_id, and it reports where the plan and the run agree and where they do not. The verdict worth acting on is ran-but-planned-skip: the workflow touched something the plan said it would not. Divergence is not automatically a planner bug — a guard that reads a prior step's response body cannot be evaluated without one, and those are reported as unresolved-in-plan and counted separately rather than held against the plan. "+workflow.RanPredicateDoc+" Read-only: it fetches one run and runs nothing.",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfCompareRunInput) (*mcp.CallToolResult, any, error) {
			raw, err := dslRaw(args.DSL)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if len(raw) == 0 {
				return errorResult("no dsl given"), nil, nil
			}
			if args.RunID == "" {
				return errorResult("run_id is required"), nil, nil
			}

			sim, err := workflow.SimulateRaw(raw, args.Input)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}

			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			rawRun, err := client.Get(ctx, workflow.RunEndpoint(args.RunID))
			if err != nil {
				return errorResult(fmt.Sprintf("reading run %s: %v", args.RunID, err)), nil, nil
			}
			run, err := workflow.ParseRun(rawRun)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if len(run.ExecutionDetails.Nodes) == 0 {
				return errorResult(fmt.Sprintf("run %s carries no execution trace, so there is nothing to "+
					"compare (a run still in progress has none yet)", args.RunID)), nil, nil
			}

			res, jerr := jsonResult(workflow.CompareRun(sim, run))
			if jerr != nil {
				return errorResult(jerr.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "workflows_event_types", "List the Directory Insights event types a jc_events workflow trigger can listen for, with what each one means. This is the vocabulary for schedule.on.one.with.type, and nothing in the workflows API validates it: a mistyped type saves, activates, and then silently never fires, which is indistinguishable from an event that simply has not happened yet. Filter by service or search by substring — narrowing to 25 results or fewer also returns payload_fields, the fields a condition on that event may reference (resource, changes, initiated_by, auth_method, geoip, ...), since a condition naming a field the event does not carry evaluates false forever. NOTE both the catalog and the field list are lower bounds: a live tenant emitted 30 types this documentation does not list, so an absent entry is not proof it is invalid.",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfEventTypesInput) (*mcp.CallToolResult, any, error) {
			matches := workflow.EventTypes(args.Service, args.Search)
			names := make([]string, 0, len(matches))
			for n := range matches {
				names = append(names, n)
			}
			sort.Strings(names)

			// The catalog is 341 entries and the payload field list is ~16
			// per entry. Returning both unfiltered is ~174KB in a single tool
			// result, which is not a reasonable thing to hand a model. Below
			// the threshold the caller has narrowed to something specific and
			// the fields are what they came for; above it, this is a browse
			// and the names are enough.
			const detailThreshold = 25
			detailed := len(names) <= detailThreshold

			rows := make([]map[string]any, 0, len(names))
			for _, n := range names {
				e := matches[n]
				row := map[string]any{"event_type": n, "describes": e.Describe}
				if e.Service != "" {
					row["service"] = e.Service
				}
				if detailed {
					row["payload_fields"] = workflow.EventFields(n)
				}
				rows = append(rows, row)
			}

			out := map[string]any{
				"total_in_catalog": workflow.EventTypeCount(),
				"matched":          len(rows),
				"event_types":      rows,
			}
			if !detailed {
				out["note"] = fmt.Sprintf(
					"payload_fields omitted: %d matches exceeds %d. Narrow with service or search to get the fields a condition may reference.",
					len(rows), detailThreshold)
			}
			res, err := jsonResult(out)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "workflows_validate", "Validate a JumpCloud Workflow DSL locally, before it reaches the API. JumpCloud accepts a malformed DSL and fails only when the workflow runs, so this is the difference between an error now and a workflow that silently never works. Checks the trigger, task structure, control flow, pagination, every Expr expression (compiled with the same engine), and every operationId against JumpCloud's own API operations. Also reports every step that sends email or calls an external connector.",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfValidateInput) (*mcp.CallToolResult, any, error) {
			raw, err := dslRaw(args.DSL)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if len(raw) == 0 {
				return errorResult("no dsl given"), nil, nil
			}

			result := workflow.ValidateRaw(raw)
			if args.Role != "" {
				d, perr := workflow.ParseDSL(raw)
				if perr != nil {
					return errorResult(perr.Error()), nil, nil
				}
				client, cerr := newV2ClientFunc()
				if cerr != nil {
					return errorResult(fmt.Sprintf("creating API client: %v", cerr)), nil, nil
				}
				name, scopes, rerr := roleScopes(ctx, client, args.Role)
				if rerr != nil {
					return errorResult(rerr.Error()), nil, nil
				}
				result = workflow.ValidateWithRole(d, name, scopes)
			}
			res, err := jsonResult(result)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "workflows_explain", "Explain what a JumpCloud Workflow does: when it fires, what each step calls (with every operationId resolved to its real METHOD and path), and which steps reach outside JumpCloud. Accepts either a DSL document or the name or ID of an existing workflow.",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfExplainInput) (*mcp.CallToolResult, any, error) {
			raw, derr := dslRaw(args.DSL)
			if derr != nil {
				return errorResult(derr.Error()), nil, nil
			}
			if len(raw) == 0 {
				if args.Identifier == "" {
					return errorResult("supply either dsl or identifier"), nil, nil
				}
				client, err := newV2ClientFunc()
				if err != nil {
					return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
				}
				id, err := resolveWorkflowID(ctx, client, args.Identifier)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				wraw, err := client.Get(ctx, workflow.WorkflowEndpoint(id))
				if err != nil {
					return errorResult(fmt.Sprintf("getting workflow: %v", err)), nil, nil
				}
				w, err := workflow.ParseWorkflow(wraw)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				raw = w.DSL
			}

			d, err := workflow.ParseDSL(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			trigger, _ := d.Trigger()
			steps := make([]map[string]any, 0)
			for _, t := range d.Tasks() {
				step := map[string]any{"task": t.Name, "depth": t.Depth}
				if id := t.OperationID(); id != "" {
					step["operationId"] = id
					if op, ok := workflow.LookupOperation(id); ok {
						step["calls"] = op.Describe()
					} else {
						step["calls"] = "UNKNOWN OPERATION"
					}
				} else if c := t.Call(); c != "" {
					step["call"] = c
				}
				if cond, ok := t.Body["if"].(string); ok {
					step["if"] = cond
				}
				steps = append(steps, step)
			}
			res, err := jsonResult(map[string]any{
				"trigger_type": trigger.TriggerType(),
				"event_type":   trigger.EventType,
				"frequency":    trigger.Frequency,
				"condition":    trigger.Condition,
				"steps":        steps,
				"side_effects": d.SideEffects(),
			})
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "workflows_create", "Create a JumpCloud Workflow. The DSL is validated locally first, so a malformed workflow fails here with a JSON path rather than at run time. The role decides which JumpCloud operations the workflow may call — it runs unattended, so choose least privilege. New workflows are inactive unless status=active. A DSL that sends email or calls external connectors is refused unless allow_side_effects=true. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			dsl, err := dslRaw(args.DSL)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if args.Name == "" || len(dsl) == 0 || args.Role == "" {
				return errorResult("name, dsl, and role are required"), nil, nil
			}
			status := args.Status
			if status == "" {
				status = workflow.StatusInactive
			}
			if status != workflow.StatusActive && status != workflow.StatusInactive {
				return errorResult(fmt.Sprintf("status must be active or inactive, got %q", status)), nil, nil
			}

			res := workflow.ValidateRaw(dsl)
			if err := res.Err(); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if len(res.SideEffects) > 0 && !args.AllowSideEffects {
				return errorResult(sideEffectRefusal(res)), nil, nil
			}

			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			roleID, err := r.Resolve(ctx, args.Role, resolve.RoleConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}

			// The role is already resolved here, so the scope gaps cost
			// nothing extra and belong in the plan: this is the last point
			// before a run-time permission failure.
			var scopeGaps []workflow.ScopeGap
			if d, perr := workflow.ParseDSL(dsl); perr == nil {
				if _, scopes, rerr := roleScopes(ctx, client, args.Role); rerr == nil {
					scopeGaps = workflow.CheckScopes(d, scopes)
				}
			}

			if !args.Execute {
				plan := map[string]any{
					"trigger":      res.TriggerType,
					"status":       status,
					"role":         args.Role,
					"side_effects": res.SideEffects,
				}
				if len(scopeGaps) > 0 {
					plan["scope_gaps"] = scopeGaps
					plan["scope_warning"] = fmt.Sprintf(
						"role %q may not permit %d of this workflow's operations; the API will reject the create if so",
						args.Role, len(scopeGaps))
				}
				return planResult("create", "workflow", args.Name, "", plan)
			}

			raw, err := client.Create(ctx, workflow.Endpoint, workflow.CreateBody(workflow.Workflow{
				Name: args.Name, Description: args.Description, DSL: dsl,
				Status: status, ExecutionRoleID: roleID,
			}))
			if err != nil {
				return errorResult(fmt.Sprintf("creating workflow: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "workflows_update", "Update a JumpCloud Workflow. The current workflow is read first and sent back complete, because this API's PUT is full-replace. A new DSL is validated locally before it is sent, and is refused if it sends email or calls external connectors unless allow_side_effects=true. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			newDSL, err := dslRaw(args.DSL)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if args.Name == "" && args.Description == "" && len(newDSL) == 0 && args.Role == "" && args.Status == "" {
				return errorResult("no changes requested: supply name, description, dsl, role, or status"), nil, nil
			}

			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveWorkflowID(ctx, client, args.Identifier)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			curRaw, err := client.Get(ctx, workflow.WorkflowEndpoint(id))
			if err != nil {
				return errorResult(fmt.Sprintf("getting workflow: %v", err)), nil, nil
			}
			next, err := workflow.ParseWorkflow(curRaw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}

			changes := map[string]any{}
			if len(newDSL) > 0 {
				next.DSL = newDSL
				changes["dsl"] = "replaced"
			}
			if args.Name != "" {
				next.Name = args.Name
				changes["name"] = args.Name
			}
			if args.Description != "" {
				next.Description = args.Description
				changes["description"] = "updated"
			}
			if args.Status != "" {
				if args.Status != workflow.StatusActive && args.Status != workflow.StatusInactive {
					return errorResult(fmt.Sprintf("status must be active or inactive, got %q", args.Status)), nil, nil
				}
				next.Status = args.Status
				changes["status"] = args.Status
			}
			if args.Role != "" {
				r := resolve.NewV2Resolver(client)
				roleID, err := r.Resolve(ctx, args.Role, resolve.RoleConfig)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				next.ExecutionRoleID = roleID
				changes["role"] = args.Role
			}

			res := workflow.ValidateRaw(next.DSL)
			if err := res.Err(); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if len(newDSL) > 0 && len(res.SideEffects) > 0 && !args.AllowSideEffects {
				return errorResult(sideEffectRefusal(res)), nil, nil
			}

			if !args.Execute {
				return planResult("update", "workflow", args.Identifier, id, map[string]any{
					"changes":      changes,
					"side_effects": res.SideEffects,
				})
			}

			raw, err := client.Update(ctx, workflow.WorkflowEndpoint(id), workflow.UpdateBody(next))
			if err != nil {
				return errorResult(fmt.Sprintf("updating workflow: %v", err)), nil, nil
			}
			return textResult(string(raw)), nil, nil
		},
	)

	addTypedTool(s, "workflows_delete", "Delete a JumpCloud Workflow. Its runs are NOT deleted: they remain listed with workflowDeletedAt set, so the audit trail survives. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfDeleteInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveWorkflowID(ctx, client, args.Identifier)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := client.Get(ctx, workflow.WorkflowEndpoint(id))
			if err != nil {
				return errorResult(fmt.Sprintf("getting workflow: %v", err)), nil, nil
			}
			w, err := workflow.ParseWorkflow(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}

			if !args.Execute {
				return planResult("delete", "workflow", args.Identifier, id, map[string]any{
					"name":    w.Name,
					"status":  w.Status,
					"trigger": w.TriggerType,
					"note":    "past runs are kept and stay listed",
				})
			}

			if _, err := client.Delete(ctx, workflow.WorkflowEndpoint(id)); err != nil {
				return errorResult(fmt.Sprintf("deleting workflow: %v", err)), nil, nil
			}
			res, err := jsonResult(map[string]any{"deleted": id, "name": w.Name})
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			return res, nil, nil
		},
	)

	addTypedTool(s, "workflows_trigger", "Start a manual run of a JumpCloud Workflow. This ONLY works for workflows whose trigger source is external; others are refused rather than posting a call that silently does nothing. The workflow then executes for real with whatever its execution role permits — it may run commands on devices, change users, send email, or call external systems. Set execute=true to actually run it; otherwise returns a plan listing the steps it would execute.",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfTriggerInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			id, err := resolveWorkflowID(ctx, client, args.Identifier)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			raw, err := client.Get(ctx, workflow.WorkflowEndpoint(id))
			if err != nil {
				return errorResult(fmt.Sprintf("getting workflow: %v", err)), nil, nil
			}
			w, err := workflow.ParseWorkflow(raw)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if w.TriggerType != "" && w.TriggerType != workflow.TriggerExternal {
				return errorResult(fmt.Sprintf(
					"workflow %q has a %s trigger, so a manual run does nothing; only external-trigger workflows can be started this way",
					args.Identifier, w.TriggerType)), nil, nil
			}

			if !args.Execute {
				steps := []string{}
				var effects []workflow.SideEffect
				if d, derr := workflow.ParseDSL(w.DSL); derr == nil {
					for _, t := range d.Tasks() {
						if id := t.OperationID(); id != "" {
							if op, ok := workflow.LookupOperation(id); ok {
								steps = append(steps, t.Name+": "+op.Describe())
								continue
							}
						}
						steps = append(steps, t.Name+": "+t.Call())
					}
					effects = d.SideEffects()
				}
				return planResult("trigger", "workflow", args.Identifier, id, map[string]any{
					"status":       w.Status,
					"will_execute": steps,
					"side_effects": effects,
				})
			}

			out, err := client.Create(ctx, workflow.TriggerEndpoint(id), workflow.TriggerBody(args.Data))
			if err != nil {
				return errorResult(fmt.Sprintf("triggering workflow: %v", err)), nil, nil
			}
			return textResult(string(out)), nil, nil
		},
	)
}

type wfHealthInput struct {
	Days int `json:"days,omitempty" jsonschema:"How many days of event history to compare against (default 7)"`
}

type wfLintInput struct {
	Templates bool `json:"templates,omitempty" jsonschema:"Lint the served template catalog instead of the tenant's workflows"`
	All       bool `json:"all,omitempty" jsonschema:"Lint both the workflows and the templates"`
	Scopes    bool `json:"scopes,omitempty" jsonschema:"Also check each workflow against its own execution role's API scopes"`
}

// roleScopesFunc reads a role's name and scopes. Indirected so tests can
// supply scopes without a live roles endpoint.
var roleScopesFunc = func(ctx context.Context, roleID string) (string, []string, error) {
	client, err := newV2ClientFunc()
	if err != nil {
		return "", nil, err
	}
	raw, err := client.Get(ctx, "/roles/"+roleID)
	if err != nil {
		return "", nil, fmt.Errorf("reading role %s: %w", roleID, err)
	}
	var role struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &role); err != nil {
		return "", nil, fmt.Errorf("decoding role: %w", err)
	}
	if role.Name == "" {
		role.Name = roleID
	}
	return role.Name, role.Scopes, nil
}

type wfCompareRunInput struct {
	DSL   map[string]any `json:"dsl" jsonschema:"The workflow DSL document to plan"`
	RunID string         `json:"run_id" jsonschema:"The id of a completed run to compare the plan against"`
	Input map[string]any `json:"input,omitempty" jsonschema:"Input to resolve ${ input.<field> } against"`
}
