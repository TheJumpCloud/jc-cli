package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/passwordvault"
)

// newPasswordVaultCmd builds the `jc password-vault` group.
//
// This is the tenant scope: status, settings, users, groups and tags.
// Credentials, folders and websites follow in their own PRs — that split is
// not arbitrary, it is where the contract changes. Everything here is
// addressed by integer id or not addressed at all; those three use 24-hex
// object ids and carry the permission model.
//
// Password Manager is NOT replaced by this command. Both products are live
// while customers migrate and `jc password-manager` stays.
func newPasswordVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "password-vault",
		Aliases: []string{"pwv", "vault"},
		Short:   "Inspect JumpCloud Password Vault",
		Long: `Read the org's Password Vault: whether it is active, its tenant settings and
default permissions, the enrolled users and groups, and the dashboard summary.

PASSWORD VAULT REPLACES PASSWORD MANAGER, and on an org that has migrated every
/passwordmanager endpoint returns 404. Both command groups are kept while
customers move across; ` + "`jc password-manager`" + ` still works on an org that
has not migrated.

THIS AREA IGNORES sort AND ITS PAGING IS BROKEN. Measured on a live tenant:
sort is accepted and has no effect, and limit/skip produce pages that both
duplicate and omit records. jc therefore fetches these lists unpaginated and
reports the server's own count. Where a listing shows more rows than the count,
that is the API including the calling user in results without counting them —
trust the count.`,
		Example: `  jc password-vault status
  jc password-vault users list -t
  jc password-vault settings
  jc password-vault tags list --target-kind CREDENTIAL -t`,
	}
	cmd.AddCommand(
		newPWVStatusCmd(),
		newPWVSettingsCmd(),
		newPWVOverviewCmd(),
		newPWVUsersCmd(),
		newPWVGroupsCmd(),
		newPWVTagsCmd(),
		newPWVCredentialsCmd(),
		newPWVFoldersCmd(),
		newPWVWebsitesCmd(),
	)
	return cmd
}

// pwvGet is the shared read path. Every read but `status` goes through the
// activation gate first, so an unactivated org gets one clear sentence rather
// than a 404 from whichever endpoint it happened to ask for.
func pwvGet(ctx context.Context, endpoint string, v url.Values) (json.RawMessage, error) {
	if err := requireVaultActive(ctx); err != nil {
		return nil, err
	}
	return pwvGetRaw(ctx, endpoint, v)
}

// pwvGetRaw skips the gate. `status` uses it — asking the gate whether the
// gate is open would not terminate.
func pwvGetRaw(ctx context.Context, endpoint string, v url.Values) (json.RawMessage, error) {
	client, err := newV2Client()
	if err != nil {
		return nil, err
	}
	if len(v) > 0 {
		endpoint += "?" + v.Encode()
	}
	return client.Get(ctx, endpoint)
}

// requireVaultActive fails with the activation message when the org has not
// enabled Password Vault.
func requireVaultActive(ctx context.Context) error {
	raw, err := pwvGetRaw(ctx, passwordvault.StatusEndpoint, nil)
	if err != nil {
		return err
	}
	status, err := passwordvault.ParseStatus(raw)
	if err != nil {
		return err
	}
	if !status.IsActive {
		return passwordvault.ErrNotActivated()
	}
	return nil
}

// writeVaultList renders a {results,totalCount} listing, reporting the
// server's count rather than the row count.
func writeVaultList(cmd *cobra.Command, rows []json.RawMessage, total int, fields []string) error {
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

// --- status ---------------------------------------------------------------

func newPWVStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Whether Password Vault is active on this org",
		Long: `Report the activation gate.

This is the one Password Vault endpoint that answers before activation — every
other one returns 404 until the product is enabled — which is what makes it
usable to tell "not activated" apart from "not entitled" or "wrong path".`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwvGetRaw(cmd.Context(), passwordvault.StatusEndpoint, nil)
			if err != nil {
				return err
			}
			// Decoded before printing so an unreadable gate is an error
			// rather than something an operator reads as "off".
			if _, err := passwordvault.ParseStatus(raw); err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
}

// --- settings -------------------------------------------------------------

func newPWVSettingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "settings",
		Aliases: []string{"tenant-settings"},
		Short:   "Org-wide Password Vault settings",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Read the org-wide Password Vault settings",
		Long: `The tenant settings: whether private secrets are allowed, whether credential
and website export are permitted, browser-extension and mobile access, and the
security-alert notification switches.

The API nests these under a "values" key where every sibling read returns the
object directly; jc unwraps it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwvGet(cmd.Context(), passwordvault.TenantSettingsEndpoint, nil)
			if err != nil {
				return err
			}
			inner, err := passwordvault.ParseWrapped(raw, "values", "tenant settings")
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), inner, output.CurrentOptions())
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:     "default-permissions",
		Aliases: []string{"defaults"},
		Short:   "Default permissions applied to new resources",
		Long: `The permission defaults applied per resource category — websites, computers,
devices, databases, cloud services, social networks and credentials — plus
whether users may remove them.

On a freshly activated org every value is an empty string, which means "no
default", not "unreadable".`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwvGet(cmd.Context(), passwordvault.DefaultPermsEndpoint, nil)
			if err != nil {
				return err
			}
			inner, err := passwordvault.ParseWrapped(raw, "defaultPermissions", "default permissions")
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), inner, output.CurrentOptions())
		},
	})
	return cmd
}

func newPWVOverviewCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "overview",
		Aliases: []string{"dashboard"},
		Short:   "Dashboard summary for the org's vault",
		Long: `Credential and resource counts by type, password-health scores, the
least-used and expiring credentials, and per-user statistics — the same
figures the console dashboard shows.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwvGet(cmd.Context(), passwordvault.DashboardOverviewEndpoint, nil)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
}

// --- users ----------------------------------------------------------------

func newPWVUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "users",
		Aliases: []string{"user"},
		Short:   "Users enrolled in Password Vault",
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List enrolled users",
		Long: `List the users enrolled in Password Vault.

THE API INCLUDES YOU IN THE RESULTS WHETHER OR NOT YOU MATCH, and the count
excludes you when you do not. A search that matches nobody returns one row —
yourself — with a count of zero. The footer reports the count, so a footer
lower than the rows shown is this behaviour, not a bug in jc.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwvGet(cmd.Context(), passwordvault.UsersEndpoint, nil)
			if err != nil {
				return err
			}
			rows, total, err := passwordvault.ParseList(raw, "Password Vault users")
			if err != nil {
				return err
			}
			if len(rows) > total && !output.CurrentOptions().Quiet {
				fmt.Fprintf(cmd.ErrOrStderr(), "note: %s\n", passwordvault.SelfInjected)
			}
			return writeVaultList(cmd, rows, total,
				[]string{"id", "userName", "emailAddress", "name", "surname", "isActive", "isScim"})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "self",
		Short: "The Password Vault record for the calling identity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwvGet(cmd.Context(), passwordvault.UsersSelfEndpoint, nil)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:     "jc-managed",
		Aliases: []string{"jc-user"},
		Short:   "Whether the calling vault user is JumpCloud-managed",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwvGet(cmd.Context(), passwordvault.UsersSelfJCEndpoint, nil)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:     "active-pwm-tenants",
		Aliases: []string{"pwm-tenants"},
		Short:   "External tenant ids the vault service reports as active",
		Long: `Returns the externalTenantIds the vault service considers active.

THIS COMMONLY RETURNS 403. A standard organization API key is not permitted to
call it, and that is the expected answer rather than a misconfiguration on your
side — the command exists for keys that are permitted. jc reports the 403 as
it comes back rather than presenting it as an empty result, because "no active
tenants" and "you may not ask" are different answers.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwvGet(cmd.Context(), passwordvault.ActivePWMTenantsEndpoint, nil)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})

	return cmd
}

// --- groups ---------------------------------------------------------------

func newPWVGroupsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "groups",
		Aliases: []string{"group"},
		Short:   "Password Vault groups",
		Long: `Password Vault groups and their members and resources.

GROUP IDS ARE INTEGERS here — not the 24-character object ids the rest of
JumpCloud uses, and not the UUIDs Password Manager uses. Both of those are
rejected with a message saying so, because they are what somebody coming from
elsewhere will try first.

A group id that does not exist returns an EMPTY LIST rather than a 404 on the
members and resources reads, so an empty result is not evidence the group is
real. Check it against ` + "`groups list`" + `.`,
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List Password Vault groups",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwvGet(cmd.Context(), passwordvault.GroupsEndpoint, nil)
			if err != nil {
				return err
			}
			rows, total, err := passwordvault.ParseList(raw, "Password Vault groups")
			if err != nil {
				return err
			}
			return writeVaultList(cmd, rows, total, []string{"id", "name", "description"})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "all",
		Short: "Every group, including ones the short listing omits",
		Long: `The full group listing.

