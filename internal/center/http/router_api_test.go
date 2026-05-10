package http_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	centerhttp "houfeng/internal/center/http"
)

const spaShell = "<!doctype html><title>houfeng-spa</title>"

func TestRouterKeepsAPINodesOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		NodesCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"node_id":"nd_001"}]`))
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if strings.TrimSpace(string(body)) == spaShell {
		t.Fatalf("expected API response, got SPA fallback body %q", string(body))
	}

	if !strings.Contains(string(body), `"node_id":"nd_001"`) {
		t.Fatalf("expected node payload, got %q", string(body))
	}
}

func TestRouterKeepsSettingsOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		SettingsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"host_sample_frequency_tier":"5m"}`))
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if strings.TrimSpace(string(body)) == spaShell {
		t.Fatalf("expected API response, got SPA fallback body %q", string(body))
	}

	if !strings.Contains(string(body), `"host_sample_frequency_tier":"5m"`) {
		t.Fatalf("expected settings payload, got %q", string(body))
	}
}

func TestRouterKeepsProvidersOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		ProvidersCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"provider_id":"pv_001"}]`))
		}),
		ProviderItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"provider_id":"pv_001"}`))
		}),
	})

	tests := []struct {
		name            string
		path            string
		wantBodySnippet string
	}{
		{name: "collection", path: "/api/providers", wantBodySnippet: `"provider_id":"pv_001"`},
		{name: "item", path: "/api/providers/pv_001", wantBodySnippet: `"provider_id":"pv_001"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
			}
			body, err := io.ReadAll(recorder.Body)
			if err != nil {
				t.Fatalf("read response body: %v", err)
			}
			if strings.TrimSpace(string(body)) == spaShell {
				t.Fatalf("expected provider API response, got SPA fallback body %q", string(body))
			}
			if !strings.Contains(string(body), tt.wantBodySnippet) {
				t.Fatalf("expected provider payload, got %q", string(body))
			}
		})
	}
}

func TestRouterKeepsAssetServicesOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		AssetServicesCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"service_id":"svc_001"}]`))
		}),
		VPSServicesHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"service_id":"svc_vps_001"}]`))
		}),
	})

	tests := []struct {
		name            string
		path            string
		wantBodySnippet string
	}{
		{name: "collection", path: "/api/services", wantBodySnippet: `"service_id":"svc_001"`},
		{name: "vps services", path: "/api/vps/vps_001/services", wantBodySnippet: `"service_id":"svc_vps_001"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
			}
			body, err := io.ReadAll(recorder.Body)
			if err != nil {
				t.Fatalf("read response body: %v", err)
			}
			if strings.TrimSpace(string(body)) == spaShell {
				t.Fatalf("expected asset service API response, got SPA fallback body %q", string(body))
			}
			if !strings.Contains(string(body), tt.wantBodySnippet) {
				t.Fatalf("expected asset service payload, got %q", string(body))
			}
		})
	}
}

func TestRouterKeepsVPSOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		VPSCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"vps_id":"vps_001"}]`))
		}),
		VPSItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"vps_id":"vps_001"}`))
		}),
		VPSNodesHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"node_id":"nd_001"}]`))
		}),
		VPSLinkNodeHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"link_id":"vnl_001"}`))
		}),
		VPSUnlinkNodeHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"link_id":"vnl_001"}`))
		}),
		VPSTimelineHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"vps_id":"vps_001","renewal_decisions":[{"decision_id":"rdec_001"}],"price_histories":[{"price_history_id":"ph_001"}],"ip_histories":[{"ip_history_id":"iph_001"}],"spec_snapshots":[{"snapshot_id":"vss_001"}],"experience_logs":[{"experience_log_id":"elog_001"}]}`))
		}),
		VPSExperienceLogsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"experience_log_id":"elog_001"}]`))
		}),
	})

	tests := []struct {
		name            string
		path            string
		wantStatus      int
		wantBodySnippet string
	}{
		{name: "collection", path: "/api/vps", wantStatus: http.StatusOK, wantBodySnippet: `"vps_id":"vps_001"`},
		{name: "item", path: "/api/vps/vps_001", wantStatus: http.StatusOK, wantBodySnippet: `"vps_id":"vps_001"`},
		{name: "nodes", path: "/api/vps/vps_001/nodes", wantStatus: http.StatusOK, wantBodySnippet: `"node_id":"nd_001"`},
		{name: "link node", path: "/api/vps/vps_001/link-node", wantStatus: http.StatusCreated, wantBodySnippet: `"link_id":"vnl_001"`},
		{name: "unlink node", path: "/api/vps/vps_001/unlink-node", wantStatus: http.StatusOK, wantBodySnippet: `"link_id":"vnl_001"`},
		{name: "timeline", path: "/api/vps/vps_001/timeline", wantStatus: http.StatusOK, wantBodySnippet: `"price_history_id":"ph_001"`},
		{name: "experience logs", path: "/api/vps/vps_001/experience-logs", wantStatus: http.StatusOK, wantBodySnippet: `"experience_log_id":"elog_001"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, recorder.Code)
			}
			body, err := io.ReadAll(recorder.Body)
			if err != nil {
				t.Fatalf("read response body: %v", err)
			}
			if strings.TrimSpace(string(body)) == spaShell {
				t.Fatalf("expected vps API response, got SPA fallback body %q", string(body))
			}
			if !strings.Contains(string(body), tt.wantBodySnippet) {
				t.Fatalf("expected vps payload, got %q", string(body))
			}
		})
	}
}

func TestRouterKeepsNodeVPSOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		NodeVPSHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"vps_id":"vps_001"}]`))
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/nodes/nd_001/vps", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if strings.TrimSpace(string(body)) == spaShell {
		t.Fatalf("expected node vps API response, got SPA fallback body %q", string(body))
	}
	if !strings.Contains(string(body), `"vps_id":"vps_001"`) {
		t.Fatalf("expected node vps payload, got %q", string(body))
	}
}

func TestRouterKeepsSubscriptionsOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		SubscriptionsCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"subscription_id":"sub_001"}]`))
		}),
		SubscriptionItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"subscription_id":"sub_001"}`))
		}),
	})

	tests := []struct {
		name            string
		path            string
		wantBodySnippet string
	}{
		{name: "collection", path: "/api/subscriptions", wantBodySnippet: `"subscription_id":"sub_001"`},
		{name: "item", path: "/api/subscriptions/sub_001", wantBodySnippet: `"subscription_id":"sub_001"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
			}
			body, err := io.ReadAll(recorder.Body)
			if err != nil {
				t.Fatalf("read response body: %v", err)
			}
			if strings.TrimSpace(string(body)) == spaShell {
				t.Fatalf("expected subscriptions API response, got SPA fallback body %q", string(body))
			}
			if !strings.Contains(string(body), tt.wantBodySnippet) {
				t.Fatalf("expected subscriptions payload, got %q", string(body))
			}
		})
	}
}

func TestRouterStillFallsBackForWebPath(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
	})

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if strings.TrimSpace(string(body)) != spaShell {
		t.Fatalf("expected SPA body %q, got %q", spaShell, string(body))
	}
}

func TestRouterDoesNotFallBackToSPAForUnknownAPIPath(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/unknown", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}

	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if strings.TrimSpace(string(body)) == spaShell {
		t.Fatalf("expected API 404 body, got SPA fallback body %q", string(body))
	}
}

func TestRouterKeepsDashboardAndEventsOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		DashboardHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"abnormal_node_count":1}`))
		}),
		EventsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		}),
		IncidentsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		}),
	})

	for _, path := range []string{"/api/dashboard", "/api/events", "/api/incidents"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusOK)
		}
		if strings.TrimSpace(recorder.Body.String()) == spaShell {
			t.Fatalf("%s returned SPA fallback body %q", path, recorder.Body.String())
		}
	}
}

func TestRouterKeepsNodeSparklinesOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version: "dev",
		NodeSparklinesHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"nodes":{"nd_001":{"cpu_usage_pct":[12.5,13.0]}}}`))
		}),
	})

	req := httptest.NewRequest(http.MethodGet,
		"/api/nodes/sparklines?metrics=cpu_usage_pct&window=24h&downsample=24", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	body := recorder.Body.String()
	if !strings.Contains(body, `"nodes"`) {
		t.Fatalf("expected sparklines payload, got %q", body)
	}
}

func TestRouterDoesNotFallBackToSPAForUnknownTargetSubtree(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		TargetItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		TargetProbeItemsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/targets/tg_001/unknown", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}

	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if strings.TrimSpace(string(body)) == spaShell {
		t.Fatalf("expected target subtree 404 body, got SPA fallback body %q", string(body))
	}
}

func TestRouterKeepsTargetSparklinesOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version: "dev",
		TargetSparklinesHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"targets":{"tg_001":{"latency":[12.5,13.0]}}}`))
		}),
	})

	req := httptest.NewRequest(http.MethodGet,
		"/api/targets/sparklines?metrics=latency&window=24h&downsample=24", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	body := recorder.Body.String()
	if !strings.Contains(body, `"targets"`) {
		t.Fatalf("expected sparklines payload, got %q", body)
	}
}

func TestRouterKeepsNodeRuntimeFactsOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		NodeRuntimeFactsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"node_id":"nd_001","latest_host_sample":null}`))
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/nodes/nd_001/runtime-facts", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if strings.TrimSpace(string(body)) == spaShell {
		t.Fatalf("expected API response, got SPA fallback body %q", string(body))
	}

	if !strings.Contains(string(body), `"node_id":"nd_001"`) {
		t.Fatalf("expected node runtime facts payload, got %q", string(body))
	}
}

func TestRouterKeepsNodeOnboardingAdminRoutesOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		NodeOnboardingHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"node_id":"nd_001","phase":"未开始接入"}`))
		}),
		NodeEnrollmentTokenHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"enroll_001"}`))
		}),
		NodeBindingConfirmRebindHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"phase":"已绑定，等待稳定观测"}`))
		}),
		NodeBindingRejectPendingHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"phase":"已绑定，等待稳定观测"}`))
		}),
		NodeBindingResetHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"phase":"未开始接入"}`))
		}),
	})

	tests := []struct {
		name            string
		method          string
		path            string
		wantBodySnippet string
	}{
		{name: "onboarding", method: http.MethodGet, path: "/api/nodes/nd_001/onboarding", wantBodySnippet: `"phase":"未开始接入"`},
		{name: "issue token", method: http.MethodPost, path: "/api/nodes/nd_001/enrollment-token", wantBodySnippet: `"token":"enroll_001"`},
		{name: "confirm rebind", method: http.MethodPost, path: "/api/nodes/nd_001/binding/confirm-rebind", wantBodySnippet: `"phase":"已绑定，等待稳定观测"`},
		{name: "reject pending", method: http.MethodPost, path: "/api/nodes/nd_001/binding/reject-pending", wantBodySnippet: `"phase":"已绑定，等待稳定观测"`},
		{name: "reset binding", method: http.MethodPost, path: "/api/nodes/nd_001/binding/reset", wantBodySnippet: `"phase":"未开始接入"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
			}

			body, err := io.ReadAll(recorder.Body)
			if err != nil {
				t.Fatalf("read response body: %v", err)
			}

			if strings.TrimSpace(string(body)) == spaShell {
				t.Fatalf("expected API response, got SPA fallback body %q", string(body))
			}
			if !strings.Contains(string(body), tt.wantBodySnippet) {
				t.Fatalf("expected body to contain %q, got %q", tt.wantBodySnippet, string(body))
			}
		})
	}
}

func TestRouterKeepsNodeActionsOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		NodeActionsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"action_id":"act_001","status":"pending"}`))
		}),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/nd_001/actions", strings.NewReader(`{"command_id":"systemd_status"}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if strings.TrimSpace(string(body)) == spaShell {
		t.Fatalf("expected node action API response, got SPA fallback body %q", string(body))
	}
	if !strings.Contains(string(body), `"action_id":"act_001"`) {
		t.Fatalf("expected node action payload, got %q", string(body))
	}
}

func TestRouterKeepsNodeLifecycleRoutesOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		NodeLifecycleControlHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"node_id":"nd_001","lifecycle_status":"已退役"}`))
		}),
	})

	for _, path := range []string{
		"/api/nodes/nd_001/lifecycle/retire",
		"/api/nodes/nd_001/lifecycle/restore-to-observing",
	} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusOK)
		}
		body, err := io.ReadAll(recorder.Body)
		if err != nil {
			t.Fatalf("read response body: %v", err)
		}
		if strings.TrimSpace(string(body)) == spaShell {
			t.Fatalf("%s returned SPA fallback body %q", path, string(body))
		}
		if !strings.Contains(string(body), `"node_id":"nd_001"`) {
			t.Fatalf("%s body = %q, want node payload", path, string(body))
		}
	}
}

func TestRouterKeepsRuntimeControlRoutesOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		NodeRuntimeControlHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"node_id":"nd_001","monitoring_status":"暂停"}`))
		}),
		TargetRuntimeControlHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"target_id":"tg_001","run_status":"已归档"}`))
		}),
	})

	tests := []struct {
		path            string
		wantBodySnippet string
	}{
		{path: "/api/nodes/nd_001/runtime/enter-maintenance", wantBodySnippet: `"node_id":"nd_001"`},
		{path: "/api/nodes/nd_001/runtime/exit-maintenance", wantBodySnippet: `"node_id":"nd_001"`},
		{path: "/api/nodes/nd_001/runtime/pause", wantBodySnippet: `"node_id":"nd_001"`},
		{path: "/api/nodes/nd_001/runtime/resume", wantBodySnippet: `"node_id":"nd_001"`},
		{path: "/api/targets/tg_001/runtime/enter-maintenance", wantBodySnippet: `"target_id":"tg_001"`},
		{path: "/api/targets/tg_001/runtime/exit-maintenance", wantBodySnippet: `"target_id":"tg_001"`},
		{path: "/api/targets/tg_001/runtime/pause", wantBodySnippet: `"target_id":"tg_001"`},
		{path: "/api/targets/tg_001/runtime/resume", wantBodySnippet: `"target_id":"tg_001"`},
		{path: "/api/targets/tg_001/runtime/archive", wantBodySnippet: `"target_id":"tg_001"`},
		{path: "/api/targets/tg_001/runtime/restore-to-paused", wantBodySnippet: `"target_id":"tg_001"`},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("%s status = %d, want %d", tt.path, recorder.Code, http.StatusOK)
			}

			body, err := io.ReadAll(recorder.Body)
			if err != nil {
				t.Fatalf("read response body: %v", err)
			}

			if strings.TrimSpace(string(body)) == spaShell {
				t.Fatalf("%s returned SPA fallback body %q", tt.path, string(body))
			}
			if !strings.Contains(string(body), tt.wantBodySnippet) {
				t.Fatalf("%s body = %q, want snippet %q", tt.path, string(body), tt.wantBodySnippet)
			}
		})
	}
}

func TestRouterKeepsTargetRuntimeFactsOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		TargetRuntimeFactsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"target_id":"tg_001","latest_probe_observations":[]}`))
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/targets/tg_001/runtime-facts", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if strings.TrimSpace(string(body)) == spaShell {
		t.Fatalf("expected API response, got SPA fallback body %q", string(body))
	}

	if !strings.Contains(string(body), `"target_id":"tg_001"`) {
		t.Fatalf("expected target runtime facts payload, got %q", string(body))
	}
}
