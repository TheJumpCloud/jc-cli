package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Data is the shared snapshot every check reads from. The fetcher
// populates it once, in parallel, before the run loop starts — so 12
// checks against the same admin list don't re-fetch admins 12 times.
//
// A nil slice on any field means that sub-fetch failed; checks that
// require that data must guard against it and surface a clear error so
// the result table can distinguish "ran clean" from "couldn't run."
// Failures land in Warnings (non-fatal) — only an all-empty fetch
// causes Fetch to return an error.
type Data struct {
	Users        []json.RawMessage
	Admins       []json.RawMessage
	Devices      []json.RawMessage
	UserGroups   []json.RawMessage
	SystemGroups []json.RawMessage
	AuthPolicies []json.RawMessage
	IPLists      []json.RawMessage
	Now          time.Time
	Warnings     []string
}

// Fetcher abstracts the JumpCloud API surface the audit needs. The CLI
// provides a real client-backed implementation; tests provide a stub.
// Each method is invoked from its own goroutine, so implementations
// must be goroutine-safe (the api.V1Client/V2Client both are).
type Fetcher interface {
	Users(ctx context.Context) ([]json.RawMessage, error)
	Admins(ctx context.Context) ([]json.RawMessage, error)
	Devices(ctx context.Context) ([]json.RawMessage, error)
	UserGroups(ctx context.Context) ([]json.RawMessage, error)
	SystemGroups(ctx context.Context) ([]json.RawMessage, error)
	AuthPolicies(ctx context.Context) ([]json.RawMessage, error)
	IPLists(ctx context.Context) ([]json.RawMessage, error)
}

// nowFn is overridable so tests can pin "now" for deterministic
// age-based checks (stale devices, password age, recently-created
// admins).
var nowFn = time.Now

// Fetch runs every Fetcher method in parallel and aggregates the
// results into a Data bundle. Same best-effort contract as the MCP
// compliance_view fetcher: a transient sub-fetch failure surfaces as a
// Warning rather than blocking the snapshot. Only if *every* sub-fetch
// fails does Fetch itself return an error — at that point there's
// nothing the audit can usefully report on.
func Fetch(ctx context.Context, f Fetcher) (*Data, error) {
	d := &Data{Now: nowFn().UTC()}

	var (
		mu       sync.Mutex
		warnings []string
	)
	warn := func(format string, args ...any) {
		mu.Lock()
		warnings = append(warnings, fmt.Sprintf(format, args...))
		mu.Unlock()
	}

	type fetchJob struct {
		name string
		run  func() ([]json.RawMessage, error)
		set  func([]json.RawMessage)
	}
	jobs := []fetchJob{
		{"users", func() ([]json.RawMessage, error) { return f.Users(ctx) }, func(v []json.RawMessage) { d.Users = v }},
		{"admins", func() ([]json.RawMessage, error) { return f.Admins(ctx) }, func(v []json.RawMessage) { d.Admins = v }},
		{"devices", func() ([]json.RawMessage, error) { return f.Devices(ctx) }, func(v []json.RawMessage) { d.Devices = v }},
		{"user groups", func() ([]json.RawMessage, error) { return f.UserGroups(ctx) }, func(v []json.RawMessage) { d.UserGroups = v }},
		{"system groups", func() ([]json.RawMessage, error) { return f.SystemGroups(ctx) }, func(v []json.RawMessage) { d.SystemGroups = v }},
		{"auth policies", func() ([]json.RawMessage, error) { return f.AuthPolicies(ctx) }, func(v []json.RawMessage) { d.AuthPolicies = v }},
		{"ip lists", func() ([]json.RawMessage, error) { return f.IPLists(ctx) }, func(v []json.RawMessage) { d.IPLists = v }},
	}

	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j fetchJob) {
			defer wg.Done()
			rows, err := j.run()
			if err != nil {
				warn("listing %s: %v", j.name, err)
				return
			}
			mu.Lock()
			j.set(rows)
			mu.Unlock()
		}(j)
	}
	wg.Wait()

	if len(warnings) > 0 {
		d.Warnings = warnings
	}

	if d.Users == nil && d.Admins == nil && d.Devices == nil &&
		d.UserGroups == nil && d.SystemGroups == nil &&
		d.AuthPolicies == nil && d.IPLists == nil {
		return nil, fmt.Errorf("all audit sub-fetches failed: %v", warnings)
	}

	return d, nil
}
