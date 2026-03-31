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

// customEmailTemplateDefaultFields is the default field subset shown for custom email template output.
var customEmailTemplateDefaultFields = []string{"type", "displayName", "description"}

// customEmailConfigDefaultFields is the default field subset shown for custom email config output.
var customEmailConfigDefaultFields = []string{"type", "subject", "title"}

// validCustomEmailTypes lists the valid custom email type values.
var validCustomEmailTypes = []string{
	"activate_gapps_user",
	"activate_o365_user",
	"lockout_notice_user",
	"password_expiration",
	"password_expiration_warning",
	"password_reset_confirmation",
	"user_change_password",
	"activate_user_custom",
}

// isValidCustomEmailType checks whether the given type is a valid custom email type.
func isValidCustomEmailType(t string) bool {
	for _, v := range validCustomEmailTypes {
		if t == v {
			return true
		}
	}
	return false
}

func newCustomEmailsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "custom-emails",
		Short: "Manage JumpCloud custom email templates and configurations",
		Long:  "List custom email template definitions, and get, create, update, or delete custom email configurations by type.",
	}

	cmd.AddCommand(newCustomEmailTemplatesCmd())
	cmd.AddCommand(newCustomEmailGetCmd())
	cmd.AddCommand(newCustomEmailCreateCmd())
	cmd.AddCommand(newCustomEmailUpdateCmd())
	cmd.AddCommand(newCustomEmailDeleteCmd())

	return cmd
}

func newCustomEmailTemplatesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "templates",
		Aliases: []string{"ls"},
		Short:   "List custom email template definitions",
		Long: `List all JumpCloud custom email template definitions.

Default fields: type, displayName, description.
Use --output table for a readable ASCII table.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCustomEmailTemplates(cmd)
		},
	}

	return cmd
}

func runCustomEmailTemplates(cmd *cobra.Command) error {
	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/customemail/templates", api.V2ListOptions{})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = customEmailTemplateDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", len(result.Data))
	}

	return nil
}

func newCustomEmailGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <type>",
		Short: "Get custom email configuration by type",
		Long: fmt.Sprintf(`Get the custom email configuration for a specific type.

Valid types: %s.`, strings.Join(validCustomEmailTypes, ", ")),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCustomEmailGet(cmd, args[0])
		},
	}

	return cmd
}

func runCustomEmailGet(cmd *cobra.Command, emailType string) error {
	if !isValidCustomEmailType(emailType) {
		return fmt.Errorf("invalid custom email type %q. Valid types: %s", emailType, strings.Join(validCustomEmailTypes, ", "))
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/customemails/"+emailType)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newCustomEmailCreateCmd() *cobra.Command {
	var (
		emailType string
		subject   string
		body      string
		title     string
		header    string
		button    string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a custom email configuration",
		Long: fmt.Sprintf(`Create a custom email configuration for a specific type.

Required fields: --type, --subject.
Valid types: %s.`, strings.Join(validCustomEmailTypes, ", ")),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCustomEmailCreate(cmd, emailType, subject, body, title, header, button)
		},
	}

	cmd.Flags().StringVar(&emailType, "type", "", "Custom email type (required)")
	cmd.Flags().StringVar(&subject, "subject", "", "Email subject (required)")
	cmd.Flags().StringVar(&body, "body", "", "Email body")
	cmd.Flags().StringVar(&title, "title", "", "Email title")
	cmd.Flags().StringVar(&header, "header", "", "Email header")
	cmd.Flags().StringVar(&button, "button", "", "Email button text")
	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("subject")

	return cmd
}

func runCustomEmailCreate(cmd *cobra.Command, emailType, subject, body, title, header, button string) error {
	if !isValidCustomEmailType(emailType) {
		return fmt.Errorf("invalid custom email type %q. Valid types: %s", emailType, strings.Join(validCustomEmailTypes, ", "))
	}

	if viper.GetBool("plan") {
		effects := []string{"type: " + emailType, "subject: " + subject}
		if body != "" {
			effects = append(effects, "body: "+body)
		}
		if title != "" {
			effects = append(effects, "title: "+title)
		}
		if header != "" {
			effects = append(effects, "header: "+header)
		}
		if button != "" {
			effects = append(effects, "button: "+button)
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "custom-email",
			Target:     emailType,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	reqBody := map[string]string{
		"type":    emailType,
		"subject": subject,
	}
	if body != "" {
		reqBody["body"] = body
	}
	if title != "" {
		reqBody["title"] = title
	}
	if header != "" {
		reqBody["header"] = header
	}
	if button != "" {
		reqBody["button"] = button
	}

	result, err := client.Create(cmd.Context(), "/customemails/"+emailType, reqBody)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newCustomEmailUpdateCmd() *cobra.Command {
	var (
		subject string
		body    string
		title   string
		header  string
		button  string
	)

	cmd := &cobra.Command{
		Use:   "update <type>",
		Short: "Update a custom email configuration",
		Long: fmt.Sprintf(`Update an existing custom email configuration for a specific type.

