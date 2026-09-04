package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// eventTypesJSON is the Directory Insights event type catalog, generated from
// JumpCloud's DI API reference by scripts/gen-event-types.py.
//
// A jc_events trigger names the event it listens for, and nothing in the
// workflows API validates that name. A mistyped type saves, activates, and
// silently never fires — indistinguishable from an event that simply has not
// happened yet, and with no run to inspect because no run ever starts. This
// catalog is the only way to catch that at author time.
//
// It is a LOWER BOUND, not a whitelist. Cross-checking against a live tenant
// found 30 of 110 emitted types absent from it — command_result, policy_result,
// radius_auth_attempt, ldap_srch and others are real and undocumented. So an
// unrecognised type is reported as a warning, never an error, on the same
// footing as the scope check: the API is the authority, this is a lower bound.
//
//go:embed eventtypes.json
var eventTypesJSON []byte

// EventType is one documented Directory Insights event.
type EventType struct {
	// Describe is JumpCloud's own one-line description.
	Describe string `json:"d"`
	// Service is the Directory Insights service it belongs to, matching
	// api.ValidInsightsServices. Empty where the docs do not group it.
	Service string `json:"s,omitempty"`
}

var (
	etOnce  sync.Once
	events  map[string]EventType
	etNames []string
)

func loadEventTypes() {
	etOnce.Do(func() {
		if err := json.Unmarshal(eventTypesJSON, &events); err != nil {
			panic(fmt.Sprintf("workflow: embedded event type catalog is corrupt: %v", err))
		}
		etNames = make([]string, 0, len(events))
		for k := range events {
			etNames = append(etNames, k)
		}
		sort.Strings(etNames)
	})
}

// LookupEventType returns the catalog entry for an event type.
func LookupEventType(name string) (EventType, bool) {
	loadEventTypes()
	e, ok := events[name]
	return e, ok
}

// EventTypeCount reports how many event types the catalog holds.
func EventTypeCount() int {
	loadEventTypes()
	return len(events)
}

// EventTypes returns the catalog, optionally filtered by service and by a
// substring matched against the name or its description.
func EventTypes(service, search string) map[string]EventType {
	loadEventTypes()
	service = strings.ToLower(strings.TrimSpace(service))
	search = strings.ToLower(strings.TrimSpace(search))

	out := map[string]EventType{}
	for name, e := range events {
		if service != "" && service != "all" && e.Service != service {
			continue
		}
		if search != "" &&
			!strings.Contains(strings.ToLower(name), search) &&
			!strings.Contains(strings.ToLower(e.Describe), search) {
			continue
		}
		out[name] = e
	}
	return out
}

// SuggestEventType returns the closest catalog entries to an unrecognised
// type. The realistic failure is a typo rather than an invention — user_suspend
// for user_suspended — so edit distance is the right measure, and naming the
// near miss is what makes the warning actionable.
func SuggestEventType(name string, limit int) []string {
	loadEventTypes()
	if limit <= 0 {
		limit = 3
	}
	needle := strings.ToLower(strings.TrimSpace(name))

	type scored struct {
		name string
		d    int
	}
	var all []scored
	for _, n := range etNames {
		lower := strings.ToLower(n)
		d := levenshtein(needle, lower)
		if strings.Contains(lower, needle) || strings.Contains(needle, lower) {
			d = 0
		}
		// Two caps, and the absolute one does the work. The relative cap
		// alone scales with the name: for a 23-character type it allowed 13
		// edits, which let feature_settings_change be "corrected" to
		// syncro_settings_update (11), autotask_settings_update (12) and
		// association_change (13). None is a typo of it.
		//
		// That matters because this list exists to catch TYPOS, and a real
		// typo is one or two characters — the three examples above are all
		// distance 1. An event type the catalog simply omits is the opposite
		// case, and offering corrections there invites someone to "fix" a
		// correct type into a wrong one, producing a workflow that silently
		// never fires: the exact failure this area was built to catch.
		if d > 3 || d > len(needle)/2+2 {
			continue
		}
		all = append(all, scored{n, d})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].d != all[j].d {
			return all[i].d < all[j].d
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
