package recorddeletion

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
)

var (
	// ErrLedgerPending means the requested entry has not yet been fully witnessed.
	ErrLedgerPending = errors.New("record deletion ledger entry pending")
	// ErrLedgerContinuity means the supplied independent continuity evidence or
	// the current chain cannot establish the trusted history.
	ErrLedgerContinuity = errors.New("record deletion ledger continuity unavailable")
)

// LedgerPhysicalIdentity is an expected provisioned postgres_system domain.
// It detects mismatches, but by itself cannot detect a rollback or clone.
type LedgerPhysicalIdentity struct {
	ID               string
	Kind             string
	SystemIdentifier string
	DatabaseOID      uint32
	DatabaseName     string
	Epoch            uint64
}

func (identity LedgerPhysicalIdentity) Validate() error {
	if !validLedgerDomainID(identity.ID) || (identity.Kind != "deletion_ledger" && identity.Kind != "deletion_witness") ||
		identity.DatabaseOID == 0 || identity.Epoch == 0 || identity.Epoch > math.MaxInt64 ||
		len(identity.SystemIdentifier) == 0 || len(identity.SystemIdentifier) > 20 || identity.SystemIdentifier[0] < '1' || identity.SystemIdentifier[0] > '9' {
		return fmt.Errorf("%w: physical identity", ErrInvalidLedgerEvidence)
	}
	if _, err := strconv.ParseUint(identity.SystemIdentifier, 10, 64); err != nil {
		return fmt.Errorf("%w: physical system identifier", ErrInvalidLedgerEvidence)
	}
	name := identity.DatabaseName
	if len(name) == 0 || len(name) > 63 || name[0] < 'a' || name[0] > 'z' {
		return fmt.Errorf("%w: database identity", ErrInvalidLedgerEvidence)
	}
	for i := range name {
		if (name[i] < 'a' || name[i] > 'z') && (name[i] < '0' || name[i] > '9') && name[i] != '_' {
			return fmt.Errorf("%w: database identity", ErrInvalidLedgerEvidence)
		}
	}
	return nil
}

// LedgerHead is an exact chain position. Genesis is sequence zero and zero hash.
type LedgerHead struct {
	Sequence uint64
	Hash     [sha256.Size]byte
}

func (head LedgerHead) valid() bool {
	return head.Sequence <= math.MaxInt64 && ((head.Sequence == 0 && zeroDeletionDigest(head.Hash)) ||
		(head.Sequence > 0 && !zeroDeletionDigest(head.Hash)))
}

// LedgerChainEntry contains the raw chain identity and, for a record route,
// the independently read SQL projection. Other routes remain uninterpreted.
type LedgerChainEntry struct {
	Sequence     uint64
	PreviousHash [sha256.Size]byte
	EntryHash    [sha256.Size]byte
	Canonical    []byte
	Route        string
	Record       *CanonicalLedgerEntry
}

// LedgerSnapshot is one database's identity, chain, head, and checkpoints,
// observed in a single read-only repeatable-read transaction. Snapshot readers
// are trusted dependencies, not data accepted from an untrusted API caller.
type LedgerSnapshot struct {
	Identity    LedgerPhysicalIdentity
	Head        LedgerHead
	Entries     []LedgerChainEntry
	Checkpoints []LedgerHead
}

type LedgerSnapshotReader interface {
	ReadLedgerSnapshot(context.Context) (LedgerSnapshot, error)
}

// LedgerContinuityObservation is passed after the verifier has checked both
// complete chains and the externally retained anchors. A guard must establish
// the current external fencing/continuity evidence, not merely compare these
// observations. Its maintenance lock must remain held through authority use.
type LedgerContinuityObservation struct {
	Primary       LedgerPhysicalIdentity
	Witness       LedgerPhysicalIdentity
	PrimaryAnchor LedgerHead
	WitnessAnchor LedgerHead
	PrimaryHead   LedgerHead
	WitnessHead   LedgerHead
}

type LedgerContinuityGuard interface {
	VerifyLedgerContinuity(context.Context, LedgerContinuityObservation) error
}

