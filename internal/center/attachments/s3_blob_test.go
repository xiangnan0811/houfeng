package attachments

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const minIOIntegrationEnvironment = "HOUFENG_MINIO_INTEGRATION"

func TestNewS3BlobStoreRejectsInvalidConfiguration(t *testing.T) {
	client, err := minio.New("127.0.0.1:9000", &minio.Options{
		Creds: credentials.NewStaticV4("test-access", "test-secret", ""),
	})
	if err != nil {
		t.Fatalf("minio.New() error = %v", err)
	}
	tests := []struct {
		name   string
		client *minio.Client
		bucket string
	}{
		{name: "nil client", bucket: "houfeng-blobs"},
		{name: "empty bucket", client: client},
		{name: "whitespace bucket", client: client, bucket: " houfeng-blobs "},
		{name: "invalid bucket", client: client, bucket: "INVALID_BUCKET"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewS3BlobStore(tt.client, tt.bucket); !errors.Is(err, ErrInvalidBlobStoreConfig) {
				t.Fatalf("NewS3BlobStore() error = %v, want ErrInvalidBlobStoreConfig", err)
			}
		})
	}
}

func TestS3BlobStorePutRejectsTypedNilReaderBeforeBackendAccess(t *testing.T) {
	t.Parallel()

	backendRequests := 0
	client, err := minio.New("s3.example.test", &minio.Options{
		Creds: credentials.NewStaticV4("test-access", "test-secret", ""),
		Transport: s3BlobRoundTripperFunc(func(*http.Request) (*http.Response, error) {
			backendRequests++
			return nil, errors.New("unexpected backend access")
		}),
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("minio.New() error = %v", err)
	}
	store, err := NewS3BlobStore(client, "houfeng-blobs")
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	content := []byte("typed-nil S3 Blob reader")
	request := blobPutRequest(content)
	request.TemporaryKey = s3BlobTemporaryPrefix + strings.Repeat("4", sha256.Size*2)
	var typedNil *bytes.Reader
	var reader io.Reader = typedNil

	err, panicValue := captureAttachmentCallPanic(func() error {
		_, err := store.Put(context.Background(), request, reader)
		return err
	})
	if panicValue != nil {
		t.Fatalf("Put(typed-nil reader) panic = %v after %d backend requests; want ErrInvalidBlobRequest before backend access",
			panicValue, backendRequests)
	}
	if !errors.Is(err, ErrInvalidBlobRequest) {
		t.Fatalf("Put(typed-nil reader) error = %v, want ErrInvalidBlobRequest", err)
	}
	if backendRequests != 0 {
		t.Fatalf("Put(typed-nil reader) backend requests = %d, want 0", backendRequests)
	}
}

func TestS3BlobStorePresignTemporaryUploadRejectsSubSecondTTLBeforeBackendCall(t *testing.T) {
	backendCalls := 0
	client, err := minio.New("s3.invalid", &minio.Options{
		Creds: credentials.NewStaticV4("test-access", "test-secret", ""),
		Transport: s3BlobRoundTripperFunc(func(*http.Request) (*http.Response, error) {
			backendCalls++
			return nil, errors.New("unexpected backend call")
		}),
	})
	if err != nil {
		t.Fatalf("minio.New() error = %v", err)
	}
	store, err := NewS3BlobStore(client, "houfeng-blobs")
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	const temporaryKey = "temporary/4444444444444444444444444444444444444444444444444444444444444444"

	uploadURL, method, requiredHeaders, err := store.PresignTemporaryUpload(
		context.Background(), temporaryKey, 500*time.Millisecond,
	)
	if !errors.Is(err, ErrInvalidBlobRequest) {
		t.Fatalf("PresignTemporaryUpload(sub-second TTL) error = %v, want ErrInvalidBlobRequest", err)
	}
	if uploadURL != "" || method != "" || requiredHeaders != nil || backendCalls != 0 {
		t.Fatalf("PresignTemporaryUpload(sub-second TTL) = %q/%q/%#v backendCalls=%d",
			uploadURL, method, requiredHeaders, backendCalls)
	}
}

func TestS3BlobStorePresignedTemporaryUploadAcceptsUnauthenticatedPut(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	store, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const temporaryKey = "temporary/3333333333333333333333333333333333333333333333333333333333333333"

	uploadURL, method, requiredHeaders, err := store.PresignTemporaryUpload(ctx, temporaryKey, 5*time.Minute)
	if err != nil {
		t.Fatalf("PresignTemporaryUpload() error = %v", err)
	}
	if uploadURL == "" || method != http.MethodPut || requiredHeaders == nil || len(requiredHeaders) != 0 {
		t.Fatalf("PresignTemporaryUpload() = %q/%q/%#v", uploadURL, method, requiredHeaders)
	}
	content := []byte("presigned temporary upload bytes")
	request, err := http.NewRequestWithContext(ctx, method, uploadURL, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("unauthenticated presigned PUT error = %v", err)
	}
	if response.Body != nil {
		defer response.Body.Close()
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("unauthenticated presigned PUT status = %d body=%q", response.StatusCode, body)
	}
	resolved, err := store.ResolveTemporaryVersion(ctx, temporaryKey)
	if err != nil {
		t.Fatalf("ResolveTemporaryVersion() error = %v", err)
	}
	if resolved.Key != temporaryKey || resolved.VersionID == "" {
		t.Fatalf("ResolveTemporaryVersion() = %#v, want exact key and version", resolved)
	}
	reader, err := store.OpenTemporaryVersion(ctx, TemporaryObjectReadRequest{
		Version: resolved, ExpectedSizeBytes: int64(len(content)),
	})
	if err != nil {
		t.Fatalf("OpenTemporaryVersion() error = %v", err)
	}
	got, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(got, content) {
		t.Fatalf("presigned temporary bytes = %q, read/close errors = %v/%v", got, readErr, closeErr)
	}
}

func TestS3ConditionalPublicationConflictClassification(t *testing.T) {
	conflict := minio.ErrorResponse{
		StatusCode: 409,
		Code:       "ConditionalRequestConflict",
		Message:    "A conflicting conditional operation is currently in progress",
	}
	if !isS3ConditionalPublicationConflict(conflict) {
		t.Fatalf("isS3ConditionalPublicationConflict(ConditionalRequestConflict) = false, want true")
	}
	if isS3ConditionalPublicationConflict(minio.ErrorResponse{StatusCode: 409, Code: "Conflict"}) {
		t.Fatal("isS3ConditionalPublicationConflict(generic 409) = true, want false")
	}
}

func TestS3DigestStateInspectionDoesNotExposePresignedURLOnTransportError(t *testing.T) {
	const accessKey = "sensitive-access-key"
	client, err := minio.New("s3.example.test", &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, "sensitive-secret-key", ""),
		Secure: true,
		Region: "us-east-1",
		Transport: s3BlobRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return nil, errors.New(request.URL.String())
		}),
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("minio.New() error = %v", err)
	}
	store, err := NewS3BlobStore(client, "houfeng-blobs")
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	err = store.ensureDigestPublicationState(context.Background(), "sha256/"+strings.Repeat("0", 64))
	if err == nil {
		t.Fatal("ensureDigestPublicationState() error = nil")
	}
	if strings.Contains(err.Error(), accessKey) || strings.Contains(err.Error(), "X-Amz-") {
		t.Fatalf("ensureDigestPublicationState() error exposes presigned credentials: %v", err)
	}
}

