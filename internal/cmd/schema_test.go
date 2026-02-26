package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/klaassen-consulting/jc/internal/schema"
)

// runSchemaCmd executes a schema command and returns stdout output and error.
func runSchemaCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return out.String(), err
}

func TestSchemaResources_JSON(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "resources")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resources []schema.ResourceSchema
	if err := json.Unmarshal([]byte(output), &resources); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(resources) != 29 {
		t.Fatalf("expected 29 resources, got %d", len(resources))
	}
}

func TestSchemaResources_Sorted(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "resources")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resources []schema.ResourceSchema
	json.Unmarshal([]byte(output), &resources)

	for i := 1; i < len(resources); i++ {
		if resources[i-1].Resource >= resources[i].Resource {
			t.Errorf("resources not sorted: %s >= %s", resources[i-1].Resource, resources[i].Resource)
		}
	}
}

func TestSchemaResources_IncludesFields(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "resources")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify field definitions are included.
	if !strings.Contains(output, "\"fields\"") {
		t.Error("expected resources output to include fields")
	}
	if !strings.Contains(output, "\"type\"") {
		t.Error("expected resources output to include field types")
	}
}

func TestSchemaUsers_JSON(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rs schema.ResourceSchema
	if err := json.Unmarshal([]byte(output), &rs); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if rs.Resource != "users" {
		t.Errorf("resource = %s, want users", rs.Resource)
	}
	if rs.APIVersion != "v1" {
		t.Errorf("api_version = %s, want v1", rs.APIVersion)
	}
	if len(rs.Verbs) == 0 {
		t.Error("expected verbs")
	}
	if len(rs.Fields) == 0 {
		t.Error("expected fields")
	}
	if len(rs.DefaultFields) == 0 {
		t.Error("expected default_fields")
	}
}

func TestSchemaUsers_HasFieldDefinitions(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rs schema.ResourceSchema
	json.Unmarshal([]byte(output), &rs)

	// Check that field definitions include expected fields.
	fieldNames := map[string]bool{}
	for _, f := range rs.Fields {
		fieldNames[f.Name] = true
	}

	for _, expected := range []string{"username", "email", "firstname", "lastname", "activated"} {
		if !fieldNames[expected] {
			t.Errorf("expected field %q in users schema", expected)
		}
	}
}

func TestSchemaUsers_FieldTypes(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rs schema.ResourceSchema
	json.Unmarshal([]byte(output), &rs)

	// Verify diverse field types exist.
	types := map[string]bool{}
	for _, f := range rs.Fields {
		types[f.Type] = true
	}

	for _, expected := range []string{"string", "bool", "datetime", "array", "object"} {
		if !types[expected] {
			t.Errorf("expected field type %q in users schema", expected)
		}
	}
}

func TestSchemaUsers_RequiredFields(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rs schema.ResourceSchema
	json.Unmarshal([]byte(output), &rs)

	required := map[string]bool{}
	for _, f := range rs.Fields {
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

func TestSchemaUsers_SortFields(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rs schema.ResourceSchema
	json.Unmarshal([]byte(output), &rs)

	if len(rs.SortFields) == 0 {
		t.Error("expected sort_fields for users")
	}
}

func TestSchemaDevices_JSON(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "devices")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rs schema.ResourceSchema
	if err := json.Unmarshal([]byte(output), &rs); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if rs.Resource != "devices" {
		t.Errorf("resource = %s, want devices", rs.Resource)
	}
	if rs.APIVersion != "v1" {
		t.Errorf("api_version = %s, want v1", rs.APIVersion)
	}
	if rs.IDField != "_id" {
		t.Errorf("id_field = %s, want _id", rs.IDField)
	}
	if rs.NameField != "hostname" {
		t.Errorf("name_field = %s, want hostname", rs.NameField)
	}
}

func TestSchemaCommands_JSON(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "commands")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var manifest schema.CommandManifest
	if err := json.Unmarshal([]byte(output), &manifest); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if manifest.Name != "jc" {
		t.Errorf("name = %s, want jc", manifest.Name)
	}
	if len(manifest.GlobalFlags) == 0 {
		t.Error("expected global flags")
	}
	if len(manifest.Commands) == 0 {
		t.Error("expected commands")
	}
}

func TestSchemaCommands_IncludesAllGroups(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "commands")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var manifest schema.CommandManifest
	json.Unmarshal([]byte(output), &manifest)

	paths := map[string]bool{}
	for _, cmd := range manifest.Commands {
		paths[cmd.Path] = true
	}

	for _, expected := range []string{"jc users", "jc devices", "jc groups", "jc commands", "jc policies", "jc apps", "jc admins", "jc insights", "jc schema"} {
		if !paths[expected] {
			t.Errorf("expected command path %q in manifest", expected)
		}
	}
}

