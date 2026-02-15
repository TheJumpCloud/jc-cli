package filter

import (
	"testing"
)

func TestParse_Equality(t *testing.T) {
	e, err := Parse("os=Mac OS X")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if e.Field != "os" {
		t.Errorf("field = %q, want %q", e.Field, "os")
	}
	if e.Operator != "eq" {
		t.Errorf("operator = %q, want %q", e.Operator, "eq")
	}
	if e.Value != "Mac OS X" {
		t.Errorf("value = %q, want %q", e.Value, "Mac OS X")
	}
}

func TestParse_Inequality(t *testing.T) {
	e, err := Parse("suspended!=true")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if e.Field != "suspended" || e.Operator != "ne" || e.Value != "true" {
		t.Errorf("got {%s, %s, %s}, want {suspended, ne, true}", e.Field, e.Operator, e.Value)
	}
}

func TestParse_GreaterThanOrEqual(t *testing.T) {
	e, err := Parse("created>=2026-01-01")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if e.Field != "created" || e.Operator != "gte" || e.Value != "2026-01-01" {
		t.Errorf("got {%s, %s, %s}, want {created, gte, 2026-01-01}", e.Field, e.Operator, e.Value)
	}
}

func TestParse_LessThanOrEqual(t *testing.T) {
	e, err := Parse("lastContact<=2026-01-15")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if e.Field != "lastContact" || e.Operator != "lte" || e.Value != "2026-01-15" {
		t.Errorf("got {%s, %s, %s}, want {lastContact, lte, 2026-01-15}", e.Field, e.Operator, e.Value)
	}
}

func TestParse_GreaterThan(t *testing.T) {
	e, err := Parse("count>10")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if e.Field != "count" || e.Operator != "gt" || e.Value != "10" {
		t.Errorf("got {%s, %s, %s}, want {count, gt, 10}", e.Field, e.Operator, e.Value)
	}
}

func TestParse_LessThan(t *testing.T) {
	e, err := Parse("age<30")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if e.Field != "age" || e.Operator != "lt" || e.Value != "30" {
		t.Errorf("got {%s, %s, %s}, want {age, lt, 30}", e.Field, e.Operator, e.Value)
	}
}

func TestParse_EmptyValue(t *testing.T) {
	e, err := Parse("department=")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if e.Field != "department" || e.Operator != "eq" || e.Value != "" {
		t.Errorf("got {%s, %s, %q}, want {department, eq, \"\"}", e.Field, e.Operator, e.Value)
	}
}

func TestParse_TrimWhitespace(t *testing.T) {
	e, err := Parse("  os  =  Mac OS X  ")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if e.Field != "os" || e.Value != "Mac OS X" {
		t.Errorf("got field=%q value=%q, want field=%q value=%q", e.Field, e.Value, "os", "Mac OS X")
	}
}

func TestParse_InvalidSyntax(t *testing.T) {
	_, err := Parse("justafieldname")
	if err == nil {
		t.Fatal("expected error for invalid syntax, got nil")
	}
	if want := "invalid filter"; !contains(err.Error(), want) {
		t.Errorf("error = %q, want to contain %q", err.Error(), want)
	}
}

func TestParse_MissingField(t *testing.T) {
	_, err := Parse("=value")
	if err == nil {
		t.Fatal("expected error for missing field, got nil")
	}
}

func TestToV1Query(t *testing.T) {
	tests := []struct {
		expr Expression
		want string
	}{
		{Expression{"os", "eq", "Mac OS X"}, "os:$eq:Mac OS X"},
		{Expression{"suspended", "ne", "true"}, "suspended:$ne:true"},
		{Expression{"created", "gte", "2026-01-01"}, "created:$gte:2026-01-01"},
		{Expression{"lastContact", "lte", "2026-01-15"}, "lastContact:$lte:2026-01-15"},
		{Expression{"count", "gt", "10"}, "count:$gt:10"},
		{Expression{"age", "lt", "30"}, "age:$lt:30"},
	}
	for _, tt := range tests {
		got := tt.expr.ToV1Query()
		if got != tt.want {
			t.Errorf("ToV1Query(%+v) = %q, want %q", tt.expr, got, tt.want)
		}
	}
}

func TestParseAll_Multiple(t *testing.T) {
	exprs, err := ParseAll([]string{"os=Mac OS X", "activated!=false"})
	if err != nil {
		t.Fatalf("ParseAll error: %v", err)
	}
	if len(exprs) != 2 {
		t.Fatalf("got %d expressions, want 2", len(exprs))
	}
	if exprs[0].Field != "os" || exprs[0].Operator != "eq" {
		t.Errorf("exprs[0] = {%s, %s}, want {os, eq}", exprs[0].Field, exprs[0].Operator)
	}
	if exprs[1].Field != "activated" || exprs[1].Operator != "ne" {
		t.Errorf("exprs[1] = {%s, %s}, want {activated, ne}", exprs[1].Field, exprs[1].Operator)
	}
}

func TestParseAll_Error(t *testing.T) {
	_, err := ParseAll([]string{"os=Mac", "badfilter"})
	if err == nil {
		t.Fatal("expected error for invalid filter in list, got nil")
	}
}

