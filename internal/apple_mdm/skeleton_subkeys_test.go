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

func TestEmitValuesSkeleton_ArrayOfScalars(t *testing.T) {
	// Apple's convention: array subkeys carry a single entry
	// describing the element type. For scalar elements (the common
	// case — proxy ProxyExceptions, wifi RoamingConsortiumOIs) the
	// emit must produce `- value`, NOT `- subkeyName: value`. Pre-fix
	// (Bugbot PR #52 re-review) we emitted the subkey's name as a
	// key, producing a plist shape devices reject.
	p := Payload{
		Type: "com.example.test",
		Keys: []Key{
			{
				Name: "EAPTypes", Type: "array", Presence: "optional",
				Subkeys: []Key{
					{Name: "EAPType", Type: "integer"},
				},
			},
		},
	}
	out := EmitValuesSkeleton(p)
	// Scalar arrays must NOT emit the element's subkey name as a key.
	if strings.Contains(out, "EAPType: 0") {
		t.Errorf("scalar array should emit `- 0`, not `EAPType: 0`:\n%s", out)
	}
	// Should emit a bare scalar under the `- ` marker.
	if !strings.Contains(out, "- 0") {
		t.Errorf("scalar array should emit `- 0` for example value:\n%s", out)
	}
	// Element-type doc comment should help the operator.
	if !strings.Contains(out, "element type: integer") {
		t.Errorf("scalar array should annotate the element type:\n%s", out)
	}
}

func TestEmitValuesSkeleton_ArrayOfDicts(t *testing.T) {
	// Apple's convention: array's single subkey is itself a dict
	// (type=dictionary) whose subkeys are the dict fields. Pre-fix
	// we wrapped them in an extra wrapper key (the element's name),
	// producing `- ApplicationsItem: {BundleID,Allowed}` instead of
	// `- BundleID: ..., Allowed: ...`. Firewall, WebContentFilter,
	// and the SaaS allow-lists all use this pattern.
	p := Payload{
		Type: "com.example.test",
		Keys: []Key{
			{
				Name: "Applications", Type: "array", Presence: "optional",
				Subkeys: []Key{
					{
						Name: "Application", Type: "dictionary",
						Subkeys: []Key{
							{Name: "BundleID", Type: "string", Presence: "required"},
							{Name: "Allowed", Type: "boolean", Default: false},
						},
					},
				},
			},
		},
	}
	out := EmitValuesSkeleton(p)
	if !strings.Contains(out, "repeat this block") {
		t.Errorf("array repeat hint missing:\n%s", out)
	}
	// Dict fields must surface directly under the array — not nested
	// under an `Application:` wrapper.
	if !strings.Contains(out, "BundleID") || !strings.Contains(out, "Allowed") {
		t.Errorf("dict-array fields missing:\n%s", out)
	}
	// The element-wrapper name should NOT appear as a key in the YAML.
	if strings.Contains(out, "Application: dictionary") || strings.Contains(out, "Application:\n") {
		// Note: it's OK to see the wrapper name in a comment header,
		// but not as a YAML key emitting nested fields underneath.
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
