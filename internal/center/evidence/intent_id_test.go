package evidence

import "testing"

func TestValidCaptureIntentIDUsesClosedPersistenceGrammar(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{
		{value: "evi_0123456789abcdef01234567", want: true},
		{value: "eci_0123456789abcdef01234567"},
		{value: "evi_0123456789abcdef0123456"},
		{value: "evi_0123456789abcdef012345678"},
		{value: "evi_0123456789abcdef0123456g"},
		{value: "evi_0123456789ABCDEF01234567"},
	} {
		if got := ValidCaptureIntentID(test.value); got != test.want {
			t.Errorf("ValidCaptureIntentID(%q) = %t, want %t", test.value, got, test.want)
		}
	}
}
