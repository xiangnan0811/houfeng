package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"houfeng/internal/center/activity"
	centerapp "houfeng/internal/center/app"
	"houfeng/internal/center/attachments"
	"houfeng/internal/center/auth"
	"houfeng/internal/center/config"
	"houfeng/internal/center/enrollment"
	centerevidence "houfeng/internal/center/evidence"
	centerhttp "houfeng/internal/center/http"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/ids"
	incidentservice "houfeng/internal/center/incidents"
	"houfeng/internal/center/installer"
	"houfeng/internal/center/notify"
	"houfeng/internal/center/portability"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/recordreadiness"
	centerrecords "houfeng/internal/center/records"
	"houfeng/internal/center/recordsearch"
	"houfeng/internal/center/retention"
	"houfeng/internal/center/runtimefacts"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/store"
	"houfeng/internal/center/store/migrate"
	"houfeng/internal/center/subscriptioncosts"
	"houfeng/internal/center/syncing"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsoverview"
)

type appRunner interface {
	Run(context.Context) error
}

type postgresDB interface {
	Close()
	Pool() *pgxpool.Pool
}

type bootstrapDeps struct {
	openPostgres                func(context.Context, string) (postgresDB, error)
	applyMigrations             func(context.Context, postgresDB) error
	admitRuntime                func(context.Context, postgresDB) error
	ensureSearchGeneration      func(context.Context, postgresDB) error
	ensureActivityGeneration    func(context.Context, postgresDB) error
	newActivityProjectionWorker func(*pgxpool.Pool, string) (centerapp.Worker, error)
	newSubjectActivityHandler   func(
		*pgxpool.Pool,
		*store.PostgresVPSAssetRepository,
		*store.PostgresMonitoringInstanceRepository,
		*store.PostgresTargetRepository,
		[]byte,
	) (http.Handler, error)
	newVPSOverviewHandler func(
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
	) (http.Handler, error)
	seedInitialUser               func(context.Context, auth.UserRepository, config.CenterConfig) error
	newSessionRepository          func(*pgxpool.Pool, []byte) (auth.SessionRepository, error)
	newIncidentNotifier           func(config.CenterConfig, centersettings.Repository) incidentservice.Notifier
	newRouter                     func(centerhttp.RouterOptions) http.Handler
	newApp                        func(string, http.Handler, ...centerapp.Worker) appRunner
	recordPlatformAdmissionGate   store.AdmissionGate
	recordSubjectTombstoneWitness *pgxpool.Pool
}

type pgxPostgresDB struct {
	pool *pgxpool.Pool
}

type settingsQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type recordCollaborationRuntime struct {
	watchesHandler             http.Handler
	inboxHandler               http.Handler
	portabilityHandler         http.Handler
	projectionWorker           *recordcollaboration.NotificationProjectionWorker
	activityDeletionAdapter    recorddeletion.Adapter
	portabilityDeletionAdapter recorddeletion.Adapter
	readiness                  *recordreadiness.Registry
}

