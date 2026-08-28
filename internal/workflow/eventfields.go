package workflow

import (
	"sort"
	"strings"
)

// Trigger payload fields, for validating ${ input.* } references in a
// jc_events workflow.
//
// A condition naming a field the event does not carry evaluates false forever.
// That is the same silent-never-fires failure as a mistyped event type, one
// layer down, and just as invisible: the workflow saves, activates, and simply
// never matches.
//
// There is no published specification for these payloads. This map is built
// from a full trigger payload captured live from a group_create run on
// 2026-08-28, plus the association fields the shipped templates rely on. It is
// therefore a LOWER BOUND like every other catalog here — an unrecognised
// field is a warning, never an error.

// commonEventFields are the envelope fields observed on a real Directory
// Insights trigger payload. Every jc_events trigger should carry these.
var commonEventFields = []string{
	"@version",
	"auth_method", // e.g. "api key" — how the actor authenticated
	"changes",     // [] of {field, from, to} for the changed object
	"client_ip",
	"event_type",
	"geoip",        // {city, region, country, latitude, longitude, timezone}
	"initiated_by", // {id, email, type} — WHO did it
	"jc_transformation_ts",
	"organization",
	"resource", // {id, name, type} — the object acted on
	"service",
	"success",
	"testing_event",
	"timestamp",
	"useragent",
}

// eventFieldExtras are fields carried by particular event families beyond the
// common envelope. Keyed by an event-type suffix so a family is covered
// without listing every member.
var eventFieldExtras = map[string][]string{
	// Binding/unbinding events carry the association itself. The shipped
	// templates read association.op and
	// association.connection.{from,to}.object_id.
	"association_change": {"association"},
}

// EventFields returns the payload fields a jc_events trigger of this type is
// known to expose, sorted. Unknown event types still get the common envelope,
// since it was present on every payload observed.
func EventFields(eventType string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(fs []string) {
		for _, f := range fs {
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	add(commonEventFields)
	for suffix, extra := range eventFieldExtras {
		if strings.HasSuffix(eventType, suffix) {
			add(extra)
		}
	}
	sort.Strings(out)
	return out
}

// KnownEventField reports whether a top-level input field is known for an
// event type.
func KnownEventField(eventType, field string) bool {
	for _, f := range EventFields(eventType) {
		if f == field {
			return true
		}
	}
	return false
}

// inputSchemaFields returns the top-level property names an external trigger's
// declared input schema allows, and whether a schema was declared at all.
//
// The API enforces this schema when a run is triggered — a value of the wrong
// type is rejected outright — so a reference to a field the schema does not
// declare can never be satisfied.
func (d DSL) inputSchemaFields() ([]string, bool) {
	if d.Input == nil {
		return nil, false
	}
	schema, ok := d.Input["schema"].(map[string]any)
	if !ok {
		return nil, false
	}
	doc, ok := schema["document"].(map[string]any)
	if !ok {
		return nil, false
	}
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(props))
	for k := range props {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, true
}
