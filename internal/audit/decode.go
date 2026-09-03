package audit

import (
	"encoding/json"
	"fmt"
	"time"
)

// decodeRecord unmarshals one record from an audit fetch, turning a
// schema mismatch into a check error instead of a shorter list.
//
// Skipping a record that will not decode is the most dangerous thing an
// audit check can do. It converts "I could not read this data" into "I
// found nothing wrong": if the user record shape drifts, every record
// fails to decode, checkUsersWithoutMFA emits zero findings, and the
// report renders "OK — checks ran clean, no findings", which an operator
// reads as every active user having MFA. A short list is a quiet wrong
// answer; a check error is a loud one.
//
// The runner already draws exactly this distinction — CheckResult.Error
// renders as [ERR], suppresses the "ran clean" line, and fails
// --exit-code via AnyCheckError. The checks only have to report it.
func decodeRecord(checkID, kind string, i, n int, raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("%s: %s record %d of %d did not decode: %w — the record "+
			"shape has changed, and this check would otherwise report fewer "+
			"findings than exist", checkID, kind, i+1, n, err)
	}
	return nil
}

// parseAuditTime parses a timestamp off an audit record, turning an
// unparseable value into a check error rather than a skipped record.
//
// Same reasoning as decodeRecord, and the same blast radius: a format
// change applies to every record at once, so stale-devices would report
// zero stale devices — indistinguishable from a fleet that is checking
// in normally.
//
// An EMPTY value is not an error. It legitimately means the field was
// never set, and each caller decides what that means for its check; only
// a non-empty value that will not parse is a schema surprise. Callers
// must therefore test for "" before calling this.
func parseAuditTime(checkID, kind, field, value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: %s %s=%q is not RFC3339: %w — the "+
			"timestamp format has changed, and this check would otherwise report "+
			"fewer findings than exist", checkID, kind, field, value, err)
	}
	return t, nil
}
