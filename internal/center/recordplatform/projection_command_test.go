package recordplatform

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const projectionCommandTestMagic = "HOUFENG-APP-PROJECTION-COMMAND-V1"

const (
	projectionCommandTestHeaderLength = 37
	projectionActivationTestLength    = 532
	projectionRotationTestLength      = 508

	projectionActivationTestProfileOffset              = 104
	projectionActivationTestMutationOffset             = 105
	projectionActivationTestWitnessedSequenceOffset    = 172
	projectionActivationTestTrustRevisionOffset        = 308
	projectionActivationTestPolicyGenerationOffset     = 412
	projectionActivationTestIdentityEpochOffset        = 484
	projectionActivationTestMinimumFenceOffset         = 524
	projectionRotationTestProfileOffset                = 104
	projectionRotationTestExpectedSequenceOffset       = 172
	projectionRotationTestExpectedIdentityEpochOffset  = 212
	projectionRotationTestExpectedIdentityDigestOffset = 220
	projectionRotationTestExpectedPolicyGeneration     = 252
	projectionRotationTestExpectedPolicyDigestOffset   = 260
	projectionRotationTestExpectedFenceOffset          = 292
	projectionRotationTestExpectedTrustRevisionOffset  = 300
	projectionRotationTestExpectedTrustDigestOffset    = 308
	projectionRotationTestNextSequenceOffset           = 340
	projectionRotationTestNextIdentityEpochOffset      = 380
	projectionRotationTestNextIdentityDigestOffset     = 388
	projectionRotationTestNextPolicyGeneration         = 420
	projectionRotationTestNextPolicyDigestOffset       = 428
	projectionRotationTestNextFenceOffset              = 460
	projectionRotationTestNextTrustRevisionOffset      = 468
	projectionRotationTestNextTrustDigestOffset        = 476
)

