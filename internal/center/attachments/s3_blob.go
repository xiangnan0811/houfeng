package attachments

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/s3utils"
)

const (
	s3BlobTemporaryPrefix               = "temporary/"
	s3BlobCleanupTimeout                = 30 * time.Second
	s3BlobConditionalConflictAttempts   = 4
	s3BlobConditionalConflictRetryDelay = 50 * time.Millisecond
	s3BlobMaximumPresignedUploadTTL     = time.Hour
)

type S3BlobStore struct {
	client *minio.Client
	core   minio.Core
	bucket string
}

var _ BlobStore = (*S3BlobStore)(nil)
var _ TemporaryObjectStore = (*S3BlobStore)(nil)
var _ TemporaryUploadPresigner = (*S3BlobStore)(nil)
var _ BlobPublicationResolver = (*S3BlobStore)(nil)

func NewS3BlobStore(client *minio.Client, bucket string) (*S3BlobStore, error) {
	if client == nil || strings.TrimSpace(bucket) != bucket || s3utils.CheckValidBucketNameStrict(bucket) != nil {
		return nil, ErrInvalidBlobStoreConfig
	}
	return &S3BlobStore{
		client: client,
		core:   minio.Core{Client: client},
		bucket: bucket,
	}, nil
}

func (store *S3BlobStore) PresignTemporaryUpload(
	ctx context.Context,
	temporaryObjectKey string,
	ttl time.Duration,
) (string, string, []string, error) {
	if ctx == nil || store == nil || store.client == nil || !validS3BlobTemporaryKey(temporaryObjectKey) ||
		ttl < time.Second || ttl > s3BlobMaximumPresignedUploadTTL {
		return "", "", nil, ErrInvalidBlobRequest
	}
	if err := ctx.Err(); err != nil {
		return "", "", nil, err
	}
	if err := store.verifyBucketContract(ctx); err != nil {
		return "", "", nil, err
	}
	uploadURL, err := store.client.PresignedPutObject(ctx, store.bucket, temporaryObjectKey, ttl)
	if err != nil {
		return "", "", nil, fmt.Errorf("presign S3 Blob temporary upload: %w", err)
	}
	return uploadURL.String(), http.MethodPut, []string{}, nil
}

func (store *S3BlobStore) ResolveTemporaryVersion(
	ctx context.Context,
	key string,
) (TemporaryObjectVersion, error) {
	if ctx == nil || store == nil || store.client == nil || !validS3BlobTemporaryKey(key) {
		return TemporaryObjectVersion{}, ErrInvalidBlobRequest
	}
	if err := ctx.Err(); err != nil {
		return TemporaryObjectVersion{}, err
	}
	if err := store.verifyBucketContract(ctx); err != nil {
		return TemporaryObjectVersion{}, err
	}
	info, err := store.client.StatObject(ctx, store.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return TemporaryObjectVersion{}, fmt.Errorf(
			"resolve current S3 Blob temporary version: %w",
			store.mapS3BlobReadError(ctx, err),
		)
	}
	if info.IsDeleteMarker || !validS3BlobVersionID(info.VersionID) {
		return TemporaryObjectVersion{}, ErrBlobVersionMismatch
	}
	return TemporaryObjectVersion{Key: key, VersionID: info.VersionID}, nil
}