func bootstrapCenter(ctx context.Context, cfg config.CenterConfig, version string, deps bootstrapDeps) (appRunner, func(), error) {
	deps = deps.withDefaults()

	db, err := deps.openPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open postgres: %w", err)
	}
	if deps.recordSubjectTombstoneWitness == nil {
		if witnessURL := strings.TrimSpace(os.Getenv("HOUFENG_DELETION_WITNESS_DATABASE_URL")); witnessURL != "" {
			witnessPool, err := pgxpool.New(ctx, witnessURL)
			if err != nil {
				db.Close()
				return nil, nil, fmt.Errorf("open deletion witness: %w", err)
			}
			deps.recordSubjectTombstoneWitness = witnessPool
		}
	}

	switch cfg.RecordPlatformMode {
	case config.RecordPlatformModeLegacy:
		if err := deps.applyMigrations(ctx, db); err != nil {
			db.Close()
			return nil, nil, fmt.Errorf("apply migrations: %w", err)
		}
	case config.RecordPlatformModeRuntimeAdmission:
		if err := deps.admitRuntime(ctx, db); err != nil {
			db.Close()
			return nil, nil, fmt.Errorf("admit app runtime: %w", err)
		}
	default:
		db.Close()
		return nil, nil, fmt.Errorf("unknown record-platform mode %d", cfg.RecordPlatformMode)
	}
	// The search projector only writes published or building generations, so one
	// has to exist before the first record commits or nothing would be indexed.
	if err := deps.ensureSearchGeneration(ctx, db); err != nil {
		db.Close()
		return nil, nil, err
	}
	// The activity worker waits when no generation is active, so a fresh install
	// needs one before the first pass or nothing would be projected.
	if err := deps.ensureActivityGeneration(ctx, db); err != nil {
		db.Close()
		return nil, nil, err
	}

	monitoringInstanceRepo := store.NewPostgresMonitoringInstanceRepositoryWithTokenHMACKey(db.Pool(), cfg.SessionHMACKey)
	targetRepo := store.NewPostgresTargetRepository(db.Pool())
	providerRepo := store.NewPostgresProviderRepository(db.Pool())
	vpsAssetRepo := store.NewPostgresVPSAssetRepository(db.Pool())
	assetDomainRepo := store.NewPostgresAssetDomainRepository(db.Pool())
	assetServiceRepo := store.NewPostgresAssetServiceRepository(db.Pool())
	assetLifecycleRepo := store.NewPostgresAssetLifecycleRepository(db.Pool())
	ipQualityRepo := store.NewPostgresIPQualityRepository(db.Pool())
	subscriptionRepo := store.NewPostgresSubscriptionRepository(db.Pool())
	subscriptionCostRepo := store.NewPostgresSubscriptionCostRepository(db.Pool())
	vpsMonitoringInstanceLinkRepo := store.NewPostgresVPSMonitoringInstanceLinkRepository(db.Pool())
	renewalDecisionRepo := store.NewPostgresRenewalDecisionRepository(db.Pool())
	runtimeFactsRepo := store.NewPostgresRuntimeFactsRepository(db.Pool())
	incidentRepo := store.NewPostgresIncidentRepository(db.Pool())
	dashboardRepo := store.NewPostgresDashboardRepository(db.Pool())
	commandAuditRepo := store.NewPostgresCommandAuditRepository(db.Pool())
	assetDecisionRepo := store.NewPostgresAssetDecisionRepository(db.Pool())
	settingsRepo := store.NewPostgresSettingsRepository(db.Pool())
	retentionRepo := store.NewPostgresRetentionRepository(db.Pool())
	retentionWorker := retention.NewWorker(retentionRepo, settingsRepo, slog.Default(), retention.DefaultWorkerInterval)
	sparklinesRepo := store.NewPostgresMonitoringInstanceSparklinesRepository(db.Pool())
	targetSparklinesRepo := store.NewPostgresTargetSparklinesRepository(db.Pool())
	notifierSettingsRepo := notifierSettingsRepository{repo: settingsRepo, db: db.Pool()}
	settingsHandlerRepo := settingsPresentationRepository{
		repo:                  settingsRepo,
		queryer:               db.Pool(),
		incidentSweepInterval: cfg.IncidentSweepInterval,
	}
	snapshotReader := incidentservice.NewPostgresSnapshotReader(db.Pool())
	enrollmentSvc := enrollment.NewService(monitoringInstanceRepo)
	syncRepo := store.NewPostgresSyncRepositoryWithTokenHMACKey(db.Pool(), cfg.SessionHMACKey)
	streamHub := runtimefacts.NewStreamHub()
	notifier := deps.newIncidentNotifier(cfg, notifierSettingsRepo)
	subscriptionDispatcher := incidentservice.NewSettingsAwareNotificationDispatcher(
		notifierSettingsRepo,
		func(botToken, chatID string) incidentservice.Notifier {
			return notify.NewTelegramNotifier(botToken, chatID)
		},
		func(webhookURL string) incidentservice.Notifier {
			return notify.NewFeishuNotifier(webhookURL)
		},
		nil,
	)
	subscriptionCostSvc := subscriptioncosts.NewService(subscriptionCostRepo, settingsRepo, map[string]subscriptioncosts.ExchangeRateProvider{
		string(centersettings.SubscriptionExchangeRateProviderFrankfurter): subscriptioncosts.NewFrankfurterProvider(nil),
		string(centersettings.SubscriptionExchangeRateProviderFixer):       subscriptioncosts.NewSettingsAwareFixerProvider(nil, settingsRepo),
	})
	exchangeRateWorker := subscriptioncosts.NewExchangeRateWorker(subscriptionCostSvc, slog.Default(), subscriptioncosts.DefaultExchangeRateWorkerInterval)
	subscriptionReminderSvc := subscriptioncosts.NewReminderService(subscriptionCostRepo, settingsRepo, subscriptionDispatcher, incidentRepo, slog.Default())
	subscriptionReminderWorker := subscriptioncosts.NewReminderWorker(subscriptionReminderSvc, subscriptioncosts.DefaultReminderWorkerInterval)
	incidentSvc := incidentservice.NewSettingsBackedService(
		monitoringInstanceRepo,
		targetRepo,
		snapshotReader,
		incidentRepo,
		notifier,
		notifierSettingsRepo,
		slog.Default(),
		5*time.Second,
		cfg.IncidentSweepInterval,
	)
	syncSvc := syncing.NewService(syncRepo, syncing.NewCompositePostSyncProcessor(incidentSvc, streamHub))

	userRepo := store.NewPostgresUserRepository(db.Pool())
	sessionRepo, err := deps.newSessionRepository(db.Pool(), cfg.SessionHMACKey)
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("create session repository: %w", err)
	}
	if err := deps.seedInitialUser(ctx, userRepo, cfg); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("seed initial user: %w", err)
	}
	authSvc := auth.New(userRepo, sessionRepo, auth.Options{
		SessionTTL:         cfg.SessionTTL,
		Now:                time.Now,
		PasswordBcryptCost: cfg.PasswordBcryptCost,
	})
	scopeRepo := store.NewPostgresRecordAuthorizationRepository(db.Pool())
	sessionCleanup := auth.NewSessionCleanupWorker(sessionRepo, slog.Default(), auth.DefaultSessionCleanupInterval)
	authMiddleware := func(next http.Handler) http.Handler {
		return centerhttp.RequireSameOrigin(cfg.PublicBaseURL)(centerhttp.RequireSession(authSvc, scopeRepo)(next))
	}
	recordsEnabled := cfg.RecordPlatformMode == config.RecordPlatformModeRuntimeAdmission
	if recordsEnabled && nilBootstrapAdmissionGate(deps.recordPlatformAdmissionGate) {
		gate, gateErr := newProductionRecordPlatformAdmissionGate(cfg)
		if gateErr != nil {
			db.Close()
			return nil, nil, fmt.Errorf("create record platform admission gate: %w", gateErr)
		}
		deps.recordPlatformAdmissionGate = gate
	}
	var recordsHandler http.Handler
	var recordSearchHandler http.Handler
	var subjectActivityHandler http.Handler
	var vpsOverviewHandler http.Handler
	var recordActionsHandler http.Handler
	var recordCommentsHandler http.Handler
	var collaborationRuntime recordCollaborationRuntime
	var recordDraftsHandler http.Handler
	var recordDeletionsHandler http.Handler
	var evidenceHandler http.Handler
	var attachmentUploadsHandler http.Handler
	var attachmentsHandler http.Handler
	var evidenceMaintenance *centerevidence.MaintenanceWorker
	if recordsEnabled {
		recordsHandler, recordSearchHandler, recordActionsHandler, recordCommentsHandler, recordDraftsHandler, recordDeletionsHandler, evidenceHandler,
			attachmentUploadsHandler, attachmentsHandler, collaborationRuntime, evidenceMaintenance, err = newRecordsHTTPHandlers(
			db.Pool(),
			vpsAssetRepo,
			monitoringInstanceRepo,
			targetRepo,
			cfg.Attachment,
			productionEvidenceSources{
				IPQuality: ipQualityRepo, Monitoring: runtimeFactsRepo, Events: incidentRepo,
				SubscriptionCosts: subscriptionCostRepo, CommandAudits: commandAuditRepo,
			},
			deps.recordPlatformAdmissionGate,
			cfg,
			deps.recordSubjectTombstoneWitness,
			comparisonRuntimeConfig{
				Enabled:          cfg.ComparisonEnabled,
				AdmissionBudget:  cfg.ComparisonAdmissionBudget,
				IntentKeyring:    cfg.ComparisonIntentKeyring,
				IntentKeyID:      cfg.ComparisonIntentKeyID,
				ReservedKeyPaths: comparisonReservedKeyPaths(cfg),
			},
		)
		if err != nil {
			db.Close()
			return nil, nil, fmt.Errorf("create records handlers: %w", err)
		}
		subjectActivityHandler, err = deps.newSubjectActivityHandler(
			db.Pool(),
			vpsAssetRepo,
			monitoringInstanceRepo,
			targetRepo,
			cfg.SessionHMACKey,
		)
		if err != nil {
			db.Close()
			return nil, nil, fmt.Errorf("create subject activity handler: %w", err)
		}
		vpsOverviewHandler, err = deps.newVPSOverviewHandler(
			db.Pool(),
			vpsAssetRepo,
			vpsMonitoringInstanceLinkRepo,
			ipQualityRepo,
			settingsRepo,
			subscriptionRepo,
			assetServiceRepo,
			assetDomainRepo,
			monitoringInstanceRepo,
			targetRepo,
			cfg.SessionHMACKey,
		)
		if err != nil {
			db.Close()
			return nil, nil, fmt.Errorf("create vps overview handler: %w", err)
		}
	}

	router := deps.newRouter(centerhttp.RouterOptions{
		Version:                                     version,
		WebDistDir:                                  cfg.WebDistDir,
		DashboardHandler:                            handlers.Dashboard(dashboardRepo),
		EventsHandler:                               handlers.Events(dashboardRepo),
		CommandAuditsHandler:                        handlers.CommandAudits(commandAuditRepo),
		IncidentsHandler:                            handlers.Incidents(incidentRepo),
		SettingsHandler:                             handlers.Settings(settingsHandlerRepo),
		RecordsEnabled:                              recordsEnabled,
		ComparisonEnabled:                           recordsEnabled && cfg.ComparisonEnabled,
		PortabilityEnabled:                          recordsEnabled && cfg.PortabilityEnabled,
		RecordsHandler:                              recordsHandler,
		RecordSearchHandler:                         recordSearchHandler,
		SubjectActivityHandler:                      subjectActivityHandler,
		RecordActionsHandler:                        recordActionsHandler,
		RecordCommentsHandler:                       recordCommentsHandler,
		RecordWatchesHandler:                        collaborationRuntime.watchesHandler,
		RecordInboxHandler:                          collaborationRuntime.inboxHandler,
		RecordDraftsHandler:                         recordDraftsHandler,
		RecordDeletionsHandler:                      recordDeletionsHandler,
		RecordPortabilityHandler:                    collaborationRuntime.portabilityHandler,
		EvidenceHandler:                             evidenceHandler,
		AttachmentUploadsHandler:                    attachmentUploadsHandler,
		AttachmentsHandler:                          attachmentsHandler,
		AssetDomainsCollectionHandler:               handlers.AssetDomainsCollection(assetDomainRepo),
		AssetServicesCollectionHandler:              handlers.AssetServicesCollection(assetServiceRepo),
		AssetDecisionOverviewHandler:                handlers.AssetDecisionOverview(assetDecisionRepo),
		AssetDecisionGroupsHandler:                  handlers.AssetDecisionGroups(assetDecisionRepo),
		AssetDecisionGroupHandler:                   handlers.AssetDecisionGroup(assetDecisionRepo),
		AssetDecisionManualGroupsHandler:            handlers.AssetDecisionManualGroups(assetDecisionRepo),
		AssetDecisionManualGroupHandler:             handlers.AssetDecisionManualGroup(assetDecisionRepo),
		AssetDecisionScenarioTemplatesHandler:       handlers.AssetDecisionScenarioTemplates(assetDecisionRepo),
		AssetDecisionScenarioTemplateHandler:        handlers.AssetDecisionScenarioTemplate(assetDecisionRepo),
		AssetDecisionRecordsHandler:                 handlers.AssetDecisionRecords(assetDecisionRepo),
		AssetDecisionRecordHandler:                  handlers.AssetDecisionRecord(assetDecisionRepo),
		ProvidersCollectionHandler:                  handlers.ProvidersCollection(providerRepo),
		ProviderItemHandler:                         handlers.ProviderItem(providerRepo),
		VPSCollectionHandler:                        handlers.VPSCollection(vpsAssetRepo, vpsMonitoringInstanceLinkRepo, assetLifecycleRepo, ipQualityRepo),
		VPSItemHandler:                              handlers.VPSItem(vpsAssetRepo, vpsMonitoringInstanceLinkRepo, assetLifecycleRepo, ipQualityRepo),
		VPSOverviewHandler:                          vpsOverviewHandler,
		VPSMonitoringInstancesHandler:               handlers.VPSMonitoringInstances(vpsMonitoringInstanceLinkRepo, monitoringInstanceRepo),
		VPSSubscriptionsHandler:                     handlers.VPSSubscriptions(subscriptionRepo),
		VPSLinkMonitoringInstanceHandler:            handlers.VPSLinkMonitoringInstance(vpsMonitoringInstanceLinkRepo),
		VPSUnlinkMonitoringInstanceHandler:          handlers.VPSUnlinkMonitoringInstance(vpsMonitoringInstanceLinkRepo),
		VPSTimelineHandler:                          handlers.VPSTimeline(renewalDecisionRepo),
		VPSExperienceLogsHandler:                    handlers.VPSExperienceLogs(renewalDecisionRepo),
		VPSDomainsHandler:                           handlers.VPSDomains(assetDomainRepo),
		VPSServicesHandler:                          handlers.VPSServices(assetServiceRepo),
		VPSIPQualityHandler:                         handlers.VPSIPQuality(ipQualityRepo),
		VPSCancellationPreviewHandler:               handlers.VPSCancellationPreview(assetLifecycleRepo),
		VPSCancellationHandler:                      handlers.VPSCancellation(assetLifecycleRepo),
		VPSExtendValidityHandler:                    handlers.VPSExtendValidity(assetLifecycleRepo),
		VPSArchiveReviewHandler:                     handlers.VPSArchiveReview(assetLifecycleRepo),
		VPSArchiveHandler:                           handlers.VPSArchive(assetLifecycleRepo),
		VPSRestoreFromArchiveHandler:                handlers.VPSRestoreFromArchive(assetLifecycleRepo),
		AssetContextTargetsHandler:                  handlers.AssetContextTargets(assetLifecycleRepo),
		SubscriptionsCollectionHandler:              handlers.SubscriptionsCollection(subscriptionRepo, subscriptionCostSvc),
		SubscriptionItemHandler:                     handlers.SubscriptionItem(subscriptionRepo),
		SubscriptionOverviewHandler:                 handlers.SubscriptionOverview(subscriptionCostSvc),
		SubscriptionStatisticsHandler:               handlers.SubscriptionStatistics(subscriptionCostSvc),
		SubscriptionSettingsHandler:                 handlers.SubscriptionSettings(subscriptionCostSvc),
		SubscriptionExchangeRateRefreshHandler:      handlers.SubscriptionExchangeRateRefresh(subscriptionCostSvc),
		SubscriptionBudgetsHandler:                  handlers.SubscriptionBudgets(subscriptionCostSvc),
		SubscriptionMonthlyBudgetsHandler:           handlers.SubscriptionMonthlyBudgets(subscriptionCostSvc),
		MonitoringInstancesCollectionHandler:        handlers.MonitoringInstancesCollection(monitoringInstanceRepo),
		MonitoringInstanceItemHandler:               handlers.MonitoringInstanceItem(monitoringInstanceRepo),
		MonitoringInstanceVPSHandler:                handlers.MonitoringInstanceVPS(vpsMonitoringInstanceLinkRepo),
		MonitoringInstanceRuntimeFactsHandler:       handlers.MonitoringInstanceRuntimeFacts(runtimeFactsRepo),
		MonitoringInstanceRuntimeStreamHandler:      handlers.MonitoringInstanceRuntimeStream(monitoringInstanceRepo, streamHub),
		MonitoringInstanceRuntimeControlHandler:     handlers.MonitoringInstanceRuntimeControls(monitoringInstanceRepo),
		MonitoringInstanceManagementReviewHandler:   handlers.MonitoringInstanceManagementReview(monitoringInstanceRepo),
		MonitoringInstanceLifecycleRetireHandler:    handlers.MonitoringInstanceLifecycleRetire(monitoringInstanceRepo),
		MonitoringInstanceLifecycleRestoreHandler:   handlers.MonitoringInstanceLifecycleRestore(monitoringInstanceRepo),
		MonitoringInstanceArchiveHandler:            handlers.MonitoringInstanceArchive(monitoringInstanceRepo),
		MonitoringInstanceRestoreFromArchiveHandler: handlers.MonitoringInstanceRestoreFromArchive(monitoringInstanceRepo),
		MonitoringInstancePermanentCleanupHandler:   handlers.MonitoringInstancePermanentCleanup(monitoringInstanceRepo),
		MonitoringInstanceOnboardingHandler:         handlers.MonitoringInstanceOnboarding(monitoringInstanceRepo),
		MonitoringInstanceEnrollmentTokenHandler:    handlers.MonitoringInstanceEnrollmentToken(monitoringInstanceRepo),
		MonitoringInstanceInstallCommandHandler: handlers.MonitoringInstanceInstallCommand(monitoringInstanceRepo, handlers.InstallCommandOptions{
			PublicBaseURL: cfg.PublicBaseURL,
			AgentVersion:  version,
		}),
		MonitoringInstanceBindingConfirmRebindHandler: handlers.MonitoringInstanceBindingConfirmRebind(monitoringInstanceRepo),
		MonitoringInstanceBindingRejectPendingHandler: handlers.MonitoringInstanceBindingRejectPending(monitoringInstanceRepo),
		MonitoringInstanceBindingResetHandler:         handlers.MonitoringInstanceBindingReset(monitoringInstanceRepo),
		MonitoringInstanceSparklinesHandler:           handlers.MonitoringInstanceSparklines(sparklinesRepo),
		MonitoringInstanceActionsHandler:              handlers.MonitoringInstanceActions(monitoringInstanceRepo),
		MonitoringInstanceBatchHandler:                handlers.MonitoringInstanceBatch(monitoringInstanceRepo),
		TargetsCollectionHandler:                      handlers.TargetsCollection(targetRepo),
		TargetItemHandler:                             handlers.TargetItem(targetRepo),
		TargetProbeItemsHandler:                       handlers.TargetProbeItems(targetRepo),
		TargetRuntimeFactsHandler:                     handlers.TargetRuntimeFacts(runtimeFactsRepo),
		TargetRuntimeControlHandler:                   handlers.TargetRuntimeControls(targetRepo),
		TargetSparklinesHandler:                       handlers.TargetSparklines(targetSparklinesRepo),
		AgentEnrollHandler:                            handlers.AgentEnrollWithOptions(enrollmentSvc, handlers.AgentEndpointOptions{TrustedProxies: cfg.TrustedProxies}),
		AgentSyncHandler:                              handlers.AgentSyncWithOptions(syncSvc, handlers.AgentEndpointOptions{TrustedProxies: cfg.TrustedProxies}),
		InstallerScriptHandler:                        handlers.InstallerScript(installer.Script),
		AuthLoginHandler: handlers.LoginWithOptions(authSvc, handlers.LoginOptions{
			TrustedProxies: cfg.TrustedProxies,
		}),
		AuthLogoutHandler:         handlers.Logout(authSvc),
		AuthMeHandler:             handlers.Me(authSvc),
		AuthChangePasswordHandler: handlers.ChangePassword(authSvc),
		AuthMiddleware:            authMiddleware,
	})
	router = centerhttp.SecurityHeaders(strings.HasPrefix(cfg.PublicBaseURL, "https://"))(router)
	router = centerhttp.RequireAllowedHost(cfg.PublicBaseURL)(router)

	workers := []centerapp.Worker{
		incidentSvc,
		retentionWorker,
		sessionCleanup,
		exchangeRateWorker,
		subscriptionReminderWorker,
	}
	if recordsEnabled {
		if collaborationRuntime.projectionWorker != nil {
			workers = append(workers, collaborationRuntime.projectionWorker)
		}
		if evidenceMaintenance != nil {
			workers = append(workers, evidenceMaintenance)
		}
		// A lease only excludes a second writer if the two disagree about who they
		// are, so the owner is minted per process rather than fixed. A restarted
		// center therefore waits out the dead lease instead of stealing a
		// generation a still-live process is writing.
		rebuildOwnerID, err := ids.New("rso")
		if err != nil {
			db.Close()
			return nil, nil, fmt.Errorf("mint record search rebuild owner id: %w", err)
		}
		// The in-transaction projector only indexes commits, so records that
		// predate the index, or a generation abandoned by a crashed rebuild, need a
		// backfill pass to become searchable.
		searchRebuilder, err := recordsearch.NewRebuildWorker(
			store.NewPostgresRecordSearchRebuildStore(db.Pool(), deps.recordPlatformAdmissionGate),
			recordsearch.RebuildWorkerOptions{
				OwnerID:            rebuildOwnerID,
				OwnerLeaseDuration: recordsearch.DefaultRebuildLeaseDuration,
				BatchSize:          recordsearch.DefaultRebuildBatchSize,
				PollInterval:       recordsearch.DefaultRebuildPollInterval,
				Logger:             slog.Default(),
			},
		)
		if err != nil {
			db.Close()
			return nil, nil, fmt.Errorf("create record search rebuild worker: %w", err)
		}
		workers = append(workers, searchRebuilder)

		activityOwnerID, err := ids.New("rao")
		if err != nil {
			db.Close()
			return nil, nil, fmt.Errorf("mint activity projection owner id: %w", err)
		}
		activityWorker, err := deps.newActivityProjectionWorker(db.Pool(), activityOwnerID)
		if err != nil {
			db.Close()
			return nil, nil, fmt.Errorf("create activity projection worker: %w", err)
		}
		workers = append(workers, activityWorker)
	}
	cleanup := db.Close
	if deps.recordSubjectTombstoneWitness != nil && deps.recordSubjectTombstoneWitness != db.Pool() {
		witness := deps.recordSubjectTombstoneWitness
		cleanup = func() {
			witness.Close()
			db.Close()
		}
	}
	return deps.newApp(cfg.HTTPAddr, router, workers...), cleanup, nil
}

