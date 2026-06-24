package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	centerhttp "houfeng/internal/center/http"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/contracts/agentapi"
)

func newTestRouter(opts centerhttp.RouterOptions) http.Handler {
	if opts.AuthMiddleware == nil {
		opts.AuthMiddleware = centerhttp.NoAuthForTestOnly()
	}
	return centerhttp.New(opts)
}

func TestRouterHealthz(t *testing.T) {
	handler := newTestRouter(centerhttp.RouterOptions{Version: "dev"})

	req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Status  string `json:"status"`
	}

	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if body.Name != "houfeng-center" {
		t.Fatalf("expected name %q, got %q", "houfeng-center", body.Name)
	}

	if body.Version != "dev" {
		t.Fatalf("expected version %q, got %q", "dev", body.Version)
	}

	if body.Status != "ok" {
		t.Fatalf("expected status %q, got %q", "ok", body.Status)
	}
}

func TestRouterPrefersMonitoringInstancesAPIOverSPAFallback(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(indexPath, []byte("<html><body>spa</body></html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	now := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	repo := &fakeMonitoringInstanceRepository{
		listMonitoringInstancesResult: []monitoringinstances.Record{{
			MonitoringInstanceID: "mi_001",
			DisplayName:          "Tokyo Edge",
			Region:               "ap-northeast-1",
			City:                 "Tokyo",
			Provider:             "Vultr",
			LifecycleStatus:      monitoringinstances.LifecyclePendingEnrollment,
			MonitoringStatus:     monitoringinstances.MonitoringEnabled,
			BindingStatus:        monitoringinstances.BindingUnbound,
			CurrentHealthStatus:  monitoringinstances.HealthNormal,
			CreatedAt:            now,
			UpdatedAt:            now,
		}},
	}

	handler := newTestRouter(centerhttp.RouterOptions{
		Version:                              "dev",
		WebDistDir:                           dir,
		MonitoringInstancesCollectionHandler: handlers.MonitoringInstancesCollection(repo),
		MonitoringInstanceItemHandler:        handlers.MonitoringInstanceItem(repo),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body []monitoringinstances.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if len(body) != 1 || body[0].MonitoringInstanceID != "mi_001" {
		t.Fatalf("expected monitoringInstances API response, got %#v", body)
	}
}

func TestRouterDispatchesTargetProbeItemsAPI(t *testing.T) {
	var called string
	handler := newTestRouter(centerhttp.RouterOptions{
		Version: "dev",
		TargetItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "item"
			w.WriteHeader(http.StatusOK)
		}),
		TargetProbeItemsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "probe-items"
			w.WriteHeader(http.StatusCreated)
		}),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/targets/tg_001/probe-items", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, recorder.Code)
	}
	if called != "probe-items" {
		t.Fatalf("expected probe items handler, got %q", called)
	}
}

func TestRouterDispatchesProviderAPIs(t *testing.T) {
	var called string
	handler := newTestRouter(centerhttp.RouterOptions{
		Version: "dev",
		ProvidersCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "collection"
			w.WriteHeader(http.StatusOK)
		}),
		ProviderItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "item"
			w.WriteHeader(http.StatusOK)
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("collection status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if called != "collection" {
		t.Fatalf("called = %q, want collection", called)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/providers/pv_001", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("item status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if called != "item" {
		t.Fatalf("called = %q, want item", called)
	}
}

func TestRouterProtectsProviderRoutes(t *testing.T) {
	collectionCalled := false
	itemCalled := false
	middlewareCalls := 0
	handler := newTestRouter(centerhttp.RouterOptions{
		Version: "dev",
		ProvidersCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			collectionCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		ProviderItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			itemCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		AuthMiddleware: func(_ http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				middlewareCalls++
				w.WriteHeader(http.StatusUnauthorized)
			})
		},
	})

	for _, path := range []string{"/api/providers", "/api/providers/pv_001"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusUnauthorized)
		}
	}
	if collectionCalled {
		t.Fatal("provider collection handler was called despite auth middleware blocking")
	}
	if itemCalled {
		t.Fatal("provider item handler was called despite auth middleware blocking")
	}
	if middlewareCalls != 2 {
		t.Fatalf("middleware calls = %d, want 2", middlewareCalls)
	}
}

