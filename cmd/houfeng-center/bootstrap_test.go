package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	centerapp "houfeng/internal/center/app"
	"houfeng/internal/center/attachments"
	"houfeng/internal/center/auth"
	"houfeng/internal/center/config"
	centerhttp "houfeng/internal/center/http"
	"houfeng/internal/center/http/sessionctx"
	incidentservice "houfeng/internal/center/incidents"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordsearch"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/targets"
)

func TestBootstrapCenterReturnsOpenPostgresError(t *testing.T) {
	cfg := config.CenterConfig{HTTPAddr: ":8080", WebDistDir: "web/dist", DatabaseURL: "postgres://center"}
	wantErr := errors.New("open boom")
	calledMigrate := false
	calledRouter := false
	calledApp := false

	app, cleanup, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
		openPostgres: func(context.Context, string) (postgresDB, error) {
			return nil, wantErr
		},
		applyMigrations: func(context.Context, postgresDB) error {
			calledMigrate = true
			return nil
		},
		newRouter: func(centerhttp.RouterOptions) http.Handler {
			calledRouter = true
			return http.NewServeMux()
		},
		newApp: func(string, http.Handler, ...centerapp.Worker) appRunner {
			calledApp = true
			return fakeApp{}
		},
	})
	if err == nil {
		t.Fatal("bootstrapCenter() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "open postgres") {
		t.Fatalf("bootstrapCenter() error = %q, want wrapped open postgres error", err)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("bootstrapCenter() error = %v, want wrapped %v", err, wantErr)
	}
	if app != nil {
		t.Fatal("bootstrapCenter() app != nil, want nil")
	}
	if cleanup != nil {
		t.Fatal("bootstrapCenter() cleanup != nil, want nil")
	}
	if calledMigrate || calledRouter || calledApp {
		t.Fatalf("downstream steps executed: migrate=%t router=%t app=%t", calledMigrate, calledRouter, calledApp)
	}
}

func TestBootstrapCenterClosesDBOnMigrationFailure(t *testing.T) {
	cfg := config.CenterConfig{HTTPAddr: ":8080", WebDistDir: "web/dist", DatabaseURL: "postgres://center"}
	db := &fakePostgresDB{}
	wantErr := errors.New("migrate boom")
	calledRouter := false
	calledApp := false

	app, cleanup, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
		openPostgres: func(context.Context, string) (postgresDB, error) {
			return db, nil
		},
		applyMigrations: func(context.Context, postgresDB) error {
			return wantErr
		},
		newRouter: func(centerhttp.RouterOptions) http.Handler {
			calledRouter = true
			return http.NewServeMux()
		},
		newApp: func(string, http.Handler, ...centerapp.Worker) appRunner {
			calledApp = true
			return fakeApp{}
		},
	})
	if err == nil {
		t.Fatal("bootstrapCenter() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "apply migrations") {
		t.Fatalf("bootstrapCenter() error = %q, want wrapped apply migrations error", err)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("bootstrapCenter() error = %v, want wrapped %v", err, wantErr)
	}
	if app != nil {
		t.Fatal("bootstrapCenter() app != nil, want nil")
	}
	if cleanup != nil {
		t.Fatal("bootstrapCenter() cleanup != nil, want nil")
	}
	if !db.closed {
		t.Fatal("bootstrapCenter() did not close DB on migration failure")
	}
	if calledRouter || calledApp {
		t.Fatalf("downstream steps executed: router=%t app=%t", calledRouter, calledApp)
	}
}

func TestBootstrapCenterRetainsMigrationPathWhenRecordPlatformIsLegacy(t *testing.T) {
	cfg := config.CenterConfig{HTTPAddr: ":8080", WebDistDir: "web/dist", DatabaseURL: "postgres://center"}
	db := &fakePostgresDB{}
	wantErr := errors.New("migrate boom")
	applyCalls := 0
	admitCalls := 0

	_, _, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
		openPostgres: func(context.Context, string) (postgresDB, error) {
			return db, nil
		},
		applyMigrations: func(context.Context, postgresDB) error {
			applyCalls++
			return wantErr
		},
		admitRuntime: func(context.Context, postgresDB) error {
			admitCalls++
			return nil
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("bootstrapCenter() error = %v, want wrapped migration failure", err)
	}
	if applyCalls != 1 {
		t.Fatalf("applyMigrations calls = %d, want 1", applyCalls)
	}
	if admitCalls != 0 {
		t.Fatalf("admitRuntime calls = %d, want 0", admitCalls)
	}
}

