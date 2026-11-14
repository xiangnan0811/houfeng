package recordbirth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
)

const (
	continuityDomain        = "houfeng.record-birth.continuity.v1"
	maxContinuityProofBytes = 4096
)

// ContinuityObservation carries the current binding and APP checkpoint anchor
// observed at the authority. Proof contains the complete signed wire token.
// Possession of a token alone does not establish that the observation is fresh.
type ContinuityObservation struct {
	Binding Binding
	Anchor  CheckpointHead
	Enabled bool
	Proof   []byte
}

// ContinuityReader reads the current authoritative continuity state. Callers
// must obtain a fresh observation for each operation that requires continuity,
// compare its projection with the decoded proof, and apply the APP admission and
// ancestry checks. The runtime reader does not possess the signing key.
type ContinuityReader interface {
	Continuity(context.Context, Binding) (ContinuityObservation, error)
}

func (observation ContinuityObservation) validateStructure() error {
	if observation.Binding.Validate() != nil || observation.Anchor.Validate() != nil {
		return ErrInvalid
	}
	return nil
}

// EncodeContinuityObservation returns unsigned canonical bytes for database
// parity. It encodes the projection only and neither signs nor checks Proof.
func EncodeContinuityObservation(observation ContinuityObservation) ([]byte, error) {
	if err := observation.validateStructure(); err != nil {
		return nil, err
	}
	size := 4 + len(continuityDomain) + checkpointBindingSize(observation.Binding) + 8 + sha256.Size + 1
	if size+sha256.Size > maxContinuityProofBytes {
		return nil, ErrInvalid
	}
	encoded := make([]byte, 0, size)
	encoded = appendText(encoded, continuityDomain)
	encoded = appendCheckpointBinding(encoded, observation.Binding)
	encoded = binary.BigEndian.AppendUint64(encoded, observation.Anchor.Sequence)
	encoded = append(encoded, observation.Anchor.Hash[:]...)
	if observation.Enabled {
		encoded = append(encoded, 1)
	} else {
		encoded = append(encoded, 0)
	}
	return encoded, nil
}

// DecodeContinuityProof validates the complete token's structure and retains an
// independent copy of it. It does not authenticate the MAC or prove freshness.
func DecodeContinuityProof(raw []byte) (ContinuityObservation, error) {
	if len(raw) < sha256.Size || len(raw) > maxContinuityProofBytes {
		return ContinuityObservation{}, ErrInvalid
	}
	reader := receiptReader{rest: raw[:len(raw)-sha256.Size]}
	if reader.text(128) != continuityDomain {
		return ContinuityObservation{}, ErrInvalid
	}
	observation := ContinuityObservation{}
	observation.Binding = readCheckpointBinding(&reader)
	observation.Anchor.Sequence = reader.uint64()
	observation.Anchor.Hash = reader.digest()
	enabled := reader.take(1)
	if reader.failed || len(reader.rest) != 0 || enabled[0] > 1 {
		return ContinuityObservation{}, ErrInvalid
	}
	observation.Enabled = enabled[0] == 1
	if err := observation.validateStructure(); err != nil {
		return ContinuityObservation{}, err
	}
	observation.Proof = append([]byte(nil), raw...)
	return observation, nil
}

// VerifyContinuityProof verifies HMAC parity for trusted key holders. Runtime
// code does not read the key, and MAC validity does not establish freshness.
func VerifyContinuityProof(raw, key []byte) (ContinuityObservation, error) {
	if len(key) != sha256.Size || len(raw) < sha256.Size || len(raw) > maxContinuityProofBytes {
		return ContinuityObservation{}, ErrInvalid
	}
	canonical := raw[:len(raw)-sha256.Size]
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	var expected [sha256.Size]byte
	if !hmac.Equal(mac.Sum(expected[:0]), raw[len(canonical):]) {
		return ContinuityObservation{}, ErrInvalid
	}
	return DecodeContinuityProof(raw)
}
