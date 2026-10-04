package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresIntegrationIPQualitySyncMetadataRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	const prefix = "ipq_sync_roundtrip"
	seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-sync-roundtrip", 4)
	repository := NewPostgresSyncRepository(runtimePool)
	repository.receptionFault = nil

	requestID := "ipqc_" + prefix
	requests := ipquality.NewCollectRequests(func() (string, error) { return requestID, nil })
	requestAt := time.Now().UTC()
	if _, err := requests.Request("mi_"+prefix, requestAt); err != nil {
		t.Fatalf("register immediate IP quality request: %v", err)
	}
	service := syncing.NewService(repository).WithIPQualityCollectCoordinator(requests)

	liveBatch, backfillBatch := syncInterleavingBatches(prefix)
	backfillBatch.IPQualityReports = nil
	dispatchAt := syncInterleavingT2.Add(-time.Minute)
	repository.now = func() time.Time { return dispatchAt }
	dispatchResult, err := service.SyncBatch(ctx, backfillBatch)
	if err != nil {
		t.Fatalf("dispatch sync batch: %v", err)
	}
	if dispatchResult.Disposition != syncing.ResultDispositionRecorded {
		t.Fatalf("dispatch disposition = %q, want recorded", dispatchResult.Disposition)
	}
	if dispatchResult.Plan.IPQualityPlan == nil || dispatchResult.Plan.IPQualityPlan.CollectRequestID != requestID {
		t.Fatalf("dispatch plan = %#v, want pending collect request %q", dispatchResult.Plan.IPQualityPlan, requestID)
	}
	if latest, ok := requests.Latest("mi_"+prefix, time.Now().UTC()); !ok || latest.Status != ipquality.CollectRequestDispatched {
		t.Fatalf("request after dispatch = %#v, ok=%t, want dispatched", latest, ok)
	}

	explicitReceivedAt := syncInterleavingT2.Add(45 * time.Second)
	fallbackReceivedAt := syncInterleavingT2.Add(90 * time.Second)
	fullReport := ipQualitySyncFullReport(prefix, "sync_live_"+prefix, syncInterleavingT2, explicitReceivedAt, requestID)
	fallbackReport := ipQualitySyncFallbackReport(prefix, "sync_live_"+prefix, syncInterleavingT1)
	liveBatch.IPQualityReports = []ipquality.ReportWrite{fullReport, fallbackReport}
	repository.now = func() time.Time { return fallbackReceivedAt }
	liveResult, err := service.SyncBatch(ctx, liveBatch)
	if err != nil {
		t.Fatalf("metadata sync batch: %v", err)
	}
	if liveResult.Disposition != syncing.ResultDispositionRecorded {
		t.Fatalf("metadata disposition = %q, want recorded", liveResult.Disposition)
	}
	if latest, ok := requests.Latest("mi_"+prefix, time.Now().UTC()); !ok || latest.Status != ipquality.CollectRequestCompleted || latest.ReportStatus != agentapi.IPQualityStatusPartial {
		t.Fatalf("request after metadata report = %#v, ok=%t, want completed partial", latest, ok)
	}
	if liveResult.Plan.IPQualityPlan == nil || liveResult.Plan.IPQualityPlan.CollectRequestID != "" {
		t.Fatalf("completed request was redispatched in plan: %#v", liveResult.Plan.IPQualityPlan)
	}

	readRepository := NewPostgresIPQualityRepository(runtimePool)
	vpsReport, err := readRepository.GetVPSIPQuality(ctx, "vps_"+prefix)
	if err != nil {
		t.Fatalf("GetVPSIPQuality() after sync: %v", err)
	}
	assertIPQualitySyncRoundTrip(t, vpsReport, explicitReceivedAt, requestID)

	storedSyncReports := readIPQualitySyncStoredReports(t, ctx, fixture, "mi_"+prefix, "sync_live_"+prefix)
	if len(storedSyncReports) != 2 {
		t.Fatalf("stored sync reports = %#v, want two reports", storedSyncReports)
	}
	storedByAgent := make(map[string]ipQualitySyncStoredReport, len(storedSyncReports))
	for _, report := range storedSyncReports {
		storedByAgent[report.AgentVersion] = report
	}
	fullStored, ok := storedByAgent["agent/ip-quality-v2"]
	if !ok {
		t.Fatalf("stored sync reports = %#v, missing full metadata report", storedSyncReports)
	}
	if fullStored.RawJSONIsNull || fullStored.RawJSONIsJSONNull || !fullStored.ReceivedAt.Equal(explicitReceivedAt) {
		t.Fatalf("full stored report = %#v, want explicit received_at and non-null raw JSON", fullStored)
	}
	fallbackStored, ok := storedByAgent["agent/ip-quality-zero-received"]
	if !ok {
		t.Fatalf("stored sync reports = %#v, missing zero-received report", storedSyncReports)
	}
	if fallbackStored.RawJSONIsNull || !fallbackStored.RawJSONIsJSONNull || !fallbackStored.ReceivedAt.Equal(fallbackReceivedAt) {
		t.Fatalf("fallback stored report = %#v, want batch received_at and JSON null raw value", fallbackStored)
	}

	saveBatchID := "save_reports_" + prefix
	saveExplicitReceivedAt := syncInterleavingT2.Add(3 * time.Minute)
	saveZeroObservedAt := syncInterleavingT1.Add(-2 * time.Minute)
	saveExplicitObservedAt := syncInterleavingT1.Add(-time.Minute)
	saveBefore := time.Now().UTC()
	if err := readRepository.SaveReports(ctx, []ipquality.ReportWrite{
		ipQualitySyncStandaloneReport(prefix, saveBatchID, "agent/save-explicit", saveExplicitObservedAt, saveExplicitReceivedAt),
		ipQualitySyncStandaloneReport(prefix, saveBatchID, "agent/save-zero", saveZeroObservedAt, time.Time{}),
	}); err != nil {
		t.Fatalf("SaveReports() metadata/null comparison rows: %v", err)
	}
	saveAfter := time.Now().UTC()
	storedSaveReports := readIPQualitySyncStoredReports(t, ctx, fixture, "mi_"+prefix, saveBatchID)
	if len(storedSaveReports) != 2 {
		t.Fatalf("stored SaveReports rows = %#v, want two rows", storedSaveReports)
	}
	storedSaveByAgent := make(map[string]ipQualitySyncStoredReport, len(storedSaveReports))
	for _, report := range storedSaveReports {
		storedSaveByAgent[report.AgentVersion] = report
	}
	saveExplicit, ok := storedSaveByAgent["agent/save-explicit"]
	if !ok || !saveExplicit.RawJSONIsNull || !saveExplicit.ReceivedAt.Equal(saveExplicitReceivedAt) {
		t.Fatalf("SaveReports explicit row = %#v, want SQL NULL raw and explicit received_at", saveExplicit)
	}
	saveZero, ok := storedSaveByAgent["agent/save-zero"]
	if !ok || !saveZero.RawJSONIsNull || saveZero.ReceivedAt.Before(saveBefore) || saveZero.ReceivedAt.After(saveAfter) {
		t.Fatalf("SaveReports zero row = %#v, want SQL NULL raw and current received_at between %s and %s", saveZero, saveBefore, saveAfter)
	}
}