// LedgerVerificationTrust must come from the trusted maintenance coordinator.
// Anchors are retained outside the two databases; a genesis anchor is valid
// only when the guard proves that genesis is the actual trusted starting point.
type LedgerVerificationTrust struct {
	Primary       LedgerPhysicalIdentity
	Witness       LedgerPhysicalIdentity
	PrimaryAnchor LedgerHead
	WitnessAnchor LedgerHead
	Continuity    LedgerContinuityGuard
}

type LedgerLocator struct {
	LedgerDomain [sha256.Size]byte
	Sequence     uint64
	EntryHash    [sha256.Size]byte
}

// VerifiedLedgerEntry is issued only by LedgerVerifier after both independent
// reads and the external continuity guard succeed. Copies expose no mutable
// internal byte slices. Its zero value is never verified authority.
type VerifiedLedgerEntry struct {
	entry       CanonicalLedgerEntry
	locator     LedgerLocator
	primaryHead LedgerHead
	witnessHead LedgerHead
	proofDigest [sha256.Size]byte
	verified    bool
}

func (entry VerifiedLedgerEntry) Valid() bool {
	return entry.verified && entry.entry.Validate() == nil && entry.locator.LedgerDomain == entry.entry.LedgerDomain &&
		entry.locator.Sequence == entry.entry.Sequence && entry.locator.EntryHash == entry.entry.EntryHash &&
		entry.primaryHead.valid() && entry.witnessHead.valid() && entry.witnessHead.Sequence >= entry.locator.Sequence &&
		entry.primaryHead.Sequence >= entry.witnessHead.Sequence && !zeroDeletionDigest(entry.proofDigest)
}

func (entry VerifiedLedgerEntry) Entry() CanonicalLedgerEntry {
	if !entry.Valid() {
		return CanonicalLedgerEntry{}
	}
	copyEntry := entry.entry
	copyEntry.Canonical = append([]byte(nil), entry.entry.Canonical...)
	return copyEntry
}

func (entry VerifiedLedgerEntry) Locator() LedgerLocator {
	if !entry.Valid() {
		return LedgerLocator{}
	}
	return entry.locator
}

func (entry VerifiedLedgerEntry) PrimaryHead() LedgerHead {
	if !entry.Valid() {
		return LedgerHead{}
	}
	return entry.primaryHead
}

func (entry VerifiedLedgerEntry) WitnessHead() LedgerHead {
	if !entry.Valid() {
		return LedgerHead{}
	}
	return entry.witnessHead
}

func (entry VerifiedLedgerEntry) ProofDigest() [sha256.Size]byte {
	if !entry.Valid() {
		return [sha256.Size]byte{}
	}
	return entry.proofDigest
}

type LedgerVerifier struct {
	primary LedgerSnapshotReader
	witness LedgerSnapshotReader
	trust   LedgerVerificationTrust
}

func NewLedgerVerifier(primary, witness LedgerSnapshotReader, trust LedgerVerificationTrust) (*LedgerVerifier, error) {
	if nilDeletionServiceDependency(primary) || nilDeletionServiceDependency(witness) || !validLedgerTrust(trust) {
		return nil, fmt.Errorf("%w: verifier trust", ErrLedgerContinuity)
	}
	return &LedgerVerifier{primary: primary, witness: witness, trust: trust}, nil
}

func validLedgerTrust(trust LedgerVerificationTrust) bool {
	return !nilDeletionServiceDependency(trust.Continuity) && trust.Primary.Validate() == nil && trust.Witness.Validate() == nil &&
		trust.Primary.Kind == "deletion_ledger" && trust.Witness.Kind == "deletion_witness" &&
		trust.Primary.ID != trust.Witness.ID && trust.Primary.SystemIdentifier != trust.Witness.SystemIdentifier &&
		trust.PrimaryAnchor.valid() && trust.WitnessAnchor.valid()
}

