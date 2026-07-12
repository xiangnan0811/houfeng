package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/commandaudits"
)

func TestCommandAuditResponseOwnsNestedIdentityAllowlists(t *testing.T) {
	t.Parallel()

	responseType := reflect.TypeOf(commandAuditActionResponse{})
	for _, fieldName := range []string{"MonitoringInstance", "Actor"} {
		field, ok := responseType.FieldByName(fieldName)
		if !ok {
			t.Fatalf("commandAuditActionResponse is missing %s", fieldName)
		}
		fieldType := field.Type
		if fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		if fieldType.PkgPath() != responseType.PkgPath() {
			t.Fatalf("%s response type = %v from %q, want a handler-owned allowlist DTO", fieldName, field.Type, fieldType.PkgPath())
		}
	}
}

func TestCommandAuditsDefaultsToFixedThirtyDayWindowAndMetadataResponse(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 12, 12, 30, 0, 0, time.UTC)
	exitCode := 0
	repo := &fakeCommandAuditRepository{pages: []commandaudits.Page{{
		Items: []commandaudits.Action{{
			ID:       "act_001",
			ActionID: "act_001",
			MonitoringInstance: commandaudits.MonitoringInstanceIdentity{
				ID:   "mi_001",
				Name: "Tokyo Edge",
			},
			CommandID:   "uptime",
			Sensitivity: "standard",
			Outcome:     "succeeded",
			Actor: &commandaudits.ActorIdentity{
				UserID:      "usr_001",
				Username:    "admin",
				DisplayName: "管理员",
			},
			StartedAt: now.Add(-time.Hour),
			Events: []commandaudits.Event{{
				AuditID:    "cmd_aud_001",
				EventType:  "completed",
				Source:     "agent_sync",
				OccurredAt: now.Add(-30 * time.Minute),
				ExitCode:   &exitCode,
			}},
		}},
	}}}
	handler := CommandAuditsWithOptions(repo, CommandAuditOptions{Now: func() time.Time { return now }})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/command-audits", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if len(repo.queries) != 1 {
		t.Fatalf("repository calls = %d, want 1", len(repo.queries))
	}
	query := repo.queries[0]
	if query.StartedFrom == nil || !query.StartedFrom.Equal(now.Add(-30*24*time.Hour)) || !query.StartedTo.Equal(now) || query.Limit != 20 {
		t.Fatalf("default query = %#v", query)
	}
	var raw map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	items, ok := raw["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("response items = %#v", raw["items"])
	}
	body := strings.ToLower(recorder.Body.String())
	for _, forbidden := range []string{"stdout", "stderr", "details", "hasmore", "has_more"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked internal/output field %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestCommandAuditsNormalizesInitialFiltersAndCustomBounds(t *testing.T) {
	t.Parallel()

	nowCalls := 0
	repo := &fakeCommandAuditRepository{pages: []commandaudits.Page{{Items: make([]commandaudits.Action, 0)}}}
	handler := CommandAuditsWithOptions(repo, CommandAuditOptions{Now: func() time.Time {
		nowCalls++
		return time.Date(2026, time.July, 12, 12, 30, 0, 0, time.UTC)
	}})
	params := url.Values{
		"window":              {"custom"},
		"started_from":        {"2026-07-01T00:00:00+08:00"},
		"started_to":          {"2026-07-02T00:00:00+08:00"},
		"monitoring_instance": {"  Tokyo Edge  "},
		"command_id":          {" uptime "},
		"sensitivity":         {" sensitive "},
		"outcome":             {" failed "},
		"actor":               {" admin "},
		"action_id":           {" act_001 "},
		"limit":               {"100"},
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/command-audits?"+params.Encode(), nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if nowCalls != 1 {
		t.Fatalf("Now calls = %d, want exactly 1", nowCalls)
	}
	query := repo.queries[0]
	wantFrom := time.Date(2026, time.June, 30, 16, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, time.July, 1, 16, 0, 0, 0, time.UTC)
	if query.StartedFrom == nil || !query.StartedFrom.Equal(wantFrom) || !query.StartedTo.Equal(wantTo) {
		t.Fatalf("custom bounds = (%v, %v), want (%v, %v)", query.StartedFrom, query.StartedTo, wantFrom, wantTo)
	}
	if query.MonitoringInstance != "Tokyo Edge" || query.CommandID != "uptime" || query.Sensitivity != "sensitive" || query.Outcome != "failed" || query.Actor != "admin" || query.ActionID != "act_001" || query.Limit != 100 {
		t.Fatalf("normalized query = %#v", query)
	}
}

func TestCommandAuditsSupportsNamedWindows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		window       string
		wantDuration time.Duration
		wantFrom     bool
	}{
		{window: "24h", wantDuration: 24 * time.Hour, wantFrom: true},
		{window: "7d", wantDuration: 7 * 24 * time.Hour, wantFrom: true},
		{window: "30d", wantDuration: 30 * 24 * time.Hour, wantFrom: true},
		{window: "all", wantFrom: false},
	}
	for _, tt := range tests {
		t.Run(tt.window, func(t *testing.T) {
			repo := &fakeCommandAuditRepository{pages: []commandaudits.Page{{Items: make([]commandaudits.Action, 0)}}}
			handler := CommandAuditsWithOptions(repo, CommandAuditOptions{Now: func() time.Time { return now }})
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/command-audits?window="+tt.window, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d; body=%s", recorder.Code, recorder.Body.String())
			}
			query := repo.queries[0]
			if !query.StartedTo.Equal(now) {
				t.Fatalf("StartedTo = %v, want %v", query.StartedTo, now)
			}
			if !tt.wantFrom {
				if query.StartedFrom != nil {
					t.Fatalf("StartedFrom = %v, want nil", query.StartedFrom)
				}
				return
			}
			if query.StartedFrom == nil || !query.StartedFrom.Equal(now.Add(-tt.wantDuration)) {
				t.Fatalf("StartedFrom = %v, want %v", query.StartedFrom, now.Add(-tt.wantDuration))
			}
		})
	}
}