func TestPostgresIntegrationIPQualitySyncRollbackAndReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-sync-rollback", 4)
	repository := NewPostgresSyncRepository(runtimePool)
	repository.receptionFault = nil

	t.Run("duplicate service rolls back and replay is exact", func(t *testing.T) {
		const prefix = "ipq_sync_rollback"
		seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
		requestID := "ipqc_" + prefix
		requests := ipquality.NewCollectRequests(func() (string, error) { return requestID, nil })
		requestAt := time.Now().UTC()
		if _, err := requests.Request("mi_"+prefix, requestAt); err != nil {
			t.Fatalf("register immediate IP quality request: %v", err)
		}
		service := syncing.NewService(repository).WithIPQualityCollectCoordinator(requests)

		liveBatch, _ := syncInterleavingBatches(prefix)
		badReport := ipQualitySyncFullReport(prefix, "sync_live_"+prefix, syncInterleavingT2, time.Time{}, requestID)
		badReport.ServiceUnlocks = append(badReport.ServiceUnlocks, badReport.ServiceUnlocks[0])
		liveBatch.IPQualityReports = []ipquality.ReportWrite{badReport}
		receivedAt := syncInterleavingT2.Add(2 * time.Minute)
		repository.now = func() time.Time { return receivedAt }
		before := readIPQualitySyncFactCounts(t, ctx, fixture, "mi_"+prefix)
		if before != (ipQualitySyncFactCounts{}) {
			t.Fatalf("initial rollback fixture facts = %#v, want empty", before)
		}
		if _, err := service.SyncBatch(ctx, liveBatch); err == nil {
			t.Fatal("duplicate service source ApplyBatch error = nil, want unique-key failure")
		} else {
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
				t.Fatalf("duplicate service source error = %v, want PostgreSQL unique violation", err)
			}
		}
		afterFailure := readIPQualitySyncFactCounts(t, ctx, fixture, "mi_"+prefix)
		if afterFailure != before {
			t.Fatalf("facts after duplicate service rollback = %#v, want %#v", afterFailure, before)
		}
		if latest, ok := requests.Latest("mi_"+prefix, time.Now().UTC()); !ok || latest.Status != ipquality.CollectRequestPending || latest.CompletedAt != nil {
			t.Fatalf("request after failed sync = %#v, ok=%t, want pending and incomplete", latest, ok)
		}

		goodBatch := liveBatch
		goodReport := badReport
		goodReport.ServiceUnlocks = append([]ipquality.ServiceUnlockWrite(nil), badReport.ServiceUnlocks[:len(badReport.ServiceUnlocks)-1]...)
		goodBatch.IPQualityReports = []ipquality.ReportWrite{goodReport}
		goodResult, err := service.SyncBatch(ctx, goodBatch)
		if err != nil {
			t.Fatalf("corrected sync batch: %v", err)
		}
		if goodResult.Disposition != syncing.ResultDispositionRecorded {
			t.Fatalf("corrected disposition = %q, want recorded", goodResult.Disposition)
		}
		completed, ok := requests.Latest("mi_"+prefix, time.Now().UTC())
		if !ok || completed.Status != ipquality.CollectRequestCompleted || completed.CompletedAt == nil {
			t.Fatalf("request after corrected sync = %#v, ok=%t, want completed", completed, ok)
		}
		countsAfterRecord := readIPQualitySyncFactCounts(t, ctx, fixture, "mi_"+prefix)
		if countsAfterRecord.AgentSyncBatches != 1 || countsAfterRecord.Heartbeats != 1 || countsAfterRecord.HostSamples != 1 || countsAfterRecord.ProbeObservations != 3 || countsAfterRecord.Reports != 1 || countsAfterRecord.ProviderResults != 3 || countsAfterRecord.ServiceUnlocks != 2 {
			t.Fatalf("facts after corrected sync = %#v, want one recorded batch with full IP quality matrix", countsAfterRecord)
		}

		replayAt := receivedAt.Add(time.Minute)
		repository.now = func() time.Time { return replayAt }
		replayResult, err := service.SyncBatch(ctx, goodBatch)
		if err != nil {
			t.Fatalf("exact replay: %v", err)
		}
		if replayResult.Disposition != syncing.ResultDispositionExactDuplicate {
			t.Fatalf("replay disposition = %q, want exact_duplicate", replayResult.Disposition)
		}
		countsAfterReplay := readIPQualitySyncFactCounts(t, ctx, fixture, "mi_"+prefix)
		if countsAfterReplay != countsAfterRecord {
			t.Fatalf("facts after exact replay = %#v, want unchanged %#v", countsAfterReplay, countsAfterRecord)
		}
		completedAfterReplay, ok := requests.Latest("mi_"+prefix, time.Now().UTC())
		if !ok || completedAfterReplay.Status != ipquality.CollectRequestCompleted || completedAfterReplay.CompletedAt == nil || !completedAfterReplay.CompletedAt.Equal(*completed.CompletedAt) {
			t.Fatalf("request after exact replay = %#v, ok=%t, want unchanged completion", completedAfterReplay, ok)
		}
	})

	t.Run("invalid token and fingerprint write nothing", func(t *testing.T) {
		const prefix = "ipq_sync_auth_reject"
		seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
		liveBatch, _ := syncInterleavingBatches(prefix)
		liveBatch.IPQualityReports = nil
		repository.now = func() time.Time { return syncInterleavingT2.Add(4 * time.Minute) }
		if _, err := repository.ApplyBatch(ctx, func() syncing.Batch {
			batch := liveBatch
			batch.SyncToken = "wrong-token"
			return batch
		}()); !errors.Is(err, syncing.ErrInvalidSyncToken) {
			t.Fatalf("invalid token error = %v, want ErrInvalidSyncToken", err)
		}
		if counts := readIPQualitySyncFactCounts(t, ctx, fixture, "mi_"+prefix); counts != (ipQualitySyncFactCounts{}) {
			t.Fatalf("facts after invalid token = %#v, want empty", counts)
		}

		fingerprintBatch := liveBatch
		fingerprintBatch.LiveSignal = nil
		fingerprintReport := ipQualitySyncFullReport(prefix, "sync_live_"+prefix, syncInterleavingT2, time.Time{}, "")
		fingerprintReport.Fingerprint = "wrong-fingerprint"
		fingerprintBatch.IPQualityReports = []ipquality.ReportWrite{fingerprintReport}
		if _, err := repository.ApplyBatch(ctx, fingerprintBatch); !errors.Is(err, syncing.ErrBindingNotAccepted) {
			t.Fatalf("invalid fingerprint error = %v, want ErrBindingNotAccepted", err)
		}
		if counts := readIPQualitySyncFactCounts(t, ctx, fixture, "mi_"+prefix); counts != (ipQualitySyncFactCounts{}) {
			t.Fatalf("facts after invalid fingerprint = %#v, want empty", counts)
		}
	})

	t.Run("paused monitoring suppresses reports and leaves request pending", func(t *testing.T) {
		const prefix = "ipq_sync_paused"
		seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
		if _, err := fixture.db.Exec(ctx, `update public.monitoring_instances set monitoring_status = $1 where monitoring_instance_id = $2`, monitoringinstances.MonitoringPaused, "mi_"+prefix); err != nil {
			t.Fatalf("pause monitoring instance: %v", err)
		}
		requestID := "ipqc_" + prefix
		requests := ipquality.NewCollectRequests(func() (string, error) { return requestID, nil })
		requestAt := time.Now().UTC()
		if _, err := requests.Request("mi_"+prefix, requestAt); err != nil {
			t.Fatalf("register paused immediate IP quality request: %v", err)
		}
		service := syncing.NewService(repository).WithIPQualityCollectCoordinator(requests)
		batch, _ := syncInterleavingBatches(prefix)
		batch.IPQualityReports = []ipquality.ReportWrite{ipQualitySyncFullReport(prefix, "sync_live_"+prefix, syncInterleavingT2, time.Time{}, requestID)}
		repository.now = func() time.Time { return syncInterleavingT2 }
		result, err := service.SyncBatch(ctx, batch)
		if err != nil {
			t.Fatalf("paused sync batch: %v", err)
		}
		if result.Disposition != syncing.ResultDispositionSuppressed || !result.StopCollection {
			t.Fatalf("paused result = %#v, want suppressed stop-collection result", result)
		}
		if latest, ok := requests.Latest("mi_"+prefix, time.Now().UTC()); !ok || latest.Status != ipquality.CollectRequestPending || latest.CompletedAt != nil {
			t.Fatalf("paused request = %#v, ok=%t, want pending and incomplete", latest, ok)
		}
		if counts := readIPQualitySyncFactCounts(t, ctx, fixture, "mi_"+prefix); counts != (ipQualitySyncFactCounts{}) {
			t.Fatalf("facts after paused sync = %#v, want empty", counts)
		}
	})
}
func TestPostgresIntegrationIPQualityReadSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	t.Run("A to B", func(t *testing.T) {
		const prefix = "ipq_snapshot_ab"
		seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
		observedAt := time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC)
		writerPool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-snapshot-ab-writer", 1)
		aID, err := saveIPQualitySnapshotReport(t, ctx, writerPool, ipQualitySnapshotReport(
			"mi_"+prefix, "fp_"+prefix, syncInterleavingIPAddress(prefix),
			"snapshot_a_"+prefix, "snapshot-A", observedAt, observedAt.Add(time.Minute), false,
		))
		if err != nil {
			t.Fatalf("save snapshot A: %v", err)
		}
		before := runIPQualitySnapshotReadBarrier(t, ctx, fixture, "ab", "vps_"+prefix,
			func(repo *PostgresIPQualityRepository) (ipquality.VPSReport, error) {
				return repo.GetVPSIPQuality(ctx, "vps_"+prefix)
			},
			func(ctx context.Context, repo *PostgresIPQualityRepository, pool *pgxpool.Pool) error {
				_, err := saveIPQualitySnapshotReport(t, ctx, pool, ipQualitySnapshotReport(
					"mi_"+prefix, "fp_"+prefix, syncInterleavingIPAddress(prefix),
					"snapshot_b_"+prefix, "snapshot-B", observedAt.Add(time.Minute), observedAt.Add(2*time.Minute), false,
				))
				return err
			},
		)
		assertIPQualitySnapshotComplete(t, before, aID, "snapshot-A", "link", false)
		later, err := NewPostgresIPQualityRepository(writerPool).GetVPSIPQuality(ctx, "vps_"+prefix)
		if err != nil {
			t.Fatalf("read snapshot B: %v", err)
		}
		assertIPQualitySnapshotComplete(t, later, "", "snapshot-B", "link", false)
	})

	t.Run("empty to first B", func(t *testing.T) {
		const prefix = "ipq_snapshot_empty"
		seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
		observedAt := time.Date(2026, time.September, 1, 11, 0, 0, 0, time.UTC)
		writerPool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-snapshot-empty-writer", 1)
		before := runIPQualitySnapshotReadBarrier(t, ctx, fixture, "empty", "vps_"+prefix,
			func(repo *PostgresIPQualityRepository) (ipquality.VPSReport, error) {
				return repo.GetVPSIPQuality(ctx, "vps_"+prefix)
			},
			func(ctx context.Context, repo *PostgresIPQualityRepository, pool *pgxpool.Pool) error {
				_, err := saveIPQualitySnapshotReport(t, ctx, pool, ipQualitySnapshotReport(
					"mi_"+prefix, "fp_"+prefix, syncInterleavingIPAddress(prefix),
					"snapshot_b_"+prefix, "snapshot-B", observedAt, observedAt.Add(time.Minute), false,
				))
				return err
			},
		)
		assertIPQualitySnapshotEmpty(t, before)
		later, err := NewPostgresIPQualityRepository(writerPool).GetVPSIPQuality(ctx, "vps_"+prefix)
		if err != nil {
			t.Fatalf("read first snapshot B: %v", err)
		}
		assertIPQualitySnapshotComplete(t, later, "", "snapshot-B", "link", false)
	})

	t.Run("same timestamp live wins over backfill", func(t *testing.T) {
		const prefix = "ipq_snapshot_order"
		seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
		observedAt := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
		writerPool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-snapshot-order-writer", 1)
		aID, err := saveIPQualitySnapshotReport(t, ctx, writerPool, ipQualitySnapshotReport(
			"mi_"+prefix, "fp_"+prefix, syncInterleavingIPAddress(prefix),
			"snapshot_backfill_"+prefix, "snapshot-backfill", observedAt, observedAt.Add(10*time.Minute), true,
		))
		if err != nil {
			t.Fatalf("save backfill snapshot: %v", err)
		}
		before := runIPQualitySnapshotReadBarrier(t, ctx, fixture, "order", "vps_"+prefix,
			func(repo *PostgresIPQualityRepository) (ipquality.VPSReport, error) {
				return repo.GetVPSIPQuality(ctx, "vps_"+prefix)
			},
			func(ctx context.Context, repo *PostgresIPQualityRepository, pool *pgxpool.Pool) error {
				_, err := saveIPQualitySnapshotReport(t, ctx, pool, ipQualitySnapshotReport(
					"mi_"+prefix, "fp_"+prefix, syncInterleavingIPAddress(prefix),
					"snapshot_live_"+prefix, "snapshot-live", observedAt, observedAt.Add(time.Minute), false,
				))
				return err
			},
		)
		assertIPQualitySnapshotComplete(t, before, aID, "snapshot-backfill", "link", false)
		later, err := NewPostgresIPQualityRepository(writerPool).GetVPSIPQuality(ctx, "vps_"+prefix)
		if err != nil {
			t.Fatalf("read live snapshot: %v", err)
		}
		assertIPQualitySnapshotComplete(t, later, "", "snapshot-live", "link", false)
	})

	t.Run("link removal and replacement", func(t *testing.T) {
		const prefix = "ipq_snapshot_link"
		seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
		observedAt := time.Date(2026, time.September, 1, 13, 0, 0, 0, time.UTC)
		writerPool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-snapshot-link-writer", 1)
		aID, err := saveIPQualitySnapshotReport(t, ctx, writerPool, ipQualitySnapshotReport(
			"mi_"+prefix, "fp_"+prefix, syncInterleavingIPAddress(prefix),
			"snapshot_link_a_"+prefix, "snapshot-link-A", observedAt, observedAt.Add(time.Minute), false,
		))
		if err != nil {
			t.Fatalf("save linked snapshot A: %v", err)
		}
		before := runIPQualitySnapshotReadBarrier(t, ctx, fixture, "link", "vps_"+prefix,
			func(repo *PostgresIPQualityRepository) (ipquality.VPSReport, error) {
				return repo.GetVPSIPQuality(ctx, "vps_"+prefix)
			},
			func(ctx context.Context, repo *PostgresIPQualityRepository, pool *pgxpool.Pool) error {
				monitoringRepository := NewPostgresMonitoringInstanceRepository(pool)
				if _, err := monitoringRepository.RetireMonitoringInstance(ctx, "mi_"+prefix, monitoringinstances.LifecycleActionInput{
					Reason:         "replace snapshot predecessor",
					IdempotencyKey: "retire-" + prefix,
				}); err != nil {
					return err
				}
				current, _, _, err := monitoringRepository.CreateLinkedMonitoringInstanceIdempotent(
					ctx,
					"vps_"+prefix,
					monitoringinstances.LinkedCreateWireIdentity{
						DisplayName: prefix + " replacement",
						Provider:    "Fixture",
						Region:      "Region",
						City:        "City",
					},
					"create-"+prefix+"-replacement",
				)
				if err != nil {
					return err
				}
				_, err = saveIPQualitySnapshotReport(t, ctx, pool, ipQualitySnapshotReport(
					current.MonitoringInstanceID, "fp_"+prefix+"_new", syncInterleavingIPAddress(prefix),
					"snapshot_link_b_"+prefix, "snapshot-link-B", observedAt.Add(time.Minute), observedAt.Add(2*time.Minute), false,
				))
				return err
			},
		)
		assertIPQualitySnapshotComplete(t, before, aID, "snapshot-link-A", "link", false)
		later, err := NewPostgresIPQualityRepository(writerPool).GetVPSIPQuality(ctx, "vps_"+prefix)
		if err != nil {
			t.Fatalf("read replacement link snapshot: %v", err)
		}
		assertIPQualitySnapshotComplete(t, later, "", "snapshot-link-B", "link", false)
	})

	t.Run("address change", func(t *testing.T) {
		const (
			prefix = "ipq_snapshot_address"
			oldIP  = "198.51.100.10"
			newIP  = "198.51.100.11"
		)
		seedIPQualitySnapshotIPMatchFixture(t, ctx, fixture, prefix, "vps_"+prefix, "mi_"+prefix, oldIP)
		observedAt := time.Date(2026, time.September, 1, 14, 0, 0, 0, time.UTC)
		writerPool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-snapshot-address-writer", 1)
		aID, err := saveIPQualitySnapshotReport(t, ctx, writerPool, ipQualitySnapshotReport(
			"mi_"+prefix, "fp_"+prefix, oldIP,
			"snapshot_address_a_"+prefix, "snapshot-address-A", observedAt, observedAt.Add(time.Minute), false,
		))
		if err != nil {
			t.Fatalf("save address snapshot A: %v", err)
		}
		before := runIPQualitySnapshotReadBarrier(t, ctx, fixture, "address", "vps_"+prefix,
			func(repo *PostgresIPQualityRepository) (ipquality.VPSReport, error) {
				return repo.GetVPSIPQuality(ctx, "vps_"+prefix)
			},
			func(ctx context.Context, repo *PostgresIPQualityRepository, pool *pgxpool.Pool) error {
				if _, err := pool.Exec(ctx, `update public.vps_assets set ipv4 = $1 where vps_id = $2`, newIP, "vps_"+prefix); err != nil {
					return err
				}
				_, err := saveIPQualitySnapshotReport(t, ctx, pool, ipQualitySnapshotReport(
					"mi_"+prefix, "fp_"+prefix, newIP,
					"snapshot_address_b_"+prefix, "snapshot-address-B", observedAt.Add(time.Minute), observedAt.Add(2*time.Minute), false,
				))
				return err
			},
		)
		assertIPQualitySnapshotComplete(t, before, aID, "snapshot-address-A", "ip_match", false)
		later, err := NewPostgresIPQualityRepository(writerPool).GetVPSIPQuality(ctx, "vps_"+prefix)
		if err != nil {
			t.Fatalf("read address snapshot B: %v", err)
		}
		assertIPQualitySnapshotComplete(t, later, "", "snapshot-address-B", "ip_match", false)
	})

	t.Run("shared address becomes ambiguous", func(t *testing.T) {
		const (
			prefix   = "ipq_snapshot_shared"
			sharedIP = "198.51.100.20"
		)
		seedIPQualitySnapshotIPMatchFixture(t, ctx, fixture, prefix, "vps_"+prefix, "mi_"+prefix, sharedIP)
		observedAt := time.Date(2026, time.September, 1, 15, 0, 0, 0, time.UTC)
		writerPool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-snapshot-shared-writer", 1)
		aID, err := saveIPQualitySnapshotReport(t, ctx, writerPool, ipQualitySnapshotReport(
			"mi_"+prefix, "fp_"+prefix, sharedIP,
			"snapshot_shared_a_"+prefix, "snapshot-shared-A", observedAt, observedAt.Add(time.Minute), false,
		))
		if err != nil {
			t.Fatalf("save shared snapshot A: %v", err)
		}
		before := runIPQualitySnapshotReadBarrier(t, ctx, fixture, "shared", "vps_"+prefix,
			func(repo *PostgresIPQualityRepository) (ipquality.VPSReport, error) {
				return repo.GetVPSIPQuality(ctx, "vps_"+prefix)
			},
			func(ctx context.Context, repo *PostgresIPQualityRepository, pool *pgxpool.Pool) error {
				_, err := pool.Exec(ctx, `
					insert into public.vps_assets (vps_id, display_name, ipv4, lifecycle_status)
					values ($1,$2,$3,'active')`, "vps_"+prefix+"_other", prefix+" other", sharedIP)
				return err
			},
		)
		assertIPQualitySnapshotComplete(t, before, aID, "snapshot-shared-A", "ip_match", false)
		later, err := NewPostgresIPQualityRepository(writerPool).GetVPSIPQuality(ctx, "vps_"+prefix)
		if err != nil {
			t.Fatalf("read shared ambiguous snapshot: %v", err)
		}
		assertIPQualitySnapshotComplete(t, later, "", "snapshot-shared-A", "ip_match", true)
	})

	t.Run("selected historical detail remains fixed", func(t *testing.T) {
		const prefix = "ipq_snapshot_history"
		seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
		observedAt := time.Date(2026, time.September, 1, 16, 0, 0, 0, time.UTC)
		writerPool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-snapshot-history-writer", 1)
		aID, err := saveIPQualitySnapshotReport(t, ctx, writerPool, ipQualitySnapshotReport(
			"mi_"+prefix, "fp_"+prefix, syncInterleavingIPAddress(prefix),
			"snapshot_history_a_"+prefix, "snapshot-history-A", observedAt, observedAt.Add(time.Minute), false,
		))
		if err != nil {
			t.Fatalf("save history snapshot A: %v", err)
		}
		before := runIPQualitySnapshotReadBarrier(t, ctx, fixture, "history", "vps_"+prefix,
			func(repo *PostgresIPQualityRepository) (ipquality.VPSReport, error) {
				return repo.GetVPSIPQualityReportDetail(ctx, "vps_"+prefix, aID)
			},
			func(ctx context.Context, repo *PostgresIPQualityRepository, pool *pgxpool.Pool) error {
				_, err := saveIPQualitySnapshotReport(t, ctx, pool, ipQualitySnapshotReport(
					"mi_"+prefix, "fp_"+prefix, syncInterleavingIPAddress(prefix),
					"snapshot_history_b_"+prefix, "snapshot-history-B", observedAt.Add(time.Minute), observedAt.Add(2*time.Minute), false,
				))
				return err
			},
		)
		assertIPQualitySnapshotComplete(t, before, aID, "snapshot-history-A", "link", false)
		if before.History == nil || len(before.History) != 0 {
			t.Fatalf("historical detail history = %#v, want non-nil empty slice", before.History)
		}
		later, err := NewPostgresIPQualityRepository(writerPool).GetVPSIPQualityReportDetail(ctx, "vps_"+prefix, aID)
		if err != nil {
			t.Fatalf("read selected historical A after B: %v", err)
		}
		assertIPQualitySnapshotComplete(t, later, aID, "snapshot-history-A", "link", false)
		if later.History == nil || len(later.History) != 0 {
			t.Fatalf("historical detail after B history = %#v, want non-nil empty slice", later.History)
		}
	})
}

