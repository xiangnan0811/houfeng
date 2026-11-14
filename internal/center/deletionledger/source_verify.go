package deletionledger

import (
	"context"
	"fmt"
	"math"

	"houfeng/internal/center/recorddeletion"
)

// VerifiedSourceRetirement is issued only after independent actual W1 and W2
// snapshots, their complete projections and external continuity all agree.
type VerifiedSourceRetirement struct {
	entry                    SourceRetirementEntry
	locator                  recorddeletion.LedgerLocator
	primaryHead, witnessHead recorddeletion.LedgerHead
	proofDigest              [32]byte
	verified                 bool
}

func sourceHeadValid(head recorddeletion.LedgerHead) bool {
	return head.Sequence <= math.MaxInt64 && ((head.Sequence == 0 && head.Hash == ([32]byte{})) || (head.Sequence > 0 && head.Hash != ([32]byte{})))
}
func (value VerifiedSourceRetirement) Valid() bool {
	return value.verified && value.entry.Validate() == nil && value.locator.LedgerDomain == value.entry.LedgerDomain &&
		value.locator.Sequence == value.entry.Sequence && value.locator.EntryHash == value.entry.EntryHash &&
		sourceHeadValid(value.primaryHead) && sourceHeadValid(value.witnessHead) &&
		value.witnessHead.Sequence >= value.locator.Sequence && value.primaryHead.Sequence >= value.witnessHead.Sequence && value.proofDigest != ([32]byte{})
}
func (value VerifiedSourceRetirement) Entry() SourceRetirementEntry {
	if !value.Valid() {
		return SourceRetirementEntry{}
	}
	return cloneSourceEntry(value.entry)
}
func (value VerifiedSourceRetirement) Locator() recorddeletion.LedgerLocator {
	if !value.Valid() {
		return recorddeletion.LedgerLocator{}
	}
	return value.locator
}
func (value VerifiedSourceRetirement) PrimaryHead() recorddeletion.LedgerHead {
	if !value.Valid() {
		return recorddeletion.LedgerHead{}
	}
	return value.primaryHead
}
func (value VerifiedSourceRetirement) WitnessHead() recorddeletion.LedgerHead {
	if !value.Valid() {
		return recorddeletion.LedgerHead{}
	}
	return value.witnessHead
}
func (value VerifiedSourceRetirement) ProofDigest() [32]byte {
	if !value.Valid() {
		return [32]byte{}
	}
	return value.proofDigest
}

type SourceRetirementVerifier struct {
	primary *Primary
	witness *Witness
	trust   recorddeletion.LedgerVerificationTrust
}

func NewSourceRetirementVerifier(primary *Primary, witness *Witness, trust recorddeletion.LedgerVerificationTrust) (*SourceRetirementVerifier, error) {
	if primary == nil || witness == nil || primary.client.pool == nil || witness.client.pool == nil || witness.primary == nil ||
		witness.primary.client.expected != primary.client.expected || primary.client.expected != trust.Primary || witness.client.expected != trust.Witness {
		return nil, fmt.Errorf("%w: source verifier configuration", recorddeletion.ErrLedgerContinuity)
	}
	// Reuse the established validation of identities, anchors and trusted guard.
	if _, err := recorddeletion.NewLedgerVerifier(primary, witness, trust); err != nil {
		return nil, err
	}
	return &SourceRetirementVerifier{primary: primary, witness: witness, trust: trust}, nil
}

func (verifier *SourceRetirementVerifier) Verify(ctx context.Context, locator recorddeletion.LedgerLocator) (VerifiedSourceRetirement, error) {
	if ctx == nil || verifier == nil || locator.Sequence == 0 || locator.Sequence > math.MaxInt64 || locator.LedgerDomain == ([32]byte{}) || locator.EntryHash == ([32]byte{}) {
		return VerifiedSourceRetirement{}, sourceInvalid("locator")
	}
	if err := ctx.Err(); err != nil {
		return VerifiedSourceRetirement{}, err
	}
	domain, err := recorddeletion.LedgerDomainDigest(verifier.trust.Primary.ID)
	if err != nil || locator.LedgerDomain != domain {
		return VerifiedSourceRetirement{}, sourceInvalid("locator domain")
	}
	// Witness first prevents a normal concurrent append from producing a false
	// witness-ahead result. Each read has its own physical identity check.
	witness, witnessRows, err := verifier.witness.client.readStoredSnapshot(ctx)
	if err != nil {
		return VerifiedSourceRetirement{}, fmt.Errorf("read source witness: %w", err)
	}
	primary, primaryRows, err := verifier.primary.client.readStoredSnapshot(ctx)
	if err != nil {
		return VerifiedSourceRetirement{}, fmt.Errorf("read source primary: %w", err)
	}
	observation, err := recorddeletion.VerifyLedgerSnapshotPair(primary, witness, verifier.trust)
	if err != nil {
		return VerifiedSourceRetirement{}, err
	}
	for index := range witnessRows {
		if !equalStoredRow(primaryRows[index], witnessRows[index]) {
			return VerifiedSourceRetirement{}, fmt.Errorf("%w: full witness projection fork", recorddeletion.ErrLedgerContinuity)
		}
	}
	if err := verifier.trust.Continuity.VerifyLedgerContinuity(ctx, observation); err != nil {
		return VerifiedSourceRetirement{}, fmt.Errorf("%w: source external fence: %v", recorddeletion.ErrLedgerContinuity, err)
	}
	if locator.Sequence > witness.Head.Sequence {
		if locator.Sequence <= primary.Head.Sequence && primary.Entries[locator.Sequence-1].EntryHash != locator.EntryHash {
			return VerifiedSourceRetirement{}, sourceInvalid("locator hash")
		}
		return VerifiedSourceRetirement{}, recorddeletion.ErrLedgerPending
	}
	row := primaryRows[locator.Sequence-1]
	if !claimsSourceCodec(row.canonical) {
		return VerifiedSourceRetirement{}, sourceInvalid("unknown locator codec")
	}
	entry, err := verifier.primary.client.sourceRow(row)
	if err != nil || entry.EntryHash != locator.EntryHash {
		return VerifiedSourceRetirement{}, sourceInvalid("locator entry")
	}
	proof, err := recorddeletion.LedgerWitnessProofDigest(domain, locator.Sequence, locator.EntryHash, witness.Identity.ID)
	if err != nil {
		return VerifiedSourceRetirement{}, err
	}
	return VerifiedSourceRetirement{entry: cloneSourceEntry(entry), locator: locator, primaryHead: primary.Head,
		witnessHead: witness.Head, proofDigest: proof, verified: true}, nil
}
