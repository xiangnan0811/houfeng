//go:build linux

package attachments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLocalNamespaceMarkerInterruptedPublication(t *testing.T) {
	for _, state := range []string{"empty temp", "partial temp", "complete temp", "foreign temp", "truncated final", "foreign final"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			root := filepath.Join(t.TempDir(), "blobs")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			uid, gid := uint32(os.Geteuid()), uint32(os.Getegid())
			newID := func() string {
				id, err := newLocalUUID()
				if err != nil {
					t.Fatal(err)
				}
				return id
			}
			request := LocalProvisionRequest{Root: root, NamespaceID: newID(), IncarnationID: newID(), RequestID: newID(), OwnerUID: uid, OwnerGID: gid}
			rootFD, err := openLocalRoot(root, false, uid, gid)
			if err != nil {
				t.Fatal(err)
			}
			lockFD, err := openLocalProvisionLock(ctx, rootFD, true, true, uid, gid)
			if err != nil {
				unix.Close(rootFD)
				t.Fatal(err)
			}
			lockStat, err := localStat(lockFD, unix.S_IFREG, uid, gid, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			lockNonce, err := localReadFile(lockFD, 36)
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(localMarkerForRequest(request, lockStat, lockNonce))
			if err != nil {
				t.Fatal(err)
			}
			if err := unix.Close(lockFD); err != nil {
				t.Fatal(err)
			}
			if err := unix.Close(rootFD); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, localMarkerTempPrefix+request.RequestID)
			written := body
			switch state {
			case "empty temp":
				written = nil
			case "partial temp":
				written = body[:len(body)/2]
			case "foreign temp":
				written = []byte("foreign temporary state")
			case "truncated final":
				path = filepath.Join(root, localMarkerName)
				written = body[:len(body)/2]
			case "foreign final":
				path = filepath.Join(root, localMarkerName)
				other := localMarkerForRequest(request, lockStat, lockNonce)
				other.NamespaceID = newID()
				written, err = json.Marshal(other)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(path, written, 0o600); err != nil {
				t.Fatal(err)
			}
			repository := &localBlobTestRepository{}
			result, err := ProvisionLocalNamespace(ctx, request, repository)
			if state == "foreign temp" || state == "truncated final" || state == "foreign final" {
				if err == nil {
					t.Fatal("foreign or truncated marker accepted")
				}
				after, readErr := os.ReadFile(path)
				if readErr != nil || !bytes.Equal(after, written) {
					t.Fatal("foreign or truncated artifact changed")
				}
				return
			}
			if err != nil {
				t.Fatalf("retry after %s: %v", state, err)
			}
			if result.AdmissionState != "active" || result.RequestID != request.RequestID {
				t.Fatal("retry did not admit expected namespace")
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("temporary marker remains")
			}
			actual, err := os.ReadFile(filepath.Join(root, localMarkerName))
			if err != nil || !bytes.Equal(actual, body) {
				t.Fatal("published marker differs from durable request")
			}
			if _, err := ProvisionLocalNamespace(ctx, request, repository); err != nil {
				t.Fatalf("published marker replay: %v", err)
			}
		})
	}
}