// Verify reads witness first, then primary, so a concurrent normal append can
// advance primary beyond witness without making an older primary snapshot look
// like witness-ahead. Each individual database snapshot remains consistent.
func (verifier *LedgerVerifier) Verify(ctx context.Context, locator LedgerLocator) (VerifiedLedgerEntry, error) {
	if ctx == nil || verifier == nil || locator.Sequence == 0 || locator.Sequence > math.MaxInt64 ||
		zeroDeletionDigest(locator.LedgerDomain) || zeroDeletionDigest(locator.EntryHash) {
		return VerifiedLedgerEntry{}, fmt.Errorf("%w: locator", ErrInvalidLedgerEvidence)
	}
	if err := ctx.Err(); err != nil {
		return VerifiedLedgerEntry{}, err
	}
	domain, err := LedgerDomainDigest(verifier.trust.Primary.ID)
	if err != nil || domain != locator.LedgerDomain {
		return VerifiedLedgerEntry{}, fmt.Errorf("%w: ledger domain", ErrInvalidLedgerEvidence)
	}
	witness, err := verifier.witness.ReadLedgerSnapshot(ctx)
	if err != nil {
		return VerifiedLedgerEntry{}, fmt.Errorf("read witness snapshot: %w", err)
	}
	primary, err := verifier.primary.ReadLedgerSnapshot(ctx)
	if err != nil {
		return VerifiedLedgerEntry{}, fmt.Errorf("read ledger snapshot: %w", err)
	}
	observation, err := VerifyLedgerSnapshotPair(primary, witness, verifier.trust)
	if err != nil {
		return VerifiedLedgerEntry{}, err
	}
	if err := verifier.trust.Continuity.VerifyLedgerContinuity(ctx, observation); err != nil {
		return VerifiedLedgerEntry{}, fmt.Errorf("%w: external fence: %v", ErrLedgerContinuity, err)
	}
	if locator.Sequence > witness.Head.Sequence {
		if locator.Sequence <= primary.Head.Sequence && primary.Entries[locator.Sequence-1].EntryHash != locator.EntryHash {
			return VerifiedLedgerEntry{}, fmt.Errorf("%w: locator hash", ErrInvalidLedgerEvidence)
		}
		return VerifiedLedgerEntry{}, ErrLedgerPending
	}
	row := primary.Entries[locator.Sequence-1]
	if row.Route != LedgerRecordRoute || row.Record == nil || row.EntryHash != locator.EntryHash {
		return VerifiedLedgerEntry{}, fmt.Errorf("%w: locator entry", ErrInvalidLedgerEvidence)
	}
	proof, err := LedgerWitnessProofDigest(domain, locator.Sequence, locator.EntryHash, witness.Identity.ID)
	if err != nil {
		return VerifiedLedgerEntry{}, err
	}
	value := *row.Record
	value.Canonical = append([]byte(nil), row.Record.Canonical...)
	return VerifiedLedgerEntry{entry: value, locator: locator, primaryHead: primary.Head,
		witnessHead: witness.Head, proofDigest: proof, verified: true}, nil
}

// VerifyLedgerSnapshotPair validates coherent physical observations against
// externally retained anchors and each other. It does not itself establish
// external fencing; callers must still hold and verify the maintenance guard.
func VerifyLedgerSnapshotPair(primary, witness LedgerSnapshot, trust LedgerVerificationTrust) (LedgerContinuityObservation, error) {
	if !validLedgerTrust(trust) || primary.Identity != trust.Primary || witness.Identity != trust.Witness {
		return LedgerContinuityObservation{}, fmt.Errorf("%w: physical identity mismatch", ErrLedgerContinuity)
	}
	domain, err := LedgerDomainDigest(trust.Primary.ID)
	if err != nil {
		return LedgerContinuityObservation{}, err
	}
	if err := ValidateLedgerSnapshot(primary, domain); err != nil {
		return LedgerContinuityObservation{}, err
	}
	if err := ValidateLedgerSnapshot(witness, domain); err != nil {
		return LedgerContinuityObservation{}, err
	}
	if !snapshotContains(primary, trust.PrimaryAnchor) || !snapshotContains(witness, trust.WitnessAnchor) {
		return LedgerContinuityObservation{}, fmt.Errorf("%w: retained anchor missing", ErrLedgerContinuity)
	}
	if witness.Head.Sequence > primary.Head.Sequence {
		return LedgerContinuityObservation{}, fmt.Errorf("%w: witness ahead", ErrLedgerContinuity)
	}
	for index, witnessed := range witness.Entries {
		first := primary.Entries[index]
		if first.Sequence != witnessed.Sequence || first.EntryHash != witnessed.EntryHash ||
			!bytes.Equal(first.Canonical, witnessed.Canonical) || first.Route != witnessed.Route {
			return LedgerContinuityObservation{}, fmt.Errorf("%w: witness fork", ErrLedgerContinuity)
		}
	}
	return LedgerContinuityObservation{Primary: primary.Identity, Witness: witness.Identity,
		PrimaryAnchor: trust.PrimaryAnchor, WitnessAnchor: trust.WitnessAnchor,
		PrimaryHead: primary.Head, WitnessHead: witness.Head}, nil
}

