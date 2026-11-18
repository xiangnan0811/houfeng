package attachments

import (
	"context"
	"io"
)

type localBlobCutpoint string

const (
	localBlobAfterTempOpen         localBlobCutpoint = "after-temp-open"
	localBlobAfterTempIdentity     localBlobCutpoint = "after-temp-identity"
	localBlobAfterTempRegistration localBlobCutpoint = "after-temp-registration"
	localBlobAfterTempLink         localBlobCutpoint = "after-temp-link"
	localBlobAfterTempCreate       localBlobCutpoint = "after-temp-create"
	localBlobAfterCopy             localBlobCutpoint = "after-copy"
	localBlobAfterFileSync         localBlobCutpoint = "after-file-sync"
	localBlobBeforePublish         localBlobCutpoint = "before-publish"
	localBlobAfterPublish          localBlobCutpoint = "after-publish"
	localBlobAfterDelete           localBlobCutpoint = "after-delete"
	localBlobGuardWaiting          localBlobCutpoint = "guard-waiting"
)

type localBlobHooks struct {
	cutpoint func(localBlobCutpoint) error
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

func (reader *localBlobSectionReadCloser) Close() error { return reader.closer.Close() }
