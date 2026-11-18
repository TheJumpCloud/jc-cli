package cmd

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/privaccess"
)

// newPrivilegedAccessCmd builds `jc privileged-access`, read-only. The API's
// quirks, and the secret-bearing endpoints deliberately left out, are
// documented in internal/privaccess.
func newPrivilegedAccessCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "privileged-access",
		Aliases: []string{"pam"},
		Short:   "Read JumpCloud Privileged Access (PAM)",
		Long: `Read the org's Privileged Access setup: the privileged servers, databases and
websites people connect to through a connector, the privileged credentials
linked to them, the blocking rules that stop risky commands and queries, the
connectors, and the connection log.

READ-ONLY. Nothing here creates, changes or deletes anything, and no command
reads a secret: credential secrets, MFA codes, rotation scripts, the backup
key, connector install files and session recordings are not available.

Servers, databases, websites and blocking rules are addressed by id or exact
name; credentials by their 24-character hex id. A database's connection string
is shown as [redacted], because it can embed a password.

Privileged Access shares its backend with Password Vault. The users listing
includes the calling user whether or not they match, so trust the count over
the number of rows.`,
		Example: `  jc privileged-access status
  jc pam servers list -t
  jc pam databases get "Prod DB"
  jc pam credentials history <id>
  jc pam connections status "Prod DB"`,
	}
	cmd.AddCommand(
		newPAMStatusCmd(),
		newPAMDashboardCmd(),
		pamCountedListGroup("users", []string{"user"}, "Users enrolled in Privileged Access",
			"The users enrolled in Privileged Access and Password Vault (one backend).\n\n"+
				"The API includes the calling user in every listing whether or not they match,\n"+
				"while its count excludes them, so the row count can exceed the total.",
			privaccess.UsersEndpoint, []string{"id", "userName", "emailAddress", "isActive"}),
		newPAMTagsCmd(),
		newPAMGroupsCmd(),
		newPAMResourceCmd(privaccess.Servers, []string{"server"},
			[]string{"id", "name", "uri", "connector", "archived", "isInUse", "recordEnabled"}),
		newPAMResourceCmd(privaccess.Databases, []string{"database", "db", "dbs"},
			[]string{"id", "name", "provider", "uri", "connector", "archived", "isInUse"}),
		newPAMResourceCmd(privaccess.Websites, []string{"website", "sites"},
			[]string{"id", "name", "uri", "connector", "archived", "isInUse"}),
		newPAMCredentialsCmd(),
		newPAMBlockingRulesCmd(),
		newPAMConnectorsCmd(),
		newPAMConnectionsCmd(),
		pamCountedListGroup("resource-managers", []string{"resource-manager"}, "Resource managers",
			"The resource managers: people or groups delegated to manage a set of privileged resources.",
			privaccess.ResourceManagersEndpoint, nil),
	)
	return cmd
}

// pamClient returns the v2 client after the activation gate.
func pamClient(ctx context.Context) (privaccess.Getter, error) {
	client, err := newV2Client()
	if err != nil {
		return nil, err
	}
	if err := privaccess.RequireActive(ctx, client); err != nil {
		return nil, err
	}
	return client, nil
}

func pamWriteList(cmd *cobra.Command, rows []json.RawMessage, total int, fields []string) error {
	opts := output.CurrentOptions()
	opts.DefaultFields = fields
	if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
		return err
	}
	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(rows), total)
	}
	return nil
}

func pamWriteTokenPage(cmd *cobra.Command, p privaccess.TokenPage, fields []string) error {
	opts := output.CurrentOptions()
	opts.DefaultFields = fields
	if err := output.WriteList(cmd.OutOrStdout(), p.Items, opts); err != nil {
		return err
	}
	if p.Next != "" && !opts.Quiet {
		cmd.PrintErrf("more available: rerun with --token %s\n", p.Next)
	}
	return nil
}

func newPAMStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Whether Privileged Access is active on this org",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), privaccess.StatusEndpoint)
			if err != nil {
				return err
			}
			if _, err := privaccess.ParseStatus(raw); err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
}

func newPAMDashboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "dashboard",
		Aliases: []string{"overview"},
		Short:   "Counts of servers, databases, websites, rules and users, and the most used resources",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := pamClient(cmd.Context())
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), privaccess.DashboardEndpoint)
			if err != nil {
				return err
			}
			if raw, err = privaccess.Redact(raw); err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
}

