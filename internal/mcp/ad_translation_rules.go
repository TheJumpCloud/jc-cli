package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/resolve"
	"github.com/klaassen-consulting/jc/internal/transrule"
)

type adTransRuleListInput struct {
	Identifier string   `json:"identifier" jsonschema:"Active Directory domain or ID"`
	Limit      int      `json:"limit,omitempty" jsonschema:"Maximum number of results to return (0 = all)"`
	Sort       string   `json:"sort,omitempty" jsonschema:"Field to sort by. Prefix with - for descending"`
	Filter     []string `json:"filter,omitempty" jsonschema:"Filter expressions (e.g. direction=EXPORT)"`
}

type adTransRuleCreateInput struct {
	Identifier  string   `json:"identifier" jsonschema:"Active Directory domain or ID"`
	Source      string   `json:"source" jsonschema:"Source field path or expression (e.g. firstname)"`
	Destination string   `json:"destination" jsonschema:"Destination attribute path (e.g. givenName)"`
	SourceType  string   `json:"source_type,omitempty" jsonschema:"Source type: path (default), expr, constant, or template"`
	Direction   string   `json:"direction,omitempty" jsonschema:"Direction: export (default) or import"`
	AppliedOn   []string `json:"applied_on,omitempty" jsonschema:"Operations the rule applies on: create, update (default both)"`
	Execute     bool     `json:"execute,omitempty" jsonschema:"Set to true to create. Without this the tool returns a plan."`
}

type adTransRuleUpdateInput struct {
	Identifier  string   `json:"identifier" jsonschema:"Active Directory domain or ID"`
	RuleID      string   `json:"rule_id" jsonschema:"objectId of the translation rule to update"`
	Source      string   `json:"source,omitempty" jsonschema:"New source field path or expression"`
	Destination string   `json:"destination,omitempty" jsonschema:"New destination attribute path"`
	SourceType  string   `json:"source_type,omitempty" jsonschema:"New source type: path, expr, constant, or template"`
	AppliedOn   []string `json:"applied_on,omitempty" jsonschema:"New operations the rule applies on: create, update"`
	Execute     bool     `json:"execute,omitempty" jsonschema:"Set to true to apply the update. Without this the tool returns a plan."`
}

type adTransRuleDeleteInput struct {
	Identifier string `json:"identifier" jsonschema:"Active Directory domain or ID"`
	RuleID     string `json:"rule_id" jsonschema:"objectId of the translation rule to delete"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to delete. Without this the tool returns a plan."`
}

type adTransRuleBulkInput struct {
	Identifier string `json:"identifier" jsonschema:"Active Directory domain or ID"`
	BulkJSON   string `json:"bulk_json" jsonschema:"Raw JSON object with at least one of insertTranslationRules, updateTranslationRules, deleteTranslationRuleObjectIds"`
	Execute    bool   `json:"execute,omitempty" jsonschema:"Set to true to apply. Without this the tool returns a plan."`
}

type adTransRulePreviewInput struct {
	RulesJSON string `json:"rules_json" jsonschema:"Raw JSON array of translation rule objects (or a body wrapped in {\"translationRules\": …} / {\"rules\": …})"`
	User      string `json:"user" jsonschema:"Username, email, or ID of the user to preview against"`
	AD        string `json:"ad,omitempty" jsonschema:"Active Directory domain or ID to preview against"`
}