func TestLocalBlobManagedNamespaceAndGuard(t *testing.T) {
	t.Run("raw_mutation_has_no_authority", func(t *testing.T) {
		store := newTestLocalBlobStore(t, "", localBlobHooks{})
		content := []byte("managed bytes")
		if _, err := store.LocalBlobStore.Put(context.Background(), blobPutRequest(content), bytes.NewReader(content)); !errors.Is(err, ErrManagedBlobRequired) {
			t.Fatalf("raw Put: %v", err)
		}
		version, err := store.Put(context.Background(), blobPutRequest(content), bytes.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		for _, delete := range []func(context.Context, ObjectVersion) (DeletionReceipt, error){store.Delete, store.DeleteGCVersion} {
			if receipt, err := delete(context.Background(), version); !errors.Is(err, ErrManagedBlobRequired) || receipt.Deleted {
				t.Fatalf("raw delete: %#v, %v", receipt, err)
			}
		}
		reader, err := store.Open(context.Background(), version, FullByteRange())
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || !bytes.Equal(got, content) {
			t.Fatalf("protected bytes %q, %v", got, err)
		}
	})
	t.Run("runtime_never_provisions", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "missing")
		repository := &localBlobTestRepository{}
		if _, err := NewLocalBlobStore(context.Background(), root, repository); !errors.Is(err, ErrLocalNamespaceClosed) {
			t.Fatalf("unprovisioned startup: %v", err)
		}
		if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("runtime created root: %v", err)
		}
	})
	t.Run("guard_cancellation_independent_keys_and_revocation", func(t *testing.T) {
		store := newTestLocalBlobStore(t, "", localBlobHooks{})
		firstKey, secondKey := "sha256/"+strings.Repeat("1", 64), "sha256/"+strings.Repeat("2", 64)
		held, release := make(chan *LocalKeyGuard, 1), make(chan struct{})
		finished := make(chan error, 1)
		go func() {
			finished <- store.WithKeyGuard(context.Background(), firstKey, func(_ context.Context, g *LocalKeyGuard) error {
				held <- g
				<-release
				return g.run(firstKey, func() error { return nil })
			})
		}()
		guard := <-held
		if err := store.WithKeyGuard(context.Background(), secondKey, func(context.Context, *LocalKeyGuard) error { return nil }); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := store.WithKeyGuard(ctx, firstKey, func(context.Context, *LocalKeyGuard) error { t.Error("canceled waiter entered guard"); return nil }); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled wait: %v", err)
		}
		if err := guard.run(secondKey, func() error { t.Error("cross-key capability accepted"); return nil }); !errors.Is(err, ErrLocalGuardExpired) {
			t.Fatalf("cross-key: %v", err)
		}
		close(release)
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
		if err := guard.run(firstKey, func() error { t.Error("escaped capability accepted"); return nil }); !errors.Is(err, ErrLocalGuardExpired) {
			t.Fatalf("escaped guard: %v", err)
		}
	})
	t.Run("registered_anchor_is_never_recreated", func(t *testing.T) {
		store := newTestLocalBlobStore(t, "", localBlobHooks{})
		key := "sha256/" + strings.Repeat("3", 64)
		if err := store.WithKeyGuard(context.Background(), key, func(context.Context, *LocalKeyGuard) error { return nil }); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(store.root, ".locks", key[7:]+".lock")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := store.WithKeyGuard(context.Background(), key, func(context.Context, *LocalKeyGuard) error { t.Error("missing anchor admitted"); return nil }); !errors.Is(err, ErrLocalIdentityChanged) {
			t.Fatalf("missing anchor: %v", err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("anchor repaired: %v", err)
		}
	})
	t.Run("directory_replacement_prevents_write", func(t *testing.T) {
		store := newTestLocalBlobStore(t, "", localBlobHooks{})
		if err := os.Rename(filepath.Join(store.root, "sha256"), filepath.Join(store.root, "original")); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(store.root, "sha256"), 0o700); err != nil {
			t.Fatal(err)
		}
		content := []byte("must not reach replacement")
		if _, err := store.Put(context.Background(), blobPutRequest(content), bytes.NewReader(content)); !errors.Is(err, ErrLocalIdentityChanged) {
			t.Fatalf("replacement admitted: %v", err)
		}
		for _, name := range []string{"sha256", "original"} {
			entries, err := os.ReadDir(filepath.Join(store.root, name))
			if err != nil || len(entries) != 0 {
				t.Fatalf("replacement write in %s: %v %v", name, entries, err)
			}
		}
	})
	t.Run("unknown_dispatch_prevents_bytes", func(t *testing.T) {
		store := newTestLocalBlobStore(t, "", localBlobHooks{})
		store.repository.dispatchError = true
		content := []byte("unconfirmed dispatch")
		if _, err := store.Put(context.Background(), blobPutRequest(content), bytes.NewReader(content)); !errors.Is(err, ErrWriteOutcomeUnknown) {
			t.Fatalf("unknown dispatch: %v", err)
		}
		entries, err := os.ReadDir(filepath.Join(store.root, "sha256"))
		if err != nil || len(entries) != 0 {
			t.Fatalf("unconfirmed IO: %v %v", entries, err)
		}
	})
	t.Run("committed_dispatch_readback_allows_one_attempt", func(t *testing.T) {
		store := newTestLocalBlobStore(t, "", localBlobHooks{})
		store.repository.dispatchError, store.repository.dispatchCommitted = true, true
		content := []byte("confirmed original dispatch")
		version, err := store.Put(context.Background(), blobPutRequest(content), bytes.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Stat(context.Background(), version); err != nil {
			t.Fatal(err)
		}
		if len(store.repository.writes) != 1 {
			t.Fatalf("readback created %d attempts", len(store.repository.writes))
		}
	})
	t.Run("unknown_return_keeps_both_owned_names", func(t *testing.T) {
		store := newTestLocalBlobStore(t, "", localBlobHooks{})
		store.repository.returnError = true
		content := []byte("pending durable return")
		version, err := store.Put(context.Background(), blobPutRequest(content), bytes.NewReader(content))
		if !errors.Is(err, ErrWriteOutcomeUnknown) || version != (ObjectVersion{}) {
			t.Fatalf("unconfirmed publication: %#v %v", version, err)
		}
		o := store.repository.writes[store.repository.lastID]
		for _, name := range []string{o.Key, *o.TemporaryKey} {
			got, err := os.ReadFile(filepath.Join(store.root, name))
			if err != nil || !bytes.Equal(got, content) {
				t.Fatalf("lost pending target %s: %q %v", name, got, err)
			}
		}
		if o.ReturnedAt != nil {
			t.Fatal("uncommitted return became durable")
		}
	})
}