func TestBootstrapCenterUsesRuntimeAdmissionWhenRecordPlatformEnabled(t *testing.T) {
	cfg := config.CenterConfig{
		RecordPlatformMode: config.RecordPlatformModeRuntimeAdmission,
		HTTPAddr:           ":8080",
		WebDistDir:         "web/dist",
		DatabaseURL:        "postgres://center",
		SessionHMACKey:     []byte("0123456789abcdef0123456789abcdef"),
		Attachment: config.AttachmentConfig{
			BlobBackend:          attachments.BackendKindLocal,
			BlobRoot:             t.TempDir(),
			Limits:               attachments.DefaultLimits(),
			ProcessorMaxAttempts: 3,
		},
	}
	db := &fakePostgresDB{}
	applyCalls := 0
	admitCalls := 0
	searchGenerationCalls := 0
	var gotRouterOptions centerhttp.RouterOptions
	var gotWorkers []centerapp.Worker

	app, cleanup, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
		openPostgres: func(context.Context, string) (postgresDB, error) {
			return db, nil
		},
		applyMigrations: func(context.Context, postgresDB) error {
			applyCalls++
			return nil
		},
		admitRuntime: func(context.Context, postgresDB) error {
			admitCalls++
			return nil
		},
		ensureSearchGeneration: func(context.Context, postgresDB) error {
			searchGenerationCalls++
			return nil
		},
		seedInitialUser: func(context.Context, auth.UserRepository, config.CenterConfig) error {
			return nil
		},
		newSessionRepository: func(*pgxpool.Pool, []byte) (auth.SessionRepository, error) {
			return fakeSessionRepository{}, nil
		},
		newRouter: func(options centerhttp.RouterOptions) http.Handler {
			gotRouterOptions = options
			return http.NewServeMux()
		},
		newApp: func(_ string, _ http.Handler, workers ...centerapp.Worker) appRunner {
			gotWorkers = append([]centerapp.Worker(nil), workers...)
			return fakeApp{}
		},
	})
	if err != nil {
		t.Fatalf("bootstrapCenter() error = %v, want nil", err)
	}
	if app == nil || cleanup == nil {
		t.Fatal("bootstrapCenter() returned a nil app or cleanup, want both")
	}
	if applyCalls != 0 {
		t.Fatalf("applyMigrations calls = %d, want 0", applyCalls)
	}
	if admitCalls != 1 {
		t.Fatalf("admitRuntime calls = %d, want 1", admitCalls)
	}
	// Without a published generation the search projector writes nothing, so
	// bootstrap has to guarantee one exists before the first record commits.
	if searchGenerationCalls != 1 {
		t.Fatalf("ensureSearchGeneration calls = %d, want 1", searchGenerationCalls)
	}
	if len(gotWorkers) != 6 {
		t.Fatalf("runtime workers = %d, want evidence maintenance disabled until Child 10 supplies admission", len(gotWorkers))
	}
	// The projector only indexes commits, so records written before the index
	// existed stay invisible until a rebuild backfills them. That backfill has to
	// be a running worker, not a manual step.
	var rebuilder *recordsearch.RebuildWorker
	for _, worker := range gotWorkers {
		if candidate, ok := worker.(*recordsearch.RebuildWorker); ok {
			rebuilder = candidate
		}
	}
	if rebuilder == nil {
		t.Fatalf("runtime workers = %#v, want a record search rebuild worker", gotWorkers)
	}
	if !gotRouterOptions.RecordsEnabled || gotRouterOptions.RecordsHandler == nil ||
		gotRouterOptions.RecordWatchesHandler == nil || gotRouterOptions.RecordInboxHandler == nil ||
		gotRouterOptions.RecordDraftsHandler == nil || gotRouterOptions.RecordDeletionsHandler == nil ||
		gotRouterOptions.EvidenceHandler == nil || gotRouterOptions.AttachmentUploadsHandler == nil || gotRouterOptions.AttachmentsHandler == nil {
		t.Fatalf(
			"runtime Records router options = enabled:%t records:%v drafts:%v deletions:%v evidence:%v uploads:%v attachments:%v, want enabled and non-nil handlers",
			gotRouterOptions.RecordsEnabled,
			gotRouterOptions.RecordsHandler,
			gotRouterOptions.RecordDraftsHandler,
			gotRouterOptions.RecordDeletionsHandler,
			gotRouterOptions.EvidenceHandler,
			gotRouterOptions.AttachmentUploadsHandler,
			gotRouterOptions.AttachmentsHandler,
		)
	}
	if gotRouterOptions.VPSTimelineHandler == nil || gotRouterOptions.VPSExperienceLogsHandler == nil {
		t.Fatal("runtime admission removed legacy VPS timeline or experience handler")
	}
	actor := mustBootstrapRecordsActor(t)
	for _, handlerCase := range []struct {
		name     string
		method   string
		path     string
		body     string
		handler  http.Handler
		wantCode string
	}{
		{name: "records", method: http.MethodGet, path: "/api/records", handler: gotRouterOptions.RecordsHandler, wantCode: "record_service_unavailable"},
		{name: "watch", method: http.MethodGet, path: "/api/records/rec_httpcontract/watch", handler: gotRouterOptions.RecordWatchesHandler, wantCode: "record_service_unavailable"},
		{name: "inbox", method: http.MethodGet, path: "/api/record-notifications", handler: gotRouterOptions.RecordInboxHandler, wantCode: "record_service_unavailable"},
		{name: "drafts", method: http.MethodGet, path: "/api/record-drafts", handler: gotRouterOptions.RecordDraftsHandler, wantCode: "record_service_unavailable"},
		{name: "deletion preview", method: http.MethodPost, path: "/api/records/rec_httpcontract/permanent-delete-preview", handler: gotRouterOptions.RecordDeletionsHandler, wantCode: "deletion_safety_unavailable"},
		{name: "deletion status", method: http.MethodGet, path: "/api/record-deletions/rpo_httpcontract", handler: gotRouterOptions.RecordDeletionsHandler, wantCode: "deletion_status_unavailable"},
		{name: "evidence preview", method: http.MethodPost, path: "/api/evidence/capture-previews", body: `{}`, handler: gotRouterOptions.EvidenceHandler, wantCode: "evidence_service_unavailable"},
		{name: "evidence read", method: http.MethodGet, path: "/api/evidence/evs_httpcontract", handler: gotRouterOptions.EvidenceHandler, wantCode: "evidence_service_unavailable"},
		{name: "attachment upload", method: http.MethodPost, path: "/api/attachment-uploads", body: `{"draft_id":"rdf_httpcontract0001","display_name":"notes.txt","declared_size_bytes":4,"media_type":"text/plain"}`, handler: gotRouterOptions.AttachmentUploadsHandler, wantCode: "attachment_service_unavailable"},
	} {
		t.Run(handlerCase.name+" fails closed without transaction admission", func(t *testing.T) {
			request := httptest.NewRequest(handlerCase.method, handlerCase.path, strings.NewReader(handlerCase.body))
			if handlerCase.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
			recorder := httptest.NewRecorder()
			handlerCase.handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusServiceUnavailable ||
				!strings.Contains(recorder.Body.String(), `"code":"`+handlerCase.wantCode+`"`) {
				t.Fatalf("status = %d body=%s, want stable %s 503", recorder.Code, recorder.Body.String(), handlerCase.wantCode)
			}
			if strings.Contains(recorder.Body.String(), "drt1_") {
				t.Fatalf("fail-closed handler returned deletion token: %s", recorder.Body.String())
			}
		})
	}
	cleanup()
	if !db.closed {
		t.Fatal("cleanup() did not close DB")
	}
}

