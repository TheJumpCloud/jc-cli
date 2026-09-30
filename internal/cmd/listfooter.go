package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/text"
)

// The row-count footer every list command writes to stderr, so that piping
// stdout to a file or another process never picks it up.
//
// There are three entry points. A command that knows only how many rows it is
// about to print calls writeCountFooter; one that also knows the server-side
// total calls writeListFooter, which says "N of TOTAL" whenever the page falls
// short of the whole list; a list whose rows have a better name than "item"
// calls writeNounFooter. All of them go through text.Count, so a one-row list
// reads "── 1 item ──" rather than the "── 1 items ──" that makes a tool look
// unfinished.

// writeCountFooter writes a "── N items ──" footer to stderr.
func writeCountFooter(cmd *cobra.Command, count int) {
	writeNounFooter(cmd, count, "item", "items")
}

// writeNounFooter is writeCountFooter for a list whose rows have a better name
// than "item" — bundles, recipes, MCP tools, saved searches. Both forms are
// given: "saved search" does not pluralise to "saved searchs".
func writeNounFooter(cmd *cobra.Command, count int, singular, plural string) {
	fmt.Fprintf(cmd.ErrOrStderr(), "── %s ──\n", text.CountOf(count, singular, plural))
}

// writeListFooter writes a "── N of TOTAL items ──" footer to stderr, or the
// plain "── N items ──" form when this page is the whole list.
func writeListFooter(cmd *cobra.Command, count, total int) {
	if count == total {
		writeCountFooter(cmd, count)
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "── %d of %s ──\n", count, text.Count(total, "item"))
}
