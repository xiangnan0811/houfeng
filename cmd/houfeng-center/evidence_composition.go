package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"

	centerevidence "houfeng/internal/center/evidence"
	evidenceadapters "houfeng/internal/center/evidence/adapters"
	"houfeng/internal/center/http/handlers"
	centerrecords "houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

type productionEvidenceSources struct {
	IPQuality         evidenceadapters.IPQualityEvidenceSource
	Monitoring        evidenceadapters.MonitoringEvidenceSource
	Events            evidenceadapters.MonitoringEventSource
	SubscriptionCosts evidenceadapters.SubscriptionCostSource
	CommandAudits     evidenceadapters.CommandAuditSource
}

type productionEvidenceCompositionDependencies struct {
	Pool     *pgxpool.Pool
	Gate     store.AdmissionGate
	Subjects centerrecords.SubjectAdapterRegistry
	Sources  productionEvidenceSources
}

type productionEvidenceComposition struct {
	registry       centerevidence.Registry
	repository     *store.PostgresEvidenceRepository
	authorizations *store.PostgresCurrentRecordAuthorizationSource
	application    *centerevidence.Service
	preparer       *centerevidence.RevisionPreparer
	handler        http.Handler
	worker         *centerevidence.MaintenanceWorker
	observer       *centerevidence.MaintenanceObserver
	export         *centerevidence.ExportAdapter
	deletion       *centerevidence.DeletionAdapter
	recovery       *centerevidence.RecoveryAdapter
}

