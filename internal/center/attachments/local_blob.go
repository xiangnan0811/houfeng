package attachments

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type localBlobCutpoint string

const (
	localBlobAfterTempCreate localBlobCutpoint = "after-temp-create"
	localBlobAfterCopy       localBlobCutpoint = "after-copy"
	localBlobAfterFileSync   localBlobCutpoint = "after-file-sync"
	localBlobBeforePublish   localBlobCutpoint = "before-publish"
	localBlobAfterPublish    localBlobCutpoint = "after-publish"
)

type localBlobHooks struct {
	cutpoint func(localBlobCutpoint) error
}

type LocalBlobStore struct {
	root      string
	sha256Dir string
	hooks     localBlobHooks
	mutation  sync.Mutex
}

var _ BlobStore = (*LocalBlobStore)(nil)
var _ BlobPublicationResolver = (*LocalBlobStore)(nil)

func NewLocalBlobStore(root string) (*LocalBlobStore, error) {
	return newLocalBlobStore(root, localBlobHooks{})
}

func newLocalBlobStore(root string, hooks localBlobHooks) (*LocalBlobStore, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, ErrInvalidBlobStoreConfig
	}
	cleanRoot := filepath.Clean(root)
	if cleanRoot == string(filepath.Separator) {
		return nil, ErrInvalidBlobStoreConfig
	}
	if err := os.MkdirAll(cleanRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create local Blob root: %w", err)
	}
	if err := ensurePrivateLocalBlobDirectory(cleanRoot); err != nil {
		return nil, err
	}
	evaluatedRoot, err := filepath.EvalSymlinks(cleanRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve local Blob root: %w", err)
	}
	if filepath.Clean(evaluatedRoot) != cleanRoot {
		return nil, ErrInvalidBlobStoreConfig
	}
	sha256Dir := filepath.Join(cleanRoot, "sha256")
	if err := os.MkdirAll(sha256Dir, 0o700); err != nil {
		return nil, fmt.Errorf("create local Blob digest directory: %w", err)
	}
	if err := ensurePrivateLocalBlobDirectory(sha256Dir); err != nil {
		return nil, err
	}
	if err := syncLocalBlobDirectory(cleanRoot); err != nil {
		return nil, fmt.Errorf("sync local Blob root: %w", err)
	}
	return &LocalBlobStore{root: cleanRoot, sha256Dir: sha256Dir, hooks: hooks}, nil
}