func newActivityExportReader(pool *pgxpool.Pool) (*activity.ExportReader, error) {
	namespace := activity.Namespace{ProjectID: "default"}
	repository, err := store.NewActivityProjectionRepository(pool)
	if err != nil {
		return nil, err
	}
	recordDomain, err := store.NewRecordDomainActivitySource(pool, namespace)
	if err != nil {
		return nil, err
	}
	monitoringEvents, err := store.NewMonitoringEventActivitySource(pool, namespace)
	if err != nil {
		return nil, err
	}
	commandAudits, err := store.NewCommandAuditActivitySource(pool, namespace)
	if err != nil {
		return nil, err
	}
	evidenceSource, err := store.NewEvidenceActivitySource(pool, namespace)
	if err != nil {
		return nil, err
	}
	assetHistory, err := store.NewAssetHistoryActivitySource(pool, namespace)
	if err != nil {
		return nil, err
	}
	return activity.NewExportReader(activity.ExportReaderDeps{
		HeadStore: repository,
		Pages:     repository,
		Adapters: []activity.ExportReadySourceAdapter{
			recordDomain, monitoringEvents, commandAudits, evidenceSource, assetHistory,
		},
	})
}

func newActivityProjectionWorker(pool *pgxpool.Pool, ownerID string) (*activity.Worker, error) {
	namespace := activity.Namespace{ProjectID: "default"}
	repository, err := store.NewActivityProjectionRepository(pool)
	if err != nil {
		return nil, err
	}
	recordDomain, err := store.NewRecordDomainActivitySource(pool, namespace)
	if err != nil {
		return nil, err
	}
	monitoringEvents, err := store.NewMonitoringEventActivitySource(pool, namespace)
	if err != nil {
		return nil, err
	}
	commandAudits, err := store.NewCommandAuditActivitySource(pool, namespace)
	if err != nil {
		return nil, err
	}
	evidence, err := store.NewEvidenceActivitySource(pool, namespace)
	if err != nil {
		return nil, err
	}
	assetHistory, err := store.NewAssetHistoryActivitySource(pool, namespace)
	if err != nil {
		return nil, err
	}
	projector, err := activity.NewProjector(activity.ProjectorOptions{
		Namespace: namespace,
		Adapters: []activity.SourceAdapter{
			recordDomain, monitoringEvents, commandAudits, evidence, assetHistory,
		},
		Checkpoints: repository,
		Publisher:   repository,
	})
	if err != nil {
		return nil, err
	}
	return activity.NewWorker(activity.WorkerOptions{
		Projector:   projector,
		Leases:      repository,
		Generations: repository,
		OwnerID:     ownerID,
		Logger:      slog.Default(),
	})
}