type ipQualityReadSnapshotBarrier struct {
	pool    *pgxpool.Pool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *ipQualityReadSnapshotBarrier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := b.pool.Query(ctx, sql, args...)
	return b.query(ctx, rows, err)
}

func (b *ipQualityReadSnapshotBarrier) beginTx(ctx context.Context, options pgx.TxOptions) (ipQualityTx, error) {
	tx, err := b.pool.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &ipQualityReadSnapshotTx{tx: tx, barrier: b}, nil
}

func (b *ipQualityReadSnapshotBarrier) query(ctx context.Context, rows pgx.Rows, err error) (pgx.Rows, error) {
	if err != nil {
		return nil, err
	}
	if err := b.wait(ctx); err != nil {
		rows.Close()
		return nil, err
	}
	return rows, nil
}

func (b *ipQualityReadSnapshotBarrier) wait(ctx context.Context) error {
	b.once.Do(func() { close(b.entered) })
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type ipQualityReadSnapshotTx struct {
	tx      pgx.Tx
	barrier *ipQualityReadSnapshotBarrier
}

func (tx *ipQualityReadSnapshotTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := tx.tx.Query(ctx, sql, args...)
	return tx.barrier.query(ctx, rows, err)
}

func (tx *ipQualityReadSnapshotTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return tx.tx.Exec(ctx, sql, args...)
}

func (tx *ipQualityReadSnapshotTx) Commit(ctx context.Context) error {
	return tx.tx.Commit(ctx)
}

func (tx *ipQualityReadSnapshotTx) Rollback(ctx context.Context) error {
	return tx.tx.Rollback(ctx)
}

func runIPQualitySnapshotReadBarrier(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	name, vpsID string,
	read func(*PostgresIPQualityRepository) (ipquality.VPSReport, error),
	mutate func(context.Context, *PostgresIPQualityRepository, *pgxpool.Pool) error,
) ipquality.VPSReport {
	t.Helper()
	readerPool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-snapshot-"+name+"-reader", 1)
	writerPool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-snapshot-"+name+"-mutator", 1)
	barrier := &ipQualityReadSnapshotBarrier{
		pool:    readerPool,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	reader := &PostgresIPQualityRepository{db: barrier, beginTx: barrier.beginTx}
	writer := NewPostgresIPQualityRepository(writerPool)
	resultCh := make(chan struct {
		report ipquality.VPSReport
		err    error
	}, 1)
	go func() {
		report, err := read(reader)
		resultCh <- struct {
			report ipquality.VPSReport
			err    error
		}{report: report, err: err}
	}()
	select {
	case <-barrier.entered:
	case <-ctx.Done():
		close(barrier.release)
		t.Fatalf("wait for read snapshot barrier: %v", ctx.Err())
	}
	if err := mutate(ctx, writer, writerPool); err != nil {
		close(barrier.release)
		t.Fatalf("mutate after read snapshot barrier: %v", err)
	}
	close(barrier.release)
	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatalf("read through repeatable-read snapshot: %v", result.err)
		}
		return result.report
	case <-ctx.Done():
		t.Fatalf("read through repeatable-read snapshot: %v", ctx.Err())
		return ipquality.VPSReport{}
	}
}

