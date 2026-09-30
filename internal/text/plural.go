// Package text holds small English-language helpers shared by the CLI, the TUI
// and the MCP server, so that a count and its noun agree wherever they are
// printed. It deliberately does no general pluralisation: a noun whose plural
// is not a bare "s" states that plural itself.
package text

import "strconv"

// Plural is the "s" suffix for a count: "" when n is 1, "s" otherwise. Use it
// when the noun is already in the format string — "%d check error%s".
func Plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// Count renders a count together with its noun, pluralising the noun with a
// bare "s" when n is not 1: Count(1, "item") is "1 item" and Count(0, "item")
// is "0 items". For a noun English pluralises some other way, use CountOf.
func Count(n int, singular string) string {
	return CountOf(n, singular, singular+"s")
}

// CountOf is Count with the plural form given explicitly, for nouns that do
// not take a bare "s": CountOf(1, "entry", "entries").
func CountOf(n int, singular, plural string) string {
	noun := plural
	if n == 1 {
		noun = singular
	}
	return strconv.Itoa(n) + " " + noun
}
