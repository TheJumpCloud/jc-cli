package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/agents"
	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
)

// The ten lifecycle and membership writes for AI Agent Identities.
//
// Two things shape every command here, and both were established by probing:
//
// EVERY WRITE READS BACK. A membership change answers 200 with an EMPTY
// BODY, so there is nothing in the response to confirm; a suspend answers
// with a state that jc checks rather than assumes. Reporting a success that
// was never observed is the failure mode this avoids, and it is the same
// discipline internal/passwordvault needed for a different reason.
//
// THE TWO UPDATE VERBS HAVE OPPOSITE SEMANTICS. `agents update` sends only
// what changed. `agent-groups update` must read the group and merge, because
// that endpoint clears every field it is not given — see
// agents.GroupPatchIsFullReplace.

// --- Shared write plumbing ------------------------------------------------

// agentsConfirm runs the plan/confirm gate shared by every write here.
//
// Returns false when the caller should stop — either because --plan was
// asked for and rendered, or because the operator declined. An error means
// the command cannot safely proceed.
func agentsConfirm(cmd *cobra.Command, p *plan.Plan, prompt string, warning string) (bool, error) {
	if viper.GetBool("plan") {
		return false, renderPlan(cmd, p)
	}
	if !p.Reversible || warning != "" {
		if mustAbortWithoutTTY() {
			return false, fmt.Errorf("%s requires --force or --non-interactive "+
				"(or preview with --plan first)", prompt)
		}
		if shouldConfirm() {
			if warning != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), warning)
			}
			ok, err := askYesNo(cmd, prompt+"?")
			if err != nil {
				return false, err
			}
			if !ok {
				fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
				return false, nil
			}
		}
	}
	return true, nil
}

// --- agents register ------------------------------------------------------

