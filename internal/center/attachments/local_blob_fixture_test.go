//go:build linux

package attachments

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// This fixture supplies protocol decisions for filesystem tests. PostgreSQL
// authority, origin authentication and restart durability are tested separately
// using the direct-role integration fixture; this is not database evidence.
type localBlobTestRepository struct {
	mu                sync.Mutex
	namespace         LocalNamespace
	anchors           map[string]LocalAnchor
	writes            map[string]WriteObligation
	lastID            string
	dispatchError     bool
	dispatchCommitted bool
	returnError       bool
}

func (repository *localBlobTestRepository) CheckLocalProvisionAuthority(context.Context) error {
	return nil
}
func (repository *localBlobTestRepository) LoadLocalNamespace(context.Context, string) (LocalNamespace, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.namespace.NamespaceID == "" {
		return LocalNamespace{}, ErrBlobNotFound
	}
	return repository.namespace, nil
}
func (repository *localBlobTestRepository) ProvisionLocalNamespace(_ context.Context, n LocalNamespace) (LocalNamespace, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.namespace.NamespaceID != "" && !sameLocalNamespaceIdentity(n, repository.namespace) {
		return LocalNamespace{}, ErrLocalIdentityChanged
	}
	if repository.namespace.NamespaceID == "" {
		n.AdmissionState = "closed"
		repository.namespace = n
	}
	return repository.namespace, nil
}
func (repository *localBlobTestRepository) AdmitLocalNamespace(_ context.Context, a LocalNamespaceAdmission) (LocalNamespace, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if a.NamespaceID != repository.namespace.NamespaceID || a.IncarnationID != repository.namespace.IncarnationID || a.RequestID != repository.namespace.RequestID || a.MarkerDigest != repository.namespace.MarkerDigest {
		return LocalNamespace{}, ErrLocalIdentityChanged
	}
	repository.namespace.AdmissionState = "active"
	return repository.namespace, nil
}
func (repository *localBlobTestRepository) LoadLocalAnchor(_ context.Context, namespace, incarnation, key string) (LocalAnchor, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	a, found := repository.anchors[key]
	if !found {
		return a, ErrBlobNotFound
	}
	if a.NamespaceID != namespace || a.IncarnationID != incarnation {
		return a, ErrLocalIdentityChanged
	}
	return a, nil
}
func (repository *localBlobTestRepository) RegisterLocalAnchor(_ context.Context, a LocalAnchor) (LocalAnchor, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if old, found := repository.anchors[a.Key]; found && old != a {
		return LocalAnchor{}, ErrLocalIdentityChanged
	}
	repository.anchors[a.Key] = a
	return a, nil
}
func (repository *localBlobTestRepository) RegisterWrite(_ context.Context, r WriteRegistration) (WriteObligation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, found := repository.writes[r.ObligationID]; found {
		return existing, nil
	}
	temporary := "sha256/.blob-tmp-" + r.ObligationID
	o := WriteObligation{ID: r.ObligationID, WriterInstance: r.WriterInstance, RequestSeq: r.RequestSeq, ProjectID: r.ProjectID,
		NamespaceID: r.NamespaceID, IncarnationID: r.IncarnationID, BackendKind: r.BackendKind, Key: r.Key, SHA256: r.SHA256, Size: r.Size, TemporaryKey: &temporary, Outcome: "registered"}
	repository.writes[o.ID], repository.lastID = o, o.ID
	return o, nil
}
func (repository *localBlobTestRepository) ResolveWrite(_ context.Context, r WriteIdentity) (WriteObligation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	o, ok := repository.writes[r.ObligationID]
	if !ok || o.WriterInstance != r.WriterInstance {
		return WriteObligation{}, ErrBlobNotFound
	}
	return o, nil
}
func (repository *localBlobTestRepository) DispatchWrite(_ context.Context, r WriteDispatch) (WriteObligation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	o := repository.writes[r.ObligationID]
	if o.DispatchID != nil {
		return WriteObligation{}, ErrBlobConflict
	}
	if !repository.dispatchError || repository.dispatchCommitted {
		now := time.Now().UTC()
		o.DispatchID, o.DispatchedAt = &r.DispatchID, &now
		repository.writes[o.ID] = o
	}
	if repository.dispatchError {
		return WriteObligation{}, errors.New("dispatch response lost")
	}
	return o, nil
}
func (repository *localBlobTestRepository) RecordWriteTemporary(_ context.Context, r WriteTemporaryCreation) (WriteObligation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	o := repository.writes[r.ObligationID]
	o.TemporaryDevice, o.TemporaryInode = &r.Device, &r.Inode
	repository.writes[o.ID] = o
	return o, nil
}
func (repository *localBlobTestRepository) RecordWriteReturn(_ context.Context, r WriteReturn) (WriteObligation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.returnError {
		return WriteObligation{}, errors.New("return transaction unavailable")
	}
	o := repository.writes[r.ObligationID]
	now := time.Now().UTC()
	o.ReturnedAt, o.Outcome, o.Version = &now, r.Outcome, r.Version
	repository.writes[o.ID] = o
	return o, nil
}

type localBlobTestStore struct {
	*LocalBlobStore
	writer     *ManagedBlobWriter
	repository *localBlobTestRepository
}

func (store *localBlobTestStore) Put(ctx context.Context, request PutRequest, reader io.Reader) (ObjectVersion, error) {
	written, err := store.writer.Put(ctx, WriteRequest{Content: request, ProjectID: "default", Origin: WriteOrigin{Kind: "import_archive", ID: "rij_filesystem_fixture", Generation: 1}}, reader)
	return written.Version, err
}
func newTestLocalBlobStore(t *testing.T, root string, hooks localBlobHooks) *localBlobTestStore {
	t.Helper()
	if root == "" {
		root = filepath.Join(t.TempDir(), "blobs")
	}
	repository := &localBlobTestRepository{anchors: make(map[string]LocalAnchor), writes: make(map[string]WriteObligation)}
	newID := func() string {
		t.Helper()
		value, err := newLocalUUID()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	_, err := ProvisionLocalNamespace(context.Background(), LocalProvisionRequest{Root: root, NamespaceID: newID(), IncarnationID: newID(), RequestID: newID(), OwnerUID: uint32(os.Geteuid()), OwnerGID: uint32(os.Getegid())}, repository)
	if err != nil {
		t.Fatal(err)
	}
	store, err := newLocalBlobStore(context.Background(), root, repository, hooks)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	writer, err := NewManagedBlobWriter(store, BackendKindLocal, repository)
	if err != nil {
		t.Fatal(err)
	}
	return &localBlobTestStore{LocalBlobStore: store, writer: writer, repository: repository}
}