func TestLocalBlobStorePutRejectsTypedNilReaderBeforeFilesystemSideEffects(t *testing.T) {
	store := newTestLocalBlobStore(t, "", localBlobHooks{})
	var reader *bytes.Reader
	if _, err := store.Put(context.Background(), blobPutRequest([]byte("nil reader")), reader); !errors.Is(err, ErrInvalidBlobRequest) {
		t.Fatalf("typed nil: %v", err)
	}
	if len(store.repository.writes) != 0 {
		t.Fatal("invalid reader registered a write")
	}
}

func TestLocalBlobStoreUsesPrivateModesAndSingleConditionalPublish(t *testing.T) {
	store := newTestLocalBlobStore(t, "", localBlobHooks{})
	content := []byte("concurrent immutable bytes")
	const count = 8
	type result struct {
		version ObjectVersion
		err     error
	}
	results := make(chan result, count)
	var group sync.WaitGroup
	for range count {
		group.Add(1)
		go func() {
			defer group.Done()
			version, err := store.Put(context.Background(), blobPutRequest(content), bytes.NewReader(content))
			results <- result{version, err}
		}()
	}
	group.Wait()
	close(results)
	var first ObjectVersion
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if first == (ObjectVersion{}) {
			first = result.version
		} else if first != result.version {
			t.Fatalf("dedup replaced materialization: %#v %#v", first, result.version)
		}
	}
	if len(store.repository.writes) != count {
		t.Fatalf("actual attempts=%d want=%d", len(store.repository.writes), count)
	}
	assertPathMode(t, store.root, 0o700)
	assertPathMode(t, filepath.Join(store.root, "sha256"), 0o700)
	assertPathMode(t, filepath.Join(store.root, ".locks"), 0o700)
	assertPathMode(t, filepath.Join(store.root, first.Key), 0o600)
	entries, err := os.ReadDir(filepath.Join(store.root, "sha256"))
	if err != nil || len(entries) != 1 || entries[0].Name() != first.Key[7:] {
		t.Fatalf("successful write left names: %v %v", entries, err)
	}
	reader, err := store.Open(context.Background(), first, ClosedByteRange(1, 4))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(got, content[1:5]) {
		t.Fatalf("range=%q %v", got, err)
	}
}

func TestLocalBlobStoreFailureCutpointsPreserveOwnedRecoveryTargets(t *testing.T) {
	content := []byte("cutpoint bytes")
	injected := errors.New("injected physical interruption")
	for _, cutpoint := range []localBlobCutpoint{localBlobAfterTempCreate, localBlobAfterCopy, localBlobAfterFileSync, localBlobBeforePublish, localBlobAfterPublish} {
		t.Run(string(cutpoint), func(t *testing.T) {
			store := newTestLocalBlobStore(t, "", localBlobHooks{cutpoint: func(current localBlobCutpoint) error {
				if current == cutpoint {
					return injected
				}
				return nil
			}})
			if _, err := store.Put(context.Background(), blobPutRequest(content), bytes.NewReader(content)); !errors.Is(err, injected) {
				t.Fatalf("cutpoint: %v", err)
			}
			o := store.repository.writes[store.repository.lastID]
			if o.Outcome != "failed" || o.ReturnedAt == nil || o.TemporaryInode == nil {
				t.Fatalf("lost failed responsibility: %#v", o)
			}
			got, err := os.ReadFile(filepath.Join(store.root, *o.TemporaryKey))
			if err != nil {
				t.Fatal(err)
			}
			if cutpoint == localBlobAfterTempCreate {
				if len(got) != 0 {
					t.Fatalf("pre-copy bytes=%q", got)
				}
			} else if !bytes.Equal(got, content) {
				t.Fatalf("lost temporary bytes=%q", got)
			}
			final, err := os.ReadFile(filepath.Join(store.root, o.Key))
			if cutpoint == localBlobAfterPublish {
				if err != nil || !bytes.Equal(final, content) {
					t.Fatalf("rolled back published bytes %q %v", final, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unexpected final %q %v", final, err)
			}
		})
	}
}

func TestLocalBlobStoreRejectsSymlinkRoot(t *testing.T) {
	store := newTestLocalBlobStore(t, "", localBlobHooks{})
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(store.root, link); err != nil {
		t.Fatal(err)
	}
	copyRepository := &localBlobTestRepository{namespace: store.namespace}
	copyRepository.namespace.RootPath = link
	if opened, err := NewLocalBlobStore(context.Background(), link, copyRepository); err == nil {
		opened.Close()
		t.Fatal("symlink root accepted")
	}
}

func assertPathMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("mode %s=%o want=%o", path, info.Mode().Perm(), want)
	}
}