func newAgentsRegisterCmd() *cobra.Command {
	var name, email, owner, description string
	var audiences []string

	cmd := &cobra.Command{
		Use:     "register",
		Aliases: []string{"create", "add"},
		Short:   "Create an AI agent identity",
		Long: `Create an agent identity.

Three things are required and the API spec marks none of them so: --name,
--email and --owner. The email identifies the agent and must not already
belong to another agent in the organization. The owner is the JumpCloud user
id of an ACTIVATED admin who is responsible for the agent — not an email, and
not an admin id.

A new agent starts active and with no credentials. It can reach nothing until
it is put in a group, and it cannot authenticate until a credential is
created for it with "agents credentials create".`,
		Example: `  jc agents register --name build-bot \
      --email build-bot@example.com \
      --owner 5ec7221c4d224c0e309577e7

  # Preview only; makes no request.
  jc agents register --name build-bot --email b@example.com \
      --owner 5ec7221c4d224c0e309577e7 --plan`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := agents.BuildAgentCreate(agents.AgentCreate{
				Name: name, Email: email, OwnerID: owner,
				Description: description, AllowedAudiences: audiences,
			})
			if err != nil {
				return err
			}

			proceed, err := agentsConfirm(cmd, &plan.Plan{
				Action:   "create",
				Resource: "AI agent",
				Target:   name,
				Effects: []string{
					"email: " + email,
					"owner: " + owner,
					"state: active, with no credentials",
				},
				Reversible: true,
			}, "Create agent "+name, "")
			if err != nil || !proceed {
				return err
			}

			client, err := agentsClient(agents.AgentsEndpoint)
			if err != nil {
				return err
			}
			raw, err := client.Create(cmd.Context(), agents.AgentsEndpoint, body)
			if err != nil {
				return explainAgentCreateError(err, email, owner)
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Display name for the agent (required)")
	cmd.Flags().StringVar(&email, "email", "", "Email identifying the agent (required, unique per org)")
	cmd.Flags().StringVar(&owner, "owner", "", "JumpCloud user id of the owning admin (required)")
	cmd.Flags().StringVar(&description, "description", "", "Free-text description")
	cmd.Flags().StringArrayVar(&audiences, "audience", nil,
		"OAuth audience the agent's tokens may target (repeatable)")
	return cmd
}

// explainAgentCreateError names the field that collided. The server's own
// messages are good here by this API's standards, but they name wire fields
// rather than flags, and one of them describes the wrong problem.
func explainAgentCreateError(err error, email, owner string) error {
	switch {
	case agents.EmailConflict(err):
		return fmt.Errorf("another agent in this organization already uses the email %s — "+
			"an agent's email is its identifier and has to be unique. Find it with: "+
			"jc agents list --filter email:eq:%s", email, email)
	case agents.InvalidOwner(err):
		return fmt.Errorf("--owner %s is not an activated user in this organization. It must be "+
			"the id of an ACTIVATED JumpCloud user, which you can check with: jc users get %s",
			owner, owner)
	}
	return explainAgentsError(err)
}

// --- agents update --------------------------------------------------------

func newAgentsUpdateCmd() *cobra.Command {
	var name, email, owner, description string
	var audiences []string

	cmd := &cobra.Command{
		Use:     "update <agentId>",
		Aliases: []string{"set"},
		Short:   "Change an AI agent identity",
		Long: `Change an agent's name, email, owner, description or allowed audiences.

ONLY WHAT YOU PASS IS CHANGED. This endpoint is genuinely partial, unlike
` + "`agent-groups update`" + `, so a flag you leave out is left alone rather
than cleared.

There is no --state flag: ` + agents.AgentStateIsNotPatchable + `.

--description "" clears the description. An empty --name is refused, because
the API has no way to clear a name.`,
		Example: `  jc agents update 6aa85171a36f7d16b20d9b60 --description "nightly builds"

  # Hand the agent to a different owner.
  jc agents update 6aa85171a36f7d16b20d9b60 --owner 5ec7221c4d224c0e309577e7

  # Clear the description.
  jc agents update 6aa85171a36f7d16b20d9b60 --description ""`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := resolveAgentID("agent", args[0]); err != nil {
				return err
			}
			body, err := agents.BuildAgentPatch(
				flagIfChanged(cmd, "name", name),
				flagIfChanged(cmd, "email", email),
				flagIfChanged(cmd, "description", description),
				flagIfChanged(cmd, "owner", owner),
				audiencesIfChanged(cmd, audiences),
			)
			if err != nil {
				return err
			}

			proceed, err := agentsConfirm(cmd, &plan.Plan{
				Action:     "update",
				Resource:   "AI agent",
				Target:     args[0],
				Effects:    planEffectsFromBody(body),
				Reversible: true,
			}, "Update agent "+args[0], "")
			if err != nil || !proceed {
				return err
			}

			client, err := agentsClient(agents.AgentEndpoint(args[0]))
			if err != nil {
				return err
			}
			raw, err := client.Patch(cmd.Context(), agents.AgentEndpoint(args[0]), body)
			if err != nil {
				return explainAgentCreateError(err, email, owner)
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "New display name")
	cmd.Flags().StringVar(&email, "email", "", "New email (unique per org)")
	cmd.Flags().StringVar(&owner, "owner", "", "New owning user's JumpCloud id")
	cmd.Flags().StringVar(&description, "description", "", `New description; "" clears it`)
	cmd.Flags().StringArrayVar(&audiences, "audience", nil,
		"Replace the allowed audiences (repeatable; pass once with \"\" to clear)")
	return cmd
}

// flagIfChanged returns a pointer to the value only when the operator
// actually set the flag.
//
// This is what makes a partial update partial, and it is the reason the
// update path takes pointers rather than strings: "" from an unset flag and
// "" meaning "clear this" are different requests, and a string cannot tell
// them apart.
func flagIfChanged(cmd *cobra.Command, flag, value string) *string {
	if !cmd.Flags().Changed(flag) {
		return nil
	}
	return &value
}

func audiencesIfChanged(cmd *cobra.Command, values []string) *[]string {
	if !cmd.Flags().Changed("audience") {
		return nil
	}
	// A single empty value is how an operator clears a list flag, since
	// --audience with no argument is not expressible.
	if len(values) == 1 && values[0] == "" {
		empty := []string{}
		return &empty
	}
	return &values
}

// planEffectsFromBody renders a request body as plan effects, sorted so the
// preview is stable.
func planEffectsFromBody(body json.RawMessage) []string {
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		return []string{"(could not render the request body)"}
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sortStrings(keys)

	effects := make([]string, 0, len(keys))
	for _, k := range keys {
		v, _ := json.Marshal(fields[k])
		effects = append(effects, k+": "+truncateForPlan(string(v)))
	}
	return effects
}

// --- agents suspend / activate -------------------------------------------

func newAgentsSuspendCmd() *cobra.Command {
	return newAgentStateCmd(
		"suspend", "Suspend an AI agent (stops it authenticating)",
		`Suspend an agent. Its credentials stop working immediately, so anything the
agent was doing stops.

This is the kill switch for a misbehaving agent, and it is reversible:
"agents activate" puts it back. It is classed destructive anyway, on the same
rule that makes `+"`jc users lock`"+` destructive — the harm is to the work in
flight, not to the record.

Suspending an already-suspended agent succeeds and changes nothing, so a
retry after a timeout is safe.`,
		"suspended", false,
		"Suspending an agent stops its credentials working immediately; anything it is doing stops.",
	)
}

func newAgentsActivateCmd() *cobra.Command {
	return newAgentStateCmd(
		"activate", "Return a suspended AI agent to service",
		`Activate an agent, so its credentials work again.

Activating an already-active agent succeeds and changes nothing.`,
		"active", true, "",
	)
}

// newAgentStateCmd builds suspend and activate, which differ only in the
// endpoint they call, the state they expect back, and how loudly they ask.
func newAgentStateCmd(verb, short, long, wantState string, reversible bool, warning string) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " <agentId>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := resolveAgentID("agent", id); err != nil {
				return err
			}

			proceed, err := agentsConfirm(cmd, &plan.Plan{
				Action:     verb,
				Resource:   "AI agent",
				Target:     id,
				Effects:    []string{"state: " + wantState},
				Reversible: reversible,
			}, titleVerb(verb)+" agent "+id, warning)
			if err != nil || !proceed {
				return err
			}

			endpoint := agents.AgentSuspendEndpoint(id)
			if verb == "activate" {
				endpoint = agents.AgentActivateEndpoint(id)
			}

			client, err := agentsClient(endpoint)
			if err != nil {
				return err
			}
			raw, err := client.Create(cmd.Context(), endpoint, nil)
			if err != nil {
				return explainAgentsError(err)
			}

			// The response carries the new state. Reading it rather than
			// assuming it is the difference between reporting what happened
			// and reporting what was requested.
			got, err := agents.ParseAgentState(raw)
			if err != nil {
				return err
			}
			if got != wantState {
				return fmt.Errorf("asked to %s agent %s, and the API reports its state as %q "+
					"rather than %q — the change may not have taken effect", verb, id, got, wantState)
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
}

// --- agents delete --------------------------------------------------------

func newAgentsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <agentId>",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete an AI agent identity (cannot be undone)",
		Long: `Delete an agent identity.

THIS CANNOT BE UNDONE. The agent, its credentials and its group memberships
all go. If you only want to stop the agent working, "agents suspend" does
that reversibly and is almost always what you want instead.

The agent's groups and the applications they grant are NOT affected — only
this agent's membership in them.`,
		Example: `  jc agents delete 6aa85171a36f7d16b20d9b60 --plan
  jc agents delete 6aa85171a36f7d16b20d9b60`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := resolveAgentID("agent", id); err != nil {
				return err
			}

			proceed, err := agentsConfirm(cmd, &plan.Plan{
				Action:   "delete",
				Resource: "AI agent",
				Target:   id,
				Effects: []string{
					"the agent record is removed",
					"its credentials stop working",
					"its group memberships are dropped",
					"irreversible: suspend instead to stop it reversibly",
				},
				Reversible: false,
			}, "Delete agent "+id,
				"Deleting an agent cannot be undone. `jc agents suspend` stops it reversibly.")
			if err != nil || !proceed {
				return err
			}

			client, err := agentsClient(agents.AgentEndpoint(id))
			if err != nil {
				return err
			}
			if _, err := client.Delete(cmd.Context(), agents.AgentEndpoint(id)); err != nil {
				return explainAgentsError(err)
			}

			// A delete answers with an empty body, so success is confirmed
			// by the record being gone rather than by what came back.
			fmt.Fprintf(cmd.ErrOrStderr(), "Deleted agent %s\n", id)
			return nil
		},
	}
}

