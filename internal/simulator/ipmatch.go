package simulator

import (
	"net"
	"strings"
)

// MatchIP checks whether the given IP address matches any entry in the list.
// Entries can be: single IPs ("10.0.0.1"), CIDR ranges ("10.0.0.0/24"),
// or IP ranges ("10.0.0.1-10.0.0.255").
func MatchIP(ip string, entries []string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}

	for _, entry := range entries {
		if matchEntry(parsed, entry) {
			return true
		}
	}
	return false
}

func matchEntry(ip net.IP, entry string) bool {
	entry = strings.TrimSpace(entry)

	// CIDR notation: "10.0.0.0/24"
	if strings.Contains(entry, "/") {
		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			return false
		}
		return network.Contains(ip)
	}

	// IP range: "10.0.0.1-10.0.0.255"
	if strings.Contains(entry, "-") {
		parts := strings.SplitN(entry, "-", 2)
		start := net.ParseIP(strings.TrimSpace(parts[0]))
		end := net.ParseIP(strings.TrimSpace(parts[1]))
		if start == nil || end == nil {
			return false
		}
		return ipInRange(ip, start, end)
	}

	// Single IP.
	single := net.ParseIP(entry)
	if single == nil {
		return false
	}
	return ip.Equal(single)
}

// ipInRange returns true if ip is between start and end (inclusive).
func ipInRange(ip, start, end net.IP) bool {
	// Normalize to 16-byte representation for consistent comparison.
	ip = ip.To16()
	start = start.To16()
	end = end.To16()

	if ip == nil || start == nil || end == nil {
		return false
	}

	return bytesCompare(ip, start) >= 0 && bytesCompare(ip, end) <= 0
}

// bytesCompare compares two byte slices lexicographically.
func bytesCompare(a, b net.IP) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}