// LedgerWitnessProofDigest is an observation binding, not a substitute for
// independent reads or the trusted maintenance verifier.
func LedgerWitnessProofDigest(domain [sha256.Size]byte, sequence uint64, hash [sha256.Size]byte, witnessID string) ([sha256.Size]byte, error) {
	witnessDomain, err := LedgerDomainDigest(witnessID)
	if err != nil || zeroDeletionDigest(domain) || sequence == 0 || sequence > math.MaxInt64 || zeroDeletionDigest(hash) {
		return [sha256.Size]byte{}, fmt.Errorf("%w: witness binding", ErrInvalidLedgerEvidence)
	}
	body := appendLedgerBytes(nil, []byte("HOUFENG-RECORD-LEDGER-WITNESS-V1"))
	body = appendLedgerBytes(body, domain[:])
	body = binary.BigEndian.AppendUint64(body, sequence)
	body = appendLedgerBytes(body, hash[:])
	body = appendLedgerBytes(body, witnessDomain[:])
	return sha256.Sum256(body), nil
}

// ValidateLedgerSnapshot checks continuity and every record projection without
// interpreting other routes' canonical formats or conferring replay authority.
func ValidateLedgerSnapshot(snapshot LedgerSnapshot, domain [sha256.Size]byte) error {
	if snapshot.Identity.Validate() != nil || !snapshot.Head.valid() || zeroDeletionDigest(domain) ||
		uint64(len(snapshot.Entries)) != snapshot.Head.Sequence {
		return fmt.Errorf("%w: invalid snapshot", ErrLedgerContinuity)
	}
	var previous [sha256.Size]byte
	for index, row := range snapshot.Entries {
		if row.Sequence != uint64(index)+1 || row.PreviousHash != previous || len(row.Canonical) == 0 ||
			len(row.Canonical) > ledgerMaxCanonicalBytes || sha256.Sum256(row.Canonical) != row.EntryHash || zeroDeletionDigest(row.EntryHash) {
			return fmt.Errorf("%w: broken chain", ErrLedgerContinuity)
		}
		switch row.Route {
		case LedgerRecordRoute:
			if row.Record == nil || row.Record.Validate() != nil || row.Record.Sequence != row.Sequence ||
				row.Record.PreviousHash != row.PreviousHash || row.Record.EntryHash != row.EntryHash ||
				row.Record.LedgerDomain != domain || !bytes.Equal(row.Record.Canonical, row.Canonical) {
				return fmt.Errorf("%w: record projection", ErrInvalidLedgerEvidence)
			}
		case "source_permanent_delete":
			if row.Record != nil {
				return fmt.Errorf("%w: source route projection", ErrInvalidLedgerEvidence)
			}
		default:
			return fmt.Errorf("%w: unknown route", ErrInvalidLedgerEvidence)
		}
		previous = row.EntryHash
	}
	if previous != snapshot.Head.Hash {
		return fmt.Errorf("%w: head mismatch", ErrLedgerContinuity)
	}
	for _, checkpoint := range snapshot.Checkpoints {
		if !checkpoint.valid() || !snapshotContains(snapshot, checkpoint) {
			return fmt.Errorf("%w: checkpoint mismatch", ErrLedgerContinuity)
		}
	}
	return nil
}

func snapshotContains(snapshot LedgerSnapshot, anchor LedgerHead) bool {
	if !anchor.valid() || anchor.Sequence > snapshot.Head.Sequence {
		return false
	}
	return anchor.Sequence == 0 || snapshot.Entries[anchor.Sequence-1].EntryHash == anchor.Hash
}
