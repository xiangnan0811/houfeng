package store

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/incidents"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/retention"
)

func TestPostgresIntegrationRetentionLiveHeartbeatWindow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtime := fixture.openDirectRuntimePool(t, ctx, "retention-live-heartbeats", 4)
	repo := NewPostgresRetentionRepository(runtime)
	snapshots := incidents.NewPostgresSnapshotReader(runtime)
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	for _, prefix := range []string{"short_live", "short_skew", "short_sparse"} {
		seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
	}
	insert := func(instance, batch string, observed, received time.Time, backfilled bool) {
		t.Helper()
		if _, err := fixture.db.Exec(ctx, `
			insert into monitoring_instance_heartbeats
				(monitoring_instance_id, observed_at, received_at, agent_version, fingerprint, sync_batch_id, is_backfilled)
			values ($1,$2,$3,$4,'fp',$4,$5)`, instance, observed, received, batch, backfilled); err != nil {
			t.Fatal(err)
		}
	}
	insertSeries := func(instance string, amount int, startObserved, startReceived time.Time) {
		t.Helper()
		if _, err := fixture.db.Exec(ctx, `
			insert into monitoring_instance_heartbeats
				(monitoring_instance_id, observed_at, received_at, agent_version, fingerprint, sync_batch_id, is_backfilled)
			select $1::text, $3::timestamptz + n * interval '5 seconds', $4::timestamptz + n * interval '5 seconds',
				'agent/series', 'fp', concat($1::text, '-', n), false
			from generate_series(1,$2::int) n`, instance, amount, startObserved, startReceived); err != nil {
			t.Fatal(err)
		}
	}
	insertSeries("mi_short_live", 4100, now.AddDate(0, 0, -14), now.AddDate(0, 0, -14))
	insertSeries("mi_short_live", incidents.HeartbeatRecoveryCandidateLimit, now.AddDate(0, 0, -1), now.AddDate(0, 0, -1))
	insert("mi_short_live", "exact_short_cut", now.AddDate(0, 0, -7), now.AddDate(0, 0, -7), false)
	insert("mi_short_live", "legacy_witness", now.AddDate(0, 0, -14), now, true)
	insert("mi_short_live", "expired_raw", now.AddDate(0, 0, -31), now, true)
	insert("mi_short_skew", "later_observation", now.AddDate(0, 0, -10), now.AddDate(0, 0, -9), false)
	insertSeries("mi_short_skew", incidents.HeartbeatRecoveryCandidateLimit, now.AddDate(0, 0, -28), now.Add(time.Hour*-24))
	insert("mi_short_sparse", "sparse_live", now.AddDate(0, 0, -9), now.AddDate(0, 0, -9), false)

	startedAt := now.AddDate(0, 0, -29)
	before, err := snapshots.ListRecentLiveHeartbeatReceipts(ctx, "mi_short_live", startedAt)
	if err != nil || len(before) != 3 {
		t.Fatalf("precondition recovery receipts = %+v, err = %v", before, err)
	}
	versionBefore, err := loadMonitoringRecordSubject(ctx, runtime, "mi_short_skew")
	if err != nil || versionBefore.AgentVersion != "later_observation" {
		t.Fatalf("precondition latest subject = %+v, err = %v", versionBefore, err)
	}
	inheritedPolicy := retention.Policy{RawLayerDays: 30, AggregateLayerDays: 365}
	baseline, err := repo.ApplyRetention(ctx, inheritedPolicy, now)
	if err != nil || baseline.DeletedHeartbeats != 1 || baseline.DeletedLiveHeartbeatsEarly != 0 {
		t.Fatalf("inherit must apply only normal expiry: %+v, err = %v", baseline, err)
	}
	policy := inheritedPolicy
	policy.LiveHeartbeatDays = 7
	for _, want := range []int64{4096, 4, 0} {
		result, err := repo.ApplyRetention(ctx, policy, now)
		if err != nil || result.DeletedHeartbeats != want || result.DeletedLiveHeartbeatsEarly != want {
			t.Fatalf("early cleanup got %+v, err = %v, want %d", result, err, want)
		}
	}
	for _, batch := range []string{"exact_short_cut", "legacy_witness", "later_observation", "sparse_live"} {
		var exists bool
		if err := runtime.QueryRow(ctx, `select exists(select 1 from monitoring_instance_heartbeats where sync_batch_id = $1)`, batch).Scan(&exists); err != nil || !exists {
			t.Fatalf("preserved heartbeat %s missing, err = %v", batch, err)
		}
	}
	after, err := snapshots.ListRecentLiveHeartbeatReceipts(ctx, "mi_short_live", startedAt)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("recovery receipts changed after early cleanup: %+v -> %+v, err = %v", before, after, err)
	}
	versionAfter, err := loadMonitoringRecordSubject(ctx, runtime, "mi_short_skew")
	if err != nil || versionAfter.AgentVersion != versionBefore.AgentVersion {
		t.Fatalf("latest subject changed after early cleanup: %+v, err = %v", versionAfter, err)
	}
	// The skewed prefix falls out of the long window before the older receipt
	// with a newer observation. The latter must still reach recovery readers.
	if _, err := repo.ApplyRetention(ctx, policy, now.AddDate(0, 0, 3)); err != nil {
		t.Fatal(err)
	}
	skewed, err := snapshots.ListRecentLiveHeartbeatReceipts(ctx, "mi_short_skew", startedAt)
	if err != nil || len(skewed) != 1 || skewed[0].SyncBatchID != "later_observation" {
		t.Fatalf("wrong recovery receipts after long-window cutoff advanced: %+v, err = %v", skewed, err)
	}
}

