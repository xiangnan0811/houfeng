package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	centerapp "houfeng/internal/center/app"
	"houfeng/internal/center/auth"
	"houfeng/internal/center/config"
	"houfeng/internal/center/enrollment"
	centerhttp "houfeng/internal/center/http"
	"houfeng/internal/center/http/handlers"
	incidentservice "houfeng/internal/center/incidents"
	"houfeng/internal/center/notify"
	"houfeng/internal/center/retention"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/store"
	"houfeng/internal/center/store/migrate"
	"houfeng/internal/center/syncing"
	"houfeng/internal/center/targets"
)

type appRunner interface {
	Run(context.Context) error
}

type postgresDB interface {
	Close()
	Pool() *pgxpool.Pool
}

type bootstrapDeps struct {
	openPostgres        func(context.Context, string) (postgresDB, error)
	applyMigrations     func(context.Context, postgresDB) error
	seedInitialUser     func(context.Context, auth.UserRepository, config.CenterConfig) error
	newIncidentNotifier func(config.CenterConfig, centersettings.Repository) incidentservice.Notifier
	newRouter           func(centerhttp.RouterOptions) http.Handler
	newApp              func(string, http.Handler, ...centerapp.Worker) appRunner
}

type pgxPostgresDB struct {
	pool *pgxpool.Pool
}

type settingsQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func bootstrapCenter(ctx context.Context, cfg config.CenterConfig, version string, deps bootstrapDeps) (appRunner, func(), error) {
	deps = deps.withDefaults()

	db, err := deps.openPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open postgres: %w", err)
	}

	if err := deps.applyMigrations(ctx, db); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("apply migrations: %w", err)
	}

	nodeRepo := store.NewPostgresNodeRepository(db.Pool())
	targetRepo := store.NewPostgresTargetRepository(db.Pool())
	providerRepo := store.NewPostgresProviderRepository(db.Pool())
	vpsAssetRepo := store.NewPostgresVPSAssetRepository(db.Pool())
	subscriptionRepo := store.NewPostgresSubscriptionRepository(db.Pool())
	vpsNodeLinkRepo := store.NewPostgresVPSNodeLinkRepository(db.Pool())
	runtimeFactsRepo := store.NewPostgresRuntimeFactsRepository(db.Pool())
	incidentRepo := store.NewPostgresIncidentRepository(db.Pool())
	dashboardRepo := store.NewPostgresDashboardRepository(db.Pool())
	settingsRepo := store.NewPostgresSettingsRepository(db.Pool())
	retentionRepo := store.NewPostgresRetentionRepository(db.Pool())
	retentionWorker := retention.NewWorker(retentionRepo, settingsRepo, slog.Default(), retention.DefaultWorkerInterval)
	sparklinesRepo := store.NewPostgresNodeSparklinesRepository(db.Pool())
	targetSparklinesRepo := store.NewPostgresTargetSparklinesRepository(db.Pool())
	notifierSettingsRepo := notifierSettingsRepository{repo: settingsRepo, db: db.Pool()}
	settingsHandlerRepo := settingsPresentationRepository{
		repo:                  settingsRepo,
		queryer:               db.Pool(),
		incidentSweepInterval: cfg.IncidentSweepInterval,
	}
	snapshotReader := incidentservice.NewPostgresSnapshotReader(db.Pool())
	enrollmentSvc := enrollment.NewService(nodeRepo)
	syncRepo := store.NewPostgresSyncRepository(db.Pool())
	notifier := deps.newIncidentNotifier(cfg, notifierSettingsRepo)
	incidentSvc := incidentservice.NewSettingsBackedService(
		nodeRepo,
		targetRepo,
		snapshotReader,
		incidentRepo,
		notifier,
		notifierSettingsRepo,
		slog.Default(),
		30*time.Second,
		cfg.IncidentSweepInterval,
	)
	syncSvc := syncing.NewService(syncRepo, incidentSvc)

	userRepo := store.NewPostgresUserRepository(db.Pool())
	sessionRepo := store.NewPostgresSessionRepository(db.Pool())
	if err := deps.seedInitialUser(ctx, userRepo, cfg); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("seed initial user: %w", err)
	}
	authSvc := auth.New(userRepo, sessionRepo, auth.Options{
		SessionTTL: cfg.SessionTTL,
		Now:        time.Now,
	})
	sessionCleanup := auth.NewSessionCleanupWorker(sessionRepo, slog.Default(), auth.DefaultSessionCleanupInterval)
	authMW := centerhttp.RequireSession(authSvc)

	router := deps.newRouter(centerhttp.RouterOptions{
		Version:                         version,
		WebDistDir:                      cfg.WebDistDir,
		DashboardHandler:                handlers.Dashboard(dashboardRepo),
		EventsHandler:                   handlers.Events(dashboardRepo),
		IncidentsHandler:                handlers.Incidents(incidentRepo),
		SettingsHandler:                 handlers.Settings(settingsHandlerRepo),
		ProvidersCollectionHandler:      handlers.ProvidersCollection(providerRepo),
		ProviderItemHandler:             handlers.ProviderItem(providerRepo),
		VPSCollectionHandler:            handlers.VPSCollection(vpsAssetRepo, vpsNodeLinkRepo),
		VPSItemHandler:                  handlers.VPSItem(vpsAssetRepo, vpsNodeLinkRepo),
		VPSNodesHandler:                 handlers.VPSNodes(vpsNodeLinkRepo),
		VPSLinkNodeHandler:              handlers.VPSLinkNode(vpsNodeLinkRepo),
		VPSUnlinkNodeHandler:            handlers.VPSUnlinkNode(vpsNodeLinkRepo),
		SubscriptionsCollectionHandler:  handlers.SubscriptionsCollection(subscriptionRepo),
		SubscriptionItemHandler:         handlers.SubscriptionItem(subscriptionRepo),
		NodesCollectionHandler:          handlers.NodesCollection(nodeRepo),
		NodeItemHandler:                 handlers.NodeItem(nodeRepo),
		NodeVPSHandler:                  handlers.NodeVPS(vpsNodeLinkRepo),
		NodeRuntimeFactsHandler:         handlers.NodeRuntimeFacts(runtimeFactsRepo),
		NodeRuntimeControlHandler:       handlers.NodeRuntimeControls(nodeRepo),
		NodeLifecycleControlHandler:     handlers.NodeLifecycleControls(nodeRepo),
		NodeOnboardingHandler:           handlers.NodeOnboarding(nodeRepo),
		NodeEnrollmentTokenHandler:      handlers.NodeEnrollmentToken(nodeRepo),
		NodeBindingConfirmRebindHandler: handlers.NodeBindingConfirmRebind(nodeRepo),
		NodeBindingRejectPendingHandler: handlers.NodeBindingRejectPending(nodeRepo),
		NodeBindingResetHandler:         handlers.NodeBindingReset(nodeRepo),
		NodeSparklinesHandler:           handlers.NodeSparklines(sparklinesRepo),
		NodeActionsHandler:              handlers.NodeActions(nodeRepo),
		NodeBatchHandler:                handlers.NodeBatch(nodeRepo),
		TargetsCollectionHandler:        handlers.TargetsCollection(targetRepo),
		TargetItemHandler:               handlers.TargetItem(targetRepo),
		TargetProbeItemsHandler:         handlers.TargetProbeItems(targetRepo),
		TargetRuntimeFactsHandler:       handlers.TargetRuntimeFacts(runtimeFactsRepo),
		TargetRuntimeControlHandler:     handlers.TargetRuntimeControls(targetRepo),
		TargetSparklinesHandler:         handlers.TargetSparklines(targetSparklinesRepo),
		AgentEnrollHandler:              handlers.AgentEnroll(enrollmentSvc),
		AgentSyncHandler:                handlers.AgentSync(syncSvc),
		AuthLoginHandler:                handlers.Login(authSvc),
		AuthLogoutHandler:               handlers.Logout(authSvc),
		AuthMeHandler:                   handlers.Me(authSvc),
		AuthChangePasswordHandler:       handlers.ChangePassword(authSvc),
		AuthMiddleware:                  authMW,
	})

	return deps.newApp(cfg.HTTPAddr, router, incidentSvc, retentionWorker, sessionCleanup), db.Close, nil
}

