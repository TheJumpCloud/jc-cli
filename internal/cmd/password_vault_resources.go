package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/passwordvault"
)

// The three resource families: credentials, folders and websites. Reads only
// in this PR; the writes follow, and the secret-retrieval endpoints after
// that.
//
// Two things about this file exist because of API defects rather than design.
//
// The plain detail read is broken for credentials AND websites. Reproduced
// on 2026-09-25 against a freshly created credential whose access policy
// granted the calling user Manage, View Detail and Connect: GET /{id} and
// GET /{id}/activities both answered 400 asking for the View Detail
// permission that was granted, while /history and /managers answered 200.
// Websites behave identically. So `credentials get` surfaces that as the
// defect it is rather than as a permission the operator should go and fix,
// and `websites get` reads the edit view, which works and returns a superset.
//
// Absence is reported three different ways. An absent credential or website
// gives 404; an absent folder gives 400 "Folder not found."; an absent
// credential's /managers gives 400 "Credential not found." Callers cannot
// assume a single shape.

// vaultObjectArg validates a 24-hex id before it reaches the API.
func vaultObjectArg(kind, id string) error {
	if !passwordvault.IsObjectID(id) {
		return passwordvault.ErrNotVaultObjectID(kind, id)
	}
	return nil
}

// vaultListCmd builds a top-level family listing.
func vaultListCmd(short, long, endpoint string, fields []string) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   short,
		Long:    long,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwvGet(cmd.Context(), endpoint, nil)
			if err != nil {
				return err
			}
			rows, total, err := passwordvault.ParseList(raw, short)
			if err != nil {
				return err
			}
			return writeVaultList(cmd, rows, total, fields)
		},
	}
}

// vaultSubListCmd builds a per-object sub-listing.
func vaultSubListCmd(use, kind, short, long string, endpoint func(string) string, fields []string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <id>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultObjectArg(kind, args[0]); err != nil {
				return err
			}
			raw, err := pwvGet(cmd.Context(), endpoint(args[0]), nil)
			if err != nil {
				return err
			}
			rows, total, perr := passwordvault.ParseList(raw, short)
			if perr != nil {
				return perr
			}
			return writeVaultList(cmd, rows, total, fields)
		},
	}
}

