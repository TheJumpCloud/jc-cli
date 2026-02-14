package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// stdinSource is the reader used for --stdin processing. Overridable in tests.
var stdinSource io.Reader = os.Stdin

// isStdinPiped returns true when stdin is piped (not a terminal).
func isStdinPiped() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice == 0
}

// readLinesFromStdin reads non-empty, trimmed lines from stdin.
func readLinesFromStdin() ([]string, error) {
	scanner := bufio.NewScanner(stdinSource)
	var lines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading stdin: %w", err)
	}
	return lines, nil
}

// stdinBatchResult tracks results for batch stdin operations.
type stdinBatchResult struct {
	Succeeded int
	Failed    int
	Errors    []string
}

// runStdinBatch processes identifiers from stdin, calling fn for each one.
// Progress is written to progressW (typically cmd.ErrOrStderr()).
func runStdinBatch(identifiers []string, resourceType string, action string, progressW io.Writer, fn func(identifier string) error) *stdinBatchResult {
	result := &stdinBatchResult{}

	for i, id := range identifiers {
		fmt.Fprintf(progressW, "%s %d of %d: %s %s... ", action, i+1, len(identifiers), resourceType, id)

		if err := fn(id); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", id, err))
			fmt.Fprintln(progressW, "FAILED")
		} else {
			result.Succeeded++
			fmt.Fprintln(progressW, "done")
		}
	}

	fmt.Fprintf(progressW, "\n── Summary: %d succeeded, %d failed ──\n", result.Succeeded, result.Failed)
	return result
}
