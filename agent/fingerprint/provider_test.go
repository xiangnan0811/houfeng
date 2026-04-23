package fingerprint

import "testing"

func TestStableFingerprintIsDeterministic(t *testing.T) {
	input := "machine-id-001"

	gotFirst := stableFingerprint(input)
	gotSecond := stableFingerprint(input)

	if gotFirst == "" {
		t.Fatal("stableFingerprint() returned empty string")
	}
	if gotFirst != gotSecond {
		t.Fatalf("stableFingerprint() mismatch: %q != %q", gotFirst, gotSecond)
	}
	if gotFirst == input {
		t.Fatalf("stableFingerprint() leaked raw input %q", input)
	}
	if len(gotFirst) != 64 {
		t.Fatalf("stableFingerprint() length = %d, want %d", len(gotFirst), 64)
	}
}
