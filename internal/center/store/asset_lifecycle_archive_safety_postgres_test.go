package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
	"sync"
	"testing"
	"time"
)

func archiveSafetyFixture(t *testing.T) (context.Context, *pgxpool.Pool, *PostgresAssetLifecycleRepository, vpsassets.Record) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	db := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vps, err := NewPostgresVPSAssetRepository(db).CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "Archive safety", LifecycleStatus: vpsassets.LifecycleActive})
	if err != nil {
		t.Fatal(err)
	}
	return ctx, db, NewPostgresAssetLifecycleRepository(db), vps
}

func archiveSafetyRequest(t *testing.T, ctx context.Context, r *PostgresAssetLifecycleRepository, vps vpsassets.Record, key string) assetlifecycle.ApplyArchiveInput {
	t.Helper()
	review, err := r.GetVPSArchiveReview(ctx, vps.VPSID)
	if err != nil {
		t.Fatal(err)
	}
	return assetlifecycle.ApplyArchiveInput{ConfirmationName: vps.DisplayName, Reason: "confirmed resource no longer used", PreviewDigest: review.PreviewDigest, IdempotencyKey: key, NeverConnectedConfirmation: review.OnlineEvidence.NeverConnected}
}

func TestArchiveSafetyPostgresNeverConnectedIdempotencyAndRestore(t *testing.T) {
	ctx, db, r, vps := archiveSafetyFixture(t)
	input := archiveSafetyRequest(t, ctx, r, vps, "request")
	unconfirmed := input
	unconfirmed.NeverConnectedConfirmation = false
	if _, err := r.ApplyVPSArchive(ctx, vps.VPSID, unconfirmed); !errors.Is(err, assetlifecycle.ErrInvalidLifecycleActionInput) {
		t.Fatalf("unconfirmed archive = %v", err)
	}
	result, err := r.ApplyVPSArchive(ctx, vps.VPSID, input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := r.ApplyVPSArchive(ctx, vps.VPSID, input)
	if err != nil {
		t.Fatal(err)
	}
	if retry.VPS.ArchivedAt == nil || !retry.VPS.ArchivedAt.Equal(*result.VPS.ArchivedAt) {
		t.Fatal("retry did not return original receipt")
	}
	input.Reason = "different"
	if _, err := r.ApplyVPSArchive(ctx, vps.VPSID, input); !errors.Is(err, assetlifecycle.ErrArchiveIdempotencyConflict) {
		t.Fatalf("reused key = %v", err)
	}
	restored, err := r.RestoreVPSFromArchive(ctx, vps.VPSID, assetlifecycle.RestoreArchiveInput{Reason: "reassess"})
	if err != nil {
		t.Fatal(err)
	}
	if restored.LifecycleStatus != vpsassets.LifecycleActive || len(restored.UsageTags) != 1 || restored.UsageTags[0] != "闲置" {
		t.Fatalf("restored=%+v", restored)
	}
	var count int
	if err := db.QueryRow(ctx, `select count(*) from asset_lifecycle_actions where vps_id=$1 and action_type='archive_vps' and status='completed'`, vps.VPSID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("completed archive count %d %v", count, err)
	}
}

func seedArchiveSafetySession(t *testing.T, ctx context.Context, db *pgxpool.Pool, vpsID string) {
	t.Helper()
	if _, err := db.Exec(ctx, `insert into monitoring_instances(monitoring_instance_id,vps_id,display_name,region,city,provider,lifecycle_status,monitoring_status,binding_status,ever_connected,last_trusted_online_at) values('archive-mi',$1,'archive agent','region','city','provider','已接入','暂停','已绑定',true,now()-interval '181 minutes')`, vpsID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `insert into monitoring_agent_sessions(session_id,monitoring_instance_id,token_hash,started_at,last_trusted_online_at,ever_connected) values('archive-session','archive-mi','hash',now()-interval '1 day',now()-interval '181 minutes',true); insert into receiver_health(id,boot_id,healthy_since,checked_at,healthy) values(true,'boot',now()-interval '181 minutes',now(),true)`); err != nil {
		t.Fatal(err)
	}

}

func TestArchiveSafetyPostgresDurableBoundaryAndFreshSignal(t *testing.T) {
	ctx, db, r, vps := archiveSafetyFixture(t)
	seedArchiveSafetySession(t, ctx, db, vps.VPSID)
	review, err := r.GetVPSArchiveReview(ctx, vps.VPSID)
	if err != nil {
		t.Fatal(err)
	}
	if !review.Eligible || review.OnlineEvidence.NeverConnected {
		t.Fatalf("eligible old session=%+v", review)
	}
	input := archiveSafetyRequest(t, ctx, r, vps, "fresh-signal")
	if _, err := db.Exec(ctx, `update monitoring_agent_sessions set last_trusted_online_at=clock_timestamp() where session_id='archive-session'`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApplyVPSArchive(ctx, vps.VPSID, input); !errors.Is(err, assetlifecycle.ErrLifecycleActionBlocked) {
		t.Fatalf("new signal must block: %v", err)
	}
	if _, err := db.Exec(ctx, `update monitoring_agent_sessions set last_trusted_online_at=now()-interval '179 minutes 59 seconds'; update receiver_health set checked_at=clock_timestamp()`); err != nil {
		t.Fatal(err)
	}
	review, err = r.GetVPSArchiveReview(ctx, vps.VPSID)
	if err != nil || review.Eligible {
		t.Fatalf("179m59s allowed %v %+v", err, review)
	}
	if _, err := db.Exec(ctx, `update monitoring_agent_sessions set last_trusted_online_at=now()-interval '180 minutes';update receiver_health set checked_at=clock_timestamp()`); err != nil {
		t.Fatal(err)
	}
	input = archiveSafetyRequest(t, ctx, r, vps, "boundary")
	if _, err := r.ApplyVPSArchive(ctx, vps.VPSID, input); err != nil {
		t.Fatal(err)
	}
	var lifecycle, capability string
	if err := db.QueryRow(ctx, `select n.lifecycle_status,s.capability from monitoring_instances n join monitoring_agent_sessions s using(monitoring_instance_id) where n.monitoring_instance_id='archive-mi'`).Scan(&lifecycle, &capability); err != nil || lifecycle != "已退役" || capability != "evidence_only" {
		t.Fatalf("retirement %s %s %v", lifecycle, capability, err)
	}
}

func TestArchiveSafetyPostgresConcurrentRetryAndRollback(t *testing.T) {
	ctx, db, r, vps := archiveSafetyFixture(t)
	seedArchiveSafetySession(t, ctx, db, vps.VPSID)
	if _, err := db.Exec(ctx, `insert into targets(target_id,name,target_type,host,run_status) values('rollback-probe','rollback','http','example.test','启用')`); err != nil {
		t.Fatal(err)
	}
	targetID := "rollback-probe"
	if _, err := NewPostgresAssetServiceRepository(db).CreateAssetService(ctx, assetservices.CreateInput{VPSID: vps.VPSID, Name: "rollback service", TargetID: &targetID}); err != nil {
		t.Fatal(err)
	}
	input := archiveSafetyRequest(t, ctx, r, vps, "concurrent")
	// Failing the final receipt insert proves all prior lifecycle and followup
	// writes roll back, rather than leaving a half archived resource.
	if _, err := db.Exec(ctx, `create function archive_receipt_fail() returns trigger language plpgsql as $$begin raise exception 'injected receipt failure';end$$;create trigger archive_receipt_fail before insert on vps_archive_requests for each row execute function archive_receipt_fail()`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApplyVPSArchive(ctx, vps.VPSID, input); err == nil {
		t.Fatal("expected injected receipt failure")
	}
	var state string
	if err := db.QueryRow(ctx, `select lifecycle_status from vps_assets where vps_id=$1`, vps.VPSID).Scan(&state); err != nil || state != "active" {
		t.Fatalf("partial archive %s %v", state, err)
	}
	var instanceState, capability, targetState string
	var openAssociations int
	if err := db.QueryRow(ctx, `select n.lifecycle_status,s.capability,(select lifecycle_status from targets where target_id='rollback-probe'),(select count(*) from asset_service_associations where ended_at is null) from monitoring_instances n join monitoring_agent_sessions s using(monitoring_instance_id) where n.vps_id=$1`, vps.VPSID).Scan(&instanceState, &capability, &targetState, &openAssociations); err != nil || instanceState != "已接入" || capability != "full" || targetState != "active" || openAssociations != 1 {
		t.Fatalf("partial related mutation: MI=%s capability=%s target=%s associations=%d err=%v", instanceState, capability, targetState, openAssociations, err)
	}
	if _, err := db.Exec(ctx, `drop trigger archive_receipt_fail on vps_archive_requests`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := r.ApplyVPSArchive(ctx, vps.VPSID, input); failures <- err }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRow(ctx, `select count(*) from vps_archive_requests where vps_id=$1`, vps.VPSID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("receipt count %d %v", count, err)
	}
}

func TestArchiveSafetyPostgresWaitsForConcurrentHeartbeat(t *testing.T) {
	ctx, db, r, vps := archiveSafetyFixture(t)
	seedArchiveSafetySession(t, ctx, db, vps.VPSID)
	input := archiveSafetyRequest(t, ctx, r, vps, "concurrent-heartbeat")
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := lockAssetGraphForSync(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `update monitoring_agent_sessions set last_trusted_online_at=clock_timestamp() where session_id='archive-session'`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := r.ApplyVPSArchive(ctx, vps.VPSID, input); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("archive escaped in-flight heartbeat lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, assetlifecycle.ErrLifecycleActionBlocked) {
		t.Fatalf("committed heartbeat not reread: %v", err)
	}
}

type archiveFaultUnavailableDB struct{}

func (archiveFaultUnavailableDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("database unavailable")
}

func TestArchiveSafetyPostgresReceiverRestartAndUnpersistedFault(t *testing.T) {
	ctx, db, r, vps := archiveSafetyFixture(t)
	seedArchiveSafetySession(t, ctx, db, vps.VPSID)
	observer, err := NewReceiverHealthObserver(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	// The first healthy check after boot must start a fresh window, regardless
	// of how long the previous process had been healthy.
	if err := observer.observe(ctx, true, ""); err != nil {
		t.Fatal(err)
	}
	anchor := receiverClockAnchor.Load()
	shifted := *anchor
	shifted.databaseTime = shifted.databaseTime.Add(-10 * time.Second)
	receiverClockAnchor.Store(&shifted)
	betweenTicks, err := r.GetVPSArchiveReview(ctx, vps.VPSID)
	receiverClockAnchor.Store(anchor)
	if err != nil || betweenTicks.OnlineEvidence.ReceiverHealthy {
		t.Fatalf("between-tick database jump trusted: %+v %v", betweenTicks, err)
	}
	review, err := r.GetVPSArchiveReview(ctx, vps.VPSID)
	if err != nil {
		t.Fatal(err)
	}
	if review.Eligible || review.OnlineEvidence.NeverConnected {
		t.Fatalf("restart bypassed window: %+v", review)
	}
	if review.OnlineEvidence.ReceiverHealthy {
		t.Fatal("restoring clock before next observation erased known clock fault")
	}
	// Simulate a database wall clock leap while the process monotonic clock
	// advances normally. Local wall-clock-only checks would miss this.
	observer.previousDB = observer.previousDB.Add(-4 * time.Hour)
	if err := observer.observe(ctx, true, ""); err != nil {
		t.Fatal(err)
	}
	var healthy bool
	var faultReason string
	if err := db.QueryRow(ctx, `select healthy,failure_reason from receiver_health where id`).Scan(&healthy, &faultReason); err != nil || healthy || faultReason != "database_clock_jump" {
		t.Fatalf("database jump health %t reason %s err %v", healthy, faultReason, err)
	}
	if err := observer.observe(ctx, true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `update receiver_health set healthy_since=now()-interval '181 minutes',checked_at=now(),healthy=true`); err != nil {
		t.Fatal(err)
	}
	review, err = r.GetVPSArchiveReview(ctx, vps.VPSID)
	if err != nil || !review.Eligible {
		t.Fatalf("pre-fault %+v %v", review, err)
	}
	if err := MarkReceiverHealthFault(ctx, archiveFaultUnavailableDB{}, "receive failure"); err == nil {
		t.Fatal("expected unavailable persistence")
	}
	// A stale healthy DB row is insufficient when the process knows reception
	// failed, even if it could not persist that failure.
	review, err = r.GetVPSArchiveReview(ctx, vps.VPSID)
	if err != nil {
		t.Fatal(err)
	}
	if review.Eligible || review.OnlineEvidence.ReceiverHealthy {
		t.Fatalf("unpersisted fault bypassed window: %+v", review)
	}
	if err := observer.observe(ctx, false, "receive failure"); err != nil {
		t.Fatal(err)
	}
	if err := observer.observe(ctx, true, ""); err != nil {
		t.Fatal(err)
	}
	review, err = r.GetVPSArchiveReview(ctx, vps.VPSID)
	if err != nil || review.Eligible {
		t.Fatalf("fault recovery retained old window: %+v %v", review, err)
	}
}

func TestArchiveSafetyPostgresSharedRelationsAndTargetContexts(t *testing.T) {
	ctx, db, r, vps := archiveSafetyFixture(t)
	other, err := NewPostgresVPSAssetRepository(db).CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "shared owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `insert into targets(target_id,name,target_type,host,run_status) values('shared-probe','shared','http','example.test','启用'),('exclusive-probe','exclusive','http','private.test','启用')`); err != nil {
		t.Fatal(err)
	}
	targetID := "shared-probe"
	service, err := NewPostgresAssetServiceRepository(db).CreateAssetService(ctx, assetservices.CreateInput{VPSID: vps.VPSID, Name: "shared service", TargetID: &targetID})
	if err != nil {
		t.Fatal(err)
	}
	stale := archiveSafetyRequest(t, ctx, r, vps, "stale-relations")
	if _, err := db.Exec(ctx, `insert into asset_service_associations(id,service_id,vps_id,target_id) values('other-association',$1,$2,'shared-probe')`, service.ServiceID, other.VPSID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApplyVPSArchive(ctx, vps.VPSID, stale); !errors.Is(err, assetlifecycle.ErrStaleArchivePreview) {
		t.Fatalf("shared relationship change did not invalidate preview: %v", err)
	}
	count, err := r.CountRunningTargetsForVPS(ctx, vps.VPSID)
	if err != nil || count != 1 {
		t.Fatalf("current association running count %d %v", count, err)
	}
	contexts, err := r.ListTargetAssetContexts(ctx)
	if err != nil || len(contexts) != 1 || contexts[0].LinkedVPSCount != 2 {
		t.Fatalf("current association target contexts %+v %v", contexts, err)
	}
	targetID = "exclusive-probe"
	if _, err := NewPostgresAssetServiceRepository(db).CreateAssetService(ctx, assetservices.CreateInput{VPSID: vps.VPSID, Name: "exclusive service", TargetID: &targetID}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `insert into active_incidents(incident_id,object_type,object_id,incident_class,severity,started_at,last_evaluated_at,status,source_summary) values('incident-private','target','exclusive-probe','target_probe_failure','告警',now(),now(),'active','failing')`); err != nil {
		t.Fatal(err)
	}
	input := archiveSafetyRequest(t, ctx, r, vps, "shared-archive")
	if _, err := r.ApplyVPSArchive(ctx, vps.VPSID, input); err != nil {
		t.Fatal(err)
	}
	var ended, current, closedEvents, notifications int
	if err := db.QueryRow(ctx, `select count(*) filter(where vps_id=$1 and ended_at is not null),count(*) filter(where vps_id=$2 and ended_at is null) from asset_service_associations`, vps.VPSID, other.VPSID).Scan(&ended, &current); err != nil || ended != 2 || current != 1 {
		t.Fatalf("association ends %d current %d %v", ended, current, err)
	}
	var sharedState, exclusiveState string
	if err := db.QueryRow(ctx, `select (select lifecycle_status from targets where target_id='shared-probe'),(select lifecycle_status from targets where target_id='exclusive-probe')`).Scan(&sharedState, &exclusiveState); err != nil || sharedState != "active" || exclusiveState != "retired" {
		t.Fatalf("target lifecycles %s %s %v", sharedState, exclusiveState, err)
	}
	if err := db.QueryRow(ctx, `select (select count(*) from state_change_events where event_type='incident_closed_by_management'),(select count(*) from notification_records)`).Scan(&closedEvents, &notifications); err != nil || closedEvents != 1 || notifications != 0 {
		t.Fatalf("management closure events %d notifications %d err %v", closedEvents, notifications, err)
	}
	contexts, err = r.ListTargetAssetContexts(ctx)
	if err != nil || len(contexts) != 1 || contexts[0].LinkedVPSCount != 1 || contexts[0].Summaries[0].VPSID != other.VPSID {
		t.Fatalf("remaining contexts %+v %v", contexts, err)
	}
}

func TestArchiveSafetyPostgresIndependentValidity(t *testing.T) {
	ctx, db, r, vps := archiveSafetyFixture(t)
	expiry := subscriptions.NewDate(time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC))
	if _, err := r.ExtendVPSValidity(ctx, vps.VPSID, assetlifecycle.ExtendValidityInput{ExtendTo: &expiry, Reason: "resource entitlement confirmed"}); err != nil {
		t.Fatalf("no-subscription validity: %v", err)
	}
	billing := subscriptions.NewDate(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))
	sub, err := NewPostgresSubscriptionRepository(db).CreateSubscription(ctx, subscriptions.CreateInput{VPSID: vps.VPSID, DisplayName: "billing", Currency: "USD", BillingMonths: 1, RenewAt: &billing, Status: subscriptions.StatusActive, RenewalMode: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	expiry = subscriptions.NewDate(time.Date(2027, 2, 2, 0, 0, 0, 0, time.UTC))
	if _, err := r.ExtendVPSValidity(ctx, vps.VPSID, assetlifecycle.ExtendValidityInput{ExtendTo: &expiry, Reason: "short extension"}); err != nil {
		t.Fatal(err)
	}
	var actualExpiry, actualBilling string
	if err := db.QueryRow(ctx, `select v.expires_at::text,s.renew_at::text from vps_assets v join subscriptions s using(vps_id) where s.subscription_id=$1`, sub.SubscriptionID).Scan(&actualExpiry, &actualBilling); err != nil || actualExpiry != "2027-02-02" || actualBilling != "2026-12-01" {
		t.Fatalf("expiry %s billing %s %v", actualExpiry, actualBilling, err)
	}

}

func TestArchiveSafetyPostgresRuntimeRoleEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtime := fixture.openDirectRuntimePool(t, ctx, "archive-safety-runtime", 3)
	vps, err := NewPostgresVPSAssetRepository(runtime).CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "strict runtime archive"})
	if err != nil {
		t.Fatal(err)
	}
	seedArchiveSafetySession(t, ctx, runtime, vps.VPSID)
	observer, err := NewReceiverHealthObserver(ctx, runtime)
	if err != nil {
		t.Fatalf("runtime receiver initialization: %v", err)
	}
	if err := observer.observe(ctx, true, ""); err != nil {
		t.Fatalf("runtime receiver read/write: %v", err)
	}
	// Age only the test fixture's observation origin; production never adjusts
	// observation time. All operations under test use the direct runtime role.
	if _, err := fixture.db.Exec(ctx, `update receiver_health set healthy_since=now()-interval '181 minutes'`); err != nil {
		t.Fatal(err)
	}
	r := NewPostgresAssetLifecycleRepository(runtime)
	input := archiveSafetyRequest(t, ctx, r, vps, "strict-runtime")
	if _, err := r.ApplyVPSArchive(ctx, vps.VPSID, input); err != nil {
		t.Fatalf("runtime archive: %v", err)
	}
	if _, err := r.ApplyVPSArchive(ctx, vps.VPSID, input); err != nil {
		t.Fatalf("runtime idempotent read: %v", err)
	}
	if _, err := r.RestoreVPSFromArchive(ctx, vps.VPSID, assetlifecycle.RestoreArchiveInput{Reason: "runtime restore"}); err != nil {
		t.Fatalf("runtime restore: %v", err)
	}
	var sessionCapability, instanceLifecycle string
	if err := runtime.QueryRow(ctx, `select s.capability,n.lifecycle_status from monitoring_instances n join monitoring_agent_sessions s using(monitoring_instance_id) where n.vps_id=$1`, vps.VPSID).Scan(&sessionCapability, &instanceLifecycle); err != nil || sessionCapability != "evidence_only" || instanceLifecycle != "已退役" {
		t.Fatalf("restore resurrected runtime privileges: %s %s %v", sessionCapability, instanceLifecycle, err)
	}
	_, err = runtime.Exec(ctx, `delete from monitoring_agent_sessions where false`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("runtime can erase durable enrollment fact: %v", err)
	}
}

func TestArchiveSafetyPostgresHistoricalSessionCannotUsePendingException(t *testing.T) {
	ctx, db, r, vps := archiveSafetyFixture(t)
	seedArchiveSafetySession(t, ctx, db, vps.VPSID)
	if _, err := db.Exec(ctx, `update monitoring_instances set lifecycle_status='已退役' where monitoring_instance_id='archive-mi';update monitoring_agent_sessions set capability='evidence_only',last_trusted_online_at=clock_timestamp() where session_id='archive-session'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `insert into monitoring_instances(monitoring_instance_id,vps_id,display_name,region,city,provider,lifecycle_status,monitoring_status,binding_status) values('pending-replacement',$1,'replacement','region','city','provider','待接入','启用','未绑定')`, vps.VPSID); err != nil {
		t.Fatal(err)
	}
	review, err := r.GetVPSArchiveReview(ctx, vps.VPSID)
	if err != nil {
		t.Fatal(err)
	}
	if review.Eligible || review.OnlineEvidence.NeverConnected || review.OnlineEvidence.ManualConfirmationRequired {
		t.Fatalf("retired historical session bypassed by current pending instance: %+v", review)
	}
	var found bool
	for _, item := range review.OnlineEvidence.Instances {
		if item.SessionID == "archive-session" && item.LastTrustedOnlineAt != nil {
			found = true
		}
	}
	if !found {
		t.Fatal("archive evidence omitted historical session")
	}
}

func TestArchiveSafetyPostgresLifecycleReasonRequired(t *testing.T) {
	ctx, db, r, vps := archiveSafetyFixture(t)
	input := archiveSafetyRequest(t, ctx, r, vps, "blank-reason")
	input.Reason = " \t "
	if _, err := r.ApplyVPSArchive(ctx, vps.VPSID, input); !errors.Is(err, assetlifecycle.ErrInvalidLifecycleActionInput) {
		t.Fatalf("blank archive reason: %v", err)
	}
	input.Reason = "confirmed end of use"
	if _, err := r.ApplyVPSArchive(ctx, vps.VPSID, input); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RestoreVPSFromArchive(ctx, vps.VPSID, assetlifecycle.RestoreArchiveInput{Reason: " \t "}); !errors.Is(err, assetlifecycle.ErrInvalidLifecycleActionInput) {
		t.Fatalf("blank restore reason: %v", err)
	}
	var state string
	if err := db.QueryRow(ctx, `select lifecycle_status from vps_assets where vps_id=$1`, vps.VPSID).Scan(&state); err != nil || state != "archived" {
		t.Fatalf("invalid restore changed state=%s err=%v", state, err)
	}
}

func TestArchiveSafetyPostgresHealthObservationDoesNotWaitForGraph(t *testing.T) {
	ctx, db, _, vps := archiveSafetyFixture(t)
	seedArchiveSafetySession(t, ctx, db, vps.VPSID)
	o, err := NewReceiverHealthObserver(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.observe(ctx, true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `update receiver_health set healthy_since=clock_timestamp()-interval '181 minutes'`); err != nil {
		t.Fatal(err)
	}
	var before time.Time
	if err := db.QueryRow(ctx, `select healthy_since from receiver_health where id`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	faultGeneration := receiverFaultGeneration.Load()
	graph, err := beginAssetGraphTx(ctx, db.BeginTx)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Rollback(ctx)
	for i := 0; i < 2; i++ {
		checkCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		err := o.observe(checkCtx, true, "")
		cancel()
		if err != nil {
			t.Fatalf("healthy observation blocked by graph transaction: %v", err)
		}
		if i == 0 {
			time.Sleep(4100 * time.Millisecond)
		}
	}
	var after time.Time
	var healthy bool
	if err := db.QueryRow(ctx, `select healthy_since,healthy from receiver_health where id`).Scan(&after, &healthy); err != nil || !healthy || !before.Equal(after) || receiverFaultGeneration.Load() != faultGeneration {
		t.Fatalf("business graph reset health: before=%v after=%v healthy=%t err=%v", before, after, healthy, err)
	}
}

func TestArchiveSafetyPostgresFinalHealthRecheck(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		t.Run(map[bool]string{true: "healthy tick permits commit", false: "health failure rolls back"}[healthy], func(t *testing.T) {
			ctx, db, r, vps := archiveSafetyFixture(t)
			seedArchiveSafetySession(t, ctx, db, vps.VPSID)
			o, err := NewReceiverHealthObserver(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			if err := o.observe(ctx, true, ""); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(ctx, `update receiver_health set healthy_since=clock_timestamp()-interval '181 minutes'`); err != nil {
				t.Fatal(err)
			}
			input := archiveSafetyRequest(t, ctx, r, vps, "final-health-check")
			if _, err := db.Exec(ctx, `create function pause_archive_business() returns trigger language plpgsql as $$begin if new.lifecycle_status='archived' then perform pg_advisory_xact_lock(1213154899,818281);end if;return new;end$$;create trigger pause_archive_business before update of lifecycle_status on vps_assets for each row execute function pause_archive_business()`); err != nil {
				t.Fatal(err)
			}
			holder, err := db.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer holder.Rollback(ctx)
			if _, err := holder.Exec(ctx, `select pg_advisory_xact_lock(1213154899,818281)`); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := r.ApplyVPSArchive(ctx, vps.VPSID, input); done <- err }()
			if err := waitForBlockedLifecycleSessions(ctx, db, 1); err != nil {
				t.Fatal(err)
			}
			checkCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			err = o.observe(checkCtx, healthy, "injected receive failure")
			cancel()
			if err != nil {
				t.Fatalf("archive held health row during business work: %v", err)
			}
			if err := holder.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			err = <-done
			if healthy && err != nil {
				t.Fatalf("benign health tick invalidated archive: %v", err)
			}
			if !healthy && !errors.Is(err, assetlifecycle.ErrLifecycleActionBlocked) {
				t.Fatalf("health fault after initial safety check ignored: %v", err)
			}
			var state string
			if err := db.QueryRow(ctx, `select lifecycle_status from vps_assets where vps_id=$1`, vps.VPSID).Scan(&state); err != nil {
				t.Fatal(err)
			}
			want := "active"
			if healthy {
				want = "archived"
			}
			if state != want {
				t.Fatalf("final state %s want %s", state, want)
			}
		})
	}
}