func newSubjectActivityHandler(
	pool *pgxpool.Pool,
	vpsRepository *store.PostgresVPSAssetRepository,
	monitoringInstanceRepository *store.PostgresMonitoringInstanceRepository,
	targetRepository *store.PostgresTargetRepository,
	sessionHMACKey []byte,
) (http.Handler, error) {
	subjects, err := centerrecords.NewSubjectAdapterRegistry([]centerrecords.SubjectSourceAdapter{
		store.NewVPSRecordSubjectAdapter(vpsRepository),
		store.NewMonitoringInstanceRecordSubjectAdapter(monitoringInstanceRepository),
		store.NewTargetRecordSubjectAdapter(targetRepository),
	})
	if err != nil {
		return nil, fmt.Errorf("subject registry: %w", err)
	}
	repository, err := store.NewActivityProjectionRepository(pool)
	if err != nil {
		return nil, err
	}
	codec, err := activity.NewCursorCodec(sessionHMACKey)
	if err != nil {
		return nil, fmt.Errorf("activity cursor codec: %w", err)
	}
	service, err := activity.NewService(
		repository,
		repository,
		store.NewActivityLiveSubjectResolver(subjects),
		codec,
	)
	if err != nil {
		return nil, err
	}
	return handlers.SubjectActivity(service), nil
}

