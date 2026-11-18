//go:build linux

package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

const localMaterializationAttribute = "user.houfeng.blob-materialization"

type LocalBlobStore struct {
	root       string
	namespace  LocalNamespace
	repository LocalNamespaceRepository
	files      localBlobDescriptors
	lifecycle  sync.RWMutex
	closed     bool
	hooks      localBlobHooks
}

var _ BlobStore = (*LocalBlobStore)(nil)
var _ BlobPublicationResolver = (*LocalBlobStore)(nil)

// NewLocalBlobStore only opens an existing admitted namespace. It creates no
// directories, repairs no identities and never acquires administrative access.
func NewLocalBlobStore(ctx context.Context, root string, repository LocalNamespaceRepository) (*LocalBlobStore, error) {
	return newLocalBlobStore(ctx, root, repository, localBlobHooks{})
}

func newLocalBlobStore(ctx context.Context, root string, repository LocalNamespaceRepository, hooks localBlobHooks) (*LocalBlobStore, error) {
	if ctx == nil || nilUploadServiceDependency(repository) {
		return nil, ErrInvalidBlobStoreConfig
	}
	n, err := repository.LoadLocalNamespace(ctx, root)
	if err != nil {
		return nil, errors.Join(ErrLocalNamespaceClosed, err)
	}
	if n.RootPath != root || n.AdmissionState != "active" {
		return nil, ErrLocalNamespaceClosed
	}
	store := &LocalBlobStore{root: root, namespace: n, repository: repository, files: emptyLocalDescriptors(), hooks: hooks}
	fail := func(err error) (*LocalBlobStore, error) { return nil, errors.Join(err, store.files.close()) }
	store.files.root, err = openLocalRoot(root, false, n.OwnerUID, n.OwnerGID)
	if err != nil {
		return fail(err)
	}
	store.files.provision, err = openLocalProvisionLock(ctx, store.files.root, false, false, n.OwnerUID, n.OwnerGID)
	if err != nil {
		return fail(err)
	}
	store.files.digest, err = openLocalAt(store.files.root, "sha256", unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return fail(err)
	}
	store.files.locks, err = openLocalAt(store.files.root, ".locks", unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return fail(err)
	}
	if err := store.validateIdentity(ctx); err != nil {
		return fail(err)
	}
	return store, nil
}

func (store *LocalBlobStore) Close() error {
	if store == nil {
		return nil
	}
	store.lifecycle.Lock()
	defer store.lifecycle.Unlock()
	if store.closed {
		return nil
	}
	store.closed = true
	return store.files.close()
}

func (store *LocalBlobStore) Namespace() LocalNamespace { return store.namespace }

