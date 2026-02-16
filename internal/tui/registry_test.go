package tui

import (
	"testing"

	"github.com/klaassen-consulting/jc/internal/schema"
)

func TestBuildRegistry_AllListableResourcesMapped(t *testing.T) {
	entries := BuildRegistry()
	entryKeys := make(map[string]bool)
	for _, e := range entries {
		entryKeys[e.Key] = true
	}

	for name := range schema.Resources {
		if skipInTUI[name] {
			continue
		}
		if !entryKeys[name] {
			t.Errorf("schema resource %q not mapped in TUI registry", name)
		}
	}
}

func TestBuildRegistry_SkipsNonListable(t *testing.T) {
	entries := BuildRegistry()
	entryKeys := make(map[string]bool)
	for _, e := range entries {
		entryKeys[e.Key] = true
	}

	for name := range skipInTUI {
		if entryKeys[name] {
			t.Errorf("resource %q should be skipped in TUI", name)
		}
	}
}

func TestBuildRegistry_Count(t *testing.T) {
	entries := BuildRegistry()
	want := len(schema.Resources) - len(skipInTUI)
	if len(entries) != want {
		t.Errorf("registry has %d entries, want %d", len(entries), want)
	}
}

func TestBuildRegistry_AllHaveCategory(t *testing.T) {
	entries := BuildRegistry()
	for _, e := range entries {
		if e.Category == "" {
			t.Errorf("resource %q has no category", e.Key)
		}
	}
}

func TestBuildRegistry_AllHaveDisplayName(t *testing.T) {
	entries := BuildRegistry()
	for _, e := range entries {
		if e.DisplayName == "" {
			t.Errorf("resource %q has no display name", e.Key)
		}
	}
}

func TestBuildRegistry_SortedByCategoryThenName(t *testing.T) {
	entries := BuildRegistry()
	for i := 1; i < len(entries); i++ {
		prevCatIdx := categoryIndex(entries[i-1].Category)
		currCatIdx := categoryIndex(entries[i].Category)

		if prevCatIdx > currCatIdx {
			t.Errorf("resource %q (cat %s) should come before %q (cat %s)",
				entries[i].Key, entries[i].Category,
				entries[i-1].Key, entries[i-1].Category)
		}
		if prevCatIdx == currCatIdx && entries[i-1].DisplayName > entries[i].DisplayName {
			t.Errorf("resource %q should come before %q in category %s",
				entries[i].Key, entries[i-1].Key, entries[i].Category)
		}
	}
}

func TestRegistryByKey(t *testing.T) {
	m := RegistryByKey()
	want := len(schema.Resources) - len(skipInTUI)
	if len(m) != want {
		t.Errorf("RegistryByKey has %d entries, want %d", len(m), want)
	}

	entry, ok := m["users"]
	if !ok {
		t.Fatal("missing 'users' entry")
	}
	if entry.DisplayName != "Users" {
		t.Errorf("users display name = %q, want 'Users'", entry.DisplayName)
	}
	if entry.ClientType != ClientV1 {
		t.Errorf("users client type = %d, want ClientV1 (%d)", entry.ClientType, ClientV1)
	}
}

func TestClientTypeMapping(t *testing.T) {
	m := RegistryByKey()

	tests := []struct {
		key  string
		want ClientType
	}{
		{"users", ClientV1},
		{"devices", ClientV1},
		{"commands", ClientV1},
		{"apps", ClientV1},
		{"admins", ClientV1},
		{"policies", ClientV2},
		{"groups", ClientV2},
		{"auth-policies", ClientV2},
	}

	for _, tt := range tests {
		e, ok := m[tt.key]
		if !ok {
			t.Errorf("missing resource %q", tt.key)
			continue
		}
		if e.ClientType != tt.want {
			t.Errorf("%s client type = %d, want %d", tt.key, e.ClientType, tt.want)
		}
	}
}

func TestAdminsEndpointOverride(t *testing.T) {
	m := RegistryByKey()
	e, ok := m["admins"]
	if !ok {
		t.Fatal("missing 'admins' entry")
	}
	if e.ListEndpoint != "/users" {
		t.Errorf("admins endpoint = %q, want '/users'", e.ListEndpoint)
	}
	if e.ClientType != ClientV1 {
		t.Errorf("admins client type = %d, want ClientV1", e.ClientType)
	}
}
