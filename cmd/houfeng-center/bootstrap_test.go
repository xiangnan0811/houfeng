package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"houfeng/internal/center/activity"
	centerapp "houfeng/internal/center/app"
	"houfeng/internal/center/attachments"
	"houfeng/internal/center/auth"
	"houfeng/internal/center/config"
	centerhttp "houfeng/internal/center/http"
	"houfeng/internal/center/http/sessionctx"
	incidentservice "houfeng/internal/center/incidents"
	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordsearch"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/store"
)

func TestBootstrapCenterReturnsOpenPostgresError(t *testing.T) {
	cfg := config.CenterConfig{HTTPAddr: ":8080", WebDistDir: "web/dist", DatabaseURL: "postgres://center"}
	wantErr := errors.New("open boom")
	calledMigrate := false
	calledRouter := false
	calledApp := false

	app, cleanup, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
		newReceiverHealthObserver: func(context.Context, *pgxpool.Pool) (centerapp.Worker, error) { return fakeReceiverHealthWorker{}, nil },
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
		newReceiverHealthObserver: func(context.Context, *pgxpool.Pool) (centerapp.Worker, error) { return fakeReceiverHealthWorker{}, nil },
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
		newReceiverHealthObserver: func(context.Context, *pgxpool.Pool) (centerapp.Worker, error) { return fakeReceiverHealthWorker{}, nil },
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
		RecordDeploymentID: centerTestDeployment,
		SessionHMACKey:     []byte("0123456789abcdef0123456789abcdef"),
		// This unit fixture exercises admission and HTTP routing without opening a
		// database-backed local namespace or contacting the configured S3 endpoint.
		Attachment: config.AttachmentConfig{
			BlobBackend:          attachments.BackendKindS3,
			S3Endpoint:           "127.0.0.1:9000",
			S3Bucket:             "fixture-blobs",
			S3AccessKey:          "fixture-access",
			S3SecretKey:          "fixture-secret",
			Limits:               attachments.DefaultLimits(),
			ProcessorMaxAttempts: 3,
		},
	}
	db := &fakePostgresDB{}
	applyCalls := 0
	admitCalls := 0
	searchGenerationCalls := 0
	activityGenerationCalls := 0
	var router http.Handler
	var gotWorkers []centerapp.Worker
	var openedDSN string

	app, cleanup, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
		loadRuntimeLaunch:   centerTestLaunch,
		verifyRuntimeLaunch: centerTestVerifyLaunch,
		newRecordAdmissionGate: func(config.CenterConfig) (store.AdmissionGate, error) {
			return nil, nil
		},
		newReceiverHealthObserver: func(context.Context, *pgxpool.Pool) (centerapp.Worker, error) { return fakeReceiverHealthWorker{}, nil },
		openPostgres: func(_ context.Context, dsn string) (postgresDB, error) {
			openedDSN = dsn
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
		ensureActivityGeneration: func(context.Context, postgresDB) error {
			activityGenerationCalls++
			return nil
		},
		newActivityProjectionWorker: func(*pgxpool.Pool, string) (centerapp.Worker, error) {
			return fakeActivityProjectionWorker{}, nil
		},
		newSubjectActivityHandler: func(
			*pgxpool.Pool,
			*store.PostgresVPSAssetRepository,
			*store.PostgresMonitoringInstanceRepository,
			*store.PostgresTargetRepository,
			[]byte,
		) (http.Handler, error) {
			return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), nil
		},
		newVPSOverviewHandler: func(
			*pgxpool.Pool,
			*store.PostgresVPSAssetRepository,
			*store.PostgresVPSMonitoringInstanceLinkRepository,
			*store.PostgresIPQualityRepository,
			*store.PostgresSettingsRepository,
			*store.PostgresSubscriptionRepository,
			*store.PostgresAssetServiceRepository,
			*store.PostgresAssetDomainRepository,
			*store.PostgresMonitoringInstanceRepository,
			*store.PostgresTargetRepository,
			[]byte,
			bool,
		) (http.Handler, error) {
			return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), nil
		},
		seedInitialUser: func(context.Context, auth.UserRepository, config.CenterConfig) error {
			return nil
		},
		newSessionRepository: func(*pgxpool.Pool, []byte) (auth.SessionRepository, error) {
			return fakeSessionRepository{}, nil
		},
		newRouter: func(options centerhttp.RouterOptions) http.Handler {
			options.AuthMiddleware = func(next http.Handler) http.Handler { return next }
			router = centerhttp.New(options)
			return router
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
	connection, err := pgxpool.ParseConfig(openedDSN)
	if err != nil {
		t.Fatalf("parse APP connection: %v", err)
	}
	_, launchDigest, _ := centerTestLaunch("", "center")
	if got := connection.ConnConfig.RuntimeParams["houfeng.birth_launch_digest"]; got != fmt.Sprintf("%x", launchDigest) {
		t.Fatalf("APP launch digest = %q, want protected digest", got)
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
	if activityGenerationCalls != 1 {
		t.Fatalf("ensureActivityGeneration calls = %d, want 1", activityGenerationCalls)
	}
	// The projector only indexes commits, so records written before the index
	// existed stay invisible until a rebuild backfills them. That backfill has to
	// be a running worker, not a manual step.
	var rebuilder *recordsearch.RebuildWorker
	var activityWorker centerapp.Worker
	var intelligenceWorker *ipquality.IntelligenceWorker
	for _, worker := range gotWorkers {
		if candidate, ok := worker.(*ipquality.IntelligenceWorker); ok {
			intelligenceWorker = candidate
		}
		if candidate, ok := worker.(*recordsearch.RebuildWorker); ok {
			rebuilder = candidate
		}
		if _, ok := worker.(fakeActivityProjectionWorker); ok {
			activityWorker = worker
		}
	}
	if rebuilder == nil {
		t.Fatalf("runtime workers = %#v, want a record search rebuild worker", gotWorkers)
	}
	if activityWorker == nil {
		t.Fatalf("runtime workers = %#v, want an activity projection worker", gotWorkers)
	}
	if intelligenceWorker == nil {
		t.Fatal("missing IP quality intelligence lifecycle worker")
	}
	actor := mustBootstrapRecordsActor(t)
	for _, handlerCase := range []struct {
		name     string
		method   string
		path     string
		body     string
		wantCode string
	}{
		{name: "records", method: http.MethodGet, path: "/api/records", wantCode: "record_service_unavailable"},
		{name: "search", method: http.MethodGet, path: "/api/records/search?q=fixture", wantCode: "record_service_unavailable"},
		{name: "actions", method: http.MethodGet, path: "/api/records/rec_httpcontract/actions", wantCode: "record_service_unavailable"},
		{name: "comments", method: http.MethodGet, path: "/api/records/rec_httpcontract/comments", wantCode: "record_service_unavailable"},
		{name: "watch", method: http.MethodGet, path: "/api/records/rec_httpcontract/watch", wantCode: "record_service_unavailable"},
		{name: "inbox", method: http.MethodGet, path: "/api/record-notifications", wantCode: "record_service_unavailable"},
		{name: "drafts", method: http.MethodGet, path: "/api/record-drafts", wantCode: "record_service_unavailable"},
		{name: "deletion preview", method: http.MethodPost, path: "/api/records/rec_httpcontract/permanent-delete-preview", wantCode: "deletion_safety_unavailable"},
		{name: "deletion execute", method: http.MethodPost, path: "/api/records/rec_httpcontract/permanent-delete", body: `{}`, wantCode: "deletion_safety_unavailable"},
		{name: "deletion status", method: http.MethodGet, path: "/api/record-deletions/rpo_httpcontract", wantCode: "deletion_status_unavailable"},
		{name: "evidence preview", method: http.MethodPost, path: "/api/evidence/capture-previews", body: `{}`, wantCode: "evidence_service_unavailable"},
		{name: "evidence read", method: http.MethodGet, path: "/api/evidence/evs_httpcontract", wantCode: "evidence_service_unavailable"},
		{name: "attachment upload", method: http.MethodPost, path: "/api/attachment-uploads", body: `{"draft_id":"rdf_httpcontract0001","display_name":"notes.txt","declared_size_bytes":4,"media_type":"text/plain"}`, wantCode: "attachment_service_unavailable"},
	} {
		t.Run(handlerCase.name+" fails closed without transaction admission", func(t *testing.T) {
			request := httptest.NewRequest(handlerCase.method, handlerCase.path, strings.NewReader(handlerCase.body))
			if handlerCase.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
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
	cleanup()
	if !db.closed || db.closeCalls != 1 {
		t.Fatalf("cleanup closed DB %d times, want exactly once", db.closeCalls)
	}
}

func TestBootstrapCenterClosesDBOnRuntimeAdmissionFailure(t *testing.T) {
	cfg := config.CenterConfig{
		RecordPlatformMode: config.RecordPlatformModeRuntimeAdmission,
		HTTPAddr:           ":8080",
		WebDistDir:         "web/dist",
		DatabaseURL:        "postgres://center",
		RecordDeploymentID: centerTestDeployment,
	}
	db := &fakePostgresDB{}
	wantErr := errors.New("runtime admission boom")
	applyCalls := 0
	admitCalls := 0

	app, cleanup, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
		loadRuntimeLaunch:         centerTestLaunch,
		newReceiverHealthObserver: func(context.Context, *pgxpool.Pool) (centerapp.Worker, error) { return fakeReceiverHealthWorker{}, nil },
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
		RecordDeploymentID: centerTestDeployment,
	}
	db := &fakePostgresDB{}
	applyCalls := 0
	deps := (bootstrapDeps{
		newReceiverHealthObserver: func(context.Context, *pgxpool.Pool) (centerapp.Worker, error) { return fakeReceiverHealthWorker{}, nil }}).withDefaults()
	deps.openPostgres = func(context.Context, string) (postgresDB, error) {
		return db, nil
	}
	deps.loadRuntimeLaunch = centerTestLaunch
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

func TestBootstrapCenterRejectsUntrustedLaunchBeforeOpeningAPP(t *testing.T) {
	for _, tc := range []struct {
		name   string
		dsn    string
		loader func(string, string) (recordbirth.LaunchBinding, [32]byte, error)
	}{
		{"missing launch", "postgres://center", func(string, string) (recordbirth.LaunchBinding, [32]byte, error) {
			return recordbirth.LaunchBinding{}, [32]byte{}, errors.New("missing")
		}},
		{"different deployment", "postgres://center", func(path, kind string) (recordbirth.LaunchBinding, [32]byte, error) {
			launch, digest, err := centerTestLaunch(path, kind)
			launch.DeploymentID = "dp-" + strings.Repeat("d", 64)
			return launch, digest, err
		}},
		{"different process", "postgres://center", func(path, _ string) (recordbirth.LaunchBinding, [32]byte, error) {
			return centerTestLaunch(path, "content-processor")
		}},
		{"preloaded digest", "postgres://center?houfeng.birth_launch_digest=old", centerTestLaunch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opened := false
			cfg := config.CenterConfig{RecordPlatformMode: config.RecordPlatformModeRuntimeAdmission, DatabaseURL: tc.dsn, RecordDeploymentID: centerTestDeployment}
			_, _, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
				loadRuntimeLaunch: tc.loader,
				openPostgres: func(context.Context, string) (postgresDB, error) {
					opened = true
					return nil, errors.New("unexpected APP connection")
				},
			})
			if err == nil || opened {
				t.Fatalf("launch rejection = %v, APP opened = %t", err, opened)
			}
		})
	}
}

func TestBootstrapCenterDefaultLaunchLoaderRejectsMissingFile(t *testing.T) {
	opened := false
	cfg := config.CenterConfig{
		RecordPlatformMode: config.RecordPlatformModeRuntimeAdmission, DatabaseURL: "postgres://center",
		RecordDeploymentID: centerTestDeployment, BirthLaunchFile: filepath.Join(t.TempDir(), "missing-launch.json"),
	}
	_, _, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
		openPostgres: func(context.Context, string) (postgresDB, error) {
			opened = true
			return nil, errors.New("unexpected APP connection")
		},
	})
	if err == nil || opened {
		t.Fatalf("missing launch error = %v, APP opened = %t", err, opened)
	}
}