func TestParseAll_Empty(t *testing.T) {
	exprs, err := ParseAll([]string{})
	if err != nil {
		t.Fatalf("ParseAll error: %v", err)
	}
	if len(exprs) != 0 {
		t.Errorf("got %d expressions, want 0", len(exprs))
	}
}

func TestToV1Queries(t *testing.T) {
	exprs := []Expression{
		{"os", "eq", "Mac OS X"},
		{"suspended", "ne", "true"},
	}
	queries := ToV1Queries(exprs)
	if len(queries) != 2 {
		t.Fatalf("got %d queries, want 2", len(queries))
	}
	if queries[0] != "os:$eq:Mac OS X" {
		t.Errorf("queries[0] = %q, want %q", queries[0], "os:$eq:Mac OS X")
	}
	if queries[1] != "suspended:$ne:true" {
		t.Errorf("queries[1] = %q, want %q", queries[1], "suspended:$ne:true")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestToV2Query(t *testing.T) {
	tests := []struct {
		expr Expression
		want string
	}{
		{Expression{"name", "eq", "Engineering"}, "name:eq:Engineering"},
		{Expression{"type", "ne", "custom"}, "type:ne:custom"},
		{Expression{"created", "gte", "2026-01-01"}, "created:gte:2026-01-01"},
	}
	for _, tt := range tests {
		got := tt.expr.ToV2Query()
		if got != tt.want {
			t.Errorf("ToV2Query(%+v) = %q, want %q", tt.expr, got, tt.want)
		}
	}
}

func TestToV2Queries(t *testing.T) {
	exprs := []Expression{
		{"name", "eq", "Engineering"},
		{"type", "ne", "custom"},
	}
	queries := ToV2Queries(exprs)
	if len(queries) != 2 {
		t.Fatalf("got %d queries, want 2", len(queries))
	}
	if queries[0] != "name:eq:Engineering" {
		t.Errorf("queries[0] = %q, want %q", queries[0], "name:eq:Engineering")
	}
	if queries[1] != "type:ne:custom" {
		t.Errorf("queries[1] = %q, want %q", queries[1], "type:ne:custom")
	}
}

// --- Edge Cases ---

func TestParse_ValueWithColons(t *testing.T) {
	e, err := Parse("timestamp>=2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if e.Field != "timestamp" {
		t.Errorf("field = %q, want %q", e.Field, "timestamp")
	}
	if e.Operator != "gte" {
		t.Errorf("operator = %q, want %q", e.Operator, "gte")
	}
	if e.Value != "2026-01-01T00:00:00Z" {
		t.Errorf("value = %q, want %q", e.Value, "2026-01-01T00:00:00Z")
	}
}

func TestParse_ValueWithEquals(t *testing.T) {
	// Only the first = is the operator; rest is part of the value.
	e, err := Parse("query=name=john")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if e.Field != "query" {
		t.Errorf("field = %q, want %q", e.Field, "query")
	}
	if e.Operator != "eq" {
		t.Errorf("operator = %q, want %q", e.Operator, "eq")
	}
	if e.Value != "name=john" {
		t.Errorf("value = %q, want %q", e.Value, "name=john")
	}
}

func TestParse_UnicodeValue(t *testing.T) {
	e, err := Parse("name=Müller")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if e.Value != "Müller" {
		t.Errorf("value = %q, want %q", e.Value, "Müller")
	}
}

func TestParse_FieldWithDots(t *testing.T) {
	e, err := Parse("addresses.locality=Denver")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if e.Field != "addresses.locality" {
		t.Errorf("field = %q, want %q", e.Field, "addresses.locality")
	}
	if e.Value != "Denver" {
		t.Errorf("value = %q, want %q", e.Value, "Denver")
	}
}

func TestParse_OnlyOperator(t *testing.T) {
	// ">=" is matched as: `>=` at idx=0 fails (idx > 0 check), then `=` at idx=1
	// succeeds with field=">" and value="". This is a quirk of the parser.
	e, err := Parse(">=")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	// The ">" becomes the field name and "=" is the operator.
	if e.Field != ">" || e.Operator != "eq" || e.Value != "" {
		t.Errorf("got {%q, %q, %q}, want {>, eq, \"\"}", e.Field, e.Operator, e.Value)
	}
}

func TestParse_WhitespaceValue(t *testing.T) {
	e, err := Parse("field= ")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	// TrimSpace should produce empty value.
	if e.Value != "" {
		t.Errorf("value = %q, want empty string", e.Value)
	}
}

func TestParse_EmptyString(t *testing.T) {
	_, err := Parse("")
	if err == nil {
		t.Fatal("expected error for empty string, got nil")
	}
}

func TestToV1Query_SpecialChars(t *testing.T) {
	e := Expression{Field: "timestamp", Operator: "gte", Value: "2026-01-01T00:00:00Z"}
	got := e.ToV1Query()
	want := "timestamp:$gte:2026-01-01T00:00:00Z"
	if got != want {
		t.Errorf("ToV1Query = %q, want %q", got, want)
	}
}

func TestToV2Query_EmptyValue(t *testing.T) {
	e := Expression{Field: "department", Operator: "eq", Value: ""}
	got := e.ToV2Query()
	want := "department:eq:"
	if got != want {
		t.Errorf("ToV2Query = %q, want %q", got, want)
	}
}
