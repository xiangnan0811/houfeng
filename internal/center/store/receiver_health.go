package store

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/ids"
)

var receiverFaultGeneration atomic.Uint64
var receiverObservedFaultGeneration atomic.Uint64
var receiverFaultMu sync.Mutex
var receiverLatestFaultReason = "reception_fault"

type receiverClockSample struct {
	databaseTime time.Time
	processTime  time.Time
}

var receiverClockAnchor atomic.Pointer[receiverClockSample]

type ReceiverHealthObserver struct {
	beginTx           func(context.Context, pgx.TxOptions) (pgx.Tx, error)
	bootID            string
	faultGeneration   uint64
	previous          time.Time
	previousDB        time.Time
	previousDBProcess time.Time
	ready             atomic.Bool
}

// NewReceiverHealthObserver resets persisted continuity synchronously. Invoke
// before the HTTP server starts accepting requests, once for each Center boot.
func NewReceiverHealthObserver(ctx context.Context, db *pgxpool.Pool) (*ReceiverHealthObserver, error) {
	bootID, err := ids.New("receiver")
	if err != nil {
		return nil, err
	}
	o := &ReceiverHealthObserver{beginTx: db.BeginTx, bootID: bootID, faultGeneration: receiverFaultGeneration.Load()}
	if err := o.observe(ctx, false, "center_restart"); err != nil {
		return nil, err
	}
	return o, nil
}

// Run checks service readiness and performs a database read/write using the
// actual runtime role every five seconds. Context cancellation ends observation.
func (o *ReceiverHealthObserver) SetReady(ready bool) {
	o.ready.Store(ready)
	if !ready {
		NoteReceiverHealthFault("service_not_ready")
	}
}

func (o *ReceiverHealthObserver) Run(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := o.observeTick(ctx, time.Now()); err != nil {
				slog.Error("observe receiver health failed", "error", err)
			}
		}
	}
}

func (o *ReceiverHealthObserver) observeTick(ctx context.Context, now time.Time) error {
	healthy, reason := o.ready.Load(), ""
	if !healthy {
		reason = "service_not_ready"
	}
	// time.Sub uses monotonic time; UnixNano exposes wall clock jumps.
	if !o.previous.IsZero() {
		drift := time.Duration(now.UnixNano()-o.previous.UnixNano()) - now.Sub(o.previous)
		if drift > time.Second || drift < -time.Second {
			healthy = false
			reason = "clock_jump"
		}
	}
	o.previous = now
	checkCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if err := o.observe(checkCtx, healthy, reason); err != nil {
		NoteReceiverHealthFault("health_observation_failed")
		return err
	}
	return nil
}