func (d bootstrapDeps) withDefaults() bootstrapDeps {
	if d.openPostgres == nil {
		d.openPostgres = func(ctx context.Context, databaseURL string) (postgresDB, error) {
			pool, err := store.OpenPostgres(ctx, databaseURL)
			if err != nil {
				return nil, err
			}
			return pgxPostgresDB{pool: pool}, nil
		}
	}
	if d.applyMigrations == nil {
		d.applyMigrations = func(ctx context.Context, db postgresDB) error {
			return migrate.Apply(ctx, db.Pool())
		}
	}
	if d.seedInitialUser == nil {
		d.seedInitialUser = func(ctx context.Context, repo auth.UserRepository, cfg config.CenterConfig) error {
			return auth.SeedInitialUser(ctx, repo, cfg.InitialUsername, cfg.InitialPassword, cfg.InitialDisplayName, time.Now)
		}
	}
	if d.newIncidentNotifier == nil {
		d.newIncidentNotifier = func(cfg config.CenterConfig, settingsRepo centersettings.Repository) incidentservice.Notifier {
			var fallback incidentservice.Notifier
			if cfg.TelegramBotToken != "" && cfg.TelegramChatID != "" {
				fallback = notify.NewTelegramNotifier(cfg.TelegramBotToken, cfg.TelegramChatID)
			}
			return incidentservice.NewSettingsAwareNotifier(
				settingsRepo,
				func(botToken, chatID string) incidentservice.Notifier {
					return notify.NewTelegramNotifier(botToken, chatID)
				},
				fallback,
			)
		}
	}
	if d.newRouter == nil {
		d.newRouter = centerhttp.New
	}
	if d.newApp == nil {
		d.newApp = func(addr string, handler http.Handler, workers ...centerapp.Worker) appRunner {
			return centerapp.New(addr, handler, workers...)
		}
	}
	return d
}

func (p pgxPostgresDB) Close() {
	p.pool.Close()
}

func (p pgxPostgresDB) Pool() *pgxpool.Pool {
	return p.pool
}

type settingsPresentationRepository struct {
	repo                  centersettings.Repository
	queryer               settingsQueryer
	incidentSweepInterval time.Duration
}

func (r settingsPresentationRepository) GetSettings(ctx context.Context) (centersettings.CenterSettings, error) {
	record, err := r.repo.GetSettings(ctx)
	if err != nil {
		return centersettings.CenterSettings{}, err
	}

	persisted, err := r.hasPersistedSettings(ctx)
	if err != nil {
		return centersettings.CenterSettings{}, err
	}
	if persisted {
		return record, nil
	}

	return applyEffectiveFreshInstallSettings(record, r.incidentSweepInterval), nil
}

func (r settingsPresentationRepository) PutSettings(ctx context.Context, input centersettings.CenterSettings) (centersettings.CenterSettings, error) {
	return r.repo.PutSettings(ctx, input)
}

func (r settingsPresentationRepository) hasPersistedSettings(ctx context.Context) (bool, error) {
	if r.queryer == nil {
		return true, nil
	}

	var sentinel int
	err := r.queryer.QueryRow(ctx, `
		select 1
		from center_settings
		where settings_id = $1`, centersettings.SingletonID).Scan(&sentinel)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query persisted center settings presence: %w", err)
	}
	return true, nil
}