This endpoint returns no count field, unlike every other listing in the area,
so the footer here is the number of rows and nothing more.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwvGet(cmd.Context(), passwordvault.GroupsAllEndpoint, nil)
			if err != nil {
				return err
			}
			rows, err := passwordvault.ParseResultsOnly(raw, "Password Vault groups")
			if err != nil {
				return err
			}
			return writeVaultList(cmd, rows, len(rows), []string{"id", "name", "description"})
		},
	})

	cmd.AddCommand(newPWVGroupSubCmd("members", "List a group's members",
		`The users in a Password Vault group.

A group id that does not exist returns an empty list rather than a 404, so an
empty result here does not prove the group is real.`,
		passwordvault.GroupMembersEndpoint, []string{"id", "userName", "emailAddress"}))

	cmd.AddCommand(newPWVGroupSubCmd("resources", "List the resources assigned to a group",
		`The credentials, websites and other resources a group has been given access to.

A group id that does not exist returns an empty list rather than a 404.`,
		passwordvault.GroupResourcesEndpoint, []string{"id", "name", "category"}))

	cmd.AddCommand(newPWVAssignableCmd())
	return cmd
}

// newPWVGroupSubCmd builds the group sub-listings, which differ only in
// endpoint and default fields.
func newPWVGroupSubCmd(use, short, long string, endpoint func(int) string, fields []string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <groupId>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := passwordvault.ParseGroupID(args[0])
			if err != nil {
				return err
			}
			raw, err := pwvGet(cmd.Context(), endpoint(id), nil)
			if err != nil {
				return err
			}
			rows, total, err := passwordvault.ParseList(raw, "Password Vault group "+use)
			if err != nil {
				return err
			}
			return writeVaultList(cmd, rows, total, fields)
		},
	}
}

func newPWVAssignableCmd() *cobra.Command {
	var category string
	cmd := &cobra.Command{
		Use:   "assignable-resources <groupId>",
		Short: "Resources a group could be given, by category",
		Long: `The resources of one category that are available to assign to a group.

A CATEGORY IS REQUIRED — the API rejects the call without one — but it does
NOT validate the value: an unrecognised category returns an empty list rather
than an error. An empty result here is therefore not evidence that the group
has no assignable resources of that kind, and jc repeats the category it asked
for so a typo is visible.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := passwordvault.ParseGroupID(args[0])
			if err != nil {
				return err
			}
			v, err := passwordvault.AssignableQuery(category)
			if err != nil {
				return err
			}
			raw, err := pwvGet(cmd.Context(), passwordvault.GroupAssignableEndpoint(id), v)
			if err != nil {
				return err
			}
			rows, total, err := passwordvault.ParseList(raw, "assignable resources")
			if err != nil {
				return err
			}
			if len(rows) == 0 && !output.CurrentOptions().Quiet {
				fmt.Fprintf(cmd.ErrOrStderr(), "no assignable resources for category %q — note that %s\n",
					category, passwordvault.CategoryUnvalidated)
			}
			return writeVaultList(cmd, rows, total, []string{"id", "name", "category"})
		},
	}
	cmd.Flags().StringVar(&category, "category", "",
		"Resource category to list, e.g. CREDENTIAL or WEBSITE (required)")
	return cmd
}

// --- tags -----------------------------------------------------------------

func newPWVTagsCmd() *cobra.Command {
	var targetKind string
	cmd := &cobra.Command{
		Use:     "tags",
		Aliases: []string{"tag"},
		Short:   "Tags defined in the vault",
	}
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List tags for credentials or resources",
		Long: `List the tags defined for one target kind.

A TARGET KIND IS REQUIRED. The API rejects a call without one, and its error
names a bare targetKind query parameter that does not work — the filter form
jc sends is what the server actually accepts. Unlike the assignable-resources
category, this value IS validated, so a wrong one is an error rather than an
empty list.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := passwordvault.TagsQuery(targetKind)
			if err != nil {
				return err
			}
			raw, err := pwvGet(cmd.Context(), passwordvault.TagsEndpoint, v)
			if err != nil {
				return err
			}
			rows, err := passwordvault.ParseResultsOnly(raw, "Password Vault tags")
			if err != nil {
				return err
			}
			return writeVaultList(cmd, rows, len(rows), []string{"id", "name"})
		},
	}
	list.Flags().StringVar(&targetKind, "target-kind", "",
		"Which tags to list: CREDENTIAL or RESOURCE (required)")
	cmd.AddCommand(list)
	return cmd
}
