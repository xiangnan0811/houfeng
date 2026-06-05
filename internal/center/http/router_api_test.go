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

func TestRouterKeepsAPIMonitoringInstancesOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		MonitoringInstancesCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"monitoring_instance_id":"mi_001"}]`))
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances", nil)
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

	if !strings.Contains(string(body), `"monitoring_instance_id":"mi_001"`) {
		t.Fatalf("expected monitoringInstance payload, got %q", string(body))
	}
}

func TestRouterKeepsSettingsOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		SettingsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"host_sample_frequency_tier":"5s"}`))
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

	if !strings.Contains(string(body), `"host_sample_frequency_tier":"5s"`) {
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

func TestRouterKeepsAssetDomainsOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		AssetDomainsCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"domain_id":"dom_001"}]`))
		}),
		VPSDomainsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"domain_id":"dom_vps_001"}]`))
		}),
	})

	tests := []struct {
		name            string
		path            string
		wantBodySnippet string
	}{
		{name: "collection", path: "/api/domains", wantBodySnippet: `"domain_id":"dom_001"`},
		{name: "vps domains", path: "/api/vps/vps_001/domains", wantBodySnippet: `"domain_id":"dom_vps_001"`},
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
				t.Fatalf("expected asset domain API response, got SPA fallback body %q", string(body))
			}
			if !strings.Contains(string(body), tt.wantBodySnippet) {
				t.Fatalf("expected asset domain payload, got %q", string(body))
			}
		})
	}
}

func TestRouterKeepsAssetDecisionsOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		AssetDecisionOverviewHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"group_count":1}`))
		}),
		AssetDecisionGroupsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"group_id":"adg_auto_001"}]`))
		}),
		AssetDecisionGroupHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"group_id":"adg_auto_001","members":[]}`))
		}),
		AssetDecisionManualGroupsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"manual_group_id":"admg_001"}]`))
		}),
		AssetDecisionManualGroupHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"manual_group_id":"admg_001","members":[]}`))
		}),
		AssetDecisionRecordsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"record_id":"adr_001"}]`))
		}),
		AssetDecisionRecordHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"record_id":"adr_001","members":[]}`))
		}),
	})

	tests := []struct {
		name            string
		path            string
		wantBodySnippet string
	}{
		{name: "overview", path: "/api/asset-decisions/overview", wantBodySnippet: `"group_count":1`},
		{name: "groups", path: "/api/asset-decisions/groups", wantBodySnippet: `"group_id":"adg_auto_001"`},
		{name: "group detail", path: "/api/asset-decisions/groups/adg_auto_001", wantBodySnippet: `"members":[]`},
		{name: "manual groups", path: "/api/asset-decisions/manual-groups", wantBodySnippet: `"manual_group_id":"admg_001"`},
		{name: "manual group detail", path: "/api/asset-decisions/manual-groups/admg_001", wantBodySnippet: `"members":[]`},
		{name: "manual group members", path: "/api/asset-decisions/manual-groups/admg_001/members/vps_001", wantBodySnippet: `"manual_group_id":"admg_001"`},
		{name: "records", path: "/api/asset-decisions/records", wantBodySnippet: `"record_id":"adr_001"`},
		{name: "record detail", path: "/api/asset-decisions/records/adr_001", wantBodySnippet: `"members":[]`},
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
				t.Fatalf("expected asset decisions API response, got SPA fallback body %q", string(body))
			}
			if !strings.Contains(string(body), tt.wantBodySnippet) {
				t.Fatalf("expected asset decisions payload, got %q", string(body))
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
		VPSMonitoringInstancesHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"monitoring_instance_id":"mi_001"}]`))
		}),
		VPSSubscriptionsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"subscription_id":"sub_001"}]`))
		}),
		VPSLinkMonitoringInstanceHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"link_id":"vnl_001"}`))
		}),
		VPSUnlinkMonitoringInstanceHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		VPSDomainsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"domain_id":"dom_001"}]`))
		}),
		VPSServicesHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"service_id":"svc_001"}]`))
		}),
		VPSCancellationPreviewHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"vps":{"vps_id":"vps_001"},"warnings":[],"blockers":[]}`))
		}),
		VPSCancellationHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"action":{"action_id":"ala_001"},"steps":[]}`))
		}),
		VPSExtendValidityHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"action":{"action_id":"ala_extend"},"steps":[]}`))
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
		{name: "monitoring_instances", path: "/api/vps/vps_001/monitoring-instances", wantStatus: http.StatusOK, wantBodySnippet: `"monitoring_instance_id":"mi_001"`},
		{name: "subscriptions", path: "/api/vps/vps_001/subscriptions", wantStatus: http.StatusOK, wantBodySnippet: `"subscription_id":"sub_001"`},
		{name: "link monitoringInstance", path: "/api/vps/vps_001/link-monitoring-instance", wantStatus: http.StatusCreated, wantBodySnippet: `"link_id":"vnl_001"`},
		{name: "unlink monitoringInstance", path: "/api/vps/vps_001/unlink-monitoring-instance", wantStatus: http.StatusOK, wantBodySnippet: `"link_id":"vnl_001"`},
		{name: "timeline", path: "/api/vps/vps_001/timeline", wantStatus: http.StatusOK, wantBodySnippet: `"price_history_id":"ph_001"`},
		{name: "experience logs", path: "/api/vps/vps_001/experience-logs", wantStatus: http.StatusOK, wantBodySnippet: `"experience_log_id":"elog_001"`},
		{name: "domains", path: "/api/vps/vps_001/domains", wantStatus: http.StatusOK, wantBodySnippet: `"domain_id":"dom_001"`},
		{name: "services", path: "/api/vps/vps_001/services", wantStatus: http.StatusOK, wantBodySnippet: `"service_id":"svc_001"`},
		{name: "cancellation preview", path: "/api/vps/vps_001/cancellation-preview", wantStatus: http.StatusOK, wantBodySnippet: `"warnings":[]`},
		{name: "cancellation", path: "/api/vps/vps_001/cancellation", wantStatus: http.StatusOK, wantBodySnippet: `"action_id":"ala_001"`},
		{name: "extend validity", path: "/api/vps/vps_001/extend-validity", wantStatus: http.StatusOK, wantBodySnippet: `"action_id":"ala_extend"`},
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

func TestRouterKeepsMonitoringInstanceVPSOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		MonitoringInstanceVPSHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"vps_id":"vps_001"}]`))
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/vps", nil)
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
		t.Fatalf("expected monitoringInstance vps API response, got SPA fallback body %q", string(body))
	}
	if !strings.Contains(string(body), `"vps_id":"vps_001"`) {
		t.Fatalf("expected monitoringInstance vps payload, got %q", string(body))
	}
}

func TestRouterKeepsAssetContextOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		AssetContextMonitoringInstancesHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"monitoring_instance_id":"mi_001","cancellation_attention":true}]`))
		}),
		AssetContextTargetsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"target_id":"tg_001","cancellation_attention":true}]`))
		}),
	})

	tests := []struct {
		name            string
		path            string
		wantBodySnippet string
	}{
		{name: "monitoring_instances", path: "/api/asset-context/monitoring-instances", wantBodySnippet: `"monitoring_instance_id":"mi_001"`},
		{name: "targets", path: "/api/asset-context/targets", wantBodySnippet: `"target_id":"tg_001"`},
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
				t.Fatalf("expected asset context API response, got SPA fallback body %q", string(body))
			}
			if !strings.Contains(string(body), tt.wantBodySnippet) {
				t.Fatalf("expected asset context payload, got %q", string(body))
			}
		})
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
		SubscriptionStatisticsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("window") != "year" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"window":"year","cost_month_buckets":[{"bucket":"2026-06","monthly_cost":90,"renewal_count":0,"data_insufficient":false}]}`))
		}),
		SubscriptionMonthlyBudgetsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"budget_month":"2026-06-01","base_currency":"CNY","monthly_limit":120}]`))
		}),
	})

	tests := []struct {
		name            string
		path            string
		wantBodySnippet string
	}{
		{name: "collection", path: "/api/subscriptions", wantBodySnippet: `"subscription_id":"sub_001"`},
		{name: "item", path: "/api/subscriptions/sub_001", wantBodySnippet: `"subscription_id":"sub_001"`},
		{name: "statistics", path: "/api/subscriptions/statistics?window=year", wantBodySnippet: `"cost_month_buckets"`},
		{name: "monthly budgets", path: "/api/subscription-monthly-budgets", wantBodySnippet: `"monthly_limit"`},
		{name: "monthly budget item", path: "/api/subscription-monthly-budgets/2026-06", wantBodySnippet: `"monthly_limit"`},
		{name: "monthly budget bulk", path: "/api/subscription-monthly-budgets/bulk", wantBodySnippet: `"monthly_limit"`},
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

	req := httptest.NewRequest(http.MethodGet, "/monitoring", nil)
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
			_, _ = w.Write([]byte(`{"abnormal_monitoring_instance_count":1}`))
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

func TestRouterKeepsMonitoringInstanceSparklinesOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version: "dev",
		MonitoringInstanceSparklinesHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"monitoring_instances":{"mi_001":{"cpu_usage_pct":[12.5,13.0]}}}`))
		}),
	})

	req := httptest.NewRequest(http.MethodGet,
		"/api/monitoring-instances/sparklines?metrics=cpu_usage_pct&window=24h&downsample=24", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	body := recorder.Body.String()
	if !strings.Contains(body, `"monitoring_instances"`) {
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

func TestRouterKeepsMonitoringInstanceRuntimeFactsOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		MonitoringInstanceRuntimeFactsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"monitoring_instance_id":"mi_001","latest_host_sample":null}`))
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/runtime-facts", nil)
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

	if !strings.Contains(string(body), `"monitoring_instance_id":"mi_001"`) {
		t.Fatalf("expected monitoringInstance runtime facts payload, got %q", string(body))
	}
}

