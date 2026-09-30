package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/monitoringinstances"
)

type fakeIPQualityCollectLinks struct {
	summaries []assetlinks.MonitoringInstanceSummary
	err       error
}

func (f fakeIPQualityCollectLinks) ListMonitoringInstancesForVPS(context.Context, string) ([]assetlinks.MonitoringInstanceSummary, error) {
	return f.summaries, f.err
}

type fakeIPQualityCollectSettings struct {
	enabled bool
	err     error
}

func (f fakeIPQualityCollectSettings) IPQualityEnabled(context.Context) (bool, error) {
	return f.enabled, f.err
}

type ipQualityCollectResponse struct {
	Enabled              bool                      `json:"enabled"`
	Available            bool                      `json:"available"`
	UnavailableReason    string                    `json:"unavailable_reason"`
	MonitoringInstanceID string                    `json:"monitoring_instance_id"`
	AgentLastSyncAt      *time.Time                `json:"agent_last_sync_at"`
	Request              *ipquality.CollectRequest `json:"request"`
	Error                string                    `json:"error"`
	Reason               string                    `json:"reason"`
}

func boundCollectInstance(id string) assetlinks.MonitoringInstanceSummary {
	lastSync := time.Date(2026, time.September, 30, 7, 59, 0, 0, time.UTC)
	return assetlinks.MonitoringInstanceSummary{
		MonitoringInstanceID: id,
		IsCurrent:            true,
		LifecycleStatus:      monitoringinstances.LifecycleInUse,
		BindingStatus:        monitoringinstances.BindingBound,
		MonitoringStatus:     monitoringinstances.MonitoringEnabled,
		VPSLifecycleStatus:   "active",
		LastSyncAt:           &lastSync,
	}
}

func newTestCollectRegistry() *ipquality.CollectRequests {
	counter := 0
	return ipquality.NewCollectRequests(func() (string, error) {
		counter++
		return fmt.Sprintf("ipqc_%03d", counter), nil
	})
}

func serveIPQualityCollect(t *testing.T, handler http.Handler, method, path string) (int, ipQualityCollectResponse) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	var body ipQualityCollectResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s %s body %q: %v", method, path, recorder.Body.String(), err)
	}
	return recorder.Code, body
}

func TestVPSIPQualityCollectQueuesAndReportsProgress(t *testing.T) {
	t.Parallel()
	retired := boundCollectInstance("mi_old")
	retired.IsCurrent = false
	retired.LifecycleStatus = monitoringinstances.LifecycleRetired
	registry := newTestCollectRegistry()
	handler := handlers.VPSIPQualityCollect(
		fakeIPQualityCollectLinks{summaries: []assetlinks.MonitoringInstanceSummary{retired, boundCollectInstance("mi_001")}},
		fakeIPQualityCollectSettings{enabled: true},
		registry,
	)

	code, idle := serveIPQualityCollect(t, handler, http.MethodGet, "/api/vps/vps_001/ip-quality/collect")
	if code != http.StatusOK || !idle.Available || idle.Request != nil || idle.MonitoringInstanceID != "mi_001" || idle.AgentLastSyncAt == nil {
		t.Fatalf("GET before request = %d %#v, want available without request", code, idle)
	}

	code, queued := serveIPQualityCollect(t, handler, http.MethodPost, "/api/vps/vps_001/ip-quality/collect")
	if code != http.StatusAccepted || queued.Request == nil || queued.Request.Status != ipquality.CollectRequestPending || queued.Request.MonitoringInstanceID != "mi_001" {
		t.Fatalf("POST = %d %#v, want accepted pending request for current instance", code, queued)
	}
	code, repeated := serveIPQualityCollect(t, handler, http.MethodPost, "/api/vps/vps_001/ip-quality/collect")
	if code != http.StatusAccepted || repeated.Request == nil || repeated.Request.RequestID != queued.Request.RequestID {
		t.Fatalf("repeat POST = %d %#v, want the active request reused", code, repeated)
	}

	registry.PendingRequestID("mi_001", time.Now())
	code, progress := serveIPQualityCollect(t, handler, http.MethodGet, "/api/vps/vps_001/ip-quality/collect")
	if code != http.StatusOK || progress.Request == nil || progress.Request.Status != ipquality.CollectRequestDispatched {
		t.Fatalf("GET after dispatch = %d %#v, want dispatched request", code, progress)
	}
}

