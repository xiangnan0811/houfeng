//go:build linux

package attachments

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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
		NamespaceID: r.NamespaceID, IncarnationID: r.IncarnationID, BackendKind: r.BackendKind, StorageBinding: r.StorageBinding, Key: r.Key, SHA256: r.SHA256, Size: r.Size, TemporaryKey: &temporary, Outcome: "registered"}
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

// routeTestBlob is the invoked in-memory provider; its random identity belongs
// to that provider instance and changes only when the test rotates it.
type routeTestBlob struct {
	route            [32]byte
	checks           int
	failCheck        int
	rotateCheck      int
	rotateAfterWrite bool
	writes           int
}

func (b *routeTestBlob) GCStorageIdentity() ([32]byte, error) {
	b.checks++
	if b.checks == b.rotateCheck {
		_, _ = rand.Read(b.route[:])
	}
	if b.checks == b.failCheck {
		return [32]byte{}, ErrInvalidBlobStoreConfig
	}
	return b.route, nil
}
func (b *routeTestBlob) Put(_ context.Context, r PutRequest, in io.Reader) (ObjectVersion, error) {
	data, err := io.ReadAll(in)
	if err != nil {
		return ObjectVersion{}, err
	}
	b.writes++
	result := ObjectVersion{Key: "sha256/" + hexDigest(r.ExpectedSHA256), VersionID: "real-return-version", SHA256: sha256.Sum256(data), SizeBytes: int64(len(data))}
	if b.rotateAfterWrite {
		_, _ = rand.Read(b.route[:])
	}
	return result, nil
}
func (b *routeTestBlob) Open(context.Context, ObjectVersion, ByteRange) (io.ReadCloser, error) {
	return nil, ErrBlobNotFound
}
func (b *routeTestBlob) Stat(context.Context, ObjectVersion) (ObjectInfo, error) {
	return ObjectInfo{}, ErrBlobNotFound
}
func (b *routeTestBlob) Delete(context.Context, ObjectVersion) (DeletionReceipt, error) {
	return DeletionReceipt{}, ErrBlobNotFound
}
func (b *routeTestBlob) ResolveTemporaryVersion(context.Context, string) (TemporaryObjectVersion, error) {
	return TemporaryObjectVersion{}, ErrBlobNotFound
}
func (b *routeTestBlob) OpenTemporaryVersion(context.Context, TemporaryObjectReadRequest) (io.ReadCloser, error) {
	return nil, ErrBlobNotFound
}
func (b *routeTestBlob) DeleteTemporaryVersion(context.Context, TemporaryObjectVersion) error {
	return nil
}
func (b *routeTestBlob) PublishTemporaryVersion(ctx context.Context, r TemporaryObjectPublishRequest) (ObjectVersion, error) {
	return b.Put(ctx, PutRequest{ExpectedSHA256: r.ExpectedSHA256, ExpectedSizeBytes: r.ExpectedSizeBytes}, strings.NewReader("content"))
}

type routeTestNoIdentity struct{ BlobStore }

type routeTestRepository struct {
	*localBlobTestRepository
	loseRegister bool
	loseReturn   bool
}

func (r *routeTestRepository) RegisterWrite(ctx context.Context, request WriteRegistration) (WriteObligation, error) {
	o, err := r.localBlobTestRepository.RegisterWrite(ctx, request)
	if err == nil && r.loseRegister {
		r.loseRegister = false
		return WriteObligation{}, errors.New("registration response lost")
	}
	return o, err
}
func (r *routeTestRepository) RecordWriteReturn(ctx context.Context, request WriteReturn) (WriteObligation, error) {
	o, err := r.localBlobTestRepository.RecordWriteReturn(ctx, request)
	if err == nil && r.loseReturn {
		r.loseReturn = false
		return WriteObligation{}, errors.New("return response lost")
	}
	return o, err
}

