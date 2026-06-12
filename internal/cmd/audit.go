package cmd

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/api"
	"github.com/klaassen-consulting/jc/internal/audit"
	"github.com/klaassen-consulting/jc/internal/config"
	"github.com/klaassen-consulting/jc/internal/mcp"
	"github.com/klaassen-consulting/jc/internal/output"
)

func newAuditCmd() *cobra.Command {
	var (
		categoriesFlag []string
		severityFlag   string
		thresholdFlag  string
		exitCodeFlag   bool
	)

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Run cross-resource health checks (security, compliance, hygiene, identity)",
		Long: `Run a battery of health checks across the configured JumpCloud org and
report any findings, grouped by category and ordered by severity.

The check registry is composable — adding a new check is a one-line
registration in internal/audit/. The same primitive backs the
jc-security-audit and jc-compliance-check skills (which now invoke
` + "`jc audit --category {security,compliance} --output json`" + ` instead of
scripting raw queries).

Severities, low → high: info, low, medium, high, critical.

Default invocation runs every registered check; use --category and
--severity to scope. Pair --exit-code with --threshold for CI gating
(non-zero exit when any finding meets or exceeds the threshold).

Output formats: json (skill-friendly), human (default, grouped by
category), table, csv, ndjson, yaml.`,
		Example: `  jc audit                              # everything, human-readable
  jc audit --category security          # security checks only
  jc audit --severity high              # high/critical findings only
  jc audit --output json                # machine-readable for skills
  jc audit --exit-code --threshold high # CI gate: fail on high+ findings`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuditHealth(cmd, auditHealthOpts{
				categories:   categoriesFlag,
				minSeverity:  severityFlag,
				threshold:    thresholdFlag,
				exitOnThresh: exitCodeFlag,
			})
		},
	}

	cmd.Flags().StringSliceVar(&categoriesFlag, "category", nil,
		"Restrict to one or more categories: security, compliance, hygiene, identity")
	cmd.Flags().StringVar(&severityFlag, "severity", "",
		"Show only findings at or above this severity (info, low, medium, high, critical)")
	cmd.Flags().StringVar(&thresholdFlag, "threshold", "high",
		"Severity threshold used by --exit-code")
	cmd.Flags().BoolVar(&exitCodeFlag, "exit-code", false,
		"Exit with code 1 if any finding meets or exceeds --threshold (for CI gating)")

	cmd.AddCommand(newAuditVerifyCmd())
	return cmd
}

type auditHealthOpts struct {
	categories   []string
	minSeverity  string
	threshold    string
	exitOnThresh bool
}

func runAuditHealth(cmd *cobra.Command, o auditHealthOpts) error {
	// Validate severity / threshold up front so we fail before the
	// API fetch on a typo — wasted round-trips erode trust in --exit-code
	// gates that retry on transient failures.
	var minSev audit.Severity
	if o.minSeverity != "" {
		minSev = audit.Severity(strings.ToLower(o.minSeverity))
		if !minSev.Valid() {
			return fmt.Errorf("invalid --severity %q: want one of info, low, medium, high, critical", o.minSeverity)
		}
	}
	threshold := audit.Severity(strings.ToLower(o.threshold))
	if !threshold.Valid() {
		return fmt.Errorf("invalid --threshold %q: want one of info, low, medium, high, critical", o.threshold)
	}

	var cats []audit.Category
	for _, c := range o.categories {
		cat := audit.Category(strings.ToLower(c))
		if !cat.Valid() {
			return fmt.Errorf("invalid --category %q: want one of security, compliance, hygiene, identity", c)
		}
		cats = append(cats, cat)
	}

	v1, err := newV1ClientForAudit()
	if err != nil {
		return fmt.Errorf("building v1 client: %w", err)
	}
	v2, err := newV2ClientForAudit()
	if err != nil {
		return fmt.Errorf("building v2 client: %w", err)
	}

	ctx := cmd.Context()
	data, err := audit.Fetch(ctx, &clientFetcher{v1: v1, v2: v2})
	if err != nil {
		return fmt.Errorf("fetching audit data: %w", err)
	}

	results, err := audit.Run(ctx, data, audit.RunOptions{
		Categories:  cats,
		MinSeverity: minSev,
	})
	if err != nil {
		return fmt.Errorf("running audit: %w", err)
	}
	audit.SortByCategoryAndSeverity(results)

	if err := renderAuditResults(cmd.OutOrStdout(), results, data.Warnings); err != nil {
		return fmt.Errorf("rendering: %w", err)
	}

	if o.exitOnThresh && audit.AnyFindingAtLeast(results, threshold) {
		return &ExitError{
			Code: 1,
			Err:  fmt.Errorf("audit found findings at or above threshold %q", threshold),
		}
	}
	return nil
}

