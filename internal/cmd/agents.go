package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/agents"
	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/output"
)

// newAgentsCmd builds the `jc agents` group — JumpCloud's AI Agent
// Identities.
//
// These live under Identity & Access rather than AI & Automation on purpose:
// an agent is an identity with credentials and app assignments, managed
// alongside users and admins, and that is where an operator will look for it.
// The AI & Automation group is jc's own assistant features.
func newAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "agents",
		Aliases: []string{"agent"},
		Short:   "Inspect JumpCloud AI agent identities",
		Long: `Read JumpCloud's AI agent identities: autonomous workloads that hold their own
identity, their own OAuth client credentials, and membership in agent groups
that are assigned to applications.

THE ORG ID IS SENT FOR YOU. Most of these endpoints require an x-org-id
header that the API spec marks optional. jc sends your active profile's
organization id, so there is no flag for it — run ` + "`jc auth login`" + ` if the
profile has none, or set JC_ORG_ID.

CREDENTIALS CARRY NO SECRET HERE. ` + "`agents credentials list`" + ` reports a
credential's client id, type, status and expiry. It never returns the
client secret, and neither does any other read: a secret is returned once,
by the call that creates it.`,
		Example: `  # Every agent in the org.
  jc agents list -t

  # Only the suspended ones.
  jc agents list --filter state:eq:suspended -t

  # What one agent can reach, and whether directly or through a group.
  jc agents applications 6aa85171a36f7d16b20d9b60 -t
  jc agents memberof 6aa85171a36f7d16b20d9b60 -t

  # How it authenticates, without revealing anything.
  jc agents credentials list 6aa85171a36f7d16b20d9b60 -t`,
	}
	cmd.AddCommand(
		newAgentsListCmd(),
		newAgentsGetCmd(),
		newAgentsMemberOfCmd(),
		newAgentsApplicationsCmd(),
		newAgentsCredentialsCmd(),
		newAgentsRegisterCmd(),
		newAgentsUpdateCmd(),
		newAgentsSuspendCmd(),
		newAgentsActivateCmd(),
		newAgentsDeleteCmd(),
	)
	return cmd
}

// newAgentGroupsCmd builds the `jc agent-groups` group.
//
// The command is hyphenated because that is how an operator reads it; the
// endpoint is /agentgroups, one word, because that is how the server spells
// it. The alias covers anyone who types the endpoint's spelling.
func newAgentGroupsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "agent-groups",
		Aliases: []string{"agent-group", "agentgroups"},
		Short:   "Inspect JumpCloud AI agent groups",
		Long: `Read JumpCloud's agent groups: the collections through which agents are
granted access to applications.

Agent groups are assigned to applications and to nothing else — no policies,
no commands, no devices — so ` + "`associations`" + ` has exactly one kind of target.

This half of the area behaves differently from ` + "`jc agents`" + `, and the
differences are the server's: no organization id is needed, listings page by
offset rather than by cursor, --sort works here and is ignored there, and the
two accept different filter fields.`,
		Example: `  # Every agent group.
  jc agent-groups list -t

  # Which agents are in one, by name.
  jc agent-groups members 6aa85141bda774000189bca6 -t

  # Which applications it grants.
  jc agent-groups associations 6aa85141bda774000189bca6 -t`,
	}
	cmd.AddCommand(
		newAgentGroupsListCmd(),
		newAgentGroupsGetCmd(),
		newAgentGroupsMembersCmd(),
		newAgentGroupsAssociationsCmd(),
		newAgentGroupsCreateCmd(),
		newAgentGroupsUpdateCmd(),
		newAgentGroupsDeleteCmd(),
		newAgentGroupsAddMemberCmd(),
		newAgentGroupsRemoveMemberCmd(),
		newAgentGroupsAssignAppCmd(),
		newAgentGroupsUnassignAppCmd(),
	)
	return cmd
}

// --- Shared plumbing ------------------------------------------------------