// vaultColumnsCmd builds the Excel column-metadata read.
func vaultColumnsCmd(what, endpoint string) *cobra.Command {
	return &cobra.Command{
		Use:     "columns",
		Aliases: []string{"excel-columns"},
		Short:   "Column names the " + what + " import and export use",
		Long: `The column names JumpCloud's Excel import and export use for ` + what + `.

Read-only metadata. The import and export operations themselves are not in
this command group.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := pwvGet(cmd.Context(), endpoint, nil)
			if err != nil {
				return err
			}
			inner, err := passwordvault.ParseColumns(raw)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), inner, output.CurrentOptions())
		},
	}
}

// --- credentials ----------------------------------------------------------

func newPWVCredentialsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "credentials",
		Aliases: []string{"credential", "creds"},
		Short:   "Stored credentials",
		Long: `The credentials stored in the vault.

THE DETAIL READ IS BROKEN SERVER-SIDE. ` + "`credentials get`" + ` asks the endpoint the
API documents, and that endpoint refuses with a demand for a permission the
caller already holds — reproduced against a freshly created credential that
granted it explicitly. The listing carries the metadata; the full record is
only reachable through the secret-bearing read, which is not in this group.`,
	}

	cmd.AddCommand(vaultListCmd(
		"List stored credentials",
		`List the credentials stored in the vault, with their type, folder, owner
permissions and password strength.

Fetched unpaginated: this area ignores sort and its limit/skip paging
duplicates and omits records.`,
		passwordvault.CredentialsEndpoint,
		[]string{"id", "name", "credentialType", "username", "folderName", "permissions", "archived"}))

	cmd.AddCommand(&cobra.Command{
		Use:   "get <id>",
		Short: "One credential (currently refused by the API)",
		Long: `Read one credential by its 24-character hex id.

THIS CURRENTLY FAILS, and not because of anything you can change. The endpoint
answers 400 asking for a "View Detail" permission that the calling user holds;
it was reproduced against a credential created moments earlier whose access
policy granted Manage, View Detail and Connect. The command is here so the
defect is visible and reported accurately rather than absent and puzzling.

Use ` + "`credentials list`" + ` for metadata in the meantime.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultObjectArg("credential", args[0]); err != nil {
				return err
			}
			raw, err := pwvGet(cmd.Context(), passwordvault.CredentialEndpoint(args[0]), nil)
			if err != nil {
				if passwordvault.DetailReadRefused(err) {
					return passwordvault.ErrDetailReadRefused("credential",
						"`jc password-vault credentials list` carries the metadata.")
				}
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "activities <id>",
		Short: "Access history for one credential (currently refused by the API)",
		Long: `Who accessed a credential and when.

Refused by the same defect as ` + "`credentials get`" + `: the endpoint demands a
permission the caller holds. Use ` + "`credentials history`" + `, which works, for the
record's version history.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultObjectArg("credential", args[0]); err != nil {
				return err
			}
			raw, err := pwvGet(cmd.Context(), passwordvault.CredentialActivities(args[0]), nil)
			if err != nil {
				if passwordvault.DetailReadRefused(err) {
					return passwordvault.ErrDetailReadRefused("credential activities",
						"`jc password-vault credentials history` works and carries the version history.")
				}
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "history <id>",
		Short: "Version history for one credential",
		Long: `The credential's versions, newest first.

This is the one listing in the area that pages with a continuation token
rather than skip and limit, and so the one whose paging can be trusted. The
token is reported alongside the items.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultObjectArg("credential", args[0]); err != nil {
				return err
			}
			raw, err := pwvGet(cmd.Context(), passwordvault.CredentialHistory(args[0]), nil)
			if err != nil {
				return err
			}
			items, token, err := passwordvault.ParseHistory(raw)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = []string{"version", "creationDate"}
			if err := output.WriteList(cmd.OutOrStdout(), items, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				writeListFooter(cmd, len(items), len(items))
				if token != "" {
					cmd.PrintErrf("more versions available; continuation token: %s\n", token)
				}
			}
			return nil
		},
	})

	cmd.AddCommand(vaultSubListCmd("managers", "credential",
		"Who manages one credential",
		`The users who hold management rights over a credential.

An id that does not exist is reported here as 400 "Credential not found."
rather than the 404 the detail read gives — absence has more than one shape in
this area.`,
		passwordvault.CredentialManagers,
		[]string{"id", "username", "firstName", "lastName"}))

	cmd.AddCommand(vaultColumnsCmd("credential", passwordvault.CredentialColumnsPath))
	return cmd
}

// --- folders --------------------------------------------------------------

func newPWVFoldersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "folders",
		Aliases: []string{"folder"},
		Short:   "Vault folders",
		Long: `The folders credentials and websites are organised into.

A FOLDER THAT DOES NOT EXIST IS REPORTED AS 400, not 404 — the message reads
"Folder not found." with status INVALID_ARGUMENT. jc reports it as absence
rather than as a malformed request.

Folders also use their own permission vocabulary: Folder.Manage,
Folder.Item.Manage, Folder.View and Folder.Connect, where credentials and
websites use Manage and View Detail. That matters for the writes, not these
reads, but it is the reason folders are the odd family out.`,
	}

	cmd.AddCommand(vaultListCmd(
		"List vault folders",
		"List the folders in the vault, with their description and sharing state.",
		passwordvault.FoldersEndpoint,
		[]string{"id", "name", "description", "isPrivate", "permissions"}))

	cmd.AddCommand(newPWVFolderGetCmd("get", "One folder",
		`Read one folder by its 24-character hex id.

The API wraps the record in a "folder" key; jc unwraps it.`,
		passwordvault.FolderEndpoint))

	cmd.AddCommand(newPWVFolderGetCmd("edit-view", "One folder as the editor sees it",
		`The folder with the fields an edit needs: its access policies, and the ids of
the credentials and resources inside it.

This is a superset of `+"`get`"+` and is what the writes will build on.`,
		passwordvault.FolderEditEndpoint))

	cmd.AddCommand(vaultSubListCmd("items", "folder",
		"What is inside one folder",
		"The credentials and resources a folder contains.",
		passwordvault.FolderItemsEndpoint,
		[]string{"id", "name", "category"}))

	cmd.AddCommand(vaultSubListCmd("managers", "folder",
		"Who manages one folder",
		"The users who hold management rights over a folder.",
		passwordvault.FolderManagers,
		[]string{"id", "username", "firstName", "lastName"}))

	return cmd
}

