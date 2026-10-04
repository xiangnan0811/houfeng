package ipidentity

import "testing"

func TestParseAcceptsHostAddressForms(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "trimmed ipv4", value: " 192.0.2.1 ", valid: true},
		{name: "trimmed ipv6", value: "\u2003 2001:0DB8:0:0:0:0:0:1 \u3000", valid: true},
		{name: "mapped dotted ipv6", value: "::ffff:192.0.2.1", valid: true},
		{name: "mapped hexadecimal ipv6", value: "::ffff:c000:201", valid: true},
		{name: "unspecified ipv4", value: "0.0.0.0", valid: true},
		{name: "unspecified ipv6", value: "::", valid: true},
		{name: "empty", value: "", valid: false},
		{name: "zone", value: "2001:db8::1%eth0", valid: false},
		{name: "cidr", value: "192.0.2.1/32", valid: false},
		{name: "port", value: "[2001:db8::1]:443", valid: false},
		{name: "short ipv4", value: "192.0.2", valid: false},
		{name: "leading zero octet", value: "192.0.02.1", valid: false},
		{name: "out of range octet", value: "192.0.2.256", valid: false},
		{name: "invalid hexadecimal", value: "2001:db8::g", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := Parse(tt.value)
			if ok != tt.valid {
				t.Fatalf("Parse(%q) valid = %t, want %t", tt.value, ok, tt.valid)
			}
		})
	}
}

func TestChangedComparesAddressIdentityAndInvalidText(t *testing.T) {
	tests := []struct {
		name        string
		left, right string
		changed     bool
	}{
		{name: "ipv4 whitespace", left: " 192.0.2.1", right: "192.0.2.1 ", changed: false},
		{name: "ipv6 expanded and compressed", left: "2001:0db8:0:0:0:0:0:1", right: "2001:db8::1", changed: false},
		{name: "ipv6 case", left: "2001:DB8::1", right: "2001:db8::1", changed: false},
		{name: "ipv6 different address", left: "2001:db8::1", right: "2001:db8::2", changed: true},
		{name: "mapped representations", left: "::ffff:192.0.2.1", right: "::ffff:c000:201", changed: false},
		{name: "mapped and ordinary ipv4 stay separate", left: "::ffff:192.0.2.1", right: "192.0.2.1", changed: true},
		{name: "unspecified representations", left: "::", right: "0:0:0:0:0:0:0:0", changed: false},
		{name: "both empty", left: "", right: " ", changed: false},
		{name: "same invalid text", left: "not-an-address", right: " not-an-address ", changed: false},
		{name: "invalid text correction", left: "not-an-address", right: "", changed: true},
		{name: "invalid to valid", left: "not-an-address", right: "192.0.2.1", changed: true},
		{name: "invalid text changed", left: "not-an-address", right: "still-not-an-address", changed: true},
		{name: "zone correction", left: "2001:db8::1%eth0", right: "2001:db8::1", changed: true},
		{name: "cidr correction", left: "192.0.2.1/32", right: "192.0.2.1", changed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if changed := Changed(tt.left, tt.right); changed != tt.changed {
				t.Fatalf("Changed(%q, %q) = %t, want %t", tt.left, tt.right, changed, tt.changed)
			}
		})
	}
}
