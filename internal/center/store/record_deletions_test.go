package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

func TestPostgresRecordDeletionCoreHealthAndPreviewUseAdmittedContentFreeSnapshots(t *testing.T) {
	t.Parallel()

	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "as record_core_surface_count"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*int64) = int64(len(recorddeletion.RecordCoreSurfaceNames()))
				return nil
			}}
		case strings.Contains(sql, "as core_dependency_material"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*[]byte) = []byte(`{"revisions":["rrv_corepreview01"]}`)
				*dest[1].(*[]byte) = []byte(`{"revision_count":1,"draft_count":0}`)
				return nil
			}}
		default:
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		}
	}
	repository := testStoreDeletionRepository(tx)

	health, err := repository.RecordCoreHealth(context.Background())
	if err != nil {
		t.Fatalf("RecordCoreHealth() error = %v", err)
	}
	if !health.Healthy() || health.Revision() != 1 || health.ProofDigest() == ([32]byte{}) {
		t.Fatalf("RecordCoreHealth() = %#v", health)
	}
	tx.committed = false
	target := recorddeletion.PreviewTarget{
		Object: recordplatform.ObjectRef{
			ProjectID:  string(recordplatform.ProjectIDDefault),
			ObjectKind: "record",
			ObjectID:   "rec_corepreview01",
		},
		CurrentRevisionID:     "rrv_corepreview01",
		LockVersion:           2,
		AuthorizationEpoch:    3,
		ContentDeliveryEpoch:  4,
		DependencyGraphDigest: testStoreRecordPlatformDigest(0x52),
	}
	preview, err := repository.PreviewRecordCore(context.Background(), target)
	if err != nil {
		t.Fatalf("PreviewRecordCore() error = %v", err)
	}
	if err := preview.Validate(); err != nil {
		t.Fatalf("PreviewRecordCore() = %#v: %v", preview, err)
	}
	if !tx.committed || len(tx.querySQL) != 2 {
		t.Fatalf("core health/preview queries=%d committed=%t, want 2/true", len(tx.querySQL), tx.committed)
	}
	previewSQL := tx.querySQL[1]
	for _, want := range []string{
		"reservation.state in ('fenced', 'committed')",
		"root.current_revision_id = $2",
		"root.lock_version = $3",
		"root.authorization_epoch = $4",
		"epoch.delivery_epoch = $5",
		"payload_hash",
		"checkpoint_payload_hash",
		"canonical_hash",
	} {
		if !strings.Contains(previewSQL, want) {
			t.Errorf("core preview SQL missing %q", want)
		}
	}
	for _, forbidden := range []string{"body_markdown", "current_title", "identity_snapshot"} {
		if strings.Contains(previewSQL, forbidden) {
			t.Errorf("core preview SQL reads content-bearing column %q", forbidden)
		}
	}
}

func TestPostgresRecordDeletionCreatePreviewPersistsContentFreeBindingAndReturnsLocator(t *testing.T) {
	t.Parallel()

	command := testStoreDeletionCreatePreviewCommand(t)
	expiresAt := time.Date(2026, time.August, 3, 18, 10, 0, 0, time.UTC)
	tx := &fakeRecordPlatformTx{queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
		if !strings.Contains(sql, "insert into public.deletion_reservations") {
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		}
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			*dest[0].(*time.Time) = expiresAt
			return nil
		}}
	}}
	repository := NewPostgresRecordDeletionRepository(nil, allowRecordPlatformAdmissionGate)
	repository.newReservationID = func() (string, error) { return "drs_0123456789abcdef", nil }
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

	stored, err := repository.CreatePreview(context.Background(), command)
	if err != nil {
		t.Fatalf("CreatePreview() error = %v", err)
	}
	if stored.ReservationID != "drs_0123456789abcdef" || stored.Object != command.Object ||
		stored.ActorScopeDigest != command.ActorScopeDigest || stored.TokenCommitment != command.TokenCommitment ||
		stored.BindingDigest != command.BindingDigest || stored.WitnessHead != command.WitnessHead ||
		!command.RequestFingerprint.MatchesPersisted(stored.RequestFingerprint) || !stored.ExpiresAt.Equal(expiresAt) ||
		stored.Operation != nil || stored.Validate() != nil {
		t.Fatalf("CreatePreview() = %#v", stored)
	}
	if len(tx.querySQL) != 1 || !strings.Contains(tx.querySQL[0], "transaction_timestamp() + ($") ||
		!strings.Contains(tx.querySQL[0], "interval '1 microsecond'") || !tx.committed {
		t.Fatalf("CreatePreview() SQL=%#v committed=%t", tx.querySQL, tx.committed)
	}
	joined := strings.ToLower(strings.Join(tx.querySQL, "\n"))
	for _, column := range []string{
		"actor_scope_digest", "deletion_token_commitment", "request_fingerprint",
		"preview_binding_digest", "preview_current_revision_id", "preview_lock_version",
		"preview_authorization_epoch", "preview_content_delivery_epoch",
		"preview_dependency_graph_digest", "preview_backup_inventory_digest",
		"preview_processor_inventory_digest", "adapter_readiness_digest",
		"adapter_preview_digest", "preview_witness_sequence", "preview_witness_entry_hash",
	} {
		if !strings.Contains(joined, column) {
			t.Errorf("CreatePreview() SQL missing %q", column)
		}
	}
	for _, arguments := range tx.queryArgs {
		for _, argument := range arguments {
			if value, ok := argument.(string); ok && strings.HasPrefix(value, "drt1_") {
				t.Fatalf("CreatePreview() persisted raw token %q", value)
			}
		}
	}
}

func TestPostgresRecordDeletionResolvePreviewUsesLocatorAndLoadsDurableReplay(t *testing.T) {
	t.Parallel()

	command := testStoreDeletionCreatePreviewCommand(t)
	reservationID := "drs_0123456789abcdef"
	expiresAt := time.Date(2026, time.August, 3, 18, 10, 0, 0, time.UTC)
	fingerprintBytes, err := command.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}
	operation := recorddeletion.DeletionOperation{
		OperationID:     "rpo_resolvepreview01",
		ReservationID:   reservationID,
		Object:          command.Object,
		ReasonCode:      recorddeletion.DeletionReasonUserConfirmed,
		State:           recorddeletion.DeletionStateWitnessPending,
		FenceEpoch:      9,
		LedgerSequence:  11,
		LedgerEntryHash: testStoreRecordPlatformDigest(0x4a),
	}
	issued, err := recordplatform.NewIssuedDeletionRequestTokenV1()
	if err != nil {
		t.Fatalf("NewIssuedDeletionRequestTokenV1() error = %v", err)
	}
	token, err := recordplatform.ParseDeletionRequestTokenTransportV1(issued.Transport())
	if err != nil {
		t.Fatalf("ParseDeletionRequestTokenTransportV1() error = %v", err)
	}

	tx := &fakeRecordPlatformTx{queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
		if !strings.Contains(sql, "from public.deletion_reservations as reservation") {
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		}
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			*dest[0].(*string) = reservationID
			*dest[1].(*string) = command.Object.ProjectID
			*dest[2].(*string) = command.Object.ObjectKind
			*dest[3].(*string) = command.Object.ObjectID
			*dest[4].(*[]byte) = append([]byte(nil), command.ActorScopeDigest[:]...)
			*dest[5].(*[]byte) = append([]byte(nil), command.TokenCommitment[:]...)
			*dest[6].(*[]byte) = append([]byte(nil), fingerprintBytes[:]...)
			*dest[7].(*[]byte) = append([]byte(nil), command.BindingDigest[:]...)
			*dest[8].(*int64) = int64(command.WitnessHead.Sequence)
			*dest[9].(*[]byte) = append([]byte(nil), command.WitnessHead.EntryHash[:]...)
			*dest[10].(*time.Time) = expiresAt
			*dest[11].(*bool) = true
			*dest[12].(*string) = operation.OperationID
			*dest[13].(*string) = string(operation.ReasonCode)
			*dest[14].(*string) = string(operation.State)
			*dest[15].(*int64) = int64(operation.FenceEpoch)
			*dest[16].(*int64) = int64(operation.LedgerSequence)
			*dest[17].(*[]byte) = append([]byte(nil), operation.LedgerEntryHash[:]...)
			*dest[18].(*int64) = int64(operation.ReleaseEpoch)
			*dest[19].(*[]byte) = append([]byte(nil), operation.ReceiptDigest[:]...)
			return nil
		}}
	}}
	repository := NewPostgresRecordDeletionRepository(nil, allowRecordPlatformAdmissionGate)
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }
	lookup := recorddeletion.PreviewLookup{ReservationID: reservationID, Object: command.Object, Token: token}

	stored, err := repository.ResolvePreview(context.Background(), lookup)
	if err != nil {
		t.Fatalf("ResolvePreview() error = %v", err)
	}
	if stored.ReservationID != reservationID || stored.Object != command.Object ||
		stored.ActorScopeDigest != command.ActorScopeDigest || stored.TokenCommitment != command.TokenCommitment ||
		stored.BindingDigest != command.BindingDigest || stored.WitnessHead != command.WitnessHead ||
		!command.RequestFingerprint.MatchesPersisted(stored.RequestFingerprint) || !stored.ExpiresAt.Equal(expiresAt) ||
		stored.Operation == nil || *stored.Operation != operation || stored.Validate() != nil {
		t.Fatalf("ResolvePreview() = %#v", stored)
	}
	if len(tx.querySQL) != 1 || !tx.committed {
		t.Fatalf("ResolvePreview() queries=%d committed=%t", len(tx.querySQL), tx.committed)
	}
	compact := strings.ToLower(strings.Join(strings.Fields(tx.querySQL[0]), " "))
	for _, required := range []string{
		"where reservation.reservation_id = $1",
		"reservation.project_id = $2",
		"reservation.object_kind = $3",
		"reservation.object_id = $4",
		"left join public.record_purge_operations",
		"operation.operation_id is not null",
	} {
		if !strings.Contains(compact, required) {
			t.Errorf("ResolvePreview() SQL missing %q:\n%s", required, compact)
		}
	}
	if strings.Contains(compact, "deletion_token_commitment =") {
		t.Fatalf("ResolvePreview() uses token commitment as lookup key:\n%s", compact)
	}
}

