//go:build linux

package attachments_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/store"
)

// Invoked only by the isolated store integration fixture. The event/control
// descriptors prove arrival at physical boundaries without timing guesses.
func TestLocalBlobProcessHelper(t *testing.T) {
	mode := os.Getenv("HOUFENG_LOCAL_PROCESS_MODE")
	if mode == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, control := os.NewFile(3, "local-events"), os.NewFile(4, "local-control")
	defer events.Close()
	defer control.Close()
	barrier := func(event byte) error {
		if _, err := events.Write([]byte{event}); err != nil {
			return err
		}
		var command [1]byte
		_, err := io.ReadFull(control, command[:])
		return err
	}
	pool, err := pgxpool.New(ctx, os.Getenv("HOUFENG_LOCAL_PROCESS_DATABASE_URL"))
	if err != nil {
		t.Fatal("open process database")
	}
	defer pool.Close()
	witness, err := store.NewWitnessedRecordSubjectTombstoneReader("", recordauth.ProjectIDDefault, nil, pool)
	if err != nil {
		t.Fatal(err)
	}
	repository := store.NewPostgresLocalBlobRepository(pool, witness, nil)
	var waited sync.Once
	blob, err := attachments.OpenLocalBlobProcessFixture(ctx, os.Getenv("HOUFENG_LOCAL_PROCESS_ROOT"), repository, func(point string) error {
		if mode == "cancel-wait" && point == "guard-waiting" {
			var waitErr error
			waited.Do(func() { waitErr = barrier('W'); cancel() })
			return waitErr
		}
		if point == os.Getenv("HOUFENG_LOCAL_PROCESS_CUTPOINT") {
			return barrier('B')
		}
		return nil
	})
	if err != nil {
		t.Fatalf("open process Blob store: %v", err)
	}
	defer blob.Close()
	switch mode {
	case "hold", "cancel-wait":
		err = blob.WithKeyGuard(ctx, os.Getenv("HOUFENG_LOCAL_PROCESS_KEY"), func(context.Context, *attachments.LocalKeyGuard) error {
			if mode == "cancel-wait" {
				return errors.New("contended waiter entered guard")
			}
			return barrier('H')
		})
		if mode == "cancel-wait" && errors.Is(err, context.Canceled) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
	case "put", "put-collision":
		content := []byte(os.Getenv("HOUFENG_LOCAL_PROCESS_CONTENT"))
		digest := sha256.Sum256(content)
		digestText := hex.EncodeToString(digest[:])
		writer, err := attachments.NewManagedBlobWriter(blob, attachments.BackendKindLocal, repository)
		if err != nil {
			t.Fatal(err)
		}
		_, err = writer.Put(ctx, attachments.WriteRequest{ProjectID: "default", Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))},
			Origin: attachments.WriteOrigin{Kind: "import_archive", ID: os.Getenv("HOUFENG_LOCAL_PROCESS_JOB"), Generation: 1, ArchiveDigest: &digestText}}, bytes.NewReader(content))
		if mode == "put-collision" {
			if !errors.Is(err, os.ErrExist) {
				t.Fatalf("temporary-name collision = %v, want existing-file error", err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
	case "cleanup", "cleanup-collision":
		var request attachments.LocalCleanupPreparation
		if err := json.Unmarshal([]byte(os.Getenv("HOUFENG_LOCAL_PROCESS_CLEANUP")), &request); err != nil {
			t.Fatal(err)
		}
		coordinator, err := attachments.NewLocalCleanupCoordinator(blob, repository)
		if err != nil {
			t.Fatal(err)
		}
		_, err = coordinator.Cleanup(ctx, request, os.Getenv("HOUFENG_LOCAL_PROCESS_OWNER"))
		if mode == "cleanup-collision" {
			if !errors.Is(err, attachments.ErrLocalIdentityChanged) {
				t.Fatalf("foreign temporary cleanup = %v, want identity conflict", err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("unknown local process mode")
	}
}
