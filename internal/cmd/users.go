package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/filter"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/plan"
	"github.com/klaassen-consulting/jc/internal/resolve"
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

// resolveUser resolves a username or ID to a JumpCloud user ID.
func resolveUser(ctx context.Context, client *api.V1Client, identifier string) (string, error) {
	r := resolve.NewResolver(client)
	return r.Resolve(ctx, identifier, resolve.UserConfig)
}

func newUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "users",
		Aliases: []string{"u"},
		Short:   "Manage JumpCloud users",
		Long:    "List, get, search, create, update, delete, lock, unlock, reset MFA, and reset password for JumpCloud system users.\n\nAliases: u, users",
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
	cmd.AddCommand(newUsersSSHKeysCmd())
	cmd.AddCommand(newUsersSSHKeyAddCmd())
	cmd.AddCommand(newUsersSSHKeyDeleteCmd())

	return cmd
}

func newUsersListCmd() *cobra.Command {
	var (
		limitFlag  int
		sortFlag   string
		filterFlag []string
		searchFlag string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all users",
		Long: `List all JumpCloud system users.

Default fields: username, email, firstname, lastname, activated, suspended.
Use --output table for a readable ASCII table.

Filter examples:
  --filter 'activated=true'               Exact match
  --filter 'suspended!=true'              Inequality
  --filter 'created>=2026-01-01'          Greater than or equal
  --filter 'activated=true' --filter 'suspended!=true'   Multiple filters (AND)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersList(cmd, limitFlag, sortFlag, filterFlag, searchFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringVar(&sortFlag, "sort", "", "Sort field (prefix with - for descending, e.g. -created)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'field=value', 'field!=value', 'field>=value')")
	cmd.Flags().StringVar(&searchFlag, "search", "", "Full-text search across fields")

	return cmd
}

func runUsersList(cmd *cobra.Command, limit int, sort string, filters []string, search string) error {
	// Parse and validate filter expressions.
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

	client, err := newV1Client()
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/systemusers", api.ListOptions{
		Limit:  limit,
		Sort:   sort,
		Filter: filter.ToV1Queries(exprs),
		Search: search,
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
		limitFlag  int
		filterFlag []string
	)

	cmd := &cobra.Command{
		Use:   "search <term>",
		Short: "Search for users by keyword",
		Long: `Search for JumpCloud users by keyword across username, email, firstname, and lastname fields.

Uses the V1 POST /api/search/systemusers endpoint for case-insensitive searching.
Default fields: username, email, firstname, lastname, activated, suspended.
Use --output table for a readable ASCII table.

Results can be further filtered with --filter:
  jc users search john --filter 'activated=true'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersSearch(cmd, args[0], limitFlag, filterFlag)
		},
	}

	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Maximum number of results to return (0 = all)")
	cmd.Flags().StringArrayVar(&filterFlag, "filter", nil, "Filter results (e.g. 'field=value', 'field!=value', 'field>=value')")

	return cmd
}