func newProductionEvidenceComposition(
	dependencies productionEvidenceCompositionDependencies,
) (*productionEvidenceComposition, error) {
	if dependencies.Pool == nil || nilBootstrapAdmissionGate(dependencies.Gate) ||
		!exactProductionEvidenceSubjectKinds(dependencies.Subjects.Kinds()) ||
		nilBootstrapDependency(dependencies.Sources.IPQuality) ||
		nilBootstrapDependency(dependencies.Sources.Monitoring) ||
		nilBootstrapDependency(dependencies.Sources.Events) ||
		nilBootstrapDependency(dependencies.Sources.SubscriptionCosts) ||
		nilBootstrapDependency(dependencies.Sources.CommandAudits) {
		return nil, centerevidence.ErrEvidenceServiceUnavailable
	}

	resolver, err := evidenceadapters.NewRecordEvidenceSourceResolver(dependencies.Subjects)
	if err != nil {
		return nil, fmt.Errorf("%w: source resolver", centerevidence.ErrEvidenceServiceUnavailable)
	}
	options := evidenceadapters.AdapterOptions{}
	ipQuality, err := evidenceadapters.NewIPQualityAdapter(dependencies.Sources.IPQuality, resolver, options)
	if err != nil {
		return nil, fmt.Errorf("%w: IP quality kind", centerevidence.ErrEvidenceServiceUnavailable)
	}
	monitoringHost, err := evidenceadapters.NewMonitoringHostAdapter(dependencies.Sources.Monitoring, resolver, options)
	if err != nil {
		return nil, fmt.Errorf("%w: monitoring host kind", centerevidence.ErrEvidenceServiceUnavailable)
	}
	monitoringProbe, err := evidenceadapters.NewMonitoringProbeAdapter(dependencies.Sources.Monitoring, resolver, options)
	if err != nil {
		return nil, fmt.Errorf("%w: monitoring probe kind", centerevidence.ErrEvidenceServiceUnavailable)
	}
	monitoringEvent, err := evidenceadapters.NewMonitoringEventAdapter(dependencies.Sources.Events, resolver, options)
	if err != nil {
		return nil, fmt.Errorf("%w: monitoring event kind", centerevidence.ErrEvidenceServiceUnavailable)
	}
	subscriptionCost, err := evidenceadapters.NewSubscriptionCostAdapter(dependencies.Sources.SubscriptionCosts, resolver, options)
	if err != nil {
		return nil, fmt.Errorf("%w: subscription cost kind", centerevidence.ErrEvidenceServiceUnavailable)
	}
	commandAudit, err := evidenceadapters.NewCommandAuditAdapter(dependencies.Sources.CommandAudits, resolver, options)
	if err != nil {
		return nil, fmt.Errorf("%w: command audit kind", centerevidence.ErrEvidenceServiceUnavailable)
	}
	registry, err := centerevidence.NewRegistry([]centerevidence.Kind{
		ipQuality, monitoringHost, monitoringProbe, monitoringEvent, subscriptionCost, commandAudit,
	})
	if err != nil || !exactProductionEvidenceKindKeys(registry.Keys()) {
		return nil, fmt.Errorf("%w: exact evidence registry", centerevidence.ErrEvidenceServiceUnavailable)
	}

	subjectReadResolver := store.NewRecordSubjectReadResolver(dependencies.Subjects, nil)
	authorizations := store.NewPostgresCurrentRecordAuthorizationSource(dependencies.Pool, subjectReadResolver, dependencies.Gate)
	repository, err := store.NewPostgresEvidenceRepositoryWithReadSources(
		dependencies.Pool, dependencies.Gate, registry, authorizations, subjectReadResolver,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: evidence repository", centerevidence.ErrEvidenceServiceUnavailable)
	}
	capacity, err := centerevidence.NewCapacityEnforcer(centerevidence.DefaultCapacityPolicy(), repository)
	if err != nil {
		return nil, fmt.Errorf("%w: evidence capacity", centerevidence.ErrEvidenceServiceUnavailable)
	}
	application, err := centerevidence.NewService(registry, repository, repository, capacity)
	if err != nil {
		return nil, fmt.Errorf("%w: evidence application", centerevidence.ErrEvidenceServiceUnavailable)
	}
	preparer, err := centerevidence.NewRevisionPreparer(registry, repository, repository, repository, capacity)
	if err != nil {
		return nil, fmt.Errorf("%w: evidence preparer", centerevidence.ErrEvidenceServiceUnavailable)
	}
	exportAdapter, err := centerevidence.NewExportAdapter(registry, repository)
	if err != nil {
		return nil, fmt.Errorf("%w: evidence export", centerevidence.ErrEvidenceServiceUnavailable)
	}
	deletionAdapter, err := centerevidence.NewDeletionAdapter(repository)
	if err != nil {
		return nil, fmt.Errorf("%w: evidence deletion", centerevidence.ErrEvidenceServiceUnavailable)
	}
	recoveryAdapter, err := centerevidence.NewRecoveryAdapter(registry, repository)
	if err != nil {
		return nil, fmt.Errorf("%w: evidence recovery", centerevidence.ErrEvidenceServiceUnavailable)
	}
	observer := centerevidence.NewMaintenanceObserver()
	worker := centerevidence.NewMaintenanceWorker(repository, observer, centerevidence.MaintenanceWorkerOptions{
		Interval:          centerevidence.DefaultMaintenanceInterval,
		IntentBatchLimit:  centerevidence.MaxMaintenanceBatchSize,
		PayloadBatchLimit: centerevidence.MaxMaintenanceBatchSize,
		BacklogProbeLimit: centerevidence.MaxMaintenanceBatchSize,
		CapacityPolicy:    centerevidence.DefaultCapacityPolicy(),
		Logger:            slog.Default(),
	})
	if worker == nil {
		return nil, fmt.Errorf("%w: evidence maintenance", centerevidence.ErrEvidenceServiceUnavailable)
	}
	return &productionEvidenceComposition{
		registry: registry, repository: repository, authorizations: authorizations,
		application: application, preparer: preparer, handler: handlers.Evidence(application),
		worker: worker, observer: observer, export: exportAdapter,
		deletion: deletionAdapter, recovery: recoveryAdapter,
	}, nil
}

func exactProductionEvidenceSubjectKinds(got []centerrecords.SubjectKind) bool {
	want := []centerrecords.SubjectKind{
		centerrecords.SubjectKindMonitoringInstance,
		centerrecords.SubjectKindTarget,
		centerrecords.SubjectKindVPS,
	}
	return reflect.DeepEqual(got, want)
}

func exactProductionEvidenceKindKeys(got []centerevidence.KindKey) bool {
	want := centerevidence.KnownKindKeys()
	sort.Slice(want, func(left, right int) bool { return want[left].String() < want[right].String() })
	return reflect.DeepEqual(got, want)
}

func nilBootstrapDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return reflected.IsNil()
	default:
		return false
	}
}