func TestContractActivationProjectionCommandV1RoundTripUsesExactLayout(t *testing.T) {
	command := projectionTestActivationCommand()

	got, err := command.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	want := projectionTestActivationBytes(command)
	if !bytes.Equal(got, want) {
		t.Fatalf("MarshalBinary() = %x, want %x", got, want)
	}
	if len(got) != projectionActivationTestLength {
		t.Fatalf("activation command length = %d, want %d", len(got), projectionActivationTestLength)
	}
	if !bytes.Equal(got[:33], []byte(projectionCommandTestMagic)) {
		t.Fatalf("activation magic = %q, want %q", got[:33], projectionCommandTestMagic)
	}
	if got[33] != 0 || got[34] != 1 || got[35] != 1 || got[36] != 18 {
		t.Fatalf("activation header = %x, want version=1 operation=1 fields=18", got[33:37])
	}

	decoded, err := ParseContractActivationProjectionCommandV1(got)
	if err != nil {
		t.Fatalf("ParseContractActivationProjectionCommandV1() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, command) {
		t.Fatalf("decoded activation = %#v, want %#v", decoded, command)
	}
}

func TestDomainRotationProjectionCommandV1RoundTripUsesExactLayout(t *testing.T) {
	command := projectionTestRotationCommand()

	got, err := command.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	want := projectionTestRotationBytes(command)
	if !bytes.Equal(got, want) {
		t.Fatalf("MarshalBinary() = %x, want %x", got, want)
	}
	if len(got) != projectionRotationTestLength {
		t.Fatalf("rotation command length = %d, want %d", len(got), projectionRotationTestLength)
	}
	if !bytes.Equal(got[:33], []byte(projectionCommandTestMagic)) {
		t.Fatalf("rotation magic = %q, want %q", got[:33], projectionCommandTestMagic)
	}
	if got[33] != 0 || got[34] != 1 || got[35] != 2 || got[36] != 21 {
		t.Fatalf("rotation header = %x, want version=1 operation=2 fields=21", got[33:37])
	}

	decoded, err := ParseDomainRotationProjectionCommandV1(got)
	if err != nil {
		t.Fatalf("ParseDomainRotationProjectionCommandV1() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, command) {
		t.Fatalf("decoded rotation = %#v, want %#v", decoded, command)
	}
}

func TestDomainRotationProjectionCommandV1AllowsStrictlyAdvancingLedgerSequence(t *testing.T) {
	command := projectionTestRotationCommand()
	command.NextWitnessedLedgerSequence = command.ExpectedWitnessedLedgerSequence + 3

	raw, err := command.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	decoded, err := ParseDomainRotationProjectionCommandV1(raw)
	if err != nil {
		t.Fatalf("ParseDomainRotationProjectionCommandV1() error = %v", err)
	}
	if decoded.NextWitnessedLedgerSequence != command.NextWitnessedLedgerSequence {
		t.Fatalf("decoded next witnessed ledger sequence = %d, want %d", decoded.NextWitnessedLedgerSequence, command.NextWitnessedLedgerSequence)
	}
}

func TestProjectionCommandV1RejectsMalformedActivationEnvelope(t *testing.T) {
	valid := projectionTestActivationBytes(projectionTestActivationCommand())

	cases := []struct {
		name string
		raw  []byte
	}{
		{name: "empty", raw: nil},
		{name: "wrong magic", raw: projectionTestMutate(valid, func(raw []byte) { raw[0] = 'X' })},
		{name: "wrong version", raw: projectionTestMutate(valid, func(raw []byte) { raw[34] = 2 })},
		{name: "foreign operation", raw: projectionTestMutate(valid, func(raw []byte) { raw[35] = 2 })},
		{name: "wrong field count", raw: projectionTestMutate(valid, func(raw []byte) { raw[36] = 19 })},
		{name: "truncated", raw: append([]byte(nil), valid[:len(valid)-1]...)},
		{name: "trailing byte", raw: append(append([]byte(nil), valid...), 0)},
		{name: "bad deployment token", raw: projectionTestMutate(valid, func(raw []byte) { raw[projectionCommandTestHeaderLength+3] = 'A' })},
		{name: "bad mutation token", raw: projectionTestMutate(valid, func(raw []byte) { raw[projectionActivationTestMutationOffset+3] = 'B' })},
		{name: "unknown profile", raw: projectionTestMutate(valid, func(raw []byte) { raw[projectionActivationTestProfileOffset] = 3 })},
		{name: "witnessed sequence is not one", raw: projectionTestMutate(valid, func(raw []byte) {
			binary.BigEndian.PutUint64(raw[projectionActivationTestWitnessedSequenceOffset:], 2)
		})},
		{name: "zero trust revision", raw: projectionTestMutate(valid, func(raw []byte) {
			binary.BigEndian.PutUint64(raw[projectionActivationTestTrustRevisionOffset:], 0)
		})},
		{name: "wrong initial policy generation", raw: projectionTestMutate(valid, func(raw []byte) {
			binary.BigEndian.PutUint64(raw[projectionActivationTestPolicyGenerationOffset:], 2)
		})},
		{name: "wrong initial identity epoch", raw: projectionTestMutate(valid, func(raw []byte) {
			binary.BigEndian.PutUint64(raw[projectionActivationTestIdentityEpochOffset:], 2)
		})},
		{name: "zero minimum fence", raw: projectionTestMutate(valid, func(raw []byte) {
			binary.BigEndian.PutUint64(raw[projectionActivationTestMinimumFenceOffset:], 0)
		})},
	}
	for _, offset := range []int{
		projectionActivationTestWitnessedSequenceOffset,
		projectionActivationTestTrustRevisionOffset,
		projectionActivationTestPolicyGenerationOffset,
		projectionActivationTestIdentityEpochOffset,
		projectionActivationTestMinimumFenceOffset,
	} {
		cases = append(cases, struct {
			name string
			raw  []byte
		}{
			name: "high-bit integer at offset " + strconv.Itoa(offset),
			raw:  projectionTestMutate(valid, func(raw []byte) { raw[offset] |= 0x80 }),
		})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseContractActivationProjectionCommandV1(tc.raw); err == nil {
				t.Fatalf("ParseContractActivationProjectionCommandV1(%x) error = nil, want rejection", tc.raw)
			}
		})
	}
}

