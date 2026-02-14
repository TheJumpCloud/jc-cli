package filter

import (
	"fmt"
	"strings"
)

// FilterError is a structured error for invalid filter expressions.
type FilterError struct {
	Expression string // the raw filter expression that failed
	Message    string // human-readable error message
}

func (e *FilterError) Error() string {
	return e.Message
}

// Expression represents a parsed filter expression with field, operator, and value.
type Expression struct {
	Field    string
	Operator string // One of: eq, ne, gt, gte, lt, lte
	Value    string
}

// operatorMap maps user-friendly operators to V1 API operators.
var operatorMap = map[string]string{
	"!=": "ne",
	">=": "gte",
	"<=": "lte",
	">":  "gt",
	"<":  "lt",
	"=":  "eq",
}

// orderedOps lists operators in order of specificity (longest first)
// so that ">=" is matched before ">".
var orderedOps = []string{"!=", ">=", "<=", ">", "<", "="}

// Parse parses a user-friendly filter expression like "field=value"
// or "field>=value" into an Expression.
func Parse(expr string) (Expression, error) {
	for _, op := range orderedOps {
		idx := strings.Index(expr, op)
		if idx > 0 {
			field := strings.TrimSpace(expr[:idx])
			value := strings.TrimSpace(expr[idx+len(op):])
			if field == "" {
				return Expression{}, &FilterError{
					Expression: expr,
					Message:    fmt.Sprintf("invalid filter %q: missing field name", expr),
				}
			}
			return Expression{
				Field:    field,
				Operator: operatorMap[op],
				Value:    value,
			}, nil
		}
	}
	return Expression{}, &FilterError{
		Expression: expr,
		Message:    fmt.Sprintf("invalid filter %q: expected format 'field=value', 'field!=value', 'field>=value', 'field<=value', 'field>value', or 'field<value'", expr),
	}
}

// ToV1Query converts a filter expression to the JumpCloud V1 API
// filter format: "field:$op:value".
func (e Expression) ToV1Query() string {
	return fmt.Sprintf("%s:$%s:%s", e.Field, e.Operator, e.Value)
}

// ParseAll parses multiple filter expressions and returns them all.
func ParseAll(exprs []string) ([]Expression, error) {
	result := make([]Expression, 0, len(exprs))
	for _, expr := range exprs {
		e, err := Parse(expr)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

// ToV1Queries converts multiple filter expressions to V1 API filter
// query parameter values.
func ToV1Queries(exprs []Expression) []string {
	result := make([]string, len(exprs))
	for i, e := range exprs {
		result[i] = e.ToV1Query()
	}
	return result
}

// ToV2Query converts a filter expression to the JumpCloud V2 API
// filter format: "field:op:value" (no $ prefix on operator).
func (e Expression) ToV2Query() string {
	return fmt.Sprintf("%s:%s:%s", e.Field, e.Operator, e.Value)
}

// ToV2Queries converts multiple filter expressions to V2 API filter
// query parameter values.
func ToV2Queries(exprs []Expression) []string {
	result := make([]string, len(exprs))
	for i, e := range exprs {
		result[i] = e.ToV2Query()
	}
	return result
}
