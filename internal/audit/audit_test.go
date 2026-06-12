package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestSeverity_AtLeast(t *testing.T) {
	if !SeverityCritical.AtLeast(SeverityLow) {
		t.Error("critical should be >= low")
	}
	if SeverityLow.AtLeast(SeverityCritical) {
		t.Error("low should not be >= critical")
	}
	if !SeverityHigh.AtLeast(SeverityHigh) {
		t.Error("equal severities should compare >=")
	}
	// Unknown severity ranks at 0 — should never satisfy any real threshold.
	if Severity("bogus").AtLeast(SeverityInfo) {
		t.Error("unknown severity should not satisfy info threshold")
	}
}

func TestCheckResult_MaxSeverity(t *testing.T) {
	r := CheckResult{
		Findings: []Finding{
			{Severity: SeverityLow},
			{Severity: SeverityCritical},
			{Severity: SeverityMedium},
		},
	}
	if r.MaxSeverity() != SeverityCritical {
		t.Errorf("got %v, want critical", r.MaxSeverity())
	}
	if (CheckResult{}.MaxSeverity()) != "" {
		t.Error("empty findings should yield empty max")
	}
}

func TestRegister_DuplicateIDPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate ID")
		}
	}()
	// admins-without-mfa is already registered by init()
	Register(AuditCheck{
		ID:       "admins-without-mfa",
		Title:    "dup",
		Category: CategorySecurity,
		Run:      func(context.Context, *Data) ([]Finding, error) { return nil, nil },
	})
}

func TestRun_CategoryFilter(t *testing.T) {
	d := &Data{Admins: []json.RawMessage{}}
	res, err := Run(context.Background(), d, RunOptions{Categories: []Category{CategoryCompliance}})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	for _, r := range res {
		if r.Category != CategoryCompliance {
			t.Errorf("category filter leaked: got %v", r.Category)
		}
	}
	if len(res) == 0 {
		t.Error("expected at least one compliance result")
	}
}

