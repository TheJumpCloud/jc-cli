package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerPrompts adds MCP prompts to the server. Prompts are pre-built
// guidance templates that help AI agents approach common JumpCloud workflows.
func (s *Server) registerPrompts() {
	s.registerOnboardUserPrompt()
	s.registerOffboardUserPrompt()
	s.registerSecurityAuditPrompt()
	s.registerFindUserInfoPrompt()
	s.registerTroubleshootAuthPrompt()
	s.registerComplianceCheckPrompt()
}

func (s *Server) registerOnboardUserPrompt() {
	s.mcpServer.AddPrompt(
		&mcp.Prompt{
			Name:        "onboard_user",
			Description: "Guide through complete user onboarding: create user, add to groups, verify setup",
			Arguments: []*mcp.PromptArgument{
				{Name: "username", Description: "Username for the new user", Required: true},
				{Name: "email", Description: "Email address for the new user", Required: true},
				{Name: "firstname", Description: "First name", Required: false},
				{Name: "lastname", Description: "Last name", Required: false},
				{Name: "groups", Description: "Comma-separated group names to add the user to", Required: false},
			},
		},
		func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			args := req.Params.Arguments
			username := args["username"]
			email := args["email"]
			firstname := args["firstname"]
			lastname := args["lastname"]
			groups := args["groups"]

			var steps []string
			steps = append(steps, fmt.Sprintf("1. Create a new JumpCloud user with username=%q and email=%q.", username, email))
			if firstname != "" || lastname != "" {
				steps = append(steps, fmt.Sprintf("   Include firstname=%q and lastname=%q.", firstname, lastname))
			}
			steps = append(steps, "   Use the users_create tool.")
			steps = append(steps, "")

			if groups != "" {
				groupList := strings.Split(groups, ",")
				for i, g := range groupList {
					g = strings.TrimSpace(g)
					steps = append(steps, fmt.Sprintf("%d. Add the user to group %q using groups_add_member with member_type=user.", i+2, g))
				}
				steps = append(steps, "")
			}

			nextStep := len(steps)/2 + 2
			steps = append(steps, fmt.Sprintf("%d. Verify the user was created by calling users_get with identifier=%q.", nextStep, username))
			steps = append(steps, "")
			steps = append(steps, "Important safety notes:")
			steps = append(steps, "- All mutating tools require execute=true to actually perform changes.")
			steps = append(steps, "- Without execute=true, tools return a plan showing what would happen.")
			steps = append(steps, "- Review each plan before executing.")

			return &mcp.GetPromptResult{
				Description: fmt.Sprintf("Onboard user %s (%s)", username, email),
				Messages: []*mcp.PromptMessage{
					{
						Role:    "user",
						Content: &mcp.TextContent{Text: fmt.Sprintf("Please onboard a new JumpCloud user with the following steps:\n\n%s", strings.Join(steps, "\n"))},
					},
				},
			}, nil
		},
	)
}

func (s *Server) registerOffboardUserPrompt() {
	s.mcpServer.AddPrompt(
		&mcp.Prompt{
			Name:        "offboard_user",
			Description: "Guide through user offboarding: lock account, remove group memberships, reset MFA, optionally delete",
			Arguments: []*mcp.PromptArgument{
				{Name: "username", Description: "Username or ID of the user to offboard", Required: true},
				{Name: "delete", Description: "Set to 'true' to delete the user after offboarding (default: false)", Required: false},
			},
		},
		func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			args := req.Params.Arguments
			username := args["username"]
			deleteUser := strings.EqualFold(args["delete"], "true")

			steps := []string{
				fmt.Sprintf("1. Lock the user account %q using users_lock to prevent login immediately.", username),
				"",
				fmt.Sprintf("2. Remove %q from ALL user groups using groups_remove_member with member_type=user.", username),
				"   First, list all groups with groups_list to find memberships.",
				"",
				fmt.Sprintf("3. Reset MFA enrollment for %q using users_reset_mfa.", username),
				"   This ensures the user cannot re-authenticate even if unlocked.",
				"",
				fmt.Sprintf("4. Verify the user's state by calling users_get with identifier=%q.", username),
				"   Confirm: account_locked=true, no group memberships.",
			}

			if deleteUser {
				steps = append(steps, "")
				steps = append(steps, fmt.Sprintf("5. DELETE the user %q using users_delete.", username))
				steps = append(steps, "   WARNING: This is irreversible. The user and all their associations will be permanently removed.")
			}

			steps = append(steps, "")
			steps = append(steps, "Important safety notes:")
			steps = append(steps, "- All mutating tools require execute=true to actually perform changes.")
			steps = append(steps, "- Review the plan output before confirming each destructive step.")
			steps = append(steps, "- Offboarding is partially reversible (unlock, re-add to groups) unless the user is deleted.")

			return &mcp.GetPromptResult{
				Description: fmt.Sprintf("Offboard user %s", username),
				Messages: []*mcp.PromptMessage{
					{
						Role:    "user",
						Content: &mcp.TextContent{Text: fmt.Sprintf("Please offboard JumpCloud user %q with the following steps:\n\n%s", username, strings.Join(steps, "\n"))},
					},
				},
			}, nil
		},
	)
}

