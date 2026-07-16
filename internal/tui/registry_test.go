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
		// Also index sub-menu children.
		for _, child := range e.SubMenu {
			entryKeys[child.Key] = true
		}
	}

	for name := range schema.Resources {
		if skipInTUI[name] {
			continue
		}
		// "groups" is split into "user-groups" and "device-groups".
		if name == "groups" {
			if !entryKeys["user-groups"] {
				t.Error("schema resource 'groups' not mapped as 'user-groups' in TUI registry")
			}
			if !entryKeys["device-groups"] {
				t.Error("schema resource 'groups' not mapped as 'device-groups' in TUI registry")
			}
			continue
		}
		// "assets" is split into sub-menu children.
		if name == "assets" {
			if !entryKeys["device-assets"] {
				t.Error("schema resource 'assets' not mapped as 'device-assets' in TUI registry")
			}
			if !entryKeys["accessory-assets"] {
				t.Error("schema resource 'assets' not mapped as 'accessory-assets' in TUI registry")
			}
			if !entryKeys["location-assets"] {
				t.Error("schema resource 'assets' not mapped as 'location-assets' in TUI registry")
			}
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
	// "groups" splits into 2 entries (+1), gsuite/office365 fold into cloud-directories (-2+1),
	// "recipes" adds a virtual workflow entry (+1),
	// "apple-mdm-payloads" adds a virtual device-management entry (+1),
	// "apple-mdm-custom-policies" adds a second virtual device-management entry (+1),
	// "windows-mdm-csp" + "windows-mdm-registry" add two more virtual
	// device-management entries (+2, KLA-462), and
	// "windows-mdm-custom-policies" a third (+1, KLA-464), and
	// "bundles" a security entry (+1, KLA-477), and
	// "directories" a user-mgmt entry (+1, KLA-479), and
	// "password-policies" a security entry (+1, KLA-480), and
	// "patch-management" a device-mgmt entry (+1, KLA-481),
	// plus len(placeholderEntries) placeholders.
	want := len(schema.Resources) - len(skipInTUI) - len(cloudDirResources) + 1 + 1 + 1 + 1 + 1 + 2 + 1 + 1 + 1 + 1 + 1 + len(placeholderEntries)
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
	// "groups" splits into 2 entries (+1), gsuite/office365 fold into cloud-directories (-2+1),
	// "recipes" adds a virtual workflow entry (+1),
	// "apple-mdm-payloads" adds a virtual device-management entry (+1),
	// "windows-mdm-csp" + "windows-mdm-registry" add two more virtual
	// device-management entries (+2, KLA-462), and
	// "windows-mdm-custom-policies" a third (+1, KLA-464), and
	// "bundles" a security entry (+1, KLA-477), and
	// "directories" a user-mgmt entry (+1, KLA-479), and
	// "password-policies" a security entry (+1, KLA-480), and
	// "patch-management" a device-mgmt entry (+1, KLA-481),
	// plus len(placeholderEntries) placeholders.
	want := len(schema.Resources) - len(skipInTUI) - len(cloudDirResources) + 1 + 1 + 1 + 1 + 1 + 2 + 1 + 1 + 1 + 1 + 1 + len(placeholderEntries)
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
		{"user-groups", ClientV2},
		{"device-groups", ClientV2},
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

func TestBuildRegistry_GroupsSplit(t *testing.T) {
	m := RegistryByKey()

	ug, ok := m["user-groups"]
	if !ok {
		t.Fatal("missing 'user-groups' entry")
	}
	if ug.DisplayName != "User Groups" {
		t.Errorf("user-groups display name = %q, want 'User Groups'", ug.DisplayName)
	}
	if ug.ListEndpoint != "/usergroups" {
		t.Errorf("user-groups endpoint = %q, want '/usergroups'", ug.ListEndpoint)
	}
	if ug.Category != CategoryUserMgmt {
		t.Errorf("user-groups category = %q, want CategoryUserMgmt", ug.Category)
	}

	dg, ok := m["device-groups"]
	if !ok {
		t.Fatal("missing 'device-groups' entry")
	}
	if dg.DisplayName != "Device Groups" {
		t.Errorf("device-groups display name = %q, want 'Device Groups'", dg.DisplayName)
	}
	if dg.ListEndpoint != "/systemgroups" {
		t.Errorf("device-groups endpoint = %q, want '/systemgroups'", dg.ListEndpoint)
	}
	if dg.Category != CategoryDeviceMgmt {
		t.Errorf("device-groups category = %q, want CategoryDeviceMgmt", dg.Category)
	}

	// Original "groups" key should not exist.
	if _, ok := m["groups"]; ok {
		t.Error("'groups' key should not exist — it should be split into user-groups and device-groups")
	}
}

func TestBuildRegistry_SystemInsightsPivot(t *testing.T) {
	m := RegistryByKey()
	si, ok := m["system-insights"]
	if !ok {
		t.Fatal("missing 'system-insights' entry")
	}
	if si.PivotField != "system_id" {
		t.Errorf("system-insights PivotField = %q, want 'system_id'", si.PivotField)
	}
	if si.PivotTargetKey != "devices" {
		t.Errorf("system-insights PivotTargetKey = %q, want 'devices'", si.PivotTargetKey)
	}
}

func TestBuildRegistry_NoPivotByDefault(t *testing.T) {
	m := RegistryByKey()
	for key, entry := range m {
		if key == "system-insights" {
			continue
		}
		if entry.PivotField != "" {
			t.Errorf("resource %q has unexpected PivotField = %q", key, entry.PivotField)
		}
		if entry.PivotTargetKey != "" {
			t.Errorf("resource %q has unexpected PivotTargetKey = %q", key, entry.PivotTargetKey)
		}
	}
}

func TestRegistryKeyForGraphType(t *testing.T) {
	tests := []struct {
		graphType string
		wantKey   string
	}{
		{"user", "users"},
		{"system", "devices"},
		{"user_group", "user-groups"},
		{"system_group", "device-groups"},
		{"application", "apps"},
		{"command", "commands"},
		{"policy", "policies"},
		{"radius_server", "radius"},
		{"ldap_server", "ldap"},
		{"policy_group", "policy-groups"},
		{"software_app", "software"},
	}
	for _, tt := range tests {
		got := RegistryKeyForGraphType(tt.graphType)
		if got != tt.wantKey {
			t.Errorf("RegistryKeyForGraphType(%q) = %q, want %q", tt.graphType, got, tt.wantKey)
		}
	}
}

func TestRegistryKeyForGraphType_Unknown(t *testing.T) {
	got := RegistryKeyForGraphType("nonexistent")
	if got != "" {
		t.Errorf("RegistryKeyForGraphType(nonexistent) = %q, want empty", got)
	}
}

func TestBuildRegistry_SearchEndpoints(t *testing.T) {
	m := RegistryByKey()

	tests := []struct {
		key            string
		wantEndpoint   string
		wantFieldCount int
	}{
		{"users", "/search/systemusers", 4},
		{"devices", "/search/systems", 3},
		{"policies", "", 0},
		{"commands", "", 0},
	}

	for _, tt := range tests {
		e, ok := m[tt.key]
		if !ok {
			t.Errorf("missing resource %q", tt.key)
			continue
		}
		if e.SearchEndpoint != tt.wantEndpoint {
			t.Errorf("%s SearchEndpoint = %q, want %q", tt.key, e.SearchEndpoint, tt.wantEndpoint)
		}
		if len(e.SearchFields) != tt.wantFieldCount {
			t.Errorf("%s SearchFields count = %d, want %d", tt.key, len(e.SearchFields), tt.wantFieldCount)
		}
	}
}

func TestMembershipTarget(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{"user_group", "user"},
		{"device_group", "system"},
		{"user", ""},
		{"device", ""},
		{"application", ""},
	}
	for _, tt := range tests {
		got := MembershipTarget(tt.source)
		if got != tt.want {
			t.Errorf("MembershipTarget(%q) = %q, want %q", tt.source, got, tt.want)
		}
	}
}

func TestMembershipEndpoint(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{"user_group", "/usergroups"},
		{"device_group", "/systemgroups"},
		{"user", ""},
		{"device", ""},
	}
	for _, tt := range tests {
		got := MembershipEndpoint(tt.source)
		if got != tt.want {
			t.Errorf("MembershipEndpoint(%q) = %q, want %q", tt.source, got, tt.want)
		}
	}
}

