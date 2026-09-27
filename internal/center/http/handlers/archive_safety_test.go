package handlers_test

import (
	"encoding/json"
	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/http/handlers"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestArchiveSafetyHTTPConflictReturnsLatestReview(t *testing.T) {
	latest := assetlifecycle.ArchiveReview{PreviewDigest: "fresh-review", Eligible: false, BlockerDetails: []assetlifecycle.BlockerDetail{{Code: "recent_trusted_online_signal", ObjectID: "historical-agent"}}}
	for _, test := range []struct {
		name, code string
		err        error
		withReview bool
	}{
		{"expired preview", "archive_preview_stale", &assetlifecycle.StaleArchivePreviewError{Review: latest}, true},
		{"fresh heartbeat", "lifecycle_action_blocked", &assetlifecycle.ArchiveBlockedError{Review: latest}, true},
		{"reused key", "archive_idempotency_conflict", assetlifecycle.ErrArchiveIdempotencyConflict, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/archive", strings.NewReader(`{"confirmation_name":"VPS","reason":"ended","preview_digest":"old","idempotency_key":"request"}`))
			handlers.VPSArchive(&fakeAssetLifecycleRepository{archiveErr: test.err}).ServeHTTP(recorder, request)
			if recorder.Code != http.StatusConflict {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var body struct {
				Code   string                       `json:"code"`
				Review assetlifecycle.ArchiveReview `json:"review"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != test.code || (test.withReview && (body.Review.PreviewDigest != "fresh-review" || len(body.Review.BlockerDetails) != 1)) {
				t.Fatalf("conflict=%+v", body)
			}
		})
	}
}
