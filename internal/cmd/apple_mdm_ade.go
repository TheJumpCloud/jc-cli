package cmd

// Automated Device Enrollment, Apple Business Manager and Account-Driven User
// Enrollment: the read commands.
//
// None of these endpoints pages. Each answers one envelope carrying its rows
// and a totalCount, so they go through client.Get and the shared parsers in
// internal/apple_mdm rather than ListAll — which would look for a Link header
// that is never there and fall back to skip paging that these routes ignore.

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/apple_mdm"
	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

// adeTokenDefaultFields leads with what an operator is actually checking:
// which ABM org the token is for, and when it expires.
var adeTokenDefaultFields = []string{"objectId", "orgName", "serverName", "serverTokenExpiry", "deviceCount", "lastSyncedAt"}

var abmDeviceDefaultFields = []string{"serialNumber", "model", "deviceAssignedDate", "profileStatus"}

// backgroundTaskDefaultFields keeps this readable. The full record carries
// fifteen columns including a code signature and a path decorated twice; what
// an operator wants is which task, what state it is in, and whether it is a
// daemon or an agent.
var backgroundTaskDefaultFields = []string{"identifier", "label", "state", "type", "ownerUID"}

func newAppleMDMADETokensCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ade-tokens",
		Aliases: []string{"ade-token"},
		Short:   "Automated Device Enrollment tokens",
		Long: `Inspect the Automated Device Enrollment (ADE) tokens linking this
organization to Apple Business Manager.

A token is what lets a device bought through ABM enroll itself. When one
expires, DEP enrollment stops for every device behind it, so ` + "`stats`" + ` is
usually the first thing worth reading.`,
	}
	cmd.AddCommand(newADETokensListCmd())
	cmd.AddCommand(newADETokenGetCmd())
	cmd.AddCommand(newADETokensStatsCmd())
	cmd.AddCommand(newADETokenOrgNamesCmd())
	return cmd
}

func newADETokensListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List Automated Device Enrollment tokens",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), apple_mdm.ADETokensEndpoint)
			if err != nil {
				return err
			}
			rows, total, err := apple_mdm.ParseADETokens(raw)
			if err != nil {
				return err
			}
			return writeADERows(cmd, rows, total, adeTokenDefaultFields, "ADE token", "ADE tokens")
		},
	}
}

func newADETokenGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <token-id>",
		Short: "Get one Automated Device Enrollment token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			if err := apple_mdm.RequireObjectID("an ADE token", args[0]); err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), apple_mdm.ADETokenEndpoint(args[0]))
			if err != nil {
				return err
			}
			one, err := apple_mdm.ParseADEToken(raw)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), one, output.CurrentOptions())
		},
	}
}

func newADETokensStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Summarise ADE token health",
		Long: `Count the organization's ADE tokens by state.

The total is not a health check. A tenant with one token read totalCount 1,
expiringCount 1 and validCount 0 at the same time — counted, expiring, and not
valid. So this prints a verdict alongside the numbers rather than leaving
"1 token" to be read as "enrollment works".`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), apple_mdm.ADETokensStatsEndpoint)
			if err != nil {
				return err
			}
			stats, err := apple_mdm.ParseADETokenStats(raw)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			if err := output.WriteSingle(cmd.OutOrStdout(), raw, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly && stats.NeedsAttention() {
				fmt.Fprintf(cmd.ErrOrStderr(), "── %s ──\n", describeADEHealth(stats))
			}
			return nil
		},
	}
}

// describeADEHealth says which of the counts is the problem, because
// "needs attention" on its own sends the reader back to the numbers.
func describeADEHealth(s apple_mdm.ADETokenStats) string {
	switch {
	case s.ExpiredCount > 0 && s.ExpiringCount > 0:
		return fmt.Sprintf("%d expired and %d expiring: DEP enrollment is already failing for some devices",
			s.ExpiredCount, s.ExpiringCount)
	case s.ExpiredCount > 0:
		return fmt.Sprintf("%d expired: DEP enrollment is failing for devices behind %s",
			s.ExpiredCount, pluralTokens(s.ExpiredCount))
	case s.ExpiringCount > 0:
		return fmt.Sprintf("%d expiring: renew in Apple Business Manager before DEP enrollment stops",
			s.ExpiringCount)
	default:
		return fmt.Sprintf("%d tokens, none valid: DEP enrollment will not work", s.TotalCount)
	}
}

