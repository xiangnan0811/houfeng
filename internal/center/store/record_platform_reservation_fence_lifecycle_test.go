package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordplatform"
)

func TestPostgresRecordPlatformReservationFenceLifecycleUsesSharedLiveOwnerFence(t *testing.T) {
	expiresAt := time.Date(2026, time.July, 24, 18, 1, 0, 0, time.UTC)
	fence := recordplatform.DeletionReservationFenceV1{
		ReservationID: "drs_01",
		Object:        recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"},
		FenceEpoch:    4,
		Owner:         recordplatform.OwnerLease{OwnerID: "worker_01", Generation: 4, ExpiresAt: expiresAt.Add(-time.Minute)},
	}
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		if !strings.Contains(sql, "update public.deletion_reservations") && !strings.Contains(sql, "update public.deletion_fence_leases") {
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected query") }}
		}
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			*(dest[0].(*string)) = "worker_01"
			*(dest[1].(*int64)) = 4
			*(dest[2].(*time.Time)) = expiresAt
			return nil
		}}
	}
	repository := newReservationFenceTestRepository(tx)

	renewed, err := repository.RenewDeletionReservationFence(context.Background(), fence, time.Minute)
	if err != nil {
		t.Fatalf("RenewDeletionReservationFence() error = %v", err)
	}
	if renewed.FenceEpoch != fence.FenceEpoch || renewed.Owner.OwnerID != fence.Owner.OwnerID || renewed.Owner.Generation != fence.Owner.Generation || !renewed.Owner.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("RenewDeletionReservationFence() = %#v, want renewed shared owner", renewed)
	}
	if len(tx.querySQL) != 2 || !strings.Contains(tx.querySQL[0], "public.deletion_reservations") || !strings.Contains(tx.querySQL[1], "public.deletion_fence_leases") {
		t.Fatalf("renew SQL = %#v, want reservation then fence lease", tx.querySQL)
	}
	for _, sql := range tx.querySQL {
		for _, fragment := range []string{"owner_id", "owner_generation", "expires_at > transaction_timestamp()"} {
			if !strings.Contains(sql, fragment) {
				t.Fatalf("renew SQL missing %q:\n%s", fragment, sql)
			}
		}
	}
}

func TestPostgresRecordPlatformReleaseAndAssertReservationFenceAreFenced(t *testing.T) {
	fence := recordplatform.DeletionReservationFenceV1{
		ReservationID: "drs_01",
		Object:        recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"},
		FenceEpoch:    4,
		Owner:         recordplatform.OwnerLease{OwnerID: "worker_01", Generation: 4, ExpiresAt: time.Date(2026, time.July, 24, 18, 0, 0, 0, time.UTC)},
	}
	t.Run("release", func(t *testing.T) {
		tx := &fakeRecordPlatformTx{exec: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}}
		repository := newReservationFenceTestRepository(tx)
		if err := repository.ReleaseDeletionReservationFence(context.Background(), fence); err != nil {
			t.Fatalf("ReleaseDeletionReservationFence() error = %v", err)
		}
		if len(tx.execSQL) != 2 || !strings.Contains(tx.execSQL[0], "public.deletion_reservations") || !strings.Contains(tx.execSQL[1], "public.deletion_fence_leases") {
			t.Fatalf("release SQL = %#v, want reservation then fence lease", tx.execSQL)
		}
		for _, fragment := range []string{"state = 'released'", "owner_id = ''", "owner_generation = 0", "owner_expires_at = null", "expires_at > transaction_timestamp()"} {
			if !strings.Contains(tx.execSQL[0], fragment) {
				t.Fatalf("release reservation SQL missing %q:\n%s", fragment, tx.execSQL[0])
			}
		}
	})
	t.Run("stale assert", func(t *testing.T) {
		tx := &fakeRecordPlatformTx{queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		}}
		repository := newReservationFenceTestRepository(tx)
		if err := repository.AssertDeletionReservationFence(context.Background(), fence); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("AssertDeletionReservationFence() error = %v, want ErrLostOwnerLease", err)
		}
	})
}