func TestCommandAuditsRejectsInvalidInputsBeforeRepository(t *testing.T) {
	t.Parallel()

	validCursor := mustEncodeCommandAuditCursor(t, commandAuditCursorState{
		Version:         1,
		Filters:         commandAuditCursorFilters{Window: "30d"},
		StartedFrom:     timePtr(time.Date(2026, time.June, 12, 0, 0, 0, 0, time.UTC)),
		StartedTo:       time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC),
		Limit:           20,
		BeforeStartedAt: time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC),
		BeforeID:        "act_001",
	})
	tests := []string{
		"?window=tomorrow",
		"?window=custom&started_from=2026-07-01T00:00:00Z",
		"?window=custom&started_from=bad&started_to=2026-07-02T00:00:00Z",
		"?window=custom&started_from=2026-07-02T00:00:00Z&started_to=2026-07-01T00:00:00Z",
		"?window=24h&started_from=2026-07-01T00:00:00Z&started_to=2026-07-02T00:00:00Z",
		"?limit=0",
		"?limit=101",
		"?limit=many",
		"?sensitivity=private",
		"?outcome=done",
		"?command_id=unknown_command",
		"?cursor=not-base64",
		"?cursor=" + url.QueryEscape(validCursor) + "&limit=20",
		"?window=24h&window=7d",
		"?unknown=value",
	}
	for _, suffix := range tests {
		t.Run(suffix, func(t *testing.T) {
			repo := &fakeCommandAuditRepository{}
			handler := CommandAuditsWithOptions(repo, CommandAuditOptions{Now: func() time.Time {
				return time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC)
			}})
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/command-audits"+suffix, nil))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if len(repo.queries) != 0 {
				t.Fatalf("repository called with %#v", repo.queries)
			}
		})
	}
}