func TestBootstrapCenterClosesDBOnRuntimeAdmissionFailure(t *testing.T) {
	cfg := config.CenterConfig{
		RecordPlatformMode: config.RecordPlatformModeRuntimeAdmission,
		HTTPAddr:           ":8080",
		WebDistDir:         "web/dist",
		DatabaseURL:        "postgres://center",
	}
	db := &fakePostgresDB{}
	wantErr := errors.New("runtime admission boom")
	applyCalls := 0
	admitCalls := 0

	app, cleanup, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
		openPostgres: func(context.Context, string) (postgresDB, error) {
			return db, nil
		},
		applyMigrations: func(context.Context, postgresDB) error {
			applyCalls++
			return nil
		},
		admitRuntime: func(context.Context, postgresDB) error {
			admitCalls++
			return wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("bootstrapCenter() error = %v, want wrapped runtime admission failure", err)
	}
	if app != nil || cleanup != nil {
		t.Fatal("bootstrapCenter() returned an app or cleanup, want neither")
	}
	if applyCalls != 0 {
		t.Fatalf("applyMigrations calls = %d, want 0", applyCalls)
	}
	if admitCalls != 1 {
		t.Fatalf("admitRuntime calls = %d, want 1", admitCalls)
	}
	if !db.closed {
		t.Fatal("bootstrapCenter() did not close DB on runtime admission failure")
	}
}

func TestBootstrapCenterDefaultRuntimeAdmissionFailsClosed(t *testing.T) {
	cfg := config.CenterConfig{
		RecordPlatformMode: config.RecordPlatformModeRuntimeAdmission,
		HTTPAddr:           ":8080",
		WebDistDir:         "web/dist",
		DatabaseURL:        "postgres://center",
	}
	db := &fakePostgresDB{}
	applyCalls := 0
	deps := (bootstrapDeps{}).withDefaults()
	deps.openPostgres = func(context.Context, string) (postgresDB, error) {
		return db, nil
	}
	deps.applyMigrations = func(context.Context, postgresDB) error {
		applyCalls++
		return nil
	}

	_, _, err := bootstrapCenter(context.Background(), cfg, "dev", deps)
	if err == nil || !strings.Contains(err.Error(), "current app ACL runtime admission has no PostgreSQL pool") {
		t.Fatalf("bootstrapCenter() error = %v, want default current app ACL runtime admission error", err)
	}
	if applyCalls != 0 {
		t.Fatalf("applyMigrations calls = %d, want 0", applyCalls)
	}
	if !db.closed {
		t.Fatal("bootstrapCenter() did not close DB after default runtime admission failure")
	}
}

