package cmd

import (
	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/workday"
)

// newWorkdayCmd builds the `jc workday` group.
//
// Two commands, not four. The area serves a workers listing and an import
// results endpoint as well, and neither is implemented: no tenant available
// has a Workday integration, so their response shapes have never been seen.
// See workday.Unprobed.
func newWorkdayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workday",
		Aliases: []string{"workdays"},
		Short:   "Inspect JumpCloud's Workday import integrations",
		Long: `List and inspect the org's Workday HR-import integrations.

DELIBERATELY INCOMPLETE. JumpCloud also serves a workers listing and an import
results endpoint for this area. Neither is implemented here, because no tenant
available for testing has a Workday integration configured and their response
shapes have therefore never been observed. Shipping a parser for a response
nobody has seen is a guess, and this area's list endpoint already breaks the
pattern its siblings follow — it returns a bare JSON array rather than an
envelope — so guessing would be a poor bet specifically here.

The writes are not exposed either. Beyond the usual reason, one of them is
POST /workdays/{id}/import, which carries the users scope: it creates and
updates real directory users from HR data, and no dry run for it was found.`,
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List Workday integrations",
		Long:    "Returns a bare array. An org with no Workday integration returns an empty one.",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), workday.Endpoint)
			if err != nil {
				return err
			}
			rows, perr := workday.ParseList(raw)
			if perr != nil {
				return perr
			}
			return output.WriteList(cmd.OutOrStdout(), rows, output.CurrentOptions())
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <id>",
		Short: "Get one Workday integration",
		Long: `Get one integration by its JumpCloud object id.

There is no lookup by name: the integration record has never been observed
with data in it, so which field holds a display name — or whether one exists —
is not established. The id shape is checked locally so a typo reads as a typo
rather than as a 400 from the API.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !workday.IsObjectID(args[0]) {
				return workday.ErrNotObjectID(args[0])
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), workday.IntegrationEndpoint(args[0]))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})

	return cmd
}
