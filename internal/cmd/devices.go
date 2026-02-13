package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
)

// deviceDefaultFields is the default field subset shown for device list/table output.
var deviceDefaultFields = []string{"displayName", "hostname", "os", "osVersion", "lastContact", "agentVersion"}

func newDevicesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "devices",
		Short: "Manage JumpCloud devices",
		Long:  "List, get, and delete JumpCloud systems (devices).",
	}

	cmd.AddCommand(newDevicesListCmd())
	cmd.AddCommand(newDevicesGetCmd())
	cmd.AddCommand(newDevicesDeleteCmd())

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

func newDevicesDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <device-id>",
		Short: "Delete a device",
		Long: `Delete a JumpCloud system (device).

Shows the device's hostname, OS, and last contact date before prompting for
confirmation. Use --force to skip the confirmation prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevicesDelete(cmd, args[0])
		},
	}

	return cmd
}

func runDevicesDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	// Fetch the device first so we can show details in the confirmation prompt.
	deviceData, err := client.Get(cmd.Context(), "/systems/"+identifier)
	if err != nil {
		return err
	}

	var device struct {
		Hostname    string `json:"hostname"`
		OS          string `json:"os"`
		LastContact string `json:"lastContact"`
	}
	if err := json.Unmarshal(deviceData, &device); err != nil {
		return fmt.Errorf("parsing device data: %w", err)
	}

	// Confirmation prompt (unless --force is set).
	if !viper.GetBool("force") {
		lastContact := device.LastContact
		if idx := strings.Index(lastContact, "T"); idx > 0 {
			lastContact = lastContact[:idx]
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete device %s (%s, last contact %s)? [y/N] ",
			device.Hostname, device.OS, lastContact)
		reader := getConfirmReader()
		answer, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading confirmation: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
			return nil
		}
	}

	_, err = client.Delete(cmd.Context(), "/systems/"+identifier)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Device %s deleted successfully.\n", device.Hostname)
	return nil
}