func TestPostgresRecordDeletionResolveOperationStatusLoadsContentFreeAuthorizedProjection(t *testing.T) {
	t.Parallel()

	operation := testStoreDeletionOperation(recorddeletion.DeletionStateOnlinePurged)
	operation.ReceiptDigest = testStoreRecordPlatformDigest(0x62)
	initiatorActorID := "usr_aaaaaaaaaaaaaaaaaaaaaaaa"
	tx := &fakeRecordPlatformTx{queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
		if !strings.Contains(sql, "from public.record_purge_operations as operation") ||
			!strings.Contains(sql, "join public.deletion_reservations as reservation") {
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected query") }}
		}
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			*dest[0].(*string) = operation.OperationID
			*dest[1].(*string) = operation.ReservationID
			*dest[2].(*string) = operation.Object.ProjectID
			*dest[3].(*string) = operation.Object.ObjectKind
			*dest[4].(*string) = operation.Object.ObjectID
			*dest[5].(*string) = initiatorActorID
			*dest[6].(*string) = string(operation.ReasonCode)
			*dest[7].(*string) = string(operation.State)
			*dest[8].(*int64) = int64(operation.FenceEpoch)
			*dest[9].(*int64) = int64(operation.LedgerSequence)
			*dest[10].(*[]byte) = append([]byte(nil), operation.LedgerEntryHash[:]...)
			*dest[11].(*int64) = int64(operation.ReleaseEpoch)
			*dest[12].(*[]byte) = append([]byte(nil), operation.ReceiptDigest[:]...)
			return nil
		}}
	}}
	repository := testStoreDeletionRepository(tx)

	status, err := repository.ResolveOperationStatus(
		context.Background(),
		recordplatform.ProjectIDDefault,
		operation.OperationID,
	)
	if err != nil {
		t.Fatalf("ResolveOperationStatus() error = %v", err)
	}
	if status.Operation != operation || status.InitiatorActorID != initiatorActorID || status.Validate() != nil {
		t.Fatalf("ResolveOperationStatus() = %#v", status)
	}
	if len(tx.querySQL) != 1 || len(tx.queryArgs) != 1 || !tx.committed {
		t.Fatalf("ResolveOperationStatus() queries=%d args=%d committed=%t", len(tx.querySQL), len(tx.queryArgs), tx.committed)
	}
	compact := strings.ToLower(strings.Join(strings.Fields(tx.querySQL[0]), " "))
	for _, required := range []string{
		"where operation.operation_id = $1",
		"operation.project_id = $2",
		"reservation.project_id = operation.project_id",
	} {
		if !strings.Contains(compact, required) {
			t.Errorf("ResolveOperationStatus() SQL missing %q:\n%s", required, compact)
		}
	}
	for _, forbidden := range []string{
		"deletion_token_commitment",
		"request_fingerprint",
		"actor_scope_digest",
		"preview_binding_digest",
		"body_markdown",
		"current_title",
	} {
		if strings.Contains(compact, forbidden) {
			t.Errorf("ResolveOperationStatus() SQL includes forbidden field %q", forbidden)
		}
	}
	if got := tx.queryArgs[0]; len(got) != 2 || got[0] != operation.OperationID || got[1] != string(recordplatform.ProjectIDDefault) {
		t.Fatalf("ResolveOperationStatus() args = %#v", got)
	}
}

func TestPostgresRecordDeletionResolveOperationStatusFailsClosedForMissingProjection(t *testing.T) {
	t.Parallel()

	tx := &fakeRecordPlatformTx{queryRow: func(context.Context, string, ...any) pgx.Row {
		return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
	}}
	repository := testStoreDeletionRepository(tx)

	_, err := repository.ResolveOperationStatus(
		context.Background(),
		recordplatform.ProjectIDDefault,
		"rpo_missing01",
	)
	if !errors.Is(err, recorddeletion.ErrDeletionStatusUnavailable) {
		t.Fatalf("ResolveOperationStatus() error = %v, want ErrDeletionStatusUnavailable", err)
	}
}

func TestPostgresRecordDeletionReservePreviewAtomicallyRechecksCASFencesAndCreatesOperation(t *testing.T) {
	t.Parallel()

	command := testStoreDeletionReservePreviewCommand(t)
	ownerExpiresAt := time.Date(2026, time.August, 3, 18, 2, 0, 0, time.UTC)
	startedAt := time.Date(2026, time.August, 3, 18, 0, 0, 0, time.UTC)
	fingerprintBytes, err := command.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "as preview_binding_digest") && strings.Contains(sql, "for update"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*[]byte) = append([]byte(nil), command.Preview.ActorScopeDigest[:]...)
				*dest[1].(*[]byte) = append([]byte(nil), command.Preview.TokenCommitment[:]...)
				*dest[2].(*[]byte) = append([]byte(nil), fingerprintBytes[:]...)
				*dest[3].(*[]byte) = append([]byte(nil), command.Preview.BindingDigest[:]...)
				*dest[4].(*string) = command.Record.CurrentRevisionID
				*dest[5].(*int64) = int64(command.Record.LockVersion)
				*dest[6].(*int64) = int64(command.Record.AuthorizationEpoch)
				*dest[7].(*int64) = int64(command.Record.ContentDeliveryEpoch)
				*dest[8].(*[]byte) = append([]byte(nil), command.Record.DependencyGraphDigest[:]...)
				*dest[9].(*[]byte) = append([]byte(nil), command.Record.BackupInventoryDigest[:]...)
				*dest[10].(*[]byte) = append([]byte(nil), command.Record.ProcessorInventoryDigest[:]...)
				*dest[11].(*int64) = int64(command.Preview.WitnessHead.Sequence)
				*dest[12].(*[]byte) = append([]byte(nil), command.Preview.WitnessHead.EntryHash[:]...)
				return nil
			}}
		case strings.Contains(sql, "from public.records as record") && strings.Contains(sql, "for update"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*string) = command.Record.CurrentRevisionID
				*dest[1].(*int64) = int64(command.Record.LockVersion)
				*dest[2].(*int64) = int64(command.Record.AuthorizationEpoch)
				return nil
			}}
		case strings.Contains(sql, "from public.deletion_reservations") && strings.Contains(sql, "select state"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*string) = "previewed"
				*dest[1].(*string) = command.Preview.Object.ProjectID
				*dest[2].(*string) = command.Preview.Object.ObjectKind
				*dest[3].(*string) = command.Preview.Object.ObjectID
				*dest[4].(*int64) = 0
				*dest[5].(**time.Time) = nil
				*dest[6].(*time.Time) = command.Preview.ExpiresAt
				return nil
			}}
		case strings.Contains(sql, "from public.content_delivery_epochs"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*int64) = int64(command.Record.ContentDeliveryEpoch)
				return nil
			}}
		case strings.Contains(sql, "from public.deletion_fence_leases"):
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		case strings.Contains(sql, "from public.object_content_leases"):
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		case strings.Contains(sql, "update public.content_delivery_epochs"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*int64) = int64(command.Record.ContentDeliveryEpoch + 1)
				return nil
			}}
		case strings.Contains(sql, "update public.deletion_reservations"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*string) = command.OwnerID
				*dest[1].(*int64) = 1
				*dest[2].(*time.Time) = ownerExpiresAt
				return nil
			}}
		case strings.Contains(sql, "insert into public.deletion_fence_leases"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*string) = command.OwnerID
				*dest[1].(*int64) = 1
				*dest[2].(*time.Time) = ownerExpiresAt
				return nil
			}}
		case strings.Contains(sql, "insert into public.record_purge_operations"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*time.Time) = startedAt
				return nil
			}}
		default:
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		}
	}
	tx.exec = func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
		if !strings.Contains(sql, "insert into public.record_deletion_audits") {
			return pgconn.NewCommandTag("INSERT 0 0"), errors.New("unexpected exec")
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	repository := NewPostgresRecordDeletionRepository(nil, allowRecordPlatformAdmissionGate)
	repository.newOperationID = func() (string, error) { return "rpo_0123456789abcdef", nil }
	repository.newAuditID = func() (string, error) { return "rda_0123456789abcdef", nil }
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

	operation, err := repository.ReservePreview(context.Background(), command)
	if err != nil {
		t.Fatalf("ReservePreview() error = %v", err)
	}
	want := recorddeletion.DeletionOperation{
		OperationID:   "rpo_0123456789abcdef",
		ReservationID: command.Preview.ReservationID,
		Object:        command.Preview.Object,
		ReasonCode:    command.ReasonCode,
		State:         recorddeletion.DeletionStateProvisionalFenced,
		FenceEpoch:    command.Record.ContentDeliveryEpoch + 1,
	}
	if operation != want || operation.Validate() != nil {
		t.Fatalf("ReservePreview() = %#v, want %#v", operation, want)
	}
	if !tx.committed || tx.rollbackCount != 1 {
		t.Fatalf("ReservePreview() committed=%t rollbacks=%d", tx.committed, tx.rollbackCount)
	}
	joinedQueries := strings.ToLower(strings.Join(tx.querySQL, "\n"))
	joinedExecs := strings.ToLower(strings.Join(tx.execSQL, "\n"))
	for _, required := range []string{
		"preview_binding_digest", "from public.records as record", "for update",
		"update public.content_delivery_epochs", "update public.deletion_reservations",
		"insert into public.deletion_fence_leases", "insert into public.record_purge_operations",
	} {
		if !strings.Contains(joinedQueries, required) {
			t.Errorf("ReservePreview() queries missing %q", required)
		}
	}
	for _, required := range []string{"insert into public.record_deletion_audits", "'fenced'"} {
		if !strings.Contains(joinedExecs, required) {
			t.Errorf("ReservePreview() audit missing %q", required)
		}
	}
	casIndex := sqlIndexContaining(tx.querySQL, "from public.records as record")
	fenceIndex := sqlIndexContaining(tx.querySQL, "update public.deletion_reservations")
	operationIndex := sqlIndexContaining(tx.querySQL, "insert into public.record_purge_operations")
	if casIndex < 0 || fenceIndex <= casIndex || operationIndex <= fenceIndex {
		t.Fatalf("ReservePreview() order CAS=%d fence=%d operation=%d", casIndex, fenceIndex, operationIndex)
	}
}