func TestRouterProtectedRouteFailsClosedWithoutAuthMiddleware(t *testing.T) {
	called := false
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version: "dev",
		DashboardHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if called {
		t.Fatal("protected handler was called without auth middleware")
	}
}

func TestRouterAllowsExplicitNoAuthForTests(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version: "dev",
		DashboardHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		AuthMiddleware: centerhttp.NoAuthForTestOnly(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

func TestRouterDispatchesVPSAPIs(t *testing.T) {
	var called string
	handler := newTestRouter(centerhttp.RouterOptions{
		Version: "dev",
		VPSCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "collection"
			w.WriteHeader(http.StatusOK)
		}),
		VPSItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "item"
			w.WriteHeader(http.StatusOK)
		}),
		VPSMonitoringInstancesHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "monitoring-instances"
			w.WriteHeader(http.StatusOK)
		}),
		VPSSubscriptionsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "subscriptions"
			w.WriteHeader(http.StatusCreated)
		}),
		VPSLinkMonitoringInstanceHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "link-monitoring-instance"
			w.WriteHeader(http.StatusCreated)
		}),
		VPSUnlinkMonitoringInstanceHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "unlink-monitoring-instance"
			w.WriteHeader(http.StatusOK)
		}),
		VPSIPQualityHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "ip-quality"
			w.WriteHeader(http.StatusOK)
		}),
		VPSCancellationPreviewHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "cancellation-preview"
			w.WriteHeader(http.StatusOK)
		}),
		VPSCancellationHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "cancellation"
			w.WriteHeader(http.StatusOK)
		}),
		VPSExtendValidityHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "extend-validity"
			w.WriteHeader(http.StatusOK)
		}),
		VPSArchiveReviewHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "archive-review"
			w.WriteHeader(http.StatusOK)
		}),
		VPSArchiveHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "archive"
			w.WriteHeader(http.StatusOK)
		}),
		VPSRestoreFromArchiveHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "restore-from-archive"
			w.WriteHeader(http.StatusOK)
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/vps", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("collection status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if called != "collection" {
		t.Fatalf("called = %q, want collection", called)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/vps/vps_001", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("item status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if called != "item" {
		t.Fatalf("called = %q, want item", called)
	}

	for _, tt := range []struct {
		method string
		path   string
		want   int
		called string
	}{
		{method: http.MethodGet, path: "/api/vps/vps_001/monitoring-instances", want: http.StatusOK, called: "monitoring-instances"},
		{method: http.MethodPost, path: "/api/vps/vps_001/subscriptions", want: http.StatusCreated, called: "subscriptions"},
		{method: http.MethodPost, path: "/api/vps/vps_001/link-monitoring-instance", want: http.StatusCreated, called: "link-monitoring-instance"},
		{method: http.MethodPost, path: "/api/vps/vps_001/unlink-monitoring-instance", want: http.StatusOK, called: "unlink-monitoring-instance"},
		{method: http.MethodGet, path: "/api/vps/vps_001/ip-quality", want: http.StatusOK, called: "ip-quality"},
		{method: http.MethodGet, path: "/api/vps/vps_001/ip-quality/reports/ipq_001", want: http.StatusOK, called: "ip-quality"},
		{method: http.MethodGet, path: "/api/vps/vps_001/cancellation-preview", want: http.StatusOK, called: "cancellation-preview"},
		{method: http.MethodPost, path: "/api/vps/vps_001/cancellation", want: http.StatusOK, called: "cancellation"},
		{method: http.MethodPost, path: "/api/vps/vps_001/extend-validity", want: http.StatusOK, called: "extend-validity"},
		{method: http.MethodGet, path: "/api/vps/vps_001/archive-review", want: http.StatusOK, called: "archive-review"},
		{method: http.MethodPost, path: "/api/vps/vps_001/archive", want: http.StatusOK, called: "archive"},
		{method: http.MethodPost, path: "/api/vps/vps_001/restore-from-archive", want: http.StatusOK, called: "restore-from-archive"},
	} {
		req = httptest.NewRequest(tt.method, tt.path, nil)
		recorder = httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != tt.want {
			t.Fatalf("%s status = %d, want %d", tt.path, recorder.Code, tt.want)
		}
		if called != tt.called {
			t.Fatalf("%s called = %q, want %q", tt.path, called, tt.called)
		}
	}
}

