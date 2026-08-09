package attachments

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
)

func TestLocalBlobStoreConformance(t *testing.T) {
	runBlobStoreConformance(t, func(t *testing.T) BlobStore {
		t.Helper()
		store, err := NewLocalBlobStore(t.TempDir())
		if err != nil {
			t.Fatalf("NewLocalBlobStore() error = %v", err)
		}
		return store
	})
}

func TestLocalBlobStorePutRejectsTypedNilReaderBeforeFilesystemSideEffects(t *testing.T) {
	t.Parallel()

	cutpointCalls := 0
	store, err := newLocalBlobStore(t.TempDir(), localBlobHooks{
		cutpoint: func(localBlobCutpoint) error {
			cutpointCalls++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("newLocalBlobStore() error = %v", err)
	}
	content := []byte("typed-nil local Blob reader")
	var typedNil *bytes.Reader
	var reader io.Reader = typedNil

	err, panicValue := captureAttachmentCallPanic(func() error {
		_, err := store.Put(context.Background(), blobPutRequest(content), reader)
		return err
	})
	if panicValue != nil {
		t.Fatalf("Put(typed-nil reader) panic = %v after %d filesystem cutpoints; want ErrInvalidBlobRequest before side effects",
			panicValue, cutpointCalls)
	}
	if !errors.Is(err, ErrInvalidBlobRequest) {
		t.Fatalf("Put(typed-nil reader) error = %v, want ErrInvalidBlobRequest", err)
	}
	if cutpointCalls != 0 {
		t.Fatalf("Put(typed-nil reader) filesystem cutpoint calls = %d, want 0", cutpointCalls)
	}
}

func TestLocalBlobStoreUsesPrivateModesAndSingleConditionalPublish(t *testing.T) {
	root := filepath.Join(t.TempDir(), "blob-root")
	store, err := NewLocalBlobStore(root)
	if err != nil {
		t.Fatalf("NewLocalBlobStore() error = %v", err)
	}
	content := []byte("concurrent immutable bytes")
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

	assertPathMode(t, root, 0o700)
	assertPathMode(t, filepath.Join(root, "sha256"), 0o700)
	objectPath := filepath.Join(root, filepath.FromSlash(first.Key))
	assertPathMode(t, objectPath, 0o600)
	entries, err := os.ReadDir(filepath.Dir(objectPath))
	if err != nil {
		t.Fatalf("ReadDir(object directory) error = %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{filepath.Base(objectPath)}) {
		t.Fatalf("object directory entries = %#v", names)
	}
}

func TestLocalBlobStoreConditionalPublishDoesNotReplaceAcrossStores(t *testing.T) {
	root := filepath.Join(t.TempDir(), "blob-root")
	content := []byte("cross-store immutable bytes")
	request := blobPutRequest(content)
	reachedPublish := make(chan struct{}, 2)
	releasePublish := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
	stores := make([]*LocalBlobStore, len(releasePublish))
	for index := range stores {
		index := index
		store, err := newLocalBlobStore(root, localBlobHooks{
			cutpoint: func(current localBlobCutpoint) error {
				if current != localBlobBeforePublish {
					return nil
				}
				reachedPublish <- struct{}{}
				<-releasePublish[index]
				return nil
			},
		})
		if err != nil {
			t.Fatalf("newLocalBlobStore(%d) error = %v", index, err)
		}
		stores[index] = store
	}

	type putResult struct {
		version ObjectVersion
		err     error
	}
	results := [2]chan putResult{make(chan putResult, 1), make(chan putResult, 1)}
	for index, store := range stores {
		go func(index int, store *LocalBlobStore) {
			version, err := store.Put(context.Background(), request, bytes.NewReader(content))
			results[index] <- putResult{version: version, err: err}
		}(index, store)
	}
	for range stores {
		<-reachedPublish
	}

	close(releasePublish[0])
	first := <-results[0]
	if first.err != nil {
		t.Fatalf("Put(first publisher) error = %v", first.err)
	}
	objectPath := filepath.Join(root, filepath.FromSlash(first.version.Key))
	firstInfo, err := os.Lstat(objectPath)
	if err != nil {
		t.Fatalf("Lstat(first published object) error = %v", err)
	}

	close(releasePublish[1])
	second := <-results[1]
	if second.err != nil {
		t.Fatalf("Put(second publisher) error = %v", second.err)
	}
	if second.version != first.version {
		t.Fatalf("Put(second publisher) version = %#v, want %#v", second.version, first.version)
	}
	info, err := stores[1].Stat(context.Background(), first.version)
	if err != nil {
		t.Fatalf("Stat(final object) error = %v", err)
	}
	if info.Version != first.version {
		t.Fatalf("Stat(final object) = %#v, want version %#v", info, first.version)
	}
	finalInfo, err := os.Lstat(objectPath)
	if err != nil {
		t.Fatalf("Lstat(final object) error = %v", err)
	}
	if !os.SameFile(firstInfo, finalInfo) {
		t.Fatal("second conditional publish replaced the already published inode")
	}
	assertPathMode(t, objectPath, 0o600)
	stored, err := os.ReadFile(objectPath)
	if err != nil {
		t.Fatalf("ReadFile(final object) error = %v", err)
	}
	if !bytes.Equal(stored, content) {
		t.Fatalf("ReadFile(final object) = %q, want %q", stored, content)
	}
	entries, err := os.ReadDir(filepath.Dir(objectPath))
	if err != nil {
		t.Fatalf("ReadDir(object directory) error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(objectPath) {
		t.Fatalf("object directory entries = %#v, want only final object", entries)
	}
}

func TestLocalBlobStoreFailureCutpointsLeaveNoObjectOrTemporaryResidue(t *testing.T) {
	content := []byte("cutpoint bytes")
	request := blobPutRequest(content)
	cutpointError := errors.New("injected local Blob cutpoint")
	for _, cutpoint := range []localBlobCutpoint{
		localBlobAfterTempCreate,
		localBlobAfterCopy,
		localBlobAfterFileSync,
		localBlobBeforePublish,
		localBlobAfterPublish,
	} {
		t.Run(string(cutpoint), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "blob-root")
			store, err := newLocalBlobStore(root, localBlobHooks{
				cutpoint: func(current localBlobCutpoint) error {
					if current == cutpoint {
						return cutpointError
					}
					return nil
				},
			})
			if err != nil {
				t.Fatalf("newLocalBlobStore() error = %v", err)
			}
			if _, err := store.Put(context.Background(), request, bytes.NewReader(content)); !errors.Is(err, cutpointError) {
				t.Fatalf("Put(cutpoint) error = %v, want injected error", err)
			}
			entries, err := os.ReadDir(filepath.Join(root, "sha256"))
			if err != nil {
				t.Fatalf("ReadDir() error = %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("Put(cutpoint) residue = %#v", entries)
			}
		})
	}
}

func TestLocalBlobStoreRejectsSymlinkRoot(t *testing.T) {
	parent := t.TempDir()
	realRoot := filepath.Join(parent, "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatalf("Mkdir(real root) error = %v", err)
	}
	linkRoot := filepath.Join(parent, "link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Fatalf("Symlink(root) error = %v", err)
	}
	if _, err := NewLocalBlobStore(linkRoot); !errors.Is(err, ErrInvalidBlobStoreConfig) {
		t.Fatalf("NewLocalBlobStore(symlink) error = %v, want ErrInvalidBlobStoreConfig", err)
	}
}

func assertPathMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat(%q) error = %v", path, err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("Lstat(%q) mode = %o, want %o", path, info.Mode().Perm(), want)
	}
}
