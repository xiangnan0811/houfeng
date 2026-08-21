package recordbackup

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
)

type S3Store struct {
	client *minio.Client
	bucket string
}

func NewS3Store(client *minio.Client, bucket string) (*S3Store, error) {
	if client == nil || strings.TrimSpace(bucket) == "" {
		return nil, fmt.Errorf("%w: s3 store", ErrInvalidBackupRequest)
	}
	return &S3Store{client: client, bucket: bucket}, nil
}

func (store *S3Store) Stage(ctx context.Context, artifact ArtifactRef, body io.Reader) error {
	if err := store.ready(ctx, artifact); err != nil {
		return err
	}
	_, err := store.client.PutObject(ctx, store.bucket, store.key("staging", artifact), body, -1, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return fmt.Errorf("%w: s3 stage", ErrBackupIncomplete)
	}
	return nil
}

func (store *S3Store) Publish(ctx context.Context, artifact ArtifactRef) error {
	if err := store.ready(ctx, artifact); err != nil {
		return err
	}
	source := minio.CopySrcOptions{Bucket: store.bucket, Object: store.key("staging", artifact)}
	destination := minio.CopyDestOptions{Bucket: store.bucket, Object: store.key("published", artifact)}
	if _, err := store.client.CopyObject(ctx, destination, source); err != nil {
		return fmt.Errorf("%w: s3 publish", ErrBackupIncomplete)
	}
	return store.client.RemoveObject(ctx, store.bucket, store.key("staging", artifact), minio.RemoveObjectOptions{})
}

func (store *S3Store) Abort(ctx context.Context, artifact ArtifactRef) error {
	if err := store.ready(ctx, artifact); err != nil {
		return err
	}
	return store.client.RemoveObject(ctx, store.bucket, store.key("staging", artifact), minio.RemoveObjectOptions{})
}

func (store *S3Store) AbortMultipart(ctx context.Context, artifact ArtifactRef) error {
	if err := store.ready(ctx, artifact); err != nil {
		return err
	}
	for object := range store.client.ListIncompleteUploads(ctx, store.bucket, store.key("multipart", artifact), true) {
		if object.Err != nil {
			return fmt.Errorf("%w: s3 multipart list", ErrBackupCleanupRequired)
		}
		if err := store.client.RemoveIncompleteUpload(ctx, store.bucket, object.Key); err != nil {
			return fmt.Errorf("%w: s3 multipart abort", ErrBackupCleanupRequired)
		}
	}
	return nil
}

func (store *S3Store) ReleasePin(ctx context.Context, artifact ArtifactRef) error {
	if err := store.ready(ctx, artifact); err != nil {
		return err
	}
	return store.client.RemoveObject(ctx, store.bucket, store.key("pins", artifact), minio.RemoveObjectOptions{})
}

func (store *S3Store) ReleaseWorkspace(ctx context.Context) error {
	if ctx == nil || store == nil || store.client == nil {
		return ErrBackupUnavailable
	}
	for _, prefix := range []string{"staging/", "multipart/", "pins/"} {
		for object := range store.client.ListObjects(ctx, store.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
			if object.Err != nil {
				return fmt.Errorf("%w: s3 workspace list", ErrBackupCleanupRequired)
			}
			if err := store.client.RemoveObject(ctx, store.bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
				return fmt.Errorf("%w: s3 workspace", ErrBackupCleanupRequired)
			}
		}
	}
	return nil
}

func (store *S3Store) Open(ctx context.Context, artifact ArtifactRef) (io.ReadCloser, error) {
	if err := store.ready(ctx, artifact); err != nil {
		return nil, err
	}
	reader, err := store.client.GetObject(ctx, store.bucket, store.key("published", artifact), minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("%w: s3 open", ErrBackupUnavailable)
	}
	return reader, nil
}

func (store *S3Store) ready(ctx context.Context, artifact ArtifactRef) error {
	if ctx == nil || store == nil || store.client == nil {
		return ErrBackupUnavailable
	}
	if artifact.kind == "" || artifact.keyVersion == "" {
		return ErrInvalidBackupRequest
	}
	return nil
}

func (store *S3Store) key(class string, artifact ArtifactRef) string {
	return class + "/" + artifact.kind + "/" + artifact.keyVersion
}
