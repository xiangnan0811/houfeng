//go:build linux

package attachments

import "context"

// OpenLocalBlobProcessFixture exposes physical cutpoints only to the external
// test binary, which uses the production PostgreSQL repository and coordinator.
func OpenLocalBlobProcessFixture(ctx context.Context, root string, repository LocalNamespaceRepository, cutpoint func(string) error) (*LocalBlobStore, error) {
	return newLocalBlobStore(ctx, root, repository, localBlobHooks{cutpoint: func(current localBlobCutpoint) error { return cutpoint(string(current)) }})
}