func TestRouterProtectsVPSRoutes(t *testing.T) {
	collectionCalled := false
	itemCalled := false
	subscriptionsCalled := false
	cancellationPreviewCalled := false
	cancellationCalled := false
	extendValidityCalled := false
	archiveReviewCalled := false
	archiveCalled := false
	restoreFromArchiveCalled := false
	middlewareCalls := 0
	handler := newTestRouter(centerhttp.RouterOptions{
		Version: "dev",
		VPSCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			collectionCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		VPSItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			itemCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		VPSSubscriptionsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subscriptionsCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		VPSCancellationPreviewHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cancellationPreviewCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		VPSCancellationHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cancellationCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		VPSExtendValidityHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			extendValidityCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		VPSArchiveReviewHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			archiveReviewCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		VPSArchiveHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			archiveCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		VPSRestoreFromArchiveHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			restoreFromArchiveCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		AuthMiddleware: func(_ http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				middlewareCalls++
				w.WriteHeader(http.StatusUnauthorized)
			})
		},
	})

	for _, path := range []string{"/api/vps", "/api/vps/vps_001", "/api/vps/vps_001/monitoring-instances", "/api/vps/vps_001/subscriptions", "/api/vps/vps_001/link-monitoring-instance", "/api/vps/vps_001/unlink-monitoring-instance", "/api/vps/vps_001/cancellation-preview", "/api/vps/vps_001/cancellation", "/api/vps/vps_001/extend-validity", "/api/vps/vps_001/archive-review", "/api/vps/vps_001/archive", "/api/vps/vps_001/restore-from-archive"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusUnauthorized)
		}
	}
	if collectionCalled {
		t.Fatal("vps collection handler was called despite auth middleware blocking")
	}
	if itemCalled {
		t.Fatal("vps item handler was called despite auth middleware blocking")
	}
	if subscriptionsCalled {
		t.Fatal("vps subscriptions handler was called despite auth middleware blocking")
	}
	if cancellationPreviewCalled {
		t.Fatal("vps cancellation preview handler was called despite auth middleware blocking")
	}
	if cancellationCalled {
		t.Fatal("vps cancellation handler was called despite auth middleware blocking")
	}
	if extendValidityCalled {
		t.Fatal("vps extend validity handler was called despite auth middleware blocking")
	}
	if archiveReviewCalled {
		t.Fatal("vps archive review handler was called despite auth middleware blocking")
	}
	if archiveCalled {
		t.Fatal("vps archive handler was called despite auth middleware blocking")
	}
	if restoreFromArchiveCalled {
		t.Fatal("vps restore from archive handler was called despite auth middleware blocking")
	}
	if middlewareCalls != 12 {
		t.Fatalf("middleware calls = %d, want 12", middlewareCalls)
	}
}

