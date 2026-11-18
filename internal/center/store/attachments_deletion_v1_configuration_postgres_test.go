package store

import (
	"fmt"
	"testing"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

type v1ConfigurationFacts struct {
	targetAttachments, targetRefs, survivorAttachments, survivorRefs int
	blobs, claims, receipts                                          int
	logical, reserved, physical, quotaVersion                        int64
}

func v1ConfigurationReadFacts(t *testing.T, f *v1AuthorityFixture, survivor string) v1ConfigurationFacts {
	t.Helper()
	var x v1ConfigurationFacts
	err := f.db.db.QueryRow(f.ctx, `select
		(select count(*)::int from public.record_attachments where record_id=$1),
		(select count(*)::int from public.record_revision_attachments where record_id=$1),
		(select count(*)::int from public.record_attachments where record_id=$2),
		(select count(*)::int from public.record_revision_attachments where record_id=$2),
		(select count(*)::int from public.blob_objects where blob_key=$3 and object_version=$4),
		(select count(*)::int from public.blob_gc_deletions where operation_id=$5),
		(select count(*)::int from public.attachment_purge_receipts where operation_id=$5),
		logical_bytes,reserved_bytes,physical_bytes,quota_version
		from public.attachment_quota_accounts where project_id='default'`,
		f.target.Operation.Object.ObjectID, survivor, f.object.Key, f.object.VersionID, f.target.Operation.OperationID).
		Scan(&x.targetAttachments, &x.targetRefs, &x.survivorAttachments, &x.survivorRefs,
			&x.blobs, &x.claims, &x.receipts, &x.logical, &x.reserved, &x.physical, &x.quotaVersion)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func v1ConfigurationUnconfiguredAdapter(t *testing.T, f *v1AuthorityFixture) *attachments.DeletionAdapter {
	t.Helper()
	adapter, err := attachments.NewDeletionAdapter(NewPostgresAttachmentRepository(f.worker))
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func TestPostgresMinIOIntegrationAttachmentV1BackendConfiguration(t *testing.T) {
	t.Run("shared object requires configured backend before commit", func(t *testing.T) {
		f := newV1AuthorityFixture(t)
		runtime := f.db.openDirectRuntimePool(t, f.ctx, "v1-config-survivor", 2)
		fixture := ImportAttachmentGCFixture{Runtime: runtime, Gate: f.db.admissionGate}
		records := newRevisionMutationAdmittedRepository(t, fixture)
		survivor, err := records.CommitRevision(f.ctx, revisionBirthBatchCommand(t, f.ctx, fixture, recordsPostgresCompleteRevisionInput(t, "V1 shared survivor"), "v1-config-survivor"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.db.Exec(f.ctx, `insert into public.record_attachments
			(attachment_id,project_id,record_id,attachment_state,display_name,media_type,logical_size_bytes,blob_key,blob_object_version,created_by)
			values ('att_v1configsurvivor','default',$1,'available','survivor.txt','text/plain',$2,$3,$4,'usr_records1')`,
			survivor.RecordID, f.object.SizeBytes, f.object.Key, f.object.VersionID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.db.Exec(f.ctx, `insert into public.record_revision_attachments (record_id,revision_id,ordinal,attachment_id)
			values ($1,$2,0,'att_v1configsurvivor')`, survivor.RecordID, survivor.RevisionID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.db.Exec(f.ctx, `update public.attachment_quota_accounts set logical_bytes=logical_bytes+$1 where project_id='default'`, f.object.SizeBytes); err != nil {
			t.Fatal(err)
		}
		before := v1ConfigurationReadFacts(t, f, survivor.RecordID)
		if before.targetAttachments != 1 || before.targetRefs != 1 || before.survivorAttachments != 1 || before.survivorRefs != 1 || before.blobs != 1 || before.claims != 0 || before.receipts != 0 || before.logical != 2*f.object.SizeBytes || before.physical != f.object.SizeBytes {
			t.Fatalf("invalid shared fixture: %+v", before)
		}
		unconfigured := v1ConfigurationUnconfiguredAdapter(t, f)
		if _, err := unconfigured.PurgeDeletion(f.ctx, f.target); err == nil {
			t.Fatal("unconfigured shared S3 purge succeeded")
		}
		if after := v1ConfigurationReadFacts(t, f, survivor.RecordID); after != before {
			t.Fatalf("rejected purge changed durable state: before=%+v after=%+v", before, after)
		}
		f.survivors(t, false)
		calls := 0
		observer := &attachmentDeletionObservingBlobStore{BlobGCDeleter: f.blob, beforeDelete: func(attachments.ObjectVersion) error {
			calls++
			return fmt.Errorf("shared object must not be deleted")
		}}
		_, configured := f.adapter(t, observer)
		receipt, err := configured.PurgeDeletion(f.ctx, f.target)
		if err != nil {
			t.Fatal(err)
		}
		if err := configured.VerifyDeletion(f.ctx, f.target, receipt); err != nil {
			t.Fatal(err)
		}
		after := v1ConfigurationReadFacts(t, f, survivor.RecordID)
		if after.targetAttachments != 0 || after.targetRefs != 0 || after.survivorAttachments != 1 || after.survivorRefs != 1 || after.blobs != 1 || after.claims != 0 || after.receipts != 3 || after.logical != f.object.SizeBytes || after.reserved != before.reserved || after.physical != before.physical || after.quotaVersion != before.quotaVersion+1 || calls != 0 {
			t.Fatalf("configured shared purge: before=%+v after=%+v calls=%d", before, after, calls)
		}
		f.survivors(t, false)
	})

	t.Run("empty target needs no backend", func(t *testing.T) {
		f := newV1AuthorityFixture(t)
		runtime := f.db.openDirectRuntimePool(t, f.ctx, "v1-config-empty", 2)
		fixture := ImportAttachmentGCFixture{Runtime: runtime, Gate: f.db.admissionGate}
		records := newRevisionMutationAdmittedRepository(t, fixture)
		empty, err := records.CommitRevision(f.ctx, revisionBirthBatchCommand(t, f.ctx, fixture, recordsPostgresCompleteRevisionInput(t, "V1 empty target"), "v1-config-empty"))
		if err != nil {
			t.Fatal(err)
		}
		op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_v1configempty", ReservationID: "drs_v1configempty", Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: empty.RecordID}, ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
		f.target = seedVerifiedS3AttachmentDeletionOperation(t, f.ctx, f.db, empty, op)
		before := v1ConfigurationReadFacts(t, f, "")
		if before.targetAttachments != 0 || before.targetRefs != 0 || before.claims != 0 || before.receipts != 0 {
			t.Fatalf("target not empty: %+v", before)
		}
		adapter := v1ConfigurationUnconfiguredAdapter(t, f)
		receipt, err := adapter.PurgeDeletion(f.ctx, f.target)
		if err != nil {
			t.Fatal(err)
		}
		if err := adapter.VerifyDeletion(f.ctx, f.target, receipt); err != nil {
			t.Fatal(err)
		}
		after := v1ConfigurationReadFacts(t, f, "")
		before.receipts = 3
		if after != before || receipt.RemovedRowCount != 0 {
			t.Fatalf("empty purge changed unrelated state: want=%+v got=%+v receipt=%+v", before, after, receipt)
		}
		f.survivors(t, false)
	})

	t.Run("completed proof replay needs no backend or IO", func(t *testing.T) {
		f := newV1AuthorityFixture(t)
		calls := 0
		observer := &attachmentDeletionObservingBlobStore{BlobGCDeleter: f.blob, beforeDelete: func(attachments.ObjectVersion) error {
			calls++
			return nil
		}}
		_, configured := f.adapter(t, observer)
		receipt, err := configured.PurgeDeletion(f.ctx, f.target)
		if err != nil || calls != 1 {
			t.Fatalf("initial exact-version purge: calls=%d err=%v", calls, err)
		}
		before := v1ConfigurationReadFacts(t, f, "")
		if before.claims != 1 || before.receipts != 3 || before.logical != 0 || before.physical != 0 {
			t.Fatalf("initial purge facts: %+v", before)
		}
		unconfigured := v1ConfigurationUnconfiguredAdapter(t, f)
		replay, err := unconfigured.PurgeDeletion(f.ctx, f.target)
		if err != nil || replay != receipt {
			t.Fatalf("unconfigured completed replay: receipt=%+v err=%v", replay, err)
		}
		if err := unconfigured.VerifyDeletion(f.ctx, f.target, receipt); err != nil {
			t.Fatal(err)
		}
		if after := v1ConfigurationReadFacts(t, f, ""); after != before || calls != 1 {
			t.Fatalf("completed replay changed state: before=%+v after=%+v calls=%d", before, after, calls)
		}
		f.survivors(t, true)
	})
}

func TestAttachmentV1PreparationRequiredBackendsDecode(t *testing.T) {
	command := attachments.AttachmentDeletionCommand{}
	for _, tc := range []struct {
		name, backends string
		valid          bool
	}{
		{"empty", `[]`, true},
		{"s3", `["s3"]`, true},
		{"null", `null`, false},
		{"string", `"s3"`, false},
		{"object", `{}`, false},
		{"null element", `[null]`, false},
		{"boolean element", `[true]`, false},
		{"unsupported", `["local"]`, false},
		{"duplicate", `["s3","s3"]`, false},
		{"unsorted", `["s3","local"]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"counts":{"logical_attachment":0,"upload_part":0,"blob_object":0},"verified_at":"2026-01-01T00:00:00Z","claims":[],"required_backends":` + tc.backends + `}`)
			_, err := attachmentPurgeV1DecodePreparation(raw, command)
			if (err == nil) != tc.valid {
				t.Fatalf("decode backends %s: %v", tc.backends, err)
			}
		})
	}
}