func pluralTokens(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

func newADETokenOrgNamesCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "org-names",
		Aliases: []string{"orgs"},
		Short:   "List the Apple Business Manager organizations behind the ADE tokens",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), apple_mdm.ADETokenOrgNamesEndpoint)
			if err != nil {
				return err
			}
			names, total, err := apple_mdm.ParseOrgNames(raw)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			// A bare list of strings has no fields to select, so it is
			// emitted as objects with one named column rather than handed to
			// the field machinery as scalars.
			rows := make([]json.RawMessage, 0, len(names))
			for _, n := range names {
				b, err := json.Marshal(map[string]string{"orgName": n})
				if err != nil {
					return err
				}
				rows = append(rows, b)
			}
			if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
				return err
			}
			if !opts.Quiet && !opts.IDsOnly {
				writeNounListFooter(cmd, len(rows), total, "organization", "organizations")
			}
			return nil
		},
	}
}

func newAppleMDMADESettingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ade-settings",
		Aliases: []string{"ade-setting"},
		Short:   "Automated Device Enrollment settings per OS family",
		Long: `Read the enrollment experience configured for an ADE token — the setup
assistant panes that are skipped, the welcome screen text, whether enrollment
is mandatory.

Settings are held per OS family. The API names them DARWIN and IOS, not macOS
and iOS; jc accepts either spelling. The spec also lists UNKNOWN, which the API
answers with HTTP 400, so jc refuses it here rather than passing it on.`,
	}
	cmd.AddCommand(newADESettingsListCmd())
	cmd.AddCommand(newADESettingGetCmd())
	return cmd
}

func newADESettingsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list <token-id>",
		Aliases: []string{"ls"},
		Short:   "List ADE settings for every OS family on a token",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			if err := apple_mdm.RequireObjectID("an ADE token", args[0]); err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), apple_mdm.ADESettingsEndpoint(args[0]))
			if err != nil {
				return err
			}
			rows, total, err := apple_mdm.ParseADESettings(raw)
			if err != nil {
				return err
			}
			return writeADERows(cmd, rows, total, nil, "OS family", "OS families")
		},
	}
}

func newADESettingGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <token-id> <os-family>",
		Short: "Get ADE settings for one OS family",
		Example: `  jc apple-mdm ade-settings get 6a7e1bb88cf0be0001a4e3cd macos
  jc apple-mdm ade-settings get 6a7e1bb88cf0be0001a4e3cd ios`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := apple_mdm.RequireObjectID("an ADE token", args[0]); err != nil {
				return err
			}
			osFamily, err := apple_mdm.NormalizeOSFamily(args[1])
			if err != nil {
				return err
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), apple_mdm.ADESettingEndpoint(args[0], osFamily))
			if err != nil {
				return err
			}
			one, err := apple_mdm.ParseADESetting(raw)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), one, output.CurrentOptions())
		},
	}
}

func newAppleMDMABMDevicesCmd() *cobra.Command {
	var appleMDM string

	cmd := &cobra.Command{
		Use:     "abm-devices",
		Aliases: []string{"abm"},
		Short:   "List devices assigned in Apple Business Manager",
		Long: `List the devices Apple Business Manager has assigned to this organization.

These are ABM assignments, not enrolled devices: a device appears here once it
is assigned to a JumpCloud MDM server in ABM, whether or not it has ever been
switched on. For devices that have actually enrolled, use ` + "`jc apple-mdm devices`" + `.

Without --apple-mdm the list is organization-wide.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			endpoint := apple_mdm.ABMDevicesEndpoint
			if appleMDM != "" {
				id, err := resolveAppleMDM(cmd.Context(), client, appleMDM)
				if err != nil {
					return err
				}
				endpoint = apple_mdm.ManagerABMDevicesEndpoint(id)
			}
			raw, err := client.Get(cmd.Context(), endpoint)
			if err != nil {
				return err
			}
			rows, total, err := apple_mdm.ParseABMDevices(raw)
			if err != nil {
				return err
			}
			return writeADERows(cmd, rows, total, abmDeviceDefaultFields, "ABM device", "ABM devices")
		},
	}

	cmd.Flags().StringVar(&appleMDM, "apple-mdm", "",
		"Scope the list to one Apple MDM configuration (name or id)")
	_ = cmd.RegisterFlagCompletionFunc("apple-mdm", completeResourceNames(resolve.AppleMDMConfig))
	return cmd
}

func newAppleMDMBackgroundTasksCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "background-tasks <device-id>",
		Aliases: []string{"tasks"},
		Short:   "List the MDM background tasks queued for a device",
		Long: `List the MDM background tasks queued for one managed device.

The id is a DEVICE id, not an Apple MDM id, even though it occupies the same
position in the path as one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			if err := apple_mdm.RequireObjectID("a managed device", args[0]); err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), apple_mdm.BackgroundTasksEndpoint(args[0]))
			if err != nil {
				return err
			}
			rows, total, err := apple_mdm.ParseBackgroundTasks(raw)
			if err != nil {
				return err
			}
			return writeADERows(cmd, rows, total, backgroundTaskDefaultFields, "task", "tasks")
		},
	}
}

func newAppleMDMADUECmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adue",
		Short: "Account-Driven User Enrollment",
		Long: `Inspect Account-Driven User Enrollment (ADUE) — the BYOD path where a
user enrolls a personal device by signing in with their JumpCloud account
rather than through Apple Business Manager.`,
	}
	cmd.AddCommand(newADUEGetCmd())
	cmd.AddCommand(newADUEConfigCmd())
	cmd.AddCommand(newADUEValidateCmd())
	return cmd
}

// orgIDForADUE returns the organization the ADUE reads are scoped to. These
// two endpoints take an organization id where their siblings take an Apple MDM
// id, so the identifier is not interchangeable with the one the other
// commands accept.
func orgIDForADUE() (string, error) {
	orgID := config.OrgID()
	if orgID == "" {
		return "", fmt.Errorf("these reads are scoped to an organization and jc has no org id; " +
			"check `jc auth status`, or set JC_ORG_ID")
	}
	return orgID, nil
}

func newADUEGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show the ADUE enrollment URL for this organization",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			orgID, err := orgIDForADUE()
			if err != nil {
				return err
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), apple_mdm.ADUEEndpoint(orgID))
			if err != nil {
				return err
			}
			url, err := apple_mdm.ParseADUERedirect(raw)
			if err != nil {
				return err
			}
			b, err := json.Marshal(map[string]string{"redirectUrl": url})
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), b, output.CurrentOptions())
		},
	}
}

func newADUEConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show the ADUE service configuration for this organization",
		Long: `Show the ADUE service configuration — the servers an Apple device is
pointed at when a user enrolls by signing in.

The API returns this configuration as a JSON document inside a JSON string. jc
unwraps it, so what you get is the configuration rather than an escaped blob.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			orgID, err := orgIDForADUE()
			if err != nil {
				return err
			}
			client, err := newV2Client()
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), apple_mdm.ADUEConfigEndpoint(orgID))
			if err != nil {
				return err
			}
			inner, err := apple_mdm.ParseADUEConfig(raw)
			if err != nil {
				return err
			}
			return output.WriteSingle(cmd.OutOrStdout(), inner, output.CurrentOptions())
		},
	}
}

func newADUEValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <apple-mdm>",
		Short: "Check whether an Apple MDM has a usable service discovery URL",
		Long: `Check whether an Apple MDM configuration has a usable service discovery URL.

The API reports a missing URL as HTTP 200 with a NOT_FOUND code in the body, so
a caller that trusts the status code reports success for an organization that
has no URL at all. jc reads the body and exits 1 when the URL does not resolve.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.AppleMDMConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newV2Client()
			if err != nil {
				return err
			}
			id, err := resolveAppleMDM(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			raw, err := client.Get(cmd.Context(), apple_mdm.ValidateServiceDiscoveryURLEndpoint(id))
			if err != nil {
				return err
			}
			res, err := apple_mdm.ParseServiceDiscovery(raw)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			if err := output.WriteSingle(cmd.OutOrStdout(), raw, opts); err != nil {
				return err
			}
			if !res.OK() {
				// Reported through the exit code as well as the body, so this
				// is usable in a script without parsing the output.
				return &CLIError{
					Code:       ErrCodeGeneral,
					Message:    res.Describe(),
					Suggestion: "configure a service discovery URL for this Apple MDM, or use `jc apple-mdm adue config` to see what the organization currently points at",
				}
			}
			if !opts.Quiet && !opts.IDsOnly {
				fmt.Fprintf(cmd.ErrOrStderr(), "── %s ──\n", res.Describe())
			}
			return nil
		},
	}
}

// writeADERows is the shared tail of every list read in this area: emit the
// rows, then a footer that names them. The footer distinguishes a partial
// page from a complete one, which matters because these endpoints report a
// totalCount that can exceed what they returned.
func writeADERows(cmd *cobra.Command, rows []json.RawMessage, total int, fields []string, singular, plural string) error {
	opts := output.CurrentOptions()
	// DefaultFields, not Fields: the output layer resolves --fields, --exclude
	// and --all against it, so setting Fields here would make a default look
	// like an explicit request and silently defeat --all.
	if len(fields) > 0 {
		opts.DefaultFields = fields
	}
	if err := output.WriteList(cmd.OutOrStdout(), rows, opts); err != nil {
		return err
	}
	if !opts.Quiet && !opts.IDsOnly {
		writeNounListFooter(cmd, len(rows), total, singular, plural)
	}
	return nil
}