func TestRouterDispatchesTargetAssetContextAPI(t *testing.T) {
	var called string
	handler := newTestRouter(centerhttp.RouterOptions{
		Version: "dev",
		AssetContextTargetsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "targets"
			w.WriteHeader(http.StatusOK)
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/asset-context/targets", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("target context status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if called != "targets" {
		t.Fatalf("called = %q, want targets", called)
	}
}

func TestRouterDispatchesSubscriptionAPIs(t *testing.T) {
	var called string
	handler := newTestRouter(centerhttp.RouterOptions{
		Version: "dev",
		SubscriptionsCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "collection"
			w.WriteHeader(http.StatusOK)
		}),
		SubscriptionItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "item"
			w.WriteHeader(http.StatusOK)
		}),
		SubscriptionMonthlyBudgetsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "monthly-budgets"
			w.WriteHeader(http.StatusOK)
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("collection status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if called != "collection" {
		t.Fatalf("called = %q, want collection", called)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/subscriptions/sub_001", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("item status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if called != "item" {
		t.Fatalf("called = %q, want item", called)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/subscription-monthly-budgets", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("monthly budgets status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if called != "monthly-budgets" {
		t.Fatalf("called = %q, want monthly-budgets", called)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/subscription-monthly-budgets/2026-06", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("monthly budget item status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if called != "monthly-budgets" {
		t.Fatalf("called = %q, want monthly-budgets", called)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/subscription-monthly-budgets/bulk", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("monthly budget bulk status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if called != "monthly-budgets" {
		t.Fatalf("called = %q, want monthly-budgets", called)
	}
}

func TestRouterDispatchesMonitoringInstanceVPSAPI(t *testing.T) {
	var called string
	handler := newTestRouter(centerhttp.RouterOptions{
		Version: "dev",
		MonitoringInstanceItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "item"
			w.WriteHeader(http.StatusOK)
		}),
		MonitoringInstanceVPSHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "vps"
			w.WriteHeader(http.StatusOK)
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/vps", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if called != "vps" {
		t.Fatalf("expected monitoringInstance vps handler, got %q", called)
	}
}

func TestRouterDispatchesMonitoringInstanceManagementAPIs(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/api/monitoring-instances/mi_001/management-review", want: "management-review"},
		{path: "/api/monitoring-instances/mi_001/lifecycle/retire", want: "lifecycle-retire"},
		{path: "/api/monitoring-instances/mi_001/lifecycle/restore", want: "lifecycle-restore"},
		{path: "/api/monitoring-instances/mi_001/archive", want: "archive"},
		{path: "/api/monitoring-instances/mi_001/restore-from-archive", want: "restore-from-archive"},
		{path: "/api/monitoring-instances/mi_001/permanent-cleanup", want: "permanent-cleanup"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			var called string
			handler := newTestRouter(centerhttp.RouterOptions{
				Version: "dev",
				MonitoringInstanceItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = "item"
					w.WriteHeader(http.StatusOK)
				}),
				MonitoringInstanceManagementReviewHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = "management-review"
					w.WriteHeader(http.StatusOK)
				}),
				MonitoringInstanceLifecycleRetireHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = "lifecycle-retire"
					w.WriteHeader(http.StatusOK)
				}),
				MonitoringInstanceLifecycleRestoreHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = "lifecycle-restore"
					w.WriteHeader(http.StatusOK)
				}),
				MonitoringInstanceArchiveHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = "archive"
					w.WriteHeader(http.StatusOK)
				}),
				MonitoringInstanceRestoreFromArchiveHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = "restore-from-archive"
					w.WriteHeader(http.StatusOK)
				}),
				MonitoringInstancePermanentCleanupHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = "permanent-cleanup"
					w.WriteHeader(http.StatusOK)
				}),
			})

			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			if strings.HasSuffix(tt.path, "/management-review") {
				req = httptest.NewRequest(http.MethodGet, tt.path, nil)
			}
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
			if called != tt.want {
				t.Fatalf("called = %q, want %q", called, tt.want)
			}
		})
	}
}

func TestRouterProtectsSubscriptionRoutes(t *testing.T) {
	collectionCalled := false
	itemCalled := false
	monthlyBudgetsCalled := false
	middlewareCalls := 0
	handler := newTestRouter(centerhttp.RouterOptions{
		Version: "dev",
		SubscriptionsCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			collectionCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		SubscriptionItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			itemCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		SubscriptionMonthlyBudgetsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			monthlyBudgetsCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		AuthMiddleware: func(_ http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				middlewareCalls++
				w.WriteHeader(http.StatusUnauthorized)
			})
		},
	})

	for _, path := range []string{"/api/subscriptions", "/api/subscriptions/sub_001", "/api/subscription-monthly-budgets", "/api/subscription-monthly-budgets/2026-06", "/api/subscription-monthly-budgets/bulk"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusUnauthorized)
		}
	}
	if collectionCalled {
		t.Fatal("subscription collection handler was called despite auth middleware blocking")
	}
	if itemCalled {
		t.Fatal("subscription item handler was called despite auth middleware blocking")
	}
	if monthlyBudgetsCalled {
		t.Fatal("subscription monthly budgets handler was called despite auth middleware blocking")
	}
	if middlewareCalls != 5 {
		t.Fatalf("middleware calls = %d, want 5", middlewareCalls)
	}
}

type fakeMonitoringInstanceRepository struct {
	listMonitoringInstancesResult  []monitoringinstances.Record
	getMonitoringInstanceResult    monitoringinstances.Record
	createMonitoringInstanceResult monitoringinstances.Record
}