// --- agent-groups create / update / delete -------------------------------

func newAgentGroupsCreateCmd() *cobra.Command {
	var name, description string

	cmd := &cobra.Command{
		Use:     "create",
		Aliases: []string{"add"},
		Short:   "Create an AI agent group",
		Long: `Create an agent group.

Only --name is required, and group names are unique within the organization.
A new group has no members and grants nothing until it is associated with an
application.`,
		Example: `  jc agent-groups create --name build-agents --description "CI workloads"`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := agents.BuildGroupCreate(name, description)
			if err != nil {
				return err
			}

			proceed, err := agentsConfirm(cmd, &plan.Plan{
				Action:     "create",
				Resource:   "AI agent group",
				Target:     name,
				Effects:    planEffectsFromBody(body),
				Reversible: true,
			}, "Create agent group "+name, "")
			if err != nil || !proceed {
				return err
			}

			client, err := agentsClient(agents.GroupsEndpoint)
			if err != nil {
				return err
			}
			raw, err := client.Create(cmd.Context(), agents.GroupsEndpoint, body)
			if err != nil {
				if agents.GroupAlreadyExists(err) {
					return fmt.Errorf("an agent group called %q already exists — group names are "+
						"unique in an organization. Find it with: jc agent-groups list "+
						"--filter name:eq:%s", name, name)
				}
				return explainAgentsError(err)
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Display name for the group (required, unique per org)")
	cmd.Flags().StringVar(&description, "description", "", "Free-text description")
	return cmd
}

func newAgentGroupsUpdateCmd() *cobra.Command {
	var name, description string

	cmd := &cobra.Command{
		Use:     "update <groupId>",
		Aliases: []string{"set"},
		Short:   "Rename an AI agent group or change its description",
		Long: `Change a group's name or description.

THIS ENDPOINT REPLACES THE GROUP. The API requires the name on every update
and clears any field it is not given, so renaming a group would wipe its
description. jc therefore reads the group first and merges your change onto
what is already there — which means the update is based on the group as it
was a moment ago.

That is the opposite of ` + "`agents update`" + `, which is genuinely partial.
The difference is the server's, not jc's.`,
		Example: `  jc agent-groups update 6aa85141bda774000189bca6 --name ci-agents
  jc agent-groups update 6aa85141bda774000189bca6 --description "" `,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := resolveAgentID("agent group", id); err != nil {
				return err
			}
			if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("description") {
				return fmt.Errorf("nothing to update: pass --name or --description")
			}

			client, err := agentsClient(agents.GroupEndpoint(id))
			if err != nil {
				return err
			}

			// Read first. Not an optimisation — the endpoint clears what it
			// is not given, so there is no correct request that does not
			// include the group's current values.
			existing, err := client.Get(cmd.Context(), agents.GroupEndpoint(id))
			if err != nil {
				return explainAgentsError(err)
			}
			body, err := agents.MergeGroupPatch(existing,
				flagIfChanged(cmd, "name", name),
				flagIfChanged(cmd, "description", description))
			if err != nil {
				return err
			}

			proceed, err := agentsConfirm(cmd, &plan.Plan{
				Action:     "update",
				Resource:   "AI agent group",
				Target:     id,
				Effects:    planEffectsFromBody(body),
				Reversible: true,
			}, "Update agent group "+id, "")
			if err != nil || !proceed {
				return err
			}

			raw, err := client.Patch(cmd.Context(), agents.GroupEndpoint(id), body)
			if err != nil {
				if agents.GroupAlreadyExists(err) {
					return fmt.Errorf("an agent group called %q already exists", name)
				}
				return explainAgentsError(err)
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "New display name")
	cmd.Flags().StringVar(&description, "description", "", `New description; "" clears it`)
	return cmd
}

func newAgentGroupsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <groupId>",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete an AI agent group (cannot be undone)",
		Long: `Delete an agent group.

THIS CANNOT BE UNDONE, and every agent in the group loses the access the
group granted.

The agents themselves are NOT deleted — verified against a live tenant by
deleting a group that still had a member: the group went, the agent stayed,
and its memberships became empty. Nothing cascades.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := resolveAgentID("agent group", id); err != nil {
				return err
			}

			client, err := agentsClient(agents.GroupEndpoint(id))
			if err != nil {
				return err
			}

			// Count the members so the confirmation can say how many agents
			// lose access. A failure to read them is not a reason to refuse
			// the delete, but it is a reason to say the count is unknown
			// rather than to print zero.
			memberNote := "members: unknown (could not be read)"
			if raw, mErr := client.Get(cmd.Context(), agents.GroupMembersEndpoint(id)); mErr == nil {
				if refs, pErr := agents.ParseMembers(raw, "members"); pErr == nil {
					memberNote = fmt.Sprintf("%d agent(s) lose this group's access", len(refs))
				}
			}

			proceed, err := agentsConfirm(cmd, &plan.Plan{
				Action:   "delete",
				Resource: "AI agent group",
				Target:   id,
				Effects: []string{
					memberNote,
					"the agents themselves are not deleted",
					"irreversible",
				},
				Reversible: false,
			}, "Delete agent group "+id,
				"Deleting an agent group cannot be undone, and its agents lose the access it granted.")
			if err != nil || !proceed {
				return err
			}

			// A group delete answers with the record it removed, which is
			// the read-back: the response confirms what went.
			raw, err := client.Delete(cmd.Context(), agents.GroupEndpoint(id))
			if err != nil {
				return explainAgentsError(err)
			}
			if len(raw) > 0 {
				return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Deleted agent group %s\n", id)
			return nil
		},
	}
}

// --- agent-groups membership and associations ----------------------------

func newAgentGroupsAddMemberCmd() *cobra.Command {
	return newMembershipCmd("add-member", agents.OpAdd, "agent",
		"Add an AI agent to a group",
		`Add an agent to a group, which grants it everything the group is associated
with.

Adding an agent that is already in the group is refused rather than ignored,
so this is NOT idempotent — a retry after a timeout can report a failure for
a change that already took effect. jc says so when it happens instead of
reporting a plain error.`)
}

func newAgentGroupsRemoveMemberCmd() *cobra.Command {
	return newMembershipCmd("remove-member", agents.OpRemove, "agent",
		"Remove an AI agent from a group",
		`Remove an agent from a group, revoking everything the group granted it.

The agent is not deleted and keeps any access it has through other groups.`)
}

func newAgentGroupsAssignAppCmd() *cobra.Command {
	return newMembershipCmd("assign-app", agents.OpAdd, agents.AssociationTargetApplication,
		"Grant a group access to an application",
		`Associate an agent group with an application, granting every agent in the
group access to it.

Applications are the only thing an agent group can be associated with.`)
}

func newAgentGroupsUnassignAppCmd() *cobra.Command {
	return newMembershipCmd("unassign-app", agents.OpRemove, agents.AssociationTargetApplication,
		"Revoke a group's access to an application",
		`Remove a group's association with an application, revoking it for every agent
in the group.`)
}

// newMembershipCmd builds the four membership writes. All four are one POST
// with an op field — there is no DELETE on these endpoints — so they differ
// only in which endpoint, which op, and which kind of thing is being linked.
func newMembershipCmd(use, op, objType, short, long string) *cobra.Command {
	var targetID string

	isApp := objType == agents.AssociationTargetApplication
	targetFlag := "agent"
	if isApp {
		targetFlag = "app"
	}

	cmd := &cobra.Command{
		Use:   use + " <groupId>",
		Short: short,
		Long: long + `

A successful membership change returns an EMPTY BODY, so jc reads the
membership back and reports what it actually finds rather than echoing the
request.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			groupID := args[0]
			if err := resolveAgentID("agent group", groupID); err != nil {
				return err
			}
			if strings.TrimSpace(targetID) == "" {
				// Without this, an omitted flag reports `"" is not a
				// JumpCloud object id`, which describes the empty string
				// rather than the missing flag.
				return fmt.Errorf("which %s? pass --%s with its JumpCloud id", objType, targetFlag)
			}
			body, err := agents.MembershipBody(op, targetID, objType)
			if err != nil {
				return err
			}

			verb := "Add"
			reversible := true
			if op == agents.OpRemove {
				verb = "Remove"
				// Revoking access is reversible by re-adding, but it
				// interrupts whatever the agent is doing — the same rule
				// that makes `jc groups remove-member` destructive.
				reversible = false
			}
			proceed, err := agentsConfirm(cmd, &plan.Plan{
				Action:     use,
				Resource:   "AI agent group membership",
				Target:     groupID,
				Effects:    []string{objType + ": " + targetID, "op: " + op},
				Reversible: reversible,
			}, fmt.Sprintf("%s %s %s %s group %s", verb, objType, targetID,
				map[bool]string{true: "to", false: "from"}[op == agents.OpAdd], groupID), "")
			if err != nil || !proceed {
				return err
			}

			endpoint := agents.GroupMembersEndpoint(groupID)
			if isApp {
				endpoint = agents.GroupAssociationsEndpoint(groupID)
			}

			client, err := agentsClient(endpoint)
			if err != nil {
				return err
			}
			if _, err := client.Create(cmd.Context(), endpoint, body); err != nil {
				return agents.ExplainMembershipError(err, op, objType, targetID, groupID)
			}

			return verifyMembership(cmd, client, endpoint, isApp, op, objType, targetID, groupID)
		},
	}
	cmd.Flags().StringVar(&targetID, targetFlag, "",
		"JumpCloud id of the "+objType+" to link (required)")
	return cmd
}