func TestProjectionCommandV1RejectsMalformedRotationEnvelopeAndTransitions(t *testing.T) {
	valid := projectionTestRotationBytes(projectionTestRotationCommand())

	cases := []struct {
		name string
		raw  []byte
	}{
		{name: "foreign operation", raw: projectionTestMutate(valid, func(raw []byte) { raw[35] = 1 })},
		{name: "wrong field count", raw: projectionTestMutate(valid, func(raw []byte) { raw[36] = 20 })},
		{name: "truncated", raw: append([]byte(nil), valid[:len(valid)-1]...)},
		{name: "trailing byte", raw: append(append([]byte(nil), valid...), 0)},
		{name: "next ledger does not advance", raw: projectionTestMutate(valid, func(raw []byte) {
			binary.BigEndian.PutUint64(raw[projectionRotationTestNextSequenceOffset:], 9)
		})},
		{name: "next identity epoch skips", raw: projectionTestMutate(valid, func(raw []byte) {
			binary.BigEndian.PutUint64(raw[projectionRotationTestNextIdentityEpochOffset:], 5)
		})},
		{name: "next identity digest does not change", raw: projectionTestMutate(valid, func(raw []byte) {
			copy(raw[projectionRotationTestNextIdentityDigestOffset:], raw[projectionRotationTestExpectedIdentityDigestOffset:projectionRotationTestExpectedIdentityDigestOffset+32])
		})},
		{name: "same policy generation changes digest", raw: projectionTestMutate(valid, func(raw []byte) {
			binary.BigEndian.PutUint64(raw[projectionRotationTestNextPolicyGeneration:], 5)
			raw[projectionRotationTestNextPolicyDigestOffset] ^= 0xff
		})},
		{name: "policy skips generation", raw: projectionTestMutate(valid, func(raw []byte) {
			binary.BigEndian.PutUint64(raw[projectionRotationTestNextPolicyGeneration:], 7)
		})},
		{name: "fence decreases", raw: projectionTestMutate(valid, func(raw []byte) {
			binary.BigEndian.PutUint64(raw[projectionRotationTestNextFenceOffset:], 6)
		})},
		{name: "trust revision decreases", raw: projectionTestMutate(valid, func(raw []byte) {
			binary.BigEndian.PutUint64(raw[projectionRotationTestNextTrustRevisionOffset:], 8)
		})},
		{name: "same trust revision changes hash", raw: projectionTestMutate(valid, func(raw []byte) {
			binary.BigEndian.PutUint64(raw[projectionRotationTestNextTrustRevisionOffset:], 9)
			raw[projectionRotationTestNextTrustDigestOffset] ^= 0xff
		})},
	}
	for _, offset := range []int{
		projectionRotationTestExpectedSequenceOffset,
		projectionRotationTestExpectedIdentityEpochOffset,
		projectionRotationTestExpectedPolicyGeneration,
		projectionRotationTestExpectedFenceOffset,
		projectionRotationTestExpectedTrustRevisionOffset,
		projectionRotationTestNextSequenceOffset,
		projectionRotationTestNextIdentityEpochOffset,
		projectionRotationTestNextPolicyGeneration,
		projectionRotationTestNextFenceOffset,
		projectionRotationTestNextTrustRevisionOffset,
	} {
		cases = append(cases, struct {
			name string
			raw  []byte
		}{
			name: "high-bit integer at offset " + strconv.Itoa(offset),
			raw:  projectionTestMutate(valid, func(raw []byte) { raw[offset] |= 0x80 }),
		})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseDomainRotationProjectionCommandV1(tc.raw); err == nil {
				t.Fatalf("ParseDomainRotationProjectionCommandV1(%x) error = nil, want rejection", tc.raw)
			}
		})
	}
}

func TestProjectionCommandV1MarshalRejectsInvalidValues(t *testing.T) {
	t.Run("activation", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*ContractActivationProjectionCommandV1)
		}{
			{name: "bad deployment", mutate: func(command *ContractActivationProjectionCommandV1) {
				command.DeploymentID = strings.ToUpper(command.DeploymentID)
			}},
			{name: "bad mutation", mutate: func(command *ContractActivationProjectionCommandV1) { command.ActivationMutationID = "tm-short" }},
			{name: "unknown profile", mutate: func(command *ContractActivationProjectionCommandV1) { command.ActiveProfile = ProjectionProfile(3) }},
			{name: "high-bit sequence", mutate: func(command *ContractActivationProjectionCommandV1) {
				command.WitnessedLedgerSequence = uint64(1) << 63
			}},
			{name: "zero fence", mutate: func(command *ContractActivationProjectionCommandV1) { command.MinimumFenceContractVersion = 0 }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				command := projectionTestActivationCommand()
				tc.mutate(&command)
				if _, err := command.MarshalBinary(); err == nil {
					t.Fatal("MarshalBinary() error = nil, want rejection")
				}
			})
		}
	})

	t.Run("rotation", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*DomainRotationProjectionCommandV1)
		}{
			{name: "next identity does not change", mutate: func(command *DomainRotationProjectionCommandV1) {
				command.NextIdentitySetDigest = command.ExpectedIdentitySetDigest
			}},
			{name: "policy generation skips", mutate: func(command *DomainRotationProjectionCommandV1) { command.NextAdapterPolicyGeneration += 2 }},
			{name: "fence decreases", mutate: func(command *DomainRotationProjectionCommandV1) {
				command.NextMinimumFenceContractVersion = command.ExpectedMinimumFenceContractVersion - 1
			}},
			{name: "trust hash changes without revision", mutate: func(command *DomainRotationProjectionCommandV1) {
				command.NextTrustHeadHash[0] ^= 0xff
				command.NextTrustRevision = command.ExpectedTrustRevision
			}},
			{name: "high-bit next sequence", mutate: func(command *DomainRotationProjectionCommandV1) {
				command.NextWitnessedLedgerSequence = uint64(1) << 63
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				command := projectionTestRotationCommand()
				tc.mutate(&command)
				if _, err := command.MarshalBinary(); err == nil {
					t.Fatal("MarshalBinary() error = nil, want rejection")
				}
			})
		}
	})
}

