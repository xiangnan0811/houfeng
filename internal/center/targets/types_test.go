package targets

import "testing"

func TestIsValidFrequencyTierAcceptsFiveSeconds(t *testing.T) {
	t.Parallel()

	if !IsValidFrequencyTier("5s") {
		t.Fatal("IsValidFrequencyTier(\"5s\") = false, want true")
	}
	if IsValidFrequencyTier("30s") {
		t.Fatal("IsValidFrequencyTier(\"30s\") = true, want false")
	}
}
