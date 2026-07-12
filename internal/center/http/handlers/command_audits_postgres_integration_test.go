package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/store"
	storemigrate "houfeng/internal/center/store/migrate"
)

func TestPostgresIntegrationCommandAuditHandlerCursorSurvivesPermanentCleanup(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryCommandAuditHandlerDatabase(t, ctx)
	if err := storemigrate.Apply(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	if _, err := db.Exec(ctx, `
		insert into users (user_id, username, password_hash, display_name)
		values ('usr_handler_audit', 'handler-admin', 'hash', 'Handler 管理员')
	`); err != nil {
		t.Fatalf("insert command audit actor: %v", err)
	}

	monitoringRepo := store.NewPostgresMonitoringInstanceRepository(db)
	record, err := monitoringRepo.CreateMonitoringInstance(ctx, monitoringinstances.CreateInput{
		DisplayName:     "Tokyo Handler Audit",
		Group:           "production",
		Region:          "ap-northeast",
		City:            "Tokyo",
		Provider:        "test",
		LifecycleStatus: monitoringinstances.LifecycleInUse,
		Labels:          []string{"command-audit"},
	})
	if err != nil {
		t.Fatalf("create monitoring instance: %v", err)
	}

	upperBound := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)
	startedAt := upperBound.Add(-time.Minute)
	for _, actionID := range []string{"act_handler_001", "act_handler_002", "act_handler_003"} {
		if err := monitoringRepo.QueueCommandAction(ctx, record.MonitoringInstanceID, monitoringinstances.QueueCommandActionInput{
			ActionID:    actionID,
			CommandID:   "uptime",
			Sensitivity: "standard",
			ActorUserID: "usr_handler_audit",
			Source:      monitoringinstances.CommandActionSourceWeb,
			QueuedAt:    startedAt,
		}); err != nil {
			t.Fatalf("queue %s: %v", actionID, err)
		}
	}
	if _, err := db.Exec(ctx, `delete from users where user_id = 'usr_handler_audit'`); err != nil {
		t.Fatalf("delete command audit actor: %v", err)
	}
	if _, err := db.Exec(ctx, `
		update monitoring_instances
		set archived_at = now(), archived_reason = 'handler integration cleanup'
		where monitoring_instance_id = $1
	`, record.MonitoringInstanceID); err != nil {
		t.Fatalf("archive monitoring instance: %v", err)
	}
	cleanup, err := monitoringRepo.PermanentCleanupMonitoringInstance(ctx, record.MonitoringInstanceID, monitoringinstances.PermanentCleanupInput{
		Reason:           "handler integration cleanup",
		ConfirmationName: "Tokyo Handler Audit",
	})
	if err != nil {
		t.Fatalf("permanent cleanup monitoring instance: %v", err)
	}
	if !cleanup.Deleted || cleanup.Counts.CommandActionAuditCount != 3 || cleanup.DeletedReferenceCount != 0 {
		t.Fatalf("cleanup = %#v, want three preserved command audits and no deleted audit references", cleanup)
	}

	handler := CommandAuditsWithOptions(
		store.NewPostgresCommandAuditRepository(db),
		CommandAuditOptions{Now: func() time.Time { return upperBound }},
	)
	params := url.Values{
		"window":       {"custom"},
		"started_from": {upperBound.Add(-time.Hour).Format(time.RFC3339)},
		"started_to":   {upperBound.Format(time.RFC3339)},
		"limit":        {"2"},
	}
	first, firstBody := servePostgresCommandAuditRequest(t, handler, "/api/command-audits?"+params.Encode())
	if len(first.Items) != 2 || first.Items[0].ID != "act_handler_003" || first.Items[1].ID != "act_handler_002" || first.NextCursor == "" {
		t.Fatalf("first handler page = %#v", first)
	}
	for _, item := range first.Items {
		if !item.MonitoringInstance.Deleted || item.MonitoringInstance.Name != "Tokyo Handler Audit" {
			t.Fatalf("deleted monitoring instance snapshot = %#v", item.MonitoringInstance)
		}
		if item.Actor == nil || item.Actor.Username != "handler-admin" || item.Actor.DisplayName != "Handler 管理员" {
			t.Fatalf("deleted actor snapshot = %#v", item.Actor)
		}
	}
	assertPostgresCommandAuditBodyIsMetadataOnly(t, firstBody)
	if _, err := db.Exec(ctx, `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, monitoring_instance_name_snapshot,
			command_id, sensitivity, event_type, source, occurred_at
		) values (
			'cmd_aud_handler_after_upper_bound', 'act_handler_after_upper_bound', $1, 'Tokyo Handler Audit',
			'uptime', 'standard', 'queued', 'web', $2
		)
	`, record.MonitoringInstanceID, upperBound.Add(time.Second)); err != nil {
		t.Fatalf("insert action after first cursor page: %v", err)
	}

	second, secondBody := servePostgresCommandAuditRequest(
		t,
		handler,
		"/api/command-audits?cursor="+url.QueryEscape(first.NextCursor),
	)
	if len(second.Items) != 1 || second.Items[0].ID != "act_handler_001" || second.NextCursor != "" {
		t.Fatalf("second handler page = %#v", second)
	}
	if strings.Contains(secondBody, "act_handler_after_upper_bound") {
		t.Fatalf("cursor continuation crossed its fixed upper bound: %s", secondBody)
	}
	assertPostgresCommandAuditBodyIsMetadataOnly(t, secondBody)
}

func servePostgresCommandAuditRequest(t *testing.T, handler http.Handler, target string) (commandAuditListResponse, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, body=%s", target, recorder.Code, recorder.Body.String())
	}
	var response commandAuditListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode GET %s response: %v", target, err)
	}
	return response, recorder.Body.String()
}

func assertPostgresCommandAuditBodyIsMetadataOnly(t *testing.T, body string) {
	t.Helper()
	lower := strings.ToLower(body)
	for _, forbidden := range []string{"stdout", "stderr", "details"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("command audit response leaked %q: %s", forbidden, body)
		}
	}
}

func openTemporaryCommandAuditHandlerDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required for command audit handler integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for command audit handler integration tests")
	}

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	databaseName := fmt.Sprintf("houfeng_cmd_audit_handler_%d_%d", time.Now().UnixNano(), os.Getpid())
	if !regexp.MustCompile(`^[a-z_][a-z0-9_]*$`).MatchString(databaseName) {
		t.Fatalf("unsafe generated database name %q", databaseName)
	}

	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open postgres admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)
	quotedDatabase := `"` + strings.ReplaceAll(databaseName, `"`, `""`) + `"`
	if _, err := adminPool.Exec(ctx, `create database `+quotedDatabase); err != nil {
		t.Fatalf("create temporary postgres database %q: %v", databaseName, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(dropCtx, `drop database if exists `+quotedDatabase+` with (force)`); err != nil {
			t.Errorf("drop temporary postgres database %q: %v", databaseName, err)
		}
	})

	testConfig := adminConfig.Copy()
	testConfig.ConnConfig.Database = databaseName
	testPool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("open temporary postgres database %q: %v", databaseName, err)
	}
	t.Cleanup(testPool.Close)
	if err := testPool.Ping(ctx); err != nil {
		t.Fatalf("ping temporary postgres database %q: %v", databaseName, err)
	}
	return testPool
}
