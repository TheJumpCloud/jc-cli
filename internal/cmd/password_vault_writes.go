package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/passwordvault"
	"github.com/klaassen-consulting/jc/internal/plan"
)

// Password Vault writes.
//
// A WRITE RETURNS 200 WITH AN EMPTY BODY WHETHER OR NOT IT DID ANYTHING.
// POST /credentials/archive with {} answers 200 and archives nothing; only
// {"ids":[...]} takes effect. POST /folders/{id}/items with {"ids":[...]}
// answers 200 and adds nothing; the key is "credentialIds". The wrong body
// and the right one are indistinguishable by their responses.
//
// So every command here reads the record back afterwards and reports what the
// server actually holds. Operations whose effect cannot be read back are not
// shipped — see passwordvault.HeldBackWrites — because there would be no way
// to tell the caller the truth about them.
//
// Every create grants the caller the family's full management set. An object
// created without it can afterwards be neither deleted nor repaired through
// the API.

func pwvPost(ctx context.Context, endpoint string, body any) (json.RawMessage, error) {
	if err := requireVaultActive(ctx); err != nil {
		return nil, err
	}
	client, err := newV2Client()
	if err != nil {
		return nil, err
	}
	return client.Create(ctx, endpoint, body)
}

func pwvPut(ctx context.Context, endpoint string, body any) (json.RawMessage, error) {
	if err := requireVaultActive(ctx); err != nil {
		return nil, err
	}
	client, err := newV2Client()
	if err != nil {
		return nil, err
	}
	return client.Update(ctx, endpoint, body)
}

func pwvDelete(ctx context.Context, endpoint string) (json.RawMessage, error) {
	if err := requireVaultActive(ctx); err != nil {
		return nil, err
	}
	client, err := newV2Client()
	if err != nil {
		return nil, err
	}
	return client.Delete(ctx, endpoint)
}

// vaultSelfID returns the caller's INTEGER vault user id, which every access
// policy needs. It is not the JumpCloud user object id.
func vaultSelfID(ctx context.Context) (int, error) {
	raw, err := pwvGet(ctx, passwordvault.UsersSelfEndpoint, nil)
	if err != nil {
		return 0, err
	}
	var self struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(raw, &self); err != nil {
		return 0, fmt.Errorf("decoding your Password Vault user record: %w", err)
	}
	if self.ID <= 0 {
		return 0, fmt.Errorf("your Password Vault user record carries no id; " +
			"an access policy cannot be built without it")
	}
	return self.ID, nil
}

// createdID reads the {"id": "..."} a create returns — the entire response.
func createdID(raw json.RawMessage) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decoding the create response: %w", err)
	}
	if out.ID == "" {
		return "", fmt.Errorf("the create returned no id, so nothing can be confirmed — " +
			"check the vault before retrying, since this API reports success regardless")
	}
	return out.ID, nil
}

// presentInList reports whether id appears in a family listing. An undecodable
// row makes the answer unknown, and unknown is not "absent".
func presentInList(ctx context.Context, endpoint, id string) (bool, error) {
	raw, err := pwvGet(ctx, endpoint, nil)
	if err != nil {
		return false, err
	}
	rows, _, err := passwordvault.ParseList(raw, "the listing")
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		var rec struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(r, &rec); err != nil {
			return false, fmt.Errorf("a record in the listing could not be read, so the "+
				"write could not be confirmed: %w", err)
		}
		if rec.ID == id {
			return true, nil
		}
	}
	return false, nil
}

// confirmWrite verifies a write by reading the listing back.
func confirmWrite(cmd *cobra.Command, ctx context.Context, endpoint, what, id string, wantPresent bool) error {
	present, err := presentInList(ctx, endpoint, id)
	if err != nil {
		return err
	}
	if present != wantPresent {
		state := "absent"
		if present {
			state = "still present"
		}
		return fmt.Errorf("the API reported success, but %s %s is %s afterwards — the write "+
			"did NOT take effect. %s", what, id, state, passwordvault.WriteReportsSuccessRegardless)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "confirmed: %s %s\n", what, id)
	return nil
}

