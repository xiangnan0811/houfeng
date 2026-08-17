package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordcollaboration"
)

func TestRecordWatchesHandlerReadsAndMutatesClosedNoStoreStatus(t *testing.T) {
	actor := testRecordActionActor(t)
	application := &recordWatchHandlerStub{result: recordcollaboration.WatchStatus{
		RecordID: "rec_watchparent1", UserID: actor.UserID, Version: 2,
		Preference: recordcollaboration.FollowerPreferenceMuted,
		Sources:    recordcollaboration.FollowerSources{Owner: true, Comment: true}, RecordFenceEpoch: 5,
		UpdatedAt: time.Date(2026, 8, 17, 3, 0, 0, 0, time.UTC),
	}}
	handler := RecordWatches(application)

	request := httptest.NewRequest(http.MethodPatch, "/api/records/rec_watchparent1/watch", strings.NewReader(`{"preference":"muted"}`))
	request.Header.Set("Idempotency-Key", "watch-mute-1")
	request.Header.Set("If-Match", `"1"`)
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != recordPrivateCacheControl || recorder.Header().Get("ETag") != `"2"` {
		t.Fatalf("status/headers = %d %#v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	if application.setCalls != 1 || application.set.RecordID != "rec_watchparent1" || application.set.ExpectedVersion != 1 ||
		application.set.Preference != recordcollaboration.FollowerPreferenceMuted || application.set.IdempotencyKey != "watch-mute-1" ||
		application.set.Actor.UserID != actor.UserID {
		t.Fatalf("set request = %#v calls=%d", application.set, application.setCalls)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"record_fence_epoch", "authorization_epoch", "body", "render", "outbox", "evidence"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}

	application.result = recordcollaboration.WatchStatus{RecordID: "rec_watchparent1", UserID: actor.UserID, Preference: recordcollaboration.FollowerPreferenceDefault}
	request = httptest.NewRequest(http.MethodGet, "/api/records/rec_watchparent1/watch", nil)
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("ETag") != `"0"` || application.getCalls != 1 {
		t.Fatalf("GET status/etag/calls = %d/%q/%d body=%s", recorder.Code, recorder.Header().Get("ETag"), application.getCalls, recorder.Body.String())
	}
}

func TestRecordWatchesHandlerRequiresExactPatchContract(t *testing.T) {
	actor := testRecordActionActor(t)
	tests := []struct {
		name, body string
		headers    http.Header
	}{
		{name: "missing idempotency", body: `{"preference":"watching"}`, headers: http.Header{"If-Match": {`"0"`}}},
		{name: "missing if match", body: `{"preference":"watching"}`, headers: http.Header{"Idempotency-Key": {"watch"}}},
		{name: "weak etag", body: `{"preference":"watching"}`, headers: http.Header{"Idempotency-Key": {"watch"}, "If-Match": {`W/"0"`}}},
		{name: "unknown preference", body: `{"preference":"all"}`, headers: http.Header{"Idempotency-Key": {"watch"}, "If-Match": {`"0"`}}},
		{name: "unknown json", body: `{"preference":"watching","content":"secret"}`, headers: http.Header{"Idempotency-Key": {"watch"}, "If-Match": {`"0"`}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			application := &recordWatchHandlerStub{}
			request := httptest.NewRequest(http.MethodPatch, "/api/records/rec_watchparent1/watch", strings.NewReader(tt.body))
			request.Header = tt.headers.Clone()
			request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
			recorder := httptest.NewRecorder()
			RecordWatches(application).ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest && recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if application.setCalls != 0 {
				t.Fatalf("application called %d times", application.setCalls)
			}
		})
	}
}

type recordWatchHandlerStub struct {
	result   recordcollaboration.WatchStatus
	set      recordcollaboration.WatchSetApplicationRequest
	get      recordcollaboration.WatchReadApplicationRequest
	setCalls int
	getCalls int
}

func (stub *recordWatchHandlerStub) SetWatch(_ context.Context, request recordcollaboration.WatchSetApplicationRequest) (recordcollaboration.WatchStatus, error) {
	stub.setCalls++
	stub.set = request
	return stub.result, nil
}

func (stub *recordWatchHandlerStub) GetWatch(_ context.Context, request recordcollaboration.WatchReadApplicationRequest) (recordcollaboration.WatchStatus, error) {
	stub.getCalls++
	stub.get = request
	return stub.result, nil
}
