package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/internal/center/assetrelations"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/vpsfollowups"
)

type relationHandlerRepo struct {
	vps, kind, id, actor, reason string
	calls                        int
}

func (r *relationHandlerRepo) List(_ context.Context, vps, kind string, current bool) ([]assetrelations.Record, error) {
	r.vps, r.kind = vps, kind
	r.calls++
	return []assetrelations.Record{}, nil
}
func (r *relationHandlerRepo) Link(_ context.Context, vps, kind string, in assetrelations.LinkInput, actor string) (assetrelations.Record, error) {
	r.vps, r.kind, r.id, r.actor = vps, kind, in.ObjectID, actor
	r.calls++
	return assetrelations.Record{ObjectID: in.ObjectID, VPSID: vps}, nil
}
func (r *relationHandlerRepo) End(_ context.Context, vps, kind, id, reason, actor string) (assetrelations.Record, error) {
	r.vps, r.kind, r.id, r.reason, r.actor = vps, kind, id, reason, actor
	r.calls++
	return assetrelations.Record{AssociationID: id, VPSID: vps}, nil
}

func TestVPSAssociationMutationUsesPathAndAuthenticatedActor(t *testing.T) {
	repo := &relationHandlerRepo{}
	req := httptest.NewRequest(http.MethodPatch, "/api/vps/vps_source/service-associations/assoc_1/end", strings.NewReader(`{"reason":"moved"}`))
	req = req.WithContext(sessionctx.WithUserID(req.Context(), "operator"))
	w := httptest.NewRecorder()
	handlers.VPSAssetAssociations(repo, assetrelations.Service).ServeHTTP(w, req)
	if w.Code != http.StatusOK || repo.vps != "vps_source" || repo.id != "assoc_1" || repo.actor != "operator" || repo.reason != "moved" {
		t.Fatalf("response=%d repo=%+v", w.Code, repo)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/vps/vps_other/service-associations", strings.NewReader(`{"object_id":"service_1"}`))
	w = httptest.NewRecorder()
	handlers.VPSAssetAssociations(repo, assetrelations.Service).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized || repo.calls != 1 {
		t.Fatalf("unauthenticated mutation reached repository: %d %+v", w.Code, repo)
	}
}

type followupHandlerRepo struct {
	vps, id, actor string
	input          vpsfollowups.ResolveInput
	calls          int
}

func (r *followupHandlerRepo) List(context.Context, string) ([]vpsfollowups.Record, error) {
	return []vpsfollowups.Record{}, nil
}
func (r *followupHandlerRepo) Create(context.Context, string, vpsfollowups.CreateInput, string) (vpsfollowups.Record, error) {
	return vpsfollowups.Record{}, nil
}
func (r *followupHandlerRepo) Resolve(_ context.Context, vps, id string, in vpsfollowups.ResolveInput, actor string) (vpsfollowups.Record, error) {
	r.vps, r.id, r.input, r.actor = vps, id, in, actor
	r.calls++
	return vpsfollowups.Record{}, nil
}

func TestVPSFollowupClosureRequiresReasonAndPreservesScope(t *testing.T) {
	repo := &followupHandlerRepo{}
	for _, body := range []string{`{"status":"ignored"}`, `{"status":"pending","reason":"reopen"}`} {
		req := httptest.NewRequest(http.MethodPatch, "/api/vps/archived_vps/followups/item_1", strings.NewReader(body))
		req = req.WithContext(sessionctx.WithUserID(req.Context(), "operator"))
		w := httptest.NewRecorder()
		handlers.VPSFollowups(repo).ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest || repo.calls != 0 {
			t.Fatalf("invalid closure = %d %+v", w.Code, repo)
		}
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/vps/archived_vps/followups/item_1", strings.NewReader(`{"status":"ignored","reason":"confirmed with provider"}`))
	req = req.WithContext(sessionctx.WithUserID(req.Context(), "operator"))
	w := httptest.NewRecorder()
	handlers.VPSFollowups(repo).ServeHTTP(w, req)
	if w.Code != http.StatusOK || repo.vps != "archived_vps" || repo.id != "item_1" || repo.actor != "operator" || repo.input.Status != "ignored" {
		t.Fatalf("closure = %d %+v", w.Code, repo)
	}
}