// vaultConfirm handles --plan and the confirmation prompt.
func vaultConfirm(cmd *cobra.Command, action, resource, target string, effects []string) (bool, error) {
	if viper.GetBool("plan") {
		return false, renderPlan(cmd, &plan.Plan{
			Action: action, Resource: resource, Target: target,
			Effects: effects, Reversible: false,
		})
	}
	if mustAbortWithoutTTY() {
		return false, fmt.Errorf("%s %s requires --force or --non-interactive "+
			"(or preview with --plan first)", action, target)
	}
	if shouldConfirm() {
		ok, err := askYesNo(cmd, fmt.Sprintf("%s %s %s?", action, resource, target))
		if err != nil {
			return false, err
		}
		if !ok {
			fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
			return false, nil
		}
	}
	return true, nil
}

// lockoutLong is the shared explanation on every create.
const lockoutLong = "\n\nThis grants you the family's full management permission set, and jc\n" +
	"offers no way not to: an object created without it can afterwards be neither\n" +
	"deleted nor have its permissions changed through the API. There is no recovery\n" +
	"path, and objects stranded that way are invisible to the listing as well.\n\n" +
	"The write is read back afterwards and the command reports what the vault\n" +
	"actually holds, because this API answers 200 whether or not a write took effect."

// --- credential writes ----------------------------------------------------

func newPWVCredentialWriteCmds() []*cobra.Command {
	var credType, username, notes string

	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a credential",
		Long:  "Create a credential in the vault." + lockoutLong,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ok, err := vaultConfirm(cmd, "create", "credential", args[0],
				[]string{"type: " + credType, "grants you Manage, View Detail and Connect"})
			if err != nil || !ok {
				return err
			}
			selfID, err := vaultSelfID(cmd.Context())
			if err != nil {
				return err
			}
			policy, err := passwordvault.AccessPolicyFor(selfID, "credential")
			if err != nil {
				return err
			}
			body := map[string]any{
				"name":           args[0],
				"credentialType": credType,
				"accessPolicies": policy,
			}
			if username != "" {
				body["username"] = username
			}
			if notes != "" {
				body["notes"] = notes
			}
			raw, err := pwvPut(cmd.Context(), passwordvault.CredentialsEndpoint, body)
			if err != nil {
				return err
			}
			id, err := createdID(raw)
			if err != nil {
				return err
			}
			if err := confirmWrite(cmd, cmd.Context(), passwordvault.CredentialsEndpoint,
				"credential", id, true); err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
	create.Flags().StringVar(&credType, "type", "PASSWORD",
		"Credential type: PASSWORD, KEY, CONNECTION_STRING, SECURE_NOTE, TWO_FA, CREDIT_CARD, ID_CARD, IDENTITY")
	create.Flags().StringVar(&username, "username", "", "Username stored on the credential")
	create.Flags().StringVar(&notes, "notes", "", "Free-text notes")

	del := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a credential",
		Long: `Delete a credential by its 24-character hex id.

The deletion is confirmed by reading the listing back, because this API
answers 200 whether or not a write took effect.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultObjectArg("credential", args[0]); err != nil {
				return err
			}
			ok, err := vaultConfirm(cmd, "delete", "credential", args[0], []string{"permanent"})
			if err != nil || !ok {
				return err
			}
			if _, err := pwvDelete(cmd.Context(), passwordvault.CredentialEndpoint(args[0])); err != nil {
				return err
			}
			return confirmWrite(cmd, cmd.Context(), passwordvault.CredentialsEndpoint,
				"credential", args[0], false)
		},
	}

	archive := newPWVArchiveCmd("archive", "Archive credentials",
		passwordvault.CredentialsEndpoint+"/archive", true)
	unarchive := newPWVArchiveCmd("unarchive", "Restore archived credentials",
		passwordvault.CredentialsEndpoint+"/unarchive", false)

	clone := &cobra.Command{
		Use:     "clone <id>",
		Aliases: []string{"duplicate"},
		Short:   "Duplicate a credential",
		Long: `Duplicate a credential, secret included.

The copy is a full credential in its own right, so it is one more place the
secret exists. It is confirmed by reading the listing back.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultObjectArg("credential", args[0]); err != nil {
				return err
			}
			ok, err := vaultConfirm(cmd, "duplicate", "credential", args[0],
				[]string{"copies the secret into a second credential"})
			if err != nil || !ok {
				return err
			}
			raw, err := pwvPost(cmd.Context(),
				passwordvault.CredentialEndpoint(args[0])+"/clones", map[string]any{})
			if err != nil {
				return err
			}
			id, err := createdID(raw)
			if err != nil {
				return err
			}
			if err := confirmWrite(cmd, cmd.Context(), passwordvault.CredentialsEndpoint,
				"credential copy", id, true); err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}

	return []*cobra.Command{create, del, archive, unarchive, clone}
}