func (f *fakeMonitoringInstanceRepository) ListMonitoringInstances(context.Context, ...monitoringinstances.ListScope) ([]monitoringinstances.Record, error) {
	return f.listMonitoringInstancesResult, nil
}

func (f *fakeMonitoringInstanceRepository) GetMonitoringInstance(context.Context, string) (monitoringinstances.Record, error) {
	return f.getMonitoringInstanceResult, nil
}

func (f *fakeMonitoringInstanceRepository) CreateMonitoringInstance(context.Context, monitoringinstances.CreateInput) (monitoringinstances.Record, error) {
	return f.createMonitoringInstanceResult, nil
}

func (f *fakeMonitoringInstanceRepository) UpdateMonitoringInstanceMetadata(context.Context, string, monitoringinstances.UpdateMetadataInput) (monitoringinstances.Record, error) {
	return monitoringinstances.Record{}, nil
}

func (f *fakeMonitoringInstanceRepository) SetPendingAction(context.Context, string, string, string) error {
	return nil
}

func (f *fakeMonitoringInstanceRepository) GetPendingAction(context.Context, string) (string, string, error) {
	return "", "", nil
}

func (f *fakeMonitoringInstanceRepository) ClearPendingAction(context.Context, string) error {
	return nil
}

func (f *fakeMonitoringInstanceRepository) StoreActionResult(context.Context, string, []byte) error {
	return nil
}

func TestRouterDispatchesAgentEndpointsBeforeSPAFallback(t *testing.T) {
	t.Run("enroll", func(t *testing.T) {
		var called bool
		handler := newTestRouter(centerhttp.RouterOptions{
			Version:    "dev",
			WebDistDir: "testdata/web",
			AgentEnrollHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"accepted"}`))
			}),
		})

		req := httptest.NewRequest(http.MethodPost, "/api/agent/enroll", nil)
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
		}
		if !called {
			t.Fatal("agent enroll handler was not called")
		}
		if strings.TrimSpace(recorder.Body.String()) == spaShell {
			t.Fatalf("expected agent API response, got SPA fallback body %q", recorder.Body.String())
		}
	})

	t.Run("sync", func(t *testing.T) {
		var called bool
		handler := newTestRouter(centerhttp.RouterOptions{
			Version:    "dev",
			WebDistDir: "testdata/web",
			AgentSyncHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"accepted"}`))
			}),
		})

		req := httptest.NewRequest(http.MethodPost, "/api/agent/sync", nil)
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
		}
		if !called {
			t.Fatal("agent sync handler was not called")
		}
		if strings.TrimSpace(recorder.Body.String()) == spaShell {
			t.Fatalf("expected agent API response, got SPA fallback body %q", recorder.Body.String())
		}
	})
}

func TestRouterKeepsProbeItemsSubtreeOutOfTargetItemHandler(t *testing.T) {
	var called string
	handler := newTestRouter(centerhttp.RouterOptions{
		Version: "dev",
		TargetItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "item"
			w.WriteHeader(http.StatusOK)
		}),
		TargetProbeItemsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "probe-items"
			w.WriteHeader(http.StatusNotFound)
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/targets/tg_001/probe-items/pb_001", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
	if called != "probe-items" {
		t.Fatalf("expected probe items subtree handler, got %q", called)
	}
}

func TestRouterDispatchesMonitoringInstanceRuntimeFactsAPI(t *testing.T) {
	var called string
	handler := newTestRouter(centerhttp.RouterOptions{
		Version: "dev",
		MonitoringInstanceItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "item"
			w.WriteHeader(http.StatusOK)
		}),
		MonitoringInstanceRuntimeFactsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "runtime-facts"
			w.WriteHeader(http.StatusOK)
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/runtime-facts", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if called != "runtime-facts" {
		t.Fatalf("expected runtime facts handler, got %q", called)
	}
}

