package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCP_ListPrompts(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}

	if len(result.Prompts) != 6 {
		t.Fatalf("expected 6 prompts, got %d", len(result.Prompts))
	}

	// Verify all expected prompts are registered.
	names := make(map[string]bool)
	for _, p := range result.Prompts {
		names[p.Name] = true
		if p.Description == "" {
			t.Errorf("prompt %s: expected description", p.Name)
		}
	}
	expected := []string{
		"onboard_user",
		"offboard_user",
		"security_audit",
		"find_user_info",
		"troubleshoot_auth",
		"compliance_check",
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("expected prompt %q to be registered", name)
		}
	}
}

func TestMCP_ListPrompts_HasArguments(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}

	// Each prompt should have at least one argument.
	for _, p := range result.Prompts {
		if len(p.Arguments) == 0 {
			t.Errorf("prompt %s: expected at least one argument", p.Name)
		}
	}
}

func TestMCP_Initialize_IncludesPromptCapability(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	initResult := cs.InitializeResult()
	if initResult.Capabilities == nil {
		t.Fatal("expected capabilities")
	}
	if initResult.Capabilities.Prompts == nil {
		t.Fatal("expected prompts capability to be advertised")
	}
}

func TestMCP_GetPrompt_OnboardUser(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name: "onboard_user",
		Arguments: map[string]string{
			"username":  "jdoe",
			"email":     "jdoe@acme.com",
			"firstname": "John",
			"lastname":  "Doe",
			"groups":    "Engineering, Sales",
		},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}

	if result.Description == "" {
		t.Error("expected description")
	}
	if !strings.Contains(result.Description, "jdoe") {
		t.Errorf("expected description to mention username, got %q", result.Description)
	}
	if len(result.Messages) == 0 {
		t.Fatal("expected at least one message")
	}

	msg := result.Messages[0]
	if msg.Role != "user" {
		t.Errorf("expected role 'user', got %q", msg.Role)
	}
	tc, ok := msg.Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", msg.Content)
	}
	if !strings.Contains(tc.Text, "jdoe") {
		t.Error("expected prompt text to contain username")
	}
	if !strings.Contains(tc.Text, "jdoe@acme.com") {
		t.Error("expected prompt text to contain email")
	}
	if !strings.Contains(tc.Text, "Engineering") {
		t.Error("expected prompt text to contain group name")
	}
	if !strings.Contains(tc.Text, "Sales") {
		t.Error("expected prompt text to contain second group name")
	}
	if !strings.Contains(tc.Text, "users_create") {
		t.Error("expected prompt text to reference users_create tool")
	}
	if !strings.Contains(tc.Text, "execute=true") {
		t.Error("expected prompt text to mention execute=true safety note")
	}
}

func TestMCP_GetPrompt_OnboardUser_MinimalArgs(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name: "onboard_user",
		Arguments: map[string]string{
			"username": "jdoe",
			"email":    "jdoe@acme.com",
		},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}

	tc := result.Messages[0].Content.(*mcp.TextContent)
	if !strings.Contains(tc.Text, "jdoe") {
		t.Error("expected prompt text to contain username")
	}
	// Without groups, should not mention group adding steps.
	if strings.Contains(tc.Text, "groups_add_member") {
		t.Error("expected no group steps when groups not provided")
	}
}

func TestMCP_GetPrompt_OffboardUser(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name: "offboard_user",
		Arguments: map[string]string{
			"username": "jdoe",
		},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}

	tc := result.Messages[0].Content.(*mcp.TextContent)
	if !strings.Contains(tc.Text, "Lock") {
		t.Error("expected prompt to mention locking")
	}
	if !strings.Contains(tc.Text, "users_lock") {
		t.Error("expected prompt to reference users_lock tool")
	}
	if !strings.Contains(tc.Text, "groups_remove_member") || !strings.Contains(tc.Text, "groups_list") {
		t.Error("expected prompt to reference group removal")
	}
	if !strings.Contains(tc.Text, "users_reset_mfa") {
		t.Error("expected prompt to reference MFA reset")
	}
	// Without delete=true, should not mention deletion.
	if strings.Contains(tc.Text, "DELETE") {
		t.Error("expected no deletion step when delete not set")
	}
}