// newPWVFolderGetCmd builds the two single-folder reads, which differ only in
// endpoint. Both unwrap the "folder" envelope and both translate the 400 that
// means "absent".
func newPWVFolderGetCmd(use, short, long string, endpoint func(string) string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <id>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultObjectArg("folder", args[0]); err != nil {
				return err
			}
			raw, err := pwvGet(cmd.Context(), endpoint(args[0]), nil)
			if err != nil {
				if passwordvault.FolderNotFoundIs400(err) {
					return errFolderAbsent(args[0])
				}
				return err
			}
			inner, err := passwordvault.ParseWrappedFolder(raw)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), inner, output.CurrentOptions())
		},
	}
}

// errFolderAbsent reports a missing folder as absence. The API says 400
// INVALID_ARGUMENT, which reads as "your request was malformed" and sends
// people to check their id format rather than the folder's existence.
func errFolderAbsent(id string) error {
	return fmt.Errorf("no Password Vault folder with id %s (the API reports this as a 400 "+
		"INVALID_ARGUMENT rather than a 404, but it means the folder is not there)", id)
}

// --- websites -------------------------------------------------------------

func newPWVWebsitesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "websites",
		Aliases: []string{"website", "sites"},
		Short:   "Saved websites",
		Long: `The websites saved in the vault, with the credentials linked to them.

` + "`websites get`" + ` READS THE EDIT VIEW. The endpoint the API documents for a
website detail read refuses with a demand for a permission the caller holds —
the same defect that breaks the credential detail read — while the edit view
answers and returns a superset of what the broken endpoint would have. jc uses
the one that works rather than shipping a command that always fails.`,
	}

	cmd.AddCommand(vaultListCmd(
		"List saved websites",
		"List the websites saved in the vault, with their URI, folder and tags.",
		passwordvault.WebsitesEndpoint,
		[]string{"id", "name", "uri", "folderName", "tags", "archived"}))

	cmd.AddCommand(&cobra.Command{
		Use:   "get <id>",
		Short: "One website",
		Long: `Read one website by its 24-character hex id.

Reads the edit view, because the documented detail endpoint refuses with a
demand for a permission the caller holds. The edit view returns a superset —
the access policies and the ids of linked credentials as well as the record
itself — so nothing is lost by the substitution.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultObjectArg("website", args[0]); err != nil {
				return err
			}
			raw, err := pwvGet(cmd.Context(), passwordvault.WebsiteEditEndpoint(args[0]), nil)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "activities <id>",
		Short: "Access history for one website (currently refused by the API)",
		Long: "Refused by the same defect as the website detail endpoint: it demands a\n" +
			"permission the calling user holds.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultObjectArg("website", args[0]); err != nil {
				return err
			}
			raw, err := pwvGet(cmd.Context(), passwordvault.WebsiteActivities(args[0]), nil)
			if err != nil {
				if passwordvault.DetailReadRefused(err) {
					return passwordvault.ErrDetailReadRefused("website activities", "")
				}
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})

	cmd.AddCommand(vaultSubListCmd("managers", "website",
		"Who manages one website",
		"The users who hold management rights over a saved website.",
		passwordvault.WebsiteManagers,
		[]string{"id", "username", "firstName", "lastName"}))

	cmd.AddCommand(newPWVWebsiteSingleCmd("connect-links",
		"Links the browser extension uses to connect",
		`The connect links for a website.

On a website with no linked credentials this answers 400 with an empty message
body — literally "3 INVALID_ARGUMENT: ". jc passes the server's answer through
rather than inventing a meaning for it.`,
		passwordvault.WebsiteConnectLinks))

	cmd.AddCommand(newPWVWebsiteSingleCmd("parameters-extension",
		"Form-filling parameters for the browser extension",
		`The selectors and timings the browser extension uses to fill this site's
login form: which elements hold the username and password, what to hide, how
long to wait, and whether to submit automatically.`,
		passwordvault.WebsiteParamsExtension))

	cmd.AddCommand(vaultColumnsCmd("website", passwordvault.WebsiteColumnsPath))
	return cmd
}

// newPWVWebsiteSingleCmd builds the per-website single-object reads.
func newPWVWebsiteSingleCmd(use, short, long string, endpoint func(string) string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <id>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultObjectArg("website", args[0]); err != nil {
				return err
			}
			raw, err := pwvGet(cmd.Context(), endpoint(args[0]), nil)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
}