func TestRouterDispatchesMonitoringInstanceRuntimeStreamAPI(t *testing.T) {
	var called string
	handler := newTestRouter(centerhttp.RouterOptions{
		Version: "dev",
		MonitoringInstanceRuntimeFactsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "runtime-facts"
			w.WriteHeader(http.StatusOK)
		}),
		MonitoringInstanceRuntimeStreamHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "runtime-stream"
			w.WriteHeader(http.StatusOK)
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/runtime-stream", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if called != "runtime-stream" {
		t.Fatalf("expected runtime-stream handler, got %q", called)
	}
}

func TestRouterDispatchesTargetRuntimeFactsAPI(t *testing.T) {
	var called string
	handler := newTestRouter(centerhttp.RouterOptions{
		Version: "dev",
		TargetItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "item"
			w.WriteHeader(http.StatusOK)
		}),
		TargetProbeItemsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "probe-items"
			w.WriteHeader(http.StatusOK)
		}),
		TargetRuntimeFactsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "runtime-facts"
			w.WriteHeader(http.StatusOK)
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/targets/tg_001/runtime-facts", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if called != "runtime-facts" {
		t.Fatalf("expected runtime facts handler, got %q", called)
	}
}

func TestRouterRegistersAuthRoutesPublic(t *testing.T) {
	loginCalled := false
	logoutCalled := false
	meCalled := false
	pwCalled := false
	opts := centerhttp.RouterOptions{
		Version: "test",
		AuthLoginHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			loginCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		AuthLogoutHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			logoutCalled = true
			w.WriteHeader(http.StatusNoContent)
		}),
		AuthMeHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			meCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		AuthChangePasswordHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			pwCalled = true
			w.WriteHeader(http.StatusNoContent)
		}),
		AuthMiddleware: func(_ http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			})
		},
	}
	mux := centerhttp.New(opts)

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodPost, "/api/auth/login"},
		{http.MethodPost, "/api/auth/logout"},
		{http.MethodGet, "/api/auth/me"},
		{http.MethodPut, "/api/auth/password"},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s -> 404, want registered", tc.method, tc.path)
		}
		if w.Code == http.StatusUnauthorized {
			t.Errorf("%s %s -> 401, want public (auth routes must skip middleware)", tc.method, tc.path)
		}
	}
	if !loginCalled || !logoutCalled || !meCalled || !pwCalled {
		t.Fatalf("auth handlers not all reached: login=%v logout=%v me=%v pw=%v",
			loginCalled, logoutCalled, meCalled, pwCalled)
	}
}