func (s *Server) registerSecurityAuditPrompt() {
	s.mcpServer.AddPrompt(
		&mcp.Prompt{
			Name:        "security_audit",
			Description: "Run a security audit: check MFA adoption, find inactive users, review recent auth failures",
			Arguments: []*mcp.PromptArgument{
				{Name: "timerange", Description: "Time range for event queries (e.g. 24h, 7d, 30d). Default: 7d", Required: false},
			},
		},
		func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			timerange := req.Params.Arguments["timerange"]
			if timerange == "" {
				timerange = "7d"
			}

			steps := []string{
				"Perform a JumpCloud security audit by gathering the following data:",
				"",
				"1. MFA Adoption — List all users and check how many have MFA/TOTP enabled.",
				"   Use users_list to get all users.",
				"   Look at the totp_enabled and mfa fields to assess adoption.",
				"",
				fmt.Sprintf("2. Authentication Failures — Query failed SSO auth events from the last %s.", timerange),
				fmt.Sprintf("   Use insights_count with service=sso, event_type=sso_auth_failed, last=%s.", timerange),
				fmt.Sprintf("   Then use insights_query with the same params to see details (limit to 20)."),
				"",
				fmt.Sprintf("3. Recent Admin Activity — Query admin events from the last %s.", timerange),
				fmt.Sprintf("   Use insights_query with service=admin, last=%s, limit=20.", timerange),
				"",
				"4. Inactive Users — List users sorted by last login to find potentially stale accounts.",
				"   Use users_list with sort=-lastLogin and review users with old or missing login dates.",
				"",
				"5. Summary — Present findings in a clear report format:",
				"   - Total users and MFA adoption percentage",
				"   - Number of failed auth events and top affected users",
				"   - Recent admin actions worth noting",
				"   - Users that may need attention (inactive, no MFA)",
			}

			return &mcp.GetPromptResult{
				Description: "Security audit of JumpCloud organization",
				Messages: []*mcp.PromptMessage{
					{
						Role:    "user",
						Content: &mcp.TextContent{Text: strings.Join(steps, "\n")},
					},
				},
			}, nil
		},
	)
}

func (s *Server) registerFindUserInfoPrompt() {
	s.mcpServer.AddPrompt(
		&mcp.Prompt{
			Name:        "find_user_info",
			Description: "Deep user lookup: profile, group memberships, devices, recent auth events",
			Arguments: []*mcp.PromptArgument{
				{Name: "username", Description: "Username or ID of the user to look up", Required: true},
			},
		},
		func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			username := req.Params.Arguments["username"]

			steps := []string{
				fmt.Sprintf("Perform a deep lookup of JumpCloud user %q:", username),
				"",
				fmt.Sprintf("1. User Profile — Get full user details using users_get with identifier=%q.", username),
				"   Report: username, email, name, department, job title, activated, suspended, MFA status.",
				"",
				"2. Group Memberships — Find all groups this user belongs to.",
				"   List all user groups with groups_list, then check membership.",
				"",
				"3. Recent Authentication — Query recent auth events for this user.",
				"   Use insights_query with service=sso, last=7d to find events initiated by this user.",
				"",
				"4. Summary — Present a consolidated user profile:",
				"   - Account status (active/locked/suspended)",
				"   - MFA enrollment status",
				"   - Group memberships",
				"   - Last authentication activity",
			}

			return &mcp.GetPromptResult{
				Description: fmt.Sprintf("User info lookup for %s", username),
				Messages: []*mcp.PromptMessage{
					{
						Role:    "user",
						Content: &mcp.TextContent{Text: strings.Join(steps, "\n")},
					},
				},
			}, nil
		},
	)
}