func TestBootstrapCenterBuildsAppOnSuccess(t *testing.T) {
	cfg := config.CenterConfig{
		HTTPAddr:         ":8080",
		WebDistDir:       "web/dist",
		DatabaseURL:      "postgres://center",
		TelegramBotToken: "seed-bot-token",
		TelegramChatID:   "seed-chat-id",
		TrustedProxies:   []string{"10.0.0.0/8"},
		SessionHMACKey:   []byte("0123456789abcdef0123456789abcdef"),
	}
	db := &fakePostgresDB{}
	app := fakeApp{}
	var gotOpts centerhttp.RouterOptions
	var gotAddr string
	var gotHandler http.Handler
	var gotNotifierCfg config.CenterConfig
	var gotSettingsRepo centersettings.Repository
	var gotSessionHMACKey []byte

	builtApp, cleanup, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
		openPostgres: func(context.Context, string) (postgresDB, error) {
			return db, nil
		},
		applyMigrations: func(context.Context, postgresDB) error {
			return nil
		},
		ensureSearchGeneration: func(context.Context, postgresDB) error {
			return nil
		},
		seedInitialUser: func(context.Context, auth.UserRepository, config.CenterConfig) error {
			return nil
		},
		newSessionRepository: func(_ *pgxpool.Pool, key []byte) (auth.SessionRepository, error) {
			gotSessionHMACKey = append([]byte(nil), key...)
			return fakeSessionRepository{}, nil
		},
		newIncidentNotifier: func(inputCfg config.CenterConfig, repo centersettings.Repository) incidentservice.Notifier {
			gotNotifierCfg = inputCfg
			gotSettingsRepo = repo
			return &fakeIncidentNotifier{}
		},
		newRouter: func(opts centerhttp.RouterOptions) http.Handler {
			gotOpts = opts
			return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
		},
		newApp: func(addr string, handler http.Handler, workers ...centerapp.Worker) appRunner {
			gotAddr = addr
			gotHandler = handler
			if len(workers) != 5 {
				t.Fatalf("len(workers) = %d, want 5", len(workers))
			}
			for i, worker := range workers {
				if worker == nil {
					t.Fatalf("workers[%d] = nil, want non-nil", i)
				}
				// There is no index to rebuild while the records platform is off,
				// and a worker polling those tables would fail every pass.
				if _, ok := worker.(*recordsearch.RebuildWorker); ok {
					t.Fatalf("workers[%d] is a record search rebuild worker, want none while records are disabled", i)
				}
			}
			return app
		},
	})
	if err != nil {
		t.Fatalf("bootstrapCenter() error = %v, want nil", err)
	}
	if builtApp != app {
		t.Fatal("bootstrapCenter() did not return constructed app")
	}
	if cleanup == nil {
		t.Fatal("bootstrapCenter() cleanup = nil, want non-nil")
	}
	if gotOpts.Version != "dev" {
		t.Fatalf("router version = %q, want %q", gotOpts.Version, "dev")
	}
	if gotOpts.WebDistDir != cfg.WebDistDir {
		t.Fatalf("router webDistDir = %q, want %q", gotOpts.WebDistDir, cfg.WebDistDir)
	}
	if gotOpts.DashboardHandler == nil {
		t.Fatal("router dashboard handler = nil, want non-nil")
	}
	if gotOpts.EventsHandler == nil {
		t.Fatal("router events handler = nil, want non-nil")
	}
	if gotOpts.CommandAuditsHandler == nil {
		t.Fatal("router command audits handler = nil, want non-nil")
	}
	if gotOpts.IncidentsHandler == nil {
		t.Fatal("router incidents handler = nil, want non-nil")
	}
	if gotOpts.SettingsHandler == nil {
		t.Fatal("router settings handler = nil, want non-nil")
	}
	if gotOpts.RecordsEnabled || gotOpts.RecordsHandler != nil || gotOpts.RecordDraftsHandler != nil ||
		gotOpts.RecordDeletionsHandler != nil || gotOpts.EvidenceHandler != nil || gotOpts.AttachmentUploadsHandler != nil ||
		gotOpts.AttachmentsHandler != nil {
		t.Fatalf(
			"legacy Records router options = enabled:%t records:%v drafts:%v deletions:%v evidence:%v uploads:%v attachments:%v, want disabled and nil handlers",
			gotOpts.RecordsEnabled,
			gotOpts.RecordsHandler,
			gotOpts.RecordDraftsHandler,
			gotOpts.RecordDeletionsHandler,
			gotOpts.EvidenceHandler,
			gotOpts.AttachmentUploadsHandler,
			gotOpts.AttachmentsHandler,
		)
	}
	if gotOpts.AssetDomainsCollectionHandler == nil {
		t.Fatal("router asset domains collection handler = nil, want non-nil")
	}
	if gotOpts.AssetServicesCollectionHandler == nil {
		t.Fatal("router asset services collection handler = nil, want non-nil")
	}
	if gotOpts.AssetDecisionOverviewHandler == nil {
		t.Fatal("router asset decision overview handler = nil, want non-nil")
	}
	if gotOpts.AssetDecisionGroupsHandler == nil {
		t.Fatal("router asset decision groups handler = nil, want non-nil")
	}
	if gotOpts.AssetDecisionGroupHandler == nil {
		t.Fatal("router asset decision group handler = nil, want non-nil")
	}
	if gotOpts.AssetDecisionManualGroupsHandler == nil {
		t.Fatal("router asset decision manual groups handler = nil, want non-nil")
	}
	if gotOpts.AssetDecisionManualGroupHandler == nil {
		t.Fatal("router asset decision manual group handler = nil, want non-nil")
	}
	if gotOpts.AssetDecisionScenarioTemplatesHandler == nil {
		t.Fatal("router asset decision scenario templates handler = nil, want non-nil")
	}
	if gotOpts.AssetDecisionScenarioTemplateHandler == nil {
		t.Fatal("router asset decision scenario template handler = nil, want non-nil")
	}
	if gotOpts.AssetDecisionRecordsHandler == nil {
		t.Fatal("router asset decision records handler = nil, want non-nil")
	}
	if gotOpts.AssetDecisionRecordHandler == nil {
		t.Fatal("router asset decision record handler = nil, want non-nil")
	}
	if gotOpts.ProvidersCollectionHandler == nil {
		t.Fatal("router providers collection handler = nil, want non-nil")
	}
	if gotOpts.ProviderItemHandler == nil {
		t.Fatal("router provider item handler = nil, want non-nil")
	}
	if gotOpts.VPSCollectionHandler == nil {
		t.Fatal("router vps collection handler = nil, want non-nil")
	}
	if gotOpts.VPSItemHandler == nil {
		t.Fatal("router vps item handler = nil, want non-nil")
	}
	if gotOpts.VPSMonitoringInstancesHandler == nil {
		t.Fatal("router vps monitoringInstances handler = nil, want non-nil")
	}
	if gotOpts.VPSSubscriptionsHandler == nil {
		t.Fatal("router vps subscriptions handler = nil, want non-nil")
	}
	if gotOpts.VPSLinkMonitoringInstanceHandler == nil {
		t.Fatal("router vps link monitoringInstance handler = nil, want non-nil")
	}
	if gotOpts.VPSUnlinkMonitoringInstanceHandler == nil {
		t.Fatal("router vps unlink monitoringInstance handler = nil, want non-nil")
	}
	if gotOpts.VPSTimelineHandler == nil {
		t.Fatal("router vps timeline handler = nil, want non-nil")
	}
	if gotOpts.VPSExperienceLogsHandler == nil {
		t.Fatal("router vps experience logs handler = nil, want non-nil")
	}
	if gotOpts.VPSDomainsHandler == nil {
		t.Fatal("router vps domains handler = nil, want non-nil")
	}
	if gotOpts.VPSServicesHandler == nil {
		t.Fatal("router vps services handler = nil, want non-nil")
	}
	if gotOpts.VPSIPQualityHandler == nil {
		t.Fatal("router vps ip quality handler = nil, want non-nil")
	}
	if gotOpts.VPSCancellationPreviewHandler == nil {
		t.Fatal("router vps cancellation preview handler = nil, want non-nil")
	}
	if gotOpts.VPSCancellationHandler == nil {
		t.Fatal("router vps cancellation handler = nil, want non-nil")
	}
	if gotOpts.VPSExtendValidityHandler == nil {
		t.Fatal("router vps extend validity handler = nil, want non-nil")
	}
	if gotOpts.VPSArchiveReviewHandler == nil {
		t.Fatal("router vps archive review handler = nil, want non-nil")
	}
	if gotOpts.VPSArchiveHandler == nil {
		t.Fatal("router vps archive handler = nil, want non-nil")
	}
	if gotOpts.VPSRestoreFromArchiveHandler == nil {
		t.Fatal("router vps restore from archive handler = nil, want non-nil")
	}
	if gotOpts.AssetContextTargetsHandler == nil {
		t.Fatal("router asset context targets handler = nil, want non-nil")
	}
	if gotOpts.SubscriptionsCollectionHandler == nil {
		t.Fatal("router subscriptions collection handler = nil, want non-nil")
	}
	if gotOpts.SubscriptionItemHandler == nil {
		t.Fatal("router subscription item handler = nil, want non-nil")
	}
	if gotOpts.SubscriptionMonthlyBudgetsHandler == nil {
		t.Fatal("router subscription monthly budgets handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstancesCollectionHandler == nil {
		t.Fatal("router monitoringInstances collection handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceItemHandler == nil {
		t.Fatal("router monitoringInstance item handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceVPSHandler == nil {
		t.Fatal("router monitoringInstance vps handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceRuntimeFactsHandler == nil {
		t.Fatal("router monitoringInstance runtime facts handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceRuntimeStreamHandler == nil {
		t.Fatal("router monitoringInstance runtime stream handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceRuntimeControlHandler == nil {
		t.Fatal("router monitoringInstance runtime control handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceManagementReviewHandler == nil {
		t.Fatal("router monitoringInstance management review handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceLifecycleRetireHandler == nil {
		t.Fatal("router monitoringInstance lifecycle retire handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceLifecycleRestoreHandler == nil {
		t.Fatal("router monitoringInstance lifecycle restore handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceArchiveHandler == nil {
		t.Fatal("router monitoringInstance archive handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceRestoreFromArchiveHandler == nil {
		t.Fatal("router monitoringInstance restore from archive handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstancePermanentCleanupHandler == nil {
		t.Fatal("router monitoringInstance permanent cleanup handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceOnboardingHandler == nil {
		t.Fatal("router monitoringInstance onboarding handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceEnrollmentTokenHandler == nil {
		t.Fatal("router monitoringInstance enrollment token handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceInstallCommandHandler == nil {
		t.Fatal("router monitoringInstance install command handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceBindingConfirmRebindHandler == nil {
		t.Fatal("router monitoringInstance binding confirm rebind handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceBindingRejectPendingHandler == nil {
		t.Fatal("router monitoringInstance binding reject pending handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceBindingResetHandler == nil {
		t.Fatal("router monitoringInstance binding reset handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceSparklinesHandler == nil {
		t.Fatal("router monitoringInstance sparklines handler = nil, want non-nil")
	}
	if gotOpts.MonitoringInstanceActionsHandler == nil {
		t.Fatal("router monitoringInstance actions handler = nil, want non-nil")
	}
	if gotOpts.TargetsCollectionHandler == nil {
		t.Fatal("router targets collection handler = nil, want non-nil")
	}
	if gotOpts.TargetItemHandler == nil {
		t.Fatal("router target item handler = nil, want non-nil")
	}
	if gotOpts.TargetProbeItemsHandler == nil {
		t.Fatal("router target probe items handler = nil, want non-nil")
	}
	if gotOpts.TargetRuntimeFactsHandler == nil {
		t.Fatal("router target runtime facts handler = nil, want non-nil")
	}
	if gotOpts.TargetRuntimeControlHandler == nil {
		t.Fatal("router target runtime control handler = nil, want non-nil")
	}
	if gotOpts.TargetSparklinesHandler == nil {
		t.Fatal("router target sparklines handler = nil, want non-nil")
	}
	if gotOpts.AgentEnrollHandler == nil {
		t.Fatal("router agent enroll handler = nil, want non-nil")
	}
	if gotOpts.AgentSyncHandler == nil {
		t.Fatal("router agent sync handler = nil, want non-nil")
	}
	if gotOpts.InstallerScriptHandler == nil {
		t.Fatal("router installer script handler = nil, want non-nil")
	}
	if gotOpts.AuthMiddleware == nil {
		t.Fatal("router auth middleware = nil, want non-nil")
	}
	if gotAddr != cfg.HTTPAddr {
		t.Fatalf("app addr = %q, want %q", gotAddr, cfg.HTTPAddr)
	}
	if gotHandler == nil {
		t.Fatal("app handler = nil, want non-nil")
	}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	gotHandler.ServeHTTP(recorder, req)
	if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security header missing from app handler")
	}

	recorder = httptest.NewRecorder()
	protected := gotOpts.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req = httptest.NewRequest(http.MethodPost, "https://center.example.com/api/settings", nil)
	req.Header.Set("Origin", "https://evil.example.net")
	protected.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-site unsafe request status = %d, want 403", recorder.Code)
	}
	if gotNotifierCfg.TelegramBotToken != cfg.TelegramBotToken {
		t.Fatalf("notifier seed bot token = %q, want %q", gotNotifierCfg.TelegramBotToken, cfg.TelegramBotToken)
	}
	if gotNotifierCfg.TelegramChatID != cfg.TelegramChatID {
		t.Fatalf("notifier seed chat id = %q, want %q", gotNotifierCfg.TelegramChatID, cfg.TelegramChatID)
	}
	if gotSettingsRepo == nil {
		t.Fatal("settings repo = nil, want runtime settings repository for notifier wiring")
	}
	if string(gotSessionHMACKey) != string(cfg.SessionHMACKey) {
		t.Fatalf("session HMAC key = %q, want configured key", string(gotSessionHMACKey))
	}
	if db.closed {
		t.Fatal("DB closed before cleanup")
	}

	cleanup()
	if !db.closed {
		t.Fatal("cleanup() did not close DB")
	}
}

func mustBootstrapRecordsActor(t *testing.T) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    "usr_0123456789abcdef01234567",
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
		GroupIDs:  make([]string, 0),
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

func TestBootstrapDefaultSeedInitialUserUsesConfiguredBcryptCost(t *testing.T) {
	users := authTestUserRepository{}
	cfg := config.CenterConfig{
		InitialUsername:    strings.Join([]string{"fixture", "operator"}, "-"),
		InitialPassword:    strings.Join([]string{"fixture", "credential", "2026!"}, "-"),
		PasswordBcryptCost: bcrypt.MinCost,
	}

	deps := bootstrapDeps{}.withDefaults()
	if err := deps.seedInitialUser(context.Background(), users, cfg); err != nil {
		t.Fatalf("seedInitialUser: %v", err)
	}

	user, err := users.FindByUsername(context.Background(), cfg.InitialUsername)
	if err != nil {
		t.Fatalf("FindByUsername: %v", err)
	}
	got, err := bcrypt.Cost([]byte(user.PasswordHash))
	if err != nil {
		t.Fatalf("bcrypt.Cost: %v", err)
	}
	if got != bcrypt.MinCost {
		t.Fatalf("seed bcrypt cost = %d, want %d", got, bcrypt.MinCost)
	}
}

func TestBootstrapAuthServiceUsesConfiguredBcryptCost(t *testing.T) {
	body, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatalf("read bootstrap.go: %v", err)
	}

	if !strings.Contains(string(body), "PasswordBcryptCost: cfg.PasswordBcryptCost") {
		t.Fatal("bootstrap auth.New options must pass cfg.PasswordBcryptCost")
	}
}

func TestBootstrapAgentTokenRepositoriesUseConfiguredHMACKey(t *testing.T) {
	body, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatalf("read bootstrap.go: %v", err)
	}
	source := string(body)

	for _, want := range []string{
		"store.NewPostgresMonitoringInstanceRepositoryWithTokenHMACKey(db.Pool(), cfg.SessionHMACKey)",
		"store.NewPostgresSyncRepositoryWithTokenHMACKey(db.Pool(), cfg.SessionHMACKey)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("bootstrap.go missing configured agent token HMAC wiring %q", want)
		}
	}
}

func TestBootstrapWiresRequiredRecordAuthorizationScopeRepositoryWithoutFallback(t *testing.T) {
	body, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatalf("read bootstrap.go: %v", err)
	}
	source := string(body)

	for _, want := range []string{
		"scopeRepo := store.NewPostgresRecordAuthorizationRepository(db.Pool())",
		"centerhttp.RequireSession(authSvc, scopeRepo)(next)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("bootstrap.go missing required record authorization wiring %q", want)
		}
	}
	for _, forbidden := range []string{
		"RequireSession(authSvc)(next)",
		"if scopeRepo == nil",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("bootstrap.go retains optional record authorization fallback %q", forbidden)
		}
	}
}

func TestBootstrapRegistersRecordAttachmentRevisionParticipant(t *testing.T) {
	body, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatalf("read bootstrap.go: %v", err)
	}
	source := string(body)
	if !strings.Contains(source, "store.NewRecordAttachmentRevisionParticipant()") {
		t.Fatal("bootstrap.go does not register the attachment revision participant")
	}
	if strings.Contains(source, "store.NewPostgresRecordRepository(pool, nil, nil)") {
		t.Fatal("bootstrap.go still constructs the Records repository without participants")
	}
}

func TestBootstrapRegistersRecordCollaborationRevisionParticipantWithoutAdmissionFallback(t *testing.T) {
	body, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatalf("read bootstrap.go: %v", err)
	}
	source := string(body)
	for _, required := range []string{
		"store.NewCollaborationRevisionParticipant(",
		"store.NewPostgresCollaborationMembershipReader()",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("bootstrap.go missing collaboration revision wiring %q", required)
		}
	}
	for _, forbidden := range []string{
		"store.AdmissionGateFunc(",
		"NewCollaborationRevisionParticipant(nil)",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("bootstrap.go contains collaboration admission fallback %q", forbidden)
		}
	}
}

// Search must reuse the record read service and the injected admission gate. A
// second read path would become a second authorization decision, and a bypassed
// gate would let the index answer while the record surface is closed.
func TestBootstrapWiresRecordSearchThroughSharedReadServiceAndAdmission(t *testing.T) {
	source, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{
		"recordsearch.NewService(store.NewPostgresRecordSearchStore(pool, effectiveGate), readService)",
		"handlers.RecordSearch(searchService)",
		"RecordSearchHandler:",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("bootstrap missing record search wiring %q", required)
		}
	}
	for _, forbidden := range []string{
		"NewPostgresRecordSearchStore(pool, nil)",
		"NewPostgresRecordSearchStore(pool, store.AdmissionGateFunc",
		"NewPostgresRecordSearchStore(pool, allow",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("bootstrap contains forbidden record search admission bypass %q", forbidden)
		}
	}
}

func TestBootstrapWiresRecordActionsThroughSharedAuthorizationMembershipAndAdmission(t *testing.T) {
	source, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{
		"store.NewPostgresRecordActionRepository(pool, effectiveGate, collaborationMembers, authorizations)",
		"recordcollaboration.NewActionService(authorizations, actionRepository)",
		"recordcollaboration.NewActionApplication(",
		"handlers.RecordActions(actionApplication)",
		"RecordActionsHandler:",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("bootstrap missing record action wiring %q", required)
		}
	}
	for _, forbidden := range []string{
		"NewPostgresRecordActionRepository(pool, store.AdmissionGateFunc",
		"NewPostgresRecordActionRepository(pool, allow",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("bootstrap contains forbidden record action admission bypass %q", forbidden)
		}
	}
}

func TestBootstrapWiresRecordCommentsThroughSharedAuthorizationMembershipAndAdmission(t *testing.T) {
	source, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{
		"store.NewPostgresRecordCommentRepository(pool, effectiveGate, collaborationMembers, authorizations)",
		"recordcollaboration.NewCommentService(authorizations, commentRepository)",
		"recordcollaboration.NewCommentApplication(",
		"handlers.RecordComments(commentApplication)",
		"RecordCommentsHandler:",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("bootstrap missing record comment wiring %q", required)
		}
	}
	for _, forbidden := range []string{
		"NewPostgresRecordCommentRepository(pool, store.AdmissionGateFunc",
		"NewPostgresRecordCommentRepository(pool, allow",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("bootstrap contains forbidden record comment admission bypass %q", forbidden)
		}
	}
}

