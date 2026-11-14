package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordbirth"
)

func TestPostgresIntegrationImportMappingBirthAllocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	var columnType string
	var notNull, hasDefault bool
	if err := fixture.db.QueryRow(ctx, `select format_type(atttypid,atttypmod),attnotnull,atthasdef
		from pg_catalog.pg_attribute where attrelid='public.record_import_entity_mappings'::regclass
		and attname='birth_allocation_id' and not attisdropped`).Scan(&columnType, &notNull, &hasDefault); err != nil || columnType != "text" || notNull || hasDefault {
		t.Fatalf("mapping birth column: type=%s not-null=%v default=%v error=%v", columnType, notNull, hasDefault, err)
	}
	if err := fixture.db.QueryRow(ctx, `select format_type(atttypid,atttypmod),attnotnull,atthasdef
		from pg_catalog.pg_attribute where attrelid='public.record_import_entity_mappings'::regclass
		and attname='owner_target_record_id' and not attisdropped`).Scan(&columnType, &notNull, &hasDefault); err != nil || columnType != "text" || notNull || hasDefault {
		t.Fatalf("mapping owner column: type=%s not-null=%v default=%v error=%v", columnType, notNull, hasDefault, err)
	}
	var canInsert bool
	if err := runtime.QueryRow(ctx, `select has_table_privilege(current_user,'public.record_import_entity_mappings','insert')`).Scan(&canInsert); err != nil || canInsert {
		t.Fatalf("runtime bypass privilege = %v, %v", canInsert, err)
	}
	const job = "rij_birthmapping"
	const plan = "rip_birthmapping"
	const source = "rec_legacyarchive"
	const actor = "usr_aaaaaaaaaaaaaaaaaaaaaaaa"
	now := time.Now().UTC()
	archive := sha256.Sum256([]byte("import birth mapping archive"))
	planDigest := sha256.Sum256([]byte("import birth mapping plan"))
	if _, err := fixture.db.Exec(ctx, `insert into public.record_import_jobs
		(import_job_id,project_id,actor_id,idempotency_key,job_state,identity_classification,archive_digest,expires_at,created_at,updated_at)
		values($1,'default',$2,'mapping-job','quarantined','unknown',$3,$4,$5,$5)`, job, actor, archive[:], now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.record_import_plans
		(import_plan_id,import_job_id,plan_digest,object_count,remap_count,expires_at,created_at)
		values($1,$2,$3,3,3,$4,$5)`, plan, job, planDigest[:], now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	// A predecessor row survives the nullable column addition without acquiring
	// a retroactive birth allocation.
	legacyDigest := sha256.Sum256([]byte("rec_oldstored"))
	if _, err := fixture.db.Exec(ctx, `insert into public.record_import_entity_mappings
		(import_plan_id,entity_kind,source_id,source_identity_digest,target_id,created_at)
		values($1,'record','rec_oldstored',$2,'rec_oldtarget',$3)`, plan, legacyDigest[:], now); err != nil {
		t.Fatal(err)
	}
	var legacyNull bool
	if err := runtime.QueryRow(ctx, `select birth_allocation_id is null and owner_target_record_id is null from public.record_import_entity_mappings
		where import_plan_id=$1 and source_id='rec_oldstored'`, plan).Scan(&legacyNull); err != nil || !legacyNull {
		t.Fatalf("legacy mapping acquired authority: %v %v", legacyNull, err)
	}

	allocationKey := func(jobID, sourceID string) string {
		t.Helper()
		body, err := json.Marshal(struct{ Domain, JobID, SourceID string }{"houfeng.record-import-allocation-key.v1", jobID, sourceID})
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(body)
		return "import-" + hex.EncodeToString(digest[:])
	}
	repository := NewPostgresRecordBirthRepository(runtime, gate, nil)
	allocate := func(owner string, purpose recordbirth.Purpose, jobID string) recordbirth.Allocation {
		t.Helper()
		request := recordbirth.Request{ProjectID: "default", ActorID: owner, ActorScopeDigest: sha256.Sum256([]byte("mapping scope")),
			Purpose: purpose, IdempotencyKey: allocationKey(jobID, source), RequestFingerprint: sha256.Sum256([]byte("mapping request"))}
		allocation, err := repository.BeginAllocation(ctx, request, nil, nil)
		if err != nil {
			t.Fatalf("allocate: %v", err)
		}
		if _, err := runtime.Exec(ctx, `insert into public.content_delivery_epochs(project_id,object_kind,object_id,delivery_epoch)
			values('default','record',$1,0) on conflict do nothing`, allocation.RecordID); err != nil {
			t.Fatal(err)
		}
		return allocation
	}
	good := allocate(actor, recordbirth.PurposeImport, job)
	wrongOwner := allocate("usr_bbbbbbbbbbbbbbbbbbbbbbbb", recordbirth.PurposeImport, job)
	wrongPurpose := allocate(actor, recordbirth.PurposeCreate, job)
	wrongJob := allocate(actor, recordbirth.PurposeImport, "rij_otherjob")
	encodeMapping := func(kind, sourceID, target, allocation string, owners ...string) []byte {
		t.Helper()
		owner := ""
		if len(owners) != 0 {
			owner = owners[0]
		}
		digest := sha256.Sum256([]byte(sourceID))
		body, err := json.Marshal(struct {
			PlanID              string    `json:"import_plan_id"`
			Kind                string    `json:"entity_kind"`
			Source              string    `json:"source_id"`
			Digest              string    `json:"source_identity_digest"`
			Target              string    `json:"target_id"`
			CreatedAt           time.Time `json:"created_at"`
			Allocation          string    `json:"birth_allocation_id"`
			OwnerTargetRecordID string    `json:"owner_target_record_id"`
		}{plan, kind, sourceID, hex.EncodeToString(digest[:]), target, now, allocation, owner})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	for name, tc := range map[string]struct{ target, allocation string }{
		"owner":        {wrongOwner.RecordID, wrongOwner.AllocationID},
		"purpose":      {wrongPurpose.RecordID, wrongPurpose.AllocationID},
		"target":       {wrongOwner.RecordID, good.AllocationID},
		"original job": {wrongJob.RecordID, wrongJob.AllocationID},
	} {
		t.Run(name, func(t *testing.T) {
			tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = tx.Exec(ctx, `select public.record_import_mapping_insert($1)`, encodeMapping("record", source, tc.target, tc.allocation))
			birthRequireSQLState(t, err, "55000")
		})
	}
	validCommand := encodeMapping("record", source, good.RecordID, good.AllocationID)
	duplicate := append(validCommand[:len(validCommand)-1], []byte(`,"birth_allocation_id":"`+good.AllocationID+`"}`)...)
	for name, body := range map[string][]byte{
		"non-record allocation": encodeMapping("attachment", "att_source", "att_target", good.AllocationID),
		"duplicate allocation":  duplicate,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runtime.Exec(ctx, `select public.record_import_mapping_insert($1)`, body)
			birthRequireSQLState(t, err, "22023")
		})
	}
	lock, err := json.Marshal(map[string]string{"project_id": good.Request.ProjectID, "actor_id": good.Request.ActorID,
		"actor_scope_digest": hex.EncodeToString(good.Request.ActorScopeDigest[:]), "purpose": string(good.Request.Purpose),
		"idempotency_key": good.Request.IdempotencyKey, "request_fingerprint": hex.EncodeToString(good.Request.RequestFingerprint[:]),
		"allocation_id": good.AllocationID, "record_id": good.RecordID})
	if err != nil {
		t.Fatal(err)
	}
	changedDigest := sha256.Sum256([]byte("different immutable request"))
	for field, replacement := range map[string]string{
		"actor_id": "usr_bbbbbbbbbbbbbbbbbbbbbbbb", "actor_scope_digest": hex.EncodeToString(changedDigest[:]),
		"purpose": "comparison_save", "idempotency_key": "different-key", "request_fingerprint": hex.EncodeToString(changedDigest[:]),
		"record_id": wrongOwner.RecordID,
	} {
		t.Run("allocation "+field, func(t *testing.T) {
			var changed map[string]string
			if err := json.Unmarshal(lock, &changed); err != nil {
				t.Fatal(err)
			}
			changed[field] = replacement
			body, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			_, err = runtime.Exec(ctx, `select public.record_birth_allocation_lock($1)`, body)
			birthRequireSQLState(t, err, "55000")
		})
	}
	// A direct late mapping must restart immediately rather than waiting for a
	// lower-rank record scope held by another transaction.
	holder, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `select public.record_birth_allocation_lock($1)`, lock); err != nil {
		t.Fatal(err)
	}
	contender, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		_ = holder.Rollback(ctx)
		t.Fatal(err)
	}
	_, err = contender.Exec(ctx, `select public.record_import_mapping_insert($1)`, encodeMapping("record", source, good.RecordID, good.AllocationID))
	birthRequireSQLState(t, err, "40001")
	_ = contender.Rollback(ctx)
	_ = holder.Rollback(ctx)
	tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select public.record_birth_allocation_lock($1)`, lock); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `select public.record_import_plan_lock($1)`, []byte(`{"import_job_id":"rij_birthmapping","namespace_ids":""}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `select public.record_import_mapping_insert($1)`, encodeMapping("record", source, good.RecordID, good.AllocationID)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `select public.record_import_mapping_insert($1)`, encodeMapping("attachment", "att_archive", "att_target", "", good.RecordID)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `select public.record_import_mapping_insert($1)`, encodeMapping("evidence", "evs_archive", "evs_target", "", good.RecordID)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var gotAllocation, attachmentOwner, evidenceOwner string
	var nonRecordNull bool
	if err := runtime.QueryRow(ctx, `select
		(select birth_allocation_id from public.record_import_entity_mappings where import_plan_id=$1 and entity_kind='record' and source_id=$2),
		(select birth_allocation_id is null from public.record_import_entity_mappings where import_plan_id=$1 and entity_kind='attachment'),
		(select owner_target_record_id from public.record_import_entity_mappings where import_plan_id=$1 and entity_kind='attachment'),
		(select owner_target_record_id from public.record_import_entity_mappings where import_plan_id=$1 and entity_kind='evidence')`, plan, source).Scan(&gotAllocation, &nonRecordNull, &attachmentOwner, &evidenceOwner); err != nil || gotAllocation != good.AllocationID || !nonRecordNull || attachmentOwner != good.RecordID || evidenceOwner != good.RecordID {
		t.Fatalf("stored mapping allocation=%q non-record-null=%v attachment-owner=%q evidence-owner=%q error=%v", gotAllocation, nonRecordNull, attachmentOwner, evidenceOwner, err)
	}
	for name, tc := range map[string]struct {
		body  []byte
		state string
	}{
		"missing evidence owner":      {encodeMapping("evidence", "evs_missing", "evs_missing", ""), "22023"},
		"missing attachment owner":    {encodeMapping("attachment", "att_missing", "att_missing", ""), "22023"},
		"wrong record owner":          {encodeMapping("attachment", "att_other", "att_other", "", wrongOwner.RecordID), "55000"},
		"wrong evidence owner":        {encodeMapping("evidence", "evs_other", "evs_other", "", wrongOwner.RecordID), "55000"},
		"owner on record":             {encodeMapping("record", "rec_newsource", good.RecordID, good.AllocationID, good.RecordID), "22023"},
		"duplicate attachment target": {encodeMapping("attachment", "att_another", "att_target", "", good.RecordID), "23505"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runtime.Exec(ctx, `select public.record_import_mapping_insert($1)`, tc.body)
			birthRequireSQLState(t, err, tc.state)
		})
	}
}
