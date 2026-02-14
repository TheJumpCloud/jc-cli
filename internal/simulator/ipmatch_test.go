package simulator

import "testing"

func TestMatchIP_SingleIP(t *testing.T) {
	entries := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}

	if !MatchIP("10.0.0.1", entries) {
		t.Error("10.0.0.1 should match")
	}
	if !MatchIP("10.0.0.3", entries) {
		t.Error("10.0.0.3 should match")
	}
	if MatchIP("10.0.0.4", entries) {
		t.Error("10.0.0.4 should not match")
	}
}

func TestMatchIP_CIDR(t *testing.T) {
	entries := []string{"10.0.0.0/24"}

	if !MatchIP("10.0.0.1", entries) {
		t.Error("10.0.0.1 should match 10.0.0.0/24")
	}
	if !MatchIP("10.0.0.254", entries) {
		t.Error("10.0.0.254 should match 10.0.0.0/24")
	}
	if MatchIP("10.0.1.1", entries) {
		t.Error("10.0.1.1 should not match 10.0.0.0/24")
	}
}

func TestMatchIP_Range(t *testing.T) {
	entries := []string{"10.0.0.10-10.0.0.20"}

	if !MatchIP("10.0.0.10", entries) {
		t.Error("10.0.0.10 should match range start")
	}
	if !MatchIP("10.0.0.15", entries) {
		t.Error("10.0.0.15 should match within range")
	}
	if !MatchIP("10.0.0.20", entries) {
		t.Error("10.0.0.20 should match range end")
	}
	if MatchIP("10.0.0.9", entries) {
		t.Error("10.0.0.9 should not match (before range)")
	}
	if MatchIP("10.0.0.21", entries) {
		t.Error("10.0.0.21 should not match (after range)")
	}
}

func TestMatchIP_Mixed(t *testing.T) {
	entries := []string{"192.168.1.1", "10.0.0.0/8", "172.16.0.1-172.16.0.10"}

	if !MatchIP("192.168.1.1", entries) {
		t.Error("exact IP should match")
	}
	if !MatchIP("10.255.255.255", entries) {
		t.Error("IP in CIDR should match")
	}
	if !MatchIP("172.16.0.5", entries) {
		t.Error("IP in range should match")
	}
	if MatchIP("192.168.1.2", entries) {
		t.Error("192.168.1.2 should not match any entry")
	}
}

func TestMatchIP_InvalidIP(t *testing.T) {
	entries := []string{"10.0.0.0/24"}

	if MatchIP("not-an-ip", entries) {
		t.Error("invalid IP should not match")
	}
	if MatchIP("", entries) {
		t.Error("empty IP should not match")
	}
}

func TestMatchIP_InvalidEntry(t *testing.T) {
	entries := []string{"not-valid", "also/bad/cidr", "1.2.3.4-bad"}

	if MatchIP("10.0.0.1", entries) {
		t.Error("should not match invalid entries")
	}
}

func TestMatchIP_IPv6(t *testing.T) {
	entries := []string{"::1", "fe80::/10"}

	if !MatchIP("::1", entries) {
		t.Error("::1 should match")
	}
	if !MatchIP("fe80::1", entries) {
		t.Error("fe80::1 should match fe80::/10")
	}
	if MatchIP("2001:db8::1", entries) {
		t.Error("2001:db8::1 should not match")
	}
}

func TestMatchIP_EmptyEntries(t *testing.T) {
	if MatchIP("10.0.0.1", nil) {
		t.Error("should not match empty entries")
	}
	if MatchIP("10.0.0.1", []string{}) {
		t.Error("should not match empty entries")
	}
}

func TestMatchIP_WhitespaceEntry(t *testing.T) {
	entries := []string{" 10.0.0.1 ", " 10.0.0.0/24 "}

	if !MatchIP("10.0.0.1", entries) {
		t.Error("should match with trimmed whitespace")
	}
	if !MatchIP("10.0.0.100", entries) {
		t.Error("should match CIDR with trimmed whitespace")
	}
}