func TestManagedBlobWriterSourceBinding(t *testing.T) {
	newFixture := func(t *testing.T) (*routeTestBlob, *routeTestRepository) {
		t.Helper()
		blob := &routeTestBlob{}
		if _, err := rand.Read(blob.route[:]); err != nil {
			t.Fatal(err)
		}
		return blob, &routeTestRepository{localBlobTestRepository: &localBlobTestRepository{writes: make(map[string]WriteObligation)}}
	}
	request := WriteRequest{Content: PutRequest{ExpectedSHA256: sha256.Sum256([]byte("content")), ExpectedSizeBytes: 7}, ProjectID: "default", Origin: WriteOrigin{Kind: "import_archive", ID: "rij_fixture", Generation: 1}}
	t.Run("capability_and_typed_nil", func(t *testing.T) {
		blob, repo := newFixture(t)
		if _, err := NewManagedBlobWriter(routeTestNoIdentity{blob}, BackendKindS3, repo); !errors.Is(err, ErrInvalidBlobStoreConfig) {
			t.Fatalf("missing identity: %v", err)
		}
		var nilBlob *routeTestBlob
		if _, err := NewManagedBlobWriter(nilBlob, BackendKindS3, repo); !errors.Is(err, ErrInvalidBlobStoreConfig) {
			t.Fatalf("typed nil: %v", err)
		}
	})
	for _, tc := range []struct {
		name        string
		fail        int
		rotate      bool
		lose        bool
		wantWrites  int
		wantOutcome string
	}{
		{"before_registration", 2, false, false, 0, ""},
		{"before_dispatch", 3, false, false, 0, "registered"},
		{"before_io", 4, false, false, 0, "registered"},
		{"after_io", 0, true, false, 1, "remote_unknown"},
		{"commit_response_loss", 0, false, true, 1, "materialized"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blob, repo := newFixture(t)
			blob.failCheck, blob.rotateAfterWrite = tc.fail, tc.rotate
			if tc.lose {
				repo.loseRegister, repo.loseReturn = true, true
				repo.dispatchError, repo.dispatchCommitted = true, true
			}
			writer, err := NewManagedBlobWriter(blob, BackendKindS3, repo)
			if err != nil {
				t.Fatal(err)
			}
			written, err := writer.Put(context.Background(), request, strings.NewReader("content"))
			if tc.lose && err != nil {
				t.Fatal(err)
			}
			if !tc.lose && err == nil {
				t.Fatal("expected route failure")
			}
			if blob.writes != tc.wantWrites {
				t.Fatalf("writes=%d", blob.writes)
			}
			if !tc.lose && written.ObligationID != "" {
				t.Fatal("reported success after route failure")
			}
			if tc.wantOutcome != "" {
				o := repo.writes[repo.lastID]
				if o.Outcome != tc.wantOutcome || o.StorageBinding == nil {
					t.Fatalf("obligation=%+v", o)
				}
				if tc.rotate && (o.Version == nil || *o.Version != "real-return-version") {
					t.Fatalf("lost returned version: %+v", o)
				}
			}
		})
	}
	t.Run("route_changed_before_dispatch", func(t *testing.T) {
		blob, repo := newFixture(t)
		blob.rotateCheck = 3
		writer, err := NewManagedBlobWriter(blob, BackendKindS3, repo)
		if err != nil {
			t.Fatal(err)
		}
		written, err := writer.Put(context.Background(), request, strings.NewReader("content"))
		if err == nil || written.ObligationID != "" || blob.writes != 0 || repo.writes[repo.lastID].DispatchedAt != nil {
			t.Fatalf("route drift dispatch = %+v, %v", written, err)
		}
	})
	t.Run("temporary_promotion_changed_route", func(t *testing.T) {
		blob, repo := newFixture(t)
		blob.rotateAfterWrite = true
		writer, err := NewManagedBlobWriter(blob, BackendKindS3, repo)
		if err != nil {
			t.Fatal(err)
		}
		promote := request
		promote.Content.TemporaryKey = "temporary/source"
		written, err := writer.PublishTemporaryVersion(context.Background(), promote, TemporaryObjectVersion{Key: promote.Content.TemporaryKey, VersionID: "source-version"})
		if err == nil || written.ObligationID != "" || blob.writes != 1 {
			t.Fatalf("promotion = %+v, %v", written, err)
		}
		o := repo.writes[repo.lastID]
		if o.Outcome != "remote_unknown" || o.Version == nil || *o.Version != "real-return-version" {
			t.Fatalf("promotion return = %+v", o)
		}
	})
}
