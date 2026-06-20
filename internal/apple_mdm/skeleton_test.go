package apple_mdm

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestEmitValuesSkeleton_ShapeAndComments(t *testing.T) {
	p := Payload{
		Type:        "com.example.test",
		Title:       "Test",
		Description: "A test payload for skeleton generation.",
		Keys: []Key{
			{Name: "RequiredKey", Type: "string", Presence: "required", Content: "Must be set."},
			{Name: "FlagA", Type: "boolean", Presence: "optional", Default: true, Content: "An optional flag."},
			{Name: "Mode", Type: "string", Presence: "optional",
				RangeList: []any{"none", "soft", "hard"}, Default: "none"},
			{Name: "Limit", Type: "integer", Presence: "optional",
				Range: &Range{Min: 1, Max: 100}},
		},
	}
	out := EmitValuesSkeleton(p)

	// Header — type + title + description.
	if !strings.Contains(out, "# com.example.test") {
		t.Error("header missing payload type")
	}
	if !strings.Contains(out, "Test") {
		t.Error("header missing title")
	}
	if !strings.Contains(out, "A test payload") {
		t.Error("header missing description")
	}

	// Required keys section + RequiredKey uncommented.
	if !strings.Contains(out, "Required keys") {
		t.Error("missing 'Required keys' section header")
	}
	if !strings.Contains(out, "\nRequiredKey: ") {
		t.Errorf("RequiredKey not uncommented in skeleton:\n%s", out)
	}

	// Optional keys section + each optional commented out.
	if !strings.Contains(out, "Optional keys") {
		t.Error("missing 'Optional keys' section header")
	}
	for _, name := range []string{"FlagA", "Mode", "Limit"} {
		if !strings.Contains(out, "# "+name+": ") {
			t.Errorf("optional key %q not commented in skeleton", name)
		}
	}

	// Affordances surface inline.
	if !strings.Contains(out, "enum{none,soft,hard}") {
		t.Error("enum affordance missing for Mode")
	}
	if !strings.Contains(out, "range [1..100]") {
		t.Error("range affordance missing for Limit")
	}
	if !strings.Contains(out, "default=true") {
		t.Error("default affordance missing for FlagA")
	}
}

func TestEmitValuesSkeleton_OutputParsesAsYAML(t *testing.T) {
	// The skeleton must remain valid YAML after the operator un-comments
	// some lines. Verify the as-emitted-out form parses cleanly — this
	// catches accidental indentation or quoting bugs.
	p := Payload{
		Type: "com.example.test",
		Keys: []Key{
			{Name: "RequiredStr", Type: "string", Presence: "required"},
			{Name: "OptionalBool", Type: "boolean", Presence: "optional", Default: true},
		},
	}
	out := EmitValuesSkeleton(p)
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("emitted skeleton fails YAML parse: %v\n%s", err, out)
	}
	// Only the required key should be present (optionals are commented).
	if _, ok := parsed["RequiredStr"]; !ok {
		t.Error("RequiredStr missing from parsed skeleton")
	}
	if _, ok := parsed["OptionalBool"]; ok {
		t.Error("OptionalBool should be commented out (and thus absent)")
	}
}

func TestEmitValuesSkeleton_RoundTripsThroughCoerceAndValidate(t *testing.T) {
	// Synthesize a payload, generate the skeleton, simulate an operator
	// uncommenting one optional key, parse, and run through
	// CoerceAndValidate. The whole authoring loop has to work end-to-end
	// for the TUI to be useful.
	p := Payload{
		Type: "com.example.test",
		Keys: []Key{
			{Name: "Required", Type: "string", Presence: "required"},
			{Name: "Optional", Type: "boolean", Presence: "optional", Default: false},
		},
	}
	skel := EmitValuesSkeleton(p)
	// Pretend the operator uncommented and set the Optional line.
	edited := strings.Replace(skel, "# Optional: false", "Optional: true", 1)
	// Set the Required field to a real value.
	edited = strings.Replace(edited, `Required: ""`, `Required: "hello"`, 1)

	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(edited), &parsed); err != nil {
		t.Fatalf("edited skeleton YAML parse: %v\n%s", err, edited)
	}
	got, err := CoerceAndValidate(p, parsed)
	if err != nil {
		t.Fatalf("CoerceAndValidate: %v", err)
	}
	if got["Required"] != "hello" {
		t.Errorf("Required = %v, want hello", got["Required"])
	}
	if got["Optional"] != true {
		t.Errorf("Optional = %v, want true", got["Optional"])
	}
}
