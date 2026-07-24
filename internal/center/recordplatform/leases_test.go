package recordplatform

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestServingLeaseV1IsOnlyADatabaseAssertionToken(t *testing.T) {
	now := time.Date(2026, time.July, 24, 15, 0, 0, 0, time.UTC)
	objectLease := ObjectContentLeaseV1{
		Object: ObjectRef{ProjectID: string(ProjectIDDefault), ObjectKind: "record", ObjectID: "rec_01"},
		Owner:  OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: now.Add(time.Minute)},
	}
	serving, err := NewServingLeaseV1(objectLease, 4)
	if err != nil {
		t.Fatalf("NewServingLeaseV1() error = %v", err)
	}
	if serving.Object != objectLease.Object || serving.Owner != objectLease.Owner || serving.CapturedEpoch != 4 {
		t.Fatalf("NewServingLeaseV1() = %#v, want only the acquired object lease and captured epoch", serving)
	}
	if _, exists := reflect.TypeFor[ServingLeaseV1]().MethodByName("ValidAt"); exists {
		t.Fatal("ServingLeaseV1 must not authorize from caller-supplied epoch or deletion-fence observations")
	}
}

func TestClientContentLeaseV1CannotCarryObjectServingAuthority(t *testing.T) {
	clientType := reflect.TypeFor[ClientContentLeaseV1]()
	for _, forbidden := range []string{"Object", "ObjectKind", "ObjectID", "CapturedEpoch"} {
		if _, exists := clientType.FieldByName(forbidden); exists {
			t.Fatalf("ClientContentLeaseV1 must not expose object serving field %q", forbidden)
		}
	}
}

func TestLeaseWorkGuardV1StopsBeforeObservedExpiryWhenRenewalFails(t *testing.T) {
	now := time.Date(2026, time.July, 24, 15, 0, 0, 0, time.UTC)
	clock := &fakeLeaseClock{now: now}
	guard, err := NewLeaseWorkGuardV1(clock, OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("NewLeaseWorkGuardV1() error = %v", err)
	}
	if !guard.CanContinue() {
		t.Fatal("new live lease work guard must allow work")
	}
	if err := guard.Renew(func(OwnerLease) (OwnerLease, error) { return OwnerLease{}, errors.New("database renewal failed") }); !errors.Is(err, ErrLeaseRenewalStopped) {
		t.Fatalf("LeaseWorkGuardV1.Renew() error = %v, want ErrLeaseRenewalStopped", err)
	}
	if clock.Now().After(now.Add(time.Second)) {
		t.Fatal("fake clock unexpectedly advanced")
	}
	if guard.CanContinue() {
		t.Fatal("failed renewal must stop work before the previous observed expiry")
	}
}

func TestLeaseWorkGuardV1RejectsTypedNilClock(t *testing.T) {
	now := time.Date(2026, time.July, 24, 15, 0, 0, 0, time.UTC)
	owner := OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: now.Add(time.Minute)}
	var clock *fakeLeaseClock

	guard, err := NewLeaseWorkGuardV1(clock, owner)
	if guard != nil || !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("NewLeaseWorkGuardV1() = (%#v, %v), want nil, ErrInvalidLease", guard, err)
	}
	if owner.LocallyLive(clock) {
		t.Fatal("OwnerLease.LocallyLive() accepted a typed-nil clock")
	}
}

func TestLeaseWorkGuardV1SynchronizesRenewAndCanContinue(t *testing.T) {
	now := time.Date(2026, time.July, 24, 15, 0, 0, 0, time.UTC)
	clock := &fakeLeaseClock{now: now}
	guard, err := NewLeaseWorkGuardV1(clock, OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatalf("NewLeaseWorkGuardV1() error = %v", err)
	}

	const iterations = 10_000
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		for range iterations {
			if err := guard.Renew(func(owner OwnerLease) (OwnerLease, error) {
				owner.ExpiresAt = now.Add(time.Hour)
				return owner, nil
			}); err != nil {
				t.Errorf("LeaseWorkGuardV1.Renew() error = %v", err)
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		for range iterations {
			_ = guard.CanContinue()
		}
	}()
	close(start)
	workers.Wait()
}

func TestDeletionReservationFenceV1RejectsZeroFenceEpoch(t *testing.T) {
	fence := DeletionReservationFenceV1{
		ReservationID: "drs_01",
		Object:        ObjectRef{ProjectID: string(ProjectIDDefault), ObjectKind: "record", ObjectID: "rec_01"},
		Owner: OwnerLease{
			OwnerID:    "worker_01",
			Generation: 2,
			ExpiresAt:  time.Date(2026, time.July, 24, 16, 1, 0, 0, time.UTC),
		},
	}
	if err := fence.Validate(); !errors.Is(err, ErrInvalidReservationFence) {
		t.Fatalf("DeletionReservationFenceV1.Validate() error = %v, want ErrInvalidReservationFence", err)
	}
}

type fakeLeaseClock struct {
	now time.Time
}

func (clock *fakeLeaseClock) Now() time.Time {
	return clock.now
}