func TestMCP_GetPrompt_OffboardUser_WithDelete(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name: "offboard_user",
		Arguments: map[string]string{
			"username": "jdoe",
			"delete":   "true",
		},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}

	tc := result.Messages[0].Content.(*mcp.TextContent)
	if !strings.Contains(tc.Text, "DELETE") {
		t.Error("expected prompt to include delete step")
	}
	if !strings.Contains(tc.Text, "users_delete") {
		t.Error("expected prompt to reference users_delete tool")
	}
	if !strings.Contains(tc.Text, "irreversible") {
		t.Error("expected prompt to warn about irreversibility")
	}
}

func TestMCP_GetPrompt_SecurityAudit(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      "security_audit",
		Arguments: map[string]string{},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}

	tc := result.Messages[0].Content.(*mcp.TextContent)
	// Default timerange is 7d.
	if !strings.Contains(tc.Text, "7d") {
		t.Error("expected default timerange of 7d")
	}
	if !strings.Contains(tc.Text, "MFA") {
		t.Error("expected MFA check section")
	}
	if !strings.Contains(tc.Text, "insights_count") || !strings.Contains(tc.Text, "insights_query") {
		t.Error("expected references to insights tools")
	}
	if !strings.Contains(tc.Text, "users_list") {
		t.Error("expected reference to users_list tool")
	}
}

func TestMCP_GetPrompt_SecurityAudit_CustomTimeRange(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name: "security_audit",
		Arguments: map[string]string{
			"timerange": "30d",
		},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}

	tc := result.Messages[0].Content.(*mcp.TextContent)
	if !strings.Contains(tc.Text, "30d") {
		t.Error("expected custom timerange of 30d")
	}
}

func TestMCP_GetPrompt_FindUserInfo(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name: "find_user_info",
		Arguments: map[string]string{
			"username": "jdoe",
		},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}

	if !strings.Contains(result.Description, "jdoe") {
		t.Errorf("expected description to mention username, got %q", result.Description)
	}

	tc := result.Messages[0].Content.(*mcp.TextContent)
	if !strings.Contains(tc.Text, "users_get") {
		t.Error("expected reference to users_get tool")
	}
	if !strings.Contains(tc.Text, "groups_list") {
		t.Error("expected reference to group lookup")
	}
	if !strings.Contains(tc.Text, "insights_query") {
		t.Error("expected reference to auth event query")
	}
}

func TestMCP_GetPrompt_TroubleshootAuth(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name: "troubleshoot_auth",
		Arguments: map[string]string{
			"username": "jdoe",
		},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}

	tc := result.Messages[0].Content.(*mcp.TextContent)
	if !strings.Contains(tc.Text, "jdoe") {
		t.Error("expected username in prompt text")
	}
	if !strings.Contains(tc.Text, "users_get") {
		t.Error("expected reference to users_get")
	}
	if !strings.Contains(tc.Text, "account_locked") {
		t.Error("expected mention of account_locked status")
	}
	if !strings.Contains(tc.Text, "users_unlock") {
		t.Error("expected remediation: users_unlock")
	}
	if !strings.Contains(tc.Text, "users_reset_password") {
		t.Error("expected remediation: users_reset_password")
	}
	if !strings.Contains(tc.Text, "users_reset_mfa") {
		t.Error("expected remediation: users_reset_mfa")
	}
	if !strings.Contains(tc.Text, "execute=true") {
		t.Error("expected safety note about execute=true")
	}
}

