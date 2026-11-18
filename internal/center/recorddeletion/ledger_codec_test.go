package recorddeletion

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/recordplatform"
)

func ledgerTestRequest(t *testing.T) LedgerAppendRequest {
	t.Helper()
	fingerprint, err := recordplatform.ParseTrustedPersistedRequestFingerprintV1(bytes.Repeat([]byte{0x33}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var token [32]byte
	copy(token[:], bytes.Repeat([]byte{0x22}, 32))
	return LedgerAppendRequest{EntryType: LedgerEntryDeleteCommit, DeploymentID: recordplatform.DeploymentID("dp-" + strings.Repeat("a", 64)),
		ProjectID: recordplatform.ProjectIDDefault, OperationID: "rpo_x", ActorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		Object:          recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"},
		TokenCommitment: token, RequestFingerprint: fingerprint, ReasonCode: DeletionReasonUserConfirmed, DeletionContractVersion: 1}
}

func ledgerTestEntry(t *testing.T, domain [32]byte, sequence uint64, previous [32]byte, request LedgerAppendRequest) CanonicalLedgerEntry {
	t.Helper()
	entry := CanonicalLedgerEntry{DeletionLedgerEntry: DeletionLedgerEntry{Request: request, Sequence: sequence},
		LedgerDomain: domain, PreviousHash: previous, ConfirmedAt: time.UnixMicro(1).UTC()}
	canonical, err := EncodeCanonicalLedgerEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	entry.Canonical = canonical
	entry.EntryHash = sha256.Sum256(canonical)
	return entry
}

func TestRecordLedgerCanonicalBytesAndProjection(t *testing.T) {
	var domain [32]byte
	copy(domain[:], bytes.Repeat([]byte{0x11}, 32))
	entry := ledgerTestEntry(t, domain, 1, [32]byte{}, ledgerTestRequest(t))
	// Fixed literal field lengths and order are an independent format oracle.
	want := []byte("\x00\x00\x00\x1aHOUFENG-RECORD-DELETION-V1" +
		"\x00\x00\x00\x00\x00\x00\x00\x01" + "\x00\x00\x00\x20" + strings.Repeat("\x11", 32) +
		"\x00\x00\x00\x00\x00\x00\x00\x01" + "\x00\x00\x00\x20" + strings.Repeat("\x00", 32) +
		"\x00\x00\x00\x0ddelete_commit" + "\x00\x00\x00\x43dp-" + strings.Repeat("a", 64) +
		"\x00\x00\x00\x07default" + "\x00\x00\x00\x05rpo_x" +
		"\x00\x00\x00\x1cusr_aaaaaaaaaaaaaaaaaaaaaaaa" + "\x00\x00\x00\x17record_permanent_delete" +
		"\x00\x00\x00\x06record" + "\x00\x00\x00\x06rec_01" +
		"\x00\x00\x00\x20" + strings.Repeat("\x22", 32) + "\x00\x00\x00\x20" + strings.Repeat("\x33", 32) +
		"\x00\x00\x00\x0euser_confirmed" + "\x00\x00\x00\x00\x00\x00\x00\x01" +
		"\x00\x00\x00\x00\x00\x00\x00\x00" + "\x00\x00\x00\x00\x00\x00\x00\x01")
	if !bytes.Equal(entry.Canonical, want) {
		t.Fatalf("canonical bytes = %x, want %x", entry.Canonical, want)
	}
	decoded, err := DecodeCanonicalLedgerEntry(want)
	if err != nil || decoded.Validate() != nil || decoded.EntryHash != sha256.Sum256(want) {
		t.Fatalf("decode/validate: %v", err)
	}
	for _, change := range []struct {
		name  string
		apply func(*CanonicalLedgerEntry)
	}{
		{"operation", func(v *CanonicalLedgerEntry) { v.Request.OperationID = "rpo_other" }},
		{"token", func(v *CanonicalLedgerEntry) { v.Request.TokenCommitment[0] ^= 1 }},
		{"sequence", func(v *CanonicalLedgerEntry) { v.Sequence++ }},
		{"domain", func(v *CanonicalLedgerEntry) { v.LedgerDomain[0] ^= 1 }},
		{"hash", func(v *CanonicalLedgerEntry) { v.EntryHash[0] ^= 1 }},
		{"time", func(v *CanonicalLedgerEntry) { v.ConfirmedAt = v.ConfirmedAt.Add(time.Microsecond) }},
	} {
		t.Run(change.name, func(t *testing.T) {
			mutated := entry
			change.apply(&mutated)
			if mutated.Validate() == nil {
				t.Fatal("conflicting SQL projection was accepted")
			}
		})
	}
}

func TestRecordLedgerRejectsMalformedCanonical(t *testing.T) {
	var domain [32]byte
	domain[0] = 1
	entry := ledgerTestEntry(t, domain, 1, [32]byte{}, ledgerTestRequest(t))
	for _, tc := range []struct {
		name    string
		corrupt func([]byte) []byte
	}{
		{"truncated", func(b []byte) []byte { return b[:len(b)-1] }},
		{"trailing", func(b []byte) []byte { return append(b, 0) }},
		{"invalid utf8", func(b []byte) []byte { b[4] = 0xff; return b }},
		{"overflowing length", func(b []byte) []byte { copy(b[:4], []byte{0xff, 0xff, 0xff, 0xff}); return b }},
		{"unknown version", func(b []byte) []byte { b[37] = 2; return b }},
		{"unknown route", func(b []byte) []byte { b[bytes.Index(b, []byte(LedgerRecordRoute))] = 'x'; return b }},
		{"conflicting release", func(b []byte) []byte { b[len(b)-9] = 1; return b }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := tc.corrupt(append([]byte(nil), entry.Canonical...))
			if _, err := DecodeCanonicalLedgerEntry(value); err == nil {
				t.Fatal("malformed bytes accepted")
			}
		})
	}
}

type ledgerTestReader struct{ snapshot LedgerSnapshot }

func (reader ledgerTestReader) ReadLedgerSnapshot(context.Context) (LedgerSnapshot, error) {
	return reader.snapshot, nil
}

type ledgerTestGuard struct{ err error }

func (guard ledgerTestGuard) VerifyLedgerContinuity(context.Context, LedgerContinuityObservation) error {
	return guard.err
}

func ledgerTestIdentity(id, kind, system, database string) LedgerPhysicalIdentity {
	return LedgerPhysicalIdentity{ID: "rd-" + strings.Repeat(id, 64), Kind: kind, SystemIdentifier: system, DatabaseOID: 1, DatabaseName: database, Epoch: 1}
}

func ledgerTestRow(entry CanonicalLedgerEntry) LedgerChainEntry {
	copyEntry := entry
	return LedgerChainEntry{Sequence: entry.Sequence, PreviousHash: entry.PreviousHash, EntryHash: entry.EntryHash,
		Canonical: append([]byte(nil), entry.Canonical...), Route: LedgerRecordRoute, Record: &copyEntry}
}

func TestRecordLedgerVerifierRequiresWitnessedContinuousPrefix(t *testing.T) {
	primaryID := ledgerTestIdentity("a", "deletion_ledger", "1", "ledger")
	witnessID := ledgerTestIdentity("b", "deletion_witness", "2", "witness")
	domain, err := LedgerDomainDigest(primaryID.ID)
	if err != nil {
		t.Fatal(err)
	}
	first := ledgerTestEntry(t, domain, 1, [32]byte{}, ledgerTestRequest(t))
	secondRequest := ledgerTestRequest(t)
	secondRequest.OperationID = "rpo_y"
	second := ledgerTestEntry(t, domain, 2, first.EntryHash, secondRequest)
	primary := LedgerSnapshot{Identity: primaryID, Head: LedgerHead{Sequence: 2, Hash: second.EntryHash}, Entries: []LedgerChainEntry{ledgerTestRow(first), ledgerTestRow(second)}}
	witness := LedgerSnapshot{Identity: witnessID, Head: LedgerHead{Sequence: 1, Hash: first.EntryHash}, Entries: []LedgerChainEntry{ledgerTestRow(first)}}
	trust := LedgerVerificationTrust{Primary: primaryID, Witness: witnessID, PrimaryAnchor: LedgerHead{Sequence: 1, Hash: first.EntryHash}, Continuity: ledgerTestGuard{}}
	verify := func(p, w LedgerSnapshot, trust LedgerVerificationTrust, entry CanonicalLedgerEntry) (VerifiedLedgerEntry, error) {
		v, err := NewLedgerVerifier(ledgerTestReader{p}, ledgerTestReader{w}, trust)
		if err != nil {
			return VerifiedLedgerEntry{}, err
		}
		return v.Verify(context.Background(), LedgerLocator{LedgerDomain: domain, Sequence: entry.Sequence, EntryHash: entry.EntryHash})
	}
	verified, err := verify(primary, witness, trust, first)
	if err != nil || !verified.Valid() || verified.PrimaryHead() != primary.Head || verified.WitnessHead() != witness.Head {
		t.Fatalf("valid prefix rejected: %v", err)
	}
	copyEntry := verified.Entry()
	copyEntry.Canonical[0] ^= 0xff
	if !verified.Valid() || bytes.Equal(copyEntry.Canonical, verified.Entry().Canonical) {
		t.Fatal("caller mutated verified evidence")
	}
	if (VerifiedLedgerEntry{}).Valid() {
		t.Fatal("zero evidence is valid")
	}
	if _, err := verify(primary, witness, trust, second); !errors.Is(err, ErrLedgerPending) {
		t.Fatalf("unwitnessed tail = %v", err)
	}

	wrongAnchor := trust
	wrongAnchor.PrimaryAnchor.Hash[0] ^= 1
	if _, err := verify(primary, witness, wrongAnchor, first); !errors.Is(err, ErrLedgerContinuity) {
		t.Fatalf("bad anchor = %v", err)
	}
	denied := trust
	denied.Continuity = ledgerTestGuard{err: errors.New("fence lost")}
	if _, err := verify(primary, witness, denied, first); !errors.Is(err, ErrLedgerContinuity) {
		t.Fatalf("lost fence = %v", err)
	}
	ahead := witness
	ahead.Entries = append(append([]LedgerChainEntry(nil), primary.Entries...), ledgerTestRow(ledgerTestEntry(t, domain, 3, second.EntryHash, ledgerTestRequest(t))))
	ahead.Head = LedgerHead{Sequence: 3, Hash: ahead.Entries[2].EntryHash}
	if _, err := verify(primary, ahead, trust, first); !errors.Is(err, ErrLedgerContinuity) {
		t.Fatalf("witness ahead = %v", err)
	}
	forkRequest := ledgerTestRequest(t)
	forkRequest.OperationID = "rpo_fork"
	fork := ledgerTestEntry(t, domain, 1, [32]byte{}, forkRequest)
	forked := LedgerSnapshot{Identity: witnessID, Head: LedgerHead{Sequence: 1, Hash: fork.EntryHash}, Entries: []LedgerChainEntry{ledgerTestRow(fork)}}
	if _, err := verify(primary, forked, trust, first); !errors.Is(err, ErrLedgerContinuity) {
		t.Fatalf("fork = %v", err)
	}
	missing := primary
	missing.Entries = append([]LedgerChainEntry(nil), primary.Entries[:1]...)
	if _, err := verify(missing, witness, trust, first); !errors.Is(err, ErrLedgerContinuity) {
		t.Fatalf("missing chain entry = %v", err)
	}
}

func TestRecordLedgerVerifierDoesNotInterpretSourceRoute(t *testing.T) {
	primaryID := ledgerTestIdentity("c", "deletion_ledger", "3", "ledger")
	witnessID := ledgerTestIdentity("d", "deletion_witness", "4", "witness")
	domain, err := LedgerDomainDigest(primaryID.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("unrecognized source canonical bytes")
	sourceHash := sha256.Sum256(raw)
	source := LedgerChainEntry{Sequence: 1, EntryHash: sourceHash, Canonical: raw, Route: "source_permanent_delete"}
	entry := ledgerTestEntry(t, domain, 2, sourceHash, ledgerTestRequest(t))
	primary := LedgerSnapshot{Identity: primaryID, Head: LedgerHead{Sequence: 2, Hash: entry.EntryHash}, Entries: []LedgerChainEntry{source, ledgerTestRow(entry)}}
	witness := LedgerSnapshot{Identity: witnessID, Head: primary.Head, Entries: []LedgerChainEntry{source, ledgerTestRow(entry)}}
	trust := LedgerVerificationTrust{Primary: primaryID, Witness: witnessID, Continuity: ledgerTestGuard{}}
	v, err := NewLedgerVerifier(ledgerTestReader{primary}, ledgerTestReader{witness}, trust)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(context.Background(), LedgerLocator{LedgerDomain: domain, Sequence: 2, EntryHash: entry.EntryHash}); err != nil {
		t.Fatalf("record after source: %v", err)
	}
	if _, err := v.Verify(context.Background(), LedgerLocator{LedgerDomain: domain, Sequence: 1, EntryHash: sourceHash}); !errors.Is(err, ErrInvalidLedgerEvidence) {
		t.Fatalf("source replay authority = %v", err)
	}
}
