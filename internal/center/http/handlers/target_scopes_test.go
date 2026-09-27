package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/targets"
)

type scopedTargetRepository struct {
	*fakeTargetRepository
	scope  targets.ListScope
	called bool
}

func (r *scopedTargetRepository) ListTargetsByScope(_ context.Context, scope targets.ListScope) ([]targets.TargetRecord, error) {
	r.called = true
	r.scope = scope
	return []targets.TargetRecord{{TargetID: "target-history", LifecycleStatus: targets.LifecycleRetired, RunStatus: targets.RunStatusPaused}}, nil
}

func TestTargetsCollectionScopesPreserveHistoricalAccess(t *testing.T) {
	for _, tt := range []struct {
		query  string
		want   targets.ListScope
		status int
	}{
		{"", targets.ListScopeCurrent, http.StatusOK},
		{"?scope=current", targets.ListScopeCurrent, http.StatusOK},
		{"?scope=retired", targets.ListScopeRetired, http.StatusOK},
		{"?scope=all", targets.ListScopeAll, http.StatusOK},
		{"?scope=archived", "", http.StatusBadRequest},
	} {
		t.Run(tt.query, func(t *testing.T) {
			repo := &scopedTargetRepository{fakeTargetRepository: &fakeTargetRepository{}}
			w := httptest.NewRecorder()
			handlers.TargetsCollection(repo).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/targets"+tt.query, nil))
			if w.Code != tt.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tt.status == http.StatusOK && (!repo.called || repo.scope != tt.want) {
				t.Fatalf("called=%v scope=%q", repo.called, repo.scope)
			}
			if tt.status == http.StatusBadRequest && repo.called {
				t.Fatal("invalid scope reached repository")
			}
		})
	}
}