func TestS3BlobStoreGenericHead404RequiresEnabledBucket(t *testing.T) {
	versioningRequests := 0
	client, err := minio.New("s3.example.test", &minio.Options{
		Creds:        credentials.NewStaticV4("test-access", "test-secret", ""),
		Secure:       true,
		Region:       "us-east-1",
		BucketLookup: minio.BucketLookupPath,
		Transport: s3BlobRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method == http.MethodGet && request.URL.Query().Has("versioning") {
				versioningRequests++
				body := `<Error><Code>NoSuchBucket</Code><Message>missing bucket</Message></Error>`
				return &http.Response{
					StatusCode:    http.StatusNotFound,
					Header:        http.Header{"Content-Type": {"application/xml"}},
					Body:          io.NopCloser(strings.NewReader(body)),
					ContentLength: int64(len(body)),
					Request:       request,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       http.NoBody,
				Request:    request,
			}, nil
		}),
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("minio.New() error = %v", err)
	}
	store, err := NewS3BlobStore(client, "missing-blobs")
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	content := []byte("generic HEAD absence needs bucket proof")
	request := blobPutRequest(content)
	version := ObjectVersion{
		Key:       "sha256/" + hexDigest(request.ExpectedSHA256),
		VersionID: "missing-version",
		SHA256:    request.ExpectedSHA256,
		SizeBytes: request.ExpectedSizeBytes,
	}

	receipt, err := store.Delete(context.Background(), version)
	if err == nil {
		t.Fatalf("Delete(generic HEAD 404) = %#v, nil error; want backend error", receipt)
	}
	if !errors.Is(err, ErrInvalidBlobStoreConfig) {
		t.Fatalf("Delete(generic HEAD 404) error = %v, want ErrInvalidBlobStoreConfig", err)
	}
	if receipt.Deleted {
		t.Fatalf("Delete(generic HEAD 404) = %#v, want Deleted=false", receipt)
	}
	if versioningRequests == 0 {
		t.Fatal("Delete(generic HEAD 404) did not verify bucket versioning")
	}
}

func TestS3BlobStoreOperationsRevalidateBucketContract(t *testing.T) {
	content := []byte("bucket contract drift must fail before object access")
	request := blobPutRequest(content)
	version := ObjectVersion{
		Key:       "sha256/" + hexDigest(request.ExpectedSHA256),
		VersionID: "exact-version",
		SHA256:    request.ExpectedSHA256,
		SizeBytes: request.ExpectedSizeBytes,
	}
	operations := []struct {
		name string
		run  func(*S3BlobStore, ObjectVersion) error
	}{
		{
			name: "open",
			run: func(store *S3BlobStore, version ObjectVersion) error {
				reader, err := store.Open(context.Background(), version, FullByteRange())
				if reader != nil {
					_ = reader.Close()
				}
				return err
			},
		},
		{
			name: "stat",
			run: func(store *S3BlobStore, version ObjectVersion) error {
				_, err := store.Stat(context.Background(), version)
				return err
			},
		},
		{
			name: "delete",
			run: func(store *S3BlobStore, version ObjectVersion) error {
				_, err := store.Delete(context.Background(), version)
				return err
			},
		},
	}
	drifts := []struct {
		name           string
		versioningBody string
		objectLockBody string
	}{
		{
			name:           "versioning suspended",
			versioningBody: `<VersioningConfiguration><Status>Suspended</Status></VersioningConfiguration>`,
		},
		{
			name:           "Object Lock configured",
			versioningBody: `<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`,
			objectLockBody: `<ObjectLockConfiguration></ObjectLockConfiguration>`,
		},
	}

	for _, drift := range drifts {
		for _, operation := range operations {
			t.Run(drift.name+"/"+operation.name, func(t *testing.T) {
				objectRequests := 0
				client, err := minio.New("s3.example.test", &minio.Options{
					Creds:        credentials.NewStaticV4("test-access", "test-secret", ""),
					Secure:       true,
					Region:       "us-east-1",
					BucketLookup: minio.BucketLookupPath,
					Transport: s3BlobRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
						switch {
						case request.Method == http.MethodGet && request.URL.Query().Has("versioning"):
							return s3BlobTestXMLResponse(request, http.StatusOK, drift.versioningBody), nil
						case request.Method == http.MethodGet && request.URL.Query().Has("object-lock"):
							if drift.objectLockBody != "" {
								return s3BlobTestXMLResponse(request, http.StatusOK, drift.objectLockBody), nil
							}
							return s3BlobTestErrorResponse(request, http.StatusNotFound, "ObjectLockConfigurationNotFoundError"), nil
						default:
							objectRequests++
							return s3BlobTestErrorResponse(request, http.StatusInternalServerError, "InternalError"), nil
						}
					}),
					MaxRetries: 1,
				})
				if err != nil {
					t.Fatalf("minio.New() error = %v", err)
				}
				store, err := NewS3BlobStore(client, "houfeng-blobs")
				if err != nil {
					t.Fatalf("NewS3BlobStore() error = %v", err)
				}

				err = operation.run(store, version)
				if !errors.Is(err, ErrInvalidBlobStoreConfig) {
					t.Fatalf("%s() error = %v, want ErrInvalidBlobStoreConfig", operation.name, err)
				}
				if objectRequests != 0 {
					t.Fatalf("%s() object requests = %d, want 0", operation.name, objectRequests)
				}
			})
		}
	}
}

func TestNewBlobTemporaryKeyMatchesS3Contract(t *testing.T) {
	t.Parallel()

	key, err := NewBlobTemporaryKey()
	if err != nil {
		t.Fatalf("NewBlobTemporaryKey() error = %v", err)
	}
	if !validS3BlobTemporaryKey(key) {
		t.Fatalf("NewBlobTemporaryKey() = %q, want valid S3 temporary key", key)
	}
}

func TestS3BlobStoreRejectsInvalidTemporaryKeysBeforeBackendAccess(t *testing.T) {
	content := []byte("invalid caller-provided temporary key")
	tests := []struct {
		name string
		key  string
	}{
		{name: "absent"},
		{name: "unsafe path", key: "temporary/../../escape"},
		{name: "non-temporary digest key", key: "sha256/" + strings.Repeat("0", 64)},
		{name: "short entropy", key: "temporary/deadbeef"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backendRequests := 0
			client, err := minio.New("s3.example.test", &minio.Options{
				Creds:        credentials.NewStaticV4("test-access", "test-secret", ""),
				Secure:       true,
				Region:       "us-east-1",
				BucketLookup: minio.BucketLookupPath,
				Transport: s3BlobRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
					backendRequests++
					return s3BlobTestErrorResponse(request, http.StatusInternalServerError, "InternalError"), nil
				}),
				MaxRetries: 1,
			})
			if err != nil {
				t.Fatalf("minio.New() error = %v", err)
			}
			store, err := NewS3BlobStore(client, "houfeng-blobs")
			if err != nil {
				t.Fatalf("NewS3BlobStore() error = %v", err)
			}
			request := blobPutRequest(content)
			request.TemporaryKey = tt.key

			if _, err := store.Put(context.Background(), request, bytes.NewReader(content)); !errors.Is(err, ErrInvalidBlobRequest) {
				t.Fatalf("Put(%s temporary key) error = %v, want ErrInvalidBlobRequest", tt.name, err)
			}
			if backendRequests != 0 {
				t.Fatalf("Put(%s temporary key) backend requests = %d, want 0", tt.name, backendRequests)
			}
		})
	}
}

func TestS3BlobStoreUsesCallerProvidedTemporaryKey(t *testing.T) {
	const temporaryKey = s3BlobTemporaryPrefix + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const bucket = "houfeng-blobs"
	var putKey string
	client, err := minio.New("s3.example.test", &minio.Options{
		Creds:        credentials.NewStaticV4("test-access", "test-secret", ""),
		Secure:       true,
		Region:       "us-east-1",
		BucketLookup: minio.BucketLookupPath,
		Transport: s3BlobRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
			switch {
			case request.Method == http.MethodGet && request.URL.Query().Has("versioning"):
				body := `<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`
				return s3BlobTestXMLResponse(request, http.StatusOK, body), nil
			case request.Method == http.MethodGet && request.URL.Query().Has("object-lock"):
				return s3BlobTestErrorResponse(request, http.StatusNotFound, "ObjectLockConfigurationNotFoundError"), nil
			case request.Method == http.MethodPut:
				putKey = strings.TrimPrefix(request.URL.Path, "/"+bucket+"/")
				return s3BlobTestErrorResponse(request, http.StatusInternalServerError, "InternalError"), nil
			case request.Method == http.MethodHead:
				return s3BlobTestErrorResponse(request, http.StatusNotFound, minio.NoSuchKey), nil
			default:
				return nil, fmt.Errorf("unexpected S3 request: %s %s", request.Method, request.URL.Redacted())
			}
		}),
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("minio.New() error = %v", err)
	}
	store, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	content := []byte("caller-provided temporary key")
	request := blobPutRequest(content)
	request.TemporaryKey = temporaryKey

	if _, err := store.Put(context.Background(), request, bytes.NewReader(content)); err == nil {
		t.Fatal("Put() error = nil, want injected backend error")
	}
	if putKey != temporaryKey {
		t.Fatalf("Put() temporary key = %q, want %q", putKey, temporaryKey)
	}
}

