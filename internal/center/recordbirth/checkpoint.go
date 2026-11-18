package recordbirth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"math"
)

const (
	checkpointEntryDomain   = "houfeng.record-birth.app-checkpoint-entry.v1"
	checkpointProofDomain   = "houfeng.record-birth.app-checkpoint.v1"
	maxCheckpointProofBytes = 4096
)

// CheckpointHead identifies a position in the APP checkpoint chain. The zero
// head denotes an empty chain; it is not evidence of request absence.
type CheckpointHead struct {
	Sequence uint64
	Hash     [32]byte
}

func (head CheckpointHead) Validate() error {
	if head.Sequence > math.MaxInt64 || (head.Sequence == 0) != (head.Hash == ([32]byte{})) {
		return ErrInvalid
	}
	return nil
}

// Checkpoint is the immutable allocation entry created under its original
// binding. Later generations must retain that binding in the encoded entry.
type Checkpoint struct {
	Binding      Binding
	Sequence     uint64
	PreviousHash [32]byte
	AllocationID string
	Request      Request
}

func (checkpoint Checkpoint) Validate() error {
	if checkpoint.Binding.Validate() != nil || checkpoint.Request.Validate() != nil ||
		checkpoint.Request.ProjectID != checkpoint.Binding.ProjectID ||
		checkpoint.Sequence == 0 || checkpoint.Sequence > math.MaxInt64 ||
		(checkpoint.PreviousHash == ([32]byte{})) != (checkpoint.Sequence == 1) ||
		!hexIdentity(checkpoint.AllocationID, "rqa_", 32) {
		return ErrInvalid
	}
	return nil
}

// EncodeCheckpoint returns the canonical APP chain entry, without a MAC.
func EncodeCheckpoint(checkpoint Checkpoint) ([]byte, error) {
	if err := checkpoint.Validate(); err != nil {
		return nil, err
	}
	encoded := make([]byte, 0, checkpointSize(checkpoint))
	return appendCheckpoint(encoded, checkpoint), nil
}