// fetchTranslationRules lists an AD's translation rules as typed values plus
// the raw JSON for output.
func fetchTranslationRules(ctx context.Context, client *api.V2Client, adID string, opts api.V2ListOptions) ([]transrule.Rule, []json.RawMessage, error) {
	opts.ResponseKey = "rules"
	result, err := client.ListAll(ctx, transrule.Endpoint(adID), opts)
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

func (s *Server) registerADTranslationRuleTools() {
	addTypedTool(s, "ad_translation_rules_list", "List the attribute translation rules of a JumpCloud Active Directory integration. Returns objects with objectId, source, destination, sourceType, direction, appliedOn, editable. Rules with editable=false are JumpCloud defaults.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adTransRuleListInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(listInput{Limit: args.Limit, Sort: args.Sort, Filter: args.Filter})
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			adID, err := r.Resolve(ctx, args.Identifier, resolve.ActiveDirectoryConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			_, raw, err := fetchTranslationRules(ctx, client, adID, opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing translation rules: %v", err)), nil, nil
			}
			return rawListPage(raw)
		},
	)

	addTypedTool(s, "ad_translation_rules_recommendations", "List JumpCloud's catalog of recommended Active Directory translation rules. These carry no objectId and are not attached to any integration — use them as a starting point for ad_translation_rules_create.",
		func(ctx context.Context, req *mcp.CallToolRequest, args listInput) (*mcp.CallToolResult, any, error) {
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			opts, err := buildV2ListOptions(args)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			opts.ResponseKey = "rules"
			result, err := client.ListAll(ctx, transrule.RecommendationEndpoint, opts)
			if err != nil {
				return errorResult(fmt.Sprintf("listing recommended translation rules: %v", err)), nil, nil
			}
			return rawListPage(result.Data)
		},
	)

	addTypedTool(s, "ad_translation_rules_create", "Create an attribute translation rule on a JumpCloud Active Directory integration. Set execute=true to create; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adTransRuleCreateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			sourceType := args.SourceType
			if sourceType == "" {
				sourceType = "path"
			}
			direction := args.Direction
			if direction == "" {
				direction = "export"
			}
			appliedOn := args.AppliedOn
			if len(appliedOn) == 0 {
				appliedOn = []string{"create", "update"}
			}
			st, err := transrule.NormalizeSourceType(sourceType)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			dir, err := transrule.NormalizeDirection(direction)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			ops, err := transrule.NormalizeAppliedOn(appliedOn)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			body := map[string]any{
				"source":      args.Source,
				"destination": args.Destination,
				"sourceType":  st,
				"direction":   dir,
				"appliedOn":   ops,
			}
			if !args.Execute {
				return planResult("create", "AD translation rule", args.Identifier, "", body)
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			adID, err := r.Resolve(ctx, args.Identifier, resolve.ActiveDirectoryConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if _, err := client.Create(ctx, transrule.Endpoint(adID), body); err != nil {
				return errorResult(fmt.Sprintf("creating translation rule: %v", err)), nil, nil
			}
			// The create endpoint returns an empty body, so re-read the list and
			// return the rule that now matches the requested mapping.
			rules, _, err := fetchTranslationRules(ctx, client, adID, api.V2ListOptions{})
			if err == nil {
				for _, rule := range rules {
					if rule.Source == args.Source && rule.Destination == args.Destination {
						res, jerr := jsonResult(rule)
						if jerr != nil {
							return errorResult(jerr.Error()), nil, nil
						}
						return res, nil, nil
					}
				}
			}
			return textResult(fmt.Sprintf("Translation rule %s → %s created.", args.Source, args.Destination)), nil, nil
		},
	)

	addTypedTool(s, "ad_translation_rules_update", "Update an attribute translation rule of a JumpCloud Active Directory integration. The API's PUT is a full replace, so the current rule is fetched and unspecified fields are preserved. A rule's direction cannot be changed, and rules with editable=false are rejected by the API. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adTransRuleUpdateInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			var (
				srcPtr  *string
				dstPtr  *string
				typePtr *string
				ops     []string
			)
			if args.Source != "" {
				srcPtr = &args.Source
			}
			if args.Destination != "" {
				dstPtr = &args.Destination
			}
			if args.SourceType != "" {
				st, err := transrule.NormalizeSourceType(args.SourceType)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				typePtr = &st
			}
			if len(args.AppliedOn) > 0 {
				normalized, err := transrule.NormalizeAppliedOn(args.AppliedOn)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
				ops = normalized
			}
			if srcPtr == nil && dstPtr == nil && typePtr == nil && ops == nil {
				return errorResult("no fields to update: specify at least one of source, destination, source_type, applied_on"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			adID, err := r.Resolve(ctx, args.Identifier, resolve.ActiveDirectoryConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			rules, _, err := fetchTranslationRules(ctx, client, adID, api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing translation rules: %v", err)), nil, nil
			}
			cur, ok := transrule.FindRule(rules, args.RuleID)
			if !ok {
				return errorResult(fmt.Sprintf("translation rule %q not found on Active Directory %q", args.RuleID, args.Identifier)), nil, nil
			}
			body := transrule.UpdateBody(cur, srcPtr, dstPtr, typePtr, ops)
			if !args.Execute {
				return planResult("update", "AD translation rule", args.RuleID, adID, body)
			}
			data, err := client.Update(ctx, transrule.RuleEndpoint(adID, args.RuleID), body)
			if err != nil {
				return errorResult(fmt.Sprintf("updating translation rule: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ad_translation_rules_delete", "Delete an attribute translation rule from a JumpCloud Active Directory integration. Set execute=true to delete; otherwise returns a plan describing the rule's mapping.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adTransRuleDeleteInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			adID, err := r.Resolve(ctx, args.Identifier, resolve.ActiveDirectoryConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			rules, _, err := fetchTranslationRules(ctx, client, adID, api.V2ListOptions{})
			if err != nil {
				return errorResult(fmt.Sprintf("listing translation rules: %v", err)), nil, nil
			}
			cur, ok := transrule.FindRule(rules, args.RuleID)
			if !ok {
				return errorResult(fmt.Sprintf("translation rule %q not found on Active Directory %q", args.RuleID, args.Identifier)), nil, nil
			}
			mapping := fmt.Sprintf("%s → %s", cur.Source, cur.Destination)
			if !args.Execute {
				return planResult("delete", "AD translation rule", mapping, args.RuleID, nil)
			}
			if _, err := client.Delete(ctx, transrule.RuleEndpoint(adID, args.RuleID)); err != nil {
				return errorResult(fmt.Sprintf("deleting translation rule: %v", err)), nil, nil
			}
			return textResult(fmt.Sprintf("Translation rule %q deleted successfully.", mapping)), nil, nil
		},
	)

	addTypedTool(s, "ad_translation_rules_bulk", "Apply a batch of translation-rule insert/update/delete operations to an Active Directory in one request. bulk_json is an object with at least one of insertTranslationRules, updateTranslationRules, deleteTranslationRuleObjectIds. Set execute=true to apply; otherwise returns a plan.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adTransRuleBulkInput) (*mcp.CallToolResult, any, error) {
			if s.readOnly {
				return errorResult("server is in read-only mode"), nil, nil
			}
			body, ops, err := transrule.ParseBulkFile([]byte(args.BulkJSON))
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if !args.Execute {
				return planResult("bulk", "AD translation rules", args.Identifier, "", ops.Summary())
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			r := resolve.NewV2Resolver(client)
			adID, err := r.Resolve(ctx, args.Identifier, resolve.ActiveDirectoryConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			data, err := client.Create(ctx, transrule.BulkEndpoint(adID), body)
			if err != nil {
				return errorResult(fmt.Sprintf("applying bulk translation rules: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)

	addTypedTool(s, "ad_translation_rules_preview", "Preview what a set of translation rules produces for a real JumpCloud user. Read-only: returns sourceUser (before translation) and destinationUser (after). rules_json is an array of rule objects, or a body wrapped in {\"translationRules\": …} so ad_translation_rules_list output round-trips.",
		func(ctx context.Context, req *mcp.CallToolRequest, args adTransRulePreviewInput) (*mcp.CallToolResult, any, error) {
			rules, err := transrule.ParsePreviewRules([]byte(args.RulesJSON))
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			v1, err := newV1ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			userID, err := resolveV1(ctx, v1, args.User, resolve.UserConfig)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			client, err := newV2ClientFunc()
			if err != nil {
				return errorResult(fmt.Sprintf("creating API client: %v", err)), nil, nil
			}
			var adID string
			if args.AD != "" {
				r := resolve.NewV2Resolver(client)
				adID, err = r.Resolve(ctx, args.AD, resolve.ActiveDirectoryConfig)
				if err != nil {
					return errorResult(err.Error()), nil, nil
				}
			}
			data, err := client.Create(ctx, transrule.PreviewEndpoint, transrule.PreviewBody(rules, userID, adID))
			if err != nil {
				return errorResult(fmt.Sprintf("previewing translation rules: %v", err)), nil, nil
			}
			return textResult(string(data)), nil, nil
		},
	)
}
