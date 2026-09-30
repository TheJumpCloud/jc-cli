package identityrisk

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestRequiresWindow(t *testing.T) {
	// The split is counter-intuitive and undocumented, so it is pinned:
	// the aggregates need a window, the two record reads do not.
	needs := []string{StatsEndpoint, IdentitiesEndpoint, LoginTypesEndpoint,
		GeolocationsEndpoint, FactorsTimelineEndpoint}
	for _, e := range needs {
		if !RequiresWindow(e) {
			t.Errorf("%s should require a time window", e)
		}
	}
	for _, e := range []string{EventsEndpoint, IdentityEndpoint("abc"), EventEndpoint("abc")} {
		if RequiresWindow(e) {
			t.Errorf("%s should NOT require a time window", e)
		}
	}
}

func TestEndpoints(t *testing.T) {
	id := "6aa825580af05c00013a37a4"
	cases := map[string]string{
		EventEndpoint(id):         "/identityrisk/events/" + id,
		EventResolveEndpoint(id):  "/identityrisk/events/" + id + "/resolve",
		IdentityEndpoint(id):      "/identityrisk/identities/" + id,
		IdentityTrendEndpoint(id): "/identityrisk/identities/" + id + "/trend",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("endpoint = %q, want %q", got, want)
		}
	}
}

func TestIsObjectID(t *testing.T) {
	for _, ok := range []string{"6aa825580af05c00013a37a4", "608b8cf181c10b3303716501", strings.ToUpper("6aa825580af05c00013a37a4")} {
		if !IsObjectID(ok) {
			t.Errorf("%q should be an object id", ok)
		}
	}
	for _, bad := range []string{"", "not-an-id", "1", "6aa825580af05c00013a37a", "6aa825580af05c00013a37a44", "00000000-0000-0000-0000-000000000000"} {
		if IsObjectID(bad) {
			t.Errorf("%q should NOT be an object id", bad)
		}
	}
}

func TestBuildResolve(t *testing.T) {
	got, err := BuildResolve("resolved", "safe", "looked into it")
	if err != nil {
		t.Fatalf("BuildResolve error: %v", err)
	}
	if got.ResolutionStatus != "RISK_RESOLUTION_STATUS_RESOLVED" {
		t.Errorf("status = %q", got.ResolutionStatus)
	}
	if got.ResolutionState != "RISK_RESOLUTION_STATE_SAFE" {
		t.Errorf("state = %q", got.ResolutionState)
	}
	if got.ResolutionNotes != "looked into it" {
		t.Errorf("notes = %q", got.ResolutionNotes)
	}

	// A full wire value passes through, so an API script needs no rewrite.
	got, err = BuildResolve("RISK_RESOLUTION_STATUS_DISMISSED", "UNSAFE", "")
	if err != nil {
		t.Fatalf("wire values should be accepted: %v", err)
	}
	if got.ResolutionStatus != "RISK_RESOLUTION_STATUS_DISMISSED" || got.ResolutionState != "RISK_RESOLUTION_STATE_UNSAFE" {
		t.Errorf("wire passthrough = %+v", got)
	}

	// Empty notes must not be sent as "" — omitempty keeps the body minimal.
	body, _ := json.Marshal(got)
	if strings.Contains(string(body), "resolutionNotes") {
		t.Errorf("empty notes should be omitted, got %s", body)
	}
}

func TestBuildResolve_Rejects(t *testing.T) {
	cases := []struct{ status, state, why string }{
		{"", "safe", "missing status"},
		{"resolved", "", "missing state"},
		{"bogus", "safe", "unknown status"},
		{"resolved", "bogus", "unknown state"},
		// The server reads UNSPECIFIED as an absent field and 400s with
		// "resolution_state is required", so offering it would be offering a
		// guaranteed failure.
		{"resolved", "RISK_RESOLUTION_STATE_UNSPECIFIED", "UNSPECIFIED state"},
		{"RISK_RESOLUTION_STATUS_UNSPECIFIED", "safe", "UNSPECIFIED status"},
		// The spec's enum advertises OPEN. The server refuses every transition
		// away from resolved, so it is not offered.
		{"RISK_RESOLUTION_STATUS_OPEN", "safe", "OPEN status"},
		{"open", "safe", "open short form"},
	}
	for _, c := range cases {
		if _, err := BuildResolve(c.status, c.state, ""); err == nil {
			t.Errorf("%s should be rejected", c.why)
		}
	}
}

func TestExpandLevel(t *testing.T) {
	if got, err := ExpandLevel("medium"); err != nil || got != "RISK_LEVEL_MEDIUM" {
		t.Errorf("ExpandLevel(medium) = %q, %v", got, err)
	}
	if got, err := ExpandLevel("RISK_LEVEL_CRITICAL"); err != nil || got != "RISK_LEVEL_CRITICAL" {
		t.Errorf("wire value should pass through, got %q, %v", got, err)
	}
	if _, err := ExpandLevel("catastrophic"); err == nil {
		t.Error("unknown level should be rejected")
	}
	// The error names the valid options, so a typo is self-correcting.
	_, err := ExpandLevel("catastrophic")
	for _, want := range []string{"low", "medium", "high", "critical"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should list %q: %v", want, err)
		}
	}
}

