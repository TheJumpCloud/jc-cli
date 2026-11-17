package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/aigateway"
	"github.com/klaassen-consulting/jc/internal/output"
)

// aiGatewayServerFields is the default column set for servers.
var aiGatewayServerFields = []string{"object_id", "name", "prefix", "auth_type", "target_url", "application_object_id"}

// newAIGatewayCmd builds `jc ai-gateway`: read-only access to the AI Gateway
// (MCP Gateway) configuration through an undocumented preview API. See
// internal/aigateway for the contract.
func newAIGatewayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ai-gateway",
		Aliases: []string{"aigw", "mcp-gateway"},
		Short:   "View the AI Gateway: MCP servers, redirect URIs, catalog (preview)",
		Long: `View the JumpCloud AI Gateway (MCP Gateway) configuration: the MCP servers
it fronts, the OAuth redirect URIs it accepts, and its catalog of
preconfigured connectors.

PREVIEW. These endpoints are not in JumpCloud's published API reference. They
are the ones the Admin Portal's AI Gateway pages use, and they accept an
ordinary admin API key, but JumpCloud makes no promise about their stability.
jc checks every response and fails with an explicit error, rather than
showing an empty or partial result, when the shape is not the one it knows.

READ-ONLY. Servers and redirect URIs are changed in the Admin Portal
(Access > AI Gateway). Credentials are never shown: in a server's auth_config
only known-safe values (client id, scopes, header name, hints) are printed and
every other value is replaced with "[redacted]".

Whether the gateway is switched on for the org: ` + "`jc org features get mcp`" + `.
Which apps users see as AI Ready: ` + "`jc apps list --ai-ready true`" + `.`,
	}

	servers := &cobra.Command{
		Use:     "servers",
		Aliases: []string{"server"},
		Short:   "MCP servers the gateway fronts",
	}

	var appFlag string
	serversList := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List AI Gateway servers",
		Long: `List the MCP servers configured in the AI Gateway, each linked to an SSO
application. --app shows only the servers of one application (by name or id).`,
		Example: `  jc ai-gateway servers list -t
  jc ai-gateway servers list --app Salesforce`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAIGatewayServersList(cmd, appFlag)
		},
	}
	serversList.Flags().StringVar(&appFlag, "app", "", "Only servers linked to this application (name or ID)")

	serversGet := &cobra.Command{
		Use:     "get <name-or-id>",
		Short:   "Get one AI Gateway server",
		Example: `  jc ai-gateway servers get Salesforce`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAIGatewayServersGet(cmd, args[0])
		},
	}
	servers.AddCommand(serversList, serversGet)

	redirectURIs := &cobra.Command{
		Use:     "redirect-uris",
		Aliases: []string{"redirect-uri"},
		Short:   "OAuth redirect URIs the gateway accepts",
	}
	redirectURIs.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the OAuth redirect URIs MCP clients may use",
		Example: `  jc ai-gateway redirect-uris list -t`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			uris, err := aigateway.ListRedirectURIs(cmd.Context(), client)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"object_id", "url", "created_at"}
			return output.WriteList(cmd.OutOrStdout(), uris, opts)
		},
	})

	catalog := &cobra.Command{
		Use:   "catalog",
		Short: "Catalog of preconfigured MCP connectors",
	}
	catalog.AddCommand(&cobra.Command{
		Use:   "resolve <app-name-or-id>",
		Short: "Show whether an application is linked to a catalog connector",
		Long: `Show whether an SSO application is linked to a catalog connector, and which
one. An app without one answers associated: false with a reason.`,
		Example: `  jc ai-gateway catalog resolve Salesforce`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, err := resolveAppID(cmd, args[0])
			if err != nil {
				return err
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := aigateway.Resolve(cmd.Context(), client, appID)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}, &cobra.Command{
		Use:     "connector <connector-id>",
		Short:   "Get one catalog connector's definition",
		Long:    "Get a catalog connector by id (for example \"salesforce\", as reported by `catalog resolve`). The catalog has no list endpoint.",
		Example: `  jc ai-gateway catalog connector salesforce`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := aigateway.GetConnector(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})

	cmd.AddCommand(servers, redirectURIs, catalog)
	return cmd
}

func runAIGatewayServersList(cmd *cobra.Command, app string) error {
	var appID string
	if app != "" {
		id, err := resolveAppID(cmd, app)
		if err != nil {
			return err
		}
		appID = id
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	servers, err := aigateway.ListServers(cmd.Context(), client)
	if err != nil {
		return err
	}
	if appID != "" {
		servers = aigateway.FilterByApp(servers, appID)
	}
	opts := output.CurrentOptions()
	opts.DefaultFields = aiGatewayServerFields
	return output.WriteList(cmd.OutOrStdout(), withAuthType(servers), opts)
}

func runAIGatewayServersGet(cmd *cobra.Command, ident string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	var server json.RawMessage
	if aigateway.ValidID(ident) {
		server, err = aigateway.GetServer(cmd.Context(), client, ident)
	} else {
		var all []json.RawMessage
		all, err = aigateway.ListServers(cmd.Context(), client)
		if err == nil {
			server, err = aigateway.FindServer(all, ident)
		}
	}
	if err != nil {
		return err
	}
	return output.WriteSingle(cmd.OutOrStdout(), withAuthType([]json.RawMessage{server})[0], output.CurrentOptions())
}

// withAuthType adds an auth_type column ("oauth", "api_token") derived from
// auth_config, which a table cannot show because it is nested.
func withAuthType(servers []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(servers))
	for _, s := range servers {
		var m map[string]any
		if json.Unmarshal(s, &m) != nil {
			out = append(out, s)
			continue
		}
		m["auth_type"] = aigateway.AuthType(s)
		b, err := json.Marshal(m)
		if err != nil {
			out = append(out, s)
			continue
		}
		out = append(out, b)
	}
	return out
}

// resolveAppID turns an application name or id into its id.
func resolveAppID(cmd *cobra.Command, ident string) (string, error) {
	v1, err := newV1Client()
	if err != nil {
		return "", err
	}
	id, err := resolveApp(cmd.Context(), v1, ident)
	if err != nil {
		return "", fmt.Errorf("application %q: %w", ident, err)
	}
	return id, nil
}
