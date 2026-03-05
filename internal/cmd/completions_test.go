package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/zalando/go-keyring"

	"github.com/klaassen-consulting/jc/internal/resolve"
)

func TestCompleteResourceNames(t *testing.T) {
	keyring.MockInit()
	dir := t.TempDir()
	t.Setenv("JC_CONFIG", dir)
	viper.Reset()

	cacheDir := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		t.Fatal(err)
	}
	viper.Set("cache.directory", cacheDir)

	cacheContent := `{
		"jdoe": {"id": "aaa111aaa111aaa111aaa111", "timestamp": "2026-03-05T00:00:00Z"},
		"admin": {"id": "bbb222bbb222bbb222bbb222", "timestamp": "2026-03-05T00:00:00Z"}
	}`
	if err := os.WriteFile(filepath.Join(cacheDir, "users.json"), []byte(cacheContent), 0600); err != nil {
		t.Fatal(err)
	}

	fn := completeResourceNames(resolve.UserConfig)

	t.Run("returns names and IDs", func(t *testing.T) {
		completions, directive := fn(&cobra.Command{}, nil, "")
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("expected ShellCompDirectiveNoFileComp, got %d", directive)
		}
		if len(completions) != 4 {
			t.Errorf("expected 4 completions (2 names + 2 IDs), got %d: %v", len(completions), completions)
		}
		found := map[string]bool{}
		for _, c := range completions {
			found[c] = true
		}
		for _, want := range []string{
			"jdoe\taaa111aaa111aaa111aaa111",
			"admin\tbbb222bbb222bbb222bbb222",
			"aaa111aaa111aaa111aaa111\tjdoe",
			"bbb222bbb222bbb222bbb222\tadmin",
		} {
			if !found[want] {
				t.Errorf("missing completion: %q", want)
			}
		}
	})

	t.Run("no completions when arg already provided", func(t *testing.T) {
		completions, directive := fn(&cobra.Command{}, []string{"existing-arg"}, "")
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("expected ShellCompDirectiveNoFileComp, got %d", directive)
		}
		if len(completions) != 0 {
			t.Errorf("expected 0 completions, got %d", len(completions))
		}
	})

	t.Run("no completions when cache missing", func(t *testing.T) {
		noExist := resolve.ResourceConfig{CacheKey: "nonexistent"}
		fn2 := completeResourceNames(noExist)
		completions, directive := fn2(&cobra.Command{}, nil, "")
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("expected ShellCompDirectiveNoFileComp, got %d", directive)
		}
		if len(completions) != 0 {
			t.Errorf("expected 0 completions, got %d", len(completions))
		}
	})

	t.Run("no completions when cache is empty", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(cacheDir, "empty.json"), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		emptyConfig := resolve.ResourceConfig{CacheKey: "empty"}
		fn3 := completeResourceNames(emptyConfig)
		completions, directive := fn3(&cobra.Command{}, nil, "")
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("expected ShellCompDirectiveNoFileComp, got %d", directive)
		}
		if len(completions) != 0 {
			t.Errorf("expected 0 completions, got %d", len(completions))
		}
	})
}
