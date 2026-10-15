package settings

import (
	"strings"
	"testing"
)

func TestMaxMindSourcePairValidation(t *testing.T) {
	for _, test := range []struct {
		id, key string
		valid   bool
	}{
		{"", "", true}, {"12345678", "private-key", true}, {" 12345678 ", " private-key ", true},
		{"12345678", "", false}, {"", "private-key", false}, {"account", "private-key", false},
		{"123:456", "private-key", false}, {"12345678", "private\nkey", false},
		{strings.Repeat("1", 257), "private-key", false}, {"12345678", strings.Repeat("x", 257), false},
	} {
		input := FreshInstallDefaults()
		input.IPQuality.Sources.MaxMindAccountID, input.IPQuality.Sources.MaxMindLicenseKey = test.id, test.key
		result, err := Validate(input)
		if (err == nil) != test.valid {
			t.Fatalf("pair validity mismatch valid=%t err=%v", test.valid, err)
		}
		if test.valid && (result.IPQuality.Sources.MaxMindAccountID != strings.TrimSpace(test.id) || result.IPQuality.Sources.MaxMindLicenseKey != strings.TrimSpace(test.key)) {
			t.Fatal("paired credentials were not normalized")
		}
	}
}
