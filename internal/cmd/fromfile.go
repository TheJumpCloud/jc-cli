package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/klaassen-consulting/jc/internal/plan"
)

// Batch identifier sources (KLA-446): every single-identifier mutating
// command accepts its identifiers from one of
//
//   - an inline argument       jc users delete alice
//   - --from-file <path>       jc users delete --from-file offboard.txt
//   - --stdin (or a pipe)      jc users list --ids | jc users delete --stdin
//
// exactly one at a time. File/stdin lists are newline-separated, with
// blank lines and #-comments ignored — the on-disk runbook format
// (users-to-offboard.txt living in git next to the runbook).
//
// The batch path reuses each command's EXISTING single-item run
// function per row, so per-resource resolve/validation/API logic is
// identical between single and batch invocations. Two consequences,
// both deliberate:
//
//   - Batch execution requires --force or --non-interactive: one
//     confirmation prompt per row is useless for a 500-row file, and
//     silently skipping confirmation (what the old stdin-only path
//     did) hid the fact that a destructive batch was running
//     unconfirmed. The requirement makes that explicit.
//   - --plan renders ONE aggregated plan for the whole batch (the old
//     stdin path bypassed plan mode entirely).

// batchIdentifier is one row of a batch source, carrying its original
// line number so failure reports point back into the file.
type batchIdentifier struct {
	Value string
	Line  int
}

// readBatchIdentifiers parses a newline-separated identifier list:
// whitespace-trimmed, blank lines and #-comment lines skipped, line
// numbers preserved.
func readBatchIdentifiers(r io.Reader) ([]batchIdentifier, error) {
	scanner := bufio.NewScanner(r)
	var out []batchIdentifier
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		out = append(out, batchIdentifier{Value: text, Line: line})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading identifiers: %w", err)
	}
	return out, nil
}

// addBatchSourceFlags registers the two batch-source flags on a
// single-identifier mutating command.
func addBatchSourceFlags(cmd *cobra.Command) {
	cmd.Flags().String("from-file", "",
		"Read identifiers from a file (one per line; blank lines and # comments ignored)")
	cmd.Flags().Bool("stdin", false,
		"Read identifiers from stdin (one per line; implied when stdin is piped and no argument is given)")
}

// collectBatchIdentifiers resolves the identifier source, enforcing
// mutual exclusion. isBatch is false only for the single-inline-arg
// case.
func collectBatchIdentifiers(cmd *cobra.Command, args []string) (ids []batchIdentifier, isBatch bool, err error) {
	fromFile, _ := cmd.Flags().GetString("from-file")
	useStdin, _ := cmd.Flags().GetBool("stdin")

	sources := 0
	if len(args) > 0 {
		sources++
	}
	if fromFile != "" {
		sources++
	}
	if useStdin {
		sources++
	}
	if sources > 1 {
		return nil, false, fmt.Errorf("choose one identifier source: an inline argument, --from-file, or --stdin")
	}

	switch {
	case len(args) > 0:
		return []batchIdentifier{{Value: args[0], Line: 1}}, false, nil
	case fromFile != "":
		f, err := os.Open(fromFile)
		if err != nil {
			return nil, false, fmt.Errorf("opening --from-file: %w", err)
		}
		defer f.Close()
		ids, err := readBatchIdentifiers(f)
		if err != nil {
			return nil, false, err
		}
		if len(ids) == 0 {
			return nil, false, fmt.Errorf("--from-file %s contains no identifiers (blank lines and # comments are ignored)", fromFile)
		}
		return ids, true, nil
	case useStdin || isStdinPiped():
		ids, err := readBatchIdentifiers(stdinSource)
		if err != nil {
			return nil, false, err
		}
		if len(ids) == 0 {
			return nil, false, fmt.Errorf("stdin contained no identifiers")
		}
		return ids, true, nil
	default:
		return nil, false, fmt.Errorf("requires an identifier argument, --from-file, or --stdin")
	}
}

// batchRunE wraps a command's single-item run function with batch
// source handling. resourceType/action feed progress lines and the
// aggregated plan ("delete", "user").
func batchRunE(resourceType, action string, single func(*cobra.Command, string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ids, isBatch, err := collectBatchIdentifiers(cmd, args)
		if err != nil {
			return err
		}
		if !isBatch {
			return single(cmd, ids[0].Value)
		}
		return runBatchMutation(cmd, ids, resourceType, action, single)
	}
}

// runBatchMutation executes (or plans) the mutation across all rows.
func runBatchMutation(cmd *cobra.Command, ids []batchIdentifier, resourceType, action string, single func(*cobra.Command, string) error) error {
	// Aggregated plan: one box for the whole batch instead of N
	// (and instead of the old stdin path's nothing-at-all).
	if viper.GetBool("plan") {
		effects := make([]string, 0, len(ids))
		for _, id := range ids {
			effects = append(effects, fmt.Sprintf("line %d: %s %s %s", id.Line, action, resourceType, id.Value))
		}
		p := &plan.Plan{
			Action:   action,
			Resource: fmt.Sprintf("%s (batch of %d)", resourceType, len(ids)),
			Target:   fmt.Sprintf("%d identifiers", len(ids)),
			Effects:  effects,
		}
		return renderPlan(cmd, p)
	}

	// Batch execution is always unattended-by-design: demand the
	// explicit skip-confirmation signal rather than prompting N times
	// or silently skipping.
	if !shouldSkipConfirm() {
		return fmt.Errorf(
			"batch %s of %d %ss requires --force or --non-interactive (or preview with --plan first)",
			action, len(ids), resourceType)
	}

	progressW := cmd.ErrOrStderr()
	succeeded, failed := 0, 0
	var failures []string
	for i, id := range ids {
		fmt.Fprintf(progressW, "%s %d of %d: %s %s... ", action, i+1, len(ids), resourceType, id.Value)
		if err := single(cmd, id.Value); err != nil {
			failed++
			failures = append(failures, fmt.Sprintf("line %d (%s): %s", id.Line, id.Value, err))
			fmt.Fprintln(progressW, "FAILED")
			continue
		}
		succeeded++
		fmt.Fprintln(progressW, "done")
	}

	fmt.Fprintf(progressW, "\n── Summary: %d succeeded, %d failed ──\n", succeeded, failed)
	if failed > 0 {
		for _, f := range failures {
			fmt.Fprintln(progressW, "  "+f)
		}
		return fmt.Errorf("%d of %d %s operations failed", failed, len(ids), action)
	}
	return nil
}