Specify only the fields you want to change. The updated configuration is returned.
Valid types: %s.`, strings.Join(validCustomEmailTypes, ", ")),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCustomEmailUpdate(cmd, args[0], subject, body, title, header, button)
		},
	}

	cmd.Flags().StringVar(&subject, "subject", "", "Email subject")
	cmd.Flags().StringVar(&body, "body", "", "Email body")
	cmd.Flags().StringVar(&title, "title", "", "Email title")
	cmd.Flags().StringVar(&header, "header", "", "Email header")
	cmd.Flags().StringVar(&button, "button", "", "Email button text")

	return cmd
}

func runCustomEmailUpdate(cmd *cobra.Command, emailType, subject, body, title, header, button string) error {
	if !isValidCustomEmailType(emailType) {
		return fmt.Errorf("invalid custom email type %q. Valid types: %s", emailType, strings.Join(validCustomEmailTypes, ", "))
	}

	fields := map[string]string{}

	if cmd.Flags().Changed("subject") {
		fields["subject"] = subject
	}
	if cmd.Flags().Changed("body") {
		fields["body"] = body
	}
	if cmd.Flags().Changed("title") {
		fields["title"] = title
	}
	if cmd.Flags().Changed("header") {
		fields["header"] = header
	}
	if cmd.Flags().Changed("button") {
		fields["button"] = button
	}

	if len(fields) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --subject, --body, --title)")
	}

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range fields {
			effects = append(effects, fmt.Sprintf("%s: %s", k, v))
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "custom-email",
			Target:     emailType,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV2Client()
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/customemails/"+emailType, fields)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newCustomEmailDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <type>",
		Aliases: []string{"rm"},
		Short:   "Delete a custom email configuration",
		Long: fmt.Sprintf(`Delete a custom email configuration for a specific type.

Shows the type before prompting for confirmation.
Use --force to skip the confirmation prompt.
Valid types: %s.`, strings.Join(validCustomEmailTypes, ", ")),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCustomEmailDelete(cmd, args[0])
		},
	}

	return cmd
}

func runCustomEmailDelete(cmd *cobra.Command, emailType string) error {
	if !isValidCustomEmailType(emailType) {
		return fmt.Errorf("invalid custom email type %q. Valid types: %s", emailType, strings.Join(validCustomEmailTypes, ", "))
	}

	// Fetch the config first so we can confirm it exists.
	client, err := newV2Client()
	if err != nil {
		return err
	}

	configData, err := client.Get(cmd.Context(), "/customemails/"+emailType)
	if err != nil {
		return err
	}

	var config struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(configData, &config); err != nil {
		return fmt.Errorf("parsing custom email config: %w", err)
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "custom-email",
			Target:   emailType,
			Effects:  []string{"Remove custom email configuration permanently"},
		}
		return renderPlan(cmd, p)
	}

	// Confirmation prompt (unless --force is set).
	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete custom email config %q? [y/N] ", emailType)
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

	_, err = client.Delete(cmd.Context(), "/customemails/"+emailType)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Custom email config %q deleted successfully.\n", emailType)
	return nil
}
