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
	"houfeng/internal/center/nodes"
	"houfeng/internal/contracts/agentapi"
)

func TestRouterHealthz(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{Version: "dev"})

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

func TestRouterPrefersNodesAPIOverSPAFallback(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(indexPath, []byte("<html><body>spa</body></html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	now := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	repo := &fakeNodeRepository{
		listNodesResult: []nodes.Record{{
			NodeID:              "nd_001",
			DisplayName:         "Tokyo Edge",
			Region:              "ap-northeast-1",
			City:                "Tokyo",
			Provider:            "Vultr",
			LifecycleStatus:     nodes.LifecyclePendingEnrollment,
			MonitoringStatus:    nodes.MonitoringEnabled,
			BindingStatus:       nodes.BindingUnbound,
			CurrentHealthStatus: nodes.HealthNormal,
			CreatedAt:           now,
			UpdatedAt:           now,
		}},
	}

	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:                "dev",
		WebDistDir:             dir,
		NodesCollectionHandler: handlers.NodesCollection(repo),
		NodeItemHandler:        handlers.NodeItem(repo),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body []nodes.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if len(body) != 1 || body[0].NodeID != "nd_001" {
		t.Fatalf("expected nodes API response, got %#v", body)
	}
}

func TestRouterDispatchesTargetProbeItemsAPI(t *testing.T) {
	var called string
	handler := centerhttp.New(centerhttp.RouterOptions{
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
	handler := centerhttp.New(centerhttp.RouterOptions{
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
	handler := centerhttp.New(centerhttp.RouterOptions{
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

type fakeNodeRepository struct {
	listNodesResult  []nodes.Record
	getNodeResult    nodes.Record
	createNodeResult nodes.Record
}

func (f *fakeNodeRepository) ListNodes(context.Context) ([]nodes.Record, error) {
	return f.listNodesResult, nil
}

func (f *fakeNodeRepository) GetNode(context.Context, string) (nodes.Record, error) {
	return f.getNodeResult, nil
}

func (f *fakeNodeRepository) CreateNode(context.Context, nodes.CreateInput) (nodes.Record, error) {
	return f.createNodeResult, nil
}

func (f *fakeNodeRepository) UpdateNodeMetadata(context.Context, string, nodes.UpdateMetadataInput) (nodes.Record, error) {
	return nodes.Record{}, nil
}

func (f *fakeNodeRepository) SetPendingAction(context.Context, string, string, string) error {
	return nil
}

func (f *fakeNodeRepository) GetPendingAction(context.Context, string) (string, string, error) {
	return "", "", nil
}

func (f *fakeNodeRepository) ClearPendingAction(context.Context, string) error {
	return nil
}

func (f *fakeNodeRepository) StoreActionResult(context.Context, string, []byte) error {
	return nil
}

func TestRouterDispatchesAgentEndpointsBeforeSPAFallback(t *testing.T) {
	t.Run("enroll", func(t *testing.T) {
		var called bool
		handler := centerhttp.New(centerhttp.RouterOptions{
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
		handler := centerhttp.New(centerhttp.RouterOptions{
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
	handler := centerhttp.New(centerhttp.RouterOptions{
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

func TestRouterDispatchesNodeRuntimeFactsAPI(t *testing.T) {
	var called string
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version: "dev",
		NodeItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "item"
			w.WriteHeader(http.StatusOK)
		}),
		NodeRuntimeFactsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "runtime-facts"
			w.WriteHeader(http.StatusOK)
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/nodes/nd_001/runtime-facts", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if called != "runtime-facts" {
		t.Fatalf("expected runtime facts handler, got %q", called)
	}
}

func TestRouterDispatchesTargetRuntimeFactsAPI(t *testing.T) {
	var called string
	handler := centerhttp.New(centerhttp.RouterOptions{
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
