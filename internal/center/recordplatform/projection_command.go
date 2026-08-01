package recordplatform

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

const (
	projectionCommandMagic             = "HOUFENG-APP-PROJECTION-COMMAND-V1"
	projectionCommandVersion           = 1
	projectionCommandHeaderLength      = 37
	projectionDeploymentIDLength       = 67
	projectionMutationIDLength         = 67
	projectionDigestLength             = 32
	projectionActivationOperation      = 1
	projectionActivationFieldCount     = 18
	projectionActivationCommandLength  = 532
	projectionRotationOperation        = 2
	projectionRotationFieldCount       = 21
	projectionRotationCommandLength    = 508
	projectionCASReceiptDomainSeparate = "HOUFENG-APP-PROJECTION-CAS-RECEIPT-V1"
)

var ErrInvalidProjectionCommand = errors.New("invalid record-platform projection command")

type ProjectionProfile uint8

const (
	ProjectionProfilePostgresSync ProjectionProfile = 1
	ProjectionProfileS3WORM       ProjectionProfile = 2
)

// ContractActivationProjectionCommandV1 is the local APP projection for a
// runtime-verified contract activation. It does not validate external proof.
type ContractActivationProjectionCommandV1 struct {
	DeploymentID                string
	ActiveProfile               ProjectionProfile
	ActivationMutationID        string
	WitnessedLedgerSequence     uint64
	WitnessedLedgerHash         [projectionDigestLength]byte
	PlanDigest                  [projectionDigestLength]byte
	AuthorizationArtifactDigest [projectionDigestLength]byte
	ActivationBundleDigest      [projectionDigestLength]byte
	TrustRevision               uint64
	TrustHeadHash               [projectionDigestLength]byte
	InventoryDigest             [projectionDigestLength]byte
	ApprovalPolicyDigest        [projectionDigestLength]byte
	AdapterPolicyGeneration     uint64
	AdapterPolicyDigest         [projectionDigestLength]byte
	DrainReceiptDigest          [projectionDigestLength]byte
	IdentitySetEpoch            uint64
	IdentitySetDigest           [projectionDigestLength]byte
	MinimumFenceContractVersion uint64
}

// DomainRotationProjectionCommandV1 compares a fully witnessed active local
// projection and advances its ledger, identity, policy, fence, and trust data.
type DomainRotationProjectionCommandV1 struct {
	DeploymentID                        string
	ActiveProfile                       ProjectionProfile
	RotationMutationID                  string
	ExpectedWitnessedLedgerSequence     uint64
	ExpectedWitnessedLedgerHash         [projectionDigestLength]byte
	ExpectedIdentitySetEpoch            uint64
	ExpectedIdentitySetDigest           [projectionDigestLength]byte
	ExpectedAdapterPolicyGeneration     uint64
	ExpectedAdapterPolicyDigest         [projectionDigestLength]byte
	ExpectedMinimumFenceContractVersion uint64
	ExpectedTrustRevision               uint64
	ExpectedTrustHeadHash               [projectionDigestLength]byte
	NextWitnessedLedgerSequence         uint64
	NextWitnessedLedgerHash             [projectionDigestLength]byte
	NextIdentitySetEpoch                uint64
	NextIdentitySetDigest               [projectionDigestLength]byte
	NextAdapterPolicyGeneration         uint64
	NextAdapterPolicyDigest             [projectionDigestLength]byte
	NextMinimumFenceContractVersion     uint64
	NextTrustRevision                   uint64
	NextTrustHeadHash                   [projectionDigestLength]byte
}

