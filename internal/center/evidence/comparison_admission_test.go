package evidence

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestComparisonAdmissionRejectsOverBudgetAndSaturates(t *testing.T) {
	t.Parallel()

	admission, err := NewComparisonAdmission(ComparisonAdmissionTokenBytes)
	if err != nil {
		t.Fatalf("NewComparisonAdmission() error = %v", err)
	}
	_, err = admission.Acquire(context.Background(), ComparisonAdmissionTokenBytes+1)
	if !errors.Is(err, ErrComparisonRequestMemoryLimit) {
		t.Fatalf("over-budget error = %v, want %v", err, ErrComparisonRequestMemoryLimit)
	}

	release, err := admission.Acquire(context.Background(), ComparisonAdmissionTokenBytes)
	if err != nil {
		t.Fatalf("first acquire error = %v", err)
	}
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancelWait()
	_, err = admission.Acquire(waitCtx, ComparisonAdmissionTokenBytes)
	if !errors.Is(err, ErrComparisonCapacityExhausted) {
		t.Fatalf("saturated wait error = %v, want %v", err, ErrComparisonCapacityExhausted)
	}

	waited := make(chan error, 1)
	go func() {
		queued, acquireErr := admission.Acquire(context.Background(), ComparisonAdmissionTokenBytes)
		if acquireErr == nil {
			queued()
		}
		waited <- acquireErr
	}()
	time.Sleep(20 * time.Millisecond)
	release()
	select {
	case err := <-waited:
		if err != nil {
			t.Fatalf("queued acquire after release error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued acquire did not wake after release")
	}
}

func TestComparisonAdmissionCancelDrainsTokens(t *testing.T) {
	t.Parallel()

	admission, err := NewComparisonAdmission(ComparisonAdmissionTokenBytes)
	if err != nil {
		t.Fatalf("NewComparisonAdmission() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := admission.Acquire(ctx, ComparisonAdmissionTokenBytes); err != nil {
		t.Fatalf("acquire error = %v", err)
	}
	cancel()

	deadline := time.Now().Add(time.Second)
	var recovered error
	for time.Now().Before(deadline) {
		release, err := admission.Acquire(context.Background(), ComparisonAdmissionTokenBytes)
		if err == nil {
			release()
			return
		}
		recovered = err
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cancel did not drain tokens within 1s: last error = %v", recovered)
}

func TestComparisonAdmissionRejectsQueueOverflow(t *testing.T) {
	t.Parallel()

	admission, err := NewComparisonAdmission(ComparisonAdmissionTokenBytes)
	if err != nil {
		t.Fatalf("NewComparisonAdmission() error = %v", err)
	}
	hold, err := admission.Acquire(context.Background(), ComparisonAdmissionTokenBytes)
	if err != nil {
		t.Fatalf("hold acquire error = %v", err)
	}
	defer hold()

	waitCtx, cancelWait := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelWait()
	for index := 0; index < ComparisonAdmissionMaxQueue; index++ {
		go func() {
			release, acquireErr := admission.Acquire(waitCtx, ComparisonAdmissionTokenBytes)
			if acquireErr == nil {
				release()
			}
		}()
	}
	time.Sleep(150 * time.Millisecond)
	started := time.Now()
	_, err = admission.Acquire(context.Background(), ComparisonAdmissionTokenBytes)
	if !errors.Is(err, ErrComparisonCapacityExhausted) {
		t.Fatalf("overflow error = %v, want %v", err, ErrComparisonCapacityExhausted)
	}
	if time.Since(started) > 200*time.Millisecond {
		t.Fatalf("overflow waited %s instead of rejecting a full queue", time.Since(started))
	}
}

func TestComparisonAdmissionWaitTimeout(t *testing.T) {
	t.Parallel()

	admission, err := NewComparisonAdmission(ComparisonAdmissionTokenBytes)
	if err != nil {
		t.Fatalf("NewComparisonAdmission() error = %v", err)
	}
	hold, err := admission.Acquire(context.Background(), ComparisonAdmissionTokenBytes)
	if err != nil {
		t.Fatalf("hold acquire error = %v", err)
	}
	defer hold()

	started := time.Now()
	_, err = admission.Acquire(context.Background(), ComparisonAdmissionTokenBytes)
	if !errors.Is(err, ErrComparisonCapacityExhausted) {
		t.Fatalf("timed wait error = %v, want %v", err, ErrComparisonCapacityExhausted)
	}
	elapsed := time.Since(started)
	if elapsed < ComparisonAdmissionWait || elapsed > ComparisonAdmissionWait+time.Second {
		t.Fatalf("timed wait = %s, want about %s", elapsed, ComparisonAdmissionWait)
	}
}
