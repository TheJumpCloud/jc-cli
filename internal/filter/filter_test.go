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
