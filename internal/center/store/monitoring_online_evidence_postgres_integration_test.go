package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresIntegrationMonitoringOnlineEvidenceProjection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db := openTemporaryRuntimeFactsPostgresDatabase(t, ctx)
	const id = "mi_online_evidence"
	base := time.Now().UTC().Truncate(time.Microsecond)
	insertRuntimeFactsMonitoringInstance(t, ctx, db, id, "fp-current", base.Add(-time.Hour))
	if _, err := db.Exec(ctx, `update monitoring_instances set last_trusted_online_at=null, monitoring_status='暂停' where monitoring_instance_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	repo := NewPostgresMonitoringInstanceRepository(db)
	initial, err := repo.GetMonitoringInstanceOnlineEvidence(ctx, id)
	if err != nil || initial.LastTrustedOnlineAt != nil || initial.ReadAt.IsZero() || initial.Type != "online_evidence" || initial.MonitoringInstanceID != id {
		t.Fatalf("initial evidence=%#v err=%v, want nullable independent projection", initial, err)
	}
	insertRuntimeFactsHostSample(t, ctx, db, id, base.Add(-time.Minute), base, "fp-current", 42, 42, true, "online-evidence-backfill")
	afterSample, err := repo.GetMonitoringInstanceOnlineEvidence(ctx, id)
	if err != nil || afterSample.LastTrustedOnlineAt != nil {
		t.Fatalf("backfilled sample changed online evidence=%#v err=%v", afterSample, err)
	}
	const token = "mas_online.secret"
	if _, err := db.Exec(ctx, `insert into monitoring_agent_sessions(session_id,monitoring_instance_id,token_hash,fingerprint_hash,capability) values('mas_online',$1,$2,'fp-current','evidence_only')`, id, hashSyncToken(token)); err != nil {
		t.Fatal(err)
	}
	var before time.Time
	if err := db.QueryRow(ctx, `select clock_timestamp()`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	syncRepo := NewPostgresSyncRepository(db)
	syncRepo.now = func() time.Time { return base.Add(-4 * 24 * time.Hour) }
	batch := syncing.Batch{
		SessionID: "mas_online", MonitoringInstanceID: id, SyncToken: token,
		LiveSignal: &agentapi.LiveSignal{ID: "trusted-one", Fingerprint: "fp-current"},
		Observations: observations.BatchWrite{HostSamples: []observations.HostSampleWrite{{
			MonitoringInstanceID: id, ObservedAt: base, Fingerprint: "fp-current", SyncBatchID: "forbidden-evidence-only-sample",
		}}},
	}
	result, err := syncRepo.ApplyBatch(ctx, batch)
	if err != nil || !result.StopCollection {
		t.Fatalf("evidence-only sync result=%#v err=%v, want collection denied", result, err)
	}
	evidence, err := repo.GetMonitoringInstanceOnlineEvidence(ctx, id)
	if err != nil || evidence.LastTrustedOnlineAt == nil || evidence.LastTrustedOnlineAt.Before(before) || evidence.ReadAt.Before(*evidence.LastTrustedOnlineAt) {
		t.Fatalf("evidence=%#v err=%v, want database clock trusted live evidence", evidence, err)
	}
	var after time.Time
	if err := db.QueryRow(ctx, `select clock_timestamp()`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if evidence.ReadAt.Before(before) || evidence.ReadAt.After(after) {
		t.Fatalf("read_at=%v outside database clock bounds %v..%v", evidence.ReadAt, before, after)
	}
	var samples int
	var capability string
	if err := db.QueryRow(ctx, `select (select count(*) from host_samples where monitoring_instance_id=$1),capability from monitoring_agent_sessions where session_id='mas_online'`, id).Scan(&samples, &capability); err != nil {
		t.Fatal(err)
	}
	if samples != 1 || capability != "evidence_only" {
		t.Fatalf("samples=%d capability=%s, evidence read or sync restored collection authority", samples, capability)
	}
	if _, err := syncRepo.ApplyBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	duplicate, err := repo.GetMonitoringInstanceOnlineEvidence(ctx, id)
	if err != nil || duplicate.LastTrustedOnlineAt == nil || !duplicate.LastTrustedOnlineAt.Equal(*evidence.LastTrustedOnlineAt) {
		t.Fatalf("duplicate signal changed online evidence=%#v err=%v", duplicate, err)
	}
	if _, err := db.Exec(ctx, `update monitoring_instances set lifecycle_status='已退役',binding_status='未绑定',binding_fingerprint=null where monitoring_instance_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	retired, err := repo.GetMonitoringInstanceOnlineEvidence(ctx, id)
	if err != nil || retired.LastTrustedOnlineAt == nil || !retired.LastTrustedOnlineAt.Equal(*evidence.LastTrustedOnlineAt) {
		t.Fatalf("retirement hid trusted evidence=%#v err=%v", retired, err)
	}
	if _, err := repo.GetMonitoringInstanceOnlineEvidence(ctx, "mi_missing"); !errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
		t.Fatalf("missing projection err=%v", err)
	}
}