// newPWVArchiveCmd builds the archive and unarchive commands, which take the
// same body and differ only in endpoint and wording.
func newPWVArchiveCmd(use, short, endpoint string, archiving bool) *cobra.Command {
	verb := "archive"
	if !archiving {
		verb = "restore"
	}
	return &cobra.Command{
		Use:   use + " <id> [id...]",
		Short: short,
		Long: `` + capitaliseFirst(verb) + ` one or more credentials by id.

An archived credential disappears from the default listing; it is still there
under --filter archived:eq:true.

THE BODY KEY MATTERS AND THE API WILL NOT TELL YOU. This endpoint takes
{"ids": [...]}; a request with an empty or differently-keyed body answers 200
and does nothing at all. jc refuses an empty id list rather than sending one,
and reads the archived listing back afterwards to confirm.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := passwordvault.ArchiveBody(args)
			if err != nil {
				return err
			}
			ok, err := vaultConfirm(cmd, verb, "credentials", fmt.Sprintf("%d", len(args)),
				[]string{fmt.Sprintf("%d credential(s)", len(args))})
			if err != nil || !ok {
				return err
			}
			if _, err := pwvPost(cmd.Context(), endpoint, json.RawMessage(body)); err != nil {
				return err
			}
			// Confirm against the archived view: presence there is the
			// observable effect, and a 200 alone proves nothing.
			raw, err := pwvGet(cmd.Context(), passwordvault.CredentialsEndpoint,
				mustValues("filter", "archived:eq:true"))
			if err != nil {
				return err
			}
			rows, _, err := passwordvault.ParseList(raw, "archived credentials")
			if err != nil {
				return err
			}
			archived := map[string]bool{}
			for _, r := range rows {
				var rec struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(r, &rec); err != nil {
					return fmt.Errorf("an archived record could not be read, so the write "+
						"could not be confirmed: %w", err)
				}
				archived[rec.ID] = true
			}
			for _, id := range args {
				if archived[id] != archiving {
					return fmt.Errorf("the API reported success, but credential %s is %s "+
						"archived afterwards — the write did NOT take effect. %s", id,
						map[bool]string{true: "still", false: "not"}[archived[id]],
						passwordvault.WriteReportsSuccessRegardless)
				}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "confirmed: %d credential(s) %sd\n", len(args), verb)
			return nil
		},
	}
}

func capitaliseFirst(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 32
	}
	return string(b)
}

// --- folder writes --------------------------------------------------------

func newPWVFolderWriteCmds() []*cobra.Command {
	var description string

	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a folder",
		Long: "Create a vault folder.\n\nFolders use their own permission vocabulary — " +
			"Folder.Manage, Folder.Item.Manage,\nFolder.View — where credentials and websites " +
			"use Manage and View Detail.\nPassing the credential vocabulary to a folder is " +
			"accepted and grants nothing," + "\nwhich is one of the ways an object gets " +
			"stranded." + lockoutLong,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ok, err := vaultConfirm(cmd, "create", "folder", args[0],
				[]string{"grants you Folder.Manage, Folder.Item.Manage and Folder.View"})
			if err != nil || !ok {
				return err
			}
			selfID, err := vaultSelfID(cmd.Context())
			if err != nil {
				return err
			}
			policy, err := passwordvault.AccessPolicyFor(selfID, "folder")
			if err != nil {
				return err
			}
			inner := map[string]any{"name": args[0], "accessPolicies": policy}
			if description != "" {
				inner["description"] = description
			}
			raw, err := pwvPost(cmd.Context(), passwordvault.FoldersEndpoint,
				map[string]any{"folder": inner})
			if err != nil {
				return err
			}
			id, err := createdID(raw)
			if err != nil {
				return err
			}
			if err := confirmWrite(cmd, cmd.Context(), passwordvault.FoldersEndpoint,
				"folder", id, true); err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
	create.Flags().StringVar(&description, "description", "", "Folder description")

	rename := &cobra.Command{
		Use:   "rename <id> <new-name>",
		Short: "Rename a folder",
		Long: `Change a folder's name.

This is PUT /folders/{id}/properties, which carries only the folder's own
properties — it does not touch access policies or contents.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultObjectArg("folder", args[0]); err != nil {
				return err
			}
			ok, err := vaultConfirm(cmd, "rename", "folder", args[0],
				[]string{"new name: " + args[1]})
			if err != nil || !ok {
				return err
			}
			if _, err := pwvPut(cmd.Context(),
				passwordvault.FolderEndpoint(args[0])+"/properties",
				map[string]any{"name": args[1]}); err != nil {
				return err
			}
			// Read the name back: a 200 here proves nothing.
			raw, err := pwvGet(cmd.Context(), passwordvault.FolderEndpoint(args[0]), nil)
			if err != nil {
				return err
			}
			inner, err := passwordvault.ParseWrappedFolder(raw)
			if err != nil {
				return err
			}
			var got struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(inner, &got); err != nil {
				return fmt.Errorf("decoding the folder after renaming: %w", err)
			}
			if got.Name != args[1] {
				return fmt.Errorf("the API reported success, but the folder is still called %q — "+
					"the rename did NOT take effect. %s", got.Name,
					passwordvault.WriteReportsSuccessRegardless)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "confirmed: folder %s renamed to %q\n", args[0], got.Name)
			return output.WriteSingle(cmd.OutOrStdout(), inner, output.CurrentOptions())
		},
	}

	del := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a folder",
		Long: `Delete a folder by its 24-character hex id.

Confirmed by reading the listing back afterwards.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultObjectArg("folder", args[0]); err != nil {
				return err
			}
			ok, err := vaultConfirm(cmd, "delete", "folder", args[0], []string{"permanent"})
			if err != nil || !ok {
				return err
			}
			if _, err := pwvDelete(cmd.Context(), passwordvault.FolderEndpoint(args[0])); err != nil {
				if passwordvault.FolderNotFoundIs400(err) {
					return errFolderAbsent(args[0])
				}
				return err
			}
			return confirmWrite(cmd, cmd.Context(), passwordvault.FoldersEndpoint,
				"folder", args[0], false)
		},
	}

	addItems := &cobra.Command{
		Use:   "add-items <folderId> <credentialId> [credentialId...]",
		Short: "Put credentials into a folder",
		Long: `Add one or more credentials to a folder.

