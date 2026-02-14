package mcp

import "testing"

func TestToolFilter_NoLists(t *testing.T) {
	f := newToolFilter(nil, nil)
	if !f.isAllowed("users_list") {
		t.Error("expected all tools allowed when no lists configured")
	}
	if !f.isAllowed("devices_erase") {
		t.Error("expected all tools allowed when no lists configured")
	}
}

func TestToolFilter_BlockList(t *testing.T) {
	f := newToolFilter(nil, []string{"devices_erase", "devices_lock"})
	if !f.isAllowed("users_list") {
		t.Error("expected users_list allowed")
	}
	if f.isAllowed("devices_erase") {
		t.Error("expected devices_erase blocked")
	}
	if f.isAllowed("devices_lock") {
		t.Error("expected devices_lock blocked")
	}
	if !f.isAllowed("devices_list") {
		t.Error("expected devices_list allowed")
	}
}

func TestToolFilter_AllowList(t *testing.T) {
	f := newToolFilter([]string{"users_list", "users_get"}, nil)
	if !f.isAllowed("users_list") {
		t.Error("expected users_list allowed")
	}
	if !f.isAllowed("users_get") {
		t.Error("expected users_get allowed")
	}
	if f.isAllowed("users_delete") {
		t.Error("expected users_delete blocked (not in allow list)")
	}
	if f.isAllowed("devices_list") {
		t.Error("expected devices_list blocked (not in allow list)")
	}
}

func TestToolFilter_WildcardAllow(t *testing.T) {
	f := newToolFilter([]string{"users_*"}, nil)
	if !f.isAllowed("users_list") {
		t.Error("expected users_list allowed by wildcard")
	}
	if !f.isAllowed("users_delete") {
		t.Error("expected users_delete allowed by wildcard")
	}
	if f.isAllowed("devices_list") {
		t.Error("expected devices_list blocked (not matching allow pattern)")
	}
}

func TestToolFilter_WildcardBlock(t *testing.T) {
	f := newToolFilter(nil, []string{"devices_*"})
	if !f.isAllowed("users_list") {
		t.Error("expected users_list allowed")
	}
	if f.isAllowed("devices_list") {
		t.Error("expected devices_list blocked by wildcard")
	}
	if f.isAllowed("devices_erase") {
		t.Error("expected devices_erase blocked by wildcard")
	}
}

func TestToolFilter_BlockPrecedence(t *testing.T) {
	// Allow all users tools, but block users_delete specifically.
	f := newToolFilter([]string{"users_*"}, []string{"users_delete"})
	if !f.isAllowed("users_list") {
		t.Error("expected users_list allowed")
	}
	if !f.isAllowed("users_get") {
		t.Error("expected users_get allowed")
	}
	if f.isAllowed("users_delete") {
		t.Error("expected users_delete blocked (block takes precedence)")
	}
}

func TestToolFilter_MultiplePatterns(t *testing.T) {
	f := newToolFilter([]string{"users_*", "devices_list", "jc_ping"}, nil)
	if !f.isAllowed("users_list") {
		t.Error("expected users_list allowed")
	}
	if !f.isAllowed("devices_list") {
		t.Error("expected devices_list allowed")
	}
	if !f.isAllowed("jc_ping") {
		t.Error("expected jc_ping allowed")
	}
	if f.isAllowed("devices_erase") {
		t.Error("expected devices_erase blocked")
	}
}

func TestToolFilter_EmptyAllowList(t *testing.T) {
	// Empty allow list (not nil) should still allow everything.
	f := newToolFilter([]string{}, nil)
	if !f.isAllowed("users_list") {
		t.Error("expected users_list allowed with empty allow list")
	}
}

func TestToolFilter_EmptyBlockList(t *testing.T) {
	// Empty block list should not block anything.
	f := newToolFilter(nil, []string{})
	if !f.isAllowed("users_list") {
		t.Error("expected users_list allowed with empty block list")
	}
}
