package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"testing"
)

type blobStoreConformanceFactory func(*testing.T) BlobStore

func runBlobStoreConformance(t *testing.T, factory blobStoreConformanceFactory) {
	t.Helper()

	t.Run("conditional put and exact stat", func(t *testing.T) {
		store := factory(t)
		content := []byte("immutable attachment bytes")
		request := blobPutRequest(content)

		first, err := store.Put(context.Background(), request, bytes.NewReader(content))
		if err != nil {
			t.Fatalf("Put(first) error = %v", err)
		}
		second, err := store.Put(context.Background(), request, bytes.NewReader(content))
		if err != nil {
			t.Fatalf("Put(dedupe) error = %v", err)
		}
		if second != first {
			t.Fatalf("Put(dedupe) = %#v, want %#v", second, first)
		}
		if first.Key != "sha256/"+hexDigest(request.ExpectedSHA256) ||
			first.SHA256 != request.ExpectedSHA256 || first.SizeBytes != int64(len(content)) || first.VersionID == "" {
			t.Fatalf("Put() version = %#v", first)
		}

		info, err := store.Stat(context.Background(), first)
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}
		if info.Version != first {
			t.Fatalf("Stat() = %#v, want version %#v", info, first)
		}
	})

	t.Run("size and digest mismatch fail closed", func(t *testing.T) {
		store := factory(t)
		content := []byte("verified bytes")
		request := blobPutRequest(content)
		request.ExpectedSizeBytes++
		if _, err := store.Put(context.Background(), request, bytes.NewReader(content)); !errors.Is(err, ErrBlobSizeMismatch) {
			t.Fatalf("Put(size mismatch) error = %v, want ErrBlobSizeMismatch", err)
		}

		request = blobPutRequest(content)
		request.ExpectedSHA256 = sha256.Sum256([]byte("different bytes"))
		if _, err := store.Put(context.Background(), request, bytes.NewReader(content)); !errors.Is(err, ErrBlobHashMismatch) {
			t.Fatalf("Put(hash mismatch) error = %v, want ErrBlobHashMismatch", err)
		}
	})

	t.Run("full and closed range reads", func(t *testing.T) {
		store := factory(t)
		content := []byte("0123456789")
		version, err := store.Put(context.Background(), blobPutRequest(content), bytes.NewReader(content))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		full, err := store.Open(context.Background(), version, FullByteRange())
		if err != nil {
			t.Fatalf("Open(full) error = %v", err)
		}
		assertBlobRead(t, full, content)

		partial, err := store.Open(context.Background(), version, ClosedByteRange(2, 5))
		if err != nil {
			t.Fatalf("Open(range) error = %v", err)
		}
		assertBlobRead(t, partial, []byte("2345"))

		for _, byteRange := range []ByteRange{
			{},
			ClosedByteRange(-1, 2),
			ClosedByteRange(4, 3),
			ClosedByteRange(0, int64(len(content))),
		} {
			if _, err := store.Open(context.Background(), version, byteRange); !errors.Is(err, ErrInvalidBlobRange) {
				t.Errorf("Open(%#v) error = %v, want ErrInvalidBlobRange", byteRange, err)
			}
		}
	})

	t.Run("version mismatch and idempotent delete", func(t *testing.T) {
		store := factory(t)
		content := []byte("delete me")
		version, err := store.Put(context.Background(), blobPutRequest(content), bytes.NewReader(content))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}
		wrongVersion := version
		wrongVersion.VersionID += "-wrong"
		if _, err := store.Stat(context.Background(), wrongVersion); !errors.Is(err, ErrBlobVersionMismatch) {
			t.Fatalf("Stat(version mismatch) error = %v, want ErrBlobVersionMismatch", err)
		}
		if _, err := store.Open(context.Background(), wrongVersion, FullByteRange()); !errors.Is(err, ErrBlobVersionMismatch) {
			t.Fatalf("Open(version mismatch) error = %v, want ErrBlobVersionMismatch", err)
		}
		if _, err := store.Delete(context.Background(), wrongVersion); !errors.Is(err, ErrBlobVersionMismatch) {
			t.Fatalf("Delete(version mismatch) error = %v, want ErrBlobVersionMismatch", err)
		}

		deleted, err := store.Delete(context.Background(), version)
		if err != nil {
			t.Fatalf("Delete(first) error = %v", err)
		}
		if !deleted.Deleted || deleted.Version != version {
			t.Fatalf("Delete(first) = %#v", deleted)
		}
		if _, err := store.Stat(context.Background(), version); !errors.Is(err, ErrBlobNotFound) {
			t.Fatalf("Stat(deleted) error = %v, want ErrBlobNotFound", err)
		}
		deleted, err = store.Delete(context.Background(), version)
		if err != nil {
			t.Fatalf("Delete(replay) error = %v", err)
		}
		if deleted.Deleted || deleted.Version != version {
			t.Fatalf("Delete(replay) = %#v", deleted)
		}
	})

	t.Run("partial reader failure does not poison retry", func(t *testing.T) {
		store := factory(t)
		content := []byte("retry after interrupted reader")
		request := blobPutRequest(content)
		readerError := errors.New("injected Blob reader failure")
		if _, err := store.Put(context.Background(), request, &failingBlobReader{
			content: content[:8], err: readerError,
		}); !errors.Is(err, readerError) {
			t.Fatalf("Put(interrupted) error = %v, want injected error", err)
		}
		if _, err := store.Put(context.Background(), request, bytes.NewReader(content)); err != nil {
			t.Fatalf("Put(retry) error = %v", err)
		}
	})
}

func blobPutRequest(content []byte) PutRequest {
	digest := sha256.Sum256(content)
	temporaryKey, err := newS3BlobTemporaryKey()
	if err != nil {
		panic(err)
	}
	return PutRequest{
		ExpectedSHA256:    digest,
		ExpectedSizeBytes: int64(len(content)),
		TemporaryKey:      temporaryKey,
	}
}

func assertBlobRead(t *testing.T, reader io.ReadCloser, want []byte) {
	t.Helper()
	got, err := io.ReadAll(reader)
	if err != nil {
		_ = reader.Close()
		t.Fatalf("ReadAll() error = %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Open() bytes = %q, want %q", got, want)
	}
}

type failingBlobReader struct {
	content []byte
	err     error
	done    bool
}

func (reader *failingBlobReader) Read(buffer []byte) (int, error) {
	if len(reader.content) > 0 {
		count := copy(buffer, reader.content)
		reader.content = reader.content[count:]
		return count, nil
	}
	if !reader.done {
		reader.done = true
		return 0, reader.err
	}
	return 0, io.EOF
}
