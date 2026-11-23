package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
)

const emailSubscriptionsEndpoint = "/notifications/subscriptions/email"

// emailSubscriptionsOwnerNote is shared by the group and both leaves: the API
// has no admin parameter, so these are always the credentials' own preferences.
const emailSubscriptionsOwnerNote = `These are the preferences of the calling administrator: the one the API
credentials identify. The API takes no admin parameter, so there is no way to
read or change another administrator's email preferences.`

var emailSubscriptionDefaultFields = []string{"emailKey", "category", "title", "subscribed"}

type emailSubscription = map[string]any

func newAdminsEmailSubscriptionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "email-subscriptions",
		Short: "Read and change the calling admin's email preferences",
		Long: `Read and change which JumpCloud notification emails the calling administrator receives.

` + emailSubscriptionsOwnerNote,
	}
	cmd.AddCommand(newAdminsEmailSubscriptionsListCmd())
	cmd.AddCommand(newAdminsEmailSubscriptionsSetCmd())
	return cmd
}

// fetchEmailSubscriptions GETs the calling admin's subscriptions. An empty
// list is returned as such; callers decide how to say so.
func fetchEmailSubscriptions(cmd *cobra.Command) ([]emailSubscription, error) {
	client, err := newV2Client()
	if err != nil {
		return nil, err
	}
	raw, err := client.Get(cmd.Context(), emailSubscriptionsEndpoint)
	if err != nil {
		return nil, err
	}
	return parseEmailSubscriptions(raw)
}

func parseEmailSubscriptions(raw []byte) ([]emailSubscription, error) {
	var body struct {
		Subscriptions []emailSubscription `json:"subscriptions"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("parsing email subscriptions: %w", err)
	}
	return body.Subscriptions, nil
}

func subscriptionKey(s emailSubscription) string {
	k, _ := s["emailKey"].(string)
	return k
}

func subscriptionOn(s emailSubscription) bool {
	b, _ := s["subscribed"].(bool)
	return b
}

func newAdminsEmailSubscriptionsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the calling admin's email subscriptions",
		Long: `List the JumpCloud notification emails and whether the calling administrator is subscribed.

` + emailSubscriptionsOwnerNote + `

Columns: emailKey, category, title, subscribed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			subs, err := fetchEmailSubscriptions(cmd)
			if err != nil {
				return err
			}
			if len(subs) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "No email subscriptions returned. The API key may not resolve to an administrator (email preferences belong to an admin, not to a key), or the catalog is empty.")
			}
			data := make([]json.RawMessage, 0, len(subs))
			for _, s := range subs {
				b, err := json.Marshal(s)
				if err != nil {
					return err
				}
				data = append(data, b)
			}
			opts := output.CurrentOptions()
			opts.DefaultFields = emailSubscriptionDefaultFields
			return output.WriteList(cmd.OutOrStdout(), data, opts)
		},
	}
}

func newAdminsEmailSubscriptionsSetCmd() *cobra.Command {
	var subscribed bool
	cmd := &cobra.Command{
		Use:   "set <emailKey>... --subscribed=true|false",
		Short: "Subscribe or unsubscribe the calling admin from emails",
		Long: `Turn the named notification emails on or off for the calling administrator.

` + emailSubscriptionsOwnerNote + `

The current list is read first, only the named keys change, and the complete
list is sent back so the PUT cannot drop other subscriptions. Unknown keys fail
before any write and the valid keys are listed (see 'list'). After the write
the response is checked: if it does not show the requested value the command
fails. Use --plan to preview, --force to skip the confirmation prompt.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("subscribed") {
				return NewCLIError(ErrCodeUsageError, "--subscribed=true|false is required", "e.g. jc admins email-subscriptions set <emailKey> --subscribed=false")
			}
			return runAdminsEmailSubscriptionsSet(cmd, args, subscribed)
		},
	}
	cmd.Flags().BoolVar(&subscribed, "subscribed", false, "Desired state for the named emails (true or false)")
	return cmd
}

func runAdminsEmailSubscriptionsSet(cmd *cobra.Command, keys []string, want bool) error {
	current, err := fetchEmailSubscriptions(cmd)
	if err != nil {
		return err
	}

	byKey := make(map[string]emailSubscription, len(current))
	valid := make([]string, 0, len(current))
	for _, s := range current {
		byKey[subscriptionKey(s)] = s
		valid = append(valid, subscriptionKey(s))
	}
	sort.Strings(valid)

	var unknown []string
	for _, k := range keys {
		if _, ok := byKey[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		list := "(none: the API returned no subscriptions; the API key may not resolve to an administrator)"
		if len(valid) > 0 {
			list = strings.Join(valid, ", ")
		}
		return NewCLIError(ErrCodeUsageError,
			fmt.Sprintf("unknown emailKey %s. Valid emailKeys: %s", strings.Join(unknown, ", "), list),
			"Run 'jc admins email-subscriptions list' to see them.")
	}

	named := make(map[string]bool, len(keys))
	var effects []string
	for _, k := range keys {
		if named[k] {
			continue
		}
		named[k] = true
		effects = append(effects, fmt.Sprintf("%s: subscribed %t -> %t", k, subscriptionOn(byKey[k]), want))
	}

	if viper.GetBool("plan") {
		return renderPlan(cmd, &plan.Plan{
			Action:     "update",
			Resource:   "email subscriptions",
			Target:     "calling administrator",
			Effects:    effects,
			Reversible: true,
		})
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Change email subscriptions?\n  %s\n[y/N] ", strings.Join(effects, "\n  "))
		answer, err := getConfirmReader().ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading confirmation: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
			return nil
		}
	}

	// Send the complete list with only the named keys changed.
	full := make([]emailSubscription, 0, len(current))
	for _, s := range current {
		c := make(emailSubscription, len(s))
		for k, v := range s {
			c[k] = v
		}
		if named[subscriptionKey(s)] {
			c["subscribed"] = want
		}
		full = append(full, c)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}
	raw, err := client.Update(cmd.Context(), emailSubscriptionsEndpoint, map[string]any{"subscriptions": full})
	if err != nil {
		return err
	}

	got, err := parseEmailSubscriptions(raw)
	if err != nil {
		return fmt.Errorf("the PUT succeeded but its response could not be checked: %w", err)
	}
	gotByKey := make(map[string]emailSubscription, len(got))
	for _, s := range got {
		gotByKey[subscriptionKey(s)] = s
	}
	var bad []string
	for _, s := range full {
		k := subscriptionKey(s)
		g, ok := gotByKey[k]
		switch {
		case !ok:
			bad = append(bad, fmt.Sprintf("%s missing from the response", k))
		case subscriptionOn(g) != subscriptionOn(s):
			bad = append(bad, fmt.Sprintf("%s is subscribed=%t, requested %t", k, subscriptionOn(g), subscriptionOn(s)))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("the change did not take effect: %s", strings.Join(bad, "; "))
	}

	for _, e := range effects {
		fmt.Fprintln(cmd.OutOrStdout(), "Updated "+e)
	}
	return nil
}
