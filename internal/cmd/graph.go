package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
)

// graphDefaultFields is the default field subset shown for graph association output.
// Graph association objects have a nested "to" object containing "type" and "id".
var graphDefaultFields = []string{"to"}

// validSourceTypes lists the resource types that can be used as --from sources.
var validSourceTypes = []string{
	"user", "device", "user_group", "device_group", "application",
}

// validTargetTypes lists the resource types that can be used as --to targets.
var validTargetTypes = []string{
	"user", "system", "user_group", "system_group", "application",
	"policy", "command",
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
Target types: user, system, user_group, system_group, application, policy, command

Examples:
  jc graph traverse --from user:jdoe --to user_group
  jc graph traverse --from device:JDOE-MBP --to system_group
  jc graph traverse --from user_group:Engineering --to application
  jc graph traverse --from user_group:Engineering --to user
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

	// Validate --to target type.
	if !isValidTargetType(to) {
		return fmt.Errorf("invalid target type %q. Valid types: %s",
			to, strings.Join(validTargetTypes, ", "))
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
	endpoint := sourceCfg.endpointPrefix + "/" + id + "/associations?targets=" + to

	v2Client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := v2Client.ListAll(ctx, endpoint, api.V2ListOptions{})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = graphDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
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

// isValidTargetType checks if the given type is a valid graph target type.
func isValidTargetType(t string) bool {
	for _, v := range validTargetTypes {
		if t == v {
			return true
		}
	}
	return false
}