func TestPostgresRecordDeletionClaimWorkTakesOverExpiredCompoundFence(t *testing.T) {
	t.Parallel()

	command := testStoreDeletionReservePreviewCommand(t)
	fingerprintBytes, err := command.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}
	observedAt := time.Date(2026, time.August, 3, 19, 0, 0, 0, time.UTC)
	oldOwner := recordplatform.OwnerLease{
		OwnerID:    "deletion_worker_old",
		Generation: 4,
		ExpiresAt:  observedAt.Add(-time.Second),
	}
	newExpiresAt := observedAt.Add(2 * time.Minute)
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from public.record_purge_operations as operation") &&
			strings.Contains(sql, "for update of operation, reservation skip locked"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*string) = "rpo_claimtakeover01"
				*dest[1].(*string) = command.Preview.ReservationID
				*dest[2].(*string) = command.Preview.Object.ProjectID
				*dest[3].(*string) = string(recorddeletion.DeletionStateLedgerCommitUnknown)
				*dest[4].(*string) = string(command.DeploymentID)
				*dest[5].(*string) = command.ActorID
				*dest[6].(*string) = string(command.ReasonCode)
				*dest[7].(*int64) = int64(command.DeletionContractVersion)
				*dest[8].(*string) = ""
				*dest[9].(*int64) = 0
				*dest[10].(*[]byte) = nil
				*dest[11].(*int64) = 0
				*dest[12].(*[]byte) = nil
				*dest[13].(*string) = ""
				*dest[14].(*string) = oldOwner.OwnerID
				*dest[15].(*int64) = int64(oldOwner.Generation)
				*dest[16].(*time.Time) = oldOwner.ExpiresAt
				*dest[17].(*string) = "fenced"
				*dest[18].(*string) = command.Preview.Object.ObjectKind
				*dest[19].(*string) = command.Preview.Object.ObjectID
				*dest[20].(*[]byte) = append([]byte(nil), command.Preview.TokenCommitment[:]...)
				*dest[21].(*[]byte) = append([]byte(nil), fingerprintBytes[:]...)
				*dest[22].(*int64) = int64(command.Record.ContentDeliveryEpoch + 1)
				*dest[23].(*string) = oldOwner.OwnerID
				*dest[24].(*int64) = int64(oldOwner.Generation)
				reservationExpiry := oldOwner.ExpiresAt
				*dest[25].(**time.Time) = &reservationExpiry
				*dest[26].(*time.Time) = observedAt
				return nil
			}}
		case strings.Contains(sql, "from public.deletion_fence_leases") && strings.Contains(sql, "for update"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*string) = oldOwner.OwnerID
				*dest[1].(*int64) = int64(oldOwner.Generation)
				*dest[2].(*time.Time) = oldOwner.ExpiresAt
				return nil
			}}
		case strings.Contains(sql, "update public.deletion_reservations") ||
			strings.Contains(sql, "update public.deletion_fence_leases") ||
			strings.Contains(sql, "update public.record_purge_operations"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*string) = "deletion_worker_new"
				*dest[1].(*int64) = 5
				*dest[2].(*time.Time) = newExpiresAt
				return nil
			}}
		default:
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected query") }}
		}
	}
	repository := testStoreDeletionRepository(tx)

	claim, err := repository.ClaimDeletionWork(context.Background(), recorddeletion.DeletionWorkClaimInput{
		OwnerID:            "deletion_worker_new",
		OwnerLeaseDuration: 2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("ClaimDeletionWork() error = %v", err)
	}
	if claim == nil || claim.Validate() != nil {
		t.Fatalf("ClaimDeletionWork() = %#v, want valid claim", claim)
	}
	if claim.Stage != recorddeletion.DeletionWorkResolveDeleteCommit ||
		claim.Operation.State != recorddeletion.DeletionStateLedgerCommitUnknown ||
		claim.Owner.OwnerID != "deletion_worker_new" || claim.Owner.Generation != 5 ||
		!claim.Owner.ExpiresAt.Equal(newExpiresAt) {
		t.Fatalf("ClaimDeletionWork() = %#v, want expired-owner takeover", claim)
	}
	if claim.Request.OperationID != claim.Operation.OperationID ||
		claim.Request.EntryType != recorddeletion.LedgerEntryDeleteCommit ||
		claim.Request.TokenCommitment != command.Preview.TokenCommitment ||
		!claim.Request.RequestFingerprint.Equal(command.Preview.RequestFingerprint) {
		t.Fatalf("ClaimDeletionWork() request = %#v, want persisted content-free identity", claim.Request)
	}
	if len(tx.querySQL) != 5 || !tx.committed {
		t.Fatalf("ClaimDeletionWork() queries=%d committed=%t, want 5/true", len(tx.querySQL), tx.committed)
	}
	selection := strings.ToLower(tx.querySQL[0])
	for _, fragment := range []string{
		"for update of operation, reservation skip locked",
		"operation.owner_id = $1",
		"operation.owner_expires_at <= transaction_timestamp()",
		"order by operation.started_at, operation.operation_id",
		"operation.operation_state <> 'provisional_fenced'",
		"from public.object_content_leases as content_lease",
		"content_lease.expires_at > transaction_timestamp()",
	} {
		if !strings.Contains(selection, fragment) {
			t.Errorf("claim selection SQL missing %q:\n%s", fragment, tx.querySQL[0])
		}
	}
	for _, sql := range tx.querySQL[1:] {
		if !strings.Contains(sql, "owner_generation") || !strings.Contains(sql, "expires_at") ||
			!strings.Contains(sql, " = $") {
			t.Errorf("claim owner transition lacks exact tuple fencing:\n%s", sql)
		}
	}
}

func TestPostgresRecordDeletionClaimWorkRenewsCommittedOperationOwner(t *testing.T) {
	t.Parallel()

	command := testStoreDeletionReservePreviewCommand(t)
	fingerprintBytes, err := command.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}
	observedAt := time.Date(2026, time.August, 3, 20, 0, 0, 0, time.UTC)
	oldOwner := recordplatform.OwnerLease{
		OwnerID:    "deletion_worker_01",
		Generation: 7,
		ExpiresAt:  observedAt.Add(time.Minute),
	}
	newExpiresAt := observedAt.Add(2 * time.Minute)
	entryHash := testStoreRecordPlatformDigest(0x5a)
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from public.record_purge_operations as operation") &&
			strings.Contains(sql, "for update of operation, reservation skip locked"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*string) = "rpo_claimcommitted01"
				*dest[1].(*string) = command.Preview.ReservationID
				*dest[2].(*string) = command.Preview.Object.ProjectID
				*dest[3].(*string) = string(recorddeletion.DeletionStateFencePropagating)
				*dest[4].(*string) = string(command.DeploymentID)
				*dest[5].(*string) = command.ActorID
				*dest[6].(*string) = string(command.ReasonCode)
				*dest[7].(*int64) = int64(command.DeletionContractVersion)
				*dest[8].(*string) = string(recorddeletion.LedgerEntryDeleteCommit)
				*dest[9].(*int64) = 13
				*dest[10].(*[]byte) = append([]byte(nil), entryHash[:]...)
				*dest[11].(*int64) = 0
				*dest[12].(*[]byte) = nil
				*dest[13].(*string) = ""
				*dest[14].(*string) = oldOwner.OwnerID
				*dest[15].(*int64) = int64(oldOwner.Generation)
				*dest[16].(*time.Time) = oldOwner.ExpiresAt
				*dest[17].(*string) = "committed"
				*dest[18].(*string) = command.Preview.Object.ObjectKind
				*dest[19].(*string) = command.Preview.Object.ObjectID
				*dest[20].(*[]byte) = append([]byte(nil), command.Preview.TokenCommitment[:]...)
				*dest[21].(*[]byte) = append([]byte(nil), fingerprintBytes[:]...)
				*dest[22].(*int64) = int64(command.Record.ContentDeliveryEpoch + 1)
				*dest[23].(*string) = ""
				*dest[24].(*int64) = 0
				reservationExpiry, ok := dest[25].(**time.Time)
				if !ok {
					return errors.New("committed reservation owner expiry is not nullable")
				}
				*reservationExpiry = nil
				*dest[26].(*time.Time) = observedAt
				return nil
			}}
		case strings.Contains(sql, "update public.record_purge_operations"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*string) = oldOwner.OwnerID
				*dest[1].(*int64) = int64(oldOwner.Generation)
				*dest[2].(*time.Time) = newExpiresAt
				return nil
			}}
		default:
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected query") }}
		}
	}
	repository := testStoreDeletionRepository(tx)

	claim, err := repository.ClaimDeletionWork(context.Background(), recorddeletion.DeletionWorkClaimInput{
		OwnerID:            oldOwner.OwnerID,
		OwnerLeaseDuration: 2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("ClaimDeletionWork() error = %v", err)
	}
	if claim == nil || claim.Validate() != nil {
		t.Fatalf("ClaimDeletionWork() = %#v, want valid committed-state claim", claim)
	}
	if claim.Stage != recorddeletion.DeletionWorkPropagatePermanentFence ||
		claim.Owner.Generation != oldOwner.Generation || !claim.Owner.ExpiresAt.Equal(newExpiresAt) {
		t.Fatalf("ClaimDeletionWork() = %#v, want same-generation committed renewal", claim)
	}
	if len(tx.querySQL) != 2 || !strings.Contains(tx.querySQL[1], "update public.record_purge_operations") || !tx.committed {
		t.Fatalf("ClaimDeletionWork() queries=%#v committed=%t, want selection + operation renewal", tx.querySQL, tx.committed)
	}
}