func TestS3TemporaryObjectStoreResolvesKnownKeyAndDeletesExactVersion(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	store, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	key, err := newS3BlobTemporaryKey()
	if err != nil {
		t.Fatalf("newS3BlobTemporaryKey() error = %v", err)
	}
	content := []byte("persisted temporary object restart reconciliation")
	upload, err := client.PutObject(
		context.Background(),
		bucket,
		key,
		bytes.NewReader(content),
		int64(len(content)),
		minio.PutObjectOptions{ContentType: "application/octet-stream"},
	)
	if err != nil {
		t.Fatalf("PutObject(temporary) error = %v", err)
	}

	resolved, err := store.ResolveTemporaryVersion(context.Background(), key)
	if err != nil {
		t.Fatalf("ResolveTemporaryVersion() error = %v", err)
	}
	want := TemporaryObjectVersion{Key: key, VersionID: upload.VersionID}
	if resolved != want {
		t.Fatalf("ResolveTemporaryVersion() = %#v, want %#v", resolved, want)
	}
	if err := store.DeleteTemporaryVersion(context.Background(), resolved); err != nil {
		t.Fatalf("DeleteTemporaryVersion() error = %v", err)
	}
	assertNoS3TemporaryVersions(t, client, bucket)
	if err := store.DeleteTemporaryVersion(context.Background(), resolved); err != nil {
		t.Fatalf("DeleteTemporaryVersion(replay) error = %v", err)
	}
}

func TestS3TemporaryObjectStoreReadsAndPublishesExactVersionWithoutDeletingSource(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	store, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	key, err := newS3BlobTemporaryKey()
	if err != nil {
		t.Fatalf("newS3BlobTemporaryKey() error = %v", err)
	}
	content := []byte("direct upload exact temporary publication")
	upload, err := client.PutObject(
		context.Background(), bucket, key, bytes.NewReader(content), int64(len(content)),
		minio.PutObjectOptions{ContentType: "application/octet-stream"},
	)
	if err != nil {
		t.Fatalf("PutObject(temporary) error = %v", err)
	}
	temporary := TemporaryObjectVersion{Key: key, VersionID: upload.VersionID}
	reader, err := store.OpenTemporaryVersion(context.Background(), TemporaryObjectReadRequest{
		Version: temporary, ExpectedSizeBytes: int64(len(content)),
	})
	if err != nil {
		t.Fatalf("OpenTemporaryVersion() error = %v", err)
	}
	read, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(read, content) {
		t.Fatalf("OpenTemporaryVersion() = (%q, %v, %v)", read, readErr, closeErr)
	}
	digest := sha256.Sum256(content)
	published, err := store.PublishTemporaryVersion(context.Background(), TemporaryObjectPublishRequest{
		Version: temporary, ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content)),
	})
	if err != nil {
		t.Fatalf("PublishTemporaryVersion() error = %v", err)
	}
	if published.Key != "sha256/"+hexDigest(digest) || published.SHA256 != digest ||
		published.SizeBytes != int64(len(content)) {
		t.Fatalf("PublishTemporaryVersion() = %#v", published)
	}
	current, err := store.ResolveTemporaryVersion(context.Background(), key)
	if err != nil || current != temporary {
		t.Fatalf("ResolveTemporaryVersion(after publish) = (%#v, %v), want %#v", current, err, temporary)
	}
}

func TestS3TemporaryObjectStoreRejectsReplacedVersionBeforeReadOrPublish(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	store, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	key, err := newS3BlobTemporaryKey()
	if err != nil {
		t.Fatalf("newS3BlobTemporaryKey() error = %v", err)
	}
	put := func(content []byte) minio.UploadInfo {
		t.Helper()
		upload, putErr := client.PutObject(
			context.Background(), bucket, key, bytes.NewReader(content), int64(len(content)),
			minio.PutObjectOptions{ContentType: "application/octet-stream"},
		)
		if putErr != nil {
			t.Fatalf("PutObject(temporary) error = %v", putErr)
		}
		return upload
	}
	staleContent := []byte("stale direct upload")
	stale := put(staleContent)
	_ = put([]byte("replacement direct upload"))
	version := TemporaryObjectVersion{Key: key, VersionID: stale.VersionID}
	if _, err := store.OpenTemporaryVersion(context.Background(), TemporaryObjectReadRequest{
		Version: version, ExpectedSizeBytes: int64(len(staleContent)),
	}); !errors.Is(err, ErrBlobVersionMismatch) {
		t.Fatalf("OpenTemporaryVersion(stale) error = %v, want ErrBlobVersionMismatch", err)
	}
	if _, err := store.PublishTemporaryVersion(context.Background(), TemporaryObjectPublishRequest{
		Version: version, ExpectedSHA256: sha256.Sum256(staleContent), ExpectedSizeBytes: int64(len(staleContent)),
	}); !errors.Is(err, ErrBlobVersionMismatch) {
		t.Fatalf("PublishTemporaryVersion(stale) error = %v, want ErrBlobVersionMismatch", err)
	}
}

func TestS3TemporaryObjectStoreRejectsMissingExactVersionBeforeBackendAccess(t *testing.T) {
	backendRequests := 0
	client, err := minio.New("s3.example.test", &minio.Options{
		Creds:        credentials.NewStaticV4("test-access", "test-secret", ""),
		Secure:       true,
		Region:       "us-east-1",
		BucketLookup: minio.BucketLookupPath,
		Transport: s3BlobRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
			backendRequests++
			return s3BlobTestErrorResponse(request, http.StatusInternalServerError, "InternalError"), nil
		}),
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("minio.New() error = %v", err)
	}
	store, err := NewS3BlobStore(client, "houfeng-blobs")
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	key, err := newS3BlobTemporaryKey()
	if err != nil {
		t.Fatalf("newS3BlobTemporaryKey() error = %v", err)
	}

	err = store.DeleteTemporaryVersion(context.Background(), TemporaryObjectVersion{Key: key})
	if !errors.Is(err, ErrInvalidBlobRequest) {
		t.Fatalf("DeleteTemporaryVersion(missing version) error = %v, want ErrInvalidBlobRequest", err)
	}
	if backendRequests != 0 {
		t.Fatalf("DeleteTemporaryVersion(missing version) backend requests = %d, want 0", backendRequests)
	}
}

func TestS3TemporaryObjectStoreRejectsStaleVersionWithoutDeletingIt(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	store, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	key, err := newS3BlobTemporaryKey()
	if err != nil {
		t.Fatalf("newS3BlobTemporaryKey() error = %v", err)
	}
	putTemporary := func(content string) minio.UploadInfo {
		t.Helper()
		upload, putErr := client.PutObject(
			context.Background(),
			bucket,
			key,
			strings.NewReader(content),
			int64(len(content)),
			minio.PutObjectOptions{ContentType: "application/octet-stream"},
		)
		if putErr != nil {
			t.Fatalf("PutObject(temporary) error = %v", putErr)
		}
		return upload
	}
	stale := putTemporary("stale temporary version")
	current := putTemporary("replacement temporary version")

	err = store.DeleteTemporaryVersion(context.Background(), TemporaryObjectVersion{
		Key:       key,
		VersionID: stale.VersionID,
	})
	if !errors.Is(err, ErrBlobVersionMismatch) {
		t.Fatalf("DeleteTemporaryVersion(stale) error = %v, want ErrBlobVersionMismatch", err)
	}
	for name, versionID := range map[string]string{"stale": stale.VersionID, "current": current.VersionID} {
		info, statErr := client.StatObject(
			context.Background(),
			bucket,
			key,
			minio.StatObjectOptions{VersionID: versionID},
		)
		if statErr != nil {
			t.Fatalf("StatObject(%s version) error = %v", name, statErr)
		}
		if info.VersionID != versionID || info.IsDeleteMarker {
			t.Fatalf("StatObject(%s version) = version %q delete_marker=%t", name, info.VersionID, info.IsDeleteMarker)
		}
	}
}