func (store *LocalBlobStore) Put(
	ctx context.Context,
	request PutRequest,
	reader io.Reader,
) (ObjectVersion, error) {
	if ctx == nil || store == nil || nilUploadServiceDependency(reader) || request.Validate() != nil {
		return ObjectVersion{}, ErrInvalidBlobRequest
	}
	if err := ctx.Err(); err != nil {
		return ObjectVersion{}, err
	}
	version := localBlobObjectVersion(request)
	objectPath, err := store.objectPath(version)
	if err != nil {
		return ObjectVersion{}, err
	}

	temporary, err := os.CreateTemp(store.sha256Dir, ".blob-tmp-*")
	if err != nil {
		return ObjectVersion{}, fmt.Errorf("create local Blob temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if temporaryPath != "" {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return ObjectVersion{}, fmt.Errorf("set local Blob temporary mode: %w", err)
	}
	if err := store.runCutpoint(localBlobAfterTempCreate); err != nil {
		return ObjectVersion{}, err
	}

	hasher := sha256.New()
	written, err := io.Copy(
		io.MultiWriter(temporary, hasher),
		io.LimitReader(&blobContextReader{ctx: ctx, reader: reader}, request.ExpectedSizeBytes+1),
	)
	if err != nil {
		return ObjectVersion{}, fmt.Errorf("stream local Blob temporary content: %w", err)
	}
	if written != request.ExpectedSizeBytes {
		return ObjectVersion{}, ErrBlobSizeMismatch
	}
	var actualDigest [sha256.Size]byte
	copy(actualDigest[:], hasher.Sum(nil))
	if actualDigest != request.ExpectedSHA256 {
		return ObjectVersion{}, ErrBlobHashMismatch
	}
	if err := store.runCutpoint(localBlobAfterCopy); err != nil {
		return ObjectVersion{}, err
	}
	if err := temporary.Sync(); err != nil {
		return ObjectVersion{}, fmt.Errorf("sync local Blob temporary file: %w", err)
	}
	if err := store.runCutpoint(localBlobAfterFileSync); err != nil {
		return ObjectVersion{}, err
	}
	if err := temporary.Close(); err != nil {
		return ObjectVersion{}, fmt.Errorf("close local Blob temporary file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return ObjectVersion{}, err
	}

	store.mutation.Lock()
	defer store.mutation.Unlock()
	if _, err := os.Lstat(objectPath); err == nil {
		if err := verifyLocalBlobPath(ctx, objectPath, version); err != nil {
			return ObjectVersion{}, fmt.Errorf("%w: %w", ErrBlobConflict, err)
		}
		if err := removeLocalBlobTemporary(temporaryPath); err != nil {
			return ObjectVersion{}, err
		}
		temporaryPath = ""
		if err := syncLocalBlobDirectory(store.sha256Dir); err != nil {
			return ObjectVersion{}, fmt.Errorf("sync deduplicated local Blob directory: %w", err)
		}
		return version, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return ObjectVersion{}, fmt.Errorf("inspect local Blob destination: %w", err)
	}
	if err := store.runCutpoint(localBlobBeforePublish); err != nil {
		return ObjectVersion{}, err
	}
	if err := os.Link(temporaryPath, objectPath); errors.Is(err, os.ErrExist) {
		if err := verifyLocalBlobPath(ctx, objectPath, version); err != nil {
			return ObjectVersion{}, fmt.Errorf("%w: %w", ErrBlobConflict, err)
		}
		if err := removeLocalBlobTemporary(temporaryPath); err != nil {
			return ObjectVersion{}, err
		}
		temporaryPath = ""
		if err := syncLocalBlobDirectory(store.sha256Dir); err != nil {
			return ObjectVersion{}, fmt.Errorf("sync deduplicated local Blob directory: %w", err)
		}
		return version, nil
	} else if err != nil {
		return ObjectVersion{}, fmt.Errorf("publish local Blob: %w", err)
	}
	if err := removeLocalBlobTemporary(temporaryPath); err != nil {
		return ObjectVersion{}, errors.Join(err, cleanupPublishedLocalBlob(objectPath, store.sha256Dir))
	}
	temporaryPath = ""
	if err := store.runCutpoint(localBlobAfterPublish); err != nil {
		return ObjectVersion{}, errors.Join(err, cleanupPublishedLocalBlob(objectPath, store.sha256Dir))
	}
	if err := syncLocalBlobDirectory(store.sha256Dir); err != nil {
		return ObjectVersion{}, errors.Join(
			fmt.Errorf("sync local Blob digest directory: %w", err),
			cleanupPublishedLocalBlob(objectPath, store.sha256Dir),
		)
	}
	return version, nil
}

func (store *LocalBlobStore) Open(
	ctx context.Context,
	version ObjectVersion,
	byteRange ByteRange,
) (io.ReadCloser, error) {
	if ctx == nil || store == nil {
		return nil, ErrInvalidBlobRequest
	}
	if err := validateLocalBlobVersion(version); err != nil {
		return nil, err
	}
	if err := byteRange.validate(version.SizeBytes); err != nil {
		return nil, err
	}
	objectPath, err := store.objectPath(version)
	if err != nil {
		return nil, err
	}
	file, err := openVerifiedLocalBlob(ctx, objectPath, version)
	if err != nil {
		return nil, err
	}
	if byteRange.kind == byteRangeKindFull {
		return file, nil
	}
	length := byteRange.EndInclusive - byteRange.Start + 1
	return &localBlobSectionReadCloser{
		Reader: io.NewSectionReader(file, byteRange.Start, length),
		closer: file,
	}, nil
}

func (store *LocalBlobStore) Stat(ctx context.Context, version ObjectVersion) (ObjectInfo, error) {
	if ctx == nil || store == nil {
		return ObjectInfo{}, ErrInvalidBlobRequest
	}
	if err := validateLocalBlobVersion(version); err != nil {
		return ObjectInfo{}, err
	}
	objectPath, err := store.objectPath(version)
	if err != nil {
		return ObjectInfo{}, err
	}
	file, err := openVerifiedLocalBlob(ctx, objectPath, version)
	if err != nil {
		return ObjectInfo{}, err
	}
	if err := file.Close(); err != nil {
		return ObjectInfo{}, fmt.Errorf("close local Blob after stat: %w", err)
	}
	return ObjectInfo{Version: version}, nil
}

// ResolveBlobPublicationObject resolves the deterministic local final key
// without scanning the Blob directory.  The digest-derived version is part of
// the local backend contract, so Stat still performs the full byte/integrity
// verification before the version is returned to the reconciler.
func (store *LocalBlobStore) ResolveBlobPublicationObject(
	ctx context.Context,
	target BlobPublicationTarget,
) (ObjectVersion, error) {
	if ctx == nil || store == nil || target.Validate() != nil || target.BackendKind != BackendKindLocal {
		return ObjectVersion{}, ErrInvalidBlobPublicationRequest
	}
	version := ObjectVersion{
		Key: target.Key, VersionID: "local-v1-" + hexDigest(target.SHA256),
		SHA256: target.SHA256, SizeBytes: target.SizeBytes,
	}
	if _, err := store.Stat(ctx, version); err != nil {
		return ObjectVersion{}, err
	}
	return version, nil
}

func (store *LocalBlobStore) Delete(
	ctx context.Context,
	version ObjectVersion,
) (DeletionReceipt, error) {
	receipt := DeletionReceipt{Version: version}
	if ctx == nil || store == nil {
		return receipt, ErrInvalidBlobRequest
	}
	if err := validateLocalBlobVersion(version); err != nil {
		return receipt, err
	}
	objectPath, err := store.objectPath(version)
	if err != nil {
		return receipt, err
	}
	store.mutation.Lock()
	defer store.mutation.Unlock()
	file, err := openVerifiedLocalBlob(ctx, objectPath, version)
	if errors.Is(err, ErrBlobNotFound) {
		return receipt, nil
	}
	if err != nil {
		return receipt, err
	}
	if err := file.Close(); err != nil {
		return receipt, fmt.Errorf("close local Blob before delete: %w", err)
	}
	if err := os.Remove(objectPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return receipt, nil
		}
		return receipt, fmt.Errorf("delete local Blob: %w", err)
	}
	if err := syncLocalBlobDirectory(store.sha256Dir); err != nil {
		return receipt, fmt.Errorf("sync local Blob deletion: %w", err)
	}
	receipt.Deleted = true
	return receipt, nil
}

func (store *LocalBlobStore) objectPath(version ObjectVersion) (string, error) {
	if store == nil || version.Validate() != nil {
		return "", ErrInvalidBlobRequest
	}
	path := filepath.Join(store.root, filepath.FromSlash(version.Key))
	relative, err := filepath.Rel(store.root, path)
	if err != nil || relative != filepath.Join("sha256", hexDigest(version.SHA256)) ||
		filepath.Dir(path) != store.sha256Dir {
		return "", ErrInvalidBlobRequest
	}
	return path, nil
}

func (store *LocalBlobStore) runCutpoint(cutpoint localBlobCutpoint) error {
	if store.hooks.cutpoint == nil {
		return nil
	}
	return store.hooks.cutpoint(cutpoint)
}

func localBlobObjectVersion(request PutRequest) ObjectVersion {
	digest := hexDigest(request.ExpectedSHA256)
	return ObjectVersion{
		Key:       "sha256/" + digest,
		VersionID: "local-v1-" + digest,
		SHA256:    request.ExpectedSHA256,
		SizeBytes: request.ExpectedSizeBytes,
	}
}

func validateLocalBlobVersion(version ObjectVersion) error {
	if version.Validate() != nil {
		return ErrInvalidBlobRequest
	}
	if version.VersionID != "local-v1-"+hexDigest(version.SHA256) {
		return ErrBlobVersionMismatch
	}
	return nil
}

func openVerifiedLocalBlob(
	ctx context.Context,
	path string,
	version ObjectVersion,
) (*os.File, error) {
	pathInfo, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrBlobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("inspect local Blob: %w", err)
	}
	if !pathInfo.Mode().IsRegular() || pathInfo.Mode().Perm() != 0o600 {
		return nil, ErrBlobConflict
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrBlobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open local Blob: %w", err)
	}
	closeOnError := func(err error) (*os.File, error) {
		_ = file.Close()
		return nil, err
	}
	fileInfo, err := file.Stat()
	if err != nil {
		return closeOnError(fmt.Errorf("stat open local Blob: %w", err))
	}
	if !os.SameFile(pathInfo, fileInfo) || !fileInfo.Mode().IsRegular() || fileInfo.Mode().Perm() != 0o600 {
		return closeOnError(ErrBlobConflict)
	}
	if fileInfo.Size() != version.SizeBytes {
		return closeOnError(ErrBlobSizeMismatch)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, &blobContextReader{ctx: ctx, reader: file}); err != nil {
		return closeOnError(fmt.Errorf("hash local Blob: %w", err))
	}
	var actualDigest [sha256.Size]byte
	copy(actualDigest[:], hasher.Sum(nil))
	if actualDigest != version.SHA256 {
		return closeOnError(ErrBlobHashMismatch)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return closeOnError(fmt.Errorf("rewind local Blob: %w", err))
	}
	return file, nil
}

func verifyLocalBlobPath(ctx context.Context, path string, version ObjectVersion) error {
	file, err := openVerifiedLocalBlob(ctx, path, version)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close verified local Blob: %w", err)
	}
	return nil
}

func ensurePrivateLocalBlobDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect local Blob directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidBlobStoreConfig
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("set local Blob directory mode: %w", err)
	}
	return nil
}

func syncLocalBlobDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return err
	}
	return directory.Close()
}

func cleanupPublishedLocalBlob(path, directory string) error {
	removeErr := os.Remove(path)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(removeErr, syncLocalBlobDirectory(directory))
}

func removeLocalBlobTemporary(path string) error {
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove local Blob temporary file: %w", err)
	}
	return nil
}

type blobContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *blobContextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

type localBlobSectionReadCloser struct {
	io.Reader
	closer io.Closer
}

func (reader *localBlobSectionReadCloser) Close() error {
	return reader.closer.Close()
}
