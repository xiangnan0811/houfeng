// Package recordbirth coordinates durable record allocation and creation.
// A record's spelling alone never establishes allocation or certification.
package recordbirth

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"strings"

	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/recordplatform"
)

var (
	ErrInvalid     = errors.New("invalid record birth command")
	ErrConflict    = errors.New("record birth binding conflict")
	ErrPending     = errors.New("record birth outcome pending")
	ErrUnavailable = errors.New("record birth authority unavailable")
	ErrUncertified = errors.New("record birth is not certified")
)

// Purpose is fixed before any pre-root content effect.
type Purpose string

const (
	PurposeCreate     Purpose = "record_create"
	PurposeEvidence   Purpose = "evidence_preview"
	PurposeComparison Purpose = "comparison_save"
	PurposeImport     Purpose = "import"
)

// Request identifies one allocation independently of deployment generations.
// ActorID participates in the stable key; ActorScopeDigest is an immutable
// binding, so a changed scope cannot allocate a second ID for the same request.
type Request struct {
	ProjectID          string
	ActorID            string
	ActorScopeDigest   [32]byte
	Purpose            Purpose
	IdempotencyKey     string
	RequestFingerprint [32]byte
}

func (request Request) Validate() error {
	if request.ProjectID != string(recordplatform.ProjectIDDefault) || !opaqueID(request.ActorID) ||
		request.ActorScopeDigest == ([32]byte{}) || request.RequestFingerprint == ([32]byte{}) {
		return ErrInvalid
	}
	switch request.Purpose {
	case PurposeCreate, PurposeEvidence, PurposeComparison, PurposeImport:
	default:
		return ErrInvalid
	}
	key := recordplatform.IdempotencyKey{
		ProjectID:     recordplatform.ProjectIDDefault,
		OperationKind: recordplatform.OperationKindRecordCreate,
		Key:           request.IdempotencyKey,
	}
	if key.Validate() != nil {
		return ErrInvalid
	}
	return nil
}

// Binding identifies an enabled W2 birth authority. Its existence in memory is
// not proof: both databases validate their own current protected binding.
type Binding struct {
	AuthorityID   string
	LineageID     string
	DeploymentID  string
	ProjectID     string
	Generation    uint64
	KeyID         string
	CutoverDigest [32]byte
}

func (binding Binding) Validate() error {
	if !hexIdentity(binding.AuthorityID, "rd-", 64) || !hexIdentity(binding.LineageID, "bl-", 64) ||
		!hexIdentity(binding.DeploymentID, "dp-", 64) || !hexIdentity(binding.KeyID, "bk-", 64) ||
		binding.ProjectID != string(recordplatform.ProjectIDDefault) || binding.Generation == 0 ||
		binding.Generation > math.MaxInt64 || binding.CutoverDigest == ([32]byte{}) {
		return ErrInvalid
	}
	return nil
}

// InitialCreation binds the actual first revision to its allocated identity.
// The APP deferred constraint recomputes this digest from committed rows.
type InitialCreation struct {
	ReservationID      string
	RecordID           string
	RevisionID         string
	AuthorID           string
	CanonicalHash      [32]byte
	RequestFingerprint [32]byte
}

func (creation InitialCreation) Validate() error {
	domain := recordidentity.Classify(creation.RecordID)
	if !hexIdentity(creation.ReservationID, "rba_", 32) ||
		(domain != recordidentity.Ordinary && domain != recordidentity.Certifiable) ||
		len(creation.RevisionID) < 5 || len(creation.RevisionID) > 68 || !strings.HasPrefix(creation.RevisionID, "rrv_") || !opaqueID(creation.AuthorID) ||
		creation.CanonicalHash == ([32]byte{}) || creation.RequestFingerprint == ([32]byte{}) {
		return ErrInvalid
	}
	for index := 4; index < len(creation.RevisionID); index++ {
		ch := creation.RevisionID[index]
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') {
			return ErrInvalid
		}
	}
	return nil
}

func (creation InitialCreation) Commitment() ([32]byte, error) {
	if err := creation.Validate(); err != nil {
		return [32]byte{}, err
	}
	// The fields have bounded lengths; one allocation supplies the entire frame.
	texts := [...]string{"houfeng.record-birth.initial.v1", creation.ReservationID, creation.RecordID, creation.RevisionID, creation.AuthorID}
	size := 64
	for _, text := range texts {
		size += 4 + len(text)
	}
	encoded := make([]byte, 0, size)
	for _, text := range texts {
		encoded = appendText(encoded, text)
	}
	encoded = append(encoded, creation.CanonicalHash[:]...)
	encoded = append(encoded, creation.RequestFingerprint[:]...)
	return sha256.Sum256(encoded), nil
}

func opaqueID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for index := range value {
		ch := value[index]
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
			return false
		}
	}
	return true
}

func hexIdentity(value, prefix string, digits int) bool {
	if len(value) != len(prefix)+digits || !strings.HasPrefix(value, prefix) {
		return false
	}
	for index := len(prefix); index < len(value); index++ {
		ch := value[index]
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func appendText(dst []byte, value string) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(value)))
	return append(dst, value...)
}
