package store

import (
	"context"

	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/assetlinks"

	"houfeng/internal/center/vpsassets"
)

func successfulCancellationTx(t *testing.T, vps vpsassets.Record, now time.Time) *fakeAssetLifecycleTx {
	t.Helper()
	return &fakeAssetLifecycleTx{
		queryFunc: emptyLifecycleListQuery,
		queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "from vps_assets"):
				return fakeAssetLifecycleRowFunc(func(dest ...any) error {
					scanVPSAssetRecordDestinations(dest, vps)
					return nil
				})
			case strings.Contains(sql, "update vps_assets"):
				return fakeAssetLifecycleRowFunc(func(dest ...any) error {
					record := vps
					record.LifecycleStatus = vpsassets.LifecycleCancelled
					record.UsageStatus = vpsassets.UsageIdle
					record.RenewalDecision = vpsassets.RenewalCancel
					scanVPSAssetRecordDestinations(dest, record)
					return nil
				})
			case strings.Contains(sql, "insert into renewal_decisions"):
				return fakeAssetLifecycleRowFunc(func(dest ...any) error {
					*(dest[0].(*string)) = "rdec_001"
					*(dest[1].(*string)) = "vps_001"
					fromDecision := "keep"
					*(dest[2].(**string)) = &fromDecision
					*(dest[3].(*string)) = "cancel"
					*(dest[4].(*string)) = "expired and no renewal"
					*(dest[5].(*time.Time)) = now
					*(dest[6].(*time.Time)) = now
					return nil
				})
			default:
				return fakeAssetLifecycleRowFunc(func(dest ...any) error { return pgx.ErrNoRows })
			}
		},
	}
}

func emptyLifecycleListQuery(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return &fakeSubscriptionRows{}, nil
}

func assetLifecycleTestVPSRecord(vpsID string, lifecycle vpsassets.LifecycleStatus, renewal vpsassets.RenewalDecision, now time.Time, archivedAt *time.Time) vpsassets.Record {
	return vpsassets.Record{
		VPSID:           vpsID,
		DisplayName:     "Frankfurt Legacy",
		ProviderName:    "Hetzner",
		ProductName:     "CX21",
		Country:         "DE",
		Region:          "Hesse",
		City:            "Frankfurt",
		IPv4:            "192.0.2.10",
		SSHHost:         "192.0.2.10",
		SSHPort:         22,
		SSHUser:         "root",
		OSName:          "Debian 12",
		Virtualization:  "kvm",
		LifecycleStatus: lifecycle,
		UsageStatus:     vpsassets.UsageInUse,
		RenewalDecision: renewal,
		Importance:      "normal",
		CreatedAt:       now,
		UpdatedAt:       now,
		ArchivedAt:      archivedAt,
	}
}

