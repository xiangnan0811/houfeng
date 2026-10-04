// Package ipidentity compares host addresses without changing their stored text.
package ipidentity

import (
	"net/netip"
	"strings"
)

// Parse accepts a host address without a zone. IPv4-mapped IPv6 remains IPv6.
func Parse(value string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || addr.Zone() != "" {
		return netip.Addr{}, false
	}
	return addr, true
}

// Changed compares valid addresses by identity and otherwise compares trimmed text.
func Changed(left, right string) bool {
	leftAddr, leftOK := Parse(left)
	rightAddr, rightOK := Parse(right)
	if leftOK && rightOK {
		return leftAddr != rightAddr
	}
	return strings.TrimSpace(left) != strings.TrimSpace(right)
}