func newVPSOverviewHandler(
	pool *pgxpool.Pool,
	vpsRepository *store.PostgresVPSAssetRepository,
	monitoringLinks *store.PostgresVPSMonitoringInstanceLinkRepository,
	ipQuality *store.PostgresIPQualityRepository,
	settingsRepository *store.PostgresSettingsRepository,
	subscriptionRepository *store.PostgresSubscriptionRepository,
	serviceRepository *store.PostgresAssetServiceRepository,
	domainRepository *store.PostgresAssetDomainRepository,
	monitoringInstanceRepository *store.PostgresMonitoringInstanceRepository,
	targetRepository *store.PostgresTargetRepository,
	sessionHMACKey []byte,
) (http.Handler, error) {
	sources, err := store.NewVPSOverviewRepository(
		vpsRepository, monitoringLinks, ipQuality, settingsRepository, subscriptionRepository,
		serviceRepository, domainRepository,
	)
	if err != nil {
		return nil, err
	}
	subjects, err := centerrecords.NewSubjectAdapterRegistry([]centerrecords.SubjectSourceAdapter{
		store.NewVPSRecordSubjectAdapter(vpsRepository),
		store.NewMonitoringInstanceRecordSubjectAdapter(monitoringInstanceRepository),
		store.NewTargetRecordSubjectAdapter(targetRepository),
	})
	if err != nil {
		return nil, fmt.Errorf("subject registry: %w", err)
	}
	repository, err := store.NewActivityProjectionRepository(pool)
	if err != nil {
		return nil, err
	}
	codec, err := activity.NewCursorCodec(sessionHMACKey)
	if err != nil {
		return nil, fmt.Errorf("activity cursor codec: %w", err)
	}
	activityService, err := activity.NewService(
		repository, repository, store.NewActivityLiveSubjectResolver(subjects), codec,
	)
	if err != nil {
		return nil, err
	}
	overviewService, err := vpsoverview.NewService(sources, activityService)
	if err != nil {
		return nil, err
	}
	return handlers.VPSOverview(overviewService), nil
}

func newProductionWitnessedRecordSubjectTombstoneSource(
	cfg config.CenterConfig,
	local *pgxpool.Pool,
	witness *pgxpool.Pool,
) store.WitnessedRecordSubjectTombstoneSource {
	reader, err := store.NewWitnessedRecordSubjectTombstoneReader(
		recordplatform.DeploymentID(strings.TrimSpace(cfg.RecordDeploymentID)),
		recordauth.ProjectIDDefault,
		optionalRecordSubjectQueryer(witness),
		optionalRecordSubjectQueryer(local),
	)
	if err != nil {
		return &store.WitnessedRecordSubjectTombstoneReader{}
	}
	return reader
}

func optionalRecordSubjectQueryer(pool *pgxpool.Pool) *pgxpool.Pool {
	if pool == nil {
		return nil
	}
	return pool
}

func newProductionRecordPlatformAdmissionGate(cfg config.CenterConfig) (store.AdmissionGate, error) {
	instanceID := strings.TrimSpace(cfg.RecordInstanceID)
	deploymentID := strings.TrimSpace(cfg.RecordDeploymentID)
	instanceKind := strings.TrimSpace(cfg.RecordInstanceKind)
	capability := strings.TrimSpace(cfg.RecordInstanceCapability)
	set := 0
	for _, value := range []string{instanceID, deploymentID, instanceKind, capability} {
		if value != "" {
			set++
		}
	}
	if set == 0 {
		return nil, nil
	}
	if set != 4 {
		return nil, fmt.Errorf("record admission identity is incomplete")
	}
	return store.NewDeploymentMembershipAdmissionGate(store.DeploymentMembershipIdentity{
		InstanceID:   instanceID,
		DeploymentID: recordplatform.DeploymentID(deploymentID),
		ProjectID:    recordplatform.ProjectIDDefault,
		InstanceKind: instanceKind,
		Capability:   capability,
	})
}

func nilBootstrapAdmissionGate(gate store.AdmissionGate) bool {
	if gate == nil {
		return true
	}
	value := reflect.ValueOf(gate)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return value.IsNil()
	default:
		return false
	}
}

func comparisonReservedKeyPaths(cfg config.CenterConfig) []string {
	_ = cfg
	reserved := make([]string, 0, 2)
	if path := strings.TrimSpace(os.Getenv("HOUFENG_SESSION_HMAC_KEY_FILE")); path != "" {
		reserved = append(reserved, path)
	}
	if path := strings.TrimSpace(os.Getenv("HOUFENG_RECORD_DELETION_KEY_FILE")); path != "" {
		reserved = append(reserved, path)
	}
	if path := strings.TrimSpace(os.Getenv("HOUFENG_BACKUP_HMAC_KEY_FILE")); path != "" {
		reserved = append(reserved, path)
	}
	return reserved
}

