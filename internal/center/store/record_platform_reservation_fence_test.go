package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordplatform"
)

func TestPostgresRecordPlatformFenceDeletionReservationLocksInRequiredOrderAndBindsOneGeneration(t *testing.T) {
	now := time.Date(2026, time.July, 24, 17, 0, 0, 0, time.UTC)
	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from public.deletion_reservations"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "previewed"
				*(dest[1].(*string)) = "default"
				*(dest[2].(*string)) = "record"
				*(dest[3].(*string)) = "rec_01"
				*(dest[4].(*int64)) = 0
				*(dest[5].(**time.Time)) = nil
				*(dest[6].(*time.Time)) = now.Add(time.Hour)
				return nil
			}}
		case strings.Contains(sql, "from public.content_delivery_epochs"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*int64)) = 3
				return nil
			}}
		case strings.Contains(sql, "from public.deletion_fence_leases"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "expired_owner"
				*(dest[1].(*int64)) = 3
				*(dest[2].(*time.Time)) = now.Add(-time.Minute)
				*(dest[3].(*bool)) = false
				return nil
			}}
		case strings.Contains(sql, "from public.object_content_leases"):
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		case strings.Contains(sql, "update public.content_delivery_epochs"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*int64)) = 4
				return nil
			}}
		case strings.Contains(sql, "update public.deletion_reservations"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "worker_01"
				*(dest[1].(*int64)) = 4
				*(dest[2].(*time.Time)) = now.Add(time.Minute)
				return nil
			}}
		case strings.Contains(sql, "insert into public.deletion_fence_leases"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "worker_01"
				*(dest[1].(*int64)) = 4
				*(dest[2].(*time.Time)) = now.Add(time.Minute)
				return nil
			}}
		default:
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected query") }}
		}
	}
	repository := newReservationFenceTestRepository(tx)

	fence, err := repository.FenceDeletionReservation(context.Background(), recordplatform.ReservationFenceInputV1{
		ReservationID:      "drs_01",
		Object:             recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"},
		OwnerID:            "worker_01",
		OwnerLeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("FenceDeletionReservation() error = %v", err)
	}
	if fence.FenceEpoch != 4 || fence.Owner.OwnerID != "worker_01" || fence.Owner.Generation != 4 || !fence.Owner.ExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("FenceDeletionReservation() = %#v, want epoch 4 and shared generation 4", fence)
	}
	if !tx.committed {
		t.Fatal("FenceDeletionReservation() did not commit the completed fence transaction")
	}
	if len(tx.querySQL) < 7 {
		t.Fatalf("reservation fence queries = %#v, want lock and update sequence", tx.querySQL)
	}
	wantRelations := []string{
		"public.deletion_reservations",
		"public.content_delivery_epochs",
		"public.deletion_fence_leases",
		"public.object_content_leases",
	}
	for index, relation := range wantRelations {
		if !strings.Contains(tx.querySQL[index], relation) || !strings.Contains(tx.querySQL[index], "for update") {
			t.Fatalf("lock query %d = %q, want FOR UPDATE on %s", index, tx.querySQL[index], relation)
		}
	}
	for _, fragment := range []string{"state = 'fenced'", "fence_epoch", "owner_id", "owner_generation", "owner_expires_at"} {
		if !strings.Contains(tx.querySQL[5], fragment) {
			t.Fatalf("reservation update missing %q:\n%s", fragment, tx.querySQL[5])
		}
	}
}

func TestPostgresRecordPlatformFenceDeletionReservationFailsClosedForMissingEpochAndLiveLeases(t *testing.T) {
	now := time.Date(2026, time.July, 24, 17, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name         string
		wantErr      error
		fenceLease   bool
		objectLease  bool
		missingEpoch bool
	}{
		{name: "missing content epoch", wantErr: recordplatform.ErrContentDeliveryEpochMissing, missingEpoch: true},
		{name: "live deletion fence", wantErr: recordplatform.ErrDeletionFenceLeaseLive, fenceLease: true},
		{name: "live object content lease", wantErr: recordplatform.ErrObjectContentLeaseLive, objectLease: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeRecordPlatformTx{}
			tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "from public.deletion_reservations"):
					return previewedReservationRow(now)
				case strings.Contains(sql, "from public.content_delivery_epochs"):
					if test.missingEpoch {
						return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
					}
					return fakeRecordPlatformRow{scan: func(dest ...any) error { *(dest[0].(*int64)) = 3; return nil }}
				case strings.Contains(sql, "from public.deletion_fence_leases"):
					if !test.fenceLease {
						return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
					}
					return liveDeletionFenceLeaseRow(now)
				case strings.Contains(sql, "from public.object_content_leases"):
					if !test.objectLease {
						return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
					}
					return liveObjectLeaseRow(now)
				default:
					return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected mutation after fail-closed state") }}
				}
			}
			repository := newReservationFenceTestRepository(tx)
			_, err := repository.FenceDeletionReservation(context.Background(), recordplatform.ReservationFenceInputV1{
				ReservationID:      "drs_01",
				Object:             recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"},
				OwnerID:            "worker_01",
				OwnerLeaseDuration: time.Minute,
			})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("FenceDeletionReservation() error = %v, want %v", err, test.wantErr)
			}
			if tx.committed || len(tx.querySQL) > 4 {
				t.Fatalf("fail-closed transaction committed=%v queries=%#v, want no mutation", tx.committed, tx.querySQL)
			}
		})
	}
}

func newReservationFenceTestRepository(tx *fakeRecordPlatformTx) *PostgresRecordPlatformRepository {
	return &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil }),
	}
}

func previewedReservationRow(now time.Time) pgx.Row {
	return fakeRecordPlatformRow{scan: func(dest ...any) error {
		*(dest[0].(*string)) = "previewed"
		*(dest[1].(*string)) = "default"
		*(dest[2].(*string)) = "record"
		*(dest[3].(*string)) = "rec_01"
		*(dest[4].(*int64)) = 0
		*(dest[5].(**time.Time)) = nil
		*(dest[6].(*time.Time)) = now.Add(time.Hour)
		return nil
	}}
}

func liveObjectLeaseRow(now time.Time) pgx.Row {
	return fakeRecordPlatformRow{scan: func(dest ...any) error {
		*(dest[0].(*string)) = "worker_02"
		*(dest[1].(*int64)) = 3
		*(dest[2].(*time.Time)) = now.Add(time.Minute)
		*(dest[3].(*bool)) = true
		return nil
	}}
}

func liveDeletionFenceLeaseRow(now time.Time) pgx.Row {
	return fakeRecordPlatformRow{scan: func(dest ...any) error {
		*(dest[0].(*string)) = "worker_02"
		*(dest[1].(*int64)) = 3
		*(dest[2].(*time.Time)) = now.Add(time.Minute)
		*(dest[3].(*bool)) = true
		return nil
	}}
}