// pamCountedListGroup is a group whose only verb lists an unpaginated
// {results, totalCount} endpoint.
func pamCountedListGroup(use string, aliases []string, short, long, endpoint string, fields []string) *cobra.Command {
	group := &cobra.Command{Use: use, Aliases: aliases, Short: short}
	group.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   short,
		Long:    long,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := pamClient(cmd.Context())
			if err != nil {
				return err
			}
			rows, total, err := privaccess.ListResults(cmd.Context(), client, endpoint, use)
			if err != nil {
				return err
			}
			return pamWriteList(cmd, rows, total, fields)
		},
	})
	return group
}

func newPAMTagsCmd() *cobra.Command {
	group := &cobra.Command{Use: "tags", Short: "Tags used on privileged resources"}
	group.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Tags used on privileged resources",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := pamClient(cmd.Context())
			if err != nil {
				return err
			}
			rows, err := privaccess.ListUncounted(cmd.Context(), client, privaccess.TagsEndpoint, "results", "tags")
			if err != nil {
				return err
			}
			return pamWriteList(cmd, rows, len(rows), nil)
		},
	})
	return group
}

func newPAMGroupsCmd() *cobra.Command {
	var product string
	group := &cobra.Command{Use: "groups", Short: "Groups that grant access to privileged resources"}
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Groups that grant access to privileged resources",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint, err := privaccess.GroupsEndpoint(product)
			if err != nil {
				return err
			}
			client, err := pamClient(cmd.Context())
			if err != nil {
				return err
			}
			rows, err := privaccess.ListUncounted(cmd.Context(), client, endpoint, "results", "groups")
			if err != nil {
				return err
			}
			return pamWriteList(cmd, rows, len(rows), nil)
		},
	}
	list.Flags().StringVar(&product, "product", "", "Only groups for this product: PAM or PWM (Password Vault)")
	group.AddCommand(list)
	return group
}

// newPAMResourceCmd builds list and get for servers, databases or websites.
func newPAMResourceCmd(k privaccess.Kind, aliases, fields []string) *cobra.Command {
	plural := k.Name + "s"
	group := &cobra.Command{Use: plural, Aliases: aliases, Short: "Privileged " + plural}
	group.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List privileged " + plural,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := pamClient(cmd.Context())
			if err != nil {
				return err
			}
			rows, total, err := privaccess.List(cmd.Context(), client, k)
			if err != nil {
				return err
			}
			return pamWriteList(cmd, rows, total, fields)
		},
	})
	group.AddCommand(&cobra.Command{
		Use:   "get <id-or-name>",
		Short: "One privileged " + k.Name + " in full",
		Long: `One privileged ` + k.Name + `: its address, connector, access policies, linked
credentials and recording settings.

This reads the edit view. The API's plain detail read refuses with a demand for
a "View Detail" permission that the caller holds; the edit view returns a
superset. Secret-looking values, a database connection string among them, are
shown as [redacted].`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := pamClient(cmd.Context())
			if err != nil {
				return err
			}
			raw, err := privaccess.Get(cmd.Context(), client, k, args[0])
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})
	return group
}