func (command ContractActivationProjectionCommandV1) MarshalBinary() ([]byte, error) {
	if err := command.validate(); err != nil {
		return nil, err
	}

	raw := projectionCommandHeader(projectionActivationOperation, projectionActivationFieldCount, projectionActivationCommandLength)
	raw = append(raw, command.DeploymentID...)
	raw = append(raw, byte(command.ActiveProfile))
	raw = append(raw, command.ActivationMutationID...)
	raw = appendProjectionUint64(raw, command.WitnessedLedgerSequence)
	raw = append(raw, command.WitnessedLedgerHash[:]...)
	raw = append(raw, command.PlanDigest[:]...)
	raw = append(raw, command.AuthorizationArtifactDigest[:]...)
	raw = append(raw, command.ActivationBundleDigest[:]...)
	raw = appendProjectionUint64(raw, command.TrustRevision)
	raw = append(raw, command.TrustHeadHash[:]...)
	raw = append(raw, command.InventoryDigest[:]...)
	raw = append(raw, command.ApprovalPolicyDigest[:]...)
	raw = appendProjectionUint64(raw, command.AdapterPolicyGeneration)
	raw = append(raw, command.AdapterPolicyDigest[:]...)
	raw = append(raw, command.DrainReceiptDigest[:]...)
	raw = appendProjectionUint64(raw, command.IdentitySetEpoch)
	raw = append(raw, command.IdentitySetDigest[:]...)
	raw = appendProjectionUint64(raw, command.MinimumFenceContractVersion)
	return raw, nil
}

