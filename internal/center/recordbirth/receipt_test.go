package recordbirth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func birthVectorEvent() Event {
	return Event{
		Binding: Binding{
			AuthorityID: "rd-" + strings.Repeat("1", 64), LineageID: "bl-" + strings.Repeat("2", 64),
			DeploymentID: "dp-" + strings.Repeat("3", 64), ProjectID: "default", Generation: 1,
			KeyID: "bk-" + strings.Repeat("4", 64), CutoverDigest: birthVectorDigest(6),
		},
		Sequence: 1, Kind: EventReserved, ReservationID: "rba_" + strings.Repeat("5", 32),
		RecordID: "rec2_8" + strings.Repeat("0", 31),
		Request: Request{
			ProjectID: "default", ActorID: "usr_one", ActorScopeDigest: birthVectorDigest(8),
			Purpose: PurposeCreate, IdempotencyKey: "create-1", RequestFingerprint: birthVectorDigest(7),
		},
	}
}

func birthVectorDigest(value byte) [32]byte {
	var result [32]byte
	for index := range result {
		result[index] = value
	}
	return result
}

func TestBirthReceiptIndependentVectorAndTampering(t *testing.T) {
	event := birthVectorEvent()
	canonical, err := EncodeEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	key := birthVectorDigest(9)
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(canonical)
	signature := mac.Sum(nil)
	// These constants were computed with an independent struct.pack/HMAC
	// encoder, not by the production Go encoder or a self-round-trip oracle.
	if len(canonical) != 639 || hex.EncodeToString(signature) != "9460215189261ed92b9349c7ecc2aeb63c5fb896bbc8f422dacd545be555ee5f" {
		t.Fatal("receipt framing or MAC differs from the independent vector")
	}
	receipt := append(canonical, signature...)
	digest := sha256.Sum256(receipt)
	if hex.EncodeToString(digest[:]) != "cd0882cf8ed1bce0bd82c65fed1a8486336c8c4432d87065722746f9cb28ab73" {
		t.Fatal("receipt chain hash differs from the independent vector")
	}
	decoded, err := VerifyReceipt(receipt, key[:])
	if err != nil || decoded != event {
		t.Fatalf("verify original receipt: %v", err)
	}
	for index := range receipt {
		receipt[index] ^= 1
		_, err := VerifyReceipt(receipt, key[:])
		receipt[index] ^= 1
		if err == nil {
			t.Fatalf("accepted tampered byte %d", index)
		}
	}
	for size := range len(receipt) {
		if _, err := VerifyReceipt(receipt[:size], key[:]); err == nil {
			t.Fatalf("accepted truncated receipt of %d bytes", size)
		}
	}
	if _, err := DecodeReceipt(append(receipt, 0)); err == nil {
		t.Fatal("accepted trailing data")
	}
	wrongKey := birthVectorDigest(10)
	if _, err := VerifyReceipt(receipt, wrongKey[:]); err == nil {
		t.Fatal("accepted another receipt key")
	}
}

func TestBirthReceiptRejectsInvalidDomainAndTransitions(t *testing.T) {
	original := birthVectorEvent()
	cases := map[string]func(*Event){
		"ordinary domain":             func(event *Event) { event.RecordID = "rec2_0" + strings.Repeat("0", 31) },
		"legacy domain":               func(event *Event) { event.RecordID = "rec_legacy" },
		"unbound first link":          func(event *Event) { event.PreviousHash[0] = 1 },
		"missing later link":          func(event *Event) { event.Sequence = 2 },
		"missing prepared commitment": func(event *Event) { event.Kind = EventPrepared },
		"missing created commitment":  func(event *Event) { event.Kind = EventCreated },
		"reserved with commitment":    func(event *Event) { event.InitialCommitment[0] = 1 },
		"cross project":               func(event *Event) { event.Request.ProjectID = "another" },
		"unknown purpose":             func(event *Event) { event.Request.Purpose = "unknown" },
		"unknown event":               func(event *Event) { event.Kind = "confirmed" },
		"missing cutover":             func(event *Event) { event.Binding.CutoverDigest = [32]byte{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			event := original
			mutate(&event)
			if _, err := EncodeEvent(event); err == nil {
				t.Fatal("accepted an invalid birth event")
			}
		})
	}
}

func TestInitialBirthCommitmentIndependentVector(t *testing.T) {
	event := birthVectorEvent()
	creation := InitialCreation{
		ReservationID: event.ReservationID, RecordID: event.RecordID, RevisionID: "rrv_0123456789abcdef",
		AuthorID: event.Request.ActorID, CanonicalHash: birthVectorDigest(10), RequestFingerprint: birthVectorDigest(11),
	}
	digest, err := creation.Commitment()
	if err != nil || hex.EncodeToString(digest[:]) != "fa71218b7a88afb7b71c58cd5132fe8594425780a6c986ce8b90c23e67444156" {
		t.Fatalf("initial commitment differs from independent vector: %v", err)
	}
	changed := creation
	changed.CanonicalHash[0] ^= 1
	other, err := changed.Commitment()
	if err != nil || other == digest {
		t.Fatal("initial commitment did not bind the first revision content")
	}
	changed = creation
	changed.RevisionID = "rrv_bad_revision"
	if _, err := changed.Commitment(); err == nil {
		t.Fatal("accepted a revision identity outside its existing grammar")
	}
}