func newRecordsHTTPHandlers(
	pool *pgxpool.Pool,
	vpsRepository *store.PostgresVPSAssetRepository,
	monitoringInstanceRepository *store.PostgresMonitoringInstanceRepository,
	targetRepository *store.PostgresTargetRepository,
	attachmentConfig config.AttachmentConfig,
	evidenceSources productionEvidenceSources,
	gate store.AdmissionGate,
	cfg config.CenterConfig,
	tombstoneWitness *pgxpool.Pool,
	comparison comparisonRuntimeConfig,
) (http.Handler, http.Handler, http.Handler, http.Handler, http.Handler, http.Handler, http.Handler, http.Handler, http.Handler, recordCollaborationRuntime, *centerevidence.MaintenanceWorker, error) {
	effectiveGate := gate
	if nilBootstrapAdmissionGate(effectiveGate) {
		effectiveGate = nil
	}
	subjects, err := centerrecords.NewSubjectAdapterRegistry([]centerrecords.SubjectSourceAdapter{
		store.NewVPSRecordSubjectAdapter(vpsRepository),
		store.NewMonitoringInstanceRecordSubjectAdapter(monitoringInstanceRepository),
		store.NewTargetRecordSubjectAdapter(targetRepository),
	})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record subject registry: %w", err)
	}
	witness := newProductionWitnessedRecordSubjectTombstoneSource(cfg, pool, tombstoneWitness)
	subjectResolver := store.NewRecordSubjectReadResolver(subjects, witness)
	authorizations := store.NewPostgresCurrentRecordAuthorizationSource(pool, subjectResolver, effectiveGate)
	var evidenceComposition *productionEvidenceComposition
	if effectiveGate != nil {
		evidenceComposition, err = newProductionEvidenceComposition(productionEvidenceCompositionDependencies{
			Pool: pool, Gate: effectiveGate, Subjects: subjects, Sources: evidenceSources,
			Tombstones:        witness,
			ComparisonEnabled: comparison.Enabled,
			Comparison:        comparison,
		})
		if err != nil {
			return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create evidence composition: %w", err)
		}
		authorizations = evidenceComposition.authorizations
	}

	// The deployment-membership authority owns the injected gate. The default
	// nil/typed-nil path registers stable transports while every record and
	// evidence operation remains closed.
	collaborationMembers := store.NewPostgresCollaborationMembershipReader()
	var comparisonSigner centerevidence.ComparisonIntentSigner
	if evidenceComposition != nil {
		comparisonSigner = evidenceComposition.comparisonSigner
	}
	recordRepository, err := store.NewPostgresRecordRepository(pool, effectiveGate, []centerrecords.RevisionParticipant{
		store.NewRecordAttachmentRevisionParticipant(),
		store.NewCollaborationRevisionParticipant(collaborationMembers),
		store.NewComparisonRevisionParticipant(comparisonSigner),
		store.NewRecordEvidenceRevisionParticipant(),
		store.NewRecordSearchRevisionParticipant(),
	})
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record repository: %w", err)
	}
	draftRepository := store.NewPostgresRecordDraftRepository(pool, effectiveGate)
	attachmentRepository := store.NewPostgresAttachmentRepository(pool)
	contentLeaseRepository := store.NewPostgresRecordPlatformRepository(pool, effectiveGate)
	blob, err := newAttachmentBlobStore(attachmentConfig)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create attachment Blob store: %w", err)
	}
	scanner, err := newAttachmentScanner(attachmentConfig)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create attachment scanner readiness: %w", err)
	}
	uploadService, err := attachments.NewUploadService(
		draftRepository,
		attachmentRepository,
		blob,
		attachments.UploadServiceOptions{
			TransportKind:           attachments.TransportKind(attachmentConfig.BlobBackend),
			Limits:                  attachmentConfig.Limits,
			ArchiveScannerReadiness: newArchiveScannerReadiness(scanner),
			ProcessorMaxAttempts:    attachmentConfig.ProcessorMaxAttempts,
		},
	)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create attachment upload service: %w", err)
	}
	downloadService, err := attachments.NewDownloadService(
		attachmentRepository,
		authorizations,
		contentLeaseRepository,
		blob,
		attachments.DownloadServiceOptions{Limits: attachmentConfig.Limits},
	)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create attachment download service: %w", err)
	}

	readService, err := centerrecords.NewRecordReadService(authorizations, authorizations, recordRepository)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record read service: %w", err)
	}
	revisionService, err := centerrecords.NewRevisionService(subjects, authorizations, recordRepository)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record revision service: %w", err)
	}
	lifecycleService, err := centerrecords.NewRecordLifecycleService(authorizations, recordRepository)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record lifecycle service: %w", err)
	}
	draftService, err := centerrecords.NewDraftService(draftRepository, authorizations)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record draft service: %w", err)
	}
	application, err := centerrecords.NewApplication(
		readService,
		revisionService,
		lifecycleService,
		draftService,
		centerrecords.ApplicationOptions{
			IdempotencyOwnerID: "records_api",
			OwnerLeaseDuration: time.Minute,
			IdempotencyTTL:     24 * time.Hour,
			OutboxTTL:          24 * time.Hour,
		},
	)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create records application: %w", err)
	}
	actionRepository := store.NewPostgresRecordActionRepository(pool, effectiveGate, collaborationMembers, authorizations)
	actionService, err := recordcollaboration.NewActionService(authorizations, actionRepository)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record action service: %w", err)
	}
	actionApplication, err := recordcollaboration.NewActionApplication(
		actionService,
		recordcollaboration.ActionApplicationOptions{
			IdempotencyOwnerID: "record_actions_api",
			OwnerLeaseDuration: time.Minute,
			IdempotencyTTL:     24 * time.Hour,
			OutboxTTL:          24 * time.Hour,
		},
	)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record action application: %w", err)
	}
	commentRepository := store.NewPostgresRecordCommentRepository(pool, effectiveGate, collaborationMembers, authorizations)
	commentService, err := recordcollaboration.NewCommentService(authorizations, commentRepository)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record comment service: %w", err)
	}
	commentApplication, err := recordcollaboration.NewCommentApplication(
		commentService,
		recordcollaboration.CommentApplicationOptions{
			IdempotencyOwnerID: "record_comments_api",
			OwnerLeaseDuration: time.Minute,
			IdempotencyTTL:     24 * time.Hour,
			OutboxTTL:          24 * time.Hour,
		},
	)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record comment application: %w", err)
	}
	watchRepository := store.NewPostgresRecordWatchRepository(pool, effectiveGate, collaborationMembers, authorizations)
	watchService, err := recordcollaboration.NewWatchService(authorizations, watchRepository)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record watch service: %w", err)
	}
	watchApplication, err := recordcollaboration.NewWatchApplication(
		watchService,
		recordcollaboration.WatchApplicationOptions{
			IdempotencyOwnerID: "record_watches_api",
			OwnerLeaseDuration: time.Minute,
			IdempotencyTTL:     24 * time.Hour,
		},
	)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record watch application: %w", err)
	}
	notificationRepository := store.NewPostgresRecordNotificationRepository(
		pool, effectiveGate, collaborationMembers, authorizations, 30*24*time.Hour,
	)
	notificationQueue := store.NewPostgresRecordPlatformRepository(pool, effectiveGate)
	notificationProjector, err := recordcollaboration.NewNotificationProjector(notificationQueue, notificationRepository, 5*time.Second)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record notification projector: %w", err)
	}
	collaborationRuntime := recordCollaborationRuntime{
		watchesHandler: handlers.RecordWatches(watchApplication),
		inboxHandler:   handlers.RecordInbox(notificationRepository),
	}
	if effectiveGate != nil {
		collaborationRuntime.projectionWorker, err = recordcollaboration.NewNotificationProjectionWorker(
			notificationProjector,
			recordcollaboration.NotificationProjectionWorkerOptions{
				OwnerID: "record_notifications_projector", OwnerLeaseDuration: time.Minute,
				PollInterval: time.Second, Logger: slog.Default(),
			},
		)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record notification projection worker: %w", err)
		}
	}
	// Search hydrates through the same read service the record endpoints use, so
	// one authorization decision covers both surfaces.
	searchService, err := recordsearch.NewService(store.NewPostgresRecordSearchStore(pool, effectiveGate), readService)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record search service: %w", err)
	}
	var evidencePreparer *centerevidence.RevisionPreparer
	var evidenceHandler http.Handler = handlers.Evidence(nil)
	var evidenceWorker *centerevidence.MaintenanceWorker
	if evidenceComposition != nil {
		evidencePreparer = evidenceComposition.preparer
		evidenceHandler = evidenceComposition.handler
		evidenceWorker = evidenceComposition.worker
	}
	// Activity deletion adapter is constructed here so Child 7's surface is not
	// forgotten when the permanent-deletion transport opens. Later Records
	// children still own the remaining adapters and ledger/witness clients;
	// until every RequiredAdapterNames member is wired and healthy, the
	// production deletion transport remains explicitly closed.
	deletionRepository := store.NewPostgresRecordDeletionRepository(pool, effectiveGate)
	activityDeletionAdapter, err := activity.NewDeletionAdapter(deletionRepository)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create activity deletion adapter: %w", err)
	}
	var _ recorddeletion.Adapter = activityDeletionAdapter
	portabilityDeletionAdapter, err := portability.NewDeletionAdapter(deletionRepository)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record portability deletion adapter: %w", err)
	}
	var _ recorddeletion.Adapter = portabilityDeletionAdapter
	collaborationRuntime.activityDeletionAdapter = activityDeletionAdapter
	collaborationRuntime.portabilityDeletionAdapter = portabilityDeletionAdapter
	readiness, err := newProductionRecordReadinessRegistry(
		deletionRepository,
		attachmentRepository,
		evidenceComposition,
		activityDeletionAdapter,
		portabilityDeletionAdapter,
		effectiveGate,
		witness,
		pool,
	)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record readiness registry: %w", err)
	}
	collaborationRuntime.readiness = readiness

	if cfg.PortabilityEnabled {
		backendKind := "local"
		if attachmentConfig.BlobBackend == attachments.BackendKindS3 {
			backendKind = "s3"
		}
		comparisonKind, err := centerevidence.NewComparisonResultKind()
		if err != nil {
			return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create comparison result kind: %w", err)
		}
		activityReader, err := newActivityExportReader(pool)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create activity export reader: %w", err)
		}
		var evidenceExporter portability.EvidenceExporter
		var snapshotSource portability.SnapshotSource
		if evidenceComposition != nil {
			evidenceExporter = evidenceComposition.export
			snapshotSource = evidenceComposition.repository
		}
		portabilityOptions := portability.Options{
			Enabled:         true,
			BackendKind:     backendKind,
			Documents:       application,
			Jobs:            store.NewPostgresRecordPortabilityRepository(pool, effectiveGate),
			Evidence:        evidenceExporter,
			Snapshots:       snapshotSource,
			Comparison:      comparisonKind,
			Activity:        activityReader,
			PDF:             portability.NewIsolatedDocumentPDFRenderer(contentProcessorPDFBinary()),
			Imports:         store.NewPostgresRecordPortabilityRepository(pool, effectiveGate),
			Importer:        application,
			EvidenceImports: portability.NewKnownKindEvidenceImporter(),
			Rebuilder:       portability.NewAuthoritativeProjectionRebuilder(),
			Staging:         portability.NewLeasedBlobStore(blob),
			Attachments:     portability.NewDownloadAttachmentSource(downloadService),
			AttachmentBlobs: blob,
		}
		if evidenceComposition != nil {
			portabilityOptions.Kinds = evidenceComposition.registry
		}
		portabilityService, err := portability.NewService(portabilityOptions)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, nil, nil, nil, recordCollaborationRuntime{}, nil, fmt.Errorf("create record portability service: %w", err)
		}
		collaborationRuntime.portabilityHandler = handlers.RecordPortability(portabilityService)
	}

	recordOptions := handlers.RecordHandlerOptions{EvidencePreparer: evidencePreparer}
	if comparison.Enabled && evidenceComposition != nil {
		recordOptions.ComparisonSave = evidenceComposition.application
		recordOptions.CompletedIdempotency = func(
			ctx context.Context,
			actor recordauth.ActorScope,
			operation recordplatform.OperationKind,
			key string,
		) (bool, error) {
			return recordRepository.PeekCompletedIdempotency(ctx, recordplatform.IdempotencyKey{
				ProjectID:     recordplatform.ProjectID(actor.ProjectID),
				OperationKind: operation,
				Key:           key,
			})
		}
	}
	return handlers.RecordsWithOptions(application, recordOptions),
		handlers.RecordSearch(searchService),
		handlers.RecordActions(actionApplication), handlers.RecordComments(commentApplication), handlers.RecordDrafts(application), handlers.RecordDeletions(nil), evidenceHandler,
		handlers.AttachmentUploads(uploadService), handlers.AttachmentsWithOptions(downloadService), collaborationRuntime, evidenceWorker, nil
}

