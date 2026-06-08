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
	"houfeng/internal/center/ipquality"
	"houfeng/internal/contracts/agentapi"
)

type fakeVPSIPQualityRepository struct {
	result ipquality.VPSReport
	err    error
	vpsID  string
}

func (f *fakeVPSIPQualityRepository) GetVPSIPQuality(_ context.Context, vpsID string) (ipquality.VPSReport, error) {
	f.vpsID = vpsID
	if f.err != nil {
		return ipquality.VPSReport{}, f.err
	}
	return f.result, nil
}

func TestVPSIPQualityHandlerReturnsReport(t *testing.T) {
	now := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
	repo := &fakeVPSIPQualityRepository{result: ipquality.VPSReport{
		Summary: &ipquality.Summary{
			VPSID:          "vps_001",
			ObservedAt:     now,
			IPAddress:      "203.0.113.10",
			IPVersion:      4,
			Status:         agentapi.IPQualityStatusSuccess,
			RiskLevel:      "low",
			AssignmentMode: "link",
			ProviderCount:  1,
		},
		ProviderResults: []ipquality.ProviderResultRead{{Provider: "ipinfo", UsageType: "hosting"}},
		ServiceUnlocks:  []ipquality.ServiceUnlockRead{{Service: "netflix", Status: "unlocked", Region: "US"}},
		History:         []ipquality.Summary{},
	}}
	handler := handlers.VPSIPQuality(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/vps/vps_001/ip-quality", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.vpsID != "vps_001" {
		t.Fatalf("repo vpsID = %q, want vps_001", repo.vpsID)
	}
	var body ipquality.VPSReport
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Summary == nil || body.Summary.IPAddress != "203.0.113.10" {
		t.Fatalf("Summary = %#v, want ip quality summary", body.Summary)
	}
	if len(body.ProviderResults) != 1 || body.ProviderResults[0].Provider != "ipinfo" {
		t.Fatalf("ProviderResults = %#v, want ipinfo", body.ProviderResults)
	}
}

func TestVPSIPQualityHandlerRejectsWrongMethod(t *testing.T) {
	handler := handlers.VPSIPQuality(&fakeVPSIPQualityRepository{})
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/ip-quality", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func TestVPSIPQualityHandlerMapsRepositoryFailure(t *testing.T) {
	handler := handlers.VPSIPQuality(&fakeVPSIPQualityRepository{err: errors.New("boom")})
	req := httptest.NewRequest(http.MethodGet, "/api/vps/vps_001/ip-quality", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}
