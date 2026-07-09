package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
)

var userStateDefaultFields = []string{"id", "userId", "state", "startDate", "endDate"}

func newUserStatesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user-states",
		Short: "Manage scheduled user state changes",
		Long:  "List, create, get, and delete scheduled user state transitions (suspend/reactivate on a given date).",
	}

	cmd.AddCommand(newUserStatesListCmd())
	cmd.AddCommand(newUserStatesCreateCmd())
	cmd.AddCommand(newUserStatesGetCmd())
	cmd.AddCommand(newUserStatesDeleteCmd())

	return cmd
}

func newUserStatesListCmd() *cobra.Command {
	var limitFlag int

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List scheduled user state changes",
		Long: `List all scheduled user state changes.

Default fields: id, userId, state, startDate, endDate.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUserStatesList(cmd, limitFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")

	return cmd
}

func runUserStatesList(cmd *cobra.Command, limit int) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/bulk/userstates", api.V2ListOptions{Limit: limit})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = userStateDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newUserStatesCreateCmd() *cobra.Command {
	var (
		userFlag      string
		stateFlag     string
		startDateFlag string
		endDateFlag   string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Schedule a user state change",
		Long: `Schedule a user state transition (suspend or reactivate) on a future date.

Required fields: --user, --state, --start-date.
State must be "suspended" or "activated".
Dates should be in YYYY-MM-DD or RFC 3339 format.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUserStatesCreate(cmd, userFlag, stateFlag, startDateFlag, endDateFlag)
		},
	}

	cmd.Flags().StringVar(&userFlag, "user", "", "User name or ID (required)")
	cmd.Flags().StringVar(&stateFlag, "state", "", "Target state: suspended or activated (required)")
	cmd.Flags().StringVar(&startDateFlag, "start-date", "", "Date for state change (YYYY-MM-DD or RFC 3339) (required)")
	cmd.Flags().StringVar(&endDateFlag, "end-date", "", "Optional end date to revert the state change")
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("state")
	_ = cmd.MarkFlagRequired("start-date")

	return cmd
}

func runUserStatesCreate(cmd *cobra.Command, user, state, startDate, endDate string) error {
	// Validate state.
	state = strings.ToLower(state)
	if state != "suspended" && state != "activated" {
		return NewCLIError(ErrCodeValidationError,
			fmt.Sprintf("invalid state %q: must be 'suspended' or 'activated'", state),
			"Use --state suspended or --state activated")
	}

	// Resolve user.
	v1Client, err := newV1Client()
	if err != nil {
		return err
	}
	userID, err := resolveUser(cmd.Context(), v1Client, user)
	if err != nil {
		return err
	}

	if viper.GetBool("plan") {
		effects := []string{
			"userId: " + userID,
			"state: " + state,
			"startDate: " + startDate,
		}
		if endDate != "" {
			effects = append(effects, "endDate: "+endDate)
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "user state change",
			Target:     user,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	body := map[string]any{
		"user_id":    userID,
		"state":      state,
		"start_date": startDate,
	}
	if endDate != "" {
		body["end_date"] = endDate
	}

	result, err := client.Create(cmd.Context(), "/bulk/userstates", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newUserStatesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <state-id>",
		Short: "Get a scheduled user state change by ID",
		Long:  "Get a single scheduled user state change by its ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUserStatesGet(cmd, args[0])
		},
	}
}

func runUserStatesGet(cmd *cobra.Command, id string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/bulk/userstates/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newUserStatesDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <state-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a scheduled user state change",
		Long: `Delete a scheduled user state change.

Accepts a state change ID. Use --force to skip confirmation.`,
		Args: cobra.MaximumNArgs(1),
		RunE: batchRunE("user state", "delete", runUserStatesDelete),
	}
	addBatchSourceFlags(cmd)
	return cmd
}

func runUserStatesDelete(cmd *cobra.Command, id string) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	// Fetch to show details.
	stateData, err := client.Get(cmd.Context(), "/bulk/userstates/"+id)
	if err != nil {
		return err
	}

	var stateInfo struct {
		UserID    string `json:"userId"`
		State     string `json:"state"`
		StartDate string `json:"startDate"`
	}
	_ = json.Unmarshal(stateData, &stateInfo)

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "user state change",
			Target:   id,
			Effects:  []string{fmt.Sprintf("Cancel scheduled %s for user %s on %s", stateInfo.State, stateInfo.UserID, stateInfo.StartDate)},
		}
		return renderPlan(cmd, p)
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete scheduled state change %s? [y/N] ", id)
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

	_, err = client.Delete(cmd.Context(), "/bulk/userstates/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Scheduled state change %s deleted successfully.\n", id)
	return nil
}