func TestBootstrapCenterClosesAPPWhenLaunchContinuityFails(t *testing.T) {
	db := &fakePostgresDB{}
	wantErr := errors.New("checkpoint ancestry unavailable")
	cfg := config.CenterConfig{RecordPlatformMode: config.RecordPlatformModeRuntimeAdmission, DatabaseURL: "postgres://center", RecordDeploymentID: centerTestDeployment}
	cfg.BirthLaunchFile = "/protected/center-launch.json"
	cfg.BirthReader.IdentityFile = "/not-read-for-never-installed"
	_, _, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
		loadRuntimeLaunch: func(path, kind string) (recordbirth.LaunchBinding, [32]byte, error) {
			if path != cfg.BirthLaunchFile || kind != "center" {
				t.Fatalf("launch request = %q %q", path, kind)
			}
			return centerTestLaunch(path, kind)
		},
		openPostgres: func(context.Context, string) (postgresDB, error) { return db, nil },
		admitRuntime: func(context.Context, postgresDB) error { return nil },
		verifyRuntimeLaunch: func(_ context.Context, _ *pgxpool.Pool, reader *recordbirth.PostgresAuthority, launch recordbirth.LaunchBinding) error {
			if reader != nil || launch.BirthInstalled {
				t.Fatal("never-installed startup unexpectedly opened W2")
			}
			return wantErr
		},
	})
	if !errors.Is(err, wantErr) || !db.closed || db.closeCalls != 1 {
		t.Fatalf("verification failure = %v, APP closes = %d", err, db.closeCalls)
	}
}

