package cmd

import (
	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
)

// deviceDefaultFields is the default field subset shown for device list/table output.
var deviceDefaultFields = []string{"displayName", "hostname", "os", "osVersion", "lastContact", "agentVersion"}

func newDevicesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "devices",
		Short: "Manage JumpCloud devices",
		Long:  "List and get JumpCloud systems (devices).",
	}

	cmd.AddCommand(newDevicesListCmd())
	cmd.AddCommand(newDevicesGetCmd())

	return cmd
}

func newDevicesListCmd() *cobra.Command {
	var (
		limitFlag int
		sortFlag  string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all devices",
		Long: `List all JumpCloud systems (devices).

Default fields: displayName, hostname, os, osVersion, lastContact, agentVersion.
Use --output table for a readable ASCII table.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevicesList(cmd, limitFlag, sortFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -lastContact)")

	return cmd
}

func runDevicesList(cmd *cobra.Command, limit int, sort string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/systems", api.ListOptions{
		Limit: limit,
		Sort:  sort,
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = deviceDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(result.Data), result.TotalCount)
	}

	return nil
}

func newDevicesGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <hostname-or-id>",
		Short: "Get a device by hostname or ID",
		Long: `Get a single JumpCloud system (device) by ID.

Accepts a 24-character hex system ID. Name resolution (hostname → ID)
will be available in a future release.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevicesGet(cmd, args[0])
		},
	}

	return cmd
}

func runDevicesGet(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/systems/"+identifier)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}