func TestS3TemporaryObjectStoreRejectsStaleVersionBeforeDeleteRequest(t *testing.T) {
	const (
		bucket       = "houfeng-blobs"
		persisted    = "persisted-version"
		replacement  = "replacement-version"
		temporaryKey = s3BlobTemporaryPrefix + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	)
	deleteRequests := 0
	client, err := minio.New("s3.example.test", &minio.Options{
		Creds:        credentials.NewStaticV4("test-access", "test-secret", ""),
		Secure:       true,
		Region:       "us-east-1",
		BucketLookup: minio.BucketLookupPath,
		Transport: s3BlobRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
			switch {
			case request.Method == http.MethodGet && request.URL.Query().Has("versioning"):
				body := `<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`
				return s3BlobTestXMLResponse(request, http.StatusOK, body), nil
			case request.Method == http.MethodGet && request.URL.Query().Has("object-lock"):
				return s3BlobTestErrorResponse(request, http.StatusNotFound, "ObjectLockConfigurationNotFoundError"), nil
			case request.Method == http.MethodDelete:
				deleteRequests++
				return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
			case request.Method == http.MethodHead && request.URL.Query().Has("versionId"):
				return s3BlobTestErrorResponse(request, http.StatusNotFound, minio.NoSuchVersion), nil
			case request.Method == http.MethodHead:
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Length":   {"1"},
						"ETag":             {`"replacement"`},
						"Last-Modified":    {"Tue, 04 Aug 2026 00:00:00 GMT"},
						"X-Amz-Version-Id": {replacement},
					},
					Body:    http.NoBody,
					Request: request,
				}, nil
			default:
				return nil, fmt.Errorf("unexpected S3 request: %s %s", request.Method, request.URL.Redacted())
			}
		}),
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("minio.New() error = %v", err)
	}
	store, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}

	err = store.DeleteTemporaryVersion(context.Background(), TemporaryObjectVersion{
		Key:       temporaryKey,
		VersionID: persisted,
	})
	if !errors.Is(err, ErrBlobVersionMismatch) {
		t.Fatalf("DeleteTemporaryVersion(stale) error = %v, want ErrBlobVersionMismatch", err)
	}
	if deleteRequests != 0 {
		t.Fatalf("DeleteTemporaryVersion(stale) DELETE requests = %d, want 0", deleteRequests)
	}
}

func TestS3TemporaryObjectStoreRejectsCurrentDeleteMarkerWithoutDeletingPersistedVersion(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	store, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	key, err := newS3BlobTemporaryKey()
	if err != nil {
		t.Fatalf("newS3BlobTemporaryKey() error = %v", err)
	}
	content := []byte("temporary version hidden by current delete marker")
	upload, err := client.PutObject(
		context.Background(),
		bucket,
		key,
		bytes.NewReader(content),
		int64(len(content)),
		minio.PutObjectOptions{ContentType: "application/octet-stream"},
	)
	if err != nil {
		t.Fatalf("PutObject(temporary) error = %v", err)
	}
	if err := client.RemoveObject(context.Background(), bucket, key, minio.RemoveObjectOptions{}); err != nil {
		t.Fatalf("RemoveObject(create delete marker) error = %v", err)
	}

	err = store.DeleteTemporaryVersion(context.Background(), TemporaryObjectVersion{
		Key:       key,
		VersionID: upload.VersionID,
	})
	if !errors.Is(err, ErrBlobVersionMismatch) {
		t.Fatalf("DeleteTemporaryVersion(delete marker current) error = %v, want ErrBlobVersionMismatch", err)
	}
	info, err := client.StatObject(
		context.Background(),
		bucket,
		key,
		minio.StatObjectOptions{VersionID: upload.VersionID},
	)
	if err != nil {
		t.Fatalf("StatObject(persisted version after rejected cleanup) error = %v", err)
	}
	if info.VersionID != upload.VersionID || info.IsDeleteMarker {
		t.Fatalf("persisted version after rejected cleanup = version %q delete_marker=%t", info.VersionID, info.IsDeleteMarker)
	}
}

func TestS3BlobStoreConformance(t *testing.T) {
	requireMinIOIntegration(t)
	runBlobStoreConformance(t, func(t *testing.T) BlobStore {
		client, bucket := newMinIOBlobFixture(t)
		store, err := NewS3BlobStore(client, bucket)
		if err != nil {
			t.Fatalf("NewS3BlobStore() error = %v", err)
		}
		return store
	})
}

func TestS3BlobStoreAlwaysRemovesTemporaryVersions(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	store, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	content := []byte("temporary cleanup bytes")
	request := blobPutRequest(content)

	if _, err := store.Put(context.Background(), request, bytes.NewReader(content[:5])); !errors.Is(err, ErrBlobSizeMismatch) {
		t.Fatalf("Put(short) error = %v, want ErrBlobSizeMismatch", err)
	}
	assertNoS3TemporaryVersions(t, client, bucket)

	if _, err := store.Put(context.Background(), request, bytes.NewReader(append(append([]byte{}, content...), '!'))); !errors.Is(err, ErrBlobSizeMismatch) {
		t.Fatalf("Put(oversized) error = %v, want ErrBlobSizeMismatch", err)
	}
	assertNoS3TemporaryVersions(t, client, bucket)

	wantDifferentDigest := request
	wantDifferentDigest.ExpectedSHA256 = blobPutRequest([]byte("different content")).ExpectedSHA256
	if _, err := store.Put(context.Background(), wantDifferentDigest, bytes.NewReader(content)); !errors.Is(err, ErrBlobHashMismatch) {
		t.Fatalf("Put(hash mismatch) error = %v, want ErrBlobHashMismatch", err)
	}
	assertNoS3TemporaryVersions(t, client, bucket)

	readerError := errors.New("injected S3 Blob reader error")
	if _, err := store.Put(context.Background(), request, &failingBlobReader{
		content: content[:5],
		err:     readerError,
	}); !errors.Is(err, readerError) {
		t.Fatalf("Put(interrupted) error = %v, want injected error", err)
	}
	assertNoS3TemporaryVersions(t, client, bucket)

	version, err := store.Put(context.Background(), request, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put(success) error = %v", err)
	}
	assertNoS3TemporaryVersions(t, client, bucket)

	deduplicated, err := store.Put(context.Background(), request, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put(dedupe) error = %v", err)
	}
	if deduplicated != version {
		t.Fatalf("Put(dedupe) = %#v, want %#v", deduplicated, version)
	}
	assertNoS3TemporaryVersions(t, client, bucket)
}

func TestS3BlobStorePublishedVersionSurvivesTransientVerificationFailure(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	dedupeStore, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore(dedupe) error = %v", err)
	}
	content := []byte("published bytes survive transient verification failure")
	putRequest := blobPutRequest(content)
	finalKey := "sha256/" + hexDigest(putRequest.ExpectedSHA256)
	transientErr := errors.New("injected post-publication verification failure")

	var mutex sync.Mutex
	finalPublished := false
	var deduplicated ObjectVersion
	var dedupeErr error
	var injectOnce sync.Once
	transport := s3BlobRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		mutex.Lock()
		published := finalPublished
		mutex.Unlock()
		if published && request.Method == http.MethodHead && request.URL.Path == "/"+bucket+"/"+finalKey &&
			request.URL.Query().Get("versionId") == "" {
			injectOnce.Do(func() {
				deduplicated, dedupeErr = dedupeStore.Put(context.Background(), putRequest, bytes.NewReader(content))
			})
			return nil, transientErr
		}

		response, err := http.DefaultTransport.RoundTrip(request)
		if err == nil && request.Method == http.MethodPut && request.URL.Path == "/"+bucket+"/"+finalKey &&
			response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			mutex.Lock()
			finalPublished = true
			mutex.Unlock()
		}
		return response, err
	})
	faultClient := newMinIOBlobClientWithTransport(t, transport)
	faultStore, err := NewS3BlobStore(faultClient, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore(fault) error = %v", err)
	}

	if _, err := faultStore.Put(context.Background(), putRequest, bytes.NewReader(content)); !errors.Is(err, transientErr) {
		t.Fatalf("Put(transient verification failure) error = %v, want injected error", err)
	}
	if dedupeErr != nil {
		t.Fatalf("concurrent dedupe Put() error = %v", dedupeErr)
	}
	if deduplicated == (ObjectVersion{}) {
		t.Fatal("concurrent dedupe Put() returned zero version")
	}
	if _, err := dedupeStore.Stat(context.Background(), deduplicated); err != nil {
		t.Fatalf("Stat(deduplicated version after publisher failure) error = %v", err)
	}
	assertNoS3TemporaryVersions(t, client, bucket)
}

