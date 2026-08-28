package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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

	addTypedTool(s, "workflows_runs_get", "Get one JumpCloud Workflow run with its full per-step execution trace. Each step records the HTTP method, URL and status it called, AND the complete response body under node_output.body — so intermediate state is directly inspectable and does not have to be exfiltrated through an email step to be observed. Skipped steps are marked, and a failing step halts the run: everything after it reports 'Not executed — workflow failed at a prior task'. This is the only place a failed run's cause is visible.",
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

	addTypedTool(s, "workflows_templates_list", "List the JumpCloud Workflow templates. The DSL has no published schema, so these server-served templates are the most reliable specification available — start here when authoring. Returns id, name, category, and description without the DSL bodies.",
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
			summaries := make([]map[string]any, 0, len(templates))
			for _, t := range templates {
				summaries = append(summaries, map[string]any{
					"id": t.ID, "name": t.Name, "category": t.Category, "description": t.Description,
				})
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

	addTypedTool(s, "workflows_event_types", "List the Directory Insights event types a jc_events workflow trigger can listen for, with what each one means. This is the vocabulary for schedule.on.one.with.type, and nothing in the workflows API validates it: a mistyped type saves, activates, and then silently never fires, which is indistinguishable from an event that simply has not happened yet. Filter by service or search by substring. NOTE the catalog is a lower bound — a live tenant emitted 30 types this documentation does not list, so an absent type is not proof it is invalid.",
		func(ctx context.Context, req *mcp.CallToolRequest, args wfEventTypesInput) (*mcp.CallToolResult, any, error) {
			matches := workflow.EventTypes(args.Service, args.Search)
			names := make([]string, 0, len(matches))
			for n := range matches {
				names = append(names, n)
			}
			sort.Strings(names)

			rows := make([]map[string]any, 0, len(names))
			for _, n := range names {
				e := matches[n]
				row := map[string]any{"event_type": n, "describes": e.Describe}
				if e.Service != "" {
					row["service"] = e.Service
				}
				rows = append(rows, row)
			}
			res, err := jsonResult(map[string]any{
				"total_in_catalog": workflow.EventTypeCount(),
				"matched":          len(rows),
				"event_types":      rows,
			})
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