func runUsersSearch(cmd *cobra.Command, term string, limit int, filters []string) error {
	// Parse and validate filter expressions.
	exprs, err := filter.ParseAll(filters)
	if err != nil {
		return err
	}

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

	// Add V1-style filter expressions to the search body if provided.
	if len(exprs) > 0 {
		searchBody["filter"] = filter.ToV1Queries(exprs)
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
		Long: `Get a single JumpCloud user by username or ID.

Accepts a username (e.g., "jdoe") or a 24-character hex user ID.
Usernames are resolved to IDs automatically with caching (use --no-cache to bypass).`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserConfig),
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

	id, err := resolveUser(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Get(cmd.Context(), "/systemusers/"+id)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newUsersCreateCmd() *cobra.Command {
	var (
		username    string
		email       string
		firstname   string
		lastname    string
		department  string
		ifNotExists bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new user",
		Long: `Create a new JumpCloud system user.

Required fields: --username and --email.
The newly created user object is returned.

Use --if-not-exists to skip creation when the username already exists
(returns the existing user instead of failing). This makes the operation
idempotent and safe for agent retries.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersCreate(cmd, username, email, firstname, lastname, department, ifNotExists)
		},
	}

	cmd.Flags().StringVar(&username, "username", "", "Username (required)")
	cmd.Flags().StringVar(&email, "email", "", "Email address (required)")
	cmd.Flags().StringVar(&firstname, "firstname", "", "First name")
	cmd.Flags().StringVar(&lastname, "lastname", "", "Last name")
	cmd.Flags().StringVar(&department, "department", "", "Department")
	cmd.Flags().BoolVar(&ifNotExists, "if-not-exists", false, "Skip creation if username already exists (idempotent)")
	_ = cmd.MarkFlagRequired("username")
	_ = cmd.MarkFlagRequired("email")

	return cmd
}

func runUsersCreate(cmd *cobra.Command, username, email, firstname, lastname, department string, ifNotExists bool) error {
	// --if-not-exists: check if user already exists, return existing if so.
	if ifNotExists {
		client, err := newV1Client()
		if err != nil {
			return err
		}
		id, resolveErr := resolveUser(cmd.Context(), client, username)
		if resolveErr == nil {
			// User exists — fetch and return it.
			existing, err := client.Get(cmd.Context(), "/systemusers/"+id)
			if err != nil {
				return err
			}
			opts := output.CurrentOptions()
			return output.WriteSingle(cmd.OutOrStdout(), existing, opts)
		}
	}

	if viper.GetBool("plan") {
		effects := []string{"username: " + username, "email: " + email}
		if firstname != "" {
			effects = append(effects, "firstname: "+firstname)
		}
		if lastname != "" {
			effects = append(effects, "lastname: "+lastname)
		}
		if department != "" {
			effects = append(effects, "department: "+department)
		}
		p := &plan.Plan{
			Action:     "create",
			Resource:   "user",
			Target:     username,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

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
	if department != "" {
		body["department"] = department
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
		Use:   "update <username-or-id>",
		Short: "Update a user",
		Long: `Update an existing JumpCloud system user.

Accepts a username or 24-character hex user ID.
Specify only the fields you want to change. The updated user object is returned.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserConfig),
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

	if viper.GetBool("plan") {
		var effects []string
		for k, v := range body {
			effects = append(effects, k+": "+v)
		}
		p := &plan.Plan{
			Action:     "update",
			Resource:   "user",
			Target:     identifier,
			Effects:    effects,
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveUser(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.Update(cmd.Context(), "/systemusers/"+id, body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newUsersDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete [username-or-id]",
		Aliases: []string{"rm"},
		Short:   "Delete a user",
		Long: `Delete a JumpCloud system user.

Accepts a username or 24-character hex user ID.
Shows the user's username and email before prompting for confirmation.
Use --force to skip the confirmation prompt.

Stdin mode:
  Use --stdin to read usernames/IDs from stdin (one per line).
  When stdin is piped, --stdin is implied automatically.
  In stdin mode, --force is implied (no confirmation prompts).

  jc users list --filter 'suspended=true' --ids | jc users delete --force
  cat users.txt | jc users delete --stdin --force`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			useStdin, _ := cmd.Flags().GetBool("stdin")
			if useStdin || (len(args) == 0 && isStdinPiped()) {
				return runUsersDeleteStdin(cmd)
			}
			if len(args) == 0 {
				return fmt.Errorf("requires a username or ID argument (or use --stdin)")
			}
			return runUsersDelete(cmd, args[0])
		},
	}

	cmd.Flags().Bool("stdin", false, "Read usernames/IDs from stdin (one per line)")

	return cmd
}

func runUsersDelete(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveUser(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch the user first so we can show details in the confirmation/plan.
	userData, err := client.Get(cmd.Context(), "/systemusers/"+id)
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

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "user",
			Target:   fmt.Sprintf("%s (%s)", user.Username, id),
			Effects:  []string{"Remove user from JumpCloud", "User will lose access to all resources"},
		}
		return renderPlan(cmd, p)
	}

	// Confirmation prompt (unless --force is set).
	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
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

	_, err = client.Delete(cmd.Context(), "/systemusers/"+id)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "User %s deleted successfully.\n", user.Username)
	return nil
}

// runUsersDeleteStdin reads usernames/IDs from stdin and deletes each one.
func runUsersDeleteStdin(cmd *cobra.Command) error {
	identifiers, err := readLinesFromStdin()
	if err != nil {
		return err
	}

	if len(identifiers) == 0 {
		return nil
	}

	client, err := newV1Client()
	if err != nil {
		return err
	}

	result := runStdinBatch(identifiers, "user", "Deleting", cmd.ErrOrStderr(), func(identifier string) error {
		id, err := resolveUser(cmd.Context(), client, identifier)
		if err != nil {
			return err
		}
		_, err = client.Delete(cmd.Context(), "/systemusers/"+id)
		return err
	})

	if result.Failed > 0 {
		return fmt.Errorf("%d of %d deletions failed", result.Failed, result.Succeeded+result.Failed)
	}
	return nil
}

func newUsersLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock <username-or-id>",
		Short: "Lock a user account",
		Long:              "Lock a JumpCloud user account by setting account_locked=true. Accepts a username or ID.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersLockUnlock(cmd, args[0], true)
		},
	}
	return cmd
}

func newUsersUnlockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlock <username-or-id>",
		Short: "Unlock a user account",
		Long:              "Unlock a JumpCloud user account by setting account_locked=false. Accepts a username or ID.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserConfig),
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

	id, err := resolveUser(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch user to get username for confirmation/plan message.
	userData, err := client.Get(cmd.Context(), "/systemusers/"+id)
	if err != nil {
		return err
	}

	var user struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(userData, &user); err != nil {
		return fmt.Errorf("parsing user data: %w", err)
	}

	action := "lock"
	if !lock {
		action = "unlock"
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:     action,
			Resource:   "user",
			Target:     fmt.Sprintf("%s (%s)", user.Username, id),
			Effects:    []string{fmt.Sprintf("Set account_locked=%v", lock)},
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	body := map[string]any{
		"account_locked": lock,
	}
	_, err = client.Update(cmd.Context(), "/systemusers/"+id, body)
	if err != nil {
		return err
	}

	past := "locked"
	if !lock {
		past = "unlocked"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "User %s %s successfully.\n", user.Username, past)
	return nil
}

func newUsersResetMFACmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset-mfa <username-or-id>",
		Short: "Reset MFA enrollment for a user",
		Long: `Reset TOTP/MFA enrollment for a JumpCloud user.

Accepts a username or 24-character hex user ID.
The user will need to re-enroll in MFA on their next login.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserConfig),
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

	id, err := resolveUser(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch user to get username for confirmation message.
	userData, err := client.Get(cmd.Context(), "/systemusers/"+id)
	if err != nil {
		return err
	}

	var user struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(userData, &user); err != nil {
		return fmt.Errorf("parsing user data: %w", err)
	}

	_, err = client.Post(cmd.Context(), "/systemusers/"+id+"/resetmfa", nil)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "User %s MFA reset successfully. The user will need to re-enroll in MFA.\n", user.Username)
	return nil
}

func newUsersResetPasswordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset-password <username-or-id>",
		Short: "Trigger a password reset for a user",
		Long:              "Trigger a password reset email for a JumpCloud user. Accepts a username or ID. The user's password will expire and they will be prompted to set a new one.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserConfig),
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

	id, err := resolveUser(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	// Fetch user to get username for confirmation message.
	userData, err := client.Get(cmd.Context(), "/systemusers/"+id)
	if err != nil {
		return err
	}

	var user struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(userData, &user); err != nil {
		return fmt.Errorf("parsing user data: %w", err)
	}

	_, err = client.Post(cmd.Context(), "/systemusers/"+id+"/expire", nil)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "User %s password reset triggered successfully.\n", user.Username)
	return nil
}

// --- SSH Key sub-commands ---

var sshKeyDefaultFields = []string{"_id", "name", "public_key"}

func newUsersSSHKeysCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ssh-keys <username-or-id>",
		Short: "List SSH keys for a user",
		Long: `List all SSH keys registered for a JumpCloud user.

Accepts a username or 24-character hex user ID.
Default fields: _id, name, public_key.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersSSHKeys(cmd, args[0])
		},
	}
}

