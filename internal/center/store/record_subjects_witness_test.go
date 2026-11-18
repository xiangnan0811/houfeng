package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/sourceretirement"
)

func TestWitnessedRecordSubjectTombstoneReaderRequiresLocalHistory(t *testing.T) {
	t.Parallel()
	reader := mustWitnessedRecordSubjectTombstoneReader(t, nil)
	_, err := reader.ResolveWitnessedRecordSubjectTombstone(context.Background(), recordauth.ProjectIDDefault, recordauth.SourceKindVPS, testStoreRecordVPSID)
	if !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("missing APP history error = %v, want unavailable", err)
	}
	if _, err := reader.Prefetch(context.Background(), []sourceretirement.SourceKey{reader.sourceKey(recordauth.ProjectIDDefault, recordauth.SourceKindVPS, testStoreRecordVPSID)}); !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("missing APP prefetch error = %v, want unavailable", err)
	}
}

func TestWitnessedRecordSubjectTombstoneReaderUnenrolledEligibility(t *testing.T) {
	t.Parallel()
	reader := mustWitnessedRecordSubjectTombstoneReader(t, nil)
	key := reader.sourceKey(recordauth.ProjectIDDefault, recordauth.SourceKindVPS, testStoreRecordVPSID)
	for name, facts := range map[string]sourceAuthorityFacts{
		"clean":                        {},
		"legacy floor is not W2 proof": {legacy: true},
		"barrier":                      {barrier: true},
		"old-generation pin":           {anyPin: true},
		"unknown":                      {unknown: true},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := reader.resolveObserved(key, facts, preparedSourceObservation{})
			want := ErrRecordSubjectUnavailable
			if name == "clean" {
				want = ErrWitnessedRecordSubjectTombstoneNotFound
			}
			if !errors.Is(err, want) {
				t.Fatalf("resolve error = %v, want %v", err, want)
			}
		})
	}
	reader.witness = &sourceretirement.Authority{}
	if _, err := reader.resolveObserved(key, sourceAuthorityFacts{}, preparedSourceObservation{}); !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("configured W2 without enrollment error = %v", err)
	}
}

func TestWitnessedRecordSubjectTombstoneReaderAcceptsExactW2Pin(t *testing.T) {
	t.Parallel()
	reader, key, facts, prepared := sourcePinReaderFixture(t)
	got, err := reader.resolveObserved(key, facts, prepared)
	if err != nil {
		t.Fatalf("resolve exact W2 pin: %v", err)
	}
	terminal := prepared.witness.Terminal
	if got.Version != WitnessedRecordSubjectTombstoneVersionV1 || got.ProjectID != key.ProjectID || got.Kind != key.Kind || got.SourceID != key.ID ||
		!bytesEqualScope(got.LastLiveScope, terminal.LastLive) || !bytesEqualScope(got.AuthorizationFloor, terminal.FinalFloor) ||
		got.AuthorizationFloorDigest != terminal.FinalFloor.CanonicalHash {
		t.Fatalf("result does not preserve verified W2 scopes: %#v", got)
	}
}

