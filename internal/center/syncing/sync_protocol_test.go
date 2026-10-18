package syncing

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strconv"
	"testing"
	"time"

	"houfeng/internal/center/agentplan"
	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/observations"
	"houfeng/internal/contracts/agentapi"
)

const testProducerID = "b88d099e-39d1-4d01-8cec-034698e108e7"

func validSyncBatch(instanceID string, reports ...ipquality.ReportWrite) Batch {
	identity := testProducerID + ":1"
	for i := range reports {
		reports[i].SyncBatchID = identity
	}
	return Batch{
		ProtocolRevision:     agentapi.SyncProtocolRevision,
		SessionID:            "session_test",
		ProducerID:           testProducerID,
		BatchSequence:        1,
		AllocatedThrough:     1,
		MonitoringInstanceID: instanceID,
		SyncToken:            "sync_test",
		Heartbeats: []HeartbeatPayload{{
			ObservedAt:  time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC),
			Fingerprint: "fp",
			SyncBatchID: identity,
		}},
		IPQualityReports: reports,
	}
}

func TestServiceRejectsInvalidProtocolBeforeRepositoryAndSideEffects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		mutate func(*Batch)
		want   error
	}{
		{"missing revision", func(b *Batch) { b.ProtocolRevision = 0 }, ErrSyncProtocolRequired},
		{"old revision", func(b *Batch) { b.ProtocolRevision = 1 }, ErrSyncProtocolRequired},
		{"future revision", func(b *Batch) { b.ProtocolRevision++ }, ErrSyncProtocolRequired},
		{"missing producer", func(b *Batch) { b.ProducerID = "" }, ErrInvalidReplayState},
		{"opaque producer", func(b *Batch) { b.ProducerID = "producer_test" }, ErrInvalidReplayState},
		{"compact UUID", func(b *Batch) { b.ProducerID = "b88d099e39d14d018cec034698e108e7" }, ErrInvalidReplayState},
		{"malformed UUID", func(b *Batch) { b.ProducerID = "g88d099e-39d1-4d01-8cec-034698e108e7" }, ErrInvalidReplayState},
		{"missing session", func(b *Batch) { b.SessionID = "" }, ErrInvalidReplayState},
		{"negative allocation", func(b *Batch) { b.AllocatedThrough = -1 }, ErrInvalidReplayState},
		{"negative floor", func(b *Batch) { b.RetiredThrough = -1 }, ErrInvalidReplayState},
		{"floor exceeds allocation", func(b *Batch) { b.RetiredThrough = 2 }, ErrInvalidReplayState},
		{"negative sequence", func(b *Batch) { b.BatchSequence = -1 }, ErrInvalidReplayState},
		{"sequence exceeds allocation", func(b *Batch) { b.BatchSequence = 2 }, ErrInvalidReplayState},
		{"wrapped batch sequence", func(b *Batch) { b.BatchSequence = math.MinInt64 }, ErrInvalidReplayState},
		{"wrapped allocation", func(b *Batch) { b.AllocatedThrough = math.MinInt64 }, ErrInvalidReplayState},
		{"wrapped retirement", func(b *Batch) { b.RetiredThrough = math.MinInt64 }, ErrInvalidReplayState},
		{"retired sequence", func(b *Batch) { b.RetiredThrough = 1 }, ErrSyncBatchRetired},
		{"sequence below retirement", func(b *Batch) { b.AllocatedThrough = 3; b.RetiredThrough = 2 }, ErrSyncBatchRetired},
		{"missing heartbeat", func(b *Batch) { b.Heartbeats = nil }, ErrHeartbeatRequired},
		{"heartbeat identity", func(b *Batch) { b.Heartbeats[0].SyncBatchID = "other:1" }, ErrInvalidReplayState},
		{"mixed heartbeat identities", func(b *Batch) {
			b.Heartbeats = append(b.Heartbeats, HeartbeatPayload{SyncBatchID: testProducerID + ":2"})
		}, ErrInvalidReplayState},
		{"host sample identity", func(b *Batch) {
			b.Observations.HostSamples = []observations.HostSampleWrite{{SyncBatchID: testProducerID + ":2"}}
		}, ErrInvalidReplayState},
		{"probe identity", func(b *Batch) {
			b.Observations.ProbeObservations = []observations.ProbeObservationWrite{{SyncBatchID: testProducerID + ":2"}}
		}, ErrInvalidReplayState},
		{"IP report identity", func(b *Batch) {
			b.IPQualityReports = []ipquality.ReportWrite{{SyncBatchID: testProducerID + ":2"}}
		}, ErrInvalidReplayState},
		{"zero live sequence", func(b *Batch) {
			b.LiveSignal = &agentapi.LiveSignal{Fingerprint: "fp"}
		}, ErrInvalidReplayState},
		{"negative live sequence", func(b *Batch) {
			b.LiveSignal = &agentapi.LiveSignal{Sequence: -1, Fingerprint: "fp"}
		}, ErrInvalidReplayState},
		{"wrapped live sequence", func(b *Batch) {
			b.LiveSignal = &agentapi.LiveSignal{Sequence: math.MinInt64, Fingerprint: "fp"}
		}, ErrInvalidReplayState},
		{"missing live fingerprint", func(b *Batch) {
			b.LiveSignal = &agentapi.LiveSignal{Sequence: 1}
		}, ErrInvalidReplayState},
		{"zero sequence heartbeat", func(b *Batch) { b.BatchSequence = 0 }, ErrInvalidReplayState},
		{"zero sequence host sample", func(b *Batch) {
			b.BatchSequence = 0
			b.Heartbeats = nil
			b.Observations.HostSamples = []observations.HostSampleWrite{{SyncBatchID: testProducerID + ":1"}}
		}, ErrInvalidReplayState},
		{"zero sequence probe", func(b *Batch) {
			b.BatchSequence = 0
			b.Heartbeats = nil
			b.Observations.ProbeObservations = []observations.ProbeObservationWrite{{SyncBatchID: testProducerID + ":1"}}
		}, ErrInvalidReplayState},
		{"zero sequence IP report", func(b *Batch) {
			b.BatchSequence = 0
			b.Heartbeats = nil
			b.IPQualityReports = []ipquality.ReportWrite{{SyncBatchID: testProducerID + ":1"}}
		}, ErrInvalidReplayState},
		{"zero sequence command result", func(b *Batch) {
			b.BatchSequence = 0
			b.Heartbeats = nil
			b.CommandResults = []CommandResult{{ActionID: "action", CommandID: "command"}}
		}, ErrInvalidReplayState},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			batch := validSyncBatch("mi_001")
			tc.mutate(&batch)
			repo := &fakeSyncRepository{result: Result{Disposition: ResultDispositionRecorded}}
			postSync := &fakePostSyncProcessor{}
			coordinator := &fakeCollectCoordinator{requestID: "pending"}
			service := NewService(repo, postSync).WithIPQualityCollectCoordinator(coordinator)

			got, err := service.SyncBatch(context.Background(), batch)
			if !errors.Is(err, tc.want) {
				t.Fatalf("SyncBatch() error = %v, want %v", err, tc.want)
			}
			if !reflect.DeepEqual(got, Result{}) {
				t.Fatalf("invalid batch returned result %#v", got)
			}
			if repo.calls != 0 || postSync.calls != 0 || coordinator.pendingCalls != 0 || len(coordinator.observed) != 0 {
				t.Fatalf("invalid batch reached side effects: repository=%d postSync=%d pending=%d observed=%d", repo.calls, postSync.calls, coordinator.pendingCalls, len(coordinator.observed))
			}
		})
	}
}