// newV1ClientForAudit / newV2ClientForAudit are package vars so audit
// unit tests can stub clients without going through the full auth
// resolution path.
var newV1ClientForAudit = api.NewV1Client
var newV2ClientForAudit = api.NewV2Client

// clientFetcher adapts the v1/v2 API clients to the audit.Fetcher
// interface. Lives in cmd/ (rather than audit/) so the audit package
// stays unaware of the api/ client surface — easier to mock and easier
// to retarget at a different transport (MCP, in-memory) later.
type clientFetcher struct {
	v1 *api.V1Client
	v2 *api.V2Client
}

func (f *clientFetcher) Users(ctx context.Context) ([]json.RawMessage, error) {
	r, err := f.v1.ListAll(ctx, "/systemusers", api.ListOptions{})
	if err != nil {
		return nil, err
	}
	return r.Data, nil
}

func (f *clientFetcher) Admins(ctx context.Context) ([]json.RawMessage, error) {
	// V1 /users serves admins (not /administrators which doesn't exist).
	// See internal/cmd/admins.go and the compliance MCP App for prior art.
	r, err := f.v1.ListAll(ctx, "/users", api.ListOptions{})
	if err != nil {
		return nil, err
	}
	return r.Data, nil
}

func (f *clientFetcher) Devices(ctx context.Context) ([]json.RawMessage, error) {
	r, err := f.v1.ListAll(ctx, "/systems", api.ListOptions{})
	if err != nil {
		return nil, err
	}
	return r.Data, nil
}

func (f *clientFetcher) UserGroups(ctx context.Context) ([]json.RawMessage, error) {
	r, err := f.v2.ListAll(ctx, "/usergroups", api.V2ListOptions{})
	if err != nil {
		return nil, err
	}
	return r.Data, nil
}

func (f *clientFetcher) SystemGroups(ctx context.Context) ([]json.RawMessage, error) {
	r, err := f.v2.ListAll(ctx, "/systemgroups", api.V2ListOptions{})
	if err != nil {
		return nil, err
	}
	return r.Data, nil
}

func (f *clientFetcher) AuthPolicies(ctx context.Context) ([]json.RawMessage, error) {
	r, err := f.v2.ListAll(ctx, "/authn/policies", api.V2ListOptions{})
	if err != nil {
		return nil, err
	}
	return r.Data, nil
}

func (f *clientFetcher) IPLists(ctx context.Context) ([]json.RawMessage, error) {
	r, err := f.v2.ListAll(ctx, "/iplists", api.V2ListOptions{})
	if err != nil {
		return nil, err
	}
	return r.Data, nil
}

// renderAuditResults dispatches on --output. JSON, table, csv, and
// ndjson re-use the shared output package by marshalling the findings
// slice to json.RawMessage rows. Human format is hand-rolled for the
// grouped-by-category layout — output.WriteList's default human format
// is a flat key:value dump that's hard to scan for an audit report.
func renderAuditResults(w io.Writer, results []audit.CheckResult, warnings []string) error {
	opts := output.CurrentOptions()
	switch opts.Format {
	case output.FormatJSON:
		return writeAuditJSON(w, results, warnings)
	case output.FormatNDJSON:
		return writeAuditNDJSON(w, results)
	case output.FormatTable, output.FormatCSV, output.FormatYAML:
		return writeAuditTabular(w, results, opts)
	default:
		return writeAuditHuman(w, results, warnings)
	}
}

// auditJSONPayload is the JSON shape callers should depend on. It's
// intentionally a wrapper object (not a bare array of findings) so
// future additions (run metadata, timings, warnings) extend the shape
// without breaking existing consumers.
type auditJSONPayload struct {
	Results  []audit.CheckResult `json:"results"`
	Findings []audit.Finding     `json:"findings"`
	Warnings []string            `json:"warnings,omitempty"`
}