func (store *S3BlobStore) OpenTemporaryVersion(
	ctx context.Context,
	request TemporaryObjectReadRequest,
) (io.ReadCloser, error) {
	if ctx == nil || store == nil || store.client == nil || request.Validate() != nil ||
		!validS3BlobTemporaryKey(request.Version.Key) || !validS3BlobVersionID(request.Version.VersionID) {
		return nil, ErrInvalidBlobRequest
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := store.verifyBucketContract(ctx); err != nil {
		return nil, err
	}
	if err := store.verifyCurrentTemporaryVersion(ctx, request.Version); err != nil {
		return nil, err
	}
	object, info, _, err := store.core.GetObject(ctx, store.bucket, request.Version.Key, minio.GetObjectOptions{
		VersionID: request.Version.VersionID,
	})
	if err != nil {
		return nil, fmt.Errorf("open exact S3 Blob temporary version: %w", store.mapS3BlobReadError(ctx, err))
	}
	if info.IsDeleteMarker || info.VersionID != request.Version.VersionID {
		_ = object.Close()
		return nil, ErrBlobVersionMismatch
	}
	if info.Size != request.ExpectedSizeBytes {
		_ = object.Close()
		return nil, ErrBlobSizeMismatch
	}
	return object, nil
}

func (store *S3BlobStore) PublishTemporaryVersion(
	ctx context.Context,
	request TemporaryObjectPublishRequest,
) (ObjectVersion, error) {
	if ctx == nil || store == nil || store.client == nil || request.Validate() != nil ||
		!validS3BlobTemporaryKey(request.Version.Key) || !validS3BlobVersionID(request.Version.VersionID) {
		return ObjectVersion{}, ErrInvalidBlobRequest
	}
	if err := ctx.Err(); err != nil {
		return ObjectVersion{}, err
	}
	if err := store.verifyBucketContract(ctx); err != nil {
		return ObjectVersion{}, err
	}
	if err := store.verifyCurrentTemporaryVersion(ctx, request.Version); err != nil {
		return ObjectVersion{}, err
	}
	putRequest := PutRequest{
		ExpectedSHA256: request.ExpectedSHA256, ExpectedSizeBytes: request.ExpectedSizeBytes,
		TemporaryKey: request.Version.Key,
	}
	temporary := ObjectVersion{
		Key: request.Version.Key, VersionID: request.Version.VersionID,
		SHA256: request.ExpectedSHA256, SizeBytes: request.ExpectedSizeBytes,
	}
	published, err := store.publishExactTemporary(
		ctx, temporary, "sha256/"+hexDigest(request.ExpectedSHA256), putRequest,
	)
	if err != nil {
		return ObjectVersion{}, err
	}
	if err := store.verifyCurrentTemporaryVersion(ctx, request.Version); err != nil {
		return ObjectVersion{}, err
	}
	return published, nil
}

func (store *S3BlobStore) DeleteTemporaryVersion(
	ctx context.Context,
	version TemporaryObjectVersion,
) error {
	if ctx == nil || store == nil || store.client == nil || version.Validate() != nil ||
		!validS3BlobTemporaryKey(version.Key) || !validS3BlobCleanupVersionID(version.VersionID) {
		return ErrInvalidBlobRequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.verifyBucketContract(ctx); err != nil {
		return err
	}
	if err := store.verifyCurrentTemporaryVersion(ctx, version); errors.Is(err, ErrBlobNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	return store.removeTemporary(ctx, version.Key, version.VersionID)
}

func (store *S3BlobStore) verifyCurrentTemporaryVersion(
	ctx context.Context,
	version TemporaryObjectVersion,
) error {
	current, err := store.client.StatObject(ctx, store.bucket, version.Key, minio.StatObjectOptions{})
	if err != nil {
		if current.IsDeleteMarker {
			return ErrBlobVersionMismatch
		}
		if isS3CurrentObjectMissing(err) {
			exact, exactErr := store.client.StatObject(ctx, store.bucket, version.Key, minio.StatObjectOptions{
				VersionID: version.VersionID,
			})
			if exactErr == nil || exact.IsDeleteMarker {
				return ErrBlobVersionMismatch
			}
			if isS3ExactVersionMissing(exactErr) {
				return store.mapS3BlobReadError(ctx, exactErr)
			}
			return fmt.Errorf("stat exact S3 Blob temporary version: %w", exactErr)
		}
		return fmt.Errorf("stat current S3 Blob temporary version: %w", err)
	}
	if current.IsDeleteMarker || !validS3BlobVersionID(current.VersionID) || current.VersionID != version.VersionID {
		return ErrBlobVersionMismatch
	}
	return nil
}

func (store *S3BlobStore) Put(
	ctx context.Context,
	request PutRequest,
	reader io.Reader,
) (version ObjectVersion, resultErr error) {
	if ctx == nil || store == nil || store.client == nil || nilUploadServiceDependency(reader) || request.Validate() != nil ||
		!validS3BlobTemporaryKey(request.TemporaryKey) {
		return ObjectVersion{}, ErrInvalidBlobRequest
	}
	if err := ctx.Err(); err != nil {
		return ObjectVersion{}, err
	}
	if err := store.verifyBucketContract(ctx); err != nil {
		return ObjectVersion{}, err
	}
	temporaryKey := request.TemporaryKey
	temporaryVersionID := ""
	defer func() {
		if !validS3BlobCleanupVersionID(temporaryVersionID) {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s3BlobCleanupTimeout)
		defer cancel()
		if cleanupErr := store.removeTemporary(cleanupCtx, temporaryKey, temporaryVersionID); cleanupErr != nil {
			version = ObjectVersion{}
			resultErr = errors.Join(resultErr, cleanupErr)
		}
	}()

	uploadReader := &s3BlobExactSizeReader{
		ctx:       ctx,
		reader:    reader,
		remaining: request.ExpectedSizeBytes,
	}
	upload, err := store.client.PutObject(
		ctx,
		store.bucket,
		temporaryKey,
		uploadReader,
		request.ExpectedSizeBytes,
		s3BlobPutOptions(request),
	)
	if err != nil {
		return ObjectVersion{}, fmt.Errorf("upload S3 Blob temporary object: %w", err)
	}
	temporaryVersionID = upload.VersionID
	if !validS3BlobVersionID(temporaryVersionID) {
		return ObjectVersion{}, ErrInvalidBlobStoreConfig
	}
	if err := ensureS3BlobReaderExhausted(ctx, reader); err != nil {
		return ObjectVersion{}, err
	}
	temporaryVersion := ObjectVersion{
		Key:       temporaryKey,
		VersionID: temporaryVersionID,
		SHA256:    request.ExpectedSHA256,
		SizeBytes: request.ExpectedSizeBytes,
	}
	if err := store.verifyExactVersion(ctx, temporaryVersion, false); err != nil {
		return ObjectVersion{}, fmt.Errorf("verify S3 Blob temporary object: %w", err)
	}

	finalKey := "sha256/" + hexDigest(request.ExpectedSHA256)
	published, err := store.publishExactTemporary(ctx, temporaryVersion, finalKey, request)
	if err != nil {
		return ObjectVersion{}, err
	}
	return published, nil
}

func (store *S3BlobStore) verifyBucketContract(ctx context.Context) error {
	if err := store.verifyBucketVersioning(ctx); err != nil {
		return err
	}
	_, _, _, _, err := store.client.GetObjectLockConfig(ctx, store.bucket)
	if err != nil {
		if isS3ObjectLockConfigurationAbsent(err) {
			return nil
		}
		return fmt.Errorf("%w: inspect S3 Blob bucket Object Lock: %w", ErrInvalidBlobStoreConfig, err)
	}
	return ErrInvalidBlobStoreConfig
}

func (store *S3BlobStore) verifyBucketVersioning(ctx context.Context) error {
	versioning, err := store.client.GetBucketVersioning(ctx, store.bucket)
	if err != nil {
		return fmt.Errorf("%w: inspect S3 Blob bucket versioning: %w", ErrInvalidBlobStoreConfig, err)
	}
	if !versioning.Enabled() {
		return ErrInvalidBlobStoreConfig
	}
	return nil
}

func (store *S3BlobStore) Open(
	ctx context.Context,
	version ObjectVersion,
	byteRange ByteRange,
) (io.ReadCloser, error) {
	if ctx == nil || store == nil || store.client == nil {
		return nil, ErrInvalidBlobRequest
	}
	if err := validateS3BlobVersion(version); err != nil {
		return nil, err
	}
	if err := byteRange.validate(version.SizeBytes); err != nil {
		return nil, err
	}
	if err := store.verifyBucketContract(ctx); err != nil {
		return nil, err
	}
	if err := store.verifyExactVersion(ctx, version, true); err != nil {
		return nil, err
	}

	opts := minio.GetObjectOptions{VersionID: version.VersionID}
	expectedReadSize := version.SizeBytes
	if byteRange.kind == byteRangeKindClosed {
		if err := opts.SetRange(byteRange.Start, byteRange.EndInclusive); err != nil {
			return nil, ErrInvalidBlobRange
		}
		expectedReadSize = byteRange.EndInclusive - byteRange.Start + 1
	}
	object, info, _, err := store.core.GetObject(ctx, store.bucket, version.Key, opts)
	if err != nil {
		return nil, store.mapS3BlobReadError(ctx, err)
	}
	if info.IsDeleteMarker || info.VersionID != version.VersionID || info.Size != expectedReadSize {
		_ = object.Close()
		return nil, ErrBlobVersionMismatch
	}
	return object, nil
}

func (store *S3BlobStore) Stat(ctx context.Context, version ObjectVersion) (ObjectInfo, error) {
	if ctx == nil || store == nil || store.client == nil {
		return ObjectInfo{}, ErrInvalidBlobRequest
	}
	if err := validateS3BlobVersion(version); err != nil {
		return ObjectInfo{}, err
	}
	if err := store.verifyBucketContract(ctx); err != nil {
		return ObjectInfo{}, err
	}
	if err := store.verifyExactVersion(ctx, version, true); err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{Version: version}, nil
}

// ResolveBlobPublicationObject performs one exact final-key lookup and then
// re-verifies the bytes under the observed current version.  It deliberately
// does not list keys or versions.
func (store *S3BlobStore) ResolveBlobPublicationObject(
	ctx context.Context,
	target BlobPublicationTarget,
) (ObjectVersion, error) {
	if ctx == nil || store == nil || store.client == nil || target.Validate() != nil ||
		target.BackendKind != BackendKindS3 {
		return ObjectVersion{}, ErrInvalidBlobPublicationRequest
	}
	if err := store.verifyBucketContract(ctx); err != nil {
		return ObjectVersion{}, err
	}
	version, err := store.existingDigestVersion(ctx, target.Key, PutRequest{
		ExpectedSHA256: target.SHA256, ExpectedSizeBytes: target.SizeBytes,
	})
	if err != nil {
		return ObjectVersion{}, err
	}
	return version, nil
}

func (store *S3BlobStore) Delete(
	ctx context.Context,
	version ObjectVersion,
) (DeletionReceipt, error) {
	receipt := DeletionReceipt{Version: version}
	if ctx == nil || store == nil || store.client == nil {
		return receipt, ErrInvalidBlobRequest
	}
	if err := validateS3BlobVersion(version); err != nil {
		return receipt, err
	}
	if err := store.verifyBucketContract(ctx); err != nil {
		return receipt, err
	}
	if err := store.verifyExactVersion(ctx, version, true); errors.Is(err, ErrBlobNotFound) {
		return receipt, nil
	} else if err != nil {
		return receipt, err
	}
	if err := store.client.RemoveObject(ctx, store.bucket, version.Key, minio.RemoveObjectOptions{
		VersionID: version.VersionID,
	}); err != nil {
		return receipt, fmt.Errorf("delete exact S3 Blob version: %w", err)
	}
	if err := store.requireVersionAbsent(ctx, version.Key, version.VersionID); err != nil {
		return receipt, err
	}
	receipt.Deleted = true
	return receipt, nil
}

func (store *S3BlobStore) publishExactTemporary(
	ctx context.Context,
	temporary ObjectVersion,
	finalKey string,
	request PutRequest,
) (ObjectVersion, error) {
	var lastConflict error
	for attempt := 0; attempt < s3BlobConditionalConflictAttempts; attempt++ {
		if attempt > 0 {
			if err := waitForS3BlobConditionalConflictRetry(ctx); err != nil {
				return ObjectVersion{}, err
			}
		}
		if err := store.ensureDigestPublicationState(ctx, finalKey); err != nil {
			return ObjectVersion{}, err
		}
		upload, err := store.putExactTemporary(ctx, temporary, finalKey, request)
		if err != nil {
			if isS3ConditionalRequestConflict(err) {
				lastConflict = err
				existing, existingErr := store.existingDigestVersion(ctx, finalKey, request)
				if existingErr == nil {
					return existing, nil
				}
				if !errors.Is(existingErr, ErrBlobNotFound) {
					return ObjectVersion{}, errors.Join(
						fmt.Errorf("conditionally publish S3 Blob: %w", err),
						existingErr,
					)
				}
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ObjectVersion{}, ctxErr
				}
				continue
			}
			if lastConflict != nil && errors.Is(err, ErrBlobNotFound) {
				return ObjectVersion{}, fmt.Errorf(
					"%w: exact S3 Blob temporary source unavailable after conditional conflict",
					ErrBlobConflict,
				)
			}
			return store.resolveConditionalPublicationFailure(ctx, finalKey, request, err)
		}
		return store.verifyPublishedUpload(ctx, finalKey, request, upload)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ObjectVersion{}, ctxErr
	}
	return ObjectVersion{}, fmt.Errorf(
		"%w: S3 Blob conditional publication did not converge: %w",
		ErrBlobConflict,
		lastConflict,
	)
}

func (store *S3BlobStore) putExactTemporary(
	ctx context.Context,
	temporary ObjectVersion,
	finalKey string,
	request PutRequest,
) (minio.UploadInfo, error) {
	source, info, _, err := store.core.GetObject(ctx, store.bucket, temporary.Key, minio.GetObjectOptions{
		VersionID: temporary.VersionID,
	})
	if err != nil {
		return minio.UploadInfo{}, fmt.Errorf("open exact S3 Blob temporary version for publication: %w", store.mapS3BlobReadError(ctx, err))
	}
	if info.IsDeleteMarker || info.VersionID != temporary.VersionID || info.Size != temporary.SizeBytes {
		_ = source.Close()
		return minio.UploadInfo{}, ErrBlobVersionMismatch
	}
	defer source.Close()

	options := s3BlobPutOptions(request)
	options.SetMatchETagExcept("*")
	return store.client.PutObject(
		ctx,
		store.bucket,
		finalKey,
		source,
		request.ExpectedSizeBytes,
		options,
	)
}

func (store *S3BlobStore) verifyPublishedUpload(
	ctx context.Context,
	finalKey string,
	request PutRequest,
	upload minio.UploadInfo,
) (ObjectVersion, error) {
	if !validS3BlobVersionID(upload.VersionID) {
		return ObjectVersion{}, errors.Join(
			ErrInvalidBlobStoreConfig,
			store.removeExactVersionAfterFailure(ctx, finalKey, upload.VersionID),
		)
	}
	published := ObjectVersion{
		Key:       finalKey,
		VersionID: upload.VersionID,
		SHA256:    request.ExpectedSHA256,
		SizeBytes: request.ExpectedSizeBytes,
	}
	if err := store.verifyExactVersion(ctx, published, true); err != nil {
		verificationErr := fmt.Errorf("verify published S3 Blob: %w", err)
		if isS3PublishedSemanticInvalidity(err) {
			return ObjectVersion{}, errors.Join(
				verificationErr,
				store.removeExactVersionAfterFailure(ctx, finalKey, upload.VersionID),
			)
		}
		return ObjectVersion{}, verificationErr
	}
	return published, nil
}

func (store *S3BlobStore) resolveConditionalPublicationFailure(
	ctx context.Context,
	key string,
	request PutRequest,
	publishErr error,
) (ObjectVersion, error) {
	publicationErr := fmt.Errorf("conditionally publish S3 Blob: %w", publishErr)
	if !isS3ConditionalPublicationConflict(publishErr) {
		return ObjectVersion{}, publicationErr
	}
	version, err := store.existingDigestVersion(ctx, key, request)
	if err != nil {
		if errors.Is(err, ErrBlobNotFound) {
			return ObjectVersion{}, errors.Join(publicationErr, ErrBlobConflict)
		}
		return ObjectVersion{}, errors.Join(publicationErr, err)
	}
	return version, nil
}

func waitForS3BlobConditionalConflictRetry(ctx context.Context) error {
	timer := time.NewTimer(s3BlobConditionalConflictRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (store *S3BlobStore) ensureDigestPublicationState(ctx context.Context, key string) error {
	presigned, err := store.client.PresignedHeadObject(ctx, store.bucket, key, time.Minute, nil)
	if err != nil {
		return fmt.Errorf("prepare S3 Blob digest state inspection: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, presigned.String(), nil)
	if err != nil {
		return fmt.Errorf("build S3 Blob digest state inspection: %w", err)
	}
	response, err := store.client.CredContext().Client.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return errors.New("inspect S3 Blob digest state")
	}
	if response.Body != nil {
		_ = response.Body.Close()
	}
	if strings.EqualFold(response.Header.Get("x-amz-delete-marker"), "true") {
		return ErrBlobConflict
	}

	_, err = store.client.StatObject(ctx, store.bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return nil
	}
	if isS3CurrentObjectMissing(err) {
		return store.verifyBucketVersioning(ctx)
	}
	return fmt.Errorf("stat S3 Blob digest destination: %w", err)
}

func (store *S3BlobStore) existingDigestVersion(
	ctx context.Context,
	key string,
	request PutRequest,
) (ObjectVersion, error) {
	current, err := store.client.StatObject(ctx, store.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectVersion{}, fmt.Errorf("stat existing S3 Blob digest object: %w", store.mapS3BlobReadError(ctx, err))
	}
	if current.IsDeleteMarker || !validS3BlobVersionID(current.VersionID) {
		return ObjectVersion{}, ErrBlobConflict
	}
	version := ObjectVersion{
		Key:       key,
		VersionID: current.VersionID,
		SHA256:    request.ExpectedSHA256,
		SizeBytes: request.ExpectedSizeBytes,
	}
	if err := store.verifyExactVersion(ctx, version, true); err != nil {
		return ObjectVersion{}, fmt.Errorf("%w: %w", ErrBlobConflict, err)
	}
	return version, nil
}

func (store *S3BlobStore) verifyExactVersion(
	ctx context.Context,
	version ObjectVersion,
	requireCurrent bool,
) error {
	if requireCurrent {
		current, err := store.client.StatObject(ctx, store.bucket, version.Key, minio.StatObjectOptions{})
		if err != nil {
			if current.IsDeleteMarker {
				return ErrBlobVersionMismatch
			}
			if isS3CurrentObjectMissing(err) {
				exact, exactErr := store.client.StatObject(ctx, store.bucket, version.Key, minio.StatObjectOptions{
					VersionID: version.VersionID,
				})
				if exactErr == nil || exact.IsDeleteMarker {
					return ErrBlobVersionMismatch
				}
				if isS3ExactVersionMissing(exactErr) {
					return store.mapS3BlobReadError(ctx, exactErr)
				}
				return fmt.Errorf("stat exact S3 Blob version: %w", exactErr)
			}
			return fmt.Errorf("stat current S3 Blob version: %w", err)
		}
		if current.IsDeleteMarker || !validS3BlobVersionID(current.VersionID) || current.VersionID != version.VersionID {
			return ErrBlobVersionMismatch
		}
	}

	exact, err := store.client.StatObject(ctx, store.bucket, version.Key, minio.StatObjectOptions{
		VersionID: version.VersionID,
	})
	if err != nil {
		if exact.IsDeleteMarker {
			return ErrBlobVersionMismatch
		}
		return store.mapS3BlobReadError(ctx, err)
	}
	if exact.IsDeleteMarker || exact.VersionID != version.VersionID {
		return ErrBlobVersionMismatch
	}
	if exact.Size != version.SizeBytes {
		return ErrBlobSizeMismatch
	}

	object, info, _, err := store.core.GetObject(ctx, store.bucket, version.Key, minio.GetObjectOptions{
		VersionID: version.VersionID,
	})
	if err != nil {
		return store.mapS3BlobReadError(ctx, err)
	}
	if info.IsDeleteMarker || info.VersionID != version.VersionID || info.Size != version.SizeBytes {
		_ = object.Close()
		return ErrBlobVersionMismatch
	}
	hasher := sha256.New()
	read, readErr := io.Copy(hasher, object)
	closeErr := object.Close()
	if readErr != nil || closeErr != nil {
		return errors.Join(readErr, closeErr)
	}
	if read != version.SizeBytes {
		return ErrBlobSizeMismatch
	}
	var actualDigest [sha256.Size]byte
	copy(actualDigest[:], hasher.Sum(nil))
	if actualDigest != version.SHA256 {
		return ErrBlobHashMismatch
	}
	return nil
}

func (store *S3BlobStore) removeTemporary(ctx context.Context, key, versionID string) error {
	if !validS3BlobCleanupVersionID(versionID) {
		return ErrInvalidBlobStoreConfig
	}
	if err := store.client.RemoveObject(ctx, store.bucket, key, minio.RemoveObjectOptions{
		VersionID: versionID,
	}); err != nil {
		return fmt.Errorf("remove exact S3 Blob temporary version: %w", err)
	}
	return store.requireVersionAbsent(ctx, key, versionID)
}

func (store *S3BlobStore) requireVersionAbsent(ctx context.Context, key, versionID string) error {
	info, err := store.client.StatObject(ctx, store.bucket, key, minio.StatObjectOptions{VersionID: versionID})
	if err != nil && isS3ExactVersionMissing(err) {
		return store.verifyBucketVersioning(ctx)
	}
	if err != nil {
		return fmt.Errorf("confirm exact S3 Blob version deletion: %w", err)
	}
	if info.IsDeleteMarker {
		return ErrBlobVersionMismatch
	}
	return ErrBlobConflict
}

func (store *S3BlobStore) removeExactVersionAfterFailure(ctx context.Context, key, versionID string) error {
	if !validS3BlobCleanupVersionID(versionID) {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s3BlobCleanupTimeout)
	defer cancel()
	if err := store.client.RemoveObject(cleanupCtx, store.bucket, key, minio.RemoveObjectOptions{
		VersionID: versionID,
	}); err != nil {
		return fmt.Errorf("remove failed S3 Blob publication: %w", err)
	}
	return store.requireVersionAbsent(cleanupCtx, key, versionID)
}

func newS3BlobTemporaryKey() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate S3 Blob temporary key: %w", err)
	}
	return s3BlobTemporaryPrefix + fmt.Sprintf("%x", random), nil
}

func s3BlobPutOptions(request PutRequest) minio.PutObjectOptions {
	return minio.PutObjectOptions{
		ContentType:      "application/octet-stream",
		DisableMultipart: true,
		UserMetadata: map[string]string{
			"houfeng-sha256": hexDigest(request.ExpectedSHA256),
		},
	}
}

func validateS3BlobVersion(version ObjectVersion) error {
	if version.Validate() != nil {
		return ErrInvalidBlobRequest
	}
	if !validS3BlobVersionID(version.VersionID) {
		return ErrBlobVersionMismatch
	}
	return nil
}

func validS3BlobVersionID(versionID string) bool {
	return versionID != "" && versionID != "null" && len(versionID) <= 1024
}

func validS3BlobCleanupVersionID(versionID string) bool {
	return versionID != "" && len(versionID) <= 1024
}

func validS3BlobTemporaryKey(key string) bool {
	if len(key) != len(s3BlobTemporaryPrefix)+sha256.Size*2 || !strings.HasPrefix(key, s3BlobTemporaryPrefix) {
		return false
	}
	for _, character := range key[len(s3BlobTemporaryPrefix):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func isS3PublishedSemanticInvalidity(err error) bool {
	return errors.Is(err, ErrBlobVersionMismatch) ||
		errors.Is(err, ErrBlobSizeMismatch) ||
		errors.Is(err, ErrBlobHashMismatch)
}

func (store *S3BlobStore) mapS3BlobReadError(ctx context.Context, err error) error {
	if isS3ExactVersionMissing(err) {
		if versioningErr := store.verifyBucketVersioning(ctx); versioningErr != nil {
			return versioningErr
		}
		return ErrBlobNotFound
	}
	return err
}

func isS3CurrentObjectMissing(err error) bool {
	return minio.ToErrorResponse(err).Code == minio.NoSuchKey
}

func isS3ExactVersionMissing(err error) bool {
	code := minio.ToErrorResponse(err).Code
	return code == minio.NoSuchKey || code == minio.NoSuchVersion
}

func isS3ConditionalPublicationConflict(err error) bool {
	response := minio.ToErrorResponse(err)
	return response.StatusCode == http.StatusPreconditionFailed || response.Code == "PreconditionFailed" ||
		response.Code == "ConditionalRequestConflict"
}

func isS3ConditionalRequestConflict(err error) bool {
	return minio.ToErrorResponse(err).Code == "ConditionalRequestConflict"
}

func isS3ObjectLockConfigurationAbsent(err error) bool {
	response := minio.ToErrorResponse(err)
	return response.Code == "ObjectLockConfigurationNotFoundError" ||
		response.Code == "NoSuchObjectLockConfiguration"
}

func ensureS3BlobReaderExhausted(ctx context.Context, reader io.Reader) error {
	one := make([]byte, 1)
	read, err := io.ReadFull(&blobContextReader{ctx: ctx, reader: reader}, one)
	if read > 0 {
		return ErrBlobSizeMismatch
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

type s3BlobExactSizeReader struct {
	ctx       context.Context
	reader    io.Reader
	remaining int64
	terminal  error
}

func (reader *s3BlobExactSizeReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	if reader.terminal != nil {
		return 0, reader.terminal
	}
	if reader.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(buffer)) > reader.remaining {
		buffer = buffer[:reader.remaining]
	}
	read, err := reader.reader.Read(buffer)
	reader.remaining -= int64(read)
	if errors.Is(err, io.EOF) && reader.remaining > 0 {
		reader.terminal = ErrBlobSizeMismatch
		return read, reader.terminal
	}
	if err != nil && !errors.Is(err, io.EOF) {
		reader.terminal = err
	}
	return read, err
}