func TestServiceAdmitsCanonicalBatchAndReplayControlBoundaries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		mutate func(*Batch)
	}{
		{"canonical facts", func(b *Batch) {
			identity := b.Heartbeats[0].SyncBatchID
			b.Observations = observations.BatchWrite{
				HostSamples:       []observations.HostSampleWrite{{SyncBatchID: identity}},
				ProbeObservations: []observations.ProbeObservationWrite{{SyncBatchID: identity}},
			}
			b.IPQualityReports = []ipquality.ReportWrite{{SyncBatchID: identity}}
			b.CommandResults = []CommandResult{{ActionID: "action", CommandID: "command"}}
		}},
		{"maximum signed sequences", func(b *Batch) {
			b.BatchSequence = math.MaxInt64
			b.AllocatedThrough = math.MaxInt64
			b.RetiredThrough = math.MaxInt64 - 1
			b.Heartbeats[0].SyncBatchID = b.ProducerID + ":" + strconv.FormatInt(b.BatchSequence, 10)
			b.LiveSignal = &agentapi.LiveSignal{Sequence: math.MaxInt64, Fingerprint: "fp"}
		}},
		{"beyond replay window reaches repository", func(b *Batch) {
			b.BatchSequence = agentapi.SyncReplayWindow + 1
			b.AllocatedThrough = b.BatchSequence
			b.Heartbeats[0].SyncBatchID = b.ProducerID + ":" + strconv.FormatInt(b.BatchSequence, 10)
		}},
		{"initial control", func(b *Batch) {
			b.BatchSequence = 0
			b.AllocatedThrough = 0
			b.Heartbeats = nil
		}},
		{"retire all with live control", func(b *Batch) {
			b.BatchSequence = 0
			b.RetiredThrough = b.AllocatedThrough
			b.Heartbeats = nil
			b.LiveSignal = &agentapi.LiveSignal{Sequence: 1, Fingerprint: "fp"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			batch := validSyncBatch("mi_001")
			tc.mutate(&batch)
			repo := &fakeSyncRepository{result: Result{Disposition: ResultDispositionRecorded}}
			_, err := NewService(repo).SyncBatch(context.Background(), batch)
			if err != nil || repo.calls != 1 {
				t.Fatalf("SyncBatch() error = %v, repository calls = %d; want admitted batch", err, repo.calls)
			}
		})
	}
}