func TestS3BlobStoreRejectsNoncurrentAndDeleteMarkerIdentities(t *testing.T) {
	requireMinIOIntegration(t)

	t.Run("noncurrent version", func(t *testing.T) {
		client, bucket := newMinIOBlobFixture(t)
		store, err := NewS3BlobStore(client, bucket)
		if err != nil {
			t.Fatalf("NewS3BlobStore() error = %v", err)
		}
		content := []byte("noncurrent exact version")
		version, err := store.Put(context.Background(), blobPutRequest(content), bytes.NewReader(content))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}
		replacement := []byte("unauthorized replacement")
		if _, err := client.PutObject(
			context.Background(),
			bucket,
			version.Key,
			bytes.NewReader(replacement),
			int64(len(replacement)),
			minio.PutObjectOptions{DisableMultipart: true},
		); err != nil {
			t.Fatalf("PutObject(replacement) error = %v", err)
		}
		assertS3VersionIdentityRejected(t, store, version)
	})

	t.Run("current delete marker", func(t *testing.T) {
		client, bucket := newMinIOBlobFixture(t)
		store, err := NewS3BlobStore(client, bucket)
		if err != nil {
			t.Fatalf("NewS3BlobStore() error = %v", err)
		}
		content := []byte("delete marker exact version")
		version, err := store.Put(context.Background(), blobPutRequest(content), bytes.NewReader(content))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}
		if err := client.RemoveObject(context.Background(), bucket, version.Key, minio.RemoveObjectOptions{}); err != nil {
			t.Fatalf("RemoveObject(delete marker) error = %v", err)
		}
		assertS3VersionIdentityRejected(t, store, version)
	})
}

func TestS3BlobStorePutDoesNotReviveCurrentDeleteMarker(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	store, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	content := []byte("current delete marker remains authoritative")
	request := blobPutRequest(content)
	version, err := store.Put(context.Background(), request, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put(first) error = %v", err)
	}
	if err := client.RemoveObject(context.Background(), bucket, version.Key, minio.RemoveObjectOptions{}); err != nil {
		t.Fatalf("RemoveObject(delete marker) error = %v", err)
	}
	before := s3VersionsForKey(t, client, bucket, version.Key)

	if _, err := store.Put(context.Background(), request, bytes.NewReader(content)); !errors.Is(err, ErrBlobConflict) {
		t.Fatalf("Put(over delete marker) error = %v, want ErrBlobConflict", err)
	}
	after := s3VersionsForKey(t, client, bucket, version.Key)
	if len(after) != len(before) {
		t.Fatalf("Put(over delete marker) version count = %d, want %d", len(after), len(before))
	}
	if len(after) == 0 || !after[0].IsDeleteMarker {
		t.Fatalf("Put(over delete marker) latest version = %#v, want delete marker", after)
	}
	assertNoS3TemporaryVersions(t, client, bucket)
}

func TestS3BlobStoreDeleteMissingBucketFailsClosed(t *testing.T) {
	requireMinIOIntegration(t)
	client, _ := newMinIOBlobFixture(t)
	missingBucket := minIOFixtureBucketName(t, "houfeng-missing")
	store, err := NewS3BlobStore(client, missingBucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	content := []byte("missing backend must not look deleted")
	request := blobPutRequest(content)
	version := ObjectVersion{
		Key:       "sha256/" + hexDigest(request.ExpectedSHA256),
		VersionID: "missing-bucket-version",
		SHA256:    request.ExpectedSHA256,
		SizeBytes: request.ExpectedSizeBytes,
	}
	receipt, err := store.Delete(context.Background(), version)
	if err == nil {
		t.Fatalf("Delete(missing bucket) = %#v, nil error; want backend error", receipt)
	}
	if receipt.Deleted {
		t.Fatalf("Delete(missing bucket) = %#v, want Deleted=false", receipt)
	}
	if errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("Delete(missing bucket) error = %v, must not map backend absence to ErrBlobNotFound", err)
	}
}

func TestS3BlobStoreConditional409ConvergesOnlyToVerifiedDigest(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	store, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	content := []byte("verified conditional conflict digest")
	request := blobPutRequest(content)
	existing, err := store.Put(context.Background(), request, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put(existing) error = %v", err)
	}
	conflict := minio.ErrorResponse{
		StatusCode: 409,
		Code:       "ConditionalRequestConflict",
		Message:    "A conflicting conditional operation is currently in progress",
	}

	resolved, err := store.resolveConditionalPublicationFailure(context.Background(), existing.Key, request, conflict)
	if err != nil {
		t.Fatalf("resolveConditionalPublicationFailure(existing) error = %v", err)
	}
	if resolved != existing {
		t.Fatalf("resolveConditionalPublicationFailure(existing) = %#v, want %#v", resolved, existing)
	}

	absentContent := []byte("absent conditional conflict digest")
	absentRequest := blobPutRequest(absentContent)
	absentKey := "sha256/" + hexDigest(absentRequest.ExpectedSHA256)
	resolved, err = store.resolveConditionalPublicationFailure(context.Background(), absentKey, absentRequest, conflict)
	if err == nil {
		t.Fatalf("resolveConditionalPublicationFailure(absent) = %#v, nil error", resolved)
	}
	if resolved != (ObjectVersion{}) {
		t.Fatalf("resolveConditionalPublicationFailure(absent) = %#v, want zero version", resolved)
	}
}

func TestS3BlobStoreConditional409RetriesWithFreshTemporaryReader(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	content := []byte("conditional conflict retry reopens exact temporary bytes")
	putRequest := blobPutRequest(content)
	finalKey := "sha256/" + hexDigest(putRequest.ExpectedSHA256)
	conditionalPuts := 0
	transport := s3BlobRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPut && request.URL.Path == "/"+bucket+"/"+finalKey &&
			request.Header.Get("If-None-Match") == "*" {
			conditionalPuts++
			if conditionalPuts == 1 {
				if _, err := io.Copy(io.Discard, request.Body); err != nil {
					return nil, err
				}
				_ = request.Body.Close()
				return s3BlobTestErrorResponse(request, http.StatusConflict, "ConditionalRequestConflict"), nil
			}
		}
		return http.DefaultTransport.RoundTrip(request)
	})
	store, err := NewS3BlobStore(newMinIOBlobClientWithTransport(t, transport), bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}

	version, err := store.Put(context.Background(), putRequest, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put(409 then retry) error = %v", err)
	}
	if conditionalPuts != 2 {
		t.Fatalf("Put(409 then retry) conditional PUT count = %d, want 2", conditionalPuts)
	}
	if _, err := store.Stat(context.Background(), version); err != nil {
		t.Fatalf("Stat(409 retry version) error = %v", err)
	}
	assertNoS3TemporaryVersions(t, client, bucket)
}

func TestS3BlobStoreConditional409ConvergenceHonorsCancellation(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	content := []byte("conditional conflict bounded wait cancellation")
	putRequest := blobPutRequest(content)
	finalKey := "sha256/" + hexDigest(putRequest.ExpectedSHA256)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conditionalPuts := 0
	var scheduleCancellation sync.Once
	transport := s3BlobRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPut && request.URL.Path == "/"+bucket+"/"+finalKey &&
			request.Header.Get("If-None-Match") == "*" {
			conditionalPuts++
			if _, err := io.Copy(io.Discard, request.Body); err != nil {
				return nil, err
			}
			_ = request.Body.Close()
			return s3BlobTestErrorResponse(request, http.StatusConflict, "ConditionalRequestConflict"), nil
		}
		response, err := http.DefaultTransport.RoundTrip(request)
		if err == nil && conditionalPuts > 0 && request.Method == http.MethodHead &&
			request.URL.Path == "/"+bucket+"/"+finalKey && request.URL.Query().Get("versionId") == "" {
			scheduleCancellation.Do(func() {
				time.AfterFunc(25*time.Millisecond, cancel)
			})
		}
		return response, err
	})
	store, err := NewS3BlobStore(newMinIOBlobClientWithTransport(t, transport), bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}

	started := time.Now()
	if _, err := store.Put(ctx, putRequest, bytes.NewReader(content)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Put(cancel bounded 409 convergence) error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Put(cancel bounded 409 convergence) elapsed = %v, want <= 1s", elapsed)
	}
	if conditionalPuts != 1 {
		t.Fatalf("Put(cancel bounded 409 convergence) conditional PUT count = %d, want 1", conditionalPuts)
	}
	assertNoS3TemporaryVersions(t, client, bucket)
}

