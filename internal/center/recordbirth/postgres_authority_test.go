package recordbirth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// This vector was calculated independently of both Go and the SQL encoder.
// The key is synthetic test material and is never used by an authority.
func TestRecordBirthReceiptFixedVector(t *testing.T) {
	var cutover, scope, fingerprint [32]byte
	for i := range cutover {
		cutover[i], scope[i], fingerprint[i] = 6, 8, 7
	}
	event := Event{
		Binding: Binding{AuthorityID: "rd-" + strings.Repeat("1", 64), LineageID: "bl-" + strings.Repeat("2", 64),
			DeploymentID: "dp-" + strings.Repeat("3", 64), ProjectID: "default", Generation: 1,
			KeyID: "bk-" + strings.Repeat("4", 64), CutoverDigest: cutover},
		Sequence: 1, Kind: EventReserved, ReservationID: "rba_" + strings.Repeat("5", 32),
		RecordID: "rec2_8" + strings.Repeat("0", 31),
		Request: Request{ProjectID: "default", ActorID: "usr_one", ActorScopeDigest: scope,
			Purpose: PurposeCreate, IdempotencyKey: "create-1", RequestFingerprint: fingerprint},
	}
	encoded, err := EncodeEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != 639 {
		t.Fatalf("canonical length = %d", len(encoded))
	}
	key := bytes.Repeat([]byte{9}, 32)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(encoded)
	if got := hex.EncodeToString(mac.Sum(nil)); got != "9460215189261ed92b9349c7ecc2aeb63c5fb896bbc8f422dacd545be555ee5f" {
		t.Fatalf("fixed vector MAC = %s", got)
	}
	receipt := append(encoded, mac.Sum(nil)...)
	hash := sha256.Sum256(receipt)
	if got := hex.EncodeToString(hash[:]); got != "cd0882cf8ed1bce0bd82c65fed1a8486336c8c4432d87065722746f9cb28ab73" {
		t.Fatalf("fixed vector receipt hash = %s", got)
	}
	if got, err := VerifyReceipt(receipt, key); err != nil || got != event {
		t.Fatalf("fixed vector verification = %+v, %v", got, err)
	}
	for _, index := range []int{0, len(encoded) / 2, len(receipt) - 1} {
		tampered := bytes.Clone(receipt)
		tampered[index] ^= 1
		if _, err := VerifyReceipt(tampered, key); err == nil {
			t.Fatalf("tamper at %d accepted", index)
		}
	}
}