func TestServiceDeferredSkipsPostSyncAndDoesNotDispatchPlan(t *testing.T) {
	t.Parallel()

	registry := ipquality.NewCollectRequests(func() (string, error) { return "collect_pending", nil })
	now := time.Now().UTC()
	if _, err := registry.Request("mi_001", now); err != nil {
		t.Fatal(err)
	}
	repo := &fakeSyncRepository{result: Result{
		Disposition:           ResultDispositionDeferred,
		LiveCollectionAllowed: true,
		Plan: agentplan.SyncPlan{
			PendingAction: &agentplan.PendingAction{ActionID: "must_not_dispatch", CommandID: "command"},
			IPQualityPlan: &agentplan.IPQualityPlan{Enabled: true},
		},
	}}
	postSync := &fakePostSyncProcessor{}
	service := NewService(repo, postSync).WithIPQualityCollectCoordinator(registry)
	batch := validSyncBatch("mi_001", ipquality.ReportWrite{
		Status: "success", IPVersion: 4,
		CollectionMetadata: agentapi.ParseCollectionMetadata([]byte(`{"collect_request_id":"collect_pending"}`)),
	})
	batch.LiveSignal = &agentapi.LiveSignal{Sequence: 1, Fingerprint: "fp"}
	batch.IPQualityCollectionRevision = agentapi.IPQualitySelectiveCollectionRevision

	got, err := service.SyncBatch(context.Background(), batch)
	if err != nil {
		t.Fatal(err)
	}
	if got.Disposition != ResultDispositionDeferred || got.LiveCollectionAllowed || !reflect.DeepEqual(got.Plan, agentplan.SyncPlan{}) {
		t.Fatalf("deferred response must not authorize collection or dispatch a plan: %#v", got)
	}
	if postSync.calls != 0 {
		t.Fatalf("postSync calls = %d, want 0", postSync.calls)
	}
	current, ok := registry.Latest("mi_001", now)
	if !ok || current.Status != ipquality.CollectRequestPending {
		t.Fatalf("deferred reports/plan changed pending collection: %#v, found=%v", current, ok)
	}
}
