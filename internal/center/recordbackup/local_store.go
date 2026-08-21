package recordbackup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type LocalStore struct {
	root string
}

func NewLocalStore(root string) (*LocalStore, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, fmt.Errorf("%w: local root", ErrInvalidBackupRequest)
	}
	clean := filepath.Clean(root)
	if err := os.MkdirAll(clean, 0o700); err != nil {
		return nil, fmt.Errorf("%w: mkdir", ErrInvalidBackupRequest)
	}
	return &LocalStore{root: clean}, nil
}

func (store *LocalStore) Stage(ctx context.Context, artifact ArtifactRef, body io.Reader) error {
	if err := store.ready(ctx, artifact); err != nil {
		return err
	}
	path := store.path("staging", artifact)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("%w: stage dir", ErrBackupIncomplete)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("%w: stage create", ErrBackupIncomplete)
	}
	defer file.Close()
	if _, err := io.Copy(file, body); err != nil {
		return fmt.Errorf("%w: stage copy", ErrBackupIncomplete)
	}
	return file.Sync()
}

func (store *LocalStore) Publish(ctx context.Context, artifact ArtifactRef) error {
	if err := store.ready(ctx, artifact); err != nil {
		return err
	}
	source := store.path("staging", artifact)
	destination := store.path("published", artifact)
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("%w: publish dir", ErrBackupIncomplete)
	}
	if err := os.Rename(source, destination); err != nil {
		return fmt.Errorf("%w: publish", ErrBackupIncomplete)
	}
	return nil
}

func (store *LocalStore) Abort(ctx context.Context, artifact ArtifactRef) error {
	if err := store.ready(ctx, artifact); err != nil {
		return err
	}
	return removeIfPresent(store.path("staging", artifact))
}

func (store *LocalStore) AbortMultipart(ctx context.Context, artifact ArtifactRef) error {
	if err := store.ready(ctx, artifact); err != nil {
		return err
	}
	return removeIfPresent(store.path("multipart", artifact))
}

func (store *LocalStore) ReleasePin(ctx context.Context, artifact ArtifactRef) error {
	if err := store.ready(ctx, artifact); err != nil {
		return err
	}
	return removeIfPresent(store.path("pins", artifact))
}

func (store *LocalStore) ReleaseWorkspace(ctx context.Context) error {
	if ctx == nil || store == nil {
		return ErrBackupUnavailable
	}
	for _, name := range []string{"staging", "multipart", "pins"} {
		if err := os.RemoveAll(filepath.Join(store.root, name)); err != nil {
			return fmt.Errorf("%w: workspace", ErrBackupCleanupRequired)
		}
	}
	return nil
}

func (store *LocalStore) Open(ctx context.Context, artifact ArtifactRef) (io.ReadCloser, error) {
	if err := store.ready(ctx, artifact); err != nil {
		return nil, err
	}
	file, err := os.Open(store.path("published", artifact))
	if err != nil {
		return nil, fmt.Errorf("%w: open", ErrBackupUnavailable)
	}
	return file, nil
}

func (store *LocalStore) ready(ctx context.Context, artifact ArtifactRef) error {
	if ctx == nil || store == nil || store.root == "" {
		return ErrBackupUnavailable
	}
	if artifact.kind == "" || artifact.keyVersion == "" {
		return ErrInvalidBackupRequest
	}
	return nil
}

func (store *LocalStore) path(class string, artifact ArtifactRef) string {
	return filepath.Join(store.root, class, artifact.kind, artifact.keyVersion)
}

func removeIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%w: remove", ErrBackupCleanupRequired)
	}
	return nil
}
