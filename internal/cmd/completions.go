package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/resolve"
)

// completeResourceNames returns a ValidArgsFunction that provides tab completions
// from the resolver cache for the given resource config. Both names and IDs are
// offered as candidates. Names show the ID as a description; IDs show the name.
// Returns no completions if the cache is empty or missing.
func completeResourceNames(cfg resolve.ResourceConfig) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		entries := resolve.ReadCacheEntries(cfg.CacheKey)
		if len(entries) == 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var completions []string
		for name, entry := range entries {
			completions = append(completions, fmt.Sprintf("%s\t%s", name, entry.ID))
			completions = append(completions, fmt.Sprintf("%s\t%s", entry.ID, name))
		}
		sort.Strings(completions)
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}