func TestRouterAppliesAuthMiddlewareToProtectedRoutes(t *testing.T) {
	dashboardInnerCalled := false
	providersInnerCalled := false
	providerItemInnerCalled := false
	vpsInnerCalled := false
	vpsItemInnerCalled := false
	vpsMonitoringInstancesInnerCalled := false
	vpsSubscriptionsInnerCalled := false
	monitoringInstanceVPSInnerCalled := false
	subscriptionsInnerCalled := false
	subscriptionItemInnerCalled := false
	mwCalled := false
	opts := centerhttp.RouterOptions{
		Version: "test",
		DashboardHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			dashboardInnerCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		ProvidersCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			providersInnerCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		ProviderItemHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			providerItemInnerCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		VPSCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			vpsInnerCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		VPSItemHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			vpsItemInnerCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		VPSMonitoringInstancesHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			vpsMonitoringInstancesInnerCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		VPSSubscriptionsHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			vpsSubscriptionsInnerCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		MonitoringInstanceVPSHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			monitoringInstanceVPSInnerCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		SubscriptionsCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			subscriptionsInnerCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		SubscriptionItemHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			subscriptionItemInnerCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		AuthMiddleware: func(_ http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				mwCalled = true
				w.WriteHeader(http.StatusUnauthorized)
			})
		},
	}
	mux := centerhttp.New(opts)
	r := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (middleware should block)", w.Code)
	}
	if dashboardInnerCalled {
		t.Fatal("inner dashboard handler must not be called when middleware blocks")
	}
	if !mwCalled {
		t.Fatal("middleware not invoked")
	}

	mwCalled = false
	r = httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("provider status = %d, want 401 (middleware should block)", w.Code)
	}
	if providersInnerCalled {
		t.Fatal("inner provider handler must not be called when middleware blocks")
	}
	if !mwCalled {
		t.Fatal("middleware not invoked for provider route")
	}

	mwCalled = false
	r = httptest.NewRequest(http.MethodGet, "/api/providers/pv_001", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("provider item status = %d, want 401 (middleware should block)", w.Code)
	}
	if providerItemInnerCalled {
		t.Fatal("inner provider item handler must not be called when middleware blocks")
	}
	if !mwCalled {
		t.Fatal("middleware not invoked for provider item route")
	}

	mwCalled = false
	r = httptest.NewRequest(http.MethodGet, "/api/vps", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("vps status = %d, want 401 (middleware should block)", w.Code)
	}
	if vpsInnerCalled {
		t.Fatal("inner vps handler must not be called when middleware blocks")
	}
	if !mwCalled {
		t.Fatal("middleware not invoked for vps route")
	}

	mwCalled = false
	r = httptest.NewRequest(http.MethodGet, "/api/vps/vps_001", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("vps item status = %d, want 401 (middleware should block)", w.Code)
	}
	if vpsItemInnerCalled {
		t.Fatal("inner vps item handler must not be called when middleware blocks")
	}
	if !mwCalled {
		t.Fatal("middleware not invoked for vps item route")
	}

	mwCalled = false
	r = httptest.NewRequest(http.MethodGet, "/api/vps/vps_001/monitoring-instances", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("vps monitoringInstances status = %d, want 401 (middleware should block)", w.Code)
	}
	if vpsMonitoringInstancesInnerCalled {
		t.Fatal("inner vps monitoringInstances handler must not be called when middleware blocks")
	}
	if !mwCalled {
		t.Fatal("middleware not invoked for vps monitoringInstances route")
	}

	mwCalled = false
	r = httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/subscriptions", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("vps subscriptions status = %d, want 401 (middleware should block)", w.Code)
	}
	if vpsSubscriptionsInnerCalled {
		t.Fatal("inner vps subscriptions handler must not be called when middleware blocks")
	}
	if !mwCalled {
		t.Fatal("middleware not invoked for vps subscriptions route")
	}

	mwCalled = false
	r = httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/vps", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("monitoringInstance vps status = %d, want 401 (middleware should block)", w.Code)
	}
	if monitoringInstanceVPSInnerCalled {
		t.Fatal("inner monitoringInstance vps handler must not be called when middleware blocks")
	}
	if !mwCalled {
		t.Fatal("middleware not invoked for monitoringInstance vps route")
	}

	mwCalled = false
	r = httptest.NewRequest(http.MethodGet, "/api/subscriptions", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("subscriptions status = %d, want 401 (middleware should block)", w.Code)
	}
	if subscriptionsInnerCalled {
		t.Fatal("inner subscriptions handler must not be called when middleware blocks")
	}
	if !mwCalled {
		t.Fatal("middleware not invoked for subscriptions route")
	}

	mwCalled = false
	r = httptest.NewRequest(http.MethodGet, "/api/subscriptions/sub_001", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("subscription item status = %d, want 401 (middleware should block)", w.Code)
	}
	if subscriptionItemInnerCalled {
		t.Fatal("inner subscription item handler must not be called when middleware blocks")
	}
	if !mwCalled {
		t.Fatal("middleware not invoked for subscription item route")
	}
}

func TestRouterHealthzAndAgentRoutesBypassAuthMiddleware(t *testing.T) {
	enrollCalled := false
	syncCalled := false
	opts := centerhttp.RouterOptions{
		Version: "test",
		AgentEnrollHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			enrollCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		AgentSyncHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			syncCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		AuthMiddleware: func(_ http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			})
		},
	}
	mux := centerhttp.New(opts)

	r := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code == http.StatusUnauthorized {
		t.Fatalf("healthz blocked by middleware, want bypass")
	}

	r = httptest.NewRequest(http.MethodPost, agentapi.EnrollPath, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code == http.StatusUnauthorized {
		t.Fatalf("agent enroll blocked by middleware, want bypass")
	}
	if !enrollCalled {
		t.Fatal("enroll handler not reached")
	}

	r = httptest.NewRequest(http.MethodPost, agentapi.SyncPath, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code == http.StatusUnauthorized {
		t.Fatalf("agent sync blocked by middleware, want bypass")
	}
	if !syncCalled {
		t.Fatal("sync handler not reached")
	}
}
