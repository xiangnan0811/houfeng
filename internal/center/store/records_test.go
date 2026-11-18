package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/records"
)

func testRecordRevisionDigest(value byte) [32]byte {
	var digest [32]byte
	for index := range digest {
		digest[index] = value
	}
	return digest
}

type storeRevisionParticipantStub struct {
	name      string
	apply     func(context.Context, pgx.Tx, records.RevisionCommitted) error
	resources func(context.Context, pgx.Tx, records.RevisionCommitCommand) (records.RevisionMutationResources, error)
}

func (participant *storeRevisionParticipantStub) Name() string {
	return participant.name
}

func (participant *storeRevisionParticipantStub) MutationResources(ctx context.Context, tx pgx.Tx, command records.RevisionCommitCommand) (records.RevisionMutationResources, error) {
	if participant.resources != nil {
		return participant.resources(ctx, tx, command)
	}
	// Arbitrary test callbacks may write fixture-only relations outside the
	// closed production families. Never silently advertise an empty footprint.
	return records.RevisionMutationResources{}, records.ErrInvalidRevisionParticipant
}

func (participant *storeRevisionParticipantStub) ApplyRevision(ctx context.Context, tx pgx.Tx, committed records.RevisionCommitted) error {
	return participant.apply(ctx, tx, committed)
}

type fakeRecordRevisionTx struct {
	pgx.Tx
	now                           time.Time
	steps                         *[]string
	failAt                        string
	failed                        bool
	admissionCalls                int
	rootExists                    bool
	rootLifecycle                 string
	currentRevisionID             *string
	currentRevisionNo             *int64
	rootLockVersion               int64
	rootAuthorizationEpoch        int64
	currentCanonicalHash          []byte
	currentRevisionCreatedAt      *time.Time
	insertedRevisionNo            int64
	projectedLockVersion          int64
	projectedAuthorizationEpoch   int64
	domainActivityKind            string
	outboxEventKind               string
	lifecycleTarget               string
	lifecycleLockVersion          int64
	lifecycleAuthorizationEpoch   int64
	idempotencyRequestFingerprint []byte
	idempotencyResultFingerprint  []byte
	replayRevisionID              string
	replayRevisionNo              int64
	replayRevisionCreatedAt       time.Time
	replayRevisionCreated         bool
	replayLifecycleRevisionID     string
	replayLifecycleActivityKind   string
	replayLifecycleLockVersion    int64
	replayLifecycleAuthEpoch      int64
	replayLifecycleChangedAt      time.Time
	publishedDraft                *records.Draft
}

func (tx *fakeRecordRevisionTx) Commit(context.Context) error {
	*tx.steps = append(*tx.steps, "commit")
	if tx.shouldFail("commit") {
		return errRecordRevisionCutPoint
	}
	return nil
}

func (tx *fakeRecordRevisionTx) Rollback(context.Context) error {
	*tx.steps = append(*tx.steps, "rollback_cleanup")
	return nil
}