func TestBootstrapWiresRecordWatchesInboxAndProjectionThroughSharedFailClosedDependencies(t *testing.T) {
	source, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{
		"store.NewPostgresRecordWatchRepository(pool, effectiveGate, collaborationMembers, authorizations)",
		"recordcollaboration.NewWatchService(authorizations, watchRepository)",
		"handlers.RecordWatches(watchApplication)",
		"store.NewPostgresRecordNotificationRepository(",
		"recordcollaboration.NewNotificationProjector(",
		"recordcollaboration.NewNotificationProjectionWorker(",
		"handlers.RecordInbox(notificationRepository)",
		"RecordWatchesHandler:",
		"RecordInboxHandler:",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("bootstrap missing record notification wiring %q", required)
		}
	}
	for _, forbidden := range []string{
		"NewPostgresRecordWatchRepository(pool, store.AdmissionGateFunc",
		"NewPostgresRecordNotificationRepository(pool, store.AdmissionGateFunc",
		"NewPostgresRecordNotificationRepositoryWithExternalBindings(",
		"NewNotificationProjectorWithExternalDelivery(",
		"NewScopedExternalDeliveryProcessor(",
		"NewOutboxWorker(",
		"OutboxSender",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("bootstrap contains forbidden notification fallback/delivery primitive %q", forbidden)
		}
	}
}