func saveIPQualitySnapshotReport(t *testing.T, ctx context.Context, pool *pgxpool.Pool, report ipquality.ReportWrite) (string, error) {
	t.Helper()
	if err := NewPostgresIPQualityRepository(pool).SaveReports(ctx, []ipquality.ReportWrite{report}); err != nil {
		return "", err
	}
	var reportID string
	err := pool.QueryRow(ctx, `
		select report_id
		from public.ip_quality_reports
		where monitoring_instance_id = $1 and sync_batch_id = $2
		order by report_id desc
		limit 1`, report.MonitoringInstanceID, report.SyncBatchID).Scan(&reportID)
	return reportID, err
}

func ipQualitySnapshotReport(monitoringInstanceID, fingerprint, ipAddress, syncBatchID, marker string, observedAt, receivedAt time.Time, backfilled bool) ipquality.ReportWrite {
	report := ipQualitySyncFullReport("ipq_snapshot_fixture", syncBatchID, observedAt, receivedAt, "")
	report.MonitoringInstanceID = monitoringInstanceID
	report.Fingerprint = fingerprint
	report.IPAddress = ipAddress
	report.AgentVersion = marker
	report.IsBackfilled = backfilled
	report.RawJSON = json.RawMessage(fmt.Sprintf(`{"snapshot_marker":%q}`, marker))
	report.DiagnosticsJSON = json.RawMessage(fmt.Sprintf(`{"snapshot_marker":%q}`, marker))
	for index := range report.ProviderResults {
		report.ProviderResults[index].ExtraJSON = json.RawMessage(fmt.Sprintf(`{"snapshot_marker":%q}`, marker))
	}
	for index := range report.ServiceUnlocks {
		report.ServiceUnlocks[index].ExtraJSON = json.RawMessage(fmt.Sprintf(`{"snapshot_marker":%q}`, marker))
	}
	return report
}