func newProductionRecordReadinessRegistry(
	deletionRepository *store.PostgresRecordDeletionRepository,
	attachmentRepository *store.PostgresAttachmentRepository,
	evidenceComposition *productionEvidenceComposition,
	activityDeletion recorddeletion.Adapter,
	portabilityDeletion recorddeletion.Adapter,
	gate store.AdmissionGate,
	witness store.WitnessedRecordSubjectTombstoneSource,
	pool *pgxpool.Pool,
) (*recordreadiness.Registry, error) {
	deletions := make([]recorddeletion.Adapter, 0, 7)
	core, err := recorddeletion.NewCoreAdapter(deletionRepository)
	if err != nil {
		return nil, fmt.Errorf("core deletion adapter: %w", err)
	}
	deletions = append(deletions, core)
	attachmentDeletion, err := attachments.NewDeletionAdapter(attachmentRepository)
	if err != nil {
		return nil, fmt.Errorf("attachment deletion adapter: %w", err)
	}
	deletions = append(deletions, attachmentDeletion)
	if evidenceComposition != nil && evidenceComposition.deletion != nil {
		deletions = append(deletions, evidenceComposition.deletion)
	}
	searchDeletion, err := recordsearch.NewDeletionAdapter(deletionRepository)
	if err != nil {
		return nil, fmt.Errorf("search deletion adapter: %w", err)
	}
	deletions = append(deletions, searchDeletion)
	deletions = append(deletions, activityDeletion, portabilityDeletion)
	collaborationDeletion, err := recordcollaboration.NewDeletionAdapter(deletionRepository)
	if err != nil {
		return nil, fmt.Errorf("collaboration deletion adapter: %w", err)
	}
	deletions = append(deletions, collaborationDeletion)

	recoveries := make([]recordreadiness.RecoveryAdapter, 0, 4)
	if _, err := recorddeletion.NewRecoveryAdapter(deletionRepository); err == nil {
		recoveries = append(recoveries, recordreadiness.NewPresentRecovery(
			recordreadiness.CapabilityRecoveryRecordCore, recordreadiness.CapabilityContractVersionV1,
		))
	}
	if _, err := attachments.NewRecoveryAdapter(attachmentRepository); err == nil {
		recoveries = append(recoveries, recordreadiness.NewPresentRecovery(
			recordreadiness.CapabilityRecoveryRecordAttachments, recordreadiness.CapabilityContractVersionV1,
		))
	}
	if evidenceComposition != nil && evidenceComposition.recovery != nil {
		recoveries = append(recoveries, recordreadiness.NewPresentRecovery(
			recordreadiness.CapabilityRecoveryRecordEvidence, recordreadiness.CapabilityContractVersionV1,
		))
	}
	if activityStore, err := store.NewActivityProjectionRepository(pool); err == nil {
		if _, err := activity.NewRecoveryAdapter(activityStore); err == nil {
			recoveries = append(recoveries, recordreadiness.NewPresentRecovery(
				recordreadiness.CapabilityRecoveryRecordActivityProjection, recordreadiness.CapabilityContractVersionV1,
			))
		}
	}

	input := recordreadiness.RegistryInput{
		DeletionAdapters: deletions,
		RecoveryAdapters: recoveries,
	}
	if !nilBootstrapAdmissionGate(gate) {
		input.Membership = recordreadiness.MembershipAuthority(gate)
		input.Witness = recordreadiness.WitnessAuthority(witness)
	}
	return recordreadiness.NewRegistry(input)
}