func TestMemberOfTarget(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{"user", "user_group"},
		{"device", "system_group"},
		{"user_group", ""},
		{"device_group", ""},
		{"application", ""},
	}
	for _, tt := range tests {
		got := MemberOfTarget(tt.source)
		if got != tt.want {
			t.Errorf("MemberOfTarget(%q) = %q, want %q", tt.source, got, tt.want)
		}
	}
}

func TestResourceEntry_PlaceholderField(t *testing.T) {
	e := ResourceEntry{
		Key:         "test",
		DisplayName: "Test",
		Placeholder: true,
	}
	if !e.Placeholder {
		t.Error("Placeholder field should be true")
	}
}

func TestResourceEntry_SubMenuField(t *testing.T) {
	e := ResourceEntry{
		Key:         "cloud-dirs",
		DisplayName: "Cloud Directories",
		SubMenu: []ResourceEntry{
			{Key: "gsuite", DisplayName: "Google Workspace"},
			{Key: "office365", DisplayName: "M365"},
		},
	}
	if len(e.SubMenu) != 2 {
		t.Errorf("SubMenu length = %d, want 2", len(e.SubMenu))
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

func TestBuildRegistry_PlaceholderEntries(t *testing.T) {
	entries := BuildRegistry()
	placeholders := make(map[string]bool)
	for _, e := range entries {
		if e.Placeholder {
			placeholders[e.Key] = true
		}
	}

	// hr-directories graduated to the real "directories" entry
	// (KLA-479); the rest await public API surface (or their own
	// tickets: KLA-480/481/482).
	want := []string{
		"vault",
		"mfa-configurations", "device-trust",
	}
	for _, k := range want {
		if !placeholders[k] {
			t.Errorf("missing placeholder %q", k)
		}
	}
}

func TestBuildRegistry_CloudDirectoriesSubMenu(t *testing.T) {
	entries := BuildRegistry()
	var found bool
	for _, e := range entries {
		if e.Key == "cloud-directories" {
			found = true
			if len(e.SubMenu) != 2 {
				t.Errorf("cloud-directories SubMenu length = %d, want 2", len(e.SubMenu))
			}
			if e.SubMenu[0].Key != "gsuite" {
				t.Errorf("SubMenu[0].Key = %q, want 'gsuite'", e.SubMenu[0].Key)
			}
			if e.SubMenu[1].Key != "office365" {
				t.Errorf("SubMenu[1].Key = %q, want 'office365'", e.SubMenu[1].Key)
			}
			break
		}
	}
	if !found {
		t.Error("missing 'cloud-directories' entry")
	}
}

func TestBuildRegistry_GsuiteOffice365NotTopLevel(t *testing.T) {
	entries := BuildRegistry()
	for _, e := range entries {
		if e.Key == "gsuite" || e.Key == "office365" {
			t.Errorf("resource %q should not be a top-level entry (it's inside Cloud Directories sub-menu)", e.Key)
		}
	}
}

func TestColumnAssignment(t *testing.T) {
	for _, cat := range CategoryOrder {
		col := CategoryColumn(cat)
		if col < 0 || col > 2 {
			t.Errorf("category %q has column %d, want 0-2", cat, col)
		}
	}

	// Verify specific assignments from design:
	if CategoryColumn(CategoryUserMgmt) != 0 {
		t.Error("User Management should be in column 0")
	}
	if CategoryColumn(CategorySecurity) != 0 {
		t.Error("Security should be in column 0")
	}
	if CategoryColumn(CategoryDeviceMgmt) != 1 {
		t.Error("Device Management should be in column 1")
	}
	if CategoryColumn(CategorySettings) != 1 {
		t.Error("Settings should be in column 1")
	}
	if CategoryColumn(CategoryAccess) != 2 {
		t.Error("Access should be in column 2")
	}
	if CategoryColumn(CategoryInsights) != 2 {
		t.Error("Insights should be in column 2")
	}
}

func TestBuildRegistry_NewCategories(t *testing.T) {
	entries := BuildRegistry()
	cats := make(map[Category]bool)
	for _, e := range entries {
		cats[e.Category] = true
	}
	want := []Category{
		CategoryUserMgmt,
		CategoryDeviceMgmt,
		CategoryAccess,
		CategorySecurity,
		CategoryInsights,
		CategorySettings,
	}
	for _, c := range want {
		if !cats[c] {
			t.Errorf("missing category %q", c)
		}
	}
}