func TestBootstrapWiresConfiguredAttachmentServices(t *testing.T) {
	body, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatalf("read bootstrap.go: %v", err)
	}
	source := string(body)
	for _, required := range []string{
		"store.NewPostgresAttachmentRepository",
		"attachments.NewUploadService",
		"attachments.NewDownloadService",
		"handlers.AttachmentUploads(uploadService)",
		"handlers.AttachmentsWithOptions(downloadService)",
		"newArchiveScannerReadiness",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("bootstrap.go does not contain configured attachment wiring %q", required)
		}
	}
	for _, forbidden := range []string{"handlers.AttachmentUploads(nil)", "handlers.Attachments()"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("bootstrap.go retains fail-closed placeholder %q", forbidden)
		}
	}
}

func TestArchiveScannerReadinessUsesLiveFunctionalProbe(t *testing.T) {
	if readiness := newArchiveScannerReadiness(nil); readiness != nil {
		t.Fatal("newArchiveScannerReadiness(nil) must leave archive admission fail closed")
	}
	probeCalls := 0
	readiness := newArchiveScannerReadiness(attachments.ProcessorScanner(func(_ context.Context, source io.Reader) (attachments.ProcessorResultCode, error) {
		probeCalls++
		body, err := io.ReadAll(source)
		if err != nil {
			t.Fatalf("read readiness probe: %v", err)
		}
		if len(body) != 0 {
			t.Fatalf("readiness probe bytes = %d, want empty functional probe", len(body))
		}
		return attachments.ProcessorResultCodeClean, nil
	}))
	status, err := readiness(context.Background())
	if err != nil || status != attachments.ScannerStatusHealthy || probeCalls != 1 {
		t.Fatalf("readiness() = (%q, %v), calls %d, want healthy functional probe", status, err, probeCalls)
	}

	readiness = newArchiveScannerReadiness(attachments.ProcessorScanner(func(context.Context, io.Reader) (attachments.ProcessorResultCode, error) {
		return "", errors.New("daemon detail that must stay internal")
	}))
	status, err = readiness(context.Background())
	if err != nil || status != attachments.ScannerStatusUnhealthy {
		t.Fatalf("unhealthy readiness() = (%q, %v), want content-free unhealthy status", status, err)
	}
}