// Hash returns the SHA-256 hash of the complete canonical checkpoint entry.
func (checkpoint Checkpoint) Hash() ([32]byte, error) {
	encoded, err := EncodeCheckpoint(checkpoint)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

// CheckpointProof binds an original checkpoint to the current authority and
// the expected and proposed heads. Its codec checks endpoint consistency, not
// ancestry or current admission; those require the trusted database protocol.
type CheckpointProof struct {
	Binding        Binding
	ExpectedAnchor CheckpointHead
	ProposedHead   CheckpointHead
	Checkpoint     Checkpoint
}

func (proof CheckpointProof) Validate() error {
	if err := proof.validateStructure(); err != nil {
		return err
	}
	hash, err := proof.Checkpoint.Hash()
	if err != nil {
		return err
	}
	return proof.validateCheckpointHash(hash)
}

func (proof CheckpointProof) validateStructure() error {
	original := proof.Checkpoint.Binding
	if proof.Binding.Validate() != nil || proof.Checkpoint.Validate() != nil ||
		proof.ExpectedAnchor.Validate() != nil || proof.ProposedHead.Validate() != nil ||
		proof.Binding.AuthorityID != original.AuthorityID || proof.Binding.LineageID != original.LineageID ||
		proof.Binding.DeploymentID != original.DeploymentID || proof.Binding.ProjectID != original.ProjectID ||
		original.Generation > proof.Binding.Generation || proof.ProposedHead.Sequence == 0 ||
		proof.ExpectedAnchor.Sequence > proof.ProposedHead.Sequence ||
		proof.Checkpoint.Sequence > proof.ProposedHead.Sequence ||
		(proof.ExpectedAnchor.Sequence == proof.ProposedHead.Sequence && proof.ExpectedAnchor.Hash != proof.ProposedHead.Hash) {
		return ErrInvalid
	}
	return nil
}

func (proof CheckpointProof) validateCheckpointHash(hash [32]byte) error {
	if (proof.ExpectedAnchor.Sequence == proof.Checkpoint.Sequence && proof.ExpectedAnchor.Hash != hash) ||
		(proof.ProposedHead.Sequence == proof.Checkpoint.Sequence && proof.ProposedHead.Hash != hash) {
		return ErrInvalid
	}
	return nil
}

// EncodeCheckpointProof returns the unsigned canonical proof. The trusted APP
// database appends its HMAC; this package does not issue or sign proofs.
func EncodeCheckpointProof(proof CheckpointProof) ([]byte, error) {
	if err := proof.validateStructure(); err != nil {
		return nil, err
	}
	size := 4 + len(checkpointProofDomain) + checkpointBindingSize(proof.Binding) +
		2*(8+sha256.Size) + checkpointSize(proof.Checkpoint) + sha256.Size
	if size+sha256.Size > maxCheckpointProofBytes {
		return nil, ErrInvalid
	}
	encoded := make([]byte, 0, size)
	encoded = appendText(encoded, checkpointProofDomain)
	encoded = appendCheckpointBinding(encoded, proof.Binding)
	encoded = binary.BigEndian.AppendUint64(encoded, proof.ExpectedAnchor.Sequence)
	encoded = append(encoded, proof.ExpectedAnchor.Hash[:]...)
	encoded = binary.BigEndian.AppendUint64(encoded, proof.ProposedHead.Sequence)
	encoded = append(encoded, proof.ProposedHead.Hash[:]...)
	entryStart := len(encoded)
	encoded = appendCheckpoint(encoded, proof.Checkpoint)
	hash := sha256.Sum256(encoded[entryStart:])
	if err := proof.validateCheckpointHash(hash); err != nil {
		return nil, err
	}
	encoded = append(encoded, hash[:]...)
	return encoded, nil
}

// DecodeCheckpointProof validates framing and endpoint consistency, but does
// not authenticate the appended MAC or establish ancestry or admission.
func DecodeCheckpointProof(raw []byte) (CheckpointProof, error) {
	if len(raw) < sha256.Size || len(raw) > maxCheckpointProofBytes {
		return CheckpointProof{}, ErrInvalid
	}
	canonical := raw[:len(raw)-sha256.Size]
	reader := receiptReader{rest: canonical}
	if reader.text(128) != checkpointProofDomain {
		return CheckpointProof{}, ErrInvalid
	}
	proof := CheckpointProof{}
	proof.Binding = readCheckpointBinding(&reader)
	proof.ExpectedAnchor.Sequence = reader.uint64()
	proof.ExpectedAnchor.Hash = reader.digest()
	proof.ProposedHead.Sequence = reader.uint64()
	proof.ProposedHead.Hash = reader.digest()
	entryStart := len(canonical) - len(reader.rest)
	proof.Checkpoint = readCheckpoint(&reader)
	entryEnd := len(canonical) - len(reader.rest)
	entryHash := reader.digest()
	if reader.failed || len(reader.rest) != 0 || proof.validateStructure() != nil {
		return CheckpointProof{}, ErrInvalid
	}
	hash := sha256.Sum256(canonical[entryStart:entryEnd])
	if entryHash != hash || proof.validateCheckpointHash(hash) != nil {
		return CheckpointProof{}, ErrInvalid
	}
	return proof, nil
}

// VerifyCheckpointProof checks a MAC for trusted parity tests or key holders.
// Private keys remain in the database; this does not establish admission.
func VerifyCheckpointProof(raw, key []byte) (CheckpointProof, error) {
	proof, err := DecodeCheckpointProof(raw)
	if err != nil || len(key) != sha256.Size {
		return CheckpointProof{}, ErrInvalid
	}
	canonical := raw[:len(raw)-sha256.Size]
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	var expected [sha256.Size]byte
	if !hmac.Equal(mac.Sum(expected[:0]), raw[len(canonical):]) {
		return CheckpointProof{}, ErrInvalid
	}
	return proof, nil
}

func checkpointBindingSize(binding Binding) int {
	return 5*4 + len(binding.AuthorityID) + len(binding.LineageID) + len(binding.DeploymentID) +
		len(binding.ProjectID) + len(binding.KeyID) + 8 + sha256.Size
}

func checkpointSize(checkpoint Checkpoint) int {
	return 4 + len(checkpointEntryDomain) + checkpointBindingSize(checkpoint.Binding) + 8 + sha256.Size +
		4 + len(checkpoint.AllocationID) + 4 + len(checkpoint.Request.ActorID) + sha256.Size +
		4 + len(checkpoint.Request.Purpose) + 4 + len(checkpoint.Request.IdempotencyKey) + sha256.Size
}

func appendCheckpointBinding(dst []byte, binding Binding) []byte {
	dst = appendText(dst, binding.AuthorityID)
	dst = appendText(dst, binding.LineageID)
	dst = appendText(dst, binding.DeploymentID)
	dst = appendText(dst, binding.ProjectID)
	dst = binary.BigEndian.AppendUint64(dst, binding.Generation)
	dst = appendText(dst, binding.KeyID)
	return append(dst, binding.CutoverDigest[:]...)
}

func appendCheckpoint(dst []byte, checkpoint Checkpoint) []byte {
	dst = appendText(dst, checkpointEntryDomain)
	dst = appendCheckpointBinding(dst, checkpoint.Binding)
	dst = binary.BigEndian.AppendUint64(dst, checkpoint.Sequence)
	dst = append(dst, checkpoint.PreviousHash[:]...)
	dst = appendText(dst, checkpoint.AllocationID)
	dst = appendText(dst, checkpoint.Request.ActorID)
	dst = append(dst, checkpoint.Request.ActorScopeDigest[:]...)
	dst = appendText(dst, string(checkpoint.Request.Purpose))
	dst = appendText(dst, checkpoint.Request.IdempotencyKey)
	return append(dst, checkpoint.Request.RequestFingerprint[:]...)
}

func readCheckpointBinding(reader *receiptReader) Binding {
	binding := Binding{}
	binding.AuthorityID = reader.text(67)
	binding.LineageID = reader.text(67)
	binding.DeploymentID = reader.text(67)
	binding.ProjectID = reader.text(128)
	binding.Generation = reader.uint64()
	binding.KeyID = reader.text(67)
	binding.CutoverDigest = reader.digest()
	return binding
}

func readCheckpoint(reader *receiptReader) Checkpoint {
	if reader.text(128) != checkpointEntryDomain {
		reader.failed = true
		return Checkpoint{}
	}
	checkpoint := Checkpoint{}
	checkpoint.Binding = readCheckpointBinding(reader)
	checkpoint.Sequence = reader.uint64()
	checkpoint.PreviousHash = reader.digest()
	checkpoint.AllocationID = reader.text(36)
	checkpoint.Request.ProjectID = checkpoint.Binding.ProjectID
	checkpoint.Request.ActorID = reader.text(128)
	checkpoint.Request.ActorScopeDigest = reader.digest()
	checkpoint.Request.Purpose = Purpose(reader.text(32))
	checkpoint.Request.IdempotencyKey = reader.text(200)
	checkpoint.Request.RequestFingerprint = reader.digest()
	return checkpoint
}
