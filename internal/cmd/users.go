package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
)

// userDefaultFields is the default field subset shown for user list/table output.
var userDefaultFields = []string{"username", "email", "firstname", "lastname", "activated", "suspended"}

// newV1Client creates a V1 API client. Overridable in tests.
var newV1Client = func() (*api.V1Client, error) {
	return api.NewV1Client()
}

// confirmReader is the reader used for confirmation prompts. Overridable in tests.
var confirmReader *bufio.Reader

func getConfirmReader() *bufio.Reader {
	if confirmReader != nil {
		return confirmReader
	}
	return bufio.NewReader(os.Stdin)
}

func newUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "Manage JumpCloud users",
		Long:  "List, get, search, create, update, delete, lock, unlock, reset MFA, and reset password for JumpCloud system users.",
	}

	cmd.AddCommand(newUsersListCmd())
	cmd.AddCommand(newUsersGetCmd())
	cmd.AddCommand(newUsersSearchCmd())
	cmd.AddCommand(newUsersCreateCmd())
	cmd.AddCommand(newUsersUpdateCmd())
	cmd.AddCommand(newUsersDeleteCmd())
	cmd.AddCommand(newUsersLockCmd())
	cmd.AddCommand(newUsersUnlockCmd())
	cmd.AddCommand(newUsersResetMFACmd())
	cmd.AddCommand(newUsersResetPasswordCmd())

	return cmd
}

func newUsersListCmd() *cobra.Command {
	var (
		limitFlag int
		sortFlag  string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all users",
		Long: `List all JumpCloud system users.

Default fields: username, email, firstname, lastname, activated, suspended.
Use --output table for a readable ASCII table.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersList(cmd, limitFlag, sortFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -created)")

	return cmd
}

func runUsersList(cmd *cobra.Command, limit int, sort string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/systemusers", api.ListOptions{
		Limit: limit,
		Sort:  sort,
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = userDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	// Print footer with count info (only in non-quiet, non-IDs mode).
	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(result.Data), result.TotalCount)
	}

	return nil
}

func newUsersSearchCmd() *cobra.Command {
	var (
		limitFlag int
	)

	cmd := &cobra.Command{
		Use:   "search <term>",
		Short: "Search for users by keyword",
		Long: `Search for JumpCloud users by keyword across username, email, firstname, and lastname fields.

Uses the V1 POST /api/search/systemusers endpoint for case-insensitive searching.
Default fields: username, email, firstname, lastname, activated, suspended.
Use --output table for a readable ASCII table.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersSearch(cmd, args[0], limitFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")

	return cmd
}

func runUsersSearch(cmd *cobra.Command, term string, limit int) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	// Build the V1 search request body.
	// The searchFilter uses $or to match across multiple fields with case-insensitive regex.
	searchBody := map[string]any{
		"searchFilter": map[string]any{
			"searchTerm": term,
			"fields":     []string{"username", "email", "firstname", "lastname"},
		},
	}

	result, err := client.Search(cmd.Context(), "/search/systemusers", searchBody, api.SearchOptions{
		Limit: limit,
	})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = userDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(result.Data), result.TotalCount)
	}

	return nil
}

func newUsersGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <username-or-id>",
		Short: "Get a user by username or ID",
		Long: `Get a single JumpCloud user by ID.

Accepts a 24-character hex user ID. Name resolution (username → ID)
will be available in a future release.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersGet(cmd, args[0])
		},
	}

	return cmd
}

func runUsersGet(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/systemusers/"+identifier)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newUsersCreateCmd() *cobra.Command {
	var (
		username  string
		email     string
		firstname string
		lastname  string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new user",
		Long: `Create a new JumpCloud system user.

Required fields: --username and --email.
The newly created user object is returned.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersCreate(cmd, username, email, firstname, lastname)
		},
	}

	cmd.Flags().StringVar(&username, "username", "", "Username (required)")
	cmd.Flags().StringVar(&email, "email", "", "Email address (required)")
	cmd.Flags().StringVar(&firstname, "firstname", "", "First name")
	cmd.Flags().StringVar(&lastname, "lastname", "", "Last name")
	_ = cmd.MarkFlagRequired("username")
	_ = cmd.MarkFlagRequired("email")

	return cmd
}