func TestProjectionCASReceiptDigestV1BindsExactCommandLengthAndBytes(t *testing.T) {
	command := projectionTestActivationBytes(projectionTestActivationCommand())
	got := ProjectionCASReceiptDigestV1(command)

	preimage := append([]byte("HOUFENG-APP-PROJECTION-CAS-RECEIPT-V1"), make([]byte, 4)...)
	binary.BigEndian.PutUint32(preimage[len("HOUFENG-APP-PROJECTION-CAS-RECEIPT-V1"):], uint32(len(command)))
	preimage = append(preimage, command...)
	want := sha256.Sum256(preimage)
	if got != want {
		t.Fatalf("ProjectionCASReceiptDigestV1() = %x, want %x", got, want)
	}

	other := append([]byte(nil), command...)
	other[len(other)-1] ^= 0xff
	if ProjectionCASReceiptDigestV1(other) == got {
		t.Fatal("ProjectionCASReceiptDigestV1() did not bind exact command bytes")
	}
}

func projectionTestActivationCommand() ContractActivationProjectionCommandV1 {
	return ContractActivationProjectionCommandV1{
		DeploymentID:                "dp-" + strings.Repeat("a", 64),
		ActiveProfile:               ProjectionProfilePostgresSync,
		ActivationMutationID:        "tm-" + strings.Repeat("b", 64),
		WitnessedLedgerSequence:     1,
		WitnessedLedgerHash:         projectionTestDigest(1),
		PlanDigest:                  projectionTestDigest(2),
		AuthorizationArtifactDigest: projectionTestDigest(3),
		ActivationBundleDigest:      projectionTestDigest(4),
		TrustRevision:               5,
		TrustHeadHash:               projectionTestDigest(6),
		InventoryDigest:             projectionTestDigest(7),
		ApprovalPolicyDigest:        projectionTestDigest(8),
		AdapterPolicyGeneration:     1,
		AdapterPolicyDigest:         projectionTestDigest(9),
		DrainReceiptDigest:          projectionTestDigest(10),
		IdentitySetEpoch:            1,
		IdentitySetDigest:           projectionTestDigest(11),
		MinimumFenceContractVersion: 12,
	}
}

func projectionTestRotationCommand() DomainRotationProjectionCommandV1 {
	return DomainRotationProjectionCommandV1{
		DeploymentID:                        "dp-" + strings.Repeat("c", 64),
		ActiveProfile:                       ProjectionProfileS3WORM,
		RotationMutationID:                  "tm-" + strings.Repeat("d", 64),
		ExpectedWitnessedLedgerSequence:     9,
		ExpectedWitnessedLedgerHash:         projectionTestDigest(13),
		ExpectedIdentitySetEpoch:            3,
		ExpectedIdentitySetDigest:           projectionTestDigest(14),
		ExpectedAdapterPolicyGeneration:     5,
		ExpectedAdapterPolicyDigest:         projectionTestDigest(15),
		ExpectedMinimumFenceContractVersion: 7,
		ExpectedTrustRevision:               9,
		ExpectedTrustHeadHash:               projectionTestDigest(16),
		NextWitnessedLedgerSequence:         10,
		NextWitnessedLedgerHash:             projectionTestDigest(17),
		NextIdentitySetEpoch:                4,
		NextIdentitySetDigest:               projectionTestDigest(18),
		NextAdapterPolicyGeneration:         6,
		NextAdapterPolicyDigest:             projectionTestDigest(19),
		NextMinimumFenceContractVersion:     8,
		NextTrustRevision:                   10,
		NextTrustHeadHash:                   projectionTestDigest(20),
	}
}

