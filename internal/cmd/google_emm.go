package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/googleemm"
	"github.com/klaassen-consulting/jc/internal/output"
)

// newGoogleEMMCmd builds the `jc google-emm` group.
//
// Reads only. The writes are held back deliberately and the reason is in the
// long help rather than buried here: six of them are device commands
// including erase, and DELETE on an enterprise unbinds the org's whole
// Android Enterprise integration.
func newGoogleEMMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "google-emm",
		Aliases: []string{"gemm", "android-enterprise"},
		Short:   "Inspect JumpCloud's Google EMM (Android Enterprise) integration",
		Long: `Read the org's Android Enterprise setup: the enterprise binding, its
connection status, enrolled devices and enrollment tokens.

Two things about this area are worth knowing before you use it.

THE SPEC DESCRIBES TWO ID SYSTEMS AND THERE IS ONE. It distinguishes
enterpriseId from enterpriseObjectId as though some endpoints took Google's id
and others took JumpCloud's. Every endpoint takes the 24-character objectId,
including connection-status, which the spec calls enterpriseId and which
returns "enterpriseId": "<the objectId>" in its own body. Google's id is
display-only. jc accepts either and sends the right one.

A NON-EXISTENT ENTERPRISE REPORTS ZERO DEVICES RATHER THAN AN ERROR. The
devices endpoint answers {"count":0,"devices":[]} for any well-formed id,
including one no enterprise has ever had, while its siblings under the same
path return 404. So "no devices are enrolled" and "there is no such
enterprise" are the same response. jc confirms the enterprise exists before
reporting any device count, which is why every device command resolves first.

This group is READ-ONLY. The writes are not exposed: six are device commands
including erase-device, and deleting an enterprise unbinds Android Enterprise
for the whole organization.`,
	}
	cmd.AddCommand(
		newGEMMEnterprisesCmd(),
		newGEMMDevicesCmd(),
		newGEMMTokensCmd(),
	)
	return cmd
}

// gemmGet is the shared read path.
func gemmGet(ctx context.Context, endpoint string) (json.RawMessage, error) {
	client, err := newV2Client()
	if err != nil {
		return nil, err
	}
	return client.Get(ctx, endpoint)
}

// resolveGEMMEnterprise adapts the shared resolver to the CLI's client, so
// this and the MCP tools agree about what an operator typed.
func resolveGEMMEnterprise(ctx context.Context, identifier string) (googleemm.Enterprise, error) {
	return googleemm.ResolveEnterprise(ctx, gemmGet, identifier)
}

func newGEMMEnterprisesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "enterprises",
		Aliases: []string{"enterprise"},
		Short:   "Android Enterprise bindings",
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List Android Enterprise bindings",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := gemmGet(cmd.Context(), googleemm.EnterprisesEndpoint)
			if err != nil {
				return err
			}
			rows, total, perr := parseGEMMList(raw, "enterprises")
			if perr != nil {
				return perr
			}
			return writeGEMMList(cmd, rows, total)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <name-google-id-or-object-id>",
		Short: "Get one Android Enterprise binding",
		Long: `Get one enterprise by display name, Google resource name, bare Google id,
or JumpCloud object id.

There is no single-enterprise endpoint — GET /enterprises/{id} is a 404 — so
this lists and filters. Behaves like a get; is not one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := resolveGEMMEnterprise(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			out, merr := json.Marshal(e)
			if merr != nil {
				return merr
			}
			return output.WriteSingle(cmd.OutOrStdout(), out, output.CurrentOptions())
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "connection-status <name-google-id-or-object-id>",
		Short: "Whether the enterprise is still connected to Google",
		Long:  "Returns a bare object, not a list envelope.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := resolveGEMMEnterprise(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			raw, err := gemmGet(cmd.Context(), googleemm.ConnectionStatusEndpoint(e.ObjectID))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})

	return cmd
}

func newGEMMDevicesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "devices",
		Aliases: []string{"device"},
		Short:   "Enrolled Android devices",
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "list <enterprise>",
		Aliases: []string{"ls"},
		Short:   "List devices enrolled in one enterprise",
		Long: `List the Android devices enrolled in an enterprise.

The enterprise is resolved and confirmed first, deliberately. This endpoint
answers {"count":0,"devices":[]} for any well-formed id — including one that
belongs to no enterprise — so an unconfirmed count would be a confident zero
about something that does not exist.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := resolveGEMMEnterprise(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			raw, err := gemmGet(cmd.Context(), googleemm.EnterpriseDevicesEndpoint(e.ObjectID))
			if err != nil {
				return err
			}
			rows, total, perr := googleemm.ParseDevices(raw)
			if perr != nil {
				return perr
			}
			return writeGEMMList(cmd, rows, total)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <device-id>",
		Short: "Get one enrolled Android device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := gemmGet(cmd.Context(), googleemm.DeviceEndpoint(args[0]))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "policy-results <device-id>",
		Short: "Policy application results for one device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := gemmGet(cmd.Context(), googleemm.DevicePolicyResultsEndpoint(args[0]))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})

	return cmd
}

func newGEMMTokensCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "enrollment-tokens",
		Aliases: []string{"tokens", "enrollment-token"},
		Short:   "Enrollment tokens for an enterprise",
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "list <enterprise>",
		Aliases: []string{"ls"},
		Short:   "List enrollment tokens",
		Long:    "Note this endpoint's envelope is {results, totalCount}, unlike the other lists in this area.",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := resolveGEMMEnterprise(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			raw, err := gemmGet(cmd.Context(), googleemm.EnterpriseTokensEndpoint(e.ObjectID))
			if err != nil {
				return err
			}
			rows, total, perr := googleemm.ParseTokens(raw)
			if perr != nil {
				return perr
			}
			return writeGEMMList(cmd, rows, total)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <enterprise> <token-id>",
		Short: "Get one enrollment token",
		Long:  "Token ids are opaque strings, not 24-character hex ids.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := resolveGEMMEnterprise(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			raw, err := gemmGet(cmd.Context(), googleemm.EnterpriseTokenEndpoint(e.ObjectID, args[1]))
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), raw, output.CurrentOptions())
		},
	})

	return cmd
}

// parseGEMMList reads the {count, <key>} envelope the enterprises list uses.
func parseGEMMList(raw json.RawMessage, key string) ([]json.RawMessage, int, error) {
	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, 0, fmt.Errorf("Google EMM %s response is not an object: %w", key, err)
	}
	var rows []json.RawMessage
	if arr, ok := env[key]; ok {
		if err := json.Unmarshal(arr, &rows); err != nil {
			return nil, 0, fmt.Errorf("Google EMM %s is not an array: %w", key, err)
		}
	}
	total := len(rows)
	if c, ok := env["count"]; ok {
		var n int
		if err := json.Unmarshal(c, &n); err == nil {
			total = n
		}
	}
	return rows, total, nil
}

func writeGEMMList(cmd *cobra.Command, rows []json.RawMessage, total int) error {
	opts := output.CurrentOptions()
	if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
		return err
	}
	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d of %d ──\n", len(rows), total)
	}
	return nil
}