func TestS3BlobStoreConditional409BoundedFailureIsConflict(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	content := []byte("conditional conflict bounded failure")
	putRequest := blobPutRequest(content)
	finalKey := "sha256/" + hexDigest(putRequest.ExpectedSHA256)
	conditionalPuts := 0
	transport := s3BlobRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPut && request.URL.Path == "/"+bucket+"/"+finalKey &&
			request.Header.Get("If-None-Match") == "*" {
			conditionalPuts++
			if _, err := io.Copy(io.Discard, request.Body); err != nil {
				return nil, err
			}
			_ = request.Body.Close()
			return s3BlobTestErrorResponse(request, http.StatusConflict, "ConditionalRequestConflict"), nil
		}
		return http.DefaultTransport.RoundTrip(request)
	})
	store, err := NewS3BlobStore(newMinIOBlobClientWithTransport(t, transport), bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}

	_, err = store.Put(context.Background(), putRequest, bytes.NewReader(content))
	if !errors.Is(err, ErrBlobConflict) {
		t.Fatalf("Put(exhausted 409 convergence) error = %v, want ErrBlobConflict", err)
	}
	if errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("Put(exhausted 409 convergence) error = %v, must not expose ErrBlobNotFound", err)
	}
	const wantConditionalPuts = 4
	if conditionalPuts != wantConditionalPuts {
		t.Fatalf("Put(exhausted 409 convergence) conditional PUT count = %d, want %d", conditionalPuts, wantConditionalPuts)
	}
	assertNoS3TemporaryVersions(t, client, bucket)
}

func TestS3BlobStoreConditional409RetrySourceMissingIsConflict(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	content := []byte("conditional conflict missing retry source")
	putRequest := blobPutRequest(content)
	finalKey := "sha256/" + hexDigest(putRequest.ExpectedSHA256)
	conditionalPuts := 0
	conflictSeen := false
	transport := s3BlobRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPut && request.URL.Path == "/"+bucket+"/"+finalKey &&
			request.Header.Get("If-None-Match") == "*" {
			conditionalPuts++
			conflictSeen = true
			if _, err := io.Copy(io.Discard, request.Body); err != nil {
				return nil, err
			}
			_ = request.Body.Close()
			return s3BlobTestErrorResponse(request, http.StatusConflict, "ConditionalRequestConflict"), nil
		}
		if conflictSeen && request.Method == http.MethodGet &&
			strings.HasPrefix(request.URL.Path, "/"+bucket+"/"+s3BlobTemporaryPrefix) &&
			request.URL.Query().Get("versionId") != "" {
			return s3BlobTestErrorResponse(request, http.StatusNotFound, minio.NoSuchVersion), nil
		}
		return http.DefaultTransport.RoundTrip(request)
	})
	store, err := NewS3BlobStore(newMinIOBlobClientWithTransport(t, transport), bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}

	_, err = store.Put(context.Background(), putRequest, bytes.NewReader(content))
	if !errors.Is(err, ErrBlobConflict) {
		t.Fatalf("Put(409 then missing retry source) error = %v, want ErrBlobConflict", err)
	}
	if errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("Put(409 then missing retry source) error = %v, must not expose ErrBlobNotFound", err)
	}
	if conditionalPuts != 1 {
		t.Fatalf("Put(409 then missing retry source) conditional PUT count = %d, want 1", conditionalPuts)
	}
	assertNoS3TemporaryVersions(t, client, bucket)
}

func TestS3BlobStoreRemovesExactNullTemporaryVersion(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	store, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	if err := client.SuspendVersioning(context.Background(), bucket); err != nil {
		t.Fatalf("SuspendVersioning() error = %v", err)
	}
	key := s3BlobTemporaryPrefix + "null-version-cleanup"
	content := []byte("exact null temporary version")
	upload, err := client.PutObject(
		context.Background(),
		bucket,
		key,
		bytes.NewReader(content),
		int64(len(content)),
		minio.PutObjectOptions{DisableMultipart: true},
	)
	if err != nil {
		t.Fatalf("PutObject(null version) error = %v", err)
	}
	if upload.VersionID != "" {
		t.Fatalf("PutObject(suspended versioning) VersionID = %q, want empty SDK response", upload.VersionID)
	}
	versions := s3VersionsForKey(t, client, bucket, key)
	if len(versions) != 1 || versions[0].VersionID != "null" {
		t.Fatalf("PutObject(suspended versioning) versions = %#v, want one null version", versions)
	}
	if validS3BlobVersionID(versions[0].VersionID) {
		t.Fatal("validS3BlobVersionID(null) = true, want false")
	}
	if err := client.EnableVersioning(context.Background(), bucket); err != nil {
		t.Fatalf("EnableVersioning() before cleanup error = %v", err)
	}

	if err := store.removeTemporary(context.Background(), key, versions[0].VersionID); err != nil {
		t.Fatalf("removeTemporary(null version) error = %v", err)
	}
	assertNoS3TemporaryVersions(t, client, bucket)
}

func TestS3BlobStoreTemporaryCleanupNeverResolvesOrDeletesWithoutExactVersion(t *testing.T) {
	currentStatRequests := 0
	deleteRequests := 0
	client, err := minio.New("s3.example.test", &minio.Options{
		Creds:        credentials.NewStaticV4("test-access", "test-secret", ""),
		Secure:       true,
		Region:       "us-east-1",
		BucketLookup: minio.BucketLookupPath,
		Transport: s3BlobRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
			switch {
			case request.Method == http.MethodGet && request.URL.Query().Has("versioning"):
				body := `<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`
				return s3BlobTestXMLResponse(request, http.StatusOK, body), nil
			case request.Method == http.MethodHead && request.URL.Query().Has("versionId"):
				return s3BlobTestErrorResponse(request, http.StatusNotFound, minio.NoSuchVersion), nil
			case request.Method == http.MethodHead:
				currentStatRequests++
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Length":   {"11"},
						"ETag":             {`"replacement"`},
						"Last-Modified":    {"Tue, 04 Aug 2026 00:00:00 GMT"},
						"X-Amz-Version-Id": {"replacement-version"},
					},
					Body:    http.NoBody,
					Request: request,
				}, nil
			case request.Method == http.MethodDelete:
				deleteRequests++
				return &http.Response{
					StatusCode: http.StatusNoContent,
					Header:     make(http.Header),
					Body:       http.NoBody,
					Request:    request,
				}, nil
			default:
				return nil, fmt.Errorf("unexpected S3 request: %s %s", request.Method, request.URL.Redacted())
			}
		}),
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("minio.New() error = %v", err)
	}
	store, err := NewS3BlobStore(client, "houfeng-blobs")
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}

	err = store.removeTemporary(
		context.Background(),
		s3BlobTemporaryPrefix+strings.Repeat("0", sha256.Size*2),
		"",
	)
	if !errors.Is(err, ErrInvalidBlobStoreConfig) {
		t.Fatalf("removeTemporary(missing exact version) error = %v, want ErrInvalidBlobStoreConfig", err)
	}
	if currentStatRequests != 0 {
		t.Fatalf("removeTemporary(missing exact version) current Stat requests = %d, want 0", currentStatRequests)
	}
	if deleteRequests != 0 {
		t.Fatalf("removeTemporary(missing exact version) DELETE requests = %d, want 0", deleteRequests)
	}
}

