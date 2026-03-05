# Dynamic Shell Completions Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add tab-completion for resource name/ID arguments across ~95 commands using the existing resolver cache.

**Architecture:** A single shared `completeResourceNames()` function reads the resolver cache file and returns both names and IDs as Cobra completion candidates. Each resource command adds a one-liner `ValidArgsFunction`. A new `ReadCacheEntries()` export on the resolve package enables cache reading without a client.

**Tech Stack:** Go, Cobra (ValidArgsFunction), existing resolve cache infrastructure.

---

### Task 1: Export Cache Reading from Resolve Package

**Files:**
- Modify: `internal/resolve/resolve.go:25-32` — export `CacheEntry` and add `ReadCacheEntries()`
- Create: `internal/cmd/completions.go` — shared completion function
- Create: `internal/cmd/completions_test.go` — tests

**Step 1: Export the cache entry type and add ReadCacheEntries**

In `internal/resolve/resolve.go`, the private `cacheEntry` type (line 26) needs to be exported, and a standalone `ReadCacheEntries()` function added.

Change the existing code at line 25-32:

```go
// cacheEntry holds a cached name→ID mapping with a timestamp.
type cacheEntry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
}

// cacheFile represents the on-disk cache for a single resource type.
type cacheFile map[string]cacheEntry
```

To:

```go
// CacheEntry holds a cached name→ID mapping with a timestamp.
type CacheEntry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
}

// cacheFile represents the on-disk cache for a single resource type.
type cacheFile map[string]CacheEntry

// ReadCacheEntries reads the resolver cache file for a given resource type
// and returns all cached name→ID mappings. Returns nil if the cache file
// doesn't exist, is empty, or can't be parsed. This function does not
// require a Resolver or API client — it only reads the local cache.
func ReadCacheEntries(cacheKey string) map[string]CacheEntry {
	path := filepath.Join(cacheDir(), cacheKey+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries map[string]CacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}
	return entries
}
```

**Important:** The `readCacheFile` method on `Resolver` (line 663) returns `cacheFile` which is now `map[string]CacheEntry`. The `writeCacheFile` method (line 677) also uses `cacheFile`. Both are internal and will work with the renamed type. Search the file for all references to `cacheEntry` and update them to `CacheEntry`.

**Step 2: Run tests to verify nothing broke**

Run: `go test ./internal/resolve/ -count=1 -v`
Expected: All existing tests PASS (the type rename is backwards-compatible for JSON marshaling).

**Step 3: Create `internal/cmd/completions.go`**

```go
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
```

**Step 4: Create `internal/cmd/completions_test.go`**

```go
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

	// Create cache directory and file
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
		// Verify both names and IDs appear
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
```

**Step 5: Run the completions tests**

Run: `go test ./internal/cmd/ -run TestCompleteResourceNames -count=1 -v`
Expected: All 4 subtests PASS.

**Step 6: Commit**

```bash
git add internal/resolve/resolve.go internal/cmd/completions.go internal/cmd/completions_test.go
git commit -m "feat: add cache-based shell completion infrastructure"
```

---

### Task 2: Add Completions to Users Commands

**Files:**
- Modify: `internal/cmd/users.go`

Add `ValidArgsFunction: completeResourceNames(resolve.UserConfig),` to these commands (insert after the `Args:` line in each):

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 226 |
| update | after line 353 |
| delete | after line 444 |
| lock | after line 557 |
| unlock | after line 570 |
| reset-mfa | after line 642 (note: 642 is corrected, was listed as 643) |
| reset-password | after line 688 |
| ssh-keys | after line 741 |
| ssh-key-add | after line 791 |
| ssh-key-delete | after line 853 |

**Do NOT add to:** `search` (line 159) — takes free-text keyword, not a name/ID.

**Step 1: Add ValidArgsFunction to all 10 commands**

For each command struct, add this line immediately after the `Args:` line:

```go
		ValidArgsFunction: completeResourceNames(resolve.UserConfig),
```

**Step 2: Run tests**