func writeAuditJSON(w io.Writer, results []audit.CheckResult, warnings []string) error {
	payload := auditJSONPayload{
		Results:  results,
		Findings: flattenFindings(results),
		Warnings: warnings,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func writeAuditNDJSON(w io.Writer, results []audit.CheckResult) error {
	enc := json.NewEncoder(w)
	for _, f := range flattenFindings(results) {
		if err := enc.Encode(f); err != nil {
			return err
		}
	}
	return nil
}

func writeAuditTabular(w io.Writer, results []audit.CheckResult, opts output.Options) error {
	rows := make([]json.RawMessage, 0)
	for _, f := range flattenFindings(results) {
		b, err := json.Marshal(f)
		if err != nil {
			return err
		}
		rows = append(rows, b)
	}
	opts.DefaultFields = []string{"severity", "category", "check_id", "resource_ref", "title"}
	return output.WriteList(w, rows, opts)
}

func flattenFindings(results []audit.CheckResult) []audit.Finding {
	var all []audit.Finding
	for _, r := range results {
		all = append(all, r.Findings...)
	}
	return all
}

// severityGlyph is the single-char tag used in human output. Chosen to
// be ASCII so the report copy-pastes cleanly into runbooks and Slack;
// color (when stdout is a tty) carries the visual weight.
func severityGlyph(s audit.Severity) string {
	switch s {
	case audit.SeverityCritical:
		return "X"
	case audit.SeverityHigh:
		return "!"
	case audit.SeverityMedium:
		return "*"
	case audit.SeverityLow:
		return "."
	case audit.SeverityInfo:
		return "i"
	default:
		return "?"
	}
}

func writeAuditHuman(w io.Writer, results []audit.CheckResult, warnings []string) error {
	totalFindings := 0
	for _, r := range results {
		totalFindings += len(r.Findings)
	}

	if totalFindings == 0 {
		fmt.Fprintf(w, "OK — %d checks ran clean, no findings.\n", len(results))
		writeAuditWarnings(w, warnings)
		return nil
	}

	// Group results by category for display. Sort was already applied
	// by SortByCategoryAndSeverity, so just walk in order.
	var currentCat audit.Category
	for _, r := range results {
		if r.Category != currentCat {
			currentCat = r.Category
			fmt.Fprintf(w, "\n== %s ==\n", strings.ToUpper(string(r.Category)))
		}
		if r.Error != "" {
			fmt.Fprintf(w, "  [ERR] %s — %s\n", r.CheckID, r.Error)
			continue
		}
		if len(r.Findings) == 0 {
			continue
		}
		fmt.Fprintf(w, "  %s — %s (%d)\n", r.CheckID, r.Title, len(r.Findings))
		for _, f := range r.Findings {
			fmt.Fprintf(w, "    %s [%s] %s\n", severityGlyph(f.Severity), strings.ToUpper(string(f.Severity)), f.Detail)
			if f.RemediationHint != "" {
				fmt.Fprintf(w, "      → %s\n", f.RemediationHint)
			}
		}
	}

	fmt.Fprintf(w, "\n%d findings across %d checks.\n", totalFindings, len(results))
	writeAuditWarnings(w, warnings)
	return nil
}

func writeAuditWarnings(w io.Writer, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintln(w, "\nWarnings (partial data — these sub-fetches failed):")
	for _, msg := range warnings {
		fmt.Fprintf(w, "  - %s\n", msg)
	}
}

// ─── existing: audit verify ────────────────────────────────────────

func newAuditVerifyCmd() *cobra.Command {
	var profileFlag, logPath, pubkeyOverride string

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify signatures in the signed MCP audit log",
		Long: `Read every signed manifest in the MCP signed-audit-log and verify the
chain against the configured per-profile public key.

Used to catch tampering with the audit trail. A successful run reports
the count of verified records and exits 0; any signature mismatch,
truncation, or decode error exits with a non-zero code.

By default, reads ~/.config/jc/mcp-audit-signed.log and uses the active
profile's signing pubkey from config (set the first time
'mcp.sign_destructive_ops' is enabled and a destructive op fires).

Pass --pubkey to verify against an externally-attested key — useful when
investigating a host whose config might itself be tampered with.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := profileFlag
			if profile == "" {
				profile = config.ActiveProfile()
			}

			pubkeyB64 := pubkeyOverride
			if pubkeyB64 == "" {
				pubkeyB64 = config.SigningPubkey(profile)
			}
			if pubkeyB64 == "" {
				return fmt.Errorf("no signing public key for profile %q. Either run a signed destructive op first to bootstrap, or pass --pubkey explicitly", profile)
			}
			pubBytes, err := base64.StdEncoding.DecodeString(pubkeyB64)
			if err != nil {
				return fmt.Errorf("decoding stored pubkey: %w", err)
			}
			if len(pubBytes) != ed25519.PublicKeySize {
				return fmt.Errorf("stored pubkey is %d bytes, want %d", len(pubBytes), ed25519.PublicKeySize)
			}

			path := logPath
			if path == "" {
				path = filepath.Join(config.ConfigDir(), "mcp-audit-signed.log")
			}
			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("opening signed audit log %s: %w", path, err)
			}
			defer f.Close()

			verified, verr := mcp.VerifyManifestStream(f, ed25519.PublicKey(pubBytes))
			if verr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Verified %d manifest(s) before failure: %v\n", verified, verr)
				return &ExitError{Code: 1, Err: verr}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "OK — verified %d signed manifest(s) for profile %q.\n", verified, profile)
			return nil
		},
	}

	cmd.Flags().StringVar(&profileFlag, "profile", "", "Profile whose signing pubkey to use (defaults to active profile)")
	cmd.Flags().StringVar(&logPath, "log", "", "Path to the signed audit log (defaults to ~/.config/jc/mcp-audit-signed.log)")
	cmd.Flags().StringVar(&pubkeyOverride, "pubkey", "", "Verify against this base64 Ed25519 pubkey instead of the configured one (override when investigating a possibly-tampered host)")

	return cmd
}