func TestSchemaCommands_HasSubcommands(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "commands")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var manifest schema.CommandManifest
	json.Unmarshal([]byte(output), &manifest)

	for _, cmd := range manifest.Commands {
		if cmd.Path == "jc users" {
			if len(cmd.Subcommands) == 0 {
				t.Error("expected users to have subcommands")
			}
			found := false
			for _, sub := range cmd.Subcommands {
				if sub == "list" {
					found = true
				}
			}
			if !found {
				t.Error("expected users to have 'list' subcommand")
			}
			return
		}
	}
	t.Error("did not find jc users in commands")
}

func TestSchemaCommands_GlobalFlagsIncludeOutput(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "commands")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var manifest schema.CommandManifest
	json.Unmarshal([]byte(output), &manifest)

	found := false
	for _, f := range manifest.GlobalFlags {
		if f.Name == "output" {
			found = true
			if f.Type != "string" {
				t.Errorf("output flag type = %s, want string", f.Type)
			}
			if f.Default != "json" {
				t.Errorf("output flag default = %s, want json", f.Default)
			}
		}
	}
	if !found {
		t.Error("expected output in global flags")
	}
}

func TestSchemaUnknownResource(t *testing.T) {
	_, err := runSchemaCmd(t, "schema", "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown resource")
	}
	if !strings.Contains(err.Error(), "unknown resource") {
		t.Errorf("expected 'unknown resource' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Available resources") {
		t.Errorf("expected error to list available resources, got: %v", err)
	}
}

func TestSchemaCmd_Help(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(output, "resources") {
		t.Error("expected help to mention resources subcommand")
	}
	if !strings.Contains(output, "commands") {
		t.Error("expected help to mention commands subcommand")
	}
	if !strings.Contains(output, "JSON") {
		t.Error("expected help to mention JSON output")
	}
}

func TestRootCmd_IncludesSchema(t *testing.T) {
	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"--help"})
	rootCmd.Execute()

	if !strings.Contains(out.String(), "schema") {
		t.Error("expected root help to include 'schema' command")
	}
}

func TestSchemaGroups_V2(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "groups")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rs schema.ResourceSchema
	json.Unmarshal([]byte(output), &rs)

	if rs.APIVersion != "v2" {
		t.Errorf("api_version = %s, want v2", rs.APIVersion)
	}
	if rs.IDField != "id" {
		t.Errorf("id_field = %s, want id", rs.IDField)
	}
}

func TestSchemaInsights_NoFilter(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "insights")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rs schema.ResourceSchema
	json.Unmarshal([]byte(output), &rs)

	if rs.FilterSupport {
		t.Error("expected insights to have filter_support=false")
	}
	if rs.IDField != "" {
		t.Errorf("expected insights to have empty id_field, got %s", rs.IDField)
	}
}

func TestSchemaOutput_AlwaysJSON(t *testing.T) {
	// Even with --output table, schema commands should produce valid JSON.
	output, err := runSchemaCmd(t, "schema", "resources")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be valid JSON.
	if !json.Valid([]byte(output)) {
		t.Error("expected valid JSON output")
	}
}

func TestSchemaOutput_PrettyPrinted(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Pretty-printed JSON has indentation.
	if !strings.Contains(output, "  ") {
		t.Error("expected pretty-printed JSON with indentation")
	}
}

func TestSchemaAllResources_HaveFieldTypes(t *testing.T) {
	output, err := runSchemaCmd(t, "schema", "resources")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resources []schema.ResourceSchema
	json.Unmarshal([]byte(output), &resources)

	for _, rs := range resources {
		if len(rs.Fields) == 0 {
			t.Errorf("resource %s has no fields", rs.Resource)
		}
		for _, f := range rs.Fields {
			if f.Type == "" {
				t.Errorf("resource %s field %s has empty type", rs.Resource, f.Name)
			}
		}
	}
}

func TestSchemaResources_ContainsExpectedFieldTypes(t *testing.T) {
	// AC: "Schema includes field types: string, bool, int, datetime, array, object"
	output, err := runSchemaCmd(t, "schema", "resources")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allTypes := map[string]bool{}
	var resources []schema.ResourceSchema
	json.Unmarshal([]byte(output), &resources)
	for _, rs := range resources {
		for _, f := range rs.Fields {
			allTypes[f.Type] = true
		}
	}

	for _, expected := range []string{"string", "bool", "int", "datetime", "array", "object"} {
		if !allTypes[expected] {
			t.Errorf("expected field type %q across all resources", expected)
		}
	}
}
