package mcp

import "path/filepath"

// toolFilter determines whether a tool name is allowed based on
// allow/block list patterns. Patterns use glob-style matching via
// filepath.Match (e.g., "users_*", "devices_erase").
//
// Rules:
//   - If allowed is non-empty, only tools matching at least one allow pattern are permitted.
//   - If blocked is non-empty, tools matching any block pattern are denied.
//   - Block takes precedence over allow.
//   - Empty lists mean no restriction for that direction.
type toolFilter struct {
	allowed []string
	blocked []string
}

// newToolFilter creates a filter from allow/block pattern lists.
func newToolFilter(allowed, blocked []string) *toolFilter {
	return &toolFilter{
		allowed: allowed,
		blocked: blocked,
	}
}

// isAllowed returns true if the named tool should be registered.
func (f *toolFilter) isAllowed(name string) bool {
	// Check block list first (takes precedence).
	for _, pattern := range f.blocked {
		if matched, _ := filepath.Match(pattern, name); matched {
			return false
		}
	}

	// If no allow list, everything not blocked is allowed.
	if len(f.allowed) == 0 {
		return true
	}

	// Check allow list.
	for _, pattern := range f.allowed {
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
	}

	return false
}