func TestPostgresRecordDeletionDeleteCommitCutPointsFenceExactOwner(t *testing.T) {
	t.Parallel()

	appendClaim := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkAppendDeleteCommit)
	resolveClaim := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkResolveDeleteCommit)
	confirmClaim := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkConfirmDeleteWitness)
	entry := testStoreDeletionLedgerEntry(t, appendClaim.Request)
	receipt := recorddeletion.DeletionWitnessReceipt{
		Sequence: entry.Sequence, EntryHash: entry.EntryHash,
		ProofDigest: testStoreRecordPlatformDigest(0x6b),
	}

	tests := []struct {
		name        string
		fromState   recorddeletion.DeletionState
		toState     recorddeletion.DeletionState
		claim       recorddeletion.ClaimedDeletionWork
		run         func(*PostgresRecordDeletionRepository) error
		wantLedger  bool
		wantWitness bool
	}{
		{
			name:      "append acknowledgement unknown",
			fromState: recorddeletion.DeletionStateProvisionalFenced,
			toState:   recorddeletion.DeletionStateLedgerCommitUnknown,
			claim:     appendClaim,
			run: func(repository *PostgresRecordDeletionRepository) error {
				return repository.MarkDeleteCommitUnknown(context.Background(), appendClaim)
			},
		},
		{
			name:      "delete entry persisted after append",
			fromState: recorddeletion.DeletionStateProvisionalFenced,
			toState:   recorddeletion.DeletionStateWitnessPending,
			claim:     appendClaim,
			run: func(repository *PostgresRecordDeletionRepository) error {
				return repository.RecordDeleteEntry(context.Background(), appendClaim, entry)
			},
			wantLedger: true,
		},
		{
			name:      "delete entry persisted after unknown resolution",
			fromState: recorddeletion.DeletionStateLedgerCommitUnknown,
			toState:   recorddeletion.DeletionStateWitnessPending,
			claim:     resolveClaim,
			run: func(repository *PostgresRecordDeletionRepository) error {
				return repository.RecordDeleteEntry(context.Background(), resolveClaim, entry)
			},
			wantLedger: true,
		},
		{
			name:      "delete witness persisted",
			fromState: recorddeletion.DeletionStateWitnessPending,
			toState:   recorddeletion.DeletionStateDeleteRequested,
			claim:     confirmClaim,
			run: func(repository *PostgresRecordDeletionRepository) error {
				return repository.MarkDeleteWitnessed(context.Background(), confirmClaim, receipt)
			},
			wantLedger: true, wantWitness: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tx := &fakeRecordPlatformTx{exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}}
			repository := testStoreDeletionRepository(tx)

			if err := tt.run(repository); err != nil {
				t.Fatalf("worker transition error = %v", err)
			}
			if len(tx.execSQL) != 1 || !tx.committed {
				t.Fatalf("worker transition execs=%d committed=%t, want 1/true", len(tx.execSQL), tx.committed)
			}
			sql := strings.ToLower(tx.execSQL[0])
			for _, fragment := range []string{
				"update public.record_purge_operations",
				"operation_state = $",
				"operation_state = $",
				"owner_id = $",
				"owner_generation = $",
				"owner_expires_at = $",
			} {
				if !strings.Contains(sql, fragment) {
					t.Errorf("worker transition SQL missing %q:\n%s", fragment, tx.execSQL[0])
				}
			}
			arguments := tx.execArgs[0]
			if !storeDeletionArgumentsContain(arguments, tt.fromState) ||
				!storeDeletionArgumentsContain(arguments, tt.toState) ||
				!storeDeletionArgumentsContain(arguments, tt.claim.Owner.ExpiresAt) {
				t.Errorf("worker transition args = %#v, want states %s -> %s and exact expiry %s", arguments, tt.fromState, tt.toState, tt.claim.Owner.ExpiresAt)
			}
			joined := strings.Join([]string{sql, fmt.Sprint(arguments...)}, " ")
			if tt.wantLedger != strings.Contains(joined, string(recorddeletion.LedgerEntryDeleteCommit)) {
				t.Errorf("worker transition ledger persistence = %t, want %t: SQL=%s args=%#v", strings.Contains(joined, string(recorddeletion.LedgerEntryDeleteCommit)), tt.wantLedger, tx.execSQL[0], arguments)
			}
			if tt.wantWitness != strings.Contains(sql, "witness_proof_digest") {
				t.Errorf("worker transition witness persistence = %t, want %t: %s", strings.Contains(sql, "witness_proof_digest"), tt.wantWitness, tx.execSQL[0])
			}
		})
	}
}

func TestPostgresRecordDeletionOutcomeCutPointsRemainFenced(t *testing.T) {
	t.Parallel()

	resolveDelete := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkResolveDeleteCommit)
	resolveOutcome := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkResolveNotCommitted)
	outcomeRequest := resolveDelete.Request.AttemptNotCommitted(resolveOutcome.Operation.ReleaseEpoch)
	outcomeEntry := testStoreDeletionLedgerEntry(t, outcomeRequest)

	tests := []struct {
		name       string
		claim      recorddeletion.ClaimedDeletionWork
		run        func(*PostgresRecordDeletionRepository) error
		wantLedger bool
	}{
		{
			name:  "outcome append acknowledgement unknown",
			claim: resolveDelete,
			run: func(repository *PostgresRecordDeletionRepository) error {
				return repository.MarkOutcomeCommitUnknown(context.Background(), resolveDelete, resolveOutcome.Operation.ReleaseEpoch)
			},
		},
		{
			name:  "outcome entry after delete absence proof",
			claim: resolveDelete,
			run: func(repository *PostgresRecordDeletionRepository) error {
				return repository.RecordOutcomeEntry(context.Background(), resolveDelete, outcomeEntry)
			},
			wantLedger: true,
		},
		{
			name:  "resolved outcome entry after acknowledgement loss",
			claim: resolveOutcome,
			run: func(repository *PostgresRecordDeletionRepository) error {
				return repository.RecordOutcomeEntry(context.Background(), resolveOutcome, outcomeEntry)
			},
			wantLedger: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tx := &fakeRecordPlatformTx{exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}}
			repository := testStoreDeletionRepository(tx)

			if err := tt.run(repository); err != nil {
				t.Fatalf("outcome transition error = %v", err)
			}
			if len(tx.execSQL) != 1 || !tx.committed {
				t.Fatalf("outcome transition execs=%d committed=%t, want operation-only 1/true", len(tx.execSQL), tx.committed)
			}
			sql := strings.ToLower(tx.execSQL[0])
			for _, fragment := range []string{
				"update public.record_purge_operations",
				"operation_state = $",
				"release_epoch = $",
				"owner_id = $",
				"owner_generation = $",
				"owner_expires_at = $",
			} {
				if !strings.Contains(sql, fragment) {
					t.Errorf("outcome transition SQL missing %q:\n%s", fragment, tx.execSQL[0])
				}
			}
			if strings.Contains(sql, "deletion_reservations") || strings.Contains(sql, "deletion_fence_leases") {
				t.Fatalf("outcome pending transition released compound fence: %s", tx.execSQL[0])
			}
			arguments := tx.execArgs[0]
			if !storeDeletionArgumentsContain(arguments, recorddeletion.DeletionStateReleasePending) ||
				!storeDeletionArgumentsContain(arguments, int64(resolveOutcome.Operation.ReleaseEpoch)) ||
				!storeDeletionArgumentsContain(arguments, tt.claim.Owner.ExpiresAt) {
				t.Errorf("outcome transition args = %#v, want release-pending epoch and exact owner", arguments)
			}
			joined := strings.Join([]string{sql, fmt.Sprint(arguments...)}, " ")
			if tt.wantLedger != strings.Contains(joined, string(recorddeletion.LedgerEntryAttemptNotCommitted)) {
				t.Errorf("outcome ledger persistence = %t, want %t: SQL=%s args=%#v", strings.Contains(joined, string(recorddeletion.LedgerEntryAttemptNotCommitted)), tt.wantLedger, tx.execSQL[0], arguments)
			}
		})
	}
}