func TestWitnessedRecordSubjectTombstoneReaderRejectsW2Drift(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*sourceAuthorityFacts, *preparedSourceObservation){
		"APP closed":           func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.admission.mode = "closed" },
		"APP generation drift": func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.admission.generation++ },
		"APP domain drift":     func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.admission.domain.DatabaseOID++ },
		"APP recovery drift": func(f *sourceAuthorityFacts, _ *preparedSourceObservation) {
			f.admission.recoveryID = "another-recovery"
		},
		"APP unknown":         func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.unknown = true },
		"missing barrier":     func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.barrier = false },
		"missing current pin": func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.pin = false },
		"barrier deployment":  func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.barrierDeployment += "0" },
		"retirement identity": func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.retirementID = "other" },
		"origin generation":   func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.originGeneration++ },
		"barrier digest":      func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.barrierDigest = []byte("wrong") },
		"last-live canonical": func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.lastLive = append(f.lastLive, 0) },
		"floor canonical":     func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.finalFloor = append(f.finalFloor, 0) },
		"pin generation":      func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.pinGeneration++ },
		"pin retirement":      func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.pinRetirementID = "other" },
		"pin origin":          func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.pinOriginGeneration++ },
		"pin barrier":         func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.pinBarrierDigest = nil },
		"pin sequence":        func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.pinSequence++ },
		"pin canonical": func(f *sourceAuthorityFacts, _ *preparedSourceObservation) {
			f.pinCanonical = append(f.pinCanonical, 0)
		},
		"pin hash": func(f *sourceAuthorityFacts, _ *preparedSourceObservation) { f.pinHash = nil },
		"legacy conflicting floor": func(f *sourceAuthorityFacts, _ *preparedSourceObservation) {
			f.legacy = true
			f.legacyDigest = make([]byte, 32)
		},
		"W1 history never accepted as W2":      func(_ *sourceAuthorityFacts, p *preparedSourceObservation) { p.witness.LegacyBlocked = true },
		"W2 recovering":                        func(_ *sourceAuthorityFacts, p *preparedSourceObservation) { p.witness.Mode = "recovering" },
		"W2 handoff incomplete":                func(_ *sourceAuthorityFacts, p *preparedSourceObservation) { p.witness.HandoffCompleted = false },
		"W2 generation":                        func(_ *sourceAuthorityFacts, p *preparedSourceObservation) { p.witness.Generation++ },
		"W2 physical domain":                   func(_ *sourceAuthorityFacts, p *preparedSourceObservation) { p.witness.Domain.DatabaseOID++ },
		"W2 head behind terminal":              func(_ *sourceAuthorityFacts, p *preparedSourceObservation) { p.witness.HeadSequence = 0 },
		"W2 head hash mismatch":                func(_ *sourceAuthorityFacts, p *preparedSourceObservation) { p.witness.HeadHash[0] ^= 1 },
		"W2 unknown":                           func(_ *sourceAuthorityFacts, p *preparedSourceObservation) { p.witness.CoverageUnknown = true },
		"W2 missing terminal with APP history": func(_ *sourceAuthorityFacts, p *preparedSourceObservation) { p.witness.Terminal = nil },
		"W2 source identity": func(_ *sourceAuthorityFacts, p *preparedSourceObservation) {
			p.witness.Terminal.Source.ID = "vps_ffffffffffffffff"
		},
		"W2 mutated scope projection": func(_ *sourceAuthorityFacts, p *preparedSourceObservation) {
			p.witness.Terminal.FinalFloor.CanonicalHash[0] ^= 1
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader, key, facts, prepared := sourcePinReaderFixture(t)
			mutate(&facts, &prepared)
			if _, err := reader.resolveObserved(key, facts, prepared); !errors.Is(err, ErrRecordSubjectUnavailable) {
				t.Fatalf("resolve drift error = %v, want unavailable", err)
			}
		})
	}
}

func TestWitnessedRecordSubjectTombstoneReaderPendingPrepareDoesNotRetire(t *testing.T) {
	t.Parallel()
	reader, key, facts, prepared := sourcePinReaderFixture(t)
	facts.barrier, facts.pin, facts.anyPin = false, false, false
	prepared.witness.Terminal = nil
	prepared.witness.PrepareCount = 2
	if _, err := reader.resolveObserved(key, facts, prepared); !errors.Is(err, ErrWitnessedRecordSubjectTombstoneNotFound) {
		t.Fatalf("prepare-only source error = %v, want proven live eligibility", err)
	}
}

func TestWitnessedRecordSubjectTombstoneReaderMatchingLegacyDigestDoesNotReplacePin(t *testing.T) {
	t.Parallel()
	reader, key, facts, prepared := sourcePinReaderFixture(t)
	facts.legacy = true
	facts.legacyDigest = prepared.witness.Terminal.FinalFloor.CanonicalHash[:]
	if _, err := reader.resolveObserved(key, facts, prepared); err != nil {
		t.Fatalf("matching historical digest rejected exact W2 proof: %v", err)
	}
	facts.pin = false
	if _, err := reader.resolveObserved(key, facts, prepared); !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("floor-only proof error = %v, want unavailable", err)
	}
}