func filterAssetLifecycleAuditExecs(calls []fakeAssetLifecycleExecCall) ([]fakeAssetLifecycleExecCall, []fakeAssetLifecycleExecCall) {
	actions := []fakeAssetLifecycleExecCall{}
	steps := []fakeAssetLifecycleExecCall{}
	for _, call := range calls {
		switch {
		case strings.Contains(call.sql, "insert into asset_lifecycle_actions"):
			actions = append(actions, call)
		case strings.Contains(call.sql, "insert into asset_lifecycle_action_steps"):
			steps = append(steps, call)
		}
	}
	return actions, steps
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func scanMonitoringInstanceSummaryDestinations(dest []any, summary assetlinks.MonitoringInstanceSummary) {
	*(dest[0].(*string)) = summary.MonitoringInstanceID
	*(dest[1].(*string)) = summary.DisplayName
	*(dest[2].(*string)) = summary.Group
	*(dest[3].(*string)) = summary.Region
	*(dest[4].(*string)) = summary.City
	*(dest[5].(*string)) = summary.Provider
	*(dest[6].(*string)) = summary.LifecycleStatus
	*(dest[7].(*string)) = summary.MonitoringStatus
	*(dest[8].(*string)) = summary.BindingStatus
	*(dest[9].(*string)) = summary.CurrentHealthStatus
	*(dest[10].(**time.Time)) = cloneTimePtr(summary.LastHeartbeatAt)
	*(dest[11].(**time.Time)) = cloneTimePtr(summary.LastSyncAt)
	*(dest[12].(*int)) = summary.CurrentActiveIncidentCount
	*(dest[13].(*string)) = summary.CurrentPrimaryIssueSummary
	*(dest[14].(*time.Time)) = summary.LinkedAt
	*(dest[15].(*string)) = summary.Note
	*(dest[16].(**time.Time)) = cloneTimePtr(summary.ArchivedAt)
}

type fakeAssetLifecycleDB struct {
	txs            []*fakeAssetLifecycleTx
	queryFunc      func(context.Context, string, ...any) (pgx.Rows, error)
	queryRowFunc   func(context.Context, string, ...any) pgx.Row
	beginCount     int
	beginOptions   []pgx.TxOptions
	beginErr       error
	beginErrSticky bool
}

func (f *fakeAssetLifecycleDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.queryFunc != nil {
		return f.queryFunc(ctx, sql, args...)
	}
	return nil, errors.New("unexpected query on fake asset lifecycle db")
}

func (f *fakeAssetLifecycleDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRowFunc != nil {
		return f.queryRowFunc(ctx, sql, args...)
	}
	return fakeAssetLifecycleRowFunc(func(dest ...any) error {
		return errors.New("unexpected query row on fake asset lifecycle db")
	})
}

func (f *fakeAssetLifecycleDB) BeginTx(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	f.beginOptions = append(f.beginOptions, options)
	if f.beginErr != nil {
		err := f.beginErr
		if !f.beginErrSticky {
			f.beginErr = nil
		}
		return nil, err
	}
	if f.beginCount >= len(f.txs) {
		return nil, errors.New("unexpected BeginTx call")
	}
	tx := f.txs[f.beginCount]
	f.beginCount++
	return tx, nil
}

type fakeAssetLifecycleTx struct {
	queryRowFunc  func(context.Context, string, ...any) pgx.Row
	queryFunc     func(context.Context, string, ...any) (pgx.Rows, error)
	execFunc      func(context.Context, string, ...any) (pgconn.CommandTag, error)
	execCalls     []fakeAssetLifecycleExecCall
	commitCount   int
	rollbackCount int
	commitErr     error
}

type fakeAssetLifecycleExecCall struct {
	sql  string
	args []any
}

func (f *fakeAssetLifecycleTx) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected nested Begin on fake asset lifecycle tx")
}

func (f *fakeAssetLifecycleTx) Commit(context.Context) error {
	f.commitCount++
	if f.commitErr != nil {
		return f.commitErr
	}
	return nil
}

func (f *fakeAssetLifecycleTx) Rollback(context.Context) error {
	f.rollbackCount++
	return nil
}

func (f *fakeAssetLifecycleTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unexpected CopyFrom on fake asset lifecycle tx")
}

func (f *fakeAssetLifecycleTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}

func (f *fakeAssetLifecycleTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (f *fakeAssetLifecycleTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unexpected Prepare on fake asset lifecycle tx")
}

func (f *fakeAssetLifecycleTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execCalls = append(f.execCalls, fakeAssetLifecycleExecCall{sql: sql, args: append([]any(nil), args...)})
	if f.execFunc != nil {
		return f.execFunc(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (f *fakeAssetLifecycleTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.queryFunc != nil {
		return f.queryFunc(ctx, sql, args...)
	}
	return nil, errors.New("unexpected Query on fake asset lifecycle tx")
}

func (f *fakeAssetLifecycleTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRowFunc != nil {
		return f.queryRowFunc(ctx, sql, args...)
	}
	return fakeAssetLifecycleRowFunc(func(dest ...any) error {
		return errors.New("unexpected QueryRow on fake asset lifecycle tx")
	})
}

func (f *fakeAssetLifecycleTx) Conn() *pgx.Conn {
	return nil
}

type fakeAssetLifecycleRowFunc func(dest ...any) error

func (f fakeAssetLifecycleRowFunc) Scan(dest ...any) error {
	return f(dest...)
}
