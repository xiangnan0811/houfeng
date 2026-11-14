package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordauth"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Integration fixtures provision through the direct admin role and reopen
// through the direct runtime role. Existing bindings are never regenerated.
func newPostgresLocalBlobStore(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, root string) (*attachments.LocalBlobStore, error) {
	t.Helper()
	runtime := NewPostgresLocalBlobRepository(fixture.openDirectRuntimePool(t, ctx, "local-blob-fixture-runtime", 2), nil, nil)
	_, err := runtime.LoadLocalNamespace(ctx, root)
	if errors.Is(err, attachments.ErrBlobNotFound) {
		adminPool := fixture.openDirectRolePool(t, ctx, fixture.admin, "local-blob-fixture-admin", 1)
		administrator := NewPostgresLocalBlobRepository(adminPool, nil, nil)
		_, err = attachments.ProvisionLocalNamespace(ctx, attachments.LocalProvisionRequest{
			Root: root, NamespaceID: localBlobFixtureUUID(t), IncarnationID: localBlobFixtureUUID(t), RequestID: localBlobFixtureUUID(t),
			OwnerUID: uint32(os.Geteuid()), OwnerGID: uint32(os.Getegid()),
		}, administrator)
		adminPool.Reset()
	}
	if err != nil {
		return nil, err
	}
	blob, err := attachments.NewLocalBlobStore(ctx, root, runtime)
	if err == nil {
		t.Cleanup(func() {
			if err := blob.Close(); err != nil {
				t.Errorf("close fixture local Blob store: %v", err)
			}
		})
	}
	return blob, err
}

func localBlobFixtureUUID(t *testing.T) string {
	t.Helper()
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatal(err)
	}
	value[6] = (value[6] & 15) | 64
	value[8] = (value[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

// A seed still has an actual admitted write origin and durable responsibility.
// Deleting only its fixture-owned import job hands that responsibility off, so
// the tested GC/deletion path must resolve it using the real cleanup protocol.
func seedPostgresManagedLocalBlob(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, blob *attachments.LocalBlobStore, content []byte) attachments.WrittenBlob {
	t.Helper()
	pool := fixture.openDirectRuntimePool(t, ctx, "local-seed-writer", 2)
	repository := NewPostgresLocalBlobRepository(pool, localBlobFixtureWitness(t, pool), nil)
	writer, err := attachments.NewManagedBlobWriter(blob, attachments.BackendKindLocal, repository)
	if err != nil {
		t.Fatal(err)
	}
	return seedPostgresManagedBlob(t, ctx, fixture, writer, content)
}

func localBlobFixtureWitness(t *testing.T, pool *pgxpool.Pool) *WitnessedRecordSubjectTombstoneReader {
	t.Helper()
	witness, err := NewWitnessedRecordSubjectTombstoneReader("", recordauth.ProjectIDDefault, nil, pool)
	if err != nil {
		t.Fatal(err)
	}
	return witness
}

func seedPostgresManagedBlob(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, writer attachments.BlobWriter, content []byte) attachments.WrittenBlob {
	t.Helper()
	digest := sha256.Sum256(content)
	digestText := hex.EncodeToString(digest[:])
	var randomID [16]byte
	if _, err := rand.Read(randomID[:]); err != nil {
		t.Fatal(err)
	}
	jobID := "rij_" + hex.EncodeToString(randomID[:])
	seedRecordsPostgresUsers(t, ctx, fixture)
	if _, err := fixture.db.Exec(ctx, `insert into public.vps_assets(vps_id,display_name,lifecycle_status,usage_status)
		values ($1,'Managed Blob fixture','active','in_use') on conflict(vps_id) do nothing`, testStoreRecordVPSID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.record_import_jobs(import_job_id,actor_id,idempotency_key,job_state,
		identity_classification,archive_digest,destination_subject_kind,destination_subject_source_id,expires_at)
		values($1,'usr_aaaaaaaaaaaaaaaaaaaaaaaa',$1,'uploaded','unknown',$2,'vps',$3,clock_timestamp()+interval '1 hour')`, jobID, digest[:], testStoreRecordVPSID); err != nil {
		t.Fatal(err)
	}
	temporaryKey, err := attachments.NewBlobTemporaryKey()
	if err != nil {
		t.Fatal(err)
	}
	written, err := writer.Put(ctx, attachments.WriteRequest{
		ProjectID: "default", Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content)), TemporaryKey: temporaryKey},
		Origin: attachments.WriteOrigin{Kind: "import_archive", ID: jobID, Generation: 1, ArchiveDigest: &digestText},
	}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `delete from public.record_import_jobs where import_job_id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	return written
}

// ImportDestinationFixture keeps administrative setup private while the
// external-package workflows exercise the admitted runtime role.
type ImportDestinationFixture struct {
	Runtime                *pgxpool.Pool
	Blobs                  attachments.BlobStore
	Writer                 attachments.BlobWriter
	ClearLegacyDestination func(context.Context, string) error
	DeleteTarget           func(context.Context, string) error
}

func NewImportDestinationFixture(t *testing.T, ctx context.Context) ImportDestinationFixture {
	t.Helper()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtime := fixture.openDirectRuntimePool(t, ctx, "import-destination-runtime", 8)
	blob, err := newPostgresLocalBlobStore(t, ctx, fixture, filepath.Join(t.TempDir(), "blob-root"))
	if err != nil {
		t.Fatal(err)
	}
	writer, err := attachments.NewManagedBlobWriter(blob, attachments.BackendKindLocal, NewPostgresLocalBlobRepository(runtime, localBlobFixtureWitness(t, runtime), nil))
	if err != nil {
		t.Fatal(err)
	}
	return ImportDestinationFixture{
		Runtime: runtime, Blobs: blob, Writer: writer,
		ClearLegacyDestination: func(ctx context.Context, jobID string) error {
			_, err := fixture.db.Exec(ctx, `update public.record_import_jobs
				set destination_subject_kind=null, destination_subject_source_id=null where import_job_id=$1`, jobID)
			return err
		},
		DeleteTarget: func(ctx context.Context, targetID string) error {
			_, err := fixture.db.Exec(ctx, `delete from public.targets where target_id=$1`, targetID)
			return err
		},
	}
}