func TestRun_SeverityFilter(t *testing.T) {
	// Synthesize a Data set where admins-without-mfa would emit a CRITICAL
	// finding. Filtering at min=critical should keep it; min=info+ keeps it
	// too; only an unsatisfiable threshold drops it.
	d := &Data{Admins: []json.RawMessage{
		mustMarshal(map[string]any{"_id": "1", "email": "a@x", "enableMultiFactor": false}),
	}}
	res, err := Run(context.Background(), d, RunOptions{
		Categories:  []Category{CategorySecurity},
		MinSeverity: SeverityCritical,
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	var got int
	for _, r := range res {
		got += len(r.Findings)
	}
	if got == 0 {
		t.Error("expected at least one CRITICAL finding to survive min=critical filter")
	}
}

func TestRun_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, &Data{}, RunOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestAnyFindingAtLeast(t *testing.T) {
	results := []CheckResult{
		{Findings: []Finding{{Severity: SeverityLow}}},
		{Findings: []Finding{{Severity: SeverityHigh}}},
	}
	if !AnyFindingAtLeast(results, SeverityHigh) {
		t.Error("should detect HIGH finding")
	}
	if AnyFindingAtLeast(results, SeverityCritical) {
		t.Error("should not falsely report CRITICAL")
	}
	if AnyFindingAtLeast(results, Severity("bogus")) {
		t.Error("bogus threshold should never match")
	}
}

func TestAnyCheckError(t *testing.T) {
	// Regression guard for the Bugbot PR #47 finding: a check that
	// errored is a degraded result, not a clean run. --exit-code must
	// treat this distinctly from "no findings."
	if AnyCheckError([]CheckResult{{Findings: nil}}) {
		t.Error("clean result should not register as error")
	}
	if !AnyCheckError([]CheckResult{{Error: "admins fetch unavailable"}}) {
		t.Error("errored check should be detected")
	}
	if !AnyCheckError([]CheckResult{
		{Findings: []Finding{{Severity: SeverityLow}}},
		{Error: "users fetch unavailable"},
	}) {
		t.Error("mixed result with one error should be detected")
	}
}

func TestSortByCategoryAndSeverity(t *testing.T) {
	results := []CheckResult{
		{CheckID: "z", Category: CategoryHygiene, Findings: []Finding{{Severity: SeverityLow}}},
		{CheckID: "a", Category: CategorySecurity, Findings: []Finding{{Severity: SeverityCritical}}},
		{CheckID: "b", Category: CategorySecurity, Findings: []Finding{{Severity: SeverityLow}}},
	}
	SortByCategoryAndSeverity(results)
	// security/critical first, security/low next, hygiene last
	if results[0].CheckID != "a" || results[1].CheckID != "b" || results[2].CheckID != "z" {
		t.Errorf("sort order wrong: %v %v %v", results[0].CheckID, results[1].CheckID, results[2].CheckID)
	}
}

// ─── per-check tests ───────────────────────────────────────────────

func TestCheckAdminsWithoutMFA(t *testing.T) {
	d := &Data{Admins: []json.RawMessage{
		mustMarshal(map[string]any{"_id": "1", "email": "good@x", "enableMultiFactor": true, "totpEnrolled": true}),
		mustMarshal(map[string]any{"_id": "2", "email": "bad@x", "enableMultiFactor": false}),
	}}
	findings, err := checkAdminsWithoutMFA(context.Background(), d)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != SeverityCritical {
		t.Errorf("expected CRITICAL, got %v", findings[0].Severity)
	}
	if findings[0].ResourceRef != "admin:bad@x" {
		t.Errorf("wrong resource_ref: %s", findings[0].ResourceRef)
	}
}

func TestCheckAdminsWithoutMFA_NilDataReturnsError(t *testing.T) {
	_, err := checkAdminsWithoutMFA(context.Background(), &Data{})
	if err == nil {
		t.Error("expected error when admins fetch unavailable")
	}
}

func TestMFAAdoptionSeverity(t *testing.T) {
	tests := []struct {
		pct  float64
		want Severity
	}{
		{30, SeverityCritical},
		{60, SeverityHigh},
		{85, SeverityMedium},
		{95, ""},
		{100, ""},
	}
	for _, tc := range tests {
		if got := mfaAdoptionSeverity(tc.pct); got != tc.want {
			t.Errorf("pct=%v: got %v, want %v", tc.pct, got, tc.want)
		}
	}
}

func TestCheckStaleDevices_UsesDataNow(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	d := &Data{
		Now: now,
		Devices: []json.RawMessage{
			mustMarshal(map[string]any{"_id": "1", "hostname": "fresh", "os": "Mac OS X", "lastContact": "2026-05-25T00:00:00Z"}),
			mustMarshal(map[string]any{"_id": "2", "hostname": "stale", "os": "Mac OS X", "lastContact": "2026-04-01T00:00:00Z"}),
			mustMarshal(map[string]any{"_id": "3", "hostname": "no-contact", "os": "Mac OS X"}),
		},
	}
	findings, err := checkStaleDevices(context.Background(), d)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 stale finding, got %d", len(findings))
	}
	if findings[0].ResourceRef != "device:stale" {
		t.Errorf("wrong device flagged: %s", findings[0].ResourceRef)
	}
}

// ─── data fetcher ──────────────────────────────────────────────────

type stubFetcher struct {
	usersErr error
	calls    atomic.Int32
}

func (s *stubFetcher) noop(_ context.Context) ([]json.RawMessage, error) {
	s.calls.Add(1)
	return []json.RawMessage{}, nil
}

func (s *stubFetcher) Users(ctx context.Context) ([]json.RawMessage, error) {
	s.calls.Add(1)
	if s.usersErr != nil {
		return nil, s.usersErr
	}
	return []json.RawMessage{[]byte(`{"_id":"1"}`)}, nil
}
func (s *stubFetcher) Admins(ctx context.Context) ([]json.RawMessage, error) {
	return s.noop(ctx)
}
func (s *stubFetcher) Devices(ctx context.Context) ([]json.RawMessage, error) {
	return s.noop(ctx)
}
func (s *stubFetcher) UserGroups(ctx context.Context) ([]json.RawMessage, error) {
	return s.noop(ctx)
}
func (s *stubFetcher) SystemGroups(ctx context.Context) ([]json.RawMessage, error) {
	return s.noop(ctx)
}
func (s *stubFetcher) AuthPolicies(ctx context.Context) ([]json.RawMessage, error) {
	return s.noop(ctx)
}
func (s *stubFetcher) IPLists(ctx context.Context) ([]json.RawMessage, error) {
	return s.noop(ctx)
}

func TestFetch_ParallelSubFetches(t *testing.T) {
	s := &stubFetcher{}
	d, err := Fetch(context.Background(), s)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if d.Users == nil || len(d.Users) != 1 {
		t.Error("users not populated")
	}
	if s.calls.Load() != 7 {
		t.Errorf("expected 7 fetcher calls, got %d", s.calls.Load())
	}
}

func TestFetch_SoftFailuresBecomeWarnings(t *testing.T) {
	s := &stubFetcher{usersErr: fmt.Errorf("upstream 503")}
	d, err := Fetch(context.Background(), s)
	if err != nil {
		t.Fatalf("Fetch should not fail when at least one sub-fetch succeeds: %v", err)
	}
	if d.Users != nil {
		t.Error("expected nil users on fetch failure")
	}
	if len(d.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(d.Warnings))
	}
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
