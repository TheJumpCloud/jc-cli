package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/notification"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

func resolveNotificationChannel(ctx context.Context, client *api.V2Client, identifier string) (string, error) {
	r := resolve.NewV2Resolver(client)
	return r.Resolve(ctx, identifier, resolve.NotificationChannelConfig)
}

func newNotificationChannelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "notification-channels",
		Aliases: []string{"notification-channel", "notif-channels"},
		Short:   "Manage JumpCloud notification channels (where alerts are delivered)",
		Long: `List, get, create, update, and delete JumpCloud notification channels.

A notification channel is a delivery target for JumpCloud alerts — a webhook
URL, a Slack channel, or an email recipient set. Each channel has a type
(webhook, email, or slack) and a matching config block.

Webhook channels are fully expressible with flags (--url and the --auth-*
options). Email and Slack channels carry nested recipient/channel arrays;
supply those with --config-file (raw JSON for the "config" object).`,
	}
	cmd.AddCommand(newNotificationChannelsListCmd())
	cmd.AddCommand(newNotificationChannelsGetCmd())
	cmd.AddCommand(newNotificationChannelsCreateCmd())
	cmd.AddCommand(newNotificationChannelsUpdateCmd())
	cmd.AddCommand(newNotificationChannelsDeleteCmd())
	return cmd
}

func newNotificationChannelsListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all notification channels",
		Long: `List all JumpCloud notification channels.

Default fields: objectId, name, type, enabled, description.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotificationChannelsList(cmd, limitFlag, sortFlag, filterFlag)
		},
	}
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'type=CHANNEL_TYPE_WEBHOOK')")
	return cmd
}

func runNotificationChannelsList(cmd *cobra.Command, limit int, sort string, filters []string) error {
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	result, err := client.ListAll(cmd.Context(), "/notifications/channels", api.V2ListOptions{
		Limit:       limit,
		Sort:        sort,
		Filter:      filter.ToV2Queries(exprs),
		ResponseKey: "channels",
	})
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	opts.DefaultFields = notification.DefaultFields
	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}
	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}
	return nil
}

func newNotificationChannelsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "get <name-or-id>",
		Short:             "Get a notification channel by name or ID",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.NotificationChannelConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotificationChannelsGet(cmd, args[0])
		},
	}
	return cmd
}

func runNotificationChannelsGet(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveNotificationChannel(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	result, err := client.Get(cmd.Context(), "/notifications/channels/"+id)
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), notification.Unwrap(result), opts)
}

func newNotificationChannelsCreateCmd() *cobra.Command {
	var (
		name       string
		chType     string
		desc       string
		enabled    bool
		url        string
		authType   string
		authToken  string
		authUser   string
		authPass   string
		sslVerify  bool
		configFile string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a notification channel",
		Long: `Create a new JumpCloud notification channel.

Required: --name and --type (webhook, email, or slack).

Webhook: pass --url (and optionally --auth-type/--auth-token/--auth-username/
--auth-password/--ssl-verification).

Email/Slack: pass --config-file with the raw JSON for the "config" object
(recipient roles/users for email, slack channel ids for slack). --config-file
also works for webhook if you need the full config shape.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotificationChannelsCreate(cmd, notificationCreateArgs{
				name: name, chType: chType, desc: desc, enabled: enabled,
				url: url, authType: authType, authToken: authToken,
				authUser: authUser, authPass: authPass, sslVerify: sslVerify,
				configFile: configFile,
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Channel name (required)")
	cmd.Flags().StringVar(&chType, "type", "", "Channel type: webhook, email, or slack (required)")
	cmd.Flags().StringVar(&desc, "description", "", "Channel description")
	cmd.Flags().BoolVar(&enabled, "enabled", true, "Whether the channel is enabled")
	cmd.Flags().StringVar(&url, "url", "", "Webhook URL (webhook type)")
	cmd.Flags().StringVar(&authType, "auth-type", "", "Webhook auth type")
	cmd.Flags().StringVar(&authToken, "auth-token", "", "Webhook bearer/auth token")
	cmd.Flags().StringVar(&authUser, "auth-username", "", "Webhook basic-auth username")
	cmd.Flags().StringVar(&authPass, "auth-password", "", "Webhook basic-auth password")
	cmd.Flags().BoolVar(&sslVerify, "ssl-verification", true, "Verify the webhook's TLS certificate")
	cmd.Flags().StringVar(&configFile, "config-file", "", `Path to a JSON file with the raw "config" object (for email/slack or advanced webhook)`)
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

type notificationCreateArgs struct {
	name, chType, desc       string
	enabled                  bool
	url, authType, authToken string
	authUser, authPass       string
	sslVerify                bool
	configFile               string
}

// buildChannelConfig resolves the config block: --config-file takes precedence;
// otherwise a webhook is built from --url. Email/slack without --config-file is
// an error (their recipient/channel arrays can't be expressed as flat flags).
func buildChannelConfig(a notificationCreateArgs, apiType string) (map[string]any, error) {
	if a.configFile != "" {
		raw, err := os.ReadFile(a.configFile)
		if err != nil {
			return nil, fmt.Errorf("reading --config-file: %w", err)
		}
		var cfg map[string]any
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("parsing --config-file JSON: %w", err)
		}
		return cfg, nil
	}
	if apiType == "CHANNEL_TYPE_WEBHOOK" {
		if a.url == "" {
			return nil, fmt.Errorf("webhook channels require --url (or --config-file)")
		}
		return notification.BuildWebhookConfig(a.url, a.authType, a.authToken, a.authUser, a.authPass, a.sslVerify), nil
	}
	return nil, fmt.Errorf("--type %s requires --config-file with the recipient/channel config (only webhook is expressible with flags)", a.chType)
}

func runNotificationChannelsCreate(cmd *cobra.Command, a notificationCreateArgs) error {
	apiType, err := notification.NormalizeType(a.chType)
	if err != nil {
		return err
	}
	config, err := buildChannelConfig(a, apiType)
	if err != nil {
		return err
	}
	if viper.GetBool("plan") {
		effects := []string{"type: " + a.chType, fmt.Sprintf("enabled: %t", a.enabled)}
		if a.url != "" {
			effects = append(effects, "url: "+a.url)
		}
		return renderPlan(cmd, &plan.Plan{
			Action:     "create",
			Resource:   "notification channel",
			Target:     a.name,
			Effects:    effects,
			Reversible: true,
		})
	}
	channel := map[string]any{
		"name":    a.name,
		"type":    apiType,
		"enabled": a.enabled,
		"config":  config,
	}
	if a.desc != "" {
		channel["description"] = a.desc
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	result, err := client.Create(cmd.Context(), "/notifications/channels", notification.Body(channel))
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), notification.Unwrap(result), opts)
}

