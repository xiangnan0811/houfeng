//go:build linux

package deploy

import (
	"context"
	"time"
)

const (
	ledgerMaintenanceTotalLimit     = 60 * time.Second
	ledgerMaintenanceCleanupReserve = 15 * time.Second
	ledgerOrdinaryCleanupLimit      = 10 * time.Second
)

type ledgerMaintenanceDeadlineKey struct{}

type ledgerMaintenanceDeadline struct {
	work  time.Time
	total time.Time
}

// ledgerMaintenanceWorkContext fixes one deadline at the outer entrypoint.
// Reentry inherits the same work and cleanup windows rather than starting a
// new timeout. A caller with less than the cleanup reserve cannot start work.
func ledgerMaintenanceWorkContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, nil, ErrRecordLedgerFence
	}
	if value := ctx.Value(ledgerMaintenanceDeadlineKey{}); value != nil {
		limit, ok := value.(ledgerMaintenanceDeadline)
		if !ok || limit.work.IsZero() || !limit.work.Before(limit.total) || !time.Now().Before(limit.work) {
			return nil, nil, ErrRecordLedgerFence
		}
		work, cancel := context.WithDeadline(ctx, limit.work)
		return work, cancel, nil
	}
	total, cancelTotal := context.WithTimeout(ctx, ledgerMaintenanceTotalLimit)
	deadline, ok := total.Deadline()
	if !ok || time.Until(deadline) <= ledgerMaintenanceCleanupReserve {
		cancelTotal()
		return nil, nil, ErrRecordLedgerFence
	}
	limit := ledgerMaintenanceDeadline{work: deadline.Add(-ledgerMaintenanceCleanupReserve), total: deadline}
	work, cancelWork := context.WithDeadline(total, limit.work)
	return context.WithValue(work, ledgerMaintenanceDeadlineKey{}, limit), func() {
		cancelWork()
		cancelTotal()
	}, nil
}

// Cleanup may outlive cancellation of the work window, but not the original
// absolute deadline or an independently shorter deadline applied by a caller.
// Ordinary maintenance retains its previous ten-second cleanup bound.
func ledgerMaintenanceCleanupContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrRecordLedgerFence
	}
	deadline := time.Now().Add(ledgerOrdinaryCleanupLimit)
	if value := ctx.Value(ledgerMaintenanceDeadlineKey{}); value != nil {
		limit, ok := value.(ledgerMaintenanceDeadline)
		if !ok || limit.work.IsZero() || !limit.work.Before(limit.total) {
			return nil, nil, ErrRecordLedgerFence
		}
		deadline = limit.total
		if inherited, ok := ctx.Deadline(); ok && !inherited.Equal(limit.work) && inherited.Before(deadline) {
			deadline = inherited
		}
	} else if inherited, ok := ctx.Deadline(); ok && inherited.Before(deadline) {
		deadline = inherited
	}
	if !time.Now().Before(deadline) {
		return nil, nil, ErrRecordLedgerFence
	}
	cleanup, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	return cleanup, cancel, nil
}

func ledgerMaintenanceCallbackCleanupContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if ctx == nil || ctx.Value(ledgerMaintenanceDeadlineKey{}) == nil {
		return nil, nil, ErrRecordLedgerFence
	}
	return ledgerMaintenanceCleanupContext(ctx)
}

// A public predecessor call may be nested inside ordinary maintenance. Keep
// its earliest deadline on the fence so the outer administrator cleanup and
// any subsequent entry cannot start a new lifetime.
func (f *RecordLedgerFence) bindMaintenanceWork(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if f == nil || f.self != f || f.ctx == nil || ctx == nil || f.ctx.Err() != nil {
		return nil, nil, ErrRecordLedgerFence
	}
	limit, ok := ctx.Value(ledgerMaintenanceDeadlineKey{}).(ledgerMaintenanceDeadline)
	if !ok || limit.work.IsZero() || !limit.work.Before(limit.total) {
		return nil, nil, ErrRecordLedgerFence
	}
	if inherited, ok := ctx.Deadline(); ok && inherited.Before(limit.work) {
		limit = ledgerMaintenanceDeadline{work: inherited.Add(-ledgerMaintenanceCleanupReserve), total: inherited}
	}
	if value := f.ctx.Value(ledgerMaintenanceDeadlineKey{}); value != nil {
		scope, ok := value.(ledgerMaintenanceDeadline)
		if !ok || scope.work.IsZero() || !scope.work.Before(scope.total) {
			return nil, nil, ErrRecordLedgerFence
		}
		if scope.total.Before(limit.total) {
			limit = scope
		}
		if inherited, ok := f.ctx.Deadline(); ok && inherited.Before(scope.work) && inherited.Before(limit.total) {
			limit = ledgerMaintenanceDeadline{work: inherited.Add(-ledgerMaintenanceCleanupReserve), total: inherited}
		}
	} else if inherited, ok := f.ctx.Deadline(); ok && inherited.Before(limit.total) {
		limit = ledgerMaintenanceDeadline{work: inherited.Add(-ledgerMaintenanceCleanupReserve), total: inherited}
	}
	f.mu.Lock()
	if !f.callbackDeadline.total.IsZero() && f.callbackDeadline.total.Before(limit.total) {
		limit = f.callbackDeadline
	}
	f.callbackDeadline = limit
	f.mu.Unlock()
	work, cancel, err := ledgerMaintenanceWorkContext(context.WithValue(ctx, ledgerMaintenanceDeadlineKey{}, limit))
	if err != nil {
		return nil, nil, err
	}
	stop := context.AfterFunc(f.ctx, cancel)
	return work, func() {
		stop()
		cancel()
	}, nil
}

func ledgerFenceCleanupContext(ctx context.Context, f *RecordLedgerFence) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrRecordLedgerFence
	}
	if f != nil {
		f.mu.Lock()
		limit := f.callbackDeadline
		f.mu.Unlock()
		if !limit.total.IsZero() {
			ctx = context.WithValue(ctx, ledgerMaintenanceDeadlineKey{}, limit)
		}
	}
	return ledgerMaintenanceCleanupContext(ctx)
}