func TestPostgresRecordDeletionFinalizeNotCommittedAtomicallyReleasesWitnessedFence(t *testing.T) {
	t.Parallel()

	claim := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkConfirmNotCommittedWitness)
	receipt := recorddeletion.DeletionWitnessReceipt{
		Sequence: claim.Entry.Sequence, EntryHash: claim.Entry.EntryHash,
		ProofDigest: testStoreRecordPlatformDigest(0x6c),
	}

	for _, tt := range []struct {
		name          string
		operationRows int64
		wantErr       bool
	}{
		{name: "commit", operationRows: 1},
		{name: "stale exact owner rolls back", operationRows: 0, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tx := &fakeRecordPlatformTx{}
			tx.exec = func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				switch {
				case strings.Contains(sql, "update public.record_purge_operations"):
					return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", tt.operationRows)), nil
				case strings.Contains(sql, "update public.deletion_reservations"),
					strings.Contains(sql, "update public.deletion_fence_leases"):
					return pgconn.NewCommandTag("UPDATE 1"), nil
				case strings.Contains(sql, "insert into public.record_deletion_audits"):
					return pgconn.NewCommandTag("INSERT 0 1"), nil
				default:
					return pgconn.NewCommandTag("UPDATE 0"), errors.New("unexpected finalize SQL")
				}
			}
			repository := testStoreDeletionRepository(tx)
			repository.newAuditID = func() (string, error) { return "rda_notcommitted01", nil }

			err := repository.FinalizeNotCommitted(context.Background(), claim, receipt)
			if tt.wantErr {
				if !errors.Is(err, recordplatform.ErrLostOwnerLease) {
					t.Fatalf("FinalizeNotCommitted() error = %v, want ErrLostOwnerLease", err)
				}
				if tx.committed {
					t.Fatal("FinalizeNotCommitted() committed stale owner transition")
				}
				return
			}
			if err != nil {
				t.Fatalf("FinalizeNotCommitted() error = %v", err)
			}
			if len(tx.execSQL) != 4 || !tx.committed {
				t.Fatalf("FinalizeNotCommitted() execs=%d committed=%t, want 4/true", len(tx.execSQL), tx.committed)
			}
			wantOrder := []string{
				"update public.deletion_reservations",
				"update public.deletion_fence_leases",
				"update public.record_purge_operations",
				"insert into public.record_deletion_audits",
			}
			for index, fragment := range wantOrder {
				if !strings.Contains(strings.ToLower(tx.execSQL[index]), fragment) {
					t.Errorf("FinalizeNotCommitted() SQL %d = %q, want %q", index, tx.execSQL[index], fragment)
				}
			}
			joinedSQL := strings.ToLower(strings.Join(tx.execSQL, "\n"))
			for _, fragment := range []string{
				"state = $", "release_epoch = $", "owner_id = ''", "owner_generation = 0",
				"owner_expires_at = null", "expires_at = transaction_timestamp()",
				"witness_proof_digest = $", "completed_at = transaction_timestamp()",
			} {
				if !strings.Contains(joinedSQL, fragment) {
					t.Errorf("FinalizeNotCommitted() SQL missing %q:\n%s", fragment, joinedSQL)
				}
			}
			joinedArgs := fmt.Sprint(tx.execArgs)
			for _, value := range []string{
				string(recorddeletion.DeletionStateNotCommitted),
				string(recorddeletion.LedgerEntryAttemptNotCommitted),
				string(recorddeletion.DeletionReasonUserConfirmed),
			} {
				if !strings.Contains(joinedArgs, value) {
					t.Errorf("FinalizeNotCommitted() args missing %q: %#v", value, tx.execArgs)
				}
			}
			for _, arguments := range tx.execArgs[:3] {
				if !storeDeletionArgumentsContain(arguments, claim.Owner.ExpiresAt) {
					t.Errorf("FinalizeNotCommitted() transition lacks exact owner expiry: %#v", arguments)
				}
			}
		})
	}
}

func TestPostgresRecordDeletionPromotePermanentFenceIsAtomic(t *testing.T) {
	t.Parallel()

	claim := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkPromotePermanentFence)
	for _, tt := range []struct {
		name          string
		operationRows int64
		wantErr       bool
	}{
		{name: "commit", operationRows: 1},
		{name: "stale exact owner rolls back", operationRows: 0, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tx := &fakeRecordPlatformTx{}
			tx.exec = func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				switch {
				case strings.Contains(sql, "update public.record_purge_operations"):
					return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", tt.operationRows)), nil
				case strings.Contains(sql, "update public.deletion_reservations"),
					strings.Contains(sql, "update public.deletion_fence_leases"):
					return pgconn.NewCommandTag("UPDATE 1"), nil
				case strings.Contains(sql, "insert into public.record_deletion_audits"):
					return pgconn.NewCommandTag("INSERT 0 1"), nil
				default:
					return pgconn.NewCommandTag("UPDATE 0"), errors.New("unexpected promote SQL")
				}
			}
			repository := testStoreDeletionRepository(tx)
			repository.newAuditID = func() (string, error) { return "rda_committed01", nil }

			err := repository.PromotePermanentFence(context.Background(), claim)
			if tt.wantErr {
				if !errors.Is(err, recordplatform.ErrLostOwnerLease) {
					t.Fatalf("PromotePermanentFence() error = %v, want ErrLostOwnerLease", err)
				}
				if tx.committed {
					t.Fatal("PromotePermanentFence() committed stale owner transition")
				}
				return
			}
			if err != nil {
				t.Fatalf("PromotePermanentFence() error = %v", err)
			}
			if len(tx.execSQL) != 4 || !tx.committed {
				t.Fatalf("PromotePermanentFence() execs=%d committed=%t, want 4/true", len(tx.execSQL), tx.committed)
			}
			wantOrder := []string{
				"update public.deletion_reservations",
				"update public.deletion_fence_leases",
				"update public.record_purge_operations",
				"insert into public.record_deletion_audits",
			}
			for index, fragment := range wantOrder {
				if !strings.Contains(strings.ToLower(tx.execSQL[index]), fragment) {
					t.Errorf("PromotePermanentFence() SQL %d = %q, want %q", index, tx.execSQL[index], fragment)
				}
			}
			joinedSQL := strings.ToLower(strings.Join(tx.execSQL, "\n"))
			for _, fragment := range []string{
				"state = $", "owner_id = ''", "owner_generation = 0", "owner_expires_at = null",
				"expires_at = transaction_timestamp()", "completed_at = transaction_timestamp()",
				"operation_state = $", "witness_proof_digest is not null",
			} {
				if !strings.Contains(joinedSQL, fragment) {
					t.Errorf("PromotePermanentFence() SQL missing %q:\n%s", fragment, joinedSQL)
				}
			}
			joinedArgs := fmt.Sprint(tx.execArgs)
			for _, value := range []string{"committed", string(recorddeletion.DeletionStateFencePropagating)} {
				if !strings.Contains(joinedArgs, value) {
					t.Errorf("PromotePermanentFence() args missing %q: %#v", value, tx.execArgs)
				}
			}
			for _, arguments := range tx.execArgs[:3] {
				if !storeDeletionArgumentsContain(arguments, claim.Owner.ExpiresAt) {
					t.Errorf("PromotePermanentFence() transition lacks exact owner expiry: %#v", arguments)
				}
			}
		})
	}
}

