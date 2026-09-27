package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/vpsmaintenance"
)

type maintenanceHandlerRepo struct {
	calls int
	actor string
}

func (r *maintenanceHandlerRepo) Review(context.Context, string) (vpsmaintenance.Review, error) {
	r.calls++
	return vpsmaintenance.Review{VPSID: "v1"}, nil
}
func (r *maintenanceHandlerRepo) Start(_ context.Context, _ string, _ vpsmaintenance.StartInput, actor string) (vpsmaintenance.Review, error) {
	r.calls++
	r.actor = actor
	return vpsmaintenance.Review{VPSID: "v1", PreviewDigest: "fresh"}, vpsmaintenance.ErrConflict
}
func (r *maintenanceHandlerRepo) End(_ context.Context, _ string, _ vpsmaintenance.EndInput, actor string) (vpsmaintenance.Review, error) {
	r.calls++
	r.actor = actor
	return vpsmaintenance.Review{VPSID: "v1"}, nil
}

func TestVPSMaintenanceHandlerAuthenticationAndConflictReview(t *testing.T) {
	repo := &maintenanceHandlerRepo{}
	handler := VPSMaintenance(repo)
	request := httptest.NewRequest(http.MethodPost, "/api/vps/v1/maintenance", strings.NewReader(`{"reason":"work","preview_digest":"old"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || repo.calls != 0 {
		t.Fatalf("unauthenticated mutation=%d calls=%d", response.Code, repo.calls)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/vps/v1/maintenance", strings.NewReader(`{"reason":"work","preview_digest":"old"}`))
	request = request.WithContext(sessionctx.WithUserID(request.Context(), "operator"))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || repo.actor != "operator" || !strings.Contains(response.Body.String(), `"preview_digest":"fresh"`) {
		t.Fatalf("stale review response=%d %s actor=%s", response.Code, response.Body.String(), repo.actor)
	}
}
