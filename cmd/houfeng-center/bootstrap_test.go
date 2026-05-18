package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	centerapp "houfeng/internal/center/app"
	"houfeng/internal/center/auth"
	"houfeng/internal/center/config"
	centerhttp "houfeng/internal/center/http"
	incidentservice "houfeng/internal/center/incidents"
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

func TestBootstrapCenterBuildsAppOnSuccess(t *testing.T) {
	cfg := config.CenterConfig{
		HTTPAddr:         ":8080",
		WebDistDir:       "web/dist",
		DatabaseURL:      "postgres://center",
		TelegramBotToken: "seed-bot-token",
		TelegramChatID:   "seed-chat-id",
	}
	db := &fakePostgresDB{}
	app := fakeApp{}
	var gotOpts centerhttp.RouterOptions
	var gotAddr string
	var gotHandler http.Handler
	var gotNotifierCfg config.CenterConfig
	var gotSettingsRepo centersettings.Repository

	builtApp, cleanup, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
		openPostgres: func(context.Context, string) (postgresDB, error) {
			return db, nil
		},
		applyMigrations: func(context.Context, postgresDB) error {
			return nil
		},
		seedInitialUser: func(context.Context, auth.UserRepository, config.CenterConfig) error {
			return nil
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
			if len(workers) != 3 {
				t.Fatalf("len(workers) = %d, want 3", len(workers))
			}
			for i, worker := range workers {
				if worker == nil {
					t.Fatalf("workers[%d] = nil, want non-nil", i)
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
	if gotOpts.IncidentsHandler == nil {
		t.Fatal("router incidents handler = nil, want non-nil")
	}
	if gotOpts.SettingsHandler == nil {
		t.Fatal("router settings handler = nil, want non-nil")
	}
	if gotOpts.AssetDomainsCollectionHandler == nil {
		t.Fatal("router asset domains collection handler = nil, want non-nil")
	}
	if gotOpts.AssetServicesCollectionHandler == nil {
		t.Fatal("router asset services collection handler = nil, want non-nil")
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
	if gotOpts.VPSNodesHandler == nil {
		t.Fatal("router vps nodes handler = nil, want non-nil")
	}
	if gotOpts.VPSLinkNodeHandler == nil {
		t.Fatal("router vps link node handler = nil, want non-nil")
	}
	if gotOpts.VPSUnlinkNodeHandler == nil {
		t.Fatal("router vps unlink node handler = nil, want non-nil")
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
	if gotOpts.SubscriptionsCollectionHandler == nil {
		t.Fatal("router subscriptions collection handler = nil, want non-nil")
	}
	if gotOpts.SubscriptionItemHandler == nil {
		t.Fatal("router subscription item handler = nil, want non-nil")
	}
	if gotOpts.NodesCollectionHandler == nil {
		t.Fatal("router nodes collection handler = nil, want non-nil")
	}
	if gotOpts.NodeItemHandler == nil {
		t.Fatal("router node item handler = nil, want non-nil")
	}
	if gotOpts.NodeVPSHandler == nil {
		t.Fatal("router node vps handler = nil, want non-nil")
	}
	if gotOpts.NodeRuntimeFactsHandler == nil {
		t.Fatal("router node runtime facts handler = nil, want non-nil")
	}
	if gotOpts.NodeRuntimeControlHandler == nil {
		t.Fatal("router node runtime control handler = nil, want non-nil")
	}
	if gotOpts.NodeLifecycleControlHandler == nil {
		t.Fatal("router node lifecycle control handler = nil, want non-nil")
	}
	if gotOpts.NodeOnboardingHandler == nil {
		t.Fatal("router node onboarding handler = nil, want non-nil")
	}
	if gotOpts.NodeEnrollmentTokenHandler == nil {
		t.Fatal("router node enrollment token handler = nil, want non-nil")
	}
	if gotOpts.NodeInstallCommandHandler == nil {
		t.Fatal("router node install command handler = nil, want non-nil")
	}
	if gotOpts.NodeBindingConfirmRebindHandler == nil {
		t.Fatal("router node binding confirm rebind handler = nil, want non-nil")
	}
	if gotOpts.NodeBindingRejectPendingHandler == nil {
		t.Fatal("router node binding reject pending handler = nil, want non-nil")
	}
	if gotOpts.NodeBindingResetHandler == nil {
		t.Fatal("router node binding reset handler = nil, want non-nil")
	}
	if gotOpts.NodeSparklinesHandler == nil {
		t.Fatal("router node sparklines handler = nil, want non-nil")
	}
	if gotOpts.NodeActionsHandler == nil {
		t.Fatal("router node actions handler = nil, want non-nil")
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
	if gotAddr != cfg.HTTPAddr {
		t.Fatalf("app addr = %q, want %q", gotAddr, cfg.HTTPAddr)
	}
	if gotHandler == nil {
		t.Fatal("app handler = nil, want non-nil")
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
	if db.closed {
		t.Fatal("DB closed before cleanup")
	}

	cleanup()
	if !db.closed {
		t.Fatal("cleanup() did not close DB")
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
	if len(got.OverrideRules.NodeLabels) != 1 {
		t.Fatalf("len(NodeLabelOverrides) = %d, want 1", len(got.OverrideRules.NodeLabels))
	}
	if got.OverrideRules.NodeLabels[0].Label != "核心" {
		t.Fatalf("NodeLabelOverrides[0].Label = %q, want %q", got.OverrideRules.NodeLabels[0].Label, "核心")
	}
	if got.OverrideRules.NodeLabels[0].Overrides.HostSampleFrequencyTier == nil {
		t.Fatal("HostSampleFrequencyTier override = nil, want legacy core override")
	}
	if *got.OverrideRules.NodeLabels[0].Overrides.HostSampleFrequencyTier != targets.FrequencyTier5s {
		t.Fatalf(
			"HostSampleFrequencyTier override = %q, want %q",
			*got.OverrideRules.NodeLabels[0].Overrides.HostSampleFrequencyTier,
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
	if len(got.OverrideRules.NodeLabels) != 0 {
		t.Fatalf("len(NodeLabelOverrides) = %d, want 0", len(got.OverrideRules.NodeLabels))
	}
}

func TestSettingsPresentationRepositoryDelegatesPutSettings(t *testing.T) {
	input := centersettings.Default()
	input.RetentionPolicy.RawLayerDays = 14
	repo := &fakeCenterSettingsRepository{putSettingsResult: input}

	got, err := (settingsPresentationRepository{repo: repo}).PutSettings(context.Background(), input)
	if err != nil {
		t.Fatalf("PutSettings() error = %v", err)
	}
	if repo.putSettingsInput.RetentionPolicy.RawLayerDays != 14 {
		t.Fatalf(
			"delegated RawLayerDays = %d, want %d",
			repo.putSettingsInput.RetentionPolicy.RawLayerDays,
			14,
		)
	}
	if got.RetentionPolicy.RawLayerDays != 14 {
		t.Fatalf("returned RawLayerDays = %d, want %d", got.RetentionPolicy.RawLayerDays, 14)
	}
}

func TestEnsureLegacyCoreHostSampleOverrideAugmentsExistingCoreRule(t *testing.T) {
	rules := []centersettings.NodeLabelOverrideRule{{
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