func applyEffectiveFreshInstallSettings(record centersettings.CenterSettings, incidentSweepInterval time.Duration) centersettings.CenterSettings {
	record.IncidentDefaults.SweepIntervalSeconds = incidentSweepIntervalSeconds(incidentSweepInterval)
	record.OverrideRules.NodeLabels = ensureLegacyCoreHostSampleOverride(record.OverrideRules.NodeLabels)
	return record
}

func incidentSweepIntervalSeconds(interval time.Duration) int {
	if interval <= 0 {
		interval = time.Minute
	}
	seconds := int(interval.Round(time.Second) / time.Second)
	if seconds <= 0 {
		return 60
	}
	return seconds
}

func ensureLegacyCoreHostSampleOverride(rules []centersettings.NodeLabelOverrideRule) []centersettings.NodeLabelOverrideRule {
	const coreNodeLabel = "核心"
	coreTier := targets.FrequencyTier1m

	for i, rule := range rules {
		if rule.Label != coreNodeLabel {
			continue
		}
		if rule.Label == coreNodeLabel && rule.Overrides.HostSampleFrequencyTier != nil {
			return rules
		}

		next := append([]centersettings.NodeLabelOverrideRule(nil), rules...)
		next[i].Overrides.HostSampleFrequencyTier = &coreTier
		return next
	}

	legacyRule := centersettings.NodeLabelOverrideRule{
		Label: coreNodeLabel,
		Overrides: centersettings.SettingsOverrideFields{
			HostSampleFrequencyTier: &coreTier,
		},
	}

	return append([]centersettings.NodeLabelOverrideRule{legacyRule}, rules...)
}

type notifierSettingsRepository struct {
	repo centersettings.Repository
	db   *pgxpool.Pool
}

func (r notifierSettingsRepository) GetSettings(ctx context.Context) (centersettings.CenterSettings, error) {
	return r.repo.GetSettings(ctx)
}

func (r notifierSettingsRepository) PutSettings(ctx context.Context, input centersettings.CenterSettings) (centersettings.CenterSettings, error) {
	return r.repo.PutSettings(ctx, input)
}

func (r notifierSettingsRepository) GetPersistedTelegramSettings(ctx context.Context) (centersettings.TelegramSettings, bool, error) {
	var botToken string
	var chatID string
	var runtimeManaged bool
	err := r.db.QueryRow(ctx, `
		select telegram_bot_token, telegram_chat_id, telegram_runtime_managed
		from center_settings
		where settings_id = $1`, centersettings.SingletonID).Scan(&botToken, &chatID, &runtimeManaged)
	if errors.Is(err, pgx.ErrNoRows) {
		return centersettings.TelegramSettings{}, false, nil
	}
	if err != nil {
		return centersettings.TelegramSettings{}, false, fmt.Errorf("query persisted telegram settings: %w", err)
	}
	return centersettings.TelegramSettings{
		BotToken:       strings.TrimSpace(botToken),
		ChatID:         strings.TrimSpace(chatID),
		RuntimeManaged: runtimeManaged,
	}, true, nil
}

func (r notifierSettingsRepository) GetPersistedIncidentDefaults(ctx context.Context) (centersettings.IncidentDefaults, bool, error) {
	var raw []byte
	err := r.db.QueryRow(ctx, `
		select incident_defaults
		from center_settings
		where settings_id = $1`, centersettings.SingletonID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return centersettings.IncidentDefaults{}, false, nil
	}
	if err != nil {
		return centersettings.IncidentDefaults{}, false, fmt.Errorf("query persisted incident defaults: %w", err)
	}
	var defaults centersettings.IncidentDefaults
	if err := json.Unmarshal(raw, &defaults); err != nil {
		return centersettings.IncidentDefaults{}, false, fmt.Errorf("decode persisted incident defaults: %w", err)
	}
	return defaults, true, nil
}
