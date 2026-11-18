package portability

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"sync"

	"houfeng/internal/center/attachments"
)

type LeasedBlobStore struct {
	inner  attachments.BlobStore
	writer attachments.BlobWriter
	mu     sync.Mutex
	leases map[string]*contentLease
}

type contentLease struct {
	revoked bool
	version attachments.ObjectVersion
}

func NewLeasedBlobStore(inner attachments.BlobStore, writer attachments.BlobWriter) *LeasedBlobStore {
	return &LeasedBlobStore{inner: inner, writer: writer, leases: make(map[string]*contentLease)}
}

func (store *LeasedBlobStore) Stage(ctx context.Context, request attachments.WriteRequest, payload []byte) (attachments.WrittenBlob, error) {
	if request.Origin.Kind != "export_artifact" {
		return attachments.WrittenBlob{}, ErrExportUnavailable
	}
	return store.stage(ctx, request, payload)
}

func (store *LeasedBlobStore) StageImport(ctx context.Context, request attachments.WriteRequest, payload []byte) (attachments.WrittenBlob, error) {
	if request.Origin.Kind != "import_archive" {
		return attachments.WrittenBlob{}, ErrExportUnavailable
	}
	return store.stage(ctx, request, payload)
}

func (store *LeasedBlobStore) stage(ctx context.Context, request attachments.WriteRequest, payload []byte) (attachments.WrittenBlob, error) {
	if ctx == nil || store == nil || store.inner == nil || store.writer == nil || request.Origin.ID == "" || request.ProjectID == "" || len(payload) == 0 {
		return attachments.WrittenBlob{}, ErrExportUnavailable
	}
	temporaryKey, err := attachments.NewBlobTemporaryKey()
	if err != nil {
		return attachments.WrittenBlob{}, err
	}
	digest := sha256.Sum256(payload)
	request.Content = attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(payload)), TemporaryKey: temporaryKey}
	written, err := store.writer.Put(ctx, request, bytes.NewReader(payload))
	if err != nil {
		return attachments.WrittenBlob{}, err
	}
	if written.Version.Validate() != nil || written.Version.SHA256 != digest || written.Version.SizeBytes != int64(len(payload)) || written.NamespaceID != store.writer.NamespaceID() || written.ObligationID == "" {
		return attachments.WrittenBlob{}, ErrExportUnavailable
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if existing, ok := store.leases[request.Origin.ID]; ok && existing.revoked {
		return attachments.WrittenBlob{}, ErrExportLeaseRevoked
	}
	store.leases[request.Origin.ID] = &contentLease{version: written.Version}
	return written, nil
}

func (store *LeasedBlobStore) OpenLeased(ctx context.Context, jobID string) (io.ReadCloser, attachments.ObjectVersion, error) {
	if ctx == nil || store == nil || store.inner == nil || jobID == "" {
		return nil, attachments.ObjectVersion{}, ErrExportUnavailable
	}
	store.mu.Lock()
	lease := store.leases[jobID]
	if lease == nil {
		store.mu.Unlock()
		return nil, attachments.ObjectVersion{}, ErrExportNotFound
	}
	if lease.revoked {
		store.mu.Unlock()
		return nil, attachments.ObjectVersion{}, ErrExportLeaseRevoked
	}
	version := lease.version
	store.mu.Unlock()

	reader, err := store.inner.Open(ctx, version, attachments.FullByteRange())
	if err != nil {
		return nil, attachments.ObjectVersion{}, err
	}
	return &leasedReader{store: store, jobID: jobID, inner: reader}, version, nil
}

func (store *LeasedBlobStore) OpenPublished(
	ctx context.Context,
	jobID string,
	version attachments.ObjectVersion,
	namespaceID string,
) (io.ReadCloser, attachments.ObjectVersion, error) {
	if ctx == nil || store == nil || store.inner == nil || store.writer == nil || jobID == "" || version.Validate() != nil || namespaceID != store.writer.NamespaceID() {
		return nil, attachments.ObjectVersion{}, ErrExportUnavailable
	}
	store.mu.Lock()
	lease := store.leases[jobID]
	if lease != nil && lease.revoked {
		store.mu.Unlock()
		return nil, attachments.ObjectVersion{}, ErrExportLeaseRevoked
	}
	if lease != nil && lease.version != version {
		store.mu.Unlock()
		return nil, attachments.ObjectVersion{}, ErrExportUnavailable
	}
	if lease == nil {
		store.leases[jobID] = &contentLease{version: version}
	}
	store.mu.Unlock()

	reader, err := store.inner.Open(ctx, version, attachments.FullByteRange())
	if err != nil {
		return nil, attachments.ObjectVersion{}, err
	}
	return &leasedReader{store: store, jobID: jobID, inner: reader}, version, nil
}

func (store *LeasedBlobStore) Revoke(jobID string) {
	if store == nil || jobID == "" {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if lease, ok := store.leases[jobID]; ok {
		lease.revoked = true
		return
	}
	store.leases[jobID] = &contentLease{revoked: true}
}

func (store *LeasedBlobStore) dropLease(jobID string) {
	if store == nil || jobID == "" {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.leases, jobID)
}

func (store *LeasedBlobStore) leaseRevoked(jobID string) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	lease := store.leases[jobID]
	return lease != nil && lease.revoked
}

type leasedReader struct {
	store *LeasedBlobStore
	jobID string
	inner io.ReadCloser
}

func (reader *leasedReader) Read(buffer []byte) (int, error) {
	if reader == nil || reader.store == nil || reader.store.leaseRevoked(reader.jobID) {
		return 0, ErrExportLeaseRevoked
	}
	return reader.inner.Read(buffer)
}

func (reader *leasedReader) Close() error {
	if reader == nil || reader.inner == nil {
		return nil
	}
	return reader.inner.Close()
}