THE BODY KEY MATTERS AND THE API WILL NOT TELL YOU. This endpoint takes
{"credentialIds": [...]}. Sending {"ids": [...]} — which is what the archive
endpoints take — answers 200 and adds nothing. jc sends the right key and
reads the folder's item count back to confirm.`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			folderID, ids := args[0], args[1:]
			if err := vaultObjectArg("folder", folderID); err != nil {
				return err
			}
			body, err := passwordvault.FolderItemsBody(ids)
			if err != nil {
				return err
			}
			ok, err := vaultConfirm(cmd, "add", "credentials to folder", folderID,
				[]string{fmt.Sprintf("%d credential(s)", len(ids))})
			if err != nil || !ok {
				return err
			}
			if _, err := pwvPost(cmd.Context(),
				passwordvault.FolderItemsEndpoint(folderID), json.RawMessage(body)); err != nil {
				return err
			}
			raw, err := pwvGet(cmd.Context(), passwordvault.FolderItemsEndpoint(folderID), nil)
			if err != nil {
				return err
			}
			rows, _, err := passwordvault.ParseList(raw, "folder items")
			if err != nil {
				return err
			}
			inFolder := map[string]bool{}
			for _, r := range rows {
				var rec struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(r, &rec); err != nil {
					return fmt.Errorf("a folder item could not be read, so the write could "+
						"not be confirmed: %w", err)
				}
				inFolder[rec.ID] = true
			}
			for _, id := range ids {
				if !inFolder[id] {
					return fmt.Errorf("the API reported success, but credential %s is not in "+
						"the folder afterwards — the write did NOT take effect. %s", id,
						passwordvault.WriteReportsSuccessRegardless)
				}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "confirmed: %d credential(s) in folder %s\n",
				len(ids), folderID)
			return nil
		},
	}

	return []*cobra.Command{create, rename, del, addItems}
}

