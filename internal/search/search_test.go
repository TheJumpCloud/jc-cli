package search

import (
	"reflect"
	"testing"
)

func TestLookup_NameAndAlias(t *testing.T) {
	if r, ok := Lookup("systems"); !ok || r.Endpoint != "/search/systems" {
		t.Errorf("Lookup(systems) = %+v, %v", r, ok)
	}
	if r, ok := Lookup("systemusers"); !ok || r.Name != "users" {
		t.Errorf("alias systemusers should resolve to users, got %+v", r)
	}
	if r, ok := Lookup("commandresults"); !ok || r.Name != "command-results" {
		t.Errorf("alias commandresults should resolve, got %+v", r)
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("Lookup(nope) should fail")
	}
	// organizations is intentionally NOT covered.
	if _, ok := Lookup("organizations"); ok {
		t.Error("organizations must not be a covered search resource")
	}
}

func TestResourceNames_Sorted(t *testing.T) {
	got := ResourceNames()
	want := []string{"command-results", "commands", "systems", "users"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResourceNames() = %v, want %v", got, want)
	}
}

func TestBody_TermUsesDefaultFields(t *testing.T) {
	r := Resources["users"]
	body := r.Body("john", nil, nil)
	sf := body["searchFilter"].(map[string]any)
	if sf["searchTerm"] != "john" {
		t.Errorf("searchTerm = %v", sf["searchTerm"])
	}
	if !reflect.DeepEqual(sf["fields"], r.SearchFields) {
		t.Errorf("fields = %v, want default %v", sf["fields"], r.SearchFields)
	}
	if _, ok := body["filter"]; ok {
		t.Error("no filter should be present")
	}
}

func TestBody_SearchFieldOverride(t *testing.T) {
	body := Resources["systems"].Body("web", []string{"hostname"}, nil)
	sf := body["searchFilter"].(map[string]any)
	if !reflect.DeepEqual(sf["fields"], []string{"hostname"}) {
		t.Errorf("override fields = %v", sf["fields"])
	}
}

func TestBody_MatchAllAndFilter(t *testing.T) {
	// No term → no searchFilter (match-all), just the filter.
	body := Resources["commands"].Body("", nil, []string{"commandType:eq:linux"})
	if _, ok := body["searchFilter"]; ok {
		t.Error("empty term should omit searchFilter")
	}
	if f := body["filter"].([]string); f[0] != "commandType:eq:linux" {
		t.Errorf("filter = %v", body["filter"])
	}
	// Fully empty → match-all with no keys.
	empty := Resources["commands"].Body("", nil, nil)
	if len(empty) != 0 {
		t.Errorf("empty body should be {}, got %v", empty)
	}
}
