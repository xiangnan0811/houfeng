package portability

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sync"

	"houfeng/internal/center/attachments"
)

// These fixtures model bytes and application leases only. They do not provide
// PostgreSQL write authority or evidence for the local filesystem protocol.
const portabilityTestNamespace = "8ca382ce-0946-4c16-8722-f6dc33f41df7"

type portabilityMemoryObject struct {
	version attachments.ObjectVersion
	payload []byte
}

type portabilityMemoryBlob struct {
	mu       sync.Mutex
	objects  map[string]portabilityMemoryObject
	sequence uint64
}

func newPortabilityMemoryBlob() *portabilityMemoryBlob {
	return &portabilityMemoryBlob{objects: make(map[string]portabilityMemoryObject)}
}

func (blob *portabilityMemoryBlob) Put(ctx context.Context, request attachments.PutRequest, reader io.Reader) (attachments.ObjectVersion, error) {
	if err := ctx.Err(); err != nil {
		return attachments.ObjectVersion{}, err
	}
	if err := request.Validate(); err != nil {
		return attachments.ObjectVersion{}, err
	}
	payload, err := io.ReadAll(reader)
	if err != nil {
		return attachments.ObjectVersion{}, err
	}
	digest := sha256.Sum256(payload)
	if digest != request.ExpectedSHA256 || int64(len(payload)) != request.ExpectedSizeBytes {
		return attachments.ObjectVersion{}, attachments.ErrInvalidBlobRequest
	}
	key := "sha256/" + hex.EncodeToString(digest[:])
	blob.mu.Lock()
	defer blob.mu.Unlock()
	if existing, ok := blob.objects[key]; ok {
		return existing.version, nil
	}
	blob.sequence++
	version := attachments.ObjectVersion{Key: key, VersionID: fmt.Sprintf("memory-%d", blob.sequence), SHA256: digest, SizeBytes: int64(len(payload))}
	blob.objects[key] = portabilityMemoryObject{version: version, payload: payload}
	return version, nil
}

func (blob *portabilityMemoryBlob) Open(ctx context.Context, version attachments.ObjectVersion, byteRange attachments.ByteRange) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	blob.mu.Lock()
	defer blob.mu.Unlock()
	object, ok := blob.objects[version.Key]
	if !ok || object.version != version {
		return nil, attachments.ErrBlobNotFound
	}
	payload := object.payload
	if byteRange != attachments.FullByteRange() {
		if byteRange.Start < 0 || byteRange.EndInclusive < byteRange.Start || byteRange.EndInclusive >= int64(len(payload)) {
			return nil, attachments.ErrInvalidBlobRequest
		}
		payload = payload[byteRange.Start : byteRange.EndInclusive+1]
	}
	return io.NopCloser(bytes.NewReader(payload)), nil
}

func (blob *portabilityMemoryBlob) Stat(ctx context.Context, version attachments.ObjectVersion) (attachments.ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return attachments.ObjectInfo{}, err
	}
	blob.mu.Lock()
	defer blob.mu.Unlock()
	object, ok := blob.objects[version.Key]
	if !ok || object.version != version {
		return attachments.ObjectInfo{}, attachments.ErrBlobNotFound
	}
	return attachments.ObjectInfo{Version: version}, nil
}

func (blob *portabilityMemoryBlob) Delete(ctx context.Context, version attachments.ObjectVersion) (attachments.DeletionReceipt, error) {
	if err := ctx.Err(); err != nil {
		return attachments.DeletionReceipt{}, err
	}
	blob.mu.Lock()
	defer blob.mu.Unlock()
	object, ok := blob.objects[version.Key]
	if ok && object.version != version {
		return attachments.DeletionReceipt{}, attachments.ErrBlobNotFound
	}
	delete(blob.objects, version.Key)
	return attachments.DeletionReceipt{Version: version, Deleted: ok}, nil
}

// MinIO lease tests also use this writer with their real S3 store and memory
// job repository. Their evidence covers S3 bytes, not durable database claims.
type portabilityMemoryWriter struct {
	blob      attachments.BlobStore
	namespace string
}

func (writer *portabilityMemoryWriter) NamespaceID() string { return writer.namespace }
func (writer *portabilityMemoryWriter) Put(ctx context.Context, request attachments.WriteRequest, reader io.Reader) (attachments.WrittenBlob, error) {
	if request.ProjectID != "default" || request.Origin.ID == "" || request.Origin.Generation <= 0 {
		return attachments.WrittenBlob{}, attachments.ErrInvalidBlobRequest
	}
	var identity [16]byte
	if _, err := rand.Read(identity[:]); err != nil {
		return attachments.WrittenBlob{}, err
	}
	version, err := writer.blob.Put(ctx, request.Content, reader)
	if err != nil {
		return attachments.WrittenBlob{}, err
	}
	return attachments.WrittenBlob{Version: version, NamespaceID: writer.namespace, ObligationID: "rwo_" + hex.EncodeToString(identity[:])}, nil
}

func portabilityStageRequest(kind, id string) attachments.WriteRequest {
	return attachments.WriteRequest{ProjectID: "default", Origin: attachments.WriteOrigin{Kind: kind, ID: id, Generation: 1}}
}