func TestPostgresRecordDeletionPermanentFenceAndPurgeCutPoints(t *testing.T) {
	t.Parallel()

	propagateClaim := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkPropagatePermanentFence)
	readyTx := &fakeRecordPlatformTx{queryRow: func(context.Context, string, ...any) pgx.Row {
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			*dest[0].(*bool) = true
			return nil
		}}
	}}
	readyRepository := testStoreDeletionRepository(readyTx)
	ready, err := readyRepository.PermanentFenceApplied(context.Background(), propagateClaim)
	if err != nil || !ready {
		t.Fatalf("PermanentFenceApplied() = (%t, %v), want true/nil", ready, err)
	}
	if len(readyTx.querySQL) != 1 || !readyTx.committed {
		t.Fatalf("PermanentFenceApplied() queries=%d committed=%t, want 1/true", len(readyTx.querySQL), readyTx.committed)
	}
	readySQL := strings.ToLower(readyTx.querySQL[0])
	for _, fragment := range []string{
		"from public.record_purge_operations as operation",
		"reservation.state = 'committed'",
		"not exists", "public.deletion_fence_leases", "public.object_content_leases",
		"operation.owner_id = $", "operation.owner_generation = $", "operation.owner_expires_at = $",
	} {
		if !strings.Contains(readySQL, fragment) {
			t.Errorf("PermanentFenceApplied() SQL missing %q:\n%s", fragment, readyTx.querySQL[0])
		}
	}
	if !storeDeletionArgumentsContain(readyTx.queryArgs[0], propagateClaim.Owner.ExpiresAt) {
		t.Errorf("PermanentFenceApplied() query lacks exact owner expiry: %#v", readyTx.queryArgs[0])
	}

	readFencedClaim := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkBeginOnlinePurge)
	onlinePurgingClaim := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkPurgeOnline)
	receipt := recorddeletion.OnlinePurgeReceipt{
		OperationID:   onlinePurgingClaim.Operation.OperationID,
		ReceiptDigest: testStoreRecordPlatformDigest(0x6d),
	}
	tests := []struct {
		name      string
		claim     recorddeletion.ClaimedDeletionWork
		fromState recorddeletion.DeletionState
		toState   recorddeletion.DeletionState
		run       func(*PostgresRecordDeletionRepository) error
		terminal  bool
	}{
		{
			name: "mark read fenced", claim: propagateClaim,
			fromState: recorddeletion.DeletionStateFencePropagating,
			toState:   recorddeletion.DeletionStateReadFenced,
			run: func(repository *PostgresRecordDeletionRepository) error {
				return repository.MarkReadFenced(context.Background(), propagateClaim)
			},
		},
		{
			name: "begin online purge", claim: readFencedClaim,
			fromState: recorddeletion.DeletionStateReadFenced,
			toState:   recorddeletion.DeletionStateOnlinePurging,
			run: func(repository *PostgresRecordDeletionRepository) error {
				return repository.BeginOnlinePurge(context.Background(), readFencedClaim)
			},
		},
		{
			name: "complete online purge", claim: onlinePurgingClaim,
			fromState: recorddeletion.DeletionStateOnlinePurging,
			toState:   recorddeletion.DeletionStateOnlinePurged,
			run: func(repository *PostgresRecordDeletionRepository) error {
				return repository.CompleteOnlinePurge(context.Background(), onlinePurgingClaim, receipt)
			},
			terminal: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tx := &fakeRecordPlatformTx{exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}}
			repository := testStoreDeletionRepository(tx)
			if err := tt.run(repository); err != nil {
				t.Fatalf("purge cut point error = %v", err)
			}
			if len(tx.execSQL) != 1 || !tx.committed {
				t.Fatalf("purge cut point execs=%d committed=%t, want 1/true", len(tx.execSQL), tx.committed)
			}
			sql := strings.ToLower(tx.execSQL[0])
			for _, fragment := range []string{
				"update public.record_purge_operations", "operation_state = $",
				"owner_id = $", "owner_generation = $", "owner_expires_at = $",
			} {
				if !strings.Contains(sql, fragment) {
					t.Errorf("purge cut point SQL missing %q:\n%s", fragment, tx.execSQL[0])
				}
			}
			arguments := tx.execArgs[0]
			if !storeDeletionArgumentsContain(arguments, tt.fromState) ||
				!storeDeletionArgumentsContain(arguments, tt.toState) ||
				!storeDeletionArgumentsContain(arguments, tt.claim.Owner.ExpiresAt) {
				t.Errorf("purge cut point args = %#v, want %s -> %s and exact owner", arguments, tt.fromState, tt.toState)
			}
			if tt.terminal {
				for _, fragment := range []string{
					"receipt_digest = $", "completed_at = transaction_timestamp()",
					"owner_id = ''", "owner_generation = 0", "owner_expires_at = null",
					"reservation.state = 'committed'",
				} {
					if !strings.Contains(sql, fragment) {
						t.Errorf("online purge completion SQL missing %q:\n%s", fragment, tx.execSQL[0])
					}
				}
			}
		})
	}
}

func TestPostgresRecordDeletionRetryRoundTripPersistsOnlyClosedStage(t *testing.T) {
	t.Parallel()

	purgeClaim := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkPurgeOnline)
	retryClaim := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkResolveRetry)
	tests := []struct {
		name      string
		claim     recorddeletion.ClaimedDeletionWork
		fromState recorddeletion.DeletionState
		toState   recorddeletion.DeletionState
		run       func(*PostgresRecordDeletionRepository) error
	}{
		{
			name: "record retry", claim: purgeClaim,
			fromState: recorddeletion.DeletionStateOnlinePurging,
			toState:   recorddeletion.DeletionStateRetryRequired,
			run: func(repository *PostgresRecordDeletionRepository) error {
				return repository.MarkRetryRequired(context.Background(), purgeClaim, recorddeletion.DeletionWorkPurgeOnline)
			},
		},
		{
			name: "resume exact retry", claim: retryClaim,
			fromState: recorddeletion.DeletionStateRetryRequired,
			toState:   recorddeletion.DeletionStateOnlinePurging,
			run: func(repository *PostgresRecordDeletionRepository) error {
				return repository.ResumeRetry(context.Background(), retryClaim, recorddeletion.DeletionWorkPurgeOnline)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tx := &fakeRecordPlatformTx{exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}}
			repository := testStoreDeletionRepository(tx)
			if err := tt.run(repository); err != nil {
				t.Fatalf("retry transition error = %v", err)
			}
			if len(tx.execSQL) != 1 || !tx.committed {
				t.Fatalf("retry transition execs=%d committed=%t, want 1/true", len(tx.execSQL), tx.committed)
			}
			sql := strings.ToLower(tx.execSQL[0])
			for _, fragment := range []string{
				"update public.record_purge_operations", "operation_state = $", "retry_from",
				"owner_id = $", "owner_generation = $", "owner_expires_at = $",
			} {
				if !strings.Contains(sql, fragment) {
					t.Errorf("retry transition SQL missing %q:\n%s", fragment, tx.execSQL[0])
				}
			}
			arguments := tx.execArgs[0]
			for _, expected := range []any{tt.fromState, tt.toState, recorddeletion.DeletionWorkPurgeOnline, tt.claim.Owner.ExpiresAt} {
				if !storeDeletionArgumentsContain(arguments, expected) {
					t.Errorf("retry transition args missing %#v: %#v", expected, arguments)
				}
			}
			if strings.Contains(fmt.Sprint(arguments), "error") || strings.Contains(fmt.Sprint(arguments), "failed") {
				t.Errorf("retry transition persisted error text: %#v", arguments)
			}
		})
	}

	invalidTx := &fakeRecordPlatformTx{}
	invalidRepository := testStoreDeletionRepository(invalidTx)
	if err := invalidRepository.MarkRetryRequired(context.Background(), purgeClaim, recorddeletion.DeletionWorkPromotePermanentFence); err == nil {
		t.Fatal("MarkRetryRequired() accepted a stage different from the claimed cut point")
	}
	if len(invalidTx.execSQL) != 0 || len(invalidTx.querySQL) != 0 {
		t.Fatalf("invalid retry touched database: queries=%d execs=%d", len(invalidTx.querySQL), len(invalidTx.execSQL))
	}
}

func TestPostgresRecordDeletionCoreHealthAndPreviewFailClosed(t *testing.T) {
	t.Parallel()

	t.Run("missing owned surface", func(t *testing.T) {
		tx := &fakeRecordPlatformTx{queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*int64) = int64(len(recorddeletion.RecordCoreSurfaceNames()) - 1)
				return nil
			}}
		}}
		repository := testStoreDeletionRepository(tx)
		if _, err := repository.RecordCoreHealth(context.Background()); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
			t.Fatalf("RecordCoreHealth() error = %v, want ErrDeletionSafetyUnavailable", err)
		}
		if tx.committed {
			t.Fatal("RecordCoreHealth() committed an invalid surface snapshot")
		}
	})

	t.Run("stale preview tuple", func(t *testing.T) {
		tx := &fakeRecordPlatformTx{}
		repository := testStoreDeletionRepository(tx)
		target := recorddeletion.PreviewTarget{
			Object:            recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_corepreview01"},
			CurrentRevisionID: "rrv_corepreview01", LockVersion: 2, AuthorizationEpoch: 3,
			ContentDeliveryEpoch: 4, DependencyGraphDigest: testStoreRecordPlatformDigest(0x52),
		}
		if _, err := repository.PreviewRecordCore(context.Background(), target); !errors.Is(err, recorddeletion.ErrDeletionPreviewStale) {
			t.Fatalf("PreviewRecordCore() error = %v, want ErrDeletionPreviewStale", err)
		}
		if tx.committed {
			t.Fatal("PreviewRecordCore() committed a stale preview snapshot")
		}
	})
}

