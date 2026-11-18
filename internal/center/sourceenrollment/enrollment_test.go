package sourceenrollment

import (
	"encoding/hex"
	"testing"

	"houfeng/internal/center/sourceretirement"
)

func TestCredentialCommitmentSeparatesPurposeAndFields(t *testing.T) {
	original := CredentialCommitment("runtime", "ab", "c")
	if original == CredentialCommitment("admin", "ab", "c") ||
		original == CredentialCommitment("runtime", "a", "bc") {
		t.Fatal("purpose or length-prefixed fields were not bound")
	}
}

func TestCredentialSecretFingerprintDomainAndPassword(t *testing.T) {
	password := "trusted-high-entropy-credential"
	fingerprint := CredentialSecretFingerprint(password)
	// Independent SHA-256 vector for u32-length-prefixed domain and password.
	if hex.EncodeToString(fingerprint[:]) != "ce07c2a5c30a7c1d7016f5be8a4a76ea50a0c950816f3be5b9bf16670ce6f58c" {
		t.Fatal("secret fingerprint does not match the canonical credential codec")
	}
	if fingerprint == CredentialSecretFingerprint(password+"x") ||
		fingerprint == CredentialCommitment("runtime", "principal", password) {
		t.Fatal("secret fingerprint did not separate password or commitment domain")
	}
}

func TestActivationDigestBindsIdentitiesAndCommitments(t *testing.T) {
	app := sourceretirement.DomainIdentity{ID: "app", SystemIdentifier: "10", DatabaseOID: 2, DatabaseName: "app", Epoch: 1}
	witness := sourceretirement.DomainIdentity{ID: "witness", SystemIdentifier: "20", DatabaseOID: 3, DatabaseName: "witness", Epoch: 1}
	var commitments [6][32]byte
	commitments[0][0] = 1
	original := activationDigest("deployment", app, witness, commitments)
	if original != activationDigest("deployment", app, witness, commitments) {
		t.Fatal("identical activation inputs were not deterministic")
	}
	changedApp := app
	changedApp.DatabaseOID++
	changedWitness := witness
	changedWitness.Epoch++
	changedCommitments := commitments
	changedCommitments[4][0]++
	for _, altered := range [][32]byte{
		activationDigest("other", app, witness, commitments),
		activationDigest("deployment", changedApp, witness, commitments),
		activationDigest("deployment", app, changedWitness, commitments),
		activationDigest("deployment", app, witness, changedCommitments),
	} {
		if original == altered {
			t.Fatal("activation failed to bind an input")
		}
	}
}