func TestPreparedSourceAuthorityRejectsMissingTransaction(t *testing.T) {
	t.Parallel()
	var prepared *PreparedSourceAuthority
	_, err := prepared.ForTransaction(nil).ResolveWitnessedRecordSubjectTombstone(context.Background(), recordauth.ProjectIDDefault, recordauth.SourceKindVPS, testStoreRecordVPSID)
	if !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("missing prepared transaction error = %v", err)
	}
}

func mustWitnessedRecordSubjectTombstoneReader(t *testing.T, local currentRecordAuthorizationDB) *WitnessedRecordSubjectTombstoneReader {
	t.Helper()
	identity := testDeploymentMembershipIdentity()
	reader, err := NewWitnessedRecordSubjectTombstoneReader(recordplatform.DeploymentID(identity.DeploymentID), recordauth.ProjectID(identity.ProjectID), nil, local)
	if err != nil {
		t.Fatalf("NewWitnessedRecordSubjectTombstoneReader: %v", err)
	}
	return reader
}

func sourcePinReaderFixture(t *testing.T) (*WitnessedRecordSubjectTombstoneReader, sourceretirement.SourceKey, sourceAuthorityFacts, preparedSourceObservation) {
	t.Helper()
	reader := mustWitnessedRecordSubjectTombstoneReader(t, nil)
	reader.witness = &sourceretirement.Authority{}
	key := reader.sourceKey(recordauth.ProjectIDDefault, recordauth.SourceKindVPS, testStoreRecordVPSID)
	domain := sourceretirement.DomainIdentity{ID: "rd-" + strings.Repeat("a", 64), SystemIdentifier: "123456789", DatabaseOID: 42, DatabaseName: "witness", Epoch: 1}
	terminal := sourceretirement.Terminal{
		Domain: domain, Source: key, RetirementID: "retirement-test", OriginGeneration: 1,
		BarrierDigest: sha256.Sum256([]byte("committed APP barrier")),
		LastLive:      mustStoreProjectVisibility(t), FinalFloor: mustStoreProjectVisibility(t), Sequence: 1,
	}
	canonical, err := sourceretirement.EncodeTerminal(terminal)
	if err != nil {
		t.Fatalf("encode terminal: %v", err)
	}
	hash := sha256.Sum256(canonical)
	admission := sourceAuthorityAdmission{enrolled: true, deploymentID: key.DeploymentID, domain: domain, generation: 2, mode: "active"}
	facts := sourceAuthorityFacts{
		admission: admission, barrier: true, barrierDeployment: key.DeploymentID,
		retirementID: terminal.RetirementID, originGeneration: 1, barrierDigest: terminal.BarrierDigest[:],
		lastLive: terminal.LastLive.CanonicalBytes(), finalFloor: terminal.FinalFloor.CanonicalBytes(),
		pin: true, anyPin: true, pinRetirementID: terminal.RetirementID, pinGeneration: 2,
		pinOriginGeneration: 1, pinSequence: 1, pinBarrierDigest: terminal.BarrierDigest[:], pinCanonical: canonical, pinHash: hash[:],
	}
	prepared := preparedSourceObservation{admission: admission, external: true, witness: sourceretirement.Observation{
		Domain: domain, Generation: 2, Mode: "active", HandoffCompleted: true,
		HeadSequence: 1, HeadHash: hash, Terminal: &terminal, TerminalHash: hash, PrepareCount: 1,
	}}
	return reader, key, facts, prepared
}

func bytesEqualScope(left, right recordauth.VisibilityScope) bool {
	return bytes.Equal(left.CanonicalBytes(), right.CanonicalBytes()) && left.CanonicalHash == right.CanonicalHash
}