func TestBootstrapWiresSeparateCommandAuditReadRepositoryAndHandler(t *testing.T) {
	body, err := os.ReadFile("bootstrap.go")
	if err != nil {
		t.Fatalf("read bootstrap.go: %v", err)
	}
	source := string(body)
	for _, want := range []string{
		"commandAuditRepo := store.NewPostgresCommandAuditRepository(db.Pool())",
		"CommandAuditsHandler:",
		"handlers.CommandAudits(commandAuditRepo)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("bootstrap.go missing command audit wiring %q", want)
		}
	}
}

func TestSettingsPresentationRepositoryReturnsEffectiveFreshInstallSettings(t *testing.T) {
	repo := &fakeCenterSettingsRepository{
		getSettingsResult: centersettings.Default(),
	}

	got, err := (settingsPresentationRepository{
		repo:                  repo,
		queryer:               fakeSettingsQueryer{scanErr: pgx.ErrNoRows},
		incidentSweepInterval: 90 * time.Second,
	}).GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings() error = %v", err)
	}

	if got.IncidentDefaults.SweepIntervalSeconds != 90 {
		t.Fatalf("SweepIntervalSeconds = %d, want %d", got.IncidentDefaults.SweepIntervalSeconds, 90)
	}
	if len(got.OverrideRules.MonitoringInstanceLabels) != 1 {
		t.Fatalf("len(MonitoringInstanceLabelOverrides) = %d, want 1", len(got.OverrideRules.MonitoringInstanceLabels))
	}
	if got.OverrideRules.MonitoringInstanceLabels[0].Label != "核心" {
		t.Fatalf("MonitoringInstanceLabelOverrides[0].Label = %q, want %q", got.OverrideRules.MonitoringInstanceLabels[0].Label, "核心")
	}
	if got.OverrideRules.MonitoringInstanceLabels[0].Overrides.HostSampleFrequencyTier == nil {
		t.Fatal("HostSampleFrequencyTier override = nil, want legacy core override")
	}
	if *got.OverrideRules.MonitoringInstanceLabels[0].Overrides.HostSampleFrequencyTier != targets.FrequencyTier5s {
		t.Fatalf(
			"HostSampleFrequencyTier override = %q, want %q",
			*got.OverrideRules.MonitoringInstanceLabels[0].Overrides.HostSampleFrequencyTier,
			targets.FrequencyTier5s,
		)
	}
}

func TestSettingsPresentationRepositoryReturnsPersistedSettingsUnchanged(t *testing.T) {
	record := centersettings.Default()
	record.HostSampleFrequencyTier = targets.FrequencyTier15m
	record.IncidentDefaults.SweepIntervalSeconds = 180

	got, err := (settingsPresentationRepository{
		repo:                  &fakeCenterSettingsRepository{getSettingsResult: record},
		queryer:               fakeSettingsQueryer{},
		incidentSweepInterval: 45 * time.Second,
	}).GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings() error = %v", err)
	}

	if got.HostSampleFrequencyTier != targets.FrequencyTier15m {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", got.HostSampleFrequencyTier, targets.FrequencyTier15m)
	}
	if got.IncidentDefaults.SweepIntervalSeconds != 180 {
		t.Fatalf("SweepIntervalSeconds = %d, want %d", got.IncidentDefaults.SweepIntervalSeconds, 180)
	}
	if len(got.OverrideRules.MonitoringInstanceLabels) != 0 {
		t.Fatalf("len(MonitoringInstanceLabelOverrides) = %d, want 0", len(got.OverrideRules.MonitoringInstanceLabels))
	}
}