func ParseContractActivationProjectionCommandV1(raw []byte) (ContractActivationProjectionCommandV1, error) {
	if err := validateProjectionHeader(raw, projectionActivationOperation, projectionActivationFieldCount, projectionActivationCommandLength); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}

	reader := projectionCommandReader{raw: raw, offset: projectionCommandHeaderLength}
	command := ContractActivationProjectionCommandV1{}
	var err error
	if command.DeploymentID, err = reader.token(projectionDeploymentIDLength); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.ActiveProfile, err = reader.profile(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.ActivationMutationID, err = reader.token(projectionMutationIDLength); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.WitnessedLedgerSequence, err = reader.uint64(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.WitnessedLedgerHash, err = reader.digest(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.PlanDigest, err = reader.digest(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.AuthorizationArtifactDigest, err = reader.digest(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.ActivationBundleDigest, err = reader.digest(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.TrustRevision, err = reader.uint64(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.TrustHeadHash, err = reader.digest(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.InventoryDigest, err = reader.digest(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.ApprovalPolicyDigest, err = reader.digest(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.AdapterPolicyGeneration, err = reader.uint64(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.AdapterPolicyDigest, err = reader.digest(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.DrainReceiptDigest, err = reader.digest(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.IdentitySetEpoch, err = reader.uint64(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.IdentitySetDigest, err = reader.digest(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if command.MinimumFenceContractVersion, err = reader.uint64(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if err := reader.done(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	if err := command.validate(); err != nil {
		return ContractActivationProjectionCommandV1{}, err
	}
	return command, nil
}

func (command DomainRotationProjectionCommandV1) MarshalBinary() ([]byte, error) {
	if err := command.validate(); err != nil {
		return nil, err
	}

	raw := projectionCommandHeader(projectionRotationOperation, projectionRotationFieldCount, projectionRotationCommandLength)
	raw = append(raw, command.DeploymentID...)
	raw = append(raw, byte(command.ActiveProfile))
	raw = append(raw, command.RotationMutationID...)
	raw = appendProjectionUint64(raw, command.ExpectedWitnessedLedgerSequence)
	raw = append(raw, command.ExpectedWitnessedLedgerHash[:]...)
	raw = appendProjectionUint64(raw, command.ExpectedIdentitySetEpoch)
	raw = append(raw, command.ExpectedIdentitySetDigest[:]...)
	raw = appendProjectionUint64(raw, command.ExpectedAdapterPolicyGeneration)
	raw = append(raw, command.ExpectedAdapterPolicyDigest[:]...)
	raw = appendProjectionUint64(raw, command.ExpectedMinimumFenceContractVersion)
	raw = appendProjectionUint64(raw, command.ExpectedTrustRevision)
	raw = append(raw, command.ExpectedTrustHeadHash[:]...)
	raw = appendProjectionUint64(raw, command.NextWitnessedLedgerSequence)
	raw = append(raw, command.NextWitnessedLedgerHash[:]...)
	raw = appendProjectionUint64(raw, command.NextIdentitySetEpoch)
	raw = append(raw, command.NextIdentitySetDigest[:]...)
	raw = appendProjectionUint64(raw, command.NextAdapterPolicyGeneration)
	raw = append(raw, command.NextAdapterPolicyDigest[:]...)
	raw = appendProjectionUint64(raw, command.NextMinimumFenceContractVersion)
	raw = appendProjectionUint64(raw, command.NextTrustRevision)
	raw = append(raw, command.NextTrustHeadHash[:]...)
	return raw, nil
}

func ParseDomainRotationProjectionCommandV1(raw []byte) (DomainRotationProjectionCommandV1, error) {
	if err := validateProjectionHeader(raw, projectionRotationOperation, projectionRotationFieldCount, projectionRotationCommandLength); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}

	reader := projectionCommandReader{raw: raw, offset: projectionCommandHeaderLength}
	command := DomainRotationProjectionCommandV1{}
	var err error
	if command.DeploymentID, err = reader.token(projectionDeploymentIDLength); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.ActiveProfile, err = reader.profile(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.RotationMutationID, err = reader.token(projectionMutationIDLength); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.ExpectedWitnessedLedgerSequence, err = reader.uint64(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.ExpectedWitnessedLedgerHash, err = reader.digest(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.ExpectedIdentitySetEpoch, err = reader.uint64(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.ExpectedIdentitySetDigest, err = reader.digest(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.ExpectedAdapterPolicyGeneration, err = reader.uint64(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.ExpectedAdapterPolicyDigest, err = reader.digest(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.ExpectedMinimumFenceContractVersion, err = reader.uint64(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.ExpectedTrustRevision, err = reader.uint64(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.ExpectedTrustHeadHash, err = reader.digest(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.NextWitnessedLedgerSequence, err = reader.uint64(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.NextWitnessedLedgerHash, err = reader.digest(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.NextIdentitySetEpoch, err = reader.uint64(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.NextIdentitySetDigest, err = reader.digest(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.NextAdapterPolicyGeneration, err = reader.uint64(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.NextAdapterPolicyDigest, err = reader.digest(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.NextMinimumFenceContractVersion, err = reader.uint64(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.NextTrustRevision, err = reader.uint64(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if command.NextTrustHeadHash, err = reader.digest(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if err := reader.done(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	if err := command.validate(); err != nil {
		return DomainRotationProjectionCommandV1{}, err
	}
	return command, nil
}

// ProjectionCASReceiptDigestV1 derives the local receipt after a command has
// passed canonical validation. It binds both the exact length and bytes.
func ProjectionCASReceiptDigestV1(command []byte) [sha256.Size]byte {
	preimage := make([]byte, 0, len(projectionCASReceiptDomainSeparate)+4+len(command))
	preimage = append(preimage, projectionCASReceiptDomainSeparate...)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(command)))
	preimage = append(preimage, length[:]...)
	preimage = append(preimage, command...)
	return sha256.Sum256(preimage)
}

func (command ContractActivationProjectionCommandV1) validate() error {
	if !validProjectionToken(command.DeploymentID, "dp-") {
		return invalidProjectionCommand("deployment ID is not DeploymentIDV1")
	}
	if err := validateProjectionProfile(command.ActiveProfile); err != nil {
		return err
	}
	if !validProjectionToken(command.ActivationMutationID, "tm-") {
		return invalidProjectionCommand("activation mutation ID is not MutationIDV1")
	}
	if err := validatePositivePostgresBigInt(command.WitnessedLedgerSequence, "witnessed ledger sequence"); err != nil {
		return err
	}
	if command.WitnessedLedgerSequence != 1 {
		return invalidProjectionCommand("activation witnessed ledger sequence must be one")
	}
	if err := validatePositivePostgresBigInt(command.TrustRevision, "trust revision"); err != nil {
		return err
	}
	if command.AdapterPolicyGeneration != 1 {
		return invalidProjectionCommand("activation adapter policy generation must be one")
	}
	if command.IdentitySetEpoch != 1 {
		return invalidProjectionCommand("activation identity set epoch must be one")
	}
	if err := validatePositivePostgresBigInt(command.MinimumFenceContractVersion, "minimum fence contract version"); err != nil {
		return err
	}
	return nil
}

func (command DomainRotationProjectionCommandV1) validate() error {
	if !validProjectionToken(command.DeploymentID, "dp-") {
		return invalidProjectionCommand("deployment ID is not DeploymentIDV1")
	}
	if err := validateProjectionProfile(command.ActiveProfile); err != nil {
		return err
	}
	if !validProjectionToken(command.RotationMutationID, "tm-") {
		return invalidProjectionCommand("rotation mutation ID is not MutationIDV1")
	}
	for _, value := range []struct {
		name  string
		value uint64
	}{
		{name: "expected witnessed ledger sequence", value: command.ExpectedWitnessedLedgerSequence},
		{name: "expected identity set epoch", value: command.ExpectedIdentitySetEpoch},
		{name: "expected adapter policy generation", value: command.ExpectedAdapterPolicyGeneration},
		{name: "expected minimum fence contract version", value: command.ExpectedMinimumFenceContractVersion},
		{name: "expected trust revision", value: command.ExpectedTrustRevision},
		{name: "next witnessed ledger sequence", value: command.NextWitnessedLedgerSequence},
		{name: "next identity set epoch", value: command.NextIdentitySetEpoch},
		{name: "next adapter policy generation", value: command.NextAdapterPolicyGeneration},
		{name: "next minimum fence contract version", value: command.NextMinimumFenceContractVersion},
		{name: "next trust revision", value: command.NextTrustRevision},
	} {
		if err := validatePositivePostgresBigInt(value.value, value.name); err != nil {
			return err
		}
	}
	if command.NextWitnessedLedgerSequence <= command.ExpectedWitnessedLedgerSequence {
		return invalidProjectionCommand("next witnessed ledger sequence must advance")
	}
	if command.ExpectedIdentitySetEpoch == math.MaxInt64 || command.NextIdentitySetEpoch != command.ExpectedIdentitySetEpoch+1 {
		return invalidProjectionCommand("next identity set epoch must advance by one")
	}
	if command.NextIdentitySetDigest == command.ExpectedIdentitySetDigest {
		return invalidProjectionCommand("next identity set digest must change")
	}
	switch {
	case command.NextAdapterPolicyGeneration == command.ExpectedAdapterPolicyGeneration:
		if command.NextAdapterPolicyDigest != command.ExpectedAdapterPolicyDigest {
			return invalidProjectionCommand("unchanged adapter policy generation must retain its digest")
		}
	case command.ExpectedAdapterPolicyGeneration < math.MaxInt64 && command.NextAdapterPolicyGeneration == command.ExpectedAdapterPolicyGeneration+1:
		if command.NextAdapterPolicyDigest == command.ExpectedAdapterPolicyDigest {
			return invalidProjectionCommand("advanced adapter policy generation must change its digest")
		}
	default:
		return invalidProjectionCommand("next adapter policy generation must be unchanged or advance by one")
	}
	if command.NextMinimumFenceContractVersion < command.ExpectedMinimumFenceContractVersion {
		return invalidProjectionCommand("minimum fence contract version cannot decrease")
	}
	if command.NextTrustRevision < command.ExpectedTrustRevision {
		return invalidProjectionCommand("trust revision cannot decrease")
	}
	if command.NextTrustRevision == command.ExpectedTrustRevision && command.NextTrustHeadHash != command.ExpectedTrustHeadHash {
		return invalidProjectionCommand("unchanged trust revision must retain its hash")
	}
	return nil
}

func projectionCommandHeader(operation, fieldCount byte, length int) []byte {
	raw := make([]byte, 0, length)
	raw = append(raw, projectionCommandMagic...)
	raw = append(raw, 0, projectionCommandVersion, operation, fieldCount)
	return raw
}

func appendProjectionUint64(raw []byte, value uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	return append(raw, encoded[:]...)
}

func validateProjectionHeader(raw []byte, operation, fieldCount byte, exactLength int) error {
	if len(raw) != exactLength {
		return invalidProjectionCommand("length = %d, want %d", len(raw), exactLength)
	}
	if !bytes.Equal(raw[:len(projectionCommandMagic)], []byte(projectionCommandMagic)) {
		return invalidProjectionCommand("magic is not ProjectionCommandV1")
	}
	if raw[33] != 0 || raw[34] != projectionCommandVersion {
		return invalidProjectionCommand("version is not one")
	}
	if raw[35] != operation {
		return invalidProjectionCommand("operation = %d, want %d", raw[35], operation)
	}
	if raw[36] != fieldCount {
		return invalidProjectionCommand("field count = %d, want %d", raw[36], fieldCount)
	}
	return nil
}

type projectionCommandReader struct {
	raw    []byte
	offset int
}

func (reader *projectionCommandReader) token(length int) (string, error) {
	raw, err := reader.bytes(length)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (reader *projectionCommandReader) profile() (ProjectionProfile, error) {
	raw, err := reader.bytes(1)
	if err != nil {
		return 0, err
	}
	profile := ProjectionProfile(raw[0])
	if err := validateProjectionProfile(profile); err != nil {
		return 0, err
	}
	return profile, nil
}

func (reader *projectionCommandReader) uint64() (uint64, error) {
	raw, err := reader.bytes(8)
	if err != nil {
		return 0, err
	}
	value := binary.BigEndian.Uint64(raw)
	if value > math.MaxInt64 {
		return 0, invalidProjectionCommand("integer exceeds PostgreSQL bigint")
	}
	return value, nil
}

func (reader *projectionCommandReader) digest() ([projectionDigestLength]byte, error) {
	raw, err := reader.bytes(projectionDigestLength)
	if err != nil {
		return [projectionDigestLength]byte{}, err
	}
	var digest [projectionDigestLength]byte
	copy(digest[:], raw)
	return digest, nil
}

func (reader *projectionCommandReader) bytes(length int) ([]byte, error) {
	if length < 0 || reader.offset < 0 || reader.offset > len(reader.raw) || len(reader.raw)-reader.offset < length {
		return nil, invalidProjectionCommand("truncated field at offset %d", reader.offset)
	}
	value := reader.raw[reader.offset : reader.offset+length]
	reader.offset += length
	return value, nil
}

func (reader *projectionCommandReader) done() error {
	if reader.offset != len(reader.raw) {
		return invalidProjectionCommand("trailing bytes at offset %d", reader.offset)
	}
	return nil
}

func validateProjectionProfile(profile ProjectionProfile) error {
	if profile != ProjectionProfilePostgresSync && profile != ProjectionProfileS3WORM {
		return invalidProjectionCommand("unknown active profile %d", profile)
	}
	return nil
}

func validProjectionToken(value, prefix string) bool {
	if len(value) != projectionDeploymentIDLength || len(prefix) != 3 || !bytes.HasPrefix([]byte(value), []byte(prefix)) {
		return false
	}
	for index := len(prefix); index < len(value); index++ {
		if (value[index] < '0' || value[index] > '9') && (value[index] < 'a' || value[index] > 'f') {
			return false
		}
	}
	return true
}

func validatePositivePostgresBigInt(value uint64, name string) error {
	if value == 0 || value > math.MaxInt64 {
		return invalidProjectionCommand("%s is outside positive PostgreSQL bigint", name)
	}
	return nil
}

func invalidProjectionCommand(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidProjectionCommand, fmt.Sprintf(format, args...))
}
