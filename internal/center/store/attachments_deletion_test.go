package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

func TestConfigureAttachmentDeletionBlobStoreRejectsTypedNil(t *testing.T) {
	t.Parallel()

	repository := &PostgresAttachmentRepository{}
	var typedNil *attachments.LocalBlobStore
	if err := repository.ConfigureAttachmentDeletionBlobStore(attachments.BackendKindLocal, typedNil); !errors.Is(err, attachments.ErrInvalidDeletionAdapter) {
		t.Fatalf("ConfigureAttachmentDeletionBlobStore(typed nil) error = %v, want ErrInvalidDeletionAdapter", err)
	}
}

func TestPostgresAttachmentDeletionHealthAndPreviewAreDeterministic(t *testing.T) {
	t.Parallel()

	tx := &attachmentDeletionReadTx{
		healthCount:        int64(len(recorddeletion.RecordAttachmentsSurfaceNames())),
		dependencyMaterial: []byte(`{"attachments":[["att_one"]]}`),
		impactMaterial:     []byte(`{"attachment_count":1}`),
		survivingCopies:    2,
	}
	repository := &PostgresAttachmentRepository{
		beginTx: func(context.Context, pgx.TxOptions) (attachmentTx, error) { return tx, nil },
	}
	health, err := repository.AttachmentDeletionHealth(context.Background())
	if err != nil {
		t.Fatalf("AttachmentDeletionHealth() error = %v", err)
	}
	if !health.Healthy() || health.Revision() != 1 || health.ProofDigest() == ([32]byte{}) {
		t.Fatalf("AttachmentDeletionHealth() = healthy:%t revision:%d proof:%x", health.Healthy(), health.Revision(), health.ProofDigest())
	}
	target := recorddeletion.PreviewTarget{
		Object: recordplatform.ObjectRef{
			ProjectID: "default", ObjectKind: "record", ObjectID: "rec_previewone",
		},
		CurrentRevisionID:     "rrv_previewone",
		LockVersion:           3,
		AuthorizationEpoch:    4,
		ContentDeliveryEpoch:  5,
		DependencyGraphDigest: testStoreRecordPlatformDigest(0x91),
	}
	preview, err := repository.PreviewAttachmentDeletion(context.Background(), target)
	if err != nil {
		t.Fatalf("PreviewAttachmentDeletion() error = %v", err)
	}
	if preview.Validate() != nil || preview.DependencyDigest == ([32]byte{}) || preview.ImpactDigest == ([32]byte{}) {
		t.Fatalf("PreviewAttachmentDeletion() = %#v", preview)
	}
	wantCopies := []recorddeletion.AdapterSurvivingCopy{{
		Kind: recorddeletion.SurvivingCopyKindOtherRecord, CopyCount: 2,
	}}
	if !reflect.DeepEqual(preview.SurvivingCopies, wantCopies) {
		t.Fatalf("SurvivingCopies = %#v, want %#v", preview.SurvivingCopies, wantCopies)
	}
	if tx.healthNames == nil || !reflect.DeepEqual(tx.healthNames, recorddeletion.RecordAttachmentsSurfaceNames()) {
		t.Fatalf("health names = %#v", tx.healthNames)
	}
	if tx.commits != 2 {
		t.Fatalf("commits = %d, want 2", tx.commits)
	}
}

type attachmentDeletionReadTx struct {
	pgx.Tx
	healthCount        int64
	dependencyMaterial []byte
	impactMaterial     []byte
	survivingCopies    int64
	healthNames        []recorddeletion.SurfaceName
	commits            int
}

func (tx *attachmentDeletionReadTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected attachment deletion read exec")
}

func (tx *attachmentDeletionReadTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	switch {
	case strings.Contains(compact, "from pg_catalog.pg_class"):
		names, ok := args[0].([]string)
		if !ok {
			return fakeAttachmentRow{err: errors.New("unexpected health names")}
		}
		tx.healthNames = make([]recorddeletion.SurfaceName, len(names))
		for index, name := range names {
			tx.healthNames[index] = recorddeletion.SurfaceName(name)
		}
		return fakeAttachmentRow{values: []any{tx.healthCount}}
	case strings.Contains(compact, "attachment_dependency_material"):
		return fakeAttachmentRow{values: []any{tx.dependencyMaterial, tx.impactMaterial, tx.survivingCopies}}
	default:
		return fakeAttachmentRow{err: errors.New("unexpected attachment deletion read query")}
	}
}

func (tx *attachmentDeletionReadTx) Commit(context.Context) error {
	tx.commits++
	return nil
}

func (tx *attachmentDeletionReadTx) Rollback(context.Context) error { return nil }