func (store *LocalBlobStore) validateIdentity(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n := store.namespace
	current, err := store.repository.LoadLocalNamespace(ctx, store.root)
	if err != nil || current.AdmissionState != "active" || !sameLocalNamespaceIdentity(n, current) {
		return errors.Join(ErrLocalNamespaceClosed, err)
	}
	physical, err := inspectLocalNamespace(store.root, store.files, n.OwnerUID, n.OwnerGID)
	if err != nil || !sameLocalNamespaceIdentity(n, physical) {
		return errors.Join(ErrLocalIdentityChanged, err)
	}
	root, err := openLocalRoot(store.root, false, n.OwnerUID, n.OwnerGID)
	if err != nil {
		return errors.Join(ErrLocalIdentityChanged, err)
	}
	defer unix.Close(root)
	rootStat, err := localStat(root, unix.S_IFDIR, n.OwnerUID, n.OwnerGID, 0o700)
	if err != nil || rootStat.Ino != n.RootInode || rootStat.Dev != n.Device {
		return ErrLocalIdentityChanged
	}
	for _, entry := range []struct {
		name  string
		inode uint64
	}{{"sha256", n.DigestInode}, {".locks", n.LocksInode}} {
		fd, err := openLocalAt(root, entry.name, unix.O_RDONLY|unix.O_DIRECTORY, 0)
		if err != nil {
			return errors.Join(ErrLocalIdentityChanged, err)
		}
		stat, statErr := localStat(fd, unix.S_IFDIR, n.OwnerUID, n.OwnerGID, 0o700)
		closeErr := unix.Close(fd)
		if statErr != nil || closeErr != nil || stat.Ino != entry.inode || stat.Dev != n.Device {
			return errors.Join(ErrLocalIdentityChanged, statErr, closeErr)
		}
	}
	var held, named unix.Stat_t
	if err := unix.Fstat(store.files.provision, &held); err != nil {
		return err
	}
	if err := unix.Fstatat(root, localProvisionLockName, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil || held.Ino != named.Ino || held.Dev != named.Dev {
		return ErrLocalIdentityChanged
	}
	return nil
}

// The capability contains no unlock handle. Its methods are package-private;
// run serializes an in-flight synchronous action with callback revocation.
type LocalKeyGuard struct {
	store  *LocalBlobStore
	anchor LocalAnchor
	mu     sync.Mutex
	active bool
}

type localGuardContextKey struct{}

func (guard *LocalKeyGuard) run(key string, action func() error) error {
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if !guard.active || guard.anchor.Key != key {
		return ErrLocalGuardExpired
	}
	return action()
}

func (store *LocalBlobStore) WithKeyGuard(ctx context.Context, key string, callback func(context.Context, *LocalKeyGuard) error) error {
	if ctx == nil || store == nil || callback == nil || !validLocalKey(key) {
		return ErrInvalidBlobRequest
	}
	if ctx.Value(localGuardContextKey{}) != nil {
		return fmt.Errorf("%w: nested key guard", ErrInvalidBlobRequest)
	}
	store.lifecycle.RLock()
	defer store.lifecycle.RUnlock()
	if store.closed {
		return ErrLocalNamespaceClosed
	}
	if err := store.validateIdentity(ctx); err != nil {
		return err
	}
	n := store.namespace
	registered, err := store.repository.LoadLocalAnchor(ctx, n.NamespaceID, n.IncarnationID, key)
	if err != nil && !errors.Is(err, ErrBlobNotFound) {
		return err
	}
	known := err == nil
	name := strings.TrimPrefix(key, "sha256/") + ".lock"
	fd, err := openLocalAt(store.files.locks, name, unix.O_RDWR, 0)
	if errors.Is(err, unix.ENOENT) && !known {
		fd, err = openLocalAt(store.files.locks, name, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL, 0o600)
		if errors.Is(err, unix.EEXIST) {
			fd, err = openLocalAt(store.files.locks, name, unix.O_RDWR, 0)
		}
	}
	if err != nil {
		return errors.Join(ErrLocalIdentityChanged, err)
	}
	defer unix.Close(fd)
	if err := localFlock(ctx, fd, unix.LOCK_EX, store.hooks.cutpoint); err != nil {
		return err
	}
	if err := store.validateIdentity(ctx); err != nil {
		return err
	}
	stat, err := localStat(fd, unix.S_IFREG, n.OwnerUID, n.OwnerGID, 0o600)
	if err != nil || stat.Dev != n.Device || stat.Nlink != 1 {
		return errors.Join(ErrLocalIdentityChanged, err)
	}
	var named unix.Stat_t
	if err := unix.Fstatat(store.files.locks, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil || named.Ino != stat.Ino || named.Dev != stat.Dev {
		return ErrLocalIdentityChanged
	}
	// Another initializer may have committed while we waited on flock.
	registered, err = store.repository.LoadLocalAnchor(ctx, n.NamespaceID, n.IncarnationID, key)
	if err != nil && !errors.Is(err, ErrBlobNotFound) {
		return err
	}
	known = err == nil
	if stat.Size == 0 && !known {
		nonce, err := newLocalUUID()
		if err != nil {
			return err
		}
		if err := localWriteFile(fd, []byte(nonce)); err != nil {
			return err
		}
		if err := unix.Fsync(store.files.locks); err != nil {
			return err
		}
	}
	data, err := localReadFile(fd, 36)
	if err != nil || !validLocalUUID(string(data)) {
		return ErrLocalIdentityChanged
	}
	anchor := LocalAnchor{NamespaceID: n.NamespaceID, IncarnationID: n.IncarnationID, Key: key, Device: stat.Dev, Inode: stat.Ino, Nonce: string(data)}
	if known && registered != anchor {
		return ErrLocalIdentityChanged
	}
	if !known {
		registered, err = store.repository.RegisterLocalAnchor(ctx, anchor)
		if err != nil {
			registered, err = store.repository.LoadLocalAnchor(ctx, n.NamespaceID, n.IncarnationID, key)
			if err != nil {
				return errors.Join(ErrLocalNamespaceClosed, err)
			}
		}
		if registered != anchor {
			return ErrLocalIdentityChanged
		}
	}
	guard := &LocalKeyGuard{store: store, anchor: anchor, active: true}
	defer func() { guard.mu.Lock(); guard.active = false; guard.mu.Unlock() }()
	return callback(context.WithValue(ctx, localGuardContextKey{}, guard), guard)
}

func validLocalKey(key string) bool {
	if len(key) != 71 || !strings.HasPrefix(key, "sha256/") {
		return false
	}
	for _, character := range key[7:] {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func (store *LocalBlobStore) Put(context.Context, PutRequest, io.Reader) (ObjectVersion, error) {
	return ObjectVersion{}, ErrManagedBlobRequired
}
func (store *LocalBlobStore) Delete(_ context.Context, version ObjectVersion) (DeletionReceipt, error) {
	return DeletionReceipt{Version: version}, ErrManagedBlobRequired
}
func (store *LocalBlobStore) DeleteGCVersion(_ context.Context, version ObjectVersion) (DeletionReceipt, error) {
	return DeletionReceipt{Version: version}, ErrManagedBlobRequired
}

func validateLocalBlobVersion(version ObjectVersion) error {
	if version.Validate() != nil {
		return ErrInvalidBlobRequest
	}
	if !strings.HasPrefix(version.VersionID, "local-v2-rwo_") || len(version.VersionID) != len("local-v2-rwo_")+32 {
		return ErrBlobVersionMismatch
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(version.VersionID, "local-v2-rwo_")); err != nil {
		return ErrBlobVersionMismatch
	}
	return nil
}

func (store *LocalBlobStore) Open(ctx context.Context, version ObjectVersion, byteRange ByteRange) (io.ReadCloser, error) {
	if ctx == nil || store == nil || validateLocalBlobVersion(version) != nil || byteRange.validate(version.SizeBytes) != nil {
		return nil, ErrInvalidBlobRequest
	}
	store.lifecycle.RLock()
	defer store.lifecycle.RUnlock()
	if store.closed {
		return nil, ErrLocalNamespaceClosed
	}
	if err := store.validateIdentity(ctx); err != nil {
		return nil, err
	}
	file, _, err := store.openVerified(ctx, version, true)
	if err != nil {
		return nil, err
	}
	if byteRange.kind == byteRangeKindFull {
		return file, nil
	}
	return &localBlobSectionReadCloser{Reader: io.NewSectionReader(file, byteRange.Start, byteRange.EndInclusive-byteRange.Start+1), closer: file}, nil
}

func (store *LocalBlobStore) Stat(ctx context.Context, version ObjectVersion) (ObjectInfo, error) {
	reader, err := store.Open(ctx, version, FullByteRange())
	if err != nil {
		return ObjectInfo{}, err
	}
	if err := reader.Close(); err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{Version: version}, nil
}

func (store *LocalBlobStore) ResolveBlobPublicationObject(ctx context.Context, target BlobPublicationTarget) (ObjectVersion, error) {
	if ctx == nil || store == nil || target.Validate() != nil || target.BackendKind != BackendKindLocal {
		return ObjectVersion{}, ErrInvalidBlobPublicationRequest
	}
	store.lifecycle.RLock()
	defer store.lifecycle.RUnlock()
	if store.closed {
		return ObjectVersion{}, ErrLocalNamespaceClosed
	}
	if err := store.validateIdentity(ctx); err != nil {
		return ObjectVersion{}, err
	}
	file, version, err := store.openVerified(ctx, ObjectVersion{Key: target.Key, SHA256: target.SHA256, SizeBytes: target.SizeBytes}, false)
	if err != nil {
		return ObjectVersion{}, err
	}
	return version, file.Close()
}

func (store *LocalBlobStore) openVerified(ctx context.Context, version ObjectVersion, exact bool) (*os.File, ObjectVersion, error) {
	if !validLocalKey(version.Key) || version.Key != "sha256/"+hexDigest(version.SHA256) || version.SizeBytes <= 0 {
		return nil, version, ErrInvalidBlobRequest
	}
	fd, err := openLocalAt(store.files.digest, version.Key[7:], unix.O_RDONLY, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, version, ErrBlobNotFound
	}
	if err != nil {
		return nil, version, err
	}
	file := os.NewFile(uintptr(fd), "local-blob")
	fail := func(err error) (*os.File, ObjectVersion, error) { file.Close(); return nil, version, err }
	stat, err := localStat(fd, unix.S_IFREG, store.namespace.OwnerUID, store.namespace.OwnerGID, 0o600)
	if err != nil || stat.Dev != store.namespace.Device {
		return fail(ErrLocalIdentityChanged)
	}
	if stat.Size != version.SizeBytes {
		return fail(ErrBlobSizeMismatch)
	}
	var materialization [128]byte
	n, err := unix.Fgetxattr(fd, localMaterializationAttribute, materialization[:])
	if err != nil {
		return fail(errors.Join(ErrBlobVersionMismatch, err))
	}
	actual := string(materialization[:n])
	if exact && actual != version.VersionID {
		return fail(ErrBlobVersionMismatch)
	}
	version.VersionID = actual
	if err := validateLocalBlobVersion(version); err != nil {
		return fail(err)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, &blobContextReader{ctx: ctx, reader: file}); err != nil {
		return fail(err)
	}
	var digest [32]byte
	copy(digest[:], hasher.Sum(nil))
	if digest != version.SHA256 {
		return fail(ErrBlobHashMismatch)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	return file, version, nil
}

func (store *LocalBlobStore) runCutpoint(cutpoint localBlobCutpoint) error {
	if store.hooks.cutpoint == nil {
		return nil
	}
	return store.hooks.cutpoint(cutpoint)
}

// putGuarded never removes a target or temporary name on error. The persisted
// obligation owns both names, including a partially written or linked inode.
func (store *LocalBlobStore) putGuarded(ctx context.Context, guard *LocalKeyGuard, obligation WriteObligation, request PutRequest, reader io.Reader, created func(uint64, uint64) error) (ObjectVersion, error) {
	var result ObjectVersion
	err := guard.run(obligation.Key, func() error {
		if guard.store != store || obligation.DispatchID == nil || obligation.DispatchedAt == nil || obligation.TemporaryKey == nil || *obligation.TemporaryKey != "sha256/.blob-tmp-"+obligation.ID {
			return ErrManagedBlobRequired
		}
		if err := store.validateIdentity(ctx); err != nil {
			return err
		}
		name := strings.TrimPrefix(*obligation.TemporaryKey, "sha256/")
		fd, err := openLocalAt(store.files.digest, ".", unix.O_RDWR|unix.O_TMPFILE, 0o600)
		if err != nil {
			return err
		}
		file := os.NewFile(uintptr(fd), "local-blob-temporary")
		defer file.Close()
		if err := store.runCutpoint(localBlobAfterTempOpen); err != nil {
			return err
		}
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil {
			return err
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o7777 != 0o600 || stat.Uid != store.namespace.OwnerUID || stat.Gid != store.namespace.OwnerGID || stat.Dev != store.namespace.Device || stat.Nlink != 0 {
			return ErrLocalIdentityChanged
		}
		version := ObjectVersion{Key: obligation.Key, VersionID: "local-v2-" + obligation.ID, SHA256: request.ExpectedSHA256, SizeBytes: request.ExpectedSizeBytes}
		if err := unix.Fsetxattr(fd, localMaterializationAttribute, []byte(version.VersionID), unix.XATTR_CREATE); err != nil {
			return err
		}
		if err := unix.Fsync(fd); err != nil {
			return err
		}
		if err := store.runCutpoint(localBlobAfterTempIdentity); err != nil {
			return err
		}
		if err := created(stat.Dev, stat.Ino); err != nil {
			return err
		}
		if err := store.runCutpoint(localBlobAfterTempRegistration); err != nil {
			return err
		}
		// Linking our own pinned descriptor through procfs works without
		// CAP_DAC_READ_SEARCH. A named inode is always initialized and registered.
		proc, err := unix.Open("/proc/self/fd", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		defer unix.Close(proc)
		var procfs unix.Statfs_t
		if err := unix.Fstatfs(proc, &procfs); err != nil {
			return err
		}
		if procfs.Type != unix.PROC_SUPER_MAGIC {
			return ErrLocalIdentityChanged
		}
		if err := unix.Linkat(proc, strconv.Itoa(fd), store.files.digest, name, unix.AT_SYMLINK_FOLLOW); err != nil {
			return err
		}
		if err := store.runCutpoint(localBlobAfterTempLink); err != nil {
			return err
		}
		if err := unix.Fsync(store.files.digest); err != nil {
			return err
		}
		if err := store.runCutpoint(localBlobAfterTempCreate); err != nil {
			return err
		}
		hasher := sha256.New()
		written, err := io.Copy(io.MultiWriter(file, hasher), io.LimitReader(&blobContextReader{ctx: ctx, reader: reader}, request.ExpectedSizeBytes+1))
		if err != nil {
			return err
		}
		if written != request.ExpectedSizeBytes {
			return ErrBlobSizeMismatch
		}
		var digest [32]byte
		copy(digest[:], hasher.Sum(nil))
		if digest != request.ExpectedSHA256 {
			return ErrBlobHashMismatch
		}
		if err := store.runCutpoint(localBlobAfterCopy); err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			return err
		}
		if err := store.runCutpoint(localBlobAfterFileSync); err != nil {
			return err
		}
		if err := store.validateIdentity(ctx); err != nil {
			return err
		}
		if err := store.runCutpoint(localBlobBeforePublish); err != nil {
			return err
		}
		if err := unix.Linkat(store.files.digest, name, store.files.digest, obligation.Key[7:], 0); errors.Is(err, unix.EEXIST) {
			existing, actual, err := store.openVerified(ctx, version, false)
			if err != nil {
				return errors.Join(ErrBlobConflict, err)
			}
			result = actual
			return existing.Close()
		} else if err != nil {
			return err
		}
		result = version
		if err := unix.Fsync(store.files.digest); err != nil {
			return err
		}
		return store.runCutpoint(localBlobAfterPublish)
	})
	return result, err
}

func (store *LocalBlobStore) removeKnownTemporary(ctx context.Context, guard *LocalKeyGuard, obligation WriteObligation) error {
	return guard.run(obligation.Key, func() error {
		if err := store.validateIdentity(ctx); err != nil {
			return err
		}
		if guard.store != store || obligation.TemporaryKey == nil || *obligation.TemporaryKey != "sha256/.blob-tmp-"+obligation.ID {
			return ErrLocalIdentityChanged
		}
		name := strings.TrimPrefix(*obligation.TemporaryKey, "sha256/")
		fd, err := openLocalAt(store.files.digest, name, unix.O_RDONLY, 0)
		if errors.Is(err, unix.ENOENT) {
			return unix.Fsync(store.files.digest)
		}
		if err != nil {
			return err
		}
		defer unix.Close(fd)
		stat, err := localStat(fd, unix.S_IFREG, store.namespace.OwnerUID, store.namespace.OwnerGID, 0o600)
		if err != nil || stat.Dev != store.namespace.Device {
			return ErrLocalIdentityChanged
		}
		if obligation.DispatchedAt == nil || obligation.DispatchID == nil {
			return ErrLocalIdentityChanged
		}
		if (obligation.TemporaryDevice == nil) != (obligation.TemporaryInode == nil) {
			return ErrLocalIdentityChanged
		}
		if obligation.TemporaryDevice != nil && (stat.Dev != *obligation.TemporaryDevice || stat.Ino != *obligation.TemporaryInode) {
			return ErrLocalIdentityChanged
		}
		// A creation commit may be unknown after a crash. The private exact name,
		// admitted directory, dispatched attempt and durable materialization xattr
		// still identify it; a recorded inode, when available, must also match.
		var materialization [128]byte
		n, err := unix.Fgetxattr(fd, localMaterializationAttribute, materialization[:])
		if err != nil || string(materialization[:n]) != "local-v2-"+obligation.ID {
			return ErrLocalIdentityChanged
		}
		var named unix.Stat_t
		if err := unix.Fstatat(store.files.digest, name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil || named.Ino != stat.Ino || named.Dev != stat.Dev {
			return ErrLocalIdentityChanged
		}
		if err := unix.Unlinkat(store.files.digest, name, 0); err != nil {
			return err
		}
		return unix.Fsync(store.files.digest)
	})
}
