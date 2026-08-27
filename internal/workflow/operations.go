package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// operationsJSON is the operationId index generated from JumpCloud's OpenAPI
// spec by scripts/gen-operation-index.py. Workflow DSL invokes JumpCloud's own
// API by operationId, and nothing server-side validates that id at create
// time, so a typo yields a workflow that only fails once it runs. Embedding
// the index lets validation catch it at author time and lets `explain` render
// each step as METHOD /path.
//
//go:embed operations.json
var operationsJSON []byte

// Operation is one JumpCloud API operation a jc_operation step can call.
type Operation struct {
	Method  string `json:"m"`
	Path    string `json:"p"`
	Summary string `json:"s"`
}

// APIVersion returns the JumpCloud API version the operation lives under.
// jc_operation steps carry a `version` field alongside operationId, and across
// all 33 steps in the 12 shipped templates its value matches this exactly.
func (o Operation) APIVersion() int {
	if strings.HasPrefix(o.Path, "/api/v2/") {
		return 2
	}
	return 1
}

// Describe renders the operation for one line of explain output.
func (o Operation) Describe() string {
	s := o.Method + " " + o.Path
	if o.Summary != "" {
		s += " — " + o.Summary
	}
	return s
}

var (
	opsOnce sync.Once
	ops     map[string]Operation
	opNames []string
)

func loadOperations() {
	opsOnce.Do(func() {
		if err := json.Unmarshal(operationsJSON, &ops); err != nil {
			// The index is generated and embedded at build time; a parse
			// failure is a build defect, not a runtime condition.
			panic(fmt.Sprintf("workflow: embedded operation index is corrupt: %v", err))
		}
		opNames = make([]string, 0, len(ops))
		for k := range ops {
			opNames = append(opNames, k)
		}
		sort.Strings(opNames)
	})
}

// LookupOperation returns the operation for an operationId.
func LookupOperation(id string) (Operation, bool) {
	loadOperations()
	op, ok := ops[id]
	return op, ok
}

// OperationCount reports how many operations the index holds.
func OperationCount() int {
	loadOperations()
	return len(ops)
}

// OperationIDs returns every known operationId, sorted.
func OperationIDs() []string {
	loadOperations()
	out := make([]string, len(opNames))
	copy(out, opNames)
	return out
}

// looksLegacyID reports whether an operationId uses the deprecated snake_case
// form. JumpCloud is migrating these to standardized camelCase ids
// (systemusers_list → getApiSystemusers); the old ones still appear in older
// workflows and in copied examples.
func looksLegacyID(id string) bool {
	return strings.Contains(id, "_")
}

// SuggestOperation returns the closest known operationIds to an unknown one,
// so a typo produces a usable message instead of a bare rejection.
//
// Two different failures need two different scores. A camelCase typo is a
// near-miss and edit distance finds it. A legacy snake_case id is not a typo
// at all — it is a different naming scheme for the same operation, so its
// words are matched against candidates instead ("systemusers_list" shares
// "systemusers" with "getApiSystemusers", which edit distance would never
// rank highly).
func SuggestOperation(id string, limit int) []string {
	loadOperations()
	if limit <= 0 {
		limit = 3
	}

	type scored struct {
		name string
		rank float64
	}
	var all []scored

	if looksLegacyID(id) {
		parts := make([]string, 0, 4)
		for _, p := range strings.Split(strings.ToLower(id), "_") {
			if p != "" {
				parts = append(parts, p)
			}
		}
		for _, name := range opNames {
			lower := strings.ToLower(name)
			matched := 0
			for _, p := range parts {
				if strings.Contains(lower, p) {
					matched += len(p)
				}
			}
			if matched == 0 {
				continue
			}
			// Score by how MUCH of the id matched, not how many words did:
			// "list" happens to appear inside "Iplists", and a short
			// accidental hit must not outrank a long real one like
			// "systemusers". Length breaks ties toward the plain operation
			// over a deeply nested one.
			all = append(all, scored{name, -float64(matched)*1000 + float64(len(name))})
		}
	} else {
		needle := strings.ToLower(id)
		for _, name := range opNames {
			lower := strings.ToLower(name)
			d := levenshtein(needle, lower)
			if strings.Contains(lower, needle) || strings.Contains(needle, lower) {
				d = 0
			}
			// A suggestion further away than this is noise, not help.
			if d > len(needle)/2+2 {
				continue
			}
			all = append(all, scored{name, float64(d)})
		}
	}

	sort.SliceStable(all, func(i, j int) bool {
		if all[i].rank != all[j].rank {
			return all[i].rank < all[j].rank
		}
		return all[i].name < all[j].name
	})

	out := make([]string, 0, limit)
	for _, s := range all {
		if len(out) == limit {
			break
		}
		out = append(out, s.name)
	}
	return out
}

// levenshtein is the standard edit distance, two rows at a time.
func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