func TestPostgresRecordDeletionPurgeRecordCoreDeletesExactSurfacesBeforeReceipt(t *testing.T) {
	t.Parallel()

	operation := testStoreDeletionOperation(recorddeletion.DeletionStateOnlinePurging)
	verifiedAt := time.Date(2026, time.August, 3, 16, 0, 0, 0, time.UTC)
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "for update of operation, reservation"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*string) = string(recorddeletion.DeletionStateOnlinePurging)
				*dest[1].(*string) = "committed"
				*dest[2].(*string) = operation.Object.ObjectID
				*dest[3].(*int64) = int64(operation.FenceEpoch)
				return nil
			}}
		case strings.Contains(sql, "from public.record_core_purge_receipts"):
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		case strings.Contains(sql, "as content_present"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*bool) = false
				return nil
			}}
		case strings.Contains(sql, "insert into public.record_core_purge_receipts"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*time.Time) = verifiedAt
				return nil
			}}
		default:
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		}
	}
	deleteOrder := []string{
		"record_draft_checkpoints",
		"record_drafts",
		"record_domain_activities",
		"record_revision_participants",
		"record_revision_tags",
		"record_revision_subjects",
		"record_revisions",
		"records",
		"content_delivery_epochs",
	}
	tx.exec = func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
		index := len(tx.execSQL) - 1
		if index == 0 {
			if !strings.Contains(sql, "update public.records") || !strings.Contains(sql, "current_revision_id = null") {
				t.Fatalf("core purge first SQL = %q, want current projection clear", sql)
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}
		deleteIndex := index - 1
		if deleteIndex >= len(deleteOrder) {
			t.Fatalf("unexpected core purge delete %d SQL = %q", deleteIndex, sql)
		}
		if !strings.Contains(sql, "delete from public."+deleteOrder[deleteIndex]) {
			t.Fatalf("core purge delete %d SQL = %q, want %s", deleteIndex, sql, deleteOrder[deleteIndex])
		}
		return pgconn.NewCommandTag("DELETE 1"), nil
	}

	repository := testStoreDeletionRepository(tx)
	adapter, err := recorddeletion.NewCoreAdapter(testStoreCorePurgeAdapter{repository: repository})
	if err != nil {
		t.Fatalf("NewCoreAdapter() error = %v", err)
	}
	receipt, err := adapter.PurgeDeletion(context.Background(), recorddeletion.PurgeTarget{Operation: operation})
	if err != nil {
		t.Fatalf("PurgeDeletion() error = %v", err)
	}
	if receipt.OperationID != operation.OperationID || receipt.AdapterName != recorddeletion.AdapterNameRecordCore ||
		receipt.RemovedRowCount != uint64(len(deleteOrder)) || !receipt.VerifiedAbsentAt.Equal(verifiedAt) {
		t.Fatalf("PurgeDeletion() receipt = %#v", receipt)
	}
	if len(tx.execSQL) != len(deleteOrder)+1 || !tx.committed {
		t.Fatalf("core purge mutations=%d committed=%t, want %d/true", len(tx.execSQL), tx.committed, len(deleteOrder)+1)
	}
	if len(tx.querySQL) != 4 || !strings.Contains(tx.querySQL[len(tx.querySQL)-1], "insert into public.record_core_purge_receipts") {
		t.Fatalf("core purge queries = %#v, want lock/receipt/absence/receipt-insert", tx.querySQL)
	}
	receiptInsertSQL := strings.ToLower(tx.querySQL[len(tx.querySQL)-1])
	for _, forbidden := range []string{"project_id", "record_id"} {
		if strings.Contains(receiptInsertSQL, forbidden) {
			t.Errorf("core purge receipt insert persists object identity field %q:\n%s", forbidden, receiptInsertSQL)
		}
	}
	receiptInsertArgs := tx.queryArgs[len(tx.queryArgs)-1]
	for _, forbidden := range []string{operation.Object.ProjectID, operation.Object.ObjectID} {
		if storeDeletionArgumentsContain(receiptInsertArgs, forbidden) {
			t.Errorf("core purge receipt insert persists object identity argument %q: %#v", forbidden, receiptInsertArgs)
		}
	}
	absenceSQL := strings.ToLower(tx.querySQL[2])
	for _, fragment := range []string{
		"from public.content_delivery_epochs",
		"project_id = $2",
		"object_kind = 'record'",
		"object_id = $1",
	} {
		if !strings.Contains(absenceSQL, fragment) {
			t.Errorf("core purge absence SQL missing %q:\n%s", fragment, tx.querySQL[2])
		}
	}
	for _, arguments := range append(append([][]any(nil), tx.execArgs...), tx.queryArgs...) {
		for _, argument := range arguments {
			if value, ok := argument.(string); ok && (strings.Contains(value, "markdown") || strings.Contains(value, "title")) {
				t.Fatalf("core purge persisted content-bearing argument %q", value)
			}
		}
	}

	verifyTx := &fakeRecordPlatformTx{}
	verifyTx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from public.record_core_purge_receipts"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*[]byte) = append([]byte(nil), receipt.SurfaceDigest[:]...)
				*dest[1].(*[]byte) = append([]byte(nil), receipt.ReceiptDigest[:]...)
				*dest[2].(*int64) = int64(receipt.RemovedRowCount)
				*dest[3].(*time.Time) = receipt.VerifiedAbsentAt
				return nil
			}}
		case strings.Contains(sql, "as content_present"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*bool) = false
				return nil
			}}
		default:
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		}
	}
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return verifyTx, nil }
	if err := adapter.VerifyDeletion(context.Background(), recorddeletion.PurgeTarget{Operation: operation}, receipt); err != nil {
		t.Fatalf("VerifyDeletion() error = %v", err)
	}
	if len(verifyTx.querySQL) != 2 || len(verifyTx.execSQL) != 0 || !verifyTx.committed {
		t.Fatalf("VerifyDeletion() queries=%d execs=%d committed=%t, want 2/0/true", len(verifyTx.querySQL), len(verifyTx.execSQL), verifyTx.committed)
	}

	for _, tt := range []struct {
		name       string
		receipt    recorddeletion.AdapterPurgeReceipt
		wantErr    bool
		wantCommit bool
	}{
		{name: "exact replay", receipt: receipt, wantCommit: true},
		{name: "corrupt persisted digest", receipt: func() recorddeletion.AdapterPurgeReceipt {
			corrupt := receipt
			corrupt.ReceiptDigest[0] ^= 0xff
			return corrupt
		}(), wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			replayTx := &fakeRecordPlatformTx{}
			replayTx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "for update of operation, reservation"):
					return fakeRecordPlatformRow{scan: func(dest ...any) error {
						*dest[0].(*string) = string(recorddeletion.DeletionStateOnlinePurging)
						*dest[1].(*string) = "committed"
						*dest[2].(*string) = operation.Object.ObjectID
						*dest[3].(*int64) = int64(operation.FenceEpoch)
						return nil
					}}
				case strings.Contains(sql, "from public.record_core_purge_receipts"):
					return fakeRecordPlatformRow{scan: func(dest ...any) error {
						*dest[0].(*[]byte) = append([]byte(nil), tt.receipt.SurfaceDigest[:]...)
						*dest[1].(*[]byte) = append([]byte(nil), tt.receipt.ReceiptDigest[:]...)
						*dest[2].(*int64) = int64(tt.receipt.RemovedRowCount)
						*dest[3].(*time.Time) = tt.receipt.VerifiedAbsentAt
						return nil
					}}
				default:
					return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
				}
			}
			repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return replayTx, nil }
			got, err := adapter.PurgeDeletion(context.Background(), recorddeletion.PurgeTarget{Operation: operation})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("PurgeDeletion() corrupt replay = %#v, want error", got)
				}
			} else if err != nil || got != receipt {
				t.Fatalf("PurgeDeletion() replay = (%#v, %v), want original receipt", got, err)
			}
			if len(replayTx.execSQL) != 0 || replayTx.committed != tt.wantCommit {
				t.Fatalf("replay execs=%d committed=%t, want 0/%t", len(replayTx.execSQL), replayTx.committed, tt.wantCommit)
			}
		})
	}
}

func TestRecordCorePurgeReceiptDigestDoesNotRetainObjectIdentity(t *testing.T) {
	t.Parallel()

	operation := testStoreDeletionOperation(recorddeletion.DeletionStateOnlinePurging)
	surfaceDigest := recorddeletion.RecordCoreSurfaceDigest()
	want := digestRecordCorePurgeReceipt(operation, surfaceDigest, 9)

	otherObject := operation
	otherObject.Object.ProjectID = "another_project"
	otherObject.Object.ObjectID = "rec_anotherpurge01"
	if got := digestRecordCorePurgeReceipt(otherObject, surfaceDigest, 9); got != want {
		t.Fatal("content-free core purge receipt digest changes with project/record identity")
	}
}

type testStoreCorePurgeAdapter struct {
	repository *PostgresRecordDeletionRepository
}

func (adapter testStoreCorePurgeAdapter) RecordCoreHealth(context.Context) (recorddeletion.AdapterHealthSnapshot, error) {
	return recorddeletion.AdapterHealthSnapshot{}, nil
}

func (adapter testStoreCorePurgeAdapter) PreviewRecordCore(context.Context, recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error) {
	return recorddeletion.AdapterPreviewSnapshot{}, nil
}

func (adapter testStoreCorePurgeAdapter) PurgeRecordCore(ctx context.Context, command recorddeletion.CorePurgeCommand) (recorddeletion.AdapterPurgeReceipt, error) {
	return adapter.repository.PurgeRecordCore(ctx, command)
}

func (adapter testStoreCorePurgeAdapter) VerifyRecordCorePurge(ctx context.Context, command recorddeletion.CorePurgeCommand, receipt recorddeletion.AdapterPurgeReceipt) error {
	return adapter.repository.VerifyRecordCorePurge(ctx, command, receipt)
}

func testStoreDeletionRepository(tx *fakeRecordPlatformTx) *PostgresRecordDeletionRepository {
	return &PostgresRecordDeletionRepository{
		platform: &PostgresRecordPlatformRepository{
			beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
			gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil }),
		},
	}
}

func testStoreDeletionOperation(state recorddeletion.DeletionState) recorddeletion.DeletionOperation {
	operation := recorddeletion.DeletionOperation{
		OperationID:   "rpo_corepurge01",
		ReservationID: "drs_corepurge01",
		Object: recordplatform.ObjectRef{
			ProjectID:  string(recordplatform.ProjectIDDefault),
			ObjectKind: "record",
			ObjectID:   "rec_corepurge01",
		},
		ReasonCode:      recorddeletion.DeletionReasonUserConfirmed,
		State:           state,
		FenceEpoch:      7,
		LedgerSequence:  11,
		LedgerEntryHash: testStoreRecordPlatformDigest(0x61),
	}
	return operation
}