func TestMCP_GetPrompt_ComplianceCheck_All(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      "compliance_check",
		Arguments: map[string]string{},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}

	tc := result.Messages[0].Content.(*mcp.TextContent)
	// Default focus is "all" — should include all sections.
	if !strings.Contains(tc.Text, "MFA Enforcement") {
		t.Error("expected MFA section")
	}
	if !strings.Contains(tc.Text, "Device Management") {
		t.Error("expected device section")
	}
	if !strings.Contains(tc.Text, "Policy Coverage") {
		t.Error("expected policy section")
	}
	if !strings.Contains(tc.Text, "Summary Report") {
		t.Error("expected summary section")
	}
}

func TestMCP_GetPrompt_ComplianceCheck_MFAOnly(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name: "compliance_check",
		Arguments: map[string]string{
			"focus": "mfa",
		},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}

	tc := result.Messages[0].Content.(*mcp.TextContent)
	if !strings.Contains(tc.Text, "MFA Enforcement") {
		t.Error("expected MFA section")
	}
	if strings.Contains(tc.Text, "Device Management") {
		t.Error("expected no device section when focus=mfa")
	}
	if strings.Contains(tc.Text, "Policy Coverage") {
		t.Error("expected no policy section when focus=mfa")
	}
}

func TestMCP_GetPrompt_ComplianceCheck_DevicesOnly(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name: "compliance_check",
		Arguments: map[string]string{
			"focus": "devices",
		},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}

	tc := result.Messages[0].Content.(*mcp.TextContent)
	if strings.Contains(tc.Text, "MFA Enforcement") {
		t.Error("expected no MFA section when focus=devices")
	}
	if !strings.Contains(tc.Text, "Device Management") {
		t.Error("expected device section")
	}
}

func TestMCP_GetPrompt_ComplianceCheck_PoliciesOnly(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	result, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name: "compliance_check",
		Arguments: map[string]string{
			"focus": "policies",
		},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}

	tc := result.Messages[0].Content.(*mcp.TextContent)
	if strings.Contains(tc.Text, "MFA Enforcement") {
		t.Error("expected no MFA section when focus=policies")
	}
	if strings.Contains(tc.Text, "Device Management") {
		t.Error("expected no device section when focus=policies")
	}
	if !strings.Contains(tc.Text, "Policy Coverage") {
		t.Error("expected policy section")
	}
}

func TestMCP_GetPrompt_NotFound(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()
	_, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
		Name: "nonexistent_prompt",
	})
	if err == nil {
		t.Fatal("expected error for unknown prompt")
	}
}

func TestMCP_GetPrompt_AllPromptsHaveUserRole(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()

	// Test each prompt returns messages with role=user.
	tests := []struct {
		name string
		args map[string]string
	}{
		{"onboard_user", map[string]string{"username": "test", "email": "test@test.com"}},
		{"offboard_user", map[string]string{"username": "test"}},
		{"security_audit", map[string]string{}},
		{"find_user_info", map[string]string{"username": "test"}},
		{"troubleshoot_auth", map[string]string{"username": "test"}},
		{"compliance_check", map[string]string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
				Name:      tt.name,
				Arguments: tt.args,
			})
			if err != nil {
				t.Fatalf("GetPrompt %s: %v", tt.name, err)
			}
			for i, msg := range result.Messages {
				if msg.Role != "user" {
					t.Errorf("message %d: expected role 'user', got %q", i, msg.Role)
				}
			}
		})
	}
}

func TestMCP_GetPrompt_AllPromptsHaveDescription(t *testing.T) {
	setupTest(t)
	_, cs := connectTestServer(t, Options{})

	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]string
	}{
		{"onboard_user", map[string]string{"username": "test", "email": "test@test.com"}},
		{"offboard_user", map[string]string{"username": "test"}},
		{"security_audit", map[string]string{}},
		{"find_user_info", map[string]string{"username": "test"}},
		{"troubleshoot_auth", map[string]string{"username": "test"}},
		{"compliance_check", map[string]string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
				Name:      tt.name,
				Arguments: tt.args,
			})
			if err != nil {
				t.Fatalf("GetPrompt %s: %v", tt.name, err)
			}
			if result.Description == "" {
				t.Errorf("expected non-empty description for prompt %s", tt.name)
			}
		})
	}
}
