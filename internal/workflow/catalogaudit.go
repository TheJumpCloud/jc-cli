package workflow

import "sort"

// Auditing the event-type catalog against reality.
//
// The catalog is generated from JumpCloud's Directory Insights documentation,
// and the documentation is incomplete in a way that matters: it omits the
// `workflows` service entirely, so workflow_create, workflow_delete and
// workflow_run — all of them TRIGGERABLE — were reported as unknown event
// types by the one check that exists to catch real typos.
//
// The generator's own docstring has warned since it was written that the
// catalog is a lower bound, having found 30 of 110 emitted types absent. But a
// warning in a comment reconciles nothing. This does: point it at a live
// tenant, diff what is actually emitted against what is known, and the gap
// stops being a thing someone has to remember.
//
// The reverse direction is NOT a defect. A catalog entry with no observed
// event usually means the tenant does not use that feature, and treating it as
// drift would bury the direction that matters under noise.

// CatalogGap is one event type a tenant emits that the catalog does not know.
type CatalogGap struct {
	EventType string `json:"event_type"`
	// Count is how many were seen in the audited window.
	Count int `json:"count"`
	// Suggestions are near-miss catalog entries. A gap with a close neighbour
	// is more likely a documentation omission than a new event family — and
	// on the trigger side, more likely to be someone's typo.
	Suggestions []string `json:"closest_known,omitempty"`
}

// CatalogAudit is the comparison of a live tenant against the catalog.
type CatalogAudit struct {
	// Window describes the period audited, for the report to be meaningful.
	Window string `json:"window"`
	// Emitted is how many distinct types the tenant produced.
	Emitted int `json:"emitted_types"`
	// Known is the catalog size at the time of the audit.
	Known int `json:"catalog_size"`
	// Gaps are emitted types absent from the catalog, worst first by volume.
	Gaps []CatalogGap `json:"gaps"`
	// Covered is how many emitted types the catalog already knew.
	Covered int `json:"covered"`
	// Note states what the result does and does not prove.
	Note string `json:"note"`
}

// AuditCatalog diffs observed event types against the catalog.
//
// observed maps event type to occurrence count, as `insights distinct` returns
// it. A quiet tenant emits few types, so an empty gap list is weak evidence of
// a complete catalog and the note says so.
func AuditCatalog(observed map[string]int, window string) CatalogAudit {
	a := CatalogAudit{
		Window:  window,
		Emitted: len(observed),
		Known:   len(EventTypes("", "")),
		Note: "Gaps are event types this tenant emitted that the catalog does not list — the " +
			"catalog is generated from documentation that has been observed to omit an entire " +
			"service. The reverse direction is not reported: a catalog entry with no observed " +
			"event usually means this tenant does not use that feature. An empty gap list is " +
			"only as strong as the window and the tenant's activity.",
	}

	for name, count := range observed {
		if _, known := LookupEventType(name); known {
			a.Covered++
			continue
		}
		a.Gaps = append(a.Gaps, CatalogGap{
			EventType:   name,
			Count:       count,
			Suggestions: SuggestEventType(name, 3),
		})
	}

	// Loudest first: a type emitted hundreds of times is a bigger hole than
	// one seen once.
	sort.SliceStable(a.Gaps, func(i, j int) bool {
		if a.Gaps[i].Count != a.Gaps[j].Count {
			return a.Gaps[i].Count > a.Gaps[j].Count
		}
		return a.Gaps[i].EventType < a.Gaps[j].EventType
	})
	return a
}
