package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/pwm"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

// newPasswordManagerCmd builds the `jc password-manager` group.
//
// Reads only. Writes are held back deliberately, and the reason is in the
// long help rather than buried here: the API can CREATE a shared folder and
// offers no way to delete one, so every write is one-way on a live tenant.
func newPasswordManagerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "password-manager",
		Aliases: []string{"pwm", "password-mgr"},
		Short:   "Inspect JumpCloud Password Manager",
		Long: `Read the org's Password Manager: enrolled users and groups, shared folders,
stored item counts, password-hygiene scores and the policies in force.

Two things about this area are worth knowing before you use it.

IDs ARE UUIDS, not the 24-character hex ids the rest of JumpCloud uses, and a
Password Manager record links back to the directory only through its
externalId. So a JumpCloud user id will not address a Password Manager record
directly — pass a name, username or email and jc resolves it across the bridge.

THE API ANSWERS HTTP 500 FOR ANY ID IT DOES NOT LIKE — a JumpCloud id, a
well-formed UUID that does not exist, and presumably a genuine fault all return
"Unexpected error". jc therefore checks the id shape before calling, so a typo
reads as a typo rather than as an outage.

This group is READ-ONLY. Password Manager writes are not exposed: creating a
shared folder works and deleting one is not implemented server-side, so a write
here cannot be undone through the API.`,
	}
	cmd.AddCommand(
		newPWMOverviewCmd(),
		newPWMUsersCmd(),
		newPWMGroupsCmd(),
		newPWMFoldersCmd(),
		newPWMItemsCmd(),
		newPWMPoliciesCmd(),
		newPWMBackupKeysCmd(),
	)
	return cmd
}

// pwmGet is the shared read path.
func pwmGet(ctx context.Context, endpoint string) (json.RawMessage, error) {
	client, err := newV2Client()
	if err != nil {
		return nil, err
	}
	return client.Get(ctx, endpoint)
}

// resolvePWMUser turns what an operator typed into a Password Manager user.
//
// Three routes, in the order that costs least: a PWM UUID is used directly; a
// name matching a Password Manager record is used as-is; otherwise the name is
// resolved against the DIRECTORY and matched here through externalId, which is
// the only link between the two systems.
func resolvePWMUser(ctx context.Context, identifier string) (pwm.User, error) {
	raw, err := pwmGet(ctx, pwm.UsersEndpoint)
	if err != nil {
		return pwm.User{}, err
	}
	users, err := pwm.ParseUsers(raw)
	if err != nil {
		return pwm.User{}, err
	}

	if u, ok := pwm.FindUser(users, identifier); ok {
		return u, nil
	}

	// The bridge. A JumpCloud id given directly is honoured here too, since
	// that is exactly what externalId holds.
	//
	// Users live on V1, so this is the V1 resolver: the V2 one 404s on
	// /systemusers, which is what the first version of this did.
	jcID := identifier
	if !isJCObjectID(identifier) {
		v1, cerr := newV1Client()
		if cerr != nil {
			return pwm.User{}, cerr
		}
		jcID, err = resolve.NewResolver(v1).Resolve(ctx, identifier, resolve.UserConfig)
		if err != nil {
			return pwm.User{}, fmt.Errorf("%q matches no Password Manager user, and no JumpCloud "+
				"user either: %w", identifier, err)
		}
	}
	if u, ok := pwm.FindUserByExternalID(users, jcID); ok {
		return u, nil
	}
	return pwm.User{}, fmt.Errorf("%q is a JumpCloud user but is not enrolled in Password Manager "+
		"(%d users are)", identifier, len(users))
}

func isJCObjectID(s string) bool {
	if len(s) != 24 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func newPWMOverviewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "overview",
		Short: "Password Manager summary for the org",
		Long: `Org-wide Password Manager counters: enrolled users and groups, shared folders,
stored passwords and the hygiene numbers — weak, old and compromised passwords,
and the overall score.

Returns a bare object, not a list envelope.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwmGet(cmd.Context(), pwm.OverviewEndpoint)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
}

func newPWMUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "users",
		Aliases: []string{"user"},
		Short:   "Password Manager users",
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List users enrolled in Password Manager",
		Long: `List the users enrolled in Password Manager, with their item counts and
password-hygiene scores.

Each record carries externalId, the JumpCloud user id it corresponds to. That
is the only link between the two systems.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwmGet(cmd.Context(), pwm.UsersEndpoint)
			if err != nil {
				return err
			}
			rows, total, err := pwm.ParseList(raw)
			if err != nil {
				return err
			}
			return writePWMList(cmd, rows, total)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <name-username-email-or-id>",
		Short: "Get one Password Manager user",
		Long: `Get one enrolled user.

Accepts a Password Manager UUID, or a name, username or email — including a
JumpCloud username, which is resolved through the directory and matched on
externalId.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			u, err := resolvePWMUser(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			raw, err := pwmGet(cmd.Context(), pwm.UserEndpoint(u.ID))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "folders <name-username-email-or-id>",
		Short: "Shared folders one user can reach",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			u, err := resolvePWMUser(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			raw, err := pwmGet(cmd.Context(), pwm.UserSharedFoldersEndpoint(u.ID))
			if err != nil {
				return err
			}
			rows, total, err := pwm.ParseList(raw)
			if err != nil {
				return err
			}
			return writePWMList(cmd, rows, total)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "items <name-username-email-or-id>",
		Short: "Item counts for one user",
		Long: `Stored items belonging to one user.

This endpoint returns BOTH a results array and a separate items array. Both
were empty on every tenant probed, so which one carries the records when
populated is not established — jc reports both rather than picking one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			u, err := resolvePWMUser(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			raw, err := pwmGet(cmd.Context(), pwm.UserItemsEndpoint(u.ID))
			if err != nil {
				return err
			}
			return writePWMItems(cmd, raw)
		},
	})

	return cmd
}

func newPWMGroupsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "groups",
		Aliases: []string{"group"},
		Short:   "Password Manager groups",
		Long: `Groups enrolled in Password Manager.

LIST ONLY. There is no detail endpoint: /passwordmanager/groups/{id} and every
sub-resource under it return HTTP 404 on a healthy tenant, so the list is the
whole surface. Each record carries externalId, the JumpCloud group it mirrors.`,
	}
	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List groups enrolled in Password Manager",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwmGet(cmd.Context(), pwm.GroupsEndpoint)
			if err != nil {
				return err
			}
			rows, total, err := pwm.ParseList(raw)
			if err != nil {
				return err
			}
			return writePWMList(cmd, rows, total)
		},
	})
	return cmd
}

func newPWMFoldersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "folders",
		Aliases: []string{"folder", "shared-folders"},
		Short:   "Password Manager shared folders",
		Long: `Org shared folders, who can reach them, and what they hold.

Read-only, deliberately: a folder can be created through the API and there is
no delete route for one, so a write here cannot be undone.`,
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List org shared folders",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwmGet(cmd.Context(), pwm.SharedFoldersEndpoint)
			if err != nil {
				return err
			}
			rows, total, err := pwm.ParseList(raw)
			if err != nil {
				return err
			}
			return writePWMList(cmd, rows, total)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get one shared folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := resolvePWMFolder(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			raw, err := pwmGet(cmd.Context(), pwm.FolderEndpoint(id))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})

	for _, spec := range []struct {
		use, short string
		endpoint   func(string) string
	}{
		{"users <name-or-id>", "Users with access to a shared folder", pwm.FolderUsersEndpoint},
		{"groups <name-or-id>", "Groups with access to a shared folder", pwm.FolderGroupsEndpoint},
	} {
		endpoint := spec.endpoint
		cmd.AddCommand(&cobra.Command{
			Use:   spec.use,
			Short: spec.short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				id, err := resolvePWMFolder(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				raw, err := pwmGet(cmd.Context(), endpoint(id))
				if err != nil {
					return err
				}
				rows, total, perr := pwm.ParseList(raw)
				if perr != nil {
					return perr
				}
				return writePWMList(cmd, rows, total)
			},
		})
	}

	return cmd
}

func resolvePWMFolder(ctx context.Context, identifier string) (string, error) {
	if pwm.IsID(identifier) {
		return identifier, nil
	}
	raw, err := pwmGet(ctx, pwm.SharedFoldersEndpoint)
	if err != nil {
		return "", err
	}
	folders, err := pwm.ParseFolders(raw)
	if err != nil {
		return "", err
	}
	f, ok := pwm.FindFolder(folders, identifier)
	if !ok {
		return "", fmt.Errorf("no shared folder %q (%d exist)", identifier, len(folders))
	}
	return f.Identity(), nil
}

func newPWMItemsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "items",
		Short: "Stored items across the org",
		Long: `Items stored in Password Manager org-wide.

This endpoint returns BOTH a results array and a separate items array, and both
were empty on every tenant probed, so which carries the records when populated
is not established. jc reports both rather than picking one and being wrong
silently.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwmGet(cmd.Context(), pwm.ItemsEndpoint)
			if err != nil {
				return err
			}
			return writePWMItems(cmd, raw)
		},
	}
}

func newPWMPoliciesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "policies",
		Aliases: []string{"policy"},
		Short:   "Password Manager policies",
	}
	for _, spec := range []struct{ use, short, endpoint string }{
		{"folders", "Shared-folder policy", pwm.FolderPolicyEndpoint},
		{"company", "Company-wide policy, including whether export is disabled", pwm.CompanyPolicyEndpoint},
	} {
		endpoint := spec.endpoint
		cmd.AddCommand(&cobra.Command{
			Use:   spec.use,
			Short: spec.short,
			Long:  spec.short + ".\n\nReturns a bare object, not a list envelope.",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				raw, err := pwmGet(cmd.Context(), endpoint)
				if err != nil {
					return err
				}
				return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
			},
		})
	}
	return cmd
}

func newPWMBackupKeysCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backup-keys",
		Short: "Cloud backup key records",
		Long: `List the cloud backup key records.

READ ONLY, and deliberately so: the four write operations on this family manage
encryption key material and are not undoable, so they are held back rather than
exposed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwmGet(cmd.Context(), pwm.BackupKeysEndpoint)
			if err != nil {
				return err
			}
			rows, total, err := pwm.ParseList(raw)
			if err != nil {
				return err
			}
			return writePWMList(cmd, rows, total)
		},
	}
}

func writePWMList(cmd *cobra.Command, rows []json.RawMessage, total int) error {
	opts := output.CurrentOptions()
	if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
		return err
	}
	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d of %d ──\n", len(rows), total)
	}
	return nil
}

// writePWMItems reports both arrays, because which one carries the records is
// not established and silently picking would be a guess presented as fact.
func writePWMItems(cmd *cobra.Command, raw json.RawMessage) error {
	results, items, total, err := pwm.ParseItems(raw)
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	if opts.Format == "json" {
		out, merr := json.MarshalIndent(map[string]any{
			"results": results, "items": items, "totalCount": total,
		}, "", "  ")
		if merr != nil {
			return merr
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}
	if err := output.WriteList(cmd.OutOrStdout(), results, opts); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "── %d in results, %d in items, totalCount %d ──\n",
		len(results), len(items), total)
	return nil
}