// verifyMembership reads the membership back after a write.
//
// Necessary because the write answers with an empty body: there is nothing
// to inspect, so the only way to report a change rather than a request is to
// go and look. A verification that cannot be performed is reported as such
// rather than assumed to have passed.
func verifyMembership(cmd *cobra.Command, client *api.V2Client, endpoint string, isApp bool,
	op, objType, targetID, groupID string) error {

	path := endpoint
	if isApp {
		q, err := agents.AssociationsQuery("")
		if err != nil {
			return err
		}
		path = agents.WithQuery(endpoint, q)
	}

	raw, err := client.Get(cmd.Context(), path)
	if err != nil {
		return fmt.Errorf("the change was accepted, but reading the membership back to confirm "+
			"it failed: %w — check with `jc agent-groups %s %s`", err,
			map[bool]string{true: "associations", false: "members"}[isApp], groupID)
	}
	refs, err := agents.ParseMembers(raw, "members")
	if err != nil {
		return fmt.Errorf("the change was accepted, but the membership response could not be "+
			"read back to confirm it: %w", err)
	}

	present := false
	for _, r := range refs {
		if r.ID == targetID {
			present = true
			break
		}
	}
	want := op == agents.OpAdd
	if present != want {
		return fmt.Errorf("the API accepted the change, but reading the membership back shows "+
			"%s %s is %s in group %s — the write reported success without taking effect",
			objType, targetID,
			map[bool]string{true: "still present", false: "absent"}[present], groupID)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "── %s %s %s group %s, confirmed by read-back ──\n",
		objType, targetID,
		map[bool]string{true: "added to", false: "removed from"}[want], groupID)
	return nil
}

// titleVerb capitalises a command verb for a confirmation prompt.
func titleVerb(v string) string {
	if v == "" {
		return v
	}
	return strings.ToUpper(v[:1]) + v[1:]
}
