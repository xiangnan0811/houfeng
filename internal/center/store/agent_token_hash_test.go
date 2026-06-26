package store

import "testing"

func TestAgentTokenHashUsesPurposeSeparatedHMAC(t *testing.T) {
	hashes := newAgentTokenHasher([]byte("0123456789abcdef0123456789abcdef"))

	enrollmentHash := hashes.hashEnrollmentToken("enroll_001")
	syncHash := hashes.hashSyncToken("sync_001")

	if !isHMACAgentTokenHash(enrollmentHash) {
		t.Fatalf("enrollment hash = %q, want versioned hmac hash", enrollmentHash)
	}
	if !isHMACAgentTokenHash(syncHash) {
		t.Fatalf("sync hash = %q, want versioned hmac hash", syncHash)
	}
	if enrollmentHash == hashOpaqueToken("enroll_001") {
		t.Fatal("enrollment hash equals legacy plain sha256 hash")
	}
	if syncHash == hashOpaqueToken("sync_001") {
		t.Fatal("sync hash equals legacy plain sha256 hash")
	}
	if enrollmentHash == hashes.hashSyncToken("enroll_001") {
		t.Fatal("enrollment and sync hash purposes produced identical values")
	}
}

func TestAgentTokenHashVerifiesLegacySHA256(t *testing.T) {
	hashes := newAgentTokenHasher([]byte("0123456789abcdef0123456789abcdef"))

	if !hashes.enrollmentTokenMatches(hashOpaqueToken("enroll_001"), "enroll_001") {
		t.Fatal("legacy enrollment sha256 hash did not verify")
	}
	if !hashes.syncTokenMatches(hashOpaqueToken("sync_001"), "sync_001") {
		t.Fatal("legacy sync sha256 hash did not verify")
	}
	if hashes.syncTokenMatches(hashOpaqueToken("sync_001"), "wrong") {
		t.Fatal("legacy sync sha256 hash verified wrong token")
	}
}