func assertIPQualitySnapshotEmpty(t *testing.T, report ipquality.VPSReport) {
	t.Helper()
	if report.Summary != nil || report.LatestReport != nil {
		t.Fatalf("empty snapshot summary/latest = %#v/%#v, want nil", report.Summary, report.LatestReport)
	}
	if report.ProviderResults == nil || report.ServiceUnlocks == nil || report.History == nil {
		t.Fatalf("empty snapshot slices = %#v/%#v/%#v, want non-nil empty slices", report.ProviderResults, report.ServiceUnlocks, report.History)
	}
	if len(report.ProviderResults) != 0 || len(report.ServiceUnlocks) != 0 || len(report.History) != 0 {
		t.Fatalf("empty snapshot slices = %#v/%#v/%#v, want empty", report.ProviderResults, report.ServiceUnlocks, report.History)
	}
}

func assertIPQualitySnapshotComplete(t *testing.T, report ipquality.VPSReport, wantReportID, marker, assignmentMode string, ambiguous bool) {
	t.Helper()
	if report.Summary == nil || report.LatestReport == nil {
		t.Fatalf("snapshot summary/latest = %#v/%#v, want complete response", report.Summary, report.LatestReport)
	}
	if wantReportID != "" && report.LatestReport.ReportID != wantReportID {
		t.Fatalf("snapshot report id = %q, want %q", report.LatestReport.ReportID, wantReportID)
	}
	if report.Summary.ReportID != report.LatestReport.ReportID {
		t.Fatalf("snapshot summary/report ids = %q/%q, want same id", report.Summary.ReportID, report.LatestReport.ReportID)
	}
	if report.LatestReport.AgentVersion != marker || report.Summary.AssignmentMode != assignmentMode || report.Summary.Ambiguous != ambiguous {
		t.Fatalf("snapshot marker/assignment = %q/%q/%t, want %q/%q/%t", report.LatestReport.AgentVersion, report.Summary.AssignmentMode, report.Summary.Ambiguous, marker, assignmentMode, ambiguous)
	}
	if !strings.Contains(string(report.LatestReport.RawJSON), marker) || !strings.Contains(string(report.LatestReport.DiagnosticsJSON), marker) {
		t.Fatalf("snapshot report JSON marker missing: raw=%s diagnostics=%s", report.LatestReport.RawJSON, report.LatestReport.DiagnosticsJSON)
	}
	if len(report.ProviderResults) != 3 || len(report.ServiceUnlocks) != 2 {
		t.Fatalf("snapshot child counts = %d/%d, want 3/2", len(report.ProviderResults), len(report.ServiceUnlocks))
	}
	if !strings.Contains(string(report.ProviderResults[0].ExtraJSON), marker) || !strings.Contains(string(report.ServiceUnlocks[0].ExtraJSON), marker) {
		t.Fatalf("snapshot child marker missing: providers=%s unlocks=%s", report.ProviderResults[0].ExtraJSON, report.ServiceUnlocks[0].ExtraJSON)
	}
	if report.ProviderResults == nil || report.ServiceUnlocks == nil || report.History == nil {
		t.Fatalf("snapshot slices = %#v/%#v/%#v, want non-nil", report.ProviderResults, report.ServiceUnlocks, report.History)
	}
}

