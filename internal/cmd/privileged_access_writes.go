package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TheJumpCloud/jc-cli/internal/output"
	"github.com/TheJumpCloud/jc-cli/internal/privaccess"
)

// Privileged Access writes. The rules that shape them, above all the lockout
// a full-replace update without an access policy causes, are in
// internal/privaccess/writes.go. Every command confirms (or takes --force),
// supports --plan, and reads its effect back before reporting success.

// pamWriter returns the v2 client after the activation gate.
func pamWriter(cmd *cobra.Command) (privaccess.Writer, error) {
	client, err := newV2Client()
	if err != nil {
		return nil, err
	}
	if err := privaccess.RequireActive(cmd.Context(), client); err != nil {
		return nil, err
	}
	return client, nil
}

const pamUpdateLong = `

Only the named fields change. The API replaces the whole record on every
update and clears whatever it is not sent, and a record sent without its access
policy loses every permission and can no longer be read, changed or deleted
through the API. So jc reads the record, changes the named fields on it, sends
the rest back exactly as read, refuses if the access policy grants Manage to
nobody, and reads the record back afterwards to confirm the change and that the
policy did not move.`

func addPAMResourceWriteCmds(group *cobra.Command, k privaccess.Kind) {
	plural := k.Name + "s"

	// create
	var spec privaccess.ResourceSpec
	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a privileged " + k.Name,
		Long: `Create a privileged ` + k.Name + `. You are granted Manage, View Detail and Connect
on it; jc offers no way not to, because an object created without Manage can
afterwards be neither changed nor deleted through the API.

The name must not already be in use, so the new ` + k.Name + ` can be addressed by it.
The ` + k.Name + ` is confirmed by reading the listing back.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec.Name = args[0]
			ok, err := vaultConfirm(cmd, "create", "privileged "+k.Name, args[0],
				[]string{"uri: " + spec.URI, "grants you Manage, View Detail and Connect"})
			if err != nil || !ok {
				return err
			}
			client, err := pamWriter(cmd)
			if err != nil {
				return err
			}
			id, err := privaccess.CreateResource(cmd.Context(), client, k, spec)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "confirmed: created %s %s\n", k.Name, id)
			return output.WriteSingle(cmd.OutOrStdout(), []byte(fmt.Sprintf(`{"id":%q}`, id)), output.CurrentOptions())
		},
	}
	create.Flags().StringVar(&spec.URI, "uri", "", "Host, address or URL (required)")
	create.Flags().StringVar(&spec.Notes, "notes", "", "Free-text notes")
	create.Flags().StringVar(&spec.Tags, "tags", "", "Tags, comma-separated")
	switch k.Endpoint {
	case privaccess.ServersEndpoint:
		create.Flags().StringVar(&spec.OS, "os", "linux", "Operating system: linux or windows")
		create.Flags().StringVar(&spec.Protocol, "protocol", "ssh", "Connection protocol, e.g. ssh or rdp")
		create.Flags().IntVar(&spec.Port, "port", 22, "Port")
	case privaccess.DatabasesEndpoint:
		create.Flags().StringVar(&spec.Provider, "provider", "", "Database engine, e.g. postgresql, mysql, sqlserver (required)")
		create.Flags().IntVar(&spec.Port, "port", 0, "Port (required)")
		create.Flags().StringVar(&spec.DatabaseName, "database-name", "", "Database name")
	}
	_ = create.MarkFlagRequired("uri")

	// update
	update := &cobra.Command{
		Use:   "update <id-or-name>",
		Short: "Change fields on a privileged " + k.Name,
		Long:  "Change the name, address, notes or tags of a privileged " + k.Name + "." + pamUpdateLong,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			changes := map[string]any{}
			for flag, key := range map[string]string{"name": "name", "uri": "uri", "notes": "notes",
				"tags": "tags", "os": "os", "database-name": "databaseName"} {
				if f := cmd.Flags().Lookup(flag); f != nil && f.Changed {
					changes[key] = f.Value.String()
				}
			}
			if f := cmd.Flags().Lookup("port"); f != nil && f.Changed {
				p, _ := cmd.Flags().GetInt("port")
				changes["port"] = p
			}
			if len(changes) == 0 {
				return fmt.Errorf("nothing to change: pass at least one field flag")
			}
			effects := make([]string, 0, len(changes))
			for key, v := range changes {
				effects = append(effects, fmt.Sprintf("%s → %v", key, v))
			}
			ok, err := vaultConfirm(cmd, "update", "privileged "+k.Name, args[0], effects)
			if err != nil || !ok {
				return err
			}
			client, err := pamWriter(cmd)
			if err != nil {
				return err
			}
			raw, err := privaccess.UpdateResource(cmd.Context(), client, k, args[0], changes)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "confirmed: updated %s %s\n", k.Name, args[0])
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
	update.Flags().String("name", "", "New name")
	update.Flags().String("uri", "", "New host, address or URL")
	update.Flags().String("notes", "", "New notes (empty string clears)")
	update.Flags().String("tags", "", "New tags, comma-separated (empty string clears)")
	switch k.Endpoint {
	case privaccess.ServersEndpoint:
		update.Flags().String("os", "", "New operating system: linux or windows")
	case privaccess.DatabasesEndpoint:
		update.Flags().Int("port", 0, "New port")
		update.Flags().String("database-name", "", "New database name")
	}

	del := &cobra.Command{
		Use:   "delete <id-or-name>",
		Short: "Delete a privileged " + k.Name,
		Long: `Delete a privileged ` + k.Name + `, archived or not. Permanent. The deletion is
confirmed by reading both the active and the archived listing back.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ok, err := vaultConfirm(cmd, "delete", "privileged "+k.Name, args[0], []string{"permanent"})
			if err != nil || !ok {
				return err
			}
			client, err := pamWriter(cmd)
			if err != nil {
				return err
			}
			id, err := privaccess.DeleteResource(cmd.Context(), client, k, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "confirmed: deleted %s %s\n", k.Name, id)
			return nil
		},
	}

	archiveCmd := func(archive bool) *cobra.Command {
		use, short, verb := "archive", "Archive privileged "+plural, "archive"
		long := `Archive one or more privileged ` + plural + ` by id or name. An archived ` + k.Name + `
leaves the listing and can no longer be connected to; ` + "`unarchive`" + ` restores it.`
		if !archive {
			use, short, verb = "unarchive", "Restore archived privileged "+plural, "unarchive"
			long = `Restore one or more archived privileged ` + plural + ` by id or name.`
		}
		return &cobra.Command{
			Use:   use + " <id-or-name> [id-or-name...]",
			Short: short,
			Long:  long + "\n\nThe change is confirmed by reading the archived listing back.",
			Args:  cobra.MinimumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				ok, err := vaultConfirm(cmd, verb, "privileged "+plural, strings.Join(args, ", "),
					[]string{fmt.Sprintf("%d %s(s)", len(args), k.Name)})
				if err != nil || !ok {
					return err
				}
				client, err := pamWriter(cmd)
				if err != nil {
					return err
				}
				ids, err := privaccess.SetArchived(cmd.Context(), client, k, args, archive)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "confirmed: %sd %s\n", verb, strings.Join(ids, ", "))
				return nil
			},
		}
	}

	clone := &cobra.Command{
		Use:     "clone <id-or-name>",
		Aliases: []string{"duplicate"},
		Short:   "Duplicate a privileged " + k.Name,
		Long: `Duplicate a privileged ` + k.Name + `. The copy is named "<name> - Copy" and carries
the original's settings, notes and access policy. It is confirmed by reading the
listing back.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ok, err := vaultConfirm(cmd, "duplicate", "privileged "+k.Name, args[0],
				[]string{"creates \"<name> - Copy\" with the same access policy"})
			if err != nil || !ok {
				return err
			}
			client, err := pamWriter(cmd)
			if err != nil {
				return err
			}
			id, err := privaccess.CloneResource(cmd.Context(), client, k, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "confirmed: created copy %s\n", id)
			return output.WriteSingle(cmd.OutOrStdout(), []byte(fmt.Sprintf(`{"id":%q}`, id)), output.CurrentOptions())
		},
	}

	group.AddCommand(create, update, del, archiveCmd(true), archiveCmd(false), clone)
}

func addPAMCredentialWriteCmds(group *cobra.Command) {
	del := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a privileged credential",
		Long: `Delete a privileged credential, archived or not, by its 24-character hex id.
Permanent: the secret goes with it. Confirmed by reading both listings back.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ok, err := vaultConfirm(cmd, "delete", "privileged credential", args[0],
				[]string{"permanent, secret included"})
			if err != nil || !ok {
				return err
			}
			client, err := pamWriter(cmd)
			if err != nil {
				return err
			}
			if err := privaccess.DeleteCredential(cmd.Context(), client, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "confirmed: deleted credential %s\n", args[0])
			return nil
		},
	}
	archiveCmd := func(archive bool) *cobra.Command {
		use, short, verb := "archive", "Archive privileged credentials", "archive"
		if !archive {
			use, short, verb = "unarchive", "Restore archived privileged credentials", "unarchive"
		}
		return &cobra.Command{
			Use:   use + " <id> [id...]",
			Short: short,
			Long: short + ` by 24-character hex id, in one call. Confirmed by
reading the archived listing back.`,
			Args: cobra.MinimumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				ok, err := vaultConfirm(cmd, verb, "privileged credentials", strings.Join(args, ", "),
					[]string{fmt.Sprintf("%d credential(s)", len(args))})
				if err != nil || !ok {
					return err
				}
				client, err := pamWriter(cmd)
				if err != nil {
					return err
				}
				if err := privaccess.SetCredentialsArchived(cmd.Context(), client, args, archive); err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "confirmed: %sd %d credential(s)\n", verb, len(args))
				return nil
			},
		}
	}
	clone := &cobra.Command{
		Use:     "clone <id>",
		Aliases: []string{"duplicate"},
		Short:   "Duplicate a privileged credential, secret included",
		Long: `Duplicate a privileged credential. The copy is named "<name> - Copy", carries
the same secret and access policy, and is one more place the secret exists.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ok, err := vaultConfirm(cmd, "duplicate", "privileged credential", args[0],
				[]string{"copies the secret into a second credential"})
			if err != nil || !ok {
				return err
			}
			client, err := pamWriter(cmd)
			if err != nil {
				return err
			}
			id, err := privaccess.CloneCredential(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "confirmed: created copy %s\n", id)
			return output.WriteSingle(cmd.OutOrStdout(), []byte(fmt.Sprintf(`{"id":%q}`, id)), output.CurrentOptions())
		},
	}
	group.AddCommand(del, archiveCmd(true), archiveCmd(false), clone)
}

func addPAMBlockingRuleWriteCmds(group *cobra.Command) {
	update := &cobra.Command{
		Use:   "update <id-or-name>",
		Short: "Change a blocking rule's name, notes, tags or priority",
		Long: `Change a blocking rule's name, notes, tags or priority. Its criteria and the
resources it applies to are left as they are.` + pamUpdateLong,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			changes := map[string]any{}
			for _, key := range []string{"name", "notes", "tags"} {
				if f := cmd.Flags().Lookup(key); f.Changed {
					changes[key] = f.Value.String()
				}
			}
			if cmd.Flags().Changed("priority") {
				p, _ := cmd.Flags().GetInt("priority")
				changes["priority"] = p
			}
			if len(changes) == 0 {
				return fmt.Errorf("nothing to change: pass --name, --notes, --tags or --priority")
			}
			effects := make([]string, 0, len(changes))
			for key, v := range changes {
				effects = append(effects, fmt.Sprintf("%s → %v", key, v))
			}
			ok, err := vaultConfirm(cmd, "update", "blocking rule", args[0], effects)
			if err != nil || !ok {
				return err
			}
			client, err := pamWriter(cmd)
			if err != nil {
				return err
			}
			raw, err := privaccess.UpdateBlockingRule(cmd.Context(), client, args[0], changes)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "confirmed: updated blocking rule %s\n", args[0])
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
	update.Flags().String("name", "", "New name")
	update.Flags().String("notes", "", "New notes (empty string clears)")
	update.Flags().String("tags", "", "New tags (empty string clears)")
	update.Flags().Int("priority", 0, "New priority (1 or more)")

	del := &cobra.Command{
		Use:   "delete <id-or-name>",
		Short: "Delete a blocking rule",
		Long:  "Delete a blocking rule. Permanent. Confirmed by reading the listing back.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ok, err := vaultConfirm(cmd, "delete", "blocking rule", args[0], []string{"permanent"})
			if err != nil || !ok {
				return err
			}
			client, err := pamWriter(cmd)
			if err != nil {
				return err
			}
			id, err := privaccess.DeleteBlockingRule(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "confirmed: deleted blocking rule %s\n", id)
			return nil
		},
	}
	group.AddCommand(update, del)
}
