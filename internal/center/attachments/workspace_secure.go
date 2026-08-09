package attachments

import (
	"context"
	"io"
)

// secureProcessorWorkspaceRoot and secureProcessorWorkspace are the narrow
// filesystem capabilities used by the production processor path.  The
// concrete implementation is platform-specific; Linux uses descriptor-
// anchored *at operations and non-Linux builds fail closed.
type secureProcessorWorkspaceRoot interface {
	openWorkspace(context.Context, string) (secureProcessorWorkspace, error)
	removeWorkspace(context.Context, string) (int64, error)
	close() error
}

type secureProcessorWorkspace interface {
	readSourceBounded(context.Context, int64) ([]byte, error)
	sourceSize(context.Context) (int64, error)
	materializeSource(context.Context, io.Reader, BlobObject, int64) error
	preparePreviewDirectories(context.Context, PreviewConfig) error
	ensurePreviewAbsent(context.Context) error
	commandSourcePath() string
	close() error
}
