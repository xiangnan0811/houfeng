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

func TestLeaseWorkGuardV1NilRenewerPermanentlyStopsWork(t *testing.T) {
	now := time.Date(2026, time.July, 24, 15, 0, 0, 0, time.UTC)
	clock := &fakeLeaseClock{now: now}
	guard, err := NewLeaseWorkGuardV1(clock, OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("NewLeaseWorkGuardV1() error = %v", err)
	}

	if err := guard.Renew(nil); !errors.Is(err, ErrLeaseRenewalStopped) {
		t.Fatalf("LeaseWorkGuardV1.Renew(nil) error = %v, want ErrLeaseRenewalStopped", err)
	}
	if guard.CanContinue() {
		t.Fatal("nil renewal callback must permanently stop work before the observed expiry")
	}

	renewCalled := false
	err = guard.Renew(func(owner OwnerLease) (OwnerLease, error) {
		renewCalled = true
		owner.ExpiresAt = now.Add(2 * time.Minute)
		return owner, nil
	})
	if !errors.Is(err, ErrLeaseRenewalStopped) {
		t.Fatalf("LeaseWorkGuardV1.Renew() after nil renewer error = %v, want ErrLeaseRenewalStopped", err)
	}
	if renewCalled {
		t.Fatal("stopped work guard must not invoke a later renewal callback")
	}
	if guard.CanContinue() {
		t.Fatal("successful renewal attempt must not revive work after a nil renewal callback")
	}
}

func TestLeaseWorkGuardV1NilRenewerStopsInFlightRenewal(t *testing.T) {
	now := time.Date(2026, time.July, 24, 15, 0, 0, 0, time.UTC)
	clock := &fakeLeaseClock{now: now}
	guard, err := NewLeaseWorkGuardV1(clock, OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("NewLeaseWorkGuardV1() error = %v", err)
	}

	renewStarted := make(chan struct{})
	finishRenewal := make(chan struct{})
	renewResult := make(chan error, 1)
	go func() {
		renewResult <- guard.Renew(func(owner OwnerLease) (OwnerLease, error) {
			close(renewStarted)
			<-finishRenewal
			owner.ExpiresAt = now.Add(2 * time.Minute)
			return owner, nil
		})
	}()
	<-renewStarted

	nilRenewResult := make(chan error, 1)
	go func() {
		nilRenewResult <- guard.Renew(nil)
	}()

	select {
	case err = <-nilRenewResult:
	case <-time.After(time.Second):
		close(finishRenewal)
		t.Fatal("LeaseWorkGuardV1.Renew(nil) blocked behind an in-flight renewal")
	}
	close(finishRenewal)
	if !errors.Is(err, ErrLeaseRenewalStopped) {
		t.Fatalf("LeaseWorkGuardV1.Renew(nil) error = %v, want ErrLeaseRenewalStopped", err)
	}
	if err = <-renewResult; !errors.Is(err, ErrLeaseRenewalStopped) {
		t.Fatalf("in-flight LeaseWorkGuardV1.Renew() error = %v, want ErrLeaseRenewalStopped", err)
	}
	if guard.CanContinue() {
		t.Fatal("successful in-flight renewal must not revive work after a nil renewal callback")
	}
}

func TestLeaseWorkGuardV1NilReceiverFailsClosed(t *testing.T) {
	var guard *LeaseWorkGuardV1
	if guard.CanContinue() {
		t.Fatal("nil LeaseWorkGuardV1 receiver must not allow work")
	}
	if err := guard.Renew(nil); !errors.Is(err, ErrLeaseRenewalStopped) {
		t.Fatalf("nil LeaseWorkGuardV1.Renew(nil) error = %v, want ErrLeaseRenewalStopped", err)
	}

	renewCalled := false
	err := guard.Renew(func(owner OwnerLease) (OwnerLease, error) {
		renewCalled = true
		return owner, nil
	})
	if !errors.Is(err, ErrLeaseRenewalStopped) {
		t.Fatalf("nil LeaseWorkGuardV1.Renew() error = %v, want ErrLeaseRenewalStopped", err)
	}
	if renewCalled {
		t.Fatal("nil LeaseWorkGuardV1 receiver must not invoke renewal callback")
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