func seedIPQualitySnapshotIPMatchFixture(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, prefix, vpsID, monitoringInstanceID, ipAddress string) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `
		insert into public.vps_assets (vps_id, display_name, ipv4, lifecycle_status)
		values ($1,$2,$3,'active')`, vpsID, prefix, ipAddress); err != nil {
		t.Fatalf("seed IP-match VPS %q: %v", vpsID, err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.monitoring_instances (
			vps_id, monitoring_instance_id, display_name, region, city, provider,
			lifecycle_status, monitoring_status, binding_status, binding_fingerprint,
			binding_epoch_started_at, sync_token_hash
		) values ($1,$2,$3,'','','',$4,$5,$6,$7,$8,$9)`,
		vpsID, monitoringInstanceID, prefix+" monitor",
		monitoringinstances.LifecycleInUse, monitoringinstances.MonitoringEnabled,
		monitoringinstances.BindingBound, "fp_"+prefix, syncInterleavingT1.Add(-time.Hour),
		hashSyncToken("token_"+prefix),
	); err != nil {
		t.Fatalf("seed IP-match monitoring instance %q: %v", monitoringInstanceID, err)
	}
}

type ipQualitySyncStoredReport struct {
	ReportID          string
	AgentVersion      string
	ReceivedAt        time.Time
	RawJSONIsNull     bool
	RawJSONIsJSONNull bool
}

func readIPQualitySyncStoredReports(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, monitoringInstanceID, syncBatchID string) []ipQualitySyncStoredReport {
	t.Helper()
	rows, err := fixture.db.Query(ctx, `
		select report_id, agent_version, received_at,
			raw_json is null,
			raw_json is not null and raw_json = 'null'::jsonb
		from public.ip_quality_reports
		where monitoring_instance_id = $1 and sync_batch_id = $2
		order by observed_at desc, report_id`, monitoringInstanceID, syncBatchID)
	if err != nil {
		t.Fatalf("query stored IP quality reports: %v", err)
	}
	defer rows.Close()
	stored := make([]ipQualitySyncStoredReport, 0)
	for rows.Next() {
		var report ipQualitySyncStoredReport
		if err := rows.Scan(&report.ReportID, &report.AgentVersion, &report.ReceivedAt, &report.RawJSONIsNull, &report.RawJSONIsJSONNull); err != nil {
			t.Fatalf("scan stored IP quality report: %v", err)
		}
		stored = append(stored, report)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate stored IP quality reports: %v", err)
	}
	return stored
}

type ipQualitySyncFactCounts struct {
	AgentSyncBatches  int
	Heartbeats        int
	HostSamples       int
	ProbeObservations int
	Reports           int
	ProviderResults   int
	ServiceUnlocks    int
}

func readIPQualitySyncFactCounts(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, monitoringInstanceID string) ipQualitySyncFactCounts {
	t.Helper()
	var counts ipQualitySyncFactCounts
	if err := fixture.db.QueryRow(ctx, `
		select
			(select count(*)::int from public.agent_sync_batches where monitoring_instance_id = $1),
			(select count(*)::int from public.monitoring_instance_heartbeats where monitoring_instance_id = $1),
			(select count(*)::int from public.host_samples where monitoring_instance_id = $1),
			(select count(*)::int from public.probe_observations where monitoring_instance_id = $1),
			(select count(*)::int from public.ip_quality_reports where monitoring_instance_id = $1),
			(select count(*)::int
			 from public.ip_quality_provider_results p
			 join public.ip_quality_reports r on r.report_id = p.report_id
			 where r.monitoring_instance_id = $1),
			(select count(*)::int
			 from public.ip_quality_service_unlocks s
			 join public.ip_quality_reports r on r.report_id = s.report_id
			 where r.monitoring_instance_id = $1)`, monitoringInstanceID).Scan(
		&counts.AgentSyncBatches,
		&counts.Heartbeats,
		&counts.HostSamples,
		&counts.ProbeObservations,
		&counts.Reports,
		&counts.ProviderResults,
		&counts.ServiceUnlocks,
	); err != nil {
		t.Fatalf("read IP quality sync fact counts: %v", err)
	}
	return counts
}

func assertIPQualitySyncRoundTrip(t *testing.T, report ipquality.VPSReport, wantReceivedAt time.Time, requestID string) {
	t.Helper()
	if report.Summary == nil || report.LatestReport == nil {
		t.Fatalf("VPS report = %#v, want summary and latest report", report)
	}
	latest := report.LatestReport
	if latest.AgentVersion != "agent/ip-quality-v2" || latest.Fingerprint == "" || latest.SyncBatchID == "" || latest.IPAddress == "" || latest.IPVersion != 4 || latest.Status != agentapi.IPQualityStatusPartial || latest.ASN != "AS64500" || latest.Organization != "Example Transit" || latest.UseRegionCode != "US" || latest.UseRegionName != "United States" || latest.RegisteredRegionCode != "US" || latest.RegisteredRegionName != "United States" || latest.RiskLevel != "medium" || latest.ErrorCode != "provider_partial" || latest.ErrorSummary != "one provider failed" || latest.IsBackfilled || !latest.ReceivedAt.Equal(wantReceivedAt) {
		t.Fatalf("latest report = %#v, want complete metadata round-trip", latest)
	}
	if latest.Latitude == nil || *latest.Latitude != 35.6895 || latest.Longitude == nil || *latest.Longitude != 139.69171 {
		t.Fatalf("latest coordinates = (%v,%v), want normalized coordinates", latest.Latitude, latest.Longitude)
	}
	assertIPQualityJSONFields(t, latest.RawJSON, `{"nested":{"token":"[redacted]","ordinary":"kept"}}`)
	assertIPQualityJSONFields(t, latest.DiagnosticsJSON, fmt.Sprintf(`{"source_version":"v2","service_probe_revision":1,"collect_request_id":"%s","nested":{"password":"[redacted]","ordinary":"diagnostic-kept"}}`, requestID))
	wantCoverage := &ipquality.Coverage{
		ExpectedProviderCount: 3, SuccessfulProviderCount: 1, FailedProviderCount: 1, NotConfiguredProviderCount: 1,
		ExpectedServiceCount: 2, SuccessfulServiceCount: 1, FailedServiceCount: 0, SkippedServiceCount: 1,
	}
	if latest.Coverage == nil || *latest.Coverage != *wantCoverage || report.Summary.Coverage == nil || *report.Summary.Coverage != *wantCoverage {
		t.Fatalf("coverage latest=%#v summary=%#v, want %#v", latest.Coverage, report.Summary.Coverage, wantCoverage)
	}
	if report.Summary.Status != agentapi.IPQualityStatusPartial || report.Summary.AssignmentMode != "link" || report.Summary.ProviderCount != 3 || report.Summary.UnlockableCount != 2 || len(report.History) != 2 {
		t.Fatalf("summary/history = %#v/%#v, want linked partial summary with two reports", report.Summary, report.History)
	}

	providers := make(map[string]ipquality.ProviderResultRead, len(report.ProviderResults))
	for _, provider := range report.ProviderResults {
		providers[provider.Provider] = provider
	}
	if len(providers) != 3 {
		t.Fatalf("provider results = %#v, want success/failure/not_configured rows", report.ProviderResults)
	}
	success, ok := providers["ipapi.is"]
	if !ok || success.Status != "success" || success.SourceType != "default" || success.LatencyMS == nil || *success.LatencyMS != 31 || success.UsageType != "isp" || success.CompanyType != "isp" || success.RiskLevel != "low" || success.RiskScore != "3" || success.RegionCode != "US" || success.RegionName != "United States" || success.IsProxy == nil || *success.IsProxy || success.IsTor == nil || *success.IsTor || success.IsVPN == nil || *success.IsVPN || success.IsServer == nil || !*success.IsServer || success.IsAbuser == nil || *success.IsAbuser || success.IsRobot == nil || *success.IsRobot {
		t.Fatalf("success provider = %#v, want all provider metadata", success)
	}
	assertIPQualityJSONFields(t, success.ExtraJSON, `{"nested":{"api_key":"[redacted]","ordinary":"kept"}}`)
	failure, ok := providers["ip2location.io"]
	if !ok || failure.Status != "failure" || failure.SourceType != "default" || failure.LatencyMS == nil || *failure.LatencyMS != 420 || failure.ErrorCode != "rate_limit" || failure.ErrorSummary != "provider rate limited" {
		t.Fatalf("failure provider = %#v, want failure metadata", failure)
	}
	optional, ok := providers["maxmind"]
	if !ok || optional.Status != "not_configured" || optional.SourceType != "optional" || optional.ErrorCode != "not_configured" || optional.ErrorSummary != "optional source not configured" {
		t.Fatalf("optional provider = %#v, want not_configured metadata", optional)
	}

	services := make(map[string]ipquality.ServiceUnlockRead, len(report.ServiceUnlocks))
	for _, service := range report.ServiceUnlocks {
		services[service.Service+"|"+service.Source] = service
	}
	if len(services) != 2 {
		t.Fatalf("service unlocks = %#v, want two sources for one service", report.ServiceUnlocks)
	}
	unlocked, ok := services["netflix|netflix_title_probe"]
	if !ok || unlocked.Status != "unlocked" || unlocked.ProbeStatus != "success" || unlocked.LatencyMS == nil || *unlocked.LatencyMS != 211 || unlocked.Region != "US" || unlocked.UnlockType != "full" {
		t.Fatalf("unlocked service = %#v, want success metadata", unlocked)
	}
	assertIPQualityJSONFields(t, unlocked.ExtraJSON, `{"nested":{"cookie":"[redacted]","ordinary":"kept"}}`)
	skipped, ok := services["netflix|legacy_probe"]
	if !ok || skipped.Status != "unknown" || skipped.ProbeStatus != "skipped" || skipped.ErrorCode != "unsupported_probe" || skipped.ErrorSummary != "probe skipped" {
		t.Fatalf("skipped service = %#v, want unknown/skipped metadata", skipped)
	}
	assertIPQualityJSONFields(t, skipped.ExtraJSON, `{"truncated":true,"reason":"extra_json_size_limit"}`)
}

func assertIPQualityJSONFields(t *testing.T, raw json.RawMessage, expected string) {
	t.Helper()
	var got, want map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode stored JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatalf("decode expected JSON: %v", err)
	}
	for key, value := range want {
		if !reflect.DeepEqual(got[key], value) {
			t.Errorf("stored JSON field %q = %#v, want %#v", key, got[key], value)
		}
	}
}

func ipQualitySyncFullReport(prefix, syncBatchID string, observedAt, receivedAt time.Time, requestID string) ipquality.ReportWrite {
	return ipquality.ReportWrite{
		MonitoringInstanceID: "mi_" + prefix,
		ObservedAt:           observedAt,
		ReceivedAt:           receivedAt,
		AgentVersion:         "agent/ip-quality-v2",
		Fingerprint:          "fp_" + prefix,
		SyncBatchID:          syncBatchID,
		IPAddress:            syncInterleavingIPAddress(prefix),
		IPVersion:            4,
		Status:               agentapi.IPQualityStatusPartial,
		ASN:                  "AS64500",
		Organization:         "Example Transit",
		Latitude:             new(35.6895),
		Longitude:            new(139.69171),
		UseRegionCode:        "US",
		UseRegionName:        "United States",
		RegisteredRegionCode: "US",
		RegisteredRegionName: "United States",
		RiskLevel:            "medium",
		ErrorCode:            "provider_partial",
		ErrorSummary:         "one provider failed",
		RawJSON:              json.RawMessage(`{"nested":{"token":"secret-token","ordinary":"kept"}}`),
		CoverageJSON:         json.RawMessage(`{"expected_provider_count":3,"successful_provider_count":1,"failed_provider_count":1,"skipped_provider_count":0,"not_configured_provider_count":1,"expected_service_count":2,"successful_service_count":1,"failed_service_count":0,"skipped_service_count":1,"not_configured_service_count":0}`),
		DiagnosticsJSON:      json.RawMessage(fmt.Sprintf(`{"source_version":"v2","service_probe_revision":1,"collect_request_id":"%s","nested":{"password":"secret-password","ordinary":"diagnostic-kept"}}`, requestID)),
		CollectRequestID:     requestID,
		ProviderResults: []ipquality.ProviderResultWrite{
			{
				Provider:    "ipapi.is",
				Status:      "success",
				SourceType:  "default",
				LatencyMS:   new(31),
				UsageType:   "isp",
				CompanyType: "isp",
				RiskLevel:   "low",
				RiskScore:   "3",
				RegionCode:  "US",
				RegionName:  "United States",
				IsProxy:     new(false),
				IsTor:       new(false),
				IsVPN:       new(false),
				IsServer:    new(true),
				IsAbuser:    new(false),
				IsRobot:     new(false),
				ExtraJSON:   json.RawMessage(`{"nested":{"api_key":"provider-secret","ordinary":"kept"}}`),
			},
			{
				Provider:     "ip2location.io",
				Status:       "failure",
				SourceType:   "default",
				LatencyMS:    new(420),
				ErrorCode:    "rate_limit",
				ErrorSummary: "provider rate limited",
			},
			{
				Provider:     "maxmind",
				Status:       "not_configured",
				SourceType:   "optional",
				ErrorCode:    "not_configured",
				ErrorSummary: "optional source not configured",
			},
		},
		ServiceUnlocks: []ipquality.ServiceUnlockWrite{
			{
				Service:     "netflix",
				Source:      "netflix_title_probe",
				Status:      "unlocked",
				ProbeStatus: "success",
				LatencyMS:   new(211),
				Region:      "US",
				UnlockType:  "full",
				ExtraJSON:   json.RawMessage(`{"nested":{"cookie":"service-secret","ordinary":"kept"}}`),
			},
			{
				Service:      "netflix",
				Source:       "legacy_probe",
				Status:       "unknown",
				ProbeStatus:  "skipped",
				ErrorCode:    "unsupported_probe",
				ErrorSummary: "probe skipped",
				ExtraJSON:    json.RawMessage(fmt.Sprintf(`{"payload":%q}`, strings.Repeat("x", ipquality.MaxRawJSONBytes+1))),
			},
		},
	}
}

func ipQualitySyncFallbackReport(prefix, syncBatchID string, observedAt time.Time) ipquality.ReportWrite {
	return ipquality.ReportWrite{
		MonitoringInstanceID: "mi_" + prefix,
		ObservedAt:           observedAt,
		AgentVersion:         "agent/ip-quality-zero-received",
		Fingerprint:          "fp_" + prefix,
		SyncBatchID:          syncBatchID,
		IPAddress:            syncInterleavingIPAddress(prefix),
		IPVersion:            4,
		Status:               agentapi.IPQualityStatusSuccess,
	}
}

func ipQualitySyncStandaloneReport(prefix, syncBatchID, agentVersion string, observedAt, receivedAt time.Time) ipquality.ReportWrite {
	return ipquality.ReportWrite{
		MonitoringInstanceID: "mi_" + prefix,
		ObservedAt:           observedAt,
		ReceivedAt:           receivedAt,
		AgentVersion:         agentVersion,
		Fingerprint:          "fp_" + prefix,
		SyncBatchID:          syncBatchID,
		IPAddress:            syncInterleavingIPAddress(prefix),
		IPVersion:            4,
		Status:               agentapi.IPQualityStatusSuccess,
	}
}