func TestPostgresIntegrationRetentionRollingLiveHeartbeatLoad(t *testing.T) {
	for _, instances := range []int{25, 40} {
		t.Run(fmt.Sprintf("%d_agents", instances), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			fixture := newRecordsPostgresFixture(t, ctx)
			runtime := fixture.openDirectRuntimePool(t, ctx, "retention-rolling-heartbeats", 3)
			repo := NewPostgresRetentionRepository(runtime)
			for i := 1; i <= instances; i++ {
				seedSyncInterleavingFixture(t, ctx, fixture, fmt.Sprintf("roll_%d", i), monitoringinstances.LifecycleInUse)
			}
			now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
			insertBatch := func(tag string, rows int, start time.Time) {
				t.Helper()
				if _, err := fixture.db.Exec(ctx, `
					insert into monitoring_instance_heartbeats
						(monitoring_instance_id, observed_at, received_at, agent_version, fingerprint, sync_batch_id, is_backfilled)
					select monitoring_instance_id, $3::timestamptz + n * interval '5 seconds',
						$3::timestamptz + n * interval '5 seconds', 'agent/load-test', 'fp',
						concat(monitoring_instance_id, '-', $1::text, '-', n), false
					from monitoring_instances cross join generate_series(0,$2::int-1) n`, tag, rows, start); err != nil {
					t.Fatal(err)
				}
			}
			// Cover the age boundary and the receipt prefix without storing
			// seven days of intervening data. Advance an hour per pass at the
			// five-second ingress rate and verify one full batch rolls out.
			prefixRows := incidents.HeartbeatRecoveryCandidateLimit
			insertBatch("recent", prefixRows, now.Add(-2*time.Hour))
			insertBatch("expired", 720, now.AddDate(0, 0, -7).Add(-time.Hour))
			policy := retention.Policy{RawLayerDays: 30, AggregateLayerDays: 365, LiveHeartbeatDays: 7}
			var startWAL string
			if err := runtime.QueryRow(ctx, `select pg_current_wal_insert_lsn()::text`).Scan(&startWAL); err != nil {
				t.Fatal(err)
			}
			var heapBeforeVacuum int64
			for pass := 0; pass < 6; pass++ {
				passTime := now.Add(time.Duration(pass) * time.Hour)
				insertBatch(fmt.Sprintf("pass-%d", pass), 720, passTime.AddDate(0, 0, -7))
				result, err := repo.ApplyRetention(ctx, policy, passTime)
				if err != nil || result.DeletedLiveHeartbeatsEarly != int64(720*instances) {
					t.Fatalf("load %d pass %d got %+v, err = %v", instances, pass, result, err)
				}
				var retained int
				if err := runtime.QueryRow(ctx, `select count(*) from monitoring_instance_heartbeats`).Scan(&retained); err != nil || retained != (prefixRows+720)*instances {
					t.Fatalf("load %d pass %d retained = %d, err = %v", instances, pass, retained, err)
				}
				if pass == 2 {
					if err := runtime.QueryRow(ctx, `select pg_table_size('monitoring_instance_heartbeats')`).Scan(&heapBeforeVacuum); err != nil {
						t.Fatal(err)
					}
					if _, err := fixture.db.Exec(ctx, `vacuum (analyze, truncate false) monitoring_instance_heartbeats`); err != nil {
						t.Fatal(err)
					}
				}
			}
			var heap, indexes, wal int64
			if err := runtime.QueryRow(ctx, `
				select pg_table_size('monitoring_instance_heartbeats'), pg_indexes_size('monitoring_instance_heartbeats'),
					pg_wal_lsn_diff(pg_current_wal_insert_lsn(), $1::text::pg_lsn)::bigint`, startWAL).Scan(&heap, &indexes, &wal); err != nil {
				t.Fatal(err)
			}
			t.Logf("%d instances, six hourly passes, %d rows retained; allocated table before vacuum=%d final=%d, final indexes=%d, WAL generated=%d bytes",
				instances, (prefixRows+720)*instances, heapBeforeVacuum, heap, indexes, wal)
		})
	}
}
