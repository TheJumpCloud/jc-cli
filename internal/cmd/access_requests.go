package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
)

var accessRequestDefaultFields = []string{"accessId", "requestorId", "resourceId", "accessState", "expiry"}

func newAccessRequestsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "access-requests",
		Aliases: []string{"ar"},
		Short:   "Manage JumpCloud access requests",
		Long:    "List, get, create, update, and revoke JumpCloud temporary elevated device privilege requests.",
	}

	cmd.AddCommand(newAccessRequestsListCmd())
	cmd.AddCommand(newAccessRequestsGetCmd())
	cmd.AddCommand(newAccessRequestsCreateCmd())
	cmd.AddCommand(newAccessRequestsUpdateCmd())
	cmd.AddCommand(newAccessRequestsRevokeCmd())

	return cmd
}

func newAccessRequestsListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all access requests",
		Long: `List all JumpCloud access requests.

Default fields: accessId, requestorId, resourceId, accessState, expiry.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'accessState:eq:granted'     Granted requests only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAccessRequestsList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'accessState:eq:granted')")

	return cmd
}

func runAccessRequestsList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/accessrequests", api.V2ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV2Queries(exprs),
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = accessRequestDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newAccessRequestsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <access-id>",
		Short: "Get an access request by ID",
		Long: `Get a single JumpCloud access request by its access ID.

Accepts the 24-character hex access ID returned when creating a request.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAccessRequestsGet(cmd, args[0])
		},
	}
	return cmd
}

func runAccessRequestsGet(cmd *cobra.Command, id string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	result, err := client.Get(cmd.Context(), "/accessrequests/"+id)
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAccessRequestsCreateCmd() *cobra.Command {
	var (
		userFlag        string
		deviceFlag      string
		expiryFlag      string
		sudoFlag        bool
		sudoNoPasswdFlag bool
		remarksFlag     string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an access request",
		Long: `Create a JumpCloud access request for temporary elevated device privileges.

Required flags: --user, --device, --expiry.
The --user and --device flags accept names or 24-character hex IDs.

Examples:
  jc access-requests create --user alice --device JDOE-MBP --expiry 2026-04-01T00:00:00Z
  jc access-requests create --user aabbccddee112233aabb1001 --device aabbccddee112233aabb2001 --expiry 2026-04-01T00:00:00Z --sudo`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAccessRequestsCreate(cmd, userFlag, deviceFlag, expiryFlag, sudoFlag, sudoNoPasswdFlag, remarksFlag)
		},
	}

	cmd.Flags().StringVar(&userFlag, "user", "", "User name or ID (required)")
	cmd.Flags().StringVar(&deviceFlag, "device", "", "Device hostname or ID (required)")
	cmd.Flags().StringVar(&expiryFlag, "expiry", "", "Expiry timestamp in RFC 3339 format (required)")
	cmd.Flags().BoolVar(&sudoFlag, "sudo", false, "Request sudo access")
	cmd.Flags().BoolVar(&sudoNoPasswdFlag, "sudo-nopasswd", false, "Request passwordless sudo access")
	cmd.Flags().StringVar(&remarksFlag, "remarks", "", "Optional remarks for the request")
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("device")
	_ = cmd.MarkFlagRequired("expiry")

	return cmd
}

func runAccessRequestsCreate(cmd *cobra.Command, user, device, expiry string, sudo, sudoNoPasswd bool, remarks string) error {
	if viper.GetBool("plan") {
		effects := []string{
			"user: " + user,
			"device: " + device,
			"expiry: " + expiry,
		}
		if sudo || sudoNoPasswd {
			effects = append(effects, "sudo: enabled")
		}
		if remarks != "" {
			effects = append(effects, "remarks: "+remarks)
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "access request",
			Target:     user + " → " + device,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	// Resolve user name/ID.
	v1client, err := newV1Client()
	if err != nil {
		return err
	}

	userID, err := resolveUser(cmd.Context(), v1client, user)
	if err != nil {
		return err
	}

	deviceID, err := resolveDevice(cmd.Context(), v1client, device)
	if err != nil {
		return err
	}

	body := map[string]any{
		"requestorId":  userID,
		"resourceId":   deviceID,
		"resourceType": "device",
		"expiry":       expiry,
	}
	if remarks != "" {
		body["remarks"] = remarks
	}
	if sudo || sudoNoPasswd {
		body["additionalAttributes"] = map[string]any{
			"sudo": map[string]any{
				"enabled":         sudo || sudoNoPasswd,
				"withoutPassword": sudoNoPasswd,
			},
		}
	}

	v2client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := v2client.Create(cmd.Context(), "/accessrequests", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAccessRequestsUpdateCmd() *cobra.Command {
	var (
		expiryFlag  string
		remarksFlag string
	)

	cmd := &cobra.Command{
		Use:   "update <access-id>",
		Short: "Update an access request",
		Long: `Update a JumpCloud access request by its access ID.

At least one field flag must be specified.

Examples:
  jc access-requests update aabbccddee112233aabb0001 --expiry 2026-05-01T00:00:00Z
  jc access-requests update aabbccddee112233aabb0001 --remarks "Extended by admin"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAccessRequestsUpdate(cmd, args[0], expiryFlag, remarksFlag)
		},
	}

	cmd.Flags().StringVar(&expiryFlag, "expiry", "", "New expiry timestamp in RFC 3339 format")
	cmd.Flags().StringVar(&remarksFlag, "remarks", "", "Updated remarks")

	return cmd
}

func runAccessRequestsUpdate(cmd *cobra.Command, id, expiry, remarks string) error {
	body := map[string]any{}

	if cmd.Flags().Changed("expiry") {
		body["expiry"] = expiry
	}
	if cmd.Flags().Changed("remarks") {
		body["remarks"] = remarks
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --expiry, --remarks)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, fmt.Sprintf("%s: %v", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "access request",
			Target:     id,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/accessrequests/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newAccessRequestsRevokeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <access-id>",
		Short: "Revoke an access request",
		Long: `Revoke a JumpCloud access request by its access ID.

This removes temporary elevated privileges from the user.
Requires confirmation unless --force is set.

Examples:
  jc access-requests revoke aabbccddee112233aabb0001
  jc access-requests revoke aabbccddee112233aabb0001 --force`,
		Args: cobra.MaximumNArgs(1),
		RunE: batchRunE("access request", "revoke", runAccessRequestsRevoke),
	}
	addBatchSourceFlags(cmd)
	return cmd
}

func runAccessRequestsRevoke(cmd *cobra.Command, id string) error {
	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "revoke",
			Resource: "access request",
			Target:   id,
			Effects:  []string{"Remove temporary elevated privileges"},
		}
		return renderPlan(cmd, p)
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Revoke access request %q? [y/N] ", id)
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

	client, err := newV2Client()
	if err != nil {
		return err
	}

	_, err = client.Create(cmd.Context(), "/accessrequests/"+id+"/revoke", map[string]any{})
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Access request %q revoked successfully.\n", id)
	return nil
}