func (tx *fakeRecordRevisionTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	step := recordRevisionSQLStep(sql)
	*tx.steps = append(*tx.steps, step)
	if tx.shouldFail(step) {
		return pgconn.CommandTag{}, errRecordRevisionCutPoint
	}
	if step == "current_projection" {
		tx.projectedLockVersion = args[13].(int64)
		tx.projectedAuthorizationEpoch = args[14].(int64)
	}
	switch step {
	case "root_create":
		if tx.rootExists {
			return pgconn.NewCommandTag("INSERT 0 0"), nil
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case "fence_epoch_init", "revision_subject", "revision_tag", "revision_participant", "current_projection", "idempotency_complete":
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	default:
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
}

func (tx *fakeRecordRevisionTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	step := recordRevisionSQLStep(sql)
	*tx.steps = append(*tx.steps, step)
	if tx.shouldFail(step) {
		return fakeRecordRevisionRow{err: errRecordRevisionCutPoint}
	}
	switch step {
	case "idempotency_lock":
		if len(tx.idempotencyRequestFingerprint) == 0 {
			return fakeRecordRevisionRow{err: pgx.ErrNoRows}
		}
		return fakeRecordRevisionRow{values: []any{
			tx.idempotencyRequestFingerprint,
			tx.idempotencyResultFingerprint,
			"completed",
			"",
			int64(0),
			nil,
			tx.now.Add(24 * time.Hour),
			tx.now,
		}}
	case "fence_reservation_lock", "fence_lease_lock", "fence_reservation_recheck", "draft_attachment_lookup":
		return fakeRecordRevisionRow{err: pgx.ErrNoRows}
	case "idempotency_claim":
		return fakeRecordRevisionRow{values: []any{"records_api_01", int64(1), tx.now.Add(time.Minute)}}
	case "fence_epoch_lock":
		return fakeRecordRevisionRow{values: []any{int64(0)}}
	case "root_lock":
		lifecycle := tx.rootLifecycle
		if lifecycle == "" {
			lifecycle = string(records.LifecycleActive)
		}
		values := []any{
			"rec_order", "default", lifecycle, tx.currentRevisionID,
			tx.rootLockVersion, tx.rootAuthorizationEpoch, tx.currentCanonicalHash,
		}
		compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
		if strings.Contains(compact, "current_revision.revision_no") {
			values = append(values, tx.currentRevisionNo, tx.currentRevisionCreatedAt)
		}
		return fakeRecordRevisionRow{values: values}
	case "draft_lock":
		if tx.publishedDraft == nil {
			return fakeRecordRevisionRow{err: pgx.ErrNoRows}
		}
		draft := *tx.publishedDraft
		var recordID *string
		var baseRevisionID *string
		if draft.RecordID != "" {
			recordIDValue := draft.RecordID
			baseRevisionIDValue := draft.BaseRevisionID
			recordID = &recordIDValue
			baseRevisionID = &baseRevisionIDValue
		}
		payloadHash := draft.Payload.Hash()
		etagDigest, err := draft.ETag.Digest()
		if err != nil {
			return fakeRecordRevisionRow{err: err}
		}
		return fakeRecordRevisionRow{values: []any{
			draft.DraftID,
			string(draft.ProjectID),
			recordID,
			baseRevisionID,
			draft.AuthorID,
			draft.Payload.JSON(),
			append([]byte(nil), payloadHash[:]...),
			int64(draft.Version),
			append([]byte(nil), etagDigest[:]...),
			draft.WarningAt,
			draft.CreatedAt,
			draft.UpdatedAt,
			draft.ExpiresAt,
		}}
	case "revision_insert":
		tx.insertedRevisionNo = args[4].(int64)
		return fakeRecordRevisionRow{values: []any{tx.now}}
	case "domain_activity":
		tx.domainActivityKind = args[4].(string)
		return fakeRecordRevisionRow{values: []any{tx.now}}
	case "lifecycle_update":
		tx.lifecycleTarget = args[1].(string)
		tx.lifecycleLockVersion = args[2].(int64)
		tx.lifecycleAuthorizationEpoch = args[3].(int64)
		return fakeRecordRevisionRow{values: []any{tx.now}}
	case "outbox":
		tx.outboxEventKind = fmt.Sprint(args[1])
		return fakeRecordRevisionRow{values: []any{int64(41)}}
	case "revision_replay":
		return fakeRecordRevisionRow{values: []any{
			tx.replayRevisionID,
			tx.replayRevisionNo,
			tx.replayRevisionCreatedAt,
			tx.replayRevisionCreated,
		}}
	case "lifecycle_replay":
		return fakeRecordRevisionRow{values: []any{
			tx.replayLifecycleRevisionID,
			tx.replayLifecycleActivityKind,
			tx.replayLifecycleLockVersion,
			tx.replayLifecycleAuthEpoch,
			tx.replayLifecycleChangedAt,
		}}
	default:
		return fakeRecordRevisionRow{err: errors.New("unexpected query row step: " + step)}
	}
}

func (tx *fakeRecordRevisionTx) shouldFail(step string) bool {
	if tx.failed || tx.failAt != step {
		return false
	}
	tx.failed = true
	return true
}

type fakeRecordRevisionRow struct {
	values []any
	err    error
}

func (row fakeRecordRevisionRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != len(row.values) {
		return errors.New("unexpected scan destination count")
	}
	for index := range dest {
		if row.values[index] == nil {
			continue
		}
		target := reflect.ValueOf(dest[index])
		value := reflect.ValueOf(row.values[index])
		if target.Kind() != reflect.Pointer || !value.Type().AssignableTo(target.Elem().Type()) {
			return errors.New("unexpected scan destination type")
		}
		target.Elem().Set(value)
	}
	return nil
}

func recordRevisionSQLStep(sql string) string {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	switch {
	case strings.Contains(compact, "select request_fingerprint"):
		return "idempotency_lock"
	case strings.Contains(compact, "insert into public.record_idempotency_keys"):
		return "idempotency_claim"
	case strings.Contains(compact, "from public.deletion_reservations") && strings.Contains(compact, "for update"):
		return "fence_reservation_lock"
	case strings.Contains(compact, "insert into public.content_delivery_epochs"):
		return "fence_epoch_init"
	case strings.Contains(compact, "from public.content_delivery_epochs"):
		return "fence_epoch_lock"
	case strings.Contains(compact, "from public.deletion_fence_leases"):
		return "fence_lease_lock"
	case strings.Contains(compact, "from public.deletion_reservations"):
		return "fence_reservation_recheck"
	case strings.Contains(compact, "insert into public.records"):
		return "root_create"
	case strings.Contains(compact, "from public.records") && strings.Contains(compact, "for update"):
		return "root_lock"
	case strings.Contains(compact, "from public.record_drafts") && strings.Contains(compact, "for update"):
		return "draft_lock"
	case strings.Contains(compact, "insert into public.record_revisions"):
		return "revision_insert"
	case strings.Contains(compact, "from public.record_revisions"):
		return "revision_replay"
	case strings.Contains(compact, "insert into public.record_revision_subjects"):
		return "revision_subject"
	case strings.Contains(compact, "insert into public.record_revision_tags"):
		return "revision_tag"
	case strings.Contains(compact, "insert into public.record_revision_participants"):
		return "revision_participant"
	case strings.Contains(compact, "update public.records") && strings.Contains(compact, "set lifecycle"):
		return "lifecycle_update"
	case strings.Contains(compact, "update public.records"):
		return "current_projection"
	case strings.Contains(compact, "update public.record_search_documents"):
		return "search_lifecycle"
	case strings.Contains(compact, "insert into public.record_domain_activities"):
		return "domain_activity"
	case strings.Contains(compact, "from public.record_domain_activities"):
		return "lifecycle_replay"
	case strings.Contains(compact, "insert into public.record_outbox"):
		return "outbox"
	case strings.Contains(compact, "from public.record_attachments") && strings.Contains(compact, "where draft_id = $1"):
		return "draft_attachment_lookup"
	case strings.Contains(compact, "delete from public.record_draft_checkpoints"):
		return "draft_checkpoint_delete"
	case strings.Contains(compact, "delete from public.record_drafts"):
		return "draft_delete"
	case strings.Contains(compact, "update public.record_idempotency_keys"):
		return "idempotency_complete"
	default:
		return "unexpected_sql"
	}
}

var errRecordRevisionCutPoint = errors.New("record revision cut point failed")
