package ipquality

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	centersettings "houfeng/internal/center/settings"
)

type workerIntelligenceRepo struct {
	mu        sync.Mutex
	claims    []IntelligenceClaim
	eligible  bool
	completed chan IntelligenceSnapshot
	claimed   chan string
	claimErr  error
}

func (r *workerIntelligenceRepo) ClaimIPQualityIntelligence(ctx context.Context, owner string) (*IntelligenceClaim, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.claimed != nil {
		select {
		case r.claimed <- owner:
		default:
		}
	}
	if r.claimErr != nil {
		return nil, r.claimErr
	}
	if len(r.claims) == 0 {
		return nil, nil
	}
	claim := r.claims[0]
	r.claims = r.claims[1:]
	claim.OwnerID = owner
	return &claim, nil
}
func (r *workerIntelligenceRepo) CompleteIPQualityIntelligence(ctx context.Context, _ IntelligenceClaim, snapshot IntelligenceSnapshot) error {
	select {
	case r.completed <- snapshot:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *workerIntelligenceRepo) IPQualityIntelligenceEligible(context.Context, IntelligenceClaim) (bool, error) {
	return r.eligible, nil
}
func (r *workerIntelligenceRepo) LoadIPQualitySourceCache(context.Context, string, string, string) (*SourceCacheEntry, error) {
	return nil, nil
}

type workerIntelligenceSettings struct {
	value centersettings.CenterSettings
	err   error
}

func (s workerIntelligenceSettings) GetSettings(context.Context) (centersettings.CenterSettings, error) {
	return s.value, s.err
}
func newWorkerTest(t *testing.T, repo *workerIntelligenceRepo) *IntelligenceWorker {
	t.Helper()
	worker, err := NewIntelligenceWorker(repo, workerIntelligenceSettings{value: centersettings.Default()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return worker
}
func workerTestClaim() IntelligenceClaim {
	return IntelligenceClaim{ReportID: "report", DeadlineAt: time.Now().Add(10 * time.Minute), LeaseUntil: time.Now().Add(120 * time.Second)}
}

func TestIntelligenceWorkerImmediateSerialClaimsAndWake(t *testing.T) {
	repo := &workerIntelligenceRepo{eligible: true, completed: make(chan IntelligenceSnapshot, 3), claims: []IntelligenceClaim{workerTestClaim(), workerTestClaim()}, claimed: make(chan string, 8)}
	worker := newWorkerTest(t, repo)
	other := newWorkerTest(t, &workerIntelligenceRepo{})
	if other.ownerID == worker.ownerID {
		t.Fatal("process-local workers share ownership identity")
	}
	worker.interval = time.Hour
	active := 0
	worker.collect = func(ctx context.Context, _ IntelligenceClaim, _ centersettings.IPQualitySettings) IntelligenceSnapshot {
		active++
		defer func() { active-- }()
		if active != 1 {
			t.Error("overlapping jobs")
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 60*time.Second {
			t.Error("missing collection budget")
		}
		return IntelligenceSnapshot{State: "complete"}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	for range 2 {
		select {
		case <-repo.completed:
		case <-time.After(time.Second):
			t.Fatal("worker did not immediately drain serial claims")
		}
	}
	repo.mu.Lock()
	repo.claims = append(repo.claims, workerTestClaim())
	repo.mu.Unlock()
	worker.Wake()
	worker.Wake()
	select {
	case <-repo.completed:
	case <-time.After(time.Second):
		t.Fatal("coalesced wake did not trigger claim")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestIntelligenceWorkerDeadlineAndShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "budget", true: "shutdown"}[shutdown], func(t *testing.T) {
			repo := &workerIntelligenceRepo{eligible: true, completed: make(chan IntelligenceSnapshot, 1)}
			worker := newWorkerTest(t, repo)
			claim := workerTestClaim()
			claim.DeadlineAt = time.Now().Add(30 * time.Millisecond)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			worker.collect = func(child context.Context, _ IntelligenceClaim, _ centersettings.IPQualitySettings) IntelligenceSnapshot {
				deadline, _ := child.Deadline()
				if !deadline.Equal(claim.DeadlineAt) {
					t.Error("claim deadline was not respected")
				}
				if shutdown {
					cancel()
				}
				<-child.Done()
				return IntelligenceSnapshot{State: "failed"}
			}
			worker.runClaim(ctx, claim)
			if shutdown {
				select {
				case <-repo.completed:
					t.Fatal("shutdown finalized a recoverable claim")
				default:
				}
			} else {
				select {
				case result := <-repo.completed:
					if result.State != "failed" {
						t.Fatal("timeout was reported as success")
					}
				default:
					t.Fatal("network budget timeout prevented DB finalization")
				}
			}
		})
	}
}

func TestIntelligenceWorkerLifecycleAndSettingsFailure(t *testing.T) {
	repo := &workerIntelligenceRepo{eligible: false, completed: make(chan IntelligenceSnapshot, 1)}
	worker := newWorkerTest(t, repo)
	worker.collect = func(context.Context, IntelligenceClaim, centersettings.IPQualitySettings) IntelligenceSnapshot {
		t.Fatal("ineligible job performed network work")
		return IntelligenceSnapshot{}
	}
	worker.runClaim(context.Background(), workerTestClaim())
	if result := <-repo.completed; result.State != "skipped" {
		t.Fatal("invalid lifecycle not skipped")
	}
	worker.settings = workerIntelligenceSettings{err: errors.New("private-secret")}
	worker.runClaim(context.Background(), workerTestClaim())
	select {
	case <-repo.completed:
		t.Fatal("settings failure finalized without usable config")
	default:
	}
}
