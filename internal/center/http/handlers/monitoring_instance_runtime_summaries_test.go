package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/store"
)

type fakeMonitoringInstanceRuntimeSummariesRepository struct {
	result store.MonitoringInstanceRuntimeSummaries
	err    error
}

func (f *fakeMonitoringInstanceRuntimeSummariesRepository) GetMonitoringInstanceRuntimeSummaries(context.Context) (store.MonitoringInstanceRuntimeSummaries, error) {
	if f.err != nil {
		return store.MonitoringInstanceRuntimeSummaries{}, f.err
	}
	return f.result, nil
}

func TestMonitoringInstanceRuntimeSummariesHandlerPreservesNullAndZero(t *testing.T) {
	readAt := time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)
	observedAt := readAt.Add(-time.Minute)
	receivedAt := observedAt.Add(2 * time.Second)
	zero := int64(0)
	handler := handlers.MonitoringInstanceRuntimeSummaries(&fakeMonitoringInstanceRuntimeSummariesRepository{
		result: store.MonitoringInstanceRuntimeSummaries{
			ReadAt: readAt,
			MonitoringInstances: map[string]*store.MonitoringInstanceRuntimeSummary{
				"mi_zero": {
					ObservedAt:        observedAt,
					ReceivedAt:        receivedAt,
					UptimeSeconds:     0,
					NetInBytesPerSec:  &zero,
					NetOutBytesPerSec: &zero,
				},
				"mi_unknown": nil,
			},
		},
	})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/runtime-summaries", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body struct {
		ReadAt              time.Time                                          `json:"read_at"`
		MonitoringInstances map[string]*store.MonitoringInstanceRuntimeSummary `json:"monitoring_instances"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.ReadAt.Equal(readAt) {
		t.Fatalf("read_at = %s, want %s", body.ReadAt, readAt)
	}
	if body.MonitoringInstances["mi_unknown"] != nil {
		t.Fatalf("mi_unknown = %#v, want null", body.MonitoringInstances["mi_unknown"])
	}
	zeroSummary := body.MonitoringInstances["mi_zero"]
	if zeroSummary == nil || zeroSummary.UptimeSeconds != 0 || zeroSummary.NetInBytesPerSec == nil || *zeroSummary.NetInBytesPerSec != 0 || zeroSummary.NetOutBytesPerSec == nil || *zeroSummary.NetOutBytesPerSec != 0 {
		t.Fatalf("mi_zero = %#v, want zero values retained", zeroSummary)
	}
}

func TestMonitoringInstanceRuntimeSummariesHandlerErrors(t *testing.T) {
	tests := []struct {
		name   string
		method string
		err    error
		want   int
	}{
		{name: "wrong method", method: http.MethodPost, want: http.StatusMethodNotAllowed},
		{name: "repository error", method: http.MethodGet, err: errors.New("database unavailable"), want: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := handlers.MonitoringInstanceRuntimeSummaries(&fakeMonitoringInstanceRuntimeSummariesRepository{err: tt.err})
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(tt.method, "/api/monitoring-instances/runtime-summaries", nil))
			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}
