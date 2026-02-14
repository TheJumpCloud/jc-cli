package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
)

// graphDefaultFields is the default field subset shown for graph association output.
// Associations are flattened from {"to":{"type":..,"id":..}} to {"type":..,"id":..}.
var graphDefaultFields = []string{"type", "id"}

// validSourceTypes lists the resource types that can be used as --from sources.
var validSourceTypes = []string{
	"user", "device", "user_group", "device_group", "application",
}

// validTargetsBySource maps each source type to its allowed --to target types.
// Discovered via live JumpCloud V2 API probing — invalid combos return HTTP 400.
var validTargetsBySource = map[string][]string{
	"user":         {"active_directory", "application", "g_suite", "idp_routing_policy", "ldap_server", "office_365", "password_manager_item", "radius_server", "system", "system_group"},
	"device":       {"command", "policy", "policy_group", "user", "user_group"},
	"user_group":   {"active_directory", "application", "g_suite", "idp_routing_policy", "ldap_server", "office_365", "password_manager_item", "radius_server", "system", "system_group"},
	"device_group": {"command", "policy", "policy_group", "user", "user_group"},
	"application":  {"user", "user_group"},
}

// targetToAPIParam maps user-friendly target aliases to the actual V2 API parameter values.
// Targets not in this map pass through unchanged.
var targetToAPIParam = map[string]string{
	"device":       "system",
	"device_group": "system_group",
}

// resourceTypeConfig maps user-friendly source type names to V2 API endpoint prefixes
// and the resolution function needed to resolve names to IDs.
type graphSourceConfig struct {
	// endpointPrefix is the V2 API path prefix (e.g., "/users", "/systems").
	endpointPrefix string
	// resolveFunc resolves a name-or-id to a JumpCloud ID.
	resolveFunc func(ctx context.Context, identifier string) (string, error)
}

func newGraphCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Traverse JumpCloud graph associations",
		Long:  "Traverse associations between JumpCloud resources (users, devices, groups, apps, etc.).",
	}

	cmd.AddCommand(newGraphTraverseCmd())

	return cmd
}

func newGraphTraverseCmd() *cobra.Command {
	var (
		fromFlag string
		toFlag   string
	)

	cmd := &cobra.Command{
		Use:   "traverse --from <type>:<name-or-id> --to <target_type>",
		Short: "Traverse associations between resources",
		Long: `Traverse JumpCloud graph associations between resources.

The --from flag specifies the source resource as type:name-or-id.
The --to flag specifies the target resource type to find associations for.

Source types: user, device, user_group, device_group, application

Valid targets per source type:
  user:         active_directory, application, g_suite, ldap_server, office_365,
                radius_server, system (device), system_group (device_group)
  device:       command, policy, policy_group, user, user_group
  user_group:   active_directory, application, g_suite, ldap_server, office_365,
                radius_server, system (device), system_group (device_group)
  device_group: command, policy, policy_group, user, user_group
  application:  user, user_group

"device" is an alias for "system" and "device_group" is an alias for "system_group".

Examples:
  jc graph traverse --from user:jdoe --to system
  jc graph traverse --from device:JDOE-MBP --to user_group
  jc graph traverse --from user_group:Engineering --to application
  jc graph traverse --from device_group:Servers --to command
  jc graph traverse --from application:Slack --to user_group`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGraphTraverse(cmd, fromFlag, toFlag)
		},
	}

	cmd.Flags().StringVar(&fromFlag, "from", "", "Source resource as type:name-or-id (e.g., user:jdoe)")
	cmd.Flags().StringVar(&toFlag, "to", "", "Target resource type (e.g., user_group)")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")

	return cmd
}

func runGraphTraverse(cmd *cobra.Command, from, to string) error {
	// Parse --from into type and identifier.
	sourceType, identifier, err := parseFromFlag(from)
	if err != nil {
		return err
	}

	// Validate --to target type against allowed targets for this source.
	if !isValidTargetType(sourceType, to) {
		return fmt.Errorf("invalid target type %q for source %q. Valid targets for %s: %s",
			to, sourceType, sourceType, strings.Join(validTargetsBySource[sourceType], ", "))
	}

	// Map user-friendly target aliases to API parameter values.
	apiTarget := to
	if mapped, ok := targetToAPIParam[to]; ok {
		apiTarget = mapped
	}

	ctx := cmd.Context()

	// Build the source config and resolve the identifier to an ID.
	sourceCfg, err := getSourceConfig(sourceType)
	if err != nil {
		return err
	}

	id, err := sourceCfg.resolveFunc(ctx, identifier)
	if err != nil {
		return err
	}

	// Build the V2 graph associations endpoint.
	// Format: /v2/{resource_type}/{id}/associations?targets={target_type}
	endpoint := sourceCfg.endpointPrefix + "/" + id + "/associations?targets=" + apiTarget

	v2Client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := v2Client.ListAll(ctx, endpoint, api.V2ListOptions{})
	if err != nil {
		return err
	}

	// Flatten nested association objects: {"to":{"type":..,"id":..}} → {"type":..,"id":..}
	data := flattenAssociations(result.Data)

	opts := output.CurrentOptions()
	opts.DefaultFields = graphDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(data))
	}

	return nil
}