func (o *ReceiverHealthObserver) observe(ctx context.Context, healthy bool, reason string) error {
	observedFaultGeneration, faultReason := receiverHealthFaultSnapshot()
	// Recheck at the write boundary as a fault may arrive between the Run
	// loop's readiness check and this call.
	if observedFaultGeneration != o.faultGeneration {
		healthy = false
		reason = faultReason
		o.faultGeneration = observedFaultGeneration
	}
	// Reception readiness is independent of asset business transactions. A
	// long graph mutation must not prevent the health observation from ticking.
	tx, err := o.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var dbNow time.Time
	if err := tx.QueryRow(ctx, `select clock_timestamp()`).Scan(&dbNow); err != nil {
		return err
	}
	processNow := time.Now()
	if receiverDatabaseClockJump(o.previousDB, o.previousDBProcess, dbNow, processNow) {
		healthy = false
		reason = "database_clock_jump"
	}
	_, err = tx.Exec(ctx, `insert into receiver_health(id,boot_id,healthy_since,checked_at,healthy,failure_reason)
	 values(true,$1,case when $2 then $4::timestamptz else null end,$4,$2,$3)
 on conflict(id) do update set boot_id=excluded.boot_id,
 healthy_since=case when excluded.healthy then case when receiver_health.healthy and receiver_health.boot_id=excluded.boot_id and receiver_health.checked_at <= excluded.checked_at and receiver_health.checked_at >= excluded.checked_at - interval '15 seconds' then receiver_health.healthy_since else excluded.checked_at end else null end,
	 checked_at=excluded.checked_at,healthy=excluded.healthy,failure_reason=excluded.failure_reason`, o.bootID, healthy, reason, dbNow)
	if err != nil {
		return fmt.Errorf("observe receiver health: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	o.previousDB = dbNow
	o.previousDBProcess = processNow
	receiverClockAnchor.Store(&receiverClockSample{databaseTime: dbNow, processTime: processNow})
	// An unseen operational fault must not be acknowledged by a check which
	// started before that fault. The next observation resets the DB window.
	if receiverFaultGeneration.Load() == observedFaultGeneration {
		receiverObservedFaultGeneration.Store(observedFaultGeneration)
	}
	return nil
}

func receiverDatabaseClockJump(previousDB, previousProcess, currentDB, currentProcess time.Time) bool {
	if previousDB.IsZero() || previousProcess.IsZero() {
		return false
	}
	drift := currentDB.Sub(previousDB) - currentProcess.Sub(previousProcess)
	return drift > time.Second || drift < -time.Second
}

// NoteReceiverHealthFault immediately invalidates in-process archive evidence.
// Admission rejection paths call this without database I/O or per-request logs;
// the observer persists the latest allowlisted reason once per five-second tick.
func NoteReceiverHealthFault(reason string) {
	switch reason {
	case "sync_rate_limited", "sync_capacity_exhausted", "sync_internal_error",
		"sync_persistence_failure", "service_not_ready", "health_observation_failed",
		"database_clock_jump", "clock_jump":
	default:
		reason = "reception_fault"
	}
	receiverFaultMu.Lock()
	receiverLatestFaultReason = reason
	receiverFaultGeneration.Add(1)
	receiverFaultMu.Unlock()
}

func receiverHealthFaultSnapshot() (uint64, string) {
	receiverFaultMu.Lock()
	defer receiverFaultMu.Unlock()
	return receiverFaultGeneration.Load(), receiverLatestFaultReason
}

// MarkReceiverHealthFault invalidates memory first, even if PostgreSQL is down.
// The next successful observation cannot accidentally retain prior continuity.
func MarkReceiverHealthFault(ctx context.Context, db interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, reason string) error {
	NoteReceiverHealthFault(reason)
	_, err := db.Exec(ctx, `update receiver_health set healthy=false,healthy_since=null,failure_reason=$1,checked_at=clock_timestamp() where id`, reason)
	return err
}

func loadArchiveOnlineEvidence(ctx context.Context, q assetLifecycleQueryer, vpsID string, lock bool) (assetlifecycle.ArchiveOnlineEvidence, error) {
	e := assetlifecycle.ArchiveOnlineEvidence{Instances: []assetlifecycle.ArchiveInstanceEvidence{}}
	if err := loadArchiveReceiverEvidence(ctx, q, &e, lock); err != nil {
		return e, err
	}
	rows, err := q.Query(ctx, `select n.monitoring_instance_id,''::text session_id,n.ever_connected,null::timestamptz started_at,n.last_trusted_online_at from monitoring_instances n where n.vps_id=$1
	 union all select n.monitoring_instance_id,s.session_id,s.ever_connected,s.started_at,s.last_trusted_online_at from monitoring_instances n join monitoring_agent_sessions s using(monitoring_instance_id) where n.vps_id=$1
	 order by monitoring_instance_id,session_id`, vpsID)
	if err != nil {
		return e, err
	}
	defer rows.Close()
	for rows.Next() {
		var item assetlifecycle.ArchiveInstanceEvidence
		if err = rows.Scan(&item.MonitoringInstanceID, &item.SessionID, &item.EverConnected, &item.SessionStartedAt, &item.LastTrustedOnlineAt); err != nil {
			return e, err
		}
		e.Instances = append(e.Instances, item)
	}
	return e, rows.Err()
}

func loadArchiveReceiverEvidence(ctx context.Context, q assetLifecycleQueryer, e *assetlifecycle.ArchiveOnlineEvidence, lock bool) error {
	e.ReceiverHealthy = false
	e.HealthySince = nil
	e.LastHealthCheckAt = nil
	e.ReceiverGeneration = ""
	healthQuery := `select boot_id,healthy_since,checked_at,healthy from receiver_health where id`
	if lock {
		healthQuery += ` for update`
	}
	// An absent health row is deliberately untrusted, never a bypass.
	rows, err := q.Query(ctx, healthQuery)
	if err != nil {
		return err
	}
	if rows.Next() {
		err = rows.Scan(&e.ReceiverGeneration, &e.HealthySince, &e.LastHealthCheckAt, &e.ReceiverHealthy)
	}
	rows.Close()
	if err != nil {
		return err
	}
	if err = rows.Err(); err != nil {
		return err
	}
	// Read the clock after obtaining the row lock, so time spent waiting cannot
	// make an expired observation appear fresh at the final commit boundary.
	if err := q.QueryRow(ctx, `select clock_timestamp()`).Scan(&e.ObservedAt); err != nil {
		return err
	}
	observedProcessTime := time.Now()
	if receiverFaultGeneration.Load() != receiverObservedFaultGeneration.Load() {
		e.ReceiverHealthy = false
	}
	if anchor := receiverClockAnchor.Load(); anchor != nil && receiverDatabaseClockJump(anchor.databaseTime, anchor.processTime, e.ObservedAt, observedProcessTime) {
		// Persisting here could self-deadlock the archive's final row lock. The
		// process fault generation is the immediate latch; observation must
		// subsequently reset the persisted window even if the clock is restored.
		NoteReceiverHealthFault("database_clock_jump")
		e.ReceiverHealthy = false
	}
	return nil
}
