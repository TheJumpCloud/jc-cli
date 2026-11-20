package direports

import "testing"

func TestRequestPath(t *testing.T) {
	if p, err := RequestPath("users-to-user-groups"); err != nil || p != "/reports/users-to-user-groups" {
		t.Fatalf("got %s %v", p, err)
	}
	if _, err := RequestPath("nope"); err == nil {
		t.Error("an unknown type must be refused")
	}
}

func TestPickArtifact(t *testing.T) {
	rep := Report{ID: "r", Status: "COMPLETED", Artifacts: []Artifact{{"a1", "JSON"}, {"a2", "CSV"}}}
	if a, err := PickArtifact(rep, "csv"); err != nil || a.ID != "a2" {
		t.Errorf("by format: %v %v", a, err)
	}
	if a, err := PickArtifact(rep, "a1"); err != nil || a.Format != "JSON" {
		t.Errorf("by id: %v %v", a, err)
	}
	if _, err := PickArtifact(rep, "xml"); err == nil {
		t.Error("missing format must be an error")
	}
	if _, err := PickArtifact(Report{ID: "r", Status: "FAILED"}, "csv"); err == nil {
		t.Error("a failed report must not be downloadable")
	}
}