func runUsersCreate(cmd *cobra.Command, username, email, firstname, lastname string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	body := map[string]string{
		"username": username,
		"email":    email,
	}
	if firstname != "" {
		body["firstname"] = firstname
	}
	if lastname != "" {
		body["lastname"] = lastname
	}

	result, err := client.Create(cmd.Context(), "/systemusers", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newUsersUpdateCmd() *cobra.Command {
	var (
		email      string
		firstname  string
		lastname   string
		department string
		jobTitle   string
	)

	cmd := &cobra.Command{
		Use:   "update <user-id>",
		Short: "Update a user",
		Long: `Update an existing JumpCloud system user.

Specify only the fields you want to change. The updated user object is returned.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersUpdate(cmd, args[0], email, firstname, lastname, department, jobTitle)
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "Email address")
	cmd.Flags().StringVar(&firstname, "firstname", "", "First name")
	cmd.Flags().StringVar(&lastname, "lastname", "", "Last name")
	cmd.Flags().StringVar(&department, "department", "", "Department")
	cmd.Flags().StringVar(&jobTitle, "jobTitle", "", "Job title")

	return cmd
}

func runUsersUpdate(cmd *cobra.Command, identifier, email, firstname, lastname, department, jobTitle string) error {
	// Build update body from flags that were explicitly set.
	body := map[string]string{}

	if cmd.Flags().Changed("email") {
		body["email"] = email
	}
	if cmd.Flags().Changed("firstname") {
		body["firstname"] = firstname
	}
	if cmd.Flags().Changed("lastname") {
		body["lastname"] = lastname
	}
	if cmd.Flags().Changed("department") {
		body["department"] = department
	}
	if cmd.Flags().Changed("jobTitle") {
		body["jobTitle"] = jobTitle
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update. Specify at least one field flag (e.g., --email, --department)")
	}

	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/systemusers/"+identifier, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newUsersDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <user-id>",
		Short: "Delete a user",
		Long: `Delete a JumpCloud system user.

Shows the user's username and email before prompting for confirmation.
Use --force to skip the confirmation prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersDelete(cmd, args[0])
		},
	}

	return cmd
}

func runUsersDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	// Fetch the user first so we can show details in the confirmation prompt.
	userData, err := client.Get(cmd.Context(), "/systemusers/"+identifier)
	if err != nil {
		return err
	}

	var user struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	}
	if err := json.Unmarshal(userData, &user); err != nil {
		return fmt.Errorf("parsing user data: %w", err)
	}

	// Confirmation prompt (unless --force is set).
	if !viper.GetBool("force") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete user %s (%s)? [y/N] ", user.Username, user.Email)
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

	_, err = client.Delete(cmd.Context(), "/systemusers/"+identifier)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "User %s deleted successfully.\n", user.Username)
	return nil
}

func newUsersLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock <user-id>",
		Short: "Lock a user account",
		Long:  "Lock a JumpCloud user account by setting account_locked=true.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersLockUnlock(cmd, args[0], true)
		},
	}
	return cmd
}

func newUsersUnlockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlock <user-id>",
		Short: "Unlock a user account",
		Long:  "Unlock a JumpCloud user account by setting account_locked=false.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersLockUnlock(cmd, args[0], false)
		},
	}
	return cmd
}

func runUsersLockUnlock(cmd *cobra.Command, identifier string, lock bool) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	// Fetch user to get username for confirmation message.
	userData, err := client.Get(cmd.Context(), "/systemusers/"+identifier)
	if err != nil {
		return err
	}

	var user struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(userData, &user); err != nil {
		return fmt.Errorf("parsing user data: %w", err)
	}

	body := map[string]any{
		"account_locked": lock,
	}
	_, err = client.Update(cmd.Context(), "/systemusers/"+identifier, body)
	if err != nil {
		return err
	}

	action := "locked"
	if !lock {
		action = "unlocked"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "User %s %s successfully.\n", user.Username, action)
	return nil
}

func newUsersResetMFACmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset-mfa <user-id>",
		Short: "Reset MFA enrollment for a user",
		Long: `Reset TOTP/MFA enrollment for a JumpCloud user.

The user will need to re-enroll in MFA on their next login.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersResetMFA(cmd, args[0])
		},
	}
	return cmd
}

func runUsersResetMFA(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	// Fetch user to get username for confirmation message.
	userData, err := client.Get(cmd.Context(), "/systemusers/"+identifier)
	if err != nil {
		return err
	}

	var user struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(userData, &user); err != nil {
		return fmt.Errorf("parsing user data: %w", err)
	}

	_, err = client.Post(cmd.Context(), "/systemusers/"+identifier+"/resetmfa", nil)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "User %s MFA reset successfully. The user will need to re-enroll in MFA.\n", user.Username)
	return nil
}

func newUsersResetPasswordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset-password <user-id>",
		Short: "Trigger a password reset for a user",
		Long:  "Trigger a password reset email for a JumpCloud user. The user's password will expire and they will be prompted to set a new one.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersResetPassword(cmd, args[0])
		},
	}
	return cmd
}

func runUsersResetPassword(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	// Fetch user to get username for confirmation message.
	userData, err := client.Get(cmd.Context(), "/systemusers/"+identifier)
	if err != nil {
		return err
	}

	var user struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(userData, &user); err != nil {
		return fmt.Errorf("parsing user data: %w", err)
	}

	_, err = client.Post(cmd.Context(), "/systemusers/"+identifier+"/expire", nil)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "User %s password reset triggered successfully.\n", user.Username)
	return nil
}

// writeListFooter writes a "── N of TOTAL items ──" footer to stderr.
func writeListFooter(cmd *cobra.Command, count, total int) {
	if count == total {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", count)
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d of %d items ──\n", count, total)
	}
}
