package audit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// driftedRecords returns records that are valid JSON but no longer the
// shape the checks decode into — here, the endpoint began returning bare
// identifiers instead of objects. A scalar is hostile to every check's
// struct, which is what makes it the right fixture for the table test
// below: checks decode narrow, differing field subsets, so a fixture
// that only drifts one field leaves some checks decoding happily and the
// test passing for the wrong reason.
func driftedRecords() []json.RawMessage {
	return []json.RawMessage{
		json.RawMessage(`"5ec9ce0000c9510e358c9918"`),
		json.RawMessage(`"5ec9ce0000c9510e358c9919"`),
	}
}

func allDrifted(now time.Time) *Data {
	r := driftedRecords()
	return &Data{
		Users: r, Admins: r, Devices: r, UserGroups: r,
		SystemGroups: r, AuthPolicies: r, IPLists: r, Now: now,
	}
}

// The narrower, real drift: a field the check reads as a string starts
// arriving as an object. This is what Password Manager's group field did
// — a []string that began arriving as objects made every record fail to
// decode, and the CLI reported "0 users are enrolled" on a populated
// tenant. Here the same shape must reach the operator as an error.
func TestCheckUsersWithoutMFA_FieldTypeDriftIsReported(t *testing.T) {
	d := &Data{Now: time.Now().UTC(), Users: []json.RawMessage{
		json.RawMessage(`{"_id":"u1","username":{"given":"ada","family":"lovelace"},` +
			`"email":"ada@example.com","activated":true}`),
	}}
	findings, err := checkByID(t, "users-without-mfa").Run(context.Background(), d)
	if err == nil {
		t.Fatalf("a username that changed from string to object was skipped, "+
			"leaving %d findings — the tenant would look fully enrolled", len(findings))
	}
}

// Every check that reads a record must report a decode failure rather
// than skipping the record. A skipped record shortens the list, and a
// short list is indistinguishable from a clean org.
func TestChecks_ReportDriftedRecordsRatherThanSkippingThem(t *testing.T) {
	d := allDrifted(time.Now().UTC())
	for _, c := range All() {
		t.Run(c.ID, func(t *testing.T) {
			findings, err := c.Run(context.Background(), d)
			if err == nil {
				t.Fatalf("check %q silently accepted a record it could not decode "+
					"and returned %d findings — a schema change would render as "+
					"a clean audit", c.ID, len(findings))
			}
			if !strings.Contains(err.Error(), c.ID) {
				t.Errorf("error should name the check that failed, got: %v", err)
			}
		})
	}
}

// The consequence, stated in the runner's own terms: a run over data no
// check can decode must not look like a clean run. This is the assertion
// that actually protects the operator — TestChecks_ReportDrifted… above
// checks the mechanism, this one checks the outcome.
func TestRun_UndecodableDataDoesNotRenderAsACleanAudit(t *testing.T) {
	results, err := Run(context.Background(), allDrifted(time.Now().UTC()), RunOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !AnyCheckError(results) {
		t.Fatal("a run where no record decoded reported no check errors — " +
			"`jc audit` would print \"OK — checks ran clean, no findings\" " +
			"and --exit-code would green-light a CI gate")
	}
	for _, r := range results {
		if r.Error == "" {
			t.Errorf("check %q ran clean on undecodable data (%d findings)", r.CheckID, len(r.Findings))
		}
	}
}

// A timestamp that stops being RFC3339 has the same blast radius as a
// decode failure: it applies to every record at once, so the check
// reports zero and the fleet looks healthy.
func TestChecks_UnparseableTimestampIsAnErrorNotASkip(t *testing.T) {
	cases := []struct {
		check string
		data  *Data
	}{
		{"stale-devices", &Data{Devices: []json.RawMessage{
			json.RawMessage(`{"_id":"d1","hostname":"host-42","os":"Mac OS X","lastContact":"2026/01/02 15:04"}`)}}},
		{"password-age", &Data{Users: []json.RawMessage{
			json.RawMessage(`{"_id":"u1","username":"ada","activated":true,"password_date":"01-02-2026"}`)}}},
		{"recently-created-admins", &Data{Admins: []json.RawMessage{
			json.RawMessage(`{"_id":"a1","email":"a@b.c","created":"yesterday"}`)}}},
	}
	for _, tc := range cases {
		t.Run(tc.check, func(t *testing.T) {
			tc.data.Now = time.Now().UTC()
			c := checkByID(t, tc.check)
			if _, err := c.Run(context.Background(), tc.data); err == nil {
				t.Fatalf("check %q skipped a record whose timestamp would not parse — "+
					"a format change would report zero findings", tc.check)
			}
		})
	}
}

// The counterpart, and the reason parseAuditTime does not simply reject
// everything: an EMPTY timestamp is not drift. It means the field was
// never set, each check already decides what that means, and turning it
// into an error would make a normal org fail its audit.
func TestChecks_EmptyTimestampStaysALegitimateSkip(t *testing.T) {
	cases := []struct {
		check string
		data  *Data
	}{
		{"stale-devices", &Data{Devices: []json.RawMessage{
			json.RawMessage(`{"_id":"d1","hostname":"host-42","os":"Mac OS X","lastContact":""}`)}}},
		{"password-age", &Data{Users: []json.RawMessage{
			json.RawMessage(`{"_id":"u1","username":"ada","activated":true,"password_date":""}`)}}},
		{"recently-created-admins", &Data{Admins: []json.RawMessage{
			json.RawMessage(`{"_id":"a1","email":"a@b.c","created":""}`)}}},
	}
	for _, tc := range cases {
		t.Run(tc.check, func(t *testing.T) {
			tc.data.Now = time.Now().UTC()
			c := checkByID(t, tc.check)
			findings, err := c.Run(context.Background(), tc.data)
			if err != nil {
				t.Fatalf("an unset timestamp is not a schema change and must not "+
					"fail the check, got: %v", err)
			}
			if len(findings) != 0 {
				t.Errorf("an unset timestamp should yield no finding, got %d", len(findings))
			}
		})
	}
}

// The decode error has to say enough for an operator to act: which
// check, which record, and that the count is not trustworthy. "cannot
// unmarshal" alone sends them looking for a bug in their directory.
func TestDecodeRecord_ErrorNamesTheCheckAndTheRisk(t *testing.T) {
	var v struct {
		Username string `json:"username"`
	}
	err := decodeRecord("users-without-mfa", "user", 3, 7,
		json.RawMessage(`{"username":{"given":"ada"}}`), &v)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"users-without-mfa", "user record 4 of 7", "fewer findings than exist"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q, got: %v", want, err)
		}
	}
}

func checkByID(t *testing.T, id string) AuditCheck {
	t.Helper()
	for _, c := range All() {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no registered check %q", id)
	return AuditCheck{}
}