func contentProcessorPDFBinary() string {
	if path, err := exec.LookPath("houfeng-content-processor"); err == nil && strings.TrimSpace(path) != "" {
		return path
	}
	if executable, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(executable), "houfeng-content-processor")
		if info, statErr := os.Stat(sibling); statErr == nil && !info.IsDir() {
			return sibling
		}
	}
	return "houfeng-content-processor"
}

func newAttachmentBlobStore(attachmentConfig config.AttachmentConfig) (attachments.BlobStore, error) {
	switch attachmentConfig.BlobBackend {
	case attachments.BackendKindLocal:
		return attachments.NewLocalBlobStore(attachmentConfig.BlobRoot)
	case attachments.BackendKindS3:
		client, err := minio.New(attachmentConfig.S3Endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(attachmentConfig.S3AccessKey, attachmentConfig.S3SecretKey, ""),
			Secure: attachmentConfig.S3Secure,
		})
		if err != nil {
			return nil, errors.New("create S3 client")
		}
		return attachments.NewS3BlobStore(client, attachmentConfig.S3Bucket)
	default:
		return nil, errors.New("unsupported attachment Blob backend")
	}
}

func newAttachmentScanner(attachmentConfig config.AttachmentConfig) (attachments.ProcessorScanner, error) {
	if attachmentConfig.ClamAVAddress == "" {
		return nil, nil
	}
	scanner, err := attachments.NewClamAVScanner(attachments.ClamAVScannerConfig{
		Network:          attachmentConfig.ClamAVNetwork,
		Address:          attachmentConfig.ClamAVAddress,
		DialTimeout:      attachmentConfig.ClamAVDialTimeout,
		OperationTimeout: attachmentConfig.ClamAVOperationTimeout,
		ChunkSize:        attachmentConfig.ClamAVChunkSize,
		MaxInputBytes:    attachmentConfig.Limits.MaxFileBytes,
		MaxResponseBytes: attachmentConfig.ClamAVResponseLimit,
	})
	if err != nil {
		return nil, err
	}
	return attachments.ProcessorScanner(scanner.Scan), nil
}

func newArchiveScannerReadiness(scanner attachments.ProcessorScanner) attachments.ArchiveScannerReadiness {
	if scanner == nil {
		return nil
	}
	return func(ctx context.Context) (attachments.ScannerStatus, error) {
		code, err := scanner(ctx, bytes.NewReader(nil))
		if err != nil || code != attachments.ProcessorResultCodeClean {
			return attachments.ScannerStatusUnhealthy, nil
		}
		return attachments.ScannerStatusHealthy, nil
	}
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
	if d.admitRuntime == nil {
		d.admitRuntime = func(ctx context.Context, db postgresDB) error {
			return migrate.AdmitAppACLCurrentRuntime(ctx, db.Pool())
		}
	}
	if d.ensureSearchGeneration == nil {
		d.ensureSearchGeneration = func(ctx context.Context, db postgresDB) error {
			return store.EnsurePublishedRecordSearchGeneration(ctx, db.Pool())
		}
	}
	if d.ensureActivityGeneration == nil {
		d.ensureActivityGeneration = func(ctx context.Context, db postgresDB) error {
			return store.EnsureActiveActivityProjectionGeneration(ctx, db.Pool())
		}
	}
	if d.newActivityProjectionWorker == nil {
		d.newActivityProjectionWorker = func(pool *pgxpool.Pool, ownerID string) (centerapp.Worker, error) {
			return newActivityProjectionWorker(pool, ownerID)
		}
	}
	if d.newSubjectActivityHandler == nil {
		d.newSubjectActivityHandler = newSubjectActivityHandler
	}
	if d.newVPSOverviewHandler == nil {
		d.newVPSOverviewHandler = newVPSOverviewHandler
	}
	if d.seedInitialUser == nil {
		d.seedInitialUser = func(ctx context.Context, repo auth.UserRepository, cfg config.CenterConfig) error {
			return auth.SeedInitialUserWithOptions(ctx, repo, auth.SeedInitialUserOptions{
				Username:           cfg.InitialUsername,
				Password:           cfg.InitialPassword,
				DisplayName:        cfg.InitialDisplayName,
				Now:                time.Now,
				PasswordBcryptCost: cfg.PasswordBcryptCost,
			})
		}
	}
	if d.newSessionRepository == nil {
		d.newSessionRepository = func(pool *pgxpool.Pool, hmacKey []byte) (auth.SessionRepository, error) {
			return store.NewPostgresSessionRepository(pool, hmacKey)
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
	record.OverrideRules.MonitoringInstanceLabels = ensureLegacyCoreHostSampleOverride(record.OverrideRules.MonitoringInstanceLabels)
	return record
}

func incidentSweepIntervalSeconds(interval time.Duration) int {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	seconds := int(interval.Round(time.Second) / time.Second)
	if seconds <= 0 {
		return 5
	}
	return seconds
}

func ensureLegacyCoreHostSampleOverride(rules []centersettings.MonitoringInstanceLabelOverrideRule) []centersettings.MonitoringInstanceLabelOverrideRule {
	const coreMonitoringInstanceLabel = "核心"
	coreTier := targets.FrequencyTier5s

	for i, rule := range rules {
		if rule.Label != coreMonitoringInstanceLabel {
			continue
		}
		if rule.Label == coreMonitoringInstanceLabel && rule.Overrides.HostSampleFrequencyTier != nil {
			return rules
		}

		next := append([]centersettings.MonitoringInstanceLabelOverrideRule(nil), rules...)
		next[i].Overrides.HostSampleFrequencyTier = &coreTier
		return next
	}

	legacyRule := centersettings.MonitoringInstanceLabelOverrideRule{
		Label: coreMonitoringInstanceLabel,
		Overrides: centersettings.SettingsOverrideFields{
			HostSampleFrequencyTier: &coreTier,
		},
	}

	return append([]centersettings.MonitoringInstanceLabelOverrideRule{legacyRule}, rules...)
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