func TestSettingsPresentationRepositoryDelegatesPutSettings(t *testing.T) {
	input := centersettings.Default()
	input.RetentionPolicy.RawLayerDays = 30
	repo := &fakeCenterSettingsRepository{putSettingsResult: input}

	got, err := (settingsPresentationRepository{repo: repo}).PutSettings(context.Background(), input)
	if err != nil {
		t.Fatalf("PutSettings() error = %v", err)
	}
	if repo.putSettingsInput.RetentionPolicy.RawLayerDays != 30 {
		t.Fatalf(
			"delegated RawLayerDays = %d, want %d",
			repo.putSettingsInput.RetentionPolicy.RawLayerDays,
			30,
		)
	}
	if got.RetentionPolicy.RawLayerDays != 30 {
		t.Fatalf("returned RawLayerDays = %d, want %d", got.RetentionPolicy.RawLayerDays, 30)
	}
}

func TestEnsureLegacyCoreHostSampleOverrideAugmentsExistingCoreRule(t *testing.T) {
	rules := []centersettings.MonitoringInstanceLabelOverrideRule{{
		Label: "核心",
		Overrides: centersettings.SettingsOverrideFields{
			ProbeFrequencyDefaults: &centersettings.ProbeFrequencyOverride{
				HTTP: stringPtr(targets.FrequencyTier15m),
			},
		},
	}}

	got := ensureLegacyCoreHostSampleOverride(rules)

	if len(got) != 1 {
		t.Fatalf("len(rules) = %d, want 1", len(got))
	}
	if got[0].Overrides.HostSampleFrequencyTier == nil {
		t.Fatal("HostSampleFrequencyTier override = nil, want legacy core override added in-place")
	}
	if *got[0].Overrides.HostSampleFrequencyTier != targets.FrequencyTier5s {
		t.Fatalf(
			"HostSampleFrequencyTier override = %q, want %q",
			*got[0].Overrides.HostSampleFrequencyTier,
			targets.FrequencyTier5s,
		)
	}
	if got[0].Overrides.ProbeFrequencyDefaults == nil || got[0].Overrides.ProbeFrequencyDefaults.HTTP == nil {
		t.Fatal("ProbeFrequencyDefaults.HTTP = nil, want existing override fields preserved")
	}
	if *got[0].Overrides.ProbeFrequencyDefaults.HTTP != targets.FrequencyTier15m {
		t.Fatalf(
			"ProbeFrequencyDefaults.HTTP = %q, want %q",
			*got[0].Overrides.ProbeFrequencyDefaults.HTTP,
			targets.FrequencyTier15m,
		)
	}
}

type fakePostgresDB struct {
	closed bool
}

func (f *fakePostgresDB) Close() {
	f.closed = true
}

func (f *fakePostgresDB) Pool() *pgxpool.Pool {
	return nil
}

type fakeSettingsQueryer struct {
	scanErr error
}

func (f fakeSettingsQueryer) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakePGXRow{scanErr: f.scanErr}
}

type fakePGXRow struct {
	scanErr error
}

func (r fakePGXRow) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if len(dest) > 0 {
		if value, ok := dest[0].(*int); ok {
			*value = 1
		}
	}
	return nil
}

type fakeCenterSettingsRepository struct {
	getSettingsResult centersettings.CenterSettings
	getSettingsErr    error
	putSettingsInput  centersettings.CenterSettings
	putSettingsResult centersettings.CenterSettings
	putSettingsErr    error
}

func (f *fakeCenterSettingsRepository) GetSettings(context.Context) (centersettings.CenterSettings, error) {
	if f.getSettingsErr != nil {
		return centersettings.CenterSettings{}, f.getSettingsErr
	}
	return f.getSettingsResult, nil
}

func (f *fakeCenterSettingsRepository) PutSettings(_ context.Context, input centersettings.CenterSettings) (centersettings.CenterSettings, error) {
	f.putSettingsInput = input
	if f.putSettingsErr != nil {
		return centersettings.CenterSettings{}, f.putSettingsErr
	}
	return f.putSettingsResult, nil
}

func stringPtr(value string) *string {
	return &value
}

type fakeApp struct{}

func (fakeApp) Run(context.Context) error {
	return nil
}

type fakeIncidentNotifier struct{}

func (*fakeIncidentNotifier) Send(context.Context, string) error {
	return nil
}

type fakeSessionRepository struct{}

func (fakeSessionRepository) Create(context.Context, auth.Session) error {
	return nil
}

func (fakeSessionRepository) Find(context.Context, string) (auth.Session, error) {
	return auth.Session{}, auth.ErrSessionNotFound
}

func (fakeSessionRepository) RefreshExpires(context.Context, string, time.Time, time.Time) error {
	return nil
}

func (fakeSessionRepository) Delete(context.Context, string) error {
	return nil
}

func (fakeSessionRepository) DeleteByUserID(context.Context, string, string) error {
	return nil
}

func (fakeSessionRepository) DeleteExpiredBefore(context.Context, time.Time) (int, error) {
	return 0, nil
}

type authTestUserRepository map[string]auth.User

func (r authTestUserRepository) Create(_ context.Context, user auth.User) error {
	r[user.Username] = user
	return nil
}

func (r authTestUserRepository) FindByUsername(_ context.Context, username string) (auth.User, error) {
	user, ok := r[username]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return user, nil
}

func (r authTestUserRepository) FindByID(_ context.Context, userID string) (auth.User, error) {
	for _, user := range r {
		if user.UserID == userID {
			return user, nil
		}
	}
	return auth.User{}, auth.ErrUserNotFound
}

func (r authTestUserRepository) UpdatePassword(_ context.Context, userID, newHash string, changedAt time.Time) error {
	for username, user := range r {
		if user.UserID == userID {
			user.PasswordHash = newHash
			user.PasswordChangedAt = changedAt
			r[username] = user
			return nil
		}
	}
	return auth.ErrUserNotFound
}

func (r authTestUserRepository) CountUsers(context.Context) (int, error) {
	return len(r), nil
}
