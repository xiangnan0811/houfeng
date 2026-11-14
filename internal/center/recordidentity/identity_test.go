package recordidentity

import (
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  Domain
	}{
		{"empty", "", Invalid},
		{"legacy prefix only", "rec_", Invalid},
		{"legacy minimum", "rec_a", Legacy},
		{"legacy digits and letters", "rec_0z9a", Legacy},
		{"legacy maximum", "rec_" + strings.Repeat("z", 64), Legacy},
		{"legacy too long", "rec_" + strings.Repeat("z", 65), Invalid},
		{"legacy uppercase", "rec_A", Invalid},
		{"legacy separator", "rec_a_b", Invalid},
		{"legacy unicode", "rec_é", Invalid},
		{"legacy invalid byte", "rec_\xff", Invalid},
		{"legacy newline", "rec_a\n", Invalid},
		{"new prefix only", "rec2_", Invalid},
		{"new too short", "rec2_" + strings.Repeat("0", 31), Invalid},
		{"ordinary minimum", "rec2_" + strings.Repeat("0", 32), Ordinary},
		{"ordinary maximum high nibble", "rec2_7" + strings.Repeat("f", 31), Ordinary},
		{"certifiable minimum high nibble", "rec2_8" + strings.Repeat("0", 31), Certifiable},
		{"certifiable maximum", "rec2_" + strings.Repeat("f", 32), Certifiable},
		{"new too long", "rec2_" + strings.Repeat("0", 33), Invalid},
		{"new nonhex letter", "rec2_g" + strings.Repeat("0", 31), Invalid},
		{"new uppercase", "rec2_A" + strings.Repeat("0", 31), Invalid},
		{"new separator", "rec2_0_" + strings.Repeat("0", 30), Invalid},
		{"new unicode", "rec2_é" + strings.Repeat("0", 30), Invalid},
		{"new invalid byte", "rec2_\xff" + strings.Repeat("0", 31), Invalid},
		{"wrong prefix", "rrv_" + strings.Repeat("0", 32), Invalid},
		{"prefix case", "Rec2_" + strings.Repeat("0", 32), Invalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Classify(test.value); got != test.want {
				t.Fatalf("Classify(%q) = %v, want %v", test.value, got, test.want)
			}
			if got := IsValid(test.value); got != (test.want != Invalid) {
				t.Fatalf("IsValid(%q) = %v, want %v", test.value, got, test.want != Invalid)
			}
		})
	}
}

func TestClassifyNewDomainHighNibbles(t *testing.T) {
	for index, nibble := range "0123456789abcdef" {
		value := "rec2_" + string(nibble) + strings.Repeat("0", 31)
		want := Certifiable
		if index < 8 {
			want = Ordinary
		}
		if got := Classify(value); got != want {
			t.Fatalf("Classify(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestClassifyDoesNotAllocate(t *testing.T) {
	values := []string{"rec_legacy123", "rec2_70000000000000000000000000000000", "rec2_80000000000000000000000000000000", "invalid"}
	if allocations := testing.AllocsPerRun(100, func() {
		for _, value := range values {
			_ = Classify(value)
		}
	}); allocations != 0 {
		t.Fatalf("Classify allocated %v times per run", allocations)
	}
}
