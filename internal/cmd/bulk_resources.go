package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/output"
)

// Generalized CSV bulk engine (KLA-466) — extends `jc bulk` beyond
// users. Each resource declares a small HAND-CURATED field map: the
// recon showed schema metadata alone would produce wrong bodies (the
// schema says `role`, the API wants `roleName`; devices' schema lists
// 17 fields but only 4 are writable), so maps are explicit and each
// one is pinned by a coverage test.
//
// The CSV contract matches `jc bulk users`: a header row (lowercased,
// trimmed), one operation per row via the `operation` column
// (default create), empty cells omitted. Unlike the users passthrough
// this engine VALIDATES headers — an unknown column is an error, not
// a silently dropped typo.
//
// `jc bulk users` stays on its original code path until this engine
// proves parity (recorded on the ticket).

// bulkClient is the verb surface the engine needs; both *api.V1Client
// and *api.V2Client satisfy it.
type bulkClient interface {
	Create(ctx context.Context, endpoint string, body any) (json.RawMessage, error)
	Update(ctx context.Context, endpoint string, body any) (json.RawMessage, error)
	Delete(ctx context.Context, endpoint string) (json.RawMessage, error)
}

// bulkFieldSpec maps one CSV column to an API body field.
type bulkFieldSpec struct {
	// APIKey is the JSON body key (aliases live here: the admins
	// `role` column writes `roleName`).
	APIKey string
	// Type drives coercion: "string" or "bool".
	Type string
	// Create / Update mark writability per operation.
	Create bool
	Update bool
	// RequiredOnCreate rejects create rows missing this column.
	RequiredOnCreate bool
}

// bulkResourceSpec declares one resource's CSV surface.
type bulkResourceSpec struct {
	// Use / Short name the subcommand.
	Use, Short string
	// Endpoint is the collection path ("/usergroups").
	Endpoint string
	// IdentifierCol is the CSV column naming the target row for
	// update/delete (resolved through the resource's resolver).
	IdentifierCol string
	// AllowCreate is false for resources that can't be created via
	// API (devices are enrolled, not created).
	AllowCreate bool
	// Fields maps lowercase CSV headers → specs. The IdentifierCol
	// and "operation" are implicitly valid headers.
	Fields map[string]bulkFieldSpec
	// setup builds the right API client (V1 vs V2) plus a resolver
	// bound to it, so the engine never needs the concrete client type.
	setup func() (bulkClient, func(ctx context.Context, identifier string) (string, error), error)
}

// bulkResourceSpecs is the launch registry: device groups, user
// groups, devices (update/delete only), admins. Policies are
// deliberately absent — their body is template-specific nested JSON,
// not expressible as flat CSV (see the KLA-466 recon on the ticket).
func bulkResourceSpecs() []bulkResourceSpec {
	return []bulkResourceSpec{
		{
			Use: "user-groups", Short: "Bulk create/update/delete user groups from CSV",
			Endpoint:      "/usergroups",
			IdentifierCol: "name",
			AllowCreate:   true,
			Fields: map[string]bulkFieldSpec{
				"name":        {APIKey: "name", Type: "string", Create: true, Update: true, RequiredOnCreate: true},
				"description": {APIKey: "description", Type: "string", Create: true, Update: true},
			},
			setup: func() (bulkClient, func(context.Context, string) (string, error), error) {
				c, err := newV2Client()
				if err != nil {
					return nil, nil, err
				}
				return c, func(ctx context.Context, id string) (string, error) { return resolveUserGroup(ctx, c, id) }, nil
			},
		},
		{
			Use: "device-groups", Short: "Bulk create/update/delete device groups from CSV",
			Endpoint:      "/systemgroups",
			IdentifierCol: "name",
			AllowCreate:   true,
			Fields: map[string]bulkFieldSpec{
				"name":        {APIKey: "name", Type: "string", Create: true, Update: true, RequiredOnCreate: true},
				"description": {APIKey: "description", Type: "string", Create: true, Update: true},
			},
			setup: func() (bulkClient, func(context.Context, string) (string, error), error) {
				c, err := newV2Client()
				if err != nil {
					return nil, nil, err
				}
				return c, func(ctx context.Context, id string) (string, error) { return resolveDeviceGroup(ctx, c, id) }, nil
			},
		},
		{
			Use: "devices", Short: "Bulk update/delete devices from CSV (devices are enrolled, not created)",
			Endpoint:      "/systems",
			IdentifierCol: "hostname",
			AllowCreate:   false,
			Fields: map[string]bulkFieldSpec{
				// The four fields the single-item update exposes — the
				// schema lists 17 but the rest aren't writable.
				"displayname":                     {APIKey: "displayName", Type: "string", Update: true},
				"allowsshpasswordauthentication":  {APIKey: "allowSshPasswordAuthentication", Type: "bool", Update: true},
				"allowmultifactorauthentication":  {APIKey: "allowMultiFactorAuthentication", Type: "bool", Update: true},
				"allowpublickeyauthentication":    {APIKey: "allowPublicKeyAuthentication", Type: "bool", Update: true},
			},
			setup: func() (bulkClient, func(context.Context, string) (string, error), error) {
				c, err := newV1Client()
				if err != nil {
					return nil, nil, err
				}
				return c, func(ctx context.Context, id string) (string, error) { return resolveDevice(ctx, c, id) }, nil
			},
		},
		{
			Use: "admins", Short: "Bulk create/update/delete administrators from CSV",
			Endpoint:      "/users",
			IdentifierCol: "email",
			AllowCreate:   true,
			Fields: map[string]bulkFieldSpec{
				"email": {APIKey: "email", Type: "string", Create: true, RequiredOnCreate: true},
				// The alias that proves hand-curation: the schema calls
				// this field `role`; the API body key is `roleName`.
				"role":       {APIKey: "roleName", Type: "string", Create: true, Update: true},
				"enable-mfa": {APIKey: "enableMultiFactor", Type: "bool", Create: true, Update: true},
				"firstname":  {APIKey: "firstname", Type: "string", Create: true, Update: true},
				"lastname":   {APIKey: "lastname", Type: "string", Create: true, Update: true},
			},
			setup: func() (bulkClient, func(context.Context, string) (string, error), error) {
				c, err := newV1Client()
				if err != nil {
					return nil, nil, err
				}
				return c, func(ctx context.Context, id string) (string, error) { return resolveAdmin(ctx, c, id) }, nil
			},
		},
	}
}