func projectionTestActivationBytes(command ContractActivationProjectionCommandV1) []byte {
	raw := projectionTestHeader(1, 18)
	raw = append(raw, command.DeploymentID...)
	raw = append(raw, byte(command.ActiveProfile))
	raw = append(raw, command.ActivationMutationID...)
	raw = projectionTestAppendUint64(raw, command.WitnessedLedgerSequence)
	raw = append(raw, command.WitnessedLedgerHash[:]...)
	raw = append(raw, command.PlanDigest[:]...)
	raw = append(raw, command.AuthorizationArtifactDigest[:]...)
	raw = append(raw, command.ActivationBundleDigest[:]...)
	raw = projectionTestAppendUint64(raw, command.TrustRevision)
	raw = append(raw, command.TrustHeadHash[:]...)
	raw = append(raw, command.InventoryDigest[:]...)
	raw = append(raw, command.ApprovalPolicyDigest[:]...)
	raw = projectionTestAppendUint64(raw, command.AdapterPolicyGeneration)
	raw = append(raw, command.AdapterPolicyDigest[:]...)
	raw = append(raw, command.DrainReceiptDigest[:]...)
	raw = projectionTestAppendUint64(raw, command.IdentitySetEpoch)
	raw = append(raw, command.IdentitySetDigest[:]...)
	raw = projectionTestAppendUint64(raw, command.MinimumFenceContractVersion)
	return raw
}

func projectionTestRotationBytes(command DomainRotationProjectionCommandV1) []byte {
	raw := projectionTestHeader(2, 21)
	raw = append(raw, command.DeploymentID...)
	raw = append(raw, byte(command.ActiveProfile))
	raw = append(raw, command.RotationMutationID...)
	raw = projectionTestAppendUint64(raw, command.ExpectedWitnessedLedgerSequence)
	raw = append(raw, command.ExpectedWitnessedLedgerHash[:]...)
	raw = projectionTestAppendUint64(raw, command.ExpectedIdentitySetEpoch)
	raw = append(raw, command.ExpectedIdentitySetDigest[:]...)
	raw = projectionTestAppendUint64(raw, command.ExpectedAdapterPolicyGeneration)
	raw = append(raw, command.ExpectedAdapterPolicyDigest[:]...)
	raw = projectionTestAppendUint64(raw, command.ExpectedMinimumFenceContractVersion)
	raw = projectionTestAppendUint64(raw, command.ExpectedTrustRevision)
	raw = append(raw, command.ExpectedTrustHeadHash[:]...)
	raw = projectionTestAppendUint64(raw, command.NextWitnessedLedgerSequence)
	raw = append(raw, command.NextWitnessedLedgerHash[:]...)
	raw = projectionTestAppendUint64(raw, command.NextIdentitySetEpoch)
	raw = append(raw, command.NextIdentitySetDigest[:]...)
	raw = projectionTestAppendUint64(raw, command.NextAdapterPolicyGeneration)
	raw = append(raw, command.NextAdapterPolicyDigest[:]...)
	raw = projectionTestAppendUint64(raw, command.NextMinimumFenceContractVersion)
	raw = projectionTestAppendUint64(raw, command.NextTrustRevision)
	raw = append(raw, command.NextTrustHeadHash[:]...)
	return raw
}

func projectionTestHeader(operation, fieldCount byte) []byte {
	raw := make([]byte, 0, projectionRotationTestLength)
	raw = append(raw, projectionCommandTestMagic...)
	raw = append(raw, 0, 1, operation, fieldCount)
	return raw
}

func projectionTestAppendUint64(raw []byte, value uint64) []byte {
	encoded := make([]byte, 8)
	binary.BigEndian.PutUint64(encoded, value)
	return append(raw, encoded...)
}

func projectionTestDigest(value byte) [32]byte {
	var digest [32]byte
	for index := range digest {
		digest[index] = value
	}
	return digest
}

func projectionTestMutate(raw []byte, mutate func([]byte)) []byte {
	copyOfRaw := append([]byte(nil), raw...)
	mutate(copyOfRaw)
	return copyOfRaw
}