func TestS3BlobStorePutUnknownTemporaryVersionLeavesCurrentObjectForRecovery(t *testing.T) {
	const bucket = "houfeng-blobs"
	currentStatRequests := 0
	deleteRequests := 0
	client, err := minio.New("s3.example.test", &minio.Options{
		Creds:        credentials.NewStaticV4("test-access", "test-secret", ""),
		Secure:       true,
		Region:       "us-east-1",
		BucketLookup: minio.BucketLookupPath,
		Transport: s3BlobRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
			switch {
			case request.Method == http.MethodGet && request.URL.Query().Has("versioning"):
				body := `<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`
				return s3BlobTestXMLResponse(request, http.StatusOK, body), nil
			case request.Method == http.MethodGet && request.URL.Query().Has("object-lock"):
				return s3BlobTestErrorResponse(request, http.StatusNotFound, "ObjectLockConfigurationNotFoundError"), nil
			case request.Method == http.MethodPut:
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"ETag": {`"committed-with-unknown-version"`}},
					Body:       http.NoBody,
					Request:    request,
				}, nil
			case request.Method == http.MethodHead && request.URL.Query().Has("versionId"):
				return s3BlobTestErrorResponse(request, http.StatusNotFound, minio.NoSuchVersion), nil
			case request.Method == http.MethodHead:
				currentStatRequests++
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Length":   {"11"},
						"ETag":             {`"replacement"`},
						"Last-Modified":    {"Tue, 04 Aug 2026 00:00:00 GMT"},
						"X-Amz-Version-Id": {"replacement-version"},
					},
					Body:    http.NoBody,
					Request: request,
				}, nil
			case request.Method == http.MethodDelete:
				deleteRequests++
				return &http.Response{
					StatusCode: http.StatusNoContent,
					Header:     make(http.Header),
					Body:       http.NoBody,
					Request:    request,
				}, nil
			default:
				return nil, fmt.Errorf("unexpected S3 request: %s %s", request.Method, request.URL.Redacted())
			}
		}),
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("minio.New() error = %v", err)
	}
	store, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	content := []byte("unknown temporary version")
	request := blobPutRequest(content)
	request.TemporaryKey = s3BlobTemporaryPrefix + strings.Repeat("1", sha256.Size*2)

	if _, err := store.Put(context.Background(), request, bytes.NewReader(content)); !errors.Is(err, ErrInvalidBlobStoreConfig) {
		t.Fatalf("Put(unknown temporary version) error = %v, want ErrInvalidBlobStoreConfig", err)
	}
	if currentStatRequests != 0 {
		t.Fatalf("Put(unknown temporary version) current Stat requests = %d, want 0", currentStatRequests)
	}
	if deleteRequests != 0 {
		t.Fatalf("Put(unknown temporary version) DELETE requests = %d, want 0", deleteRequests)
	}
}

func TestS3BlobStoreRejectsObjectLockBeforeTemporaryUpload(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixtureWithObjectLock(t)
	store, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	content := []byte("must not enter retained temporary storage")
	if _, err := store.Put(context.Background(), blobPutRequest(content), bytes.NewReader(content)); !errors.Is(err, ErrInvalidBlobStoreConfig) {
		t.Fatalf("Put(Object Lock bucket) error = %v, want ErrInvalidBlobStoreConfig", err)
	}
	assertNoS3TemporaryVersions(t, client, bucket)
}