func TestBootstrapCenterInstalledBirthRequiresIndependentReader(t *testing.T) {
	loader := func(path, kind string) (recordbirth.LaunchBinding, [32]byte, error) {
		launch, digest, err := centerTestLaunch(path, kind)
		launch.BirthInstalled = true
		return launch, digest, err
	}
	t.Run("missing reader closes APP", func(t *testing.T) {
		db := &fakePostgresDB{}
		verified := false
		cfg := config.CenterConfig{RecordPlatformMode: config.RecordPlatformModeRuntimeAdmission, DatabaseURL: "postgres://center", RecordDeploymentID: centerTestDeployment}
		_, _, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
			loadRuntimeLaunch: loader,
			openPostgres:      func(context.Context, string) (postgresDB, error) { return db, nil },
			admitRuntime:      func(context.Context, postgresDB) error { return nil },
			verifyRuntimeLaunch: func(context.Context, *pgxpool.Pool, *recordbirth.PostgresAuthority, recordbirth.LaunchBinding) error {
				verified = true
				return nil
			},
		})
		if err == nil || verified || db.closeCalls != 1 {
			t.Fatalf("missing reader error = %v, verified = %t, APP closes = %d", err, verified, db.closeCalls)
		}
	})
	t.Run("reader remains available without producer", func(t *testing.T) {
		cfg := birthCompositionConfig(t, "birth_reader")
		cfg.DatabaseURL, cfg.RecordDeploymentID = "postgres://center", centerTestDeployment
		cfg.BirthReader = config.BirthReaderConfig{Connection: cfg.BirthProducer.Connection, IdentityFile: cfg.BirthProducer.IdentityFile, CredentialFile: cfg.BirthProducer.CredentialFile}
		cfg.BirthProducer = config.BirthProducerConfig{}
		db := &fakePostgresDB{pool: birthCompositionPool(t, "app_runtime")}
		wantErr := errors.New("stop after verified reader")
		verified := false
		_, _, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
			loadRuntimeLaunch: loader,
			openPostgres:      func(context.Context, string) (postgresDB, error) { return db, nil },
			admitRuntime:      func(context.Context, postgresDB) error { return nil },
			verifyRuntimeLaunch: func(_ context.Context, _ *pgxpool.Pool, reader *recordbirth.PostgresAuthority, _ recordbirth.LaunchBinding) error {
				verified = reader != nil
				return nil
			},
			ensureSearchGeneration: func(context.Context, postgresDB) error { return wantErr },
		})
		if !errors.Is(err, wantErr) || !verified || db.closeCalls != 1 {
			t.Fatalf("reader startup = %v, verified = %t, APP closes = %d", err, verified, db.closeCalls)
		}
	})
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
	var gotSettingsSource centersettings.NotificationSettingsSource
	var gotSessionHMACKey []byte

	builtApp, cleanup, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
		newReceiverHealthObserver: func(context.Context, *pgxpool.Pool) (centerapp.Worker, error) { return fakeReceiverHealthWorker{}, nil },
		openPostgres: func(context.Context, string) (postgresDB, error) {
			return db, nil
		},
		applyMigrations: func(context.Context, postgresDB) error {
			return nil
		},
		ensureSearchGeneration: func(context.Context, postgresDB) error {
			return nil
		},
		ensureActivityGeneration: func(context.Context, postgresDB) error {
			return nil
		},
		seedInitialUser: func(context.Context, auth.UserRepository, config.CenterConfig) error {
			return nil
		},
		newSessionRepository: func(_ *pgxpool.Pool, key []byte) (auth.SessionRepository, error) {
			gotSessionHMACKey = append([]byte(nil), key...)
			return fakeSessionRepository{}, nil
		},
		newIncidentNotifier: func(source centersettings.NotificationSettingsSource) incidentservice.Notifier {
			gotSettingsSource = source
			return &fakeIncidentNotifier{}
		},
		newRouter: func(opts centerhttp.RouterOptions) http.Handler {
			gotOpts = opts
			return centerhttp.New(opts)
		},
		newApp: func(addr string, handler http.Handler, workers ...centerapp.Worker) appRunner {
			gotAddr = addr
			gotHandler = handler
			intelligenceCount := 0
			for i, worker := range workers {
				if _, ok := worker.(*ipquality.IntelligenceWorker); ok {
					intelligenceCount++
				}
				if worker == nil {
					t.Fatalf("workers[%d] = nil, want non-nil", i)
				}
				// There is no index to rebuild while the records platform is off,
				// and a worker polling those tables would fail every pass.
				if _, ok := worker.(*recordsearch.RebuildWorker); ok {
					t.Fatalf("workers[%d] is a record search rebuild worker, want none while records are disabled", i)
				}
				if _, ok := worker.(*activity.Worker); ok {
					t.Fatalf("workers[%d] is an activity projection worker, want none while records are disabled", i)
				}
			}
			if intelligenceCount != 1 {
				t.Fatalf("intelligence workers = %d, want 1 even with Records disabled", intelligenceCount)
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
	if recorder.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", recorder.Code)
	}
	for _, path := range []string{"/api/records", "/api/record-drafts", "/api/attachment-uploads"} {
		recorder = httptest.NewRecorder()
		gotHandler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("legacy route %s status = %d, want 404", path, recorder.Code)
		}
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
	if gotSettingsSource == nil {
		t.Fatal("notification settings source = nil, want runtime settings repository for notifier wiring")
	}
	if string(gotSessionHMACKey) != string(cfg.SessionHMACKey) {
		t.Fatalf("session HMAC key = %q, want configured key", string(gotSessionHMACKey))
	}
	if db.closed {
		t.Fatal("DB closed before cleanup")
	}

	cleanup()
	cleanup()
	if !db.closed || db.closeCalls != 1 {
		t.Fatalf("cleanup closed DB %d times, want exactly once", db.closeCalls)
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

	deps := bootstrapDeps{
		newReceiverHealthObserver: func(context.Context, *pgxpool.Pool) (centerapp.Worker, error) { return fakeReceiverHealthWorker{}, nil }}.withDefaults()
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

func TestNewProductionRecordPlatformAdmissionGateValidatesIdentity(t *testing.T) {
	if _, err := newProductionRecordPlatformAdmissionGate(config.CenterConfig{RecordInstanceID: "api-01"}); err == nil {
		t.Fatal("partial identity error = nil, want rejection")
	}

	gate, err := newProductionRecordPlatformAdmissionGate(config.CenterConfig{
		RecordInstanceID:         "api-01",
		RecordDeploymentID:       "dp-" + strings.Repeat("a", 64),
		RecordInstanceKind:       "api",
		RecordInstanceCapability: "records.runtime",
	})
	if err != nil || gate == nil {
		t.Fatalf("complete identity gate = %#v, error = %v", gate, err)
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

type fakePostgresDB struct {
	closed     bool
	closeCalls int
	pool       *pgxpool.Pool
}

func (f *fakePostgresDB) Close() {
	f.closed = true
	f.closeCalls++
}

func (f *fakePostgresDB) Pool() *pgxpool.Pool {
	return f.pool
}

type fakeApp struct{}

func (fakeApp) Run(context.Context) error {
	return nil
}

type fakeActivityProjectionWorker struct{}

type fakeReceiverHealthWorker struct{}

func (fakeReceiverHealthWorker) Run(context.Context) error { return nil }

func (fakeActivityProjectionWorker) Run(context.Context) error {
	return nil
}

type fakeIncidentNotifier struct{}

func (*fakeIncidentNotifier) Send(context.Context, string) error {
	return nil
}

type fakeSessionRepository struct {
	sessions          map[string]auth.Session
	passwordChangedAt map[string]time.Time
}

func (fakeSessionRepository) CreateIfPasswordHash(context.Context, string, auth.Session, func() time.Time, time.Duration) (auth.Session, error) {
	return auth.Session{}, nil
}

func (fakeSessionRepository) ChangePasswordIfHash(context.Context, string, string, string, string, func() time.Time) error {
	return nil
}

func (fakeSessionRepository) TouchWithUserLock(context.Context, string, func() time.Time, time.Duration) (auth.Session, error) {
	return auth.Session{}, nil
}

func (f fakeSessionRepository) ValidateSession(_ context.Context, sessionID string, now func() time.Time) error {
	if sessionID == "" {
		return auth.ErrSessionNotFound
	}
	session, ok := f.sessions[sessionID]
	if !ok {
		return auth.ErrSessionNotFound
	}
	if !session.ExpiresAt.After(now().UTC().Truncate(time.Microsecond)) {
		return auth.ErrSessionExpired
	}
	if changedAt := f.passwordChangedAt[session.UserID]; !changedAt.IsZero() && !session.IssuedAt.IsZero() && session.IssuedAt.Before(changedAt) {
		return auth.ErrSessionExpired
	}
	return nil
}

func (fakeSessionRepository) Delete(context.Context, string) error {
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

func (r authTestUserRepository) CountUsers(context.Context) (int, error) {
	return len(r), nil
}