// bulkRowResult is one row's outcome in the report.
type bulkRowResult struct {
	Row       int    `json:"row"` // original CSV line number (header = 1)
	Operation string `json:"operation"`
	Target    string `json:"target"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

func newBulkResourceCmd(spec bulkResourceSpec) *cobra.Command {
	var filePath string

	ops := "update, delete"
	if spec.AllowCreate {
		ops = "create (default), update, delete"
	}
	cols := make([]string, 0, len(spec.Fields))
	for h := range spec.Fields {
		cols = append(cols, h)
	}
	sort.Strings(cols)

	cmd := &cobra.Command{
		Use:   spec.Use + " --file <csv>",
		Short: spec.Short,
		Long: fmt.Sprintf(`Bulk-operate on %s from a CSV file.

The header row names the columns; the optional `+"`operation`"+` column
selects %s per row. The `+"`%s`"+` column identifies the target row for
update/delete (resolved by name or ID). Unknown columns are an ERROR —
typos never get silently dropped. Boolean columns accept
true/false/1/0. `+"`--plan`"+` previews per-row actions without executing.

Valid columns: %s`, spec.Use, ops, spec.IdentifierCol, strings.Join(cols, ", ")),
		Example: fmt.Sprintf(`  jc bulk %s --file batch.csv --plan
  jc bulk %s --file batch.csv --force`, spec.Use, spec.Use),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBulkResource(cmd, spec, filePath)
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "Path to the CSV file (required)")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func runBulkResource(cmd *cobra.Command, spec bulkResourceSpec, filePath string) error {
	headers, rows, err := parseBulkCSV(filePath)
	if err != nil {
		return err
	}

	// Header validation — the users passthrough silently accepts
	// typo'd columns; this engine refuses them.
	validSet := map[string]bool{spec.IdentifierCol: true, "operation": true}
	for h := range spec.Fields {
		validSet[h] = true
	}
	valid := make([]string, 0, len(validSet))
	for h := range validSet {
		valid = append(valid, h)
	}
	sort.Strings(valid)
	for _, h := range headers {
		known := h == "operation" || h == spec.IdentifierCol
		if _, ok := spec.Fields[h]; ok {
			known = true
		}
		if !known {
			return fmt.Errorf("unknown CSV column %q for %s (valid: %s)", h, spec.Use, strings.Join(valid, ", "))
		}
	}

	// Pre-validate every row: operation known/allowed, identifier
	// present where needed, bools parseable, create-required fields
	// present. All problems in one pass (the aggregate-errors
	// convention).
	type plannedRow struct {
		line      int
		op        string
		target    string
		body      map[string]any
		identCell string
	}
	var planned []plannedRow
	var problems []string
	for i, row := range rows {
		line := i + 2 // header is line 1
		fields := rowToFields(headers, row)
		op := determineOperation(fields)

		switch op {
		case "create":
			if !spec.AllowCreate {
				problems = append(problems, fmt.Sprintf("line %d: %s cannot be created via CSV", line, spec.Use))
				continue
			}
		case "update", "delete":
			if fields[spec.IdentifierCol] == "" {
				problems = append(problems, fmt.Sprintf("line %d: %s requires the %q column", line, op, spec.IdentifierCol))
				continue
			}
		default:
			problems = append(problems, fmt.Sprintf("line %d: unknown operation %q (want create, update, or delete)", line, op))
			continue
		}

		body := map[string]any{}
		rowBroken := false
		for header, cell := range fields {
			if header == "operation" {
				continue
			}
			fs, ok := spec.Fields[header]
			if !ok {
				// The identifier column without a field spec (devices
				// hostname, admins email-on-update) is target-only.
				continue
			}
			writable := (op == "create" && fs.Create) || (op == "update" && fs.Update)
			if op == "delete" || !writable {
				continue
			}
			switch fs.Type {
			case "bool":
				switch strings.ToLower(cell) {
				case "true", "1":
					body[fs.APIKey] = true
				case "false", "0":
					body[fs.APIKey] = false
				default:
					problems = append(problems, fmt.Sprintf("line %d: column %q value %q is not a boolean (true/false/1/0)", line, header, cell))
					rowBroken = true
				}
			default:
				body[fs.APIKey] = cell
			}
		}
		if op == "create" {
			for header, fs := range spec.Fields {
				if fs.RequiredOnCreate && fields[header] == "" {
					problems = append(problems, fmt.Sprintf("line %d: create requires the %q column", line, header))
					rowBroken = true
				}
			}
		}
		if rowBroken {
			continue
		}

		target := fields[spec.IdentifierCol]
		if target == "" {
			target = fmt.Sprintf("row %d", line)
		}
		planned = append(planned, plannedRow{line: line, op: op, target: target, body: body, identCell: fields[spec.IdentifierCol]})
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid CSV for %s:\n  %s", spec.Use, strings.Join(problems, "\n  "))
	}

	// Plan mode: per-row preview, no execution.
	if viper.GetBool("plan") {
		w := cmd.OutOrStdout()
		fmt.Fprintf(w, "Plan: %d %s operation(s) from %s\n", len(planned), spec.Use, filePath)
		for _, p := range planned {
			fmt.Fprintf(w, "  line %d: %s %s\n", p.line, p.op, p.target)
		}
		return nil
	}

	// Same batch gate as the KLA-446 identifier lists: unattended by
	// design, so demand the explicit skip-confirmation signal.
	if !shouldSkipConfirm() {
		return fmt.Errorf("bulk %s of %d rows requires --force or --non-interactive (or preview with --plan first)", spec.Use, len(planned))
	}

	client, resolveID, err := spec.setup()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	progressW := cmd.ErrOrStderr()
	results := make([]bulkRowResult, 0, len(planned))
	failed := 0
	for i, p := range planned {
		fmt.Fprintf(progressW, "[%d/%d] %s %s... ", i+1, len(planned), p.op, p.target)
		err := func() error {
			switch p.op {
			case "create":
				_, err := client.Create(ctx, spec.Endpoint, p.body)
				return err
			case "update":
				id, err := resolveID(ctx, p.identCell)
				if err != nil {
					return err
				}
				_, err = client.Update(ctx, spec.Endpoint+"/"+id, p.body)
				return err
			default: // delete
				id, err := resolveID(ctx, p.identCell)
				if err != nil {
					return err
				}
				_, err = client.Delete(ctx, spec.Endpoint+"/"+id)
				return err
			}
		}()
		r := bulkRowResult{Row: p.line, Operation: p.op, Target: p.target, Status: "ok"}
		if err != nil {
			failed++
			r.Status = "failed"
			r.Error = err.Error()
			fmt.Fprintln(progressW, "FAILED")
		} else {
			fmt.Fprintln(progressW, "done")
		}
		results = append(results, r)
	}

	fmt.Fprintf(progressW, "\n── Summary: %d succeeded, %d failed ──\n", len(planned)-failed, failed)
	payload := make([]json.RawMessage, 0, len(results))
	for _, r := range results {
		b, _ := json.Marshal(r)
		payload = append(payload, b)
	}
	opts := output.CurrentOptions()
	opts.DefaultFields = []string{"row", "operation", "target", "status", "error"}
	if err := output.WriteList(cmd.OutOrStdout(), payload, opts); err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d %s operations failed", failed, len(planned), spec.Use)
	}
	return nil
}
