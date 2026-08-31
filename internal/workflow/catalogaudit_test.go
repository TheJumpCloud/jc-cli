package workflow

import (
	"strings"
	"testing"
)

// The direction that matters: a type the tenant emits and the catalog does not
// know. Those are the ones that make validate warn on a legitimate trigger.
func TestAuditCatalog_ReportsEmittedButUnknown(t *testing.T) {
	got := AuditCatalog(map[string]int{
		"user_create":        50, // documented
		"zz_not_a_real_type": 3,  // gap
		"zz_another_missing": 99, // bigger gap
	}, "30d")

	if got.Emitted != 3 {
		t.Errorf("Emitted = %d, want 3", got.Emitted)
	}
	if got.Covered != 1 {
		t.Errorf("Covered = %d, want 1", got.Covered)
	}
	if len(got.Gaps) != 2 {
		t.Fatalf("gaps = %+v, want 2", got.Gaps)
	}
	// Loudest first: a type emitted 99 times is a bigger hole than one seen 3.
	if got.Gaps[0].EventType != "zz_another_missing" {
		t.Errorf("gaps must sort by volume, got %s first", got.Gaps[0].EventType)
	}
	if got.Gaps[0].Count != 99 {
		t.Errorf("count = %d, want 99", got.Gaps[0].Count)
	}
}

// A catalog entry with no observed event is NOT drift — it usually means the
// tenant does not use that feature. Reporting it would bury the direction that
// matters under hundreds of rows.
func TestAuditCatalog_DoesNotReportUnusedCatalogEntries(t *testing.T) {
	got := AuditCatalog(map[string]int{"user_create": 1}, "7d")
	if len(got.Gaps) != 0 {
		t.Errorf("only emitted-but-unknown is a gap: %+v", got.Gaps)
	}
	if got.Known < 300 {
		t.Errorf("catalog size = %d, looks wrong", got.Known)
	}
}

// An empty result must not read as proof the catalog is complete: a quiet
// tenant emits few types.
func TestAuditCatalog_NoteQualifiesAnEmptyResult(t *testing.T) {
	got := AuditCatalog(map[string]int{}, "1d")
	if len(got.Gaps) != 0 {
		t.Error("no observations means no gaps")
	}
	if got.Note == "" {
		t.Fatal("the note is what stops an empty result reading as a clean bill of health")
	}
	for _, want := range []string{"only as strong as the window", "reverse direction is not reported"} {
		if !strings.Contains(got.Note, want) {
			t.Errorf("the note should say %q: %s", want, got.Note)
		}
	}
}

// The gaps this audit found on a live tenant are now closed, so a future
// regeneration that drops them fails here rather than silently reopening the
// hole.
func TestAuditCatalog_PreviouslyFoundGapsAreClosed(t *testing.T) {
	// Verified emitted on org 5ec71e8e96bfda0611fc6c5b over 30 days.
	for _, typ := range []string{
		"software_status_update", "ldap_srch", "policy_result", "command_result",
		"slack_notification_sent", "bulk_update_alerts", "bulk_delete_alerts",
		"attributemappings_add", "attributemappings_update", "attributemappings_delete",
		"rule_config_created", "rule_config_updated", "rule_config_deleted",
		"saas_management_application_review", "radius_auth_attempt", "workflow_update",
	} {
		if _, known := LookupEventType(typ); !known {
			t.Errorf("%q is emitted by a real tenant and must stay in the catalog", typ)
		}
	}
}

// The documentation lists ldap_search; the tenant emits ldap_srch, 240 times in
// 30 days, and never the documented spelling. A workflow triggering on the
// documented name would silently never fire — the exact failure this catalog
// exists to prevent, caused by the catalog itself.
func TestAuditCatalog_LdapSrchIsTheEmittedSpelling(t *testing.T) {
	e, known := LookupEventType("ldap_srch")
	if !known {
		t.Fatal("ldap_srch is what the tenant actually emits")
	}
	if !strings.Contains(e.Describe, "ldap_search") {
		t.Errorf("the entry should warn about the documented misspelling: %q", e.Describe)
	}
}