func runUsersSSHKeys(cmd *cobra.Command, identifier string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveUser(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	result, err := client.ListAll(cmd.Context(), "/systemusers/"+id+"/sshkeys", api.ListOptions{})
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	opts.DefaultFields = sshKeyDefaultFields

	if err := output.WriteList(cmd.OutOrStdout(), result.Data, opts); err != nil {
		return err
	}

	if !opts.Quiet && !opts.IDsOnly {
		writeListFooter(cmd, len(result.Data), result.TotalCount)
	}

	return nil
}

func newUsersSSHKeyAddCmd() *cobra.Command {
	var (
		nameFlag      string
		publicKeyFlag string
	)

	cmd := &cobra.Command{
		Use:   "ssh-key-add <username-or-id>",
		Short: "Add an SSH key to a user",
		Long: `Add an SSH public key to a JumpCloud user.

Accepts a username or 24-character hex user ID.
Required flags: --name, --public-key.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersSSHKeyAdd(cmd, args[0], nameFlag, publicKeyFlag)
		},
	}

	cmd.Flags().StringVar(&nameFlag, "name", "", "Label for the SSH key (required)")
	cmd.Flags().StringVar(&publicKeyFlag, "public-key", "", "SSH public key string (required)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("public-key")

	return cmd
}

func runUsersSSHKeyAdd(cmd *cobra.Command, identifier, name, publicKey string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveUser(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:     "add",
			Resource:   "SSH key",
			Target:     identifier,
			Effects:    []string{"name: " + name, "public_key: " + publicKey[:min(40, len(publicKey))] + "..."},
			Reversible: true,
		}
		return renderPlan(cmd, p)
	}

	body := map[string]any{
		"name":       name,
		"public_key": publicKey,
	}

	result, err := client.Create(cmd.Context(), "/systemusers/"+id+"/sshkeys", body)
	if err != nil {
		return err
	}

	opts := output.CurrentOptions()
	return output.WriteSingle(cmd.OutOrStdout(), result, opts)
}

func newUsersSSHKeyDeleteCmd() *cobra.Command {
	var keyIDFlag string

	cmd := &cobra.Command{
		Use:     "ssh-key-delete <username-or-id>",
		Aliases: []string{"ssh-key-rm"},
		Short:   "Delete an SSH key from a user",
		Long: `Delete an SSH public key from a JumpCloud user.

Accepts a username or 24-character hex user ID.
Required flag: --key-id.
Use --force to skip the confirmation prompt.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeResourceNames(resolve.UserConfig),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersSSHKeyDelete(cmd, args[0], keyIDFlag)
		},
	}

	cmd.Flags().StringVar(&keyIDFlag, "key-id", "", "SSH key ID to delete (required)")
	_ = cmd.MarkFlagRequired("key-id")

	return cmd
}

func runUsersSSHKeyDelete(cmd *cobra.Command, identifier, keyID string) error {
	client, err := newV1Client()
	if err != nil {
		return err
	}

	id, err := resolveUser(cmd.Context(), client, identifier)
	if err != nil {
		return err
	}

	if viper.GetBool("plan") {
		p := &plan.Plan{
			Action:   "delete",
			Resource: "SSH key",
			Target:   fmt.Sprintf("key %s from user %s", keyID, identifier),
			Effects:  []string{"Remove SSH key " + keyID},
		}
		return renderPlan(cmd, p)
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		fmt.Fprintf(cmd.ErrOrStderr(), "Delete SSH key %s from user %s? [y/N] ", keyID, identifier)
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

	_, err = client.Delete(cmd.Context(), "/systemusers/"+id+"/sshkeys/"+keyID)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "SSH key %s deleted successfully.\n", keyID)
	return nil
}

// renderPlan renders a plan and returns an ExitError with the plan exit code.
// When --output json is explicitly requested, JSON goes to stdout.
// Otherwise, human-readable output goes to stderr.
func renderPlan(cmd *cobra.Command, p *plan.Plan) error {
	// Check if the user explicitly set --output (persistent flag on root).
	outputFlag := cmd.Root().PersistentFlags().Lookup("output")
	if outputFlag != nil && outputFlag.Changed && outputFlag.Value.String() == "json" {
		if err := p.RenderJSON(cmd.OutOrStdout()); err != nil {
			return err
		}
	} else {
		if err := p.RenderHuman(cmd.ErrOrStderr()); err != nil {
			return err
		}
	}
	return &ExitError{Code: plan.ExitCodePlan}
}

// writeListFooter writes a "── N of TOTAL items ──" footer to stderr.
func writeListFooter(cmd *cobra.Command, count, total int) {
	if count == total {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d items ──\n", count)
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), "── %d of %d items ──\n", count, total)
	}
}
