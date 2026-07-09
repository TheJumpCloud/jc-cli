package cmd

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/output"
	"github.com/klaassen-consulting/jc/internal/resolve"
)

// bulkResult tracks the outcome of a single bulk operation row.
type bulkResult struct {
	Row       int    `json:"row"`
	Operation string `json:"operation"`
	Username  string `json:"username,omitempty"`
	Status    string `json:"status"` // "succeeded", "failed", "skipped"
	Error     string `json:"error,omitempty"`
}

func newBulkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk",
		Short: "Bulk operations from CSV files",
		Long:  "Process bulk create, update, or delete operations from CSV files.",
	}

	cmd.AddCommand(newBulkUsersCmd())
	// Generalized CSV engine resources (KLA-466). Users stays on its
	// original path until the engine proves parity.
	for _, spec := range bulkResourceSpecs() {
		cmd.AddCommand(newBulkResourceCmd(spec))
	}

	return cmd
}

func newBulkUsersCmd() *cobra.Command {
	var fileFlag string

	cmd := &cobra.Command{
		Use:   "users --file <csv-file>",
		Short: "Bulk user operations from CSV",
		Long: `Process bulk user operations from a CSV file.

The CSV file must have a header row. Column names map to JumpCloud user fields.
An "operation" column determines the action for each row: create, update, or delete.

If no "operation" column is present, all rows are treated as create operations.

For update and delete operations, a "username" or "_id" column is required
to identify the target user.

Example CSV:
  operation,username,email,firstname,lastname,department
  create,jsmith,jsmith@acme.com,John,Smith,Engineering
  update,jdoe,jdoe@acme.com,,Doe Updated,Sales
  delete,bwilson,,,,

Progress is shown during processing. Failed operations are logged but do not
stop remaining operations. A summary report is printed at the end.

Use --force to skip the confirmation prompt.
Use --plan to preview all operations without executing.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBulkUsers(cmd, fileFlag)
		},
	}

	cmd.Flags().StringVar(&fileFlag, "file", "", "Path to CSV file (required)")
	_ = cmd.MarkFlagRequired("file")

	return cmd
}

// parseBulkCSV reads and validates a CSV file, returning headers and data rows.
func parseBulkCSV(filePath string) ([]string, [][]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("opening CSV file: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("parsing CSV file: %w", err)
	}

	if len(records) < 2 {
		return nil, nil, fmt.Errorf("CSV file must have a header row and at least one data row")
	}

	headers := records[0]
	// Normalize header names to lowercase and trim spaces.
	for i, h := range headers {
		headers[i] = strings.TrimSpace(strings.ToLower(h))
	}

	return headers, records[1:], nil
}

// rowToFields converts a CSV row to a map using the header names as keys.
// Empty values are omitted.
func rowToFields(headers []string, row []string) map[string]string {
	fields := make(map[string]string)
	for i, h := range headers {
		if i < len(row) {
			val := strings.TrimSpace(row[i])
			if val != "" {
				fields[h] = val
			}
		}
	}
	return fields
}

// determineOperation returns the operation for a row.
// If the row has an "operation" field, it is used; otherwise defaults to defaultOp.
func determineOperation(fields map[string]string) string {
	if op, ok := fields["operation"]; ok {
		return strings.ToLower(op)
	}
	return "create"
}

func runBulkUsers(cmd *cobra.Command, filePath string) error {
	headers, rows, err := parseBulkCSV(filePath)
	if err != nil {
		return err
	}

	// Count operations for the confirmation prompt.
	creates, updates, deletes, unknown := 0, 0, 0, 0
	for _, row := range rows {
		fields := rowToFields(headers, row)
		switch determineOperation(fields) {
		case "create":
			creates++
		case "update":
			updates++
		case "delete":
			deletes++
		default:
			unknown++
		}
	}

	if unknown > 0 {
		return fmt.Errorf("%d rows have unknown operation values. Valid operations: create, update, delete", unknown)
	}

	// Show summary and confirm (unless --force or --plan).
	isPlan := viper.GetBool("plan")
	if isPlan {
		fmt.Fprintf(cmd.ErrOrStderr(), "Plan: %d creates, %d updates, %d deletes (%d total rows)\n",
			creates, updates, deletes, len(rows))
		for i, row := range rows {
			fields := rowToFields(headers, row)
			op := determineOperation(fields)
			username := fields["username"]
			if username == "" {
				username = fields["_id"]
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "  [%d/%d] %s %s\n", i+1, len(rows), op, username)
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "No changes made (plan mode).")
		return nil
	}

	if mustAbortWithoutTTY() {
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled (no TTY for confirmation prompt). Use --force to skip.")
		return nil
	}
	if shouldConfirm() {
		summary := fmt.Sprintf("Ready to process: %d creates, %d updates, %d deletes (%d total). Continue? [y/N] ",
			creates, updates, deletes, len(rows))
		fmt.Fprint(cmd.ErrOrStderr(), summary)
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

	client, err := newV1Client()
	if err != nil {
		return err
	}

	var results []bulkResult
	succeeded, failed, skipped := 0, 0, 0

	for i, row := range rows {
		fields := rowToFields(headers, row)
		op := determineOperation(fields)
		username := fields["username"]

		// Show progress to stderr.
		label := username
		if label == "" {
			label = fields["_id"]
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Processing %d of %d: %s %s... ", i+1, len(rows), op, label)

		result := bulkResult{
			Row:       i + 1,
			Operation: op,
			Username:  username,
		}

		var opErr error
		switch op {
		case "create":
			opErr = bulkCreateUser(cmd.Context(), client, fields)
		case "update":
			opErr = bulkUpdateUser(cmd.Context(), client, fields)
		case "delete":
			opErr = bulkDeleteUser(cmd.Context(), client, fields)
		}

		if opErr != nil {
			result.Status = "failed"
			result.Error = opErr.Error()
			failed++
			fmt.Fprintln(cmd.ErrOrStderr(), "FAILED")
			if viper.GetBool("verbose") {
				fmt.Fprintf(cmd.ErrOrStderr(), "  Error: %s\n", opErr.Error())
			}
		} else {
			result.Status = "succeeded"
			succeeded++
			fmt.Fprintln(cmd.ErrOrStderr(), "done")
		}

		results = append(results, result)
	}

	// Print summary.
	fmt.Fprintf(cmd.ErrOrStderr(), "\n── Summary: %d succeeded, %d failed, %d skipped ──\n",
		succeeded, failed, skipped)

	// Output results in requested format.
	opts := output.CurrentOptions()
	opts.DefaultFields = []string{"row", "operation", "username", "status", "error"}

	// Convert results to JSON for the output engine.
	var jsonResults []json.RawMessage
	for _, r := range results {
		data, _ := json.Marshal(r)
		jsonResults = append(jsonResults, data)
	}

	return output.WriteList(cmd.OutOrStdout(), jsonResults, opts)
}

// bulkCreateUser creates a user from CSV field data.
func bulkCreateUser(ctx context.Context, client *api.V1Client, fields map[string]string) error {
	// Validate required fields.
	if fields["username"] == "" {
		return fmt.Errorf("missing required field: username")
	}
	if fields["email"] == "" {
		return fmt.Errorf("missing required field: email")
	}

	body := make(map[string]string)
	for k, v := range fields {
		if k == "operation" {
			continue
		}
		body[k] = v
	}

	_, err := client.Create(ctx, "/systemusers", body)
	return err
}

// bulkUpdateUser updates an existing user identified by username or _id.
func bulkUpdateUser(ctx context.Context, client *api.V1Client, fields map[string]string) error {
	identifier := fields["username"]
	if identifier == "" {
		identifier = fields["_id"]
	}
	if identifier == "" {
		return fmt.Errorf("update requires username or _id to identify the user")
	}

	r := resolve.NewResolver(client)
	id, err := r.Resolve(ctx, identifier, resolve.UserConfig)
	if err != nil {
		return fmt.Errorf("resolving user %q: %w", identifier, err)
	}

	body := make(map[string]string)
	for k, v := range fields {
		if k == "operation" || k == "_id" {
			continue
		}
		body[k] = v
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update")
	}

	_, err = client.Update(ctx, "/systemusers/"+id, body)
	return err
}

// bulkDeleteUser deletes a user identified by username or _id.
func bulkDeleteUser(ctx context.Context, client *api.V1Client, fields map[string]string) error {
	identifier := fields["username"]
	if identifier == "" {
		identifier = fields["_id"]
	}
	if identifier == "" {
		return fmt.Errorf("delete requires username or _id to identify the user")
	}

	r := resolve.NewResolver(client)
	id, err := r.Resolve(ctx, identifier, resolve.UserConfig)
	if err != nil {
		return fmt.Errorf("resolving user %q: %w", identifier, err)
	}

	_, err = client.Delete(ctx, "/systemusers/"+id)
	return err
}