func TestRouterKeepsMonitoringInstanceOnboardingAdminRoutesOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		MonitoringInstanceOnboardingHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"monitoring_instance_id":"mi_001","phase":"未开始接入"}`))
		}),
		MonitoringInstanceEnrollmentTokenHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"enroll_001"}`))
		}),
		MonitoringInstanceInstallCommandHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"command":"curl -fsSL https://center.example.com/api/agent/install.sh"}`))
		}),
		MonitoringInstanceBindingConfirmRebindHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"phase":"已绑定，等待稳定观测"}`))
		}),
		MonitoringInstanceBindingRejectPendingHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"phase":"已绑定，等待稳定观测"}`))
		}),
		MonitoringInstanceBindingResetHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		{name: "onboarding", method: http.MethodGet, path: "/api/monitoring-instances/mi_001/onboarding", wantBodySnippet: `"phase":"未开始接入"`},
		{name: "issue token", method: http.MethodPost, path: "/api/monitoring-instances/mi_001/enrollment-token", wantBodySnippet: `"token":"enroll_001"`},
		{name: "install command", method: http.MethodPost, path: "/api/monitoring-instances/mi_001/install-command", wantBodySnippet: `"command":"curl -fsSL https://center.example.com/api/agent/install.sh"`},
		{name: "confirm rebind", method: http.MethodPost, path: "/api/monitoring-instances/mi_001/binding/confirm-rebind", wantBodySnippet: `"phase":"已绑定，等待稳定观测"`},
		{name: "reject pending", method: http.MethodPost, path: "/api/monitoring-instances/mi_001/binding/reject-pending", wantBodySnippet: `"phase":"已绑定，等待稳定观测"`},
		{name: "reset binding", method: http.MethodPost, path: "/api/monitoring-instances/mi_001/binding/reset", wantBodySnippet: `"phase":"未开始接入"`},
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

func TestRouterServesInstallerScriptOutsideAuthMiddleware(t *testing.T) {
	calledAuth := false
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		InstallerScriptHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
			_, _ = w.Write([]byte("#!/bin/sh\necho installer\n"))
		}),
		AuthMiddleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calledAuth = true
				next.ServeHTTP(w, r)
			})
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/agent/install.sh", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if calledAuth {
		t.Fatal("installer script route was wrapped by auth middleware")
	}
	if strings.TrimSpace(recorder.Body.String()) == spaShell {
		t.Fatalf("expected installer script response, got SPA fallback body %q", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "echo installer") {
		t.Fatalf("installer body = %q, want script payload", recorder.Body.String())
	}
}

func TestRouterKeepsMonitoringInstanceActionsOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		MonitoringInstanceActionsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"action_id":"act_001","status":"pending"}`))
		}),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{"command_id":"systemd_status"}`))
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
		t.Fatalf("expected monitoringInstance action API response, got SPA fallback body %q", string(body))
	}
	if !strings.Contains(string(body), `"action_id":"act_001"`) {
		t.Fatalf("expected monitoringInstance action payload, got %q", string(body))
	}
}

func TestRouterRejectsMonitoringInstanceLifecycleRoutesWithoutSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		MonitoringInstanceItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"monitoring_instance_id":"mi_001"}`))
		}),
	})

	for _, path := range []string{
		"/api/monitoring-instances/mi_001/lifecycle/retire",
		"/api/monitoring-instances/mi_001/lifecycle/restore-to-observing",
	} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusNotFound)
		}
		body, err := io.ReadAll(recorder.Body)
		if err != nil {
			t.Fatalf("read response body: %v", err)
		}
		if strings.TrimSpace(string(body)) == spaShell {
			t.Fatalf("%s returned SPA fallback body %q", path, string(body))
		}
	}
}

func TestRouterKeepsRuntimeControlRoutesOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		MonitoringInstanceRuntimeControlHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"monitoring_instance_id":"mi_001","monitoring_status":"暂停"}`))
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
		{path: "/api/monitoring-instances/mi_001/runtime/enter-maintenance", wantBodySnippet: `"monitoring_instance_id":"mi_001"`},
		{path: "/api/monitoring-instances/mi_001/runtime/exit-maintenance", wantBodySnippet: `"monitoring_instance_id":"mi_001"`},
		{path: "/api/monitoring-instances/mi_001/runtime/pause", wantBodySnippet: `"monitoring_instance_id":"mi_001"`},
		{path: "/api/monitoring-instances/mi_001/runtime/resume", wantBodySnippet: `"monitoring_instance_id":"mi_001"`},
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