func newNotificationChannelsUpdateCmd() *cobra.Command {
	var (
		name    string
		desc    string
		enabled bool
		url     string
	)
	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Update a notification channel",
		Long: `Update an existing JumpCloud notification channel.

Specify only the fields you want to change; all others are preserved. The
channel update is a full-object replace (the PATCH endpoint rejects a sparse
body), so this command reads the current channel, applies your changes, and
writes it back.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.NotificationChannelConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotificationChannelsUpdate(cmd, args[0], name, desc, enabled, url)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "New channel name")
	cmd.Flags().StringVar(&desc, "description", "", "New channel description")
	cmd.Flags().BoolVar(&enabled, "enabled", true, "Enable or disable the channel")
	cmd.Flags().StringVar(&url, "url", "", "New webhook URL (webhook channels)")
	return cmd
}

func runNotificationChannelsUpdate(cmd *cobra.Command, identifier, name, desc string, enabled bool, url string) error {
	changedName := cmd.Flags().Changed("name")
	changedDesc := cmd.Flags().Changed("description")
	changedEnabled := cmd.Flags().Changed("enabled")
	changedURL := cmd.Flags().Changed("url")
	if !changedName && !changedDesc && !changedEnabled && !changedURL {
		return fmt.Errorf("no fields to update. Specify at least one of --name, --description, --enabled, --url")
	}
	if viper.GetBool("plan") {
		var effects []string
		if changedName {
			effects = append(effects, "name: "+name)
		}
		if changedDesc {
			effects = append(effects, "description: "+desc)
		}
		if changedEnabled {
			effects = append(effects, fmt.Sprintf("enabled: %t", enabled))
		}
		if changedURL {
			effects = append(effects, "url: "+url)
		}
		return renderPlan(cmd, &plan.Plan{
			Action:     "update",
			Resource:   "notification channel",
			Target:     identifier,
			Effects:    effects,
			Reversible: true,
		})
	}
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveNotificationChannel(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	// Read-modify-write: the PATCH endpoint is not partial (it 400s "channel
	// name is required" on a sparse body), so fetch the current channel, apply
	// only the changed fields, strip server-managed keys, and PATCH the whole
	// {channel}.
	current, err := client.Get(cmd.Context(), "/notifications/channels/"+id)
	if err != nil {
		return err
	}
	var obj map[string]any
	if err := json.Unmarshal(notification.Unwrap(current), &obj); err != nil {
		return fmt.Errorf("parsing current channel: %w", err)
	}
	if changedName {
		obj["name"] = name
	}
	if changedDesc {
		obj["description"] = desc
	}
	if changedEnabled {
		obj["enabled"] = enabled
	}
	if changedURL {
		wh := map[string]any{}
		if cfg, ok := obj["config"].(map[string]any); ok {
			if existing, ok := cfg["webhook"].(map[string]any); ok {
				wh = existing
			}
		}
		wh["url"] = url
		obj["config"] = map[string]any{"webhook": wh}
	}
	notification.StripServerManaged(obj)
	result, err := client.Patch(cmd.Context(), "/notifications/channels/"+id, notification.Body(obj))
	if err != nil {
		return err
	}
	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), notification.Unwrap(result), opts)
}

func newNotificationChannelsDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "delete <name-or-id>",
		Aliases:           []string{"rm"},
		Short:             "Delete a notification channel",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.NotificationChannelConfig),
		RunE:              batchRunE("notification channel", "delete", runNotificationChannelsDelete),
	}
	addBatchSourceFlags(cmd)
	return cmd
}

func runNotificationChannelsDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}
	id, err := resolveNotificationChannel(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}
	// Read the name back for the confirmation/success message.
	name := identifier
	if raw, err := client.Get(cmd.Context(), "/notifications/channels/"+id); err == nil {
		var ch struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(notification.Unwrap(raw), &ch) == nil && ch.Name != "" {
			name = ch.Name
		}
	}

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:   "delete",
			Resource: "notification channel",
			Target:   fmt.Sprintf("%s (%s)", name, id),
			Effects:  []string{"Alerts routed to this channel stop being delivered"},
		})
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete notification channel %q? Alerts routed to it stop being delivered. [y/N] ", name)
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
	if _, err := client.Delete(cmd.Context(), "/notifications/channels/"+id); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Notification channel %q deleted successfully.\n", name)
	return nil
}