// agentsClient returns a v2 client that sends the organization id with every
// request it makes.
//
// The header is set on THIS client, not process-wide: it applies to the
// calls this command makes and to nothing else. Sending it to the endpoints
// in the area that do not require it is harmless (verified against every
// read), so one decision covers the command's primary request and the
// catalog lookups its name resolution needs.
//
// endpoint is the command's primary request. When it requires an org id and
// the profile has none, this fails here — naming the config an operator can
// actually set, rather than forwarding a server error that names a header no
// flag is called. A command whose primary endpoint does not require one is
// still given the header when there is one, so its name lookups work.
func agentsClient(endpoint string) (*api.V2Client, error) {
	client, err := newV2Client()
	if err != nil {
		return nil, err
	}

	orgID := config.OrgID()
	if orgID == "" {
		if agents.RequiresOrgID(endpoint) {
			return nil, agents.ErrNoOrgID()
		}
		return client, nil
	}

	client.SetExtraHeaders(http.Header{agents.OrgHeader: []string{orgID}})
	return client, nil
}

// agentsWarn reports a degraded name lookup on stderr, where it cannot
// corrupt piped output.
func agentsWarn(cmd *cobra.Command) func(string) {
	return func(msg string) {
		fmt.Fprintln(cmd.ErrOrStderr(), "Warning: could not resolve "+msg)
	}
}

// explainAgentsError replaces the suggestion on the one error in this area
// whose cause the server's own message points away from.
//
// A wrong organization id produces 404 "selected organization not found",
// which jc's generic handling renders with "Verify the resource exists and
// the ID/name is correct" — sending an operator to look for a missing agent
// when the fault is a stale org_id in their config.
//
// This returns a *CLIError rather than wrapping with %w, because wrapping
// does not work here: ToCLIError checks for a CLIError first and an APIError
// second, so a wrapper's own text is discarded in favour of the API error it
// wraps. Building the CLIError keeps the status and endpoint and changes only
// the advice.
func explainAgentsError(err error) error {
	if !agents.OrgMismatch(err) {
		return err
	}
	out := ToCLIError(err)
	out.Suggestion = fmt.Sprintf("The organization id jc sent (%s) is not one this API key "+
		"can reach — check `jc auth status`, or set JC_ORG_ID", config.OrgID())
	return out
}

// writeNamedRefs renders a resolved association list.
//
// These four reads return records carrying an id and a type and nothing else,
// so the names are joined in before printing. The agent and group records
// themselves already carry a name, and are printed exactly as the server
// returned them — jc resolves where the server gives nothing to read, and
// does not rewrite records that are already legible.
func writeNamedRefs(cmd *cobra.Command, refs []agents.NamedRef, singular, plural string) error {
	rows := make([]json.RawMessage, 0, len(refs))
	for _, r := range refs {
		raw, err := json.Marshal(r)
		if err != nil {
			return err
		}
		rows = append(rows, raw)
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = []string{"name", "id", "type", "via"}
	if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
		return err
	}
	if !opts.Quiet && !opts.IDsOnly {
		writeNounFooter(cmd, len(refs), singular, plural)
	}
	return nil
}

// resolveAgentID checks an id locally so a typo reads as a typo.
func resolveAgentID(kind, id string) error {
	if !agents.IsObjectID(id) {
		return agents.ErrNotObjectID(kind, id)
	}
	return nil
}

// --- agents list ----------------------------------------------------------