func TestErrAlreadyResolved(t *testing.T) {
	if !ErrAlreadyResolved(errors.New(`{"message":"risk event is already resolved","status":"FAILED_PRECONDITION"}`)) {
		t.Error("the server's refusal should be recognised")
	}
	if ErrAlreadyResolved(errors.New("connection reset")) {
		t.Error("an unrelated error must not be read as already-resolved")
	}
	if ErrAlreadyResolved(nil) {
		t.Error("nil is not already-resolved")
	}
}

// Each parser must distinguish "nothing there" from "could not read it".
// A caller that cannot tell those apart reports an org as clean when it is
// not — the defect class documented in
// docs/solutions/conventions/absent-data-is-not-evidence.
func TestParsers_EmptyVsUnreadable(t *testing.T) {
	t.Run("events", func(t *testing.T) {
		rows, total, err := ParseEvents(json.RawMessage(`{"riskEvents":[],"totalCount":0}`))
		if err != nil || len(rows) != 0 || total != 0 {
			t.Errorf("empty = %v, %d, %v", rows, total, err)
		}
		// totalCount is the server's match count, NOT len(rows).
		rows, total, err = ParseEvents(json.RawMessage(`{"riskEvents":[{"objectId":"a"}],"totalCount":2}`))
		if err != nil || len(rows) != 1 || total != 2 {
			t.Errorf("paged = %d rows, total %d, %v", len(rows), total, err)
		}
		if _, _, err := ParseEvents(json.RawMessage(`not json`)); err == nil {
			t.Error("an unreadable body must be an error, not an empty list")
		}
	})

	t.Run("identities", func(t *testing.T) {
		rows, total, err := ParseIdentities(json.RawMessage(`{"identities":[{"objectId":"a"}],"totalIdentitiesWithRisk":7,"trendsCount":3}`))
		if err != nil || len(rows) != 1 || total != 7 {
			t.Errorf("= %d rows, total %d, %v", len(rows), total, err)
		}
		if _, _, err := ParseIdentities(json.RawMessage(`[]`)); err == nil {
			t.Error("a bare array is not this envelope")
		}
	})

	t.Run("loginTypes", func(t *testing.T) {
		rows, err := ParseLoginTypes(json.RawMessage(`{"loginTypes":[{"loginResource":"x","count":1}]}`))
		if err != nil || len(rows) != 1 {
			t.Errorf("= %d, %v", len(rows), err)
		}
		if _, err := ParseLoginTypes(json.RawMessage(`{"loginTypes":"nope"}`)); err == nil {
			t.Error("a wrong-typed field must be an error")
		}
	})

	t.Run("geolocations", func(t *testing.T) {
		rows, err := ParseGeolocations(json.RawMessage(`{"items":[{"countryCode":"US","count":4}]}`))
		if err != nil || len(rows) != 1 {
			t.Errorf("= %d, %v", len(rows), err)
		}
		if _, err := ParseGeolocations(json.RawMessage(`{`)); err == nil {
			t.Error("a truncated body must be an error")
		}
	})
}

func TestParseTimeline(t *testing.T) {
	// The live shape: a map keyed by risk-factor type, flattened and sorted so
	// the output is a stable table rather than a tenant-dependent object.
	raw := json.RawMessage(`{"timelineData":{"RISK_FACTOR_TYPE_IMPOSSIBLE_TRAVEL":[{"t":1}],"RISK_FACTOR_TYPE_DORMANT_ACCOUNT_LOGIN":[{"t":1},{"t":2}]}}`)
	got, err := ParseTimeline(raw)
	if err != nil {
		t.Fatalf("ParseTimeline error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 series, got %d", len(got))
	}
	if got[0].RiskFactorType != "RISK_FACTOR_TYPE_DORMANT_ACCOUNT_LOGIN" {
		t.Errorf("series must be name-sorted, got %q first", got[0].RiskFactorType)
	}
	if len(got[0].Buckets) != 2 || len(got[1].Buckets) != 1 {
		t.Errorf("buckets = %d, %d", len(got[0].Buckets), len(got[1].Buckets))
	}

	empty, err := ParseTimeline(json.RawMessage(`{"timelineData":{}}`))
	if err != nil || len(empty) != 0 {
		t.Errorf("empty timeline = %v, %v", empty, err)
	}
	if _, err := ParseTimeline(json.RawMessage(`{"timelineData":[]}`)); err == nil {
		t.Error("a list where a map belongs must be an error")
	}
}

func TestIrreversibleWarning(t *testing.T) {
	// The warning is the whole reason resolve is classed destructive. If it
	// ever stops saying so, the classification has quietly lost its reason.
	for _, want := range []string{"cannot be undone", "reopening"} {
		if !strings.Contains(IrreversibleWarning, want) {
			t.Errorf("IrreversibleWarning should mention %q: %q", want, IrreversibleWarning)
		}
	}
}
