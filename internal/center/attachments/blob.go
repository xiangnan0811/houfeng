package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
)

var (
	ErrInvalidBlobRequest     = errors.New("invalid Blob request")
	ErrInvalidBlobStoreConfig = errors.New("invalid Blob store configuration")
	ErrInvalidBlobRange       = errors.New("invalid Blob byte range")
	ErrBlobNotFound           = errors.New("Blob not found")
	ErrBlobConflict           = errors.New("Blob conflict")
	ErrBlobVersionMismatch    = errors.New("Blob version mismatch")
	ErrBlobSizeMismatch       = errors.New("Blob size mismatch")
	ErrBlobHashMismatch       = errors.New("Blob hash mismatch")
)

type PutRequest struct {
	ExpectedSHA256    [sha256.Size]byte
	ExpectedSizeBytes int64
	TemporaryKey      string
}

func (request PutRequest) Validate() error {
	if request.ExpectedSizeBytes <= 0 || request.ExpectedSizeBytes == math.MaxInt64 {
		return ErrInvalidBlobRequest
	}
	return nil
}

type ObjectVersion struct {
	Key       string
	VersionID string
	SHA256    [sha256.Size]byte
	SizeBytes int64
}

func (version ObjectVersion) Validate() error {
	if version.Key != "sha256/"+hexDigest(version.SHA256) || version.VersionID == "" ||
		len(version.VersionID) > 1024 || version.SizeBytes <= 0 {
		return ErrInvalidBlobRequest
	}
	return nil
}

type ObjectInfo struct {
	Version ObjectVersion
}

type DeletionReceipt struct {
	Version ObjectVersion
	Deleted bool
}

type TemporaryObjectVersion struct {
	Key       string
	VersionID string
}

func (version TemporaryObjectVersion) Validate() error {
	if version.Key == "" || len(version.Key) > 1024 || version.VersionID == "" || len(version.VersionID) > 1024 {
		return ErrInvalidBlobRequest
	}
	return nil
}

type TemporaryObjectStore interface {
	ResolveTemporaryVersion(context.Context, string) (TemporaryObjectVersion, error)
	DeleteTemporaryVersion(context.Context, TemporaryObjectVersion) error
}

type ByteRange struct {
	kind         byteRangeKind
	Start        int64
	EndInclusive int64
}

type byteRangeKind uint8

const (
	byteRangeKindFull byteRangeKind = iota + 1
	byteRangeKindClosed
)

func FullByteRange() ByteRange {
	return ByteRange{kind: byteRangeKindFull}
}

func ClosedByteRange(start, endInclusive int64) ByteRange {
	return ByteRange{kind: byteRangeKindClosed, Start: start, EndInclusive: endInclusive}
}

func (byteRange ByteRange) validate(sizeBytes int64) error {
	if sizeBytes <= 0 {
		return ErrInvalidBlobRange
	}
	switch byteRange.kind {
	case byteRangeKindFull:
		if byteRange.Start != 0 || byteRange.EndInclusive != 0 {
			return ErrInvalidBlobRange
		}
		return nil
	case byteRangeKindClosed:
	default:
		return ErrInvalidBlobRange
	}
	if byteRange.Start < 0 || byteRange.EndInclusive < byteRange.Start || byteRange.EndInclusive >= sizeBytes {
		return ErrInvalidBlobRange
	}
	return nil
}

type BlobStore interface {
	Put(context.Context, PutRequest, io.Reader) (ObjectVersion, error)
	Open(context.Context, ObjectVersion, ByteRange) (io.ReadCloser, error)
	Stat(context.Context, ObjectVersion) (ObjectInfo, error)
	Delete(context.Context, ObjectVersion) (DeletionReceipt, error)
}

func hexDigest(digest [sha256.Size]byte) string {
	return hex.EncodeToString(digest[:])
}