func newAgentsListCmd() *cobra.Command {
	var filters []string
	var limit int
	var pageToken string

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List AI agent identities",
		Long: `List the org's AI agent identities.

PAGING IS BY CURSOR HERE, not by limit and skip. With no --limit, jc follows
the server's page cursor until the listing is exhausted. With --limit, it
returns that many and reports the cursor to continue from, which --page-token
accepts.

The footer reports the server's match count, which is larger than the rows
shown whenever --limit is in play.

--filter accepts ` + fmtList(agents.AgentFilterFields()) + ` with the ` +
			fmtList(agents.FilterOperators()) + ` operators. There is no substring
match: an agent cannot be found by part of its name. --sort is not offered
because this endpoint ignores it.`,
		Example: `  jc agents list -t
  jc agents list --filter state:eq:suspended
  jc agents list --filter name:eq:"AWS JKC Agent"

  # Walk it a page at a time.
  jc agents list --limit 2
  jc agents list --limit 2 --page-token eyJsIjoyLCJzIjoyLCJ2IjoxfQ`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, f := range filters {
				if err := agents.ValidateAgentFilter(f); err != nil {
					return err
				}
			}

			client, err := agentsClient(agents.AgentsEndpoint)
			if err != nil {
				return err
			}

			var (
				rows  []json.RawMessage
				total int
				next  string
			)
			if limit > 0 || pageToken != "" {
				page, perr := agents.GetAgentsPage(cmd.Context(), client, filters, limit, pageToken)
				if perr != nil {
					return explainAgentsError(perr)
				}
				rows, total, next = page.Rows, page.TotalSize, page.NextPageToken
			} else {
				rows, total, err = agents.ListAgents(cmd.Context(), client, filters)
				if err != nil {
					return explainAgentsError(err)
				}
			}

			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"id", "name", "email", "state", "ownerId"}
			if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				writeNounListFooter(cmd, len(rows), total, "agent", "agents")
				if next != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "More: --page-token %s\n", next)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&filters, "filter", nil,
		"Filter as field:operator:value (repeatable, combined with AND). Fields: "+
			fmtList(agents.AgentFilterFields())+"; operators: "+fmtList(agents.FilterOperators()))
	cmd.Flags().IntVar(&limit, "limit", 0, "Return one page of this size instead of every agent")
	cmd.Flags().StringVar(&pageToken, "page-token", "", "Continue from a cursor reported by a previous page")
	return cmd
}

// --- agents get -----------------------------------------------------------

func newAgentsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "get <agentId>",
		Aliases: []string{"show"},
		Short:   "One AI agent identity in full",
		Long: `One agent with everything the server holds: its name, email, owner, state and
allowed audiences.

Agents are addressed by id only — there is no lookup by name. Use
` + "`jc agents list --filter name:eq:<name>`" + ` to find one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := resolveAgentID("agent", args[0]); err != nil {
				return err
			}
			client, err := agentsClient(agents.AgentEndpoint(args[0]))
			if err != nil {
				return err
			}

			raw, err := client.Get(cmd.Context(), agents.AgentEndpoint(args[0]))
			if err != nil {
				return explainAgentsError(err)
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
}

// --- agents memberof / applications --------------------------------------

func newAgentsMemberOfCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "memberof <agentId>",
		Aliases: []string{"groups"},
		Short:   "Agent groups an agent belongs to",
		Long: `The agent groups an agent belongs to, with their names.

The API returns an id and a type for each group and no name. jc joins the
names from the group catalog, because a list of hex ids answers nothing. When
the catalog cannot be read the ids are still reported, with a warning — a
missing name is cosmetic, a missing membership is not.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := resolveAgentID("agent", args[0]); err != nil {
				return err
			}
			endpoint := agents.AgentMemberOfEndpoint(args[0])
			client, err := agentsClient(endpoint)
			if err != nil {
				return err
			}

			refs, err := readGraph(cmd.Context(), client, endpoint, "memberof")
			if err != nil {
				return explainAgentsError(err)
			}
			named, err := agents.ResolveGraph(cmd.Context(),
				agents.NewCatalog(client, agentsWarn(cmd)), refs)
			if err != nil {
				return err
			}
			return writeNamedRefs(cmd, named, "group", "groups")
		},
	}
}

func newAgentsApplicationsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "applications <agentId>",
		Aliases: []string{"apps"},
		Short:   "Applications an agent can reach",
		Long: `The applications an agent can reach, with their names and the route by which
it reaches them.

An agent gets to an application through a group, so each row reports which
group granted the access — "via" is empty only for a direct assignment. That
distinction exists nowhere else in the response and is the reason this
command does not simply print what the API returned.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := resolveAgentID("agent", args[0]); err != nil {
				return err
			}
			endpoint := agents.AgentApplicationsEndpoint(args[0])
			client, err := agentsClient(endpoint)
			if err != nil {
				return err
			}

			refs, err := readGraph(cmd.Context(), client, endpoint, "applications")
			if err != nil {
				return explainAgentsError(err)
			}
			named, err := agents.ResolveGraph(cmd.Context(),
				agents.NewCatalog(client, agentsWarn(cmd)), refs)
			if err != nil {
				return err
			}
			return writeNamedRefs(cmd, named, "application", "applications")
		},
	}
}

func readGraph(ctx context.Context, client *api.V2Client, endpoint, what string) ([]agents.GraphRef, error) {
	raw, err := agents.GetBareArray(ctx, client, endpoint, nil)
	if err != nil {
		return nil, err
	}
	return agents.ParseGraph(raw, what)
}

// --- agents credentials ---------------------------------------------------

func newAgentsCredentialsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "credentials",
		Aliases: []string{"credential", "creds"},
		Short:   "An agent's inbound OAuth credentials",
		Long: `The OAuth client credentials an agent uses to authenticate to JumpCloud.
"Inbound" is the API's own word, and distinguishes these from any token the
agent uses to reach something else.

Listing them reveals no secret. ` + agents.SecretIsShowOnce + `.`,
	}
	cmd.AddCommand(
		newAgentsCredentialsListCmd(),
		newAgentsCredentialsCreateCmd(),
		newAgentsCredentialsUpdateCmd(),
		newAgentsCredentialsDeleteCmd(),
	)
	return cmd
}

func newAgentsCredentialsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list <agentId>",
		Aliases: []string{"ls"},
		Short:   "List an agent's credentials",
		Long: `An agent's OAuth credentials: client id, authentication type, status, and
when each was created, last changed and expires.

NO SECRET IS RETURNED, by this command or any other read. ` +
			agents.SecretIsShowOnce + `, so an expired or lost credential is replaced
rather than recovered.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := resolveAgentID("agent", args[0]); err != nil {
				return err
			}
			endpoint := agents.AgentCredentialsEndpoint(args[0])
			client, err := agentsClient(endpoint)
			if err != nil {
				return err
			}

			raw, err := client.Get(cmd.Context(), endpoint)
			if err != nil {
				return explainAgentsError(err)
			}
			rows, err := agents.ParseCredentials(raw)
			if err != nil {
				return err
			}
			// No read returns a secret today. Sanitised anyway, because
			// these rows go straight to generic output in every format jc
			// supports, so an API that started including one would leak it
			// everywhere at once.
			rows = agents.SanitizeCredentials(rows)

			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"clientId", "authType", "status", "expiresAt", "createdAt"}
			if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				writeNounFooter(cmd, len(rows), "credential", "credentials")
			}
			return nil
		},
	}
}

// --- agent-groups list / get ---------------------------------------------

