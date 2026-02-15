package schema

import (
	"testing"
)

func TestResourceNames_Sorted(t *testing.T) {
	names := ResourceNames()
	if len(names) != 23 {
		t.Fatalf("expected 23 resources, got %d", len(names))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Errorf("resources not sorted: %s >= %s", names[i-1], names[i])
		}
	}
}

func TestResourceNames_ContainsAllExpected(t *testing.T) {
	expected := []string{"ad", "admins", "apple-mdm", "apps", "auth-policies", "commands", "devices", "duo", "groups", "gsuite", "insights", "iplists", "ldap", "office365", "org", "policies", "policy-groups", "policy-templates", "radius", "software", "system-insights", "user-states", "users"}
	names := ResourceNames()
	if len(names) != len(expected) {
		t.Fatalf("expected %d resources, got %d", len(expected), len(names))
	}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("resource[%d] = %s, want %s", i, name, expected[i])
		}
	}
}

func TestGetResource_ValidResource(t *testing.T) {
	s := GetResource("users")
	if s == nil {
		t.Fatal("expected users schema, got nil")
	}
	if s.Resource != "users" {
		t.Errorf("resource = %s, want users", s.Resource)
	}
	if s.APIVersion != "v1" {
		t.Errorf("api_version = %s, want v1", s.APIVersion)
	}
}

func TestGetResource_InvalidResource(t *testing.T) {
	s := GetResource("nonexistent")
	if s != nil {
		t.Errorf("expected nil for nonexistent resource, got %v", s)
	}
}

func TestGetResource_HasFields(t *testing.T) {
	for _, name := range ResourceNames() {
		s := GetResource(name)
		if s == nil {
			t.Fatalf("resource %s is nil", name)
		}
		if len(s.Fields) == 0 {
			t.Errorf("resource %s has no fields defined", name)
		}
	}
}

func TestGetResource_FieldTypes(t *testing.T) {
	validTypes := map[string]bool{
		"string": true, "bool": true, "int": true,
		"datetime": true, "array": true, "object": true,
	}
	for _, name := range ResourceNames() {
		s := GetResource(name)
		for _, f := range s.Fields {
			if !validTypes[f.Type] {
				t.Errorf("resource %s field %s has invalid type %q", name, f.Name, f.Type)
			}
		}
	}
}

func TestGetResource_FieldsHaveDescriptions(t *testing.T) {
	for _, name := range ResourceNames() {
		s := GetResource(name)
		for _, f := range s.Fields {
			if f.Name == "" {
				t.Errorf("resource %s has field with empty name", name)
			}
			if f.Description == "" {
				t.Errorf("resource %s field %s has empty description", name, f.Name)
			}
		}
	}
}

func TestGetResource_UsersHasRequiredFields(t *testing.T) {
	s := GetResource("users")
	required := map[string]bool{}
	for _, f := range s.Fields {
		if f.Required {
			required[f.Name] = true
		}
	}
	if !required["username"] {
		t.Error("expected username to be required")
	}
	if !required["email"] {
		t.Error("expected email to be required")
	}
}

func TestGetResource_DefaultFieldsSubsetOfFields(t *testing.T) {
	for _, name := range ResourceNames() {
		s := GetResource(name)
		fieldNames := map[string]bool{}
		for _, f := range s.Fields {
			fieldNames[f.Name] = true
		}
		for _, df := range s.DefaultFields {
			if !fieldNames[df] {
				t.Errorf("resource %s default field %q not in field definitions", name, df)
			}
		}
	}
}

func TestGetResource_AllHaveVerbsAndVersion(t *testing.T) {
	for _, name := range ResourceNames() {
		s := GetResource(name)
		if len(s.Verbs) == 0 {
			t.Errorf("resource %s has no verbs", name)
		}
		if s.APIVersion == "" {
			t.Errorf("resource %s has no api_version", name)
		}
	}
}

func TestGetResource_SortFields(t *testing.T) {
	for _, name := range ResourceNames() {
		s := GetResource(name)
		if s.SortSupport && len(s.SortFields) == 0 {
			t.Errorf("resource %s has sort_support=true but no sort_fields", name)
		}
	}
}

func TestAllResources_Count(t *testing.T) {
	all := AllResources()
	if len(all) != 23 {
		t.Fatalf("expected 23 resources, got %d", len(all))
	}
}

func TestAllResources_Sorted(t *testing.T) {
	all := AllResources()
	for i := 1; i < len(all); i++ {
		if all[i-1].Resource >= all[i].Resource {
			t.Errorf("resources not sorted: %s >= %s", all[i-1].Resource, all[i].Resource)
		}
	}
}

func TestBuildCommandManifest_Structure(t *testing.T) {
	m := BuildCommandManifest()

	if m.Name != "jc" {
		t.Errorf("name = %s, want jc", m.Name)
	}
	if m.Description == "" {
		t.Error("expected non-empty description")
	}
	if len(m.GlobalFlags) == 0 {
		t.Error("expected global flags")
	}
	if len(m.Commands) == 0 {
		t.Error("expected commands")
	}
	if len(m.Resources) != 23 {
		t.Errorf("expected 23 resources, got %d", len(m.Resources))
	}
}

func TestBuildCommandManifest_GlobalFlags(t *testing.T) {
	m := BuildCommandManifest()

	flagNames := map[string]bool{}
	for _, f := range m.GlobalFlags {
		flagNames[f.Name] = true
	}

	for _, expected := range []string{"output", "verbose", "quiet", "force", "plan", "ids", "fields", "org"} {
		if !flagNames[expected] {
			t.Errorf("expected global flag %q", expected)
		}
	}
}

func TestBuildCommandManifest_IncludesSchemaCommand(t *testing.T) {
	m := BuildCommandManifest()

	found := false
	for _, cmd := range m.Commands {
		if cmd.Path == "jc schema" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected command manifest to include jc schema command")
	}
}

func TestBuildCommandManifest_CommandsHaveDescriptions(t *testing.T) {
	m := BuildCommandManifest()

	for _, cmd := range m.Commands {
		if cmd.Description == "" {
			t.Errorf("command %s has empty description", cmd.Path)
		}
		if cmd.Path == "" {
			t.Error("found command with empty path")
		}
	}
}

func TestBuildCommandManifest_FlagTypes(t *testing.T) {
	m := BuildCommandManifest()

	validTypes := map[string]bool{
		"string": true, "bool": true, "int": true, "string[]": true,
	}

	for _, f := range m.GlobalFlags {
		if !validTypes[f.Type] {
			t.Errorf("global flag %s has invalid type %q", f.Name, f.Type)
		}
	}
	for _, cmd := range m.Commands {
		for _, f := range cmd.Flags {
			if !validTypes[f.Type] {
				t.Errorf("command %s flag %s has invalid type %q", cmd.Path, f.Name, f.Type)
			}
		}
	}
}