func testStoreDeletionWorkerClaim(t *testing.T, stage recorddeletion.DeletionWorkStage) recorddeletion.ClaimedDeletionWork {
	t.Helper()
	command := testStoreDeletionReservePreviewCommand(t)
	operation := recorddeletion.DeletionOperation{
		OperationID:   "rpo_workercutpoint01",
		ReservationID: command.Preview.ReservationID,
		Object:        command.Preview.Object,
		ReasonCode:    command.ReasonCode,
		State:         recorddeletion.DeletionStateProvisionalFenced,
		FenceEpoch:    command.Record.ContentDeliveryEpoch + 1,
	}
	request := recorddeletion.LedgerAppendRequest{
		EntryType:               recorddeletion.LedgerEntryDeleteCommit,
		DeploymentID:            command.DeploymentID,
		ProjectID:               recordplatform.ProjectID(command.Preview.Object.ProjectID),
		OperationID:             operation.OperationID,
		ActorID:                 command.ActorID,
		Object:                  operation.Object,
		TokenCommitment:         command.Preview.TokenCommitment,
		RequestFingerprint:      command.Preview.RequestFingerprint,
		ReasonCode:              operation.ReasonCode,
		DeletionContractVersion: command.DeletionContractVersion,
	}
	claim := recorddeletion.ClaimedDeletionWork{
		Operation: operation,
		Owner: recordplatform.OwnerLease{
			OwnerID: "deletion_worker_01", Generation: 3,
			ExpiresAt: time.Date(2026, time.August, 3, 21, 0, 0, 0, time.UTC),
		},
		Stage: stage, Request: request,
	}
	switch stage {
	case recorddeletion.DeletionWorkResolveDeleteCommit:
		claim.Operation.State = recorddeletion.DeletionStateLedgerCommitUnknown
	case recorddeletion.DeletionWorkConfirmDeleteWitness:
		claim.Operation.State = recorddeletion.DeletionStateWitnessPending
		entry := testStoreDeletionLedgerEntry(t, request)
		claim.Entry = &entry
		claim.Operation.LedgerSequence = entry.Sequence
		claim.Operation.LedgerEntryHash = entry.EntryHash
	case recorddeletion.DeletionWorkResolveNotCommitted, recorddeletion.DeletionWorkConfirmNotCommittedWitness:
		claim.Operation.State = recorddeletion.DeletionStateReleasePending
		claim.Operation.ReleaseEpoch = 5
		claim.Request = request.AttemptNotCommitted(claim.Operation.ReleaseEpoch)
		if stage == recorddeletion.DeletionWorkConfirmNotCommittedWitness {
			entry := testStoreDeletionLedgerEntry(t, claim.Request)
			claim.Entry = &entry
			claim.Operation.LedgerSequence = entry.Sequence
			claim.Operation.LedgerEntryHash = entry.EntryHash
		}
	case recorddeletion.DeletionWorkPromotePermanentFence:
		claim.Operation.State = recorddeletion.DeletionStateDeleteRequested
		entry := testStoreDeletionLedgerEntry(t, request)
		claim.Operation.LedgerSequence = entry.Sequence
		claim.Operation.LedgerEntryHash = entry.EntryHash
	case recorddeletion.DeletionWorkPropagatePermanentFence,
		recorddeletion.DeletionWorkBeginOnlinePurge,
		recorddeletion.DeletionWorkPurgeOnline:
		entry := testStoreDeletionLedgerEntry(t, request)
		claim.Operation.LedgerSequence = entry.Sequence
		claim.Operation.LedgerEntryHash = entry.EntryHash
		switch stage {
		case recorddeletion.DeletionWorkPropagatePermanentFence:
			claim.Operation.State = recorddeletion.DeletionStateFencePropagating
		case recorddeletion.DeletionWorkBeginOnlinePurge:
			claim.Operation.State = recorddeletion.DeletionStateReadFenced
		case recorddeletion.DeletionWorkPurgeOnline:
			claim.Operation.State = recorddeletion.DeletionStateOnlinePurging
		}
	case recorddeletion.DeletionWorkResolveRetry:
		entry := testStoreDeletionLedgerEntry(t, request)
		claim.Operation.State = recorddeletion.DeletionStateRetryRequired
		claim.Operation.LedgerSequence = entry.Sequence
		claim.Operation.LedgerEntryHash = entry.EntryHash
		claim.RetryStage = recorddeletion.DeletionWorkPurgeOnline
	}
	if err := claim.Validate(); err != nil {
		t.Fatalf("test deletion worker claim invalid: %v", err)
	}
	return claim
}

func testStoreDeletionLedgerEntry(t *testing.T, request recorddeletion.LedgerAppendRequest) recorddeletion.DeletionLedgerEntry {
	t.Helper()
	entry := recorddeletion.DeletionLedgerEntry{
		Request: request, Sequence: 13, EntryHash: testStoreRecordPlatformDigest(0x6a),
	}
	if err := entry.Validate(); err != nil {
		t.Fatalf("test deletion ledger entry invalid: %v", err)
	}
	return entry
}

func storeDeletionArgumentsContain(arguments []any, expected any) bool {
	for _, argument := range arguments {
		if argument == expected {
			return true
		}
	}
	return false
}

func testStoreDeletionCreatePreviewCommand(t *testing.T) recorddeletion.CreatePreviewCommand {
	t.Helper()
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      recordplatform.OperationKindRecordPermanentDelete,
		ProjectID:          recordplatform.ProjectIDDefault,
		ActorScopeDigest:   testStoreRecordPlatformDigest(0x31),
		RequestScopeDigest: testStoreRecordPlatformDigest(0x32),
		PayloadDigest:      testStoreRecordPlatformDigest(0x33),
	})
	if err != nil {
		t.Fatalf("FingerprintRequestV1() error = %v", err)
	}
	return recorddeletion.CreatePreviewCommand{
		Object: recordplatform.ObjectRef{
			ProjectID:  string(recordplatform.ProjectIDDefault),
			ObjectKind: "record",
			ObjectID:   "rec_previewstore01",
		},
		ActorScopeDigest:   testStoreRecordPlatformDigest(0x31),
		TokenCommitment:    testStoreRecordPlatformDigest(0x34),
		RequestFingerprint: fingerprint,
		BindingDigest:      testStoreRecordPlatformDigest(0x35),
		Record: recorddeletion.DeletionRecordSnapshot{
			RecordID:                 "rec_previewstore01",
			CurrentRevisionID:        "rrv_previewstore01",
			LockVersion:              4,
			AuthorizationEpoch:       5,
			ContentDeliveryEpoch:     6,
			Authorization:            recordauth.ResourceScope{Version: recordauth.ResourceScopeVersionV1, ProjectID: recordauth.ProjectIDDefault},
			DependencyGraphDigest:    testStoreRecordPlatformDigest(0x36),
			BackupInventoryDigest:    testStoreRecordPlatformDigest(0x37),
			ProcessorInventoryDigest: testStoreRecordPlatformDigest(0x38),
		},
		WitnessHead:            recorddeletion.WitnessHead{Sequence: 7, EntryHash: testStoreRecordPlatformDigest(0x39)},
		AdapterReadinessDigest: testStoreRecordPlatformDigest(0x3a),
		AdapterPreviewDigest:   testStoreRecordPlatformDigest(0x3b),
		TTL:                    recorddeletion.DeletionPreviewTTL,
	}
}

func testStoreDeletionReservePreviewCommand(t *testing.T) recorddeletion.ReservePreviewCommand {
	t.Helper()
	create := testStoreDeletionCreatePreviewCommand(t)
	fingerprintBytes, err := create.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}
	persisted, err := recordplatform.ParseTrustedPersistedRequestFingerprintV1(fingerprintBytes[:])
	if err != nil {
		t.Fatalf("ParseTrustedPersistedRequestFingerprintV1() error = %v", err)
	}
	preview := recorddeletion.StoredPreview{
		ReservationID:      "drs_0123456789abcdef",
		Object:             create.Object,
		ActorScopeDigest:   create.ActorScopeDigest,
		TokenCommitment:    create.TokenCommitment,
		RequestFingerprint: persisted,
		BindingDigest:      create.BindingDigest,
		WitnessHead:        create.WitnessHead,
		ExpiresAt:          time.Date(2026, time.August, 3, 18, 10, 0, 0, time.UTC),
	}
	return recorddeletion.ReservePreviewCommand{
		DeploymentID:            recordplatform.DeploymentID("dp-" + strings.Repeat("a", 64)),
		ActorID:                 "usr_0123456789abcdef01234567",
		DeletionContractVersion: recorddeletion.RecordDeletionContractVersionV1,
		Preview:                 preview,
		Record:                  create.Record,
		ExpectedBindingDigest:   create.BindingDigest,
		RequestFingerprint:      create.RequestFingerprint,
		ObservedWitnessHead:     recorddeletion.WitnessHead{Sequence: 9, EntryHash: testStoreRecordPlatformDigest(0x4b)},
		OwnerID:                 "deletion_worker_01",
		OwnerLeaseDuration:      2 * time.Minute,
		ReasonCode:              recorddeletion.DeletionReasonUserConfirmed,
	}
}