func TestVPSIPQualityCollectRejectsUnavailableCollection(t *testing.T) {
	t.Parallel()
	unbound := boundCollectInstance("mi_001")
	unbound.BindingStatus = "pending"
	paused := boundCollectInstance("mi_001")
	paused.MonitoringStatus = monitoringinstances.MonitoringPaused
	archived := boundCollectInstance("mi_001")
	archived.VPSLifecycleStatus = "archived"

	tests := []struct {
		name      string
		enabled   bool
		summaries []assetlinks.MonitoringInstanceSummary
		reason    string
	}{
		{name: "disabled", enabled: false, summaries: []assetlinks.MonitoringInstanceSummary{boundCollectInstance("mi_001")}, reason: "disabled"},
		{name: "no instance", enabled: true, reason: "no_monitoring_instance"},
		{name: "archived vps", enabled: true, summaries: []assetlinks.MonitoringInstanceSummary{archived}, reason: "no_monitoring_instance"},
		{name: "unbound", enabled: true, summaries: []assetlinks.MonitoringInstanceSummary{unbound}, reason: "agent_not_bound"},
		{name: "paused", enabled: true, summaries: []assetlinks.MonitoringInstanceSummary{paused}, reason: "monitoring_paused"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry := newTestCollectRegistry()
			handler := handlers.VPSIPQualityCollect(fakeIPQualityCollectLinks{summaries: tt.summaries}, fakeIPQualityCollectSettings{enabled: tt.enabled}, registry)

			code, status := serveIPQualityCollect(t, handler, http.MethodGet, "/api/vps/vps_001/ip-quality/collect")
			if code != http.StatusOK || status.Available || status.UnavailableReason != tt.reason || status.Enabled != tt.enabled {
				t.Fatalf("GET = %d %#v, want unavailable %s", code, status, tt.reason)
			}
			code, rejected := serveIPQualityCollect(t, handler, http.MethodPost, "/api/vps/vps_001/ip-quality/collect")
			if code != http.StatusConflict || rejected.Reason != tt.reason || rejected.Error == "" {
				t.Fatalf("POST = %d %#v, want 409 %s", code, rejected, tt.reason)
			}
			if _, found := registry.Latest("mi_001", time.Now()); found {
				t.Fatal("rejected request must not be registered")
			}
		})
	}
}

func TestVPSIPQualityCollectHandlesErrorsAndMethods(t *testing.T) {
	t.Parallel()
	registry := newTestCollectRegistry()
	cases := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		want    int
	}{
		{
			name:    "settings error",
			handler: handlers.VPSIPQualityCollect(fakeIPQualityCollectLinks{}, fakeIPQualityCollectSettings{err: errors.New("db down")}, registry),
			method:  http.MethodGet, path: "/api/vps/vps_001/ip-quality/collect", want: http.StatusInternalServerError,
		},
		{
			name:    "links error",
			handler: handlers.VPSIPQualityCollect(fakeIPQualityCollectLinks{err: errors.New("db down")}, fakeIPQualityCollectSettings{enabled: true}, registry),
			method:  http.MethodPost, path: "/api/vps/vps_001/ip-quality/collect", want: http.StatusInternalServerError,
		},
		{
			name:    "method",
			handler: handlers.VPSIPQualityCollect(fakeIPQualityCollectLinks{}, fakeIPQualityCollectSettings{enabled: true}, registry),
			method:  http.MethodDelete, path: "/api/vps/vps_001/ip-quality/collect", want: http.StatusMethodNotAllowed,
		},
		{
			name:    "path",
			handler: handlers.VPSIPQualityCollect(fakeIPQualityCollectLinks{}, fakeIPQualityCollectSettings{enabled: true}, registry),
			method:  http.MethodGet, path: "/api/vps/vps_001/ip-quality", want: http.StatusNotFound,
		},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		tc.handler.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
		if recorder.Code != tc.want {
			t.Fatalf("%s status = %d, want %d", tc.name, recorder.Code, tc.want)
		}
	}
}
