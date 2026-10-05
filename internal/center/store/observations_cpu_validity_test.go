package store

import (
	"context"
	"math"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/observations"
)

type observationExecCall struct {
	sql  string
	args []any
}

type captureObservationExec struct {
	calls []observationExecCall
}

func (e *captureObservationExec) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	e.calls = append(e.calls, observationExecCall{sql: sql, args: append([]any(nil), args...)})
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func TestRecordObservationBatchNormalizesCPUValidityWithoutChangingValues(t *testing.T) {
	t.Parallel()

	exec := &captureObservationExec{}
	batch := observations.BatchWrite{HostSamples: []observations.HostSampleWrite{
		{CPUUsagePct: 101, CPURatesValid: new(true), CPUIOWaitPct: 2, CPUStealPct: 3},
		{CPUUsagePct: 20, CPUIOWaitPct: 30, CPUStealPct: 40},
		{CPUUsagePct: 0, CPURatesValid: new(false), CPUIOWaitPct: 0, CPUStealPct: 0},
		{CPUUsagePct: math.NaN(), CPUIOWaitPct: 4, CPUStealPct: 5},
	}}

	if err := recordObservationBatch(context.Background(), exec, batch); err != nil {
		t.Fatalf("recordObservationBatch() error = %v", err)
	}
	if len(exec.calls) != len(batch.HostSamples) {
		t.Fatalf("len(exec.calls) = %d, want %d", len(exec.calls), len(batch.HostSamples))
	}

	marker, ok := exec.calls[0].args[6].(*bool)
	if !ok || marker == nil || *marker {
		t.Fatalf("invalid explicit-true marker = %#v, want explicit false", exec.calls[0].args[6])
	}
	if got := exec.calls[0].args[5].(float64); got != 101 {
		t.Fatalf("out-of-range CPU value = %v, want original 101", got)
	}
	marker, ok = exec.calls[1].args[6].(*bool)
	if !ok || marker != nil {
		t.Fatalf("valid legacy marker = %#v, want nil", exec.calls[1].args[6])
	}
	marker, ok = exec.calls[2].args[6].(*bool)
	if !ok || marker == nil || *marker {
		t.Fatalf("explicit-false marker = %#v, want false", exec.calls[2].args[6])
	}
	if !math.IsNaN(exec.calls[3].args[5].(float64)) {
		t.Fatalf("non-finite CPU value was rewritten before persistence: %#v", exec.calls[3].args[5])
	}
	marker, ok = exec.calls[3].args[6].(*bool)
	if !ok || marker == nil || *marker {
		t.Fatalf("non-finite legacy marker = %#v, want explicit false", exec.calls[3].args[6])
	}
}
