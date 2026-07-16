package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
)

// directoriesDefaultFields is the list-view subset. "health" is
// synthesized from oAuthStatus (see flattenDirectoryHealth) because
// the raw nested object is useless at a glance — the whole point of
// this view is spotting broken integrations.
var directoriesDefaultFields = []string{"id", "name", "type", "health"}

func newDirectoriesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "directories",
		Short: "Unified view of all directory integrations (with OAuth health)",
		Long: `List every directory integration on the tenant in one view — Google
Workspace, Microsoft 365, LDAP, Active Directory, and HRIS providers —
via GET /api/v2/directories.

Each entry carries a synthesized "health" field: ok, or the OAuth
error when the integration's grant is broken (expired/revoked consent
shows up here long before sync failures get noticed). Read-only;
manage individual integrations with their dedicated commands
(jc gsuite / office365 / ldap / ad) or the Admin Portal.`,
	}
	cmd.AddCommand(newDirectoriesListCmd())
	return cmd
}

func newDirectoriesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all directory integrations with OAuth health",
		RunE:    runDirectoriesList,
	}
}

func runDirectoriesList(cmd *cobra.Command, args []string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/directories", api.V2ListOptions{})
	if err != nil {
		return err
	}

	flattened := make([]json.RawMessage, 0, len(result.Data))
	for _, raw := range result.Data {
		out, err := flattenDirectoryHealth(raw)
		if err != nil {
			return err
		}
		flattened = append(flattened, out)
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = directoriesDefaultFields
	if err := output.WriteList(cmd.OutOrStdout(), flattened, opts); err != nil {
		return err
	}
	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(flattened))
	}
	return nil
}

// flattenDirectoryHealth injects a top-level "health" field derived
// from oAuthStatus: "ok" when absent or error-free, otherwise
// "error: <message>". The original fields (incl. the full nested
// oAuthStatus) are preserved for --fields/-a consumers.
func flattenDirectoryHealth(raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("decoding directory entry: %w", err)
	}
	obj["health"] = directoryHealth(obj)
	return json.Marshal(obj)
}

func directoryHealth(obj map[string]any) string {
	status, ok := obj["oAuthStatus"].(map[string]any)
	if !ok {
		return "ok" // non-OAuth integration types (LDAP, AD) or healthy omission
	}
	errCode, _ := status["error"].(string)
	if errCode == "" {
		return "ok"
	}
	if msg, _ := status["errorMessage"].(string); msg != "" {
		const maxMsg = 80
		if len(msg) > maxMsg {
			msg = msg[:maxMsg] + "…"
		}
		return "error: " + errCode + " — " + msg
	}
	return "error: " + errCode
}