// --- website writes -------------------------------------------------------

func newPWVWebsiteWriteCmds() []*cobra.Command {
	var uri, notes string

	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Save a website",
		Long: "Save a website in the vault.\n\nWebsites use the CREDENTIAL permission " +
			"vocabulary — Manage and View Detail —\nnot the namespaced Folder.* form." + lockoutLong,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if uri == "" {
				return fmt.Errorf("--uri is required: a saved website with no address cannot be matched to anything")
			}
			ok, err := vaultConfirm(cmd, "create", "website", args[0],
				[]string{"uri: " + uri, "grants you Manage, View Detail and Connect"})
			if err != nil || !ok {
				return err
			}
			selfID, err := vaultSelfID(cmd.Context())
			if err != nil {
				return err
			}
			policy, err := passwordvault.AccessPolicyFor(selfID, "website")
			if err != nil {
				return err
			}
			inner := map[string]any{"name": args[0], "uri": uri, "accessPolicies": policy}
			if notes != "" {
				inner["notes"] = notes
			}
			raw, err := pwvPost(cmd.Context(), passwordvault.WebsitesEndpoint,
				map[string]any{"website": inner})
			if err != nil {
				return err
			}
			id, err := createdID(raw)
			if err != nil {
				return err
			}
			if err := confirmWrite(cmd, cmd.Context(), passwordvault.WebsitesEndpoint,
				"website", id, true); err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}
	create.Flags().StringVar(&uri, "uri", "", "The site's address (required)")
	create.Flags().StringVar(&notes, "notes", "", "Free-text notes")

	del := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a saved website",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultObjectArg("website", args[0]); err != nil {
				return err
			}
			ok, err := vaultConfirm(cmd, "delete", "website", args[0], []string{"permanent"})
			if err != nil || !ok {
				return err
			}
			if _, err := pwvDelete(cmd.Context(), passwordvault.WebsiteEndpoint(args[0])); err != nil {
				return err
			}
			return confirmWrite(cmd, cmd.Context(), passwordvault.WebsitesEndpoint,
				"website", args[0], false)
		},
	}

	clone := &cobra.Command{
		Use:     "clone <id>",
		Aliases: []string{"duplicate"},
		Short:   "Duplicate a saved website",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultObjectArg("website", args[0]); err != nil {
				return err
			}
			ok, err := vaultConfirm(cmd, "duplicate", "website", args[0], nil)
			if err != nil || !ok {
				return err
			}
			raw, err := pwvPost(cmd.Context(),
				passwordvault.WebsiteEndpoint(args[0])+"/clones", map[string]any{})
			if err != nil {
				return err
			}
			id, err := createdID(raw)
			if err != nil {
				return err
			}
			if err := confirmWrite(cmd, cmd.Context(), passwordvault.WebsitesEndpoint,
				"website copy", id, true); err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	}

	return []*cobra.Command{create, del, clone}
}

// mustValues builds a one-pair query string.
func mustValues(k, v string) url.Values {
	out := url.Values{}
	out.Set(k, v)
	return out
}