func (s *Server) registerTroubleshootAuthPrompt() {
	s.mcpServer.AddPrompt(
		&mcp.Prompt{
			Name:        "troubleshoot_auth",
			Description: "Diagnose authentication issues for a user: check account status, recent auth events, MFA state",
			Arguments: []*mcp.PromptArgument{
				{Name: "username", Description: "Username or ID of the user with auth issues", Required: true},
			},
		},
		func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			username := req.Params.Arguments["username"]

			steps := []string{
				fmt.Sprintf("Troubleshoot authentication issues for JumpCloud user %q:", username),
				"",
				fmt.Sprintf("1. Account Status — Check if the account is locked, suspended, or inactive."),
				fmt.Sprintf("   Use users_get with identifier=%q.", username),
				"   Check: account_locked, suspended, activated, password_expired, state.",
				"",
				"2. Recent Auth Failures — Look for failed authentication attempts.",
				"   Use insights_query with service=sso, last=24h to find failed events for this user.",
				"   Look for patterns: wrong password, MFA failures, IP anomalies.",
				"",
				"3. MFA Status — Check MFA/TOTP enrollment.",
				"   From the user profile, check totp_enabled and mfa configuration.",
				"   If MFA is the issue, users_reset_mfa can re-enroll (with execute=true).",
				"",
				"4. Diagnosis — Based on findings, suggest resolution:",
				"   - Account locked → users_unlock (with execute=true)",
				"   - Password expired → users_reset_password (with execute=true)",
				"   - MFA issues → users_reset_mfa (with execute=true)",
				"   - Suspended → requires admin review (check why it was suspended)",
				"",
				"IMPORTANT: All remediation actions require execute=true.",
				"Show the plan first, then confirm with the administrator before executing.",
			}

			return &mcp.GetPromptResult{
				Description: fmt.Sprintf("Auth troubleshooting for %s", username),
				Messages: []*mcp.PromptMessage{
					{
						Role:    "user",
						Content: &mcp.TextContent{Text: strings.Join(steps, "\n")},
					},
				},
			}, nil
		},
	)
}

func (s *Server) registerComplianceCheckPrompt() {
	s.mcpServer.AddPrompt(
		&mcp.Prompt{
			Name:        "compliance_check",
			Description: "Run compliance checks across users, devices, and policies: MFA enforcement, device management, policy coverage",
			Arguments: []*mcp.PromptArgument{
				{Name: "focus", Description: "Area to focus on: mfa, devices, policies, or all (default: all)", Required: false},
			},
		},
		func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			focus := strings.ToLower(req.Params.Arguments["focus"])
			if focus == "" {
				focus = "all"
			}

			var sections []string
			sections = append(sections, "Run a JumpCloud compliance check:")
			sections = append(sections, "")

			if focus == "all" || focus == "mfa" {
				sections = append(sections,
					"## MFA Enforcement",
					"1. List all users with users_list and examine MFA/TOTP status.",
					"2. Calculate the percentage of users with MFA enabled.",
					"3. List users WITHOUT MFA enabled — these are compliance risks.",
					"4. Check admin MFA status with admins list (jc admins list).",
					"",
				)
			}

			if focus == "all" || focus == "devices" {
				sections = append(sections,
					"## Device Management",
					"1. List all devices with devices_list.",
					"2. Identify devices with outdated agents (check agentVersion).",
					"3. Find devices with old lastContact dates (potentially stale).",
					"4. Check if all devices are assigned to at least one group.",
					"",
				)
			}

			if focus == "all" || focus == "policies" {
				sections = append(sections,
					"## Policy Coverage",
					"1. List all policies with policies_list.",
					"2. Review policy types and OS targets for coverage gaps.",
					"3. Identify if key policies exist: password, encryption, firewall, updates.",
					"",
				)
			}

			sections = append(sections,
				"## Summary Report",
				"Present a compliance scorecard with:",
				"- MFA adoption rate (target: 100%)",
				"- Devices with recent contact (target: all within 7 days)",
				"- Policy coverage by OS",
				"- Action items for any findings",
			)

			return &mcp.GetPromptResult{
				Description: "JumpCloud compliance check",
				Messages: []*mcp.PromptMessage{
					{
						Role:    "user",
						Content: &mcp.TextContent{Text: strings.Join(sections, "\n")},
					},
				},
			}, nil
		},
	)
}