func newAgentGroupsListCmd() *cobra.Command {
	var filters []string
	var limit int
	var sortBy string

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List AI agent groups",
		Long: `List the org's agent groups.

--filter accepts ` + fmtList(agents.GroupFilterFields()) + ` with the ` +
			fmtList(agents.FilterOperators()) + ` operators. A field this endpoint does
not support is rejected here rather than sent, because the server answers one
with an internal error that names nothing an operator typed.

--sort works on this listing, unlike the agents listing.`,
		Example: `  jc agent-groups list -t
  jc agent-groups list --sort -name
  jc agent-groups list --filter name:eq:"SF Agent"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, f := range filters {
				if err := agents.ValidateGroupFilter(f); err != nil {
					return err
				}
			}

			client, err := agentsClient(agents.GroupsEndpoint)
			if err != nil {
				return err
			}

			rows, err := agents.ListGroups(cmd.Context(), client, filters, limit, sortBy)
			if err != nil {
				return explainAgentsError(err)
			}

			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"id", "name", "description", "type"}
			if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				// A plain count, not "N of M": this endpoint returns a bare
				// array with no total, so there is no M to report honestly.
				writeNounFooter(cmd, len(rows), "agent group", "agent groups")
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&filters, "filter", nil,
		"Filter as field:operator:value (repeatable, combined with AND). Fields: "+
			fmtList(agents.GroupFilterFields())+"; operators: "+fmtList(agents.FilterOperators()))
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum groups to return")
	cmd.Flags().StringVar(&sortBy, "sort", "", "Sort field; prefix with - for descending")
	return cmd
}

func newAgentGroupsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "get <groupId>",
		Aliases: []string{"show"},
		Short:   "One AI agent group in full",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := resolveAgentID("agent group", args[0]); err != nil {
				return err
			}
			client, err := agentsClient(agents.GroupEndpoint(args[0]))
			if err != nil {
				return err
			}

			raw, err := client.Get(cmd.Context(), agents.GroupEndpoint(args[0]))
			if err != nil {
				return explainAgentsError(err)
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
}

// --- agent-groups members / associations ---------------------------------

func newAgentGroupsMembersCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "members <groupId>",
		Aliases: []string{"member"},
		Short:   "Agents in a group",
		Long: `The agents in a group, with their names.

As with every association read in this area, the API returns ids and types
and no names; jc joins them from the agent catalog and warns if it cannot.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGroupMembership(cmd, args[0], agents.GroupMembersEndpoint(args[0]),
				"", "members", "agent", "agents")
		},
	}
}

func newAgentGroupsAssociationsCmd() *cobra.Command {
	var target string

	cmd := &cobra.Command{
		Use:     "associations <groupId>",
		Aliases: []string{"association", "apps"},
		Short:   "Applications a group grants",
		Long: `The applications an agent group is associated with.

The API requires a target type on this call and the spec does not say so;
jc supplies it. There is only one valid value — ` + agents.AssociationTargetApplication +
			` — because agent groups reach applications and nothing else.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q, err := agents.AssociationsQuery(target)
			if err != nil {
				return err
			}
			return runGroupMembership(cmd, args[0], agents.GroupAssociationsEndpoint(args[0]),
				q.Encode(), "associations", "application", "applications")
		},
	}
	cmd.Flags().StringVar(&target, "target", agents.AssociationTargetApplication,
		"Association target type: "+fmtList(agents.AssociationTargets))
	return cmd
}

// runGroupMembership is the shared body of `members` and `associations`:
// both return the same membership shape and both need their ids named.
func runGroupMembership(cmd *cobra.Command, groupID, endpoint, rawQuery, what, singular, plural string) error {
	if err := resolveAgentID("agent group", groupID); err != nil {
		return err
	}
	client, err := agentsClient(endpoint)
	if err != nil {
		return err
	}

	path := endpoint
	if rawQuery != "" {
		path += "?" + rawQuery
	}
	raw, err := client.Get(cmd.Context(), path)
	if err != nil {
		return explainAgentsError(err)
	}
	refs, err := agents.ParseMembers(raw, what)
	if err != nil {
		return err
	}
	named, err := agents.ResolveMembers(cmd.Context(),
		agents.NewCatalog(client, agentsWarn(cmd)), refs)
	if err != nil {
		return err
	}
	return writeNamedRefs(cmd, named, singular, plural)
}

// fmtList renders a set of accepted values for help text.
func fmtList(values []string) string {
	switch len(values) {
	case 0:
		return "(none)"
	case 1:
		return values[0]
	}
	out := ""
	for i, v := range values {
		switch {
		case i == 0:
			out = v
		case i == len(values)-1:
			out += " or " + v
		default:
			out += ", " + v
		}
	}
	return out
}
