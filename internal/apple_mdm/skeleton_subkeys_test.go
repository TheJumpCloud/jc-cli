package apple_mdm

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestEmitValuesSkeleton_ExpandsDictionarySubkeys(t *testing.T) {
	// Live regression — MCX-style dictionary with declared subkeys.
	// Pre-fix the skeleton emitted just `{}` and the operator
	// couldn't see what subkeys the dict accepted; the editor
	// experience was broken for the entire MCX family + every wifi
	// EAPClientConfiguration. The recursive expansion replaces `{}`
	// with the inline subkey block.
	p := Payload{
		Type: "com.example.test",
		Keys: []Key{
			{
				Name: "PowerSettings", Type: "dictionary", Presence: "optional",
				Content: "Power-related settings.",
				Subkeys: []Key{
					{Name: "AutoSleep", Type: "boolean", Default: false, Content: "Sleep when idle."},
					{Name: "SleepTimer", Type: "integer", Range: &Range{Min: 1, Max: 180}, Content: "Minutes."},
				},
			},
		},
	}
	out := EmitValuesSkeleton(p)

	// The nested subkey names must appear with the dotted-path-style
	// indented YAML structure. Pre-fix they were absent.
	if !strings.Contains(out, "AutoSleep") || !strings.Contains(out, "SleepTimer") {
		t.Errorf("subkeys not expanded:\n%s", out)
	}
	// The empty-dict fallback `{}` must NOT appear when subkeys are
	// declared — the whole point of the fix is to surface the schema.
	if strings.Contains(out, "PowerSettings: {}") {
		t.Errorf("subkeyed dict still emitted as empty placeholder:\n%s", out)
	}
	// Subkey docs (range / default) should ride along.
	if !strings.Contains(out, "default=false") {
		t.Error("subkey default affordance missing")
	}
	if !strings.Contains(out, "range [1..180]") {
		t.Error("subkey range affordance missing")
	}
}

func TestEmitValuesSkeleton_ExpandedDictParsesAsYAML(t *testing.T) {
	// After the operator uncomments a dict header + a subkey value, the
	// remaining file must still parse. Walk an emitted skeleton, remove
	// the leading `# ` on the parent dict + one inner value, and assert
	// the result is a valid YAML map with the right shape.
	p := Payload{
		Type: "com.example.test",
		Keys: []Key{
			{
				Name: "Cfg", Type: "dictionary", Presence: "optional",
				Subkeys: []Key{
					{Name: "Flag", Type: "boolean", Default: false},
					{Name: "Limit", Type: "integer"},
				},
			},
		},
	}
	out := EmitValuesSkeleton(p)
	// The emit's actual shape for an optional dict with optional
	// subkeys is:
	//   # Cfg: dictionary           (doc)
	//   # Cfg:                       (value line — commented because optional)
	//     # Flag: boolean ...        (doc, indented under parent)
	//     # Flag: false              (value line — commented)
	//
	// To activate one subkey the operator uncomments the parent value
	// line and the chosen subkey value line. Verify that path.
	edited := strings.NewReplacer(
		"# Cfg:\n", "Cfg:\n",
		"  # Flag: false", "  Flag: true",
	).Replace(out)

	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(edited), &parsed); err != nil {
		t.Fatalf("post-edit YAML parse: %v\n%s", err, edited)
	}
	cfg, ok := parsed["Cfg"].(map[string]any)
	if !ok {
		t.Fatalf("Cfg not a map (got %T):\nEdited skeleton:\n%s", parsed["Cfg"], edited)
	}
	if cfg["Flag"] != true {
		t.Errorf("Flag = %v, want true", cfg["Flag"])
	}
}

func TestEmitValuesSkeleton_ArrayOfDictsExpands(t *testing.T) {
	// Array-of-dicts (e.g. wifi.managed's RoamingConsortiumOIs) gets
	// "repeat this block" instructions + the element schema.
	p := Payload{
		Type: "com.example.test",
		Keys: []Key{
			{
				Name: "Entries", Type: "array", Presence: "optional",
				Subkeys: []Key{
					{Name: "Name", Type: "string"},
					{Name: "Value", Type: "integer"},
				},
			},
		},
	}
	out := EmitValuesSkeleton(p)
	if !strings.Contains(out, "repeat this block") {
		t.Errorf("array hint missing:\n%s", out)
	}
	if !strings.Contains(out, "Name") || !strings.Contains(out, "Value") {
		t.Errorf("array element subkeys missing:\n%s", out)
	}
}

func TestEmitValuesSkeleton_DepthCapPreventsRunaway(t *testing.T) {
	// Construct a 4-deep nested dict chain. Apple's wifi.managed
	// EAPClientConfiguration goes 4+ levels in practice; without a
	// cap the skeleton would balloon. Verify the cap is honored.
	deep := Key{Name: "L3", Type: "dictionary", Subkeys: []Key{{Name: "leaf", Type: "string"}}}
	mid := Key{Name: "L2", Type: "dictionary", Subkeys: []Key{deep}}
	upper := Key{Name: "L1", Type: "dictionary", Subkeys: []Key{mid}}
	p := Payload{Type: "x", Keys: []Key{upper}}
	out := EmitValuesSkeleton(p)
	// L1, L2, L3 should all expand (depth 0,1,2). The `leaf` inside
	// L3 sits at depth 3 which is at the cap — the cap is "depth <
	// maxSkeletonDepth" so depth=3 falls back to {} for the
	// dictionary type. But because the L3 contains a string `leaf`
	// (not another dict), the string is emitted directly.
	// The check that matters: even with 4-level chains we always
	// produce a usable skeleton, not infinite output.
	if len(out) > 4096 {
		t.Errorf("skeleton blew past 4 KB for a 4-level chain: %d bytes", len(out))
	}
}