// flattenAssociations transforms graph association objects from nested form
// {"to":{"type":"...","id":"..."}} to flat form {"type":"...","id":"..."}.
// Non-conforming objects are passed through unchanged.
func flattenAssociations(data []json.RawMessage) []json.RawMessage {
	result := make([]json.RawMessage, 0, len(data))
	for _, raw := range data {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			result = append(result, raw)
			continue
		}

		toRaw, ok := m["to"]
		if !ok {
			result = append(result, raw)
			continue
		}

		var toObj map[string]json.RawMessage
		if err := json.Unmarshal(toRaw, &toObj); err != nil {
			result = append(result, raw)
			continue
		}

		// Build flat object with to.type and to.id promoted to top level.
		// Preserve any other top-level keys besides "to".
		flat := make(map[string]json.RawMessage)
		for k, v := range m {
			if k != "to" {
				flat[k] = v
			}
		}
		for k, v := range toObj {
			flat[k] = v
		}

		out, err := json.Marshal(flat)
		if err != nil {
			result = append(result, raw)
			continue
		}
		result = append(result, out)
	}
	return result
}

// parseFromFlag splits a "type:identifier" string into its components.
func parseFromFlag(from string) (string, string, error) {
	parts := strings.SplitN(from, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid --from format %q. Expected type:name-or-id (e.g., user:jdoe)", from)
	}

	sourceType := parts[0]
	if !isValidSourceType(sourceType) {
		return "", "", fmt.Errorf("invalid source type %q. Valid types: %s",
			sourceType, strings.Join(validSourceTypes, ", "))
	}

	return sourceType, parts[1], nil
}

// getSourceConfig returns the graph source configuration for a given resource type.
func getSourceConfig(sourceType string) (*graphSourceConfig, error) {
	switch sourceType {
	case "user":
		return &graphSourceConfig{
			endpointPrefix: "/users",
			resolveFunc: func(ctx context.Context, identifier string) (string, error) {
				client, err := newV1Client()
				if err != nil {
					return "", err
				}
				return resolveUser(ctx, client, identifier)
			},
		}, nil
	case "device":
		return &graphSourceConfig{
			endpointPrefix: "/systems",
			resolveFunc: func(ctx context.Context, identifier string) (string, error) {
				client, err := newV1Client()
				if err != nil {
					return "", err
				}
				return resolveDevice(ctx, client, identifier)
			},
		}, nil
	case "user_group":
		return &graphSourceConfig{
			endpointPrefix: "/usergroups",
			resolveFunc: func(ctx context.Context, identifier string) (string, error) {
				client, err := newV2Client()
				if err != nil {
					return "", err
				}
				return resolveUserGroup(ctx, client, identifier)
			},
		}, nil
	case "device_group":
		return &graphSourceConfig{
			endpointPrefix: "/systemgroups",
			resolveFunc: func(ctx context.Context, identifier string) (string, error) {
				client, err := newV2Client()
				if err != nil {
					return "", err
				}
				return resolveDeviceGroup(ctx, client, identifier)
			},
		}, nil
	case "application":
		return &graphSourceConfig{
			endpointPrefix: "/applications",
			resolveFunc: func(ctx context.Context, identifier string) (string, error) {
				client, err := newV1Client()
				if err != nil {
					return "", err
				}
				return resolveApp(ctx, client, identifier)
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported source type %q. Valid types: %s",
			sourceType, strings.Join(validSourceTypes, ", "))
	}
}

// isValidSourceType checks if the given type is a valid graph source type.
func isValidSourceType(t string) bool {
	for _, v := range validSourceTypes {
		if t == v {
			return true
		}
	}
	return false
}

// isValidTargetType checks if the given target type is valid for the specified source type.
// User-friendly aliases (device→system, device_group→system_group) are resolved before lookup.
func isValidTargetType(sourceType, t string) bool {
	// Map user-friendly target aliases to API names for validation.
	target := t
	if mapped, ok := targetToAPIParam[t]; ok {
		target = mapped
	}
	targets, ok := validTargetsBySource[sourceType]
	if !ok {
		return false
	}
	for _, v := range targets {
		if target == v {
			return true
		}
	}
	return false
}

// validTargetsForSource returns the display list of valid target types for a source,
// including user-friendly aliases where applicable.
func validTargetsForSource(sourceType string) []string {
	targets, ok := validTargetsBySource[sourceType]
	if !ok {
		return nil
	}
	result := make([]string, len(targets))
	copy(result, targets)
	// Add user-friendly aliases for API names that have them.
	for alias, apiName := range targetToAPIParam {
		for _, t := range targets {
			if t == apiName {
				result = append(result, alias)
				break
			}
		}
	}
	return result
}