func TestPostgresRecordPlatformReservationFenceLifecycleBindsObservedOwnerExpiry(t *testing.T) {
	expiresAt := time.Date(2026, time.July, 24, 18, 1, 0, 0, time.UTC)
	fence := recordplatform.DeletionReservationFenceV1{
		ReservationID: "drs_01",
		Object:        recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"},
		FenceEpoch:    4,
		Owner:         recordplatform.OwnerLease{OwnerID: "worker_01", Generation: 4, ExpiresAt: expiresAt.Add(-time.Minute)},
	}
	t.Run("renew", func(t *testing.T) {
		tx := &fakeRecordPlatformTx{queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = fence.Owner.OwnerID
				*(dest[1].(*int64)) = int64(fence.Owner.Generation)
				*(dest[2].(*time.Time)) = expiresAt
				return nil
			}}
		}}
		repository := newReservationFenceTestRepository(tx)
		if _, err := repository.RenewDeletionReservationFence(context.Background(), fence, time.Minute); err != nil {
			t.Fatalf("RenewDeletionReservationFence() error = %v", err)
		}
		if len(tx.querySQL) != 2 || !strings.Contains(tx.querySQL[0], "and owner_expires_at = $") || !strings.Contains(tx.querySQL[1], "and expires_at = $") {
			t.Fatalf("renew SQL = %#v, want exact observed owner-expiry predicates", tx.querySQL)
		}
		for _, arguments := range tx.queryArgs {
			assertRecordPlatformOwnerExpiryArgument(t, arguments, fence.Owner.ExpiresAt)
		}
	})
	t.Run("release", func(t *testing.T) {
		tx := &fakeRecordPlatformTx{exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}}
		repository := newReservationFenceTestRepository(tx)
		if err := repository.ReleaseDeletionReservationFence(context.Background(), fence); err != nil {
			t.Fatalf("ReleaseDeletionReservationFence() error = %v", err)
		}
		if len(tx.execSQL) != 2 || !strings.Contains(tx.execSQL[0], "and owner_expires_at = $") || !strings.Contains(tx.execSQL[1], "and expires_at = $") {
			t.Fatalf("release SQL = %#v, want exact observed owner-expiry predicates", tx.execSQL)
		}
		for _, arguments := range tx.execArgs {
			assertRecordPlatformOwnerExpiryArgument(t, arguments, fence.Owner.ExpiresAt)
		}
	})
	t.Run("assert", func(t *testing.T) {
		tx := &fakeRecordPlatformTx{queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeRecordPlatformRow{scan: func(dest ...any) error { *(dest[0].(*int)) = 1; return nil }}
		}}
		repository := newReservationFenceTestRepository(tx)
		if err := repository.AssertDeletionReservationFence(context.Background(), fence); err != nil {
			t.Fatalf("AssertDeletionReservationFence() error = %v", err)
		}
		if len(tx.querySQL) != 1 || !strings.Contains(tx.querySQL[0], "reservation.owner_expires_at = $") || !strings.Contains(tx.querySQL[0], "lease.expires_at = $") {
			t.Fatalf("assert SQL = %#v, want exact observed owner-expiry predicates", tx.querySQL)
		}
		assertRecordPlatformOwnerExpiryArgument(t, tx.queryArgs[0], fence.Owner.ExpiresAt)
	})
}

func TestPostgresRecordPlatformAssertDeletionReservationFenceRejectsCopiedObject(t *testing.T) {
	storedObject := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"}
	copiedObject := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_02"}
	fence := recordplatform.DeletionReservationFenceV1{
		ReservationID: "drs_01",
		Object:        copiedObject,
		FenceEpoch:    4,
		Owner: recordplatform.OwnerLease{
			OwnerID:    "worker_01",
			Generation: 4,
			ExpiresAt:  time.Date(2026, time.July, 24, 18, 0, 0, 0, time.UTC),
		},
	}
	tx := &fakeRecordPlatformTx{queryRow: func(_ context.Context, _ string, arguments ...any) pgx.Row {
		// Before object binding, the query contains no caller object values and
		// therefore returns the stored object-A reservation for object B as well.
		if len(arguments) == 8 {
			boundObject, ok := reservationFenceAssertionObject(arguments)
			if !ok || boundObject != storedObject {
				return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
			}
		}
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			*(dest[0].(*int)) = 1
			return nil
		}}
	}}
	repository := newReservationFenceTestRepository(tx)

	err := repository.AssertDeletionReservationFence(context.Background(), fence)
	if !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Errorf("AssertDeletionReservationFence() copied object error = %v, want ErrLostOwnerLease", err)
	}
	if len(tx.querySQL) != 1 || len(tx.queryArgs) != 1 {
		t.Fatalf("assertion queries = SQL %#v args %#v, want one query", tx.querySQL, tx.queryArgs)
	}
	for _, predicate := range []string{
		"reservation.project_id = $2",
		"reservation.object_kind = $3",
		"reservation.object_id = $4",
		"reservation.owner_id = $5",
		"reservation.owner_generation = $6",
		"reservation.fence_epoch = $7",
		"reservation.owner_expires_at = $8",
		"lease.owner_id = $5",
		"lease.owner_generation = $6",
		"lease.expires_at = $8",
	} {
		if !strings.Contains(tx.querySQL[0], predicate) {
			t.Errorf("assertion SQL missing exact predicate %q:\n%s", predicate, tx.querySQL[0])
		}
	}
	arguments := tx.queryArgs[0]
	if len(arguments) != 8 {
		t.Errorf("assertion arguments = %#v, want reservation, object tuple, owner, generation, epoch, expiry", arguments)
		return
	}
	if arguments[0] != fence.ReservationID || arguments[1] != fence.Object.ProjectID || arguments[2] != fence.Object.ObjectKind || arguments[3] != fence.Object.ObjectID || arguments[4] != fence.Owner.OwnerID || arguments[5] != fence.Owner.Generation || arguments[6] != fence.FenceEpoch {
		t.Errorf("assertion arguments = %#v, want exact copied-object fence binding", arguments)
	}
	if expiresAt, ok := arguments[7].(time.Time); !ok || !expiresAt.Equal(fence.Owner.ExpiresAt) {
		t.Errorf("assertion expiry argument = %#v, want %s", arguments[7], fence.Owner.ExpiresAt)
	}
}

func reservationFenceAssertionObject(arguments []any) (recordplatform.ObjectRef, bool) {
	projectID, projectOK := arguments[1].(string)
	objectKind, kindOK := arguments[2].(string)
	objectID, objectOK := arguments[3].(string)
	return recordplatform.ObjectRef{ProjectID: projectID, ObjectKind: objectKind, ObjectID: objectID}, projectOK && kindOK && objectOK
}
