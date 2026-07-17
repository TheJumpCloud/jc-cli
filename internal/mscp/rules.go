package mscp

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Rule is the subset of an mSCP rule YAML the converter needs. The
// tahoe-era schema: flat tags, mobileconfig_info as a map of
// PayloadType → key/values, odv as baseline-name → value.
type Rule struct {
	ID    string   `yaml:"id"`
	Title string   `yaml:"title"`
	Tags  []string `yaml:"tags"`
	// Mobileconfig is true when the rule is enforceable via a
	// configuration profile; false rules are shell-check/fix only.
	Mobileconfig bool `yaml:"mobileconfig"`
	// MobileconfigInfo maps PayloadType → payload keys/values. Values
	// may be the literal string "$ODV", substituted per baseline from
	// ODV.
	MobileconfigInfo map[string]map[string]any `yaml:"mobileconfig_info"`
	// ODV holds the organization-defined values: keyed by baseline
	// name (cis_lvl1, stig, ...) plus "recommended" and "hint".
	ODV map[string]any `yaml:"odv"`
}

// Baseline is an mSCP baseline manifest (baselines/<name>.yaml): the
// official, ordered list of rule IDs making up e.g. CIS Level 1.
type Baseline struct {
	Title string `yaml:"title"`
	// ParentValues names the ODV column this baseline reads ("cis_lvl1").
	ParentValues string `yaml:"parent_values"`
	Profile      []struct {
		Section string   `yaml:"section"`
		Rules   []string `yaml:"rules"`
	} `yaml:"profile"`
}

// RuleIDs returns the manifest's rule IDs in order.
func (b *Baseline) RuleIDs() []string {
	var ids []string
	for _, s := range b.Profile {
		ids = append(ids, s.Rules...)
	}
	return ids
}

// LoadBaseline reads baselines/<name>.yaml from a snapshot dir.
func LoadBaseline(dir, name string) (*Baseline, error) {
	path := filepath.Join(dir, "baselines", name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			available, _ := ListBaselines(dir)
			return nil, fmt.Errorf("no mSCP baseline %q in snapshot %s — available: %s",
				name, SnapshotTag, strings.Join(available, ", "))
		}
		return nil, err
	}
	var b Baseline
	if err := yaml.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parsing baseline %s: %w", path, err)
	}
	if len(b.Profile) == 0 {
		return nil, fmt.Errorf("baseline %s has no profile sections", path)
	}
	return &b, nil
}

// ListBaselines names the manifests present in a snapshot dir.
func ListBaselines(dir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "baselines"))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
		}
	}
	return names, nil
}

// LoadRules walks rules/**/*.yaml and indexes by rule ID. Individual
// unparsable files fail loudly — a broken snapshot should never
// silently thin out a security baseline.
func LoadRules(dir string) (map[string]*Rule, error) {
	root := filepath.Join(dir, "rules")
	rules := make(map[string]*Rule)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var r Rule
		if err := yaml.Unmarshal(data, &r); err != nil {
			return fmt.Errorf("parsing rule %s: %w", path, err)
		}
		if r.ID == "" {
			return nil // section/template files without an id
		}
		rules[r.ID] = &r
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("no rules found under %s", root)
	}
	return rules, nil
}

// resolveODV substitutes a rule's "$ODV" placeholder for one baseline:
// the baseline's own column wins, then "recommended". A rule that uses
// $ODV without a resolvable value is a converter error, not a silent
// drop — the generated baseline must never ship a literal "$ODV".
//
// $ODV is not always a top-level scalar: mSCP nests it inside payload
// dictionaries and lists (e.g. com.apple.mobiledevice.passwordpolicy's
// customRegex.passwordContentRegex). This walks the whole value tree so
// a nested placeholder is substituted too — the shallow version shipped
// a literal "$ODV" as a device-facing password regex (review 2026-07-17).
func resolveODV(r *Rule, parentValues string, v any) (any, error) {
	switch val := v.(type) {
	case string:
		if val != "$ODV" {
			return val, nil
		}
		if odv, ok := r.ODV[parentValues]; ok {
			return odv, nil
		}
		if odv, ok := r.ODV["recommended"]; ok {
			return odv, nil
		}
		return nil, fmt.Errorf("rule %s uses $ODV but has no %q or recommended value", r.ID, parentValues)
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, nested := range val {
			resolved, err := resolveODV(r, parentValues, nested)
			if err != nil {
				return nil, err
			}
			out[k] = resolved
		}
		return out, nil
	case []any:
		out := make([]any, len(val))
		for i, nested := range val {
			resolved, err := resolveODV(r, parentValues, nested)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil
	default:
		return v, nil
	}
}