func newPAMCredentialsCmd() *cobra.Command {
	group := &cobra.Command{
		Use:     "credentials",
		Aliases: []string{"credential", "creds"},
		Short:   "Privileged credentials (never their secrets)",
		Long: `The privileged credentials: the accounts used to log in to privileged
resources. Only credentials marked privileged appear here; the rest of the
vault is under ` + "`jc password-vault credentials`" + `.

No command here reads a secret, an MFA code or a rotation script.`,
	}
	var showPersonal bool
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List privileged credentials",
		Long: `List the privileged credentials.

Card and identity-document fields (last4Digits, cardholderName, idType,
firstName) are removed unless --show-personal is given. They are not secrets,
but they are personal data and are not needed to manage access.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := pamClient(cmd.Context())
			if err != nil {
				return err
			}
			rows, total, err := privaccess.ListCredentials(cmd.Context(), client)
			if err != nil {
				return err
			}
			if !showPersonal {
				if rows, err = privaccess.StripPersonalFields(rows); err != nil {
					return err
				}
			}
			return pamWriteList(cmd, rows, total,
				[]string{"id", "name", "credentialType", "username", "domain", "rotationEnabled", "archived"})
		},
	}
	list.Flags().BoolVar(&showPersonal, "show-personal", false, "Include card and identity-document fields")
	group.AddCommand(list)
	group.AddCommand(&cobra.Command{
		Use:   "get <id>",
		Short: "One credential's name, type and access policies (no secret)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := pamClient(cmd.Context())
			if err != nil {
				return err
			}
			raw, err := privaccess.GetCredential(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})
	group.AddCommand(&cobra.Command{
		Use:   "managers <id>",
		Short: "Who manages one credential",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := pamClient(cmd.Context())
			if err != nil {
				return err
			}
			rows, total, err := privaccess.CredentialManagers(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			return pamWriteList(cmd, rows, total, []string{"id", "username", "firstName", "lastName"})
		},
	})
	group.AddCommand(pamTokenCmd("history <id>", "A credential's version history (when it changed, not to what)",
		[]string{"version", "creationDate"}, privaccess.CredentialHistory))
	group.AddCommand(pamTokenCmd("linked-servers <id>", "The servers a credential is linked to",
		nil, privaccess.CredentialLinkedServers))
	return group
}

func pamTokenCmd(use, short string, fields []string,
	read func(context.Context, privaccess.Getter, string, string) (privaccess.TokenPage, error)) *cobra.Command {
	var token string
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := pamClient(cmd.Context())
			if err != nil {
				return err
			}
			p, err := read(cmd.Context(), client, args[0], token)
			if err != nil {
				return err
			}
			return pamWriteTokenPage(cmd, p, fields)
		},
	}
	c.Flags().StringVar(&token, "token", "", "Continuation token from the previous page")
	return c
}

func newPAMBlockingRulesCmd() *cobra.Command {
	group := &cobra.Command{
		Use:     "blocking-rules",
		Aliases: []string{"blocking-rule", "rules"},
		Short:   "Rules that block risky commands or queries in privileged sessions",
	}
	group.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List blocking rules",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := pamClient(cmd.Context())
			if err != nil {
				return err
			}
			rows, total, err := privaccess.ListBlockingRules(cmd.Context(), client)
			if err != nil {
				return err
			}
			return pamWriteList(cmd, rows, total, []string{"id", "name", "criteriaType", "severity", "priority"})
		},
	})
	group.AddCommand(&cobra.Command{
		Use:   "get <id-or-name>",
		Short: "One blocking rule in full: criteria, resources, exceptions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := pamClient(cmd.Context())
			if err != nil {
				return err
			}
			raw, err := privaccess.GetBlockingRule(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})
	return group
}

func newPAMConnectorsCmd() *cobra.Command {
	group := &cobra.Command{
		Use:     "connectors",
		Aliases: []string{"connector"},
		Short:   "Connectors: the gateways privileged sessions run through",
	}
	group.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List connectors",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := pamClient(cmd.Context())
			if err != nil {
				return err
			}
			rows, total, err := privaccess.ListPaged(cmd.Context(), client, privaccess.ConnectorsEndpoint, "connectors")
			if err != nil {
				return err
			}
			return pamWriteList(cmd, rows, total, nil)
		},
	})
	group.AddCommand(&cobra.Command{
		Use:   "versions",
		Short: "Connector releases available to install",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := pamClient(cmd.Context())
			if err != nil {
				return err
			}
			rows, err := privaccess.ListUncounted(cmd.Context(), client, privaccess.ConnectorVersionsEndpoint, "items", "connector versions")
			if err != nil {
				return err
			}
			return pamWriteList(cmd, rows, len(rows), []string{"version", "codename", "isDefault", "active", "beta"})
		},
	})
	return group
}

func newPAMConnectionsCmd() *cobra.Command {
	group := &cobra.Command{
		Use:     "connections",
		Aliases: []string{"connection", "sessions"},
		Short:   "Privileged sessions: who connected to what, and when",
	}
	group.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List privileged sessions",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := pamClient(cmd.Context())
			if err != nil {
				return err
			}
			rows, total, err := privaccess.ListPaged(cmd.Context(), client, privaccess.ConnectionsEndpoint, "connections")
			if err != nil {
				return err
			}
			return pamWriteList(cmd, rows, total, nil)
		},
	})
	group.AddCommand(&cobra.Command{
		Use:   "status <resource-id-or-name>",
		Short: "Who is connected to a server, database or website right now",
		Long: `Who is connected to a privileged server, database or website right now.

The API answers "Available" for an id that matches nothing, so jc looks the
resource up first and reports an unknown one as an error.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := pamClient(cmd.Context())
			if err != nil {
				return err
			}
			raw, kind, err := privaccess.ResourceConnectionStatus(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			if !output.CurrentOptions().Quiet {
				cmd.PrintErrf("%s %s\n", kind, args[0])
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})
	return group
}