Run: `go test ./internal/cmd/ -run TestUsers -count=1 -v`
Expected: All existing user tests PASS (adding ValidArgsFunction doesn't affect RunE behavior).

**Step 3: Commit**

```bash
git add internal/cmd/users.go
git commit -m "feat: add tab completions to users commands"
```

---

### Task 3: Add Completions to Devices Commands

**Files:**
- Modify: `internal/cmd/devices.go`

Add `ValidArgsFunction: completeResourceNames(resolve.DeviceConfig),` to:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 128 |
| delete | after line 175 |
| update | after line 304 |
| lock | after line 440 |
| restart | after line 453 |
| erase | after line 470 |
| fde-key | after line 556 |

**Do NOT add to:** `search` (line 389) — takes free-text keyword.

**Step 1: Add ValidArgsFunction to all 7 commands**

**Step 2: Run tests**

Run: `go test ./internal/cmd/ -run TestDevices -count=1 -v`
Expected: PASS.

**Step 3: Commit**

```bash
git add internal/cmd/devices.go
git commit -m "feat: add tab completions to devices commands"
```

---

### Task 4: Add Completions to Groups Commands

**Files:**
- Modify: `internal/cmd/groups.go`

Two resolvers — `UserGroupConfig` for user group commands, `DeviceGroupConfig` for device group commands.

| Command | Config | Line (after Args) |
|---------|--------|-------------------|
| user get | UserGroupConfig | after line 148 |
| user update | UserGroupConfig | after line 252 |
| user delete | UserGroupConfig | after line 327 |
| device get | DeviceGroupConfig | after line 525 |
| device update | DeviceGroupConfig | after line 629 |
| device delete | DeviceGroupConfig | after line 704 |
| add-member | UserGroupConfig | after line 830 |
| remove-member | UserGroupConfig | after line 973 |

**Note on add-member/remove-member:** The positional arg can be either a user group or device group name depending on flags. Use `UserGroupConfig` as the default — user groups are more common, and the group name will still match in either cache.

**Step 1: Add ValidArgsFunction to all 8 commands**

**Step 2: Run tests**

Run: `go test ./internal/cmd/ -run TestGroup -count=1 -v`
Expected: PASS.

**Step 3: Commit**

```bash
git add internal/cmd/groups.go
git commit -m "feat: add tab completions to groups commands"
```

---

### Task 5: Add Completions to Commands, Policies, Apps

**Files:**
- Modify: `internal/cmd/commands.go`
- Modify: `internal/cmd/policies.go`
- Modify: `internal/cmd/apps.go`

**commands.go** — `resolve.CommandConfig`:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 122 |
| update | after line 217 |
| delete | after line 276 |
| run | after line 352 |
| results | after line 468 |

**Do NOT add to:** `trigger` (line 533) — takes raw trigger name string, no resolver.

**policies.go** — `resolve.PolicyConfig`:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 121 |
| results | after line 167 |
| update | after line 297 |
| delete | after line 371 |

**apps.go** — `resolve.ApplicationConfig`:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 120 |
| update | after line 265 |
| delete | after line 343 |

**Step 1: Add ValidArgsFunction to all 12 commands across 3 files**

**Step 2: Run tests**

Run: `go test ./internal/cmd/ -run 'TestCommands|TestPolicies|TestApps' -count=1 -v`
Expected: PASS.

**Step 3: Commit**

```bash
git add internal/cmd/commands.go internal/cmd/policies.go internal/cmd/apps.go
git commit -m "feat: add tab completions to commands, policies, apps"
```

---

### Task 6: Add Completions to Auth Policies, IP Lists, Software

**Files:**
- Modify: `internal/cmd/auth_policies.go`
- Modify: `internal/cmd/iplists.go`
- Modify: `internal/cmd/software.go`

**auth_policies.go** — `resolve.AuthPolicyConfig`:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 122 |
| update | after line 269 |
| delete | after line 355 |
| enable | after line 428 |
| disable | after line 443 |
| simulate | after line 512 |
| blast-radius | after line 751 |

**iplists.go** — `resolve.IPListConfig`:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 116 |
| update | after line 228 |
| delete | after line 302 |

**software.go** — `resolve.SoftwareAppConfig`:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 119 |
| update | after line 227 |
| delete | after line 300 |
| statuses | after line 381 |
| associations | after line 428 |
| reclaim-license | after line 476 |

**Step 1: Add ValidArgsFunction to all 16 commands across 3 files**

**Step 2: Run tests**

Run: `go test ./internal/cmd/ -run 'TestAuthPolicies|TestIPLists|TestSoftware' -count=1 -v`
Expected: PASS.

**Step 3: Commit**

```bash
git add internal/cmd/auth_policies.go internal/cmd/iplists.go internal/cmd/software.go
git commit -m "feat: add tab completions to auth-policies, iplists, software"
```

---

### Task 7: Add Completions to LDAP, RADIUS, AD

**Files:**
- Modify: `internal/cmd/ldap.go`
- Modify: `internal/cmd/radius.go`
- Modify: `internal/cmd/ad.go`

**ldap.go** — `resolve.LDAPServerConfig`:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 121 |
| update | after line 234 |
| delete | after line 308 |
| samba-domains | after line 389 |
| samba-domain-get | after line 437 |
| samba-domain-create | after line 482 |
| samba-domain-update | after line 546 |
| samba-domain-delete | after line 618 |

Note: All samba-domain subcommands take the LDAP server name/ID as the first positional arg, so `LDAPServerConfig` is correct.

**radius.go** — `resolve.RADIUSServerConfig`:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 114 |
| update | after line 223 |
| delete | after line 301 |

**ad.go** — `resolve.ActiveDirectoryConfig`:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 116 |
| update | after line 220 |
| delete | after line 290 |

**Step 1: Add ValidArgsFunction to all 14 commands across 3 files**

**Step 2: Run tests**

Run: `go test ./internal/cmd/ -run 'TestLDAP|TestRADIUS|TestAD' -count=1 -v`
Expected: PASS.

**Step 3: Commit**

```bash
git add internal/cmd/ldap.go internal/cmd/radius.go internal/cmd/ad.go
git commit -m "feat: add tab completions to ldap, radius, ad"
```

---

### Task 8: Add Completions to Cloud Directory Resources

**Files:**
- Modify: `internal/cmd/gsuite.go`
- Modify: `internal/cmd/office365.go`
- Modify: `internal/cmd/duo.go`
- Modify: `internal/cmd/apple_mdm.go`

**gsuite.go** — `resolve.GsuiteConfig`:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 117 |
| translation-rules | after line 157 |
| import-users | after line 205 |

**office365.go** — `resolve.Office365Config`:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 117 |
| translation-rules | after line 153 |
| import-users | after line 201 |

**duo.go** — `resolve.DuoAccountConfig`:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 105 |
| delete | after line 195 |
| apps | after line 272 |
| app-get | after line 324 |
| app-create | after line 369 |
| app-delete | after line 434 |

**apple_mdm.go** — `resolve.AppleMDMConfig`:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 95 |
| update | after line 197 |
| delete | after line 266 |
| enrollment-profiles | after line 334 |
| devices | after line 375 |

**Step 1: Add ValidArgsFunction to all 17 commands across 4 files**

**Step 2: Run tests**

Run: `go test ./internal/cmd/ -run 'TestGsuite|TestOffice365|TestDuo|TestAppleMDM' -count=1 -v`
Expected: PASS.

**Step 3: Commit**

```bash
git add internal/cmd/gsuite.go internal/cmd/office365.go internal/cmd/duo.go internal/cmd/apple_mdm.go
git commit -m "feat: add tab completions to gsuite, office365, duo, apple-mdm"
```

---

### Task 9: Add Completions to Remaining Resources

**Files:**
- Modify: `internal/cmd/policy_groups.go`
- Modify: `internal/cmd/identity_providers.go`
- Modify: `internal/cmd/saas_management.go`

**policy_groups.go** — `resolve.PolicyGroupConfig`:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 113 |
| update | after line 216 |
| delete | after line 286 |

**identity_providers.go** — `resolve.IdentityProviderConfig`:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 122 |
| update | after line 224 |
| delete | after line 308 |

**saas_management.go** — `resolve.SaaSManagementConfig`:

| Command | Line (after Args) |
|---------|-------------------|
| get | after line 125 |
| update | after line 234 |
| delete | after line 304 |
| accounts | after line 385 |
| account-get | after line 437 |
| account-delete | after line 479 |
| usage | after line 544 |

**Do NOT add to:** `catalog-get` (line 639) — takes raw catalog app ID, no resolver.

**Step 1: Add ValidArgsFunction to all 13 commands across 3 files**

**Step 2: Run tests**

Run: `go test ./internal/cmd/ -run 'TestPolicyGroups|TestIdentityProviders|TestSaaSManagement' -count=1 -v`
Expected: PASS.

**Step 3: Commit**

```bash
git add internal/cmd/policy_groups.go internal/cmd/identity_providers.go internal/cmd/saas_management.go
git commit -m "feat: add tab completions to policy-groups, identity-providers, saas-management"
```

---

### Task 10: Run Full Test Suite and Update Progress

**Step 1: Run the full test suite**

Run: `go test ./... -count=1`
Expected: ALL tests PASS. No new test count changes needed — no new MCP tools, schema resources, or TUI entries.

**Step 2: Verify completion works manually**

Run: `go build -o ./jc . && ./jc __complete users get -- ""`
Expected: Output should list cached user names and IDs (one per line with `\t` separator), followed by `:0` (ShellCompDirectiveDefault) on the last line. If no cache exists, only `:4` (ShellCompDirectiveNoFileComp).

**Step 3: Update progress.md**

Append to the end of the first paragraph in progress.md:

```
Dynamic shell completions: tab-completion for resource name/ID arguments across ~95 commands using resolver cache. Zero-latency, offline-capable. 20 resources with completions via shared `completeResourceNames()` function.
```

**Step 4: Commit**

```bash
git add progress.md
git commit -m "docs: update progress with dynamic shell completions"
```
