package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
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
	// Scopes are the API scopes that permit this operation, from the spec's
	// x-scopes extension. Holding ANY one of them is sufficient.
	//
	// Treat this as a lower bound, not the whole truth: the live API rejected
	// postApiRuncommand under a role lacking these and named a fourth scope
	// ("systems") that the spec does not list. So a role holding one of these
	// definitely works, while a role holding none of them may still work.
	// That asymmetry is why the scope check warns rather than blocks.
	Scopes []string `json:"sc,omitempty"`
}

// PermittedBy reports whether a set of role scopes covers this operation.
// Holding any single declared scope is enough.
func (o Operation) PermittedBy(held map[string]bool) bool {
	for _, s := range o.Scopes {
		if held[s] {
			return true
		}
	}
	return false
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

// pathParamPattern matches the {placeholder} segments in an operation path.
var pathParamPattern = regexp.MustCompile(`\{(\w+)\}`)

// PathParams returns the placeholder names an operation's path requires, in
// the order they appear. 509 of the 732 catalogued operations take at least
// one, and the names are not uniform — id, user_id, group_id, UUID,
// provider_id — which is exactly why supplying the wrong one is easy.
func (o Operation) PathParams() []string {
	m := pathParamPattern.FindAllStringSubmatch(o.Path, -1)
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for _, g := range m {
		out = append(out, g[1])
	}
	return out
}

// ComparePathParams reports how a task's pathParams object differs from the
// placeholders the operation's path actually requires.
//
// Shared by validate and simulate on purpose. A live run wrote
// pathParams {"userid": ...} against /api/v2/users/{user_id}/memberof and BOTH
// accepted it — simulate even printed the correct path beside the wrong name
// without remarking on it — so the two agreeing is the point, not an
// incidental tidiness.
//
// ok is false when pathParams is present but not an object.
func ComparePathParams(op Operation, with map[string]any) (missing, unexpected []string, ok bool) {
	required := op.PathParams()

	raw, present := with["pathParams"]
	if !present {
		return required, nil, true
	}
	m, isMap := raw.(map[string]any)
	if !isMap {
		return nil, nil, false
	}

	supplied := make(map[string]bool, len(m))
	for k := range m {
		supplied[k] = true
	}
	for _, want := range required {
		if !supplied[want] {
			missing = append(missing, want)
		}
	}
	req := make(map[string]bool, len(required))
	for _, want := range required {
		req[want] = true
	}
	for k := range supplied {
		if !req[k] {
			unexpected = append(unexpected, k)
		}
	}
	sort.Strings(unexpected)
	return missing, unexpected, true
}