func TestCommandAuditsReturnsMethodAndRepositoryErrors(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("query failed")
	repo := &fakeCommandAuditRepository{err: wantErr}
	handler := CommandAuditsWithOptions(repo, CommandAuditOptions{Now: func() time.Time {
		return time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC)
	}})

	methodRecorder := httptest.NewRecorder()
	handler.ServeHTTP(methodRecorder, httptest.NewRequest(http.MethodPost, "/api/command-audits", nil))
	if methodRecorder.Code != http.StatusMethodNotAllowed || len(repo.queries) != 0 {
		t.Fatalf("method response = %d, repository calls=%d", methodRecorder.Code, len(repo.queries))
	}

	errorRecorder := httptest.NewRecorder()
	handler.ServeHTTP(errorRecorder, httptest.NewRequest(http.MethodGet, "/api/command-audits", nil))
	if errorRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("repository error status = %d, want %d", errorRecorder.Code, http.StatusInternalServerError)
	}
}

func TestCommandAuditsCursorContinuationKeepsSnapshotAndOnlyUpdatesLastKey(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)
	nowCalls := 0
	repo := &fakeCommandAuditRepository{pages: []commandaudits.Page{
		{
			Items:   []commandaudits.Action{{ID: "act_b", ActionID: "act_b", StartedAt: now.Add(-time.Hour), Events: make([]commandaudits.Event, 0)}},
			HasMore: true,
		},
		{
			Items: []commandaudits.Action{{ID: "act_a", ActionID: "act_a", StartedAt: now.Add(-2 * time.Hour), Events: make([]commandaudits.Event, 0)}},
		},
	}}
	handler := CommandAuditsWithOptions(repo, CommandAuditOptions{Now: func() time.Time {
		nowCalls++
		return now
	}})
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, httptest.NewRequest(http.MethodGet, "/api/command-audits?window=7d&actor=admin&limit=1", nil))
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d; body=%s", firstRecorder.Code, firstRecorder.Body.String())
	}
	var firstResponse struct {
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(firstRecorder.Body.Bytes(), &firstResponse); err != nil || firstResponse.NextCursor == "" {
		t.Fatalf("first response cursor = %q, error=%v", firstResponse.NextCursor, err)
	}

	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, httptest.NewRequest(http.MethodGet, "/api/command-audits?cursor="+url.QueryEscape(firstResponse.NextCursor), nil))
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("second status = %d; body=%s", secondRecorder.Code, secondRecorder.Body.String())
	}
	if nowCalls != 1 {
		t.Fatalf("Now calls = %d, want cursor continuation to reuse fixed snapshot", nowCalls)
	}
	if len(repo.queries) != 2 {
		t.Fatalf("repository queries = %#v", repo.queries)
	}
	firstQuery, secondQuery := repo.queries[0], repo.queries[1]
	if firstQuery.StartedFrom == nil || secondQuery.StartedFrom == nil || !secondQuery.StartedFrom.Equal(*firstQuery.StartedFrom) || !secondQuery.StartedTo.Equal(firstQuery.StartedTo) {
		t.Fatalf("snapshot drifted: first=%#v second=%#v", firstQuery, secondQuery)
	}
	if secondQuery.Actor != "admin" || secondQuery.Limit != 1 || secondQuery.BeforeStartedAt == nil || !secondQuery.BeforeStartedAt.Equal(now.Add(-time.Hour)) || secondQuery.BeforeID != "act_b" {
		t.Fatalf("continuation query = %#v", secondQuery)
	}
}

type fakeCommandAuditRepository struct {
	queries []commandaudits.Query
	pages   []commandaudits.Page
	err     error
}

func (f *fakeCommandAuditRepository) ListCommandAudits(_ context.Context, query commandaudits.Query) (commandaudits.Page, error) {
	f.queries = append(f.queries, query)
	if f.err != nil {
		return commandaudits.Page{}, f.err
	}
	if len(f.pages) == 0 {
		return commandaudits.Page{Items: make([]commandaudits.Action, 0)}, nil
	}
	page := f.pages[0]
	f.pages = f.pages[1:]
	return page, nil
}

func timePtr(value time.Time) *time.Time {
	return &value
}