func TestS3BlobStoreRejectsAnySuccessfulObjectLockConfiguration(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty", body: `<ObjectLockConfiguration></ObjectLockConfiguration>`},
		{name: "unknown state", body: `<ObjectLockConfiguration><ObjectLockEnabled>Future</ObjectLockEnabled></ObjectLockConfiguration>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			putRequests := 0
			client, err := minio.New("s3.example.test", &minio.Options{
				Creds:        credentials.NewStaticV4("test-access", "test-secret", ""),
				Secure:       true,
				Region:       "us-east-1",
				BucketLookup: minio.BucketLookupPath,
				Transport: s3BlobRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
					switch {
					case request.Method == http.MethodGet && request.URL.Query().Has("versioning"):
						body := `<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`
						return s3BlobTestXMLResponse(request, http.StatusOK, body), nil
					case request.Method == http.MethodGet && request.URL.Query().Has("object-lock"):
						return s3BlobTestXMLResponse(request, http.StatusOK, tt.body), nil
					case request.Method == http.MethodPut:
						putRequests++
						return s3BlobTestErrorResponse(request, http.StatusInternalServerError, "InternalError"), nil
					default:
						return nil, fmt.Errorf("unexpected S3 request: %s %s", request.Method, request.URL.Redacted())
					}
				}),
				MaxRetries: 1,
			})
			if err != nil {
				t.Fatalf("minio.New() error = %v", err)
			}
			store, err := NewS3BlobStore(client, "houfeng-blobs")
			if err != nil {
				t.Fatalf("NewS3BlobStore() error = %v", err)
			}
			content := []byte("must reject any existing Object Lock configuration")

			if _, err := store.Put(context.Background(), blobPutRequest(content), bytes.NewReader(content)); !errors.Is(err, ErrInvalidBlobStoreConfig) {
				t.Fatalf("Put(successful Object Lock %s) error = %v, want ErrInvalidBlobStoreConfig", tt.name, err)
			}
			if putRequests != 0 {
				t.Fatalf("Put(successful Object Lock %s) object PUT requests = %d, want 0", tt.name, putRequests)
			}
		})
	}
}

func TestS3BlobStoreConcurrentPublicationCreatesOneDigestVersion(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	store, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	content := []byte("one immutable S3 digest version")
	request := blobPutRequest(content)

	const writers = 8
	versions := make(chan ObjectVersion, writers)
	errorsCh := make(chan error, writers)
	var waitGroup sync.WaitGroup
	for range writers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			version, err := store.Put(context.Background(), request, bytes.NewReader(content))
			versions <- version
			errorsCh <- err
		}()
	}
	waitGroup.Wait()
	close(versions)
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent Put() error = %v", err)
		}
	}
	var first ObjectVersion
	for version := range versions {
		if first == (ObjectVersion{}) {
			first = version
		} else if version != first {
			t.Fatalf("concurrent Put() version = %#v, want %#v", version, first)
		}
	}
	versionCount := 0
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for object := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:       first.Key,
		Recursive:    true,
		WithVersions: true,
	}) {
		if object.Err != nil {
			t.Fatalf("ListObjects(digest versions) error = %v", object.Err)
		}
		if object.Key == first.Key {
			versionCount++
		}
	}
	if versionCount != 1 {
		t.Fatalf("concurrent Put() digest version count = %d, want 1", versionCount)
	}
	assertNoS3TemporaryVersions(t, client, bucket)
}

func TestS3BlobStorePublicationResolverUsesExactCurrentVersion(t *testing.T) {
	requireMinIOIntegration(t)
	client, bucket := newMinIOBlobFixture(t)
	store, err := NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	ctx := context.Background()
	putCurrent := func(t *testing.T, key string, content []byte) minio.UploadInfo {
		t.Helper()
		info, err := client.PutObject(ctx, bucket, key, bytes.NewReader(content), int64(len(content)), minio.PutObjectOptions{
			ContentType: "application/octet-stream",
		})
		if err != nil {
			t.Fatalf("PutObject(%q) error = %v", key, err)
		}
		if info.VersionID == "" {
			t.Fatalf("PutObject(%q) returned empty VersionID", key)
		}
		return info
	}
	targetFor := func(content []byte) BlobPublicationTarget {
		digest := sha256.Sum256(content)
		return BlobPublicationTarget{
			Key: "sha256/" + hexDigest(digest), SHA256: digest,
			SizeBytes: int64(len(content)), BackendKind: BackendKindS3,
		}
	}

	t.Run("exact key missing", func(t *testing.T) {
		target := targetFor([]byte("missing publication object"))
		if _, err := store.ResolveBlobPublicationObject(ctx, target); !errors.Is(err, ErrBlobNotFound) {
			t.Fatalf("ResolveBlobPublicationObject(missing) error = %v, want ErrBlobNotFound", err)
		}
	})

	t.Run("size mismatch", func(t *testing.T) {
		content := []byte("publication resolver size mismatch")
		target := targetFor(content)
		putCurrent(t, target.Key, content)
		target.SizeBytes++
		_, err := store.ResolveBlobPublicationObject(ctx, target)
		if !errors.Is(err, ErrBlobConflict) || !errors.Is(err, ErrBlobSizeMismatch) {
			t.Fatalf("ResolveBlobPublicationObject(size mismatch) error = %v, want ErrBlobConflict + ErrBlobSizeMismatch", err)
		}
	})

	t.Run("hash mismatch", func(t *testing.T) {
		content := []byte("publication resolver hash mismatch")
		target := targetFor(content)
		corrupt := bytes.Repeat([]byte{'x'}, len(content))
		putCurrent(t, target.Key, corrupt)
		_, err := store.ResolveBlobPublicationObject(ctx, target)
		if !errors.Is(err, ErrBlobConflict) || !errors.Is(err, ErrBlobHashMismatch) {
			t.Fatalf("ResolveBlobPublicationObject(hash mismatch) error = %v, want ErrBlobConflict + ErrBlobHashMismatch", err)
		}
	})

	t.Run("current version drift", func(t *testing.T) {
		content := []byte("publication resolver current version")
		target := targetFor(content)
		first := putCurrent(t, target.Key, content)
		second := putCurrent(t, target.Key, content)
		if first.VersionID == second.VersionID {
			t.Fatalf("versioned MinIO overwrites returned identical VersionID %q", first.VersionID)
		}
		resolved, err := store.ResolveBlobPublicationObject(ctx, target)
		if err != nil {
			t.Fatalf("ResolveBlobPublicationObject(current version) error = %v", err)
		}
		if resolved.VersionID != second.VersionID || resolved.Key != target.Key ||
			resolved.SHA256 != target.SHA256 || resolved.SizeBytes != target.SizeBytes {
			t.Fatalf("resolved current object = %#v, want VersionID %q and target %#v", resolved, second.VersionID, target)
		}
		old := resolved
		old.VersionID = first.VersionID
		if _, err := store.Stat(ctx, old); !errors.Is(err, ErrBlobVersionMismatch) {
			t.Fatalf("Stat(noncurrent publication version) error = %v, want ErrBlobVersionMismatch", err)
		}
	})
}

func requireMinIOIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv(minIOIntegrationEnvironment) != "1" {
		t.Skip("set HOUFENG_MINIO_INTEGRATION=1 to run the real MinIO suite")
	}
}

func newMinIOBlobFixture(t *testing.T) (*minio.Client, string) {
	t.Helper()
	return newMinIOBlobFixtureWithOptions(t, false)
}

func newMinIOBlobFixtureWithObjectLock(t *testing.T) (*minio.Client, string) {
	t.Helper()
	return newMinIOBlobFixtureWithOptions(t, true)
}

func newMinIOBlobFixtureWithOptions(t *testing.T, objectLocking bool) (*minio.Client, string) {
	t.Helper()
	endpoint := requiredMinIOEnvironment(t, "HOUFENG_MINIO_ENDPOINT")
	accessKey := requiredMinIOEnvironment(t, "HOUFENG_MINIO_ACCESS_KEY")
	secretKey := requiredMinIOEnvironment(t, "HOUFENG_MINIO_SECRET_KEY")
	bucketPrefix := requiredMinIOEnvironment(t, "HOUFENG_MINIO_BUCKET")
	secure := false
	if value := os.Getenv("HOUFENG_MINIO_SECURE"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			t.Fatalf("parse HOUFENG_MINIO_SECURE: %v", err)
		}
		secure = parsed
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
	})
	if err != nil {
		t.Fatalf("minio.New() error = %v", err)
	}

	bucket := minIOFixtureBucketName(t, bucketPrefix)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{ObjectLocking: objectLocking}); err != nil {
		t.Fatalf("MakeBucket() error = %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		for object := range client.ListObjects(cleanupCtx, bucket, minio.ListObjectsOptions{
			Recursive:    true,
			WithVersions: true,
		}) {
			if object.Err != nil {
				t.Errorf("ListObjects(cleanup) error = %v", object.Err)
				continue
			}
			if err := client.RemoveObject(cleanupCtx, bucket, object.Key, minio.RemoveObjectOptions{
				VersionID:        object.VersionID,
				GovernanceBypass: true,
			}); err != nil {
				t.Errorf("RemoveObject(cleanup) error = %v", err)
			}
		}
		if err := client.RemoveBucket(cleanupCtx, bucket); err != nil {
			t.Errorf("RemoveBucket(cleanup) error = %v", err)
		}
	})
	if !objectLocking {
		if err := client.EnableVersioning(ctx, bucket); err != nil {
			t.Fatalf("EnableVersioning() error = %v", err)
		}
	}
	if objectLocking {
		mode := minio.Governance
		validity := uint(1)
		unit := minio.Days
		if err := client.SetObjectLockConfig(ctx, bucket, &mode, &validity, &unit); err != nil {
			t.Fatalf("SetObjectLockConfig() error = %v", err)
		}
	}
	if err := func() error {
		versioning, err := client.GetBucketVersioning(ctx, bucket)
		if err != nil {
			return err
		}
		if !versioning.Enabled() {
			return errors.New("versioning is not enabled")
		}
		return nil
	}(); err != nil {
		t.Fatalf("verify MinIO fixture bucket versioning: %v", err)
	}
	return client, bucket
}

func newMinIOBlobClientWithTransport(t *testing.T, transport http.RoundTripper) *minio.Client {
	t.Helper()
	secure := false
	if value := os.Getenv("HOUFENG_MINIO_SECURE"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			t.Fatalf("parse HOUFENG_MINIO_SECURE: %v", err)
		}
		secure = parsed
	}
	client, err := minio.New(requiredMinIOEnvironment(t, "HOUFENG_MINIO_ENDPOINT"), &minio.Options{
		Creds:        credentials.NewStaticV4(requiredMinIOEnvironment(t, "HOUFENG_MINIO_ACCESS_KEY"), requiredMinIOEnvironment(t, "HOUFENG_MINIO_SECRET_KEY"), ""),
		Secure:       secure,
		Region:       "us-east-1",
		BucketLookup: minio.BucketLookupPath,
		Transport:    transport,
		MaxRetries:   1,
	})
	if err != nil {
		t.Fatalf("minio.New() error = %v", err)
	}
	return client
}

func assertNoS3TemporaryVersions(t *testing.T, client *minio.Client, bucket string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for object := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:       s3BlobTemporaryPrefix,
		Recursive:    true,
		WithVersions: true,
	}) {
		if object.Err != nil {
			t.Fatalf("ListObjects(temporary versions) error = %v", object.Err)
		}
		t.Fatalf("temporary S3 Blob residue = key %q version %q delete_marker=%t", object.Key, object.VersionID, object.IsDeleteMarker)
	}
}

func s3VersionsForKey(t *testing.T, client *minio.Client, bucket, key string) []minio.ObjectInfo {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var versions []minio.ObjectInfo
	for object := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:       key,
		Recursive:    true,
		WithVersions: true,
	}) {
		if object.Err != nil {
			t.Fatalf("ListObjects(exact versions) error = %v", object.Err)
		}
		if object.Key == key {
			versions = append(versions, object)
		}
	}
	return versions
}

func assertS3VersionIdentityRejected(t *testing.T, store *S3BlobStore, version ObjectVersion) {
	t.Helper()
	if _, err := store.Stat(context.Background(), version); !errors.Is(err, ErrBlobVersionMismatch) {
		t.Errorf("Stat(noncurrent) error = %v, want ErrBlobVersionMismatch", err)
	}
	reader, err := store.Open(context.Background(), version, FullByteRange())
	if err == nil {
		_ = reader.Close()
		t.Error("Open(noncurrent) unexpectedly succeeded")
	} else if !errors.Is(err, ErrBlobVersionMismatch) {
		t.Errorf("Open(noncurrent) error = %v, want ErrBlobVersionMismatch", err)
	}
	if receipt, err := store.Delete(context.Background(), version); !errors.Is(err, ErrBlobVersionMismatch) {
		t.Errorf("Delete(noncurrent) = %#v, %v, want ErrBlobVersionMismatch", receipt, err)
	}

	exact, _, _, err := store.core.GetObject(context.Background(), store.bucket, version.Key, minio.GetObjectOptions{
		VersionID: version.VersionID,
	})
	if err != nil {
		t.Fatalf("GetObject(original exact version) error = %v", err)
	}
	if _, err := io.Copy(io.Discard, exact); err != nil {
		_ = exact.Close()
		t.Fatalf("read original exact version after rejection: %v", err)
	}
	if err := exact.Close(); err != nil {
		t.Fatalf("close original exact version after rejection: %v", err)
	}
}

func requiredMinIOEnvironment(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s must be set when %s=1", name, minIOIntegrationEnvironment)
	}
	return value
}

func minIOFixtureBucketName(t *testing.T, prefix string) string {
	t.Helper()
	prefix = strings.Trim(strings.ToLower(prefix), "-.")
	if prefix == "" || len(prefix) > 48 {
		t.Fatalf("HOUFENG_MINIO_BUCKET must be a non-empty bucket prefix of at most 48 characters")
	}
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("generate MinIO fixture bucket suffix: %v", err)
	}
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(random))
}

type s3BlobRoundTripperFunc func(*http.Request) (*http.Response, error)

func (roundTrip s3BlobRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func s3BlobTestErrorResponse(request *http.Request, status int, code string) *http.Response {
	body := fmt.Sprintf(`<Error><Code>%s</Code><Message>injected S3 error</Message></Error>`, code)
	return s3BlobTestXMLResponse(request, status, body)
}

func s3BlobTestXMLResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Header:        http.Header{"Content-Type": {"application/xml"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       request,
	}
}
